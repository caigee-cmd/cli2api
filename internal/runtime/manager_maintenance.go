package runtime

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"log"
	"strings"
	"time"
	_ "time/tzdata"

	"github.com/caigee-cmd/cli2api/internal/accounts"
	"github.com/caigee-cmd/cli2api/internal/providers"
)

func (manager *Manager) CheckinAccount(ctx context.Context, accountID string) (Account, error) {
	if manager == nil {
		return Account{}, fmt.Errorf("account manager unavailable")
	}
	manager.mu.Lock()
	if manager.checkinRunning == nil {
		manager.checkinRunning = make(map[string]bool)
	}
	if manager.checkinRunning[accountID] {
		manager.mu.Unlock()
		return Account{}, fmt.Errorf("check-in is already running for this account")
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
		return Account{}, err
	}
	if !account.Enabled {
		return account, fmt.Errorf("account is disabled")
	}
	policy, supported := providers.CheckinFor(account.Provider, account.ProviderRegion)
	adapter, registered := manager.providers.Get(account.Provider)
	if !supported || !registered || adapter.Checkin == nil {
		return account, providers.ErrUnsupported
	}
	location, err := time.LoadLocation(policy.Timezone)
	if err != nil {
		return account, fmt.Errorf("invalid check-in timezone: %w", err)
	}
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	stop := context.AfterFunc(manager.runCtx, cancel)
	defer stop()
	if CheckedInLocalDay(account.LastCheckinAt, account.LastCheckinStatus, time.Now().In(location)) {
		manager.refreshCheckinQuota(ctx, accountID, adapter)
		return manager.store.Get(ctx, accountID)
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
		return account, errors.Join(checkErr, fmt.Errorf("save check-in result: %w", recordErr))
	}
	if result.Status != "skipped" {
		manager.refreshCheckinQuota(ctx, accountID, adapter)
	}
	readCtx, stopReading := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer stopReading()
	updated, err := manager.store.Get(readCtx, accountID)
	return updated, errors.Join(checkErr, err)
}

func (manager *Manager) refreshCheckinQuota(ctx context.Context, accountID string, adapter providers.Adapter) {
	if ctx.Err() != nil {
		return
	}
	if adapter.Prober != nil {
		manager.fetchProviderQuota(ctx, accountID, adapter.Prober)
	} else if workerURL, found := manager.AccountURL(accountID); found {
		manager.fetchQuota(ctx, accountID, workerURL, true)
	}
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
	if !account.Enabled || !account.AutoCheckin {
		return false
	}
	if recordedToday(account.LastCheckinAt, now) && account.LastCheckinStatus != "error" {
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
	retry, _ := checkinSlot("21:00", account.ID, now)
	return due.Before(retry) && !now.Before(retry) && last.Before(retry)
}

func (manager *Manager) runScheduledCheckins(ctx context.Context, now time.Time) {
	items, err := manager.store.List(ctx)
	if err != nil {
		log.Printf("checkin schedule list: %v", err)
		return
	}
	for _, account := range items {
		if ctx.Err() != nil {
			return
		}
		policy, supported := providers.CheckinFor(account.Provider, account.ProviderRegion)
		adapter, registered := manager.providers.Get(account.Provider)
		if !supported || !registered || adapter.Checkin == nil || !account.Enabled || !account.AutoCheckin {
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
		if checkinDue(account, configuredTime, now.In(location)) {
			if _, err := manager.CheckinAccount(ctx, account.ID); err != nil {
				log.Printf("checkin account=%s: %v", account.ID, err)
			}
		}
	}
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
	for _, account := range items {
		if !account.Enabled || !account.AutoCheckin {
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
		if _, err := manager.CheckinAccount(ctx, account.ID); err != nil {
			log.Printf("checkin account=%s: %v", account.ID, err)
		}
	}
}

func (manager *Manager) KeepaliveWorkBuddy(ctx context.Context, onlyOptIn bool) {
	if manager == nil || manager.workbuddy == nil {
		return
	}
	items, err := manager.store.List(ctx)
	if err != nil {
		log.Printf("workbuddy keepalive list: %v", err)
		return
	}
	for _, account := range items {
		if ctx.Err() != nil {
			return
		}
		if account.Provider != "workbuddy" || !account.Enabled || (onlyOptIn && !account.AutoCheckin) {
			continue
		}
		if err := manager.workbuddy.Keepalive(ctx, account.ID); err != nil {
			log.Printf("workbuddy keepalive account=%s: %v", account.ID, err)
		}
	}
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
	for {
		if ctx.Err() != nil {
			return
		}
		now := time.Now()
		manager.runScheduledCheckins(ctx, now)
		day := now.Format("2006-01-02")
		if now.Hour() >= 22 && lastKeepaliveDay != day {
			keepaliveCtx, stopKeepalive := context.WithTimeout(ctx, 2*time.Minute)
			manager.KeepaliveWorkBuddy(keepaliveCtx, true)
			stopKeepalive()
			lastKeepaliveDay = day
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
