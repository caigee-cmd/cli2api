package meta

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strings"

	"github.com/caigee-cmd/cli2api/internal/providers"
	"github.com/caigee-cmd/cli2api/internal/translate"
)

func (c *Client) chatRequest(ctx context.Context, credential Credential, req translate.ChatRequest, accountID string) (*http.Request, providers.ResolvedChat, error) {
	model := strings.TrimSpace(req.Model)
	caps := capsFor(model)
	if stored := c.storedReasoning(ctx, model); stored != "" && len(req.ReasoningEffort) == 0 {
		req.ReasoningEffort = []byte(`"` + stored + `"`)
	}
	body, resolved, err := buildBody(req, caps)
	if err != nil {
		return nil, providers.ResolvedChat{}, err
	}
	base := credential.ChatBaseURL()
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, base+ResponsesPath, bytes.NewReader(body))
	if err != nil {
		return nil, providers.ResolvedChat{}, err
	}
	setChatHeaders(httpReq.Header, credential.Bearer())
	_ = accountID
	return httpReq, resolved, nil
}

func (c *Client) ChatNonStream(ctx context.Context, accountID string, req translate.ChatRequest) (providers.ChatOutcome, error) {
	credential, err := c.credential(ctx, accountID)
	if err != nil {
		return providers.ChatOutcome{}, err
	}
	httpReq, resolved, err := c.chatRequest(ctx, credential, req, accountID)
	if err != nil {
		return providers.ChatOutcome{}, err
	}
	client, err := c.httpClient(ctx, accountID)
	if err != nil {
		return providers.ChatOutcome{}, err
	}
	client.Timeout = 0
	resp, err := client.Do(httpReq)
	if err != nil {
		return providers.ChatOutcome{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		return providers.ChatOutcome{}, classifiedError(resp.StatusCode, body)
	}
	outcome, err := aggregate(resp.Body, req.Model)
	if err != nil {
		return providers.ChatOutcome{}, err
	}
	outcome.ReasoningLevel = resolved.ReasoningLevel
	return outcome, nil
}

func (c *Client) ChatStream(ctx context.Context, accountID string, req translate.ChatRequest) (*http.Response, providers.ResolvedChat, error) {
	credential, err := c.credential(ctx, accountID)
	if err != nil {
		return nil, providers.ResolvedChat{}, err
	}
	httpReq, resolved, err := c.chatRequest(ctx, credential, req, accountID)
	if err != nil {
		return nil, providers.ResolvedChat{}, err
	}
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
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		resp.Body.Close()
		return nil, providers.ResolvedChat{}, classifiedError(resp.StatusCode, body)
	}
	stream, err := rewriteStream(resp.Body, req.Model)
	if err != nil {
		resp.Body.Close()
		return nil, providers.ResolvedChat{}, err
	}
	resp.Body = &responseBody{Reader: stream.Reader, closer: resp.Body}
	return resp, resolved, nil
}
