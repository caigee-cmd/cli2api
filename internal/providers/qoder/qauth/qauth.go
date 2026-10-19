// Package qauth implements Qoder gateway authentication, encryption, and HTTP
// client. Ported from EchoPing07/Qoder-2API-Go
// (https://github.com/EchoPing07/Qoder-2API-Go, commit
// 65a5ffff122fdbc165275a631459b9801f9e097a, auth/auth.go and auth/checkin.go),
// licensed under the MIT License (Copyright (c) 2026 EchoPing); see the NOTICE
// file in this directory. The ported surface: the cosy wire protocol (custom
// base64 + RSA + AES + MD5 signing), the COSY bearer construction, the
// PAT→jobToken exchange and refresh, /user/status, /model/list, OpenAPI
// quota/usage (CN + global OpenAPIToken), the SSE stream reader with idle
// watchdog, and the busy/auth/gateway error taxonomy. The daily check-in flow
// (internal/providers/qoder/checkin.go) follows auth/checkin.go's
// campaigns-list/claim structure.
//
// cli2api's existing internal/providers/qoder/cosy.go is a partial
// reimplementation used only by the chat-signing path; this package is the
// complete surface and is the canonical implementation going forward.
package qauth

import (
	"bufio"
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	cosySignatureHash "crypto/md5"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

// --- Region Configuration ---

type RegionConfig struct {
	Name        string
	AuthBase    string
	ChatBase    string
	OpenAPIBase string
	// NeedsOpenAPIToken marks regions whose quota API does not accept the
	// COSY session token. Global (qoder.sh) requires a separate OpenAPI bearer
	// obtained via /api/v1/jobToken/exchange; CN accepts the COSY
	// securityOauthToken directly.
	NeedsOpenAPIToken bool
}

var CN = &RegionConfig{
	Name:        "cn",
	AuthBase:    "https://gateway.qoder.com.cn",
	ChatBase:    "https://gateway.qoder.com.cn",
	OpenAPIBase: "https://openapi.qoder.com.cn",
}

// Global is the international Qoder region (qoder.sh). Chat/status go through
// api1.qoder.sh; quota/billing through openapi.qoder.sh and require a separate
// OpenAPI bearer token.
var Global = &RegionConfig{
	Name:              "global",
	AuthBase:          "https://api1.qoder.sh",
	ChatBase:          "https://api1.qoder.sh",
	OpenAPIBase:       "https://openapi.qoder.sh",
	NeedsOpenAPIToken: true,
}

// RegionByName maps a stored account region ("cn"/"global", empty = cn) to its
// configuration. Unknown values fall back to CN.
func RegionByName(name string) *RegionConfig {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "global", "intl", "international":
		return Global
	default:
		return CN
	}
}

// NormalizeRegion canonicalizes an account region for storage/display.
func NormalizeRegion(name string) string {
	if RegionByName(name).Name == "global" {
		return "global"
	}
	return "cn"
}

// Resolve returns the region for a PAT. Region is not encoded in the token, so
// callers that know the account's region should use RegionByName instead; this
// keeps the historical CN default for legacy single-PAT call sites.
func Resolve(pat string) (string, *RegionConfig) {
	return pat, CN
}

func AuthURL(region *RegionConfig, path string) string {
	return region.AuthBase + path
}

func ChatURL(region *RegionConfig) string {
	return region.ChatBase + "/algo/api/v2/service/pro/sse/agent_chat_generation?FetchKeys=llm_model_result&AgentId=agent_common&Encode=1"
}

func ModelListURL(region *RegionConfig) string {
	return region.ChatBase + "/algo/api/v2/model/list?Encode=1"
}

func OpenAPIURL(region *RegionConfig, path string) string {
	return region.OpenAPIBase + path
}

func FetchModelCatalog(ctx context.Context, sess *SessionContext, region *RegionConfig) (map[string]interface{}, error) {
	return CallGet(ctx, sess, ModelListURL(region))
}

// openAPIUserAgentVersion is the client version the global OpenAPI endpoints
// expect in the User-Agent header. It mirrors the pinned CLI (1.1.32).
const openAPIUserAgentVersion = "1.1.32"

// --- Custom Encoding (Qoder Base64 variant) ---

const customAlphabet = "_doRTgHZBKcGVjlvpC,@aFSx#DPuNJme&i*MzLOEn)sUrthbf%Y^w.(kIQyXqWA!"
const stdAlphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"

var s2c [128]byte
var c2s [128]byte
var s2cOK [128]bool
var c2sOK [128]bool

func init() {
	for i := 0; i < 64; i++ {
		s := stdAlphabet[i]
		c := customAlphabet[i]
		s2c[s] = c
		s2cOK[s] = true
		c2s[c] = s
		c2sOK[c] = true
	}
	s2c['='] = '$'
	s2cOK['='] = true
	c2s['$'] = '='
	c2sOK['$'] = true
}

func Encode(plaintext []byte) string {
	std := base64.StdEncoding.EncodeToString(plaintext)
	n := len(std)
	a := n / 3
	rearranged := make([]byte, n)
	copy(rearranged[:a], std[n-a:])
	copy(rearranged[a:n-a], std[a:n-a])
	copy(rearranged[n-a:], std[:a])
	result := make([]byte, n)
	for i, ch := range rearranged {
		if ch >= 128 || !s2cOK[ch] {
			panic(fmt.Sprintf("char out of alphabet: %q", ch))
		}
		result[i] = s2c[ch]
	}
	return string(result)
}

func Decode(encoded string) ([]byte, error) {
	n := len(encoded)
	mapped := make([]byte, n)
	for i := 0; i < n; i++ {
		ch := encoded[i]
		if ch >= 128 || !c2sOK[ch] {
			return nil, fmt.Errorf("char out of custom alphabet: %q", ch)
		}
		mapped[i] = c2s[ch]
	}
	a := n / 3
	std := make([]byte, n)
	copy(std[:a], mapped[n-a:])
	copy(std[a:n-a], mapped[a:n-a])
	copy(std[n-a:], mapped[:a])
	return base64.StdEncoding.DecodeString(string(std))
}

// --- Request Signing ---

const appCode = "cosy"
const defaultSecret = "d2FyLCB3YXIgbmV2ZXIgY2hhbmdlcw=="

var secretValue string
var secretOnce sync.Once

func getSecret() string {
	secretOnce.Do(func() {
		secretValue = os.Getenv("QODER_SIGNATURE_SECRET")
		if secretValue == "" {
			secretValue = defaultSecret
		}
	})
	return secretValue
}

func CurrentDate() string {
	return time.Now().UTC().Format("Mon, 02 Jan 2006 15:04:05 GMT")
}

func Sign(date string) string {
	s := appCode + "&" + getSecret() + "&" + date
	// nosemgrep: go.lang.security.audit.crypto.use_of_weak_crypto.use-of-md5 -- Qoder COSY protocol mandates MD5 for this wire signature.
	// #nosec G401 -- Legacy Qoder COSY wire signature; changing MD5 breaks authentication.
	h := cosySignatureHash.Sum([]byte(s))
	return fmt.Sprintf("%x", h)
}

// --- Bearer Construction ---

const serverPubKeyPEM = `-----BEGIN PUBLIC KEY-----
MIGfMA0GCSqGSIb3DQEBAQUAA4GNADCBiQKBgQDA8iMH5c02LilrsERw9t6Pv5Nc
4k6Pz1EaDicBMpdpxKduSZu5OANqUq8er4GM95omAGIOPOh+Nx0spthYA2BqGz+l
6HRkPJ7S236FZz73In/KVuLnwI8JJ2CbuJap8kvheCCZpmAWpb/cPx/3Vr/J6I17
XcW+ML9FoCI6AOvOzwIDAQAB
-----END PUBLIC KEY-----`

var rsaPubKey *rsa.PublicKey

func init() {
	block, _ := pem.Decode([]byte(serverPubKeyPEM))
	if block == nil {
		panic("failed to parse server public key PEM")
	}
	pub, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		panic(fmt.Sprintf("failed to parse server public key: %v", err))
	}
	rsaPubKey = pub.(*rsa.PublicKey)
}

type AuthIdentity struct {
	Name               string
	Aid                string
	UID                string
	YxUID              string
	OrganizationID     string
	OrganizationName   string
	UserType           string
	SecurityOauthToken string
	RefreshToken       string
}

type SessionContext struct {
	TempKey      []byte
	CosyKey      string
	Info         string
	Identity     AuthIdentity
	MachineID    string
	MachineToken string
	MachineType  string
}

func rsaEncrypt(tempKey []byte) ([]byte, error) {
	return rsa.EncryptPKCS1v15(rand.Reader, rsaPubKey, tempKey)
}

func aesEncrypt(plain, key []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	padLen := 16 - len(plain)%16
	padded := make([]byte, len(plain)+padLen)
	copy(padded, plain)
	for i := len(plain); i < len(padded); i++ {
		padded[i] = byte(padLen)
	}
	encrypted := make([]byte, len(padded))
	mode := cipher.NewCBCEncrypter(block, key[:16])
	mode.CryptBlocks(encrypted, padded)
	return encrypted, nil
}

func md5Hex(s string) string {
	// nosemgrep: go.lang.security.audit.crypto.use_of_weak_crypto.use-of-md5 -- Qoder COSY protocol mandates MD5 for this wire signature.
	// #nosec G401 -- Legacy Qoder COSY wire signature; changing MD5 breaks authentication.
	h := cosySignatureHash.Sum([]byte(s))
	return fmt.Sprintf("%x", h)
}

type authPayloadStruct struct {
	Name               string `json:"name"`
	Aid                string `json:"aid"`
	UID                string `json:"uid"`
	YxUID              string `json:"yx_uid"`
	OrganizationID     string `json:"organization_id"`
	OrganizationName   string `json:"organization_name"`
	UserType           string `json:"user_type"`
	SecurityOauthToken string `json:"security_oauth_token"`
	RefreshToken       string `json:"refresh_token"`
}

func authPayloadJSON(identity AuthIdentity) []byte {
	p := authPayloadStruct{
		Name:               identity.Name,
		Aid:                identity.Aid,
		UID:                identity.UID,
		YxUID:              identity.YxUID,
		OrganizationID:     identity.OrganizationID,
		OrganizationName:   identity.OrganizationName,
		UserType:           identity.UserType,
		SecurityOauthToken: identity.SecurityOauthToken,
		RefreshToken:       identity.RefreshToken,
	}
	b, err := marshalNoEscape(p)
	if err != nil {
		panic(err)
	}
	return b
}

// marshalNoEscape serializes v to compact JSON without HTML escaping,
// matching Python's json.dumps(v, separators=(",", ":")).
func marshalNoEscape(v interface{}) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

func NewSession(identity AuthIdentity, machineID, machineToken, machineType string) *SessionContext {
	tempKey := []byte(uuid.New().String()[:16])
	encrypted, err := rsaEncrypt(tempKey)
	if err != nil {
		panic(err)
	}
	cosyKey := base64.StdEncoding.EncodeToString(encrypted)
	aesPlain := authPayloadJSON(identity)
	aesEncrypted, err := aesEncrypt(aesPlain, tempKey)
	if err != nil {
		panic(err)
	}
	info := base64.StdEncoding.EncodeToString(aesEncrypted)
	return &SessionContext{
		TempKey:      tempKey,
		CosyKey:      cosyKey,
		Info:         info,
		Identity:     identity,
		MachineID:    machineID,
		MachineToken: machineToken,
		MachineType:  machineType,
	}
}

func SignRequest(payloadB64, cosyKey, cosyDate, body, pathWithoutAlgo string) string {
	s := payloadB64 + "\n" + cosyKey + "\n" + cosyDate + "\n" + body + "\n" + pathWithoutAlgo
	return md5Hex(s)
}

type cosyPayloadStruct struct {
	CosyVersion string `json:"cosyVersion"`
	IdeVersion  string `json:"ideVersion"`
	Info        string `json:"info"`
	RequestID   string `json:"requestId"`
	Version     string `json:"version"`
}

func BuildPayloadB64(info string) string {
	p := cosyPayloadStruct{
		CosyVersion: "0.1.43",
		IdeVersion:  "",
		Info:        info,
		RequestID:   uuid.New().String(),
		Version:     "v1",
	}
	b, err := marshalNoEscape(p)
	if err != nil {
		panic(err)
	}
	return base64.StdEncoding.EncodeToString(b)
}

func ComposeBearer(payloadB64, sig string) string {
	return "Bearer COSY." + payloadB64 + "." + sig
}

// --- AuthError ---

type AuthError struct {
	StatusCode int
	Detail     string
}

func (e *AuthError) Error() string {
	s := fmt.Sprintf("HTTP %d", e.StatusCode)
	if e.Detail != "" {
		// Detail carries the raw upstream body, which includes userId and trace
		// ids. Error() is what the bridge logs, so redact it here: doing it at
		// every construction site instead would be easy to miss, and Detail
		// itself is kept raw for callers that classify the failure.
		s += " " + truncateErrorBody([]byte(e.Detail))
	}
	return s
}

// --- Gateway busy (queue/concurrency) errors ---

// errBodyCap bounds how much of an upstream error response is read for
// classification and reporting. The former 300-byte cap could truncate a
// 10605 envelope mid-JSON, making it unparseable and misclassifying the
// rejection as an auth failure.
const errBodyCap = 4 << 10 // 4 KiB

// QoderBusyCode is the gateway's business code for queue/admission-control
// rejections. It rides HTTP 401/403 envelopes (and in-stream frames with a
// matching statusCodeValue) even though the session is perfectly valid:
//
//	{"code":"10605","message":"{\"isQueued\":false,...,\"retryAfterSeconds\":2,...}"}
//
// Treating it as an auth failure causes a pointless token refresh followed
// by an immediate retry that slams into the same concurrency window.
const QoderBusyCode = "10605"

// BusyError is a gateway queue/concurrency rejection (business code 10605).
// The session is valid; the request was refused by admission control and may
// succeed after RetryAfter.
type BusyError struct {
	StatusCode int
	Code       string
	Message    string
	RetryAfter time.Duration
}

func (e *BusyError) Error() string {
	s := fmt.Sprintf("gateway busy (%s): %s", e.Code, e.Message)
	if e.RetryAfter > 0 {
		s += fmt.Sprintf(" (retry after %s)", e.RetryAfter)
	}
	return s
}

// --- Generic upstream errors ---

// UpstreamError describes a non-2xx gateway response outside the dedicated
// session (AuthError) and admission-control (BusyError) categories. It
// preserves the HTTP status and the truncated response body so the chat
// handler can map the rejection back to the client with the real status and
// message instead of collapsing everything into an opaque 500. Typical
// sources: parameter rejections (400, e.g. an unsupported max_tokens range
// or reasoning tier) and gateway faults (5xx).
//
// RetryAfter carries the gateway's numeric Retry-After header when the
// rejection came with one (429); 0 means absent/unparseable, and the client
// response then simply omits the header.
type UpstreamError struct {
	StatusCode int
	Body       string
	RetryAfter time.Duration
}

func (e *UpstreamError) Error() string {
	return fmt.Sprintf("gateway HTTP %d: %s", e.StatusCode, e.Body)
}

// retryAfterHeader parses a numeric Retry-After response header into a
// duration. The HTTP-date form and unparsable/non-positive values yield 0;
// only 429 rejections are expected to carry the header at all.
func retryAfterHeader(h string) time.Duration {
	secs, err := strconv.Atoi(strings.TrimSpace(h))
	if err != nil || secs <= 0 {
		return 0
	}
	return time.Duration(secs) * time.Second
}

// busyMeta extracts retryAfterSeconds from the nested JSON message carried
// inside a 10605 payload, if present.
func busyMeta(msg string) time.Duration {
	var meta struct {
		RetryAfterSeconds int `json:"retryAfterSeconds"`
	}
	if strings.HasPrefix(strings.TrimSpace(msg), "{") && json.Unmarshal([]byte(msg), &meta) == nil && meta.RetryAfterSeconds > 0 {
		return time.Duration(meta.RetryAfterSeconds) * time.Second
	}
	return 0
}

// maxBusyMessageRunes caps the upstream-provided busy message echoed into
// logs and client-facing error bodies.
const maxBusyMessageRunes = 512

func truncateBusyMessage(s string) string {
	r := []rune(s)
	if len(r) <= maxBusyMessageRunes {
		return s
	}
	return string(r[:maxBusyMessageRunes]) + "…"
}

// maxErrorBodyChars caps how much of an upstream error body is embedded in an
// error message. The account loop logs these at WARN, so the payload must not
// reach the log verbatim: the quota response carries userId and trace ids.
const maxErrorBodyChars = 200

// redactedErrorKeys are JSON keys whose values are stripped from an upstream
// error body before it is embedded in an error message. Truncation alone is not
// enough: these fields sit near the start of the real payload.
var redactedErrorKeys = []string{"userId", "userType", "traceId", "traceID", "requestId"}

// truncateErrorBody returns a short, single-line, identity-free prefix of an
// upstream error body for embedding in an error message. Identity-valued JSON
// keys are replaced before the length cap, so a short-but-sensitive payload does
// not slip through as-is.
func truncateErrorBody(body []byte) string {
	s := strings.TrimSpace(string(body))
	for _, key := range redactedErrorKeys {
		s = redactJSONValue(s, key)
	}
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", " ")
	if len(s) <= maxErrorBodyChars {
		return s
	}
	// Cut on a rune boundary: a byte-wise slice through a multi-byte character
	// yields an invalid string, which then reaches the log garbage-escaped.
	cut := maxErrorBodyChars
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + "…"
}

// redactJSONValue replaces the value of a JSON field with "[REDACTED]". It works
// on the raw text (the body may not even be valid JSON), which is exactly the
// shape an upstream error page takes. A missing field is a no-op. The scan
// resumes after the replacement so a value is never rescanned.
//
// Whitespace around the colon is accepted because upstreams pretty-print
// (`"userId" : "..."`), and a missing closing quote is handled because a
// truncated response body is a realistic case: matching only the compact form
// would leak the identity value in both.
func redactJSONValue(s, key string) string {
	needle := `"` + key + `"`
	var out strings.Builder
	for {
		keyStart := strings.Index(s, needle)
		if keyStart < 0 {
			break
		}
		valueStart, ok := jsonValueStart(s, keyStart+len(needle))
		if !ok {
			// The key is not a field name here (e.g. it appears inside a string
			// value). Emit it unchanged and keep scanning past it.
			out.WriteString(s[:keyStart+len(needle)])
			s = s[keyStart+len(needle):]
			continue
		}
		out.WriteString(s[:valueStart])
		if s[valueStart] == '"' {
			out.WriteString(`"[REDACTED]"`)
		} else {
			out.WriteString("[REDACTED]")
		}
		s = s[jsonValueEnd(s, valueStart):]
	}
	out.WriteString(s)
	return out.String()
}

// jsonValueStart returns the index of the value's first byte, delimiter
// included: the opening quote for a string, the first character for a scalar.
// Everything before it (the key, whitespace, the colon) is copied verbatim. It
// reports false when the text after the key is not a `: <value>` pair.
func jsonValueStart(s string, from int) (int, bool) {
	i := from
	for i < len(s) && isJSONSpace(s[i]) {
		i++
	}
	if i >= len(s) || s[i] != ':' {
		return 0, false
	}
	i++
	for i < len(s) && isJSONSpace(s[i]) {
		i++
	}
	if i >= len(s) {
		return 0, false
	}
	return i, true
}

// jsonValueEnd returns the index of the first byte PAST the value starting at
// start. For a string that is one past its closing quote; for a scalar it is the
// next JSON delimiter. A string whose closing quote is missing (a truncated
// response) is treated as running to the end of the text so its content cannot
// leak.
func jsonValueEnd(s string, start int) int {
	if s[start] == '"' {
		for i := start + 1; i < len(s); i++ {
			if s[i] == '\\' {
				i++ // skip the escaped byte
				continue
			}
			if s[i] == '"' {
				return i + 1
			}
		}
		return len(s) // unterminated value: redact through the end
	}
	for i := start; i < len(s); i++ {
		switch s[i] {
		case ',', '}', ']', ' ', '\t', '\n', '\r':
			return i
		}
	}
	return len(s)
}

// isJSONSpace reports whether b is insignificant whitespace between JSON tokens.
func isJSONSpace(b byte) bool {
	return b == ' ' || b == '\t' || b == '\n' || b == '\r'
}

// busyCodeOf normalizes a JSON "code" field that may arrive as a string
// ("10605") or a number (10605).
func busyCodeOf(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var n float64
	if json.Unmarshal(raw, &n) == nil && n == float64(int64(n)) {
		return strconv.FormatInt(int64(n), 10)
	}
	return ""
}

// busyFromAuthEnvelope parses a 401/403 response body and returns a
// BusyError when it is a queue/concurrency rejection rather than an auth
// failure. Bodies seen in the wild are prefixed with an upstream status
// ("403 {..."), so decoding starts at the first '{'.
func busyFromAuthEnvelope(statusCode int, body []byte) *BusyError {
	s := strings.TrimSpace(string(body))
	if i := strings.IndexByte(s, '{'); i > 0 {
		s = s[i:]
	}
	var outer struct {
		Code    json.RawMessage `json:"code"`
		Message string          `json:"message"`
	}
	if json.Unmarshal([]byte(s), &outer) != nil || busyCodeOf(outer.Code) != QoderBusyCode {
		return nil
	}
	busy := &BusyError{StatusCode: statusCode, Code: QoderBusyCode}
	if outer.Message != "" {
		busy.Message = truncateBusyMessage(outer.Message)
		busy.RetryAfter = busyMeta(outer.Message)
	}
	return busy
}

// --- Signature API Client ---

func commonHeaders(machineID, machineToken, machineType, date, sig string) http.Header {
	h := http.Header{}
	h.Set("cosy-machinetoken", machineToken)
	h.Set("cosy-machinetype", machineType)
	h.Set("login-version", "v2")
	h.Set("appcode", appCode)
	h.Set("accept", "application/json")
	h.Set("accept-encoding", "identity")
	h.Set("cosy-version", "0.1.43")
	h.Set("cosy-clienttype", "5")
	h.Set("date", date)
	h.Set("signature", sig)
	h.Set("content-type", "application/json")
	h.Set("cosy-machineid", machineID)
	h.Set("user-agent", "Go-http-client/2.0")
	return h
}

func postEncoded(ctx context.Context, urlStr string, obj interface{}, machineID, machineToken, machineType string) (map[string]interface{}, error) {
	date := CurrentDate()
	sig := Sign(date)
	plain, err := marshalNoEscape(obj)
	if err != nil {
		return nil, err
	}
	body := Encode(plain)
	headers := commonHeaders(machineID, machineToken, machineType, date, sig)

	req, err := http.NewRequestWithContext(ctx, "POST", urlStr, strings.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header = headers

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		detail, _ := io.ReadAll(io.LimitReader(resp.Body, errBodyCap))
		drainBody(resp.Body)
		if resp.StatusCode == 401 || resp.StatusCode == 403 {
			if busy := busyFromAuthEnvelope(resp.StatusCode, detail); busy != nil {
				return nil, busy
			}
			return nil, &AuthError{StatusCode: resp.StatusCode, Detail: string(detail)}
		}
		return nil, &UpstreamError{StatusCode: resp.StatusCode, Body: string(truncateErrorBody(detail)), RetryAfter: retryAfterHeader(resp.Header.Get("Retry-After"))}
	}

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return result, nil
}

type jobTokenInnerStruct struct {
	PersonalToken      string          `json:"personalToken"`
	SecurityOauthToken string          `json:"securityOauthToken"`
	RefreshToken       string          `json:"refreshToken"`
	NeedRefresh        bool            `json:"needRefresh"`
	AuthInfo           json.RawMessage `json:"authInfo"`
}

type jobTokenOuterStruct struct {
	Payload       string `json:"payload"`
	EncodeVersion string `json:"encodeVersion"`
}

var emptyJSON = json.RawMessage("{}")

func requestJobToken(ctx context.Context, personalToken, refreshToken, securityOauthToken string, needRefresh bool, machineID, machineToken, machineType string, region *RegionConfig) (map[string]interface{}, error) {
	urlStr := AuthURL(region, "/algo/api/v3/user/jobToken?Encode=1")
	inner := jobTokenInnerStruct{
		PersonalToken:      personalToken,
		SecurityOauthToken: securityOauthToken,
		RefreshToken:       refreshToken,
		NeedRefresh:        needRefresh,
		AuthInfo:           emptyJSON,
	}
	innerJSON, err := marshalNoEscape(inner)
	if err != nil {
		return nil, err
	}
	outer := jobTokenOuterStruct{
		Payload:       string(innerJSON),
		EncodeVersion: "1",
	}
	return postEncoded(ctx, urlStr, outer, machineID, machineToken, machineType)
}

func ExchangeJobToken(ctx context.Context, personalToken, machineID, machineToken, machineType string, region *RegionConfig) (map[string]interface{}, error) {
	if region == nil {
		region = CN
	}
	return requestJobToken(ctx, personalToken, "", "", false, machineID, machineToken, machineType, region)
}

func RefreshJobToken(ctx context.Context, personalToken, refreshToken, securityOauthToken, machineID, machineToken, machineType string, region *RegionConfig) (map[string]interface{}, error) {
	if region == nil {
		region = CN
	}
	return requestJobToken(ctx, personalToken, refreshToken, securityOauthToken, true, machineID, machineToken, machineType, region)
}

// --- Account status (subscription tier + billing cycle) ---

// userStatusInnerStruct is the /user/status payload. Field order MUST match
// the official client's dict insertion order for signature compatibility.
// Only userId is populated; the token fields are sent empty because this call
// is authenticated by the machine-identity signature, not by the session.
type userStatusInnerStruct struct {
	UserID             string          `json:"userId"`
	PersonalToken      string          `json:"personalToken"`
	SecurityOauthToken string          `json:"securityOauthToken"`
	RefreshToken       string          `json:"refreshToken"`
	NeedRefresh        bool            `json:"needRefresh"`
	AuthInfo           json.RawMessage `json:"authInfo"`
}

// AccountStatus combines identity metadata from the gateway status endpoint
// with the authoritative allowance returned by Qoder OpenAPI. NextResetAtMs is
// the moment the current subscription allowance expires and refreshes.
type AccountStatus struct {
	// UserType is the account's real tier as reported by the gateway
	// ("teams", "personal_standard", ...). The jobToken response does NOT
	// carry this field, so callers must not infer it from there.
	UserType string
	// Plan is the raw plan identifier ("PLAN_TIER_TEAM", ...).
	Plan string
	// UserTag is the human-facing plan label ("Teams", ...).
	UserTag string
	// OrgName is the organization the account belongs to ("" for personal).
	OrgName string
	// NextResetAtMs is the subscription refresh instant in epoch millis.
	// 0 means the gateway did not report one.
	NextResetAtMs int64
	// IsQuotaExceeded reports whether the account is currently out of
	// allowance. Once quota usage has been fetched, this is the OpenAPI
	// verdict used by the official client rather than the reduced gateway
	// status field.
	IsQuotaExceeded      bool
	TotalUsagePercentage float64
	UserQuota            *Quota
	AddOnQuota           *Quota
	OrgResourcePackage   *OrgResourcePackage
}

// Quota is an authoritative credit allowance returned by Qoder OpenAPI.
// Percentage is a ratio in [0,1], matching the official client payload.
type Quota struct {
	Total      float64 `json:"total"`
	Used       float64 `json:"used"`
	Remaining  float64 `json:"remaining"`
	Percentage float64 `json:"percentage"`
	Unit       string  `json:"unit"`
	DetailURL  string  `json:"detailUrl,omitempty"`
}

// OrgResourcePackage is the shared organization credit pool. Available is
// authoritative: a positive cap does not necessarily mean the current member
// may consume it.
type OrgResourcePackage struct {
	Used       float64 `json:"used"`
	Cap        float64 `json:"cap"`
	Remaining  float64 `json:"remaining"`
	Percentage float64 `json:"percentage"`
	Available  bool    `json:"available"`
	Unit       string  `json:"unit"`
}

// QuotaUsage is the full cycle-scoped allowance returned by the same OpenAPI
// endpoint used by the official client's /usage view.
type QuotaUsage struct {
	UserID               string              `json:"userId"`
	UserType             string              `json:"userType"`
	UsageType            string              `json:"usageType"`
	TotalUsagePercentage float64             `json:"totalUsagePercentage"`
	IsQuotaExceeded      bool                `json:"isQuotaExceeded"`
	ExpiresAtMs          int64               `json:"expiresAt"`
	UpgradeURL           string              `json:"upgradeUrl"`
	UserQuota            *Quota              `json:"userQuota"`
	AddOnQuota           *Quota              `json:"addOnQuota,omitempty"`
	OrgResourcePackage   *OrgResourcePackage `json:"orgResourcePackage,omitempty"`
	IsPlanQuotaProrated  bool                `json:"isPlanQuotaProrated"`
}

// FetchUserStatus queries the account's subscription state.
//
// The endpoint is authenticated by the machine-identity signature only (no
// bearer session), so it is safe to call right after a jobToken exchange and
// before any session is constructed.
func FetchUserStatus(ctx context.Context, userID, machineID, machineToken, machineType string, region *RegionConfig) (map[string]interface{}, error) {
	if region == nil {
		region = CN
	}
	urlStr := AuthURL(region, "/algo/api/v3/user/status?Encode=1")
	inner := userStatusInnerStruct{
		UserID:      userID,
		NeedRefresh: false,
		AuthInfo:    emptyJSON,
	}
	innerJSON, err := marshalNoEscape(inner)
	if err != nil {
		return nil, err
	}
	outer := jobTokenOuterStruct{
		Payload:       string(innerJSON),
		EncodeVersion: "1",
	}
	return postEncoded(ctx, urlStr, outer, machineID, machineToken, machineType)
}

// FetchQuotaUsage retrieves the authoritative current-cycle allowance used by
// the official /usage view. The bearer is the securityOauthToken returned by
// the existing jobToken exchange; no additional token rotation is required.
func FetchQuotaUsage(ctx context.Context, bearer string, region *RegionConfig) (*QuotaUsage, error) {
	if bearer == "" {
		return nil, fmt.Errorf("quota usage requires a security OAuth token")
	}
	if region == nil {
		region = CN
	}
	urlStr := OpenAPIURL(region, "/api/v2/quota/usage")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, urlStr, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+bearer)

	started := time.Now()
	log.Printf("[auth] OpenAPI request: method=GET url=%s headers={Accept: application/json, Authorization: Bearer [REDACTED]} body=<empty>", urlStr)
	resp, err := (&http.Client{Timeout: 15 * time.Second}).Do(req)
	if err != nil {
		log.Printf("[auth] OpenAPI request failed: method=GET url=%s duration=%s error=%v", urlStr, time.Since(started), err)
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	log.Printf("[auth] OpenAPI response: method=GET url=%s status=%d duration=%s",
		urlStr, resp.StatusCode, time.Since(started))
	if resp.StatusCode != http.StatusOK {
		// The body is redacted and truncated before it reaches the error: the
		// account loop logs this at WARN, and the quota payload carries the
		// account's userId and trace ids. The status alone identifies the failure.
		return nil, fmt.Errorf("quota usage returned HTTP %d: %s", resp.StatusCode, truncateErrorBody(body))
	}
	var usage QuotaUsage
	if err := json.Unmarshal(body, &usage); err != nil {
		return nil, fmt.Errorf("decode quota usage: %w", err)
	}
	if usage.UserID == "" || usage.UserQuota == nil {
		return nil, fmt.Errorf("quota usage response shape unexpected")
	}
	return &usage, nil
}

// --- OpenAPI bearer (regions whose quota API rejects the COSY token) ---

// OpenAPIToken is the bearer pair minted by the OpenAPI jobToken exchange. The
// global region's quota/usage endpoint rejects the COSY securityOauthToken, so
// it needs this separately-issued token (and its refresh token) instead.
type OpenAPIToken struct {
	Token        string
	RefreshToken string
	// ExpiresAtMs is the token expiry in epoch millis (0 when unreported).
	ExpiresAtMs int64
}

// openAPITokenResponse is the wire shape of /api/v1/jobToken/exchange and
// /api/v1/jobToken/refresh. expires_at is an RFC3339 string on the observed
// global endpoint, while expires_in (milliseconds) is the fallback.
type openAPITokenResponse struct {
	Token              string      `json:"token"`
	AccessToken        string      `json:"access_token"`
	DeviceToken        string      `json:"device_token"`
	RefreshToken       string      `json:"refresh_token"`
	ExpiresAt          interface{} `json:"expires_at"`
	ExpiresIn          int64       `json:"expires_in"`
	RefreshTokenExpire interface{} `json:"refresh_token_expires_at"`
}

func (r openAPITokenResponse) token() string {
	if r.Token != "" {
		return r.Token
	}
	if r.AccessToken != "" {
		return r.AccessToken
	}
	return r.DeviceToken
}

// expiryMs resolves the absolute expiry from expires_at (RFC3339 string or
// epoch millis) with expires_in (milliseconds) as a fallback.
func (r openAPITokenResponse) expiryMs() int64 {
	switch v := r.ExpiresAt.(type) {
	case string:
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			return t.UnixMilli()
		}
	case float64:
		if v > 0 {
			return int64(v)
		}
	}
	if r.ExpiresIn > 0 {
		return time.Now().Add(time.Duration(r.ExpiresIn) * time.Millisecond).UnixMilli()
	}
	return 0
}

func openAPITokenRequest(ctx context.Context, urlStr string, payload interface{}, region *RegionConfig) (*OpenAPIToken, error) {
	plain, err := marshalNoEscape(payload)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, urlStr, bytes.NewReader(plain))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "qoder/"+openAPIUserAgentVersion)

	resp, err := (&http.Client{Timeout: 15 * time.Second}).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("openapi token endpoint HTTP %d: %s", resp.StatusCode, truncateErrorBody(body))
	}
	var parsed openAPITokenResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("decode openapi token: %w", err)
	}
	tok := parsed.token()
	if tok == "" {
		return nil, fmt.Errorf("openapi token response carried no token")
	}
	return &OpenAPIToken{Token: tok, RefreshToken: parsed.RefreshToken, ExpiresAtMs: parsed.expiryMs()}, nil
}

// ExchangeOpenAPIToken mints an OpenAPI bearer from a personal access token.
// Used by regions flagged NeedsOpenAPIToken.
func ExchangeOpenAPIToken(ctx context.Context, personalToken string, region *RegionConfig) (*OpenAPIToken, error) {
	if region == nil {
		region = CN
	}
	urlStr := OpenAPIURL(region, "/api/v1/jobToken/exchange")
	return openAPITokenRequest(ctx, urlStr, map[string]string{"personal_token": personalToken}, region)
}

// RefreshOpenAPIToken rotates an OpenAPI bearer using its refresh token.
func RefreshOpenAPIToken(ctx context.Context, refreshToken string, region *RegionConfig) (*OpenAPIToken, error) {
	if region == nil {
		region = CN
	}
	urlStr := OpenAPIURL(region, "/api/v1/jobToken/refresh")
	return openAPITokenRequest(ctx, urlStr, map[string]string{"refresh_token": refreshToken}, region)
}

// ParseAccountStatus converts a raw /user/status response into AccountStatus.
// It returns false when the response carries no recognizable account fields,
// letting callers keep any previously cached state instead of zeroing it out.
func ParseAccountStatus(raw map[string]interface{}) (AccountStatus, bool) {
	if raw == nil {
		return AccountStatus{}, false
	}
	userType, _ := raw["userType"].(string)
	plan, _ := raw["plan"].(string)
	if userType == "" && plan == "" && raw["nextResetAt"] == nil {
		return AccountStatus{}, false
	}
	st := AccountStatus{
		UserType: userType,
		Plan:     plan,
	}
	st.UserTag, _ = raw["userTag"].(string)
	st.OrgName, _ = raw["orgName"].(string)
	st.NextResetAtMs = toInt64Ms(raw["nextResetAt"])
	st.IsQuotaExceeded, _ = raw["isQuotaExceeded"].(bool)
	return st, true
}

// toInt64Ms coerces an epoch-millis field arriving as a JSON number.
func toInt64Ms(v interface{}) int64 {
	if f, ok := v.(float64); ok {
		return int64(f)
	}
	return 0
}

// drainBody reads and discards the remaining response body (up to a cap) so
// the underlying TCP connection can be returned to the pool for reuse.
// Must be called before resp.Body.Close() on error paths where the body was
// only partially read.
func drainBody(body io.Reader) {
	io.Copy(io.Discard, io.LimitReader(body, 1<<20))
}

// --- Bearer API Client ---

func makeCommonHeaders(sess *SessionContext, date, bearer, accept string) http.Header {
	h := http.Header{}
	h.Set("cosy-data-policy", "AGREE")
	h.Set("content-type", "application/json")
	h.Set("cosy-machinetype", sess.MachineType)
	h.Set("cosy-clienttype", "5")
	h.Set("cosy-date", date)
	h.Set("cosy-user", sess.Identity.UID)
	h.Set("cosy-key", sess.CosyKey)
	h.Set("accept", accept)
	h.Set("authorization", bearer)
	h.Set("accept-encoding", "identity")
	h.Set("cosy-version", "0.1.43")
	h.Set("cosy-machineid", sess.MachineID)
	h.Set("cosy-machinetoken", sess.MachineToken)
	h.Set("login-version", "v2")
	h.Set("user-agent", "Go-http-client/2.0")
	return h
}

func buildBearer(sess *SessionContext, date, body, pathSig string) string {
	payloadB64 := BuildPayloadB64(sess.Info)
	sig := SignRequest(payloadB64, sess.CosyKey, date, body, pathSig)
	return ComposeBearer(payloadB64, sig)
}

func sigPath(fullURL string) string {
	u, err := url.Parse(fullURL)
	if err != nil {
		return fullURL
	}
	path := u.Path
	if strings.HasPrefix(path, "/algo") {
		path = path[len("/algo"):]
	}
	return path
}

func CallPost(ctx context.Context, sess *SessionContext, fullURL string, jsonBody map[string]interface{}) (map[string]interface{}, error) {
	return call(ctx, sess, "POST", fullURL, jsonBody, nil)
}

func CallGet(ctx context.Context, sess *SessionContext, fullURL string) (map[string]interface{}, error) {
	return call(ctx, sess, "GET", fullURL, nil, nil)
}

func call(ctx context.Context, sess *SessionContext, method, fullURL string, jsonBody map[string]interface{}, extraHeaders map[string]string) (map[string]interface{}, error) {
	pathSig := sigPath(fullURL)
	body := ""
	if jsonBody != nil {
		plain, err := marshalNoEscape(jsonBody)
		if err != nil {
			return nil, err
		}
		body = Encode(plain)
	}
	date := strconv.FormatInt(time.Now().Unix(), 10)
	bearer := buildBearer(sess, date, body, pathSig)
	headers := makeCommonHeaders(sess, date, bearer, "application/json")
	for k, v := range extraHeaders {
		headers.Set(k, v)
	}

	var reqBody io.Reader
	if body != "" {
		reqBody = strings.NewReader(body)
	}

	req, err := http.NewRequestWithContext(ctx, method, fullURL, reqBody)
	if err != nil {
		return nil, err
	}
	req.Header = headers

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		detail, _ := io.ReadAll(io.LimitReader(resp.Body, errBodyCap))
		drainBody(resp.Body)
		if resp.StatusCode == 401 || resp.StatusCode == 403 {
			if busy := busyFromAuthEnvelope(resp.StatusCode, detail); busy != nil {
				return nil, busy
			}
			return nil, &AuthError{StatusCode: resp.StatusCode, Detail: string(detail)}
		}
		return nil, &UpstreamError{StatusCode: resp.StatusCode, Body: string(truncateErrorBody(detail)), RetryAfter: retryAfterHeader(resp.Header.Get("Retry-After"))}
	}

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return result, nil
}

// StreamCallback is called for each non-empty SSE line.
type StreamCallback func(line string) error

// --- Configurable stream timeouts ---

// DefaultChatHeaderTimeout bounds how long a chat stream may take to start
// responding (time to response headers). DefaultChatIdleTimeout bounds how
// long an established SSE stream may stay silent (no bytes received) before
// we give up. Without the idle bound a wedged gateway connection (TCP alive,
// no data) pins a goroutine + connection forever, and the server
// intentionally sets no WriteTimeout for SSE.
const (
	DefaultChatHeaderTimeout = 120 * time.Second
	DefaultChatIdleTimeout   = 5 * time.Minute
)

// StreamTimeouts bundles the two chat stream timeouts. Both can be changed
// at runtime via SetStreamTimeouts; new requests pick the values up
// immediately, in-flight streams keep running with the old ones.
type StreamTimeouts struct {
	Header time.Duration // wait for the chat stream to start responding
	Idle   time.Duration // max silence on an established SSE stream
}

func (t StreamTimeouts) normalized() StreamTimeouts {
	if t.Header <= 0 {
		t.Header = DefaultChatHeaderTimeout
	}
	if t.Idle <= 0 {
		t.Idle = DefaultChatIdleTimeout
	}
	return t
}

var (
	streamTimeoutsVal atomic.Pointer[StreamTimeouts]
	streamClientVal   atomic.Pointer[http.Client]
)

func init() {
	SetStreamTimeouts(DefaultChatHeaderTimeout, DefaultChatIdleTimeout)
}

// SetStreamTimeouts configures the chat stream timeouts and rebuilds the
// dedicated stream client. The client is swapped atomically so concurrent
// requests never observe a half-updated state.
func SetStreamTimeouts(header, idle time.Duration) {
	t := StreamTimeouts{Header: header, Idle: idle}.normalized()
	// Own transport (cloned from the default) isolates the long-lived SSE
	// connection pool from the short-request clients, and lets the header
	// timeout change without touching them. ResponseHeaderTimeout is the
	// only deadline: a Client.Timeout here would truncate long generations.
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.ResponseHeaderTimeout = t.Header
	streamClientVal.Store(&http.Client{Transport: transport})
	streamTimeoutsVal.Store(&t)
}

// CurrentStreamTimeouts returns the effective stream timeouts.
func CurrentStreamTimeouts() StreamTimeouts {
	if t := streamTimeoutsVal.Load(); t != nil {
		return *t
	}
	return StreamTimeouts{}.normalized()
}

// isDoneLine reports whether an SSE line carries the terminal [DONE]
// marker. The Qoder gateway wraps [DONE] inside its Encode-layer envelope
// ({"body":"[DONE]",...}) rather than emitting a bare "data: [DONE]";
// both forms are accepted so test fakes and real traffic both terminate
// cleanly.
func isDoneLine(line string) bool {
	if !strings.HasPrefix(line, "data:") {
		return false
	}
	payload := strings.TrimSpace(line[5:])
	if payload == "[DONE]" {
		return true
	}
	var env struct {
		Body string `json:"body"`
	}
	if json.Unmarshal([]byte(payload), &env) == nil && env.Body == "[DONE]" {
		return true
	}
	return false
}

// isFinishEvent reports whether an SSE line is the gateway's terminal
// "event:finish" marker. Some models (MiniMax) end their stream with this
// event instead of a [DONE] frame, so it must count as a clean termination
// just like [DONE].
func isFinishEvent(line string) bool {
	return strings.HasPrefix(line, "event:") && strings.TrimSpace(line[len("event:"):]) == "finish"
}

// isErrorEvent reports whether an SSE line is the gateway's terminal
// "event:error" marker: the upstream provider failed and the stream is
// about to end without [DONE]. Observed on qmodel_preview provider outages
// (429 provider_error "All backends failed").
func isErrorEvent(line string) bool {
	return strings.HasPrefix(line, "event:") && strings.TrimSpace(line[len("event:"):]) == "error"
}

// InStreamError describes a gateway failure delivered inside the SSE stream
// (HTTP status is still 200). Kept when seen so the EOF-without-[DONE]
// fallback reports the real cause instead of a misleading "connection
// truncated". Status carries the status embedded in the failure frame
// (statusCodeValue or msgCode); 0 means unknown. Exported so the chat
// handler can map the client response to the same status instead of a
// generic 500 when the stream failed before any content was delivered.
type InStreamError struct {
	Status int
	Detail string
}

func (e *InStreamError) Error() string {
	if e.Status > 0 {
		return fmt.Sprintf("gateway in-stream error (HTTP %d): %s", e.Status, e.Detail)
	}
	return fmt.Sprintf("gateway in-stream error: %s", e.Detail)
}

// detectInStreamGatewayError extracts a terminal gateway/provider failure
// from an SSE data line. Two shapes are recognized:
//
//  1. Envelope frame with an HTTP error status, e.g. (qmodel_preview
//     provider outage):
//     {"body":"{\"code\":\"provider_error\",\"message\":\"All backends failed\"...}",
//     "statusCodeValue":429,"statusCode":"TOO_MANY_REQUESTS"}
//
//  2. Bare success:false gateway error frame, e.g.
//     {"success":false,"msgCode":500,"message":"Internal Server Error"}
//
// A 200 OK envelope with normal chat/usage/[DONE] bodies returns nil.
func detectInStreamGatewayError(line string) *InStreamError {
	if !strings.HasPrefix(line, "data:") {
		return nil
	}
	payload := strings.TrimSpace(line[len("data:"):])
	var env struct {
		Body        string `json:"body"`
		StatusCode  string `json:"statusCode"`
		StatusValue int    `json:"statusCodeValue"`
	}
	if err := json.Unmarshal([]byte(payload), &env); err != nil {
		return nil
	}
	if env.StatusValue >= 400 {
		detail := env.Body
		if detail == "" {
			detail = env.StatusCode
		}
		return &InStreamError{Status: env.StatusValue, Detail: trimJSONMessage(detail)}
	}
	if env.Body != "" || env.StatusCode != "" {
		// A well-formed envelope with a 2xx status is a normal frame.
		return nil
	}
	var bare struct {
		Success bool   `json:"success"`
		MsgCode int    `json:"msgCode"`
		MsgInfo string `json:"msgInfo"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal([]byte(payload), &bare); err != nil {
		return nil
	}
	if !bare.Success && (bare.MsgCode >= 400 || bare.Message != "" || bare.MsgInfo != "") {
		// Only treat it as a failure when a message/code is present; a bare
		// {"success":false} with no reason is too ambiguous to fail on.
		detail := bare.Message
		if detail == "" {
			detail = bare.MsgInfo
		}
		if detail == "" {
			detail = fmt.Sprintf("msgCode %d", bare.MsgCode)
		}
		return &InStreamError{Status: bare.MsgCode, Detail: detail}
	}
	return nil
}

// trimJSONMessage pulls the "message" field out of a JSON-string body when
// present ("{\"code\":\"provider_error\",\"message\":\"All backends failed\"...}")
// so the surfaced error reads "provider_error: All backends failed" instead
// of a wall of escaped JSON.
func trimJSONMessage(body string) string {
	var m struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal([]byte(body), &m); err == nil {
		if m.Code != "" && m.Message != "" {
			return m.Code + ": " + m.Message
		}
		if m.Message != "" {
			return m.Message
		}
	}
	return strings.TrimSpace(body)
}

// OpenStreamLines sends a POST and reads SSE response line by line.
func OpenStreamLines(ctx context.Context, sess *SessionContext, fullURL string, jsonBody []byte, extraHeaders map[string]string, callback StreamCallback) error {
	pathSig := sigPath(fullURL)
	body := Encode(jsonBody)
	date := strconv.FormatInt(time.Now().Unix(), 10)
	bearer := buildBearer(sess, date, body, pathSig)
	headers := makeCommonHeaders(sess, date, bearer, "text/event-stream")
	headers.Set("cache-control", "no-cache")
	for k, v := range extraHeaders {
		headers.Set(k, v)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", fullURL, strings.NewReader(body))
	if err != nil {
		return err
	}
	req.Header = headers

	resp, err := streamClientVal.Load().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		errBody, _ := io.ReadAll(io.LimitReader(resp.Body, errBodyCap))
		drainBody(resp.Body)
		if resp.StatusCode == 401 || resp.StatusCode == 403 {
			if busy := busyFromAuthEnvelope(resp.StatusCode, errBody); busy != nil {
				return busy
			}
			return &AuthError{StatusCode: resp.StatusCode, Detail: string(errBody)}
		}
		return &UpstreamError{StatusCode: resp.StatusCode, Body: string(truncateErrorBody(errBody)), RetryAfter: retryAfterHeader(resp.Header.Get("Retry-After"))}
	}

	// Cut off silent streams: long generations are fine (lines keep
	// arriving), only a silent connection is killed. The watchdog polls a
	// last-activity timestamp instead of sharing a timer with the read loop —
	// Reset on a fired-but-undrained timer is racy under the pre-Go-1.23
	// timer semantics this module builds with.
	timeouts := CurrentStreamTimeouts()
	idleErr := fmt.Errorf("stream idle for over %s", timeouts.Idle)
	var lastLineNs atomic.Int64
	lastLineNs.Store(time.Now().UnixNano())
	var idleFired atomic.Bool
	cancelRead := func() {
		// Cancel the body read; scanner.Scan() then returns with an error.
		if c, ok := resp.Body.(interface{ SetReadDeadline(time.Time) error }); ok {
			c.SetReadDeadline(time.Now())
		} else {
			// Best effort for bodies without deadline support (HTTP/1.1):
			// closing unblocks the read with a "closed network connection"
			// error, which idleFired maps back to idleErr below.
			resp.Body.Close()
		}
	}

	// The gateway always terminates its stream with a [DONE] marker. An EOF
	// before it means the connection was cut mid-response; treating that as
	// success would silently truncate the answer and still emit our own
	// [DONE] to the client.
	sawDone := false
	var gwErr *InStreamError

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 10*1024*1024)
	done := make(chan struct{})
	defer close(done)
	go func() {
		checkEvery := timeouts.Idle / 4
		if checkEvery < 10*time.Millisecond {
			checkEvery = 10 * time.Millisecond
		}
		ticker := time.NewTicker(checkEvery)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				if time.Since(time.Unix(0, lastLineNs.Load())) >= timeouts.Idle {
					idleFired.Store(true)
					cancelRead()
					return
				}
			}
		}
	}()
	for scanner.Scan() {
		line := scanner.Text()
		lastLineNs.Store(time.Now().UnixNano())
		if line == "" {
			continue
		}
		if isDoneLine(line) {
			sawDone = true
		}
		if isFinishEvent(line) {
			// MiniMax-style termination: no [DONE] frame, just event:finish
			// followed by a telemetry frame and EOF.
			sawDone = true
		}
		if isErrorEvent(line) {
			// Provider-side failure: the stream will end without [DONE]; make
			// sure the EOF fallback reports the real cause (from the error
			// frames seen before this event) instead of "truncated".
			if gwErr == nil {
				gwErr = &InStreamError{Detail: "upstream stream failed (event:error)"}
			}
		}
		if e := detectInStreamGatewayError(line); e != nil && gwErr == nil {
			gwErr = e
		}
		if busy := detectInStreamBusyError(line); busy != nil {
			return busy
		}
		isAuthErr, detail := detectInStreamAuthError(line)
		if isAuthErr {
			return &AuthError{StatusCode: 401, Detail: detail}
		}
		if err := callback(line); err != nil {
			return err
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
	}
	if err := scanner.Err(); err != nil {
		if idleFired.Load() || errors.Is(err, os.ErrDeadlineExceeded) {
			return idleErr
		}
		return err
	}
	if !sawDone {
		// Prefer the gateway's own failure reason when it sent error frames
		// before the premature EOF; "connection truncated" is the fallback.
		if gwErr != nil {
			return gwErr
		}
		return fmt.Errorf("stream ended without [DONE] marker (connection truncated?)")
	}
	return nil
}

// detectInStreamBusyError reports whether an SSE frame carries the
// gateway's queue/concurrency rejection (code 10605) under a 401/403
// statusCodeValue. Such frames look like auth failures but are admission-
// control refusals; the caller should back off instead of refreshing tokens.
func detectInStreamBusyError(line string) *BusyError {
	s := strings.TrimSpace(line)
	if !strings.HasPrefix(s, "data:") {
		return nil
	}
	// Cheap pre-filter: busy frames always ride a statusCodeValue envelope.
	// Without this every content delta (potentially large base64 payloads)
	// would pay a full JSON unmarshal per line.
	if !strings.Contains(s, "statusCodeValue") {
		return nil
	}
	var obj map[string]interface{}
	if err := json.Unmarshal([]byte(strings.TrimSpace(s[5:])), &obj); err != nil {
		return nil
	}
	scv, ok := obj["statusCodeValue"].(float64)
	if !ok || (scv != 401 && scv != 403) {
		return nil
	}
	bodyMap, ok := parseBody(obj).(map[string]interface{})
	if !ok {
		return nil
	}
	var code string
	switch c := bodyMap["code"].(type) {
	case string:
		code = c
	case float64:
		if c == float64(int64(c)) {
			code = strconv.FormatInt(int64(c), 10)
		}
	}
	if code != QoderBusyCode {
		return nil
	}
	busy := &BusyError{StatusCode: int(scv), Code: code}
	if msg, _ := bodyMap["message"].(string); msg != "" {
		busy.Message = truncateBusyMessage(msg)
		busy.RetryAfter = busyMeta(msg)
	}
	return busy
}

// detectInStreamAuthError detects auth failures inside the SSE stream.
// Queue/concurrency rejections (code 10605) share the 401/403 envelope but
// are NOT auth failures; they are left to detectInStreamBusyError so callers
// can distinguish "token expired" (refresh + retry) from "back off".
func detectInStreamAuthError(line string) (bool, string) {
	s := strings.TrimSpace(line)
	if !strings.HasPrefix(s, "data:") {
		return false, ""
	}
	var obj map[string]interface{}
	if err := json.Unmarshal([]byte(strings.TrimSpace(s[5:])), &obj); err != nil {
		return false, ""
	}
	if detectInStreamBusyError(line) != nil {
		return false, ""
	}
	scv, ok := obj["statusCodeValue"].(float64)
	if ok && (scv == 401 || scv == 403) {
		return true, fmt.Sprintf("%v %s", scv, extractBodyMessage(obj))
	}
	msg := extractBodyMessage(obj)
	bodyObj := parseBody(obj)
	if bodyMap, ok := bodyObj.(map[string]interface{}); ok {
		code := bodyMap["code"]
		if code == "105" || code == float64(105) {
			if msg != "" {
				return true, msg
			}
			return true, "Login expired"
		}
	}
	return false, ""
}

func parseBody(obj map[string]interface{}) interface{} {
	body, ok := obj["body"]
	if !ok {
		return nil
	}
	switch b := body.(type) {
	case map[string]interface{}, []interface{}:
		return b
	case string:
		var parsed interface{}
		if err := json.Unmarshal([]byte(b), &parsed); err != nil {
			return nil
		}
		return parsed
	}
	return nil
}

func extractBodyMessage(obj map[string]interface{}) string {
	bodyObj := parseBody(obj)
	bodyMap, ok := bodyObj.(map[string]interface{})
	if !ok {
		return ""
	}
	if msg, ok := bodyMap["message"].(string); ok && msg != "" {
		return msg
	}
	if code, ok := bodyMap["code"]; ok && code != nil {
		s := fmt.Sprintf("%v", code)
		if s != "" && s != "<nil>" {
			return s
		}
	}
	return ""
}
