package translate

import (
	"strings"
	"testing"
)

func TestCollectResponsesPreservesTerminalResponseAndZeroUsage(t *testing.T) {
	sse := strings.Join([]string{
		"event: response.output_text.delta",
		`data: {"type":"response.output_text.delta","delta":"hi"}`,
		"",
		"event: response.completed",
		`data: {"type":"response.completed","response":{"id":"resp_1","status":"completed","usage":{"input_tokens":0,"output_tokens":0,"input_tokens_details":{"cached_tokens":0}},"output":[{"type":"reasoning","id":"rs_1","encrypted_content":"opaque"}]}}`,
		"",
	}, "\n")
	got, err := CollectResponses(strings.NewReader(sse))
	if err != nil {
		t.Fatal(err)
	}
	if string(got.Response) == "" || !strings.Contains(string(got.Response), `"encrypted_content":"opaque"`) {
		t.Fatalf("terminal response was not preserved: %s", got.Response)
	}
	if got.InputTokens == nil || *got.InputTokens != 0 || got.OutputTokens == nil || *got.OutputTokens != 0 || got.CachedTokens == nil || *got.CachedTokens != 0 {
		t.Fatalf("zero usage pointers were lost: %+v", got)
	}
}

func TestCollectResponsesRejectsMissingTerminalAndFailedResponse(t *testing.T) {
	_, err := CollectResponses(strings.NewReader("data: {\"type\":\"response.output_text.delta\",\"delta\":\"hi\"}\n\n"))
	if err != ErrResponsesIncomplete {
		t.Fatalf("missing terminal error = %v", err)
	}
	_, err = CollectResponses(strings.NewReader("data: {\"type\":\"response.failed\",\"response\":{\"status\":\"failed\"}}\n\n"))
	if err == nil {
		t.Fatal("failed terminal must return an error")
	}
	if _, ok := err.(*ResponsesStreamFailure); !ok {
		t.Fatalf("failed terminal error type = %T", err)
	}
}

func TestCollectResponsesRejectsTerminalWithoutResponse(t *testing.T) {
	_, err := CollectResponses(strings.NewReader("data: {\"type\":\"response.completed\"}\n\n"))
	if err == nil || !strings.Contains(err.Error(), "no response object") {
		t.Fatalf("missing response error = %v", err)
	}
}
