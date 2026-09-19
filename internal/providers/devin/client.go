package devin

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/caigee-cmd/cli2api/internal/accounts"
	"github.com/caigee-cmd/cli2api/internal/auth"
	"github.com/caigee-cmd/cli2api/internal/providers"
	proxyutil "github.com/caigee-cmd/cli2api/internal/proxy"
)

// Store is the persistence surface the adapter needs.
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

type loginPending struct {
	pkce        PKCECodes
	state       string
	callbackURL string
	createdAt   time.Time
	done        bool
	failed      bool
	message     string
	credential  Credential
}

type Client struct {
	store Store
	http  *http.Client

	transports proxyutil.TransportCache

	appBase    string
	apiBase    string
	serverBase string

	mu       sync.Mutex
	pending  map[string]*loginPending
	listener net.Listener
}

const loginPendingTTL = 10 * time.Minute

func NewClient(store Store) *Client {
	return &Client{
		store: store,
		http: &http.Client{
			Timeout: 120 * time.Second,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		appBase:    AppBase,
		apiBase:    APIBase,
		serverBase: ServerBase,
		pending:    map[string]*loginPending{},
	}
}

func (c *Client) SetBases(app, api, server string) {
	if strings.TrimSpace(app) != "" {
		c.appBase = strings.TrimRight(strings.TrimSpace(app), "/")
	}
	if strings.TrimSpace(api) != "" {
		c.apiBase = strings.TrimRight(strings.TrimSpace(api), "/")
	}
	if strings.TrimSpace(server) != "" {
		c.serverBase = strings.TrimRight(strings.TrimSpace(server), "/")
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
	cred = EnsureDeviceSeed(cred)
	if !cred.Ready() {
		return cred, fmt.Errorf("devin credential incomplete; re-login required")
	}
	return cred, nil
}

func (c *Client) StartLogin(ctx context.Context, accountID string) (providers.LoginSession, error) {
	_ = ctx
	pkce, err := GeneratePKCE()
	if err != nil {
		return providers.LoginSession{}, err
	}
	callbackURL, err := c.ensureCallback()
	if err != nil {
		return providers.LoginSession{}, err
	}
	state := randomHex(16)
	c.mu.Lock()
	c.pending[accountID] = &loginPending{
		pkce:        pkce,
		state:       state,
		callbackURL: callbackURL,
		createdAt:   time.Now(),
	}
	c.mu.Unlock()
	authURL := BuildAuthorizationURL(c.appBase, callbackURL, pkce.CodeChallenge, state)
	return providers.LoginSession{AuthURL: authURL, State: state}, nil
}

func (c *Client) PollLogin(ctx context.Context, accountID string) (bool, string, error) {
	c.mu.Lock()
	pending := c.pending[accountID]
	c.mu.Unlock()
	if pending == nil {
		return false, "", fmt.Errorf("login not started for account %s", accountID)
	}
	if time.Since(pending.createdAt) > loginPendingTTL {
		c.mu.Lock()
		delete(c.pending, accountID)
		c.mu.Unlock()
		return false, "", fmt.Errorf("login expired; start again")
	}
	c.mu.Lock()
	done := pending.done
	failed := pending.failed
	message := pending.message
	credential := pending.credential
	c.mu.Unlock()
	if failed {
		return false, "", fmt.Errorf("%s", firstNonEmpty(message, "login failed"))
	}
	if !done {
		return false, firstNonEmpty(message, "waiting for authorization"), nil
	}
	if err := c.finishCredential(ctx, accountID, credential); err != nil {
		return false, "", err
	}
	c.mu.Lock()
	delete(c.pending, accountID)
	c.mu.Unlock()
	return true, "login complete", nil
}

func (c *Client) CompleteLogin(ctx context.Context, accountID, callbackURL string) error {
	code, state, sessionToken, err := ParseCallbackOrPaste(callbackURL)
	if err != nil {
		return err
	}

	c.mu.Lock()
	pending := c.pending[accountID]
	c.mu.Unlock()

	var credential Credential
	if sessionToken != "" {
		credential = Credential{SessionToken: FormatSessionToken(sessionToken)}
	} else {
		verifier := ""
		if pending != nil {
			if state != "" && pending.state != "" && state != pending.state {
				return fmt.Errorf("oauth state mismatch")
			}
			verifier = pending.pkce.CodeVerifier
		}
		if verifier == "" {
			return fmt.Errorf("login not started; cannot exchange code without PKCE verifier")
		}
		client, err := c.httpClient(ctx, accountID)
		if err != nil {
			return err
		}
		token, err := ExchangeCode(ctx, client, c.apiBase, code, verifier)
		if err != nil {
			return err
		}
		credential = Credential{SessionToken: FormatSessionToken(token)}
	}

	if pending != nil && strings.TrimSpace(pending.credential.DeviceSeed) != "" {
		credential.DeviceSeed = pending.credential.DeviceSeed
	} else if _, payload, err := c.store.LoadCredentialPayload(ctx, accountID); err == nil {
		if decoded, err := DecodeCredential(payload); err == nil {
			if decoded.Ready() && sessionToken == "" && code == "" {
				return nil
			}
			credential.DeviceSeed = decoded.DeviceSeed
		}
	}

	if err := c.finishCredential(ctx, accountID, credential); err != nil {
		return err
	}
	c.mu.Lock()
	delete(c.pending, accountID)
	c.mu.Unlock()
	return nil
}

func (c *Client) finishCredential(ctx context.Context, accountID string, credential Credential) error {
	credential.SessionToken = FormatSessionToken(credential.SessionToken)
	credential = EnsureDeviceSeed(credential)
	client, err := c.httpClient(ctx, accountID)
	if err != nil {
		return err
	}
	if userName, userID, orgID, errSelf := FetchSelfProfile(ctx, client, c.apiBase, credential.SessionToken); errSelf == nil {
		if userName != "" {
			credential.UserName = userName
		}
		if userID != "" {
			credential.UserID = userID
		}
		if orgID != "" {
			credential.OrgID = orgID
		}
	}
	server := firstNonEmpty(credential.BaseURL, c.serverBase, ServerBase)
	if status, errStatus := FetchUserStatus(ctx, client, server, credential.SessionToken, credential.DeviceSeed); errStatus == nil && status != nil {
		if credential.UserName == "" {
			credential.UserName = status.UserName
		}
		if credential.UserID == "" {
			credential.UserID = status.UserID
		}
		if credential.OrgID == "" {
			credential.OrgID = status.OrgID
		}
		if credential.Email == "" {
			credential.Email = status.Email
		}
	}
	credential.BaseURL = server
	payload, err := credential.Encode()
	if err != nil {
		return err
	}
	if err := c.store.SaveCredentialPayload(ctx, accountID, CredentialFormat, payload); err != nil {
		return err
	}
	_ = c.store.Observe(ctx, accountID, credential.UserID, "ready", "", "")
	return nil
}

func (c *Client) ensureCallback() (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.listener != nil {
		addr, ok := c.listener.Addr().(*net.TCPAddr)
		if ok {
			return fmt.Sprintf("http://127.0.0.1:%d/callback", addr.Port), nil
		}
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", fmt.Errorf("devin callback listen: %w", err)
	}
	c.listener = ln
	auth.ServeLoopback(ln, "/callback", "Devin", c.acceptCallback)
	addr := ln.Addr().(*net.TCPAddr)
	return fmt.Sprintf("http://127.0.0.1:%d/callback", addr.Port), nil
}

func (c *Client) acceptCallback(ctx context.Context, rawURL string) error {
	code, state, sessionToken, err := ParseCallbackOrPaste(rawURL)
	if err != nil {
		c.markPendingFailed(err.Error())
		return err
	}
	c.mu.Lock()
	for _, pending := range c.pending {
		if pending.done || pending.failed {
			continue
		}
		if state != "" && pending.state != "" && state != pending.state {
			continue
		}
		if sessionToken != "" {
			pending.credential = Credential{SessionToken: FormatSessionToken(sessionToken)}
			pending.done = true
			pending.message = "authorization received"
			break
		}
		// Exchange happens in PollLogin/CompleteLogin with verifier.
		pending.message = "authorization code received"
		pending.credential = Credential{}
		// Store code temporarily in message path via pending fields: reuse DeviceSeed slot? Better add fields.
		// Use UserName as ephemeral code carrier is ugly. Mark done=false and stash in Email? No.
		// Instead set a synthetic token path: keep code in OrgID temporarily? Still ugly.
		// Simplest: exchange here if verifier present.
		client := &http.Client{Timeout: 30 * time.Second}
		token, exErr := ExchangeCode(ctx, client, c.apiBase, code, pending.pkce.CodeVerifier)
		if exErr != nil {
			pending.failed = true
			pending.message = exErr.Error()
			break
		}
		pending.credential = Credential{SessionToken: FormatSessionToken(token)}
		pending.done = true
		pending.message = "authorization received"
		break
	}
	c.mu.Unlock()
	return nil
}

func (c *Client) markPendingFailed(message string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, pending := range c.pending {
		if pending.done || pending.failed {
			continue
		}
		pending.failed = true
		pending.message = message
		return
	}
}

func (c *Client) Probe(ctx context.Context, accountID string) (providers.AccountHealth, error) {
	credential, err := c.credential(ctx, accountID)
	if err != nil {
		return providers.AccountHealth{LastError: err.Error()}, nil
	}
	if !credential.Ready() {
		msg := "devin credential incomplete; re-login required"
		return providers.AccountHealth{UID: credential.UserID, LastError: msg}, nil
	}
	client, err := c.httpClient(ctx, accountID)
	if err != nil {
		return providers.AccountHealth{UID: credential.UserID, LastError: err.Error()}, nil
	}
	status, err := FetchUserStatus(ctx, client, firstNonEmpty(credential.BaseURL, c.serverBase), credential.SessionToken, credential.DeviceSeed)
	if err != nil {
		return providers.AccountHealth{UID: credential.UserID, LastError: err.Error()}, nil
	}
	uid := firstNonEmpty(status.UserID, credential.UserID)
	return providers.AccountHealth{Ready: true, Hot: true, UID: uid}, nil
}

func (c *Client) Quota(ctx context.Context, accountID string) (*providers.QuotaInfo, error) {
	credential, err := c.credential(ctx, accountID)
	if err != nil {
		return nil, err
	}
	client, err := c.httpClient(ctx, accountID)
	if err != nil {
		return nil, err
	}
	status, err := FetchUserStatus(ctx, client, firstNonEmpty(credential.BaseURL, c.serverBase), credential.SessionToken, credential.DeviceSeed)
	if err != nil {
		return nil, err
	}
	return quotaFromStatus(status, time.Now().UTC()), nil
}

func quotaFromStatus(status *UserStatus, fetchedAt time.Time) *providers.QuotaInfo {
	if status == nil {
		return nil
	}
	var windows []providers.QuotaWindow
	if !status.HideDailyQuota {
		windows = append(windows, percentQuotaWindow("daily", "Daily quota", status.DailyQuotaRemainingPercent, status.DailyQuotaResetAt))
	}
	if !status.HideWeeklyQuota {
		windows = append(windows, percentQuotaWindow("weekly", "Weekly quota", status.WeeklyQuotaRemainingPercent, status.WeeklyQuotaResetAt))
	}
	info := &providers.QuotaInfo{
		Unit:      QuotaUnit,
		FetchedAt: fetchedAt.Format(time.RFC3339),
		Windows:   windows,
	}
	if len(windows) == 0 {
		return info
	}
	tightest := windows[0]
	for _, window := range windows[1:] {
		if window.Remaining < tightest.Remaining {
			tightest = window
		}
	}
	info.Used = tightest.Used
	info.Total = tightest.Total
	info.Remaining = tightest.Remaining
	info.Percentage = tightest.Percentage
	info.Unit = tightest.Unit
	info.Exceeded = tightest.Exceeded
	return info
}

func percentQuotaWindow(id, label string, remainingPercent int64, resetAt time.Time) providers.QuotaWindow {
	remaining := remainingPercent
	if remaining < 0 {
		remaining = 0
	}
	if remaining > 100 {
		remaining = 100
	}
	used := 100 - remaining
	window := providers.QuotaWindow{
		ID:         id,
		Label:      label,
		Used:       float64(used),
		Total:      100,
		Remaining:  float64(remaining),
		Percentage: float64(used),
		Unit:       QuotaUnit,
		Exceeded:   remaining <= 0,
	}
	if !resetAt.IsZero() {
		window.ResetAt = resetAt.UTC().Format(time.RFC3339)
	}
	return window
}

func (c *Client) Adapter() providers.Adapter {
	return providers.Adapter{
		ID:           "devin",
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

type importer struct{}

func (importer) ValidateImport(payload []byte) error { return ValidateCredential(payload) }

func (importer) Export(ctx context.Context, accountID string) (map[string]any, error) {
	_ = ctx
	_ = accountID
	return nil, providers.ErrUnsupported
}
