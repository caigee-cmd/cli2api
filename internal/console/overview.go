package console

import (
	"context"
	"net/http"
	"time"

	"github.com/caigee-cmd/cli2api/internal/buildinfo"
	"github.com/caigee-cmd/cli2api/internal/control"
	"github.com/caigee-cmd/cli2api/internal/endpoint"
	"github.com/caigee-cmd/cli2api/internal/providers"
)

func (h *Handler) HandleOverview(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("refresh") == "1" {
		refreshCtx, refreshCancel := context.WithTimeout(context.Background(), 5*time.Second)
		_ = h.Control.Accounts.RefreshAll(refreshCtx, true)
		refreshCancel()
	}
	accountViews, err := h.Control.Accounts.List(r.Context(), false)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "account_list_failed", err.Error())
		return
	}
	readyCount := 0
	hotCount := 0
	coolingCount := 0
	inFlight := 0
	for _, account := range accountViews {
		if account.Ready {
			readyCount++
		}
		if account.Hot {
			hotCount++
		}
		if account.DownUntil != "" {
			coolingCount++
		}
		inFlight += account.InFlight
	}
	models := h.decorateModels(r.Context(), h.filterModels(r, h.fetchWorkerModels(false)))
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":   true,
		"time": time.Now().Format(time.RFC3339),
		"proxy": map[string]any{
			"ok": true, "service": "cli2api", "port": h.cfgPort(),
			"providers":                 providerIDs(),
			"cross_provider_model_pool": h.crossProviderPoolOn(),
			"version":                   buildinfo.Version, "commit": buildinfo.Commit,
			"chat_url": "/v1/chat/completions",
		},
		"worker": map[string]any{
			"ok": readyCount > 0, "hot": hotCount > 0, "ready_count": readyCount,
			"hot_count": hotCount, "account_count": len(accountViews),
		},
		"routing": map[string]any{
			"strategy":         h.Pool.RoutingStrategy(),
			"session_affinity": h.Executor.SessionAffinity.Stats(),
		},
		"accounts": accountViews,
		"models":   models,
		"access": map[string]any{
			"openai_base_url": "/v1", "chat_completions": endpoint.ChatCompletionsPath,
			"messages": endpoint.MessagesPath, "responses": endpoint.ResponsesPath,
			"models": endpoint.ModelsPath, "health": endpoint.HealthPath,
			"hint": "Console APIs and /v1 require the API key stored in SQLite.",
		},
		"ui": map[string]any{
			"needs_api_key_for_chat":        h.cfgProxyAPIKey() != "",
			"proxy_api_key_required_for_v1": h.cfgProxyAPIKey() != "",
		},
	})
}

func (h *Handler) HandleOverviewSummary(w http.ResponseWriter, r *http.Request) {
	accountViews, err := h.Control.Accounts.List(r.Context(), false)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "account_list_failed", err.Error())
		return
	}
	readyCount := 0
	hotCount := 0
	coolingCount := 0
	inFlight := 0
	for _, account := range accountViews {
		if account.Ready {
			readyCount++
		}
		if account.Hot {
			hotCount++
		}
		if account.DownUntil != "" {
			coolingCount++
		}
		inFlight += account.InFlight
	}
	modelCount := 0
	if h.Control != nil && h.Control.Catalog != nil {
		modelCount = h.Control.Catalog.CachedCount("", control.CatalogModeMerge)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":   true,
		"time": time.Now().Format(time.RFC3339),
		"proxy": map[string]any{
			"ok": true, "service": "cli2api", "port": h.cfgPort(),
			"providers":                 providerIDs(),
			"cross_provider_model_pool": h.crossProviderPoolOn(),
			"version":                   buildinfo.Version, "commit": buildinfo.Commit,
			"chat_url": "/v1/chat/completions",
		},
		"worker": map[string]any{
			"ok": readyCount > 0, "hot": hotCount > 0,
			"ready_count": readyCount, "hot_count": hotCount,
			"account_count": len(accountViews), "cooling_count": coolingCount,
			"in_flight": inFlight,
		},
		"model_count": modelCount,
		"routing": map[string]any{
			"strategy":         h.Pool.RoutingStrategy(),
			"session_affinity": h.Executor.SessionAffinity.Stats(),
		},
		"access": map[string]any{
			"openai_base_url": "/v1", "chat_completions": endpoint.ChatCompletionsPath,
			"messages": endpoint.MessagesPath, "responses": endpoint.ResponsesPath,
			"models": endpoint.ModelsPath, "health": endpoint.HealthPath,
		},
	})
}

func providerIDs() []string {
	out := make([]string, 0, len(providers.List()))
	for _, d := range providers.List() {
		out = append(out, d.ID)
	}
	return out
}
