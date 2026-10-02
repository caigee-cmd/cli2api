package console

import (
	"net/http"
)

// SystemResources is the console-facing view of host process usage. The
// fields mirror runtime.ResourceSnapshot but the type lives here so console
// never imports the runtime package.
type SystemResources struct {
	SampledAt string                  `json:"sampled_at"`
	Server    SystemProcessResources  `json:"server"`
	Workers   []SystemWorkerResources `json:"workers"`
	TotalRSS  int64                   `json:"total_rss_bytes"`
}

type SystemProcessResources struct {
	PID        int     `json:"pid"`
	RSSBytes   int64   `json:"rss_bytes"`
	HeapBytes  int64   `json:"heap_bytes,omitempty"`
	Goroutines int     `json:"goroutines,omitempty"`
	CPUPercent float64 `json:"cpu_percent"`
}

type SystemWorkerResources struct {
	AccountID  string  `json:"account_id"`
	Label      string  `json:"label,omitempty"`
	PID        int     `json:"pid"`
	RSSBytes   int64   `json:"rss_bytes"`
	CPUPercent float64 `json:"cpu_percent"`
}

// HandleSystemResources serves GET /api/system/resources. The snapshot itself
// is produced by the injected Resources func (app wires it to the runtime
// manager); labels come from the account list so workers show friendly names.
func (h *Handler) HandleSystemResources(w http.ResponseWriter, r *http.Request) {
	if h.Resources == nil {
		writeErr(w, http.StatusServiceUnavailable, "resources_unavailable", "resource metrics are not available")
		return
	}
	res := h.Resources()
	if res == nil {
		writeErr(w, http.StatusServiceUnavailable, "resources_unavailable", "resource metrics are not available")
		return
	}
	writeJSON(w, http.StatusOK, res)
}
