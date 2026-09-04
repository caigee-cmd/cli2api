package api

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/caigee-cmd/cli2api/internal/accounts"
	"github.com/caigee-cmd/cli2api/internal/executor"
)

func newCompatibilityServer(t *testing.T, worker http.HandlerFunc) (*Server, func()) {
	t.Helper()
	upstream := httptest.NewServer(worker)
	pool := accounts.NewPool(nil, nil)
	pool.Upsert(accounts.Item{ID: "account-a", URL: upstream.URL, Provider: "qoder", Region: "global", Runtime: "child_process"})
	chatExecutor := executor.NewChatExecutor(pool, "")
	chatExecutor.HTTPClient = upstream.Client()
	server := &Server{executor: chatExecutor, pool: pool}
	return server, upstream.Close
}

func TestAnthropicMessagesNonStreamTranslatesTools(t *testing.T) {
	server, closeServer := newCompatibilityServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Fatalf("path=%s", r.URL.Path)
		}
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		messages := payload["messages"].([]any)
		if len(messages) != 2 || messages[0].(map[string]any)["role"] != "system" || messages[1].(map[string]any)["content"] != "hello" {
			t.Fatalf("messages=%#v", messages)
		}
		tools := payload["tools"].([]any)
		function := tools[0].(map[string]any)["function"].(map[string]any)
		if function["name"] != "weather" {
			t.Fatalf("tools=%#v", tools)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"model": "glm-5.2",
			"choices": []any{map[string]any{"message": map[string]any{
				"content": "", "tool_calls": []any{map[string]any{"id": "call_1", "type": "function", "function": map[string]string{"name": "weather", "arguments": `{"city":"Shanghai"}`}}},
			}, "finish_reason": "tool_calls"}},
			"usage": map[string]any{"prompt_tokens": 12, "completion_tokens": 3, "source": "upstream"},
		})
	})
	defer closeServer()

	request := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{
		"model":"qoder/glm-5.2","system":"Be concise.","max_tokens":128,
		"messages":[{"role":"user","content":[{"type":"text","text":"hello"}]}],
		"tools":[{"name":"weather","description":"weather lookup","input_schema":{"type":"object"}}],
		"tool_choice":{"type":"tool","name":"weather"}
	}`))
	recorder := httptest.NewRecorder()
	server.handleAnthropicMessages(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var response struct {
		Type       string `json:"type"`
		StopReason string `json:"stop_reason"`
		Content    []struct {
			Type  string `json:"type"`
			ID    string `json:"id"`
			Name  string `json:"name"`
			Input struct {
				City string `json:"city"`
			} `json:"input"`
		} `json:"content"`
		Usage struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Type != "message" || response.StopReason != "tool_use" || len(response.Content) != 1 || response.Content[0].Type != "tool_use" || response.Content[0].Name != "weather" || response.Content[0].Input.City != "Shanghai" {
		t.Fatalf("response=%+v", response)
	}
	if response.Usage.InputTokens != 12 || response.Usage.OutputTokens != 3 {
		t.Fatalf("usage=%+v", response.Usage)
	}
}

func TestResponsesNonStreamTranslatesInput(t *testing.T) {
	server, closeServer := newCompatibilityServer(t, func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		messages := payload["messages"].([]any)
		if len(messages) != 2 || messages[0].(map[string]any)["role"] != "system" || messages[1].(map[string]any)["content"] != "hello" {
			t.Fatalf("messages=%#v", messages)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"model": "glm-5.2", "choices": []any{map[string]any{"message": map[string]string{"content": "world"}, "finish_reason": "stop"}},
			"usage": map[string]any{"prompt_tokens": 7, "completion_tokens": 2, "source": "upstream"},
		})
	})
	defer closeServer()

	request := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{
		"model":"qoder/glm-5.2","instructions":"Respond briefly.",
		"input":[{"role":"user","content":[{"type":"input_text","text":"hello"}]}]
	}`))
	recorder := httptest.NewRecorder()
	server.handleResponses(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var response struct {
		Object string `json:"object"`
		Status string `json:"status"`
		Output []struct {
			Type    string `json:"type"`
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"output"`
		Usage struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Object != "response" || response.Status != "completed" || len(response.Output) != 1 || response.Output[0].Type != "message" || response.Output[0].Content[0].Text != "world" {
		t.Fatalf("response=%+v", response)
	}
	if response.Usage.InputTokens != 7 || response.Usage.OutputTokens != 2 {
		t.Fatalf("usage=%+v", response.Usage)
	}
}

func TestAnthropicMessagesStreamWritesProtocolEvents(t *testing.T) {
	server, closeServer := newCompatibilityServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"model\":\"glm-5.2\",\"choices\":[{\"delta\":{\"content\":\"hello\"}}]}\n\n")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":4,\"completion_tokens\":1}}\n\n")
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	})
	defer closeServer()

	request := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{"model":"qoder/glm-5.2","stream":true,"max_tokens":32,"messages":[{"role":"user","content":"hi"}]}`))
	recorder := httptest.NewRecorder()
	server.handleAnthropicMessages(recorder, request)
	body := recorder.Body.String()
	for _, event := range []string{"event: message_start", "event: content_block_start", "event: content_block_delta", "event: message_delta", "event: message_stop"} {
		if !strings.Contains(body, event) {
			t.Fatalf("missing %s in %s", event, body)
		}
	}
	if !strings.Contains(body, `"text":"hello"`) {
		t.Fatalf("body=%s", body)
	}
}

func TestResponsesStreamWritesProtocolEvents(t *testing.T) {
	server, closeServer := newCompatibilityServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"model\":\"glm-5.2\",\"choices\":[{\"delta\":{\"content\":\"hello\"}}]}\n\n")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":4,\"completion_tokens\":1}}\n\n")
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	})
	defer closeServer()

	request := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"qoder/glm-5.2","stream":true,"input":"hi"}`))
	recorder := httptest.NewRecorder()
	server.handleResponses(recorder, request)
	body := recorder.Body.String()
	for _, event := range []string{"event: response.created", "event: response.output_item.added", "event: response.output_text.delta", "event: response.completed"} {
		if !strings.Contains(body, event) {
			t.Fatalf("missing %s in %s", event, body)
		}
	}
	if !strings.Contains(body, `"delta":"hello"`) {
		t.Fatalf("body=%s", body)
	}
}

func TestResponsesStreamKeepsDistinctFunctionCallIDs(t *testing.T) {
	server, closeServer := newCompatibilityServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_a\",\"function\":{\"name\":\"first\",\"arguments\":\"{}\"}},{\"index\":1,\"id\":\"call_b\",\"function\":{\"name\":\"second\",\"arguments\":\"{}\"}}]}}]}\n\n")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"finish_reason\":\"tool_calls\"}]}\n\n")
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	})
	defer closeServer()

	request := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"qoder/glm-5.2","stream":true,"input":"hi"}`))
	recorder := httptest.NewRecorder()
	server.handleResponses(recorder, request)
	body := recorder.Body.String()
	if !strings.Contains(body, `"call_id":"call_a"`) || !strings.Contains(body, `"call_id":"call_b"`) {
		t.Fatalf("body=%s", body)
	}
	matches := regexp.MustCompile(`"id":"(fc_[^"]+)"`).FindAllStringSubmatch(body, -1)
	uniqueIDs := map[string]struct{}{}
	for _, match := range matches {
		uniqueIDs[match[1]] = struct{}{}
	}
	if len(uniqueIDs) != 2 {
		t.Fatalf("expected two distinct function-call item ids, got %#v in %s", uniqueIDs, body)
	}
}

func TestResponsesRejectsStatefulPreviousResponseID(t *testing.T) {
	server, closeServer := newCompatibilityServer(t, func(w http.ResponseWriter, _ *http.Request) {
		t.Fatal("worker should not receive unsupported stateful response request")
	})
	defer closeServer()

	request := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"qoder/glm-5.2","input":"hi","previous_response_id":"resp_previous"}`))
	recorder := httptest.NewRecorder()
	server.handleResponses(recorder, request)
	if recorder.Code != http.StatusBadRequest || !strings.Contains(recorder.Body.String(), "previous_response_id") {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}
