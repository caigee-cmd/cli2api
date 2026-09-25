package codex

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/caigee-cmd/cli2api/internal/providers"
	"github.com/caigee-cmd/cli2api/internal/translate"
)

// ResponsesStream is the native Responses capability: the client's original
// Responses body is copied per attempt and adjusted only by the Codex upstream
// constraints in normalizeCodexUpstream. It is never rebuilt through the chat
// form, so item order, call linkage, namespaces, and unknown members survive.
// The response is the upstream Responses SSE body.
func (c *Client) ResponsesStream(ctx context.Context, accountID string, req *translate.NativeResponsesRequest, options providers.RequestOptions) (*http.Response, providers.ResolvedChat, error) {
	credential, err := c.credential(ctx, accountID)
	if err != nil {
		return nil, providers.ResolvedChat{}, err
	}
	sessionID := sessionIDFor(accountID)
	model := strings.TrimSpace(options.Model)
	if model == "" {
		model = req.Model()
	}
	body, resolved, err := buildNativeBody(req, options, reasoningCaps(model), sessionID)
	if err != nil {
		return nil, providers.ResolvedChat{}, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, ChatBase+pathResponses, bytes.NewReader(body))
	if err != nil {
		return nil, providers.ResolvedChat{}, err
	}
	SetChatHeaders(httpReq.Header, credential, sessionID, true)
	applyCodexRequestHeaders(httpReq.Header, model, usesResponsesLite(model))
	client, err := c.httpClient(ctx, accountID)
	if err != nil {
		return nil, providers.ResolvedChat{}, err
	}
	client.Timeout = 0
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, providers.ResolvedChat{}, err
	}
	c.observeQuotaHeaders(accountID, resp.Header)
	if resp.StatusCode >= 300 {
		errBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		resp.Body.Close()
		return nil, providers.ResolvedChat{}, classifiedError(resp.StatusCode, errBody)
	}
	return resp, resolved, nil
}

// reasoningCaps is the catalog capability set used for the reasoning clamp;
// shared by the chat and native builders so both clamp identically.
func reasoningCaps(model string) providers.ModelCapabilities {
	caps := capsFor(model)
	if len(caps.ReasoningOptions) == 0 {
		caps.Reasoning = true
		caps.ReasoningOptions = []string{"low", "medium", "high"}
	}
	return caps
}

// buildNativeBody derives one attempt's upstream body from the client's
// Responses request. It works on a private copy (req.Fields), so a failover
// attempt always starts from the original. Account policy is applied first,
// then normalizeCodexUpstream applies the backend constraints shared with the
// translated chat/messages path.
func buildNativeBody(req *translate.NativeResponsesRequest, options providers.RequestOptions, caps providers.ModelCapabilities, sessionID string) ([]byte, providers.ResolvedChat, error) {
	fields := req.Fields()
	model := strings.TrimSpace(options.Model)
	if model == "" {
		model = req.Model()
	}
	if options.DropSystemPrompt {
		fields["instructions"] = json.RawMessage(`""`)
		input, err := dropNativeSystemInput(fields["input"])
		if err != nil {
			return nil, providers.ResolvedChat{}, err
		}
		fields["input"] = input
	}
	resolved := providers.ResolvedChat{}
	if level := providers.ResolveReasoningLevel(req.RequestedReasoningEffort(), caps); level != "" {
		reasoning, err := withReasoningEffort(fields["reasoning"], level)
		if err != nil {
			return nil, providers.ResolvedChat{}, err
		}
		fields["reasoning"] = reasoning
		resolved.ReasoningLevel = level
	}
	if err := normalizeCodexUpstream(fields, model, usesResponsesLite(model), sessionID); err != nil {
		return nil, providers.ResolvedChat{}, err
	}
	body, err := json.Marshal(fields)
	return body, resolved, err
}

func dropNativeSystemInput(raw json.RawMessage) (json.RawMessage, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || trimmed[0] != '[' {
		return raw, nil
	}
	var items []json.RawMessage
	if err := json.Unmarshal(trimmed, &items); err != nil {
		return nil, err
	}
	kept := make([]json.RawMessage, 0, len(items))
	for _, item := range items {
		var probe struct {
			Type string `json:"type"`
			Role string `json:"role"`
		}
		if json.Unmarshal(item, &probe) == nil && (probe.Type == "" || probe.Type == "message") {
			if role := strings.ToLower(strings.TrimSpace(probe.Role)); role == "system" || role == "developer" {
				continue
			}
		}
		kept = append(kept, item)
	}
	return json.Marshal(kept)
}

// withReasoningEffort sets reasoning.effort and keeps every other member
// (summary, future fields) as the client sent it.
func withReasoningEffort(raw json.RawMessage, level string) (json.RawMessage, error) {
	members := map[string]json.RawMessage{}
	if !isNullJSON(raw) {
		if err := json.Unmarshal(raw, &members); err != nil {
			return nil, err
		}
	}
	members["effort"] = mustJSON(level)
	return json.Marshal(members)
}

func mustJSON(value string) json.RawMessage {
	encoded, _ := json.Marshal(value)
	return encoded
}

func isNullJSON(raw json.RawMessage) bool {
	trimmed := bytes.TrimSpace(raw)
	return len(trimmed) == 0 || string(trimmed) == "null"
}

func emptyJSONArray(raw json.RawMessage) bool {
	trimmed := bytes.TrimSpace(raw)
	return len(trimmed) == 0 || string(trimmed) == "null" || string(trimmed) == "[]"
}
