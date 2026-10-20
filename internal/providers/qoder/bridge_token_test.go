package qoder

import (
	"testing"
	"time"
)

// A renewal response that omits securityOauthToken/refreshToken must keep the
// previously held tokens instead of blanking them (#323).
func TestApplyJobTokenKeepsTokensWhenRefreshOmitsThem(t *testing.T) {
	b := newTestBridge(t, "fake-pat-secret-0123456789abcdef")
	b.applyJobToken(map[string]any{
		"id":         "test-uid-12345",
		"name":       "tester",
		"expireTime": float64(time.Now().Add(48 * time.Hour).UnixMilli()),
	}, "personal_standard")
	if got := b.CurrentSecurityOauth(); got != "test-security-oauth-token" {
		t.Fatalf("securityOauth = %q, want previous token kept", got)
	}
	b.mu.Lock()
	rt := b.refreshToken
	ident := *b.identity
	b.mu.Unlock()
	if rt != "test-refresh-token" || ident.RefreshToken != "test-refresh-token" || ident.SecurityOauthToken != "test-security-oauth-token" {
		t.Fatalf("tokens not preserved: rt=%q ident=%+v", rt, ident)
	}
}

// A renewal that does carry new tokens must replace the old ones.
func TestApplyJobTokenUsesNewTokens(t *testing.T) {
	b := newTestBridge(t, "fake-pat-secret-0123456789abcdef")
	b.applyJobToken(map[string]any{
		"id":                 "test-uid-12345",
		"securityOauthToken": "new-sot",
		"refreshToken":       "new-rt",
		"expireTime":         float64(time.Now().Add(48 * time.Hour).UnixMilli()),
	}, "personal_standard")
	if got := b.CurrentSecurityOauth(); got != "new-sot" {
		t.Fatalf("securityOauth = %q want new-sot", got)
	}
}
