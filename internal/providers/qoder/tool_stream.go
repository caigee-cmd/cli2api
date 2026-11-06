package qoder

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// recoverToolTagStream rewrites a Cosy SSE body so fake-XML `<tool_call>`
// content echoes become structured tool_calls before writeOpenAIChunks sees
// them (hub e6e7622). Undeclared tools pass through unchanged.
func recoverToolTagStream(src io.Reader, names ...map[string]struct{}) io.Reader {
	var allowed map[string]struct{}
	if len(names) > 0 {
		allowed = names[0]
	}
	if len(allowed) == 0 {
		return src
	}
	pr, pw := io.Pipe()
	go func() {
		err := rewriteToolTagFrames(pw, src, allowed)
		_ = pw.CloseWithError(err)
	}()
	return pr
}

func rewriteToolTagFrames(w io.Writer, src io.Reader, allowed map[string]struct{}) error {
	var hold strings.Builder
	holding := false
	var holdTail string
	finished := false

	emitBody := func(body map[string]any) error {
		payload, err := json.Marshal(map[string]any{"body": body})
		if err != nil {
			return err
		}
		_, err = fmt.Fprintf(w, "data: %s\n\n", payload)
		return err
	}

	flushHold := func(finish string) error {
		buf := hold.String() + holdTail
		hold.Reset()
		holdTail = ""
		holding = false
		if buf == "" {
			return nil
		}
		if clean, tools, ok := recoverToolTagsFromContent(buf, nil, allowed); ok {
			if clean != "" {
				if err := emitBody(map[string]any{
					"choices": []any{map[string]any{"index": 0, "delta": map[string]any{"content": clean}, "finish_reason": nil}},
				}); err != nil {
					return err
				}
			}
			var decoded any
			if json.Unmarshal(tools, &decoded) == nil {
				if err := emitBody(map[string]any{
					"choices": []any{map[string]any{"index": 0, "delta": map[string]any{"tool_calls": decoded}, "finish_reason": nil}},
				}); err != nil {
					return err
				}
			}
			if finish == "" || finish == "stop" {
				finish = "tool_calls"
			}
			if finish != "" {
				finished = true
				return emitBody(map[string]any{
					"choices": []any{map[string]any{"index": 0, "delta": map[string]any{}, "finish_reason": finish}},
				})
			}
			return nil
		}
		return emitBody(map[string]any{
			"choices": []any{map[string]any{"index": 0, "delta": map[string]any{"content": buf}, "finish_reason": emptyOrNil(finish)}},
		})
	}

	err := walkUpstream(src, func(body map[string]any) error {
		frame := frameFromBody(body)
		// Pass errors / usage / structured tools / reasoning through; only
		// content is subject to tool-tag hold.
		if frame.errText != "" && frame.content == "" && frame.reasoning == "" && len(frame.tools) == 0 {
			if holding {
				if err := flushHold(""); err != nil {
					return err
				}
			}
			return emitBody(body)
		}
		if len(frame.tools) > 0 {
			if holding {
				if err := flushHold(""); err != nil {
					return err
				}
			}
			frame.tools = repairToolCallArgumentsJSON(frame.tools)
			var decoded any
			_ = json.Unmarshal(frame.tools, &decoded)
			delta := map[string]any{}
			if frame.reasoning != "" {
				delta["reasoning_content"] = frame.reasoning
			}
			if decoded != nil {
				delta["tool_calls"] = decoded
			}
			out := map[string]any{
				"choices": []any{map[string]any{"index": 0, "delta": delta, "finish_reason": emptyOrNil(frame.finish)}},
			}
			if len(frame.usage) > 0 {
				var usage any
				if json.Unmarshal(frame.usage, &usage) == nil {
					out["usage"] = usage
				}
			}
			if frame.finish != "" {
				finished = true
			}
			return emitBody(out)
		}

		piece := holdTail + frame.content
		holdTail = ""

		if !holding {
			tail := toolTagTailHold(piece)
			bodyText := piece
			if tail != "" {
				bodyText = piece[:len(piece)-len(tail)]
			}
			candidate := bodyText + tail
			if strings.Contains(strings.ToLower(candidate), toolCallOpen) || (tail != "" && strings.Contains(candidate, "<")) {
				if toolTagCandidate(candidate) || strings.Contains(strings.ToLower(candidate), toolCallOpen) {
					holding = true
					hold.WriteString(bodyText)
					holdTail = tail
					if frame.reasoning != "" {
						if err := emitBody(map[string]any{
							"choices": []any{map[string]any{"index": 0, "delta": map[string]any{"reasoning_content": frame.reasoning}, "finish_reason": nil}},
						}); err != nil {
							return err
						}
					}
					if frame.finish != "" {
						return flushHold(frame.finish)
					}
					if len(frame.usage) > 0 {
						var usage any
						if json.Unmarshal(frame.usage, &usage) == nil {
							_ = emitBody(map[string]any{"usage": usage, "choices": []any{}})
						}
					}
					return nil
				}
			}
			holdTail = tail
			out := map[string]any{}
			delta := map[string]any{}
			if bodyText != "" {
				delta["content"] = bodyText
			}
			if frame.reasoning != "" {
				delta["reasoning_content"] = frame.reasoning
			}
			out["choices"] = []any{map[string]any{"index": 0, "delta": delta, "finish_reason": emptyOrNil(frame.finish)}}
			if len(frame.usage) > 0 {
				var usage any
				if json.Unmarshal(frame.usage, &usage) == nil {
					out["usage"] = usage
				}
			}
			if frame.finish != "" {
				finished = true
			}
			if bodyText == "" && frame.reasoning == "" && frame.finish == "" && frame.usage == nil {
				return nil
			}
			return emitBody(out)
		}

		hold.WriteString(piece)
		if frame.reasoning != "" {
			if err := emitBody(map[string]any{
				"choices": []any{map[string]any{"index": 0, "delta": map[string]any{"reasoning_content": frame.reasoning}, "finish_reason": nil}},
			}); err != nil {
				return err
			}
		}
		if frame.finish != "" {
			return flushHold(frame.finish)
		}
		if !toolTagCandidate(hold.String()) {
			return flushHold("")
		}
		return nil
	})
	if err != nil {
		return err
	}
	if holding {
		if err := flushHold(""); err != nil {
			return err
		}
	}
	if !finished {
		// writeOpenAIChunks salvages missing finish; nothing else to emit.
	}
return nil
}
