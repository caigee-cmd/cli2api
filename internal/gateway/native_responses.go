package gateway

import (
	"bufio"
	"encoding/json"
	"io"
	"strings"
	"time"

	"github.com/caigee-cmd/cli2api/internal/executor"
	"github.com/caigee-cmd/cli2api/internal/translate"
)

// RelayNativeResponsesStream relays an upstream OpenAI Responses SSE body
// while observing the frames needed for accounting: first-token timing,
// terminal status, and token usage. It writes no events of its own — the
// upstream already emits a well-formed stream including response.created /
// response.completed.
//
// Frames are relayed verbatim except for one rewrite: when names is non-empty,
// flattened namespace tool names (the form providers receive) are restored to
// the caller's namespace + name on function_call items, exactly as the
// compatibility relay does. Frames that carry no such tool call stay
// byte-for-byte.
func RelayNativeResponsesStream(writer io.Writer, body io.Reader, names map[string]translate.ResponseToolName) (StreamRelayStats, error) {
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
		if _, err := io.WriteString(writer, restoreNativeFrameToolNames(frame, data, names)); err != nil {
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
		event, ok := translate.ParseResponsesEvent(payload)
		if !ok {
			return nil
		}
		if stats.FirstTokenAt == nil && event.FirstToken {
			now := time.Now()
			stats.FirstTokenAt = &now
		}
		if !event.Terminal {
			return nil
		}
		sawTerminal = true
		stats.FinishReason = event.FinishReason
		stats.PromptTokens = event.InputTokens
		stats.CompletionTokens = event.OutputTokens
		stats.CachedTokens = event.CachedTokens
		if event.FinishReason == "error" {
			return executor.ClassifyUpstreamBody(0, payload)
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

// restoreNativeFrameToolNames returns the frame to write. Without a mapping, or
// when the payload holds none of the flattened names, the original lines are
// returned untouched so passthrough stays verbatim. A frame that does need a
// rewrite is re-encoded with UseNumber so numeric fields keep their text.
func restoreNativeFrameToolNames(frame []string, data string, names map[string]translate.ResponseToolName) string {
	original := strings.Join(frame, "\n") + "\n\n"
	if len(names) == 0 || !frameMentionsToolName(data, names) {
		return original
	}
	decoder := json.NewDecoder(strings.NewReader(data))
	decoder.UseNumber()
	var payload any
	if decoder.Decode(&payload) != nil {
		return original
	}
	translate.RestoreResponseToolNames(payload, names)
	encoded, err := json.Marshal(payload)
	if err != nil {
		return original
	}
	var out strings.Builder
	for _, line := range frame {
		if !strings.HasPrefix(strings.TrimSuffix(line, "\r"), "data:") {
			out.WriteString(line)
			out.WriteString("\n")
		}
	}
	out.WriteString("data: ")
	out.Write(encoded)
	out.WriteString("\n\n")
	return out.String()
}

func frameMentionsToolName(data string, names map[string]translate.ResponseToolName) bool {
	for flat := range names {
		if strings.Contains(data, flat) {
			return true
		}
	}
	return false
}
