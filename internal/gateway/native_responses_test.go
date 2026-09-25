package gateway

import (
	"bytes"
	"strings"
	"testing"

	"github.com/caigee-cmd/cli2api/internal/translate"
)

func TestRelayNativeResponsesStreamPassthrough(t *testing.T) {
	upstream := strings.Join([]string{
		`event: response.created`,
		`data: {"type":"response.created","response":{"id":"resp_1","status":"in_progress"}}`,
		``,
		`event: response.output_text.delta`,
		`data: {"type":"response.output_text.delta","delta":"hello"}`,
		``,
		`event: response.completed`,
		`data: {"type":"response.completed","response":{"status":"completed","usage":{"input_tokens":12,"output_tokens":3,"total_tokens":15},"output":[]}}`,
		``,
	}, "\n")
	var out bytes.Buffer
	stats, err := RelayNativeResponsesStream(&out, strings.NewReader(upstream), nil)
	if err != nil {
		t.Fatal(err)
	}
	// Every upstream frame must be relayed byte-for-byte (normalized newlines).
	if !strings.Contains(out.String(), `response.output_text.delta`) || !strings.Contains(out.String(), `"delta":"hello"`) {
		t.Fatalf("missing delta passthrough:\n%s", out.String())
	}
	if !strings.Contains(out.String(), `response.completed`) {
		t.Fatalf("missing terminal passthrough:\n%s", out.String())
	}
	if stats.FinishReason != "stop" {
		t.Fatalf("finish %q", stats.FinishReason)
	}
	if stats.PromptTokens == nil || *stats.PromptTokens != 12 || stats.CompletionTokens == nil || *stats.CompletionTokens != 3 {
		t.Fatalf("usage %+v", stats)
	}
	if stats.FirstTokenAt == nil {
		t.Fatal("expected first-token timing")
	}
}

func TestRelayNativeResponsesStreamIncomplete(t *testing.T) {
	upstream := "event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"delta\":\"hi\"}\n\n"
	stats, err := RelayNativeResponsesStream(&bytes.Buffer{}, strings.NewReader(upstream), nil)
	if err == nil {
		t.Fatal("expected incomplete error")
	}
	_ = stats
}

func TestRelayNativeResponsesStreamFailedIsTerminalError(t *testing.T) {
	upstream := strings.Join([]string{
		`event: response.failed`,
		`data: {"type":"response.failed","response":{"status":"failed","error":{"code":"rate_limit_exceeded","message":"slow down"}}}`,
		``,
	}, "\n")
	var out bytes.Buffer
	stats, err := RelayNativeResponsesStream(&out, strings.NewReader(upstream), nil)
	if err == nil {
		t.Fatal("failed terminal must surface as an error")
	}
	if stats.FinishReason != "error" {
		t.Fatalf("finish %q", stats.FinishReason)
	}
	if !strings.Contains(out.String(), "response.failed") {
		t.Fatalf("failure event was not relayed: %s", out.String())
	}
}

func TestRelayNativeResponsesStreamIncompleteStatus(t *testing.T) {
	upstream := strings.Join([]string{
		`event: response.incomplete`,
		`data: {"type":"response.incomplete","response":{"status":"incomplete","usage":{"input_tokens":5,"output_tokens":100}}}`,
		``,
	}, "\n")
	stats, err := RelayNativeResponsesStream(&bytes.Buffer{}, strings.NewReader(upstream), nil)
	if err != nil {
		t.Fatal(err)
	}
	if stats.FinishReason != "length" {
		t.Fatalf("finish %q", stats.FinishReason)
	}
}

func TestRelayNativeResponsesStreamRestoresNamespacedToolNames(t *testing.T) {
	upstream := strings.Join([]string{
		`event: response.output_item.added`,
		`data: {"type":"response.output_item.added","sequence_number":3,"item":{"id":"fc_1","type":"function_call","name":"mcp__fastctx__glob","call_id":"call_1","arguments":""}}`,
		``,
		`event: response.output_item.done`,
		`data: {"type":"response.output_item.done","item":{"id":"fc_1","type":"function_call","name":"mcp__fastctx__glob","call_id":"call_1","arguments":"{\"pattern\":\"mcp__fastctx__glob\"}"}}`,
		``,
		`event: response.completed`,
		`data: {"type":"response.completed","response":{"status":"completed","output":[{"type":"function_call","name":"mcp__fastctx__glob","call_id":"call_1","arguments":"{}"}]}}`,
		``,
	}, "\n")
	names := map[string]translate.ResponseToolName{"mcp__fastctx__glob": {Namespace: "mcp__fastctx", Name: "glob"}}
	var out bytes.Buffer
	if _, err := RelayNativeResponsesStream(&out, strings.NewReader(upstream), names); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if strings.Contains(got, `"name":"mcp__fastctx__glob"`) {
		t.Fatalf("flattened tool name leaked to client:\n%s", got)
	}
	if strings.Count(got, `"namespace":"mcp__fastctx"`) != 3 || strings.Count(got, `"name":"glob"`) != 3 {
		t.Fatalf("namespace identity not restored on every item:\n%s", got)
	}
	// Tool arguments are user content and must never be rewritten.
	if !strings.Contains(got, `\"pattern\":\"mcp__fastctx__glob\"`) {
		t.Fatalf("arguments were rewritten:\n%s", got)
	}
	if !strings.Contains(got, `"sequence_number":3`) {
		t.Fatalf("numeric fields changed:\n%s", got)
	}
}

func TestRelayNativeResponsesStreamWithoutNamesIsVerbatim(t *testing.T) {
	frame := `data: {"type":"response.completed","response":{"status":"completed","output":[{"type":"function_call","name":"mcp__fastctx__glob"}]}}`
	var out bytes.Buffer
	if _, err := RelayNativeResponsesStream(&out, strings.NewReader("event: response.completed\n"+frame+"\n\n"), nil); err != nil {
		t.Fatal(err)
	}
	if out.String() != "event: response.completed\n"+frame+"\n\n" {
		t.Fatalf("frame not verbatim:\n%q", out.String())
	}
}
