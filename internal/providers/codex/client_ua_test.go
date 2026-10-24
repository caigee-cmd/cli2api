package codex

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/caigee-cmd/cli2api/internal/providers"
	"github.com/caigee-cmd/cli2api/internal/translate"
)

func TestIsCodexClientUserAgent(t *testing.T) {
	for _, ua := range []string{
		"codex-desktop/0.159.0 (Mac OS 26.5.2; arm64)",
		"Codex Desktop/26.803.41515",
		"codex-rs/0.154.0 (Mac OS 26.5.2; arm64)",
		"codex_cli_rs/0.154.0 (Mac OS 26.5.2; arm64)",
		"codex-tui/0.154.0 (Mac OS 26.5.2; arm64) iTerm.app/3.6.11 (codex-tui; 0.154.0)",
		"codex_exec/0.153.2 (Mac OS 26.6.2; arm64)",
	} {
		if !IsCodexClientUserAgent(ua) {
			t.Fatalf("expected Codex client UA: %q", ua)
		}
	}
	for _, ua := range []string{"", "curl/8.7.1", "Mozilla/5.0", "openai-python/1.0"} {
		if IsCodexClientUserAgent(ua) {
			t.Fatalf("unexpected Codex client UA: %q", ua)
		}
	}
}

func dualSIWCCredential() Credential {
	return Credential{
		AccessToken:  "siwc-token",
		RefreshToken: "siwc-refresh",
		AccountID:    "acct-1",
		Email:        "u@example.com",
		ExpiresAt:    time.Now().Add(time.Hour).Unix(), // SIWC lead is 5m
		AuthMethod:   AuthMethodChatGPT,
		ClientID:     "issued-client",
		DeviceID:     "dev-1",
		UseSIWC:      true,
		Outer: &OuterPartition{
			AccessToken:  "outer-token",
			RefreshToken: "outer-refresh",
			// Outer lead is 96h; keep fixtures outside that window so HTTP tests
			// do not hit the real token endpoint.
			ExpiresAt: time.Now().Add(10 * 24 * time.Hour).Unix(),
		},
	}
}

func TestEffectiveForRequestCodexClientUsesOuter(t *testing.T) {
	cred := dualSIWCCredential()
	got, err := cred.EffectiveForRequest("codex-desktop/0.159.0")
	if err != nil {
		t.Fatal(err)
	}
	if got.AccessToken != "outer-token" || got.UsesChatGPT() || got.ClientID != "" {
		t.Fatalf("codex client effective = %+v", got)
	}
	if !got.UseSIWC || got.Outer == nil || got.Outer.AccessToken != "outer-token" {
		t.Fatalf("dual metadata lost: %+v", got)
	}

	siwc, err := cred.EffectiveForRequest("curl/8.7.1")
	if err != nil {
		t.Fatal(err)
	}
	if siwc.AccessToken != "siwc-token" || !siwc.UsesChatGPT() {
		t.Fatalf("non-codex effective = %+v", siwc)
	}
}

func TestEffectiveForRequestCodexClientMissingOuterFailsClosed(t *testing.T) {
	cred := dualSIWCCredential()
	cred.Outer = nil
	_, err := cred.EffectiveForRequest("codex-tui/0.154.0")
	if err == nil {
		t.Fatal("expected 401 when outer access_token missing")
	}
	if !strings.Contains(err.Error(), "outer OAuth access_token") {
		t.Fatalf("error = %v", err)
	}
	e, ok := err.(*providers.Error)
	if !ok || e.Status != 401 {
		t.Fatalf("want providers.Error 401, got %T %v", err, err)
	}
}

func TestChatRequestCodexClientUAIgnoresUseSIWC(t *testing.T) {
	payload, err := dualSIWCCredential().Encode()
	if err != nil {
		t.Fatal(err)
	}
	client := NewClient(testStore{items: map[string][]byte{"a1": payload}})

	codexCtx := providers.WithClientUserAgent(context.Background(), "codex-desktop/0.159.0")
	cred, err := client.credential(codexCtx, "a1")
	if err != nil {
		t.Fatal(err)
	}
	req := translate.ChatRequest{Model: "gpt-5.5", Messages: []translate.ChatMessage{{Role: "user", Content: "hi"}}}
	httpReq, _, err := client.chatRequest(codexCtx, cred, req, "a1")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(httpReq.URL.String(), ChatBase) {
		t.Fatalf("codex client url = %s", httpReq.URL)
	}
	if httpReq.Header.Get("Authorization") != "Bearer outer-token" {
		t.Fatalf("Authorization = %q", httpReq.Header.Get("Authorization"))
	}
	if httpReq.Header.Get("Chatgpt-Account-Id") != "acct-1" {
		t.Fatalf("Chatgpt-Account-Id = %q", httpReq.Header.Get("Chatgpt-Account-Id"))
	}
	if httpReq.Header.Get("Originator") == "" {
		t.Fatal("Originator should be set for non-SIWC Codex client request")
	}

	curlCtx := providers.WithClientUserAgent(context.Background(), "curl/8.7.1")
	cred, err = client.credential(curlCtx, "a1")
	if err != nil {
		t.Fatal(err)
	}
	httpReq, _, err = client.chatRequest(curlCtx, cred, req, "a1")
	if err != nil {
		t.Fatal(err)
	}
	if httpReq.URL.String() != SIWCBaseURL+pathResponses {
		t.Fatalf("non-codex url = %s", httpReq.URL)
	}
	if httpReq.Header.Get("Authorization") != "Bearer siwc-token" {
		t.Fatalf("non-codex Authorization = %q", httpReq.Header.Get("Authorization"))
	}
	if httpReq.Header.Get("Chatgpt-Account-Id") != "" {
		t.Fatalf("SIWC should not send Chatgpt-Account-Id, got %q", httpReq.Header.Get("Chatgpt-Account-Id"))
	}
}

func TestDecodeCredentialRoundTripKeepsOuter(t *testing.T) {
	cred := dualSIWCCredential()
	raw, err := cred.Encode()
	if err != nil {
		t.Fatal(err)
	}
	got, err := DecodeCredential(raw)
	if err != nil {
		t.Fatal(err)
	}
	if !got.UseSIWC || got.Outer == nil || got.Outer.AccessToken != "outer-token" || got.AccessToken != "siwc-token" {
		t.Fatalf("round-trip dropped outer: %+v", got)
	}
}

func TestNormalizeCodexUpstreamPreservesExplicitParallelFalse(t *testing.T) {
	fields := map[string]json.RawMessage{
		"input":               json.RawMessage(`[]`),
		"tools":               json.RawMessage(`[{"type":"function","name":"lookup"}]`),
		"parallel_tool_calls": json.RawMessage(`false`),
	}
	if err := normalizeCodexUpstream(fields, "gpt-5.5", false, ""); err != nil {
		t.Fatal(err)
	}
	if string(fields["parallel_tool_calls"]) != "false" {
		t.Fatalf("parallel_tool_calls = %s", fields["parallel_tool_calls"])
	}

	// Absent defaults to true when tools are present.
	fields = map[string]json.RawMessage{
		"input": json.RawMessage(`[]`),
		"tools": json.RawMessage(`[{"type":"function","name":"lookup"}]`),
	}
	if err := normalizeCodexUpstream(fields, "gpt-5.5", false, ""); err != nil {
		t.Fatal(err)
	}
	if string(fields["parallel_tool_calls"]) != "true" {
		t.Fatalf("default parallel_tool_calls = %s", fields["parallel_tool_calls"])
	}
}

func TestSetChatHeadersUsesEffectiveCredential(t *testing.T) {
	cred, err := dualSIWCCredential().EffectiveForRequest("codex-rs/0.1")
	if err != nil {
		t.Fatal(err)
	}
	h := make(http.Header)
	SetChatHeaders(h, cred, "sess", true)
	if h.Get("Authorization") != "Bearer outer-token" || h.Get("Originator") == "" {
		t.Fatalf("headers = %v", h)
	}
}
