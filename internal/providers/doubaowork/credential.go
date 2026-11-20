// Package doubaowork is the in-process provider adapter for Doubao Work
// (豆包工作). It turns a pasted web-session Cookie into an OpenAI-compatible
// model source. The upstream speaks a native desktop protocol: chat streams a
// block-patch event format, the model menu is embedded in the page SSR, and the
// upstream rejects native tools/response_format/multi-turn fields, so the
// adapter encodes history and tools into a single prompt and decodes the reply.
//
// Login is import-only: the gateway runs headless, so there is no browser or
// desktop-client login round. An operator pastes a Cookie copied from a browser
// session; the adapter validates it on first probe and stores it in the generic
// credential payload table (format doubaowork-cookie-v1).
package doubaowork

import (
	"encoding/json"
	"fmt"
	"strings"
)

// CredentialFormat is the canonical storage format for a Doubao Work account.
const CredentialFormat = "doubaowork-cookie-v1"

const defaultCookieDomain = "doubao.com"

// Cookie is one stored browser cookie.
type Cookie struct {
	Name     string `json:"name"`
	Value    string `json:"value"`
	Domain   string `json:"domain,omitempty"`
	Path     string `json:"path,omitempty"`
	Expires  int64  `json:"expires,omitempty"`
	Secure   bool   `json:"secure,omitempty"`
	HTTPOnly bool   `json:"http_only,omitempty"`
}

// ClientInfo carries the desktop device identifiers the upstream validates.
// Fingerprint is the real s_v_web_id; it is never synthesized.
type ClientInfo struct {
	WebID       string `json:"web_id,omitempty"`
	DeviceID    string `json:"device_id,omitempty"`
	TeaUUID     string `json:"tea_uuid,omitempty"`
	Fingerprint string `json:"fingerprint,omitempty"`
	Ttwid       string `json:"ttwid,omitempty"`
}

// Credential is the canonical doubaowork-cookie-v1 payload stored per account.
type Credential struct {
	Format        string     `json:"format"`
	Cookies       []Cookie   `json:"cookies"`
	UID           string     `json:"uid,omitempty"`
	Bot           string     `json:"bot,omitempty"`
	ClientInfo    ClientInfo `json:"client_info,omitempty"`
	ClientVersion string     `json:"client_version,omitempty"`
}

// Ready reports whether the credential carries a usable login session.
func (c Credential) Ready() bool {
	return len(c.Cookies) > 0 && hasSessionCookie(c.Cookies)
}

// DisplayName is a best-effort label; uid/nickname are confirmed on probe.
func (c Credential) DisplayName() string {
	return strings.TrimSpace(c.UID)
}

// Encode normalizes and serializes the credential.
func (c Credential) Encode() ([]byte, error) {
	c.Format = CredentialFormat
	c.UID = strings.TrimSpace(c.UID)
	c.Bot = strings.TrimSpace(c.Bot)
	c.Cookies = normalizeCookies(c.Cookies)
	c.ClientInfo = mergeClientInfo(c.ClientInfo, ClientInfoFromCookies(c.Cookies))
	return json.Marshal(c)
}

// DecodeCredential accepts the canonical form, a JSON array of cookies, a JSON
// object carrying a cookie header, or a raw pasted cookie header string.
func DecodeCredential(payload []byte) (Credential, error) {
	trimmed := strings.TrimSpace(string(payload))
	if trimmed == "" {
		return Credential{}, fmt.Errorf("doubaowork credential is empty")
	}
	if strings.HasPrefix(trimmed, "[") {
		var cookies []Cookie
		if err := json.Unmarshal(payload, &cookies); err == nil {
			return finalizeCredential(Credential{Cookies: cookies})
		}
	}
	if strings.HasPrefix(trimmed, "{") {
		var alias struct {
			Format        string     `json:"format"`
			Cookies       []Cookie   `json:"cookies"`
			Cookie        string     `json:"cookie"`
			CookieHeader  string     `json:"cookie_header"`
			UID           string     `json:"uid"`
			Bot           string     `json:"bot"`
			ClientInfo    ClientInfo `json:"client_info"`
			ClientVersion string     `json:"client_version"`
		}
		if err := json.Unmarshal(payload, &alias); err == nil {
			cred := Credential{
				Format:        alias.Format,
				Cookies:       alias.Cookies,
				UID:           alias.UID,
				Bot:           alias.Bot,
				ClientInfo:    alias.ClientInfo,
				ClientVersion: alias.ClientVersion,
			}
			header := firstNonEmpty(alias.CookieHeader, alias.Cookie)
			if len(cred.Cookies) == 0 && strings.TrimSpace(header) != "" {
				parsed, err := CookiesFromHeader(header)
				if err != nil {
					return Credential{}, err
				}
				cred.Cookies = parsed
			}
			return finalizeCredential(cred)
		}
	}
	// Fallback: a raw cookie header string pasted directly.
	parsed, err := CookiesFromHeader(trimmed)
	if err != nil {
		return Credential{}, err
	}
	return finalizeCredential(Credential{Cookies: parsed})
}

// CookiesFromHeader parses "a=1; b=2" or "Cookie: a=1; b=2" into cookies and
// requires a doubao login session cookie.
func CookiesFromHeader(text string) ([]Cookie, error) {
	body := strings.TrimSpace(text)
	if idx := strings.Index(strings.ToLower(body), "cookie:"); idx == 0 {
		body = body[len("cookie:"):]
	}
	cookies := make([]Cookie, 0)
	for _, part := range strings.Split(body, ";") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		eq := strings.Index(part, "=")
		if eq <= 0 {
			continue
		}
		name := strings.TrimSpace(part[:eq])
		value := strings.TrimSpace(part[eq+1:])
		if name == "" {
			continue
		}
		cookies = append(cookies, Cookie{Name: name, Value: value, Domain: defaultCookieDomain, Path: "/", Secure: true})
	}
	if len(cookies) == 0 {
		return nil, fmt.Errorf("doubaowork cookie is empty")
	}
	if !hasSessionCookie(cookies) {
		return nil, fmt.Errorf("doubaowork cookie must contain sessionid or sessionid_ss")
	}
	return cookies, nil
}

// ClientInfoFromCookies infers device identifiers from cookie values. Only real
// values are used; missing identifiers stay empty.
func ClientInfoFromCookies(cookies []Cookie) ClientInfo {
	values := make(map[string]string, len(cookies))
	for _, cookie := range cookies {
		values[cookie.Name] = cookie.Value
	}
	pickNumeric := func(names ...string) string {
		for _, name := range names {
			if value := values[name]; isPositiveInt(value) {
				return value
			}
		}
		return ""
	}
	deviceID := pickNumeric("device_id", "deviceid", "s_v_web_id_number")
	webID := pickNumeric("web_id", "webid")
	teaUUID := pickNumeric("tea_uuid", "user_unique_id", "uuid")
	return ClientInfo{
		DeviceID:    deviceID,
		WebID:       firstNonEmpty(webID, deviceID),
		TeaUUID:     firstNonEmpty(teaUUID, deviceID),
		Fingerprint: firstNonEmpty(values["s_v_web_id"], values["fp"]),
		Ttwid:       firstNonEmpty(values["ttwid"]),
	}
}

// ValidateCredential is the CredentialCodec entry point.
func ValidateCredential(payload []byte) error {
	credential, err := DecodeCredential(payload)
	if err != nil {
		return err
	}
	if !credential.Ready() {
		return fmt.Errorf("doubaowork credential requires a session cookie")
	}
	return nil
}

func finalizeCredential(cred Credential) (Credential, error) {
	cred.Format = firstNonEmpty(cred.Format, CredentialFormat)
	cred.Cookies = normalizeCookies(cred.Cookies)
	if len(cred.Cookies) == 0 {
		return Credential{}, fmt.Errorf("doubaowork credential has no cookies")
	}
	if !hasSessionCookie(cred.Cookies) {
		return Credential{}, fmt.Errorf("doubaowork cookie must contain sessionid or sessionid_ss")
	}
	cred.ClientInfo = mergeClientInfo(cred.ClientInfo, ClientInfoFromCookies(cred.Cookies))
	return cred, nil
}

func normalizeCookies(cookies []Cookie) []Cookie {
	out := make([]Cookie, 0, len(cookies))
	for _, cookie := range cookies {
		name := strings.TrimSpace(cookie.Name)
		if name == "" {
			continue
		}
		cookie.Name = name
		cookie.Value = strings.TrimSpace(cookie.Value)
		if strings.TrimSpace(cookie.Domain) == "" {
			cookie.Domain = defaultCookieDomain
		}
		if strings.TrimSpace(cookie.Path) == "" {
			cookie.Path = "/"
		}
		out = append(out, cookie)
	}
	return out
}

// mergeClientInfo keeps non-empty values from primary, filling gaps from extra.
func mergeClientInfo(primary, extra ClientInfo) ClientInfo {
	return ClientInfo{
		WebID:       firstNonEmpty(primary.WebID, extra.WebID),
		DeviceID:    firstNonEmpty(primary.DeviceID, extra.DeviceID),
		TeaUUID:     firstNonEmpty(primary.TeaUUID, extra.TeaUUID),
		Fingerprint: firstNonEmpty(primary.Fingerprint, extra.Fingerprint),
		Ttwid:       firstNonEmpty(primary.Ttwid, extra.Ttwid),
	}
}

func hasSessionCookie(cookies []Cookie) bool {
	for _, cookie := range cookies {
		if cookie.Name == "sessionid" || cookie.Name == "sessionid_ss" {
			if strings.TrimSpace(cookie.Value) != "" {
				return true
			}
		}
	}
	return false
}

func isPositiveInt(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return value != "0"
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
