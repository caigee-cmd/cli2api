package command

import (
	"bufio"
	"encoding/json"
	"io"
	"strings"
)

// ndjsonMaxLine bounds a single NDJSON line. Tool-call input deltas carry JSON
// fragments and large tool outputs can be long, so the reader must not use
// bufio.Scanner's default 64KB limit.
const ndjsonMaxLine = 8 * 1024 * 1024

// generateEvent is one decoded NDJSON line from /alpha/generate. A single
// logical block (reasoning / text / tool call) is keyed by id so blocks can
// interleave.
type generateEvent struct {
	Type         string          `json:"type"`
	ID           string          `json:"id"`
	ToolCallID   string          `json:"toolCallId"`
	ToolName     string          `json:"toolName"`
	Name         string          `json:"name"`
	Text         string          `json:"text"`
	Delta        string          `json:"delta"`
	Input        json.RawMessage `json:"input"`
	Args         json.RawMessage `json:"args"`
	FinishReason string          `json:"finishReason"`
	Usage        *eventUsage     `json:"usage"`
	TotalUsage   *eventUsage     `json:"totalUsage"`
	Error        json.RawMessage `json:"error"`
	Message      string          `json:"message"`
}

type eventUsage struct {
	InputTokens       int `json:"inputTokens"`
	OutputTokens      int `json:"outputTokens"`
	TotalTokens       int `json:"totalTokens"`
	CachedInputTokens int `json:"cachedInputTokens"`
	InputTokenDetails struct {
		NoCacheTokens   int `json:"noCacheTokens"`
		CacheReadTokens int `json:"cacheReadTokens"`
	} `json:"inputTokenDetails"`
	Raw struct {
		PromptCacheHitTokens int `json:"prompt_cache_hit_tokens"`
	} `json:"raw"`
}

// cacheReadTokens reports cache hits across the shapes the gateway uses.
func (u *eventUsage) cacheReadTokens() int {
	if u == nil {
		return 0
	}
	if u.CachedInputTokens > 0 {
		return u.CachedInputTokens
	}
	if u.InputTokenDetails.CacheReadTokens > 0 {
		return u.InputTokenDetails.CacheReadTokens
	}
	if u.Raw.PromptCacheHitTokens > 0 {
		return u.Raw.PromptCacheHitTokens
	}
	return 0
}

// toolEventID resolves the id used to key a tool block. Per-delta events use
// `id`; the final redundant `tool-call` event uses `toolCallId`.
func (e generateEvent) toolEventID() string {
	if strings.TrimSpace(e.ID) != "" {
		return strings.TrimSpace(e.ID)
	}
	return strings.TrimSpace(e.ToolCallID)
}

func (e generateEvent) toolEventName() string {
	return firstNonEmpty(e.ToolName, e.Name)
}

// usage prefers `usage` (finish-step) and falls back to `totalUsage` (finish).
// The gateway emits both terminal events in one stream; only one carries the
// numbers, so callers must not assume `usage` is always present.
func (e generateEvent) usage() *eventUsage {
	if e.Usage != nil {
		return e.Usage
	}
	return e.TotalUsage
}

// streamEvents parses the NDJSON body line by line. Blank lines are skipped;
// malformed lines are tolerated (a stray keep-alive must not kill the stream).
func streamEvents(r io.Reader, onEvent func(generateEvent) error) error {
	reader := bufio.NewReaderSize(r, 64*1024)
	for {
		line, err := readLine(reader)
		if len(line) > 0 {
			trimmed := strings.TrimSpace(string(line))
			if trimmed != "" {
				var event generateEvent
				if json.Unmarshal([]byte(trimmed), &event) == nil && event.Type != "" {
					if cbErr := onEvent(event); cbErr != nil {
						return cbErr
					}
				}
			}
		}
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
	}
}

// readLine returns one line without its trailing newline. It avoids
// bufio.Scanner's fixed buffer ceiling so large tool deltas do not truncate.
func readLine(reader *bufio.Reader) ([]byte, error) {
	var buffer []byte
	for {
		chunk, err := reader.ReadSlice('\n')
		buffer = append(buffer, chunk...)
		if err == bufio.ErrBufferFull {
			if len(buffer) > ndjsonMaxLine {
				return buffer, nil
			}
			continue
		}
		if err != nil {
			return trimEOL(buffer), err
		}
		return trimEOL(buffer), nil
	}
}

func trimEOL(line []byte) []byte {
	line = append([]byte(nil), line...)
	for len(line) > 0 && (line[len(line)-1] == '\n' || line[len(line)-1] == '\r') {
		line = line[:len(line)-1]
	}
	return line
}

// errorFromEvent extracts a classified error from an `error` event. The gateway
// emits errors either as {"type":"error","message":…} or
// {"type":"error","error":{...}}.
func errorFromEvent(event generateEvent) error {
	if msg := strings.TrimSpace(event.Message); msg != "" {
		return newProviderError(502, msg)
	}
	if len(event.Error) > 0 && string(event.Error) != "null" {
		return newProviderError(502, string(event.Error))
	}
	return newProviderError(502, "command code stream error")
}
