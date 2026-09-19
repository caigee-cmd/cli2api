package qoder

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/caigee-cmd/cli2api/internal/accounts"
	"github.com/caigee-cmd/cli2api/internal/providers"
	"github.com/caigee-cmd/cli2api/internal/translate"
)

// Client wraps S08 worker transport behind providers.Adapter slots.
// It still talks to a child-process worker URL; it is not an in-process
// cloud client. Callers must keep using the child-process path for
// probe/quota/login/chat until those slices are accepted.
type Client struct {
	mu sync.RWMutex

	locate      func(accountID string) (string, bool)
	proxyAPIKey func() string

	modelsHTTP *http.Client
	healthHTTP *http.Client
	quotaHTTP  *http.Client
	adminHTTP  *http.Client
	chatHTTP   *http.Client

	loginTimeout  time.Duration
	loginInterval time.Duration
}

func NewClient() *Client {
	return &Client{
		modelsHTTP:    &http.Client{Timeout: 15 * time.Second},
		healthHTTP:    &http.Client{Timeout: 2 * time.Second},
		quotaHTTP:     &http.Client{Timeout: 5 * time.Second},
		adminHTTP:     &http.Client{Timeout: 120 * time.Second},
		chatHTTP:      &http.Client{Timeout: 120 * time.Second},
		loginTimeout:  90 * time.Second,
		loginInterval: 200 * time.Millisecond,
	}
}

func (c *Client) Bind(locate func(string) (string, bool), proxyAPIKey func() string) {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.locate = locate
	c.proxyAPIKey = proxyAPIKey
	c.mu.Unlock()
}

func (c *Client) SetHTTP(httpClient *http.Client) {
	if c == nil || httpClient == nil {
		return
	}
	c.mu.Lock()
	c.modelsHTTP = httpClient
	c.healthHTTP = httpClient
	c.quotaHTTP = httpClient
	c.adminHTTP = httpClient
	c.chatHTTP = httpClient
	c.mu.Unlock()
}

func (c *Client) SetLoginWait(timeout, interval time.Duration) {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.loginTimeout = timeout
	c.loginInterval = interval
	c.mu.Unlock()
}

func (c *Client) Adapter() providers.Adapter {
	// Probe/Quota stay off the registered bundle in S09: empty-URL Qoder
	// items must not enter refreshInProcess just because an Adapter exists.
	// Callers that need those methods use the Client directly.
	return providers.Adapter{
		ID:      "qoder",
		Login:   c,
		Chat:    c,
		Models:  c,
		Checkin: c,
	}
}

func (c *Client) lookup(accountID string) (string, error) {
	c.mu.RLock()
	locate := c.locate
	c.mu.RUnlock()
	if locate == nil {
		return "", ErrAccountNotRunning
	}
	workerURL, ok := locate(accountID)
	workerURL = strings.TrimRight(strings.TrimSpace(workerURL), "/")
	if !ok || workerURL == "" {
		return "", ErrAccountNotRunning
	}
	return workerURL, nil
}

func (c *Client) key() string {
	c.mu.RLock()
	fn := c.proxyAPIKey
	c.mu.RUnlock()
	if fn == nil {
		return ""
	}
	return fn()
}

func (c *Client) worker(httpClient *http.Client) WorkerClient {
	return WorkerClient{HTTP: httpClient, ProxyAPIKey: c.key()}
}

func (c *Client) accountWorker(httpClient *http.Client, accountID string) WorkerClient {
	return WorkerClient{HTTP: httpClient, ProxyAPIKey: c.key(), AccountID: accountID}
}

func (c *Client) Models(ctx context.Context, accountID string) ([]providers.ModelInfo, error) {
	workerURL, err := c.lookup(accountID)
	if err != nil {
		return nil, err
	}
	c.mu.RLock()
	httpClient := c.modelsHTTP
	c.mu.RUnlock()
	entries, status, rawBody, err := c.worker(httpClient).Models(ctx, workerURL, false)
	if err != nil {
		return nil, err
	}
	if status >= 300 {
		snippet := strings.TrimSpace(rawBody)
		if len(snippet) > 512 {
			snippet = snippet[:512]
		}
		return nil, HTTPStatusError{Op: "models", Status: status, Body: snippet}
	}
	return ModelInfos(entries), nil
}

func ModelInfos(entries []map[string]any) []providers.ModelInfo {
	out := make([]providers.ModelInfo, 0, len(entries))
	for _, entry := range entries {
		if entry == nil {
			continue
		}
		publicID := stringField(entry, "id")
		native := stringField(entry, "mapped_key", "native_model")
		if native == "" {
			native = publicID
		}
		info := providers.ModelInfo{
			PublicModel: publicID,
			NativeModel: native,
			DisplayName: stringField(entry, "display_name"),
			Credits:     stringField(entry, "credits"),
			Free:        boolField(entry, "free"),
			Capabilities: providers.ModelCapabilities{
				ContextWindow: numberField(entry, "context_length"),
				Reasoning:     boolField(entry, "is_reasoning"),
				Tools:         true,
				Images:        true,
			},
		}
		if info.PublicModel == "" && info.NativeModel == "" {
			continue
		}
		out = append(out, info)
	}
	return out
}

func CatalogIDsFromInfos(models []providers.ModelInfo) []string {
	ids := make([]string, 0, len(models)*3)
	for _, model := range models {
		ids = append(ids, model.PublicModel, model.NativeModel, model.DisplayName)
	}
	return ids
}

func (c *Client) Probe(ctx context.Context, accountID string) (providers.AccountHealth, error) {
	workerURL, err := c.lookup(accountID)
	if err != nil {
		return providers.AccountHealth{}, err
	}
	c.mu.RLock()
	httpClient := c.healthHTTP
	c.mu.RUnlock()
	health, status, err := WorkerClient{HTTP: httpClient}.Health(ctx, workerURL)
	if err != nil {
		return providers.AccountHealth{}, err
	}
	ready := status < 300 && health.OK && health.Ready
	return providers.AccountHealth{
		Ready:     ready,
		Hot:       health.Hot,
		UID:       health.UID,
		InFlight:  health.InFlight,
		LastError: health.LastError,
	}, nil
}

func (c *Client) Quota(ctx context.Context, accountID string) (*providers.QuotaInfo, error) {
	snapshot, err := c.QuotaSnapshot(ctx, accountID, false)
	if err != nil || snapshot == nil {
		return nil, err
	}
	return &providers.QuotaInfo{
		Used:       snapshot.Used,
		Total:      snapshot.Total,
		Remaining:  snapshot.Remaining,
		Percentage: snapshot.Percentage,
		Unit:       snapshot.Unit,
		Exceeded:   snapshot.Exceeded,
		FetchedAt:  snapshot.FetchedAt,
	}, nil
}

func (c *Client) QuotaSnapshot(ctx context.Context, accountID string, force bool) (*accounts.QuotaSnapshot, error) {
	workerURL, err := c.lookup(accountID)
	if err != nil {
		return nil, err
	}
	c.mu.RLock()
	httpClient := c.quotaHTTP
	c.mu.RUnlock()
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 5 * time.Second}
	}
	return c.worker(httpClient).Quota(ctx, workerURL, force)
}

func (c *Client) StartLogin(ctx context.Context, accountID string) (providers.LoginSession, error) {
	c.mu.RLock()
	timeout := c.loginTimeout
	interval := c.loginInterval
	adminHTTP := c.adminHTTP
	locate := c.locate
	c.mu.RUnlock()
	workerURL, err := WaitForAuthManager(ctx, func() (string, bool) {
		if locate == nil {
			return "", false
		}
		return locate(accountID)
	}, timeout, interval)
	if err != nil {
		return providers.LoginSession{}, err
	}
	status, _, body, err := c.accountWorker(adminHTTP, accountID).Admin(ctx, workerURL, http.MethodPost, "/admin/login/device", "", nil)
	if err != nil {
		return providers.LoginSession{}, err
	}
	if status >= 300 {
		return providers.LoginSession{}, HTTPStatusError{Op: "login", Status: status, Body: strings.TrimSpace(string(body))}
	}
	var parsed struct {
		AuthURL string `json:"authUrl"`
	}
	_ = json.Unmarshal(body, &parsed)
	return providers.LoginSession{AuthURL: parsed.AuthURL}, nil
}

func (c *Client) PollLogin(ctx context.Context, accountID string) (bool, string, error) {
	workerURL, err := c.lookup(accountID)
	if err != nil {
		return false, "", err
	}
	c.mu.RLock()
	adminHTTP := c.adminHTTP
	c.mu.RUnlock()
	status, _, body, err := c.accountWorker(adminHTTP, accountID).Admin(ctx, workerURL, http.MethodGet, "/admin/login/status", "", nil)
	if err != nil {
		return false, "", err
	}
	if status >= 300 {
		return false, "", HTTPStatusError{Op: "login", Status: status, Body: strings.TrimSpace(string(body))}
	}
	var parsed struct {
		Login struct {
			Status  string `json:"status"`
			Message string `json:"message"`
		} `json:"login"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return false, "", err
	}
	return parsed.Login.Status == "ok", parsed.Login.Message, nil
}

func (c *Client) ChatNonStream(ctx context.Context, accountID string, req translate.ChatRequest) (providers.ChatOutcome, error) {
	httpReq, resolved, err := c.newChatRequest(ctx, accountID, req, false)
	if err != nil {
		return providers.ChatOutcome{}, err
	}
	c.mu.RLock()
	httpClient := c.chatHTTP
	c.mu.RUnlock()
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	resp, err := httpClient.Do(httpReq)
	if err != nil {
		return providers.ChatOutcome{}, TransportError{Err: err}
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 300 {
		return providers.ChatOutcome{}, HTTPStatusError{Op: "chat", Status: resp.StatusCode, Body: strings.TrimSpace(string(body))}
	}
	outcome, err := decodeChatOutcome(req.Model, body)
	if err != nil {
		return providers.ChatOutcome{}, err
	}
	outcome.ReasoningLevel = resolved.ReasoningLevel
	return outcome, nil
}

func (c *Client) ChatStream(ctx context.Context, accountID string, req translate.ChatRequest) (*http.Response, providers.ResolvedChat, error) {
	httpReq, resolved, err := c.newChatRequest(ctx, accountID, req, true)
	if err != nil {
		return nil, providers.ResolvedChat{}, err
	}
	c.mu.RLock()
	httpClient := c.chatHTTP
	c.mu.RUnlock()
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	if httpClient.Timeout > 0 {
		cloned := *httpClient
		cloned.Timeout = 0
		httpClient = &cloned
	}
	resp, err := httpClient.Do(httpReq)
	if err != nil {
		return nil, providers.ResolvedChat{}, TransportError{Err: err}
	}
	return resp, resolved, nil
}

func (c *Client) newChatRequest(ctx context.Context, accountID string, req translate.ChatRequest, stream bool) (*http.Request, providers.ResolvedChat, error) {
	workerURL, err := c.lookup(accountID)
	if err != nil {
		return nil, providers.ResolvedChat{}, err
	}
	payload, err := json.Marshal(BuildChatPayload(req, stream))
	if err != nil {
		return nil, providers.ResolvedChat{}, err
	}
	httpReq, err := NewChatRequest(ctx, workerURL, accountID, "", c.key(), payload)
	if err != nil {
		return nil, providers.ResolvedChat{}, err
	}
	return httpReq, providers.ResolvedChat{ReasoningLevel: resolvedReasoningLevel(req)}, nil
}

// resolvedReasoningLevel surfaces the reasoning level that qoder forwards to
// the worker. Qoder does not clamp; the worker applies its own model policy,
// so this is the normalized client value (or stored default) only.
func resolvedReasoningLevel(req translate.ChatRequest) string {
	if len(req.ReasoningEffort) > 0 {
		var value any
		if json.Unmarshal(req.ReasoningEffort, &value) == nil {
			switch typed := value.(type) {
			case string:
				return providers.NormalizeReasoningLevel(typed)
			case map[string]any:
				for _, key := range []string{"effort", "level", "type"} {
					if text, ok := typed[key].(string); ok {
						if level := providers.NormalizeReasoningLevel(text); level != "" {
							return level
						}
					}
				}
			}
		}
	}
	if req.EnableThinking != nil {
		if *req.EnableThinking {
			return "medium"
		}
		return "none"
	}
	if req.EnableReasoning != nil {
		if *req.EnableReasoning {
			return "medium"
		}
		return "none"
	}
	if req.IsReasoning != nil {
		if *req.IsReasoning {
			return "medium"
		}
		return "none"
	}
	return ""
}

func BuildChatPayload(req translate.ChatRequest, stream bool) map[string]any {
	payload := map[string]any{
		"model":    req.Model,
		"messages": req.Messages,
		"stream":   stream,
	}
	if len(req.MaxCompletionTokens) > 0 {
		payload["max_tokens"] = req.MaxCompletionTokens
	} else if len(req.MaxTokens) > 0 {
		payload["max_tokens"] = req.MaxTokens
	}
	if len(req.Temperature) > 0 {
		payload["temperature"] = json.RawMessage(req.Temperature)
	}
	if len(req.TopP) > 0 {
		payload["top_p"] = json.RawMessage(req.TopP)
	}
	if len(req.Stop) > 0 {
		payload["stop"] = json.RawMessage(req.Stop)
	}
	if req.ParallelToolCalls != nil {
		payload["parallel_tool_calls"] = *req.ParallelToolCalls
	}
	if len(req.ResponseFormat) > 0 {
		payload["response_format"] = json.RawMessage(req.ResponseFormat)
	}
	if req.IsReasoning != nil {
		payload["is_reasoning"] = *req.IsReasoning
	}
	if req.EnableThinking != nil {
		payload["enable_thinking"] = *req.EnableThinking
	}
	if req.EnableReasoning != nil {
		payload["enable_reasoning"] = *req.EnableReasoning
	}
	if len(req.Thinking) > 0 {
		payload["thinking"] = json.RawMessage(req.Thinking)
	}
	if len(req.ReasoningEffort) > 0 {
		payload["reasoning_effort"] = json.RawMessage(req.ReasoningEffort)
	}
	if len(req.ReasoningBudgetTokens) > 0 {
		payload["reasoning_budget_tokens"] = json.RawMessage(req.ReasoningBudgetTokens)
	}
	if len(req.ContextLength) > 0 {
		payload["context_length"] = json.RawMessage(req.ContextLength)
	}
	if len(req.MaxInputTokens) > 0 {
		payload["max_input_tokens"] = json.RawMessage(req.MaxInputTokens)
	}
	if len(req.Tools) > 0 {
		payload["tools"] = json.RawMessage(req.Tools)
	}
	if len(req.ToolChoice) > 0 {
		payload["tool_choice"] = json.RawMessage(req.ToolChoice)
	}
	return payload
}

func decodeChatOutcome(fallbackModel string, body []byte) (providers.ChatOutcome, error) {
	var parsed struct {
		Model string `json:"model"`
		Usage struct {
			PromptTokens     int      `json:"prompt_tokens"`
			CompletionTokens int      `json:"completion_tokens"`
			CacheReadTokens  *int     `json:"cache_read_tokens"`
			CacheWriteTokens *int     `json:"cache_write_tokens"`
			Source           string   `json:"source"`
			Credits          *float64 `json:"credits"`
		} `json:"usage"`
		Choices []struct {
			FinishReason string `json:"finish_reason"`
			Message      struct {
				Content          string          `json:"content"`
				ReasoningContent string          `json:"reasoning_content"`
				ToolCalls        json.RawMessage `json:"tool_calls"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return providers.ChatOutcome{}, fmt.Errorf("decode worker response: %w", err)
	}
	outcome := providers.ChatOutcome{
		Model:            parsed.Model,
		PromptTokens:     parsed.Usage.PromptTokens,
		CompletionTokens: parsed.Usage.CompletionTokens,
		CacheReadTokens:  parsed.Usage.CacheReadTokens,
		CacheWriteTokens: parsed.Usage.CacheWriteTokens,
		UsageSource:      parsed.Usage.Source,
		Credits:          parsed.Usage.Credits,
		FinishReason:     "stop",
	}
	if outcome.Model == "" {
		outcome.Model = fallbackModel
	}
	if outcome.UsageSource == "" {
		outcome.UsageSource = "estimate"
	}
	if len(parsed.Choices) > 0 {
		outcome.Content = parsed.Choices[0].Message.Content
		outcome.Reasoning = parsed.Choices[0].Message.ReasoningContent
		outcome.ToolCalls = parsed.Choices[0].Message.ToolCalls
		if parsed.Choices[0].FinishReason != "" {
			outcome.FinishReason = parsed.Choices[0].FinishReason
		} else if len(outcome.ToolCalls) > 0 && string(outcome.ToolCalls) != "null" {
			outcome.FinishReason = "tool_calls"
		}
	}
	return outcome, nil
}

func stringField(entry map[string]any, keys ...string) string {
	for _, key := range keys {
		value, _ := entry[key].(string)
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func boolField(entry map[string]any, key string) bool {
	value, _ := entry[key].(bool)
	return value
}

func numberField(entry map[string]any, key string) int {
	switch value := entry[key].(type) {
	case float64:
		return int(value)
	case int:
		return value
	case json.Number:
		n, _ := value.Int64()
		return int(n)
	default:
		return 0
	}
}
