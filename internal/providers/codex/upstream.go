package codex

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"math/big"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Codex Responses rejects these top-level members outright. CLIProxyAPI strips
// the same set before every Codex call, native or translated.
var codexRejectedFields = []string{
	"max_output_tokens",
	"max_completion_tokens",
	"max_tokens",
	"temperature",
	"top_p",
	"top_k",
	"truncation",
	"user",
	"prompt_cache_options",
	"prompt_cache_retention",
	"safety_identifier",
	"stream_options",
	"previous_response_id",
	"generate",
	"context_management",
}

const (
	codexInputItemIDLimit                 = 64
	codexMessageItemIDPrefix              = "msg"
	codexReasoningItemIDPrefix            = "rs"
	codexFunctionCallItemIDPrefix         = "fc"
	codexCustomToolCallItemIDPrefix       = "ctc"
	codexCustomToolCallOutputItemIDPrefix = "ctco"

	codexInputItemIDOccupied  uint8 = 1 << 0
	codexInputItemIDPreserved uint8 = 1 << 1

	codexComplexUnionBranchThreshold = 8
	codexRoutingHintHeader           = "X-Codex-Routing-Hint"
	codexResponsesLiteHeader         = "X-OpenAI-Internal-Codex-Responses-Lite"
)

// normalizeCodexUpstream applies the ChatGPT Codex backend constraints that
// CLIProxyAPI applies on every /responses call. It edits one attempt's field
// copy. Item bytes that need no change are left untouched.
//
// lite is the catalog's responses-lite flag for the resolved model.
func normalizeCodexUpstream(fields map[string]json.RawMessage, model string, lite bool, sessionID string) error {
	fields["model"] = mustJSON(model)
	fields["stream"] = json.RawMessage(`true`)
	fields["store"] = json.RawMessage(`false`)
	for _, key := range codexRejectedFields {
		delete(fields, key)
	}
	normalizeServiceTier(fields)
	fields["include"] = json.RawMessage(`["reasoning.encrypted_content"]`)
	if isNullJSON(fields["instructions"]) {
		fields["instructions"] = json.RawMessage(`""`)
	}

	input, err := normalizeCodexInput(fields["input"])
	if err != nil {
		return err
	}
	fields["input"] = input

	if tools, err := normalizeCodexTools(fields["tools"]); err != nil {
		return err
	} else if tools != nil {
		fields["tools"] = tools
	}
	if choice, ok := normalizeCodexToolChoice(fields["tool_choice"]); ok {
		fields["tool_choice"] = choice
	}

	// Non-lite Codex requests require parallel_tool_calls=true whenever tools are
	// present; the caller's value is not accepted. Lite models reject the true
	// value, so the field is forced false. Without tools the member is omitted,
	// except on lite where the backend still expects the explicit false.
	if lite {
		fields["parallel_tool_calls"] = json.RawMessage(`false`)
	} else if !emptyJSONArray(fields["tools"]) {
		fields["parallel_tool_calls"] = json.RawMessage(`true`)
	} else {
		delete(fields, "parallel_tool_calls")
	}

	if isNullJSON(fields["prompt_cache_key"]) && sessionID != "" {
		fields["prompt_cache_key"] = mustJSON(sessionID)
	}
	return nil
}

// normalizeServiceTier keeps only the tiers the ChatGPT Codex backend accepts.
// "fast" is the client spelling of "priority" and is rewritten; anything else
// is removed rather than forwarded.
func normalizeServiceTier(fields map[string]json.RawMessage) {
	value, ok := rawJSONString(fields["service_tier"])
	if !ok {
		delete(fields, "service_tier")
		return
	}
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "priority", "fast":
		fields["service_tier"] = json.RawMessage(`"priority"`)
	case "ultrafast":
		fields["service_tier"] = json.RawMessage(`"ultrafast"`)
	default:
		delete(fields, "service_tier")
	}
}

func normalizeCodexInput(raw json.RawMessage) (json.RawMessage, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) > 0 && trimmed[0] == '"' {
		var text string
		if err := json.Unmarshal(trimmed, &text); err != nil {
			return nil, err
		}
		encoded, err := json.Marshal([]any{map[string]any{
			"type": "message", "role": "user",
			"content": []any{map[string]any{"type": "input_text", "text": text}},
		}})
		if err != nil {
			return nil, err
		}
		trimmed = encoded
	}
	var items []json.RawMessage
	if err := json.Unmarshal(trimmed, &items); err != nil {
		return nil, err
	}
	normalized := make([]json.RawMessage, 0, len(items))
	for _, item := range items {
		next, keep, err := normalizeCodexInputItem(item)
		if err != nil {
			return nil, err
		}
		if keep {
			normalized = append(normalized, next)
		}
	}
	return sanitizeCodexInputItemIDs(normalized)
}

func normalizeCodexInputItem(raw json.RawMessage) (json.RawMessage, bool, error) {
	var item map[string]json.RawMessage
	if err := json.Unmarshal(raw, &item); err != nil {
		return nil, false, err
	}
	changed := false
	set := func(key string, value json.RawMessage) {
		if !bytes.Equal(bytes.TrimSpace(item[key]), bytes.TrimSpace(value)) {
			item[key] = value
			changed = true
		}
	}
	itemType := strings.TrimSpace(rawMapString(item, "type"))
	role := strings.ToLower(strings.TrimSpace(rawMapString(item, "role")))
	if (itemType == "" || itemType == "message") && role == "system" {
		set("role", json.RawMessage(`"developer"`))
	}
	delete(item, "prompt_cache_breakpoint")
	if _, had := item["prompt_cache_breakpoint"]; had {
		changed = true
	}
	for _, key := range []string{"content", "output"} {
		if jsonKindOf(item[key]) != '[' {
			continue
		}
		next, itemChanged, err := stripPromptCacheBreakpoints(item[key])
		if err != nil {
			return nil, false, err
		}
		if itemChanged {
			set(key, next)
		}
	}
	if itemType == "reasoning" {
		next, itemChanged := normalizeReasoningItem(item)
		if !next {
			return nil, false, nil
		}
		changed = changed || itemChanged
	}
	if !changed {
		return raw, true, nil
	}
	encoded, err := json.Marshal(item)
	return encoded, true, err
}

// normalizeReasoningItem clears reasoning.content (Codex schema maxItems 0,
// promoting reasoning_text into an empty summary first) and drops an id that
// has no encrypted_content. store is forced false, so such an id is a lookup
// the backend cannot satisfy.
func normalizeReasoningItem(item map[string]json.RawMessage) (keep bool, changed bool) {
	if content := bytes.TrimSpace(item["content"]); len(content) > 0 && content[0] == '[' && string(content) != "[]" {
		if summaryEmpty(item["summary"]) {
			if promoted := promoteReasoningSummary(item["content"]); promoted != nil {
				item["summary"] = promoted
			}
		}
		item["content"] = json.RawMessage(`[]`)
		changed = true
	}
	encrypted, hasEncrypted := rawJSONString(item["encrypted_content"])
	if _, hasID := item["id"]; hasID && (!hasEncrypted || strings.TrimSpace(encrypted) == "") {
		delete(item, "id")
		changed = true
	}
	return true, changed
}

func summaryEmpty(raw json.RawMessage) bool {
	trimmed := bytes.TrimSpace(raw)
	return len(trimmed) == 0 || string(trimmed) == "null" || string(trimmed) == "[]"
}

func promoteReasoningSummary(content json.RawMessage) json.RawMessage {
	var parts []map[string]json.RawMessage
	if json.Unmarshal(content, &parts) != nil {
		return nil
	}
	summary := make([]any, 0, len(parts))
	for _, part := range parts {
		if rawMapString(part, "type") != "reasoning_text" {
			continue
		}
		text, ok := rawJSONString(part["text"])
		if !ok || text == "" {
			continue
		}
		summary = append(summary, map[string]any{"type": "summary_text", "text": text})
	}
	if len(summary) == 0 {
		return nil
	}
	encoded, err := json.Marshal(summary)
	if err != nil {
		return nil
	}
	return encoded
}

func stripPromptCacheBreakpoints(raw json.RawMessage) (json.RawMessage, bool, error) {
	var parts []json.RawMessage
	if err := json.Unmarshal(raw, &parts); err != nil {
		return nil, false, err
	}
	changed := false
	for index, part := range parts {
		if jsonKindOf(part) != '{' || !bytes.Contains(part, []byte(`"prompt_cache_breakpoint"`)) {
			continue
		}
		var members map[string]json.RawMessage
		if json.Unmarshal(part, &members) != nil {
			continue
		}
		if _, ok := members["prompt_cache_breakpoint"]; !ok {
			continue
		}
		delete(members, "prompt_cache_breakpoint")
		encoded, err := json.Marshal(members)
		if err != nil {
			return nil, false, err
		}
		parts[index] = encoded
		changed = true
	}
	if !changed {
		return raw, false, nil
	}
	encoded, err := json.Marshal(parts)
	return encoded, true, err
}

func normalizeCodexTools(raw json.RawMessage) (json.RawMessage, error) {
	if jsonKindOf(raw) != '[' {
		return nil, nil
	}
	var tools []json.RawMessage
	if err := json.Unmarshal(raw, &tools); err != nil {
		return nil, err
	}
	changed := false
	for index, tool := range tools {
		next, toolChanged, err := normalizeCodexTool(tool)
		if err != nil {
			return nil, err
		}
		if toolChanged {
			tools[index] = next
			changed = true
		}
	}
	if !changed {
		return raw, nil
	}
	return json.Marshal(tools)
}

func normalizeCodexTool(raw json.RawMessage) (json.RawMessage, bool, error) {
	var tool map[string]json.RawMessage
	if err := json.Unmarshal(raw, &tool); err != nil {
		return nil, false, err
	}
	changed := false
	toolType := rawMapString(tool, "type")
	if normalized := normalizeBuiltinToolType(toolType); normalized != "" {
		tool["type"] = mustJSON(normalized)
		toolType = normalized
		changed = true
	}
	switch toolType {
	case "namespace":
		if nested, err := normalizeCodexTools(tool["tools"]); err != nil {
			return nil, false, err
		} else if nested != nil && !bytes.Equal(bytes.TrimSpace(nested), bytes.TrimSpace(tool["tools"])) {
			tool["tools"] = nested
			changed = true
		}
	case "function", "custom":
		if params, paramChanged := normalizeToolParameters(tool["parameters"]); paramChanged {
			tool["parameters"] = params
			changed = true
		}
	}
	if !changed {
		return raw, false, nil
	}
	encoded, err := json.Marshal(tool)
	return encoded, true, err
}

func normalizeCodexToolChoice(raw json.RawMessage) (json.RawMessage, bool) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || string(trimmed) == "null" {
		return nil, false
	}
	if trimmed[0] == '"' {
		value, ok := rawJSONString(trimmed)
		if !ok {
			return nil, false
		}
		if normalized := normalizeBuiltinToolType(value); normalized != "" {
			return mustJSON(normalized), true
		}
		return nil, false
	}
	var choice map[string]json.RawMessage
	if json.Unmarshal(trimmed, &choice) != nil {
		return nil, false
	}
	changed := false
	toolType := rawMapString(choice, "type")
	if normalized := normalizeBuiltinToolType(toolType); normalized != "" {
		choice["type"] = mustJSON(normalized)
		changed = true
	}
	if nested, err := normalizeCodexTools(choice["tools"]); err == nil && nested != nil && !bytes.Equal(bytes.TrimSpace(nested), bytes.TrimSpace(choice["tools"])) {
		choice["tools"] = nested
		changed = true
	}
	if !changed {
		return nil, false
	}
	encoded, err := json.Marshal(choice)
	if err != nil {
		return nil, false
	}
	return encoded, true
}

func normalizeBuiltinToolType(toolType string) string {
	switch toolType {
	case "web_search_preview", "web_search_preview_2025_03_11":
		return "web_search"
	default:
		return ""
	}
}

// ---------------------------------------------------------------------------
// Input item ids. Codex rejects an id over 64 runes and requires the item
// type prefix (msg_, rs_, fc_, ctc_, ctco_). An encrypted reasoning item whose
// id cannot be kept is dropped: shortening it would break the replay.
// ---------------------------------------------------------------------------

func sanitizeCodexInputItemIDs(items []json.RawMessage) (json.RawMessage, error) {
	type parsed struct {
		raw     json.RawMessage
		members map[string]json.RawMessage
		drop    bool
	}
	parsedItems := make([]parsed, len(items))
	idStates := map[string]uint8{}
	for index, raw := range items {
		var members map[string]json.RawMessage
		if json.Unmarshal(raw, &members) != nil {
			parsedItems[index] = parsed{raw: raw}
			continue
		}
		entry := parsed{raw: raw, members: members}
		if shouldDropEncryptedReasoning(members) {
			entry.drop = true
			parsedItems[index] = entry
			continue
		}
		original, ok := rawJSONString(members["id"])
		if ok {
			id := normalizeCodexInputItemID(rawMapString(members, "type"), original)
			state := idStates[id]
			if id == original {
				state |= codexInputItemIDPreserved
			}
			if utf8.RuneCountInString(id) <= codexInputItemIDLimit {
				state |= codexInputItemIDOccupied
			}
			if state != 0 {
				idStates[id] = state
			}
		}
		parsedItems[index] = entry
	}

	mapped := map[string]string{}
	collisionMapped := map[string]string{}
	changed := false
	kept := make([]json.RawMessage, 0, len(parsedItems))
	for _, entry := range parsedItems {
		if entry.drop {
			changed = true
			continue
		}
		if entry.members == nil {
			kept = append(kept, entry.raw)
			continue
		}
		original, ok := rawJSONString(entry.members["id"])
		if !ok {
			kept = append(kept, entry.raw)
			continue
		}
		id := normalizeCodexInputItemID(rawMapString(entry.members, "type"), original)
		if id != original && idStates[id]&codexInputItemIDPreserved != 0 {
			collisionID, found := collisionMapped[id]
			if !found {
				for attempt := 0; ; attempt++ {
					collisionID = codexInputItemIDWithHashSuffix(id, attempt)
					if idStates[collisionID]&codexInputItemIDOccupied != 0 {
						continue
					}
					collisionMapped[id] = collisionID
					idStates[collisionID] |= codexInputItemIDOccupied
					break
				}
			}
			id = collisionID
		}
		if utf8.RuneCountInString(id) > codexInputItemIDLimit {
			shortened, found := mapped[id]
			if !found {
				shortened = shortenCodexInputItemID(id, 0)
				for attempt := 1; idStates[shortened]&codexInputItemIDOccupied != 0; attempt++ {
					shortened = shortenCodexInputItemID(id, attempt)
				}
				mapped[id] = shortened
				idStates[shortened] |= codexInputItemIDOccupied
			}
			id = shortened
		}
		if id == original {
			kept = append(kept, entry.raw)
			continue
		}
		entry.members["id"] = mustJSON(id)
		encoded, err := json.Marshal(entry.members)
		if err != nil {
			return nil, err
		}
		kept = append(kept, encoded)
		changed = true
	}
	if !changed {
		return json.Marshal(items)
	}
	return json.Marshal(kept)
}

func shouldDropEncryptedReasoning(item map[string]json.RawMessage) bool {
	if rawMapString(item, "type") != "reasoning" {
		return false
	}
	id, ok := rawJSONString(item["id"])
	if !ok || utf8.RuneCountInString(id) <= codexInputItemIDLimit {
		return false
	}
	encrypted, has := rawJSONString(item["encrypted_content"])
	return has && strings.TrimSpace(encrypted) != ""
}

func normalizeCodexInputItemID(itemType, id string) string {
	var prefix string
	switch itemType {
	case "message":
		prefix = codexMessageItemIDPrefix
	case "reasoning":
		prefix = codexReasoningItemIDPrefix
	case "function_call":
		prefix = codexFunctionCallItemIDPrefix
	case "custom_tool_call":
		prefix = codexCustomToolCallItemIDPrefix
	case "custom_tool_call_output":
		prefix = codexCustomToolCallOutputItemIDPrefix
	default:
		return id
	}
	if id == "" || strings.HasPrefix(id, prefix) {
		return id
	}
	return prefix + "_" + id
}

func shortenCodexInputItemID(id string, attempt int) string {
	return codexInputItemIDWithHashSuffix(id, attempt)
}

func codexInputItemIDWithHashSuffix(id string, attempt int) string {
	hashInput := id
	if attempt > 0 {
		hashInput += "\x00" + strconv.Itoa(attempt)
	}
	sum := sha256.Sum256([]byte(hashInput))
	suffix := "_" + hex.EncodeToString(sum[:8])
	runes := []rune(id)
	prefixLength := codexInputItemIDLimit - len(suffix)
	if len(runes) < prefixLength {
		prefixLength = len(runes)
	}
	if prefixLength < 0 {
		prefixLength = 0
	}
	return string(runes[:prefixLength]) + suffix
}

// ---------------------------------------------------------------------------
// Tool schemas. Only two Codex-rejected shapes change: a pattern using \p{},
// \P{}, or \0, and a large pure-const oneOf/anyOf that is already an enum.
// ---------------------------------------------------------------------------

func normalizeToolParameters(raw json.RawMessage) (json.RawMessage, bool) {
	if jsonKindOf(raw) != '{' {
		return nil, false
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var schema any
	if decoder.Decode(&schema) != nil {
		return nil, false
	}
	changed := stripIncompatiblePatterns(schema)
	if normalizeConstUnion(schema) {
		changed = true
	}
	if !changed {
		return nil, false
	}
	encoded, err := json.Marshal(schema)
	if err != nil {
		return nil, false
	}
	return encoded, true
}

func stripIncompatiblePatterns(value any) bool {
	changed := false
	switch schema := value.(type) {
	case map[string]any:
		if pattern, ok := schema["pattern"].(string); ok && hasUnsupportedPatternEscape(pattern) {
			delete(schema, "pattern")
			changed = true
		}
		if patternProps, ok := schema["patternProperties"].(map[string]any); ok {
			for key, sub := range patternProps {
				if hasUnsupportedPatternEscape(key) {
					delete(patternProps, key)
					changed = true
					continue
				}
				if stripIncompatiblePatterns(sub) {
					changed = true
				}
			}
		}
		for _, key := range []string{"properties", "$defs", "definitions", "dependentSchemas", "dependencies"} {
			subMap, ok := schema[key].(map[string]any)
			if !ok {
				continue
			}
			for _, sub := range subMap {
				if stripIncompatiblePatterns(sub) {
					changed = true
				}
			}
		}
		for _, key := range []string{"items", "prefixItems", "contains", "additionalProperties", "propertyNames", "unevaluatedProperties", "unevaluatedItems", "additionalItems", "contentSchema", "anyOf", "oneOf", "allOf", "not", "if", "then", "else"} {
			switch sub := schema[key].(type) {
			case map[string]any:
				if stripIncompatiblePatterns(sub) {
					changed = true
				}
			case []any:
				for _, item := range sub {
					if stripIncompatiblePatterns(item) {
						changed = true
					}
				}
			}
		}
	case []any:
		for _, item := range schema {
			if stripIncompatiblePatterns(item) {
				changed = true
			}
		}
	}
	return changed
}

func hasUnsupportedPatternEscape(pattern string) bool {
	for i := 0; i < len(pattern); i++ {
		if pattern[i] != '\\' {
			continue
		}
		if i+1 >= len(pattern) {
			break
		}
		next := pattern[i+1]
		if (next == 'p' || next == 'P') && i+2 < len(pattern) && pattern[i+2] == '{' {
			return true
		}
		if next == '0' {
			return true
		}
		i++
	}
	return false
}

func normalizeConstUnion(value any) bool {
	schema, ok := value.(map[string]any)
	if !ok {
		return false
	}
	changed := false
	for _, key := range []string{"properties", "$defs", "definitions"} {
		subMap, ok := schema[key].(map[string]any)
		if !ok {
			continue
		}
		for name, sub := range subMap {
			if rewriteConstUnion(sub) {
				subMap[name] = sub
				changed = true
			}
		}
	}
	return changed
}

func rewriteConstUnion(value any) bool {
	schema, ok := value.(map[string]any)
	if !ok {
		return false
	}
	_, hasOne := schema["oneOf"]
	_, hasAny := schema["anyOf"]
	if hasOne == hasAny {
		return false
	}
	name := "oneOf"
	if hasAny {
		name = "anyOf"
	}
	branches, ok := schema[name].([]any)
	if !ok || len(branches) < codexComplexUnionBranchThreshold {
		return false
	}
	consts := make([]any, 0, len(branches))
	seen := map[string]struct{}{}
	for _, branch := range branches {
		object, ok := branch.(map[string]any)
		if !ok {
			return false
		}
		constant, ok := object["const"]
		if !ok {
			return false
		}
		for key := range object {
			if key != "const" && key != "description" && key != "title" {
				return false
			}
		}
		key, ok := canonicalValueKey(constant)
		if !ok {
			return false
		}
		if _, dup := seen[key]; dup {
			return false
		}
		seen[key] = struct{}{}
		consts = append(consts, constant)
	}
	if existing, ok := schema["enum"].([]any); ok {
		if len(existing) != len(consts) {
			return false
		}
		existingKeys := map[string]struct{}{}
		for _, value := range existing {
			key, ok := canonicalValueKey(value)
			if !ok {
				return false
			}
			existingKeys[key] = struct{}{}
		}
		if len(existingKeys) != len(seen) {
			return false
		}
		for key := range seen {
			if _, ok := existingKeys[key]; !ok {
				return false
			}
		}
		delete(schema, name)
		return true
	}
	schema["enum"] = consts
	delete(schema, name)
	return true
}

func canonicalValueKey(value any) (string, bool) {
	switch typed := value.(type) {
	case string:
		return "s:" + typed, true
	case json.Number:
		var rat big.Rat
		if _, ok := rat.SetString(typed.String()); ok {
			return "n:" + rat.RatString(), true
		}
		return "n:" + typed.String(), true
	case bool:
		if typed {
			return "b:true", true
		}
		return "b:false", true
	case nil:
		return "null", true
	default:
		return "", false
	}
}

func jsonKindOf(raw json.RawMessage) byte {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || string(trimmed) == "null" {
		return 0
	}
	return trimmed[0]
}
