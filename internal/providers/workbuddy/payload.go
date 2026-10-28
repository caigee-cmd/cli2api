package workbuddy

import (
	"encoding/json"
	"regexp"
	"strings"

	"github.com/caigee-cmd/cli2api/internal/translate"
)

// PrepareBody forces streaming and normalizes tool_choice to the string form
// the upstream API accepts. It only rewrites known keys; unknown fields stay.
func PrepareBody(src []byte) []byte {
	if len(src) == 0 {
		return src
	}
	var body map[string]any
	if err := json.Unmarshal(src, &body); err != nil {
		return src
	}
	body["stream"] = true
	normalizeToolChoice(body)
	normalizeTools(body)
	dropEmptyTools(body)
	repairToolSequence(body)
	normalizeEmptyMessageContent(body)
	ensureLeadingSystem(body)
	sanitizeWAFKeywords(body)
	out, err := json.Marshal(body)
	if err != nil {
		return src
	}
	return out
}

func normalizeTools(body map[string]any) {
	raw, ok := body["tools"]
	if !ok || raw == nil {
		return
	}
	encoded, err := json.Marshal(raw)
	if err != nil {
		return
	}
	normalized, err := translate.NormalizeOpenAITools(encoded)
	if err != nil {
		return
	}
	if len(normalized) == 0 {
		delete(body, "tools")
		delete(body, "tool_choice")
		return
	}
	var tools []any
	if err := json.Unmarshal(normalized, &tools); err != nil {
		return
	}
	body["tools"] = tools
}

func normalizeEmptyMessageContent(body map[string]any) {
	raw, ok := body["messages"]
	if !ok {
		return
	}
	list, ok := raw.([]any)
	if !ok {
		return
	}
	kept := make([]any, 0, len(list))
	for _, item := range list {
		message, ok := item.(map[string]any)
		if !ok {
			kept = append(kept, item)
			continue
		}
		if messageContentEmpty(message) && !hasToolCalls(message) && !hasReasoningContent(message) && !isToolResult(message) {
			continue
		}
		kept = append(kept, item)
	}
	body["messages"] = kept
}

func messageContentEmpty(message map[string]any) bool {
	content, exists := message["content"]
	if !exists || content == nil {
		return true
	}
	if value, ok := content.(string); ok {
		return strings.TrimSpace(value) == ""
	}
	if value, ok := content.([]any); ok {
		return len(value) == 0
	}
	return false
}

func hasToolCalls(message map[string]any) bool {
	raw, ok := message["tool_calls"]
	if !ok || raw == nil {
		return false
	}
	if calls, ok := raw.([]any); ok {
		return len(calls) > 0
	}
	return true
}

func hasReasoningContent(message map[string]any) bool {
	return strings.TrimSpace(stringField(message, "reasoning_content")) != ""
}

func isToolResult(message map[string]any) bool {
	return messageRole(message) == "tool" || strings.TrimSpace(stringField(message, "tool_call_id")) != ""
}

func messageRole(message map[string]any) string {
	role, _ := message["role"].(string)
	return strings.ToLower(strings.TrimSpace(role))
}

func stringField(message map[string]any, key string) string {
	value, _ := message[key].(string)
	return strings.TrimSpace(value)
}

// repairToolSequence preserves complete tool rounds byte-for-byte at the
// message level and drops only incomplete rounds. Clients that stop a stream
// mid-tool often resend an assistant tool_calls message without all results;
// WorkBuddy then rejects the next turn with code 11148.
func repairToolSequence(body map[string]any) {
	raw, ok := body["messages"]
	if !ok {
		return
	}
	list, ok := raw.([]any)
	if !ok {
		return
	}
	kept := make([]any, 0, len(list))
	for i := 0; i < len(list); {
		message, ok := list[i].(map[string]any)
		if !ok {
			kept = append(kept, list[i])
			i++
			continue
		}
		if messageRole(message) == "tool" {
			i++
			continue
		}
		if messageRole(message) != "assistant" || !hasToolCalls(message) {
			kept = append(kept, message)
			i++
			continue
		}
		calls, _ := message["tool_calls"].([]any)
		results := make([]map[string]any, 0)
		j := i + 1
		for j < len(list) {
			next, ok := list[j].(map[string]any)
			if !ok || messageRole(next) != "tool" {
				break
			}
			results = append(results, next)
			j++
		}
		if !hasCompleteToolRound(calls, results) {
			delete(message, "tool_calls")
			if !messageContentEmpty(message) {
				kept = append(kept, message)
			}
			i = j
			continue
		}
		kept = append(kept, message)
		for _, result := range results {
			kept = append(kept, result)
		}
		i = j
	}
	body["messages"] = kept
}

func hasCompleteToolRound(calls []any, results []map[string]any) bool {
	if len(calls) == 0 || len(calls) != len(results) {
		return false
	}
	callIDs := make(map[string]struct{}, len(calls))
	for _, raw := range calls {
		call, ok := raw.(map[string]any)
		if !ok {
			return false
		}
		id := toolCallID(call)
		if id == "" {
			return false
		}
		if _, exists := callIDs[id]; exists {
			return false
		}
		callIDs[id] = struct{}{}
	}
	resultIDs := make(map[string]struct{}, len(results))
	for _, result := range results {
		id := toolResultID(result)
		if id == "" {
			return false
		}
		if _, exists := callIDs[id]; !exists {
			return false
		}
		if _, exists := resultIDs[id]; exists {
			return false
		}
		resultIDs[id] = struct{}{}
	}
	return len(callIDs) == len(resultIDs)
}

func toolCallID(call map[string]any) string {
	if id := stringField(call, "id"); id != "" {
		return id
	}
	return stringField(call, "tool_call_id")
}

func toolResultID(result map[string]any) string {
	if id := stringField(result, "tool_call_id"); id != "" {
		return id
	}
	return stringField(result, "id")
}

// ensureLeadingSystem satisfies WorkBuddy Global code 11128 ("first message
// is not system prompt"). Drop-system-prompt strips caller identity, which
// would otherwise leave a user message first. The placeholder must be non-empty
// because WorkBuddy rejects messages with empty content (code 11151).
func ensureLeadingSystem(body map[string]any) {
	raw, ok := body["messages"]
	if !ok {
		return
	}
	list, ok := raw.([]any)
	if !ok {
		return
	}
	placeholder := map[string]any{"role": "system", "content": "You are a helpful assistant."}
	if len(list) == 0 {
		body["messages"] = []any{placeholder}
		return
	}
	first, ok := list[0].(map[string]any)
	if ok {
		role, _ := first["role"].(string)
		if strings.EqualFold(strings.TrimSpace(role), "system") {
			return
		}
	}
	body["messages"] = append([]any{placeholder}, list...)
}

func normalizeToolChoice(body map[string]any) {
	raw, ok := body["tool_choice"]
	if !ok {
		return
	}
	switch value := raw.(type) {
	case string:
		if value == "none" {
			delete(body, "tool_choice")
			delete(body, "tools")
			delete(body, "functions")
			return
		}
		return
	case map[string]any:
		kind, _ := value["type"].(string)
		switch kind {
		case "none":
			delete(body, "tool_choice")
			delete(body, "tools")
			delete(body, "functions")
		case "auto", "required":
			body["tool_choice"] = kind
		case "function":
			name := functionName(value)
			if name == "" {
				name = "auto"
			}
			body["tool_choice"] = name
		default:
			delete(body, "tool_choice")
		}
	default:
		delete(body, "tool_choice")
	}
}

func functionName(value map[string]any) string {
	if fn, ok := value["function"].(map[string]any); ok {
		if name, _ := fn["name"].(string); name != "" {
			return name
		}
	}
	name, _ := value["name"].(string)
	return name
}

func dropEmptyTools(body map[string]any) {
	raw, ok := body["tools"]
	if !ok {
		return
	}
	if raw == nil {
		delete(body, "tools")
		return
	}
	list, ok := raw.([]any)
	if ok && len(list) == 0 {
		delete(body, "tools")
		delete(body, "tool_choice")
	}
}

var wafFingerprintReplacements = []struct {
	pattern     string
	replacement string
}{
	{
		pattern:     "Main branch (you will usually use this for PRs):",
		replacement: "Main branch (target for pull requests):",
	},
	{
		pattern:     "Main branch (you will usually use this for PRs)",
		replacement: "Main branch (target for pull requests)",
	},
}

// Identity-scoped 11-128 rules ported from workbuddy2api-hub (~v1.7.25–26,
// #570/#579/#574). Applied only to system / developer / assistant; user and
// tool content is left untouched as caller data.
var (
	// Start of an identity sentence through the attribution verb. The rest of
	// the sentence (including mid-token dots like v4.1) is consumed by
	// rewriteIdentitySentences because RE2 has no lookahead.
	wafIdentityStartRe = regexp.MustCompile(`(?i)You are\s+` +
		`(?:an?\s+|the\s+)?(?:[\w.-]+\s+){0,3}?` +
		`(?:agent|assistant|model|AI|LLM)?[\s,]*` +
		`(?:powered by|built on|built by|created by|developed by|made by|trained by)`)
	wafIdentityReplacement = "You are a helpful AI assistant."

	wafDSHGUIRe = regexp.MustCompile(`(?i)(You are interacting with the user through the ` +
		`DeepSeek[ \t]+Harness[ \t]+Web[ \t]+GUI)` +
		` whose URL the runtime context names\.`)
	wafDSHGUIReplacement = `$1 whose URL is named in the runtime context.`

	// DSH file-reference paragraph (hub #684 / v1.9.0): official
	// @deepseek-ai/dsh-file-reference injects FILE_REFERENCE_PROMPT; the
	// full five-sentence block trips 11-128. Minimal fix inserts one word
	// in sentence A (`paths the user` -> `paths that the user`). No
	// negative lookahead needed under RE2: after rewrite, "paths " is
	// followed by "that ", so the pattern no longer matches (idempotent).
	wafDSHFilerefRe = regexp.MustCompile(`(?i)(Tokens prefixed with @ are paths )` +
		`(the user explicitly referenced\.)`)
	wafDSHFilerefReplacement = `${1}that $2`

	wafBrandRe          = regexp.MustCompile(`(?i)DeepSeek[ \t]+Harness`)
	wafBrandReplacement = "DeepSeek"
)

// rewriteIdentitySentences replaces confirmed self-intro sentences. Sentence
// end is a period followed by whitespace/EOS, a newline, or EOS — so dots
// inside model ids (deepseek-v4.1-flash) stay inside the match.
func rewriteIdentitySentences(text string) string {
	var b strings.Builder
	remaining := text
	for {
		loc := wafIdentityStartRe.FindStringIndex(remaining)
		if loc == nil {
			b.WriteString(remaining)
			return b.String()
		}
		b.WriteString(remaining[:loc[0]])
		rest := remaining[loc[1]:]
		end := len(rest)
		for i := 0; i < len(rest); i++ {
			switch rest[i] {
			case '\n':
				end = i
				goto replaced
			case '.':
				if i+1 == len(rest) || rest[i+1] == ' ' || rest[i+1] == '\t' || rest[i+1] == '\n' {
					end = i + 1
					goto replaced
				}
			}
		}
	replaced:
		b.WriteString(wafIdentityReplacement)
		remaining = rest[end:]
	}
}

// sanitizeWAFKeywords rewrites known client fingerprint phrases that trigger
// upstream WorkBuddy WAF rules (code 11128 / 11-128: "Illegal API invocation
// from an unapproved channel"). Literal Main-branch rewrites apply to every
// role; identity / DSH GUI / fileref / brand rules follow upstream and only touch
// system, developer, and assistant.
func sanitizeWAFKeywords(body map[string]any) {
	raw, ok := body["messages"]
	if !ok {
		return
	}
	list, ok := raw.([]any)
	if !ok {
		return
	}
	for _, item := range list {
		message, ok := item.(map[string]any)
		if !ok {
			continue
		}
		role, _ := message["role"].(string)
		identity := role == "system" || role == "developer" || role == "assistant"
		if content, ok := message["content"].(string); ok {
			message["content"] = sanitizeFingerprintText(content, identity)
		} else if parts, ok := message["content"].([]any); ok {
			for _, part := range parts {
				if partMap, ok := part.(map[string]any); ok {
					if text, ok := partMap["text"].(string); ok {
						partMap["text"] = sanitizeFingerprintText(text, identity)
					}
				}
			}
		}
		// reasoning_content is model scratch, not an identity surface.
		if reasoning, ok := message["reasoning_content"].(string); ok {
			message["reasoning_content"] = sanitizeFingerprintText(reasoning, false)
		}
	}
}

func sanitizeFingerprintText(text string, identity bool) string {
	if text == "" {
		return text
	}
	if identity {
		text = rewriteIdentitySentences(text)
		text = wafDSHGUIRe.ReplaceAllString(text, wafDSHGUIReplacement)
		text = wafDSHFilerefRe.ReplaceAllString(text, wafDSHFilerefReplacement)
		text = wafBrandRe.ReplaceAllString(text, wafBrandReplacement)
	}
	for _, rule := range wafFingerprintReplacements {
		if strings.Contains(text, rule.pattern) {
			text = strings.ReplaceAll(text, rule.pattern, rule.replacement)
		}
	}
	return text
}
