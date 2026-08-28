package workbuddy

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/caigee-cmd/cli2api/internal/accounts"
	"github.com/caigee-cmd/cli2api/internal/providers"
	"github.com/caigee-cmd/cli2api/internal/translate"
)

// Store is the persistence surface the adapter needs. It matches
// *accounts.Store without importing the concrete manager.
type Store interface {
	Get(ctx context.Context, id string) (accounts.Account, error)
	LoadCredentialPayload(ctx context.Context, accountID string) (string, []byte, error)
	SaveCredentialPayload(ctx context.Context, accountID, format string, payload []byte) error
	Observe(ctx context.Context, id, remoteUID, status, lastError, lastKind string) error
}

type Client struct {
	store Store
	http  *http.Client

	mu          sync.Mutex
	loginStates map[string]string
}

func NewClient(store Store) *Client {
	return &Client{
		store:       store,
		http:        &http.Client{Timeout: 120 * time.Second},
		loginStates: map[string]string{},
	}
}

type envelope struct {
	Code int             `json:"code"`
	Msg  string          `json:"msg"`
	Data json.RawMessage `json:"data"`
}

func (c *Client) do(ctx context.Context, method, rawURL string, body []byte, setHeaders func(http.Header)) ([]byte, int, error) {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, rawURL, reader)
	if err != nil {
		return nil, 0, err
	}
	if setHeaders != nil {
		setHeaders(req.Header)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	payload, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, resp.StatusCode, err
	}
	return payload, resp.StatusCode, nil
}

// StartLogin requests a server-issued state and returns the browser URL.
func (c *Client) StartLogin(ctx context.Context, accountID string) (providers.LoginSession, error) {
	base := ChatBaseCN
	if account, err := c.store.Get(ctx, accountID); err == nil && account.ProviderRegion == "global" {
		base = ChatBaseGlobal
	}
	body, status, err := c.do(ctx, http.MethodPost, base+pathAuthState+"?platform=CLI", []byte("{}"),
		func(h http.Header) { setCommonHeaders(h, base == ChatBaseGlobal) })
	if err != nil {
		return providers.LoginSession{}, err
	}
	if status >= 300 {
		return providers.LoginSession{}, fmt.Errorf("auth state status=%d: %s", status, string(body))
	}
	var env envelope
	if err := json.Unmarshal(body, &env); err != nil || env.Code != 0 {
		return providers.LoginSession{}, fmt.Errorf("auth state failed: code=%d msg=%s", env.Code, env.Msg)
	}
	var data struct {
		State   string `json:"state"`
		AuthURL string `json:"authUrl"`
	}
	if err := json.Unmarshal(env.Data, &data); err != nil || data.State == "" {
		return providers.LoginSession{}, fmt.Errorf("auth state response missing state")
	}
	c.mu.Lock()
	c.loginStates[accountID] = data.State
	c.mu.Unlock()
	return providers.LoginSession{AuthURL: data.AuthURL, State: data.State}, nil
}

// PollLogin exchanges the state for tokens and account identity, then stores
// the canonical credential payload.
func (c *Client) PollLogin(ctx context.Context, accountID string) (bool, string, error) {
	c.mu.Lock()
	state := c.loginStates[accountID]
	c.mu.Unlock()
	if state == "" {
		return false, "", fmt.Errorf("login not started for account %s", accountID)
	}
	base := ChatBaseCN
	if account, err := c.store.Get(ctx, accountID); err == nil && account.ProviderRegion == "global" {
		base = ChatBaseGlobal
	}
	tokenBody, status, err := c.do(ctx, http.MethodGet, base+pathAuthToken+"?state="+url.QueryEscape(state), nil,
		func(h http.Header) { setCommonHeaders(h, base == ChatBaseGlobal) })
	if err != nil {
		return false, "", err
	}
	var tokenEnv envelope
	_ = json.Unmarshal(tokenBody, &tokenEnv)
	if status >= 500 {
		return false, "", fmt.Errorf("token endpoint status=%d", status)
	}
	if status >= 300 || tokenEnv.Code != 0 {
		return false, "waiting for authorization", nil
	}
	var token struct {
		AccessToken  string `json:"accessToken"`
		RefreshToken string `json:"refreshToken"`
		ExpiresIn    int64  `json:"expiresIn"`
		Domain       string `json:"domain"`
	}
	if err := json.Unmarshal(tokenEnv.Data, &token); err != nil || token.AccessToken == "" {
		return false, "waiting for authorization", nil
	}
	accountBody, _, err := c.do(ctx, http.MethodGet, base+pathAuthAccount+"?state="+url.QueryEscape(state), nil,
		func(h http.Header) {
			setCommonHeaders(h, base == ChatBaseGlobal)
			h.Set("Authorization", "Bearer "+token.AccessToken)
		})
	if err != nil {
		return false, "", err
	}
	var accountEnv envelope
	var identity struct {
		UID          string `json:"uid"`
		EnterpriseID string `json:"enterpriseId"`
		Nickname     string `json:"nickname"`
	}
	if json.Unmarshal(accountBody, &accountEnv) == nil && accountEnv.Code == 0 {
		_ = json.Unmarshal(accountEnv.Data, &identity)
	}
	domain := token.Domain
	if domain == "" || (base == ChatBaseGlobal && !strings.Contains(strings.ToLower(domain), DomainGlobal) && !strings.Contains(strings.ToLower(domain), "workbuddy")) {
		if base == ChatBaseGlobal {
			domain = DomainGlobal
		} else if domain == "" {
			domain = DomainCN
		}
	}
	credential := Credential{
		AccessToken:  token.AccessToken,
		RefreshToken: token.RefreshToken,
		ExpiresAt:    time.Now().Add(time.Duration(token.ExpiresIn) * time.Second).Unix(),
		Domain:       domain,
		UID:          identity.UID,
		EnterpriseID: identity.EnterpriseID,
		Nickname:     identity.Nickname,
	}
	payload, err := credential.Encode()
	if err != nil {
		return false, "", err
	}
	if err := c.store.SaveCredentialPayload(ctx, accountID, CredentialFormat, payload); err != nil {
		return false, "", err
	}
	_ = c.store.Observe(ctx, accountID, identity.UID, "ready", "", "")
	c.mu.Lock()
	delete(c.loginStates, accountID)
	c.mu.Unlock()
	return true, "login complete", nil
}

// credential loads and, when nearly expired, refreshes the stored token.
func (c *Client) credential(ctx context.Context, accountID string) (Credential, error) {
	_, payload, err := c.store.LoadCredentialPayload(ctx, accountID)
	if err != nil {
		return Credential{}, err
	}
	credential, err := DecodeCredential(payload)
	if err != nil {
		return Credential{}, err
	}
	if credential.ExpiresAt != 0 && time.Now().Add(2*time.Minute).Unix() < credential.ExpiresAt {
		return credential, nil
	}
	refreshed, err := c.Refresh(ctx, accountID, credential)
	if err != nil {
		return credential, nil
	}
	return refreshed, nil
}

func (c *Client) overlayRegion(ctx context.Context, accountID string, credential Credential) Credential {
	account, err := c.store.Get(ctx, accountID)
	if err != nil {
		return credential
	}
	if account.ProviderRegion == "global" && !credential.IsGlobal() {
		credential.Domain = DomainGlobal
	}
	if account.ProviderRegion == "cn" && credential.IsGlobal() {
		credential.Domain = DomainCN
	}
	return credential
}

func (c *Client) resolvedCredential(ctx context.Context, accountID string) (Credential, error) {
	credential, err := c.credential(ctx, accountID)
	if err != nil {
		return Credential{}, err
	}
	return c.overlayRegion(ctx, accountID, credential), nil
}

// Refresh exchanges the refresh token. Session-dead errors disable the
// account by surfacing the auth taxonomy to the manager.
func (c *Client) Refresh(ctx context.Context, accountID string, credential Credential) (Credential, error) {
	credential = c.overlayRegion(ctx, accountID, credential)
	body, status, err := c.do(ctx, http.MethodPost, credential.ChatBase()+pathTokenRefresh, []byte("{}"),
		func(h http.Header) { SetRefreshHeaders(h, credential) })
	if err != nil {
		return credential, err
	}
	classified := Classify(status, string(body))
	if classified.Kind == accounts.KindAuth {
		_ = c.store.Observe(ctx, accountID, credential.UID, "login_required", "session dead; re-login required", accounts.KindAuth)
		return credential, fmt.Errorf("workbuddy session dead: re-login required")
	}
	if status >= 300 || classified.Kind != "" {
		return credential, fmt.Errorf("refresh status=%d kind=%s", status, classified.Kind)
	}
	var env envelope
	if err := json.Unmarshal(body, &env); err != nil || env.Code != 0 {
		return credential, fmt.Errorf("refresh envelope code=%d msg=%s", env.Code, env.Msg)
	}
	var data struct {
		AccessToken  string `json:"accessToken"`
		RefreshToken string `json:"refreshToken"`
		ExpiresIn    int64  `json:"expiresIn"`
		Domain       string `json:"domain"`
	}
	if err := json.Unmarshal(env.Data, &data); err != nil || data.AccessToken == "" {
		return credential, fmt.Errorf("refresh response missing accessToken")
	}
	if data.RefreshToken != "" {
		credential.RefreshToken = data.RefreshToken
	}
	if data.Domain != "" {
		credential.Domain = data.Domain
	}
	if data.ExpiresIn > 0 {
		credential.ExpiresAt = time.Now().Add(time.Duration(data.ExpiresIn) * time.Second).Unix()
	}
	credential.AccessToken = data.AccessToken
	credential = c.overlayRegion(ctx, accountID, credential)
	payload, err := credential.Encode()
	if err != nil {
		return credential, err
	}
	if err := c.store.SaveCredentialPayload(ctx, accountID, CredentialFormat, payload); err != nil {
		return credential, err
	}
	return credential, nil
}

// Models fetches the dynamic catalog. Failure is an explicit error; there is
// no static fallback list.
func (c *Client) Models(ctx context.Context, accountID string) ([]providers.ModelInfo, error) {
	credential, err := c.resolvedCredential(ctx, accountID)
	if err != nil {
		return nil, err
	}
	body, status, err := c.do(ctx, http.MethodGet, credential.ChatBase()+pathModels, nil,
		func(h http.Header) { SetCatalogHeaders(h, credential) })
	if err != nil {
		return nil, err
	}
	if status >= 300 {
		return nil, fmt.Errorf("models status=%d: %s", status, string(body))
	}
	var env struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Data struct {
			Models []struct {
				ID              string `json:"id"`
				Name            string `json:"name"`
				MaxInputTokens  int    `json:"maxInputTokens"`
				MaxOutputTokens int    `json:"maxOutputTokens"`
				Disabled        bool   `json:"disabled"`
			} `json:"models"`
			Agents []struct {
				Name   string   `json:"name"`
				Models []string `json:"models"`
			} `json:"agents"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &env); err != nil || env.Code != 0 {
		return nil, fmt.Errorf("models envelope code=%d msg=%s", env.Code, env.Msg)
	}
	cliModels := map[string]struct{}{}
	for _, agent := range env.Data.Agents {
		if !isCLIAgent(agent.Name) {
			continue
		}
		for _, id := range agent.Models {
			cliModels[id] = struct{}{}
		}
	}
	filterCLI := len(cliModels) > 0
	var out []providers.ModelInfo
	for _, model := range env.Data.Models {
		if model.Disabled {
			continue
		}
		if filterCLI {
			if _, ok := cliModels[model.ID]; !ok {
				continue
			}
		}
		out = append(out, providers.ModelInfo{
			NativeModel: model.ID,
			PublicModel: model.ID,
			DisplayName: model.Name,
			Capabilities: providers.ModelCapabilities{
				ContextWindow: model.MaxInputTokens,
				MaxOutput:     model.MaxOutputTokens,
				Tools:         true,
				Reasoning:     true,
			},
		})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("workbuddy model catalog returned no cli models")
	}
	return out, nil
}

func (c *Client) chatRequest(ctx context.Context, credential Credential, req translate.ChatRequest) (*http.Request, error) {
	payload, err := json.Marshal(map[string]any{
		"model":       req.Model,
		"messages":    req.Messages,
		"max_tokens":  req.MaxTokens,
		"temperature": req.Temperature,
		"tools":       req.Tools,
		"tool_choice": req.ToolChoice,
	})
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		credential.ChatBase()+pathChat, bytes.NewReader(PrepareBody(payload)))
	if err != nil {
		return nil, err
	}
	SetChatHeaders(httpReq.Header, credential)
	return httpReq, nil
}

func (c *Client) ChatNonStream(ctx context.Context, accountID string, req translate.ChatRequest) (providers.ChatOutcome, error) {
	credential, err := c.resolvedCredential(ctx, accountID)
	if err != nil {
		return providers.ChatOutcome{}, err
	}
	httpReq, err := c.chatRequest(ctx, credential, req)
	if err != nil {
		return providers.ChatOutcome{}, err
	}
	resp, err := c.http.Do(httpReq)
	if err != nil {
		return providers.ChatOutcome{}, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if resp.StatusCode >= 300 {
		return providers.ChatOutcome{}, classifiedError(resp.StatusCode, body)
	}
	aggregate, err := Aggregate(bytes.NewReader(body))
	if err != nil {
		return providers.ChatOutcome{}, err
	}
	return outcomeFromAggregate(aggregate)
}

func (c *Client) ChatStream(ctx context.Context, accountID string, req translate.ChatRequest) (*http.Response, error) {
	credential, err := c.resolvedCredential(ctx, accountID)
	if err != nil {
		return nil, err
	}
	httpReq, err := c.chatRequest(ctx, credential, req)
	if err != nil {
		return nil, err
	}
	client := *c.http
	client.Timeout = 0
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		resp.Body.Close()
		return nil, classifiedError(resp.StatusCode, body)
	}
	return resp, nil
}

func outcomeFromAggregate(aggregate map[string]any) (providers.ChatOutcome, error) {
	raw, err := json.Marshal(aggregate)
	if err != nil {
		return providers.ChatOutcome{}, err
	}
	var parsed struct {
		Model string `json:"model"`
		Usage struct {
			PromptTokens     int    `json:"prompt_tokens"`
			CompletionTokens int    `json:"completion_tokens"`
			Source           string `json:"source"`
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
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return providers.ChatOutcome{}, err
	}
	out := providers.ChatOutcome{UsageSource: "upstream", FinishReason: "stop"}
	if len(parsed.Choices) > 0 {
		out.Content = parsed.Choices[0].Message.Content
		out.Reasoning = parsed.Choices[0].Message.ReasoningContent
		out.ToolCalls = parsed.Choices[0].Message.ToolCalls
		if parsed.Choices[0].FinishReason != "" {
			out.FinishReason = parsed.Choices[0].FinishReason
		}
	}
	out.Model = parsed.Model
	out.PromptTokens = parsed.Usage.PromptTokens
	out.CompletionTokens = parsed.Usage.CompletionTokens
	return out, nil
}

// ClassifiedError marks provider errors with the internal taxonomy so callers
// never see WorkBuddy-specific kinds in storage.
type ClassifiedError struct {
	Kind    string
	Status  int
	Message string
}

func (e *ClassifiedError) Error() string { return e.Message }

func classifiedError(status int, body []byte) error {
	classified := Classify(status, string(body))
	return &ClassifiedError{Kind: classified.Kind, Status: classified.Status, Message: classified.Message}
}

// Classify maps WorkBuddy HTTP/error bodies onto the internal taxonomy.
func Classify(status int, body string) providers.ClassifiedError {
	text := strings.ToLower(body)
	switch {
	case status == 402 || strings.Contains(text, "insufficient credit") ||
		strings.Contains(text, "quota exceeded") || strings.Contains(text, "积分不足") ||
		strings.Contains(text, "余额不足"):
		return providers.ClassifiedError{Kind: accounts.KindQuota, Status: 402, Message: strings.TrimSpace(body)}
	case status == 401 || strings.Contains(text, sessionDeadText) ||
		strings.Contains(text, fmt.Sprintf("%d", sessionDeadCode)):
		return providers.ClassifiedError{Kind: accounts.KindAuth, Status: 401, Message: "session dead; re-login required"}
	case status == 429 || strings.Contains(text, "soft_rate"):
		return providers.ClassifiedError{Kind: accounts.KindRateLimit, Status: 429, Message: strings.TrimSpace(body)}
	case status == 404:
		return providers.ClassifiedError{Kind: accounts.KindUnavailable, Status: 404, Message: strings.TrimSpace(body)}
	case status == 400 || accounts.IsInvalidRequestText(body):
		// Request-level rejection (content screening, malformed fields):
		// retrying on another account cannot help and the account is healthy.
		return providers.ClassifiedError{Kind: accounts.KindInvalidRequest, Status: firstNonEmptyStatus(status, 400), Message: strings.TrimSpace(body)}
	case status >= 500:
		return providers.ClassifiedError{Kind: accounts.KindUnavailable, Status: status, Message: strings.TrimSpace(body)}
	}
	var env envelope
	if json.Unmarshal([]byte(body), &env) == nil && env.Code != 0 {
		if env.Code == sessionDeadCode || strings.Contains(strings.ToLower(env.Msg), sessionDeadText) {
			return providers.ClassifiedError{Kind: accounts.KindAuth, Status: 401, Message: "session dead; re-login required"}
		}
		if accounts.IsInvalidRequestText(env.Msg) {
			return providers.ClassifiedError{Kind: accounts.KindInvalidRequest, Status: firstNonEmptyStatus(status, 400), Message: env.Msg}
		}
		return providers.ClassifiedError{Kind: accounts.KindUnavailable, Status: 502, Message: env.Msg}
	}
	return providers.ClassifiedError{}
}

func firstNonEmptyStatus(status, fallback int) int {
	if status >= 400 {
		return status
	}
	return fallback
}

// Probe reports whether the stored credential is usable. There is no WASM hot
// state; Hot mirrors Ready so the accounts console can treat signed-in accounts
// as available without probing a child-process /health URL.
func (c *Client) Probe(ctx context.Context, accountID string) (providers.AccountHealth, error) {
	credential, err := c.resolvedCredential(ctx, accountID)
	if err != nil {
		return providers.AccountHealth{LastError: err.Error()}, nil
	}
	if !credential.Ready() {
		msg := "workbuddy credential incomplete; re-login required"
		return providers.AccountHealth{UID: credential.UID, LastError: msg}, nil
	}
	return providers.AccountHealth{
		Ready: true,
		Hot:   true,
		UID:   credential.UID,
	}, nil
}

// Quota fetches display-only remaining credits from the billing meter API.
// Failures return an error for the caller to ignore without flipping readiness.
func (c *Client) Quota(ctx context.Context, accountID string) (*providers.QuotaInfo, error) {
	credential, err := c.resolvedCredential(ctx, accountID)
	if err != nil {
		return nil, err
	}
	remain, used, total, err := c.UserResource(ctx, credential)
	if err != nil {
		return nil, err
	}
	if total <= 0 && remain > 0 {
		total = remain
	}
	if used <= 0 && total > remain {
		used = total - remain
	}
	percentage := 0.0
	if total > 0 {
		percentage = (float64(used) / float64(total)) * 100
		if percentage < 0 {
			percentage = 0
		}
		if percentage > 100 {
			percentage = 100
		}
	}
	return &providers.QuotaInfo{
		Used:       float64(used),
		Total:      float64(total),
		Remaining:  float64(remain),
		Percentage: percentage,
		Unit:       "credits",
		Exceeded:   total > 0 && remain <= 0,
		FetchedAt:  time.Now().UTC().Format(time.RFC3339),
	}, nil
}

// UserResource aggregates package remain/used/total from get-user-resource.
func (c *Client) UserResource(ctx context.Context, credential Credential) (remain, used, total int64, err error) {
	now := time.Now()
	payload, err := json.Marshal(map[string]any{
		"PageNumber":               1,
		"PageSize":                 100,
		"ProductCode":              "p_tcaca",
		"Status":                   []int{0, 3},
		"PackageEndTimeRangeBegin": now.Format("2006-01-02 15:04:05"),
		"PackageEndTimeRangeEnd":   now.Add(365 * 101 * 24 * time.Hour).Format("2006-01-02 15:04:05"),
	})
	if err != nil {
		return 0, 0, 0, err
	}
	body, status, err := c.do(ctx, http.MethodPost, credential.BillingBase()+pathUserResource, payload,
		func(h http.Header) { SetBillingHeaders(h, credential) })
	if err != nil {
		return 0, 0, 0, err
	}
	if status >= 300 {
		return 0, 0, 0, fmt.Errorf("user-resource status=%d: %s", status, strings.TrimSpace(string(body)))
	}
	var env struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Data struct {
			Response struct {
				Data struct {
					TotalDosage int64             `json:"TotalDosage"`
					Accounts    []resourcePackage `json:"Accounts"`
				} `json:"Data"`
			} `json:"Response"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return 0, 0, 0, fmt.Errorf("user-resource parse: %w", err)
	}
	if env.Code != 0 {
		return 0, 0, 0, fmt.Errorf("user-resource code=%d msg=%s", env.Code, env.Msg)
	}
	remain, used, total = aggregateUserResource(env.Data.Response.Data.Accounts, env.Data.Response.Data.TotalDosage)
	return remain, used, total, nil
}

type resourcePackage struct {
	CapacityRemain      int64 `json:"CapacityRemain"`
	CapacityUsed        int64 `json:"CapacityUsed"`
	CapacitySize        int64 `json:"CapacitySize"`
	CycleCapacityRemain int64 `json:"CycleCapacityRemain"`
	CycleCapacityUsed   int64 `json:"CycleCapacityUsed"`
	CycleCapacitySize   int64 `json:"CycleCapacitySize"`
}

func packageRemainUsed(pkg resourcePackage) (remain, used, size int64) {
	if pkg.CycleCapacitySize > 0 {
		remain = pkg.CycleCapacityRemain
		size = pkg.CycleCapacitySize
		if remain < 0 {
			remain = 0
		}
		if remain > size {
			remain = size
		}
		used = size - remain
		if pkg.CycleCapacityUsed > used {
			used = pkg.CycleCapacityUsed
			if size >= used {
				remain = size - used
			}
		}
		return remain, used, size
	}
	if pkg.CycleCapacityRemain > 0 || pkg.CycleCapacityUsed > 0 {
		remain = pkg.CycleCapacityRemain
		used = pkg.CycleCapacityUsed
		size = pkg.CycleCapacitySize
		if remain < 0 {
			remain = 0
		}
		return remain, used, size
	}
	remain = pkg.CapacityRemain
	used = pkg.CapacityUsed
	size = pkg.CapacitySize
	if remain < 0 {
		remain = 0
	}
	if used == 0 && size > remain {
		used = size - remain
	}
	return remain, used, size
}

func aggregateUserResource(packages []resourcePackage, totalDosage int64) (remain, used, size int64) {
	for _, pkg := range packages {
		r, u, s := packageRemainUsed(pkg)
		remain += r
		used += u
		size += s
	}
	if size > 0 {
		if derived := size - remain; derived > used {
			used = derived
		}
	}
	if totalDosage > size {
		size = totalDosage
		if derived := size - remain; derived > used {
			used = derived
		}
	}
	return remain, used, size
}

// Adapter returns the provider capability bundle for registration.
func (c *Client) Adapter() providers.Adapter {
	return providers.Adapter{
		ID:         "workbuddy",
		Credential: credentialCodec{},
		Login:      c,
		Chat:       c,
		Models:     c,
		Classifier: classifier{},
		Prober:     c,
	}
}

type credentialCodec struct{}

func (credentialCodec) Validate(payload []byte) error { return ValidateCredential(payload) }

type classifier struct{}

func (classifier) Classify(status int, body string) providers.ClassifiedError {
	return Classify(status, body)
}
