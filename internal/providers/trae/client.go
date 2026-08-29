package trae

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/caigee-cmd/cli2api/internal/accounts"
	"github.com/caigee-cmd/cli2api/internal/providers"
	"github.com/caigee-cmd/cli2api/internal/translate"
)

// Store is the persistence surface the adapter needs.
type Store interface {
	Get(ctx context.Context, id string) (accounts.Account, error)
	LoadCredentialPayload(ctx context.Context, accountID string) (string, []byte, error)
	SaveCredentialPayload(ctx context.Context, accountID, format string, payload []byte) error
	Observe(ctx context.Context, id, remoteUID, status, lastError, lastKind string) error
}

type loginPending struct {
	machineID   string
	deviceID    string
	callbackURL string
	createdAt   time.Time
	done        bool
	failed      bool
	message     string
	credential  Credential
}

type Client struct {
	store Store
	http  *http.Client

	mu       sync.Mutex
	pending  map[string]*loginPending
	listener net.Listener
}

const catalogTimeout = 15 * time.Second

func NewClient(store Store) *Client {
	return &Client{
		store: store,
		http: &http.Client{
			Timeout: 120 * time.Second,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		pending: map[string]*loginPending{},
	}
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

func (c *Client) StartLogin(ctx context.Context, accountID string) (providers.LoginSession, error) {
	credential := Credential{Domain: DomainCN, APIHost: OAuthHost}
	if _, payload, err := c.store.LoadCredentialPayload(ctx, accountID); err == nil {
		if decoded, err := DecodeCredential(payload); err == nil {
			credential.MachineID = decoded.MachineID
			credential.DeviceID = decoded.DeviceID
			if decoded.Domain != "" {
				credential.Domain = decoded.Domain
			}
			if decoded.APIHost != "" {
				credential.APIHost = decoded.APIHost
			}
		}
	}
	credential = EnsureDevice(credential)
	callbackURL, err := c.ensureCallback()
	if err != nil {
		return providers.LoginSession{}, err
	}
	trace := randomHex(8)
	c.mu.Lock()
	c.pending[accountID] = &loginPending{
		machineID:   credential.MachineID,
		deviceID:    credential.DeviceID,
		callbackURL: callbackURL,
		createdAt:   time.Now(),
	}
	c.mu.Unlock()
	authURL := BuildLoginURL(credential.MachineID, credential.DeviceID, callbackURL, trace)
	return providers.LoginSession{AuthURL: authURL, State: trace}, nil
}

func (c *Client) PollLogin(ctx context.Context, accountID string) (bool, string, error) {
	c.mu.Lock()
	pending := c.pending[accountID]
	c.mu.Unlock()
	if pending == nil {
		return false, "", fmt.Errorf("login not started for account %s", accountID)
	}
	if time.Since(pending.createdAt) > loginPendingTTL {
		c.mu.Lock()
		delete(c.pending, accountID)
		c.mu.Unlock()
		return false, "", fmt.Errorf("login expired; start again")
	}
	c.mu.Lock()
	done := pending.done
	failed := pending.failed
	message := pending.message
	credential := pending.credential
	c.mu.Unlock()
	if failed {
		return false, "", fmt.Errorf("%s", firstNonEmpty(message, "login failed"))
	}
	if !done {
		return false, firstNonEmpty(message, "waiting for authorization"), nil
	}
	if err := c.finishCredential(ctx, accountID, credential); err != nil {
		return false, "", err
	}
	c.mu.Lock()
	delete(c.pending, accountID)
	c.mu.Unlock()
	return true, "login complete", nil
}

func (c *Client) CompleteLogin(ctx context.Context, accountID, callbackURL string) error {
	info, err := ParseCallback(callbackURL)
	if err != nil {
		return err
	}
	credential := Credential{
		AccessToken:  info.AccessToken,
		RefreshToken: info.RefreshToken,
		ExpiresAt:    unixSeconds(info.ExpiresAt),
		UID:          info.UID,
		Nickname:     info.Nickname,
		EnterpriseID: info.EnterpriseID,
		Domain:       DomainCN,
		APIHost:      OAuthHost,
	}
	c.mu.Lock()
	pending := c.pending[accountID]
	c.mu.Unlock()
	if pending != nil {
		if pending.done {
			c.mu.Lock()
			delete(c.pending, accountID)
			c.mu.Unlock()
			return nil
		}
		credential.MachineID = pending.machineID
		credential.DeviceID = pending.deviceID
	} else if _, payload, err := c.store.LoadCredentialPayload(ctx, accountID); err == nil {
		if decoded, err := DecodeCredential(payload); err == nil {
			if decoded.Ready() {
				return nil
			}
			credential.MachineID = decoded.MachineID
			credential.DeviceID = decoded.DeviceID
		}
	}
	if err := c.finishCredential(ctx, accountID, credential); err != nil {
		return err
	}
	c.mu.Lock()
	delete(c.pending, accountID)
	c.mu.Unlock()
	return nil
}

func (c *Client) finishCredential(ctx context.Context, accountID string, credential Credential) error {
	if strings.TrimSpace(credential.RefreshToken) != "" {
		refreshed, err := c.ExchangeToken(ctx, credential)
		if err != nil {
			return err
		}
		credential = refreshed
	}
	if strings.TrimSpace(credential.UID) == "" && strings.TrimSpace(credential.AccessToken) != "" {
		info, err := c.GetUserInfo(ctx, credential)
		if err == nil {
			credential.UID = info.UID
			if info.Nickname != "" {
				credential.Nickname = info.Nickname
			}
			if info.EnterpriseID != "" {
				credential.EnterpriseID = info.EnterpriseID
			}
		}
	}
	credential = EnsureDevice(credential)
	payload, err := credential.Encode()
	if err != nil {
		return err
	}
	if err := c.store.SaveCredentialPayload(ctx, accountID, CredentialFormat, payload); err != nil {
		return err
	}
	_ = c.store.Observe(ctx, accountID, credential.UID, "ready", "", "")
	return nil
}

func (c *Client) ensureCallback() (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.listener != nil {
		addr, ok := c.listener.Addr().(*net.TCPAddr)
		if ok {
			return fmt.Sprintf("http://127.0.0.1:%d%s", addr.Port, pathCallback), nil
		}
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", fmt.Errorf("trae callback listen: %w", err)
	}
	c.listener = ln
	mux := http.NewServeMux()
	mux.HandleFunc(pathCallback, c.handleCallback)
	go func() {
		_ = http.Serve(ln, mux)
	}()
	addr := ln.Addr().(*net.TCPAddr)
	return fmt.Sprintf("http://127.0.0.1:%d%s", addr.Port, pathCallback), nil
}

func (c *Client) handleCallback(w http.ResponseWriter, r *http.Request) {
	info, err := ParseCallback(r.URL.String())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		c.markPendingFailed(err.Error())
		return
	}
	credential := Credential{
		AccessToken:  info.AccessToken,
		RefreshToken: info.RefreshToken,
		ExpiresAt:    unixSeconds(info.ExpiresAt),
		UID:          info.UID,
		Nickname:     info.Nickname,
		EnterpriseID: info.EnterpriseID,
		Domain:       DomainCN,
		APIHost:      OAuthHost,
	}
	c.mu.Lock()
	for _, pending := range c.pending {
		if pending.done || pending.failed {
			continue
		}
		credential.MachineID = pending.machineID
		credential.DeviceID = pending.deviceID
		pending.credential = credential
		pending.done = true
		pending.message = "authorization received"
		break
	}
	c.mu.Unlock()
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = io.WriteString(w, `<!doctype html><meta charset="utf-8"><title>Trae login</title><p>Login complete. You can close this tab.</p>`)
}

func (c *Client) markPendingFailed(message string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, pending := range c.pending {
		if pending.done || pending.failed {
			continue
		}
		pending.failed = true
		pending.message = message
		return
	}
}

type callbackInfo struct {
	RefreshToken string
	AccessToken  string
	UID          string
	Nickname     string
	EnterpriseID string
	ExpiresAt    int64
}

func ParseCallback(rawURL string) (callbackInfo, error) {
	if strings.TrimSpace(rawURL) == "" {
		return callbackInfo{}, fmt.Errorf("empty callback url")
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return callbackInfo{}, fmt.Errorf("parse callback url: %w", err)
	}
	query := parsed.Query()
	info := callbackInfo{
		RefreshToken: firstNonEmpty(query.Get("refreshToken"), query.Get("refresh_token")),
		AccessToken:  firstNonEmpty(query.Get("accessToken"), query.Get("userJwt")),
	}
	if userInfo := query.Get("userInfo"); userInfo != "" {
		var nested struct {
			UserID       string `json:"UserID"`
			ScreenName   string `json:"ScreenName"`
			TenantID     string `json:"TenantID"`
			UID          string `json:"uid"`
			Nickname     string `json:"nickname"`
			EnterpriseID string `json:"enterpriseId"`
		}
		if json.Unmarshal([]byte(userInfo), &nested) == nil {
			info.UID = firstNonEmpty(nested.UserID, nested.UID)
			info.Nickname = firstNonEmpty(nested.ScreenName, nested.Nickname)
			info.EnterpriseID = firstNonEmpty(nested.TenantID, nested.EnterpriseID)
		}
	}
	if userJWT := query.Get("userJwt"); userJWT != "" {
		var jwt struct {
			Token         string `json:"Token"`
			RefreshToken  string `json:"RefreshToken"`
			TokenExpireAt int64  `json:"TokenExpireAt"`
		}
		if json.Unmarshal([]byte(userJWT), &jwt) == nil {
			if info.RefreshToken == "" {
				info.RefreshToken = jwt.RefreshToken
			}
			if jwt.Token != "" {
				info.AccessToken = jwt.Token
			}
			if jwt.TokenExpireAt != 0 {
				info.ExpiresAt = jwt.TokenExpireAt
			}
		}
	}
	if info.RefreshToken == "" && info.AccessToken == "" {
		return callbackInfo{}, fmt.Errorf("callback missing refreshToken and userJwt.Token")
	}
	return info, nil
}

func BuildLoginURL(machineID, deviceID, callbackURL, trace string) string {
	values := url.Values{}
	values.Set("login_version", "1")
	values.Set("auth_from", "solo")
	values.Set("login_channel", "native_ide")
	values.Set("plugin_version", PluginVersion)
	values.Set("auth_type", "local")
	values.Set("client_id", ClientID)
	values.Set("redirect", "0")
	values.Set("login_trace_id", trace)
	values.Set("auth_callback_url", callbackURL)
	values.Set("machine_id", machineID)
	values.Set("device_id", deviceID)
	values.Set("x_device_id", deviceID)
	values.Set("x_machine_id", machineID)
	values.Set("x_device_brand", "PC")
	values.Set("x_device_type", "PC")
	values.Set("x_os_version", "1.0")
	values.Set("x_app_version", IdeVersion)
	values.Set("x_app_type", "stable")
	return ConsoleHost + pathAuthorization + "?" + values.Encode()
}

func (c *Client) ExchangeToken(ctx context.Context, credential Credential) (Credential, error) {
	if strings.TrimSpace(credential.RefreshToken) == "" {
		return credential, fmt.Errorf("no refreshToken")
	}
	body, err := json.Marshal(map[string]any{
		"ClientID":     ClientID,
		"RefreshToken": credential.RefreshToken,
		"ClientSecret": "-",
		"UserID":       "",
	})
	if err != nil {
		return credential, err
	}
	payload, status, err := c.do(ctx, http.MethodPost, credential.AuthBase()+pathExchange, body, SetOAuthHeaders)
	if err != nil {
		return credential, err
	}
	if status >= 300 {
		return credential, classifiedError(status, payload)
	}
	var env struct {
		Result struct {
			Token               string `json:"Token"`
			TokenExpireAt       int64  `json:"TokenExpireAt"`
			TokenExpireDuration int64  `json:"TokenExpireDuration"`
			RefreshToken        string `json:"RefreshToken"`
			RefreshExpireAt     int64  `json:"RefreshExpireAt"`
		} `json:"Result"`
	}
	if err := json.Unmarshal(payload, &env); err != nil {
		return credential, fmt.Errorf("exchange token parse: %w", err)
	}
	if strings.TrimSpace(env.Result.Token) == "" {
		return credential, fmt.Errorf("refresh_failed: no token in response — re-login required")
	}
	credential.AccessToken = env.Result.Token
	if env.Result.RefreshToken != "" {
		credential.RefreshToken = env.Result.RefreshToken
	}
	if env.Result.TokenExpireAt != 0 {
		credential.ExpiresAt = unixSeconds(env.Result.TokenExpireAt)
	} else if env.Result.TokenExpireDuration > 0 {
		credential.ExpiresAt = time.Now().Add(time.Duration(env.Result.TokenExpireDuration) * time.Second).Unix()
	}
	if env.Result.RefreshExpireAt != 0 {
		credential.RefreshExpiresAt = unixSeconds(env.Result.RefreshExpireAt)
	}
	return credential, nil
}

type userInfo struct {
	UID          string
	Nickname     string
	EnterpriseID string
}

func (c *Client) GetUserInfo(ctx context.Context, credential Credential) (userInfo, error) {
	body, err := json.Marshal(map[string]any{
		"ReqSource":  "IDE",
		"IDEVersion": IdeVersion,
	})
	if err != nil {
		return userInfo{}, err
	}
	payload, status, err := c.do(ctx, http.MethodPost, credential.AuthBase()+pathUserInfo, body, func(h http.Header) {
		SetOAuthHeaders(h)
		if credential.AccessToken != "" {
			h.Set("X-Cloudide-Token", credential.AccessToken)
		}
	})
	if err != nil {
		return userInfo{}, err
	}
	if status >= 300 {
		return userInfo{}, classifiedError(status, payload)
	}
	var env struct {
		Result struct {
			UserID       string `json:"UserID"`
			ScreenName   string `json:"ScreenName"`
			EnterpriseID string `json:"EnterpriseID"`
		} `json:"Result"`
	}
	if err := json.Unmarshal(payload, &env); err != nil {
		return userInfo{}, fmt.Errorf("user info parse: %w", err)
	}
	return userInfo{
		UID:          env.Result.UserID,
		Nickname:     env.Result.ScreenName,
		EnterpriseID: env.Result.EnterpriseID,
	}, nil
}

func (c *Client) credential(ctx context.Context, accountID string) (Credential, error) {
	_, payload, err := c.store.LoadCredentialPayload(ctx, accountID)
	if err != nil {
		return Credential{}, err
	}
	credential, err := DecodeCredential(payload)
	if err != nil {
		return Credential{}, err
	}
	now := time.Now()
	if credential.refreshExpired(now) {
		_ = c.store.Observe(ctx, accountID, credential.UID, "login_required", "refresh token expired; re-login required", accounts.KindAuth)
		return credential, fmt.Errorf("trae refresh token expired; re-login required")
	}
	if !credential.needsRefresh(now) {
		return credential, nil
	}
	refreshed, err := c.ExchangeToken(ctx, credential)
	if err != nil {
		_ = c.store.Observe(ctx, accountID, credential.UID, "login_required", err.Error(), accounts.KindAuth)
		return credential, err
	}
	encoded, err := refreshed.Encode()
	if err != nil {
		return refreshed, err
	}
	if err := c.store.SaveCredentialPayload(ctx, accountID, CredentialFormat, encoded); err != nil {
		return refreshed, err
	}
	return refreshed, nil
}

func (c *Client) Models(ctx context.Context, accountID string) ([]providers.ModelInfo, error) {
	credential, err := c.credential(ctx, accountID)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, catalogTimeout)
	defer cancel()
	body, err := json.Marshal(map[string]any{
		"function":            Function,
		"config_names":        nil,
		"need_prompt":         false,
		"current_config_info": nil,
		"poly_prompt":         true,
		"mode_type":           nil,
		"agent_type":          nil,
	})
	if err != nil {
		return nil, err
	}
	payload, status, err := c.do(ctx, http.MethodPost, credential.ChatBase()+pathModels, body,
		func(h http.Header) { SetCatalogHeaders(h, credential) })
	if err != nil {
		return nil, err
	}
	if status >= 300 {
		return nil, classifiedError(status, payload)
	}
	var env struct {
		ConfigInfoList []struct {
			ConfigName    string `json:"config_name"`
			DisplayConfig struct {
				DisplayName string `json:"display_name"`
			} `json:"display_config"`
		} `json:"config_info_list"`
	}
	if err := json.Unmarshal(payload, &env); err != nil {
		return nil, fmt.Errorf("models parse: %w", err)
	}
	var out []providers.ModelInfo
	for _, item := range env.ConfigInfoList {
		id := strings.TrimSpace(item.ConfigName)
		if id == "" {
			continue
		}
		display := strings.TrimSpace(item.DisplayConfig.DisplayName)
		out = append(out, providers.ModelInfo{
			NativeModel: id,
			PublicModel: id,
			DisplayName: display,
			Capabilities: providers.ModelCapabilities{
				Tools:     true,
				Reasoning: true,
			},
		})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("trae model catalog returned no models")
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
	credential, err := c.credential(ctx, accountID)
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
	return outcomeFromAggregate(aggregate, req.Model)
}

func (c *Client) ChatStream(ctx context.Context, accountID string, req translate.ChatRequest) (*http.Response, error) {
	credential, err := c.credential(ctx, accountID)
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
	return rewriteSoloStream(resp.Body, req.Model), nil
}

func outcomeFromAggregate(aggregate map[string]any, fallbackModel string) (providers.ChatOutcome, error) {
	raw, err := json.Marshal(aggregate)
	if err != nil {
		return providers.ChatOutcome{}, err
	}
	var parsed struct {
		Model string `json:"model"`
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
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
	out := providers.ChatOutcome{UsageSource: "upstream", FinishReason: "stop", Model: fallbackModel}
	if parsed.Model != "" {
		out.Model = parsed.Model
	}
	if len(parsed.Choices) > 0 {
		out.Content = parsed.Choices[0].Message.Content
		out.Reasoning = parsed.Choices[0].Message.ReasoningContent
		out.ToolCalls = parsed.Choices[0].Message.ToolCalls
		if parsed.Choices[0].FinishReason != "" {
			out.FinishReason = parsed.Choices[0].FinishReason
		}
	}
	out.PromptTokens = parsed.Usage.PromptTokens
	out.CompletionTokens = parsed.Usage.CompletionTokens
	return out, nil
}

func classifiedError(status int, body []byte) error {
	return wrapClassified(Classify(status, string(body)), extractCode(string(body)))
}

func wrapClassified(classified providers.ClassifiedError, code string) error {
	failover := classified.Kind != accounts.KindInvalidRequest
	return &providers.Error{
		Kind:     classified.Kind,
		Status:   classified.Status,
		Message:  classified.Message,
		Cooldown: classifiedCooldown(classified.Kind, code),
		Failover: &failover,
	}
}

func extractCode(body string) string {
	var env struct {
		Code any `json:"code"`
	}
	if json.Unmarshal([]byte(body), &env) != nil {
		return ""
	}
	switch v := env.Code.(type) {
	case float64:
		return fmt.Sprintf("%.0f", v)
	default:
		return strings.TrimSpace(fmt.Sprint(v))
	}
}

func classifiedCooldown(kind, code string) time.Duration {
	switch code {
	case "1005":
		return quotaCooldownPlan
	case "4008":
		return quotaCooldownSolo
	case "4011":
		return hardRateCooldown
	}
	switch kind {
	case accounts.KindQuota:
		return time.Hour
	case accounts.KindAuth:
		return 30 * time.Minute
	case accounts.KindRateLimit:
		return time.Minute
	default:
		return 0
	}
}

// Classify maps Trae HTTP/error bodies onto the internal taxonomy.
func Classify(status int, body string) providers.ClassifiedError {
	text := strings.ToLower(body)
	code := extractCode(body)
	switch {
	case code == "1001" || status == 401 || strings.Contains(text, "unauthorized"):
		return providers.ClassifiedError{Kind: accounts.KindAuth, Status: 401, Message: firstNonEmpty(strings.TrimSpace(body), "session dead; re-login required")}
	case code == "1005" || (strings.Contains(text, "1005") && strings.Contains(text, "plan")):
		return providers.ClassifiedError{Kind: accounts.KindQuota, Status: 429, Message: firstNonEmpty(strings.TrimSpace(body), "plan limit")}
	case code == "4008":
		return providers.ClassifiedError{Kind: accounts.KindQuota, Status: 429, Message: firstNonEmpty(strings.TrimSpace(body), "solo credits exhausted")}
	case code == "4001":
		return providers.ClassifiedError{Kind: accounts.KindInvalidRequest, Status: 400, Message: firstNonEmpty(strings.TrimSpace(body), "model or ide version mismatch")}
	case code == "4011":
		return providers.ClassifiedError{Kind: accounts.KindRateLimit, Status: 429, Message: firstNonEmpty(strings.TrimSpace(body), "hard rate limit")}
	case status == 429 || strings.Contains(text, "too many requests"):
		return providers.ClassifiedError{Kind: accounts.KindRateLimit, Status: 429, Message: strings.TrimSpace(body)}
	case status == 404:
		return providers.ClassifiedError{Kind: accounts.KindUnavailable, Status: 404, Message: strings.TrimSpace(body)}
	case status == 400 || accounts.IsInvalidRequestText(body):
		return providers.ClassifiedError{Kind: accounts.KindInvalidRequest, Status: firstNonEmptyStatus(status, 400), Message: strings.TrimSpace(body)}
	case status >= 500:
		return providers.ClassifiedError{Kind: accounts.KindUnavailable, Status: status, Message: strings.TrimSpace(body)}
	}
	if code != "" && code != "0" && status < 300 {
		if code == "9074" {
			return providers.ClassifiedError{}
		}
		return providers.ClassifiedError{Kind: accounts.KindUnavailable, Status: 502, Message: strings.TrimSpace(body)}
	}
	return providers.ClassifiedError{}
}

func firstNonEmptyStatus(status, fallback int) int {
	if status >= 400 {
		return status
	}
	return fallback
}

func (c *Client) Probe(ctx context.Context, accountID string) (providers.AccountHealth, error) {
	credential, err := c.credential(ctx, accountID)
	if err != nil {
		return providers.AccountHealth{LastError: err.Error()}, nil
	}
	if !credential.Ready() {
		msg := "trae credential incomplete; re-login required"
		return providers.AccountHealth{UID: credential.UID, LastError: msg}, nil
	}
	return providers.AccountHealth{
		Ready: true,
		Hot:   true,
		UID:   credential.UID,
	}, nil
}

func (c *Client) Quota(ctx context.Context, accountID string) (*providers.QuotaInfo, error) {
	credential, err := c.credential(ctx, accountID)
	if err != nil {
		return nil, err
	}
	remain, used, total, err := c.UserEntUsage(ctx, credential)
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
		Unit:       QuotaUnit,
		Exceeded:   total > 0 && remain <= 0,
		FetchedAt:  time.Now().UTC().Format(time.RFC3339),
	}, nil
}

func (c *Client) UserEntUsage(ctx context.Context, credential Credential) (remain, used, total int64, err error) {
	body, status, err := c.do(ctx, http.MethodPost, credential.BillingBase()+pathEntUsage, []byte("{}"),
		func(h http.Header) { SetUgHeaders(h, credential) })
	if err != nil {
		return 0, 0, 0, err
	}
	if status >= 300 {
		return 0, 0, 0, classifiedError(status, body)
	}
	var env struct {
		UserEntitlementPackList []struct {
			EntitlementBaseInfo struct {
				Quota struct {
					CreditsLimit int64 `json:"credits_limit"`
				} `json:"quota"`
			} `json:"entitlement_base_info"`
			Usage struct {
				CreditsAmount int64 `json:"credits_amount"`
			} `json:"usage"`
		} `json:"user_entitlement_pack_list"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return 0, 0, 0, fmt.Errorf("entitlement parse: %w", err)
	}
	for _, pack := range env.UserEntitlementPackList {
		limit := pack.EntitlementBaseInfo.Quota.CreditsLimit
		if limit <= 0 {
			continue
		}
		consumed := pack.Usage.CreditsAmount
		if consumed < 0 {
			consumed = 0
		}
		left := limit - consumed
		if left < 0 {
			left = 0
		}
		total += limit
		used += consumed
		remain += left
	}
	return remain, used, total, nil
}

func (c *Client) CheckinStatus(ctx context.Context, credential Credential) ([]byte, error) {
	body, status, err := c.do(ctx, http.MethodPost, credential.BillingBase()+pathCheckinStatus, []byte("{}"),
		func(h http.Header) { SetUgHeaders(h, credential) })
	if err != nil {
		return nil, err
	}
	if status >= 300 {
		return nil, classifiedError(status, body)
	}
	return body, nil
}

func (c *Client) CheckinClaim(ctx context.Context, credential Credential) ([]byte, error) {
	body, status, err := c.do(ctx, http.MethodPost, credential.BillingBase()+pathCheckinClaim, []byte("{}"),
		func(h http.Header) { SetUgHeaders(h, credential) })
	if err != nil {
		return nil, err
	}
	if status >= 300 {
		return nil, classifiedError(status, body)
	}
	return body, nil
}

func (c *Client) Adapter() providers.Adapter {
	return providers.Adapter{
		ID:           "trae",
		Credential:   credentialCodec{},
		Login:        c,
		Chat:         c,
		Models:       c,
		Classifier:   classifier{},
		ImportExport: importer{},
		Prober:       c,
	}
}

type credentialCodec struct{}

func (credentialCodec) Validate(payload []byte) error { return ValidateCredential(payload) }

type classifier struct{}

func (classifier) Classify(status int, body string) providers.ClassifiedError {
	return Classify(status, body)
}

type importer struct{}

func (importer) ValidateImport(payload []byte) error { return ValidateCredential(payload) }

func (importer) Export(ctx context.Context, accountID string) (map[string]any, error) {
	return nil, providers.ErrUnsupported
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
