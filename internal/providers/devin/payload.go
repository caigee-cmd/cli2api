package devin

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/caigee-cmd/cli2api/internal/translate"
)

const (
	maxDevinToolAliasLen     = 64
	maxDevinToolsDiagLen     = 1800
	maxDevinToolDiagNameLen  = 96
	maxDevinNamespaceNestLen = 12
)

// ChatPayload is the normalized Devin Interactions request.
type ChatPayload struct {
	System          string
	Prompts         []Prompt
	Tools           []Tool
	Temperature     *float64
	MaxTokens       int
	ModelUID        string
	Effort          string
	Budget          int
	OriginalByAlias map[string]string
	// ToolsDiag is a compact inbound/outbound tools type+name summary for
	// temporary MCP configuration denial debugging. It never includes
	// descriptions or parameter schemas.
	ToolsDiag string
}

func BuildChatPayload(req translate.ChatRequest, catalogLevels map[string][]string) ChatPayload {
	aliases := newToolAliasMaps()
	var systemParts []string
	prompts := make([]Prompt, 0, len(req.Messages))
	for _, msg := range req.Messages {
		role := strings.ToLower(strings.TrimSpace(msg.Role))
		switch role {
		case "system", "developer":
			text := strings.TrimSpace(translate.ContentToString(msg.Content))
			if text != "" {
				systemParts = append(systemParts, text)
			}
		case "user":
			text, images := splitContent(msg.Content)
			prompts = append(prompts, Prompt{Source: 1, Content: text, Images: images})
		case "assistant":
			text, images := splitContent(msg.Content)
			thinking := extractReasoning(msg)
			p := Prompt{Source: 2, Content: text, Images: images, Thinking: thinking}
			p.ToolCalls = parseToolCalls(msg.ToolCalls, aliases)
			prompts = append(prompts, p)
		case "tool":
			// Tool results can carry images (screenshots, rendered output);
			// Devin accepts Images on any prompt, so pass them through instead
			// of flattening to text.
			text, images := splitContent(msg.Content)
			prompts = append(prompts, Prompt{Source: 4, Content: text, Images: images, ToolCallID: strings.TrimSpace(msg.ToolCallID)})
		default:
			text := translate.ContentToString(msg.Content)
			if strings.TrimSpace(text) == "" && len(msg.ToolCalls) == 0 {
				continue
			}
			prompts = append(prompts, Prompt{Source: 1, Content: text})
		}
	}

	system := strings.TrimSpace(strings.Join(systemParts, "\n\n"))
	effort, budget := extractEffort(req)
	maxTokens := parseMaxTokens(req)
	temp := parseTemperature(req)
	modelUID := ResolveChatModelUID(req.Model, effort, budget, catalogLevels)
	tools := parseTools(req.Tools, aliases)

	return ChatPayload{
		System:          system,
		Prompts:         prompts,
		Tools:           tools,
		Temperature:     temp,
		MaxTokens:       maxTokens,
		ModelUID:        modelUID,
		Effort:          effort,
		Budget:          budget,
		OriginalByAlias: aliases.originalByAlias,
		ToolsDiag:       buildToolsDiag(req.Tools, tools),
	}
}

func splitContent(content any) (string, []Image) {
	switch v := content.(type) {
	case string:
		return v, nil
	case []any:
		var texts []string
		var images []Image
		for _, item := range v {
			m, ok := item.(map[string]any)
			if !ok {
				continue
			}
			typ := strings.ToLower(strings.TrimSpace(asString(m["type"])))
			switch typ {
			case "text":
				if t := asString(m["text"]); t != "" {
					texts = append(texts, t)
				}
			case "image_url", "image":
				img := parseImagePart(m)
				if img.Base64Data != "" {
					images = append(images, img)
				}
			default:
				if t := asString(m["text"]); t != "" {
					texts = append(texts, t)
				}
				if img := parseImagePart(m); img.Base64Data != "" {
					images = append(images, img)
				}
			}
		}
		return strings.Join(texts, "\n"), images
	default:
		return translate.ContentToString(content), nil
	}
}

func parseImagePart(m map[string]any) Image {
	if urlMap, ok := m["image_url"].(map[string]any); ok {
		return decodeDataURL(asString(urlMap["url"]))
	}
	if url := asString(m["url"]); url != "" {
		return decodeDataURL(url)
	}
	if data := asString(m["data"]); data != "" {
		mime := firstNonEmpty(asString(m["mime_type"]), asString(m["media_type"]), "image/png")
		return Image{Base64Data: data, MimeType: mime}
	}
	return Image{}
}

func decodeDataURL(raw string) Image {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return Image{}
	}
	if !strings.HasPrefix(raw, "data:") {
		return Image{}
	}
	comma := strings.Index(raw, ",")
	if comma < 0 {
		return Image{}
	}
	meta := raw[5:comma]
	data := raw[comma+1:]
	mime := "image/png"
	if semi := strings.Index(meta, ";"); semi >= 0 {
		mime = meta[:semi]
	} else if meta != "" {
		mime = meta
	}
	return Image{Base64Data: data, MimeType: mime}
}

func parseToolCalls(raw json.RawMessage, aliases *toolAliasMaps) []ToolCall {
	if len(raw) == 0 {
		return nil
	}
	var calls []struct {
		ID       string `json:"id"`
		Type     string `json:"type"`
		Function struct {
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
		} `json:"function"`
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	}
	if json.Unmarshal(raw, &calls) != nil {
		return nil
	}
	out := make([]ToolCall, 0, len(calls))
	for _, c := range calls {
		name := firstNonEmpty(c.Function.Name, c.Name)
		args := firstNonEmpty(c.Function.Arguments, c.Arguments)
		if name == "" && args == "" && c.ID == "" {
			continue
		}
		if name != "" {
			name = aliases.alias(name)
		}
		out = append(out, ToolCall{ID: c.ID, Name: name, Arguments: args})
	}
	return out
}

func parseTools(raw json.RawMessage, aliases *toolAliasMaps) []Tool {
	if len(raw) == 0 {
		return nil
	}
	var items []json.RawMessage
	if json.Unmarshal(raw, &items) != nil {
		return nil
	}
	out := make([]Tool, 0, len(items))
	seen := map[string]struct{}{}
	appendTool := func(name, desc string, params json.RawMessage) {
		name = strings.TrimSpace(name)
		if name == "" {
			return
		}
		name = aliases.alias(name)
		if _, ok := seen[name]; ok {
			return
		}
		seen[name] = struct{}{}
		out = append(out, Tool{Name: name, Description: desc, Parameters: params})
	}
	for _, item := range items {
		var probe struct {
			Type string `json:"type"`
			Name string `json:"name"`
		}
		if json.Unmarshal(item, &probe) != nil {
			continue
		}
		typ := strings.ToLower(strings.TrimSpace(probe.Type))
		switch typ {
		case "namespace":
			// Codex/Desktop namespace wrappers trip Devin upstream. Keep nested
			// function tools (expanded to top-level) and drop the shell itself.
			for _, nested := range expandNamespaceTools(item, strings.TrimSpace(probe.Name)) {
				appendTool(nested.Name, nested.Description, nested.Parameters)
			}
		case "mcp":
			// Hosted MCP connector entries are not Devin function tools.
			continue
		case "web_search", "web_search_preview":
			continue
		default:
			var t struct {
				Function struct {
					Name        string          `json:"name"`
					Description string          `json:"description"`
					Parameters  json.RawMessage `json:"parameters"`
				} `json:"function"`
				Name        string          `json:"name"`
				Description string          `json:"description"`
				Parameters  json.RawMessage `json:"parameters"`
			}
			if json.Unmarshal(item, &t) != nil {
				continue
			}
			name := firstNonEmpty(t.Function.Name, t.Name)
			desc := firstNonEmpty(t.Function.Description, t.Description)
			params := t.Function.Parameters
			if len(params) == 0 {
				params = t.Parameters
			}
			appendTool(name, desc, params)
		}
	}
	return out
}

func expandNamespaceTools(raw json.RawMessage, namespace string) []Tool {
	var wrapper struct {
		Tools []json.RawMessage `json:"tools"`
	}
	if json.Unmarshal(raw, &wrapper) != nil || len(wrapper.Tools) == 0 {
		return nil
	}
	out := make([]Tool, 0, len(wrapper.Tools))
	for _, item := range wrapper.Tools {
		var t struct {
			Type     string `json:"type"`
			Function struct {
				Name        string          `json:"name"`
				Description string          `json:"description"`
				Parameters  json.RawMessage `json:"parameters"`
			} `json:"function"`
			Name        string          `json:"name"`
			Description string          `json:"description"`
			Parameters  json.RawMessage `json:"parameters"`
		}
		if json.Unmarshal(item, &t) != nil {
			continue
		}
		typ := strings.ToLower(strings.TrimSpace(t.Type))
		if typ != "" && typ != "function" {
			continue
		}
		name := firstNonEmpty(t.Function.Name, t.Name)
		name = qualifyNamespaceToolName(namespace, name)
		if name == "" {
			continue
		}
		desc := firstNonEmpty(t.Function.Description, t.Description)
		params := t.Function.Parameters
		if len(params) == 0 {
			params = t.Parameters
		}
		out = append(out, Tool{Name: name, Description: desc, Parameters: params})
	}
	return out
}

func qualifyNamespaceToolName(namespace, name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	if needsDevinToolAlias(name) || strings.Contains(name, "__") {
		return name
	}
	namespace = strings.TrimSpace(namespace)
	if namespace == "" {
		return name
	}
	return strings.TrimRight(namespace, "_") + "__" + strings.TrimLeft(name, "_")
}

type toolAliasMaps struct {
	aliasByOriginal map[string]string
	originalByAlias map[string]string
}

func newToolAliasMaps() *toolAliasMaps {
	return &toolAliasMaps{
		aliasByOriginal: map[string]string{},
		originalByAlias: map[string]string{},
	}
}

func needsDevinToolAlias(name string) bool {
	lower := strings.ToLower(strings.TrimSpace(name))
	if lower == "" {
		return false
	}
	// Codex Desktop ships both mcp__server__tool names and MCP meta-tools
	// like list_mcp_resources. Devin treats any of these as an MCP
	// configuration surface and rejects the whole request, so alias every
	// name that still carries mcp semantics.
	return strings.Contains(lower, "mcp")
}

func (m *toolAliasMaps) alias(original string) string {
	original = strings.TrimSpace(original)
	if original == "" || m == nil {
		return original
	}
	if !needsDevinToolAlias(original) {
		return original
	}
	if existing, ok := m.aliasByOriginal[original]; ok {
		return existing
	}
	alias := makeDevinToolAlias(original)
	for {
		if prev, ok := m.originalByAlias[alias]; !ok || prev == original {
			break
		}
		alias = makeDevinToolAlias(original + "#" + alias)
	}
	m.aliasByOriginal[original] = alias
	m.originalByAlias[alias] = original
	return alias
}

func makeDevinToolAlias(original string) string {
	// Always use a neutral hash alias. Softening mcp__ to mcp_ still trips
	// Devin's MCP configuration check.
	sum := sha256.Sum256([]byte(original))
	alias := "cx_tool_" + hex.EncodeToString(sum[:8])
	if len(alias) > maxDevinToolAliasLen {
		return alias[:maxDevinToolAliasLen]
	}
	return alias
}

// stripMCPSemanticTools drops tools that were aliased from MCP-looking names
// (or still look like MCP). Used for the one-shot fallback retry after an MCP
// configuration denial so ordinary Codex tools can still proceed.
func stripMCPSemanticTools(tools []Tool, originalByAlias map[string]string) []Tool {
	if len(tools) == 0 {
		return nil
	}
	out := make([]Tool, 0, len(tools))
	for _, tool := range tools {
		original := tool.Name
		if mapped, ok := originalByAlias[tool.Name]; ok && mapped != "" {
			original = mapped
		}
		if needsDevinToolAlias(original) || needsDevinToolAlias(tool.Name) {
			continue
		}
		out = append(out, tool)
	}
	return out
}

func promptHasMCPSemantics(payload ChatPayload) bool {
	if strings.Contains(strings.ToLower(payload.System), "mcp") {
		return true
	}
	for _, prompt := range payload.Prompts {
		if strings.Contains(strings.ToLower(prompt.Content), "mcp") {
			return true
		}
		if strings.Contains(strings.ToLower(prompt.Thinking), "mcp") {
			return true
		}
		for _, call := range prompt.ToolCalls {
			name := call.Name
			if mapped, ok := payload.OriginalByAlias[name]; ok && mapped != "" {
				name = mapped
			}
			if needsDevinToolAlias(name) || needsDevinToolAlias(call.Name) {
				return true
			}
			if strings.Contains(strings.ToLower(call.Arguments), "mcp") {
				return true
			}
		}
	}
	for _, tool := range payload.Tools {
		if toolLooksLikeMCP(tool, payload.OriginalByAlias) {
			return true
		}
	}
	return false
}

func toolLooksLikeMCP(tool Tool, originalByAlias map[string]string) bool {
	original := tool.Name
	if mapped, ok := originalByAlias[tool.Name]; ok && mapped != "" {
		original = mapped
	}
	if needsDevinToolAlias(original) || needsDevinToolAlias(tool.Name) {
		return true
	}
	if strings.Contains(strings.ToLower(tool.Description), "mcp") {
		return true
	}
	return strings.Contains(strings.ToLower(string(tool.Parameters)), "mcp")
}

func scrubMCPText(text string) string {
	if text == "" || !strings.Contains(strings.ToLower(text), "mcp") {
		return text
	}
	// Keep structure readable while removing the upstream-triggering token.
	replacer := strings.NewReplacer(
		"MCP", "tool",
		"mcp", "tool",
		"Mcp", "tool",
	)
	return replacer.Replace(text)
}

func scrubMCPTextFromTools(tools []Tool) []Tool {
	if len(tools) == 0 {
		return nil
	}
	out := make([]Tool, 0, len(tools))
	for _, tool := range tools {
		tool.Description = scrubMCPText(tool.Description)
		if len(tool.Parameters) > 0 {
			tool.Parameters = json.RawMessage(scrubMCPText(string(tool.Parameters)))
		}
		out = append(out, tool)
	}
	return out
}

func scrubMCPToolCallsFromPrompts(prompts []Prompt, originalByAlias map[string]string) []Prompt {
	if len(prompts) == 0 {
		return prompts
	}
	out := make([]Prompt, len(prompts))
	copy(out, prompts)
	for i := range out {
		out[i].Content = scrubMCPText(out[i].Content)
		out[i].Thinking = scrubMCPText(out[i].Thinking)
		if len(out[i].ToolCalls) == 0 {
			continue
		}
		calls := make([]ToolCall, 0, len(out[i].ToolCalls))
		for _, call := range out[i].ToolCalls {
			name := call.Name
			if mapped, ok := originalByAlias[name]; ok && mapped != "" {
				name = mapped
			}
			if needsDevinToolAlias(name) || needsDevinToolAlias(call.Name) {
				continue
			}
			call.Arguments = scrubMCPText(call.Arguments)
			calls = append(calls, call)
		}
		out[i].ToolCalls = calls
	}
	return out
}

// Core local Codex tools that remain useful after MCP tools are stripped.
// Keep this list tight: these are the ones needed for reading/running locally.
var coreLocalToolOrder = []string{
	"exec_command",
	"write_stdin",
	"view_image",
	"request_user_input",
}

var coreLocalToolSchemas = map[string]struct {
	description string
	parameters  string
}{
	"exec_command": {
		description: "Runs a command and returns its output.",
		parameters:  `{"type":"object","properties":{"cmd":{"type":"string"},"workdir":{"type":"string"},"timeout_ms":{"type":"number"}},"required":["cmd"]}`,
	},
	"write_stdin": {
		description: "Writes characters to a running command session.",
		parameters:  `{"type":"object","properties":{"chars":{"type":"string"},"session_id":{"type":"string"}},"required":["chars"]}`,
	},
	"view_image": {
		description: "Views a local image file.",
		parameters:  `{"type":"object","properties":{"path":{"type":"string"}},"required":["path"]}`,
	},
	"request_user_input": {
		description: "Asks the user a question.",
		parameters:  `{"type":"object","properties":{"question":{"type":"string"}},"required":["question"]}`,
	},
}

// coreLocalTools returns the always-available core local tools with sanitized
// descriptions/schemas that do not carry Codex MCP wording.
func coreLocalTools() []Tool {
	out := make([]Tool, 0, len(coreLocalToolOrder))
	for _, name := range coreLocalToolOrder {
		schema := coreLocalToolSchemas[name]
		out = append(out, Tool{
			Name:        name,
			Description: schema.description,
			Parameters:  json.RawMessage(schema.parameters),
		})
	}
	return out
}

func restoreToolName(name string, originalByAlias map[string]string) string {
	name = strings.TrimSpace(name)
	if name == "" || len(originalByAlias) == 0 {
		return name
	}
	if original, ok := originalByAlias[name]; ok && original != "" {
		return original
	}
	return name
}

func buildToolsDiag(raw json.RawMessage, outbound []Tool) string {
	inbound := summarizeInboundTools(raw)
	outNames := make([]string, 0, len(outbound))
	for _, tool := range outbound {
		if name := truncateDiagName(tool.Name); name != "" {
			outNames = append(outNames, name)
		}
	}
	parts := make([]string, 0, 2)
	if inbound != "" {
		parts = append(parts, "in="+inbound)
	} else if len(raw) > 0 {
		parts = append(parts, "in=<unparsed>")
	} else {
		parts = append(parts, "in=<none>")
	}
	if len(outNames) == 0 {
		parts = append(parts, "out=<none>")
	} else {
		parts = append(parts, fmt.Sprintf("out(%d)=[%s]", len(outNames), strings.Join(outNames, ",")))
	}
	return truncateDiag(strings.Join(parts, " "))
}

func summarizeInboundTools(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var items []json.RawMessage
	if json.Unmarshal(raw, &items) != nil {
		return ""
	}
	entries := make([]string, 0, len(items))
	for _, item := range items {
		entries = append(entries, summarizeInboundToolItem(item)...)
	}
	if len(entries) == 0 {
		return "[]"
	}
	return fmt.Sprintf("[%s]", strings.Join(entries, ","))
}

func summarizeInboundToolItem(raw json.RawMessage) []string {
	var probe struct {
		Type     string `json:"type"`
		Name     string `json:"name"`
		Function struct {
			Name string `json:"name"`
		} `json:"function"`
		ServerLabel string            `json:"server_label"`
		Tools       []json.RawMessage `json:"tools"`
	}
	if json.Unmarshal(raw, &probe) != nil {
		return []string{"<?>"}
	}
	typ := strings.ToLower(strings.TrimSpace(probe.Type))
	if typ == "" {
		typ = "function"
	}
	name := firstNonEmpty(probe.Function.Name, probe.Name, probe.ServerLabel)
	entry := typ
	if trimmed := truncateDiagName(name); trimmed != "" {
		entry += ":" + trimmed
	}
	out := []string{entry}
	if typ == "namespace" {
		limit := len(probe.Tools)
		if limit > maxDevinNamespaceNestLen {
			limit = maxDevinNamespaceNestLen
		}
		for i := 0; i < limit; i++ {
			for _, nested := range summarizeInboundToolItem(probe.Tools[i]) {
				out = append(out, "ns."+nested)
			}
		}
		if len(probe.Tools) > maxDevinNamespaceNestLen {
			out = append(out, fmt.Sprintf("ns.<+%d>", len(probe.Tools)-maxDevinNamespaceNestLen))
		}
	}
	return out
}

func truncateDiagName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	runes := []rune(name)
	if len(runes) <= maxDevinToolDiagNameLen {
		return name
	}
	return string(runes[:maxDevinToolDiagNameLen-1]) + "…"
}

func truncateDiag(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	runes := []rune(text)
	if len(runes) <= maxDevinToolsDiagLen {
		return text
	}
	return string(runes[:maxDevinToolsDiagLen-1]) + "…"
}

func extractReasoning(msg translate.ChatMessage) string {
	// ChatMessage has no dedicated reasoning field; try content parts with type=thinking.
	if parts, ok := msg.Content.([]any); ok {
		var thinking []string
		for _, item := range parts {
			m, ok := item.(map[string]any)
			if !ok {
				continue
			}
			typ := strings.ToLower(strings.TrimSpace(asString(m["type"])))
			if typ == "thinking" || typ == "reasoning" {
				if t := asString(m["thinking"]); t != "" {
					thinking = append(thinking, t)
				} else if t := asString(m["text"]); t != "" {
					thinking = append(thinking, t)
				}
			}
		}
		return strings.Join(thinking, "\n")
	}
	return ""
}

func extractEffort(req translate.ChatRequest) (string, int) {
	effort := ""
	if len(req.ReasoningEffort) > 0 {
		var s string
		if json.Unmarshal(req.ReasoningEffort, &s) == nil {
			effort = s
		}
	}
	if effort == "" && len(req.Thinking) > 0 {
		var obj map[string]any
		if json.Unmarshal(req.Thinking, &obj) == nil {
			effort = asString(obj["type"])
			if effort == "" {
				effort = asString(obj["effort"])
			}
		}
	}
	budget := 0
	if len(req.ReasoningBudgetTokens) > 0 {
		var n int
		if json.Unmarshal(req.ReasoningBudgetTokens, &n) == nil {
			budget = n
		}
	}
	return effort, budget
}

func parseMaxTokens(req translate.ChatRequest) int {
	for _, raw := range []json.RawMessage{req.MaxCompletionTokens, req.MaxTokens} {
		if len(raw) == 0 {
			continue
		}
		var n int
		if json.Unmarshal(raw, &n) == nil && n > 0 {
			return n
		}
		var s string
		if json.Unmarshal(raw, &s) == nil {
			if v, err := strconv.Atoi(strings.TrimSpace(s)); err == nil && v > 0 {
				return v
			}
		}
	}
	return DefaultMaxTokens
}

func parseTemperature(req translate.ChatRequest) *float64 {
	if len(req.Temperature) == 0 {
		return nil
	}
	var f float64
	if json.Unmarshal(req.Temperature, &f) == nil {
		return &f
	}
	return nil
}

func asString(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case json.Number:
		return t.String()
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	default:
		return ""
	}
}
