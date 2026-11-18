package kiro

import (
	"encoding/json"
	"strings"
)

// Markup rescue follows kiro-reverse-api. Kiro sometimes streams a tool call as
// Claude's <function_calls><invoke> text instead of a toolUseEvent. The filter
// holds text only while a tag prefix is pending, turns a finished invoke for a
// declared tool into a call, and leaves everything else as text. A call is
// rescued only when the turn ends on it: a model quoting the format as an
// example keeps writing, and that text is passed through.

const maxMarkupNameLen = 200

type markupTags struct {
	callsOpen, callsClose   string
	invokeOpen, invokeClose string
	paramOpen, paramClose   string
}

func markupDialects() []markupTags {
	return []markupTags{markupTagsFor(""), markupTagsFor("antml:"), markupTagsFor("｜DSML｜")}
}

func markupTagsFor(ns string) markupTags {
	return markupTags{
		callsOpen: "<" + ns + "function_calls>", callsClose: "</" + ns + "function_calls>",
		invokeOpen: "<" + ns + `invoke name="`, invokeClose: "</" + ns + "invoke>",
		paramOpen: "<" + ns + `parameter name="`, paramClose: "</" + ns + "parameter>",
	}
}

type markupFilter struct {
	tools map[string]map[string]string
	buf   string
	run   []toolUse
	raw   strings.Builder
	held  []toolUse
	seenN map[string]int
}

func newMarkupFilter(names map[string]string) *markupFilter {
	return &markupFilter{tools: map[string]map[string]string{}, seenN: map[string]int{}}
}

// armMarkupFilter builds a filter from the tools this request declared. A
// request with no tools has nothing to rescue.
func armMarkupFilter(tools []toolSpec) *markupFilter {
	if len(tools) == 0 {
		return nil
	}
	filter := newMarkupFilter(nil)
	for _, tool := range tools {
		filter.tools[tool.Name] = schemaTypes(tool.Parameters)
		if tool.ClientName != "" {
			filter.tools[tool.ClientName] = filter.tools[tool.Name]
		}
	}
	return filter
}

func schemaTypes(schema any) map[string]string {
	obj, _ := schema.(map[string]any)
	props, _ := obj["properties"].(map[string]any)
	types := map[string]string{}
	for key, prop := range props {
		propObj, _ := prop.(map[string]any)
		if kind, ok := propObj["type"].(string); ok {
			types[key] = kind
		}
	}
	return types
}

func (f *markupFilter) push(chunk string) string {
	if f == nil || len(f.tools) == 0 {
		return chunk
	}
	f.buf += chunk
	return f.drain(false)
}

func (f *markupFilter) flush() string {
	if f == nil {
		return ""
	}
	text := f.drain(true)
	// A run still held when the stream ends is a call the model finished on.
	// Anything after it would already have released the run back to text.
	if len(f.run) > 0 && strings.TrimSpace(f.buf) == "" {
		f.held = append(f.held, f.run...)
		f.run, f.raw = nil, strings.Builder{}
	}
	return text
}

func (f *markupFilter) seen(tool toolUse) {
	if f == nil {
		return
	}
	f.seenN[toolSignature(tool)]++
}

// rescued returns calls parsed from text, minus copies of calls Kiro also sent
// as real tool events. A failed stream returns nothing: the turn did not finish.
func (f *markupFilter) rescued(ok bool) []toolUse {
	if f == nil || !ok {
		return nil
	}
	real := map[string]int{}
	for signature, n := range f.seenN {
		real[signature] = n
	}
	var out []toolUse
	for _, tool := range f.held {
		signature := toolSignature(tool)
		if real[signature] > 0 {
			real[signature]--
			continue
		}
		out = append(out, tool)
	}
	f.held = nil
	return out
}

func toolSignature(tool toolUse) string {
	encoded, _ := json.Marshal(tool.Input)
	return tool.Name + "\x00" + string(encoded)
}

func (f *markupFilter) drain(final bool) string {
	var out strings.Builder
	for f.buf != "" {
		if len(f.run) > 0 && strings.TrimLeft(f.buf, " \t\r\n") != "" && !f.continuesRun() {
			if !final && f.partialRun() {
				break
			}
			out.WriteString(f.raw.String())
			f.run, f.raw = nil, strings.Builder{}
		}
		if len(f.run) > 0 && strings.TrimSpace(f.buf) == "" {
			if !final {
				break
			}
			f.held = append(f.held, f.run...)
			f.run, f.raw, f.buf = nil, strings.Builder{}, ""
			break
		}
		start := strings.IndexByte(f.buf, '<')
		if start < 0 {
			out.WriteString(f.buf)
			f.buf = ""
			break
		}
		if n, tool, ok := f.parseAt(f.buf[start:]); ok {
			out.WriteString(f.buf[:start])
			end := start + n
			// The closing wrapper is markup too. Leaving it in the buffer makes
			// the next pass treat "</" as text and emit a broken tag.
			rest := strings.TrimLeft(f.buf[end:], " \t\r\n")
			for _, tags := range markupDialects() {
				if strings.HasPrefix(rest, tags.callsClose) {
					end = len(f.buf) - len(rest) + len(tags.callsClose)
					break
				}
			}
			f.raw.WriteString(f.buf[start:end])
			f.buf = f.buf[end:]
			f.run = append(f.run, tool)
			continue
		}
		if !final && f.partialOpenAt(f.buf[start:]) {
			out.WriteString(f.buf[:start])
			f.buf = f.buf[start:]
			break
		}
		out.WriteString(f.buf[:start+1])
		f.buf = f.buf[start+1:]
	}
	return out.String()
}

func (f *markupFilter) continuesRun() bool {
	rest := strings.TrimLeft(f.buf, " \t\r\n")
	for _, tags := range markupDialects() {
		if strings.HasPrefix(rest, tags.invokeOpen) || strings.HasPrefix(rest, tags.callsClose) || strings.HasPrefix(rest, tags.callsOpen) {
			return true
		}
	}
	return false
}

func (f *markupFilter) partialRun() bool {
	rest := strings.TrimLeft(f.buf, " \t\r\n")
	for _, tags := range markupDialects() {
		if partialTag(rest, tags.invokeOpen) || partialTag(rest, tags.callsClose) || partialTag(rest, tags.callsOpen) {
			return true
		}
	}
	return false
}

func (f *markupFilter) partialOpenAt(buf string) bool {
	for _, tags := range markupDialects() {
		if partialTag(buf, tags.invokeOpen) || partialTag(buf, tags.callsOpen) {
			return true
		}
	}
	return false
}

func (f *markupFilter) parseAt(buf string) (int, toolUse, bool) {
	for _, tags := range markupDialects() {
		body := buf
		offset := 0
		if strings.HasPrefix(buf, tags.callsOpen) {
			body = strings.TrimLeft(buf[len(tags.callsOpen):], " \t\r\n")
			offset = len(buf) - len(body)
		}
		if !strings.HasPrefix(body, tags.invokeOpen) {
			continue
		}
		n, tool, ok := parseInvoke(body, tags, f.tools)
		if ok {
			return offset + n, tool, true
		}
	}
	return 0, toolUse{}, false
}

func parseInvoke(s string, tags markupTags, tools map[string]map[string]string) (int, toolUse, bool) {
	name, pos, ok := quotedName(s, len(tags.invokeOpen))
	if !ok {
		return 0, toolUse{}, false
	}
	types, declared := tools[name]
	if !declared {
		return 0, toolUse{}, false
	}
	input := map[string]any{}
	for {
		rest := strings.TrimLeft(s[pos:], " \t\r\n")
		pos = len(s) - len(rest)
		switch {
		case strings.HasPrefix(rest, tags.invokeClose):
			return pos + len(tags.invokeClose), toolUse{Name: name, Input: input}, true
		case strings.HasPrefix(rest, tags.paramOpen):
			key, valueAt, ok := quotedName(rest, len(tags.paramOpen))
			if !ok {
				return 0, toolUse{}, false
			}
			end := strings.Index(rest[valueAt:], tags.paramClose)
			if end < 0 {
				return 0, toolUse{}, false
			}
			input[key] = coerceParam(rest[valueAt:valueAt+end], types[key])
			pos += valueAt + end + len(tags.paramClose)
		default:
			return 0, toolUse{}, false
		}
	}
}

func quotedName(s string, start int) (string, int, bool) {
	rest := s[start:]
	end := strings.Index(rest, `">`)
	if end <= 0 || end > maxMarkupNameLen || strings.ContainsAny(rest[:end], "<>\n") {
		return "", 0, false
	}
	return rest[:end], start + end + 2, true
}

func coerceParam(raw, schemaType string) any {
	if schemaType == "string" {
		return raw
	}
	var value any
	if json.Unmarshal([]byte(strings.TrimSpace(raw)), &value) == nil {
		if _, isString := value.(string); !isString {
			return value
		}
	}
	return raw
}

func partialTag(s, tag string) bool {
	return len(s) > 0 && len(s) < len(tag) && strings.HasPrefix(tag, s)
}
