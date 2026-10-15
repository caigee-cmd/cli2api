package qwenwork

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
	proxyutil "github.com/caigee-cmd/cli2api/internal/proxy"
	"github.com/caigee-cmd/cli2api/internal/translate"
)

// Store is the persistence surface the adapter needs. It matches the SQLite
// store without importing the concrete type.
type Store interface {
	Get(ctx context.Context, id string) (accounts.Account, error)
	LoadCredentialPayload(ctx context.Context, accountID string) (string, []byte, error)
	SaveCredentialPayload(ctx context.Context, accountID, format string, payload []byte) error
	Observe(ctx context.Context, id, remoteUID, status, lastError, lastKind string) error
}

// SecretReader is optional; missing it means no global proxy, not an error.
type SecretReader interface {
	GetSecret(context.Context, string) (string, bool, error)
}

// Client is the in-process QwenWork adapter. Region comes from the account
// record; every gateway call picks its site from it.
type Client struct {
	store Store
	http  *http.Client

	transports proxyutil.TransportCache

	mu          sync.Mutex
	loginStates map[string]*loginState
	sessions    map[string]*session
	catalog     map[string]map[string]providers.ModelInfo
	refresh     map[string]*refreshCall
}

// refreshCall singleflights token refresh by account. QwenWork rotates the
// refresh token, so two concurrent exchanges with the same token would have
// the loser's token already consumed, which reads as session-dead and
// disables the account.
type refreshCall struct {
	done chan struct{}
	cred Credential
	err  error
}

// loginState is one pending device authorization round.
type loginState struct {
	verifier  string
	nonce     string
	machineID string
	createdAt time.Time
}

const (
	catalogTimeout   = 15 * time.Second
	loginStateTTL    = 10 * time.Minute
	refreshMargin    = 2 * time.Minute
	refreshTimeout   = 30 * time.Second
	accountContextOK = "ok"
	// maxChatBody bounds the buffered non-stream upstream body.
	maxChatBody = 16 << 20
)

// modelListEntry is one qwork model from /algo/api/v2/model/list.
type modelListEntry struct {
	Key             string `json:"key"`
	DisplayName     string `json:"display_name"`
	IsVL            bool   `json:"is_vl"`
	IsReasoning     bool   `json:"is_reasoning"`
	MaxInputTokens  int    `json:"max_input_tokens"`
	MaxOutputTokens int    `json:"max_output_tokens"`
	Credits         string `json:"credits"`
	Enable          *bool  `json:"enable"`
}

func (e modelListEntry) modelInfo() providers.ModelInfo {
	display := strings.TrimSpace(e.DisplayName)
	if display == "" {
		display = e.Key
	}
	return providers.ModelInfo{
		NativeModel: e.Key,
		PublicModel: e.Key,
		DisplayName: display,
		Credits:     e.Credits,
		Capabilities: providers.ModelCapabilities{
			ContextWindow: e.MaxInputTokens,
			MaxOutput:     e.MaxOutputTokens,
			Tools:         true,
			Images:        e.IsVL,
			Reasoning:     e.IsReasoning,
		},
		Scene: scene,
	}
}

func NewClient(store Store) *Client {
	return &Client{
		store:       store,
		http:        &http.Client{Timeout: 120 * time.Second},
		loginStates: map[string]*loginState{},
		sessions:    map[string]*session{},
		refresh:     map[string]*refreshCall{},
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

func (c *Client) effectiveProxy(ctx context.Context, account accounts.Account) (string, error) {
	if value := strings.TrimSpace(account.ProxyURL); value != "" {
		return value, nil
	}
	return c.globalProxy(ctx)
}

func (c *Client) httpClient(ctx context.Context, accountID string) (*http.Client, error) {
	account, err := c.store.Get(ctx, accountID)
	if err != nil {
		return nil, err
	}
	return c.httpClientFor(ctx, account)
}

// httpClientFor builds the proxied client for an already-loaded account so
// the chat path reads the account row once per request.
func (c *Client) httpClientFor(ctx context.Context, account accounts.Account) (*http.Client, error) {
	rawProxy, err := c.effectiveProxy(ctx, account)
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

// setPlainHeaders carries the desktop identity for unauthenticated gateway
// calls (login, poll, refresh, userinfo, account-context).
func setPlainHeaders(h http.Header, st site, bearer string) {
	h.Set("Accept", "application/json, text/plain, */*")
	h.Set("Content-Type", "application/json")
	h.Set("User-Agent", st.userAgent())
	if bearer != "" {
		h.Set("Authorization", "Bearer "+bearer)
	}
}

func applyHeaders(h http.Header, headers map[string]string) {
	for key, value := range headers {
		h.Set(key, value)
	}
}

// siteFor resolves the site for the account's region.
func (c *Client) siteFor(ctx context.Context, accountID string) (site, error) {
	account, err := c.store.Get(ctx, accountID)
	if err != nil {
		return site{}, err
	}
	return siteFor(account.ProviderRegion)
}

// StartLogin builds the device authorization URL. The link is single-use and
// must be opened in a browser already signed in to the target account.
func (c *Client) StartLogin(ctx context.Context, accountID string) (providers.LoginSession, error) {
	st, err := c.siteFor(ctx, accountID)
	if err != nil {
		return providers.LoginSession{}, err
	}
	verifier, challenge, err := pkcePair()
	if err != nil {
		return providers.LoginSession{}, err
	}
	nonce := newID()
	machineID := newID()
	query := url.Values{
		"challenge":        {challenge},
		"challenge_method": {"S256"},
		"nonce":            {nonce},
		"machine_id":       {machineID},
		"client_id":        {st.ClientID},
		"redirect_uri":     {st.RedirectURI},
	}
	c.mu.Lock()
	if c.loginStates == nil {
		c.loginStates = map[string]*loginState{}
	}
	// Abandoned rounds are never polled again; drop them here.
	for id, pending := range c.loginStates {
		if time.Since(pending.createdAt) > loginStateTTL {
			delete(c.loginStates, id)
		}
	}
	c.loginStates[accountID] = &loginState{
		verifier:  verifier,
		nonce:     nonce,
		machineID: machineID,
		createdAt: time.Now(),
	}
	c.mu.Unlock()
	return providers.LoginSession{
		AuthURL: st.Gateway + pathDeviceAuth + "?" + query.Encode(),
		State:   nonce,
	}, nil
}

// PollLogin checks the device token poll endpoint, then completes the
// credential from the grant plus /api/v1/userinfo.
func (c *Client) PollLogin(ctx context.Context, accountID string) (bool, string, error) {
	c.mu.Lock()
	state := c.loginStates[accountID]
	c.mu.Unlock()
	if state == nil {
		return false, "", fmt.Errorf("login not started for account %s", accountID)
	}
	if time.Since(state.createdAt) > loginStateTTL {
		c.mu.Lock()
		delete(c.loginStates, accountID)
		c.mu.Unlock()
		return false, "", fmt.Errorf("login expired; start again")
	}
	st, err := c.siteFor(ctx, accountID)
	if err != nil {
		return false, "", err
	}
	pollURL := st.Gateway + pathDevicePoll + "?nonce=" + url.QueryEscape(state.nonce) +
		"&verifier=" + url.QueryEscape(state.verifier) + "&challenge_method=S256"
	body, status, err := c.do(ctx, accountID, http.MethodGet, pollURL, nil,
		func(h http.Header) { setPlainHeaders(h, st, "") })
	if err != nil {
		return false, "", err
	}
	if status == http.StatusNotFound || status == http.StatusAccepted {
		return false, "waiting for authorization", nil
	}
	if status >= 300 {
		return false, "", fmt.Errorf("poll status=%d: %s", status, truncateBody(body))
	}
	var grant grant
	if json.Unmarshal(body, &grant) != nil || grant.accessToken() == "" {
		return false, "waiting for authorization", nil
	}
	credential := Credential{
		AccessToken:  grant.accessToken(),
		RefreshToken: grant.RefreshToken,
		ExpiresAt:    grant.expiry(time.Now()),
		UID:          grant.UserID,
		Nickname:     grant.UserName,
		MachineID:    state.machineID,
	}
	if uid, nickname, email := c.fetchUserInfo(ctx, accountID, st, credential.AccessToken); uid != "" || nickname != "" || email != "" {
		if uid != "" {
			credential.UID = uid
		}
		if credential.Nickname == "" && nickname != "" {
			credential.Nickname = nickname
		}
		if email != "" {
			credential.Email = email
		}
	}
	if credential.UID == "" {
		return false, "", fmt.Errorf("login grant missing user identity; try again")
	}
	payload, err := credential.Encode()
	if err != nil {
		return false, "", err
	}
	if err := c.store.SaveCredentialPayload(ctx, accountID, CredentialFormat, payload); err != nil {
		return false, "", err
	}
	_ = c.store.Observe(ctx, accountID, credential.UID, "ready", "", "")
	c.mu.Lock()
	delete(c.loginStates, accountID)
	c.mu.Unlock()
	return true, "login complete", nil
}

// fetchUserInfo fills the profile from /api/v1/userinfo. Failures leave the
// grant identity in place.
func (c *Client) fetchUserInfo(ctx context.Context, accountID string, st site, accessToken string) (uid, nickname, email string) {
	body, status, err := c.do(ctx, accountID, http.MethodGet, st.Gateway+pathUserInfo, nil,
		func(h http.Header) { setPlainHeaders(h, st, accessToken) })
	if err != nil || status >= 300 {
		return "", "", ""
	}
	var info struct {
		ID    string `json:"id"`
		Name  string `json:"name"`
		Email string `json:"email"`
	}
	if json.Unmarshal(body, &info) != nil {
		return "", "", ""
	}
	return strings.TrimSpace(info.ID), strings.TrimSpace(info.Name), strings.TrimSpace(info.Email)
}

// credential loads the stored token and refreshes it when less than two
// minutes remain. Concurrent callers share one refresh round-trip (the
// refresh token is single-use). A failed refresh falls back to the stored
// credential so a flaky refresh endpoint never blocks a possibly-valid token.
func (c *Client) credential(ctx context.Context, accountID string) (Credential, error) {
	credential, err := c.loadCredential(ctx, accountID)
	if err != nil {
		return Credential{}, err
	}
	if !credential.needsRefresh(time.Now()) {
		return credential, nil
	}
	refreshed, err := c.singleFlightRefresh(ctx, accountID, credential)
	if err != nil {
		return credential, nil
	}
	return refreshed, nil
}

func (c *Client) loadCredential(ctx context.Context, accountID string) (Credential, error) {
	_, payload, err := c.store.LoadCredentialPayload(ctx, accountID)
	if err != nil {
		return Credential{}, err
	}
	return DecodeCredential(payload)
}

func (c Credential) needsRefresh(now time.Time) bool {
	return c.ExpiresAt == 0 || now.Add(refreshMargin).Unix() >= c.ExpiresAt
}

// singleFlightRefresh runs at most one refresh per account at a time. The
// leader re-reads the store first: a caller that loaded the credential before
// an earlier refresh finished still holds the consumed refresh token, and
// replaying it would read as session-dead. The exchange runs on a detached
// context so one cancelled request cannot fail the waiters or lose the
// rotated pair before it is saved.
func (c *Client) singleFlightRefresh(ctx context.Context, accountID string, credential Credential) (Credential, error) {
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

	refreshCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), refreshTimeout)
	refreshed, err := c.refreshIfStale(refreshCtx, accountID, credential)
	cancel()
	call.cred, call.err = refreshed, err
	close(call.done)
	c.mu.Lock()
	delete(c.refresh, accountID)
	c.mu.Unlock()
	return refreshed, err
}

func (c *Client) refreshIfStale(ctx context.Context, accountID string, credential Credential) (Credential, error) {
	stored, err := c.loadCredential(ctx, accountID)
	if err != nil {
		return credential, err
	}
	if stored.RefreshToken != credential.RefreshToken || !stored.needsRefresh(time.Now()) {
		return stored, nil
	}
	return c.Refresh(ctx, accountID, stored)
}

// resolvedAccount loads the credential, the account row, and the account's
// site in one pass.
func (c *Client) resolvedAccount(ctx context.Context, accountID string) (Credential, accounts.Account, site, error) {
	credential, err := c.credential(ctx, accountID)
	if err != nil {
		return Credential{}, accounts.Account{}, site{}, err
	}
	account, err := c.store.Get(ctx, accountID)
	if err != nil {
		return Credential{}, accounts.Account{}, site{}, err
	}
	st, err := siteFor(account.ProviderRegion)
	if err != nil {
		return Credential{}, accounts.Account{}, site{}, err
	}
	return credential, account, st, nil
}

// resolvedCredential loads the credential and resolves the account's site.
func (c *Client) resolvedCredential(ctx context.Context, accountID string) (Credential, site, error) {
	credential, _, st, err := c.resolvedAccount(ctx, accountID)
	return credential, st, err
}

// Refresh exchanges the refresh token. The endpoint rotates the refresh
// token, so the response pair replaces the stored one. Session-dead errors
// flip the account to login_required through Observe.
func (c *Client) Refresh(ctx context.Context, accountID string, credential Credential) (Credential, error) {
	if strings.TrimSpace(credential.RefreshToken) == "" {
		return credential, fmt.Errorf("qwenwork credential has no refresh token")
	}
	st, err := c.siteFor(ctx, accountID)
	if err != nil {
		return credential, err
	}
	payload, err := json.Marshal(map[string]string{"refresh_token": credential.RefreshToken})
	if err != nil {
		return credential, err
	}
	body, status, err := c.do(ctx, accountID, http.MethodPost, st.Gateway+pathDeviceRefresh, payload,
		func(h http.Header) { setPlainHeaders(h, st, "") })
	if err != nil {
		return credential, err
	}
	classified := Classify(status, string(body))
	if classified.Kind == accounts.KindAuth {
		_ = c.store.Observe(ctx, accountID, credential.UID, "login_required", "session dead; re-login required", accounts.KindAuth)
		return credential, fmt.Errorf("qwenwork session dead: re-login required")
	}
	if status >= 300 || classified.Kind != "" {
		return credential, fmt.Errorf("refresh status=%d kind=%s", status, classified.Kind)
	}
	var grant grant
	if err := json.Unmarshal(body, &grant); err != nil {
		return credential, fmt.Errorf("refresh parse: %w", err)
	}
	if grant.accessToken() == "" || grant.RefreshToken == "" {
		return credential, fmt.Errorf("refresh response missing token pair")
	}
	credential.AccessToken = grant.accessToken()
	credential.RefreshToken = grant.RefreshToken
	credential.ExpiresAt = grant.expiry(time.Now())
	if credential.UID == "" && grant.UserID != "" {
		credential.UID = grant.UserID
	}
	if credential.UID == "" {
		c.fillIdentity(ctx, accountID, st, &credential)
	}
	if err := c.saveCredential(ctx, accountID, credential); err != nil {
		return credential, err
	}
	return credential, nil
}

func (c *Client) saveCredential(ctx context.Context, accountID string, credential Credential) error {
	encoded, err := credential.Encode()
	if err != nil {
		return err
	}
	return c.store.SaveCredentialPayload(ctx, accountID, CredentialFormat, encoded)
}

// fillIdentity completes uid and profile fields from /api/v1/userinfo. An
// imported credential may lack the uid; it stays not-ready until this fills it.
func (c *Client) fillIdentity(ctx context.Context, accountID string, st site, credential *Credential) {
	uid, nickname, email := c.fetchUserInfo(ctx, accountID, st, credential.AccessToken)
	if credential.UID == "" {
		credential.UID = uid
	}
	if credential.Nickname == "" {
		credential.Nickname = nickname
	}
	if credential.Email == "" {
		credential.Email = email
	}
}

// sessionFor caches one COSY envelope per account and rebuilds it whenever
// the access token changes.
func (c *Client) sessionFor(accountID string, credential Credential) (*session, error) {
	c.mu.Lock()
	cached := c.sessions[accountID]
	c.mu.Unlock()
	if cached != nil && cached.token == credential.AccessToken {
		return cached, nil
	}
	built, err := newSession(credential)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	if c.sessions == nil {
		c.sessions = map[string]*session{}
	}
	c.sessions[accountID] = built
	c.mu.Unlock()
	return built, nil
}

func (c *Client) rememberCatalog(st site, models []providers.ModelInfo) {
	catalog := make(map[string]providers.ModelInfo, len(models))
	for _, model := range models {
		catalog[model.NativeModel] = model
		if model.PublicModel != model.NativeModel {
			catalog[model.PublicModel] = model
		}
	}
	c.mu.Lock()
	if c.catalog == nil {
		c.catalog = map[string]map[string]providers.ModelInfo{}
	}
	c.catalog[st.Gateway] = catalog
	c.mu.Unlock()
}

// catalogModel resolves the request model against the site's remembered
// catalog. QwenWork public and native ids are identical, so an unknown id
// passes through unchanged with ok=false; no alias is invented.
func (c *Client) catalogModel(st site, model string) (providers.ModelInfo, bool) {
	model = strings.TrimSpace(model)
	if model == "" || c == nil {
		return providers.ModelInfo{}, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	catalog := c.catalog[st.Gateway]
	if info, ok := catalog[model]; ok {
		return info, true
	}
	if info, ok := catalog[accounts.CanonicalModelID(model)]; ok {
		return info, true
	}
	return providers.ModelInfo{}, false
}

// chatRequest builds the signed agent_chat_generation request.
func (c *Client) chatRequest(ctx context.Context, accountID string, credential Credential, st site, req translate.ChatRequest) (*http.Request, string, error) {
	modelKey := strings.TrimSpace(req.Model)
	info, known := c.catalogModel(st, modelKey)
	if known {
		modelKey = info.NativeModel
	}
	if modelKey == "" {
		modelKey = st.DefaultModel
	}
	maxOutput := 0
	if known {
		maxOutput = info.Capabilities.MaxOutput
	}
	encoded, err := json.Marshal(buildChatBody(req, modelKey, maxOutput))
	if err != nil {
		return nil, "", err
	}
	sess, err := c.sessionFor(accountID, credential)
	if err != nil {
		return nil, "", err
	}
	endpoint := st.Gateway + pathChat
	headers, err := sess.sign(st, string(encoded), endpoint, modelKey, "text/event-stream", time.Now())
	if err != nil {
		return nil, "", err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(encoded))
	if err != nil {
		return nil, "", err
	}
	applyHeaders(httpReq.Header, headers)
	return httpReq, modelKey, nil
}

func (c *Client) ChatNonStream(ctx context.Context, accountID string, req translate.ChatRequest) (providers.ChatOutcome, error) {
	credential, account, st, err := c.resolvedAccount(ctx, accountID)
	if err != nil {
		return providers.ChatOutcome{}, err
	}
	httpReq, model, err := c.chatRequest(ctx, accountID, credential, st, req)
	if err != nil {
		return providers.ChatOutcome{}, err
	}
	client, err := c.httpClientFor(ctx, account)
	if err != nil {
		return providers.ChatOutcome{}, err
	}
	client.Timeout = 0
	resp, err := client.Do(httpReq)
	if err != nil {
		return providers.ChatOutcome{}, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxChatBody+1))
	if err != nil {
		return providers.ChatOutcome{}, fmt.Errorf("read qwenwork stream: %w", err)
	}
	if resp.StatusCode >= 300 {
		return providers.ChatOutcome{}, classifiedError(resp.StatusCode, raw)
	}
	if len(raw) > maxChatBody {
		// A truncated body would aggregate into a silently partial answer.
		return providers.ChatOutcome{}, &providers.Error{
			Kind:    accounts.KindUnavailable,
			Status:  http.StatusBadGateway,
			Code:    "upstream_body_too_large",
			Message: fmt.Sprintf("qwenwork upstream body exceeds %d bytes; use streaming", maxChatBody),
		}
	}
	return outcomeFromUpstreamSSE(model, raw)
}

func (c *Client) ChatStream(ctx context.Context, accountID string, req translate.ChatRequest) (*http.Response, providers.ResolvedChat, error) {
	credential, account, st, err := c.resolvedAccount(ctx, accountID)
	if err != nil {
		return nil, providers.ResolvedChat{}, err
	}
	httpReq, model, err := c.chatRequest(ctx, accountID, credential, st, req)
	if err != nil {
		return nil, providers.ResolvedChat{}, err
	}
	client, err := c.httpClientFor(ctx, account)
	if err != nil {
		return nil, providers.ResolvedChat{}, err
	}
	client.Timeout = 0
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, providers.ResolvedChat{}, err
	}
	if resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		resp.Body.Close()
		return nil, providers.ResolvedChat{}, classifiedError(resp.StatusCode, raw)
	}
	guarded, err := ensureMeaningfulUpstream(resp)
	if err != nil {
		// ensureMeaningfulUpstream closes the body on failure; no client
		// bytes have been written yet, so the executor can still fail over.
		return nil, providers.ResolvedChat{}, err
	}
	return rewriteUpstreamSSE(guarded, model), providers.ResolvedChat{}, nil
}

// Models fetches the live qwork catalog. There is no static fallback list.
func (c *Client) Models(ctx context.Context, accountID string) ([]providers.ModelInfo, error) {
	credential, st, err := c.resolvedCredential(ctx, accountID)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, catalogTimeout)
	defer cancel()
	sess, err := c.sessionFor(accountID, credential)
	if err != nil {
		return nil, err
	}
	endpoint := st.Gateway + pathModelList
	headers, err := sess.sign(st, "", endpoint, "", "application/json", time.Now())
	if err != nil {
		return nil, err
	}
	body, status, err := c.do(ctx, accountID, http.MethodGet, endpoint, nil,
		func(h http.Header) { applyHeaders(h, headers) })
	if err != nil {
		return nil, err
	}
	if status >= 300 {
		return nil, fmt.Errorf("models status=%d: %s", status, catalogErrorBody(body))
	}
	var parsed struct {
		Qwork []modelListEntry `json:"qwork"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("models parse: %s", catalogErrorBody(body))
	}
	var out []providers.ModelInfo
	for _, entry := range parsed.Qwork {
		if entry.Enable != nil && !*entry.Enable {
			continue
		}
		if strings.TrimSpace(entry.Key) == "" {
			continue
		}
		out = append(out, entry.modelInfo())
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("qwenwork model list returned no models")
	}
	c.rememberCatalog(st, out)
	return out, nil
}

// catalogErrorBody keeps model-list errors bounded and free of HTML pages.
func catalogErrorBody(body []byte) string {
	text := strings.TrimSpace(string(body))
	if strings.Contains(strings.ToLower(text), "<html") || strings.Contains(strings.ToLower(text), "<!doctype") {
		return "upstream html error page"
	}
	if len(text) > 240 {
		return text[:240]
	}
	return text
}

// Probe reports whether the stored credential is usable. There is no child
// process; Hot mirrors Ready so the console treats signed-in accounts as
// available. Token death surfaces through the refresh path's login_required
// observation, not through a probe round-trip.
func (c *Client) Probe(ctx context.Context, accountID string) (providers.AccountHealth, error) {
	credential, err := c.credential(ctx, accountID)
	if err != nil {
		return providers.AccountHealth{LastError: err.Error()}, nil
	}
	if !credential.Ready() && strings.TrimSpace(credential.AccessToken) != "" {
		if st, siteErr := c.siteFor(ctx, accountID); siteErr == nil {
			c.fillIdentity(ctx, accountID, st, &credential)
			if credential.Ready() {
				_ = c.saveCredential(ctx, accountID, credential)
			}
		}
	}
	if !credential.Ready() {
		msg := "qwenwork credential incomplete; re-login required"
		return providers.AccountHealth{UID: credential.UID, LastError: msg}, nil
	}
	return providers.AccountHealth{
		Ready: true,
		Hot:   true,
		UID:   credential.UID,
	}, nil
}

// Quota maps the account-context document onto QuotaInfo.
func (c *Client) Quota(ctx context.Context, accountID string) (*providers.QuotaInfo, error) {
	credential, st, err := c.resolvedCredential(ctx, accountID)
	if err != nil {
		return nil, err
	}
	body, status, err := c.do(ctx, accountID, http.MethodGet, st.Gateway+pathAccountContext, nil,
		func(h http.Header) { setPlainHeaders(h, st, credential.AccessToken) })
	if err != nil {
		return nil, err
	}
	if status >= 300 {
		return nil, fmt.Errorf("account-context status=%d: %s", status, truncateBody(body))
	}
	var env struct {
		Code string `json:"code"`
		Data struct {
			User struct {
				Name  string `json:"name"`
				Email string `json:"email"`
			} `json:"user"`
			Plan struct {
				Name          string `json:"name"`
				UserType      string `json:"user_type"`
				IsPersonalVer bool   `json:"is_personal_version"`
			} `json:"plan"`
			Quota struct {
				Total     *float64 `json:"total"`
				Used      *float64 `json:"used"`
				Remaining *float64 `json:"remaining"`
				Exceeded  bool     `json:"exceeded"`
			} `json:"quota"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return nil, fmt.Errorf("account-context parse: %w", err)
	}
	if env.Code != "" && env.Code != accountContextOK {
		return nil, fmt.Errorf("account-context code=%s", env.Code)
	}
	quota := env.Data.Quota
	if quota.Total == nil && quota.Used == nil && quota.Remaining == nil {
		return nil, fmt.Errorf("account-context returned no quota")
	}
	used, total, remaining := numberOrZero(quota.Used), numberOrZero(quota.Total), numberOrZero(quota.Remaining)
	if total <= 0 && remaining > 0 {
		total = remaining
	}
	if used <= 0 && total > remaining {
		used = total - remaining
	}
	percentage := 0.0
	if total > 0 {
		percentage = used / total * 100
		if percentage < 0 {
			percentage = 0
		}
		if percentage > 100 {
			percentage = 100
		}
	}
	return &providers.QuotaInfo{
		Used:       used,
		Total:      total,
		Remaining:  remaining,
		Percentage: percentage,
		Unit:       "credits",
		Exceeded:   quota.Exceeded || (total > 0 && remaining <= 0),
		FetchedAt:  time.Now().UTC().Format(time.RFC3339),
		ProviderID: "qwenwork",
		Plan:       env.Data.Plan.Name,
	}, nil
}

func numberOrZero(value *float64) float64 {
	if value == nil {
		return 0
	}
	return *value
}

// Adapter returns the provider capability bundle for registration.
func (c *Client) Adapter() providers.Adapter {
	return providers.Adapter{
		ID:         "qwenwork",
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
