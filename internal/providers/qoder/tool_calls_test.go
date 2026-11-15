package qoder

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

type collectedTool struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

// Regression for #331: streamed tool_call fragments accumulate by index.
func TestCollectUpstreamAccumulatesToolCallFragments(t *testing.T) {
	stream := "data: " + `{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_weather","type":"function","function":{"name":"get_weather","arguments":"{\"city\":\"Bei"}}]}}]}` + "\n\n" +
		"data: " + `{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"jing\"}"}}]}}]}` + "\n\n" +
		"data: " + `{"choices":[{"delta":{},"finish_reason":"tool_calls"}]}` + "\n\n"
	frame, err := collectUpstream(strings.NewReader(stream))
	if err != nil {
		t.Fatal(err)
	}
	var tools []collectedTool
	if err := json.Unmarshal(frame.tools, &tools); err != nil {
		t.Fatal(err)
	}
	if len(tools) != 1 || tools[0].ID != "call_weather" || tools[0].Type != "function" || tools[0].Function.Name != "get_weather" || tools[0].Function.Arguments != `{"city":"Beijing"}` {
		t.Fatalf("tool call fragments lost: %s", frame.tools)
	}
	if frame.finish != "tool_calls" {
		t.Fatalf("finish = %q", frame.finish)
	}
}

func TestCollectUpstreamAccumulatesInterleavedToolCalls(t *testing.T) {
	stream := "data: " + `{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"a","type":"function","function":{"name":"fa","arguments":"{\"x\":"}}]}}]}` + "\n\n" +
		"data: " + `{"choices":[{"delta":{"tool_calls":[{"index":1,"id":"b","type":"function","function":{"name":"fb","arguments":"{\"y\":"}}]}}]}` + "\n\n" +
		"data: " + `{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"1}"}},{"index":1,"function":{"arguments":"2}"}}]}}]}` + "\n\n"
	frame, err := collectUpstream(strings.NewReader(stream))
	if err != nil {
		t.Fatal(err)
	}
	var tools []collectedTool
	if err := json.Unmarshal(frame.tools, &tools); err != nil {
		t.Fatal(err)
	}
	if len(tools) != 2 || tools[0].ID != "a" || tools[0].Function.Arguments != `{"x":1}` || tools[1].ID != "b" || tools[1].Function.Name != "fb" || tools[1].Function.Arguments != `{"y":2}` {
		t.Fatalf("interleaved tool calls merged wrong: %s", frame.tools)
	}
}

// The upstream reuses index 0 for a second call. Distinct ids must stay
// distinct; a later fragment that repeats an id still appends to that call.
func TestCollectUpstreamSplitsReusedToolCallIndex(t *testing.T) {
	stream := "data: " + `{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_a","type":"function","function":{"name":"echo_a","arguments":"{\"text\":"}}]}}]}` + "\n\n" +
		"data: " + `{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_b","type":"function","function":{"name":"echo_b","arguments":"{\"text\":"}}]}}]}` + "\n\n" +
		"data: " + `{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_a","function":{"arguments":"\"A\"}"}}]}}]}` + "\n\n" +
		"data: " + `{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_b","function":{"arguments":"\"B\"}"}}]}}]}` + "\n\n"
	frame, err := collectUpstream(strings.NewReader(stream))
	if err != nil {
		t.Fatal(err)
	}
	var tools []collectedTool
	if err := json.Unmarshal(frame.tools, &tools); err != nil {
		t.Fatal(err)
	}
	if len(tools) != 2 || tools[0].ID != "call_a" || tools[0].Function.Name != "echo_a" || tools[0].Function.Arguments != `{"text":"A"}` ||
		tools[1].ID != "call_b" || tools[1].Function.Name != "echo_b" || tools[1].Function.Arguments != `{"text":"B"}` {
		t.Fatalf("reused index collapsed the calls: %s", frame.tools)
	}
}

func TestWriteOpenAIChunksRenumbersReusedToolCallIndex(t *testing.T) {
	stream := "data: " + `{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_a","type":"function","function":{"name":"echo_a","arguments":"{}"}}]}}]}` + "\n\n" +
		"data: " + `{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_b","type":"function","function":{"name":"echo_b","arguments":"{}"}}]}}]}` + "\n\n" +
		"data: " + `{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_a","function":{"arguments":""}}]}}]}` + "\n\n"
	var output bytes.Buffer
	if err := writeOpenAIChunks(&output, strings.NewReader(stream), "synthetic-model"); err != nil {
		t.Fatal(err)
	}
	var indexes []float64
	for _, line := range strings.Split(output.String(), "\n") {
		line = strings.TrimSpace(line)
		data, ok := strings.CutPrefix(line, "data:")
		if !ok || strings.TrimSpace(data) == "[DONE]" {
			continue
		}
		var chunk struct {
			Choices []struct {
				Delta struct {
					ToolCalls []struct {
						Index float64 `json:"index"`
						ID    string  `json:"id"`
					} `json:"tool_calls"`
				} `json:"delta"`
			} `json:"choices"`
		}
		if json.Unmarshal([]byte(data), &chunk) != nil || len(chunk.Choices) == 0 {
			continue
		}
		for _, call := range chunk.Choices[0].Delta.ToolCalls {
			if call.ID != "" {
				indexes = append(indexes, call.Index)
			}
		}
	}
	if len(indexes) != 3 || indexes[0] != 0 || indexes[1] != 1 || indexes[2] != 0 {
		t.Fatalf("streamed indexes = %v, want 0, 1, 0", indexes)
	}
}

func TestCollectUpstreamIgnoresEmptyToolCalls(t *testing.T) {
	stream := "data: " + `{"choices":[{"delta":{"content":"plain","tool_calls":[]}}]}` + "\n\n"
	frame, err := collectUpstream(strings.NewReader(stream))
	if err != nil {
		t.Fatal(err)
	}
	if len(frame.tools) != 0 {
		t.Fatalf("empty tool array kept: %s", frame.tools)
	}
}

// Regression for #332: an empty tool_calls array is not a tool call.
func TestEmptyToolsAreNotToolCalls(t *testing.T) {
	stream := "data: " + `{"choices":[{"delta":{"content":"plain answer","tool_calls":[]},"finish_reason":null}]}` + "\n\n"
	var output bytes.Buffer
	if err := writeOpenAIChunks(&output, strings.NewReader(stream), "synthetic-model"); err != nil {
		t.Fatal(err)
	}
	text := output.String()
	if strings.Contains(text, `"tool_calls"`) || !strings.Contains(text, `"finish_reason":"stop"`) {
		t.Fatalf("empty tool array emitted a tool event or terminal: %s", text)
	}
}
