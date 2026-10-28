package meta

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

// Store is the persistence surface this adapter needs.
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

// ModelSettingReader is optional for console reasoning defaults.
type ModelSettingReader interface {
	GetProviderModelSetting(context.Context, string, string) (accounts.ProviderModelSetting, error)
}

// Client is the in-process Meta Muse adapter.
type Client struct {
	store Store
	http  *http.Client

	transports proxyutil.TransportCache

	nowFn   func() time.Time
	authHost string // test override for AuthHost
	mintURL  string // test override for MintURL

	mu     sync.Mutex
	logins map[string]*loginState
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
		logins: map[string]*loginState{},
		nowFn:  time.Now,
	}
}

func (c *Client) now() time.Time {
	if c.nowFn != nil {
		return c.nowFn()
	}
	return time.Now()
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
	proxyURL, err := c.effectiveProxy(ctx, accountID)
	if err != nil {
		return nil, err
	}
	transport, err := c.transports.Get(proxyURL)
	if err != nil {
		return nil, err
	}
	client := *c.http
	if transport != nil {
		client.Transport = transport
	}
	client.Timeout = 0
	return &client, nil
}

func (c *Client) loadCredential(ctx context.Context, accountID string) (Credential, error) {
	_, payload, err := c.store.LoadCredentialPayload(ctx, accountID)
	if err != nil {
		return Credential{}, err
	}
	return DecodeCredential(payload)
}

func (c *Client) saveCredential(ctx context.Context, accountID string, credential Credential) error {
	payload, err := credential.Encode()
	if err != nil {
		return err
	}
	format := credential.Format
	if format == "" {
		format = CredentialFormatOAuth
	}
	return c.store.SaveCredentialPayload(ctx, accountID, format, payload)
}

func (c *Client) credential(ctx context.Context, accountID string) (Credential, error) {
	credential, err := c.loadCredential(ctx, accountID)
	if err != nil {
		return Credential{}, err
	}
	if !credential.Ready() {
		return Credential{}, &providers.Error{
			Kind:    accounts.KindAuth,
			Status:  http.StatusUnauthorized,
			Message: "meta credential incomplete; sign in again",
		}
	}
	return credential, nil
}

// Probe checks that a minted/pasted API key is present.
func (c *Client) Probe(ctx context.Context, accountID string) (providers.AccountHealth, error) {
	credential, err := c.credential(ctx, accountID)
	if err != nil {
		health := providers.AccountHealth{LastError: err.Error()}
		if providerErr, ok := err.(*providers.Error); ok {
			health.LastError = providerErr.Message
		}
		return health, nil
	}
	uid := firstNonEmpty(credential.Email, credential.Name)
	_ = c.store.Observe(ctx, accountID, uid, "ready", "", "")
	return providers.AccountHealth{Ready: true, Hot: true, UID: uid}, nil
}


// Quota returns nil — Meta Muse does not expose a usage/quota endpoint
// in CPA 8.0.40; the console shows "unknown" rather than a fake zero.
func (c *Client) Quota(ctx context.Context, accountID string) (*providers.QuotaInfo, error) {
	_ = ctx
	_ = accountID
	return nil, nil
}

// Models returns the static CPA GetMetaModels table.
func (c *Client) Models(ctx context.Context, accountID string) ([]providers.ModelInfo, error) {
	_ = accountID
	return catalogModelInfos(), nil
}

// Adapter wires the capability surface.
func (c *Client) Adapter() providers.Adapter {
	return providers.Adapter{
		ID:              ProviderID,
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

func setChatHeaders(h http.Header, bearer string) {
	h.Set("Authorization", "Bearer "+bearer)
	h.Set("Content-Type", "application/json")
	h.Set("Accept", "text/event-stream")
	h.Set("User-Agent", ChatUserAgent)
	h.Set("X-Client-Id", ClientIDHeader)
}

func classifiedError(status int, body []byte) error {
	classified := Classify(status, string(body))
	return &providers.Error{
		Kind:    classified.Kind,
		Status:  classified.Status,
		Message: firstNonEmpty(classified.Message, fmt.Sprintf("meta upstream error (HTTP %d)", status)),
	}
}
