package gateway

import (
	"bytes"
	"strings"
	"testing"
)

func TestAnthropicInputTokensExcludeCacheShares(t *testing.T) {
	cases := []struct {
		name        string
		prompt      int
		read, write *int
		want        int
	}{
		{name: "no cache", prompt: 100, want: 100},
		{name: "read and write", prompt: 100, read: ptrInt(64), write: ptrInt(12), want: 24},
		{name: "never negative", prompt: 10, read: ptrInt(64), want: 0},
	}
	for _, tc := range cases {
		if got := anthropicInputTokens(tc.prompt, tc.read, tc.write); got != tc.want {
			t.Errorf("%s: input_tokens = %d, want %d", tc.name, got, tc.want)
		}
	}
}

func TestAnthropicMessageResponseSplitsInputFromCache(t *testing.T) {
	resp := anthropicMessageResponse("req", "model", "ok", "", nil, "stop", 100, 20, ptrInt(64), ptrInt(12))
	usage := resp["usage"].(map[string]any)
	if usage["input_tokens"] != 24 || usage["output_tokens"] != 20 {
		t.Fatalf("usage = %#v", usage)
	}
}

func TestRelayAnthropicStreamReportsInputTokensInMessageDelta(t *testing.T) {
	body := strings.NewReader("data: {\"model\":\"m\",\"choices\":[{\"delta\":{\"content\":\"hi\"}}]}\n\n" +
		"data: {\"choices\":[{\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":100,\"completion_tokens\":20,\"cache_read_tokens\":64,\"cache_write_tokens\":12}}\n\ndata: [DONE]\n\n")
	var out bytes.Buffer
	if _, err := RelayAnthropicStream(&out, body, "req", "model"); err != nil {
		t.Fatal(err)
	}
	stream := out.String()
	delta := stream[strings.Index(stream, "event: message_delta"):]
	for _, want := range []string{`"input_tokens":24`, `"output_tokens":20`, `"cache_read_input_tokens":64`, `"cache_creation_input_tokens":12`} {
		if !strings.Contains(delta, want) {
			t.Fatalf("message_delta missing %s:\n%s", want, delta)
		}
	}
}

func TestRelayAnthropicStreamWithoutCacheReportsFullPrompt(t *testing.T) {
	body := strings.NewReader("data: {\"choices\":[{\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":4,\"completion_tokens\":1}}\n\ndata: [DONE]\n\n")
	var out bytes.Buffer
	if _, err := RelayAnthropicStream(&out, body, "req", "model"); err != nil {
		t.Fatal(err)
	}
	delta := out.String()[strings.Index(out.String(), "event: message_delta"):]
	if !strings.Contains(delta, `"input_tokens":4`) || strings.Contains(delta, "cache_read_input_tokens") {
		t.Fatalf("message_delta:\n%s", delta)
	}
}
