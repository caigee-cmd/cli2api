package console

import (
	"net/http"
	"strings"
	"time"

	"github.com/caigee-cmd/cli2api/internal/accounts"
	applogs "github.com/caigee-cmd/cli2api/internal/logs"
)

// exportWindowHours is the widest range the cost panel can ask for. It follows
// the totals retention window, not the detail window: retention keeps the
// aggregate columns for far longer than the per-request detail, so clamping the
// panel to the detail window would hide history that is still in the database.
func exportWindowHours() int {
	return 24 * applogs.TotalsRetentionDays()
}

func (h *Handler) HandleUsage(w http.ResponseWriter, r *http.Request) {
	path := strings.Trim(r.URL.Path, "/")
	switch path {
	case "api/usage/pricing":
		if r.Method != http.MethodGet {
			writeErr(w, http.StatusMethodNotAllowed, "method_not_allowed", "GET only")
			return
		}
		catalog := make([]accounts.ModelPrice, 0)
		for _, model := range accounts.ModelPriceCatalog() {
			if model.Alias {
				continue
			}
			catalog = append(catalog, model)
		}
		// No display copy here: the console renders localized text from its own
		// i18n messages, so the API stays language-neutral.
		writeJSON(w, http.StatusOK, map[string]any{
			"currency": "CNY",
			"models":   catalog,
		})
	case "api/usage/cost":
		if r.Method != http.MethodGet {
			writeErr(w, http.StatusMethodNotAllowed, "method_not_allowed", "GET only")
			return
		}
		h.handleUsageCost(w, r)
	default:
		writeErr(w, http.StatusNotFound, "not_found", "unknown usage endpoint")
	}
}

func (h *Handler) handleUsageCost(w http.ResponseWriter, r *http.Request) {
	if h.Recorder == nil {
		writeErr(w, http.StatusServiceUnavailable, "logs_unavailable", "request logs unavailable")
		return
	}
	now := time.Now().UTC()
	to := ParseQueryTime(r.URL.Query().Get("to"), true)
	if to == nil {
		to = &now
	}
	from := ParseQueryTime(r.URL.Query().Get("from"), false)
	if from == nil {
		earliest := to.Add(-time.Duration(exportWindowHours()) * time.Hour)
		from = &earliest
	}
	// Clamp to the retention ceiling so a wide custom range cannot imply
	// full coverage of data the pruner already dropped.
	if floor := to.Add(-time.Duration(exportWindowHours()) * time.Hour); from.Before(floor) {
		from = &floor
	}
	if !to.After(*from) {
		start := to.Add(-24 * time.Hour)
		from = &start
	}
	breakdown, err := h.Recorder.CostBreakdown(r.Context(), *from, *to, r.URL.Query().Get("api_key"))
	if err != nil {
		if err.Error() == "request logs unavailable" {
			writeErr(w, http.StatusServiceUnavailable, "logs_unavailable", err.Error())
			return
		}
		writeErr(w, http.StatusInternalServerError, "cost_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, breakdown)
}
