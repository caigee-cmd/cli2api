package console

import (
	"net/http"
	"time"

	"github.com/caigee-cmd/cli2api/internal/accounts"
)

// LiveAccount is the per-account slice of the live concurrency snapshot.
type LiveAccount struct {
	ID          string `json:"id"`
	InFlight    int    `json:"in_flight"`
	MaxInFlight int    `json:"max_inflight"`
	Enabled     bool   `json:"enabled"`
}

// LiveSnapshot is the cheap payload behind /api/live, polled every few
// seconds by the overview live strip.
type LiveSnapshot struct {
	Time     string        `json:"time"`
	InFlight int           `json:"in_flight"`
	Capacity int           `json:"capacity"`
	RPM      int           `json:"rpm"`
	Accounts []LiveAccount `json:"accounts"`
}

// buildLiveSnapshot sums in-flight over all accounts and capacity (the
// effective max_inflight, default 4) over enabled accounts only.
func buildLiveSnapshot(views []accounts.AccountView, rpm int, now time.Time) LiveSnapshot {
	out := LiveSnapshot{Time: now.Format(time.RFC3339), RPM: rpm, Accounts: make([]LiveAccount, 0, len(views))}
	for _, v := range views {
		maxInFlight := accounts.DefaultMaxInFlightValue(v.MaxInFlight)
		out.InFlight += v.InFlight
		if v.Enabled {
			out.Capacity += maxInFlight
		}
		out.Accounts = append(out.Accounts, LiveAccount{ID: v.ID, InFlight: v.InFlight, MaxInFlight: maxInFlight, Enabled: v.Enabled})
	}
	return out
}

func (h *Handler) HandleLive(w http.ResponseWriter, r *http.Request) {
	views, err := h.Control.Accounts.List(r.Context(), false)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "account_list_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, buildLiveSnapshot(views, h.Recorder.RecentRequests(), time.Now()))
}
