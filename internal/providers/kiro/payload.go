package kiro

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/caigee-cmd/cli2api/internal/providers"
	"github.com/caigee-cmd/cli2api/internal/translate"
	"github.com/google/uuid"
)

// conversation is the IDE payload both chat paths share. CreateResponse
// rewrites it as Responses input items; generateAssistantResponse sends it
// as conversationState.
type conversation struct {
	Model           string
	System          string
	Anchor          string
	History         []historyTurn
	Current         userTurn
	Tools           []toolSpec
	ProfileArn      string
	Level           string
	ToolChoice      string
	ToolName        string
	DisableParallel bool
	Declared        map[string]struct{}
}

type historyTurn struct {
	User      *userTurn
	Assistant *assistantTurn
}

type userTurn struct {
	Text      string
	Images    []imagePart
	Documents []documentPart
	Results   []toolResult
}

type assistantTurn struct {
	Text      string
	ToolUses  []toolCall
	Reasoning []reasoningBlock
}

type reasoningBlock struct {
	Text      string
	Signature string
	Redacted  string
}

type toolCall struct {
	ID    string
	Name  string
	Input map[string]any
}

type toolResult struct {
	ID      string
	Output  string
	Images  []imagePart
	IsError bool
}

type toolSpec struct {
	Name        string
	Description string
	Parameters  any
	// ClientName is the name the client declared when it differs from Name.
	// Upstream calls come back under Name and are renamed before they leave.
	ClientName string
}

type imagePart struct {
	Format string
	Bytes  string
}

type documentPart struct {
	Format   string
	Name     string
	Media    string
	Bytes    string
	Filename string
}

// responsesRequest is the CreateResponse body. History is Responses input
// items: user text, assistant text, reasoning, function_call, and
// function_call_output. Tools are functions whose parameters are the
// client's input schema.
type responsesRequest struct {
	Model      string            `json:"model"`
	Input      []map[string]any  `json:"input"`
	Tools      []responsesTool   `json:"tools,omitempty"`
	Stream     bool              `json:"stream"`
	Origin     string            `json:"origin"`
	ProfileArn string            `json:"profileArn,omitempty"`
	Metadata   map[string]string `json:"metadata,omitempty"`
	Reasoning  map[string]string `json:"reasoning,omitempty"`
}

type responsesTool struct {
	Type        string `json:"type"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Parameters  any    `json:"parameters,omitempty"`
}

type openAITool struct {
	Type     string `json:"type"`
	Function struct {
		Name        string          `json:"name"`
		Description string          `json:"description"`
		Parameters  json.RawMessage `json:"parameters"`
	} `json:"function"`
}

type openAICall struct {
	ID       string `json:"id"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

func buildConversation(req translate.ChatRequest, cred Credential, model string) (conversation, error) {
	out := conversation{
		Model:      model,
		ProfileArn: cred.requestProfileArn(),
		Level:      reasoningLevel(req),
		Declared:   map[string]struct{}{},
	}
	choice, name := openAIToolChoice(req.ToolChoice)
	out.ToolChoice, out.ToolName = choice, name
	var system []string
	var pending []toolResult
	for _, message := range req.Messages {
		switch message.Role {
		case "system", "developer":
			if text := messageText(message.Content); text != "" {
				system = append(system, text)
				out.System = strings.TrimSpace(out.System + "\n\n" + text)
			}
		case "user":
			turn := userTurn{Text: messageText(message.Content), Results: pending}
			for _, result := range pending {
				turn.Images = append(turn.Images, result.Images...)
			}
			pending = nil
			images, documents := messageMedia(message.Content)
			turn.Images = append(turn.Images, images...)
			turn.Documents = documents
			if len(system) > 0 {
				turn.Text = strings.TrimSpace(strings.Join(system, "\n\n") + "\n\n" + turn.Text)
				system = nil
			}
			out.History = append(out.History, historyTurn{User: &turn})
		case "assistant":
			out.History = append(out.History, historyTurn{Assistant: assistantTurnFrom(message)})
		case "tool":
			id := strings.TrimSpace(message.ToolCallID)
			if id == "" {
				continue
			}
			output := messageText(message.Content)
			images, _ := messageMedia(message.ToolResultImages)
			if strings.TrimSpace(output) == "" && len(images) > 0 {
				output = "[Tool returned an image; the image is attached to this message.]"
			}
			pending = append(pending, toolResult{
				ID: id, Output: output, Images: images, IsError: message.ToolResultIsError,
			})
		}
	}
	if len(pending) > 0 || len(out.History) == 0 || out.History[len(out.History)-1].User == nil {
		text := ""
		if len(system) > 0 {
			text = strings.Join(system, "\n\n")
			system = nil
		}
		turn := &userTurn{Text: text, Results: pending}
		for _, result := range pending {
			turn.Images = append(turn.Images, result.Images...)
		}
		out.History = append(out.History, historyTurn{User: turn})
	}
	if len(system) > 0 {
		head := out.History[0].User
		if head == nil {
			out.History = append([]historyTurn{{User: &userTurn{Text: strings.Join(system, "\n\n")}}}, out.History...)
		} else {
			head.Text = strings.TrimSpace(strings.Join(system, "\n\n") + "\n\n" + head.Text)
		}
	}
	out.Current = *out.History[len(out.History)-1].User
	out.History = out.History[:len(out.History)-1]
	out.Anchor = conversationAnchor(req.Messages)
	tools, err := toolsFrom(req.Tools)
	if err != nil {
		return conversation{}, err
	}
	if choice == "none" {
		tools = nil
	}
	if err := validateToolChoice(choice, name, tools); err != nil {
		return conversation{}, err
	}
	out.Tools = tools
	out.DisableParallel = req.ParallelToolCalls != nil && !*req.ParallelToolCalls
	for _, tool := range tools {
		out.Declared[tool.Name] = struct{}{}
		if tool.ClientName != "" {
			out.Declared[tool.ClientName] = struct{}{}
		}
	}
	attachToolHistory(&out)
	if directive := toolChoiceDirective(choice, name, tools, out.DisableParallel); directive != "" {
		out.Current.Text = strings.TrimSpace(out.Current.Text + "\n\n" + directive)
	}
	return out, nil
}

// conversationAnchor is the first thing the user wrote. Tool results are their
// own messages, so a turn that only returns a result does not count. The same
// opening keeps the same conversation id on later turns.
func conversationAnchor(messages []translate.ChatMessage) string {
	for _, message := range messages {
		if message.Role != "user" {
			continue
		}
		if text := messageText(message.Content); text != "" {
			return text
		}
	}
	return ""
}

func assistantTurnFrom(message translate.ChatMessage) *assistantTurn {
	turn := &assistantTurn{Text: messageText(message.Content)}
	if strings.TrimSpace(message.ReasoningContent) != "" {
		turn.Reasoning = append(turn.Reasoning, reasoningBlock{Text: message.ReasoningContent})
	}
	if len(message.ToolCalls) == 0 {
		return turn
	}
	var calls []openAICall
	if json.Unmarshal(message.ToolCalls, &calls) != nil {
		return turn
	}
	for _, call := range calls {
		name := strings.TrimSpace(call.Function.Name)
		id := strings.TrimSpace(call.ID)
		if name == "" || id == "" {
			continue
		}
		input := map[string]any{}
		if raw := strings.TrimSpace(call.Function.Arguments); raw != "" {
			_ = json.Unmarshal([]byte(raw), &input)
		}
		turn.ToolUses = append(turn.ToolUses, toolCall{ID: id, Name: name, Input: input})
	}
	return turn
}

func (c conversation) responsesBody(includeReasoning bool) responsesRequest {
	body := responsesRequest{
		Model: c.Model, Stream: true, Origin: Origin, ProfileArn: c.ProfileArn,
		Metadata: map[string]string{
			"kiro:agent_mode":            AgentMode,
			"kiro:conversation_id":       conversationID(c.Model, c.System, c.Anchor),
			"kiro:agent_continuation_id": uuid.NewString(),
		},
		Reasoning: reasoningField(c.Level),
	}
	for _, item := range c.History {
		body.Input = append(body.Input, item.responses(includeReasoning)...)
	}
	body.Input = append(body.Input, c.Current.responses()...)
	for _, tool := range c.Tools {
		body.Tools = append(body.Tools, responsesTool{
			Type: "function", Name: tool.Name, Description: tool.Description, Parameters: tool.Parameters,
		})
	}
	return body
}

// clientNames maps an upstream tool name back to the name the client declared.
func (c conversation) clientNames() map[string]string {
	names := map[string]string{}
	for _, tool := range c.Tools {
		if tool.ClientName != "" && tool.ClientName != tool.Name {
			names[tool.Name] = tool.ClientName
		}
	}
	if len(names) == 0 {
		return nil
	}
	return names
}

func (c conversation) replaysReasoning() bool {
	for _, item := range c.History {
		if item.Assistant != nil && len(item.Assistant.Reasoning) > 0 {
			return true
		}
	}
	return false
}

func (t historyTurn) responses(includeReasoning bool) []map[string]any {
	switch {
	case t.User != nil:
		return t.User.responses()
	case t.Assistant != nil:
		return t.Assistant.responses(includeReasoning)
	default:
		return nil
	}
}

func (u userTurn) responses() []map[string]any {
	var items []map[string]any
	for _, result := range u.Results {
		output := any(result.Output)
		if len(result.Images) > 0 {
			parts := make([]map[string]any, 0, len(result.Images)+1)
			if strings.TrimSpace(result.Output) != "" {
				parts = append(parts, map[string]any{"type": "input_text", "text": result.Output})
			}
			for _, image := range result.Images {
				parts = append(parts, map[string]any{
					"type": "input_image", "image_url": "data:image/" + image.Format + ";base64," + image.Bytes,
				})
			}
			output = parts
		}
		items = append(items, map[string]any{
			"type": "function_call_output", "call_id": result.ID, "output": output,
		})
	}
	var parts []map[string]any
	for _, image := range u.Images {
		parts = append(parts, map[string]any{
			"type": "input_image", "image_url": "data:image/" + image.Format + ";base64," + image.Bytes,
		})
	}
	for _, doc := range u.Documents {
		parts = append(parts, map[string]any{
			"type": "input_file", "filename": doc.Filename, "file_data": "data:" + doc.Media + ";base64," + doc.Bytes,
		})
	}
	switch {
	case len(parts) > 0:
		content := make([]map[string]any, 0, len(parts)+1)
		if strings.TrimSpace(u.Text) != "" {
			content = append(content, map[string]any{"type": "input_text", "text": u.Text})
		}
		items = append(items, map[string]any{"role": "user", "content": append(content, parts...)})
	case strings.TrimSpace(u.Text) != "":
		items = append(items, map[string]any{"role": "user", "content": u.Text})
	}
	return items
}

func (a assistantTurn) responses(includeReasoning bool) []map[string]any {
	var items []map[string]any
	content := a.Text
	if includeReasoning {
		for _, block := range a.Reasoning {
			envelope := map[string]any{}
			switch {
			case block.Redacted != "":
				envelope["redactedContent"] = block.Redacted
			case block.Signature != "":
				envelope["reasoningText"] = map[string]string{"text": block.Text, "signature": block.Signature}
			default:
				continue
			}
			encoded, _ := json.Marshal(envelope)
			items = append(items, map[string]any{
				"type": "reasoning", "summary": []any{}, "encrypted_content": string(encoded),
			})
		}
	}
	if strings.TrimSpace(content) != "" {
		items = append(items, map[string]any{"role": "assistant", "content": content})
	}
	for _, call := range a.ToolUses {
		input := call.Input
		if input == nil {
			input = map[string]any{}
		}
		arguments, _ := json.Marshal(input)
		items = append(items, map[string]any{
			"type": "function_call", "call_id": call.ID, "name": call.Name, "arguments": string(arguments),
		})
	}
	return items
}

func toolsFrom(raw json.RawMessage) ([]toolSpec, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var tools []openAITool
	if err := json.Unmarshal(raw, &tools); err != nil {
		return nil, err
	}
	out := make([]toolSpec, 0, len(tools))
	used := map[string]bool{}
	for _, tool := range tools {
		if isHostedWebSearch(tool.Type) {
			out = append(out, toolSpec{
				Name: webSearchUpstreamName, ClientName: webSearchClientName,
				Description: "Search the web for current information.", Parameters: webSearchSchema(),
			})
			used[webSearchUpstreamName] = true
			continue
		}
		name := strings.TrimSpace(tool.Function.Name)
		if name == "" || !strings.EqualFold(tool.Type, "function") && tool.Type != "" {
			continue
		}
		upstream := uniqueToolName(kiroToolName(name), used)
		item := toolSpec{Name: upstream, Description: toolDescription(tool.Function.Description, upstream)}
		if upstream != name {
			item.ClientName = name
		}
		params, err := toolParameters(tool.Function.Parameters)
		if err != nil {
			return nil, err
		}
		item.Parameters = params
		out = append(out, item)
	}
	return out, nil
}

func isHostedWebSearch(kind string) bool {
	kind = strings.ToLower(strings.TrimSpace(kind))
	return kind == "web_search" || strings.HasPrefix(kind, "web_search_")
}

func webSearchSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"query": map[string]any{"type": "string", "description": "Search query"},
		},
		"required": []string{"query"},
	}
}

// kiroToolName rewrites every name into the camel case Kiro accepts, then
// shortens whatever is still past 64 characters. A name that needs no change
// comes back as it was, so the client name is only kept when it differs.
func kiroToolName(name string) string {
	if name == "" {
		return "tool"
	}
	return shortenToolName(sanitizeToolName(name))
}

// toolDescription fills an empty description and cuts one Kiro would reject
// for length. The cut keeps the opening, which is the part that says what
// the tool does.
func toolDescription(desc, name string) string {
	desc = strings.TrimSpace(desc)
	if desc == "" {
		return "Tool: " + name
	}
	if len(desc) > maxToolDescLen {
		return desc[:maxToolDescLen] + "..."
	}
	return desc
}

// toolParameters decodes the client's schema and makes sure Kiro receives an
// object. A missing schema, or one that is not an object, becomes an empty
// object rather than a schema Kiro refuses.
func toolParameters(raw json.RawMessage) (any, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return map[string]any{"type": "object"}, nil
	}
	var params any
	if err := json.Unmarshal(raw, &params); err != nil {
		return nil, err
	}
	obj, ok := params.(map[string]any)
	if !ok {
		return map[string]any{"type": "object"}, nil
	}
	cleanToolSchema(obj)
	if _, ok := obj["type"]; !ok {
		obj["type"] = "object"
	}
	return obj, nil
}

// cleanToolSchema mirrors Kiro's accepted JSON-schema subset without changing
// the caller's semantics: composition keywords stay intact, while unsupported
// additionalProperties and empty/invalid required declarations are removed.
func cleanToolSchema(schema map[string]any) {
	delete(schema, "additionalProperties")
	if required, ok := schema["required"]; ok {
		values, valid := required.([]any)
		if !valid || len(values) == 0 {
			delete(schema, "required")
		}
	}
	for _, value := range schema {
		switch child := value.(type) {
		case map[string]any:
			cleanToolSchema(child)
		case []any:
			for _, item := range child {
				if nested, ok := item.(map[string]any); ok {
					cleanToolSchema(nested)
				}
			}
		}
	}
}

// sanitizeToolName turns a namespaced or snake-case tool name into the camel
// case Kiro accepts. The client name is kept so the call can be renamed back.
func sanitizeToolName(name string) string {
	parts := strings.FieldsFunc(name, func(r rune) bool { return r == '_' || r == '-' })
	if len(parts) == 0 {
		return "tool"
	}
	var b strings.Builder
	for i, part := range parts {
		if part == "" {
			continue
		}
		if i == 0 {
			b.WriteString(strings.ToLower(part[:1]))
			b.WriteString(part[1:])
			continue
		}
		b.WriteString(strings.ToUpper(part[:1]))
		b.WriteString(part[1:])
	}
	if b.Len() == 0 {
		return "tool"
	}
	return b.String()
}

func shortenToolName(name string) string {
	if len(name) <= maxToolNameLen {
		return name
	}
	if strings.HasPrefix(name, "mcp__") {
		if last := strings.LastIndex(name, "__"); last > 5 {
			shortened := "mcp__" + name[last+2:]
			if len(shortened) <= maxToolNameLen {
				return shortened
			}
		}
	}
	return name[:maxToolNameLen]
}

func uniqueToolName(name string, used map[string]bool) string {
	if strings.TrimSpace(name) == "" {
		name = "tool"
	}
	if !used[name] {
		used[name] = true
		return name
	}
	for i := 2; ; i++ {
		suffix := fmt.Sprintf("%d", i)
		base := name
		if len(base)+len(suffix) > maxToolNameLen {
			base = base[:maxToolNameLen-len(suffix)]
		}
		candidate := base + suffix
		if !used[candidate] {
			used[candidate] = true
			return candidate
		}
	}
}

// messageText reads a chat message as text. A nil content marshals as "null",
// which is not something the user wrote, so it stays empty.
func messageText(content any) string {
	if content == nil {
		return ""
	}
	text := strings.TrimSpace(translate.ContentToString(content))
	if text == "null" {
		return ""
	}
	return text
}

func messageMedia(content any) ([]imagePart, []documentPart) {
	parts, ok := content.([]any)
	if !ok {
		return nil, nil
	}
	var images []imagePart
	var documents []documentPart
	for _, part := range parts {
		block, ok := part.(map[string]any)
		if !ok {
			continue
		}
		switch block["type"] {
		case "image_url", "image", "input_image":
			if image, ok := imageFrom(block); ok {
				images = append(images, image)
			}
		case "file", "input_file":
			if doc, ok := documentFrom(block); ok {
				documents = append(documents, doc)
			}
		}
	}
	return images, documents
}

func imageFrom(block map[string]any) (imagePart, bool) {
	raw := firstString(block, "image_url", "url")
	switch nested := block["image_url"].(type) {
	case map[string]any:
		raw = firstString(nested, "url")
	case map[string]string:
		raw = nested["url"]
	}
	format, bytes, ok := dataURL(raw, "image/")
	if !ok {
		return imagePart{}, false
	}
	return imagePart{Format: format, Bytes: bytes}, true
}

func documentFrom(block map[string]any) (documentPart, bool) {
	raw := firstString(block, "file_data", "url")
	media, bytes, ok := splitDataURL(raw)
	if !ok || strings.HasPrefix(media, "image/") {
		return documentPart{}, false
	}
	format := mediaSubtype(media)
	name := firstString(block, "filename", "name")
	if name == "" {
		name = "document." + format
	}
	return documentPart{Format: format, Name: strings.TrimSuffix(name, "."+format), Media: media, Bytes: bytes, Filename: name}, true
}

func dataURL(raw, prefix string) (string, string, bool) {
	media, bytes, ok := splitDataURL(raw)
	if !ok || !strings.HasPrefix(media, prefix) {
		return "", "", false
	}
	return mediaSubtype(media), bytes, true
}

func splitDataURL(raw string) (string, string, bool) {
	if !strings.HasPrefix(raw, "data:") {
		return "", "", false
	}
	header, bytes, ok := strings.Cut(strings.TrimPrefix(raw, "data:"), ";base64,")
	if !ok || bytes == "" {
		return "", "", false
	}
	media := strings.ToLower(strings.TrimSpace(strings.Split(header, ";")[0]))
	return media, bytes, media != ""
}

func mediaSubtype(media string) string {
	if _, sub, ok := strings.Cut(media, "/"); ok && sub != "" {
		return sub
	}
	return "bin"
}

func reasoningLevel(req translate.ChatRequest) string {
	intent := providers.ParseReasoningIntent(req)
	if intent.Disabled || intent.Level == "none" {
		return ""
	}
	return intent.Level
}

func reasoningField(level string) map[string]string {
	if level == "" {
		return nil
	}
	return map[string]string{"type": "enabled", "effort": level, "summary": "auto"}
}

func resolveModel(model string) string {
	model = strings.TrimSpace(model)
	model = strings.TrimPrefix(model, "kiro/")
	return strings.TrimSpace(model)
}

func openAIToolChoice(raw json.RawMessage) (string, string) {
	if len(raw) == 0 || string(raw) == "null" {
		return "auto", ""
	}
	var text string
	if json.Unmarshal(raw, &text) == nil {
		switch strings.ToLower(strings.TrimSpace(text)) {
		case "", "auto":
			return "auto", ""
		case "required":
			return "any", ""
		case "none":
			return "none", ""
		default:
			return "auto", ""
		}
	}
	var choice struct {
		Type     string `json:"type"`
		Name     string `json:"name"`
		Function struct {
			Name string `json:"name"`
		} `json:"function"`
	}
	if json.Unmarshal(raw, &choice) != nil {
		return "auto", ""
	}
	switch strings.ToLower(strings.TrimSpace(choice.Type)) {
	case "function", "custom":
		return "tool", firstNonEmpty(choice.Function.Name, choice.Name)
	default:
		return "auto", ""
	}
}

// validateToolChoice rejects a choice the request cannot satisfy. Kiro has no
// tool_choice field, so a required call with no tools, or a named tool the
// request did not declare, would otherwise be sent as a hint the model cannot follow.
func validateToolChoice(kind, name string, tools []toolSpec) error {
	switch kind {
	case "any":
		if len(tools) == 0 {
			return fmt.Errorf("tool_choice required requires tools")
		}
	case "tool":
		for _, tool := range tools {
			if name != "" && (tool.Name == name || tool.ClientName == name) {
				return nil
			}
		}
		return fmt.Errorf("tool_choice names an undeclared tool")
	}
	return nil
}

// toolChoiceDirective tells the model a tool call is mandatory. Kiro has no
// tool_choice field, so the requirement rides in the user turn.
func toolChoiceDirective(kind, name string, tools []toolSpec, disableParallel bool) string {
	directive := ""
	switch kind {
	case "any":
		if len(tools) > 0 {
			directive = "The client requires a tool call on this turn. Call at least one available tool."
		}
	case "tool":
		if name != "" {
			directive = "The client requires the tool \"" + name + "\" on this turn. Call that tool."
		}
	}
	if disableParallel && len(tools) > 0 {
		directive = strings.TrimSpace(directive + " The client allows at most one tool call on this turn.")
	}
	return directive
}
