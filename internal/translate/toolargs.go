package translate

import (
	"encoding/json"
	"strings"
)

const maxCloseClosers = 24

// RepairToolCallArguments turns the argument text a model streamed for a
// function tool into valid JSON when the damage is formatting-level: bare
// quotes inside a value, a trailing comma, or missing closing brackets after a
// complete value. It never pads a value that was cut mid-string, because
// repairing `{"command":"rm -rf /tmp/fo` into a legal string would run a
// command the model never finished writing; that case stays a hard error so
// the caller reports a truncated tool call instead of executing it.
func RepairToolCallArguments(raw string) (string, bool) {
	text := strings.TrimSpace(raw)
	if text == "" {
		return "{}", true
	}
	if json.Valid([]byte(text)) {
		return text, true
	}
	candidates := argumentRepairCandidates(text)
	for _, candidate := range candidates {
		if json.Valid([]byte(candidate)) {
			return candidate, true
		}
	}
	for _, candidate := range candidates {
		if closed, ok := closeArgumentTail(candidate); ok {
			return closed, true
		}
	}
	// A model sometimes appends prose after a complete object: keep the value
	// the decoder actually finished and drop the trailing noise.
	if prefix, ok := decodeArgumentPrefix(text); ok {
		return prefix, true
	}
	return "", false
}

func argumentRepairCandidates(text string) []string {
	candidates := []string{text}
	if withoutTrailing := stripTrailingCommas(text); withoutTrailing != text {
		candidates = append(candidates, withoutTrailing)
	}
	if escaped := escapeBareQuotes(text); escaped != text {
		candidates = append(candidates, escaped, stripTrailingCommas(escaped))
	}
	return candidates
}

// stripTrailingCommas removes `,` that directly precede a closing brace or
// bracket, which models emit routinely and strict JSON rejects.
func stripTrailingCommas(text string) string {
	var out strings.Builder
	out.Grow(len(text))
	for index := 0; index < len(text); index++ {
		character := text[index]
		if character == ',' {
			next := index + 1
			for next < len(text) && isJSONSpace(text[next]) {
				next++
			}
			if next < len(text) && (text[next] == '}' || text[next] == ']') {
				continue
			}
		}
		out.WriteByte(character)
	}
	return out.String()
}

// escapeBareQuotes escapes `"` characters that appear inside a JSON string
// value. A closing quote is accepted only when the next non-space byte is one
// of `, } ] :` or the end of the buffer.
func escapeBareQuotes(text string) string {
	var out strings.Builder
	out.Grow(len(text))
	inString := false
	for index := 0; index < len(text); {
		character := text[index]
		if !inString {
			out.WriteByte(character)
			if character == '"' {
				inString = true
			}
			index++
			continue
		}
		if character == '\\' && index+1 < len(text) {
			out.WriteString(text[index : index+2])
			index += 2
			continue
		}
		if character == '"' {
			next := index + 1
			for next < len(text) && isJSONSpace(text[next]) {
				next++
			}
			if next >= len(text) || strings.IndexByte(",}]:", text[next]) >= 0 {
				out.WriteByte(character)
				inString = false
			} else {
				out.WriteString(`\"`)
			}
			index++
			continue
		}
		out.WriteByte(character)
		index++
	}
	return out.String()
}

// closeArgumentTail appends the closers the model never got to emit, but only
// when no string is left open — a value cut mid-string stays unrepaired.
func closeArgumentTail(text string) (string, bool) {
	stack := make([]byte, 0, maxCloseClosers)
	var out strings.Builder
	out.Grow(len(text) + maxCloseClosers)
	inString := false
	for index := 0; index < len(text); index++ {
		character := text[index]
		if inString {
			out.WriteByte(character)
			if character == '\\' && index+1 < len(text) {
				out.WriteByte(text[index+1])
				index++
				continue
			}
			if character == '"' {
				inString = false
			}
			continue
		}
		switch character {
		case '"':
			inString = true
			out.WriteByte(character)
		case '{', '[':
			if len(stack) >= maxCloseClosers {
				return "", false
			}
			stack = append(stack, character)
			out.WriteByte(character)
		case '}', ']':
			if len(stack) == 0 {
				return "", false
			}
			open := stack[len(stack)-1]
			if (character == '}' && open != '{') || (character == ']' && open != '[') {
				return "", false
			}
			stack = stack[:len(stack)-1]
			out.WriteByte(character)
		default:
			out.WriteByte(character)
		}
	}
	if inString || len(stack) == 0 {
		return "", false
	}
	for index := len(stack) - 1; index >= 0; index-- {
		if stack[index] == '{' {
			out.WriteString("}")
		} else {
			out.WriteString("]")
		}
	}
	closed := stripTrailingCommas(out.String())
	if json.Valid([]byte(closed)) {
		return closed, true
	}
	return "", false
}

func decodeArgumentPrefix(text string) (string, bool) {
	decoder := json.NewDecoder(strings.NewReader(text))
	var value any
	if decoder.Decode(&value) != nil {
		return "", false
	}
	prefix := strings.TrimSpace(text[:decoder.InputOffset()])
	if prefix == "" || !json.Valid([]byte(prefix)) {
		return "", false
	}
	return prefix, true
}

func isJSONSpace(character byte) bool {
	return character == ' ' || character == '\t' || character == '\r' || character == '\n'
}
