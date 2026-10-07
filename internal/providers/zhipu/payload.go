package zhipu

import (
	"bytes"
	"encoding/json"
	"strings"

	"github.com/caigee-cmd/cli2api/internal/providers"
	"github.com/caigee-cmd/cli2api/internal/translate"
)

// buildChatBody serializes the internal chat request for Zhipu's OpenAI-
// compatible endpoint. Developer roles become system, because Zhipu rejects
// the OpenAI developer role. GLM reasoning_effort is rewritten onto the
// model's accepted scale.
func buildChatBody(req translate.ChatRequest, model string, level string) ([]byte, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	var obj map[string]any
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	if err := decoder.Decode(&obj); err != nil {
		return nil, err
	}
	obj["model"] = model
	obj["stream"] = true
	rewriteDeveloperRoles(obj)
	applyReasoning(obj, model, level)
	return json.Marshal(obj)
}

func rewriteDeveloperRoles(obj map[string]any) {
	messages, ok := obj["messages"].([]any)
	if !ok {
		return
	}
	for _, item := range messages {
		message, ok := item.(map[string]any)
		if !ok {
			continue
		}
		role, _ := message["role"].(string)
		if strings.EqualFold(strings.TrimSpace(role), "developer") {
			message["role"] = "system"
		}
	}
}

// applyReasoning writes the clamped level as a flat reasoning_effort.
// "none" is omitted: Zhipu does not document a disable switch, and sending
// one would invent a level the catalog does not declare.
func applyReasoning(obj map[string]any, model, level string) {
	delete(obj, "reasoning")
	if !isGLMModel(model) || level == "" || level == "none" {
		delete(obj, "reasoning_effort")
		return
	}
	obj["reasoning_effort"] = zhipuEffort(model, level)
}

// zhipuEffort maps the gateway vocabulary onto what GLM actually accepts.
// glm-5.3 keeps low; every earlier glm-* model only accepts high and max.
func zhipuEffort(model, level string) string {
	switch providers.NormalizeReasoningLevel(level) {
	case "max", "xhigh":
		return "max"
	case "low":
		if isGLM53(model) {
			return "low"
		}
		return "high"
	default:
		return "high"
	}
}

func isGLMModel(model string) bool {
	return strings.HasPrefix(canonicalModel(model), "glm-")
}

func isGLM53(model string) bool {
	id := canonicalModel(model)
	return id == "glm-5.3" || strings.HasPrefix(id, "glm-5.3-")
}

func canonicalModel(model string) string {
	id := strings.ToLower(strings.TrimSpace(model))
	if i := strings.LastIndex(id, "/"); i >= 0 {
		id = id[i+1:]
	}
	return id
}

// reasoningCaps declares the levels this adapter will send. The catalog does
// not publish them, so only ids this package rewrites get options. glm-5.3
// accepts low; older glm-* models accept high and max.
func reasoningCaps(model string) providers.ModelCapabilities {
	if !isGLMModel(model) {
		return providers.ModelCapabilities{Tools: true}
	}
	options := []string{"high", "max"}
	if isGLM53(model) {
		options = []string{"low", "high", "max"}
	}
	return providers.ModelCapabilities{
		Tools:              true,
		Reasoning:          true,
		ReasoningOptions:   options,
		ReasoningDefault:   "high",
		ReasoningType:      "effort",
		CanDisableThinking: false,
	}
}
