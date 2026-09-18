package api

import (
	"context"
	"net/http"
	"time"

	"github.com/caigee-cmd/cli2api/internal/buildinfo"
	"github.com/caigee-cmd/cli2api/internal/endpoint"
)

func (s *Server) handleOverview(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("refresh") == "1" {
		refreshCtx, refreshCancel := context.WithTimeout(context.Background(), 5*time.Second)
		_ = s.control.Accounts.RefreshAll(refreshCtx, true)
		refreshCancel()
	}
	accountViews, err := s.control.Accounts.List(r.Context(), false)
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
	models := s.decorateModelsWithContext(r.Context(), s.filterModelsForIdentity(r, s.fetchWorkerModels(false)))
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":   true,
		"time": time.Now().Format(time.RFC3339),
		"proxy": map[string]any{
			"ok": true, "service": "cli2api", "port": s.cfg.Port,
			"providers":                 providerIDs(),
			"cross_provider_model_pool": s.crossProviderModelPool.Load(),
			"version":                   buildinfo.Version, "commit": buildinfo.Commit,
			"chat_url": "/v1/chat/completions",
		},
		"worker": map[string]any{
			"ok": readyCount > 0, "hot": hotCount > 0, "ready_count": readyCount,
			"hot_count": hotCount, "account_count": len(accountViews),
		},
		"routing": map[string]any{
			"strategy":         s.pool.RoutingStrategy(),
			"session_affinity": s.executor.SessionAffinity.Stats(),
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
			"needs_api_key_for_chat":        s.cfg.ProxyAPIKey != "",
			"proxy_api_key_required_for_v1": s.cfg.ProxyAPIKey != "",
		},
	})
}

func (s *Server) handleOverviewSummary(w http.ResponseWriter, r *http.Request) {
	accountViews, err := s.control.Accounts.List(r.Context(), false)
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
	s.modelsAPICacheMu.Lock()
	if cached, ok := s.modelsAPICache[modelsAPICacheKey("", catalogModeMerge)]; ok && time.Since(cached.at) < modelsAPICacheTTL {
		modelCount = len(cached.models)
	}
	s.modelsAPICacheMu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":   true,
		"time": time.Now().Format(time.RFC3339),
		"proxy": map[string]any{
			"ok": true, "service": "cli2api", "port": s.cfg.Port,
			"providers":                 providerIDs(),
			"cross_provider_model_pool": s.crossProviderModelPool.Load(),
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
			"strategy":         s.pool.RoutingStrategy(),
			"session_affinity": s.executor.SessionAffinity.Stats(),
		},
		"access": map[string]any{
			"openai_base_url": "/v1", "chat_completions": endpoint.ChatCompletionsPath,
			"messages": endpoint.MessagesPath, "responses": endpoint.ResponsesPath,
			"models": endpoint.ModelsPath, "health": endpoint.HealthPath,
		},
	})
}
