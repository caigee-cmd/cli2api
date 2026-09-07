package workbuddy

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"
)

// Aggregate converts an upstream SSE stream into one Chat Completions object,
// merging tool_calls by index and preserving reasoning_content.
func Aggregate(reader io.Reader) (map[string]any, error) {
	result := map[string]any{
		"object": "chat.completion",
	}
	message := map[string]any{"role": "assistant"}
	toolCalls := map[int]map[string]any{}
	finishReason := "stop"
	var content, reasoning strings.Builder
	var order []int
	sawDone := false

	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for scanner.Scan() {
		line := strings.TrimSuffix(scanner.Text(), "\r")
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "" {
			continue
		}
		if payload == "[DONE]" {
			sawDone = true
			break
		}
		var chunk struct {
			ID      string `json:"id"`
			Model   string `json:"model"`
			Created int64  `json:"created"`
			Usage   any    `json:"usage"`
			Choices []struct {
				FinishReason string `json:"finish_reason"`
				Delta        struct {
					Role             string          `json:"role"`
					Content          string          `json:"content"`
					ReasoningContent string          `json:"reasoning_content"`
					ToolCalls        json.RawMessage `json:"tool_calls"`
				} `json:"delta"`
				Message json.RawMessage `json:"message"`
			} `json:"choices"`
		}
		if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
			continue
		}
		if chunk.ID != "" {
			result["id"] = chunk.ID
		}
		if chunk.Model != "" {
			result["model"] = chunk.Model
		}
		if chunk.Created != 0 {
			result["created"] = chunk.Created
		}
		if chunk.Usage != nil {
			result["usage"] = chunk.Usage
		}
		if len(chunk.Choices) == 0 {
			continue
		}
		choice := chunk.Choices[0]
		if choice.FinishReason != "" {
			finishReason = choice.FinishReason
		}
		if choice.Delta.Role != "" {
			message["role"] = choice.Delta.Role
		}
		content.WriteString(choice.Delta.Content)
		reasoning.WriteString(choice.Delta.ReasoningContent)
		if len(choice.Delta.ToolCalls) > 0 && string(choice.Delta.ToolCalls) != "null" {
			mergeToolCalls(toolCalls, &order, choice.Delta.ToolCalls)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if !sawDone {
		return nil, fmt.Errorf("workbuddy stream ended before [DONE]")
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
	if result["id"] == nil {
		result["id"] = fmt.Sprintf("chatcmpl-%d", time.Now().UnixNano())
	}
	if result["created"] == nil {
		result["created"] = time.Now().Unix()
	}
	result["choices"] = []map[string]any{{
		"index":         0,
		"message":       message,
		"finish_reason": finishReason,
	}}
	return result, nil
}

func mergeToolCalls(merged map[int]map[string]any, order *[]int, raw json.RawMessage) {
	var deltas []struct {
		Index    int    `json:"index"`
		ID       string `json:"id"`
		Type     string `json:"type"`
		Function struct {
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
		} `json:"function"`
	}
	if err := json.Unmarshal(raw, &deltas); err != nil {
		return
	}
	for _, delta := range deltas {
		call, exists := merged[delta.Index]
		if !exists {
			call = map[string]any{"index": delta.Index}
			merged[delta.Index] = call
			*order = append(*order, delta.Index)
		}
		if delta.ID != "" {
			call["id"] = delta.ID
		}
		if delta.Type != "" {
			call["type"] = delta.Type
		}
		if delta.Function.Name != "" {
			function, _ := call["function"].(map[string]any)
			if function == nil {
				function = map[string]any{}
				call["function"] = function
			}
			function["name"] = delta.Function.Name
		}
		if delta.Function.Arguments != "" {
			function, _ := call["function"].(map[string]any)
			if function == nil {
				function = map[string]any{}
				call["function"] = function
			}
			arguments, _ := function["arguments"].(string)
			function["arguments"] = arguments + delta.Function.Arguments
		}
	}
}

// rewriteChatStream strips empty WorkBuddy delta fields. Upstream chat chunks
// always include content:"", reasoning_content:"", refusal:"", tool_calls:[],
// and a dummy function_call; OpenAI-compatible clients then render a flood of
// blank thinking events.
func rewriteChatStream(resp *http.Response) *http.Response {
	pr, pw := io.Pipe()
	go func() {
		defer resp.Body.Close()
		defer pw.Close()
		if err := copySanitizedSSE(resp.Body, pw); err != nil {
			_ = pw.CloseWithError(err)
		}
	}()
	out := *resp
	out.Body = pr
	out.ContentLength = -1
	header := resp.Header.Clone()
	header.Del("Content-Length")
	out.Header = header
	return &out
}

func copySanitizedSSE(src io.Reader, dst io.Writer) error {
	scanner := bufio.NewScanner(src)
	scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	sentRole := false
	for scanner.Scan() {
		line := strings.TrimSuffix(scanner.Text(), "\r")
		if line == "" {
			continue
		}
		if !strings.HasPrefix(line, "data:") {
			if _, err := fmt.Fprintf(dst, "%s\n", line); err != nil {
				return err
			}
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "" {
			continue
		}
		cleaned, ok := sanitizeSSEPayload(payload, &sentRole)
		if !ok {
			continue
		}
		if _, err := fmt.Fprintf(dst, "data: %s\n\n", cleaned); err != nil {
			return err
		}
	}
	return scanner.Err()
}

func sanitizeSSEPayload(payload string, sentRole *bool) (string, bool) {
	if payload == "[DONE]" {
		return payload, true
	}
	decoder := json.NewDecoder(bytes.NewReader([]byte(payload)))
	decoder.UseNumber()
	var chunk map[string]any
	if err := decoder.Decode(&chunk); err != nil {
		return payload, true
	}
	keep := chunk["usage"] != nil
	if choices, ok := chunk["choices"].([]any); ok {
		for _, raw := range choices {
			choice, _ := raw.(map[string]any)
			if choice == nil {
				continue
			}
			if delta, ok := choice["delta"].(map[string]any); ok {
				sanitizeDelta(delta)
				if sentRole != nil {
					if _, hasRole := delta["role"]; hasRole && *sentRole {
						delete(delta, "role")
					}
				}
				if len(delta) > 0 {
					keep = true
					if sentRole != nil {
						if _, hasRole := delta["role"]; hasRole {
							*sentRole = true
						}
					}
				}
			}
			switch finish := choice["finish_reason"].(type) {
			case string:
				if finish == "" {
					delete(choice, "finish_reason")
				} else {
					keep = true
				}
			case nil:
				delete(choice, "finish_reason")
			}
		}
	}
	if !keep {
		return "", false
	}
	encoded, err := json.Marshal(chunk)
	if err != nil {
		return payload, true
	}
	return string(encoded), true
}

func sanitizeDelta(delta map[string]any) {
	dropEmptyString(delta, "content")
	dropEmptyString(delta, "reasoning_content")
	dropEmptyString(delta, "refusal")
	if extra, ok := delta["extra_fields"]; ok && extra == nil {
		delete(delta, "extra_fields")
	}
	if calls, ok := delta["tool_calls"].([]any); ok && len(calls) == 0 {
		delete(delta, "tool_calls")
	}
	if delta["tool_calls"] == nil {
		delete(delta, "tool_calls")
	}
	switch call := delta["function_call"].(type) {
	case nil:
		delete(delta, "function_call")
	case map[string]any:
		name, _ := call["name"].(string)
		args, _ := call["arguments"].(string)
		if name == "" && args == "" {
			delete(delta, "function_call")
		}
	}
}

func dropEmptyString(delta map[string]any, key string) {
	value, ok := delta[key].(string)
	if ok && value == "" {
		delete(delta, key)
	}
}
