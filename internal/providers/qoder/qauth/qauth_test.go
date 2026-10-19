// Package qauth tests cover the core paths a PR reviewer asked for: custom
// base64 round-trip, COSY signing, AES/RSA encryption, region selection,
// PAT->jobToken exchange and refresh, busy/auth/gateway error taxonomy, and
// the SSE stream reader. All upstreams are httptest fakes; no real network.
package qauth

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	cosySignatureHash "crypto/md5"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// =============================================================================
// 1. Custom base64 encode/decode
// =============================================================================

func TestEncodeDecodeRoundtrip(t *testing.T) {
	cases := []struct {
		name string
		in   []byte
	}{
		{"empty", []byte{}},
		{"one byte", []byte{0x00}},
		{"short ascii", []byte("hi")},
		{"five bytes", []byte("hello")},
		{"long ascii", []byte("the quick brown fox jumps over the lazy dog 1234567890")},
		{"binary zeros", bytes.Repeat([]byte{0x00}, 64)},
		{"binary 0xff", bytes.Repeat([]byte{0xff}, 64)},
		{"mixed binary", []byte{0x00, 0x01, 0x02, 0xfe, 0xff, 0x7f, 0x80, 0xc2, 0xe9, 0x83}},
		{"multibyte utf8", []byte("héllo wörld 世界 🚀")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			enc := Encode(tc.in)
			// Encoded output must never be empty when the input is non-empty
			// (even a single byte yields 4 base64 chars).
			if len(tc.in) > 0 && enc == "" {
				t.Fatalf("Encode returned empty string for non-empty input")
			}
			out, err := Decode(enc)
			if err != nil {
				t.Fatalf("Decode err = %v", err)
			}
			if !bytes.Equal(out, tc.in) {
				t.Fatalf("roundtrip mismatch\nin:  %x\nout: %x", tc.in, out)
			}
		})
	}
}

func TestDecodeInvalidChar(t *testing.T) {
	// '+' and '/' belong to standard base64 but NOT to the custom alphabet,
	// so a string carrying them is illegal.
	cases := []string{
		"abc+def",
		"a/b",
		"~tilde",
		"back`tick",
	}
	for _, in := range cases {
		if _, err := Decode(in); err == nil {
			t.Fatalf("Decode(%q) = nil err, want error", in)
		}
	}
}

// =============================================================================
// 2. Signing
// =============================================================================

func TestSignDeterministic(t *testing.T) {
	d1 := "Mon, 02 Jan 2006 15:04:05 GMT"
	d2 := "Tue, 03 Jan 2006 15:04:05 GMT"

	s1a := Sign(d1)
	s1b := Sign(d1)
	s2 := Sign(d2)

	if s1a != s1b {
		t.Fatalf("Sign not deterministic: %q vs %q for same date", s1a, s1b)
	}
	if s1a == s2 {
		t.Fatalf("Sign should differ for different dates: %q for both", s1a)
	}
	if len(s1a) != 32 {
		t.Fatalf("Sign output len = %d, want 32 (md5 hex)", len(s1a))
	}
	// Verify it is literally MD5(appCode "&" secret "&" date) using the same
	// secret the package resolves. getSecret is unexported but visible inside
	// this internal test.
	manual := fmt.Sprintf("%x", cosySignatureHash.Sum([]byte(appCode+"&"+getSecret()+"&"+d1)))
	if s1a != manual {
		t.Fatalf("Sign = %q, manual md5 = %q, want match", s1a, manual)
	}
}

func TestSignRequestDeterministic(t *testing.T) {
	sig1a := SignRequest("payload", "cosyKey", "date", "body", "/path")
	sig1b := SignRequest("payload", "cosyKey", "date", "body", "/path")
	sig2 := SignRequest("payload", "cosyKey", "OTHER", "body", "/path")
	sig3 := SignRequest("payload", "cosyKey", "date", "OTHER", "/path")

	if sig1a != sig1b {
		t.Fatalf("SignRequest not deterministic: %q vs %q", sig1a, sig1b)
	}
	if sig1a == sig2 {
		t.Fatalf("SignRequest should differ when cosyDate differs")
	}
	if sig1a == sig3 {
		t.Fatalf("SignRequest should differ when body differs")
	}
	if len(sig1a) != 32 {
		t.Fatalf("SignRequest len = %d, want 32", len(sig1a))
	}
	// Verify it matches the documented concat / md5 shape.
	manual := fmt.Sprintf("%x", cosySignatureHash.Sum([]byte("payload\ncosyKey\ndate\nbody\n/path")))
	if sig1a != manual {
		t.Fatalf("SignRequest = %q, manual = %q, want match", sig1a, manual)
	}
}

// =============================================================================
// 3. AES / RSA
// =============================================================================

func TestAesEncryptDecryptRoundtrip(t *testing.T) {
	// Implementation uses AES-CBC with IV = key[:16] and PKCS7 padding. Reverse
	// it in the test to prove the ciphertext round-trips.
	key := []byte("0123456789abcdef") // 16 bytes -> AES-128, IV = key[:16]
	cases := [][]byte{
		[]byte(""),
		[]byte("a"),
		[]byte("exactly16bytes"),
		[]byte("longer than one block, spans multiple AES blocks"),
		bytes.Repeat([]byte{0xaa}, 256),
	}
	for i, plain := range cases {
		enc, err := aesEncrypt(plain, key)
		if err != nil {
			t.Fatalf("case %d: aesEncrypt err = %v", i, err)
		}
		if len(enc) == 0 {
			t.Fatalf("case %d: empty ciphertext", i)
		}
		if len(enc)%16 != 0 {
			t.Fatalf("case %d: ciphertext len %d not a block multiple", i, len(enc))
		}

		block, err := aes.NewCipher(key)
		if err != nil {
			t.Fatalf("case %d: aes.NewCipher err = %v", i, err)
		}
		dec := make([]byte, len(enc))
		cipher.NewCBCDecrypter(block, key[:16]).CryptBlocks(dec, enc)
		padLen := int(dec[len(dec)-1])
		if padLen < 1 || padLen > 16 {
			t.Fatalf("case %d: bad pad len %d", i, padLen)
		}
		dec = dec[:len(dec)-padLen]
		if !bytes.Equal(dec, plain) {
			t.Fatalf("case %d: roundtrip mismatch\nplain: %x\ndec:   %x", i, plain, dec)
		}
	}
}

func TestRsaEncryptOutputLength(t *testing.T) {
	// The server public key is a 1024-bit RSA key, so PKCS1v15 ciphertext is
	// always 128 bytes. We cannot decrypt (no private key) but the length is a
	// sufficient smoke test that encryption ran against the real key.
	for _, plain := range [][]byte{
		[]byte("0123456789abcdef"), // 16 bytes (session tempKey size)
		[]byte("short"),
	} {
		enc, err := rsaEncrypt(plain)
		if err != nil {
			t.Fatalf("rsaEncrypt err = %v", err)
		}
		if len(enc) != 128 {
			t.Fatalf("rsaEncrypt len = %d, want 128 (1024-bit key)", len(enc))
		}
	}
}

// =============================================================================
// 4. Region selection and URL composition
// =============================================================================

func TestRegionByName(t *testing.T) {
	cases := []struct {
		in   string
		want string // .Name
	}{
		{"", "cn"},
		{"cn", "cn"},
		{"CN", "cn"},
		{"  cn  ", "cn"},
		{"global", "global"},
		{"GLOBAL", "global"},
		{"intl", "global"},
		{"international", "global"},
		{"unknown", "cn"}, // unknown falls back to CN
		{"mars", "cn"},
	}
	for _, tc := range cases {
		got := RegionByName(tc.in)
		if got == nil {
			t.Fatalf("RegionByName(%q) = nil", tc.in)
		}
		if got.Name != tc.want {
			t.Fatalf("RegionByName(%q).Name = %q, want %q", tc.in, got.Name, tc.want)
		}
	}
	// Global must be flagged NeedsOpenAPIToken; CN must not.
	if !Global.NeedsOpenAPIToken {
		t.Fatal("Global.NeedsOpenAPIToken = false, want true")
	}
	if CN.NeedsOpenAPIToken {
		t.Fatal("CN.NeedsOpenAPIToken = true, want false")
	}
}

func TestNormalizeRegion(t *testing.T) {
	cases := map[string]string{
		"":              "cn",
		"cn":            "cn",
		"CN":            "cn",
		"global":        "global",
		"INTL":          "global",
		"International": "global",
		"unknown":       "cn",
	}
	for in, want := range cases {
		if got := NormalizeRegion(in); got != want {
			t.Fatalf("NormalizeRegion(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRegionURLs(t *testing.T) {
	// CN / Global real configs.
	if got := AuthURL(CN, "/p"); got != "https://gateway.qoder.com.cn/p" {
		t.Fatalf("AuthURL(CN) = %q", got)
	}
	if got := ChatURL(CN); !strings.HasPrefix(got, "https://gateway.qoder.com.cn/algo/api/v2/service/pro/sse/agent_chat_generation") {
		t.Fatalf("ChatURL(CN) = %q", got)
	}
	if got := ModelListURL(CN); got != "https://gateway.qoder.com.cn/algo/api/v2/model/list?Encode=1" {
		t.Fatalf("ModelListURL(CN) = %q", got)
	}
	if got := OpenAPIURL(CN, "/q"); got != "https://openapi.qoder.com.cn/q" {
		t.Fatalf("OpenAPIURL(CN) = %q", got)
	}
	if got := AuthURL(Global, "/p"); got != "https://api1.qoder.sh/p" {
		t.Fatalf("AuthURL(Global) = %q", got)
	}
	if got := OpenAPIURL(Global, "/q"); got != "https://openapi.qoder.sh/q" {
		t.Fatalf("OpenAPIURL(Global) = %q", got)
	}

	// A test region pointed at an httptest base must compose the same way.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()
	region := &RegionConfig{Name: "test", AuthBase: srv.URL, ChatBase: srv.URL, OpenAPIBase: srv.URL}
	if got := AuthURL(region, "/algo/api/v3/user/jobToken?Encode=1"); got != srv.URL+"/algo/api/v3/user/jobToken?Encode=1" {
		t.Fatalf("AuthURL(test) = %q", got)
	}
}

// =============================================================================
// 5. PAT -> jobToken exchange
// =============================================================================

// jobTokenTestServer builds an httptest upstream that records the decoded
// request body and returns a canned response. The returned handler decodes
// the custom-base64 request body, parses the outer/inner jobToken envelope,
// and stores the inner fields in *got for assertion.
func jobTokenTestServer(t *testing.T, status int, respBody []byte, got *jobTokenInnerStruct, gotMachine *struct{ ID, Token, Type string }) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/algo/api/v3/user/jobToken" {
			t.Errorf("unexpected path %q", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodPost {
			t.Errorf("method = %q, want POST", r.Method)
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read body err = %v", err)
			return
		}
		plain, err := Decode(string(body))
		if err != nil {
			t.Errorf("Decode request body err = %v", err)
			return
		}
		var outer jobTokenOuterStruct
		if err := json.Unmarshal(plain, &outer); err != nil {
			t.Errorf("unmarshal outer err = %v (plain=%q)", err, plain)
			return
		}
		if outer.EncodeVersion != "1" {
			t.Errorf("encodeVersion = %q, want 1", outer.EncodeVersion)
		}
		if err := json.Unmarshal([]byte(outer.Payload), got); err != nil {
			t.Errorf("unmarshal payload err = %v (payload=%q)", err, outer.Payload)
			return
		}
		if gotMachine != nil {
			gotMachine.ID = r.Header.Get("cosy-machineid")
			gotMachine.Token = r.Header.Get("cosy-machinetoken")
			gotMachine.Type = r.Header.Get("cosy-machinetype")
		}
		// Signature header must be present and deterministic with the date.
		if sig := r.Header.Get("signature"); sig == "" || len(sig) != 32 {
			t.Errorf("signature header = %q, want 32 hex chars", sig)
		}
		if r.Header.Get("date") == "" {
			t.Errorf("date header missing")
		}
		if status != 200 {
			w.WriteHeader(status)
			_, _ = w.Write(respBody)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(respBody)
	}))
}

func TestExchangeJobTokenSuccess(t *testing.T) {
	var got jobTokenInnerStruct
	var m struct{ ID, Token, Type string }
	resp, _ := json.Marshal(map[string]any{
		"id":                 "uid-123",
		"name":               "tester",
		"securityOauthToken": "sot-xyz",
		"refreshToken":       "rt-xyz",
		"expireTime":         float64(time.Now().Add(24 * time.Hour).UnixMilli()),
	})
	srv := jobTokenTestServer(t, 200, resp, &got, &m)
	defer srv.Close()

	region := &RegionConfig{Name: "test", AuthBase: srv.URL}
	res, err := ExchangeJobToken(context.Background(), "myPAT", "mid", "mtok", "mac", region)
	if err != nil {
		t.Fatalf("ExchangeJobToken err = %v", err)
	}
	if res["id"] != "uid-123" {
		t.Fatalf("response id = %v, want uid-123", res["id"])
	}
	// Verify the request carried only the PAT (exchange, not refresh).
	if got.PersonalToken != "myPAT" {
		t.Fatalf("personalToken = %q, want myPAT", got.PersonalToken)
	}
	if got.SecurityOauthToken != "" || got.RefreshToken != "" {
		t.Fatalf("exchange must not send tokens: sot=%q rt=%q", got.SecurityOauthToken, got.RefreshToken)
	}
	if got.NeedRefresh {
		t.Fatalf("needRefresh = true, want false on exchange")
	}
	if string(got.AuthInfo) != "{}" {
		t.Fatalf("authInfo = %s, want {}", got.AuthInfo)
	}
	if m.ID != "mid" || m.Token != "mtok" || m.Type != "mac" {
		t.Fatalf("machine headers = %q/%q/%q, want mid/mtok/mac", m.ID, m.Token, m.Type)
	}
}

func TestExchangeJobTokenBusy(t *testing.T) {
	var got jobTokenInnerStruct
	// 10605 envelope: admission-control rejection. The message is itself a
	// JSON string carrying retryAfterSeconds.
	innerMsg, _ := json.Marshal(map[string]any{"retryAfterSeconds": 3, "isQueued": false})
	busyBody, _ := json.Marshal(map[string]any{
		"code":    "10605",
		"message": string(innerMsg),
	})
	srv := jobTokenTestServer(t, 401, busyBody, &got, nil)
	defer srv.Close()

	region := &RegionConfig{Name: "test", AuthBase: srv.URL}
	_, err := ExchangeJobToken(context.Background(), "myPAT", "mid", "mtok", "mac", region)
	if err == nil {
		t.Fatal("ExchangeJobToken err = nil, want BusyError")
	}
	var busy *BusyError
	if !errorsAsBusy(err, &busy) {
		t.Fatalf("err type = %T, want *BusyError", err)
	}
	if busy.Code != QoderBusyCode {
		t.Fatalf("busy.Code = %q, want %q", busy.Code, QoderBusyCode)
	}
	if busy.StatusCode != 401 {
		t.Fatalf("busy.StatusCode = %d, want 401", busy.StatusCode)
	}
	if busy.RetryAfter != 3*time.Second {
		t.Fatalf("busy.RetryAfter = %s, want 3s", busy.RetryAfter)
	}
}

// =============================================================================
// 6. Refresh
// =============================================================================

func TestRefreshJobTokenSuccess(t *testing.T) {
	var got jobTokenInnerStruct
	var m struct{ ID, Token, Type string }
	resp, _ := json.Marshal(map[string]any{
		"id":                 "uid-456",
		"securityOauthToken": "sot-fresh",
		"refreshToken":       "rt-fresh",
		"expireTime":         float64(time.Now().Add(48 * time.Hour).UnixMilli()),
	})
	srv := jobTokenTestServer(t, 200, resp, &got, &m)
	defer srv.Close()

	region := &RegionConfig{Name: "test", AuthBase: srv.URL}
	_, err := RefreshJobToken(context.Background(), "myPAT", "rt-old", "sot-old", "mid", "mtok", "mac", region)
	if err != nil {
		t.Fatalf("RefreshJobToken err = %v", err)
	}
	// Refresh must forward all three tokens and set needRefresh=true.
	if got.PersonalToken != "myPAT" {
		t.Fatalf("personalToken = %q, want myPAT", got.PersonalToken)
	}
	if got.RefreshToken != "rt-old" {
		t.Fatalf("refreshToken = %q, want rt-old", got.RefreshToken)
	}
	if got.SecurityOauthToken != "sot-old" {
		t.Fatalf("securityOauthToken = %q, want sot-old", got.SecurityOauthToken)
	}
	if !got.NeedRefresh {
		t.Fatalf("needRefresh = false, want true on refresh")
	}
	if m.ID != "mid" || m.Token != "mtok" || m.Type != "mac" {
		t.Fatalf("machine headers = %q/%q/%q, want mid/mtok/mac", m.ID, m.Token, m.Type)
	}
}

func TestRefreshJobTokenAuthError(t *testing.T) {
	var got jobTokenInnerStruct
	// 401 with a non-10605 body must surface as AuthError.
	body := []byte(`{"code":"105","message":"login expired"}`)
	srv := jobTokenTestServer(t, 401, body, &got, nil)
	defer srv.Close()

	region := &RegionConfig{Name: "test", AuthBase: srv.URL}
	_, err := RefreshJobToken(context.Background(), "myPAT", "rt", "sot", "mid", "mtok", "mac", region)
	if err == nil {
		t.Fatal("RefreshJobToken err = nil, want AuthError")
	}
	var authErr *AuthError
	if !errorsAsAuth(err, &authErr) {
		t.Fatalf("err type = %T, want *AuthError", err)
	}
	if authErr.StatusCode != 401 {
		t.Fatalf("AuthError.StatusCode = %d, want 401", authErr.StatusCode)
	}
	// Error() must redact identity-valued keys (userId) but the body here has
	// none, so just confirm it carries the status.
	if !strings.Contains(authErr.Error(), "HTTP 401") {
		t.Fatalf("AuthError.Error() = %q, want 'HTTP 401' prefix", authErr.Error())
	}
}

// =============================================================================
// 7. Error classification
// =============================================================================

func TestBusyFromAuthEnvelope(t *testing.T) {
	t.Run("10605 string code", func(t *testing.T) {
		// Body may be prefixed with the upstream status ("403 {..."), as seen
		// in the wild; the parser starts at the first '{'.
		innerMsg, _ := json.Marshal(map[string]any{"retryAfterSeconds": 5})
		body, _ := json.Marshal(map[string]any{
			"code":    "10605",
			"message": string(innerMsg),
		})
		busy := busyFromAuthEnvelope(403, []byte("403 "+string(body)))
		if busy == nil {
			t.Fatal("busyFromAuthEnvelope = nil, want BusyError")
		}
		if busy.Code != QoderBusyCode {
			t.Fatalf("Code = %q, want %q", busy.Code, QoderBusyCode)
		}
		if busy.StatusCode != 403 {
			t.Fatalf("StatusCode = %d, want 403", busy.StatusCode)
		}
		if busy.RetryAfter != 5*time.Second {
			t.Fatalf("RetryAfter = %s, want 5s", busy.RetryAfter)
		}
	})
	t.Run("10605 numeric code", func(t *testing.T) {
		body := []byte(`{"code":10605,"message":"{\"retryAfterSeconds\":2}"}`)
		busy := busyFromAuthEnvelope(401, body)
		if busy == nil {
			t.Fatal("busyFromAuthEnvelope = nil for numeric code")
		}
		if busy.Code != "10605" {
			t.Fatalf("Code = %q, want 10605", busy.Code)
		}
		if busy.RetryAfter != 2*time.Second {
			t.Fatalf("RetryAfter = %s, want 2s", busy.RetryAfter)
		}
	})
	t.Run("non-busy 401 returns nil", func(t *testing.T) {
		body := []byte(`{"code":"105","message":"login expired"}`)
		if busy := busyFromAuthEnvelope(401, body); busy != nil {
			t.Fatalf("busyFromAuthEnvelope = %+v, want nil", busy)
		}
	})
	t.Run("garbage body returns nil", func(t *testing.T) {
		if busy := busyFromAuthEnvelope(401, []byte("not json at all")); busy != nil {
			t.Fatalf("busyFromAuthEnvelope = %+v, want nil", busy)
		}
	})
}

func TestRetryAfterHeader(t *testing.T) {
	cases := []struct {
		in   string
		want time.Duration
	}{
		{"", 0},
		{"0", 0},
		{"-5", 0},
		{"abc", 0},
		{"  10  ", 10 * time.Second},
		{"30", 30 * time.Second},
		{"Wed, 21 Oct 2026 07:28:00 GMT", 0}, // HTTP-date form unsupported -> 0
	}
	for _, tc := range cases {
		if got := retryAfterHeader(tc.in); got != tc.want {
			t.Fatalf("retryAfterHeader(%q) = %s, want %s", tc.in, got, tc.want)
		}
	}
}

func TestBusyMeta(t *testing.T) {
	cases := []struct {
		msg  string
		want time.Duration
	}{
		{`{"retryAfterSeconds":7}`, 7 * time.Second},
		{`{"retryAfterSeconds":0}`, 0},
		{`not json`, 0},
		{``, 0},
		{`{"retryAfterSeconds":-1}`, 0},
	}
	for _, tc := range cases {
		if got := busyMeta(tc.msg); got != tc.want {
			t.Fatalf("busyMeta(%q) = %s, want %s", tc.msg, got, tc.want)
		}
	}
}

func TestTruncateErrorBodyRedaction(t *testing.T) {
	// Identity-valued keys are replaced before the length cap.
	body := []byte(`{"userId":"u-123","userType":"teams","traceId":"t-1","requestId":"r-1","code":"bad_request","message":"invalid"}`)
	out := truncateErrorBody(body)
	for _, secret := range []string{"u-123", "teams", "t-1", "r-1"} {
		if strings.Contains(out, secret) {
			t.Fatalf("truncateErrorBody leaked %q: %s", secret, out)
		}
	}
	if !strings.Contains(out, "[REDACTED]") {
		t.Fatalf("expected [REDACTED] marker, got %s", out)
	}
	// Non-sensitive fields survive.
	if !strings.Contains(out, "bad_request") {
		t.Fatalf("non-sensitive field lost: %s", out)
	}

	// Whitespace around the colon (pretty-printed upstream) is accepted too.
	pretty := []byte(`{"userId" : "secret-pat", "code" : "x"}`)
	out2 := truncateErrorBody(pretty)
	if strings.Contains(out2, "secret-pat") {
		t.Fatalf("pretty-printed userId leaked: %s", out2)
	}

	// Truncation caps at maxErrorBodyChars on a rune boundary.
	long := bytes.Repeat([]byte("é"), 500) // multi-byte runes
	out3 := truncateErrorBody(long)
	if !strings.HasSuffix(out3, "…") {
		t.Fatalf("truncated output should end with ellipsis, got len=%d suffix=%q", len(out3), out3[len(out3)-3:])
	}
	// The truncated length must be on a rune boundary and <= cap+1 (ellipsis).
	runes := []rune(out3)
	if len(runes) > maxErrorBodyChars+1 {
		t.Fatalf("truncated rune count %d exceeds cap+1 %d", len(runes), maxErrorBodyChars+1)
	}
}

func TestRedactJSONValueTruncatedValue(t *testing.T) {
	// A truncated response body with an unterminated string value must not
	// leak the partial value: the redaction runs to the end of the text.
	trunc := `{"userId":"abc` // no closing quote
	out := redactJSONValue(trunc, "userId")
	if strings.Contains(out, "abc") {
		t.Fatalf("truncated userId value leaked: %s", out)
	}
	if !strings.Contains(out, "[REDACTED]") {
		t.Fatalf("expected redaction, got %s", out)
	}
}

// =============================================================================
// 7b. In-stream error detection helpers
// =============================================================================

func TestIsDoneLine(t *testing.T) {
	cases := []struct {
		line string
		want bool
	}{
		{"data: [DONE]", true},
		{"data:[DONE]", true},
		{`data: {"body":"[DONE]"}`, true},
		{"data: hello", false},
		{"event: finish", false},
		{"", false},
	}
	for _, tc := range cases {
		if got := isDoneLine(tc.line); got != tc.want {
			t.Fatalf("isDoneLine(%q) = %v, want %v", tc.line, got, tc.want)
		}
	}
}

func TestIsFinishEvent(t *testing.T) {
	cases := []struct {
		line string
		want bool
	}{
		{"event:finish", true},
		{"event: finish", true},
		{"event:error", false},
		{"event: done", false},
		{"data: [DONE]", false},
	}
	for _, tc := range cases {
		if got := isFinishEvent(tc.line); got != tc.want {
			t.Fatalf("isFinishEvent(%q) = %v, want %v", tc.line, got, tc.want)
		}
	}
}

func TestDetectInStreamBusyError(t *testing.T) {
	// Build a busy frame: outer envelope with statusCodeValue=401, body is a
	// JSON string carrying code 10605 and retryAfterSeconds.
	innerMsg, _ := json.Marshal(map[string]any{"retryAfterSeconds": 4, "isQueued": false})
	innerBody, _ := json.Marshal(map[string]any{"code": "10605", "message": string(innerMsg)})
	frame, _ := json.Marshal(map[string]any{
		"body":            string(innerBody),
		"statusCodeValue": 401,
	})
	line := "data: " + string(frame)
	busy := detectInStreamBusyError(line)
	if busy == nil {
		t.Fatal("detectInStreamBusyError = nil, want BusyError")
	}
	if busy.Code != QoderBusyCode {
		t.Fatalf("Code = %q, want %q", busy.Code, QoderBusyCode)
	}
	if busy.RetryAfter != 4*time.Second {
		t.Fatalf("RetryAfter = %s, want 4s", busy.RetryAfter)
	}
	if busy.StatusCode != 401 {
		t.Fatalf("StatusCode = %d, want 401", busy.StatusCode)
	}

	// Non-busy content frames must not trigger.
	if detectInStreamBusyError("data: {\"body\":\"hello\"}") != nil {
		t.Fatal("content frame misclassified as busy")
	}
	// Plain text is not busy.
	if detectInStreamBusyError("data: plain text") != nil {
		t.Fatal("plain text misclassified as busy")
	}
}

func TestDetectInStreamAuthError(t *testing.T) {
	// 401 envelope without 10605 -> auth error.
	innerBody, _ := json.Marshal(map[string]any{"code": "105", "message": "login expired"})
	frame, _ := json.Marshal(map[string]any{
		"body":            string(innerBody),
		"statusCodeValue": 401,
	})
	ok, detail := detectInStreamAuthError("data: " + string(frame))
	if !ok {
		t.Fatal("detectInStreamAuthError = false for 401 frame")
	}
	if !strings.Contains(detail, "login expired") {
		t.Fatalf("detail = %q, want to mention 'login expired'", detail)
	}

	// 10605 busy frame must NOT be an auth error.
	busyInner, _ := json.Marshal(map[string]any{"code": "10605"})
	busyFrame, _ := json.Marshal(map[string]any{
		"body":            string(busyInner),
		"statusCodeValue": 403,
	})
	ok, _ = detectInStreamAuthError("data: " + string(busyFrame))
	if ok {
		t.Fatal("10605 frame misclassified as auth error")
	}

	// Normal content frame -> not auth error.
	if ok, _ := detectInStreamAuthError(`data: {"body":"hello"}`); ok {
		t.Fatal("content frame misclassified as auth error")
	}
}

func TestDetectInStreamGatewayError(t *testing.T) {
	t.Run("envelope 429 provider outage", func(t *testing.T) {
		innerBody, _ := json.Marshal(map[string]any{"code": "provider_error", "message": "All backends failed"})
		frame, _ := json.Marshal(map[string]any{
			"body":            string(innerBody),
			"statusCodeValue": 429,
			"statusCode":      "TOO_MANY_REQUESTS",
		})
		e := detectInStreamGatewayError("data: " + string(frame))
		if e == nil {
			t.Fatal("detectInStreamGatewayError = nil, want InStreamError")
		}
		if e.Status != 429 {
			t.Fatalf("Status = %d, want 429", e.Status)
		}
		if !strings.Contains(e.Detail, "provider_error") || !strings.Contains(e.Detail, "All backends failed") {
			t.Fatalf("Detail = %q, want provider_error: All backends failed", e.Detail)
		}
	})
	t.Run("bare success:false gateway error", func(t *testing.T) {
		frame, _ := json.Marshal(map[string]any{
			"success": false,
			"msgCode": 500,
			"message": "Internal Server Error",
		})
		e := detectInStreamGatewayError("data: " + string(frame))
		if e == nil {
			t.Fatal("detectInStreamGatewayError = nil for bare success:false")
		}
		if e.Status != 500 {
			t.Fatalf("Status = %d, want 500", e.Status)
		}
		if !strings.Contains(e.Detail, "Internal Server Error") {
			t.Fatalf("Detail = %q, want Internal Server Error", e.Detail)
		}
	})
	t.Run("normal 200 envelope returns nil", func(t *testing.T) {
		frame, _ := json.Marshal(map[string]any{
			"body":            "hello world",
			"statusCodeValue": 200,
		})
		if e := detectInStreamGatewayError("data: " + string(frame)); e != nil {
			t.Fatalf("normal frame misclassified: %+v", e)
		}
	})
	t.Run("plain text returns nil", func(t *testing.T) {
		if e := detectInStreamGatewayError("data: plain"); e != nil {
			t.Fatalf("plain text misclassified: %+v", e)
		}
	})
}

// =============================================================================
// 8. SSE stream reader (OpenStreamLines)
// =============================================================================

func newTestSession(t *testing.T) *SessionContext {
	t.Helper()
	return NewSession(AuthIdentity{
		Name:  "tester",
		UID:   "uid-test",
		YxUID: "yx-test",
	}, "mid-test", "mtok-test", "mac-test")
}

func TestOpenStreamLinesHappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		fmt.Fprint(w, "data: {\"body\":\"hello\"}\n\n")
		fmt.Fprint(w, "data: {\"body\":\"world\"}\n\n")
		fmt.Fprint(w, "data: {\"body\":\"[DONE]\"}\n\n")
	}))
	defer srv.Close()

	sess := newTestSession(t)
	var lines []string
	cb := func(line string) error {
		lines = append(lines, line)
		return nil
	}
	err := OpenStreamLines(context.Background(), sess, ChatURL(sessRegion(srv.URL)), []byte(`{"prompt":"hi"}`), nil, cb)
	if err != nil {
		t.Fatalf("OpenStreamLines err = %v", err)
	}
	if len(lines) != 3 {
		t.Fatalf("callback lines = %d, want 3", len(lines))
	}
	if !strings.Contains(lines[0], "hello") {
		t.Fatalf("line[0] = %q", lines[0])
	}
}

func TestOpenStreamLinesBareDoneTerminates(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		fmt.Fprint(w, "data: hello\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	sess := newTestSession(t)
	var n int
	cb := func(line string) error {
		n++
		return nil
	}
	if err := OpenStreamLines(context.Background(), sess, ChatURL(sessRegion(srv.URL)), []byte(`{}`), nil, cb); err != nil {
		t.Fatalf("OpenStreamLines err = %v", err)
	}
	if n != 2 {
		t.Fatalf("lines = %d, want 2", n)
	}
}

func TestOpenStreamLinesBusyError(t *testing.T) {
	innerMsg, _ := json.Marshal(map[string]any{"retryAfterSeconds": 2})
	innerBody, _ := json.Marshal(map[string]any{"code": "10605", "message": string(innerMsg)})
	frame, _ := json.Marshal(map[string]any{
		"body":            string(innerBody),
		"statusCodeValue": 401,
	})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		fmt.Fprintf(w, "data: %s\n\n", frame)
	}))
	defer srv.Close()

	sess := newTestSession(t)
	cb := func(line string) error {
		t.Errorf("callback should not fire for a busy frame, got %q", line)
		return nil
	}
	err := OpenStreamLines(context.Background(), sess, ChatURL(sessRegion(srv.URL)), []byte(`{}`), nil, cb)
	if err == nil {
		t.Fatal("OpenStreamLines err = nil, want BusyError")
	}
	var busy *BusyError
	if !errorsAsBusy(err, &busy) {
		t.Fatalf("err type = %T, want *BusyError", err)
	}
	if busy.Code != QoderBusyCode {
		t.Fatalf("Code = %q, want %q", busy.Code, QoderBusyCode)
	}
	if busy.RetryAfter != 2*time.Second {
		t.Fatalf("RetryAfter = %s, want 2s", busy.RetryAfter)
	}
}

func TestOpenStreamLinesTruncatedStream(t *testing.T) {
	// Stream ends without a [DONE] marker -> the reader surfaces a truncation
	// error rather than silently succeeding.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		fmt.Fprint(w, "data: partial\n\n")
	}))
	defer srv.Close()

	sess := newTestSession(t)
	err := OpenStreamLines(context.Background(), sess, ChatURL(sessRegion(srv.URL)), []byte(`{}`), nil, func(line string) error { return nil })
	if err == nil {
		t.Fatal("OpenStreamLines err = nil, want truncation error")
	}
	if !strings.Contains(err.Error(), "without [DONE]") {
		t.Fatalf("err = %v, want 'without [DONE]'", err)
	}
}

// =============================================================================
// helpers
// =============================================================================

// sessRegion builds a RegionConfig whose ChatBase points at the httptest
// server, so ChatURL composes an in-process URL.
func sessRegion(base string) *RegionConfig {
	return &RegionConfig{Name: "test", ChatBase: base}
}

// errorsAsBusy is a tiny shim over errors.As so the test does not need to import
// errors directly in every assertion; kept inline to avoid extra imports.
func errorsAsBusy(err error, target **BusyError) bool {
	if be, ok := err.(*BusyError); ok {
		*target = be
		return true
	}
	return false
}

func errorsAsAuth(err error, target **AuthError) bool {
	if ae, ok := err.(*AuthError); ok {
		*target = ae
		return true
	}
	return false
}
