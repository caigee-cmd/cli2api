package orcarouter

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/caigee-cmd/cli2api/internal/accounts"
	"github.com/caigee-cmd/cli2api/internal/providers"
	proxyutil "github.com/caigee-cmd/cli2api/internal/proxy"
)

// Store is the persistence surface this adapter needs. It mirrors the other
// in-process adapters: the provider talks to the account store through a narrow
// interface and never imports internal/store.
type Store interface {
	Get(ctx context.Context, id string) (accounts.Account, error)
	LoadCredentialPayload(ctx context.Context, accountID string) (string, []byte, error)
	SaveCredentialPayload(ctx context.Context, accountID, format string, payload []byte) error
	Observe(ctx context.Context, id, remoteUID, status, lastError, lastKind string) error
}

// SecretReader is optional. It backs the global proxy and the self-hosted
// origin overrides; missing it means "no override, no proxy", not an error.
type SecretReader interface {
	GetSecret(context.Context, string) (string, bool, error)
}

type catalogCacheEntry struct {
	models []providers.ModelInfo
	at     time.Time
}

// loginPending is one in-flight PKCE attempt. The verifier stays here and is
// only ever presented at the exchange.
type loginPending struct {
	verifier    string
	state       string
	callbackURL string
	authBase    string
	apiBase     string
	authURL     string
	attempt     int64
	createdAt   time.Time
	done        bool
	failed      bool
	message     string
	authCode    string
	// credential carries the exchange result forward to PollLogin.
	credential Credential
}

type Client struct {
	store Store
	http  *http.Client

	transports proxyutil.TransportCache

	mu      sync.Mutex
	pending map[string]*loginPending
	// pendingStates matches a pasted callback URL to the attempt that issued its
	// code_challenge, even when a newer login replaced the account slot.
	pendingStates map[string]*loginPending
	listener      net.Listener
	port          int
	attemptSeq    int64

	catalog map[string]catalogCacheEntry
}

// NewClient builds the adapter client.
func NewClient(store Store) *Client {
	return &Client{
		store: store,
		http: &http.Client{
			Timeout: 120 * time.Second,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		pending:       map[string]*loginPending{},
		pendingStates: map[string]*loginPending{},
		catalog:       map[string]catalogCacheEntry{},
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
	if cred.NeedsReauth() {
		return cred, fmt.Errorf("orcarouter credential was revoked; reconnect the account")
	}
	if !cred.Ready() {
		return cred, fmt.Errorf("orcarouter credential incomplete; paste an sk-orca-… key")
	}
	return cred, nil
}

// originSecret reads a stored origin override. The explicit per-origin values
// win over the shared self-hosted base; environment values win over both.
func (c *Client) originSecret(ctx context.Context, key string) string {
	store, ok := c.store.(SecretReader)
	if !ok {
		return ""
	}
	value, found, err := store.GetSecret(ctx, key)
	if err != nil || !found {
		return ""
	}
	return strings.TrimSpace(value)
}

// origins resolves the auth and inference origins for one credential. An
// account-level override wins, then the explicit per-origin environment/console
// setting, then the shared self-hosted base, then the public defaults. The two
// origins are resolved independently and one is never derived from the other.
func (c *Client) origins(ctx context.Context, credential Credential) (Origins, error) {
	explicitAuth := firstNonEmpty(credential.AuthBase, os.Getenv(AuthBaseEnv), c.originSecret(ctx, SecretAuthBase))
	explicitAPI := firstNonEmpty(credential.APIBase, os.Getenv(APIBaseEnv), c.originSecret(ctx, SecretAPIBase))
	shared := firstNonEmpty(os.Getenv(SharedEnv), c.originSecret(ctx, SecretSharedBase))
	return ResolveOrigins(shared, explicitAuth, explicitAPI)
}

// originsForChat resolves only the inference origin, which is all a chat turn
// needs. It keeps an operator's plain public default out of ResolveOrigins so a
// default-valued auth base can never turn an otherwise-default API base into a
// loopback-only rejection.
func (c *Client) originsForChat(ctx context.Context, credential Credential) (Origins, error) {
	explicitAPI := firstNonEmpty(credential.APIBase, os.Getenv(APIBaseEnv), c.originSecret(ctx, SecretAPIBase))
	if explicitAPI == "" {
		return Origins{AuthBase: DefaultAuthBase, APIBase: DefaultAPIBase}, nil
	}
	shared := firstNonEmpty(os.Getenv(SharedEnv), c.originSecret(ctx, SecretSharedBase))
	auth := firstNonEmpty(credential.AuthBase, os.Getenv(AuthBaseEnv), c.originSecret(ctx, SecretAuthBase))
	return ResolveOrigins(shared, auth, explicitAPI)
}

// apiBase resolves the inference origin for one credential: an account-level
// override wins, then the explicit API environment/console setting, then the
// shared self-hosted base, then the public default. It is never derived from the
// auth origin.
func (c *Client) apiBase(ctx context.Context, credential Credential) string {
	if value := strings.TrimSpace(credential.APIBase); value != "" {
		return strings.TrimRight(value, "/")
	}
	explicit := firstNonEmpty(os.Getenv(APIBaseEnv), c.originSecret(ctx, SecretAPIBase))
	if explicit != "" {
		if normalized, err := normalizeOrigin(explicit, "/v1"); err == nil {
			return normalized
		}
	}
	shared := firstNonEmpty(os.Getenv(SharedEnv), c.originSecret(ctx, SecretSharedBase))
	if shared != "" {
		origins, err := ResolveOrigins(shared, "", "")
		if err == nil {
			return origins.APIBase
		}
	}
	return DefaultAPIBase
}

// Adapter wires the capability surface for the pasted-API-key entry. There is no
// Login: this entry never starts a PKCE flow, it only stores a key the operator
// already holds. Both entries share the same chat path, model catalog, classifier
// and prober, so inference behaviour cannot diverge between them.
func (c *Client) Adapter() providers.Adapter {
	return providers.Adapter{
		ID:           ProviderID,
		Credential:   credentialCodec{},
		Chat:         c,
		Models:       c,
		Classifier:   classifier{},
		ImportExport: importer{},
		Prober:       c,
	}
}

// OAuthAdapter is the PKCE entry's adapter. It adds the login session on top of
// the same shared surface; only the provider id and the credential acquisition
// differ, so the two entries can never drift apart.
func (c *Client) OAuthAdapter() providers.Adapter {
	adapter := c.Adapter()
	adapter.ID = OAuthProviderID
	adapter.Login = c
	return adapter
}

type credentialCodec struct{}

func (credentialCodec) Validate(payload []byte) error { return ValidateCredential(payload) }

func (credentialCodec) Format() string { return CredentialFormat }

func (credentialCodec) PrepareImport(payload []byte) (providers.CredentialImport, error) {
	if err := ValidateCredential(payload); err != nil {
		return providers.CredentialImport{}, err
	}
	credential, err := DecodeCredential(payload)
	if err != nil {
		return providers.CredentialImport{}, err
	}
	encoded, err := credential.Encode()
	if err != nil {
		return providers.CredentialImport{}, err
	}
	return providers.CredentialImport{Payload: encoded, Ready: credential.Ready()}, nil
}

type importer struct{}

func (importer) ValidateImport(payload []byte) error { return ValidateCredential(payload) }

func (importer) Export(ctx context.Context, accountID string) (map[string]any, error) {
	return nil, providers.ErrUnsupported
}

type classifier struct{}

func (classifier) Classify(status int, body string) providers.ClassifiedError {
	return Classify(status, body)
}

// Probe validates the stored key against the inference origin's model catalog.
// A 401 is terminal: the exact credential generation the request used is marked
// needs_reauth, and the account is reported as requiring reauthentication. No
// refresh is attempted because an OrcaRouter key has no refresh grant.
func (c *Client) Probe(ctx context.Context, accountID string) (providers.AccountHealth, error) {
	credential, err := c.credential(ctx, accountID)
	if err != nil {
		return providers.AccountHealth{LastError: err.Error()}, nil
	}
	client, err := c.httpClient(ctx, accountID)
	if err != nil {
		return providers.AccountHealth{LastError: err.Error()}, nil
	}
	scoped := *client
	scoped.Timeout = catalogTimeout
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.apiBase(ctx, credential)+ModelsPath, nil)
	if err != nil {
		return providers.AccountHealth{UID: credential.UserID, LastError: err.Error()}, nil
	}
	req.Header.Set("Authorization", "Bearer "+credential.APIKey)
	req.Header.Set("Accept", "application/json")
	for name, value := range credential.Headers {
		req.Header.Set(name, value)
	}
	resp, err := scoped.Do(req)
	if err != nil {
		return providers.AccountHealth{UID: credential.UserID, LastError: redactSecrets(err.Error())}, nil
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		if classifyCredentialError(resp.StatusCode, "") {
			c.markNeedsReauth(ctx, accountID, credential.Generation, resp.StatusCode)
			return providers.AccountHealth{
				UID:       credential.UserID,
				LastError: "orcarouter key rejected (401); reconnect the account",
			}, nil
		}
		return providers.AccountHealth{
			UID:       credential.UserID,
			LastError: fmt.Sprintf("orcarouter probe failed: HTTP %d", resp.StatusCode),
		}, nil
	}
	uid := firstNonEmpty(credential.UserID, credential.Email, accountID)
	_ = c.store.Observe(ctx, accountID, uid, "ready", "", "")
	return providers.AccountHealth{Ready: true, Hot: true, UID: uid}, nil
}

// Quota is not implemented: OrcaRouter exposes no stable, non-billing usage
// endpoint through this adapter, so the console shows "unknown" rather than a
// fabricated number.
func (c *Client) Quota(ctx context.Context, accountID string) (*providers.QuotaInfo, error) {
	return nil, nil
}

// markNeedsReauth flips only the exact credential generation that made the
// rejected request. A late 401 from an old request must not mark a credential
// that a newer login already replaced.
func (c *Client) markNeedsReauth(ctx context.Context, accountID string, generation int64, status int) {
	current, _, err := loadCredential(c.store, ctx, accountID)
	if err != nil {
		return
	}
	if current.Generation != generation {
		return
	}
	if current.NeedsReauth() {
		return
	}
	current.State = CredentialStateNeedsReauth
	encoded, err := current.Encode()
	if err != nil {
		return
	}
	if err := c.store.SaveCredentialPayload(ctx, accountID, current.Format, encoded); err != nil {
		return
	}
	_ = c.store.Observe(ctx, accountID, current.UserID, "login_required",
		"orcarouter key rejected; reconnect the account", accounts.KindAuth)
}

func loadCredential(store Store, ctx context.Context, accountID string) (Credential, []byte, error) {
	_, payload, err := store.LoadCredentialPayload(ctx, accountID)
	if err != nil {
		return Credential{}, nil, err
	}
	credential, err := DecodeCredential(payload)
	if err != nil {
		return Credential{}, payload, err
	}
	return credential, payload, nil
}
