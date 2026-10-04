package zhipu

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

// Client is the in-process Zhipu adapter. One client serves every account;
// credentials and catalogs stay keyed by account id.
type Client struct {
	store Store
	http  *http.Client

	transports proxyutil.TransportCache

	mu       sync.Mutex
	catalogs map[string]catalogCache
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
		catalogs: map[string]catalogCache{},
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

func (c *Client) credential(ctx context.Context, accountID string) (Credential, error) {
	_, payload, err := c.store.LoadCredentialPayload(ctx, accountID)
	if err != nil {
		return Credential{}, err
	}
	cred, err := DecodeCredential(payload)
	if err != nil {
		return Credential{}, err
	}
	if !cred.Ready() {
		return cred, fmt.Errorf("zhipu credential incomplete; paste an API key")
	}
	return cred, nil
}

// Probe checks that the key can read its own model list. A quota failure is
// not a readiness failure; quota is probed separately.
func (c *Client) Probe(ctx context.Context, accountID string) (providers.AccountHealth, error) {
	credential, err := c.credential(ctx, accountID)
	if err != nil {
		return providers.AccountHealth{LastError: err.Error()}, nil
	}
	if _, err := c.fetchCatalog(ctx, accountID, credential); err != nil {
		if providerErr, ok := err.(*providers.Error); ok {
			return providers.AccountHealth{LastError: providerErr.Message}, nil
		}
		return providers.AccountHealth{LastError: err.Error()}, nil
	}
	_ = c.store.Observe(ctx, accountID, "", "ready", "", "")
	return providers.AccountHealth{Ready: true, Hot: true}, nil
}

// Adapter wires the capability surface. Login is absent: the only auth is a
// pasted API key, exposed through the PAT tab.
func (c *Client) Adapter() providers.Adapter {
	return providers.Adapter{
		ID:           "zhipu",
		Credential:   credentialCodec{},
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
	stored, err := reader.GetProviderModelSetting(ctx, "zhipu", accounts.CanonicalModelID(model))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(stored.ReasoningEffort)
}

func (c *Client) resolveLevel(ctx context.Context, accountID string, reqModel, requested string) (string, providers.ModelCapabilities) {
	caps := reasoningCaps(reqModel)
	if info, ok := c.lookupModel(accountID, reqModel); ok && len(info.Capabilities.ReasoningOptions) > 0 {
		caps = info.Capabilities
	}
	level := providers.ResolveReasoningLevel(requested, caps)
	if level == "" {
		level = c.storedReasoning(ctx, reqModel)
		level = providers.ResolveReasoningLevel(level, caps)
	}
	return level, caps
}
