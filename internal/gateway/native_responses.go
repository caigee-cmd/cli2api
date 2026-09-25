package gateway

import (
	"bufio"
	"encoding/json"
	"io"
	"strings"
	"time"

	"github.com/caigee-cmd/cli2api/internal/executor"
)

// RelayNativeResponsesStream relays an upstream OpenAI Responses SSE body
// verbatim while observing the frames needed for accounting: first-token
// timing, terminal status, and token usage. It writes nothing of its own —
// the upstream already emits a well-formed event stream including
// response.created / response.completed.
func RelayNativeResponsesStream(writer io.Writer, body io.Reader) (StreamRelayStats, error) {
	var stats StreamRelayStats
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64*1024), maxSSELineSize)
	var frame []string
	var sawTerminal bool
	flush := func() error {
		if len(frame) == 0 {
			return nil
		}
		eventName, data := parseSSEFrame(frame)
		if _, err := io.WriteString(writer, strings.Join(frame, "\n")+"\n\n"); err != nil {
			frame = nil
			return &StreamRelayWriteError{err: err}
		}
		frame = nil
		if classified := classifyStreamSSEError(eventName, data); classified != nil {
			return classified
		}
		payload := strings.TrimSpace(data)
		if payload == "" || payload == "[DONE]" {
			return nil
		}
		var obj struct {
			Type     string `json:"type"`
			Response struct {
				Status string `json:"status"`
				Usage  struct {
					InputTokens  int `json:"input_tokens"`
					OutputTokens int `json:"output_tokens"`
					TotalTokens  int `json:"total_tokens"`
				} `json:"usage"`
			} `json:"response"`
		}
		if json.Unmarshal([]byte(payload), &obj) != nil {
			return nil
		}
		if stats.FirstTokenAt == nil && isFirstTokenEvent(obj.Type) {
			now := time.Now()
			stats.FirstTokenAt = &now
		}
		switch obj.Type {
		case "response.completed":
			sawTerminal = true
			stats.FinishReason = "stop"
			if obj.Response.Usage.InputTokens > 0 {
				stats.PromptTokens = intPtr(obj.Response.Usage.InputTokens)
			}
			if obj.Response.Usage.OutputTokens > 0 {
				stats.CompletionTokens = intPtr(obj.Response.Usage.OutputTokens)
			}
		case "response.incomplete":
			sawTerminal = true
			stats.FinishReason = "length"
			if obj.Response.Usage.InputTokens > 0 {
				stats.PromptTokens = intPtr(obj.Response.Usage.InputTokens)
			}
			if obj.Response.Usage.OutputTokens > 0 {
				stats.CompletionTokens = intPtr(obj.Response.Usage.OutputTokens)
			}
		case "response.failed", "error":
			sawTerminal = true
			stats.FinishReason = "error"
		}
		return nil
	}
	for scanner.Scan() {
		line := strings.TrimSuffix(scanner.Text(), "\r")
		if line == "" {
			if err := flush(); err != nil {
				return stats, err
			}
			continue
		}
		frame = append(frame, line)
	}
	if err := scanner.Err(); err != nil {
		return stats, executor.StreamReadError(err)
	}
	if err := flush(); err != nil {
		return stats, err
	}
	if !sawTerminal {
		return stats, executor.StreamIncompleteError()
	}
	return stats, nil
}

func isFirstTokenEvent(eventType string) bool {
	return strings.HasSuffix(eventType, ".delta")
}

func intPtr(v int) *int { return &v }
