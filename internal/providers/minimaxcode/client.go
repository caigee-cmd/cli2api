package minimaxcode

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

type SecretReader interface {
	GetSecret(context.Context, string) (string, bool, error)
}

type ModelSettingReader interface {
	GetProviderModelSetting(context.Context, string, string) (accounts.ProviderModelSetting, error)
}

type catalogCache struct {
	models []providers.ModelInfo
	at     time.Time
}

type loginState struct {
	site        string
	accountBase string
	agentBase   string
	userCode    string
	deviceCode  string
	verifier    string
	verifyURL   string
	interval    time.Duration
	expiresAt   time.Time
	lastPolled  time.Time
	lastMessage string
	pollByUser  bool
}

type Client struct {
	store Store
	http  *http.Client

	transports proxyutil.TransportCache
	nowFn      func() time.Time

	mu       sync.Mutex
	catalogs map[string]catalogCache
	locks    map[string]*sync.Mutex
	logins   map[string]*loginState
}

func NewClient(store Store) *Client {
	return &Client{
		store: store,
		http: &http.Client{
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
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

func (c *Client) globalProxy(ctx context.Context) (string, error) {
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
			Message: "minimaxcode account has no access token; sign in again",
		}
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

// credential returns a credential whose access token is usable now. Refresh
// tokens are single-use, so refresh is serialized and persisted immediately.
func (c *Client) credential(ctx context.Context, accountID string) (Credential, error) {
	credential, err := c.loadCredential(ctx, accountID)
	if err != nil {
		return Credential{}, err
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
	refreshed, err := c.refreshToken(ctx, current)
	if err != nil {
		return Credential{}, err
	}
	next := applyToken(current, refreshed, c.now())
	// A failed write must surface: another process would still hold a stale
	// single-use token.
	if err := c.saveCredential(ctx, accountID, next); err != nil {
		return Credential{}, err
	}
	return next, nil
}

// forceRefresh is used on a 401 retry. It ignores the expiry heuristic.
func (c *Client) forceRefresh(ctx context.Context, accountID string) (Credential, error) {
	lock := c.accountLock(accountID)
	lock.Lock()
	defer lock.Unlock()
	current, err := c.loadCredential(ctx, accountID)
	if err != nil {
		return Credential{}, err
	}
	refreshed, err := c.refreshToken(ctx, current)
	if err != nil {
		return Credential{}, err
	}
	next := applyToken(current, refreshed, c.now())
	if err := c.saveCredential(ctx, accountID, next); err != nil {
		return Credential{}, err
	}
	return next, nil
}

// Probe exercises the model catalog with the live bearer token.
func (c *Client) Probe(ctx context.Context, accountID string) (providers.AccountHealth, error) {
	if _, err := c.fetchCatalogModels(ctx, accountID); err != nil {
		return providers.AccountHealth{LastError: err.Error()}, nil
	}
	credential, err := c.loadCredential(ctx, accountID)
	if err != nil {
		return providers.AccountHealth{LastError: err.Error()}, nil
	}
	_ = c.store.Observe(ctx, accountID, credential.UserID, "ready", "", "")
	return providers.AccountHealth{Ready: true, Hot: true, UID: credential.UserID}, nil
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

func (c *Client) Adapter() providers.Adapter {
	return providers.Adapter{
		ID:            ProviderID,
		Credential:    credentialCodec{},
		Login:         c,
		Chat:          c,
		Models:        c,
		Classifier:    classifier{},
		ImportExport:  importer{},
		Prober:        c,
		Checkin:       c,
		CheckinStatus: c,
	}
}

type credentialCodec struct{}

func (credentialCodec) Validate(payload []byte) error { return ValidateCredential(payload) }

type classifier struct{}

func (classifier) Classify(status int, body string) providers.ClassifiedError {
	return Classify(status, body)
}

var _ = fmt.Sprintf
