package translate

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

const defaultToolParameters = `{"type":"object","properties":{}}`

// ResponseToolName is request-local metadata; it must never reach the provider.
type ResponseToolName struct {
	Namespace string
	Name      string
}

// responseToolNames records actual declarations, rather than guessing identity
// by splitting names (both namespaces and tool names can contain underscores).
func responseToolNames(raw json.RawMessage) (map[string]ResponseToolName, error) {
	if emptyJSON(raw) {
		return nil, nil
	}
	var items []json.RawMessage
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, err
	}
	all := map[string]ResponseToolName{}
	names := map[string]ResponseToolName{}
	register := func(flat string, identity ResponseToolName) error {
		if flat == "" {
			return nil
		}
		if previous, exists := all[flat]; exists && previous != identity {
			return fmt.Errorf("tool name collision for %q", flat)
		}
		all[flat] = identity
		return nil
	}
	for _, rawItem := range items {
		var item map[string]json.RawMessage
		if json.Unmarshal(rawItem, &item) != nil {
			continue
		}
		typ := strings.ToLower(strings.TrimSpace(rawMapString(item, "type")))
		switch typ {
		case "namespace":
			for _, tool := range expandNamespaceToolItems(rawItem, strings.TrimSpace(rawMapString(item, "name"))) {
				if err := register(tool.name, tool.identity); err != nil {
					return nil, err
				}
				if tool.identity.Namespace != "" {
					names[tool.name] = tool.identity
				}
			}
		case "function", "":
			name, _, _ := toolFields(item)
			flat := name
			if err := register(flat, ResponseToolName{Name: name}); err != nil {
				return nil, err
			}
		}
	}
	return names, nil
}

// ResponseToolNames records namespace tool identities declared on a Responses
// body. The native path keeps those tools intact upstream, but the reply still
// has to restore a flattened name when one was produced. A body the
// compatibility translator cannot carry returns no names rather than an error.
func ResponseToolNames(raw []byte) map[string]ResponseToolName {
	var body struct {
		Tools json.RawMessage `json:"tools"`
	}
	if json.Unmarshal(raw, &body) != nil {
		return nil
	}
	names, err := responseToolNames(body.Tools)
	if err != nil {
		return nil
	}
	return names
}

const (
	customToolMarker     = "__codex_custom__"
	customToolParameters = `{"type":"object","properties":{"input":{"type":"string","description":"Raw freeform input for the custom tool."}},"required":["input"],"additionalProperties":false}`
)

// EncodeCustomToolName maps a Codex custom/freeform tool onto an upstream
// function name. The marker keeps the round trip reversible without changing
// the namespace/name identity carried by CustomToolCall.
func EncodeCustomToolName(namespace, name string) string {
	return customToolName(namespace, name)
}

// DecodeCustomToolName reverses EncodeCustomToolName. It returns ok=false for
// ordinary function tools.
func DecodeCustomToolName(upstreamName string) (namespace, name string, ok bool) {
	upstreamName = strings.TrimSpace(upstreamName)
	index := strings.Index(upstreamName, customToolMarker)
	if index < 0 {
		return "", "", false
	}
	name = strings.TrimSpace(upstreamName[index+len(customToolMarker):])
	if name == "" {
		return "", "", false
	}
	return strings.TrimSpace(upstreamName[:index]), name, true
}

func customToolName(namespace, name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	namespace = strings.TrimSpace(namespace)
	if namespace == "" {
		return customToolMarker + name
	}
	return strings.TrimRight(namespace, "_") + customToolMarker + name
}

func customToolDescription(description string, format json.RawMessage) string {
	description = strings.TrimSpace(description)
	if len(format) == 0 || string(format) == "null" {
		return description
	}
	formatted := strings.TrimSpace(string(format))
	if formatted == "" {
		return description
	}
	if description == "" {
		return "Custom tool input format:\n" + formatted
	}
	return description + "\n\nCustom tool input format:\n" + formatted
}

// NormalizeOpenAITools expands Codex/Desktop namespace wrappers into plain
// OpenAI function tools and drops hosted shells such as mcp / web_search.
// Nested tools may be Responses-flat or Chat Completions shaped. Short nested
// names are qualified as namespace__name; already-qualified mcp__* names stay
// unchanged. This does not apply Devin's mcp__ upstream aliasing.
func NormalizeOpenAITools(raw json.RawMessage) (json.RawMessage, error) {
	if emptyJSON(raw) {
		return nil, nil
	}
	var items []json.RawMessage
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, fmt.Errorf("tools must be an array")
	}
	out := make([]map[string]any, 0, len(items))
	seen := map[string]struct{}{}
	appendTool := func(name, description string, parameters json.RawMessage) {
		name = strings.TrimSpace(name)
		if name == "" {
			return
		}
		if _, ok := seen[name]; ok {
			return
		}
		seen[name] = struct{}{}
		if len(parameters) == 0 || string(parameters) == "null" {
			parameters = json.RawMessage(defaultToolParameters)
		}
		parameters = hardenToolSchema(parameters)
		out = append(out, map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        name,
				"description": description,
				"parameters":  json.RawMessage(parameters),
			},
		})
	}
	for _, item := range items {
		var probe map[string]json.RawMessage
		if err := json.Unmarshal(item, &probe); err != nil {
			continue
		}
		typ := strings.ToLower(strings.TrimSpace(rawMapString(probe, "type")))
		switch typ {
		case "namespace":
			for _, nested := range expandNamespaceToolItems(item, strings.TrimSpace(rawMapString(probe, "name"))) {
				appendTool(nested.name, nested.description, nested.parameters)
			}
		case "custom":
			name := customToolName("", rawMapString(probe, "name"))
			appendTool(name, customToolDescription(rawMapString(probe, "description"), rawMapJSON(probe, "format")), json.RawMessage(customToolParameters))
		case "mcp", "web_search", "web_search_preview":
			continue
		case "function", "":
			name, description, parameters := toolFields(probe)
			appendTool(name, description, parameters)
		default:
			continue
		}
	}
	if len(out) == 0 {
		return nil, nil
	}
	return json.Marshal(out)
}

type normalizedTool struct {
	name        string
	identity    ResponseToolName
	description string
	parameters  json.RawMessage
	custom      bool
}

func expandNamespaceToolItems(raw json.RawMessage, namespace string) []normalizedTool {
	var wrapper struct {
		Tools []json.RawMessage `json:"tools"`
	}
	if json.Unmarshal(raw, &wrapper) != nil || len(wrapper.Tools) == 0 {
		return nil
	}
	out := make([]normalizedTool, 0, len(wrapper.Tools))
	for _, item := range wrapper.Tools {
		var probe map[string]json.RawMessage
		if json.Unmarshal(item, &probe) != nil {
			continue
		}
		typ := strings.ToLower(strings.TrimSpace(rawMapString(probe, "type")))
		if typ == "custom" {
			name := customToolName(namespace, rawMapString(probe, "name"))
			if name == "" {
				continue
			}
			out = append(out, normalizedTool{
				name:        name,
				description: customToolDescription(rawMapString(probe, "description"), rawMapJSON(probe, "format")),
				parameters:  json.RawMessage(customToolParameters),
				custom:      true,
			})
			continue
		}
		if typ != "" && typ != "function" {
			continue
		}
		name, description, parameters := toolFields(probe)
		identity := ResponseToolName{Namespace: namespace, Name: name}
		name = qualifyNamespaceToolName(namespace, name)
		if name == "" {
			continue
		}
		out = append(out, normalizedTool{name: name, identity: identity, description: description, parameters: parameters})
	}
	return out
}

func toolFields(source map[string]json.RawMessage) (name, description string, parameters json.RawMessage) {
	var function map[string]json.RawMessage
	if raw, ok := rawMapJSONValue(source, "function"); ok {
		_ = json.Unmarshal(raw, &function)
	}
	name = firstNonEmptyTrimmed(rawMapString(function, "name"), rawMapString(source, "name"))
	description = firstNonEmptyTrimmed(rawMapString(function, "description"), rawMapString(source, "description"))
	parameters = rawMapJSON(function, "parameters")
	if len(parameters) == 0 {
		parameters = rawMapJSON(source, "parameters")
	}
	return name, description, parameters
}

func qualifyNamespaceToolName(namespace, name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	if strings.Contains(name, "__") || strings.HasPrefix(strings.ToLower(name), "mcp__") {
		return name
	}
	namespace = strings.TrimSpace(namespace)
	if namespace == "" {
		return name
	}
	return strings.TrimRight(namespace, "_") + "__" + strings.TrimLeft(name, "_")
}

// schemaNumericKeywords and schemaBooleanKeywords are the constraint fields
// WorkBuddy's Go validator rejects with code 11129 when a client sends them
// as strings ("minimum": "1", "uniqueItems": "true"). Compliant values pass
// through; unparseable numbers and nulls are dropped.
var (
	schemaNumericKeywords = map[string]struct{}{
		"minimum": {}, "maximum": {}, "exclusiveMinimum": {}, "exclusiveMaximum": {},
		"multipleOf": {}, "minLength": {}, "maxLength": {}, "minItems": {},
		"maxItems": {}, "minProperties": {}, "maxProperties": {},
	}
	schemaBooleanKeywords = map[string]struct{}{
		"uniqueItems": {}, "deprecated": {}, "readOnly": {}, "writeOnly": {},
	}
	schemaStringKeywords = map[string]struct{}{
		"description": {}, "title": {}, "format": {}, "pattern": {},
	}
)

// HardenToolSchema coerces those keywords recursively and is idempotent: a
// schema that is already valid is unchanged. A schema that is not an object
// is returned as-is. Providers that stringify parameters before this step
// still call it on the raw schema text.
func HardenToolSchema(raw json.RawMessage) json.RawMessage {
	return hardenToolSchema(raw)
}

func hardenToolSchema(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 || string(raw) == "null" {
		return raw
	}
	var node any
	if err := json.Unmarshal(raw, &node); err != nil {
		return raw
	}
	hardened, ok := hardenSchemaNode(node).(map[string]any)
	if !ok {
		return raw
	}
	out, err := json.Marshal(hardened)
	if err != nil {
		return raw
	}
	return out
}

func hardenSchemaNode(node any) any {
	obj, ok := node.(map[string]any)
	if !ok {
		return node
	}
	for key, value := range obj {
		switch key {
		case "properties", "$defs", "definitions", "patternProperties":
			if nested, isMap := value.(map[string]any); isMap {
				for name, child := range nested {
					nested[name] = hardenSchemaNode(child)
				}
				obj[key] = nested
			}
		case "anyOf", "oneOf", "allOf", "prefixItems":
			if list, isList := value.([]any); isList {
				for i, child := range list {
					list[i] = hardenSchemaNode(child)
				}
				obj[key] = list
			}
		default:
			next, keep := hardenSchemaKeyword(key, value)
			if !keep {
				delete(obj, key)
				continue
			}
			obj[key] = next
		}
	}
	return obj
}

// hardenSchemaKeyword returns keep=false when the keyword cannot be made
// valid and must be removed. default:null stays: dropping it changes meaning.
func hardenSchemaKeyword(key string, value any) (any, bool) {
	if _, numeric := schemaNumericKeywords[key]; numeric {
		if number, ok := coerceSchemaNumber(value); ok {
			return number, true
		}
		if _, isString := value.(string); isString || value == nil {
			return nil, false
		}
		return value, true
	}
	if key == "additionalProperties" {
		switch typed := value.(type) {
		case string:
			if boolean, ok := coerceSchemaBool(typed); ok {
				return boolean, true
			}
			return nil, false
		case map[string]any:
			return hardenSchemaNode(typed), true
		case nil:
			return nil, false
		default:
			return value, true
		}
	}
	if _, boolean := schemaBooleanKeywords[key]; boolean {
		if coerced, ok := coerceSchemaBool(value); ok {
			return coerced, true
		}
		if _, isString := value.(string); isString || value == nil {
			return nil, false
		}
		return value, true
	}
	if _, textual := schemaStringKeywords[key]; textual {
		text, ok := stringifySchemaValue(value)
		return text, ok
	}
	switch key {
	case "enum", "examples", "required":
		if _, isList := value.([]any); !isList {
			return nil, false
		}
		return value, true
	case "items", "additionalItems", "contains", "propertyNames",
		"unevaluatedItems", "unevaluatedProperties", "if", "then", "else", "not":
		if value == nil {
			return nil, false
		}
		if child, isMap := value.(map[string]any); isMap {
			return hardenSchemaNode(child), true
		}
		return value, true
	default:
		if value == nil && key != "default" {
			return nil, false
		}
		if child, isMap := value.(map[string]any); isMap {
			return hardenSchemaNode(child), true
		}
		return value, true
	}
}

func coerceSchemaNumber(value any) (any, bool) {
	switch typed := value.(type) {
	case float64:
		return typed, true
	case json.Number:
		return parseSchemaNumber(typed.String())
	case string:
		return parseSchemaNumber(strings.TrimSpace(typed))
	default:
		return nil, false
	}
}

func parseSchemaNumber(text string) (any, bool) {
	if text == "" {
		return nil, false
	}
	number, err := strconv.ParseFloat(text, 64)
	if err != nil {
		return nil, false
	}
	if number == float64(int64(number)) && !strings.ContainsAny(text, ".eE") {
		return int64(number), true
	}
	return number, true
}

func coerceSchemaBool(value any) (bool, bool) {
	switch typed := value.(type) {
	case bool:
		return typed, true
	case string:
		switch strings.ToLower(strings.TrimSpace(typed)) {
		case "true":
			return true, true
		case "false":
			return false, true
		}
	}
	return false, false
}

func stringifySchemaValue(value any) (any, bool) {
	switch typed := value.(type) {
	case nil:
		return nil, false
	case string:
		return typed, true
	case float64:
		if typed == float64(int64(typed)) {
			return strconv.FormatInt(int64(typed), 10), true
		}
		return strconv.FormatFloat(typed, 'f', -1, 64), true
	case bool:
		return strconv.FormatBool(typed), true
	default:
		encoded, err := json.Marshal(typed)
		if err != nil {
			return nil, false
		}
		return string(encoded), true
	}
}

func firstNonEmptyTrimmed(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
