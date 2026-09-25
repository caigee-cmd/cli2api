package gateway

import (
	"bytes"
	"strings"
	"testing"
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
	stats, err := RelayNativeResponsesStream(&out, strings.NewReader(upstream))
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
	stats, err := RelayNativeResponsesStream(&bytes.Buffer{}, strings.NewReader(upstream))
	if err == nil {
		t.Fatal("expected incomplete error")
	}
	_ = stats
}

func TestRelayNativeResponsesStreamIncompleteStatus(t *testing.T) {
	upstream := strings.Join([]string{
		`event: response.incomplete`,
		`data: {"type":"response.incomplete","response":{"status":"incomplete","usage":{"input_tokens":5,"output_tokens":100}}}`,
		``,
	}, "\n")
	stats, err := RelayNativeResponsesStream(&bytes.Buffer{}, strings.NewReader(upstream))
	if err != nil {
		t.Fatal(err)
	}
	if stats.FinishReason != "length" {
		t.Fatalf("finish %q", stats.FinishReason)
	}
}
