package codearts

import (
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
)

// Store is the persistence surface this adapter needs. It never imports
// internal/store.
type Store interface {
	Get(ctx context.Context, id string) (accounts.Account, error)
	LoadCredentialPayload(ctx context.Context, accountID string) (string, []byte, error)
	SaveCredentialPayload(ctx context.Context, accountID, format string, payload []byte) error
	Observe(ctx context.Context, id, remoteUID, status, lastError, lastKind string) error
}

// SecretReader is optional. Missing it means no global proxy.
type SecretReader interface {
	GetSecret(context.Context, string) (string, bool, error)
}

type catalogCache struct {
	models  []providers.ModelInfo
	benefit map[string]bool
	at      time.Time
}

// Client is the in-process CodeArts adapter. One client serves every account;
// credentials, catalogs and login rounds stay keyed by account id.
type Client struct {
	store Store
	http  *http.Client

	transports proxyutil.TransportCache
	nowFn      func() time.Time

	baseURL     string
	stsHost     string
	portalHost  string
	benefitBase string

	mu       sync.Mutex
	catalogs map[string]catalogCache
	locks    map[string]*sync.Mutex
	logins   map[string]*loginState
}

func NewClient(store Store) *Client {
	transport := &http.Transport{
		MaxIdleConns:        20,
		MaxIdleConnsPerHost: 4,
		IdleConnTimeout:     90 * time.Second,
	}
	return &Client{
		store: store,
		http: &http.Client{
			Transport: transport,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		baseURL:     CNBaseURL,
		stsHost:     CNSTSHost,
		portalHost:  CNPortalHost,
		benefitBase: BenefitHost,
		catalogs:    map[string]catalogCache{},
		locks:       map[string]*sync.Mutex{},
		logins:      map[string]*loginState{},
		nowFn:       time.Now,
	}
}

func (c *Client) now() time.Time {
	if c.nowFn != nil {
		return c.nowFn()
	}
	return time.Now()
}

func (c *Client) apiBase(credential Credential) string {
	if base := credential.AuthBase(); base != "" {
		return base
	}
	if c.baseURL != "" {
		return c.baseURL
	}
	return CNBaseURL
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
	if strings.TrimSpace(accountID) == "" {
		return c.globalProxy(ctx)
	}
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

// accountLock serializes refresh per account so a burst cannot burn the
// single-use refresh token more than once.
func (c *Client) accountLock(accountID string) *sync.Mutex {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.locks == nil {
		c.locks = map[string]*sync.Mutex{}
	}
	lock, ok := c.locks[accountID]
	if !ok {
		lock = &sync.Mutex{}
		c.locks[accountID] = lock
	}
	return lock
}

func (c *Client) loadCredential(ctx context.Context, accountID string) (Credential, error) {
	_, payload, err := c.store.LoadCredentialPayload(ctx, accountID)
	if err != nil {
		return Credential{}, err
	}
	return DecodeCredential(payload)
}

func (c *Client) saveCredential(ctx context.Context, accountID string, credential Credential) error {
	encoded, err := credential.Encode()
	if err != nil {
		return err
	}
	return c.store.SaveCredentialPayload(ctx, accountID, CredentialFormat, encoded)
}

// credential returns a credential that can sign a request now, refreshing and
// persisting a rotated STS trio when the stored one is near expiry.
func (c *Client) credential(ctx context.Context, accountID string) (Credential, error) {
	credential, err := c.loadCredential(ctx, accountID)
	if err != nil {
		return Credential{}, err
	}
	if !credential.Ready() && !credential.CanRefresh() {
		return credential, &providers.Error{
			Kind:    accounts.KindAuth,
			Status:  http.StatusUnauthorized,
			Message: "codearts account has no STS credential; sign in again",
		}
	}
	if !credential.NeedsRefresh(c.now()) {
		return credential, nil
	}
	lock := c.accountLock(accountID)
	lock.Lock()
	defer lock.Unlock()
	current, err := c.loadCredential(ctx, accountID)
	if err != nil {
		return Credential{}, err
	}
	if !current.NeedsRefresh(c.now()) {
		return current, nil
	}
	refreshed, err := c.refreshCredential(ctx, current)
	if err != nil {
		return Credential{}, err
	}
	if err := c.persistRefreshed(ctx, accountID, refreshed); err != nil {
		return Credential{}, err
	}
	return refreshed, nil
}

// persistRefreshed stores a rotated credential. The refresh token is
// single-use, so a failed write must fail the request instead of continuing
// with a credential only this process knows about.
func (c *Client) persistRefreshed(ctx context.Context, accountID string, credential Credential) error {
	var err error
	for attempt := 0; attempt < 2; attempt++ {
		if err = c.saveCredential(ctx, accountID, credential); err == nil {
			return nil
		}
	}
	return &providers.Error{
		Kind:    accounts.KindUnavailable,
		Status:  http.StatusBadGateway,
		Message: "codearts credential refresh succeeded but could not be saved: " + err.Error(),
	}
}

func (c *Client) forceRefresh(ctx context.Context, accountID string) (Credential, error) {
	lock := c.accountLock(accountID)
	lock.Lock()
	defer lock.Unlock()
	current, err := c.loadCredential(ctx, accountID)
	if err != nil {
		return Credential{}, err
	}
	refreshed, err := c.refreshCredential(ctx, current)
	if err != nil {
		return Credential{}, err
	}
	if err := c.persistRefreshed(ctx, accountID, refreshed); err != nil {
		return Credential{}, err
	}
	return refreshed, nil
}

// Probe verifies the current credential against the signed identity endpoint.
func (c *Client) Probe(ctx context.Context, accountID string) (providers.AccountHealth, error) {
	credential, err := c.credential(ctx, accountID)
	if err != nil {
		health := providers.AccountHealth{LastError: err.Error()}
		if providerErr, ok := err.(*providers.Error); ok {
			health.LastError = providerErr.Message
		}
		return health, nil
	}
	uid, name, domain, err := c.currentUser(ctx, credential)
	if err != nil {
		health := providers.AccountHealth{LastError: err.Error()}
		if providerErr, ok := err.(*providers.Error); ok {
			health.LastError = providerErr.Message
		}
		return health, nil
	}
	changed := false
	if uid != "" && credential.UserID != uid {
		credential.UserID = uid
		changed = true
	}
	if name != "" && credential.UserName != name {
		credential.UserName = name
		changed = true
	}
	if domain != "" && credential.DomainID != domain {
		credential.DomainID = domain
		changed = true
	}
	if changed {
		_ = c.saveCredential(ctx, accountID, credential)
	}
	_ = c.store.Observe(ctx, accountID, uid, "ready", "", "")
	return providers.AccountHealth{Ready: true, Hot: true, UID: uid}, nil
}

// Adapter wires the capability surface.
func (c *Client) Adapter() providers.Adapter {
	return providers.Adapter{
		ID:           ProviderID,
		Credential:   credentialCodec{},
		Login:        c,
		Chat:         c,
		Models:       c,
		Classifier:   classifier{},
		ImportExport: importer{},
		Prober:       c,
		Checkin:      c,
	}
}

type credentialCodec struct{}

func (credentialCodec) Validate(payload []byte) error { return ValidateCredential(payload) }

// tokenResponse is the STS oauth2/tokens response.
type tokenResponse struct {
	UserID       string      `json:"user_id"`
	UserName     string      `json:"user_name"`
	DomainID     string      `json:"domain_id"`
	RefreshToken string      `json:"refresh_token"`
	Credentials  credentials `json:"credentials"`
	Credential   legacyCred  `json:"credential"`
	Error        string      `json:"error"`
	ErrorCode    string      `json:"error_code"`
	ErrorMessage string      `json:"error_msg"`
}

type credentials struct {
	AccessKeyID     string `json:"access_key_id"`
	SecretAccessKey string `json:"secret_access_key"`
	SecurityToken   string `json:"security_token"`
	Expiration      string `json:"expiration"`
}

type legacyCred struct {
	Access        string `json:"access"`
	Secret        string `json:"secret"`
	SecurityToken string `json:"securitytoken"`
	ExpiresAt     string `json:"expires_at"`
}

// refreshExpiredError marks a terminal refresh rejection; retrying cannot help.
type refreshExpiredError struct {
	status  int
	message string
}

func (e *refreshExpiredError) Error() string { return e.message }

// requestToken posts a form to the STS token endpoint with a DPoP proof.
func (c *Client) requestToken(ctx context.Context, form map[string][]string, cred Credential) (*tokenResponse, error) {
	endpoint := c.stsHost + oauthTokensPath
	kp, err := dpopKeyPairFor(cred.DPoPPrivateKey)
	if err != nil {
		return nil, err
	}
	proof, err := signDPoPProof(kp, endpoint)
	if err != nil {
		return nil, fmt.Errorf("dpop proof: %w", err)
	}
	values := make(map[string]string, len(form))
	for k, v := range form {
		if len(v) > 0 {
			values[k] = v[0]
		}
	}
	// Drop members that were not part of the grant instead of sending empty
	// values; STS rejects an empty code or redirect_uri on refresh.
	cleaned := url.Values{}
	for _, key := range []string{"client_id", "code", "code_verifier", "grant_type", "redirect_uri", "refresh_token"} {
		if value := strings.TrimSpace(values[key]); value != "" {
			cleaned.Set(key, value)
		}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(cleaned.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("DPoP", proof)
	client, err := c.httpClient(ctx, "")
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode >= 400 {
		var tokenErr tokenResponse
		_ = json.Unmarshal(raw, &tokenErr)
		grantType := ""
		if values := form["grant_type"]; len(values) > 0 {
			grantType = values[0]
		}
		if grantType == "refresh_token" {
			code := tokenErr.ErrorCode
			message := strings.ToLower(tokenErr.ErrorMessage)
			if tokenErr.Error == "invalid_grant" || strings.Contains(code, "ExpiredRefreshToken") ||
				strings.Contains(code, "InvalidDPoPHeader") || strings.Contains(code, "invalid client id") ||
				strings.Contains(message, "has been used") {
				return nil, &refreshExpiredError{
					status: resp.StatusCode,
					message: fmt.Sprintf("codearts refresh token rejected: http=%d code=%s msg=%s",
						resp.StatusCode, code, truncate(tokenErr.ErrorMessage, 160)),
				}
			}
		}
		return nil, newProviderError(resp.StatusCode, string(raw))
	}
	var out tokenResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("parse codearts token response: %w", err)
	}
	if out.Credentials.SecurityToken == "" && out.Credential.SecurityToken != "" {
		out.Credentials = credentials{
			AccessKeyID:     out.Credential.Access,
			SecretAccessKey: out.Credential.Secret,
			SecurityToken:   out.Credential.SecurityToken,
			Expiration:      out.Credential.ExpiresAt,
		}
	}
	if out.Credentials.SecurityToken == "" {
		return nil, fmt.Errorf("codearts token response missing credentials")
	}
	return &out, nil
}

// refreshCredential rotates the STS trio with the stored refresh token, PKCE
// verifier and DPoP key.
func (c *Client) refreshCredential(ctx context.Context, credential Credential) (Credential, error) {
	if !credential.CanRefresh() {
		return Credential{}, &providers.Error{
			Kind:    accounts.KindAuth,
			Status:  http.StatusUnauthorized,
			Message: "codearts credential is expired and cannot be refreshed; sign in again",
		}
	}
	clientID := credential.ClientID
	if clientID == "" {
		clientID = ClientID
	}
	token, err := c.requestToken(ctx, map[string][]string{
		"client_id":     {clientID},
		"code_verifier": {credential.CodeVerifier},
		"grant_type":    {"refresh_token"},
		"refresh_token": {credential.RefreshToken},
	}, credential)
	if err != nil {
		if expired, ok := err.(*refreshExpiredError); ok {
			return Credential{}, &providers.Error{
				Kind:    accounts.KindAuth,
				Status:  http.StatusUnauthorized,
				Message: expired.message,
			}
		}
		return Credential{}, err
	}
	return applyToken(credential, token), nil
}

func applyToken(credential Credential, token *tokenResponse) Credential {
	credential.Format = CredentialFormat
	credential.AccessKeyID = token.Credentials.AccessKeyID
	credential.SecretAccessKey = token.Credentials.SecretAccessKey
	credential.SecurityToken = token.Credentials.SecurityToken
	if token.Credentials.Expiration != "" {
		credential.ExpiresAt = token.Credentials.Expiration
	}
	if token.UserID != "" {
		credential.UserID = token.UserID
	}
	if token.UserName != "" {
		credential.UserName = token.UserName
	}
	if token.DomainID != "" {
		credential.DomainID = token.DomainID
	}
	if token.RefreshToken != "" {
		credential.RefreshToken = token.RefreshToken
	}
	return credential
}

func truncate(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) > n {
		return s[:n]
	}
	return s
}
