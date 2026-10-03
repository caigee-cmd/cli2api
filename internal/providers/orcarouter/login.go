package orcarouter

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/caigee-cmd/cli2api/internal/auth"
	"github.com/caigee-cmd/cli2api/internal/providers"
)

const (
	// loginPendingTTL bounds one attempt. OrcaRouter auth codes live 10 minutes;
	// the attempt is truncated slightly earlier so the user is told the window
	// closed rather than being handed an expired code to paste.
	loginPendingTTL = 9 * time.Minute
	// exchangeTimeout bounds the code-for-key call.
	exchangeTimeout = 30 * time.Second
	maxExchangeBody = 1 << 20
)

// StartLogin begins one PKCE attempt on a freshly bound loopback listener.
//
// Flow A (loopback redirect) is used because this process is self-hosted
// software that can listen on 127.0.0.1:<any port>; no redirect URI has to be
// pre-registered. The verifier and state are generated fresh for every attempt
// from a cryptographic RNG, and only the S256 challenge travels on the
// authorize URL. If the listener cannot be bound the caller can still finish the
// flow through CompleteLogin with a pasted callback URL (Flow B).
func (c *Client) StartLogin(ctx context.Context, accountID string) (providers.LoginSession, error) {
	if _, err := c.store.Get(ctx, accountID); err != nil {
		return providers.LoginSession{}, err
	}
	credential, _, loadErr := loadCredential(c.store, ctx, accountID)
	if loadErr != nil {
		credential = Credential{}
	}
	origins, err := c.originsForChat(ctx, credential)
	if err != nil {
		return providers.LoginSession{}, err
	}
	verifier, challenge, err := pkcePair()
	if err != nil {
		return providers.LoginSession{}, err
	}
	state, err := newState()
	if err != nil {
		return providers.LoginSession{}, err
	}

	callbackURL := CallbackOOB
	if port, listenErr := c.ensureCallback(); listenErr == nil {
		callbackURL = loopbackCallbackURL(port)
	}

	c.mu.Lock()
	c.attemptSeq++
	attempt := c.attemptSeq
	entry := &loginPending{
		verifier:    verifier,
		state:       state,
		callbackURL: callbackURL,
		authBase:    origins.AuthBase,
		apiBase:     origins.APIBase,
		attempt:     attempt,
		createdAt:   time.Now(),
	}
	c.pending[accountID] = entry
	c.pendingStates[state] = entry
	c.mu.Unlock()

	authURL, err := buildAuthorizeURL(origins.AuthBase, callbackURL, challenge, state)
	if err != nil {
		c.clearPending(accountID, state)
		return providers.LoginSession{}, err
	}
	entry.authURL = authURL
	return providers.LoginSession{AuthURL: authURL, State: state}, nil
}

// PollLogin reports whether the terminal exchange finished. The exchange itself
// runs in acceptCallback/CompleteLogin; this only observes the outcome.
func (c *Client) PollLogin(ctx context.Context, accountID string) (bool, string, error) {
	c.mu.Lock()
	pending := c.pending[accountID]
	c.mu.Unlock()
	if pending == nil {
		return false, "", fmt.Errorf("login not started for account %s", accountID)
	}
	if time.Since(pending.createdAt) > loginPendingTTL {
		c.clearPending(accountID, pending.state)
		return false, "", fmt.Errorf("orcarouter authorization window closed; start again")
	}
	c.mu.Lock()
	done := pending.done
	failed := pending.failed
	message := pending.message
	c.mu.Unlock()
	if failed {
		return false, "", fmt.Errorf("%s", redactSecrets(firstNonEmpty(message, "orcarouter login failed")))
	}
	if !done {
		return false, firstNonEmpty(message, "waiting for authorization"), nil
	}
	c.clearPending(accountID, pending.state)
	return true, "login complete", nil
}

// CompleteLogin finishes the flow from a callback URL or a bare code the user
// copied out of the browser (Flow B). The state check is skipped for a bare code
// because there is no redirect to compare against; the PKCE verifier is still
// required, so an intercepted code is useless on its own.
func (c *Client) CompleteLogin(ctx context.Context, accountID, callback string) error {
	raw := strings.TrimSpace(callback)
	if raw == "" {
		return errors.New("paste the callback URL or the code shown in the browser")
	}
	code, state, err := parseCallbackInput(raw)
	if err != nil {
		return err
	}
	pending := c.pendingFor(accountID, state, code)
	if pending == nil {
		pending = c.currentPending(accountID)
	}
	if pending == nil {
		return errors.New("no orcarouter login in progress; start again")
	}
	if time.Since(pending.createdAt) > loginPendingTTL {
		c.clearPending(accountID, pending.state)
		return errors.New("orcarouter authorization window closed; start again")
	}
	// A redirect carries the state back; a mismatch means the code did not come
	// from the attempt that owns the verifier.
	if state != "" && !statesMatch(pending.state, state) {
		c.failPending(accountID, pending, "authorization state did not match; start again")
		return errors.New("orcarouter authorization state mismatch")
	}
	if err := c.exchange(ctx, accountID, pending, code); err != nil {
		c.failPending(accountID, pending, err.Error())
		return err
	}
	c.mu.Lock()
	pending.done = true
	pending.message = "login complete"
	c.mu.Unlock()
	return nil
}

// acceptCallback is the loopback handler the redirect lands on. The raw request
// URL is either the absolute form the browser used or the origin-form request
// URI handed to the loopback server; the path portion of the authorize URL
// stored at start time anchors both.
func (c *Client) acceptCallback(ctx context.Context, rawURL string) error {
	address, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return errors.New("orcarouter callback was not a URL")
	}
	query, err := callbackQuery(address)
	if err != nil {
		return err
	}
	state := query.Get("state")
	if oauthErr := query.Get("error"); oauthErr != "" {
		description := firstNonEmpty(query.Get("error_description"), oauthErr)
		// A denial is a request-level outcome: the API layer reports it verbatim.
		// It must also end the in-flight attempt so the poller does not hang.
		if pending := c.pendingForState(state); pending != nil {
			c.failPendingByState(state, "orcarouter authorization "+description)
		}
		return fmt.Errorf("orcarouter authorization %s", description)
	}
	code := strings.TrimSpace(query.Get("code"))
	if code == "" {
		return errors.New("orcarouter callback carried no code")
	}
	pending := c.pendingForState(state)
	if pending == nil {
		return errors.New("orcarouter authorization state mismatch or unknown attempt")
	}
	// Compare state before doing anything else with the code.
	if !statesMatch(pending.state, state) {
		c.failPendingByState(state, "authorization state did not match")
		return errors.New("orcarouter authorization state mismatch")
	}
	accountID := c.accountForState(state)
	if accountID == "" {
		return errors.New("orcarouter callback did not match an account")
	}
	if err := c.exchange(ctx, accountID, pending, code); err != nil {
		c.failPendingByState(state, err.Error())
		return err
	}
	c.mu.Lock()
	pending.done = true
	pending.message = "login complete"
	c.mu.Unlock()
	return nil
}

// callbackQuery extracts the query parameters from a loopback callback. The
// absolute URL from the browser carries a host; the origin-form request URI
// does not, so it is re-anchored against the stored loopback callback URL.
func callbackQuery(address *url.URL) (url.Values, error) {
	if strings.HasPrefix(address.Path, CallbackPath) {
		if address.RawQuery != "" || strings.Contains(address.Path, "?") {
			return url.ParseQuery(strings.TrimPrefix(address.RawQuery, "?"))
		}
	}
	if address.RawQuery != "" {
		return url.ParseQuery(address.RawQuery)
	}
	return url.Values{}, nil
}

// exchange redeems the code for a durable key and persists it. The response's
// granted scope is recorded as-is; a narrower grant than requested is surfaced
// rather than assumed away.
func (c *Client) exchange(ctx context.Context, accountID string, pending *loginPending, code string) error {
	if strings.TrimSpace(code) == "" {
		return errors.New("orcarouter auth code missing")
	}
	body, err := json.Marshal(map[string]string{
		"code":                  code,
		"code_verifier":         pending.verifier,
		"code_challenge_method": "S256",
	})
	if err != nil {
		return err
	}
	client, err := c.httpClient(ctx, accountID)
	if err != nil {
		return err
	}
	scoped := *client
	scoped.Timeout = exchangeTimeout
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, exchangeURL(pending.authBase), bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := scoped.Do(req)
	if err != nil {
		return fmt.Errorf("orcarouter exchange failed: %s", redactSecrets(err.Error()))
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxExchangeBody))
	if err != nil {
		return fmt.Errorf("orcarouter exchange failed: %s", redactSecrets(err.Error()))
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		return errors.New("orcarouter declined to issue another key (429); reuse the existing key or try again later")
	}
	if resp.StatusCode >= 300 {
		// 403 = unknown / expired / already-used code, or verifier mismatch.
		// 400 = code_challenge_method downgrade defence. Never echo the body
		// verbatim if it might carry a credential.
		return fmt.Errorf("orcarouter exchange rejected the code (HTTP %d)", resp.StatusCode)
	}
	var granted struct {
		Key    string `json:"key"`
		UserID string `json:"user_id"`
		Scope  string `json:"scope"`
		oauthError
	}
	if err := json.Unmarshal(raw, &granted); err != nil {
		return errors.New("orcarouter exchange returned an unreadable response")
	}
	if strings.TrimSpace(granted.Key) == "" {
		return fmt.Errorf("orcarouter exchange returned no key: %s", redactSecrets(granted.message()))
	}
	if !strings.HasPrefix(FormatAPIKey(granted.Key), KeyPrefix) {
		return errors.New("orcarouter exchange returned an unexpected credential shape")
	}
	if scope := strings.TrimSpace(granted.Scope); scope != "" && scope != ScopeAPI {
		// Granted narrower than requested: keep the key, record what was granted.
		pending.message = "granted scope is " + scope
	}
	format := CredentialFormatOAuth
	if existing, _, loadErr := loadCredential(c.store, ctx, accountID); loadErr == nil && existing.Format == CredentialFormat {
		// An account created through the API-key entry stays on that format.
		format = CredentialFormat
	}
	credential := Credential{
		Format:     format,
		APIKey:     FormatAPIKey(granted.Key),
		AuthBase:   pending.authBase,
		APIBase:    pending.apiBase,
		UserID:     strings.TrimSpace(granted.UserID),
		Scope:      strings.TrimSpace(granted.Scope),
		State:      CredentialStateReady,
		Generation: pending.attempt,
	}
	encoded, err := credential.Encode()
	if err != nil {
		return err
	}
	if err := c.store.SaveCredentialPayload(ctx, accountID, format, encoded); err != nil {
		return err
	}
	_ = c.store.Observe(ctx, accountID, credential.UserID, "ready", "", "")
	return nil
}

// CancelLogin drops the in-flight attempt for this account and releases the
// loopback listener when nothing else is pending. It is safe to call with no
// attempt in flight, and it never touches a stored credential.
func (c *Client) CancelLogin(_ context.Context, accountID string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if pending, ok := c.pending[accountID]; ok {
		delete(c.pendingStates, pending.state)
		delete(c.pending, accountID)
	}
	if len(c.pending) == 0 && c.listener != nil {
		_ = c.listener.Close()
		c.listener = nil
		c.port = 0
	}
	return nil
}

// PendingLogin reports whether an attempt is currently in flight for the
// account. It exists for tests and for a console that wants to show the state.
func (c *Client) PendingLogin(accountID string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	_, ok := c.pending[accountID]
	return ok
}

// ensureCallback binds the loopback listener once and reuses it.
func (c *Client) ensureCallback() (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.listener != nil && c.port > 0 {
		return c.port, nil
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, fmt.Errorf("orcarouter callback listen: %w", err)
	}
	c.listener = ln
	c.port = ln.Addr().(*net.TCPAddr).Port
	auth.ServeLoopback(ln, CallbackPath, "OrcaRouter", c.acceptCallback)
	return c.port, nil
}

func (c *Client) pendingFor(accountID, state, code string) *loginPending {
	if state != "" {
		if pending := c.pendingForState(state); pending != nil {
			return pending
		}
	}
	return c.currentPending(accountID)
}

func (c *Client) pendingForState(state string) *loginPending {
	if strings.TrimSpace(state) == "" {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.pendingStates[state]
}

func (c *Client) currentPending(accountID string) *loginPending {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.pending[accountID]
}

func (c *Client) accountForState(state string) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	for accountID, pending := range c.pending {
		if pending.state == state {
			return accountID
		}
	}
	return ""
}

func (c *Client) failPending(accountID string, pending *loginPending, message string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if pending != nil {
		pending.failed = true
		pending.message = message
	}
	if current := c.pending[accountID]; current == pending {
		delete(c.pending, accountID)
	}
}

func (c *Client) failPendingByState(state, message string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	pending := c.pendingStates[state]
	if pending == nil {
		return
	}
	if pending.done {
		return
	}
	pending.failed = true
	pending.message = message
	for accountID, entry := range c.pending {
		if entry == pending {
			delete(c.pending, accountID)
			break
		}
	}
	delete(c.pendingStates, state)
}

func (c *Client) clearPending(accountID, state string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if state != "" {
		delete(c.pendingStates, state)
	}
	if current := c.pending[accountID]; current == nil || state == "" || current.state == state {
		delete(c.pending, accountID)
	}
}

// parseCallbackInput accepts a full callback URL, a fragment/query string, or a
// bare authorization code.
func parseCallbackInput(raw string) (code, state string, err error) {
	if strings.Contains(raw, "://") {
		parsed, parseErr := url.Parse(raw)
		if parseErr != nil {
			return "", "", parseErr
		}
		query := parsed.Query()
		if oauthErr := query.Get("error"); oauthErr != "" {
			return "", "", fmt.Errorf("orcarouter authorization %s", firstNonEmpty(query.Get("error_description"), oauthErr))
		}
		return strings.TrimSpace(query.Get("code")), strings.TrimSpace(query.Get("state")), nil
	}
	if strings.HasPrefix(raw, "?") || strings.Contains(raw, "code=") {
		query, parseErr := url.ParseQuery(strings.TrimPrefix(raw, "?"))
		if parseErr != nil {
			return "", "", parseErr
		}
		if oauthErr := query.Get("error"); oauthErr != "" {
			return "", "", fmt.Errorf("orcarouter authorization %s", firstNonEmpty(query.Get("error_description"), oauthErr))
		}
		return strings.TrimSpace(query.Get("code")), strings.TrimSpace(query.Get("state")), nil
	}
	return raw, "", nil
}
