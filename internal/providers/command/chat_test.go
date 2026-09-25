package command

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/caigee-cmd/cli2api/internal/accounts"
	"github.com/caigee-cmd/cli2api/internal/providers"
	"github.com/caigee-cmd/cli2api/internal/translate"
)

func chatReq() translate.ChatRequest {
	return translate.ChatRequest{
		Model:     "deepseek/deepseek-v4-pro",
		MaxTokens: rawMessage(1024),
		Messages:  []translate.ChatMessage{{Role: "user", Content: "hello"}},
	}
}

func TestChatNonStreamAggregates(t *testing.T) {
	body := ndjson(
		`{"type":"start"}`,
		`{"type":"reasoning-start","id":"reasoning-0"}`,
		`{"type":"reasoning-delta","id":"reasoning-0","text":"th ink"}`,
		`{"type":"reasoning-end","id":"reasoning-0"}`,
		`{"type":"text-start","id":"txt-0"}`,
		`{"type":"text-delta","id":"txt-0","text":"Hel"}`,
		`{"type":"text-delta","id":"txt-0","text":"lo"}`,
		`{"type":"text-end","id":"txt-0"}`,
		`{"type":"tool-input-start","id":"call_1","toolName":"get_weather"}`,
		`{"type":"tool-input-delta","id":"call_1","delta":"{\"loc\":"}`,
		`{"type":"tool-input-delta","id":"call_1","delta":"\"Paris\"}"}`,
		`{"type":"tool-input-end","id":"call_1"}`,
		`{"type":"tool-call","toolCallId":"call_1","toolName":"get_weather","input":{"loc":"Paris"}}`,
		`{"type":"finish-step","finishReason":"tool-calls","usage":{"inputTokens":5418,"outputTokens":32,"cachedInputTokens":5376}}`,
	)
	srv, rec, reqBody := generateServer(t, 0, body)
	client, _ := newTestClient(t, srv)

	outcome, err := client.ChatNonStream(t.Context(), "acc-1", chatReq())
	if err != nil {
		t.Fatalf("ChatNonStream: %v", err)
	}
	if outcome.Content != "Hello" {
		t.Errorf("content = %q", outcome.Content)
	}
	if outcome.Reasoning != "th ink" {
		t.Errorf("reasoning = %q", outcome.Reasoning)
	}
	if outcome.FinishReason != "tool_calls" {
		t.Errorf("finish = %q", outcome.FinishReason)
	}
	if outcome.PromptTokens != 5418 || outcome.CompletionTokens != 32 {
		t.Errorf("tokens = %d/%d", outcome.PromptTokens, outcome.CompletionTokens)
	}
	if outcome.CacheReadTokens == nil || *outcome.CacheReadTokens != 5376 {
		t.Errorf("cacheRead = %v", outcome.CacheReadTokens)
	}
	if outcome.UsageSource != "upstream" {
		t.Errorf("usage source = %q", outcome.UsageSource)
	}

	var calls []struct {
		ID       string `json:"id"`
		Function struct {
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
		} `json:"function"`
	}
	if err := json.Unmarshal(outcome.ToolCalls, &calls); err != nil {
		t.Fatalf("tool calls: %v (%s)", err, outcome.ToolCalls)
	}
	if len(calls) != 1 || calls[0].ID != "call_1" || calls[0].Function.Name != "get_weather" {
		t.Fatalf("tool calls = %+v", calls)
	}
	if calls[0].Function.Arguments != `{"loc":"Paris"}` {
		t.Errorf("tool arguments = %q (the full tool-call event must win)", calls[0].Function.Arguments)
	}

	// Headers: version pin, session id, ndjson accept.
	if got := rec.header("x-command-code-version"); got != PinnedCLIVersion {
		t.Errorf("x-command-code-version = %q, want %q", got, PinnedCLIVersion)
	}
	if rec.header("x-session-id") == "" {
		t.Error("x-session-id missing")
	}
	if got := rec.header("Accept"); got != "application/x-ndjson" {
		t.Errorf("Accept = %q", got)
	}
	if got := rec.header("Authorization"); got != "Bearer user_test_key_1234567890" {
		t.Errorf("auth = %q", got)
	}
	// The model must be sent as the resolved catalog id.
	var env struct {
		Params struct {
			Model  string `json:"model"`
			Stream bool   `json:"stream"`
		} `json:"params"`
	}
	if err := json.Unmarshal([]byte(*reqBody), &env); err != nil {
		t.Fatalf("request body: %v", err)
	}
	if env.Params.Model != "deepseek/deepseek-v4-pro" || !env.Params.Stream {
		t.Errorf("params = %+v", env.Params)
	}
}

// TestChatNonStreamToolCallOnlyViaFinalEvent covers the "gotcha": per-delta
// events use id, the final tool-call event uses toolCallId. A tool call that
// only appears on the final event must still surface.
func TestChatNonStreamToolCallOnlyViaFinalEvent(t *testing.T) {
	body := ndjson(
		`{"type":"tool-call","toolCallId":"call_x","toolName":"do_thing","input":{"a":1}}`,
		`{"type":"finish-step","finishReason":"tool-calls"}`,
	)
	srv, _, _ := generateServer(t, 0, body)
	client, _ := newTestClient(t, srv)
	outcome, err := client.ChatNonStream(t.Context(), "acc-1", chatReq())
	if err != nil {
		t.Fatalf("ChatNonStream: %v", err)
	}
	if !strings.Contains(string(outcome.ToolCalls), "call_x") {
		t.Fatalf("final tool-call event ignored: %s", outcome.ToolCalls)
	}
}

func TestChatNonStreamNoTerminalEvent(t *testing.T) {
	body := ndjson(`{"type":"text-delta","id":"txt-0","text":"partial"}`)
	srv, _, _ := generateServer(t, 0, body)
	client, _ := newTestClient(t, srv)
	_, err := client.ChatNonStream(t.Context(), "acc-1", chatReq())
	if err == nil {
		t.Fatal("truncated stream must error")
	}
	var providerErr *providers.Error
	if !asProviderError(err, &providerErr) || providerErr.Kind != accounts.KindUnavailable {
		t.Fatalf("truncated stream must classify as unavailable, got %v", err)
	}
}

func TestChatNonStreamErrorEvent(t *testing.T) {
	body := ndjson(`{"type":"error","message":"Model \"x\" is not supported on this endpoint."}`)
	srv, _, _ := generateServer(t, 0, body)
	client, _ := newTestClient(t, srv)
	_, err := client.ChatNonStream(t.Context(), "acc-1", chatReq())
	if err == nil {
		t.Fatal("error event must surface")
	}
}

func TestChatStreamRewritesToSSE(t *testing.T) {
	body := ndjson(
		`{"type":"reasoning-delta","id":"r0","text":"think"}`,
		`{"type":"text-delta","id":"t0","text":"Hel"}`,
		`{"type":"text-delta","id":"t0","text":"lo"}`,
		`{"type":"tool-input-start","id":"call_1","toolName":"get_weather"}`,
		`{"type":"tool-input-delta","id":"call_1","delta":"{\"loc\":\"Paris\"}"}`,
		`{"type":"tool-call","toolCallId":"call_1","toolName":"get_weather","input":{"loc":"Paris"}}`,
		`{"type":"finish-step","finishReason":"tool-calls","usage":{"inputTokens":10,"outputTokens":2,"cachedInputTokens":4}}`,
	)
	srv, rec, _ := generateServer(t, 0, body)
	client, _ := newTestClient(t, srv)

	resp, _, err := client.ChatStream(t.Context(), "acc-1", chatReq())
	if err != nil {
		t.Fatalf("ChatStream: %v", err)
	}
	defer resp.Body.Close()
	if got := resp.Header.Get("Content-Type"); got != "text/event-stream" {
		t.Errorf("content-type = %q", got)
	}
	if got := rec.header("x-command-code-version"); got != PinnedCLIVersion {
		t.Errorf("stream request version = %q", got)
	}

	raw, _ := io.ReadAll(resp.Body)
	text := string(raw)
	if !strings.HasSuffix(text, "data: [DONE]\n\n") {
		t.Fatalf("stream must end with [DONE]: %q", text)
	}
	for _, want := range []string{
		`"reasoning_content":"think"`,
		`"content":"Hel"`,
		`"content":"lo"`,
		`"name":"get_weather"`,
		`"arguments":"{\"loc\":\"Paris\"}"`,
		`"finish_reason":"tool_calls"`,
		`"cache_read_tokens":4`,
	} {
		if !strings.Contains(text, want) {
			t.Errorf("SSE output missing %s\n%s", want, text)
		}
	}
	// The final tool-call event must not re-emit arguments the deltas already
	// streamed, or the client would see the JSON payload twice.
	if n := strings.Count(text, `"arguments":"{\"loc\":\"Paris\"}"`); n != 1 {
		t.Errorf("arguments emitted %d times, want 1\n%s", n, text)
	}
}

func TestChatStreamHTTPError(t *testing.T) {
	srv, _, _ := generateServer(t, 401, `{"success":false,"error":{"code":"UNAUTHORIZED","status":401,"message":"Invalid 'Authorization' header or token."}}`)
	client, _ := newTestClient(t, srv)
	client.SetBase(srv.URL)
	_, _, err := client.ChatStream(t.Context(), "acc-1", chatReq())
	if err == nil {
		t.Fatal("401 must error")
	}
	var providerErr *providers.Error
	if !asProviderError(err, &providerErr) || providerErr.Kind != accounts.KindAuth {
		t.Fatalf("401 should classify as auth, got %v", err)
	}
}

func TestProbe(t *testing.T) {
	srv := newWhoamiServer(t, http.StatusOK, `{"success":true,"user":{"id":"u_1","email":"a@b.c"}}`)
	client, store := newTestClient(t, srv)
	health, err := client.Probe(t.Context(), "acc-1")
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if !health.Ready || !health.Hot {
		t.Errorf("health = %+v", health)
	}
	if health.UID != "u_1" {
		t.Errorf("uid = %q", health.UID)
	}
	if store.observed["acc-1"] != "ready" {
		t.Errorf("observe status = %q", store.observed["acc-1"])
	}
}

func TestProbeBadKey(t *testing.T) {
	srv := newWhoamiServer(t, http.StatusUnauthorized, `{"success":false,"error":{"code":"UNAUTHORIZED","status":401,"message":"Invalid 'Authorization' header or token."}}`)
	client, _ := newTestClient(t, srv)
	health, err := client.Probe(t.Context(), "acc-1")
	if err != nil {
		t.Fatalf("Probe must not hard-fail on a bad key, got %v", err)
	}
	if health.Ready {
		t.Error("bad key must not be ready")
	}
	if health.LastError == "" {
		t.Error("bad key must carry a last-error message")
	}
}

func TestQuotaCredits(t *testing.T) {
	body := `{"credits":{"monthlyCredits":9.5,"purchasedCredits":1,"freeCredits":0.5,"planId":"individual-go"},` +
		`"windowLimits":{"fiveHour":{"used":3,"cap":10,"resetAt":1893456000000},"weekly":{"used":20,"cap":70,"resetAt":1893456000000}}}`
	srv := newCreditsServer(t, http.StatusOK, body)
	client, _ := newTestClient(t, srv)
	info, err := client.Quota(t.Context(), "acc-1")
	if err != nil {
		t.Fatalf("Quota: %v", err)
	}
	if info == nil {
		t.Fatal("quota must not be nil")
	}
	if info.ProviderID != "command" || info.Unit != QuotaUnit {
		t.Errorf("quota metadata = %+v", info)
	}

	byID := map[string]providers.QuotaWindow{}
	for _, w := range info.Windows {
		byID[w.ID] = w
	}
	if len(info.Windows) != 3 {
		t.Fatalf("want 5h + weekly + monthly windows, got %+v", info.Windows)
	}

	five := byID["fiveHour"]
	if five.Total != 10 || five.Used != 3 || five.Percentage != 30 {
		t.Errorf("five-hour window = %+v", five)
	}
	if five.ResetAt == "" {
		t.Error("five-hour resetAt must be formatted")
	}
	weekly := byID["weeklyLimit"]
	if weekly.Total != 70 || weekly.Used != 20 {
		t.Errorf("weekly window = %+v", weekly)
	}

	// monthly: plan individual-go = 10 total, 9.5 remaining -> 0.5 used (5%).
	monthly := byID["monthlyLimit"]
	if monthly.Total != 10 || monthly.Remaining != 9.5 || monthly.Used != 0.5 {
		t.Errorf("monthly window = %+v", monthly)
	}
	if info.Total != 10 || info.Percentage != 5 {
		t.Errorf("headline should follow the monthly plan window: %+v", info)
	}
}

func TestQuotaUnknownPlan(t *testing.T) {
	body := `{"credits":{"monthlyCredits":4,"purchasedCredits":0,"freeCredits":0,"planId":"mystery-plan"}}`
	srv := newCreditsServer(t, http.StatusOK, body)
	client, _ := newTestClient(t, srv)
	info, err := client.Quota(t.Context(), "acc-1")
	if err != nil {
		t.Fatalf("Quota: %v", err)
	}
	for _, w := range info.Windows {
		if w.ID == "monthlyLimit" {
			t.Fatalf("unknown plan must not fabricate a monthly total: %+v", w)
		}
	}
}

func TestPlanTotalCredits(t *testing.T) {
	cases := map[string]float64{
		"individual-go":       10,
		"individual-goat":     70,
		"individual-pro-v1":   80,
		"individual-provider": 15,
		"individual-max":      150,
		"individual-ultra":    300,
		"teams-pro":           40,
	}
	for plan, want := range cases {
		got, ok := planTotalCredits(plan)
		if !ok || got != want {
			t.Errorf("planTotalCredits(%q) = %v,%v want %v", plan, got, ok, want)
		}
	}
	if _, ok := planTotalCredits("nope"); ok {
		t.Error("unknown plan must not resolve")
	}
}

// TestUsagePrefersFinishStepAndFallsBackToFinish mirrors the real upstream
// stream, which sends both finish-step (usage) and finish (totalUsage).
func TestUsagePrefersFinishStepAndFallsBackToFinish(t *testing.T) {
	body := ndjson(
		`{"type":"text-delta","id":"t0","text":"OK"}`,
		`{"type":"finish-step","finishReason":"stop","usage":{"inputTokens":7586,"outputTokens":2,"cachedInputTokens":7424}}`,
		`{"type":"finish","finishReason":"stop","totalUsage":{"inputTokens":7586,"outputTokens":2,"cachedInputTokens":7424}}`,
	)
	srv, _, _ := generateServer(t, 0, body)
	client, _ := newTestClient(t, srv)
	outcome, err := client.ChatNonStream(t.Context(), "acc-1", chatReq())
	if err != nil {
		t.Fatalf("ChatNonStream: %v", err)
	}
	if outcome.PromptTokens != 7586 || outcome.CompletionTokens != 2 {
		t.Errorf("tokens = %d/%d, want 7586/2 (finish-step usage must survive the finish event)",
			outcome.PromptTokens, outcome.CompletionTokens)
	}
	if outcome.CacheReadTokens == nil || *outcome.CacheReadTokens != 7424 {
		t.Errorf("cacheRead = %v, want 7424", outcome.CacheReadTokens)
	}
}

// TestUsageFromFinishOnly covers a stream that only sends the terminal `finish`
// with `totalUsage`.
func TestUsageFromFinishOnly(t *testing.T) {
	body := ndjson(
		`{"type":"text-delta","id":"t0","text":"OK"}`,
		`{"type":"finish","finishReason":"stop","totalUsage":{"inputTokens":100,"outputTokens":7,"inputTokenDetails":{"cacheReadTokens":40}}}`,
	)
	srv, _, _ := generateServer(t, 0, body)
	client, _ := newTestClient(t, srv)
	outcome, err := client.ChatNonStream(t.Context(), "acc-1", chatReq())
	if err != nil {
		t.Fatalf("ChatNonStream: %v", err)
	}
	if outcome.PromptTokens != 100 || outcome.CompletionTokens != 7 {
		t.Errorf("tokens = %d/%d, want 100/7", outcome.PromptTokens, outcome.CompletionTokens)
	}
	if outcome.CacheReadTokens == nil || *outcome.CacheReadTokens != 40 {
		t.Errorf("cacheRead = %v, want 40 (from inputTokenDetails)", outcome.CacheReadTokens)
	}
}

// TestStreamUsageUnderFinishStep ensures the SSE usage chunk carries numbers
// even though a later finish/totalUsage event arrives.
func TestStreamUsageUnderFinishStep(t *testing.T) {
	body := ndjson(
		`{"type":"text-delta","id":"t0","text":"OK"}`,
		`{"type":"finish-step","finishReason":"stop","usage":{"inputTokens":7586,"outputTokens":2,"cachedInputTokens":7424}}`,
		`{"type":"finish","finishReason":"stop","totalUsage":{"inputTokens":7586,"outputTokens":2,"cachedInputTokens":7424}}`,
	)
	srv, _, _ := generateServer(t, 0, body)
	client, _ := newTestClient(t, srv)
	resp, _, err := client.ChatStream(t.Context(), "acc-1", chatReq())
	if err != nil {
		t.Fatalf("ChatStream: %v", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	text := string(raw)
	for _, want := range []string{`"prompt_tokens":7586`, `"completion_tokens":2`, `"cache_read_tokens":7424`} {
		if !strings.Contains(text, want) {
			t.Errorf("SSE usage missing %s\n%s", want, text)
		}
	}
}
