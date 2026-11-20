package kiro

import "testing"

func TestUpdateTokensShapes(t *testing.T) {
	cases := []struct {
		name    string
		event   map[string]any
		in, out int
	}{
		{"snake case usage", map[string]any{"usage": map[string]any{"input_tokens": 11.0, "output_tokens": 7.0}}, 11, 7},
		{"camel case top level", map[string]any{"promptTokens": 20.0, "completionTokens": 4.0}, 20, 4},
		{"nested tokenUsage", map[string]any{"metadata": map[string]any{"tokenUsage": map[string]any{"totalInputTokens": 30.0, "totalOutputTokens": 5.0}}}, 30, 5},
		{"cache split", map[string]any{"usage": map[string]any{"uncachedInputTokens": 10.0, "cacheReadInputTokens": 80.0, "cacheWriteInputTokens": 10.0, "outputTokens": 3.0}}, 100, 3},
		{"total minus output", map[string]any{"usage": map[string]any{"totalTokens": 50.0, "outputTokens": 8.0}}, 42, 8},
		{"usage is credits", map[string]any{"usage": 0.25}, 0, 0},
	}
	for _, tc := range cases {
		in, out := updateTokens(tc.event, 0, 0)
		if in != tc.in || out != tc.out {
			t.Errorf("%s: got %d/%d, want %d/%d", tc.name, in, out, tc.in, tc.out)
		}
	}
}

func TestUpdateTokensKeepsEarlierCounts(t *testing.T) {
	in, out := updateTokens(map[string]any{"content": "hi"}, 12, 3)
	if in != 12 || out != 3 {
		t.Fatalf("got %d/%d", in, out)
	}
}
