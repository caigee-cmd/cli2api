package qoder

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/caigee-cmd/cli2api/internal/accounts"
	"github.com/caigee-cmd/cli2api/internal/providers"
	"github.com/caigee-cmd/cli2api/internal/translate"
)

// Direct talks to the Qoder chat gateway from this process. It does not load
// the CLI bundle. Login and quota stay on the per-account worker.
type Direct struct {
	store  CredentialStore
	region func(ctx context.Context, accountID string) (string, error)
	http   *http.Client
	now    func() time.Time
}

func NewDirect(store CredentialStore, region func(context.Context, string) (string, error)) *Direct {
	return &Direct{
		store:  store,
		region: region,
		http:   &http.Client{},
		now:    time.Now,
	}
}

func (d *Direct) SetHTTP(client *http.Client) {
	if d == nil || client == nil {
		return
	}
	d.http = client
}

type directPrepared struct {
	request *http.Request
	model   string
	level   string
}

func (d *Direct) prepare(ctx context.Context, accountID string, req translate.ChatRequest) (directPrepared, error) {
	if d == nil || d.store == nil {
		return directPrepared{}, fmt.Errorf("qoder direct client is not configured")
	}
	credential, err := d.store.LoadCredential(ctx, accountID)
	if err != nil {
		return directPrepared{}, err
	}
	user, err := DecryptCLIUser(credential.UserBlob, credential.MachineID)
	if err != nil {
		return directPrepared{}, err
	}
	session, err := NewSession(user)
	if err != nil {
		return directPrepared{}, err
	}
	region := "global"
	if d.region != nil {
		region, err = d.region(ctx, accountID)
		if err != nil {
			return directPrepared{}, err
		}
	}
	plain := BuildPlainChatBody(req, d.now())
	model, _ := plain["model_config"].(map[string]any)
	modelKey, _ := model["key"].(string)
	body, err := json.Marshal(plain)
	if err != nil {
		return directPrepared{}, err
	}
	endpoint := chatEndpoint(region)
	encoded := EncodeBody(body)
	headers, err := session.Sign(encoded, endpoint, d.now())
	if err != nil {
		return directPrepared{}, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(encoded))
	if err != nil {
		return directPrepared{}, err
	}
	for key, value := range headers {
		httpReq.Header.Set(key, value)
	}
	if modelKey != "" {
		httpReq.Header.Set("X-Model-Key", modelKey)
		httpReq.Header.Set("X-Model-Source", "system")
	}
	return directPrepared{request: httpReq, model: modelKey, level: resolvedReasoningLevel(req)}, nil
}

func (d *Direct) ChatNonStream(ctx context.Context, accountID string, req translate.ChatRequest) (providers.ChatOutcome, error) {
	prepared, err := d.prepare(ctx, accountID, req)
	if err != nil {
		return providers.ChatOutcome{}, err
	}
	resp, err := d.http.Do(prepared.request)
	if err != nil {
		return providers.ChatOutcome{}, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return providers.ChatOutcome{}, err
	}
	if resp.StatusCode >= 300 {
		return providers.ChatOutcome{}, upstreamStatusError(resp.StatusCode, raw)
	}
	outcome, err := outcomeFromUpstreamSSE(prepared.model, raw)
	if err != nil {
		return providers.ChatOutcome{}, err
	}
	outcome.ReasoningLevel = prepared.level
	return outcome, nil
}

func (d *Direct) ChatStream(ctx context.Context, accountID string, req translate.ChatRequest) (*http.Response, providers.ResolvedChat, error) {
	prepared, err := d.prepare(ctx, accountID, req)
	if err != nil {
		return nil, providers.ResolvedChat{}, err
	}
	resp, err := d.http.Do(prepared.request)
	if err != nil {
		return nil, providers.ResolvedChat{}, err
	}
	if resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		resp.Body.Close()
		return nil, providers.ResolvedChat{}, upstreamStatusError(resp.StatusCode, raw)
	}
	return rewriteUpstreamSSE(resp, prepared.model), providers.ResolvedChat{ReasoningLevel: prepared.level}, nil
}

func upstreamStatusError(status int, body []byte) error {
	message := strings.TrimSpace(string(body))
	if message == "" {
		message = fmt.Sprintf("qoder upstream status %d", status)
	}
	kind := accounts.KindUnavailable
	switch status {
	case http.StatusUnauthorized, http.StatusForbidden:
		kind = accounts.KindAuth
	case http.StatusTooManyRequests:
		kind = accounts.KindRateLimit
	case http.StatusBadRequest:
		kind = accounts.KindInvalidRequest
	}
	return &providers.Error{Kind: kind, Status: status, Message: message}
}

// BuildPlainChatBody is the plaintext agent_chat_generation document. The
// gateway expects this shape before Encode=1; it is not the OpenAI body the
// worker used to accept.
func BuildPlainChatBody(req translate.ChatRequest, now time.Time) map[string]any {
	if now.IsZero() {
		now = time.Now()
	}
	var systemParts []string
	messages := make([]map[string]any, 0, len(req.Messages))
	for _, message := range req.Messages {
		text := strings.TrimSpace(translate.ContentToString(message.Content))
		if message.Role == "system" || message.Role == "developer" {
			if text != "" {
				systemParts = append(systemParts, text)
			}
			continue
		}
		item := map[string]any{"role": message.Role, "content": message.Content}
		if message.Name != "" {
			item["name"] = message.Name
		}
		if message.ToolCallID != "" {
			item["tool_call_id"] = message.ToolCallID
		}
		if len(message.ToolCalls) > 0 {
			item["tool_calls"] = json.RawMessage(message.ToolCalls)
		}
		messages = append(messages, item)
	}
	systemText := strings.Join(systemParts, "\n\n")
	if systemText != "" {
		messages = append([]map[string]any{{"role": "system", "content": systemText}}, messages...)
	}
	if len(messages) == 0 {
		messages = []map[string]any{{"role": "user", "content": "ping"}}
	}
	modelKey := strings.TrimSpace(req.Model)
	if modelKey == "" {
		modelKey = "auto"
	}
	maxTokens := 32000
	if n, ok := rawNumber(req.MaxCompletionTokens); ok && n > 0 {
		maxTokens = n
	} else if n, ok := rawNumber(req.MaxTokens); ok && n > 0 {
		maxTokens = n
	}
	parameters := map[string]any{"max_tokens": maxTokens}
	copyRaw(parameters, "temperature", req.Temperature)
	copyRaw(parameters, "top_p", req.TopP)
	copyRaw(parameters, "stop", req.Stop)
	copyRaw(parameters, "response_format", req.ResponseFormat)
	copyRaw(parameters, "reasoning_effort", req.ReasoningEffort)
	copyRaw(parameters, "reasoning_budget_tokens", req.ReasoningBudgetTokens)
	copyRaw(parameters, "context_length", req.ContextLength)
	copyRaw(parameters, "max_input_tokens", req.MaxInputTokens)
	copyRaw(parameters, "tool_choice", req.ToolChoice)
	if req.ParallelToolCalls != nil {
		parameters["parallel_tool_calls"] = *req.ParallelToolCalls
	}
	level := resolvedReasoningLevel(req)
	reasoning := level != "" && level != "none"
	if level != "" {
		parameters["enable_thinking"] = reasoning
	}
	maxInput := 180000
	if n, ok := rawNumber(req.MaxInputTokens); ok && n > 0 {
		maxInput = n
	}
	name := "chat"
	if text := translate.ContentToString(messages[len(messages)-1]["content"]); strings.TrimSpace(text) != "" {
		name = strings.TrimSpace(text)
	}
	if len(name) > 40 {
		name = name[:40]
	}
	return map[string]any{
		"request_id":       newID(),
		"request_set_id":   newID(),
		"chat_record_id":   newID(),
		"session_id":       newID(),
		"stream":           true,
		"chat_task":        "FREE_INPUT",
		"chat_context":     map[string]any{"text": "", "features": map[string]any{}, "extra": map[string]any{}, "chatPrompt": "", "imageUrls": []any{}},
		"is_reply":         false,
		"is_retry":         false,
		"source":           "cli",
		"version":          "1.0",
		"agent_id":         "agent_common",
		"task_id":          "common",
		"session_type":     "assistant",
		"aliyun_user_type": "",
		"model_config": map[string]any{
			"key": modelKey, "display_name": modelKey, "model": "", "format": "openai",
			"is_vl": true, "is_reasoning": reasoning, "api_key": "", "url": "",
			"source": "system", "max_input_tokens": maxInput,
		},
		"custom_model": nil,
		"system":       systemText,
		"messages":     messages,
		"tools":        rawOrEmpty(req.Tools),
		"parameters":   parameters,
		"business": map[string]any{
			"product": "cli", "version": COSYVersion, "type": "agent",
			"id": newID(), "name": name, "begin_at": now.UnixMilli(), "stage": "start",
		},
	}
}

func rawNumber(raw json.RawMessage) (int, bool) {
	if len(raw) == 0 {
		return 0, false
	}
	var n int
	if json.Unmarshal(raw, &n) != nil {
		return 0, false
	}
	return n, true
}

func copyRaw(dst map[string]any, key string, raw json.RawMessage) {
	if len(bytes.TrimSpace(raw)) == 0 || string(raw) == "null" {
		return
	}
	dst[key] = json.RawMessage(raw)
}

func rawOrEmpty(raw json.RawMessage) any {
	if len(bytes.TrimSpace(raw)) == 0 {
		return []any{}
	}
	return json.RawMessage(raw)
}

func newID() string {
	id, err := randomUUID()
	if err != nil {
		return "00000000-0000-4000-8000-000000000000"
	}
	return id
}

type upstreamFrame struct {
	content   string
	reasoning string
	tools     json.RawMessage
	finish    string
	usage     json.RawMessage
	errText   string
	errStatus int
	errKind   string
}

func outcomeFromUpstreamSSE(model string, raw []byte) (providers.ChatOutcome, error) {
	frame, err := collectUpstream(bytes.NewReader(raw))
	if err != nil {
		return providers.ChatOutcome{}, err
	}
	if frame.errText != "" && frame.content == "" && frame.reasoning == "" && len(frame.tools) == 0 {
		kind := frame.errKind
		if kind == "" {
			kind = accounts.KindUnavailable
		}
		status := frame.errStatus
		if status == 0 {
			status = http.StatusBadGateway
		}
		return providers.ChatOutcome{}, &providers.Error{Kind: kind, Status: status, Message: frame.errText}
	}
	out := providers.ChatOutcome{
		Model:        model,
		Content:      frame.content,
		Reasoning:    frame.reasoning,
		ToolCalls:    frame.tools,
		FinishReason: frame.finish,
		UsageSource:  "upstream",
	}
	if out.FinishReason == "" {
		if len(out.ToolCalls) > 0 {
			out.FinishReason = "tool_calls"
		} else {
			out.FinishReason = "stop"
		}
	}
	applyUsage(&out, frame.usage)
	return out, nil
}

func applyUsage(out *providers.ChatOutcome, raw json.RawMessage) {
	if len(raw) == 0 {
		out.UsageSource = "estimate"
		return
	}
	var usage struct {
		PromptTokens     int      `json:"prompt_tokens"`
		CompletionTokens int      `json:"completion_tokens"`
		CacheReadTokens  *int     `json:"cache_read_tokens"`
		CacheWriteTokens *int     `json:"cache_write_tokens"`
		Credits          *float64 `json:"credits"`
	}
	if json.Unmarshal(raw, &usage) != nil {
		out.UsageSource = "estimate"
		return
	}
	out.PromptTokens = usage.PromptTokens
	out.CompletionTokens = usage.CompletionTokens
	out.CacheReadTokens = usage.CacheReadTokens
	out.CacheWriteTokens = usage.CacheWriteTokens
	out.Credits = usage.Credits
}

func collectUpstream(r io.Reader) (upstreamFrame, error) {
	var frame upstreamFrame
	var content, reasoning strings.Builder
	err := walkUpstream(r, func(body map[string]any) error {
		if errText, status, kind := upstreamError(body); errText != "" {
			frame.errText = errText
			frame.errStatus = status
			frame.errKind = kind
		}
		if usage, ok := body["usage"]; ok && usage != nil {
			raw, _ := json.Marshal(usage)
			frame.usage = raw
		}
		choices, _ := body["choices"].([]any)
		if len(choices) == 0 {
			return nil
		}
		choice, _ := choices[0].(map[string]any)
		if choice == nil {
			return nil
		}
		if finish, _ := choice["finish_reason"].(string); finish != "" {
			frame.finish = finish
		}
		delta, _ := choice["delta"].(map[string]any)
		message, _ := choice["message"].(map[string]any)
		part := delta
		if part == nil {
			part = message
		}
		if part == nil {
			return nil
		}
		if text, _ := part["content"].(string); text != "" {
			content.WriteString(text)
		}
		if text, _ := part["reasoning_content"].(string); text != "" {
			reasoning.WriteString(text)
		}
		if tools := part["tool_calls"]; tools != nil {
			raw, _ := json.Marshal(tools)
			frame.tools = raw
		}
		return nil
	})
	frame.content = content.String()
	frame.reasoning = reasoning.String()
	return frame, err
}

func upstreamError(body map[string]any) (string, int, string) {
	raw, ok := body["error"]
	if !ok || raw == nil {
		return "", 0, ""
	}
	encoded, _ := json.Marshal(raw)
	var errBody struct {
		Message string `json:"message"`
		Code    any    `json:"code"`
		Type    string `json:"type"`
	}
	_ = json.Unmarshal(encoded, &errBody)
	message := strings.TrimSpace(errBody.Message)
	if message == "" {
		message = strings.TrimSpace(string(encoded))
	}
	kind := accounts.KindUnavailable
	status := http.StatusBadGateway
	code := fmt.Sprint(errBody.Code)
	if code == "insufficient_quota" || strings.Contains(strings.ToLower(message), "quota") {
		kind = accounts.KindQuota
		status = http.StatusTooManyRequests
	}
	return message, status, kind
}

func walkUpstream(r io.Reader, fn func(map[string]any) error) error {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 4<<20)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, ":") {
			continue
		}
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "" || data == "[DONE]" {
			continue
		}
		var outer map[string]any
		if json.Unmarshal([]byte(data), &outer) != nil {
			continue
		}
		body := outer
		if nested, ok := outer["body"]; ok {
			switch typed := nested.(type) {
			case string:
				var inner map[string]any
				if json.Unmarshal([]byte(typed), &inner) == nil {
					body = inner
				}
			case map[string]any:
				body = typed
			}
		}
		if err := fn(body); err != nil {
			return err
		}
	}
	return scanner.Err()
}

func rewriteUpstreamSSE(resp *http.Response, model string) *http.Response {
	pr, pw := io.Pipe()
	go func() {
		defer resp.Body.Close()
		err := writeOpenAIChunks(pw, resp.Body, model)
		_ = pw.CloseWithError(err)
	}()
	headers := resp.Header.Clone()
	headers.Set("Content-Type", "text/event-stream")
	headers.Del("Content-Length")
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     headers,
		Body:       io.NopCloser(pr),
		Request:    resp.Request,
	}
}

func writeOpenAIChunks(w io.Writer, src io.Reader, model string) error {
	err := walkUpstream(src, func(body map[string]any) error {
		frame := frameFromBody(body)
		if frame.errText != "" && frame.content == "" && frame.reasoning == "" && len(frame.tools) == 0 {
			payload, _ := json.Marshal(map[string]any{"error": map[string]any{"message": frame.errText, "type": "api_error"}})
			_, err := fmt.Fprintf(w, "data: %s\n\n", payload)
			return err
		}
		chunk := map[string]any{
			"id": "chatcmpl-qoder", "object": "chat.completion.chunk", "model": model,
			"choices": []any{map[string]any{"index": 0, "delta": deltaOf(frame), "finish_reason": emptyOrNil(frame.finish)}},
		}
		if len(frame.usage) > 0 {
			var usage any
			if json.Unmarshal(frame.usage, &usage) == nil {
				chunk["usage"] = usage
			}
		}
		payload, err := json.Marshal(chunk)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintf(w, "data: %s\n\n", payload)
		return err
	})
	if err != nil {
		return err
	}
	_, err = io.WriteString(w, "data: [DONE]\n\n")
	return err
}

func frameFromBody(body map[string]any) upstreamFrame {
	var frame upstreamFrame
	if errText, status, kind := upstreamError(body); errText != "" {
		frame.errText = errText
		frame.errStatus = status
		frame.errKind = kind
	}
	if usage, ok := body["usage"]; ok && usage != nil {
		raw, _ := json.Marshal(usage)
		frame.usage = raw
	}
	choices, _ := body["choices"].([]any)
	if len(choices) == 0 {
		return frame
	}
	choice, _ := choices[0].(map[string]any)
	if choice == nil {
		return frame
	}
	if finish, _ := choice["finish_reason"].(string); finish != "" {
		frame.finish = finish
	}
	delta, _ := choice["delta"].(map[string]any)
	message, _ := choice["message"].(map[string]any)
	part := delta
	if part == nil {
		part = message
	}
	if part == nil {
		return frame
	}
	if text, _ := part["content"].(string); text != "" {
		frame.content = text
	}
	if text, _ := part["reasoning_content"].(string); text != "" {
		frame.reasoning = text
	}
	if tools := part["tool_calls"]; tools != nil {
		raw, _ := json.Marshal(tools)
		frame.tools = raw
	}
	return frame
}

func deltaOf(frame upstreamFrame) map[string]any {
	delta := map[string]any{}
	if frame.content != "" {
		delta["content"] = frame.content
	}
	if frame.reasoning != "" {
		delta["reasoning_content"] = frame.reasoning
	}
	if len(frame.tools) > 0 {
		var tools any
		if json.Unmarshal(frame.tools, &tools) == nil {
			delta["tool_calls"] = tools
		}
	}
	return delta
}

func emptyOrNil(value string) any {
	if value == "" {
		return nil
	}
	return value
}
