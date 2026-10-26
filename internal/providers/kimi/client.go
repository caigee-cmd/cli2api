package kimi

import (
	"context"
	"fmt"
	"net/http"
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

// ModelSettingReader is optional. Missing it means the console reasoning
// default is skipped and the catalog default is used.
type ModelSettingReader interface {
	GetProviderModelSetting(context.Context, string, string) (accounts.ProviderModelSetting, error)
}

type catalogCache struct {
	models []providers.ModelInfo
	at     time.Time
}

// loginState is one in-flight device authorization for an account.
type loginState struct {
	authHost    string
	deviceID    string
	userCode    string
	deviceCode  string
	verifyURL   string
	interval    time.Duration
	expiresAt   time.Time
	lastPolled  time.Time
	lastMessage string
}

// Client is the in-process Kimi Code adapter. One client serves every account;
// credentials, catalogs and login rounds stay keyed by account id.
type Client struct {
	store Store
	http  *http.Client

	transports proxyutil.TransportCache

	nowFn func() time.Time

	// authHost is the OAuth host new login rounds start against. It defaults to
	// the China-region host and is only overridden by tests or an imported
	// credential that records a different host.
	authHost string
	// baseURL is the managed API base used when a credential does not record
	// one (a fresh login). It defaults to the China-region endpoint.
	baseURL string

	mu       sync.Mutex
	catalogs map[string]catalogCache
	locks    map[string]*sync.Mutex
	logins   map[string]*loginState
}

func NewClient(store Store) *Client {
	return &Client{
		store: store,
		http: &http.Client{
			Timeout: 0,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		authHost: CNOAuthHost,
		baseURL:  CNBaseURL,
		catalogs: map[string]catalogCache{},
		locks:    map[string]*sync.Mutex{},
		logins:   map[string]*loginState{},
		nowFn:    time.Now,
	}
}

func (c *Client) now() time.Time {
	if c.nowFn != nil {
		return c.nowFn()
	}
	return time.Now()
}

// apiBase resolves the managed API base for a credential: the endpoint recorded
// on the credential wins, then the client default (the China region).
func (c *Client) apiBase(credential Credential) string {
	if base := credential.ChatBase(); base != "" {
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

// accountLock serializes token refresh per account so a burst of requests
// cannot burn the single-use refresh token more than once.
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
	credential, err := DecodeCredential(payload)
	if err != nil {
		return Credential{}, err
	}
	if !credential.Ready() {
		return credential, &providers.Error{
			Kind:    accounts.KindAuth,
			Status:  http.StatusUnauthorized,
			Message: "kimi account has no access token; sign in again",
		}
	}
	return credential, nil
}

func (c *Client) saveCredential(ctx context.Context, accountID string, credential Credential) error {
	if credential.DeviceID == "" {
		credential.DeviceID = credential.DeviceIDOr()
	}
	encoded, err := credential.Encode()
	if err != nil {
		return err
	}
	return c.store.SaveCredentialPayload(ctx, accountID, CredentialFormat, encoded)
}

// credential returns a credential whose access token is usable now, refreshing
// and persisting a rotated token when the stored one is close to expiry.
func (c *Client) credential(ctx context.Context, accountID string) (Credential, error) {
	credential, err := c.loadCredential(ctx, accountID)
	if err != nil {
		return Credential{}, err
	}
	// The device identity must be stable for the lifetime of the account: mint
	// it once and persist it before any request can report a random value.
	if credential.DeviceID == "" {
		credential.DeviceID = newDeviceID()
		_ = c.saveCredential(ctx, accountID, credential)
	}
	if !credential.NeedsRefresh(c.now()) {
		return credential, nil
	}
	lock := c.accountLock(accountID)
	lock.Lock()
	defer lock.Unlock()
	// Another request may have refreshed while this one waited for the lock.
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

// persistRefreshed stores a rotated credential. Kimi invalidates the previous
// refresh token on every rotation, so a failed write has to surface: the request
// must fail over instead of continuing with a credential only this process
// knows about, which would make the account unusable on its next request.
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
		Message: "kimi token refresh succeeded but the rotated credential could not be saved: " + err.Error(),
	}
}

// accessToken is the convenience accessor for callers that only need the token.
func (c *Client) accessToken(ctx context.Context, accountID string) (Credential, string, error) {
	credential, err := c.credential(ctx, accountID)
	if err != nil {
		return Credential{}, "", err
	}
	return credential, credential.AccessToken, nil
}

// Probe refreshes readiness. The managed /models endpoint is the definitive
// check — it exercises the bearer token the chat path uses — and /me is read
// best-effort afterwards to fill the profile the console shows.
func (c *Client) Probe(ctx context.Context, accountID string) (providers.AccountHealth, error) {
	if _, err := c.fetchCatalogModels(ctx, accountID); err != nil {
		health := providers.AccountHealth{LastError: err.Error()}
		if providerErr, ok := err.(*providers.Error); ok {
			health.LastError = providerErr.Message
		}
		return health, nil
	}
	profile, err := c.fetchProfile(ctx, accountID)
	if err != nil {
		// /me is display-only: a failure must not flip the account cold when
		// the model list already proved the token works.
		profile = profileInfo{}
	}
	if profile.UserID != "" || profile.Plan != "" {
		c.rememberProfile(ctx, accountID, profile)
	}
	_ = c.store.Observe(ctx, accountID, profile.UserID, "ready", "", "")
	return providers.AccountHealth{Ready: true, Hot: true, UID: profile.UserID}, nil
}

// rememberProfile persists the /me fields onto the credential so the console
// keeps the profile between probes without another request.
func (c *Client) rememberProfile(ctx context.Context, accountID string, profile profileInfo) {
	credential, err := c.loadCredential(ctx, accountID)
	if err != nil {
		return
	}
	changed := false
	if profile.UserID != "" && credential.UserID != profile.UserID {
		credential.UserID = profile.UserID
		changed = true
	}
	if profile.UserName != "" && credential.UserName != profile.UserName {
		credential.UserName = profile.UserName
		changed = true
	}
	if profile.Plan != "" && credential.Plan != profile.Plan {
		credential.Plan = profile.Plan
		changed = true
	}
	if !changed {
		return
	}
	_ = c.saveCredential(ctx, accountID, credential)
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
	}
}

type credentialCodec struct{}

func (credentialCodec) Validate(payload []byte) error { return ValidateCredential(payload) }

type classifier struct{}

func (classifier) Classify(status int, body string) providers.ClassifiedError {
	return Classify(status, body)
}

func (c *Client) storedReasoning(ctx context.Context, model string) string {
	reader, ok := c.store.(ModelSettingReader)
	if !ok {
		return ""
	}
	stored, err := reader.GetProviderModelSetting(ctx, ProviderID, accounts.CanonicalModelID(model))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(stored.ReasoningEffort)
}
