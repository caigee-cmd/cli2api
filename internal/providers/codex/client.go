package codex

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
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
	"github.com/caigee-cmd/cli2api/internal/auth"
	"github.com/caigee-cmd/cli2api/internal/providers"
	proxyutil "github.com/caigee-cmd/cli2api/internal/proxy"
	"github.com/caigee-cmd/cli2api/internal/translate"
)

// Store is the persistence surface the adapter needs.
type Store interface {
	Get(ctx context.Context, id string) (accounts.Account, error)
	LoadCredentialPayload(ctx context.Context, accountID string) (string, []byte, error)
	SaveCredentialPayload(ctx context.Context, accountID, format string, payload []byte) error
	Observe(ctx context.Context, id, remoteUID, status, lastError, lastKind string) error
}

// SecretReader is optional; missing it means no global proxy.
type SecretReader interface {
	GetSecret(context.Context, string) (string, bool, error)
}

type loginPending struct {
	state        string
	codeVerifier string
	createdAt    time.Time
	done         bool
	failed       bool
	message      string
	credential   Credential
}

type Client struct {
	store Store
	http  *http.Client

	transports proxyutil.TransportCache

	mu            sync.Mutex
	pending       map[string]*loginPending
	pendingStates map[string]string // oauth state → accountID
	listener      net.Listener
	refresh       map[string]*refreshCall
	quotaCache    map[string]*providers.QuotaInfo
}

// refreshCall singleflights token refresh by account so concurrent requests
// share one round-trip (OpenAI rotates refresh tokens; a second exchange with
// the same token returns refresh_token_reused).
type refreshCall struct {
	done chan struct{}
	cred Credential
	err  error
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
		pending:       map[string]*loginPending{},
		pendingStates: map[string]string{},
		refresh:       map[string]*refreshCall{},
	}
}

func (c *Client) globalProxy(ctx context.Context) (string, error) {
	store, ok := c.store.(SecretReader)
	if !ok {
		return "", nil
	}
	value, found, err := store.GetSecret(ctx, "proxy_url")
	if err != nil {
		return "", fmt.Errorf("load global proxy setting: %w", err)
	}
	if !found {
		return "", nil
	}
	return strings.TrimSpace(value), nil
}

func (c *Client) effectiveProxy(ctx context.Context, accountID string) (string, error) {
	account, err := c.store.Get(ctx, accountID)
	if err != nil {
		return "", err
	}
	if value := strings.TrimSpace(account.ProxyURL); value != "" {
		return value, nil
	}
	return c.globalProxy(ctx)
}

func (c *Client) httpClient(ctx context.Context, accountID string) (*http.Client, error) {
	rawProxy, err := c.effectiveProxy(ctx, accountID)
	if err != nil {
		return nil, err
	}
	client := *c.http
	transport, err := c.transports.Get(rawProxy)
	if err != nil {
		return nil, err
	}
	if transport != nil {
		client.Transport = transport
	}
	return &client, nil
}

func (c *Client) do(ctx context.Context, accountID, method, rawURL string, body []byte, setHeaders func(http.Header)) ([]byte, int, error) {
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
	client, err := c.httpClient(ctx, accountID)
	if err != nil {
		return nil, 0, err
	}
	resp, err := client.Do(req)
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

// ---------------------------------------------------------------------------
// Login
// ---------------------------------------------------------------------------

func (c *Client) StartLogin(ctx context.Context, accountID string) (providers.LoginSession, error) {
	if err := c.ensureCallback(); err != nil {
		return providers.LoginSession{}, err
	}
	state := randomHex(16)
	verifier, challenge, err := pkcePair()
	if err != nil {
		return providers.LoginSession{}, err
	}
	c.mu.Lock()
	c.pending[accountID] = &loginPending{
		state:        state,
		codeVerifier: verifier,
		createdAt:    time.Now(),
	}
	c.pendingStates[state] = accountID
	c.mu.Unlock()

	params := url.Values{
		"client_id":                  {ClientID},
		"response_type":              {"code"},
		"redirect_uri":               {redirectURI},
		"scope":                      {Scope},
		"state":                      {state},
		"code_challenge":             {challenge},
		"code_challenge_method":      {"S256"},
		"prompt":                     {"login"},
		"id_token_add_organizations": {"true"},
		"codex_cli_simplified_flow":  {"true"},
	}
	return providers.LoginSession{
		AuthURL: AuthURL + "?" + params.Encode(),
		State:   state,
	}, nil
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
		delete(c.pendingStates, pending.state)
		c.mu.Unlock()
		return false, "", fmt.Errorf("login expired; start again")
	}
	c.mu.Lock()
	done, failed, message := pending.done, pending.failed, pending.message
	credential := pending.credential
	state := pending.state
	c.mu.Unlock()
	if failed {
		return false, "", fmt.Errorf("%s", firstNonEmpty(message, "login failed"))
	}
	if !done {
		return false, firstNonEmpty(message, "waiting for authorization"), nil
	}
	payload, err := credential.Encode()
	if err != nil {
		return false, "", err
	}
	if err := c.store.SaveCredentialPayload(ctx, accountID, CredentialFormat, payload); err != nil {
		return false, "", err
	}
	_ = c.store.Observe(ctx, accountID, credential.AccountID, "ready", "", "")
	c.mu.Lock()
	delete(c.pending, accountID)
	delete(c.pendingStates, state)
	c.mu.Unlock()
	return true, "login complete", nil
}

// CompleteLogin handles a callback URL pasted by the user when the automatic
// loopback cannot reach this process.
func (c *Client) CompleteLogin(ctx context.Context, accountID, callbackURL string) error {
	parsed, err := url.Parse(strings.TrimSpace(callbackURL))
	if err != nil {
		return fmt.Errorf("parse callback url: %w", err)
	}
	query := parsed.Query()
	code := query.Get("code")
	if code == "" {
		return fmt.Errorf("callback url missing code")
	}
	state := query.Get("state")
	c.mu.Lock()
	// Prefer the pending entry whose verifier minted this code: when the
	// callback carries a state, the account slot may have been replaced by a
	// newer login, so look the entry up by state instead of by accountID.
	var pending *loginPending
	pendingAccount := accountID
	if state != "" {
		if owner, ok := c.pendingStates[state]; ok {
			pending = c.pending[owner]
			pendingAccount = owner
		}
	}
	if pending == nil {
		pending = c.pending[accountID]
	}
	c.mu.Unlock()
	verifier := ""
	if pending != nil {
		verifier = pending.codeVerifier
	}
	credential, err := c.exchangeCode(ctx, accountID, code, verifier)
	if err != nil {
		return err
	}
	return c.persistCredential(ctx, pendingAccount, credential, state)
}

// ensureCallback binds the fixed loopback listener OpenAI's OAuth client
// redirects to. The port is part of the registered redirect_uri and cannot
// change; a second listener on it would fail, so it is created once.
func (c *Client) ensureCallback() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.listener != nil {
		return nil
	}
	ln, err := net.Listen("tcp", LoopbackAddr)
	if err != nil {
		return fmt.Errorf("codex callback listen %s: %w", LoopbackAddr, err)
	}
	c.listener = ln
	auth.ServeLoopback(ln, pathCallback, "Codex", c.acceptCallback)
	return nil
}

func (c *Client) acceptCallback(ctx context.Context, rawURL string) error {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return err
	}
	query := parsed.Query()
	if errText := query.Get("error"); errText != "" {
		desc := query.Get("error_description")
		c.markPendingFailed(query.Get("state"), firstNonEmpty(desc, errText))
		return fmt.Errorf("%s", firstNonEmpty(desc, errText))
	}
	code := query.Get("code")
	state := query.Get("state")
	if code == "" {
		return fmt.Errorf("callback missing code")
	}
	c.mu.Lock()
	accountID := c.pendingStates[state]
	pending := c.pending[accountID]
	c.mu.Unlock()
	if pending == nil {
		return fmt.Errorf("no pending login for state")
	}
	credential, err := c.exchangeCode(ctx, accountID, code, pending.codeVerifier)
	if err != nil {
		c.markPendingFailed(state, err.Error())
		return err
	}
	c.mu.Lock()
	pending.credential = credential
	pending.done = true
	pending.message = "authorization received"
	c.mu.Unlock()
	return nil
}

func (c *Client) markPendingFailed(state, message string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if accountID, ok := c.pendingStates[state]; ok {
		if pending := c.pending[accountID]; pending != nil {
			pending.failed = true
			pending.message = message
		}
	}
}

func (c *Client) persistCredential(ctx context.Context, accountID string, credential Credential, state string) error {
	payload, err := credential.Encode()
	if err != nil {
		return err
	}
	if err := c.store.SaveCredentialPayload(ctx, accountID, CredentialFormat, payload); err != nil {
		return err
	}
	_ = c.store.Observe(ctx, accountID, credential.AccountID, "ready", "", "")
	c.mu.Lock()
	delete(c.pending, accountID)
	if state != "" {
		delete(c.pendingStates, state)
	}
	c.mu.Unlock()
	return nil
}

// exchangeCode swaps the authorization code for tokens at the token endpoint.
func (c *Client) exchangeCode(ctx context.Context, accountID, code, verifier string) (Credential, error) {
	form := url.Values{
		"grant_type":    {"authorization_code"},
		"client_id":     {ClientID},
		"code":          {code},
		"redirect_uri":  {redirectURI},
		"code_verifier": {verifier},
	}
	payload, status, err := c.do(ctx, accountID, http.MethodPost, TokenURL, []byte(form.Encode()), func(h http.Header) {
		h.Set("Content-Type", "application/x-www-form-urlencoded")
		h.Set("Accept", "application/json")
	})
	if err != nil {
		return Credential{}, err
	}
	if status != http.StatusOK {
		return Credential{}, fmt.Errorf("token exchange failed with status %d: %s", status, strings.TrimSpace(string(payload)))
	}
	return parseTokenResponse(payload)
}

// parseTokenResponse decodes the OAuth token endpoint response and pulls
// account_id/email out of the id_token JWT.
func parseTokenResponse(payload []byte) (Credential, error) {
	var tok struct {
		IDToken      string `json:"id_token"`
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int64  `json:"expires_in"`
	}
	if err := json.Unmarshal(payload, &tok); err != nil {
		return Credential{}, fmt.Errorf("token response parse: %w", err)
	}
	if tok.AccessToken == "" {
		return Credential{}, fmt.Errorf("token response missing access_token")
	}
	credential := Credential{
		IDToken:      tok.IDToken,
		AccessToken:  tok.AccessToken,
		RefreshToken: tok.RefreshToken,
		LastRefresh:  time.Now().UTC().Format(time.RFC3339),
	}
	if tok.ExpiresIn > 0 {
		credential.ExpiresAt = time.Now().Add(time.Duration(tok.ExpiresIn) * time.Second).Unix()
	}
	if claims := parseJWTClaims(tok.IDToken); claims != nil {
		credential.AccountID = firstNonEmpty(claims.AccountID, claims.ChatGPTAccountID)
		credential.Email = claims.Email
	}
	return credential, nil
}

type jwtClaims struct {
	AccountID        string `json:"account_id"`
	ChatGPTAccountID string `json:"chatgpt_account_id"`
	Email            string `json:"email"`
}

func parseJWTClaims(token string) *jwtClaims {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil
	}
	var claims jwtClaims
	// account_id can nest under https://api.openai.com/auth.
	var raw map[string]json.RawMessage
	if json.Unmarshal(payload, &raw) != nil {
		return nil
	}
	_ = json.Unmarshal(payload, &claims)
	if claims.AccountID == "" {
		for key, value := range raw {
			if !strings.HasSuffix(key, "auth") {
				continue
			}
			var nested struct {
				AccountID string `json:"account_id"`
			}
			if json.Unmarshal(value, &nested) == nil && nested.AccountID != "" {
				claims.AccountID = nested.AccountID
			}
		}
	}
	return &claims
}

// ---------------------------------------------------------------------------
// Token refresh
// ---------------------------------------------------------------------------

// credential loads and refreshes the account credential when needed.
func (c *Client) credential(ctx context.Context, accountID string) (Credential, error) {
	_, payload, err := c.store.LoadCredentialPayload(ctx, accountID)
	if err != nil {
		return Credential{}, err
	}
	credential, err := DecodeCredential(payload)
	if err != nil {
		return Credential{}, err
	}
	if !credential.needsRefresh(time.Now()) {
		return credential, nil
	}
	return c.refreshCredential(ctx, accountID, credential)
}

func (c *Client) refreshCredential(ctx context.Context, accountID string, credential Credential) (Credential, error) {
	c.mu.Lock()
	if call, ok := c.refresh[accountID]; ok {
		c.mu.Unlock()
		select {
		case <-call.done:
			return call.cred, call.err
		case <-ctx.Done():
			return credential, ctx.Err()
		}
	}
	call := &refreshCall{done: make(chan struct{})}
	c.refresh[accountID] = call
	c.mu.Unlock()

	refreshed, err := c.refreshTokens(ctx, accountID, credential)
	if err == nil {
		if encoded, encErr := refreshed.Encode(); encErr == nil {
			_ = c.store.SaveCredentialPayload(ctx, accountID, CredentialFormat, encoded)
		}
	} else {
		_ = c.store.Observe(ctx, accountID, credential.AccountID, "login_required", err.Error(), accounts.KindAuth)
	}
	call.cred, call.err = refreshed, err
	close(call.done)
	c.mu.Lock()
	delete(c.refresh, accountID)
	c.mu.Unlock()
	return refreshed, err
}

func (c *Client) refreshTokens(ctx context.Context, accountID string, credential Credential) (Credential, error) {
	if strings.TrimSpace(credential.RefreshToken) == "" {
		return credential, fmt.Errorf("no refresh_token; re-login required")
	}
	form := url.Values{
		"grant_type":    {"refresh_token"},
		"client_id":     {ClientID},
		"refresh_token": {credential.RefreshToken},
		"scope":         {AuthScope},
	}
	payload, status, err := c.do(ctx, accountID, http.MethodPost, TokenURL, []byte(form.Encode()), func(h http.Header) {
		h.Set("Content-Type", "application/x-www-form-urlencoded")
		h.Set("Accept", "application/json")
	})
	if err != nil {
		return credential, err
	}
	if status != http.StatusOK {
		return credential, fmt.Errorf("token refresh failed with status %d: %s", status, strings.TrimSpace(string(payload)))
	}
	refreshed, err := parseTokenResponse(payload)
	if err != nil {
		return credential, err
	}
	// Preserve fields the refresh response may omit.
	if refreshed.RefreshToken == "" {
		refreshed.RefreshToken = credential.RefreshToken
	}
	if refreshed.AccountID == "" {
		refreshed.AccountID = credential.AccountID
	}
	if refreshed.Email == "" {
		refreshed.Email = credential.Email
	}
	return refreshed, nil
}

// ---------------------------------------------------------------------------
// Chat
// ---------------------------------------------------------------------------

func (c *Client) chatRequest(ctx context.Context, credential Credential, req translate.ChatRequest, accountID string) (*http.Request, providers.ResolvedChat, error) {
	body, resolved, err := buildBody(req, reasoningCaps(req.Model))
	if err != nil {
		return nil, providers.ResolvedChat{}, err
	}
	// A per-account prompt_cache_key keeps the upstream session cache warm and
	// satisfies the codex backend's Session-Id expectation (CLIProxyAPI sends a
	// session UUID; a stable account-scoped id is equivalent here because one
	// account is one upstream identity).
	sessionID := sessionIDFor(accountID)
	if sessionID != "" {
		var obj map[string]any
		if json.Unmarshal(body, &obj) == nil {
			obj["prompt_cache_key"] = sessionID
			body, _ = json.Marshal(obj)
		}
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, ChatBase+pathResponses, bytes.NewReader(body))
	if err != nil {
		return nil, providers.ResolvedChat{}, err
	}
	SetChatHeaders(httpReq.Header, credential, sessionID, true)
	applyCodexRequestHeaders(httpReq.Header, req.Model, usesResponsesLite(req.Model))
	return httpReq, resolved, nil
}

// sessionIDFor derives a deterministic UUID-shaped session id from the account
// id, matching the upstream's prompt_cache_key/Session-Id convention.
func sessionIDFor(accountID string) string {
	sum := sha256.Sum256([]byte("cli2api:codex:" + accountID))
	hexSum := hex.EncodeToString(sum[:])
	return fmt.Sprintf("%s-%s-%s-%s-%s", hexSum[0:8], hexSum[8:12], hexSum[12:16], hexSum[16:20], hexSum[20:32])
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
	c.observeQuotaHeaders(accountID, resp.Header)
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
	c.observeQuotaHeaders(accountID, resp.Header)
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

// ---------------------------------------------------------------------------
// Models / Probe / Quota
// ---------------------------------------------------------------------------

func (c *Client) Models(ctx context.Context, accountID string) ([]providers.ModelInfo, error) {
	return catalogModelInfos(), nil
}

func (c *Client) Probe(ctx context.Context, accountID string) (providers.AccountHealth, error) {
	credential, err := c.credential(ctx, accountID)
	if err != nil {
		return providers.AccountHealth{LastError: err.Error()}, nil
	}
	if !credential.Ready() {
		return providers.AccountHealth{UID: credential.AccountID, LastError: "codex credential incomplete; re-login required"}, nil
	}
	return providers.AccountHealth{Ready: true, Hot: true, UID: firstNonEmpty(credential.Email, credential.AccountID)}, nil
}

// observeQuotaHeaders snapshots the x-codex-* rate-limit headers every
// response carries, so the console can show the 5h/7d windows without a
// separate probe endpoint.
func (c *Client) observeQuotaHeaders(accountID string, header http.Header) {
	info := quotaFromHeaders(header)
	if info == nil {
		return
	}
	c.mu.Lock()
	if c.quotaCache == nil {
		c.quotaCache = map[string]*providers.QuotaInfo{}
	}
	c.quotaCache[accountID] = info
	c.mu.Unlock()
}

func (c *Client) Quota(ctx context.Context, accountID string) (*providers.QuotaInfo, error) {
	c.mu.Lock()
	cached := c.quotaCache[accountID]
	c.mu.Unlock()
	if cached != nil {
		return cached, nil
	}
	// No standalone usage endpoint: quota rides on chat response headers. Make a
	// minimal request only when nothing is cached yet? The codex upstream has no
	// cheap ping; report empty instead of burning a turn.
	if _, err := c.credential(ctx, accountID); err != nil {
		return nil, err
	}
	return &providers.QuotaInfo{
		Unit:       QuotaUnit,
		FetchedAt:  time.Now().UTC().Format(time.RFC3339),
		ProviderID: "codex",
	}, nil
}

func classifiedError(status int, body []byte) error {
	return &providers.Error{
		Kind:    Classify(status, string(body)).Kind,
		Status:  Classify(status, string(body)).Status,
		Message: Classify(status, string(body)).Message,
	}
}

func (c *Client) Adapter() providers.Adapter {
	return providers.Adapter{
		ID:              "codex",
		Credential:      credentialCodec{},
		Login:           c,
		Chat:            c,
		Models:          c,
		Classifier:      classifier{},
		ImportExport:    importer{},
		Prober:          c,
		StreamFormat:    providers.StreamFormatResponses,
		NativeResponses: c,
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

// randomHex returns n random bytes as lowercase hex.
func randomHex(n int) string {
	raw := make([]byte, n)
	if _, err := rand.Read(raw); err != nil {
		return fmt.Sprintf("%x", time.Now().UnixNano())
	}
	return hex.EncodeToString(raw)
}
