package runtime

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/caigee-cmd/cli2api/internal/accounts"
	"github.com/caigee-cmd/cli2api/internal/events"
	"github.com/caigee-cmd/cli2api/internal/providers"
	"github.com/caigee-cmd/cli2api/internal/providers/qoder"
)

// newOpID identifies one bulk operation so the console can correlate its
// per-account progress events.
func newOpID() string {
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return fmt.Sprintf("op-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(buf[:])
}

// Health/probe scheduling. Quota fetch failures must not flip Ready.
// Reads pool items and writes runtime/health via existing Merge/Set helpers.

func (m *Manager) RefreshAccount(ctx context.Context, id string, forceQuota bool) error {
	if m == nil || m.pool == nil {
		return fmt.Errorf("account manager not ready")
	}
	var err error
	provider, region := "", ""
	if item, ok := m.pool.ByID(id); ok {
		provider, region = item.Provider, item.Region
		err = m.refreshOne(ctx, item, forceQuota)
	} else {
		// Disabled accounts are deliberately kept out of the routing pool, so they
		// have no pool item to refresh. Refresh their health and quota straight
		// through the provider adapter instead of failing closed: the operator may
		// still want to inspect a disabled account's live status/credits. Nothing
		// is written back into the pool, so the account stays out of routing.
		account, getErr := m.store.Get(ctx, id)
		if getErr != nil {
			return getErr
		}
		if account.Enabled {
			return fmt.Errorf("account %s is not running", id)
		}
		provider, region = account.Provider, account.ProviderRegion
		err = m.refreshDetached(ctx, account, forceQuota)
	}
	m.events.Publish(events.TypeRefresh, id, "", m.refreshResult(ctx, id, provider, region, err))
	return err
}

// DetachedRefreshError reports that a disabled account cannot be refreshed. It
// is returned when the provider has no in-process prober at all, or when the
// probe reports not-ready without an error (for example a legacy Qoder worker
// account that predates the PAT model and has no live worker). Such an account
// must be enabled first so a worker can be spawned before its status/quota can
// be read.
type DetachedRefreshError struct{ Provider string }

func (e *DetachedRefreshError) Error() string {
	provider := strings.TrimSpace(e.Provider)
	if provider == "" {
		provider = "this provider"
	}
	return fmt.Sprintf("account provider %q cannot be refreshed while disabled; enable it first", provider)
}

// refreshDetached refreshes one account that has no pool item (a disabled
// account). It probes the provider adapter directly and persists health/quota
// to the store, but never adds the account back into the routing pool.
//
// The probe must never change the account's status: a disabled account stays
// disabled even when its provider answers ready, because it is still absent
// from the routing pool. Only the health detail and quota are refreshed so the
// operator can inspect a disabled account's live credits.
func (m *Manager) refreshDetached(ctx context.Context, account Account, forceQuota bool) error {
	if m.providers == nil {
		return &DetachedRefreshError{Provider: account.Provider}
	}
	adapter, ok := m.providers.Get(account.Provider)
	if !ok || adapter.Prober == nil {
		return &DetachedRefreshError{Provider: account.Provider}
	}
	health, err := adapter.Prober.Probe(ctx, account.ID)
	if err != nil {
		// Persist the probe error for display, but never promote the disabled
		// account to error/login_required: a transient outage or a dead
		// credential on an account the operator deliberately disabled must not
		// make the card look like a routable account that needs attention.
		kind := KindUnavailable
		if qoder.IsAuthError(err) {
			kind = KindAuth
		}
		_ = m.store.ObserveHealth(ctx, account.ID, "", err.Error(), kind)
		return fmt.Errorf("probe account %s: %w", account.ID, err)
	}
	kind := ""
	if health.LastError != "" && adapter.Classifier != nil {
		if classified := adapter.Classifier.Classify(0, health.LastError); classified.Kind != "" {
			kind = classified.Kind
		}
	}
	if err := m.store.ObserveHealth(ctx, account.ID, health.UID, health.LastError, kind); err != nil {
		return err
	}
	if !health.Ready && !health.Hot {
		// A provider that reports not-ready without an error (for example a
		// legacy Qoder worker account with no PAT and no live worker) gives us
		// nothing to refresh: there is no in-process prober to spawn the
		// missing worker while the account is disabled. Surface the typed error
		// so the console tells the operator to enable the account first instead
		// of silently reporting a successful refresh.
		if health.LastError == "" {
			return &DetachedRefreshError{Provider: account.Provider}
		}
		return nil
	}
	// Quota is fetched after readiness so an upstream quota outage never flips
	// routing state. A manual "refresh credits" (forceQuota) must complete
	// before returning so the console row carries the fresh number.
	if forceQuota {
		quotaCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
		if qerr := m.fetchProviderQuotaErr(quotaCtx, account.ID, adapter.Prober); qerr != nil {
			return qerr
		}
	} else {
		go func(accountID string, prober providers.AccountProber) {
			quotaCtx, cancel := context.WithTimeout(m.runCtx, 5*time.Second)
			defer cancel()
			m.fetchProviderQuota(quotaCtx, accountID, prober)
		}(account.ID, adapter.Prober)
	}
	return nil
}

// AccountView returns the same projection the accounts list builds, for one
// RefreshAll refreshes every routable account concurrently (full fan-out, no
// inter-account queue) and joins the non-quota failures. Quota refresh
// failures stay best-effort for background maintenance; the per-account
// detail is available through RefreshAllResults.
func (m *Manager) RefreshAll(ctx context.Context, forceQuota bool) error {
	var joined error
	for _, result := range m.RefreshAllResults(ctx, forceQuota, false) {
		if result.OK || result.Code == "quota_refresh_failed" {
			continue
		}
		joined = errors.Join(joined, fmt.Errorf("account %s: %s", result.AccountID, result.Message))
	}
	return joined
}

// RefreshAllResults refreshes every account concurrently and reports each
// account's real outcome. All accounts start at once: there is no semaphore
// and no inter-account gap; per-account single-flight gates (quota fetch,
// check-in running) still deduplicate overlapping work. When includeDisabled
// is set, disabled accounts are refreshed through the detached probe path in
// the same fan-out without re-entering the routing pool.
func (m *Manager) RefreshAllResults(ctx context.Context, forceQuota, includeDisabled bool) []accounts.AccountRefreshResult {
	if m == nil || m.pool == nil {
		return nil
	}
	items := m.pool.Items()
	var detached []Account
	if includeDisabled {
		if stored, err := m.store.List(ctx); err == nil {
			for _, account := range stored {
				if !account.Enabled {
					detached = append(detached, account)
				}
			}
		}
	}
	results := make([]accounts.AccountRefreshResult, len(items)+len(detached))
	opID := newOpID()
	total := len(results)
	var done atomic.Int64
	m.events.Publish(events.TypeBulk, "", opID, events.BulkProgress{OpID: opID, Kind: "refresh", Phase: "start", Total: total})
	publish := func(result accounts.AccountRefreshResult) {
		m.events.Publish(events.TypeRefresh, result.AccountID, opID, result)
		m.events.Publish(events.TypeBulk, result.AccountID, opID, events.BulkProgress{
			OpID: opID, Kind: "refresh", Phase: "account", Total: total,
			Done: int(done.Add(1)), AccountID: result.AccountID, Result: result,
		})
	}
	var wg sync.WaitGroup
	for i, item := range items {
		wg.Add(1)
		go func(index int, item Item) {
			defer wg.Done()
			err := m.refreshOne(ctx, item, forceQuota)
			results[index] = m.refreshResult(ctx, item.ID, item.Provider, item.Region, err)
			publish(results[index])
		}(i, item)
	}
	for i, account := range detached {
		wg.Add(1)
		go func(index int, account Account) {
			defer wg.Done()
			err := m.refreshDetached(ctx, account, forceQuota)
			results[len(items)+index] = m.refreshResult(ctx, account.ID, account.Provider, account.ProviderRegion, err)
			publish(results[len(items)+index])
		}(i, account)
	}
	wg.Wait()
	m.events.Publish(events.TypeBulk, "", opID, events.BulkProgress{OpID: opID, Kind: "refresh", Phase: "done", Total: total, Done: total})
	return results
}

// RefreshAllAsync triggers a full concurrent refresh in the background bound
// to the manager lifecycle. Per-account outcomes are persisted (status,
// last_error, quota) and published on the event bus, so no error is swallowed.
func (m *Manager) RefreshAllAsync(ctx context.Context, forceQuota, includeDisabled bool) {
	if m == nil {
		return
	}
	go func() {
		bg, cancel := context.WithTimeout(context.WithoutCancel(ctx), 90*time.Second)
		defer cancel()
		stop := context.AfterFunc(m.runCtx, cancel)
		defer stop()
		m.RefreshAllResults(bg, forceQuota, includeDisabled)
	}()
}

// refreshResult projects the real post-refresh state of one account: the
// persisted status/error from the store and the live quota snapshot from the
// pool (or the store for detached accounts). The probe error is carried
// verbatim; typed provider errors contribute their real code/kind/status.
func (m *Manager) refreshResult(ctx context.Context, accountID, provider, region string, err error) accounts.AccountRefreshResult {
	result := accounts.AccountRefreshResult{
		AccountID: accountID,
		Provider:  provider,
		Region:    region,
		OK:        err == nil,
	}
	if err != nil {
		result.Message = err.Error()
		var quotaErr *QuotaRefreshError
		if errors.As(err, &quotaErr) {
			result.Code = "quota_refresh_failed"
		}
		var providerErr *providers.Error
		if errors.As(err, &providerErr) && providerErr != nil {
			if result.Code == "" {
				result.Code = providerErr.Code
			}
			result.Kind = providerErr.Kind
			result.HTTPStatus = providerErr.Status
		}
	}
	readCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if stored, getErr := m.store.Get(readCtx, accountID); getErr == nil {
		result.Status = stored.Status
		if result.Message == "" {
			result.Message = stored.LastError
		}
		if result.Kind == "" {
			result.Kind = stored.LastErrorKind
		}
		if result.Provider == "" {
			result.Provider = stored.Provider
		}
		if result.Region == "" {
			result.Region = stored.ProviderRegion
		}
		if stored.Quota != nil {
			result.Quota = stored.Quota
		}
	}
	if item, ok := m.pool.ByID(accountID); ok && item.Quota != nil {
		result.Quota = item.Quota
	}
	return result
}

func (m *Manager) refreshOne(ctx context.Context, item Item, forceQuota bool) error {
	if item.RuntimeState == "dead" {
		return nil
	}
	if item.Runtime == string(providers.RuntimeInProcess) || strings.TrimSpace(item.URL) == "" {
		return m.refreshInProcess(ctx, item, forceQuota)
	}
	client := qoder.WorkerClient{HTTP: m.httpClient}
	health, statusCode, err := client.Health(ctx, item.URL)
	if err != nil {
		if statusCode == 0 {
			ready := false
			hot := false
			m.pool.MergeHealth(item.ID, ready, hot, 0, item.Restarts, err.Error())
			_ = m.store.Observe(ctx, item.ID, "", "error", err.Error(), KindUnavailable)
			return fmt.Errorf("health account %s: %w", item.ID, err)
		}
		return fmt.Errorf("decode account %s health: %w", item.ID, err)
	}
	ready := statusCode < 300 && health.OK && health.Ready
	m.pool.MergeHealth(item.ID, ready, health.Hot, health.InFlight, item.Restarts, health.LastError)
	if ready || health.Hot {
		m.resetRestartBackoff(item.ID)
	}
	// A worker that answers without an error but is not ready is still warming
	// up (or waiting for a credential the operator has not supplied yet). Only
	// an explicit error justifies a harder status; defaulting to login_required
	// here made a starting account look like one that needs a re-login.
	status := "starting"
	if ready || health.Hot {
		status = "ready"
	} else if health.LastError != "" {
		status = "error"
	}
	if err := m.store.Observe(ctx, item.ID, health.UID, status, health.LastError, ""); err != nil {
		return err
	}
	// Fetch quota after the health/observe path so a quota endpoint outage
	// never changes readiness. A successful exhausted snapshot may still
	// keep the account out of request routing.
	if health.Hot || ready {
		// Quota and model discovery are independent upstream operations. Run
		// them concurrently so a quota outage cannot prevent catalog refresh.
		quotaDone := make(chan error, 1)
		go func() { quotaDone <- m.fetchQuota(ctx, item.ID, item.URL, forceQuota) }()
		m.fetchAccountModels(ctx, item)
		quotaErr := <-quotaDone
		if quotaErr != nil {
			return quotaErr
		}
	}
	return nil
}

func (m *Manager) refreshInProcess(ctx context.Context, item Item, forceQuota bool) error {
	adapter, ok := m.providers.Get(item.Provider)
	if !ok || adapter.Prober == nil {
		// No prober registered: leave pool state alone and never hit ""+/health.
		// If the adapter provides a catalog, refresh models so pool routing knows them.
		if ok && adapter.Models != nil {
			m.fetchAccountModels(ctx, item)
		}
		return nil
	}
	health, err := adapter.Prober.Probe(ctx, item.ID)
	if err != nil {
		m.pool.MergeHealth(item.ID, false, false, 0, item.Restarts, err.Error())
		// A Qoder gateway 401/403 means the PAT is no longer valid, not a
		// transient outage: mark the account login_required so the card shows
		// "re-login" instead of a generic error.
		status, kind := "error", KindUnavailable
		if qoder.IsAuthError(err) {
			status, kind = "login_required", KindAuth
		}
		_ = m.store.Observe(ctx, item.ID, "", status, err.Error(), kind)
		return fmt.Errorf("probe account %s: %w", item.ID, err)
	}
	m.pool.MergeHealth(item.ID, health.Ready, health.Hot, health.InFlight, item.Restarts, health.LastError)
	if health.Ready || health.Hot {
		m.resetRestartBackoff(item.ID)
	}
	// Same rule as the worker path: not-ready without an error is "starting",
	// not "login_required". An auth-classified probe error still escalates below.
	status := "starting"
	kind := ""
	if health.Ready || health.Hot {
		status = "ready"
	} else if health.LastError != "" {
		status = "error"
		if adapter.Classifier != nil {
			classified := adapter.Classifier.Classify(0, health.LastError)
			if classified.Kind != "" {
				kind = classified.Kind
			}
			if classified.Kind == KindAuth {
				status = "login_required"
			}
		}
	}
	if err := m.store.Observe(ctx, item.ID, health.UID, status, health.LastError, kind); err != nil {
		return err
	}
	if !health.Ready && !health.Hot {
		return nil
	}
	m.fetchAccountModels(ctx, item)
	// Quota is fetched after readiness so an upstream quota outage never flips
	// Ready. A manual "refresh credits" (forceQuota) must hit upstream and
	// complete before returning, so the console row it replaces carries the
	// fresh number; the background/manual-list path stays async to keep latency
	// off the list endpoint.
	if forceQuota {
		quotaCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
		if qerr := m.fetchProviderQuotaErr(quotaCtx, item.ID, adapter.Prober); qerr != nil {
			return qerr
		}
	} else {
		go func(accountID string, prober providers.AccountProber) {
			quotaCtx, cancel := context.WithTimeout(m.runCtx, 5*time.Second)
			defer cancel()
			m.fetchProviderQuota(quotaCtx, accountID, prober)
		}(item.ID, adapter.Prober)
	}
	return nil
}

func (m *Manager) forceReady(accountID string) bool {
	if m == nil || strings.TrimSpace(accountID) == "" || strings.TrimSpace(m.config.DataDir) == "" {
		return false
	}
	_, err := os.Stat(filepath.Join(m.config.DataDir, "force-ready", accountID))
	return err == nil
}

// probeHealthOnly issues a single GET /health to a worker account with a tight
// 2s deadline. It is the liveness probe used by the maintenance-loop watchdog:
// unlike refreshOne, it does not fetch quota or refresh the model catalog, so a
// 30s-loop call costs one HTTP round-trip, not a quota probe. The pool state
// machine is left to refreshOne and the manual console refresh path; this only
// decides whether the worker process is still alive enough to serve at all.
//
// "Alive" is deliberately loose: a transport error or a non-2xx / non-ok status
// means the process is wedged or gone and worth rebooting, but a 200 with
// ready=false is NOT a failure. A worker that is still starting, rewarming, or
// waiting for login answers /health with ok=true, ready=false — killing it after
// ~90s would loop a healthy-but-warming account. readiness stays refreshOne's
// job; only the daemon being unreachable or unhealthy counts here.
//
// In-process accounts (WorkBuddy / Trae / Devin) have no child-process /health
// endpoint and never need this check — their liveness is the adapter's own
// transport, which the executor already classifies. probeHealthOnly returns
// (true, nil) for them so the watchdog skips them.
func (m *Manager) probeHealthOnly(ctx context.Context, item Item) (bool, error) {
	if item.Runtime == string(providers.RuntimeInProcess) || strings.TrimSpace(item.URL) == "" {
		return true, nil
	}
	probeCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	client := qoder.WorkerClient{HTTP: m.httpClient}
	health, statusCode, err := client.Health(probeCtx, item.URL)
	if err != nil {
		return false, err
	}
	if statusCode >= 300 || !health.OK {
		return false, nil
	}
	return true, nil
}
