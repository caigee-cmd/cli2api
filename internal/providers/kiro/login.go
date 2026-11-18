package kiro

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/caigee-cmd/cli2api/internal/auth"
	"github.com/caigee-cmd/cli2api/internal/providers"
	"github.com/google/uuid"
)

// loginMethod names which sign-in a login round runs. The console stores the
// choice before it calls StartLogin; an account with nothing stored uses
// Builder ID, which is the flow a pasted credential already refreshed as.
const (
	loginBuilderID  = "builder_id"
	loginIDC        = "idc"
	loginSSO        = "sso"
	loginMicrosoft  = "microsoft"
	loginMethodName = "kiro_login_method:"
)

// loginState is one sign-in round for one account. The fields that stay empty
// belong to a flow this round is not running.
type loginState struct {
	method    string
	region    string
	expiresAt time.Time

	// Builder ID device grant.
	client   oidcClient
	device   deviceAuthorization
	interval time.Duration
	polled   time.Time

	// Identity Center authorization code.
	verifier string
	state    string
	redirect string
	port     int
	listener net.Listener

	// SSO token import. The bearer is held only until the import finishes.
	bearer string

	// Microsoft enterprise sign-in. provider is filled in after the first
	// callback, and nextURL is the address the console should open next.
	portalState string
	provider    *microsoftProvider
	loginHint   string
	nextURL     string

	mu        sync.Mutex
	done      bool
	err       error
	completed chan struct{}
}

func (s *loginState) finish(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.done {
		return
	}
	s.done = true
	s.err = err
	close(s.completed)
}

// StartLogin opens the sign-in the console selected. Builder ID and SSO need
// no browser redirect back; Identity Center and Microsoft listen on localhost.
func (c *Client) StartLogin(ctx context.Context, accountID string) (providers.LoginSession, error) {
	account, err := c.store.Get(ctx, accountID)
	if err != nil {
		return providers.LoginSession{}, err
	}
	method := c.loginMethod(ctx, accountID)
	region := normalizeRegion(account.ProviderRegion)
	client, err := c.httpClient(ctx, accountID)
	if err != nil {
		return providers.LoginSession{}, err
	}
	state := &loginState{
		method: method, region: region, expiresAt: time.Now().Add(loginTTL),
		completed: make(chan struct{}),
	}
	var session providers.LoginSession
	switch method {
	case loginBuilderID:
		session, err = c.startBuilderID(ctx, client, state)
	case loginIDC:
		session, err = c.startIDC(ctx, client, accountID, state)
	case loginSSO:
		session, err = c.startSSO(state)
	case loginMicrosoft:
		session, err = c.startMicrosoft(accountID, state)
	default:
		err = fmt.Errorf("unknown kiro login method %q", method)
	}
	if err != nil {
		if state.listener != nil {
			_ = state.listener.Close()
		}
		return providers.LoginSession{}, err
	}
	c.keepLogin(accountID, state)
	return session, nil
}

func (c *Client) startBuilderID(ctx context.Context, client *http.Client, state *loginState) (providers.LoginSession, error) {
	registered, err := registerOIDCClient(ctx, client, state.region, builderIDStartURL, "", []string{
		"urn:ietf:params:oauth:grant-type:device_code", "refresh_token",
	})
	if err != nil {
		return providers.LoginSession{}, err
	}
	device, err := startDeviceAuthorization(ctx, client, state.region, builderIDStartURL, registered)
	if err != nil {
		return providers.LoginSession{}, err
	}
	state.client = registered
	state.device = device
	state.interval = device.Interval
	state.expiresAt = time.Now().Add(device.ExpiresIn)
	return providers.LoginSession{AuthURL: device.VerificationURI, State: device.UserCode}, nil
}

func (c *Client) startIDC(ctx context.Context, client *http.Client, accountID string, state *loginState) (providers.LoginSession, error) {
	startURL := c.idcStartURL(ctx, accountID)
	if startURL == "" {
		return providers.LoginSession{}, fmt.Errorf("identity center login requires the start url")
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return providers.LoginSession{}, fmt.Errorf("kiro login listener: %w", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	redirect := fmt.Sprintf("http://127.0.0.1:%d%s", port, idcCallbackPath)
	registered, err := registerOIDCClient(ctx, client, state.region, startURL, redirect, []string{"authorization_code", "refresh_token"})
	if err != nil {
		_ = listener.Close()
		return providers.LoginSession{}, err
	}
	verifier, challenge, err := pkcePair()
	if err != nil {
		_ = listener.Close()
		return providers.LoginSession{}, err
	}
	oauthState, err := randomState()
	if err != nil {
		_ = listener.Close()
		return providers.LoginSession{}, err
	}
	state.client = registered
	state.verifier = verifier
	state.state = oauthState
	state.redirect = redirect
	state.port = port
	state.listener = listener
	auth.ServeLoopback(listener, idcCallbackPath, "Kiro", func(callbackCtx context.Context, rawURL string) error {
		return c.completeIDC(callbackCtx, accountID, state, "http://127.0.0.1"+rawURL)
	})
	return providers.LoginSession{AuthURL: authorizeURL(state.region, registered.ID, redirect, oauthState, challenge), State: oauthState}, nil
}

func (c *Client) startSSO(state *loginState) (providers.LoginSession, error) {
	return providers.LoginSession{AuthURL: ssoPortalBase, State: "sso"}, nil
}

func (c *Client) startMicrosoft(accountID string, state *loginState) (providers.LoginSession, error) {
	oauthState, err := randomState()
	if err != nil {
		return providers.LoginSession{}, err
	}
	_, challenge, err := pkcePair()
	if err != nil {
		return providers.LoginSession{}, err
	}
	state.portalState = oauthState
	listener, err := net.Listen("tcp", "localhost:3128")
	if err != nil {
		// The port is the one Kiro redirects to. When it is taken, the operator
		// pastes the callback instead; the login still completes.
		return providers.LoginSession{AuthURL: microsoftSignIn(oauthState, challenge), State: oauthState}, nil
	}
	state.listener = listener
	go serveMicrosoftLoopback(listener, func(callbackCtx context.Context, rawURL string) error {
		return c.completeMicrosoft(callbackCtx, accountID, state, rawURL)
	})
	return providers.LoginSession{AuthURL: microsoftSignIn(oauthState, challenge), State: oauthState}, nil
}

// serveMicrosoftLoopback accepts both redirects Kiro sends to localhost:3128.
// The first lands on the sign-in callback, the second on the OAuth callback.
func serveMicrosoftLoopback(ln net.Listener, callback func(context.Context, string) error) {
	mux := http.NewServeMux()
	handle := func(w http.ResponseWriter, r *http.Request) {
		if err := callback(r.Context(), "http://localhost:3128"+r.URL.RequestURI()); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = fmt.Fprint(w, "<!doctype html><meta charset=utf-8><title>Kiro login</title><p>Login complete. You can close this tab.</p>")
	}
	mux.HandleFunc(microsoftPortalPath, handle)
	mux.HandleFunc(microsoftOAuthPath, handle)
	server := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = server.Serve(ln) }()
}

// idcStartURL reads the Identity Center portal address the operator stored for
// this account. Without it the authorization request has no issuer.
func (c *Client) idcStartURL(ctx context.Context, accountID string) string {
	reader, ok := c.store.(SecretReader)
	if !ok {
		return ""
	}
	value, found, err := reader.GetSecret(ctx, "kiro_idc_start_url:"+accountID)
	if err != nil || !found {
		return ""
	}
	return strings.TrimSpace(value)
}

// PollLogin advances the round by one step. Device grants poll the token
// endpoint; the browser flows wait for their callback.
func (c *Client) PollLogin(ctx context.Context, accountID string) (bool, string, error) {
	state := c.login(accountID)
	if state == nil {
		return false, "", fmt.Errorf("login not started for account %s", accountID)
	}
	if time.Now().After(state.expiresAt) {
		c.clearLogin(accountID)
		return false, "", fmt.Errorf("kiro login timed out; start again")
	}
	select {
	case <-state.completed:
		return c.finishLogin(ctx, accountID, state)
	default:
	}
	switch state.method {
	case loginBuilderID:
		return c.pollBuilderID(ctx, accountID, state)
	case loginSSO:
		return c.pollSSO(ctx, accountID, state)
	case loginMicrosoft:
		state.mu.Lock()
		next := state.nextURL
		state.mu.Unlock()
		if next != "" {
			return false, "open the microsoft sign-in: " + next, nil
		}
		return false, "waiting for the kiro sign-in callback", nil
	default:
		return false, "waiting for browser authorization", nil
	}
}

// CompleteLogin accepts a callback URL the browser could not deliver, or the
// SSO bearer and the Identity Center start URL, which have no redirect.
func (c *Client) CompleteLogin(ctx context.Context, accountID, callbackURL string) error {
	state := c.login(accountID)
	if state == nil {
		return fmt.Errorf("login not started for account %s", accountID)
	}
	switch state.method {
	case loginSSO:
		state.mu.Lock()
		state.bearer = strings.TrimSpace(callbackURL)
		state.mu.Unlock()
		_, _, err := c.pollSSO(ctx, accountID, state)
		return err
	case loginIDC:
		return c.completeIDC(ctx, accountID, state, callbackURL)
	case loginMicrosoft:
		return c.completeMicrosoft(ctx, accountID, state, callbackURL)
	default:
		return fmt.Errorf("this kiro login does not take a callback")
	}
}

func (c *Client) pollBuilderID(ctx context.Context, accountID string, state *loginState) (bool, string, error) {
	state.mu.Lock()
	wait := state.interval - time.Since(state.polled)
	state.mu.Unlock()
	if wait > 0 {
		return false, fmt.Sprintf("waiting for authorization; the code is %s", state.device.UserCode), nil
	}
	client, err := c.httpClient(ctx, accountID)
	if err != nil {
		return false, "", err
	}
	token, pending, err := pollDeviceToken(ctx, client, state.region, state.client, state.device.DeviceCode)
	state.mu.Lock()
	state.polled = time.Now()
	state.mu.Unlock()
	if err != nil {
		c.clearLogin(accountID)
		return false, "", err
	}
	if pending {
		return false, fmt.Sprintf("waiting for authorization; the code is %s", state.device.UserCode), nil
	}
	cred := Credential{
		AuthMethod: "builder_id", Region: state.region, Provider: "BuilderId",
		AccessToken: token.AccessToken, RefreshToken: token.RefreshToken, ExpiresAt: token.ExpiresAt.Unix(),
		ClientID: state.client.ID, ClientSecret: state.client.Secret, StartURL: builderIDStartURL,
		ProfileArn: token.ProfileArn,
	}
	if err := c.saveLogin(ctx, accountID, cred); err != nil {
		return false, "", err
	}
	state.finish(nil)
	c.clearLogin(accountID)
	return true, "login complete", nil
}

func (c *Client) pollSSO(ctx context.Context, accountID string, state *loginState) (bool, string, error) {
	state.mu.Lock()
	bearer := state.bearer
	state.mu.Unlock()
	if bearer == "" {
		return false, "paste the sso bearer token", nil
	}
	client, err := c.httpClient(ctx, accountID)
	if err != nil {
		return false, "", err
	}
	cred, err := importSSOToken(ctx, client, state.region, bearer)
	if err != nil {
		return false, "", err
	}
	if err := c.saveLogin(ctx, accountID, cred); err != nil {
		return false, "", err
	}
	state.finish(nil)
	c.clearLogin(accountID)
	return true, "login complete", nil
}

func (c *Client) completeIDC(ctx context.Context, accountID string, state *loginState, rawURL string) error {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || parsed.Path != idcCallbackPath {
		return fmt.Errorf("callback url must be the identity center redirect")
	}
	if parsed.Query().Get("state") != state.state {
		return fmt.Errorf("callback state does not match this login")
	}
	if failure := parsed.Query().Get("error"); failure != "" {
		return fmt.Errorf("identity center authorization failed: %s", failure)
	}
	code := parsed.Query().Get("code")
	if code == "" {
		return fmt.Errorf("callback url has no authorization code")
	}
	client, err := c.httpClient(ctx, accountID)
	if err != nil {
		return err
	}
	token, err := exchangeAuthorizationCode(ctx, client, state.region, state.client, code, state.verifier, state.redirect)
	if err != nil {
		return err
	}
	cred := Credential{
		AuthMethod: "idc", Region: state.region, AccessToken: token.AccessToken, RefreshToken: token.RefreshToken,
		ExpiresAt: token.ExpiresAt.Unix(), ClientID: state.client.ID, ClientSecret: state.client.Secret,
		ProfileArn: token.ProfileArn, StartURL: c.idcStartURL(ctx, accountID),
	}
	if err := c.saveLogin(ctx, accountID, cred); err != nil {
		return err
	}
	state.finish(nil)
	return nil
}

func (c *Client) completeMicrosoft(ctx context.Context, accountID string, state *loginState, rawURL string) error {
	if _, err := url.Parse(strings.TrimSpace(rawURL)); err != nil {
		return fmt.Errorf("callback url is invalid")
	}
	if strings.Contains(rawURL, microsoftPortalPath) {
		return c.advanceMicrosoft(ctx, accountID, state, rawURL)
	}
	return c.finishMicrosoft(ctx, accountID, state, rawURL)
}

// advanceMicrosoft reads the portal's descriptor and publishes the Microsoft
// authorization URL for the console to open.
func (c *Client) advanceMicrosoft(ctx context.Context, accountID string, state *loginState, rawURL string) error {
	portal, err := readMicrosoftPortal(rawURL, state.portalState)
	if err != nil {
		return err
	}
	client, err := c.httpClient(ctx, accountID)
	if err != nil {
		return err
	}
	authorize, tokenURL, err := discoverMicrosoftOIDC(ctx, client, portal.IssuerURL)
	if err != nil {
		return err
	}
	verifier, _, err := pkcePair()
	if err != nil {
		return err
	}
	oauthState, err := randomState()
	if err != nil {
		return err
	}
	provider := &microsoftProvider{
		State: oauthState, Verifier: verifier, IssuerURL: strings.TrimRight(portal.IssuerURL, "/"),
		AuthorizeURL: authorize, TokenURL: tokenURL, ClientID: portal.ClientID, Scopes: portal.Scopes,
	}
	state.mu.Lock()
	state.provider = provider
	state.loginHint = portal.LoginHint
	state.nextURL = microsoftAuthorizeURL(*provider, portal.LoginHint)
	state.mu.Unlock()
	return nil
}

func (c *Client) finishMicrosoft(ctx context.Context, accountID string, state *loginState, rawURL string) error {
	state.mu.Lock()
	provider := state.provider
	state.mu.Unlock()
	if provider == nil {
		return fmt.Errorf("paste the kiro sign-in callback before the microsoft callback")
	}
	client, err := c.httpClient(ctx, accountID)
	if err != nil {
		return err
	}
	cred, err := exchangeMicrosoftCode(ctx, client, *provider, rawURL)
	if err != nil {
		return err
	}
	cred.Region = state.region
	if err := c.saveLogin(ctx, accountID, cred); err != nil {
		return err
	}
	state.finish(nil)
	return nil
}

func (c *Client) finishLogin(ctx context.Context, accountID string, state *loginState) (bool, string, error) {
	state.mu.Lock()
	done, err := state.done, state.err
	state.mu.Unlock()
	c.clearLogin(accountID)
	if err != nil {
		return false, "", err
	}
	if done {
		return true, "login complete", nil
	}
	return false, "", fmt.Errorf("kiro login ended without a credential")
}

func (c *Client) loginMethod(ctx context.Context, accountID string) string {
	reader, ok := c.store.(SecretReader)
	if !ok {
		return loginBuilderID
	}
	value, found, err := reader.GetSecret(ctx, loginMethodName+accountID)
	if err != nil || !found {
		return loginBuilderID
	}
	switch strings.ToLower(strings.TrimSpace(value)) {
	case loginIDC, loginSSO, loginMicrosoft:
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return loginBuilderID
	}
}

func (c *Client) keepLogin(accountID string, state *loginState) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.logins == nil {
		c.logins = map[string]*loginState{}
	}
	if previous := c.logins[accountID]; previous != nil {
		previous.finish(fmt.Errorf("superseded by a new login"))
		if previous.listener != nil {
			_ = previous.listener.Close()
		}
	}
	c.logins[accountID] = state
}

func (c *Client) login(accountID string) *loginState {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.logins[accountID]
}

func (c *Client) clearLogin(accountID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if state := c.logins[accountID]; state != nil && state.listener != nil {
		_ = state.listener.Close()
	}
	delete(c.logins, accountID)
}

// saveLogin writes the credential and marks the account signed in.
func (c *Client) saveLogin(ctx context.Context, accountID string, cred Credential) error {
	if cred.MachineID == "" {
		cred.MachineID = uuid.NewString()
	}
	encoded, err := cred.Encode()
	if err != nil {
		return err
	}
	if err := c.store.SaveCredentialPayload(ctx, accountID, CredentialFormat, encoded); err != nil {
		return err
	}
	authType := "oauth"
	if cred.isAPIKey() {
		authType = "pat"
	}
	_ = c.store.SetCredentialAuthType(ctx, accountID, authType)
	_ = c.store.Observe(ctx, accountID, firstNonEmpty(cred.UserID, cred.Email), "ready", "", "")
	return nil
}
