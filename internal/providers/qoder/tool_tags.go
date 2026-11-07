package qoder

import (
	"encoding/json"
	"regexp"
	"strings"
	"unicode"
)

// Fake XML / marker constants mirrored from qoder2api-hub issue #140 (e6e7622)
// and the shared leak-marker helpers that parse_tool_call_tags depends on.
const (
	toolCallOpen   = "<tool_call"
	leakMarker     = "[assistant 请求调用工具]"
	leakMarkerAlt  = "<assistant 请求调用工具>"
	jsonFence      = "```"
)

var (
	callTagOpenRE = regexp.MustCompile(`(?i)<\s*tool_call\b([^>]*)>`)
	callTagCloseRE = regexp.MustCompile(`(?i)<\s*/\s*tool_call\s*>`)
	callTagAttrNameRE = regexp.MustCompile(`(?i)\b(?:name|tool|tool_name|function)\s*=\s*(?:"([^"]+)"|'([^']+)')`)
	callTagLeadRE = regexp.MustCompile(`^\s*([A-Za-z_][A-Za-z0-9_.:\-]*)\s*`)
	leakMarkerRE = regexp.MustCompile(`[\[<(]*\s*assistant\s*请求调用工具\s*[\]>)]?`)
	leakStructRE = regexp.MustCompile(
		`name\s*=\s*["']|<tool_call|"(?:arguments|name)"\s*:|\{"`,
	)
	toolNameRE = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.:\-]*$`)
	trailingCommaRE = regexp.MustCompile(`,\s*([}\]])`)
	assistantMarkerLooseRE = regexp.MustCompile(`(?i)<\s*assistant\s*请求调用工具\s*>`)
)

// recoveredToolCall is the OpenAI-shaped call produced by parseToolCallTags.
type recoveredToolCall struct {
	ID       string `json:"id,omitempty"`
	Type     string `json:"type,omitempty"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

// repairArgsJSON tries to turn model-mangled tool arguments into valid JSON
// (hub 70ba58b). Empty input becomes "{}"; unrepairable input returns ok=false
// so callers can keep fail-closed semantics instead of inventing a call.
func repairArgsJSON(raw string) (string, bool) {
	text := raw
	if strings.TrimSpace(text) == "" {
		return "", true
	}
	candidates := []string{text}
	if fixed := repairJSONQuotes(text); fixed != text {
		candidates = append(candidates, fixed)
	}
	if escaped := escapeUnescapedQuotes(text); escaped != text {
		candidates = append(candidates, escaped)
	}
	baseCount := len(candidates)
	for i := 0; i < baseCount; i++ {
		candidates = append(candidates, trailingCommaRE.ReplaceAllString(candidates[i], "$1"))
	}
	seen := map[string]struct{}{}
	for _, cand := range candidates {
		if _, dup := seen[cand]; dup {
			continue
		}
		seen[cand] = struct{}{}
		if json.Valid([]byte(cand)) {
			return cand, true
		}
	}
	return "", false
}

// repairJSONQuotes is the coarse hub helper: treat every bare " as escaped
// after parking already-escaped ones. Used as one repair candidate.
func repairJSONQuotes(text string) string {
	const ph = "\x00"
	s := strings.ReplaceAll(text, `\"`, ph)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return strings.ReplaceAll(s, ph, `\"`)
}

// escapeUnescapedQuotes escapes inner double quotes inside JSON string values
// using the hub state machine (closing quote only when the next non-space is
// , } ] : or end-of-input).
func escapeUnescapedQuotes(s string) string {
	var out strings.Builder
	inStr := false
	for i := 0; i < len(s); {
		c := s[i]
		if !inStr {
			out.WriteByte(c)
			if c == '"' {
				inStr = true
			}
			i++
			continue
		}
		if c == '\\' && i+1 < len(s) {
			out.WriteString(s[i : i+2])
			i += 2
			continue
		}
		if c == '"' {
			j := i + 1
			for j < len(s) && (s[j] == ' ' || s[j] == '\t' || s[j] == '\r' || s[j] == '\n') {
				j++
			}
			if j >= len(s) || strings.ContainsRune(",}]:", rune(s[j])) {
				out.WriteByte(c)
				inStr = false
			} else {
				out.WriteString(`\"`)
			}
			i++
			continue
		}
		out.WriteByte(c)
		i++
	}
	return out.String()
}

func loadsLenient(raw string) (any, bool) {
	t := strings.TrimSpace(raw)
	if t == "" {
		return map[string]any{}, true
	}
	candidates := []string{t}
	noTrailing := trailingCommaRE.ReplaceAllString(t, "$1")
	if noTrailing != t {
		candidates = append(candidates, noTrailing)
	}
	escaped := escapeUnescapedQuotes(t)
	if escaped != t {
		candidates = append(candidates, escaped, trailingCommaRE.ReplaceAllString(escaped, "$1"))
	}
	seen := map[string]struct{}{}
	for _, cand := range candidates {
		if _, dup := seen[cand]; dup {
			continue
		}
		seen[cand] = struct{}{}
		var value any
		if json.Unmarshal([]byte(cand), &value) == nil {
			return value, true
		}
	}
	return nil, false
}

func makeToolCall(name string, args any) *recoveredToolCall {
	name = strings.Trim(strings.TrimSpace(name), "\"'`")
	if name == "" || !toolNameRE.MatchString(name) {
		return nil
	}
	var argstr string
	switch v := args.(type) {
	case nil:
		argstr = "{}"
	case string:
		argstr = strings.TrimSpace(v)
		if argstr == "" {
			argstr = "{}"
		}
	default:
		raw, err := json.Marshal(v)
		if err != nil {
			return nil
		}
		argstr = string(raw)
	}
	if fixed, ok := repairArgsJSON(argstr); ok {
		argstr = fixed
		if argstr == "" {
			argstr = "{}"
		}
	} else if !json.Valid([]byte(argstr)) {
		return nil
	}
	call := &recoveredToolCall{ID: "call_" + newID(), Type: "function"}
	call.Function.Name = name
	call.Function.Arguments = argstr
	return call
}

func stripJSONFence(text string) string {
	t := strings.TrimSpace(text)
	if strings.HasPrefix(t, jsonFence) {
		inner := t[len(jsonFence):]
		if len(inner) >= 4 && strings.EqualFold(inner[:4], "json") {
			inner = inner[4:]
		}
		t = strings.TrimLeft(inner, " \t\r\n")
	}
	if strings.HasSuffix(t, jsonFence) && len(t) > len(jsonFence) {
		t = strings.TrimRight(t[:len(t)-len(jsonFence)], " \t\r\n")
	}
	return t
}

func parseOneCallTag(body, attrName string) *recoveredToolCall {
	b := stripJSONFence(strings.TrimSpace(body))
	if attrName != "" {
		b = strings.TrimLeft(b, "=:")
		b = strings.TrimSpace(b)
		if b == "" {
			return makeToolCall(attrName, map[string]any{})
		}
		args, ok := loadsLenient(b)
		if !ok {
			return nil
		}
		return makeToolCall(attrName, args)
	}
	if b == "" {
		return nil
	}
	if strings.HasPrefix(b, "{") {
		objAny, ok := loadsLenient(b)
		if !ok {
			return nil
		}
		obj, _ := objAny.(map[string]any)
		if obj == nil {
			return nil
		}
		name, _ := obj["name"].(string)
		if strings.TrimSpace(name) == "" {
			if tool, _ := obj["tool"].(string); strings.TrimSpace(tool) != "" {
				name = tool
			} else if toolName, _ := obj["tool_name"].(string); strings.TrimSpace(toolName) != "" {
				name = toolName
			}
		}
		if strings.TrimSpace(name) == "" {
			return nil
		}
		for _, key := range []string{"arguments", "input", "args", "parameters"} {
			if value, exists := obj[key]; exists {
				return makeToolCall(name, value)
			}
		}
		return makeToolCall(name, map[string]any{})
	}
	m := callTagLeadRE.FindStringSubmatch(b)
	if m == nil {
		return nil
	}
	name := strings.TrimRight(m[1], ":")
	rest := strings.TrimSpace(b[len(m[0]):])
	rest = strings.TrimLeft(rest, ",:=")
	rest = strings.TrimSpace(rest)
	if strings.HasPrefix(rest, "(") && strings.HasSuffix(rest, ")") {
		rest = strings.TrimSpace(rest[1 : len(rest)-1])
	}
	if rest == "" {
		return makeToolCall(name, map[string]any{})
	}
	args, ok := loadsLenient(rest)
	if !ok {
		return nil
	}
	return makeToolCall(name, args)
}

// parseToolCallTags recovers fake-XML `<tool_call>…</tool_call>` echoes into
// structured tool_calls (hub e6e7622 / issue #140). Guards match the hub:
// declared tools required, names must hit the allow-list, and bare tags with
// surrounding prose are left as content.
func parseToolCallTags(text string, allowedNames map[string]struct{}) (calls []recoveredToolCall, clean string, ok bool) {
	t := text
	if t == "" || !strings.Contains(strings.ToLower(t), toolCallOpen) {
		return nil, text, false
	}
	type span struct{ start, end int }
	var spans []span
	pos := 0
	for {
		loc := callTagOpenRE.FindStringSubmatchIndex(t[pos:])
		if loc == nil {
			break
		}
		abs := make([]int, len(loc))
		for i, v := range loc {
			if v >= 0 {
				abs[i] = v + pos
			} else {
				abs[i] = -1
			}
		}
		attrBlob := ""
		if abs[2] >= 0 && abs[3] >= 0 {
			attrBlob = t[abs[2]:abs[3]]
		}
		attrName := ""
		if am := callTagAttrNameRE.FindStringSubmatch(attrBlob); am != nil {
			if am[1] != "" {
				attrName = am[1]
			} else {
				attrName = am[2]
			}
		}
		openEnd := abs[1]
		closeLoc := callTagCloseRE.FindStringIndex(t[openEnd:])
		nextLoc := callTagOpenRE.FindStringIndex(t[openEnd:])
		var body string
		var end int
		switch {
		case nextLoc != nil && (closeLoc == nil || nextLoc[0] < closeLoc[0]):
			body = t[openEnd : openEnd+nextLoc[0]]
			end = openEnd + nextLoc[0]
		case closeLoc != nil:
			body = t[openEnd : openEnd+closeLoc[0]]
			end = openEnd + closeLoc[1]
		default:
			body = t[openEnd:]
			end = len(t)
		}
		if call := parseOneCallTag(body, attrName); call != nil {
			calls = append(calls, *call)
			spans = append(spans, span{start: abs[0], end: end})
		}
		pos = end
		if pos <= openEnd {
			pos = openEnd + 1
		}
		if pos >= len(t) {
			break
		}
	}
	if len(calls) == 0 {
		return nil, text, false
	}
	if len(allowedNames) == 0 {
		return nil, text, false
	}
	for _, call := range calls {
		if _, hit := allowedNames[call.Function.Name]; !hit {
			return nil, text, false
		}
	}
	hasMarker := leakMarkerRE.MatchString(t)
	lead := strings.TrimSpace(strings.Trim(leakMarkerRE.ReplaceAllString(t[:spans[0].start], ""), "`"))
	if !hasMarker && lead != "" {
		return nil, text, false
	}
	tailTxt := strings.TrimSpace(strings.Trim(leakMarkerRE.ReplaceAllString(t[spans[len(spans)-1].end:], ""), "`"))
	if tailTxt != "" && (leakStructRE.MatchString(tailTxt) || callTagOpenRE.MatchString(tailTxt)) {
		return nil, text, false
	}
	if !hasMarker && tailTxt != "" {
		return nil, text, false
	}
	var cleaned strings.Builder
	last := 0
	for _, sp := range spans {
		cleaned.WriteString(t[last:sp.start])
		last = sp.end
	}
	cleaned.WriteString(t[last:])
	out := leakMarkerRE.ReplaceAllString(cleaned.String(), "")
	out = assistantMarkerLooseRE.ReplaceAllString(out, "")
	return calls, strings.TrimSpace(out), true
}

// toolTagCandidate reports whether text still looks like an in-progress
// `<tool_call>` block (stream hold).
func toolTagCandidate(text string) bool {
	t := strings.TrimLeftFunc(text, unicode.IsSpace)
	if t == "" {
		return true
	}
	low := strings.ToLower(t)
	if strings.HasPrefix(toolCallOpen, low) {
		return true
	}
	if !strings.HasPrefix(low, toolCallOpen) {
		return false
	}
	nxt := ""
	if len(low) > len(toolCallOpen) {
		nxt = low[len(toolCallOpen) : len(toolCallOpen)+1]
	}
	switch nxt {
	case "", ">", " ", "\t", "\n", "\r", "/":
		// ok
	default:
		return false
	}
	if !strings.Contains(t, ">") {
		return true
	}
	if loc := callTagOpenRE.FindStringIndex(t); loc == nil || loc[0] != 0 {
		return false
	}
	closeLoc := callTagCloseRE.FindStringIndex(t)
	if closeLoc == nil {
		return true
	}
	rest := strings.TrimSpace(strings.Trim(leakMarkerRE.ReplaceAllString(t[closeLoc[1]:], ""), "`"))
	if rest == "" {
		return true
	}
	return !leakStructRE.MatchString(rest)
}

func toolTagTailHold(text string) string {
	i := strings.LastIndex(text, "<")
	if i < 0 || len(text)-i > len(toolCallOpen)+8 {
		return ""
	}
	tail := text[i:]
	if strings.Contains(tail, ">") {
		return ""
	}
	if strings.HasPrefix(toolCallOpen, strings.ToLower(tail)) {
		return tail
	}
	return ""
}

// allowedToolNames extracts OpenAI function tool names from a request tools
// array. Empty / undeclared tools yield a nil map (recovery stays off).
func allowedToolNames(raw json.RawMessage) map[string]struct{} {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var items []map[string]any
	if json.Unmarshal(raw, &items) != nil {
		return nil
	}
	out := map[string]struct{}{}
	for _, item := range items {
		typ, _ := item["type"].(string)
		typ = strings.ToLower(strings.TrimSpace(typ))
		switch typ {
		case "function", "":
			if fn, _ := item["function"].(map[string]any); fn != nil {
				if name, _ := fn["name"].(string); strings.TrimSpace(name) != "" {
					out[strings.TrimSpace(name)] = struct{}{}
				}
			}
			if name, _ := item["name"].(string); strings.TrimSpace(name) != "" {
				out[strings.TrimSpace(name)] = struct{}{}
			}
		case "namespace":
			tools, _ := item["tools"].([]any)
			for _, child := range tools {
				childMap, _ := child.(map[string]any)
				if childMap == nil {
					continue
				}
				if fn, _ := childMap["function"].(map[string]any); fn != nil {
					if name, _ := fn["name"].(string); strings.TrimSpace(name) != "" {
						out[strings.TrimSpace(name)] = struct{}{}
					}
				}
				if name, _ := childMap["name"].(string); strings.TrimSpace(name) != "" {
					out[strings.TrimSpace(name)] = struct{}{}
				}
			}
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// recoverToolTagsFromContent converts a completed assistant content blob that
// is actually a fake-XML tool echo into structured tool_calls. When recovery
// fails, content is left unchanged.
func recoverToolTagsFromContent(content string, tools json.RawMessage, allowed map[string]struct{}) (newContent string, newTools json.RawMessage, recovered bool) {
	if allowed == nil {
		allowed = allowedToolNames(tools)
	}
	calls, clean, ok := parseToolCallTags(content, allowed)
	if !ok || len(calls) == 0 {
		return content, nil, false
	}
	raw, err := json.Marshal(calls)
	if err != nil {
		return content, nil, false
	}
	return clean, raw, true
}

// repairToolCallArgumentsJSON walks an accumulated tool_calls JSON array and
// repairs each function.arguments string in place when possible.
func repairToolCallArgumentsJSON(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return raw
	}
	var calls []map[string]any
	if json.Unmarshal(raw, &calls) != nil || len(calls) == 0 {
		return raw
	}
	changed := false
	for _, call := range calls {
		fn, _ := call["function"].(map[string]any)
		if fn == nil {
			continue
		}
		args, _ := fn["arguments"].(string)
		if args == "" || json.Valid([]byte(args)) {
			continue
		}
		if fixed, ok := repairArgsJSON(args); ok {
			fn["arguments"] = fixed
			changed = true
		}
	}
	if !changed {
		return raw
	}
	out, err := json.Marshal(calls)
	if err != nil {
		return raw
	}
	return out
}
