package gateway

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/caigee-cmd/cli2api/internal/accounts"
	"github.com/caigee-cmd/cli2api/internal/executor"
)

const maxSSELineSize = 16 * 1024 * 1024

type StreamFlushWriter struct {
	W http.ResponseWriter
	F http.Flusher
}

func (w StreamFlushWriter) Write(data []byte) (int, error) {
	n, err := w.W.Write(data)
	if n > 0 {
		w.F.Flush()
	}
	return n, err
}

type StreamRelayStats struct {
	PromptTokens     *int
	CompletionTokens *int
	CacheReadTokens  *int
	CacheWriteTokens *int
	CachedTokens     *int
	UsageSource      string
	Credits          *float64
	ConsumedCredits  *float64
	Model            string
	FinishReason     string
	FirstTokenAt     *time.Time
	SSEEventCount    int
	BytesRead        int64
	LastEvent        string
	SawDone          bool
}

type countingReader struct {
	reader io.Reader
	bytes  int64
}

func (r *countingReader) Read(p []byte) (int, error) {
	n, err := r.reader.Read(p)
	r.bytes += int64(n)
	return n, err
}

type StreamRelayWriteError struct {
	err error
}

func (e *StreamRelayWriteError) Error() string {
	if e == nil || e.err == nil {
		return "stream write error"
	}
	return "stream write error: " + e.err.Error()
}

func (e *StreamRelayWriteError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.err
}

func RelayOpenAIStream(w http.ResponseWriter, body io.Reader) (stats StreamRelayStats, returnErr error) {
	countedBody := &countingReader{reader: body}
	defer func() { stats.BytesRead = countedBody.bytes }()
	var writer io.Writer = w
	if flusher, ok := w.(http.Flusher); ok {
		writer = StreamFlushWriter{W: w, F: flusher}
	}

	scanner := bufio.NewScanner(countedBody)
	scanner.Buffer(make([]byte, 0, 64*1024), maxSSELineSize)
	sawDone := false
	var frame []string
	var streamErr error

	flushFrame := func() error {
		if len(frame) == 0 {
			return nil
		}
		eventName, data := parseSSEFrame(frame)
		stats.SSEEventCount++
		stats.LastEvent = eventName
		if classifiedErr := classifyStreamSSEError(eventName, data); classifiedErr != nil {
			if streamErr == nil {
				streamErr = classifiedErr
			}
			if err := writeStructuredStreamError(writer, classifiedErr); err != nil {
				return err
			}
			frame = nil
			return nil
		}
		for _, line := range frame {
			line = strings.TrimSuffix(line, "\r")
			if stats.FirstTokenAt == nil && SSEDeltaHasToken(line) {
				now := time.Now()
				stats.FirstTokenAt = &now
			}
			if usage, ok := ParseStreamUsageLine(line); ok {
				usage.FirstTokenAt = stats.FirstTokenAt
				usage.SSEEventCount = stats.SSEEventCount
				usage.BytesRead = stats.BytesRead
				usage.LastEvent = stats.LastEvent
				usage.SawDone = stats.SawDone
				stats = usage
			}
			if strings.TrimSpace(strings.TrimPrefix(line, "data:")) == "[DONE]" {
				sawDone = true
				stats.SawDone = true
			}
		}
		output := strings.Join(frame, "\n") + "\n\n"
		if _, err := io.WriteString(writer, output); err != nil {
			return &StreamRelayWriteError{err: err}
		}
		frame = nil
		return nil
	}

	for scanner.Scan() {
		line := strings.TrimSuffix(scanner.Text(), "\r")
		if line == "" {
			if err := flushFrame(); err != nil {
				return stats, err
			}
			continue
		}
		frame = append(frame, line)
	}
	if err := scanner.Err(); err != nil {
		if streamErr != nil {
			return stats, streamErr
		}
		streamErr := executor.StreamReadError(err)
		if writeErr := writeStructuredStreamError(writer, streamErr); writeErr != nil {
			return stats, writeErr
		}
		return stats, streamErr
	}
	if err := flushFrame(); err != nil {
		return stats, err
	}
	if streamErr != nil {
		return stats, streamErr
	}
	if !sawDone {
		streamErr := executor.StreamIncompleteError()
		if writeErr := writeStructuredStreamError(writer, streamErr); writeErr != nil {
			return stats, writeErr
		}
		return stats, streamErr
	}
	return stats, nil
}

func writeStructuredStreamError(writer io.Writer, err error) error {
	if err == nil {
		return nil
	}
	classified := ClassifyAPIError(err)
	status := classified.Status
	if status == 0 {
		status = http.StatusBadGateway
	}
	code := firstNonEmpty(classified.Code, "upstream_error")
	typ := firstNonEmpty(classified.Type, "api_error")
	message := firstNonEmpty(classified.Message, code)
	errorPayload := map[string]any{
		"message":  message,
		"type":     typ,
		"code":     code,
		"kind":     firstNonEmpty(classified.Kind, accounts.KindUnavailable),
		"status":   status,
		"failover": classified.Failover,
	}
	if classified.RetryAfter > 0 {
		seconds := int(classified.RetryAfter / time.Second)
		if classified.RetryAfter%time.Second != 0 {
			seconds++
		}
		if seconds < 1 {
			seconds = 1
		}
		errorPayload["retry_after"] = seconds
	}
	payload, marshalErr := json.Marshal(map[string]any{"error": errorPayload})
	if marshalErr != nil {
		return marshalErr
	}
	if _, writeErr := io.WriteString(writer, "data: "+string(payload)+"\n\n"); writeErr != nil {
		return &StreamRelayWriteError{err: writeErr}
	}
	return nil
}

func parseSSEFrame(lines []string) (eventName, data string) {
	eventName = "message"
	dataLines := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSuffix(line, "\r")
		if strings.HasPrefix(line, "event:") {
			eventName = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
			if eventName == "" {
				eventName = "message"
			}
			continue
		}
		if strings.HasPrefix(line, "data:") {
			value := strings.TrimPrefix(line, "data:")
			if strings.HasPrefix(value, " ") {
				value = value[1:]
			}
			dataLines = append(dataLines, value)
		}
	}
	return eventName, strings.Join(dataLines, "\n")
}

func classifyStreamSSEError(eventName, data string) error {
	force := strings.EqualFold(strings.TrimSpace(eventName), "error")
	if !force && !streamJSONLooksLikeError(data) {
		return nil
	}
	status := streamErrorStatus(data)
	body := strings.TrimSpace(data)
	if inner := streamErrorBody(data); inner != "" {
		body = inner
	}
	return executor.ClassifyUpstreamBody(status, body)
}

func streamJSONLooksLikeError(raw string) bool {
	var value any
	if json.Unmarshal([]byte(strings.TrimSpace(raw)), &value) != nil {
		return false
	}
	return streamValueLooksLikeError(value)
}

func streamValueLooksLikeError(value any) bool {
	object, ok := value.(map[string]any)
	if !ok {
		if nested, ok := value.(string); ok {
			var parsed any
			return json.Unmarshal([]byte(strings.TrimSpace(nested)), &parsed) == nil && streamValueLooksLikeError(parsed)
		}
		return false
	}
	if _, ok := object["choices"]; ok {
		return false
	}
	if _, ok := object["error"]; ok {
		return true
	}
	if nested, ok := object["body"]; ok {
		if streamValueLooksLikeError(nested) {
			return true
		}
	}
	if status, ok := object["status"].(float64); ok && status >= 400 {
		return true
	}
	for _, key := range []string{"code", "msgCode", "kind"} {
		if _, ok := object[key]; ok {
			return true
		}
	}
	if message, ok := object["message"].(string); ok && strings.TrimSpace(message) != "" {
		return true
	}
	return false
}

func streamErrorBody(raw string) string {
	var value any
	if json.Unmarshal([]byte(strings.TrimSpace(raw)), &value) != nil {
		return ""
	}
	for {
		switch current := value.(type) {
		case string:
			var nested any
			if json.Unmarshal([]byte(strings.TrimSpace(current)), &nested) != nil {
				return current
			}
			value = nested
		case map[string]any:
			nested, ok := current["body"]
			if !ok {
				encoded, err := json.Marshal(current)
				if err != nil {
					return raw
				}
				return string(encoded)
			}
			value = nested
		default:
			return raw
		}
	}
}

func streamErrorStatus(raw string) int {
	var value any
	if json.Unmarshal([]byte(strings.TrimSpace(raw)), &value) != nil {
		return 0
	}
	for {
		switch current := value.(type) {
		case map[string]any:
			for _, key := range []string{"status", "statusCode", "statusCodeValue"} {
				if status, ok := current[key].(float64); ok && int(status) >= 400 {
					return int(status)
				}
			}
			if nested, ok := current["body"]; ok {
				value = nested
				continue
			}
			if nested, ok := current["error"]; ok {
				value = nested
				continue
			}
			return 0
		case string:
			var nested any
			if json.Unmarshal([]byte(strings.TrimSpace(current)), &nested) != nil {
				return 0
			}
			value = nested
		default:
			return 0
		}
	}
}

func IsStreamClientDisconnect(err error) bool {
	var writeErr *StreamRelayWriteError
	return errors.As(err, &writeErr)
}

func SSEDeltaHasToken(line string) bool {
	payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
	if payload == "" || payload == "[DONE]" {
		return false
	}
	var parsed struct {
		Choices []struct {
			Delta struct {
				Content          json.RawMessage `json:"content"`
				ReasoningContent json.RawMessage `json:"reasoning_content"`
				ToolCalls        json.RawMessage `json:"tool_calls"`
			} `json:"delta"`
		} `json:"choices"`
	}
	if json.Unmarshal([]byte(payload), &parsed) != nil || len(parsed.Choices) == 0 {
		return false
	}
	delta := parsed.Choices[0].Delta
	return jsonHasText(delta.Content) || jsonHasText(delta.ReasoningContent) || jsonHasArray(delta.ToolCalls)
}

func jsonHasText(raw json.RawMessage) bool {
	if len(raw) == 0 || string(raw) == "null" {
		return false
	}
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return strings.TrimSpace(text) != ""
	}
	var parts []any
	if json.Unmarshal(raw, &parts) == nil {
		return len(parts) > 0
	}
	return true
}

func jsonHasArray(raw json.RawMessage) bool {
	if len(raw) == 0 || string(raw) == "null" {
		return false
	}
	var parts []any
	if json.Unmarshal(raw, &parts) != nil {
		return false
	}
	return len(parts) > 0
}

func ParseStreamUsageLine(line string) (StreamRelayStats, bool) {
	payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
	if payload == "" || payload == "[DONE]" {
		return StreamRelayStats{}, false
	}
	var parsed struct {
		Model string `json:"model"`
		Usage *struct {
			PromptTokens     *int     `json:"prompt_tokens"`
			CompletionTokens *int     `json:"completion_tokens"`
			CacheReadTokens  *int     `json:"cache_read_tokens"`
			CacheWriteTokens *int     `json:"cache_write_tokens"`
			Source           string   `json:"source"`
			Credits          *float64 `json:"credits"`
			Credit           *float64 `json:"credit"`
			PromptDetails    struct {
				CachedTokens *int `json:"cached_tokens"`
			} `json:"prompt_tokens_details"`
		} `json:"usage"`
	}
	if err := json.Unmarshal([]byte(payload), &parsed); err != nil || parsed.Usage == nil {
		return StreamRelayStats{}, false
	}
	credits := parsed.Usage.Credits
	if credits == nil {
		credits = parsed.Usage.Credit
	}
	cacheRead := parsed.Usage.CacheReadTokens
	if cacheRead == nil {
		cacheRead = parsed.Usage.PromptDetails.CachedTokens
	}
	return StreamRelayStats{
		PromptTokens:     parsed.Usage.PromptTokens,
		CompletionTokens: parsed.Usage.CompletionTokens,
		CacheReadTokens:  cacheRead,
		CacheWriteTokens: parsed.Usage.CacheWriteTokens,
		CachedTokens:     parsed.Usage.PromptDetails.CachedTokens,
		UsageSource:      firstNonEmpty(parsed.Usage.Source, "estimate"),
		Credits:          credits,
		Model:            parsed.Model,
	}, true
}
