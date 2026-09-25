package translate

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

// MaxCollectedResponseBytes bounds a non-stream native response assembled
// from upstream SSE. The terminal response object repeats every output item,
// so the cap covers the whole stream, not one event.
const MaxCollectedResponseBytes = 64 << 20

// ErrResponsesIncomplete means the upstream stream ended before any terminal
// event. Nothing was relayed yet, so callers may treat it as retryable.
var ErrResponsesIncomplete = errors.New("upstream responses stream ended before a terminal event")

// ResponsesStreamFailure is a terminal response.failed / error event. Body is
// the event payload for the caller's error classifier.
type ResponsesStreamFailure struct {
	Body string
}

func (e *ResponsesStreamFailure) Error() string {
	return "upstream responses stream failed: " + truncateForError(e.Body, 300)
}

// CollectedResponse is a non-stream Responses result assembled from SSE.
type CollectedResponse struct {
	// Response is the terminal event's response object, verbatim: every
	// output item keeps its id, type, and members.
	Response     json.RawMessage
	FinishReason string
	InputTokens  *int
	OutputTokens *int
	CachedTokens *int
}

// CollectResponses reads an upstream Responses SSE body to its terminal event
// and returns the complete response object. It is the non-stream twin of the
// gateway's native relay and shares ParseResponsesEvent with it.
func CollectResponses(body io.Reader) (CollectedResponse, error) {
	reader := bufio.NewReaderSize(io.LimitReader(body, MaxCollectedResponseBytes+1), 64<<10)
	var total int
	var data []string
	finish := func() (CollectedResponse, bool, error) {
		if len(data) == 0 {
			return CollectedResponse{}, false, nil
		}
		payload := strings.Join(data, "\n")
		data = data[:0]
		event, ok := ParseResponsesEvent(payload)
		if !ok || !event.Terminal {
			return CollectedResponse{}, false, nil
		}
		if event.FinishReason == "error" {
			return CollectedResponse{}, true, &ResponsesStreamFailure{Body: payload}
		}
		if len(event.Response) == 0 || event.Response[0] != '{' {
			return CollectedResponse{}, true, fmt.Errorf("upstream %s carried no response object", event.Type)
		}
		return CollectedResponse{
			Response: event.Response, FinishReason: event.FinishReason,
			InputTokens: event.InputTokens, OutputTokens: event.OutputTokens, CachedTokens: event.CachedTokens,
		}, true, nil
	}
	for {
		line, err := reader.ReadString('\n')
		total += len(line)
		if total > MaxCollectedResponseBytes {
			return CollectedResponse{}, fmt.Errorf("upstream responses stream exceeds %d bytes", MaxCollectedResponseBytes)
		}
		trimmed := strings.TrimRight(line, "\r\n")
		switch {
		case trimmed == "" && line != "":
			if result, done, ferr := finish(); done {
				return result, ferr
			}
		case strings.HasPrefix(trimmed, "data:"):
			value := strings.TrimPrefix(trimmed, "data:")
			data = append(data, strings.TrimPrefix(value, " "))
		}
		if err == io.EOF {
			if result, done, ferr := finish(); done {
				return result, ferr
			}
			return CollectedResponse{}, ErrResponsesIncomplete
		}
		if err != nil {
			return CollectedResponse{}, err
		}
	}
}

func truncateForError(value string, limit int) string {
	value = strings.TrimSpace(value)
	if len(value) <= limit {
		return value
	}
	return value[:limit]
}
