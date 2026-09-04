package translate

import (
	"encoding/json"
	"fmt"
	"strings"
)

// AnthropicMessagesRequest is the supported subset of Anthropic's Messages API.
type AnthropicMessagesRequest struct {
	Model         string             `json:"model"`
	Messages      []AnthropicMessage `json:"messages"`
	System        json.RawMessage    `json:"system,omitempty"`
	MaxTokens     json.RawMessage    `json:"max_tokens"`
	Stream        bool               `json:"stream"`
	Tools         json.RawMessage    `json:"tools,omitempty"`
	ToolChoice    json.RawMessage    `json:"tool_choice,omitempty"`
	Thinking      json.RawMessage    `json:"thinking,omitempty"`
	Temperature   json.RawMessage    `json:"temperature,omitempty"`
	TopP          json.RawMessage    `json:"top_p,omitempty"`
	StopSequences json.RawMessage    `json:"stop_sequences,omitempty"`
	OutputConfig  json.RawMessage    `json:"output_config,omitempty"`
}

type AnthropicMessage struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

// ResponsesRequest is the supported stateless subset of OpenAI's Responses API.
type ResponsesRequest struct {
	Model             string          `json:"model"`
	Input             json.RawMessage `json:"input"`
	Instructions      json.RawMessage `json:"instructions,omitempty"`
	MaxOutputTokens   json.RawMessage `json:"max_output_tokens,omitempty"`
	Stream            bool            `json:"stream"`
	Tools             json.RawMessage `json:"tools,omitempty"`
	ToolChoice        json.RawMessage `json:"tool_choice,omitempty"`
	Reasoning         json.RawMessage `json:"reasoning,omitempty"`
	TopP              json.RawMessage `json:"top_p,omitempty"`
	ParallelToolCalls *bool           `json:"parallel_tool_calls,omitempty"`
	Text              json.RawMessage `json:"text,omitempty"`
	PreviousID        string          `json:"previous_response_id,omitempty"`
	Conversation      json.RawMessage `json:"conversation,omitempty"`
}

type compatibilityToolCall struct {
	ID        string
	Name      string
	Arguments json.RawMessage
}

type compatibilityToolResult struct {
	CallID  string
	Content string
	Images  []any
}

func TranslateAnthropicMessages(request AnthropicMessagesRequest) (ChatRequest, error) {
	chat := ChatRequest{
		Model:       strings.TrimSpace(request.Model),
		Stream:      request.Stream,
		MaxTokens:   request.MaxTokens,
		Temperature: request.Temperature,
		TopP:        request.TopP,
		Stop:        request.StopSequences,
		Thinking:    request.Thinking,
	}
	if chat.Model == "" {
		return ChatRequest{}, fmt.Errorf("model required")
	}
	if len(request.System) > 0 && string(request.System) != "null" {
		systemText, err := compatibilityText(request.System, map[string]bool{"text": true})
		if err != nil {
			return ChatRequest{}, fmt.Errorf("system: %w", err)
		}
		if systemText != "" {
			chat.Messages = append(chat.Messages, ChatMessage{Role: "system", Content: systemText})
		}
	}
	for messageIndex, message := range request.Messages {
		role := strings.ToLower(strings.TrimSpace(message.Role))
		if role != "user" && role != "assistant" {
			return ChatRequest{}, fmt.Errorf("messages[%d].role must be user or assistant", messageIndex)
		}
		text, toolCalls, toolResults, err := anthropicMessageParts(message.Content)
		if err != nil {
			return ChatRequest{}, fmt.Errorf("messages[%d]: %w", messageIndex, err)
		}
		if role == "assistant" {
			if len(toolResults) > 0 {
				return ChatRequest{}, fmt.Errorf("messages[%d]: tool_result is only valid in a user message", messageIndex)
			}
			if contentPresent(text) || len(toolCalls) > 0 {
				chat.Messages = append(chat.Messages, ChatMessage{Role: role, Content: text, ToolCalls: marshalToolCalls(toolCalls)})
			}
			continue
		}
		if len(toolCalls) > 0 {
			return ChatRequest{}, fmt.Errorf("messages[%d]: tool_use is only valid in an assistant message", messageIndex)
		}
		if contentPresent(text) {
			chat.Messages = append(chat.Messages, ChatMessage{Role: role, Content: text})
		}
		for _, toolResult := range toolResults {
			if toolResult.CallID == "" {
				return ChatRequest{}, fmt.Errorf("messages[%d]: tool_result requires tool_use_id", messageIndex)
			}
			chat.Messages = append(chat.Messages, ChatMessage{Role: "tool", ToolCallID: toolResult.CallID, Content: toolResult.Content})
			if len(toolResult.Images) > 0 {
				chat.Messages = append(chat.Messages, ChatMessage{Role: "user", Content: toolResult.Images})
			}
		}
	}
	if len(chat.Messages) == 0 {
		return ChatRequest{}, fmt.Errorf("messages required")
	}
	tools, err := translateAnthropicTools(request.Tools)
	if err != nil {
		return ChatRequest{}, err
	}
	chat.Tools = tools
	toolChoice, err := translateAnthropicToolChoice(request.ToolChoice)
	if err != nil {
		return ChatRequest{}, err
	}
	chat.ToolChoice = toolChoice
	chat.ParallelToolCalls = anthropicParallelToolCalls(request.ToolChoice)
	if effort := anthropicReasoningEffort(request.OutputConfig); len(effort) > 0 {
		chat.ReasoningEffort = effort
	}
	if err := validateToolChoice(chat.Tools, chat.ToolChoice); err != nil {
		return ChatRequest{}, err
	}
	return chat, nil
}

func TranslateResponses(request ResponsesRequest) (ChatRequest, error) {
	if strings.TrimSpace(request.PreviousID) != "" || !emptyJSON(request.Conversation) {
		return ChatRequest{}, fmt.Errorf("previous_response_id and conversation are not supported; send the complete conversation in input")
	}
	chat := ChatRequest{
		Model:             strings.TrimSpace(request.Model),
		Stream:            request.Stream,
		MaxTokens:         request.MaxOutputTokens,
		ReasoningEffort:   responseReasoningEffort(request.Reasoning),
		TopP:              request.TopP,
		ParallelToolCalls: request.ParallelToolCalls,
	}
	if chat.Model == "" {
		return ChatRequest{}, fmt.Errorf("model required")
	}
	if len(request.Instructions) > 0 && string(request.Instructions) != "null" {
		instructions, err := compatibilityText(request.Instructions, map[string]bool{"input_text": true, "output_text": true, "text": true})
		if err != nil {
			return ChatRequest{}, fmt.Errorf("instructions: %w", err)
		}
		if instructions != "" {
			chat.Messages = append(chat.Messages, ChatMessage{Role: "system", Content: instructions})
		}
	}
	inputMessages, err := translateResponsesInput(request.Input)
	if err != nil {
		return ChatRequest{}, err
	}
	chat.Messages = append(chat.Messages, inputMessages...)
	if len(chat.Messages) == 0 {
		return ChatRequest{}, fmt.Errorf("input required")
	}
	additionalTools, err := responsesAdditionalTools(request.Input)
	if err != nil {
		return ChatRequest{}, err
	}
	tools, err := translateResponsesTools(mergeJSONArray(request.Tools, additionalTools))
	if err != nil {
		return ChatRequest{}, err
	}
	chat.Tools = tools
	toolChoice, err := translateResponsesToolChoice(request.ToolChoice)
	if err != nil {
		return ChatRequest{}, err
	}
	chat.ToolChoice = toolChoice
	chat.ResponseFormat, err = translateResponsesTextFormat(request.Text)
	if err != nil {
		return ChatRequest{}, err
	}
	if err := validateToolChoice(chat.Tools, chat.ToolChoice); err != nil {
		return ChatRequest{}, err
	}
	return chat, nil
}

func anthropicMessageParts(raw json.RawMessage) (any, []compatibilityToolCall, []compatibilityToolResult, error) {
	if rawText, ok := rawJSONString(raw); ok {
		return rawText, nil, nil, nil
	}
	var parts []json.RawMessage
	if err := json.Unmarshal(raw, &parts); err != nil {
		return "", nil, nil, fmt.Errorf("content must be a string or an array of content blocks")
	}
	contentParts := make([]any, 0, len(parts))
	toolCalls := make([]compatibilityToolCall, 0)
	toolResults := make([]compatibilityToolResult, 0)
	for partIndex, part := range parts {
		var block map[string]json.RawMessage
		if err := json.Unmarshal(part, &block); err != nil {
			return "", nil, nil, fmt.Errorf("content[%d] must be an object", partIndex)
		}
		blockType := rawMapString(block, "type")
		switch blockType {
		case "text":
			contentParts = appendContentPart(contentParts, map[string]any{"type": "text", "text": rawMapContentString(block, "text")})
		case "thinking", "redacted_thinking":
			// Thinking signatures are provider-specific. Keep the visible answer
			// path compatible rather than replaying an invalid signed block.
		case "tool_use":
			callID := rawMapString(block, "id")
			name := rawMapString(block, "name")
			if callID == "" || name == "" {
				return "", nil, nil, fmt.Errorf("content[%d] tool_use requires id and name", partIndex)
			}
			arguments := rawMapJSON(block, "input")
			if len(arguments) == 0 {
				arguments = json.RawMessage(`{}`)
			}
			if !json.Valid(arguments) {
				return "", nil, nil, fmt.Errorf("content[%d] tool_use.input must be valid JSON", partIndex)
			}
			toolCalls = append(toolCalls, compatibilityToolCall{ID: callID, Name: name, Arguments: arguments})
		case "tool_result":
			content, images, err := anthropicToolResultContent(rawMapJSON(block, "content"))
			if err != nil {
				return "", nil, nil, fmt.Errorf("content[%d].content: %w", partIndex, err)
			}
			toolResults = append(toolResults, compatibilityToolResult{CallID: rawMapString(block, "tool_use_id"), Content: content, Images: images})
		case "image":
			image, err := anthropicImagePart(block)
			if err != nil {
				return nil, nil, nil, fmt.Errorf("content[%d]: %w", partIndex, err)
			}
			contentParts = append(contentParts, image)
		default:
			return "", nil, nil, fmt.Errorf("content[%d] type %q is not supported", partIndex, blockType)
		}
	}
	if text, ok := compactTextParts(contentParts); ok {
		return text, toolCalls, toolResults, nil
	}
	if len(contentParts) == 1 {
		return contentParts[0], toolCalls, toolResults, nil
	}
	return contentParts, toolCalls, toolResults, nil
}

func anthropicToolResultContent(raw json.RawMessage) (string, []any, error) {
	if text, ok := rawJSONString(raw); ok {
		return text, nil, nil
	}
	var blocks []json.RawMessage
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return "", nil, fmt.Errorf("must be a string or an array of text/image blocks")
	}
	texts := make([]string, 0, len(blocks))
	images := make([]any, 0)
	for index, block := range blocks {
		var source map[string]json.RawMessage
		if err := json.Unmarshal(block, &source); err != nil {
			return "", nil, fmt.Errorf("content[%d] must be an object", index)
		}
		switch rawMapString(source, "type") {
		case "text":
			texts = append(texts, rawMapContentString(source, "text"))
		case "image":
			image, err := anthropicImagePart(source)
			if err != nil {
				return "", nil, err
			}
			images = append(images, image)
		default:
			return "", nil, fmt.Errorf("content[%d] type %q is not supported", index, rawMapString(source, "type"))
		}
	}
	return strings.Join(texts, "\n"), images, nil
}

func anthropicImagePart(block map[string]json.RawMessage) (map[string]any, error) {
	var source map[string]json.RawMessage
	if json.Unmarshal(rawMapJSON(block, "source"), &source) != nil {
		return nil, fmt.Errorf("image.source must be an object")
	}
	imageURL := rawMapString(source, "url")
	if imageURL == "" {
		mediaType := rawMapString(source, "media_type")
		data := rawMapString(source, "data")
		if mediaType == "" || data == "" || rawMapString(source, "type") != "base64" {
			return nil, fmt.Errorf("image source must contain a base64 data or url")
		}
		imageURL = "data:" + mediaType + ";base64," + data
	}
	return map[string]any{"type": "image_url", "image_url": map[string]string{"url": imageURL}}, nil
}

func translateAnthropicTools(raw json.RawMessage) (json.RawMessage, error) {
	if emptyJSON(raw) {
		return nil, nil
	}
	var source []map[string]json.RawMessage
	if err := json.Unmarshal(raw, &source); err != nil {
		return nil, fmt.Errorf("tools must be an array")
	}
	tools := make([]map[string]any, 0, len(source))
	for toolIndex, tool := range source {
		name := rawMapString(tool, "name")
		if name == "" {
			return nil, fmt.Errorf("tools[%d].name required", toolIndex)
		}
		parameters := rawMapJSON(tool, "input_schema")
		if len(parameters) == 0 {
			parameters = json.RawMessage(`{"type":"object","properties":{}}`)
		}
		tools = append(tools, map[string]any{
			"type": "function",
			"function": map[string]any{
				"name": name, "description": rawMapString(tool, "description"), "parameters": json.RawMessage(parameters),
			},
		})
	}
	return json.Marshal(tools)
}

func translateAnthropicToolChoice(raw json.RawMessage) (json.RawMessage, error) {
	if emptyJSON(raw) {
		return nil, nil
	}
	var source map[string]json.RawMessage
	if err := json.Unmarshal(raw, &source); err != nil {
		return nil, fmt.Errorf("tool_choice must be an object")
	}
	switch rawMapString(source, "type") {
	case "auto", "none":
		return json.Marshal(rawMapString(source, "type"))
	case "any":
		return json.RawMessage(`"required"`), nil
	case "tool":
		name := rawMapString(source, "name")
		if name == "" {
			return nil, fmt.Errorf("tool_choice.name required for type tool")
		}
		return json.Marshal(map[string]any{"type": "function", "function": map[string]string{"name": name}})
	default:
		return nil, fmt.Errorf("tool_choice type %q is not supported", rawMapString(source, "type"))
	}
}

func translateResponsesInput(raw json.RawMessage) ([]ChatMessage, error) {
	if rawText, ok := rawJSONString(raw); ok {
		if rawText == "" {
			return nil, fmt.Errorf("input required")
		}
		return []ChatMessage{{Role: "user", Content: rawText}}, nil
	}
	var items []json.RawMessage
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, fmt.Errorf("input must be a string or an array")
	}
	messages := make([]ChatMessage, 0, len(items))
	for itemIndex, item := range items {
		var source map[string]json.RawMessage
		if err := json.Unmarshal(item, &source); err != nil {
			return nil, fmt.Errorf("input[%d] must be an object", itemIndex)
		}
		itemType := rawMapString(source, "type")
		switch itemType {
		case "", "message":
			role := strings.ToLower(rawMapString(source, "role"))
			if role != "system" && role != "developer" && role != "user" && role != "assistant" {
				return nil, fmt.Errorf("input[%d].role is not supported", itemIndex)
			}
			content, err := compatibilityContent(rawMapJSON(source, "content"), map[string]bool{"input_text": true, "output_text": true, "text": true}, true)
			if err != nil {
				return nil, fmt.Errorf("input[%d].content: %w", itemIndex, err)
			}
			messages = append(messages, ChatMessage{Role: role, Content: content})
		case "function_call_output":
			callID := rawMapString(source, "call_id")
			if callID == "" {
				return nil, fmt.Errorf("input[%d].call_id required", itemIndex)
			}
			content, err := compatibilityText(rawMapJSON(source, "output"), map[string]bool{"input_text": true, "output_text": true, "text": true})
			if err != nil {
				return nil, fmt.Errorf("input[%d].output: %w", itemIndex, err)
			}
			messages = append(messages, ChatMessage{Role: "tool", ToolCallID: callID, Content: content})
		case "input_file":
			return nil, fmt.Errorf("input[%d] file inputs are not supported by the Qoder upstream", itemIndex)
		case "function_call":
			name := rawMapString(source, "name")
			callID := firstRawMapString(source, "call_id", "id")
			if name == "" || callID == "" {
				return nil, fmt.Errorf("input[%d] function_call requires name and call_id", itemIndex)
			}
			arguments := rawMapJSON(source, "arguments")
			if value, ok := rawJSONString(arguments); ok {
				arguments = json.RawMessage(value)
			}
			if len(arguments) == 0 {
				arguments = json.RawMessage(`{}`)
			}
			if !json.Valid(arguments) {
				return nil, fmt.Errorf("input[%d].arguments must be valid JSON", itemIndex)
			}
			messages = append(messages, ChatMessage{Role: "assistant", Content: "", ToolCalls: marshalToolCalls([]compatibilityToolCall{{ID: callID, Name: name, Arguments: arguments}})})
		case "additional_tools":
			continue
		default:
			return nil, fmt.Errorf("input[%d] type %q is not supported", itemIndex, itemType)
		}
	}
	return messages, nil
}

func translateResponsesTools(raw json.RawMessage) (json.RawMessage, error) {
	if emptyJSON(raw) {
		return nil, nil
	}
	var source []map[string]json.RawMessage
	if err := json.Unmarshal(raw, &source); err != nil {
		return nil, fmt.Errorf("tools must be an array")
	}
	tools := make([]map[string]any, 0, len(source))
	for toolIndex, tool := range source {
		if toolType := rawMapString(tool, "type"); toolType != "function" {
			return nil, fmt.Errorf("tools[%d] type %q is not supported", toolIndex, toolType)
		}
		name := rawMapString(tool, "name")
		if name == "" {
			return nil, fmt.Errorf("tools[%d].name required", toolIndex)
		}
		parameters := rawMapJSON(tool, "parameters")
		if len(parameters) == 0 {
			parameters = json.RawMessage(`{"type":"object","properties":{}}`)
		}
		tools = append(tools, map[string]any{
			"type": "function",
			"function": map[string]any{
				"name": name, "description": rawMapString(tool, "description"), "parameters": json.RawMessage(parameters),
			},
		})
	}
	return json.Marshal(tools)
}

func translateResponsesToolChoice(raw json.RawMessage) (json.RawMessage, error) {
	if emptyJSON(raw) {
		return nil, nil
	}
	if _, ok := rawJSONString(raw); ok {
		return raw, nil
	}
	var source map[string]json.RawMessage
	if err := json.Unmarshal(raw, &source); err != nil {
		return nil, fmt.Errorf("tool_choice must be a string or an object")
	}
	if rawMapString(source, "type") != "function" {
		return nil, fmt.Errorf("tool_choice type %q is not supported", rawMapString(source, "type"))
	}
	name := rawMapString(source, "name")
	if name == "" {
		return nil, fmt.Errorf("tool_choice.name required")
	}
	return json.Marshal(map[string]any{"type": "function", "function": map[string]string{"name": name}})
}

func responseReasoningEffort(raw json.RawMessage) json.RawMessage {
	if emptyJSON(raw) {
		return nil
	}
	if effort, ok := rawJSONString(raw); ok {
		return json.RawMessage(fmt.Sprintf("%q", effort))
	}
	var source map[string]json.RawMessage
	if json.Unmarshal(raw, &source) != nil {
		return nil
	}
	effort := rawMapString(source, "effort")
	if effort == "" {
		return nil
	}
	encoded, _ := json.Marshal(effort)
	return encoded
}

func anthropicReasoningEffort(raw json.RawMessage) json.RawMessage {
	if emptyJSON(raw) {
		return nil
	}
	var source map[string]json.RawMessage
	if json.Unmarshal(raw, &source) != nil {
		return nil
	}
	effort := rawMapString(source, "effort")
	if effort == "" {
		return nil
	}
	encoded, _ := json.Marshal(effort)
	return encoded
}

func anthropicParallelToolCalls(raw json.RawMessage) *bool {
	if emptyJSON(raw) {
		return nil
	}
	var source map[string]json.RawMessage
	if json.Unmarshal(raw, &source) != nil {
		return nil
	}
	var disabled bool
	if json.Unmarshal(source["disable_parallel_tool_use"], &disabled) == nil && disabled {
		return &disabled
	}
	return nil
}

func ValidateChatRequest(request ChatRequest) error {
	if strings.TrimSpace(request.Model) == "" {
		return fmt.Errorf("model required")
	}
	if len(request.MaxTokens) > 0 && len(request.MaxCompletionTokens) > 0 {
		return fmt.Errorf("max_tokens and max_completion_tokens are mutually exclusive")
	}
	return validateToolChoice(request.Tools, request.ToolChoice)
}

func validateToolChoice(tools, choice json.RawMessage) error {
	if emptyJSON(choice) {
		return nil
	}
	if emptyJSON(tools) {
		return fmt.Errorf("tool_choice requires tools")
	}
	var declared []map[string]json.RawMessage
	if err := json.Unmarshal(tools, &declared); err != nil || len(declared) == 0 {
		return fmt.Errorf("tool_choice requires tools")
	}
	names := make(map[string]bool, len(declared))
	for _, tool := range declared {
		name, _ := rawJSONString(tool["name"])
		if name == "" {
			fn, _ := tool["function"]
			var function map[string]json.RawMessage
			if json.Unmarshal(fn, &function) == nil {
				name, _ = rawJSONString(function["name"])
			}
		}
		if name != "" {
			names[strings.TrimSpace(name)] = true
		}
	}
	var object map[string]json.RawMessage
	if json.Unmarshal(choice, &object) != nil {
		return nil
	}
	name := rawMapString(object, "name")
	if name == "" {
		var function map[string]json.RawMessage
		if json.Unmarshal(object["function"], &function) == nil {
			name, _ = rawJSONString(function["name"])
		}
	}
	if name != "" && !names[strings.TrimSpace(name)] {
		return fmt.Errorf("tool_choice references undeclared tool %q", name)
	}
	return nil
}

func mergeJSONArray(left, right json.RawMessage) json.RawMessage {
	if emptyJSON(left) {
		return right
	}
	if emptyJSON(right) {
		return left
	}
	var a, b []json.RawMessage
	if json.Unmarshal(left, &a) != nil || json.Unmarshal(right, &b) != nil {
		return left
	}
	merged, _ := json.Marshal(append(a, b...))
	return merged
}

func responsesAdditionalTools(raw json.RawMessage) (json.RawMessage, error) {
	if emptyJSON(raw) {
		return nil, nil
	}
	var items []json.RawMessage
	if json.Unmarshal(raw, &items) != nil {
		return nil, nil
	}
	var all []json.RawMessage
	for index, item := range items {
		var source map[string]json.RawMessage
		if json.Unmarshal(item, &source) != nil {
			continue
		}
		if rawMapString(source, "type") != "additional_tools" {
			continue
		}
		var tools []json.RawMessage
		if err := json.Unmarshal(source["tools"], &tools); err != nil {
			return nil, fmt.Errorf("input[%d].tools must be an array", index)
		}
		all = append(all, tools...)
	}
	if len(all) == 0 {
		return nil, nil
	}
	result, _ := json.Marshal(all)
	return result, nil
}

func rawMapJSONValue(source map[string]json.RawMessage, key string) (json.RawMessage, bool) {
	value := source[key]
	if len(value) == 0 || string(value) == "null" {
		return nil, false
	}
	return value, true
}

func translateResponsesTextFormat(raw json.RawMessage) (json.RawMessage, error) {
	if emptyJSON(raw) {
		return nil, nil
	}
	var format map[string]json.RawMessage
	if json.Unmarshal(raw, &format) != nil {
		return nil, fmt.Errorf("text must be an object")
	}
	if nested, ok := rawMapJSONValue(format, "format"); ok {
		raw = nested
	}
	if json.Unmarshal(raw, &format) != nil {
		return nil, fmt.Errorf("text.format must be an object")
	}
	formatType := rawMapString(format, "type")
	switch formatType {
	case "text":
		return nil, nil
	case "json_object":
		return json.RawMessage(`{"type":"json_object"}`), nil
	case "json_schema":
		name := rawMapString(format, "name")
		schema := rawMapJSON(format, "schema")
		if name == "" || len(schema) == 0 {
			return nil, fmt.Errorf("text.format json_schema requires name and schema")
		}
		var schemaValue any
		if json.Unmarshal(schema, &schemaValue) != nil {
			return nil, fmt.Errorf("text.format.schema must be valid JSON")
		}
		out := map[string]any{"type": "json_schema", "json_schema": map[string]any{"name": name, "schema": schemaValue}}
		if description := rawMapString(format, "description"); description != "" {
			out["json_schema"].(map[string]any)["description"] = description
		}
		if strict, ok := format["strict"]; ok {
			var value bool
			if json.Unmarshal(strict, &value) == nil {
				out["json_schema"].(map[string]any)["strict"] = value
			}
		}
		encoded, _ := json.Marshal(out)
		return encoded, nil
	default:
		return nil, fmt.Errorf("text.format type %q is not supported", formatType)
	}
}

func compatibilityText(raw json.RawMessage, textTypes map[string]bool) (string, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return "", nil
	}
	if text, ok := rawJSONString(raw); ok {
		return text, nil
	}
	var blocks []json.RawMessage
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return "", fmt.Errorf("must be a string or an array of text content blocks")
	}
	parts := make([]string, 0, len(blocks))
	for blockIndex, block := range blocks {
		var source map[string]json.RawMessage
		if err := json.Unmarshal(block, &source); err != nil {
			return "", fmt.Errorf("content[%d] must be an object", blockIndex)
		}
		blockType := rawMapString(source, "type")
		if !textTypes[blockType] {
			return "", fmt.Errorf("content[%d] type %q is not supported", blockIndex, blockType)
		}
		parts = appendTextPart(parts, rawMapContentString(source, "text"))
	}
	return strings.Join(parts, "\n"), nil
}

func compatibilityContent(raw json.RawMessage, textTypes map[string]bool, allowImages bool) (any, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return "", nil
	}
	if text, ok := rawJSONString(raw); ok {
		return text, nil
	}
	var blocks []json.RawMessage
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return nil, fmt.Errorf("must be a string or an array of content blocks")
	}
	parts := make([]any, 0, len(blocks))
	for blockIndex, block := range blocks {
		var source map[string]json.RawMessage
		if err := json.Unmarshal(block, &source); err != nil {
			return nil, fmt.Errorf("content[%d] must be an object", blockIndex)
		}
		blockType := rawMapString(source, "type")
		if textTypes[blockType] {
			parts = appendContentPart(parts, map[string]any{"type": "text", "text": rawMapContentString(source, "text")})
			continue
		}
		if allowImages && (blockType == "input_image" || blockType == "image_url") {
			imageURL := rawMapString(source, "image_url")
			if imageURL == "" {
				var nested map[string]json.RawMessage
				if json.Unmarshal(source["image_url"], &nested) == nil {
					imageURL = rawMapString(nested, "url")
				}
			}
			if imageURL == "" {
				return nil, fmt.Errorf("content[%d] image URL required", blockIndex)
			}
			parts = appendContentPart(parts, map[string]any{"type": "image_url", "image_url": map[string]string{"url": imageURL}})
			continue
		}
		if blockType == "input_file" || blockType == "file" {
			return nil, fmt.Errorf("content[%d] file inputs are not supported by the Qoder upstream", blockIndex)
		}
		return nil, fmt.Errorf("content[%d] type %q is not supported", blockIndex, blockType)
	}
	if text, ok := compactTextParts(parts); ok {
		return text, nil
	}
	return parts, nil
}

func compactTextParts(parts []any) (string, bool) {
	texts := make([]string, 0, len(parts))
	for _, part := range parts {
		value, ok := part.(map[string]any)
		if !ok || value["type"] != "text" {
			return "", false
		}
		text, ok := value["text"].(string)
		if !ok {
			return "", false
		}
		texts = append(texts, text)
	}
	return strings.Join(texts, "\n"), true
}

func appendContentPart(parts []any, part any) []any {
	if part == nil {
		return parts
	}
	return append(parts, part)
}

func appendTextPart(parts []string, text string) []string {
	if text == "" {
		return parts
	}
	return append(parts, text)
}

func marshalToolCalls(calls []compatibilityToolCall) json.RawMessage {
	if len(calls) == 0 {
		return nil
	}
	encoded := make([]map[string]any, 0, len(calls))
	for _, call := range calls {
		arguments := call.Arguments
		if len(arguments) == 0 {
			arguments = json.RawMessage(`{}`)
		}
		encoded = append(encoded, map[string]any{
			"id": call.ID, "type": "function",
			"function": map[string]any{"name": call.Name, "arguments": string(arguments)},
		})
	}
	result, _ := json.Marshal(encoded)
	return result
}

func rawJSONString(raw json.RawMessage) (string, bool) {
	var value string
	if json.Unmarshal(raw, &value) != nil {
		return "", false
	}
	return value, true
}

func rawMapString(source map[string]json.RawMessage, key string) string {
	value, _ := rawJSONString(source[key])
	return strings.TrimSpace(value)
}

func rawMapContentString(source map[string]json.RawMessage, key string) string {
	value, _ := rawJSONString(source[key])
	return value
}

func contentPresent(content any) bool {
	switch value := content.(type) {
	case string:
		return value != ""
	case []any:
		return len(value) > 0
	default:
		return content != nil
	}
}

func firstRawMapString(source map[string]json.RawMessage, keys ...string) string {
	for _, key := range keys {
		if value := rawMapString(source, key); value != "" {
			return value
		}
	}
	return ""
}

func rawMapJSON(source map[string]json.RawMessage, key string) json.RawMessage {
	value := source[key]
	if len(value) == 0 || string(value) == "null" {
		return nil
	}
	return append(json.RawMessage(nil), value...)
}

func emptyJSON(raw json.RawMessage) bool {
	return len(raw) == 0 || string(raw) == "null" || string(raw) == "[]"
}
