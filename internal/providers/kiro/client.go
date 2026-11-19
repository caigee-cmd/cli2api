package kiro

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"

	"github.com/caigee-cmd/cli2api/internal/accounts"
	"github.com/caigee-cmd/cli2api/internal/providers"
	proxyutil "github.com/caigee-cmd/cli2api/internal/proxy"
	"github.com/caigee-cmd/cli2api/internal/translate"
	"github.com/google/uuid"
)

// Store is the persistence surface. The adapter never imports internal/store.
type Store interface {
	Get(ctx context.Context, id string) (accounts.Account, error)
	LoadCredentialPayload(ctx context.Context, accountID string) (string, []byte, error)
	SaveCredentialPayload(ctx context.Context, accountID, format string, payload []byte) error
	Observe(ctx context.Context, id, remoteUID, status, lastError, lastKind string) error
	// SetCredentialAuthType records whether the stored credential came from a
	// browser sign-in or a pasted key. A store that does not implement it still
	// keeps the credential; the account card falls back to its default label.
	SetCredentialAuthType(ctx context.Context, accountID, authType string) error
}

// SecretReader is optional. Missing it means no global proxy.
type SecretReader interface {
	GetSecret(context.Context, string) (string, bool, error)
}

// Client calls Kiro CreateResponse for one stored credential.
type Client struct {
	store      Store
	http       *http.Client
	transports proxyutil.TransportCache

	mu     sync.Mutex
	models []providers.ModelInfo
	logins map[string]*loginState
}

func NewClient(store Store) *Client {
	return &Client{store: store, http: &http.Client{}}
}

func (c *Client) Adapter() providers.Adapter {
	return providers.Adapter{
		ID:           "kiro",
		Credential:   credentialCodec{},
		Login:        c,
		Chat:         c,
		Models:       c,
		Classifier:   classifier{},
		ImportExport: importer{store: c.store},
		Prober:       c,
	}
}

type credentialCodec struct{}

func (credentialCodec) Validate(payload []byte) error { return ValidateCredential(payload) }
func (credentialCodec) Format() string                { return CredentialFormat }

func (credentialCodec) SplitImport(payload []byte) ([][]byte, error) {
	return splitCredentialImport(payload)
}

func (credentialCodec) PrepareImport(payload []byte) (providers.CredentialImport, error) {
	chunks, err := splitCredentialImport(payload)
	if err != nil {
		return providers.CredentialImport{}, err
	}
	if len(chunks) != 1 {
		return providers.CredentialImport{}, fmt.Errorf("kiro credential package has %d accounts", len(chunks))
	}
	return prepareCredentialImport(chunks[0])
}

func prepareCredentialImport(payload []byte) (providers.CredentialImport, error) {
	if err := ValidateCredential(payload); err != nil {
		return providers.CredentialImport{}, err
	}
	cred, err := DecodeCredential(payload)
	if err != nil {
		return providers.CredentialImport{}, err
	}
	encoded, err := cred.Encode()
	if err != nil {
		return providers.CredentialImport{}, err
	}
	return providers.CredentialImport{
		Payload: encoded, Ready: cred.Ready(),
		Region: cred.importRegion(credentialStatesRegion(payload)),
		Name:   cred.importName(),
	}, nil
}

type classifier struct{}

func (classifier) Classify(status int, body string) providers.ClassifiedError {
	return Classify(status, body)
}

type importer struct{ store Store }

func (importer) ValidateImport(payload []byte) error { return ValidateCredential(payload) }

func (i importer) Export(ctx context.Context, accountID string) (map[string]any, error) {
	_, payload, err := i.store.LoadCredentialPayload(ctx, accountID)
	if err != nil {
		return nil, err
	}
	cred, err := DecodeCredential(payload)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"format": CredentialFormat, "auth_method": cred.AuthMethod, "region": cred.Region,
		"email": cred.Email, "profile_arn": cred.ProfileArn,
	}, nil
}

func (c *Client) credential(ctx context.Context, accountID string) (Credential, error) {
	_, payload, err := c.store.LoadCredentialPayload(ctx, accountID)
	if err != nil {
		return Credential{}, err
	}
	cred, err := DecodeCredential(payload)
	if err != nil {
		return Credential{}, err
	}
	if !cred.Ready() && !cred.needsRefresh() {
		return cred, fmt.Errorf("kiro credential incomplete")
	}
	return c.refreshCredential(ctx, accountID, cred)
}

func (c *Client) httpClient(ctx context.Context, accountID string) (*http.Client, error) {
	raw, err := c.proxyFor(ctx, accountID)
	if err != nil {
		return nil, err
	}
	client := *c.http
	transport, err := c.transports.Get(raw)
	if err != nil {
		return nil, err
	}
	if transport != nil {
		client.Transport = transport
	}
	return &client, nil
}

func (c *Client) proxyFor(ctx context.Context, accountID string) (string, error) {
	if strings.TrimSpace(accountID) != "" {
		account, err := c.store.Get(ctx, accountID)
		if err != nil {
			return "", err
		}
		if value := strings.TrimSpace(account.ProxyURL); value != "" {
			return value, nil
		}
	}
	reader, ok := c.store.(SecretReader)
	if !ok {
		return "", nil
	}
	value, found, err := reader.GetSecret(ctx, "proxy_url")
	if err != nil || !found {
		return "", err
	}
	return strings.TrimSpace(value), nil
}

// Probe refreshes the credential when it is due. Readiness does not depend on
// the model catalog.
func (c *Client) Probe(ctx context.Context, accountID string) (providers.AccountHealth, error) {
	cred, err := c.credential(ctx, accountID)
	if err != nil {
		return providers.AccountHealth{LastError: err.Error()}, nil
	}
	uid := firstNonEmpty(cred.UserID, cred.Email, cred.ProfileArn)
	_ = c.store.Observe(ctx, accountID, uid, "ready", "", "")
	return providers.AccountHealth{Ready: true, Hot: true, UID: uid}, nil
}

func (c *Client) Models(ctx context.Context, accountID string) ([]providers.ModelInfo, error) {
	cred, err := c.credential(ctx, accountID)
	if err != nil {
		return c.cachedOrDefault(), nil
	}
	client, err := c.httpClient(ctx, accountID)
	if err != nil {
		if cached := c.cachedModels(); len(cached) > 0 {
			return cached, nil
		}
		return c.cachedOrDefault(), nil
	}
	ensureIDEVersion(client)
	// The model list is gated on the advertised IDE build. Wait for one feed
	// read so the first catalog is not taken with the fallback version.
	refreshIDEVersion(client)
	models, err := c.listModels(ctx, client, cred)
	if err != nil || len(models) == 0 {
		// A failed live list must not be replaced by the short fallback. The
		// caller keeps the previous catalog; an empty result stays unknown
		// instead of advertising a handful of models the account may not have.
		if cached := c.cachedModels(); len(cached) > 0 {
			return cached, nil
		}
		return nil, err
	}
	models = withAdvertisedModels(models)
	c.mu.Lock()
	c.models = models
	c.mu.Unlock()
	return models, nil
}

func (c *Client) cachedModels() []providers.ModelInfo {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.models) == 0 {
		return nil
	}
	return append([]providers.ModelInfo(nil), c.models...)
}

func (c *Client) cachedOrDefault() []providers.ModelInfo {
	if cached := c.cachedModels(); len(cached) > 0 {
		return cached
	}
	return defaultModels()
}

func (c *Client) ChatNonStream(ctx context.Context, accountID string, req translate.ChatRequest) (providers.ChatOutcome, error) {
	resp, resolved, err := c.ChatStream(ctx, accountID, req)
	if err != nil {
		return providers.ChatOutcome{}, err
	}
	defer resp.Body.Close()
	return collectOutcome(resp.Body, resolved.ReasoningLevel)
}

func (c *Client) ChatStream(ctx context.Context, accountID string, req translate.ChatRequest) (*http.Response, providers.ResolvedChat, error) {
	model := resolveModel(req.Model)
	if model == "" {
		return nil, providers.ResolvedChat{}, fmt.Errorf("kiro model required")
	}
	cred, err := c.credential(ctx, accountID)
	if err != nil {
		return nil, providers.ResolvedChat{}, err
	}
	payload, err := buildConversation(req, cred, model)
	if err != nil {
		return nil, providers.ResolvedChat{}, err
	}
	client, err := c.httpClient(ctx, accountID)
	if err != nil {
		return nil, providers.ResolvedChat{}, err
	}
	ensureIDEVersion(client)
	resolved := providers.ResolvedChat{ReasoningLevel: payload.Level}
	if c.useResponses(ctx, accountID, cred) {
		body, err := c.postResponses(ctx, client, cred, payload)
		if err != nil {
			return nil, providers.ResolvedChat{}, err
		}
		return writeOpenAIStream(body, model, payload.clientNames(), armMarkupFilter(payload.Tools)), resolved, nil
	}
	body, kind, err := c.postLegacy(ctx, client, accountID, cred, payload)
	if err != nil {
		return nil, providers.ResolvedChat{}, err
	}
	if kind == streamResponses {
		return writeOpenAIStream(body, model, payload.clientNames(), armMarkupFilter(payload.Tools)), resolved, nil
	}
	return writeEventStream(body, model, payload.clientNames(), armMarkupFilter(payload.Tools)), resolved, nil
}

func (c *Client) postResponses(ctx context.Context, client *http.Client, cred Credential, payload conversation) (io.ReadCloser, error) {
	includeReasoning := payload.replaysReasoning()
	narrated := false
	for attempt := 0; attempt < 3; attempt++ {
		body, err := json.Marshal(payload.responsesBody(includeReasoning))
		if err != nil {
			return nil, err
		}
		endpoint := chatBaseFor(cred) + "/v1/responses"
		resp, raw, err := c.postOnce(ctx, client, endpoint, body, func(req *http.Request) {
			applyResponsesHeaders(req, cred, attempt+1)
		})
		if err != nil {
			return nil, err
		}
		if resp.StatusCode < 300 {
			return resp.Body, nil
		}
		resp.Body.Close()
		noteVersionGate(client, string(raw))
		// A signature Kiro does not accept fails every later turn. Retry once
		// with the thinking kept as text, which is how the legacy path sends it.
		if includeReasoning && badRequest(resp.StatusCode) {
			includeReasoning = false
			continue
		}
		// Structured tool history Kiro rejects fails before any stream output, so
		// one retry with the calls narrated as text cannot duplicate a reply.
		if !narrated && badRequest(resp.StatusCode) && flattenToolHistory(&payload) {
			narrated = true
			continue
		}
		return nil, newProviderError(resp.StatusCode, string(raw))
	}
	return nil, fmt.Errorf("kiro responses request failed")
}

func badRequest(status int) bool {
	return status == http.StatusBadRequest || status == http.StatusUnprocessableEntity
}

type streamKind int

const (
	streamLegacy streamKind = iota
	streamResponses
)

func (c *Client) postLegacy(ctx context.Context, client *http.Client, accountID string, cred Credential, payload conversation) (io.ReadCloser, streamKind, error) {
	endpoints := legacyEndpoints(cred)
	var last error
	narrated := false
	for _, endpoint := range endpoints {
		body := payload.legacyBody(endpoint.Origin, cred.isAPIKey())
		encoded, err := json.Marshal(body)
		if err != nil {
			return nil, 0, err
		}
		for attempt := 1; attempt <= legacyAttempts; attempt++ {
			resp, raw, err := c.postOnce(ctx, client, endpoint.URL, encoded, func(req *http.Request) {
				applyLegacyHeaders(req, cred, endpoint, attempt)
			})
			if err != nil {
				if ctx.Err() != nil {
					return nil, 0, ctx.Err()
				}
				last = err
				break
			}
			if resp.StatusCode < 300 {
				return resp.Body, streamLegacy, nil
			}
			resp.Body.Close()
			noteVersionGate(client, string(raw))
			last = newProviderError(resp.StatusCode, string(raw))
			// One 400 from structured tool history is retried on this same
			// endpoint with the calls narrated. Recursing would repeat the
			// rejection on every legacy host.
			if !narrated && badRequest(resp.StatusCode) && flattenToolHistory(&payload) {
				narrated = true
				body = payload.legacyBody(endpoint.Origin, cred.isAPIKey())
				if encoded, err = json.Marshal(body); err != nil {
					return nil, 0, err
				}
				retry, retryRaw, retryErr := c.postOnce(ctx, client, endpoint.URL, encoded, func(req *http.Request) {
					applyLegacyHeaders(req, cred, endpoint, attempt)
				})
				if retryErr != nil {
					return nil, 0, retryErr
				}
				if retry.StatusCode < 300 {
					return retry.Body, streamLegacy, nil
				}
				retry.Body.Close()
				last = newProviderError(retry.StatusCode, string(retryRaw))
				break
			}
			if resp.StatusCode == http.StatusTooManyRequests && strings.Contains(strings.ToLower(string(raw)), "user_request_rate_exceeded") {
				if c.recheckResponses(ctx, accountID, cred) {
					body, err := c.postResponses(ctx, client, cred, payload)
					return body, streamResponses, err
				}
			}
			if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusPaymentRequired {
				return nil, 0, last
			}
			break
		}
	}
	if last == nil {
		last = fmt.Errorf("kiro upstream request failed")
	}
	return nil, 0, last
}

func (c *Client) postOnce(ctx context.Context, client *http.Client, endpoint string, body []byte, headers func(*http.Request)) (*http.Response, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, nil, err
	}
	headers(req)
	resp, err := client.Do(req)
	if err != nil {
		return nil, nil, err
	}
	if resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, maxUpstreamErrorBytes))
		return resp, raw, nil
	}
	return resp, nil, nil
}

func applyLegacyHeaders(req *http.Request, cred Credential, endpoint legacyEndpoint, attempt int) {
	host := ""
	if req.URL != nil {
		host = req.URL.Host
	}
	if token := cred.bearer(); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if kind := cred.tokenType(); kind != "" {
		req.Header.Set("tokentype", kind)
	}
	req.Header.Set("Accept", "*/*")
	req.Header.Set("User-Agent", sdkUserAgent(cred, "codewhispererstreaming", StreamingSDKVersion, "m/E"))
	req.Header.Set("x-amz-user-agent", amzUserAgent(cred, StreamingSDKVersion))
	if endpoint.AmzTarget != "" {
		req.Header.Set("X-Amz-Target", endpoint.AmzTarget)
	}
	if endpoint.JSON10 {
		req.Header.Set("Content-Type", "application/x-amz-json-1.0")
		req.Header.Set("x-amzn-codewhisperer-optout", "false")
	} else {
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("x-amzn-kiro-agent-mode", AgentMode)
		req.Header.Set("x-amzn-codewhisperer-optout", "true")
	}
	req.Header.Set("Amz-Sdk-Request", fmt.Sprintf("attempt=%d; max=%d", attempt, legacyAttempts))
	req.Header.Set("Amz-Sdk-Invocation-Id", uuid.New().String())
	if host != "" {
		req.Host = host
	}
}

// writeEventStream reads a generateAssistantResponse body and writes the same
// OpenAI chunks CreateResponse produces.
func writeEventStream(body io.ReadCloser, model string, names map[string]string, filter *markupFilter) *http.Response {
	pr, pw := io.Pipe()
	go func() {
		defer body.Close()
		defer pw.Close()
		writeConsumed(pw, model, eventStreamSource(body), names, filter)
	}()
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(pr),
	}
}

func collectOutcome(body io.Reader, level string) (providers.ChatOutcome, error) {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64<<10), maxResponsesSSELine)
	var content, reasoning strings.Builder
	type call struct {
		id, name, args string
	}
	var calls []call
	finish := "stop"
	var prompt, completion int
	var credits float64
	sawCredits := false
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		data, ok := strings.CutPrefix(line, "data:")
		if !ok {
			continue
		}
		data = strings.TrimSpace(data)
		if data == "" || data == "[DONE]" {
			continue
		}
		var chunk struct {
			Choices []struct {
				Delta struct {
					Content          string `json:"content"`
					ReasoningContent string `json:"reasoning_content"`
					ToolCalls        []struct {
						ID       string `json:"id"`
						Function struct {
							Name      string `json:"name"`
							Arguments string `json:"arguments"`
						} `json:"function"`
					} `json:"tool_calls"`
				} `json:"delta"`
				FinishReason string `json:"finish_reason"`
			} `json:"choices"`
			Usage struct {
				PromptTokens     int      `json:"prompt_tokens"`
				CompletionTokens int      `json:"completion_tokens"`
				Credits          *float64 `json:"credits"`
				Source           string   `json:"source"`
			} `json:"usage"`
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			return providers.ChatOutcome{}, err
		}
		if chunk.Error.Message != "" {
			return providers.ChatOutcome{}, newProviderError(502, chunk.Error.Message)
		}
		if chunk.Usage.PromptTokens > 0 || chunk.Usage.CompletionTokens > 0 || chunk.Usage.Credits != nil {
			prompt, completion = chunk.Usage.PromptTokens, chunk.Usage.CompletionTokens
			if chunk.Usage.Credits != nil {
				credits, sawCredits = *chunk.Usage.Credits, true
			}
		}
		if len(chunk.Choices) == 0 {
			continue
		}
		choice := chunk.Choices[0]
		content.WriteString(choice.Delta.Content)
		reasoning.WriteString(choice.Delta.ReasoningContent)
		if choice.FinishReason != "" {
			finish = choice.FinishReason
		}
		for _, tool := range choice.Delta.ToolCalls {
			calls = append(calls, call{id: tool.ID, name: tool.Function.Name, args: tool.Function.Arguments})
		}
	}
	if err := scanner.Err(); err != nil {
		return providers.ChatOutcome{}, err
	}
	outcome := providers.ChatOutcome{
		Content: content.String(), Reasoning: reasoning.String(), FinishReason: finish,
		PromptTokens: prompt, CompletionTokens: completion, ReasoningLevel: level,
	}
	if sawCredits {
		value := credits
		outcome.Credits = &value
		outcome.UsageSource = "provider"
	}
	if len(calls) > 0 {
		raw := make([]map[string]any, 0, len(calls))
		for _, tool := range calls {
			if tool.name == "" {
				continue
			}
			raw = append(raw, map[string]any{"id": tool.id, "type": "function", "function": map[string]any{"name": tool.name, "arguments": tool.args}})
		}
		encoded, err := json.Marshal(raw)
		if err != nil {
			return providers.ChatOutcome{}, err
		}
		outcome.ToolCalls = encoded
	}
	return outcome, nil
}

// withAdvertisedModels keeps every model ListAvailableModels returned and adds
// the older client names upstream accepts on top. A name the live list already
// has is not duplicated.
func withAdvertisedModels(live []providers.ModelInfo) []providers.ModelInfo {
	seen := make(map[string]struct{}, len(live))
	for _, model := range live {
		seen[strings.ToLower(model.PublicModel)] = struct{}{}
	}
	out := append([]providers.ModelInfo(nil), live...)
	for _, model := range defaultModels() {
		if _, ok := seen[strings.ToLower(model.PublicModel)]; ok {
			continue
		}
		out = append(out, model)
	}
	return out
}

func defaultModels() []providers.ModelInfo {
	// Shown only before the first successful ListAvailableModels. The live list
	// replaces this. Names match what Kiro currently serves plus the aliases
	// upstream exposes for older client model ids.
	names := []string{
		"auto",
		"claude-sonnet-5", "claude-sonnet-4.5", "claude-sonnet-4",
		"claude-haiku-4.5",
		"claude-opus-5", "claude-opus-4.8", "claude-opus-4.7", "claude-opus-4.6", "claude-opus-4.5",
		"claude-3-5-sonnet", "claude-3-sonnet", "claude-3-opus", "claude-3-haiku",
		"claude-sonnet-4-20250514",
		"deepseek-3.2",
	}
	models := make([]providers.ModelInfo, 0, len(names))
	for _, name := range names {
		models = append(models, providers.ModelInfo{
			NativeModel: name, PublicModel: name, DisplayName: name,
			Capabilities: providers.ModelCapabilities{
				Tools: true, Images: true, Reasoning: true,
				ReasoningOptions: []string{"low", "medium", "high"}, ReasoningDefault: "medium",
			},
		})
	}
	return models
}
