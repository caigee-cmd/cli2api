package devin

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strconv"
	"strings"

	"github.com/caigee-cmd/cli2api/internal/translate"
)

const maxDevinToolAliasLen = 64

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
			text := translate.ContentToString(msg.Content)
			prompts = append(prompts, Prompt{Source: 4, Content: text, ToolCallID: strings.TrimSpace(msg.ToolCallID)})
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

	return ChatPayload{
		System:          system,
		Prompts:         prompts,
		Tools:           parseTools(req.Tools, aliases),
		Temperature:     temp,
		MaxTokens:       maxTokens,
		ModelUID:        modelUID,
		Effort:          effort,
		Budget:          budget,
		OriginalByAlias: aliases.originalByAlias,
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
	var tools []struct {
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
	if json.Unmarshal(raw, &tools) != nil {
		return nil
	}
	out := make([]Tool, 0, len(tools))
	for _, t := range tools {
		name := firstNonEmpty(t.Function.Name, t.Name)
		if name == "" {
			continue
		}
		name = aliases.alias(name)
		desc := firstNonEmpty(t.Function.Description, t.Description)
		params := t.Function.Parameters
		if len(params) == 0 {
			params = t.Parameters
		}
		out = append(out, Tool{Name: name, Description: desc, Parameters: params})
	}
	return out
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
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(name)), "mcp__")
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
	alias := strings.ReplaceAll(original, "__", "_")
	alias = strings.ReplaceAll(alias, "-", "_")
	alias = strings.TrimSpace(alias)
	if alias == "" {
		alias = "mcp_tool"
	}
	if len(alias) <= maxDevinToolAliasLen && !strings.Contains(alias, "__") {
		return alias
	}
	sum := sha256.Sum256([]byte(original))
	return "mcp_" + hex.EncodeToString(sum[:8])
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
