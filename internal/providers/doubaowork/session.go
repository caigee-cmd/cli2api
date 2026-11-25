package doubaowork

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// desktopParams are the query parameters that make a request look like the
// Doubao Work web client. aid must be 497858 (the web/desktop chat app); the
// other product line returns no office model menu. version_code 20800 is the
// value the official doubao.com page still uses today (register banner,
// 2026-11-25); the Doubao desktop app has its own build code (2.32.6 -> 23206)
// and is a different client.
var desktopParams = map[string]string{
	"aid":                    "497858",
	"real_aid":               "497858",
	"device_platform":        "web",
	"doubao_device_platform": "web",
	"version_code":           "20800",
	"language":               "zh",
	"samantha_web":           "1",
	"use-olympus-account":    "1",
	"pkg_type":               "release_version",
	"web_platform":           "browser",
}

const (
	userAgent         = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/147.0.0.0 Safari/537.36"
	challengeCooldown = 5 * time.Minute
	readyTTL          = 60 * time.Second
)

// session is the per-account upstream session: cookie jar, device identity,
// serialized chat requests, and challenge cooldown.
type session struct {
	client    *Client
	accountID string

	mu            sync.Mutex
	readyAt       time.Time
	pending       chan struct{}
	jar           *cookieJar
	uid           string
	name          string
	bot           string
	country       string
	profile       string
	clientInfo    ClientInfo
	clientVersion string
	launch        map[string]any

	challengeMu    sync.Mutex
	challengeUntil time.Time

	exec chan struct{} // size-1 semaphore serializing begin+headers
}

func (s *session) campaignGroup() string {
	if s.launch == nil {
		return ""
	}
	info, _ := s.launch["ug_campaign_info"].(map[string]any)
	if info == nil {
		return ""
	}
	return strings.TrimSpace(asString(info["campaign_group_code"]))
}

func (s *session) challengeLeft() time.Duration {
	s.challengeMu.Lock()
	defer s.challengeMu.Unlock()
	return time.Until(s.challengeUntil)
}

func (s *session) noteChallenge() {
	s.challengeMu.Lock()
	defer s.challengeMu.Unlock()
	until := time.Now().Add(challengeCooldown)
	if until.After(s.challengeUntil) {
		s.challengeUntil = until
	}
}

// acquire serializes chat setup for one account. It respects context
// cancellation so a waiting request can be aborted.
func (s *session) acquire(ctx context.Context) error {
	if s.exec == nil {
		s.mu.Lock()
		if s.exec == nil {
			s.exec = make(chan struct{}, 1)
		}
		s.mu.Unlock()
	}
	select {
	case s.exec <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *session) release() {
	select {
	case <-s.exec:
	default:
	}
}

func (s *session) ready(ctx context.Context) error {
	s.mu.Lock()
	if time.Since(s.readyAt) < readyTTL && s.uid != "" {
		s.mu.Unlock()
		return nil
	}
	if s.pending == nil {
		s.pending = make(chan struct{})
		pending := s.pending
		s.mu.Unlock()
		err := s.initialize(ctx)
		s.mu.Lock()
		s.pending = nil
		close(pending)
		if err != nil {
			s.readyAt = time.Time{}
		}
		s.mu.Unlock()
		return err
	}
	pending := s.pending
	s.mu.Unlock()
	select {
	case <-pending:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *session) initialize(ctx context.Context) error {
	credential, err := s.client.credential(ctx, s.accountID)
	if err != nil {
		return err
	}
	s.jar = newCookieJar(credential.Cookies)
	s.clientInfo = credential.ClientInfo
	s.clientVersion = credential.ClientVersion

	identity, err := s.json(ctx, http.MethodGet, "/passport/account/info/v2/", nil, map[string]string{
		"account_sdk_source": "web",
		// 2.2.10 is the account SDK version the official doubao.com download /
		// web page requests today (seen in its register banner, 2026-11-25).
		"sdk_version": "2.2.10",
	})
	if err != nil {
		return err
	}
	uid := strings.TrimSpace(firstNonEmpty(asString(identity["user_id_str"]), asString(identity["user_id"])))
	if uid == "" || uid == "0" || asBool(identity["is_visitor_account"]) {
		return newCodeError(codeLoginExpired, "doubao work login is invalid; re-import the Cookie")
	}
	if warning := credential.UID; warning != "" && warning != uid {
		return newCodeError(codeLoginExpired, "doubao work account changed; re-import the Cookie")
	}

	launch, err := s.json(ctx, http.MethodPost, "/alice/user/launch", map[string]any{}, nil)
	if err != nil {
		return err
	}

	bot := strings.TrimSpace(asString(launch["assistant_bot_id"]))
	if !isPositiveInt(bot) {
		bot = firstNonEmpty(credential.Bot, FallbackBot)
	}

	config, _ := launch["config"].(map[string]any)
	webID := strings.TrimSpace(asString(config["web_id"]))
	ttwid := strings.TrimSpace(asString(config["ttwid"]))
	if isPositiveInt(webID) {
		if s.clientInfo.WebID == "" {
			s.clientInfo.WebID = webID
		}
		if s.clientInfo.DeviceID == "" {
			s.clientInfo.DeviceID = webID
		}
		if s.clientInfo.TeaUUID == "" {
			s.clientInfo.TeaUUID = webID
		}
	}
	if ttwid != "" && s.clientInfo.Ttwid == "" {
		s.clientInfo.Ttwid = ttwid
	}

	s.mu.Lock()
	s.uid = uid
	s.bot = bot
	s.name = firstNonEmpty(asString(identity["screen_name"]), asString(identity["name"]), credential.UID)
	s.country = strings.TrimSpace(asString(launch["country"]))
	s.launch = launch
	s.readyAt = time.Now()
	s.mu.Unlock()
	return nil
}

func (s *session) buildURL(path string, extra map[string]string) (*url.URL, error) {
	base, err := url.Parse(s.client.baseURL)
	if err != nil {
		return nil, err
	}
	full, err := base.Parse(path)
	if err != nil {
		return nil, err
	}
	if full.Host != base.Host {
		return nil, fmt.Errorf("doubaowork request host mismatch")
	}
	query := url.Values{}
	for key, value := range desktopParams {
		query.Set(key, value)
	}
	if s.clientVersion != "" {
		query.Set("pc_version", s.clientVersion)
		query.Set("doubao_pc_version", s.clientVersion)
	}
	if s.clientInfo.WebID != "" {
		query.Set("web_id", s.clientInfo.WebID)
		query.Set("device_id", s.clientInfo.DeviceID)
		query.Set("tea_uuid", s.clientInfo.TeaUUID)
	}
	if s.clientInfo.Fingerprint != "" {
		query.Set("fp", s.clientInfo.Fingerprint)
	}
	if s.clientInfo.Ttwid != "" {
		query.Set("ttwid", s.clientInfo.Ttwid)
	}
	if s.country != "" {
		query.Set("region", s.country)
		query.Set("sys_region", s.country)
	}
	for key, value := range extra {
		query.Set(key, value)
	}
	full.RawQuery = query.Encode()
	return full, nil
}

func (s *session) request(ctx context.Context, method, path string, body any, extra map[string]string, headers map[string]string) (*http.Response, error) {
	target, err := s.buildURL(path, extra)
	if err != nil {
		return nil, err
	}
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reader = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(ctx, method, target.String(), reader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Agw-Js-Conv", "str")
	req.Header.Set("Origin", s.client.baseURL)
	req.Header.Set("Referer", s.client.baseURL+"/")
	req.Header.Set("User-Agent", userAgent)
	if s.jar != nil {
		if cookie := s.jar.headerFor(target); cookie != "" {
			req.Header.Set("Cookie", cookie)
		}
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	client, err := s.client.httpClient(ctx, s.accountID)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	if s.jar != nil {
		s.jar.absorb(target, resp.Header.Values("Set-Cookie"))
	}
	if resp.StatusCode >= 300 {
		payload, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		resp.Body.Close()
		typed := newProviderError(resp.StatusCode, string(payload))
		if typed.Kind == "auth" {
			s.mu.Lock()
			s.readyAt = time.Time{}
			s.mu.Unlock()
		}
		return nil, typed
	}
	return resp, nil
}

// json performs a JSON request and unwraps the {code,message,data} envelope.
func (s *session) json(ctx context.Context, method, path string, body any, extra map[string]string) (map[string]any, error) {
	resp, err := s.request(ctx, method, path, body, extra, map[string]string{"Accept": "application/json"})
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return nil, err
	}
	return decodeEnvelope(raw)
}

// fetchChatHTML fetches the /chat page HTML, which embeds the office model menu.
func (s *session) fetchChatHTML(ctx context.Context) (string, error) {
	resp, err := s.request(ctx, http.MethodGet, "/chat", nil, nil, map[string]string{"Accept": "text/html"})
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func decodeEnvelope(raw []byte) (map[string]any, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return nil, newCodeError(codeInvalidParam, "doubao returned a non-JSON response")
	}
	var envelope struct {
		Code    json.RawMessage `json:"code"`
		Message string          `json:"message"`
		Data    json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(trimmed, &envelope); err != nil {
		return nil, newCodeError(codeInvalidParam, "doubao returned malformed JSON")
	}
	if code := firstCode(envelope.Code); code != 0 || strings.EqualFold(strings.TrimSpace(envelope.Message), "error") {
		payload := string(trimmed)
		if len(envelope.Data) > 0 && bytes.Contains(envelope.Data, []byte("error_code")) {
			payload = string(envelope.Data)
		}
		return nil, newProviderError(502, payload)
	}
	if len(envelope.Data) == 0 || string(envelope.Data) == "null" {
		return map[string]any{}, nil
	}
	var data map[string]any
	if err := json.Unmarshal(envelope.Data, &data); err != nil {
		return nil, newCodeError(codeInvalidParam, "doubao response data is malformed")
	}
	return data, nil
}

// cookieJar is a minimal cookie store scoped to doubao.com.
type cookieJar struct {
	cookies map[string]Cookie
}

func newCookieJar(cookies []Cookie) *cookieJar {
	jar := &cookieJar{cookies: map[string]Cookie{}}
	for _, cookie := range cookies {
		jar.set(cookie)
	}
	return jar
}

func (j *cookieJar) set(cookie Cookie) {
	if strings.TrimSpace(cookie.Name) == "" {
		return
	}
	domain := strings.TrimPrefix(strings.TrimSpace(cookie.Domain), ".")
	if domain == "" {
		domain = defaultCookieDomain
	}
	path := strings.TrimSpace(cookie.Path)
	if path == "" {
		path = "/"
	}
	cookie.Domain = domain
	cookie.Path = path
	j.cookies[domain+"|"+path+"|"+cookie.Name] = cookie
}

func (j *cookieJar) headerFor(target *url.URL) string {
	host := target.Hostname()
	now := time.Now().Unix()
	parts := make([]string, 0, len(j.cookies))
	for _, cookie := range j.cookies {
		domain := strings.TrimPrefix(cookie.Domain, ".")
		if host != domain && !strings.HasSuffix(host, "."+domain) {
			continue
		}
		if cookie.Expires > 0 && cookie.Expires < now {
			continue
		}
		parts = append(parts, cookie.Name+"="+cookie.Value)
	}
	return strings.Join(parts, "; ")
}

func (j *cookieJar) absorb(target *url.URL, setCookies []string) {
	host := target.Hostname()
	for _, raw := range setCookies {
		cookie, ok := parseSetCookie(raw, host)
		if ok {
			j.set(cookie)
		}
	}
}

func parseSetCookie(raw, defaultDomain string) (Cookie, bool) {
	segments := strings.Split(raw, ";")
	if len(segments) == 0 {
		return Cookie{}, false
	}
	eq := strings.Index(segments[0], "=")
	if eq <= 0 {
		return Cookie{}, false
	}
	cookie := Cookie{
		Name:   strings.TrimSpace(segments[0][:eq]),
		Value:  strings.TrimSpace(segments[0][eq+1:]),
		Domain: defaultDomain,
		Path:   "/",
	}
	for _, attr := range segments[1:] {
		kv := strings.SplitN(attr, "=", 2)
		key := strings.ToLower(strings.TrimSpace(kv[0]))
		value := ""
		if len(kv) == 2 {
			value = strings.TrimSpace(kv[1])
		}
		switch key {
		case "domain":
			if value != "" {
				cookie.Domain = strings.TrimPrefix(value, ".")
			}
		case "path":
			if value != "" {
				cookie.Path = value
			}
		case "max-age":
			if secs, err := strconv.Atoi(value); err == nil {
				cookie.Expires = time.Now().Unix() + int64(secs)
			}
		case "expires":
			if parsed, err := http.ParseTime(value); err == nil {
				cookie.Expires = parsed.Unix()
			}
		case "secure":
			cookie.Secure = true
		case "httponly":
			cookie.HTTPOnly = true
		}
	}
	return cookie, cookie.Name != ""
}

func asString(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case json.Number:
		return typed.String()
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64)
	case int64:
		return strconv.FormatInt(typed, 10)
	case int:
		return strconv.Itoa(typed)
	default:
		return ""
	}
}

func asBool(value any) bool {
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		return strings.EqualFold(typed, "true")
	default:
		return false
	}
}
