package command

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/caigee-cmd/cli2api/internal/accounts"
	"github.com/caigee-cmd/cli2api/internal/providers"
	proxyutil "github.com/caigee-cmd/cli2api/internal/proxy"
)

// Store is the persistence surface the adapter needs. It mirrors the Devin
// adapter intentionally: an in-process provider talks to the account store
// through this narrow interface and never imports internal/store.
type Store interface {
	Get(ctx context.Context, id string) (accounts.Account, error)
	LoadCredentialPayload(ctx context.Context, accountID string) (string, []byte, error)
	SaveCredentialPayload(ctx context.Context, accountID, format string, payload []byte) error
	Observe(ctx context.Context, id, remoteUID, status, lastError, lastKind string) error
}

// SecretReader is optional. Missing it means no global proxy, not an error.
type SecretReader interface {
	GetSecret(context.Context, string) (string, bool, error)
}

type Client struct {
	store Store
	http  *http.Client

	transports proxyutil.TransportCache

	baseURL string

	mu        sync.Mutex
	catalog   []providers.ModelInfo
	catalogAt time.Time
}

func NewClient(store Store) *Client {
	return &Client{
		store: store,
		http: &http.Client{
			Timeout: 120 * time.Second,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		baseURL: BaseURL,
	}
}

// SetBase overrides the API origin (tests point this at a local server).
func (c *Client) SetBase(base string) {
	if strings.TrimSpace(base) != "" {
		c.baseURL = strings.TrimRight(strings.TrimSpace(base), "/")
	}
}

func (c *Client) cliVersion() string {
	if v := strings.TrimSpace(os.Getenv(VersionEnv)); v != "" {
		return v
	}
	return PinnedCLIVersion
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
		return cred, fmt.Errorf("command credential incomplete; paste a user_… key")
	}
	return cred, nil
}

// endpointBase is the base used for outbound calls: the credential's origin when
// set, otherwise the client default (BaseURL, or a SetBase override in tests).
func (c *Client) endpointBase(credential Credential) string {
	if strings.TrimSpace(credential.BaseURL) != "" {
		return strings.TrimRight(strings.TrimSpace(credential.BaseURL), "/")
	}
	if strings.TrimSpace(c.baseURL) != "" {
		return strings.TrimRight(strings.TrimSpace(c.baseURL), "/")
	}
	return BaseURL
}

// Probe validates the key against /alpha/whoami. Used per request lifecycle by
// the account refresher; quota errors never flip readiness here because quota
// is fetched separately.
func (c *Client) Probe(ctx context.Context, accountID string) (providers.AccountHealth, error) {
	credential, err := c.credential(ctx, accountID)
	if err != nil {
		return providers.AccountHealth{LastError: err.Error()}, nil
	}
	client, err := c.httpClient(ctx, accountID)
	if err != nil {
		return providers.AccountHealth{LastError: err.Error()}, nil
	}
	profile, err := c.fetchWhoami(ctx, client, credential)
	if err != nil {
		if providerErr, ok := err.(*providers.Error); ok {
			return providers.AccountHealth{UID: credential.UserID, LastError: providerErr.Message}, nil
		}
		return providers.AccountHealth{UID: credential.UserID, LastError: err.Error()}, nil
	}
	uid := firstNonEmpty(profile.UserID, credential.UserID, credential.Email)
	_ = c.store.Observe(ctx, accountID, uid, "ready", "", "")
	return providers.AccountHealth{Ready: true, Hot: true, UID: uid}, nil
}

// Adapter wires the capability surface. There is no Login (auth is a pasted
// PAT) and no check-in; PAT login is surfaced through registry capabilities and
// the existing paste-key tab.
func (c *Client) Adapter() providers.Adapter {
	return providers.Adapter{
		ID:           "command",
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
