package runtime

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"log"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	_ "time/tzdata"

	"github.com/caigee-cmd/cli2api/internal/accounts"
	"github.com/caigee-cmd/cli2api/internal/events"
	"github.com/caigee-cmd/cli2api/internal/providers"
	"github.com/caigee-cmd/cli2api/internal/providers/qoder"
)

func (manager *Manager) CheckinAccount(ctx context.Context, accountID string) (Account, error) {
	outcome, err := manager.checkinAccount(ctx, accountID, false, false)
	return outcome.account, err
}

// checkinOutcome carries the real per-attempt facts a bulk pass reports:
// the updated account, the provider's verbatim check-in result (including the
// real reward amount), and whether the post-check-in quota fetch persisted.
type checkinOutcome struct {
	account        Account
	result         providers.CheckinResult
	quotaRefreshed bool
}

// CheckinAll runs a manual all-account pass on the server. It selects from the
// full store instead of the browser's current page/filter, honors the system
// "check in disabled accounts" setting, and reports every account outcome.
// All target accounts start concurrently (full fan-out): there is no
// concurrency cap and no inter-account gap. The per-account checkinRunning
// gate still deduplicates an overlapping manual/scheduled attempt.
func (manager *Manager) CheckinAll(ctx context.Context, force, includeDisabled bool) ([]accounts.CheckinAllResult, error) {
	if manager == nil {
		return nil, fmt.Errorf("account manager unavailable")
	}
	items, err := manager.store.List(ctx)
	if err != nil {
		return nil, err
	}
	targets := make([]accounts.Account, 0, len(items))
	for _, item := range items {
		if !includeDisabled && !item.Enabled {
			continue
		}
		if _, supported := providers.CheckinFor(item.Provider, item.ProviderRegion); !supported {
			continue
		}
		if adapter, registered := manager.providers.Get(item.Provider); !registered || adapter.Checkin == nil {
			continue
		}
		targets = append(targets, item)
	}
	results := make([]accounts.CheckinAllResult, len(targets))
	opID := newOpID()
	total := len(targets)
	var done atomic.Int64
	manager.events.Publish(events.TypeBulk, "", opID, events.BulkProgress{OpID: opID, Kind: "checkin", Phase: "start", Total: total})
	var wg sync.WaitGroup
	for i, item := range targets {
		if ctx.Err() != nil {
			// Report the accounts that never started as unknown, never success.
			for j := i; j < len(targets); j++ {
				results[j] = accounts.CheckinAllResult{
					AccountID: targets[j].ID,
					Provider:  targets[j].Provider,
					Region:    targets[j].ProviderRegion,
					Status:    "unknown",
					Message:   ctx.Err().Error(),
				}
			}
			break
		}
		wg.Add(1)
		go func(index int, account accounts.Account) {
			defer wg.Done()
			outcome, checkErr := manager.checkinAccount(ctx, account.ID, includeDisabled, force)
			updated := outcome.account
			result := accounts.CheckinAllResult{
				AccountID:      account.ID,
				Provider:       account.Provider,
				Region:         account.ProviderRegion,
				Status:         updated.LastCheckinStatus,
				Message:        updated.LastCheckinMsg,
				RewardCredits:  outcome.result.RewardCredits,
				QuotaRefreshed: outcome.quotaRefreshed,
			}
			if result.Status == "" {
				// No recorded status is not evidence of success.
				result.Status = "unknown"
				if result.Message == "" {
					result.Message = "no check-in status recorded"
				}
			}
			if checkErr != nil {
				result.Status = "error"
				if result.Message == "" {
					result.Message = checkErr.Error()
				}
				var providerErr *providers.Error
				if errors.As(checkErr, &providerErr) && providerErr != nil {
					result.Code = providerErr.Code
					result.Kind = providerErr.Kind
					result.HTTPStatus = providerErr.Status
				}
			}
			results[index] = result
			manager.events.Publish(events.TypeBulk, account.ID, opID, events.BulkProgress{
				OpID: opID, Kind: "checkin", Phase: "account", Total: total,
				Done: int(done.Add(1)), AccountID: account.ID, Result: result,
			})
		}(i, item)
	}
	wg.Wait()
	manager.events.Publish(events.TypeBulk, "", opID, events.BulkProgress{OpID: opID, Kind: "checkin", Phase: "done", Total: total, Done: int(done.Load())})
	if ctx.Err() != nil {
		return results, ctx.Err()
	}
	// Verify the claims against upstream after the pass; this refreshes the
	// true today flag and the derived auto schedule without delaying the UI.
	syncCtx := context.WithoutCancel(ctx)
	go manager.SyncCheckinStatuses(syncCtx)
	return results, nil
}

func (manager *Manager) checkinAccount(ctx context.Context, accountID string, allowDisabled, force bool) (checkinOutcome, error) {
	if manager == nil {
		return checkinOutcome{}, fmt.Errorf("account manager unavailable")
	}
	manager.mu.Lock()
	if manager.checkinRunning == nil {
		manager.checkinRunning = make(map[string]bool)
	}
	if manager.checkinRunning[accountID] {
		manager.mu.Unlock()
		return checkinOutcome{}, fmt.Errorf("check-in is already running for this account")
	}
	manager.checkinRunning[accountID] = true
	manager.mu.Unlock()
	defer func() {
		manager.mu.Lock()
		delete(manager.checkinRunning, accountID)
		manager.mu.Unlock()
	}()
	account, err := manager.store.Get(ctx, accountID)
	if err != nil {
		return checkinOutcome{}, err
	}
	if !account.Enabled && !allowDisabled {
		return checkinOutcome{account: account}, fmt.Errorf("account is disabled")
	}
	policy, supported := providers.CheckinFor(account.Provider, account.ProviderRegion)
	adapter, registered := manager.providers.Get(account.Provider)
	if !supported || !registered || adapter.Checkin == nil {
		return checkinOutcome{account: account}, providers.ErrUnsupported
	}
	location, err := time.LoadLocation(policy.Timezone)
	if err != nil {
		return checkinOutcome{account: account}, fmt.Errorf("invalid check-in timezone: %w", err)
	}
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	stop := context.AfterFunc(manager.runCtx, cancel)
	defer stop()
	if !force && CheckedInLocalDay(account.LastCheckinAt, account.LastCheckinStatus, time.Now().In(location)) {
		if account.Provider == "workbuddy" {
			if err := manager.clearCheckinCooldown(ctx, accountID); err != nil {
				return checkinOutcome{account: account}, fmt.Errorf("clear cooldown after check-in: %w", err)
			}
		}
		refreshed := manager.refreshCheckinQuota(ctx, accountID, adapter)
		updated, getErr := manager.store.Get(ctx, accountID)
		return checkinOutcome{
			account:        updated,
			result:         providers.CheckinResult{Status: "already", Message: account.LastCheckinMsg},
			quotaRefreshed: refreshed,
		}, getErr
	}
	result, checkErr := adapter.Checkin.Checkin(ctx, accountID)
	if checkErr == nil && !result.Valid() {
		checkErr = fmt.Errorf("provider returned an invalid check-in result")
	}
	if checkErr != nil {
		result = providers.CheckinResult{Status: "error", Message: checkErr.Error()}
	}
	recordCtx, stopRecording := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	recordErr := manager.store.RecordCheckin(recordCtx, accountID, result.Status, result.Message, time.Now().UTC())
	stopRecording()
	if recordErr != nil {
		return checkinOutcome{account: account, result: result}, errors.Join(checkErr, fmt.Errorf("save check-in result: %w", recordErr))
	}
	manager.events.Publish(events.TypeCheckin, accountID, "", events.CheckinState{
		AccountID:     accountID,
		Status:        result.Status,
		Message:       result.Message,
		RewardCredits: result.RewardCredits,
		At:            time.Now().UTC().Format(time.RFC3339),
	})
	if account.Provider == "workbuddy" && (result.Status == "success" || result.Status == "already") {
		if err := manager.clearCheckinCooldown(ctx, accountID); err != nil {
			return checkinOutcome{account: account, result: result}, errors.Join(checkErr, fmt.Errorf("clear cooldown after check-in: %w", err))
		}
	}
	quotaRefreshed := false
	if result.Status != "skipped" {
		quotaRefreshed = manager.refreshCheckinQuota(ctx, accountID, adapter)
	}
	readCtx, stopReading := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer stopReading()
	updated, err := manager.store.Get(readCtx, accountID)
	return checkinOutcome{account: updated, result: result, quotaRefreshed: quotaRefreshed}, errors.Join(checkErr, err)
}

const (
	// checkinStatusInterval is how often the console refreshes the true
	// upstream check-in state. Verification is read-only and bounded; it never
	// claims a reward.
	checkinStatusInterval = 15 * time.Minute
	checkinStatusTimeout  = 75 * time.Second
	// A real check-in can take a few seconds to become visible upstream. Do not
	// downgrade a local success inside this grace window.
	checkinStatusGrace = 15 * time.Minute
)

// SyncCheckinStatuses refreshes every check-in-capable account from upstream.
// It is safe to call concurrently; only one pass runs at a time.
func (manager *Manager) SyncCheckinStatuses(ctx context.Context) {
	if manager == nil {
		return
	}
	manager.mu.Lock()
	if manager.checkinStatusRunning {
		manager.mu.Unlock()
		return
	}
	manager.checkinStatusRunning = true
	manager.mu.Unlock()
	defer func() {
		manager.mu.Lock()
		manager.checkinStatusRunning = false
		manager.mu.Unlock()
	}()
	items, err := manager.store.List(ctx)
	if err != nil {
		log.Printf("checkin status list: %v", err)
		return
	}
	candidates := make([]Account, 0, len(items))
	for _, account := range items {
		policy, supported := providers.CheckinFor(account.Provider, account.ProviderRegion)
		adapter, registered := manager.providers.Get(account.Provider)
		if !supported || !registered || adapter.CheckinStatus == nil {
			continue
		}
		if policy.Timezone == "" {
			continue
		}
		candidates = append(candidates, account)
	}
	var wg sync.WaitGroup
	for _, account := range candidates {
		if ctx.Err() != nil {
			break
		}
		wg.Add(1)
		go func(account Account) {
			defer wg.Done()
			manager.syncCheckinStatus(ctx, account)
		}(account)
	}
	wg.Wait()
}

func (manager *Manager) syncCheckinStatus(ctx context.Context, account Account) {
	manager.mu.Lock()
	busy := manager.checkinRunning[account.ID]
	manager.mu.Unlock()
	if busy {
		return
	}
	adapter, ok := manager.providers.Get(account.Provider)
	if !ok || adapter.CheckinStatus == nil {
		return
	}
	policy, ok := providers.CheckinFor(account.Provider, account.ProviderRegion)
	if !ok {
		return
	}
	location, err := time.LoadLocation(policy.Timezone)
	if err != nil {
		return
	}
	syncCtx, cancel := context.WithTimeout(ctx, checkinStatusTimeout)
	defer cancel()
	stop := context.AfterFunc(manager.runCtx, cancel)
	defer stop()
	result, readErr := adapter.CheckinStatus.CheckinStatus(syncCtx, account.ID)
	now := time.Now().UTC()
	// A manual/scheduled check-in can finish while the read-only probe is in
	// flight. Re-read the account before deciding, so the slower probe cannot
	// overwrite a fresh success with the state that preceded it.
	current, currentErr := manager.store.Get(ctx, account.ID)
	if currentErr != nil {
		log.Printf("checkin status account=%s refresh: %v", account.ID, currentErr)
		return
	}
	account = current
	manager.mu.Lock()
	busy = manager.checkinRunning[account.ID]
	manager.mu.Unlock()
	if busy {
		return
	}
	update := accounts.CheckinStatusUpdate{
		Source:   "upstream",
		SyncedAt: now,
	}
	// The provider check-in time setting is the single source of truth for the
	// automatic schedule and defaults to 09:00 for every provider. Upstream
	// payloads only report status; they must not reschedule the claim. The
	// stored auto time is rewritten on every sync so the console row follows the
	// setting even when another client changed it.
	if value, err := accounts.CheckinTimeDefault(ctx, manager.store, account.Provider); err == nil {
		update.AutoTime = value
	}
	if readErr != nil {
		update.Status = "unknown"
		update.Message = "上游状态同步失败：" + readErr.Error()
	} else {
		switch result.Status {
		case "already":
			update.Status = "already"
			update.Message = strings.TrimSpace(result.Message)
			if update.Message == "" {
				update.Message = "上游显示今日已签到"
			}
			if !CheckedInLocalDay(account.LastCheckinAt, account.LastCheckinStatus, now.In(location)) {
				update.CheckinAt = now.Format(time.RFC3339Nano)
			}
		case "pending":
			update.Status = "pending"
			update.Message = strings.TrimSpace(result.Message)
			if update.Message == "" {
				update.Message = "上游显示今日未签到"
			}
		case "skipped":
			update.Status = "skipped"
			update.Message = strings.TrimSpace(result.Message)
		default:
			update.Status = "unknown"
			update.Message = strings.TrimSpace(result.Message)
			if update.Message == "" {
				update.Message = "上游签到状态未知"
			}
		}
	}
	// An upstream lag right after a real check-in must not erase a fresh local
	// success. An "unknown" answer is not evidence that today's check-in is
	// missing, so a real local success stands for the rest of the provider day;
	// pending/skipped may still replace it after the grace window.
	if update.Status != "already" && CheckedInLocalDay(account.LastCheckinAt, account.LastCheckinStatus, now.In(location)) {
		preserveLocal := update.Status == "unknown"
		if last, err := time.Parse(time.RFC3339Nano, account.LastCheckinAt); err == nil && now.Sub(last) < checkinStatusGrace {
			preserveLocal = true
		}
		if preserveLocal {
			update.Status = account.LastCheckinStatus
			update.Message = account.LastCheckinMsg
			update.CheckinAt = ""
			update.Source = account.LastCheckinSource
			if update.Source == "" {
				update.Source = "local"
			}
		}
	}
	persistCtx, cancelPersist := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancelPersist()
	if err := manager.store.RecordCheckinStatus(persistCtx, account.ID, update); err != nil {
		log.Printf("checkin status account=%s: %v", account.ID, err)
		return
	}
	manager.events.Publish(events.TypeCheckin, account.ID, "", events.CheckinState{
		AccountID: account.ID,
		Status:    update.Status,
		Message:   update.Message,
		At:        now.UTC().Format(time.RFC3339),
	})
}

func (manager *Manager) clearCheckinCooldown(ctx context.Context, accountID string) error {
	if manager.pool != nil {
		if _, ok := manager.pool.ByID(accountID); ok {
			manager.pool.MarkOK(accountID, "")
			manager.Flush()
			return nil
		}
	}

	persistCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if err := manager.poolState.RecordPoolState(persistCtx, accounts.PoolState{ID: accountID}); err != nil {
		return err
	}
	return manager.poolState.SaveCooldowns(persistCtx, accountID, nil)
}

// refreshCheckinQuota refreshes the real upstream balance after a check-in and
// reports whether a fresh snapshot was persisted.
func (manager *Manager) refreshCheckinQuota(ctx context.Context, accountID string, adapter providers.Adapter) bool {
	if ctx.Err() != nil {
		return false
	}
	account, err := manager.store.Get(ctx, accountID)
	if err != nil {
		return false
	}
	// Qoder check-in lands its reward in the OpenAPI add-on bucket. Refresh the
	// real upstream OpenAPI quota in-process explicitly (the same force path the
	// adapter Prober uses) so the grant shows up even if the generic probe ever
	// changes shape.
	if account.Provider == "qoder" {
		if registered, ok := manager.providers.Get(account.Provider); ok {
			if q, ok := registered.Chat.(*qoder.Client); ok {
				return manager.refreshQoderQuota(ctx, accountID, q)
			}
		}
	}
	if adapter.Prober != nil {
		return manager.fetchProviderQuotaErr(ctx, accountID, adapter.Prober) == nil
	}
	if workerURL, found := manager.AccountURL(accountID); found {
		return manager.fetchQuota(ctx, accountID, workerURL, true) == nil
	}
	return false
}

// refreshQoderQuota pulls the real upstream OpenAPI quota (which includes the
// daily check-in add-on grant) and persists it. It never synthesizes a number.
func (manager *Manager) refreshQoderQuota(ctx context.Context, accountID string, client *qoder.Client) bool {
	// quotaSnapshot already refreshes upstream itself; do not take the shared
	// quota-fetch gate here, or a concurrent fetchProviderQuotaErr (manual
	// refresh) would deadlock until this returns.
	snapshot, err := client.QuotaSnapshot(ctx, accountID, true)
	if err != nil || snapshot == nil {
		return false
	}
	persistCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	manager.persistQuota(persistCtx, accountID, snapshot)
	return true
}

func CheckedInLocalDay(at, status string, now time.Time) bool {
	if status != "success" && status != "already" {
		return false
	}
	return recordedToday(at, now)
}

func recordedToday(at string, now time.Time) bool {
	parsed, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(at))
	if err != nil {
		return false
	}
	local := parsed.In(now.Location())
	return local.Year() == now.Year() && local.YearDay() == now.YearDay()
}

// Scheduled check-in / WorkBuddy keepalive fan out every due account in one
// tick concurrently. There is no per-tick cap and no inter-account gap: the
// per-account checkinSlot second-level hash jitter (0..59s inside the
// configured minute) is the schedule itself and naturally spreads starts,
// while per-account single-flight gates deduplicate overlapping attempts.
// Real upstream rate limits surface as genuine 429 classifications and the
// executor cooldown/failover handles them — never hidden by client queuing.
const keepaliveTickTimeout = 90 * time.Second

func checkinSlot(value, accountID string, now time.Time) (time.Time, error) {
	parsed, err := time.Parse("15:04", value)
	if err != nil {
		return time.Time{}, err
	}
	hash := fnv.New32a()
	_, _ = hash.Write([]byte(accountID))
	return time.Date(now.Year(), now.Month(), now.Day(), parsed.Hour(), parsed.Minute(), int(hash.Sum32()%60), 0, now.Location()), nil
}

func checkinDue(account Account, configuredTime string, now time.Time) bool {
	return checkinDueFor(account, configuredTime, now, false)
}

func checkinDueFor(account Account, configuredTime string, now time.Time, allowDisabled bool) bool {
	if (!account.Enabled && !allowDisabled) || !account.AutoCheckin {
		return false
	}
	if recordedToday(account.LastCheckinAt, now) &&
		(account.LastCheckinStatus == "success" || account.LastCheckinStatus == "already") {
		return false
	}
	due, err := checkinSlot(configuredTime, account.ID, now)
	if err != nil || now.Before(due) {
		return false
	}
	last, err := time.Parse(time.RFC3339Nano, account.LastCheckinAt)
	if err != nil || last.Before(due) {
		return true
	}
	// The 01:00 slot can return a provisional "already" while the provider is
	// still settling its 00:00 day. Retry at 02:05 once that window is
	// confirmed, then keep the midday and evening safety nets.
	for _, retryClock := range []string{"02:05", "12:05", "21:00"} {
		retry, err := checkinSlot(retryClock, account.ID, now)
		if err == nil && due.Before(retry) && !now.Before(retry) && last.Before(retry) {
			return true
		}
	}
	return false
}

func (manager *Manager) runScheduledCheckins(ctx context.Context, now time.Time) {
	items, err := manager.store.List(ctx)
	if err != nil {
		log.Printf("checkin schedule list: %v", err)
		return
	}
	allowDisabled := manager.allowDisabledCheckin(ctx)
	var wg sync.WaitGroup
	for _, account := range items {
		if ctx.Err() != nil {
			break
		}
		policy, supported := providers.CheckinFor(account.Provider, account.ProviderRegion)
		adapter, registered := manager.providers.Get(account.Provider)
		if !supported || !registered || adapter.Checkin == nil || (!account.Enabled && !allowDisabled) || !account.AutoCheckin {
			continue
		}
		location, err := time.LoadLocation(policy.Timezone)
		if err != nil {
			log.Printf("checkin timezone provider=%s: %v", account.Provider, err)
			continue
		}
		configuredTime, err := accounts.ResolveCheckinTime(ctx, manager.store, account)
		if err != nil {
			log.Printf("checkin settings account=%s: %v", account.ID, err)
			continue
		}
		if !checkinDueFor(account, configuredTime, now.In(location), allowDisabled) {
			continue
		}
		wg.Add(1)
		go func(account accounts.Account) {
			defer wg.Done()
			if _, err := manager.checkinAccount(ctx, account.ID, allowDisabled, false); err != nil {
				log.Printf("checkin account=%s: %v", account.ID, err)
			}
		}(account)
	}
	wg.Wait()
}

func (manager *Manager) CheckinOptedIn(ctx context.Context) {
	manager.checkinOptedIn(ctx, time.Now(), "", false)
}

func (manager *Manager) checkinOptedIn(ctx context.Context, now time.Time, scheduledTime string, retryDue bool) {
	items, err := manager.store.List(ctx)
	if err != nil {
		log.Printf("checkin list: %v", err)
		return
	}
	allowDisabled := manager.allowDisabledCheckin(ctx)
	var wg sync.WaitGroup
	for _, account := range items {
		if (!account.Enabled && !allowDisabled) || !account.AutoCheckin {
			continue
		}
		configuredTime, err := accounts.ResolveCheckinTime(ctx, manager.store, account)
		if err != nil {
			continue
		}
		if scheduledTime != "" {
			if retryDue {
				if configuredTime == scheduledTime || configuredTime > now.Format("15:04") {
					continue
				}
			} else if configuredTime != scheduledTime {
				continue
			}
		}
		wg.Add(1)
		go func(account accounts.Account) {
			defer wg.Done()
			if _, err := manager.checkinAccount(ctx, account.ID, allowDisabled, false); err != nil {
				log.Printf("checkin account=%s: %v", account.ID, err)
			}
		}(account)
	}
	wg.Wait()
}

func (manager *Manager) allowDisabledCheckin(ctx context.Context) bool {
	value, ok, err := manager.store.GetSecret(ctx, accounts.CheckinDisabledAccountsSecret)
	if err != nil || !ok {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "on", "yes":
		return true
	default:
		return false
	}
}

// KeepaliveWorkBuddy refreshes WorkBuddy sessions. Every eligible account is
// refreshed concurrently in one call (full fan-out, no cap or gap). done
// tracks account IDs already refreshed for the current local day; pass the
// same map across successive maintenance ticks. Returns true only when the
// context ended before every pending account could start.
func (manager *Manager) KeepaliveWorkBuddy(ctx context.Context, onlyOptIn bool, done map[string]struct{}) (more bool) {
	if manager == nil || manager.workbuddy == nil {
		return false
	}
	if done == nil {
		done = map[string]struct{}{}
	}
	items, err := manager.store.List(ctx)
	if err != nil {
		log.Printf("workbuddy keepalive list: %v", err)
		return false
	}
	var pending []string
	for _, account := range items {
		if account.Provider != "workbuddy" || !account.Enabled || (onlyOptIn && !account.AutoCheckin) {
			continue
		}
		if _, ok := done[account.ID]; ok {
			continue
		}
		pending = append(pending, account.ID)
	}
	if len(pending) == 0 {
		return false
	}
	var wg sync.WaitGroup
	for _, accountID := range pending {
		if ctx.Err() != nil {
			return true
		}
		wg.Add(1)
		go func(accountID string) {
			defer wg.Done()
			if err := manager.workbuddy.Keepalive(ctx, accountID); err != nil {
				log.Printf("workbuddy keepalive account=%s: %v", accountID, err)
			}
			manager.mu.Lock()
			done[accountID] = struct{}{}
			manager.mu.Unlock()
		}(accountID)
	}
	wg.Wait()
	return ctx.Err() != nil
}

func (manager *Manager) RunMaintenanceLoop(stop <-chan struct{}) {
	ctx, cancel := context.WithCancel(manager.runCtx)
	defer cancel()
	go func() {
		select {
		case <-stop:
			cancel()
		case <-ctx.Done():
		}
	}()
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	lastKeepaliveDay := ""
	keepaliveDone := map[string]struct{}{}
	keepaliveProgressDay := ""
	// Seed the throttle so the first pass does not duplicate the boot-time
	// RefreshAll: the next stale-quota sweep runs 5 minutes after startup.
	lastStaleQuota := time.Now()
	lastQuotaFresh := time.Now()
	lastCheckinStatusSync := time.Time{}
	// Liveness probes start one tick in so boot-time RefreshAll owns the first
	// /health round-trip and short-lived tests with a stub worker are not probed
	// the instant the loop starts.
	lastLiveness := time.Now()
	for {
		if ctx.Err() != nil {
			return
		}
		now := time.Now()
		if now.Sub(lastCheckinStatusSync) >= checkinStatusInterval {
			lastCheckinStatusSync = now
			go manager.SyncCheckinStatuses(ctx)
		}
		manager.runScheduledCheckins(ctx, now)
		day := now.Format("2006-01-02")
		if day != keepaliveProgressDay {
			keepaliveProgressDay = day
			keepaliveDone = map[string]struct{}{}
		}
		if now.Hour() >= 22 && lastKeepaliveDay != day {
			// Cap + gap per tick; unfinished accounts continue on later ticks
			// instead of one 2-minute burst after 22:00.
			keepaliveCtx, stopKeepalive := context.WithTimeout(ctx, keepaliveTickTimeout)
			more := manager.KeepaliveWorkBuddy(keepaliveCtx, true, keepaliveDone)
			stopKeepalive()
			if !more {
				lastKeepaliveDay = day
			}
		}
		manager.sampleResources()
		if now.Sub(lastLiveness) >= 30*time.Second {
			manager.probeWorkerLiveness(ctx)
			lastLiveness = now
		}
		if now.Sub(lastStaleQuota) >= staleQuotaInterval {
			manager.refreshStaleQuota(ctx)
			lastStaleQuota = now
		}
		if freshInterval := manager.quotaFreshInterval(ctx); freshInterval > 0 && now.Sub(lastQuotaFresh) >= freshInterval {
			lastQuotaFresh = now
			go manager.refreshQuotaFresh(ctx)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// staleQuotaInterval is how often the maintenance loop considers exhausted
// accounts. It is only the sweep cadence: the per-account backoff below
// decides whether a given account is actually re-fetched on that sweep.
const staleQuotaInterval = 5 * time.Minute

// quotaFreshInterval reads the configurable background quota-freshness sweep
// cadence. Zero disables the sweep; unset uses the default.
func (manager *Manager) quotaFreshInterval(ctx context.Context) time.Duration {
	value, ok, err := manager.store.GetSecret(ctx, accounts.QuotaFreshIntervalSecret)
	if err != nil || !ok || strings.TrimSpace(value) == "" {
		return accounts.DefaultQuotaFreshIntervalSeconds * time.Second
	}
	seconds, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil {
		return accounts.DefaultQuotaFreshIntervalSeconds * time.Second
	}
	normalized, err := accounts.NormalizeQuotaFreshIntervalSeconds(seconds)
	if err != nil {
		return accounts.DefaultQuotaFreshIntervalSeconds * time.Second
	}
	return time.Duration(normalized) * time.Second
}

// refreshQuotaFresh re-fetches the real upstream balance of every healthy
// routable account concurrently so console credit readings stay current
// without a manual refresh. Exhausted accounts are left to refreshStaleQuota's
// backoff ladder; the per-account quota-fetch gate deduplicates overlapping
// fetches. Only one sweep runs at a time.
func (manager *Manager) refreshQuotaFresh(ctx context.Context) {
	if manager == nil || manager.pool == nil {
		return
	}
	manager.mu.Lock()
	if manager.quotaFreshRunning {
		manager.mu.Unlock()
		return
	}
	manager.quotaFreshRunning = true
	manager.mu.Unlock()
	defer func() {
		manager.mu.Lock()
		manager.quotaFreshRunning = false
		manager.mu.Unlock()
	}()
	now := time.Now()
	var wg sync.WaitGroup
	for _, item := range manager.pool.Items() {
		if ctx.Err() != nil {
			break
		}
		if item.RuntimeState == "dead" {
			continue
		}
		if manager.quotaNeedsRevalidationFor(item, now) {
			continue
		}
		wg.Add(1)
		go func(item Item) {
			defer wg.Done()
			refreshCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
			defer cancel()
			manager.refreshQuotaOnly(refreshCtx, item)
		}(item)
	}
	wg.Wait()
}

// quotaProbeBackoff is the per-account wait between consecutive quota probes
// while an account keeps reporting Exceeded, indexed by the number of
// consecutive exhausted probes already spent. The first few probes stay at
// 5 minutes so a transient false Exceeded or an out-of-band top-up recovers
// quickly. A genuinely exhausted account then backs off to hours, because
// provider balances reset on a daily cadence and polling it every 5 minutes
// would otherwise burn hundreds of pointless quota calls a day.
var quotaProbeBackoff = []time.Duration{
	5 * time.Minute,  // level 0: first probe
	5 * time.Minute,  // level 1
	5 * time.Minute,  // level 2
	15 * time.Minute, // level 3
	30 * time.Minute, // level 4
	1 * time.Hour,    // level 5
	3 * time.Hour,    // level 6
	6 * time.Hour,    // level 7
	12 * time.Hour,   // level 8
	24 * time.Hour,   // level 9+ (cap: balances reset daily)
}

// quotaProbeDelay returns the wait before the next probe at the given
// consecutive-exhausted level, clamping out-of-range levels to the ends.
func quotaProbeDelay(level int) time.Duration {
	if level < 0 {
		level = 0
	}
	if level >= len(quotaProbeBackoff) {
		return quotaProbeBackoff[len(quotaProbeBackoff)-1]
	}
	return quotaProbeBackoff[level]
}

// quotaSnapshotStale reports whether an Exceeded snapshot is old enough to
// warrant a refresh at the given per-account delay. A zero/empty or
// unparseable FetchedAt means the snapshot predates timestamp tracking, so
// treat it as stale.
func quotaSnapshotStale(q *QuotaSnapshot, now time.Time, delay time.Duration) bool {
	if q == nil {
		return false
	}
	fetchedAt := strings.TrimSpace(q.FetchedAt)
	if fetchedAt == "" {
		return true
	}
	parsed, err := time.Parse(time.RFC3339, fetchedAt)
	if err != nil {
		return true
	}
	return now.Sub(parsed) >= delay
}

// quotaNeedsRevalidation reports whether an account's quota should be
// re-probed: its snapshot says Exceeded, or (Devin) a chat-path daily/weekly
// quota 429 put the whole account in cooldown while the cached snapshot still
// looks healthy. Without the second case the early release in
// clearRecoveredDevinQuota could only happen after a manual refresh
// (CLIProxyAPI b946d54e / #6690).
func quotaNeedsRevalidation(item Item, now time.Time) bool {
	if item.Quota != nil && item.Quota.Exceeded {
		return true
	}
	return item.Provider == "devin" && item.LastKind == accounts.KindQuota &&
		!item.DownUntil.IsZero() && now.Before(item.DownUntil)
}

// quotaNeedsRevalidationFor adds the FEFO-only case: a trustworthy package set
// whose live balance is empty is invisible to routing while its top-level
// Exceeded flag stays false. Without this probe, a newly granted package would
// never re-enter the pool. Unknown scopes or dates never trigger the probe.
func (manager *Manager) quotaNeedsRevalidationFor(item Item, now time.Time) bool {
	if quotaNeedsRevalidation(item, now) {
		return true
	}
	return manager != nil && manager.pool != nil && manager.pool.FEFOQuotaDepleted(item.ID)
}

// refreshStaleQuota re-fetches quota for accounts whose cached snapshot still
// reports Exceeded. A snapshot can go stale when upstream credits are
// replenished out-of-band (billing reset, add-on purchase) or when a flaky
// quota endpoint left an old Exceeded=true in place. Without this, the only
// way to clear a stale exhausted flag is a manual console refresh, so routing
// keeps skipping the account until an operator intervenes. Healthy accounts
// are never touched here; only Exceeded and stale snapshots are refreshed.
//
// Each account carries its own consecutive-exhausted level so an account that
// stays empty for days is probed less and less often instead of every five
// minutes forever. The level resets as soon as a snapshot reports a non-zero
// balance (or the account leaves the pool), so the next genuine exhaustion
// starts the fast ladder over.
func (manager *Manager) refreshStaleQuota(ctx context.Context) {
	if manager == nil || manager.pool == nil {
		return
	}
	now := time.Now()
	seen := make(map[string]struct{})
	for _, item := range manager.pool.Items() {
		if ctx.Err() != nil {
			return
		}
		seen[item.ID] = struct{}{}
		if item.RuntimeState == "dead" {
			continue
		}
		if !manager.quotaNeedsRevalidationFor(item, now) {
			// Recovered (or never exhausted): the next exhaustion is a new
			// episode and starts from the 5-minute probes again.
			manager.resetQuotaProbeLevel(item.ID)
			continue
		}
		level := manager.quotaProbeLevel(item.ID)
		if item.Quota != nil && !quotaSnapshotStale(item.Quota, now, quotaProbeDelay(level)) {
			continue
		}
		before := ""
		if item.Quota != nil {
			before = strings.TrimSpace(item.Quota.FetchedAt)
		}
		refreshCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		manager.refreshQuotaOnly(refreshCtx, item)
		cancel()
		updated, ok := manager.pool.ByID(item.ID)
		fetched := ok && updated.Quota != nil && strings.TrimSpace(updated.Quota.FetchedAt) != before
		switch {
		case !fetched:
			// Probe failed (upstream/network). Keep the level so the next
			// sweep retries at the same cadence instead of escalating a
			// transient outage into an hours-long wait.
		case updated.Quota.Exceeded || manager.pool.FEFOQuotaDepleted(item.ID):
			manager.bumpQuotaProbeLevel(item.ID)
		default:
			manager.resetQuotaProbeLevel(item.ID)
		}
	}
	manager.pruneQuotaProbeLevels(seen)
}

// refreshQuotaOnly fetches quota for one account without a health probe or
// model discovery. It resolves the in-process provider prober when present
// (WorkBuddy/Trae/Devin) and otherwise falls back to the Qoder worker URL,
// mirroring refreshCheckinQuota's dispatch.
func (manager *Manager) refreshQuotaOnly(ctx context.Context, item Item) {
	if adapter, ok := manager.providers.Get(item.Provider); ok && adapter.Prober != nil {
		manager.fetchProviderQuota(ctx, item.ID, adapter.Prober)
		return
	}
	if strings.TrimSpace(item.URL) != "" {
		_ = manager.fetchQuota(ctx, item.ID, item.URL, true)
	}
}

// quotaProbeLevel reads the consecutive-exhausted probe level for an account.
func (manager *Manager) quotaProbeLevel(accountID string) int {
	manager.quotaProbeMu.Lock()
	defer manager.quotaProbeMu.Unlock()
	return manager.quotaProbeLevels[accountID]
}

// bumpQuotaProbeLevel advances the backoff ladder after a probe that still
// reported Exceeded.
func (manager *Manager) bumpQuotaProbeLevel(accountID string) {
	manager.quotaProbeMu.Lock()
	defer manager.quotaProbeMu.Unlock()
	if manager.quotaProbeLevels == nil {
		manager.quotaProbeLevels = map[string]int{}
	}
	manager.quotaProbeLevels[accountID]++
}

// resetQuotaProbeLevel clears the ladder once an account is no longer
// exhausted, so the next exhaustion starts with the fast 5-minute probes.
func (manager *Manager) resetQuotaProbeLevel(accountID string) {
	manager.quotaProbeMu.Lock()
	defer manager.quotaProbeMu.Unlock()
	delete(manager.quotaProbeLevels, accountID)
}

// pruneQuotaProbeLevels drops ladder entries for accounts no longer in the
// pool so the map stays bounded by the current account count.
func (manager *Manager) pruneQuotaProbeLevels(seen map[string]struct{}) {
	manager.quotaProbeMu.Lock()
	defer manager.quotaProbeMu.Unlock()
	for id := range manager.quotaProbeLevels {
		if _, ok := seen[id]; !ok {
			delete(manager.quotaProbeLevels, id)
		}
	}
}

// workerLivenessThreshold is how many consecutive /health probe failures the
// watchdog tolerates before rebooting a stuck worker. At a 30s maintenance
// cadence that is roughly a 90s grace window: long enough to ride out a
// transient network blip or a worker mid-rewarm, short enough that a genuinely
// stuck process is recovered before clients notice repeated hangs.
const workerLivenessThreshold = 3

// probeWorkerLiveness is the worker-side watchdog. refreshOne and the boot-time
// RefreshAll only run at startup and on manual console refresh, so a worker
// that goes "alive but stuck" after boot — /health no longer responds, or its
// goroutine is wedged — would stay routed to forever without this loop. Each
// 30s tick issues one /health round-trip per worker account; on the
// threshold-th consecutive failure it calls restartStuckWorker, which removes
// the process from the table, stops it, then calls beginAccountRecovery so the
// worker is rebooted with backoff.
//
// In-process accounts are skipped by probeHealthOnly and never enter the
// failure counter, so WorkBuddy / Trae / Devin are unaffected.
func (manager *Manager) probeWorkerLiveness(ctx context.Context) {
	for _, item := range manager.pool.Items() {
		if item.RuntimeState == "dead" {
			continue
		}
		ready, err := manager.probeHealthOnly(ctx, item)
		if err == nil && ready {
			manager.mu.Lock()
			delete(manager.healthFailures, item.ID)
			manager.mu.Unlock()
			manager.reconcileUnreadyWorker(ctx, item)
			continue
		}
		// Skip accounts already in recovery: they are no longer in processes
		// and beginAccountRecovery owns the reboot. Touching them
		// here would double-restart or race the recovering guard.
		manager.mu.Lock()
		if manager.recovering[item.ID] {
			manager.mu.Unlock()
			continue
		}
		manager.healthFailures[item.ID]++
		fails := manager.healthFailures[item.ID]
		manager.mu.Unlock()
		if fails >= workerLivenessThreshold {
			log.Printf("worker liveness: account %s failed %d consecutive health probes (%v); restarting",
				item.ID, fails, err)
			manager.mu.Lock()
			delete(manager.healthFailures, item.ID)
			manager.mu.Unlock()
			manager.restartStuckWorker(item.ID, err)
		}
	}
}

// workerReconcileInterval throttles the "alive but not routable" re-probe. It
// only has to be long enough that a worker waiting for login is not re-probed
// on every liveness tick, and short enough that a worker which just finished
// starting is promoted on the next tick or two.
const workerReconcileInterval = 2 * time.Minute

// reconcileUnreadyWorker re-runs the full account refresh for a worker that
// answers /health but is still absent from routing.
//
// The boot-time RefreshAll starts as soon as the manager does, before a freshly
// spawned worker has bound its port. That first probe fails on transport,
// refreshOne persists status=error, and nothing re-probes afterwards: the
// account, and every model only it could serve, stays unrouteable (clients see
// "no accounts available for this api key") until an operator clicks Refresh in
// the console. A recovery restart has the same shape — startAccount leaves the
// item unready and no refresh follows it.
//
// probeWorkerLiveness is the only loop still talking to such a worker, so it is
// the right place to notice that the worker came up: a successful /health means
// the process is listening, and refreshOne is what promotes the pool item and
// the stored status back to ready.
//
// A reconcile is a full refresh, not a cheap probe: once the worker reports
// ready, refreshOne also runs fetchQuota and fetchAccountModels concurrently.
// That catalog refresh is the point — without it routing never learns which
// models the recovered account serves — so the extra upstream calls are
// accepted, and the per-account throttle above caps them at one round each per
// interval. While a worker stays unready (for example waiting for login),
// refreshOne persists login_required, or error when the worker reports a
// LastError, on every reconcile; the status is rewritten with the same value
// each time. refreshStaleQuota does not double-fetch in the same tick: it only
// touches accounts whose cached quota says Exceeded and refreshOne fetches
// quota only after the worker turns ready. A console Refresh goes through
// RefreshAccount → refreshOne directly and is not gated by this throttle, so an
// operator refresh can overlap one watchdog refresh; both then issue the same
// probe, which the pool and quota gates tolerate, and the overlap (one extra
// /health plus one extra quota/catalog fetch) only happens on an explicit
// operator action.
func (manager *Manager) reconcileUnreadyWorker(ctx context.Context, item Item) {
	if item.Runtime == string(providers.RuntimeInProcess) || strings.TrimSpace(item.URL) == "" {
		return
	}
	if item.RuntimeState == "dead" || item.RuntimeState == "ready" {
		return
	}
	manager.mu.Lock()
	last, seen := manager.reconcileAt[item.ID]
	if seen && time.Since(last) < workerReconcileInterval {
		manager.mu.Unlock()
		return
	}
	manager.reconcileAt[item.ID] = time.Now()
	manager.mu.Unlock()

	if err := manager.refreshOne(ctx, item, false); err != nil {
		log.Printf("worker reconcile: account %s is still not routable: %v", item.ID, err)
		return
	}
	if refreshed, ok := manager.pool.ByID(item.ID); ok && refreshed.RuntimeState == "ready" {
		log.Printf("worker reconcile: account %s came up after an unready start; routing restored", item.ID)
	}
}

// restartStuckWorker reboots a worker whose /health stopped answering. It must
// not simply call stopAccount: stopAccount deletes m.processes[id] before
// Stop(), so watchAccount later sees a process mismatch and returns without
// calling beginAccountRecovery — the stuck worker would become a permanently
// dead account. Instead detach the process from the table (so watchAccount
// cannot also recover it), stop it, then hand it to beginAccountRecovery
// directly, which owns the backoff restart.
func (manager *Manager) restartStuckWorker(id string, probeErr error) {
	manager.mu.Lock()
	process := manager.processes[id]
	delete(manager.processes, id)
	manager.mu.Unlock()
	manager.pool.Remove(id)
	if process != nil {
		if perr := process.Stop(); perr != nil {
			log.Printf("worker liveness: stop %s failed: %v", id, perr)
		}
	}
	reason := "worker liveness probe failed"
	if probeErr != nil {
		reason = "worker liveness probe failed: " + probeErr.Error()
	}
	manager.beginAccountRecovery(id, errors.New(reason))
}
