package meta

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

// ResponsesStream sends the client's original Responses body to api.meta.ai
// after encrypted_content shape validation/passthrough.
func (c *Client) ResponsesStream(ctx context.Context, accountID string, req *translate.NativeResponsesRequest, options providers.RequestOptions) (*http.Response, providers.ResolvedChat, error) {
	credential, err := c.credential(ctx, accountID)
	if err != nil {
		return nil, providers.ResolvedChat{}, err
	}
	model := strings.TrimSpace(options.Model)
	if model == "" {
		model = req.Model()
	}
	body, resolved, err := buildNativeBody(req, options, capsFor(model))
	if err != nil {
		return nil, providers.ResolvedChat{}, err
	}
	base := credential.ChatBaseURL()
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, base+ResponsesPath, bytes.NewReader(body))
	if err != nil {
		return nil, providers.ResolvedChat{}, err
	}
	setChatHeaders(httpReq.Header, credential.Bearer())
	client, err := c.httpClient(ctx, accountID)
	if err != nil {
		return nil, providers.ResolvedChat{}, err
	}
	client.Timeout = 0
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, providers.ResolvedChat{}, err
	}
	if resp.StatusCode >= 300 {
		errBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		resp.Body.Close()
		return nil, providers.ResolvedChat{}, classifiedError(resp.StatusCode, errBody)
	}
	return resp, resolved, nil
}

func buildNativeBody(req *translate.NativeResponsesRequest, options providers.RequestOptions, caps providers.ModelCapabilities) ([]byte, providers.ResolvedChat, error) {
	fields := req.Fields()
	model := strings.TrimSpace(options.Model)
	if model == "" {
		model = req.Model()
	}
	if model != "" {
		encoded, _ := json.Marshal(model)
		fields["model"] = encoded
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
	// Always stream for the relay path.
	fields["stream"] = json.RawMessage(`true`)
	body, err := json.Marshal(fields)
	if err != nil {
		return nil, providers.ResolvedChat{}, err
	}
	return sanitizeEncryptedContent(body), resolved, nil
}

func dropNativeSystemInput(raw json.RawMessage) (json.RawMessage, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return raw, nil
	}
	var items []json.RawMessage
	if err := json.Unmarshal(raw, &items); err != nil {
		return raw, nil
	}
	kept := items[:0]
	for _, item := range items {
		var obj map[string]json.RawMessage
		if json.Unmarshal(item, &obj) != nil {
			kept = append(kept, item)
			continue
		}
		role := strings.Trim(string(obj["role"]), `"`)
		typ := strings.Trim(string(obj["type"]), `"`)
		if typ == "message" && (role == "system" || role == "developer") {
			continue
		}
		kept = append(kept, item)
	}
	return json.Marshal(kept)
}

func withReasoningEffort(raw json.RawMessage, level string) (json.RawMessage, error) {
	obj := map[string]any{}
	if len(raw) > 0 && string(raw) != "null" {
		_ = json.Unmarshal(raw, &obj)
	}
	obj["effort"] = level
	return json.Marshal(obj)
}
