package console

import (
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/caigee-cmd/cli2api/internal/accounts"
	"github.com/caigee-cmd/cli2api/internal/executor"
)

// LiveAccount is the per-account slice of the live concurrency snapshot
// (LiveStrip / sidebar). Kept for backward compatibility.
type LiveAccount struct {
	ID          string `json:"id"`
	InFlight    int    `json:"in_flight"`
	MaxInFlight int    `json:"max_inflight"`
	Enabled     bool   `json:"enabled"`
}

// LiveWorker is one account-pool slot projected for the Overview「实时进行中」
// card. Stage-1 UI still mocks jobs/depth; Stage-3 swaps workers/rate_limits/
// sticky onto this shape without a parallel /api/inflight*.
type LiveWorker struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Provider    string  `json:"provider"`
	Busy        bool    `json:"busy"`
	InFlight    int     `json:"in_flight"`
	MaxInFlight int     `json:"max_inflight"`
	Enabled     bool    `json:"enabled"`
	RateLimited bool    `json:"rate_limited"`
	// Progress is a legacy occupancy ratio (in_flight/max_inflight). The
	// Overview card no longer treats it as % toward completion — busy slots
	// use an indeterminate shimmer; keep the field for API compat.
	Progress float64 `json:"progress"`
	// StartedAt is RFC3339 wall-clock when the pool slot went busy
	// (InFlight 0→1). Empty when idle. UI shows real elapsed from this.
	StartedAt string `json:"started_at,omitempty"`
	// LastErrorKind is AccountView.LastErrorKind — fallback when a busy→idle
	// transition has no matching recent_completions entry yet.
	LastErrorKind string `json:"last_error_kind,omitempty"`
}

// LiveRateLimit is one active account-wide or model-scoped cooldown window,
// read from AccountView.DownUntil / ModelCooldowns (already projected from
// Pool DownUntil / ModelDownUntil). Console never invents a second cooldown
// state machine here.
type LiveRateLimit struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	AccountID   string `json:"account_id"`
	Model       string `json:"model,omitempty"`
	Until       string `json:"until"`
	RemainingMs int64  `json:"remaining_ms"`
}

// LiveSnapshot is the cheap payload behind /api/live, polled every few
// seconds by the overview live strip and (Stage-3) the inflight card.
// LiveCompletion is one terminal request finish drained from the request
// recorder for this /api/live poll (busy→idle Done vs Failed routing).
type LiveCompletion struct {
	AccountID string `json:"account_id"`
	Status    string `json:"status"`
}

// LiveJob is one selecting (选号) request for the InflightLiveCard waiting
// queue. Status reuses accounts.RequestStatus* (started) — not a new state
// machine. Running occupancy stays on Workers via Pool InFlight.
type LiveJob struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Status    string `json:"status"`
	StartedAt string `json:"started_at,omitempty"`
	// AccountID set when Bind already happened but selectingMinDwell still
	// projects the job on the waiting rail (queue→slot fly target).
	AccountID string `json:"account_id,omitempty"`
}

type LiveSnapshot struct {
	Time              string                        `json:"time"`
	InFlight          int                           `json:"in_flight"`
	Capacity          int                           `json:"capacity"`
	RPM               int                           `json:"rpm"`
	Accounts          []LiveAccount                 `json:"accounts"`
	Workers           []LiveWorker                  `json:"workers"`
	RateLimits        []LiveRateLimit               `json:"rate_limits"`
	Sticky            executor.SessionAffinityStats `json:"sticky"`
	RecentCompletions []LiveCompletion              `json:"recent_completions,omitempty"`
	// Queue is Prepare→Acquire 选号 (RequestStatusStarted, no pool bind).
	Queue []LiveJob `json:"queue,omitempty"`
}

// buildLiveSnapshot sums in-flight over all accounts and capacity (the
// effective max_inflight, default 4) over enabled accounts only, then
// projects workers / rate_limits / sticky for the Overview card. It is a
// read-only console projection: Classify→Pool write path is untouched.
func buildLiveSnapshot(views []accounts.AccountView, rpm int, sticky executor.SessionAffinityStats, now time.Time) LiveSnapshot {
	out := LiveSnapshot{
		Time:       now.Format(time.RFC3339),
		RPM:        rpm,
		Accounts:   make([]LiveAccount, 0, len(views)),
		Workers:    make([]LiveWorker, 0, len(views)),
		RateLimits: make([]LiveRateLimit, 0),
		Queue:      make([]LiveJob, 0),
		Sticky:     sticky,
	}
	for _, v := range views {
		maxInFlight := accounts.DefaultMaxInFlightValue(v.MaxInFlight)
		out.InFlight += v.InFlight
		if v.Enabled {
			out.Capacity += maxInFlight
		}
		out.Accounts = append(out.Accounts, LiveAccount{
			ID: v.ID, InFlight: v.InFlight, MaxInFlight: maxInFlight, Enabled: v.Enabled,
		})

		name := strings.TrimSpace(v.Name)
		if name == "" {
			name = v.ID
		}
		accountLimited := cooldownActive(v.DownUntil, now)
		modelLimited := false
		for model, until := range v.ModelCooldowns {
			if !cooldownActive(until, now) {
				continue
			}
			modelLimited = true
			remaining := remainingMs(until, now)
			label := name
			if model != "" {
				label = fmt.Sprintf("%s · %s", name, model)
			}
			out.RateLimits = append(out.RateLimits, LiveRateLimit{
				ID:          fmt.Sprintf("%s:%s", v.ID, model),
				Label:       label,
				AccountID:   v.ID,
				Model:       model,
				Until:       until,
				RemainingMs: remaining,
			})
		}
		if accountLimited {
			out.RateLimits = append(out.RateLimits, LiveRateLimit{
				ID:          v.ID,
				Label:       name,
				AccountID:   v.ID,
				Until:       v.DownUntil,
				RemainingMs: remainingMs(v.DownUntil, now),
			})
		}

		progress := 0.0
		if maxInFlight > 0 && v.InFlight > 0 {
			progress = float64(v.InFlight) / float64(maxInFlight)
			if progress > 1 {
				progress = 1
			}
		}
		out.Workers = append(out.Workers, LiveWorker{
			ID: v.ID, Name: name, Provider: v.Provider,
			Busy: v.InFlight > 0, InFlight: v.InFlight, MaxInFlight: maxInFlight,
			Enabled: v.Enabled, RateLimited: accountLimited || modelLimited,
			Progress: progress, StartedAt: strings.TrimSpace(v.InFlightSince),
			LastErrorKind: strings.TrimSpace(v.LastErrorKind),
		})
	}
	sort.SliceStable(out.RateLimits, func(i, j int) bool {
		if out.RateLimits[i].RemainingMs != out.RateLimits[j].RemainingMs {
			return out.RateLimits[i].RemainingMs > out.RateLimits[j].RemainingMs
		}
		return out.RateLimits[i].ID < out.RateLimits[j].ID
	})
	return out
}

func cooldownActive(until string, now time.Time) bool {
	until = strings.TrimSpace(until)
	if until == "" {
		return false
	}
	t, err := time.Parse(time.RFC3339, until)
	if err != nil {
		return false
	}
	return now.Before(t)
}

func remainingMs(until string, now time.Time) int64 {
	t, err := time.Parse(time.RFC3339, strings.TrimSpace(until))
	if err != nil || !now.Before(t) {
		return 0
	}
	return t.Sub(now).Milliseconds()
}

func (h *Handler) stickyStats() executor.SessionAffinityStats {
	if h == nil || h.Executor == nil || h.Executor.SessionAffinity == nil {
		return executor.SessionAffinityStats{}
	}
	return h.Executor.SessionAffinity.Stats()
}

func (h *Handler) HandleLive(w http.ResponseWriter, r *http.Request) {
	views, err := h.Control.Accounts.List(r.Context(), false)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "account_list_failed", err.Error())
		return
	}
	rpm := 0
	if h.Recorder != nil {
		rpm = h.Recorder.RecentRequests()
	}
	snap := buildLiveSnapshot(views, rpm, h.stickyStats(), time.Now())
	if h.Recorder != nil {
		for _, c := range h.Recorder.DrainRecentCompletions() {
			snap.RecentCompletions = append(snap.RecentCompletions, LiveCompletion{
				AccountID: c.AccountID,
				Status:    c.Status,
			})
		}
		for _, q := range h.Recorder.SelectingRequests() {
			name := strings.TrimSpace(q.RequestedModel)
			if name == "" {
				name = q.ID
			}
			status := strings.TrimSpace(q.Status)
			if status == "" {
				status = accounts.RequestStatusStarted
			}
			snap.Queue = append(snap.Queue, LiveJob{
				ID:        q.ID,
				Name:      name,
				Status:    status,
				StartedAt: q.StartedAt.UTC().Format(time.RFC3339),
				AccountID: strings.TrimSpace(q.AccountID),
			})
		}
	}
	writeJSON(w, http.StatusOK, snap)
}
