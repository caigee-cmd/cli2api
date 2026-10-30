package codearts

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
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
)

// loginState is one in-flight browser authorization. The local callback
// listener receives the authorization code; the ticket polling fallback is
// still attempted when the portal answers with a secret instead.
type loginState struct {
	authURL   string
	ticketID  string
	secret    string
	verifier  string
	dpop      map[string]string
	port      int
	expiresAt time.Time
	listener  net.Listener

	mu        sync.Mutex
	done      bool
	err       error
	completed chan struct{}
}

func (s *loginState) finish(err error) {
	s.mu.Lock()
	if s.done {
		s.mu.Unlock()
		return
	}
	s.done = true
	s.err = err
	s.mu.Unlock()
	close(s.completed)
}

func (s *loginState) result() (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.done, s.err
}

// StartLogin builds the portal URL and starts the loopback listener. The
// console shows the URL; PollLogin waits for the callback to finish.
func (c *Client) StartLogin(ctx context.Context, accountID string) (providers.LoginSession, error) {
	c.cleanupLogins()
	ticketID := randomHex(16)
	verifier, challenge, err := pkcePair()
	if err != nil {
		return providers.LoginSession{}, err
	}
	dpop, err := newDPoPPrivateJWK()
	if err != nil {
		return providers.LoginSession{}, err
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return providers.LoginSession{}, fmt.Errorf("codearts login listener: %w", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	authURL := c.buildAuthorizeURL(ticketID, challenge, port)
	state := &loginState{
		authURL:   authURL,
		ticketID:  ticketID,
		verifier:  verifier,
		dpop:      dpop,
		port:      port,
		expiresAt: c.now().Add(loginTTL),
		listener:  listener,
		completed: make(chan struct{}),
	}
	auth.ServeLoopback(listener, OAuthRedirect, "CodeArts", func(callbackCtx context.Context, rawURL string) error {
		return c.handleCallback(callbackCtx, accountID, state, rawURL)
	})
	c.mu.Lock()
	if c.logins == nil {
		c.logins = map[string]*loginState{}
	}
	if previous := c.logins[accountID]; previous != nil {
		previous.finish(fmt.Errorf("superseded by a new login round"))
		_ = previous.listener.Close()
	}
	c.logins[accountID] = state
	c.mu.Unlock()
	return providers.LoginSession{AuthURL: authURL, State: ticketID}, nil
}

// PollLogin advances one login round. It returns done=true only after the
// credential has been persisted.
func (c *Client) PollLogin(ctx context.Context, accountID string) (bool, string, error) {
	c.mu.Lock()
	state := c.logins[accountID]
	c.mu.Unlock()
	if state == nil {
		return false, "", fmt.Errorf("login not started for account %s", accountID)
	}
	select {
	case <-state.completed:
		done, err := state.result()
		if err != nil {
			c.clearLogin(accountID)
			return false, "", err
		}
		if done {
			c.clearLogin(accountID)
			return true, "login complete", nil
		}
	case <-time.After(time.Second):
	}
	if c.now().After(state.expiresAt) {
		c.clearLogin(accountID)
		return false, "", fmt.Errorf("codearts login timed out; start again")
	}
	state.mu.Lock()
	secret := state.secret
	state.mu.Unlock()
	if secret != "" {
		if done, message, err := c.pollTicket(ctx, accountID, state, secret); err != nil {
			return false, "", err
		} else if done {
			return true, message, nil
		}
	}
	return false, "waiting for browser authorization", nil
}

// CompleteLogin accepts a callback URL pasted by the operator when the
// browser cannot reach the proxy host.
func (c *Client) CompleteLogin(ctx context.Context, accountID, callbackURL string) error {
	c.mu.Lock()
	state := c.logins[accountID]
	c.mu.Unlock()
	if state == nil {
		return fmt.Errorf("login not started for account %s", accountID)
	}
	parsed, err := url.Parse(strings.TrimSpace(callbackURL))
	if err != nil || parsed.Scheme != "http" || parsed.User != nil || parsed.Path != OAuthRedirect {
		return fmt.Errorf("callback URL must be the CodeArts http://127.0.0.1 callback")
	}
	host := parsed.Hostname()
	if host != "127.0.0.1" && host != "localhost" && host != "::1" {
		return fmt.Errorf("callback URL must point at a loopback host")
	}
	if parsed.Port() != fmt.Sprint(state.port) {
		return fmt.Errorf("callback port does not match this login round")
	}
	code := parsed.Query().Get("code")
	if code == "" {
		return fmt.Errorf("callback URL has no authorization code")
	}
	if err := c.completeCode(ctx, accountID, state, code); err != nil {
		return err
	}
	return nil
}

func (c *Client) handleCallback(ctx context.Context, accountID string, state *loginState, rawURL string) error {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return err
	}
	query := parsed.Query()
	if code := query.Get("code"); code != "" {
		return c.completeCode(ctx, accountID, state, code)
	}
	if secret := query.Get("secret"); secret != "" {
		state.mu.Lock()
		state.secret = secret
		state.mu.Unlock()
		return nil
	}
	return fmt.Errorf("callback is missing code or secret")
}

func (c *Client) completeCode(ctx context.Context, accountID string, state *loginState, code string) error {
	credential := Credential{
		Format:         CredentialFormat,
		ClientID:       ClientID,
		CodeVerifier:   state.verifier,
		DPoPPrivateKey: state.dpop,
	}
	token, err := c.exchangeCode(ctx, credential, code, state.port)
	if err != nil {
		state.finish(err)
		return err
	}
	credential = applyToken(credential, token)
	if credential.UserID == "" || credential.UserName == "" {
		if uid, name, domain, userErr := c.currentUser(ctx, credential); userErr == nil {
			credential.UserID = firstNonEmpty(credential.UserID, uid)
			credential.UserName = firstNonEmpty(credential.UserName, name)
			credential.DomainID = firstNonEmpty(credential.DomainID, domain)
		}
	}
	if err := c.saveCredential(ctx, accountID, credential); err != nil {
		state.finish(err)
		return err
	}
	_ = c.store.Observe(ctx, accountID, credential.UserID, "ready", "", "")
	state.finish(nil)
	return nil
}

func (c *Client) exchangeCode(ctx context.Context, credential Credential, code string, port int) (*tokenResponse, error) {
	redirect := fmt.Sprintf("http://127.0.0.1:%d%s", port, OAuthRedirect)
	token, err := c.requestToken(ctx, map[string][]string{
		"client_id":     {ClientID},
		"code":          {code},
		"code_verifier": {credential.CodeVerifier},
		"grant_type":    {"authorization_code"},
		"redirect_uri":  {redirect},
	}, credential)
	if err != nil {
		if providerErr, ok := err.(*providers.Error); ok && providerErr.Kind == accounts.KindAuth {
			return nil, err
		}
		return nil, err
	}
	return token, nil
}

func (c *Client) buildAuthorizeURL(ticketID, challenge string, port int) string {
	values := url.Values{}
	values.Set("theme", "2")
	values.Set("locale", Language)
	values.Set("uri_scheme", ClientID)
	values.Set("client_id", ClientID)
	values.Set("port", fmt.Sprint(port))
	values.Set("code_challenge", challenge)
	values.Set("code_challenge_method", "SHA-256")
	values.Set("ticket_id", ticketID)
	values.Set("plugin-name", PluginName)
	values.Set("plugin-version", PluginVersion)
	return c.portalHost + "/authorize?" + values.Encode()
}

// pollTicket is the legacy fallback: the portal calls back with a secret, and
// the ticket endpoint returns credentials. It is only used when no code ever
// arrives.
func (c *Client) pollTicket(ctx context.Context, accountID string, state *loginState, secret string) (bool, string, error) {
	values := url.Values{}
	values.Set("ticket_id", state.ticketID)
	values.Set("secret", secret)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.apiBase(Credential{})+loginTicketPath+"?"+values.Encode(), nil)
	if err != nil {
		return false, "", err
	}
	req.Header.Set("plugin-name", PluginName)
	req.Header.Set("plugin-version", PluginVersion)
	client, err := c.httpClient(ctx, accountID)
	if err != nil {
		return false, "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		return false, "", err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 400 {
		return false, "", nil
	}
	var token tokenResponse
	if err := json.Unmarshal(raw, &token); err != nil {
		return false, "", nil
	}
	if token.Credentials.SecurityToken == "" && token.Credential.SecurityToken != "" {
		token.Credentials = credentials{
			AccessKeyID:     token.Credential.Access,
			SecretAccessKey: token.Credential.Secret,
			SecurityToken:   token.Credential.SecurityToken,
			Expiration:      token.Credential.ExpiresAt,
		}
	}
	if token.Credentials.SecurityToken == "" {
		return false, "", nil
	}
	credential := applyToken(Credential{Format: CredentialFormat, ClientID: ClientID}, &token)
	if err := c.saveCredential(ctx, accountID, credential); err != nil {
		return false, "", err
	}
	state.finish(nil)
	c.clearLogin(accountID)
	return true, "login complete", nil
}

// currentUser performs a signed identity lookup. It exercises the same STS
// material chat uses, so Probe can treat success as readiness.
func (c *Client) currentUser(ctx context.Context, credential Credential) (uid, name, domain string, err error) {
	requestCtx, cancel := context.WithTimeout(ctx, metadataTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(requestCtx, http.MethodGet, c.apiBase(credential)+currentUserPath, nil)
	if err != nil {
		return "", "", "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Language", Language)
	signRequest(req, nil, credential.Sign())
	client, err := c.httpClient(ctx, "")
	if err != nil {
		return "", "", "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", "", "", err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 400 {
		return "", "", "", newProviderError(resp.StatusCode, string(raw))
	}
	var out struct {
		UserID   string `json:"user_id"`
		UserName string `json:"user_name"`
		DomainID string `json:"domain_id"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", "", "", fmt.Errorf("parse codearts current user: %w", err)
	}
	return out.UserID, out.UserName, out.DomainID, nil
}

func (c *Client) cleanupLogins() {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.now()
	for accountID, state := range c.logins {
		if now.After(state.expiresAt) {
			state.finish(fmt.Errorf("login expired"))
			if state.listener != nil {
				_ = state.listener.Close()
			}
			delete(c.logins, accountID)
		}
	}
}

func (c *Client) clearLogin(accountID string) {
	c.mu.Lock()
	state := c.logins[accountID]
	delete(c.logins, accountID)
	c.mu.Unlock()
	if state != nil && state.listener != nil {
		_ = state.listener.Close()
	}
}

func pkcePair() (verifier, challenge string, err error) {
	raw := make([]byte, 64)
	if _, err := rand.Read(raw); err != nil {
		return "", "", err
	}
	verifier = base64.RawURLEncoding.EncodeToString(raw)
	sum := sha256.Sum256([]byte(verifier))
	return verifier, base64.RawURLEncoding.EncodeToString(sum[:]), nil
}
