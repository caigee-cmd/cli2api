package server

import (
	"net/http"
	"time"

	"github.com/caigee-cmd/cli2api/internal/buildinfo"
	"github.com/caigee-cmd/cli2api/internal/endpoint"
	"github.com/caigee-cmd/cli2api/internal/providers"
)

func providerIDs() []string {
	out := make([]string, 0, len(providers.List()))
	for _, d := range providers.List() {
		out = append(out, d.ID)
	}
	return out
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":                        true,
		"service":                   "cli2api",
		"providers":                 providerIDs(),
		"cross_provider_model_pool": s.crossProviderOn(),
		"phase":                     "ui-preview",
		"chat_url":                  endpoint.ChatCompletionsPath,
		"time":                      time.Now().UTC().Format(time.RFC3339),
		"version":                   buildinfo.Version,
		"commit":                    buildinfo.Commit,
		"maintenance":               s.updater().Maintenance.Load(),
		"draining":                  s.draining(),
		"log_dropped":               s.logDrops(),
		"log_queue":                 s.logQueueDepth(),
	})
}

// handleReady separates liveness from readiness: Docker should keep the
// container alive while it drains, but a load balancer or orchestrator must
// stop sending new work as soon as the process starts shutting down.
func (s *Server) handleReady(w http.ResponseWriter, r *http.Request) {
	ready := !s.draining() && !s.updater().Maintenance.Load()
	status := http.StatusOK
	if !ready {
		status = http.StatusServiceUnavailable
	}
	writeJSON(w, status, map[string]any{
		"ok":          ready,
		"draining":    s.draining(),
		"maintenance": s.updater().Maintenance.Load(),
		"version":     buildinfo.Version,
		"commit":      buildinfo.Commit,
		"time":        time.Now().UTC().Format(time.RFC3339),
	})
}
