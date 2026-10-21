package qoder

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func completionIDs(t *testing.T, stream string) []string {
	t.Helper()
	var output bytes.Buffer
	if err := writeOpenAIChunks(&output, strings.NewReader(stream), "synthetic-model"); err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, line := range strings.Split(output.String(), "\n") {
		data, ok := strings.CutPrefix(line, "data: ")
		if !ok || data == "[DONE]" {
			continue
		}
		var chunk struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			t.Fatalf("decode %q: %v", data, err)
		}
		ids = append(ids, chunk.ID)
	}
	return ids
}

// Regression for #292: every request gets its own completion id, shared by
// all of its chunks including the synthesized terminal and usage chunks.
func TestWriteOpenAIChunksUsesOneUniqueIDPerRequest(t *testing.T) {
	cases := map[string]string{
		// No upstream finish: a terminal chunk is synthesized.
		"synthesized_finish": "data: " + `{"choices":[{"delta":{"content":"hi"}}]}` + "\n\n",
		// Finish without usage, then usage: a trailing usage chunk is synthesized.
		"synthesized_usage": "data: " + `{"choices":[{"delta":{"content":"hi"}}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}` + "\n\n" +
			"data: " + `{"choices":[{"delta":{"content":"!"},"finish_reason":"stop"}]}` + "\n\n",
	}
	for name, stream := range cases {
		t.Run(name, func(t *testing.T) {
			first := completionIDs(t, stream)
			second := completionIDs(t, stream)
			if len(first) < 2 {
				t.Fatalf("expected synthesized chunk, got ids %v", first)
			}
			for _, ids := range [][]string{first, second} {
				for _, id := range ids {
					if id != ids[0] || !strings.HasPrefix(id, "chatcmpl-") || id == "chatcmpl-qoder" {
						t.Fatalf("inconsistent or fixed ids %v", ids)
					}
				}
			}
			if first[0] == second[0] {
				t.Fatalf("two requests share completion id %q", first[0])
			}
		})
	}
}
