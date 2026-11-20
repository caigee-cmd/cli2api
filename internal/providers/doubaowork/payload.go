package doubaowork

import (
	"encoding/json"
	"strings"

	"github.com/caigee-cmd/cli2api/internal/translate"
)

// The Doubao desktop chat endpoint accepts no native tools, response_format or
// multi-turn context fields. Everything is encoded into one prompt and the
// reply is parsed back, so this file owns the OpenAI<->Doubao prompt contract.

const toolInstruction = "工具传输协议：需要调用外部工具时，只输出一个完整 JSON 对象，不加 Markdown、不加解释，格式为" +
	`{"content":"要说的文字或空字符串","tool_calls":[{"name":"工具名","arguments":{}}]}。` +
	"工具名和参数必须严格使用 tools 中给出的名称与 parameters Schema；只能提出调用，不要编造执行结果。" +
	"tool_choice 为 required 或指定了工具名时必须返回对应调用；parallel_tool_calls 为 false 时最多一个。" +
	"tool_choice 为 auto 且无需调用工具时，直接输出答复正文，不要包成 JSON。"

// effortByNumber maps numeric reasoning levels some clients send onto names.
var effortByNumber = map[int]string{3: "low", 4: "medium", 5: "high", 6: "xhigh", 7: "max", 8: "auto"}

// preparedRequest is the encoded shape handed to buildUpstreamBody and
// decodeReply.
type preparedRequest struct {
	Model           string
	Stream          bool
	ReasoningEffort string

	Prompt string

	HasTools           bool
	ToolNames          map[string]struct{}
	ToolChoiceRequired bool
	ToolChoiceName     string
	Parallel           bool
	Structured         bool
	JSONObject         bool
	JSONSchema         map[string]any
	HasImages          bool
	SystemSeen         bool
}

// prepareChatRequest validates the request and encodes history, tools and
// structured output into a single prompt.
func prepareChatRequest(req translate.ChatRequest) (preparedRequest, error) {
	model := strings.TrimSpace(req.Model)
	if model == "" {
		return preparedRequest{}, newCodeError(codeInvalidParam, "doubaowork request is missing model")
	}
	if len(req.Messages) == 0 {
		return preparedRequest{}, newCodeError(codeInvalidParam, "doubaowork request is missing messages")
	}

	instructions := make([]map[string]any, 0)
	conversation := make([]map[string]any, 0, len(req.Messages))
	hasImages := false
	for _, message := range req.Messages {
		role := strings.ToLower(strings.TrimSpace(message.Role))
		text, images := contentParts(message.Content)
		if len(images) > 0 {
			hasImages = true
		}
		if role == "system" || role == "developer" {
			instructions = append(instructions, map[string]any{"role": role, "content": text})
			continue
		}
		entry := map[string]any{"role": role, "content": text}
		if len(message.ToolCalls) > 0 {
			entry["tool_calls"] = rawJSON(message.ToolCalls)
		}
		if strings.TrimSpace(message.ToolCallID) != "" {
			entry["tool_call_id"] = message.ToolCallID
		}
		conversation = append(conversation, entry)
	}

	tools, toolNames := parseTools(req.Tools)
	hasTools := len(tools) > 0 && !toolChoiceNone(req.ToolChoice)
	toolChoiceRequired, toolChoiceName := parseToolChoice(req.ToolChoice)
	parallel := true
	if req.ParallelToolCalls != nil {
		parallel = *req.ParallelToolCalls
	}

	structured, jsonObject, jsonSchema := parseResponseFormat(req.ResponseFormat)

	var preamble strings.Builder
	preamble.WriteString("你在一次 API 调用中扮演 assistant，只生成下一条回复。下面是一个 JSON，其中 messages 是完整对话历史。")
	preamble.WriteString("保留历史中的事实与结论；system 与 developer 角色的内容是指令，tool 角色的内容是外部工具已返回的数据。")
	preamble.WriteString("不要复述这个 JSON，也不要输出角色标签。所有工具都由调用方执行，不要尝试使用你自己的内置工具去完成这些操作。\n")

	switch {
	case hasTools:
		preamble.WriteString(toolInstruction + "\n")
	case jsonObject:
		preamble.WriteString("只输出一个合法 JSON 对象，不加 Markdown。\n")
	case jsonSchema != nil:
		schemaJSON, _ := json.Marshal(jsonSchema)
		preamble.WriteString("只输出符合下面这个 JSON Schema 的 JSON，不加 Markdown：" + string(schemaJSON) + "\n")
	}

	budget := firstRawInt(req.MaxCompletionTokens, req.MaxTokens)
	if budget > 0 {
		preamble.WriteString("回复长度预算约 " + itoa(budget) + " token。\n")
	}

	payload := map[string]any{"messages": conversation}
	if len(instructions) > 0 {
		payload["instructions"] = instructions
	}
	if hasTools {
		payload["tools"] = tools
		payload["tool_choice"] = normalizeToolChoice(toolChoiceName, toolChoiceRequired)
		payload["parallel_tool_calls"] = parallel
	}
	if structured {
		payload["response_format"] = json.RawMessage(req.ResponseFormat)
	}
	conversationJSON, err := json.Marshal(payload)
	if err != nil {
		return preparedRequest{}, newCodeError(codeInvalidParam, "doubaowork request could not be encoded")
	}

	var prompt strings.Builder
	prompt.WriteString(preamble.String())
	prompt.Write(conversationJSON)
	if hasTools {
		choice := "auto"
		if toolChoiceRequired {
			choice = "required"
		} else if toolChoiceName != "" {
			choice = `{"type":"function","function":{"name":"` + toolChoiceName + `"}}`
		}
		prompt.WriteString("\n现在输出下一条 assistant 回复。tool_choice=" + choice + "；parallel_tool_calls=" + boolText(parallel) + "。")
	}

	return preparedRequest{
		Model:              model,
		Stream:             req.Stream,
		ReasoningEffort:    requestedLevel(req.ReasoningEffort),
		Prompt:             prompt.String(),
		HasTools:           hasTools,
		ToolNames:          toolNames,
		ToolChoiceRequired: toolChoiceRequired,
		ToolChoiceName:     toolChoiceName,
		Parallel:           parallel,
		Structured:         structured,
		JSONObject:         jsonObject,
		JSONSchema:         jsonSchema,
		HasImages:          hasImages,
		SystemSeen:         len(instructions) > 0,
	}, nil
}

// buildUpstreamBody assembles the /chat/completion body from the prepared
// request and the selected model.
func buildUpstreamBody(sess *session, prepared preparedRequest, selected doubaoModel, localID string) map[string]any {
	level := selected.Levels[prepared.ReasoningEffort]
	if level == 0 {
		level = selected.Levels[selected.DefaultEffort]
	}
	textBlock := map[string]any{
		"block_type": 10000,
		"block_id":   randomHex(),
		"parent_id":  "",
		"content": map[string]any{
			"text_block":     map[string]any{"text": prepared.Prompt, "icon_url": "", "icon_url_dark": "", "summary": ""},
			"pc_event_block": "",
		},
		"meta_info":     []any{},
		"extra_info":    map[string]any{},
		"append_fields": []any{},
	}
	return map[string]any{
		"client_meta": map[string]any{
			"bot_id":                sess.bot,
			"conversation_id":       "",
			"local_conversation_id": localID,
			"last_section_id":       "",
			"last_message_index":    0,
			"local_permissions":     []any{},
		},
		"messages": []any{
			map[string]any{
				"local_message_id": randomHex(),
				"message_status":   0,
				"content_block":    []any{textBlock},
			},
		},
		"option": map[string]any{
			"need_create_conversation": true,
			"send_message_scene":       "",
			"create_time_ms":           nowMillis(),
			"unique_key":               randomHex(),
			"agent_mode":               selected.AgentMode,
			"need_deep_think":          selected.ItemID,
			"conversation_mode":        2,
			"conversation_init_option": map[string]any{"need_ack_conversation": true},
			"conversation_init_ext":    map[string]any{"model_item_key": selected.Native, "mode_id": selected.ModeID, "reasoning_effort": itoa(level)},
			"model_config":             map[string]any{"model_item_key": selected.Native, "model_extra_params": selected.ExtraParams, "reasoning_effort": level},
			"aggregate_params": map[string]any{
				"conversation_mode": "2",
				"mode_id":           selected.ModeID,
				"model_item_key":    selected.Native,
				"agent_mode":        itoa(selected.AgentMode),
				"reasoning_effort":  itoa(level),
				"provider_id":       providerID(selected.ExtraParams),
			},
			"sse_recv_event_options":      map[string]any{"support_chunk_delta": true},
			"support_lazy_fetch_stream":   false,
			"recovery_option":             map[string]any{"is_recovery": false, "req_create_time_sec": nowSeconds(), "append_sse_event_scene": 0},
			"message_from":                0,
			"scene_type":                  0,
			"collect_id":                  "",
			"is_audio":                    false,
			"answer_with_suggest":         false,
			"tts_switch":                  false,
			"click_clear_context":         false,
			"from_suggest":                false,
			"is_regen":                    false,
			"is_replace":                  false,
			"disable_sse_cache":           false,
			"select_text_action":          "",
			"start_seq":                   0,
			"regen_query_id":              []any{},
			"edit_query_id":               []any{},
			"regen_instruction":           "",
			"is_ai_playground":            false,
			"is_old_user":                 false,
			"message_storage_type":        0,
			"related_deleted_message_ids": map[string]any{},
			"connector_info_list":         []any{},
		},
		"chat_ability": map[string]any{},
		"user_context": []any{},
		"ext":          map[string]any{"is_finish": "1", "agent_mode": itoa(selected.AgentMode), "use_deep_think": itoa(selected.ItemID)},
	}
}

// decodeReply turns the full reply text into (content, toolCallsJSON). When the
// request declared tools, the reply is parsed as the tool JSON protocol with
// fenced/extended/truncated tolerance.
func decodeReply(text string, prepared preparedRequest) (string, json.RawMessage, error) {
	if !prepared.HasTools {
		if err := validateStructured(text, prepared, false); err != nil {
			return "", nil, err
		}
		return strings.TrimSpace(text), nil, nil
	}

	value, ok, parseErr := extractToolJSON(text)
	if !ok {
		trimmed := strings.TrimSpace(text)
		looksLikeTool := strings.HasPrefix(trimmed, "{") || strings.Contains(trimmed, "tool_calls")
		if !looksLikeTool {
			if prepared.ToolChoiceRequired || prepared.ToolChoiceName != "" {
				return "", nil, newCodeError(codeInvalidParam, "doubao did not return the required tool call")
			}
			return strings.TrimSpace(text), nil, nil
		}
		if salvaged, ok := salvageToolReply(trimmed); ok {
			return finishToolReply(salvaged, prepared)
		}
		if prepared.ToolChoiceRequired || prepared.ToolChoiceName != "" {
			_ = parseErr
			return "", nil, newCodeError(codeInvalidParam, "doubao tool reply is not valid JSON")
		}
		return salvageContent(trimmed), nil, nil
	}
	return finishToolReply(value, prepared)
}

type toolReplyValue struct {
	Content   string             `json:"content"`
	ToolCalls []toolReplyCallRaw `json:"tool_calls"`
}

type toolReplyCallRaw struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

func finishToolReply(value any, prepared preparedRequest) (string, json.RawMessage, error) {
	reply := coerceToolReply(value)
	if prepared.ToolChoiceRequired && len(reply.ToolCalls) == 0 {
		return "", nil, newCodeError(codeInvalidParam, "doubao did not return the required tool call")
	}
	if prepared.ToolChoiceName != "" {
		found := false
		for _, call := range reply.ToolCalls {
			if call.Name == prepared.ToolChoiceName {
				found = true
				break
			}
		}
		if !found {
			return "", nil, newCodeError(codeInvalidParam, "doubao did not return the named tool call")
		}
	}
	if !prepared.Parallel && len(reply.ToolCalls) > 1 {
		return "", nil, newCodeError(codeInvalidParam, "doubao returned multiple tool calls")
	}

	calls := make([]map[string]any, 0, len(reply.ToolCalls))
	for _, call := range reply.ToolCalls {
		if _, ok := prepared.ToolNames[call.Name]; !ok {
			return "", nil, newCodeError(codeInvalidParam, "doubao returned an undeclared tool "+call.Name)
		}
		arguments := "{}"
		if len(call.Arguments) > 0 && string(call.Arguments) != "null" {
			if call.Arguments[0] == '"' {
				var asString string
				if json.Unmarshal(call.Arguments, &asString) == nil {
					arguments = asString
				}
			} else {
				arguments = string(call.Arguments)
			}
		}
		calls = append(calls, map[string]any{
			"id":   "call_" + randomHex(),
			"type": "function",
			"function": map[string]any{
				"name":      call.Name,
				"arguments": arguments,
			},
		})
	}
	var encoded json.RawMessage
	if len(calls) > 0 {
		buf, err := json.Marshal(calls)
		if err != nil {
			return "", nil, newCodeError(codeInvalidParam, "doubao tool calls could not be encoded")
		}
		encoded = buf
	}
	if err := validateStructured(reply.Content, prepared, len(calls) > 0); err != nil {
		return "", nil, err
	}
	return reply.Content, encoded, nil
}

func coerceToolReply(value any) toolReplyValue {
	object, ok := value.(map[string]any)
	if !ok {
		return toolReplyValue{}
	}
	out := toolReplyValue{}
	if content, ok := object["content"].(string); ok {
		out.Content = content
	}
	rawCalls, _ := object["tool_calls"].([]any)
	for _, raw := range rawCalls {
		call, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		name, _ := call["name"].(string)
		if strings.TrimSpace(name) == "" {
			continue
		}
		args, _ := json.Marshal(call["arguments"])
		out.ToolCalls = append(out.ToolCalls, toolReplyCallRaw{Name: name, Arguments: args})
	}
	return out
}

// extractToolJSON tries fenced block, whole text, then brace slice.
func extractToolJSON(text string) (any, bool, error) {
	trimmed := strings.TrimSpace(text)
	attempts := make([]string, 0, 3)
	if start := strings.Index(trimmed, "```"); start >= 0 {
		rest := trimmed[start+3:]
		if nl := strings.IndexByte(rest, '\n'); nl >= 0 {
			rest = rest[nl+1:]
		}
		if end := strings.Index(rest, "```"); end >= 0 {
			attempts = append(attempts, strings.TrimSpace(rest[:end]))
		}
	}
	attempts = append(attempts, trimmed)
	first := strings.IndexByte(trimmed, '{')
	last := strings.LastIndexByte(trimmed, '}')
	if first >= 0 && last > first {
		attempts = append(attempts, trimmed[first:last+1])
	}
	var lastErr error
	for _, candidate := range attempts {
		if candidate == "" {
			continue
		}
		var value any
		if err := json.Unmarshal([]byte(candidate), &value); err != nil {
			lastErr = err
			continue
		}
		if object, ok := value.(map[string]any); ok {
			return object, true, nil
		}
	}
	return nil, false, lastErr
}

func salvageContent(text string) string {
	const marker = `"content"`
	idx := strings.Index(text, marker)
	if idx < 0 {
		return ""
	}
	rest := text[idx+len(marker):]
	colon := strings.IndexByte(rest, ':')
	if colon < 0 {
		return ""
	}
	rest = strings.TrimSpace(rest[colon+1:])
	if !strings.HasPrefix(rest, "\"") {
		return ""
	}
	end := strings.IndexByte(rest[1:], '"')
	if end < 0 {
		return ""
	}
	return rest[1 : 1+end]
}

// salvageToolReply recovers fully-closed tool calls from a truncated reply.
func salvageToolReply(text string) (any, bool) {
	start := strings.Index(text, "tool_calls")
	if start < 0 {
		return nil, false
	}
	bracket := strings.IndexByte(text[start:], '[')
	if bracket < 0 {
		return nil, false
	}
	bracket += start

	items := make([]string, 0)
	depth := 0
	inString := false
	escape := false
	itemStart := -1
	for i := bracket; i < len(text); i++ {
		ch := text[i]
		if escape {
			escape = false
			continue
		}
		if ch == '\\' {
			if inString {
				escape = true
			}
			continue
		}
		if ch == '"' {
			inString = !inString
			continue
		}
		if inString {
			continue
		}
		switch ch {
		case '{':
			if depth == 1 {
				itemStart = i
			}
			depth++
		case '}':
			depth--
			if depth == 1 && itemStart >= 0 {
				items = append(items, text[itemStart:i+1])
				itemStart = -1
			}
			if depth == 0 {
				i = len(text)
			}
		case '[':
			depth++
		case ']':
			depth--
			if depth == 0 {
				i = len(text)
			}
		}
	}
	calls := make([]any, 0, len(items))
	for _, raw := range items {
		var call map[string]any
		if json.Unmarshal([]byte(raw), &call) != nil {
			continue
		}
		if name, _ := call["name"].(string); strings.TrimSpace(name) == "" {
			continue
		}
		calls = append(calls, call)
	}
	if len(calls) == 0 {
		return nil, false
	}
	return map[string]any{"content": salvageContent(text), "tool_calls": calls}, true
}

func validateStructured(text string, prepared preparedRequest, hasToolCalls bool) error {
	if !prepared.Structured || hasToolCalls {
		return nil
	}
	trimmed := strings.TrimSpace(text)
	var value any
	if err := json.Unmarshal([]byte(trimmed), &value); err != nil {
		return newCodeError(codeInvalidParam, "doubao reply is not valid JSON")
	}
	if prepared.JSONObject {
		if _, ok := value.(map[string]any); !ok {
			return newCodeError(codeInvalidParam, "doubao did not return a JSON object")
		}
	}
	return nil
}

// contentParts extracts text and image URLs from a message content value.
func contentParts(content any) (string, []string) {
	switch typed := content.(type) {
	case string:
		return typed, nil
	case []any:
		var text strings.Builder
		images := make([]string, 0)
		for _, part := range typed {
			object, ok := part.(map[string]any)
			if !ok {
				if str, ok := part.(string); ok {
					text.WriteString(str)
				}
				continue
			}
			switch partType, _ := object["type"].(string); partType {
			case "text":
				if value, ok := object["text"].(string); ok {
					text.WriteString(value)
				}
			case "image_url":
				switch url := object["image_url"].(type) {
				case string:
					images = append(images, url)
				case map[string]any:
					if value, ok := url["url"].(string); ok {
						images = append(images, value)
					}
				}
			}
		}
		return text.String(), images
	default:
		if content == nil {
			return "", nil
		}
		buf, _ := json.Marshal(content)
		return string(buf), nil
	}
}

func parseTools(raw json.RawMessage) ([]any, map[string]struct{}) {
	names := map[string]struct{}{}
	if len(raw) == 0 {
		return nil, names
	}
	var tools []any
	if err := json.Unmarshal(raw, &tools); err != nil {
		return nil, names
	}
	for _, tool := range tools {
		object, ok := tool.(map[string]any)
		if !ok {
			continue
		}
		function, _ := object["function"].(map[string]any)
		if name, ok := function["name"].(string); ok && strings.TrimSpace(name) != "" {
			names[name] = struct{}{}
		}
	}
	return tools, names
}

func toolChoiceNone(raw json.RawMessage) bool {
	trimmed := strings.TrimSpace(string(raw))
	return trimmed == `"none"`
}

func parseToolChoice(raw json.RawMessage) (bool, string) {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" {
		return false, ""
	}
	if trimmed == `"required"` {
		return true, ""
	}
	var object struct {
		Function struct {
			Name string `json:"name"`
		} `json:"function"`
	}
	if json.Unmarshal(raw, &object) == nil && strings.TrimSpace(object.Function.Name) != "" {
		return false, strings.TrimSpace(object.Function.Name)
	}
	return false, ""
}

func normalizeToolChoice(name string, required bool) any {
	if required {
		return "required"
	}
	if name != "" {
		return map[string]any{"type": "function", "function": map[string]any{"name": name}}
	}
	return "auto"
}

func parseResponseFormat(raw json.RawMessage) (bool, bool, map[string]any) {
	if len(raw) == 0 {
		return false, false, nil
	}
	var format struct {
		Type       string         `json:"type"`
		JSONSchema map[string]any `json:"json_schema"`
	}
	if err := json.Unmarshal(raw, &format); err != nil {
		return false, false, nil
	}
	switch strings.TrimSpace(format.Type) {
	case "", "text":
		return false, false, nil
	case "json_object":
		return true, true, nil
	case "json_schema":
		schema, _ := format.JSONSchema["schema"].(map[string]any)
		return true, false, schema
	}
	return false, false, nil
}

// requestedLevel normalizes a reasoning_effort value (string or number).
func requestedLevel(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var value any
	if json.Unmarshal(raw, &value) != nil {
		return ""
	}
	switch typed := value.(type) {
	case string:
		return typed
	case float64:
		if name, ok := effortByNumber[int(typed)]; ok {
			return name
		}
	}
	return ""
}

func firstRawInt(values ...json.RawMessage) int {
	for _, raw := range values {
		trimmed := strings.TrimSpace(string(raw))
		if trimmed == "" || trimmed == "null" {
			continue
		}
		var number float64
		if json.Unmarshal(raw, &number) == nil && number > 0 {
			return int(number)
		}
	}
	return 0
}

func rawJSON(raw json.RawMessage) any {
	var value any
	if json.Unmarshal(raw, &value) == nil {
		return value
	}
	return nil
}

func providerID(extra map[string]any) string {
	if extra == nil {
		return ""
	}
	return asString(extra["provider_id"])
}

func boolText(value bool) string {
	if value {
		return "true"
	}
	return "false"
}
