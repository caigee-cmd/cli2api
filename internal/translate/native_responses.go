package translate

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

// MaxNativeRequestBytes bounds a /v1/responses body. Requests carry whole
// conversations with inline images, so the cap is generous; it exists to stop
// an unbounded read, not to police normal traffic.
const MaxNativeRequestBytes = 64 << 20

// NativeResponsesRequest is a complete, validated OpenAI Responses request as
// the client sent it. It owns its bytes and is logically read-only: every
// accessor returns data derived from, or copied out of, the original body, so
// per-attempt rewrites (model mapping, reasoning clamp, prompt policy) can
// never leak into a later failover attempt.
//
// It is deliberately not a ChatRequest. The compatibility path derives one on
// demand through Compat; the native path hands the original fields to an
// adapter that speaks Responses upstream.
type NativeResponsesRequest struct {
	raw    []byte
	fields map[string]json.RawMessage
	model  string
	stream bool
}

// ParseNativeResponses validates a Responses body without flattening it. It
// rejects what no path could serve (non-object bodies, trailing JSON values,
// a missing model or input, and server-side conversation state) and keeps
// every other top-level member, including ones this package does not know.
func ParseNativeResponses(body []byte) (*NativeResponsesRequest, error) {
	raw := append([]byte(nil), bytes.TrimSpace(body)...)
	if len(raw) == 0 || raw[0] != '{' {
		return nil, errors.New("request body must be a JSON object")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	var fields map[string]json.RawMessage
	if err := decoder.Decode(&fields); err != nil {
		return nil, err
	}
	if _, err := decoder.Token(); err != io.EOF {
		return nil, errors.New("request body must contain a single JSON object")
	}
	request := &NativeResponsesRequest{raw: raw, fields: fields}

	model, ok := rawJSONString(fields["model"])
	if !ok || strings.TrimSpace(model) == "" {
		return nil, errors.New("model required")
	}
	request.model = strings.TrimSpace(model)

	if value, present := fields["stream"]; present && !isJSONNull(value) {
		if err := json.Unmarshal(value, &request.stream); err != nil {
			return nil, errors.New("stream must be a boolean")
		}
	}
	if previous, _ := rawJSONString(fields["previous_response_id"]); strings.TrimSpace(previous) != "" || !emptyJSON(fields["conversation"]) {
		return nil, errors.New("previous_response_id and conversation are not supported; send the complete conversation in input")
	}
	switch jsonKind(fields["input"]) {
	case '"':
		if text, _ := rawJSONString(fields["input"]); strings.TrimSpace(text) == "" {
			return nil, errors.New("input required")
		}
	case '[':
		var items []json.RawMessage
		if err := json.Unmarshal(fields["input"], &items); err != nil {
			return nil, fmt.Errorf("input: %w", err)
		}
		if len(items) == 0 {
			return nil, errors.New("input required")
		}
		for index, item := range items {
			if jsonKind(item) != '{' {
				return nil, fmt.Errorf("input[%d] must be an object", index)
			}
		}
	case 0:
		return nil, errors.New("input required")
	default:
		return nil, errors.New("input must be a string or an array")
	}
	if value, present := fields["instructions"]; present && !isJSONNull(value) {
		if kind := jsonKind(value); kind != '"' && kind != '[' {
			return nil, errors.New("instructions must be a string or an array")
		}
	}
	for _, key := range []string{"tools", "include"} {
		if value, present := fields[key]; present && !isJSONNull(value) && jsonKind(value) != '[' {
			return nil, fmt.Errorf("%s must be an array", key)
		}
	}
	for _, key := range []string{"reasoning", "text"} {
		if value, present := fields[key]; present && !isJSONNull(value) && jsonKind(value) != '{' {
			return nil, fmt.Errorf("%s must be an object", key)
		}
	}
	return request, nil
}

// Body returns the original request bytes. Callers must not retain and mutate
// the slice; Fields is the mutable per-attempt copy.
func (r *NativeResponsesRequest) Body() []byte {
	if r == nil {
		return nil
	}
	return append([]byte(nil), r.raw...)
}

// Model is the client's model id, including any provider prefix.
func (r *NativeResponsesRequest) Model() string { return r.model }

// Stream is the client's requested output mode, independent of how the
// upstream transports the response.
func (r *NativeResponsesRequest) Stream() bool { return r.stream }

// Fields returns a private copy of the top-level members for one attempt. The
// map and every value slice are fresh, so the caller may rewrite them freely.
func (r *NativeResponsesRequest) Fields() map[string]json.RawMessage {
	out := make(map[string]json.RawMessage, len(r.fields))
	for key, value := range r.fields {
		out[key] = append(json.RawMessage(nil), value...)
	}
	return out
}

// Compat derives the shared chat form for adapters without native Responses
// support. It may fail for input the chat form cannot carry; that failure is a
// statement about the compatibility path only, never about the request.
func (r *NativeResponsesRequest) Compat() (ResponsesTranslation, error) {
	var source ResponsesRequest
	if err := json.Unmarshal(r.raw, &source); err != nil {
		return ResponsesTranslation{}, err
	}
	return TranslateResponsesRequest(source)
}

// RequestedReasoningEffort is the client's reasoning.effort, empty when absent.
func (r *NativeResponsesRequest) RequestedReasoningEffort() string {
	var reasoning struct {
		Effort string `json:"effort"`
	}
	if value := r.fields["reasoning"]; jsonKind(value) == '{' {
		_ = json.Unmarshal(value, &reasoning)
	}
	return strings.TrimSpace(reasoning.Effort)
}

// SessionSeed is the sticky-routing anchor used when the request cannot be
// expressed in the chat form (the chat form's ContentSessionSeed is preferred
// whenever it exists, so accepted requests keep their existing affinity). It
// hashes only members that stay fixed across turns — model, instructions,
// tools, and the first user item — so appending turns never moves the seed.
// It is empty when there is no user item to anchor on.
func (r *NativeResponsesRequest) SessionSeed() string {
	firstUser := r.firstUserInput()
	if firstUser == "" {
		return ""
	}
	fingerprint, err := json.Marshal(struct {
		Model        string `json:"model"`
		Instructions string `json:"instructions,omitempty"`
		Tools        string `json:"tools,omitempty"`
		FirstUser    string `json:"first_user"`
	}{
		Model:        strings.ToLower(r.model),
		Instructions: compactSessionJSON(r.fields["instructions"]),
		Tools:        compactSessionJSON(r.fields["tools"]),
		FirstUser:    firstUser,
	})
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(append([]byte("responses-native\x00"), fingerprint...))
	return hex.EncodeToString(sum[:])
}

func (r *NativeResponsesRequest) firstUserInput() string {
	input := r.fields["input"]
	if jsonKind(input) == '"' {
		return compactSessionJSON(input)
	}
	var items []json.RawMessage
	if json.Unmarshal(input, &items) != nil {
		return ""
	}
	for _, item := range items {
		var probe struct {
			Type    string          `json:"type"`
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
		}
		if json.Unmarshal(item, &probe) != nil {
			continue
		}
		if (probe.Type == "" || probe.Type == "message") && strings.EqualFold(probe.Role, "user") {
			return compactSessionJSON(probe.Content)
		}
	}
	return ""
}

func isJSONNull(raw json.RawMessage) bool {
	return len(bytes.TrimSpace(raw)) == 0 || string(bytes.TrimSpace(raw)) == "null"
}

// jsonKind returns the first significant byte of a JSON value: '{', '[', '"',
// or another literal start; 0 when the value is absent or null.
func jsonKind(raw json.RawMessage) byte {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || string(trimmed) == "null" {
		return 0
	}
	return trimmed[0]
}
