package doubaowork

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

// BaseURL is the Doubao Work origin. The desktop/menu/chat endpoints all live
// under it.
const BaseURL = "https://www.doubao.com"

// FallbackBot is the default assistant bot id used when a pasted Cookie cannot
// resolve one from /alice/user/launch. It is a real desktop default, not a
// synthetic value.
const FallbackBot = "7338286299411103781"

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

// catalogCache holds a per-account model menu snapshot.
type catalogCache struct {
	catalog map[string]doubaoModel
	at      time.Time
}

// Client is the in-process Doubao Work adapter. One client serves every
// account; per-account session state and catalogs stay keyed by account id.
type Client struct {
	store      Store
	http       *http.Client
	transports proxyutil.TransportCache

	baseURL string

	mu       sync.Mutex
	sessions map[string]*session
	catalogs map[string]catalogCache
	failedAt map[string]time.Time
}

// NewClient constructs the adapter client.
func NewClient(store Store) *Client {
	return &Client{
		store: store,
		http: &http.Client{
			Timeout: 0,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		baseURL:  BaseURL,
		sessions: map[string]*session{},
		catalogs: map[string]catalogCache{},
		failedAt: map[string]time.Time{},
	}
}

// credential loads and decodes the stored credential for an account.
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
		return cred, newCodeError(codeLoginExpired, "doubaowork credential has no session cookie; re-import the Cookie")
	}
	return cred, nil
}

// sessionFor returns the long-lived per-account session, creating it lazily.
func (c *Client) sessionFor(accountID string) *session {
	c.mu.Lock()
	defer c.mu.Unlock()
	existing, ok := c.sessions[accountID]
	if ok {
		return existing
	}
	created := &session{client: c, accountID: accountID}
	c.sessions[accountID] = created
	return created
}

func (c *Client) globalProxy(ctx context.Context) (string, error) {
	reader, ok := c.store.(SecretReader)
	if !ok {
		return "", nil
	}
	value, found, err := reader.GetSecret(ctx, "proxy_url")
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

// Probe verifies the stored Cookie can read its own identity. It is used for
// account readiness; quota is probed separately.
func (c *Client) Probe(ctx context.Context, accountID string) (providers.AccountHealth, error) {
	sess := c.sessionFor(accountID)
	if err := sess.ready(ctx); err != nil {
		if typed, ok := err.(*providers.Error); ok {
			return providers.AccountHealth{LastError: typed.Message}, nil
		}
		return providers.AccountHealth{LastError: err.Error()}, nil
	}
	_ = c.store.Observe(ctx, accountID, sess.uid, "ready", "", "")
	return providers.AccountHealth{Ready: true, Hot: true, UID: sess.uid}, nil
}
