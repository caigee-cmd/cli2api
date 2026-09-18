package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/caigee-cmd/cli2api/internal/accounts"
	"github.com/caigee-cmd/cli2api/internal/auth"
	"github.com/caigee-cmd/cli2api/internal/executor"
	"github.com/caigee-cmd/cli2api/internal/providers"
	"github.com/caigee-cmd/cli2api/internal/translate"
)

func TestResponsesNamespaceHandlerRoundTrip(t *testing.T) {
	for _, stream := range []bool{false, true} {
		t.Run(fmt.Sprint(stream), func(t *testing.T) {
			calls := 0
			server, closeServer := newCompatibilityServer(t, func(w http.ResponseWriter, r *http.Request) {
				calls++
				var body map[string]any
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Fatal(err)
				}
				if calls == 2 {
					messages := body["messages"].([]any)
					found := false
					for _, raw := range messages {
						message := raw.(map[string]any)
						if tools, ok := message["tool_calls"].([]any); ok {
							for _, rawCall := range tools {
								call := rawCall.(map[string]any)
								if call["id"] == "call_probe" && call["function"].(map[string]any)["name"] == "mcp__fastctx__glob" {
									found = true
								}
							}
						}
					}
					if !found {
						t.Errorf("history name/id missing: %v", messages)
					}
					_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"ROUNDTRIP_OK"},"finish_reason":"stop"}]}`)
					return
				}
				if stream {
					w.Header().Set("Content-Type", "text/event-stream")
					_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_probe\",\"function\":{\"name\":\"mcp__fastctx__glob\",\"arguments\":\"{\"}}]}}]}\n\ndata: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"function\":{\"arguments\":\"}\"}}]},\"finish_reason\":\"tool_calls\"}]}\n\ndata: [DONE]\n\n")
				} else {
					_, _ = io.WriteString(w, `{"choices":[{"message":{"tool_calls":[{"id":"call_probe","type":"function","function":{"name":"mcp__fastctx__glob","arguments":"{}"}}]},"finish_reason":"tool_calls"}]}`)
				}
			})
			defer closeServer()
			tools := []any{map[string]any{"type": "namespace", "name": "mcp__fastctx", "tools": []any{map[string]any{"type": "function", "name": "glob", "parameters": map[string]any{"type": "object", "properties": map[string]any{}}}}}}
			request := map[string]any{"model": "qoder/glm-5.2", "input": "find", "tools": tools, "stream": stream}
			send := func() *httptest.ResponseRecorder {
				data, _ := json.Marshal(request)
				recorder := httptest.NewRecorder()
				server.handleResponses(recorder, httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(string(data))))
				if recorder.Code != 200 {
					t.Fatalf("status=%d %s", recorder.Code, recorder.Body.String())
				}
				return recorder
			}
			recorder := send()
			var completed map[string]any
			check := func(item map[string]any) {
				if item["name"] != "glob" || item["namespace"] != "mcp__fastctx" || item["call_id"] != "call_probe" {
					t.Fatalf("wrong tool identity: %v", item)
				}
			}
			if stream {
				seen := map[string]bool{}
				for _, line := range strings.Split(recorder.Body.String(), "\n") {
					if !strings.HasPrefix(line, "data: ") {
						continue
					}
					var event map[string]any
					if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &event); err != nil {
						t.Fatal(err)
					}
					typ, _ := event["type"].(string)
					if item, ok := event["item"].(map[string]any); ok && item["type"] == "function_call" {
						check(item)
						seen[typ] = true
					}
					if typ == "response.function_call_arguments.delta" || typ == "response.function_call_arguments.done" {
						check(event)
						seen[typ] = true
					}
					if typ == "response.completed" {
						completed = event["response"].(map[string]any)
					}
				}
				for _, typ := range []string{"response.output_item.added", "response.output_item.done", "response.function_call_arguments.delta", "response.function_call_arguments.done"} {
					if !seen[typ] {
						t.Fatalf("missing %s", typ)
					}
				}
			} else if err := json.Unmarshal(recorder.Body.Bytes(), &completed); err != nil {
				t.Fatal(err)
			}
			if completed == nil {
				t.Fatal("no completed response")
			}
			output := completed["output"].([]any)
			item := output[len(output)-1].(map[string]any)
			check(item)
			if item["arguments"] != "{}" {
				t.Fatal(item)
			}
			request["stream"] = false
			request["input"] = []any{map[string]any{"role": "user", "content": "find"}, item, map[string]any{"type": "function_call_output", "call_id": "call_probe", "output": "found"}}
			if result := send(); !strings.Contains(result.Body.String(), "ROUNDTRIP_OK") {
				t.Fatal(result.Body.String())
			}
			if calls != 2 {
				t.Fatalf("upstream calls=%d", calls)
			}
		})
	}
}

func TestCompatibilityStreamsPreserveTypedReadError(t *testing.T) {
	failover := false
	want := &providers.Error{
		Kind: accounts.KindInvalidRequest, Status: http.StatusBadRequest,
		Code: "invalid_argument", Type: "invalid_request_error", Message: "upstream rejected request",
		RetryAfter: 45 * time.Second, Failover: &failover,
	}
	for name, relay := range map[string]func(io.Writer, io.Reader, string, string) (streamRelayStats, error){
		"anthropic": relayAnthropicStream,
		"responses": relayResponsesStream,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := relay(httptest.NewRecorder(), closedStreamPipe(fmt.Errorf("Connect trailer: %w", want)), "req_1", "devin/swe-2")
			var got *providers.Error
			if !errors.As(err, &got) || got != want {
				t.Fatalf("error=%T %+v want pointer=%p", err, err, want)
			}
		})
	}
}

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

func TestPrepareCompatibilityExecutionReusesChatPreflight(t *testing.T) {
	server, closeServer := newCompatibilityServer(t, func(http.ResponseWriter, *http.Request) {
		t.Fatal("preflight must not reach the worker")
	})
	defer closeServer()
	identity := auth.KeyIdentity(accounts.APIKey{ID: "key_1", Name: "ci", Providers: []string{"qoder", "workbuddy"}, Enabled: true})
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	req = req.WithContext(auth.WithIdentity(req.Context(), identity))
	req.Header.Set("X-Qoder-Account", "account-a")
	chat := translate.ChatRequest{Model: "workbuddy/glm-5.2", Messages: []translate.ChatMessage{{Role: "user", Content: "hi"}}}

	got, err := server.prepareChatExecution(req, chat)
	if err != nil {
		t.Fatal(err)
	}
	compat, err := server.prepareCompatibilityExecution(req, chat)
	if err != nil {
		t.Fatal(err)
	}
	if got.providerFilter != "workbuddy" || got.prefer != "account-a" || got.request.Model != "glm-5.2" || got.publicModel != "workbuddy/glm-5.2" {
		t.Fatalf("chat execution=%+v", got)
	}
	if compat.providerFilter != got.providerFilter || compat.prefer != got.prefer || compat.request.Model != got.request.Model || compat.publicModel != got.publicModel {
		t.Fatalf("compat=%+v chat=%+v", compat, got)
	}
}

func TestV1AndCompatibilitySharePreflightErrors(t *testing.T) {
	server, closeServer := newCompatibilityServer(t, func(http.ResponseWriter, *http.Request) {
		t.Fatal("preflight errors must not reach the worker")
	})
	defer closeServer()
	server.crossProviderModelPool.Store(false)
	identity := auth.KeyIdentity(accounts.APIKey{ID: "key_1", Name: "ci", Providers: []string{"qoder"}, Enabled: true})

	post := func(path, body string, handler func(http.ResponseWriter, *http.Request)) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
		req = req.WithContext(auth.WithIdentity(req.Context(), identity))
		rec := httptest.NewRecorder()
		handler(rec, req)
		return rec
	}

	chatBare := post("/v1/chat/completions", `{"model":"glm-5.2","messages":[{"role":"user","content":"hi"}]}`, server.handleChatCompletions)
	anthBare := post("/v1/messages", `{"model":"glm-5.2","max_tokens":32,"messages":[{"role":"user","content":"hi"}]}`, server.handleAnthropicMessages)
	respBare := post("/v1/responses", `{"model":"glm-5.2","input":"hi"}`, server.handleResponses)
	if chatBare.Code != http.StatusBadRequest || !strings.Contains(chatBare.Body.String(), `"provider_prefix_required"`) {
		t.Fatalf("chat bare=%d %s", chatBare.Code, chatBare.Body.String())
	}
	if anthBare.Code != http.StatusBadRequest || !strings.Contains(anthBare.Body.String(), "cross-provider model pool is disabled") {
		t.Fatalf("anthropic bare=%d %s", anthBare.Code, anthBare.Body.String())
	}
	if respBare.Code != http.StatusBadRequest || !strings.Contains(respBare.Body.String(), `"provider_prefix_required"`) {
		t.Fatalf("responses bare=%d %s", respBare.Code, respBare.Body.String())
	}

	chatDenied := post("/v1/chat/completions", `{"model":"workbuddy/glm-5.2","messages":[{"role":"user","content":"hi"}]}`, server.handleChatCompletions)
	anthDenied := post("/v1/messages", `{"model":"workbuddy/glm-5.2","max_tokens":32,"messages":[{"role":"user","content":"hi"}]}`, server.handleAnthropicMessages)
	respDenied := post("/v1/responses", `{"model":"workbuddy/glm-5.2","input":"hi"}`, server.handleResponses)
	if chatDenied.Code != http.StatusForbidden || !strings.Contains(chatDenied.Body.String(), `"provider_not_allowed"`) {
		t.Fatalf("chat denied=%d %s", chatDenied.Code, chatDenied.Body.String())
	}
	if anthDenied.Code != http.StatusForbidden || !strings.Contains(anthDenied.Body.String(), "This API key cannot use provider workbuddy") {
		t.Fatalf("anthropic denied=%d %s", anthDenied.Code, anthDenied.Body.String())
	}
	if respDenied.Code != http.StatusForbidden || !strings.Contains(respDenied.Body.String(), `"provider_not_allowed"`) {
		t.Fatalf("responses denied=%d %s", respDenied.Code, respDenied.Body.String())
	}

	server.crossProviderModelPool.Store(true)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"glm-5.2","messages":[{"role":"user","content":"hi"}]}`))
	req = req.WithContext(auth.WithIdentity(req.Context(), identity))
	execution, err := server.prepareChatExecution(req, translate.ChatRequest{Model: "glm-5.2", Messages: []translate.ChatMessage{{Role: "user", Content: "hi"}}})
	if err != nil {
		t.Fatal(err)
	}
	if execution.providerFilter != "" {
		t.Fatalf("bare model with pool on must keep an empty filter, got %q", execution.providerFilter)
	}
}
