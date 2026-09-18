package accounts

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"
)

// WorkBuddy check-in/keepalive scheduler. Other providers must not gain this
// behavior by being listed here. Writes check-in records via Store; never chat cooldown.

// alreadyCheckedIn is implemented by workbuddy.AlreadyCheckedInError without
// importing that package (accounts <-> workbuddy would cycle).
type alreadyCheckedIn interface {
	AlreadyCheckedIn() bool
}

// CheckinAccount runs one WorkBuddy daily-checkin, records display fields, and
// refreshes credits. Failures never write chat cooldown.
func (m *Manager) CheckinAccount(ctx context.Context, accountID string) (Account, error) {
	if m == nil || m.workbuddy == nil {
		return Account{}, fmt.Errorf("workbuddy maintainer not configured")
	}
	account, err := m.store.Get(ctx, accountID)
	if err != nil {
		return Account{}, err
	}
	if account.Provider != "workbuddy" {
		return account, fmt.Errorf("check-in is only available for WorkBuddy accounts")
	}
	if checkedInLocalDay(account.LastCheckinAt, account.LastCheckinStatus, time.Now()) {
		if adapter, ok := m.providers.Get("workbuddy"); ok && adapter.Prober != nil {
			m.fetchProviderQuota(ctx, accountID, adapter.Prober)
		}
		return m.store.Get(ctx, accountID)
	}
	msg, checkErr := m.workbuddy.DailyCheckin(ctx, accountID)
	if msg == "" && checkErr != nil {
		msg = checkErr.Error()
	}
	if msg == "" {
		msg = "ok"
	}
	status := "success"
	var already alreadyCheckedIn
	if checkErr != nil {
		status = "error"
		if errors.As(checkErr, &already) && already.AlreadyCheckedIn() {
			status = "already"
		}
	}
	_ = m.store.RecordCheckin(ctx, accountID, status, msg, time.Now().UTC())
	if adapter, ok := m.providers.Get("workbuddy"); ok && adapter.Prober != nil {
		m.fetchProviderQuota(ctx, accountID, adapter.Prober)
	}
	account, getErr := m.store.Get(ctx, accountID)
	if getErr != nil {
		return account, getErr
	}
	if checkErr == nil || status == "already" {
		return account, nil
	}
	return account, checkErr
}

// CheckinOptedIn runs check-in for every enabled WorkBuddy account with
// workbuddy_auto_checkin on. Cooldown accounts are included; disabled skip.
func (m *Manager) CheckinOptedIn(ctx context.Context) {
	m.checkinOptedIn(ctx, time.Now(), "", false)
}

func (m *Manager) checkinOptedIn(ctx context.Context, now time.Time, scheduledTime string, retryDue bool) {
	if m == nil || m.workbuddy == nil {
		return
	}
	accounts, err := m.store.List(ctx)
	if err != nil {
		log.Printf("workbuddy checkin list: %v", err)
		return
	}
	for _, account := range accounts {
		if account.Provider != "workbuddy" || !account.Enabled || !account.WorkBuddyAutoCheckin {
			continue
		}
		if scheduledTime != "" {
			if retryDue {
				if account.WorkBuddyCheckinTime == scheduledTime || !workBuddyCheckinDue(account.WorkBuddyCheckinTime, now) {
					continue
				}
			} else if account.WorkBuddyCheckinTime != scheduledTime {
				continue
			}
		}
		if checkedInLocalDay(account.LastCheckinAt, account.LastCheckinStatus, now) {
			continue
		}
		if _, err := m.CheckinAccount(ctx, account.ID); err != nil {
			log.Printf("workbuddy checkin account_id=%s op=checkin err=%v", account.ID, err)
		}
	}
}

func workBuddyCheckinDue(value string, now time.Time) bool {
	parsed, err := time.Parse("15:04", value)
	if err != nil {
		parsed, _ = time.Parse("15:04", DefaultWorkBuddyCheckinTime)
	}
	due := time.Date(now.Year(), now.Month(), now.Day(), parsed.Hour(), parsed.Minute(), 0, 0, now.Location())
	return !due.After(now)
}

// checkedInLocalDay is true when the last recorded check-in is success or
// already on the process-local calendar day. Error rows do not skip, so the
// evening slot can retry a morning miss.
func checkedInLocalDay(at, status string, now time.Time) bool {
	switch strings.TrimSpace(status) {
	case "success", "already":
	default:
		return false
	}
	raw := strings.TrimSpace(at)
	if raw == "" {
		return false
	}
	parsed, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		parsed, err = time.Parse(time.RFC3339, raw)
		if err != nil {
			return false
		}
	}
	loc := now.Location()
	localAt := parsed.In(loc)
	localNow := now.In(loc)
	return localAt.Year() == localNow.Year() && localAt.YearDay() == localNow.YearDay()
}

// KeepaliveWorkBuddy refreshes tokens for enabled WorkBuddy accounts.
// When onlyOptIn is true, only auto-checkin accounts are touched (scheduled
// path). Manual/batch keepalive can pass false.
func (m *Manager) KeepaliveWorkBuddy(ctx context.Context, onlyOptIn bool) {
	if m == nil || m.workbuddy == nil {
		return
	}
	accounts, err := m.store.List(ctx)
	if err != nil {
		log.Printf("workbuddy keepalive list: %v", err)
		return
	}
	for _, account := range accounts {
		if account.Provider != "workbuddy" || !account.Enabled {
			continue
		}
		if onlyOptIn && !account.WorkBuddyAutoCheckin {
			continue
		}
		if err := m.workbuddy.Keepalive(ctx, account.ID); err != nil {
			log.Printf("workbuddy keepalive account_id=%s op=keepalive err=%v", account.ID, err)
		}
	}
}

// RunWorkBuddyMaintenanceLoop fires each opted-in account at its configured
// local time, retries due failures near 21:00, and keeps tokens alive near
// 22:00. Stop by closing stop.
func (m *Manager) RunWorkBuddyMaintenanceLoop(stop <-chan struct{}) {
	if m == nil {
		return
	}
	for {
		accounts, err := m.store.List(context.Background())
		if err != nil {
			log.Printf("workbuddy schedule list: %v", err)
		}
		delay, fire := nextWorkBuddyFire(time.Now(), accounts)
		if delay > time.Minute {
			delay = time.Minute
			fire = workBuddyFire{}
		}
		timer := time.NewTimer(delay)
		select {
		case <-stop:
			timer.Stop()
			return
		case <-m.runCtx.Done():
			timer.Stop()
			return
		case <-timer.C:
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			now := time.Now()
			for _, scheduledTime := range fire.checkinTimes {
				m.checkinOptedIn(ctx, now, scheduledTime, false)
			}
			if fire.retry {
				m.checkinOptedIn(ctx, now, "21:00", true)
			}
			if fire.keepalive {
				m.KeepaliveWorkBuddy(ctx, true)
			}
			cancel()
		}
	}
}

type workBuddyFire struct {
	checkinTimes []string
	retry        bool
	keepalive    bool
}

func nextWorkBuddyFire(now time.Time, accounts []Account) (time.Duration, workBuddyFire) {
	type slot struct {
		time string
		kind string
	}
	slots := []slot{{"21:00", "retry"}, {"22:00", "keepalive"}}
	seen := map[string]bool{}
	for _, account := range accounts {
		if account.Provider != "workbuddy" || !account.Enabled || !account.WorkBuddyAutoCheckin {
			continue
		}
		checkinTime, err := NormalizeWorkBuddyCheckinTime(account.WorkBuddyCheckinTime)
		if err != nil || seen[checkinTime] {
			continue
		}
		seen[checkinTime] = true
		slots = append(slots, slot{checkinTime, "checkin"})
	}
	loc := now.Location()
	var best time.Time
	var fire workBuddyFire
	for _, slot := range slots {
		parsed, _ := time.Parse("15:04", slot.time)
		candidate := time.Date(now.Year(), now.Month(), now.Day(), parsed.Hour(), parsed.Minute(), 0, 0, loc)
		candidate = candidate.Add(time.Duration(candidate.Unix()%15) * time.Minute)
		if !candidate.After(now) {
			candidate = candidate.Add(24 * time.Hour)
		}
		if best.IsZero() || candidate.Before(best) {
			best = candidate
			fire = workBuddyFire{}
		}
		if !candidate.Equal(best) {
			continue
		}
		switch slot.kind {
		case "checkin":
			fire.checkinTimes = append(fire.checkinTimes, slot.time)
		case "retry":
			fire.retry = true
		case "keepalive":
			fire.keepalive = true
		}
	}
	return best.Sub(now), fire
}
