package command

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/caigee-cmd/cli2api/internal/providers"
	"github.com/caigee-cmd/cli2api/internal/translate"
)

// generateRequest is the /alpha/generate envelope. Every config field is
// required (the gateway returns a 400 listing missing paths otherwise); the
// values are not load-bearing for routing, but neutral defaults must be present.
type generateRequest struct {
	Config         generateConfig `json:"config"`
	Memory         string         `json:"memory"`
	Taste          any            `json:"taste"`
	Skills         any            `json:"skills"`
	PermissionMode string         `json:"permissionMode"`
	Params         generateParams `json:"params"`
}

type generateConfig struct {
	WorkingDir    string   `json:"workingDir"`
	Date          string   `json:"date"`
	Environment   string   `json:"environment"`
	Structure     []string `json:"structure"`
	IsGitRepo     bool     `json:"isGitRepo"`
	CurrentBranch string   `json:"currentBranch"`
	MainBranch    string   `json:"mainBranch"`
	GitStatus     string   `json:"gitStatus"`
	RecentCommits []string `json:"recentCommits"`
}

type generateParams struct {
	Model           string       `json:"model"`
	System          string       `json:"system"`
	Messages        []cmdMessage `json:"messages"`
	Tools           []cmdTool    `json:"tools"`
	MaxTokens       int          `json:"max_tokens"`
	Temperature     *float64     `json:"temperature,omitempty"`
	Stream          bool         `json:"stream"`
	ReasoningEffort string       `json:"reasoning_effort,omitempty"`
}

type cmdMessage struct {
	Role    string `json:"role"`
	Content []any  `json:"content"`
}

type cmdTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"input_schema"`
}

// buildGenerateRequest converts an internal chat request into the strict
// /alpha/generate envelope. model must already be resolved to a catalog id.
func buildGenerateRequest(req translate.ChatRequest, model string) ([]byte, error) {
	system, messages := buildMessages(req.Messages)
	tools := buildTools(req.Tools)
	envelope := generateRequest{
		Config:         staticConfig(),
		Memory:         "",
		Taste:          nil,
		Skills:         nil,
		PermissionMode: "standard",
		Params: generateParams{
			Model:           model,
			System:          system,
			Messages:        messages,
			Tools:           tools,
			MaxTokens:       parseMaxTokens(req),
			Temperature:     parseTemperature(req),
			Stream:          true,
			ReasoningEffort: parseReasoningEffort(req),
		},
	}
	return json.Marshal(envelope)
}

func staticConfig() generateConfig {
	return generateConfig{
		WorkingDir:    "/",
		Date:          time.Now().UTC().Format("2006-01-02"),
		Environment:   CLIEnvironment,
		Structure:     []string{},
		IsGitRepo:     false,
		CurrentBranch: "",
		MainBranch:    "",
		GitStatus:     "",
		RecentCommits: []string{},
	}
}

// buildMessages folds system/developer turns into the system prompt and rewrites
// the rest into the Vercel AI SDK ModelMessage[] shape (tool-call parts on
// assistant messages, a separate role:"tool" message for tool results). It is
// not Anthropic content blocks and not OpenAI tool messages.
func buildMessages(messages []translate.ChatMessage) (string, []cmdMessage) {
	var systemParts []string
	out := make([]cmdMessage, 0, len(messages))
	toolNameByID := map[string]string{}

	for _, msg := range messages {
		switch strings.ToLower(strings.TrimSpace(msg.Role)) {
		case "system", "developer":
			if text := strings.TrimSpace(translate.ContentToString(msg.Content)); text != "" {
				systemParts = append(systemParts, text)
			}
		case "assistant":
			parts := assistantContent(msg, toolNameByID)
			if len(parts) > 0 {
				out = append(out, cmdMessage{Role: "assistant", Content: parts})
			}
		case "tool":
			part := toolResultPart(msg, toolNameByID)
			if last := len(out) - 1; last >= 0 && out[last].Role == "tool" {
				out[last].Content = append(out[last].Content, part)
			} else {
				out = append(out, cmdMessage{Role: "tool", Content: []any{part}})
			}
		default: // user and anything unexpected
			parts := userContent(msg.Content)
			if len(parts) > 0 {
				out = append(out, cmdMessage{Role: "user", Content: parts})
			}
		}
	}
	return strings.TrimSpace(strings.Join(systemParts, "\n\n")), out
}

func userContent(content any) []any {
	switch v := content.(type) {
	case nil:
		return nil
	case string:
		if strings.TrimSpace(v) == "" {
			return nil
		}
		return []any{map[string]any{"type": "text", "text": v}}
	case []any:
		parts := make([]any, 0, len(v))
		for _, item := range v {
			m, ok := item.(map[string]any)
			if !ok {
				continue
			}
			switch strings.ToLower(strings.TrimSpace(asString(m["type"]))) {
			case "text":
				if text, ok := m["text"].(string); ok && text != "" {
					parts = append(parts, map[string]any{"type": "text", "text": text})
				}
			case "image_url", "input_image", "image":
				if part := imagePart(m); part != nil {
					parts = append(parts, part)
				}
			default:
				if text, ok := m["text"].(string); ok && text != "" {
					parts = append(parts, map[string]any{"type": "text", "text": text})
				}
			}
		}
		return parts
	default:
		text := translate.ContentToString(content)
		if strings.TrimSpace(text) == "" {
			return nil
		}
		return []any{map[string]any{"type": "text", "text": text}}
	}
}

func imagePart(m map[string]any) map[string]any {
	url := ""
	if nested, ok := m["image_url"].(map[string]any); ok {
		url = asString(nested["url"])
	}
	if url == "" {
		url = asString(m["url"])
	}
	if url == "" {
		url = asString(m["image"])
	}
	if url == "" {
		return nil
	}
	mediaType := firstNonEmpty(asString(m["mediaType"]), asString(m["media_type"]), asString(m["mime_type"]))
	part := map[string]any{"type": "image", "image": url}
	if mediaType != "" {
		part["mediaType"] = mediaType
	}
	return part
}

func assistantContent(msg translate.ChatMessage, toolNameByID map[string]string) []any {
	parts := make([]any, 0, 2)
	if text := translate.ContentToString(msg.Content); strings.TrimSpace(text) != "" {
		parts = append(parts, map[string]any{"type": "text", "text": text})
	}
	for _, call := range parseToolCalls(msg.ToolCalls) {
		if strings.TrimSpace(call.ID) == "" || strings.TrimSpace(call.Name) == "" {
			continue
		}
		toolNameByID[call.ID] = call.Name
		parts = append(parts, map[string]any{
			"type":       "tool-call",
			"toolCallId": call.ID,
			"toolName":   call.Name,
			"input":      call.Input,
		})
	}
	return parts
}

func toolResultPart(msg translate.ChatMessage, toolNameByID map[string]string) map[string]any {
	callID := strings.TrimSpace(msg.ToolCallID)
	name := toolNameByID[callID]
	if name == "" {
		name = "unknown"
	}
	return map[string]any{
		"type":       "tool-result",
		"toolCallId": callID,
		"toolName":   name,
		"output":     map[string]any{"type": "text", "value": translate.ContentToString(msg.Content)},
	}
}

type openAIToolCall struct {
	ID     string
	Name   string
	Input  any
	RawArg string
}

func parseToolCalls(raw json.RawMessage) []openAIToolCall {
	if len(raw) == 0 {
		return nil
	}
	var items []struct {
		ID       string `json:"id"`
		Function struct {
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
		} `json:"function"`
	}
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil
	}
	out := make([]openAIToolCall, 0, len(items))
	for _, item := range items {
		input := any(map[string]any{})
		if strings.TrimSpace(item.Function.Arguments) != "" {
			var decoded any
			if json.Unmarshal([]byte(item.Function.Arguments), &decoded) == nil {
				input = decoded
			}
		}
		out = append(out, openAIToolCall{ID: item.ID, Name: item.Function.Name, Input: input, RawArg: item.Function.Arguments})
	}
	return out
}

// buildTools flattens Codex/namespace wrappers via the shared normalizer and
// maps each onto the {name,description,input_schema} form the gateway expects
// (it internally rewrites input_schema to Vercel's {type:"function",…}).
func buildTools(raw json.RawMessage) []cmdTool {
	normalized, err := translate.NormalizeOpenAITools(raw)
	if err != nil || len(normalized) == 0 {
		return []cmdTool{}
	}
	var items []struct {
		Function struct {
			Name        string          `json:"name"`
			Description string          `json:"description"`
			Parameters  json.RawMessage `json:"parameters"`
		} `json:"function"`
	}
	if err := json.Unmarshal(normalized, &items); err != nil {
		return []cmdTool{}
	}
	out := make([]cmdTool, 0, len(items))
	for _, item := range items {
		name := strings.TrimSpace(item.Function.Name)
		if name == "" {
			continue
		}
		schema := item.Function.Parameters
		if len(schema) == 0 || string(schema) == "null" {
			schema = json.RawMessage(`{"type":"object","properties":{}}`)
		}
		out = append(out, cmdTool{Name: name, Description: item.Function.Description, InputSchema: schema})
	}
	return out
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
			var parsed int
			if _, err := fmt.Sscanf(strings.TrimSpace(s), "%d", &parsed); err == nil && parsed > 0 {
				return parsed
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

func parseReasoningEffort(req translate.ChatRequest) string {
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
			effort = firstNonEmpty(asString(obj["type"]), asString(obj["effort"]))
		}
	}
	normalized := providers.NormalizeReasoningLevel(effort)
	if normalized == "none" {
		return ""
	}
	return normalized
}

func asString(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case json.Number:
		return t.String()
	case float64:
		return strings.TrimRight(strings.TrimRight(fmt.Sprintf("%f", t), "0"), ".")
	default:
		return ""
	}
}
