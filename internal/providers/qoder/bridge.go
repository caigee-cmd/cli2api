package qoder

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/caigee-cmd/cli2api/internal/accounts"
	"github.com/caigee-cmd/cli2api/internal/providers/qoder/qauth"
)

// Bridge manages one Qoder PAT's in-process session: cold bootstrap, proactive
// and forced renewal, dynamic model catalog, account subscription status +
// authoritative quota, and the OpenAPI bearer cache for regions whose quota
// endpoint rejects the COSY token. It is the in-process replacement for the
// per-account worker's login/catalog/quota duties; chat signing continues to
// live in cosy.go / direct.go.
//
// The session lifecycle (bootstrapSession / doRenew / EnsureFreshSession /
// forceRefresh / quotaBearer) is ported from the qoder2api reference
// (Qoder-api/bridge/session.go and bridge.go); the machine identity is derived
// deterministically from the account uid (cli2api's cosy.go derivation) so a
// restart does not mint a new device — important for the check-in path, which
// the OpenAPI may tie to a previously-seen machine fingerprint.
type Bridge struct {
	// Region is the account's region config (CN or Global). Set at construction;
	// never mutated.
	Region *qauth.RegionConfig
	// PAT is the long-lived personal access token. Set at construction; never
	// mutated.
	PAT string

	// machineID / machineToken / machineType are derived from the account uid
	// so they are stable across restarts. They are read concurrently from
	// request handlers and the renewal path, so once set they are immutable.
	machineID    string
	machineToken string
	machineType  string

	// mu guards sess/identity/tokens/expiry. ApplyJobToken takes it when
	// swapping in a freshly-minted session; request handlers read under
	// currentSess / currentIdentity / currentSecurityOauth.
	mu           sync.Mutex
	sess         *qauth.SessionContext
	identity     *qauth.AuthIdentity
	refreshToken string
	securityOath string
	expireTimeMs int64
	refreshedMs  int64

	// refreshMu serializes renewal network calls so a stampede of refresh
	// requests at expiry does not trip the gateway's token-rotation race.
	refreshMu    sync.Mutex
	bootstrapped atomic.Bool

	// catalog caches the model/list response with a TTL; single-flighted so a
	// thundering herd of routing decisions does not pile parallel model/list
	// calls onto the gateway.
	catalogMu    sync.Mutex
	catalog      []map[string]any // raw model entries, ready for ModelInfos
	catalogRaw   map[string]any   // raw model/list response for ExtractCatalog callers
	catalogTs    int64            // unix seconds
	catalogInflt bool

	// account caches the /user/status + quota/usage snapshot. Both are
	// best-effort enrichment: the cached tier covers a transient status
	// outage and the cached quota covers a transient OpenAPI outage.
	accountMu sync.Mutex
	account   *qauth.AccountStatus
	accountTs int64 // unix seconds

	// openAPIMu guards the OpenAPI bearer cache (global region only).
	openAPIMu      sync.Mutex
	openAPIToken   string
	openAPIRefresh string
	openAPIExpMs   int64
}

// refreshMarginMs renews the session this long before the reported expiry.
const refreshMarginMs = int64(2 * 3600 * 1000) // 2 hours

// unknownExpiryTTL bounds how long a session whose expireTime is missing is
// trusted. Without it, expireTimeMs==0 makes needsRefresh always true and
// degrades every request to a serialized renewal.
const unknownExpiryTTL = 30 * time.Minute

// catalogTTL bounds how long a fetched model/list response is trusted.
const catalogTTL = 10 * time.Minute

// accountTTL bounds how long a cached /user/status result is trusted.
const accountTTL = time.Hour

// accountStatusTimeout bounds the /user/status lookup performed during
// renewal (under refreshMu): it is best-effort enrichment, the cached tier
// already covers the failure, so it must not hold the session lock for the
// full request timeout.
const accountStatusTimeout = 5 * time.Second

// maxPlausibleResetMs bounds a believable subscription boundary. The OpenAPI
// quota endpoint reports 9999-12-31 (253402214400000) as a "never expires"
// sentinel for plans without an expiry; that must not be mistaken for the
// instant the allowance actually refreshes.
const maxPlausibleResetMs = int64(4102444800000) // 2100-01-01T00:00:00Z

func plausibleResetMs(ms int64) bool {
	return ms > 0 && ms < maxPlausibleResetMs
}

// NewBridge constructs a bridge for the given PAT and region. machineID is
// the caller-derived device fingerprint (cli2api derives it deterministically
// from the account uid so a restart does not mint a new device, which matters
// for the OpenAPI check-in path that may tie a campaign to a machine).
func NewBridge(pat string, region *qauth.RegionConfig, machineID, machineToken, machineType string) *Bridge {
	if region == nil {
		region = qauth.CN
	}
	return &Bridge{
		Region:       region,
		PAT:          pat,
		machineID:    machineID,
		machineToken: machineToken,
		machineType:  machineType,
	}
}

// Bootstrap performs the cold PAT→jobToken exchange and resolves the
// authoritative subscription tier BEFORE the session is built (the tier is
// AES-signed into the bearer payload and cannot be amended afterwards). It is
// idempotent for an already-bootstrapped bridge.
func (b *Bridge) Bootstrap(ctx context.Context) error {
	if b.bootstrapped.Load() {
		return nil
	}
	b.refreshMu.Lock()
	defer b.refreshMu.Unlock()
	if b.bootstrapped.Load() {
		return nil
	}
	return b.bootstrapSession(ctx)
}

func (b *Bridge) bootstrapSession(ctx context.Context) error {
	jt, err := qauth.ExchangeJobToken(ctx, b.PAT, b.machineID, b.machineToken, b.machineType, b.Region)
	if err != nil {
		return err
	}
	id, _ := jt["id"].(string)
	userType := b.resolveAccountStatus(ctx, id)
	name, _ := jt["name"].(string)
	log.Printf("[qoder-bridge] session for %s (%s) [%s] exp=%v userType=%s", name, id, b.Region.Name, jt["expireTime"], userType)
	b.applyJobToken(jt, userType)
	b.bootstrapped.Store(true)
	return nil
}

// resolveAccountStatus fetches /user/status and returns the authoritative
// subscription tier. Bounded by accountStatusTimeout because it runs under
// refreshMu; on failure it degrades to last-known tier → empty (the
// historical default then applies in applyJobToken).
func (b *Bridge) resolveAccountStatus(ctx context.Context, userID string) string {
	lookupCtx, cancel := context.WithTimeout(ctx, accountStatusTimeout)
	defer cancel()
	st, err := b.fetchAccountStatus(lookupCtx, userID)
	if err == nil && st != nil && st.UserType != "" {
		return st.UserType
	}
	if err != nil && !errors.Is(err, errNoUserID) {
		log.Printf("[qoder-bridge] WARN user/status failed (%v); reusing last known tier", err)
	}
	if cached := b.AccountStatus(); cached != nil && cached.UserType != "" {
		return cached.UserType
	}
	return ""
}

// errNoUserID marks the expected degradation where the first jobToken exchange
// carries no user id and /user/status cannot be queried yet.
var errNoUserID = errors.New("no user id available for user/status")

// fetchAccountStatus queries /user/status with a TTL cache. Callers are the
// renewal flows (serialized by refreshMu) and the admin poll, so no
// single-flight is needed here.
func (b *Bridge) fetchAccountStatus(ctx context.Context, userID string) (*qauth.AccountStatus, error) {
	now := time.Now().Unix()
	b.accountMu.Lock()
	if cached := b.account; cached != nil && now-b.accountTs < int64(accountTTL.Seconds()) {
		b.accountMu.Unlock()
		return cloneAccountStatus(cached), nil
	}
	b.accountMu.Unlock()

	if userID == "" {
		return nil, errNoUserID
	}
	raw, err := qauth.FetchUserStatus(ctx, userID, b.machineID, b.machineToken, b.machineType, b.Region)
	if err != nil {
		return nil, err
	}
	st, ok := qauth.ParseAccountStatus(raw)
	if !ok {
		return nil, fmt.Errorf("user/status response shape unexpected")
	}
	b.accountMu.Lock()
	// A successful identity refresh must not erase the last authoritative
	// quota snapshot if the immediately following OpenAPI call fails.
	if previous := b.account; previous != nil && previous.UserQuota != nil {
		st.NextResetAtMs = previous.NextResetAtMs
		st.IsQuotaExceeded = previous.IsQuotaExceeded
		st.TotalUsagePercentage = previous.TotalUsagePercentage
		st.UserQuota = previous.UserQuota
		st.AddOnQuota = previous.AddOnQuota
		st.OrgResourcePackage = previous.OrgResourcePackage
	}
	b.account = cloneAccountStatus(&st)
	b.accountTs = time.Now().Unix()
	b.accountMu.Unlock()
	return &st, nil
}

// EnsureAccountStatus returns the subscription state, refreshing both the
// gateway identity metadata and the authoritative OpenAPI quota snapshot.
// Failures preserve the last known snapshot so a temporary account-service
// outage never breaks the caller's accounting loop.
func (b *Bridge) EnsureAccountStatus(ctx context.Context) *qauth.AccountStatus {
	if err := b.EnsureFreshSession(ctx); err != nil {
		log.Printf("[qoder-bridge] WARN account status unavailable (session: %v)", err)
		return b.AccountStatus()
	}
	userID := ""
	if ident := b.currentIdentity(); ident != nil {
		userID = ident.Aid
	}
	st, err := b.fetchAccountStatus(ctx, userID)
	if err != nil {
		log.Printf("[qoder-bridge] WARN user/status refresh failed: %v", err)
		st = b.AccountStatus()
	}

	bearer, err := b.quotaBearer(ctx)
	if err != nil {
		log.Printf("[qoder-bridge] WARN quota/usage token unavailable: %v", err)
		return st
	}
	quota, err := qauth.FetchQuotaUsage(ctx, bearer, b.Region)
	if err != nil {
		log.Printf("[qoder-bridge] WARN quota/usage refresh failed: %v", err)
		return st
	}
	if st == nil {
		st = &qauth.AccountStatus{}
	} else {
		st = cloneAccountStatus(st)
	}
	if quota.UserType != "" {
		st.UserType = quota.UserType
	}
	if !plausibleResetMs(st.NextResetAtMs) {
		st.NextResetAtMs = 0
	}
	if st.NextResetAtMs == 0 && plausibleResetMs(quota.ExpiresAtMs) {
		st.NextResetAtMs = quota.ExpiresAtMs
	}
	st.IsQuotaExceeded = quota.IsQuotaExceeded
	st.TotalUsagePercentage = quota.TotalUsagePercentage
	st.UserQuota = quota.UserQuota
	st.AddOnQuota = quota.AddOnQuota
	st.OrgResourcePackage = quota.OrgResourcePackage

	b.accountMu.Lock()
	b.account = cloneAccountStatus(st)
	b.accountMu.Unlock()
	return cloneAccountStatus(st)
}

// AccountStatus returns the cached subscription state, or nil when it has
// never been resolved. Performs no network call, so it is safe from a request
// handler at any cadence.
func (b *Bridge) AccountStatus() *qauth.AccountStatus {
	b.accountMu.Lock()
	defer b.accountMu.Unlock()
	return cloneAccountStatus(b.account)
}

func cloneAccountStatus(st *qauth.AccountStatus) *qauth.AccountStatus {
	if st == nil {
		return nil
	}
	cp := *st
	if st.UserQuota != nil {
		q := *st.UserQuota
		cp.UserQuota = &q
	}
	if st.AddOnQuota != nil {
		q := *st.AddOnQuota
		cp.AddOnQuota = &q
	}
	if st.OrgResourcePackage != nil {
		q := *st.OrgResourcePackage
		cp.OrgResourcePackage = &q
	}
	return &cp
}

// applyJobToken builds the signed session from a jobToken response. userType
// is the tier resolved from /user/status; when empty the jobToken value is
// used, and only then does the historical default apply.
func (b *Bridge) applyJobToken(jt map[string]any, userType string) {
	name, _ := jt["name"].(string)
	id, _ := jt["id"].(string)
	if userType == "" {
		userType, _ = jt["userType"].(string)
	}
	if userType == "" {
		userType = "personal_standard"
	}
	securityOauth, _ := jt["securityOauthToken"].(string)
	refreshToken, _ := jt["refreshToken"].(string)

	identity := qauth.AuthIdentity{
		Name:               name,
		Aid:                id,
		UID:                id,
		UserType:           userType,
		SecurityOauthToken: securityOauth,
		RefreshToken:       refreshToken,
	}
	sess := qauth.NewSession(identity, b.machineID, b.machineToken, b.machineType)

	b.mu.Lock()
	b.sess = sess
	b.identity = &identity
	b.refreshToken = refreshToken
	b.securityOath = securityOauth
	b.expireTimeMs = toInt64(jt["expireTime"])
	b.refreshedMs = time.Now().UnixMilli()
	if b.expireTimeMs == 0 {
		log.Printf("[qoder-bridge] WARN gateway did not report expireTime; assuming %s validity", unknownExpiryTTL)
	}
	b.mu.Unlock()
}

func toInt64(v any) int64 {
	switch n := v.(type) {
	case float64:
		return int64(n)
	case string:
		val, _ := strconv.ParseInt(n, 10, 64)
		return val
	case json.Number:
		val, _ := n.Int64()
		return val
	default:
		return 0
	}
}

func (b *Bridge) needsRefresh() bool {
	nowMs := time.Now().UnixMilli()
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.sess == nil {
		return true
	}
	if b.expireTimeMs == 0 {
		return nowMs-b.refreshedMs > unknownExpiryTTL.Milliseconds()
	}
	return nowMs > b.expireTimeMs-refreshMarginMs
}

// currentSess returns the active session, panicking if none is bootstrapped.
// Callers MUST have called EnsureFreshSession first.
func (b *Bridge) currentSess() *qauth.SessionContext {
	b.mu.Lock()
	sess := b.sess
	b.mu.Unlock()
	if sess == nil {
		panic("qoder bridge session not bootstrapped")
	}
	return sess
}

func (b *Bridge) currentIdentity() *qauth.AuthIdentity {
	b.mu.Lock()
	id := b.identity
	b.mu.Unlock()
	return id
}

func (b *Bridge) currentSecurityOauth() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.securityOath
}

// quotaBearerTTLMargin refreshes the OpenAPI bearer this long before its
// reported expiry so a quota call never races a token that dies mid-flight.
const quotaBearerTTLMargin = 5 * time.Minute

// quotaBearer returns the bearer to use for the quota/usage endpoint. The CN
// region accepts the COSY session token directly; the global region rejects
// it (401 TOKEN_EXPIRE) and needs a separately-issued OpenAPI token.
func (b *Bridge) quotaBearer(ctx context.Context) (string, error) {
	if !b.Region.NeedsOpenAPIToken {
		return b.currentSecurityOauth(), nil
	}
	b.openAPIMu.Lock()
	defer b.openAPIMu.Unlock()
	if b.openAPIToken != "" && (b.openAPIExpMs == 0 || time.Now().UnixMilli() < b.openAPIExpMs-int64(quotaBearerTTLMargin.Milliseconds())) {
		return b.openAPIToken, nil
	}
	var tok *qauth.OpenAPIToken
	var err error
	if b.openAPIRefresh != "" {
		tok, err = qauth.RefreshOpenAPIToken(ctx, b.openAPIRefresh, b.Region)
		if err != nil {
			log.Printf("[qoder-bridge] openapi token refresh failed (%v); re-exchanging PAT", err)
			tok = nil
		}
	}
	if tok == nil {
		tok, err = qauth.ExchangeOpenAPIToken(ctx, b.PAT, b.Region)
		if err != nil {
			return "", err
		}
	}
	b.openAPIToken = tok.Token
	b.openAPIRefresh = tok.RefreshToken
	b.openAPIExpMs = tok.ExpiresAtMs
	return b.openAPIToken, nil
}

// invalidateOpenAPIBearer drops the cached OpenAPI bearer so the next
// quotaBearer call re-exchanges it. Used after a 401 from a region that
// requires the OpenAPI token: without this, quotaBearer would keep returning
// the rejected token until its reported expiry.
func (b *Bridge) invalidateOpenAPIBearer() {
	b.openAPIMu.Lock()
	b.openAPIToken = ""
	b.openAPIRefresh = ""
	b.openAPIExpMs = 0
	b.openAPIMu.Unlock()
}

// doRenew renews the session token via refreshToken. When force is true and
// the refresh is rejected, it falls back to a fresh PAT exchange.
func (b *Bridge) doRenew(ctx context.Context, force bool) error {
	b.mu.Lock()
	rt := b.refreshToken
	sot := b.securityOath
	b.mu.Unlock()

	jt, err := qauth.RefreshJobToken(ctx, b.PAT, rt, sot, b.machineID, b.machineToken, b.machineType, b.Region)
	if err != nil {
		if !force {
			return err
		}
		log.Printf("[qoder-bridge] refresh rejected (%v); falling back to PAT exchange", err)
		jt, err = qauth.ExchangeJobToken(ctx, b.PAT, b.machineID, b.machineToken, b.machineType, b.Region)
		if err != nil {
			return err
		}
		log.Printf("[qoder-bridge] PAT re-exchange ok (exp=%v)", jt["expireTime"])
	} else {
		label := "refreshed"
		if force {
			label = "force-refreshed"
		}
		log.Printf("[qoder-bridge] session %s (exp=%v)", label, jt["expireTime"])
	}
	id, _ := jt["id"].(string)
	b.applyJobToken(jt, b.resolveAccountStatus(ctx, id))
	return nil
}

// EnsureFreshSession proactively rotates the session token before expiry.
func (b *Bridge) EnsureFreshSession(ctx context.Context) error {
	if b.bootstrapped.Load() && !b.needsRefresh() {
		return nil
	}
	b.refreshMu.Lock()
	defer b.refreshMu.Unlock()
	if !b.bootstrapped.Load() {
		return b.bootstrapSession(ctx)
	}
	if !b.needsRefresh() {
		return nil
	}
	return b.doRenew(ctx, false)
}

// ForceRefresh forces a session rotation. Used reactively when an auth error
// is seen on a request before any content was produced.
func (b *Bridge) ForceRefresh(ctx context.Context) error {
	b.refreshMu.Lock()
	defer b.refreshMu.Unlock()
	return b.doRenew(ctx, true)
}

// CatalogModels returns the model/list entries as the []map[string]any shape
// cli2api's ModelInfos expects, with a TTL cache and single-flight. The raw
// response is also available via CatalogRaw for callers that need the
// qoder2api-style ExtractCatalog form.
func (b *Bridge) CatalogModels(ctx context.Context) ([]map[string]any, error) {
	if cached, ok := b.lookupCatalog(); ok {
		return cached, nil
	}
	b.catalogMu.Lock()
	for b.catalogInflt {
		b.catalogMu.Unlock()
		time.Sleep(50 * time.Millisecond)
		if cached, ok := b.lookupCatalog(); ok {
			return cached, nil
		}
		b.catalogMu.Lock()
	}
	b.catalogInflt = true
	b.catalogMu.Unlock()
	defer func() {
		b.catalogMu.Lock()
		b.catalogInflt = false
		b.catalogMu.Unlock()
	}()

	if err := b.EnsureFreshSession(ctx); err != nil {
		log.Printf("[qoder-bridge] WARN ensure_fresh_session failed (%v); catalog unavailable", err)
		return nil, err
	}
	raw, err := qauth.FetchModelCatalog(ctx, b.currentSess(), b.Region)
	if err != nil {
		log.Printf("[qoder-bridge] WARN model/list failed (%v)", err)
		return nil, err
	}
	entries := extractModelEntries(raw)
	b.catalogMu.Lock()
	b.catalogRaw = raw
	b.catalog = entries
	b.catalogTs = time.Now().Unix()
	b.catalogMu.Unlock()
	return entries, nil
}

// CatalogRaw returns the cached raw model/list response, fetching it if stale
// or missing. Used by callers that need the qoder2api-style ExtractCatalog
// form (display_name → key map) rather than the entry array.
func (b *Bridge) CatalogRaw(ctx context.Context) (map[string]any, error) {
	if _, err := b.CatalogModels(ctx); err != nil {
		return nil, err
	}
	b.catalogMu.Lock()
	defer b.catalogMu.Unlock()
	return b.catalogRaw, nil
}

func (b *Bridge) lookupCatalog() ([]map[string]any, bool) {
	b.catalogMu.Lock()
	defer b.catalogMu.Unlock()
	if b.catalog == nil {
		return nil, false
	}
	if time.Since(time.Unix(b.catalogTs, 0)) > catalogTTL {
		return nil, false
	}
	return b.catalog, true
}

// extractModelEntries pulls the model entry array out of a /model/list
// response. The gateway wraps the per-scene lists under a scene key ("chat");
// the worker used to surface the array as {"data":[...]} over HTTP, so this
// bridges the same shape for ModelInfos.
func extractModelEntries(raw map[string]any) []map[string]any {
	if raw == nil {
		return nil
	}
	scene, ok := raw["chat"].([]any)
	if !ok {
		return nil
	}
	out := make([]map[string]any, 0, len(scene))
	for _, item := range scene {
		if m, ok := item.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

// MachineID / MachineToken / MachineType expose the derived device identity
// for callers (e.g. the check-in path) that need to send the same headers
// outside the signed session.
func (b *Bridge) MachineID() string    { return b.machineID }
func (b *Bridge) MachineToken() string { return b.machineToken }
func (b *Bridge) MachineType() string  { return b.machineType }

// CurrentSecurityOauth exposes the live session token for the check-in
// path, which authenticates to OpenAPI with a Bearer securityOauthToken
// rather than the COSY-signed session.
func (b *Bridge) CurrentSecurityOauth() string { return b.currentSecurityOauth() }

// Region returns the bridge's region config.
func (b *Bridge) RegionConfig() *qauth.RegionConfig { return b.Region }

// QuotaSnapshot converts a cached AccountStatus into the cli2api store
// shape. Returns nil when no quota has been resolved yet.
//
// The headline mirrors the upstream client's usage view: the plan quota,
// add-on pack, and organization resource package are summed into one
// aggregate (see quota.go's snapshot for the worker-path equivalent, issue
// #229). The check-in reward lands in the add-on bucket, so a base-only
// headline never moves after a successful check-in.
func (b *Bridge) QuotaSnapshot() *accounts.QuotaSnapshot {
	st := b.AccountStatus()
	if st == nil || st.UserQuota == nil {
		return nil
	}
	buckets := []*qauth.Quota{st.UserQuota, st.AddOnQuota}
	if st.OrgResourcePackage != nil {
		buckets = append(buckets, &qauth.Quota{
			Total:      st.OrgResourcePackage.Cap,
			Used:       st.OrgResourcePackage.Used,
			Remaining:  st.OrgResourcePackage.Remaining,
			Percentage: st.OrgResourcePackage.Percentage,
			Unit:       st.OrgResourcePackage.Unit,
		})
	}
	var used, total, remaining float64
	unit := ""
	for _, q := range buckets {
		if q == nil {
			continue
		}
		used += q.Used
		total += q.Total
		remaining += q.Remaining
		if unit == "" && q.Unit != "" {
			unit = q.Unit
		}
	}
	snap := &accounts.QuotaSnapshot{
		Used:       used,
		Total:      total,
		Remaining:  remaining,
		Percentage: 0,
		Unit:       unit,
		Exceeded:   st.IsQuotaExceeded,
		FetchedAt:  time.Now().UTC().Format(time.RFC3339),
		Plan:       st.Plan,
	}
	if total > 0 {
		snap.Percentage = math.Min(used/total*100, 100)
	}
	if st.NextResetAtMs > 0 {
		snap.ExpiresAt = st.NextResetAtMs / 1000
	}
	// Exceeded keeps its previous meaning: the account is out of credits unless
	// an add-on or organization package still holds some.
	if snap.Exceeded && remaining > 0 {
		snap.Exceeded = false
	}
	if st.AddOnQuota != nil {
		snap.HasAddOn = true
		snap.AddOnUsed = st.AddOnQuota.Used
		snap.AddOnTotal = st.AddOnQuota.Total
		snap.AddOnRemaining = st.AddOnQuota.Remaining
		snap.AddOnUnit = st.AddOnQuota.Unit
	}
	if st.OrgResourcePackage != nil {
		snap.HasResourcePackage = true
		snap.ResourcePackageUsed = st.OrgResourcePackage.Used
		snap.ResourcePackageTotal = st.OrgResourcePackage.Cap
		snap.ResourcePackageRemaining = st.OrgResourcePackage.Remaining
		snap.ResourcePackageUnit = st.OrgResourcePackage.Unit
		avail := st.OrgResourcePackage.Available
		snap.ResourcePackageAvailable = &avail
	}
	return snap
}

// fmtResetTime renders an epoch-millis reset instant for logs, or "unknown".
func fmtResetTime(ms int64) string {
	if ms <= 0 {
		return "unknown"
	}
	return time.UnixMilli(ms).Local().Format("2006-01-02 15:04 MST")
}

// ensure unused imports stay referenced for future checkin/catalog callers.
var _ = strings.TrimSpace
