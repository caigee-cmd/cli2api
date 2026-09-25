package translate

import "testing"

func TestParseResponsesEventTerminalUsageKeepsExplicitZero(t *testing.T) {
	event, ok := ParseResponsesEvent(`{"type":"response.completed","response":{"usage":{"input_tokens":0,"output_tokens":0,"input_tokens_details":{"cached_tokens":0}}}}`)
	if !ok || !event.Terminal || event.FinishReason != "stop" {
		t.Fatalf("event = %+v ok=%v", event, ok)
	}
	if event.InputTokens == nil || *event.InputTokens != 0 || event.OutputTokens == nil || *event.OutputTokens != 0 || event.CachedTokens == nil || *event.CachedTokens != 0 {
		t.Fatalf("usage = %+v", event)
	}
}

func TestParseResponsesEventFirstTokenOnlyForOutputDelta(t *testing.T) {
	for _, tc := range []struct {
		typ  string
		want bool
	}{
		{"response.reasoning_summary_text.delta", false},
		{"response.function_call_arguments.delta", false},
		{"response.output_text.delta", true},
	} {
		event, ok := ParseResponsesEvent(`{"type":"` + tc.typ + `"}`)
		if !ok || event.FirstToken != tc.want {
			t.Errorf("type %q: event=%+v ok=%v", tc.typ, event, ok)
		}
	}
}
