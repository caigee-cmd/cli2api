package trae

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"
)

type soloEvent struct {
	Name string
	Data json.RawMessage
}

type streamError struct {
	Code    any    `json:"code"`
	Message string `json:"message"`
}

func (e streamError) Error() string {
	return fmt.Sprintf("solo error code=%v msg=%s", e.Code, e.Message)
}

func (e streamError) codeString() string {
	switch v := e.Code.(type) {
	case float64:
		return fmt.Sprintf("%.0f", v)
	case json.Number:
		return v.String()
	default:
		return strings.TrimSpace(fmt.Sprint(v))
	}
}

// Aggregate converts a Solo SSE stream into one OpenAI chat.completion object.
func Aggregate(reader io.Reader) (map[string]any, error) {
	result := map[string]any{"object": "chat.completion"}
	message := map[string]any{"role": "assistant"}
	toolCalls := map[int]map[string]any{}
	finishReason := "stop"
	var content, reasoning strings.Builder
	var order []int
	sawDone := false
	var usage any
	model := ""

	if err := scanSolo(reader, func(ev soloEvent) error {
		switch ev.Name {
		case "metadata":
			var meta struct {
				Model string `json:"model"`
			}
			_ = json.Unmarshal(ev.Data, &meta)
			if meta.Model != "" {
				model = meta.Model
			}
		case "output":
			var out struct {
				Response         string          `json:"response"`
				ReasoningContent string          `json:"reasoning_content"`
				FinishReason     string          `json:"finish_reason"`
				ToolCalls        json.RawMessage `json:"tool_calls"`
			}
			if err := json.Unmarshal(ev.Data, &out); err != nil {
				return nil
			}
			content.WriteString(out.Response)
			reasoning.WriteString(out.ReasoningContent)
			if out.FinishReason != "" {
				finishReason = out.FinishReason
			}
			mergeSoloToolCalls(toolCalls, &order, out.ToolCalls)
		case "token_usage":
			usage = json.RawMessage(ev.Data)
		case "done":
			sawDone = true
			var done struct {
				FinishReason string `json:"finish_reason"`
			}
			_ = json.Unmarshal(ev.Data, &done)
			if done.FinishReason != "" {
				finishReason = done.FinishReason
			}
		case "error":
			var se streamError
			if err := json.Unmarshal(ev.Data, &se); err != nil {
				return fmt.Errorf("solo error: %s", string(ev.Data))
			}
			return classifiedFromSolo(se)
		}
		return nil
	}); err != nil {
		return nil, err
	}
	if !sawDone {
		return nil, fmt.Errorf("trae stream ended before done")
	}
	message["content"] = content.String()
	if reasoning.Len() > 0 {
		message["reasoning_content"] = reasoning.String()
	}
	if len(toolCalls) > 0 {
		sort.Ints(order)
		calls := make([]any, 0, len(order))
		for _, index := range order {
			calls = append(calls, toolCalls[index])
		}
		message["tool_calls"] = calls
	}
	if model != "" {
		result["model"] = model
	}
	if usage != nil {
		result["usage"] = usage
	}
	result["id"] = fmt.Sprintf("chatcmpl-%d", time.Now().UnixNano())
	result["created"] = time.Now().Unix()
	result["choices"] = []map[string]any{{
		"index":         0,
		"message":       message,
		"finish_reason": finishReason,
	}}
	return result, nil
}

func rewriteSoloStream(src io.ReadCloser, model string) (*http.Response, error) {
	buf := bufio.NewReader(src)
	var replay []byte
	for i := 0; i < 16; i++ {
		ev, err := peekFirstSoloEvent(buf)
		if err != nil {
			src.Close()
			if errors.Is(err, io.EOF) {
				return nil, fmt.Errorf("trae stream ended before done")
			}
			return nil, err
		}
		if ev.Name == "error" {
			src.Close()
			var se streamError
			if json.Unmarshal(ev.Data, &se) != nil {
				return nil, fmt.Errorf("solo error: %s", string(ev.Data))
			}
			return nil, classifiedFromSolo(se)
		}
		replay = append(replay, encodeSoloEvent(ev)...)
		switch ev.Name {
		case "metadata", "timing_cost", "extra_info":
			continue
		default:
			rest := io.NopCloser(io.MultiReader(bytes.NewReader(replay), buf))
			return startSoloRewrite(rest, src, model), nil
		}
	}
	rest := io.NopCloser(io.MultiReader(bytes.NewReader(replay), buf))
	return startSoloRewrite(rest, src, model), nil
}

func startSoloRewrite(src io.ReadCloser, upstream io.Closer, model string) *http.Response {
	pr, pw := io.Pipe()
	go func() {
		defer upstream.Close()
		defer src.Close()
		defer pw.Close()
		id := fmt.Sprintf("chatcmpl-%d", time.Now().UnixNano())
		created := time.Now().Unix()
		var pendingUsage any
		ended := false
		writeChunk := func(delta map[string]any, finish string, usage any) error {
			chunk := map[string]any{
				"id":      id,
				"object":  "chat.completion.chunk",
				"created": created,
				"model":   model,
				"choices": []map[string]any{{
					"index": 0,
					"delta": delta,
				}},
			}
			if finish != "" {
				chunk["choices"].([]map[string]any)[0]["finish_reason"] = finish
			}
			if usage != nil {
				chunk["usage"] = usage
			}
			encoded, err := json.Marshal(chunk)
			if err != nil {
				return err
			}
			_, err = fmt.Fprintf(pw, "data: %s\n\n", encoded)
			return err
		}
		err := scanSolo(src, func(ev soloEvent) error {
			switch ev.Name {
			case "metadata":
				var meta struct {
					Model string `json:"model"`
				}
				_ = json.Unmarshal(ev.Data, &meta)
				if meta.Model != "" {
					model = meta.Model
				}
			case "output":
				var out struct {
					Response         string          `json:"response"`
					ReasoningContent string          `json:"reasoning_content"`
					ToolCalls        json.RawMessage `json:"tool_calls"`
				}
				if json.Unmarshal(ev.Data, &out) != nil {
					return nil
				}
				delta := map[string]any{}
				if out.Response != "" {
					delta["content"] = out.Response
				}
				if out.ReasoningContent != "" {
					delta["reasoning_content"] = out.ReasoningContent
				}
				if len(out.ToolCalls) > 0 && string(out.ToolCalls) != "null" {
					if remapped := remapToolCallsJSON(out.ToolCalls); remapped != nil {
						delta["tool_calls"] = remapped
					}
				}
				if len(delta) == 0 {
					return nil
				}
				usage := pendingUsage
				pendingUsage = nil
				return writeChunk(delta, "", usage)
			case "token_usage":
				pendingUsage = json.RawMessage(ev.Data)
			case "done":
				ended = true
				var done struct {
					FinishReason string `json:"finish_reason"`
				}
				_ = json.Unmarshal(ev.Data, &done)
				finish := done.FinishReason
				if finish == "" {
					finish = "stop"
				}
				usage := pendingUsage
				pendingUsage = nil
				if err := writeChunk(map[string]any{}, finish, usage); err != nil {
					return err
				}
				_, err := io.WriteString(pw, "data: [DONE]\n\n")
				return err
			case "error":
				ended = true
				var se streamError
				_ = json.Unmarshal(ev.Data, &se)
				payload, _ := json.Marshal(se.Error())
				_, _ = fmt.Fprintf(pw, "event: error\ndata: %s\n\n", payload)
				_, _ = io.WriteString(pw, "data: [DONE]\n\n")
				return classifiedFromSolo(se)
			}
			return nil
		})
		if !ended {
			_ = writeChunk(map[string]any{}, "stop", pendingUsage)
			_, _ = io.WriteString(pw, "data: [DONE]\n\n")
		}
		if err != nil {
			_ = pw.CloseWithError(err)
		}
	}()
	header := make(http.Header)
	header.Set("Content-Type", "text/event-stream")
	header.Set("Cache-Control", "no-cache")
	header.Set("Connection", "keep-alive")
	header.Set("X-Accel-Buffering", "no")
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     header,
		Body:       pr,
	}
}

func peekFirstSoloEvent(buf *bufio.Reader) (soloEvent, error) {
	var event string
	var data bytes.Buffer
	for {
		line, err := buf.ReadString('\n')
		if err != nil && len(line) == 0 {
			return soloEvent{}, err
		}
		line = strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r")
		if line == "" {
			if event == "" && data.Len() == 0 {
				if err != nil {
					return soloEvent{}, err
				}
				continue
			}
			ev := soloEvent{Name: event, Data: append(json.RawMessage(nil), data.Bytes()...)}
			if ev.Name == "" {
				ev.Name = "message"
			}
			return ev, nil
		}
		if strings.HasPrefix(line, ":") {
			continue
		}
		if strings.HasPrefix(line, "event:") {
			event = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
			continue
		}
		if strings.HasPrefix(line, "data:") {
			payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if data.Len() > 0 {
				data.WriteByte('\n')
			}
			data.WriteString(payload)
		}
		if err != nil {
			ev := soloEvent{Name: event, Data: append(json.RawMessage(nil), data.Bytes()...)}
			if ev.Name == "" {
				ev.Name = "message"
			}
			if ev.Name == "message" && data.Len() == 0 {
				return soloEvent{}, err
			}
			return ev, nil
		}
	}
}

func encodeSoloEvent(ev soloEvent) []byte {
	var b bytes.Buffer
	if ev.Name != "" {
		fmt.Fprintf(&b, "event: %s\n", ev.Name)
	}
	if len(ev.Data) > 0 {
		fmt.Fprintf(&b, "data: %s\n", ev.Data)
	}
	b.WriteByte('\n')
	return b.Bytes()
}

func scanSolo(reader io.Reader, handle func(soloEvent) error) error {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	var event string
	var data bytes.Buffer
	flush := func() error {
		if event == "" && data.Len() == 0 {
			return nil
		}
		ev := soloEvent{Name: event, Data: append(json.RawMessage(nil), data.Bytes()...)}
		event = ""
		data.Reset()
		if ev.Name == "" {
			ev.Name = "message"
		}
		return handle(ev)
	}
	for scanner.Scan() {
		line := strings.TrimSuffix(scanner.Text(), "\r")
		if line == "" {
			if err := flush(); err != nil {
				return err
			}
			continue
		}
		if strings.HasPrefix(line, ":") {
			continue
		}
		if strings.HasPrefix(line, "event:") {
			event = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
			continue
		}
		if strings.HasPrefix(line, "data:") {
			payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if data.Len() > 0 {
				data.WriteByte('\n')
			}
			data.WriteString(payload)
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	return flush()
}

func mergeSoloToolCalls(merged map[int]map[string]any, order *[]int, raw json.RawMessage) {
	if len(raw) == 0 || string(raw) == "null" {
		return
	}
	items := toolCallItems(raw)
	for i, item := range items {
		index := i
		if v, ok := item["index"].(float64); ok {
			index = int(v)
		}
		call, exists := merged[index]
		if !exists {
			call = map[string]any{"index": index, "type": "function"}
			merged[index] = call
			*order = append(*order, index)
		}
		if id, _ := item["id"].(string); id != "" {
			call["id"] = id
		}
		if typ, _ := item["type"].(string); typ != "" {
			call["type"] = typ
		}
		fn, _ := item["function"].(map[string]any)
		if fn == nil {
			fn, _ = item["function_call"].(map[string]any)
		}
		if fn == nil {
			continue
		}
		function, _ := call["function"].(map[string]any)
		if function == nil {
			function = map[string]any{}
			call["function"] = function
		}
		if name, _ := fn["name"].(string); name != "" {
			function["name"] = name
		}
		switch args := fn["arguments"].(type) {
		case string:
			prev, _ := function["arguments"].(string)
			function["arguments"] = prev + args
		default:
			if args != nil {
				rawArgs, _ := json.Marshal(args)
				prev, _ := function["arguments"].(string)
				function["arguments"] = prev + string(rawArgs)
			}
		}
	}
}

func remapToolCallsJSON(raw json.RawMessage) []any {
	items := toolCallItems(raw)
	if len(items) == 0 {
		return nil
	}
	out := make([]any, 0, len(items))
	for i, item := range items {
		if _, ok := item["index"]; !ok {
			item["index"] = i
		}
		if fn, ok := item["function_call"].(map[string]any); ok {
			delete(fn, "namespace")
			delete(fn, "partial_arguments")
			item["function"] = fn
			delete(item, "function_call")
		}
		if fn, ok := item["function"].(map[string]any); ok {
			delete(fn, "namespace")
			delete(fn, "partial_arguments")
		}
		if item["type"] == nil {
			item["type"] = "function"
		}
		out = append(out, item)
	}
	return out
}

func toolCallItems(raw json.RawMessage) []map[string]any {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return nil
	}
	if raw[0] == '{' {
		var one map[string]any
		if json.Unmarshal(raw, &one) != nil {
			return nil
		}
		return []map[string]any{one}
	}
	var list []map[string]any
	if json.Unmarshal(raw, &list) != nil {
		return nil
	}
	return list
}

func classifiedFromSolo(se streamError) error {
	body, _ := json.Marshal(map[string]any{"code": se.Code, "message": se.Message})
	return wrapClassified(Classify(0, string(body)), se.codeString())
}
