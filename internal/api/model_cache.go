package api

import (
	"net/http"
	"strings"
	"time"
)

const modelsAPICacheTTL = 5 * time.Minute

type modelsAPICacheEntry struct {
	models []map[string]any
	at     time.Time
}

type modelsAPIRefresh struct {
	done   chan struct{}
	models []map[string]any
	err    error
}

func (s *Server) handleModelsAPI(w http.ResponseWriter, r *http.Request) {
	refresh := r.URL.Query().Get("refresh") == "1"
	mode := catalogModeMerge
	// Providers page asks for one row per provider+region so credits/free stay
	// truthful. Access / Overview keep the default merged catalog.
	if strings.EqualFold(strings.TrimSpace(r.URL.Query().Get("view")), "regional") {
		mode = catalogModeExpand
	}
	models, err := s.fetchModelsAPI(refresh, s.requestedAccount(r), mode)
	if err != nil {
		// 503, not 502: some reverse proxies replace origin 502 JSON with
		// their own HTML error page, which the console then renders as the
		// catalog failure message.
		writeErr(w, http.StatusServiceUnavailable, "catalog_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"object": "list",
		"data":   s.decorateModelsWithContext(r.Context(), s.filterModelsForIdentity(r, models)),
	})
}

func modelsAPICacheKey(accountID string, mode catalogMode) string {
	view := "merged"
	if mode == catalogModeExpand {
		view = "regional"
	}
	if strings.TrimSpace(accountID) == "" {
		return "*@" + view
	}
	return accountID + "@" + view
}

func cloneModelList(models []map[string]any) []map[string]any {
	if models == nil {
		return nil
	}
	out := make([]map[string]any, 0, len(models))
	for _, model := range models {
		item := make(map[string]any, len(model))
		for key, value := range model {
			item[key] = value
		}
		out = append(out, item)
	}
	return out
}

// fetchModelsAPI serves GET /api/models from a 5-minute snapshot. Expired
// snapshots are returned immediately while one background refresh updates the
// cache. A cold cache waits for the one in-flight refresh instead of starting
// duplicate upstream catalog requests. Merge and regional views use separate
// cache keys so Providers cannot poison Access / Overview.
func (s *Server) fetchModelsAPI(refresh bool, accountID string, mode catalogMode) ([]map[string]any, error) {
	key := modelsAPICacheKey(accountID, mode)
	s.modelsAPICacheMu.Lock()
	entry, hasCache := s.modelsAPICache[key]
	if !refresh && hasCache && time.Since(entry.at) < modelsAPICacheTTL {
		s.modelsAPICacheMu.Unlock()
		return cloneModelList(entry.models), nil
	}
	if !refresh && hasCache {
		_ = s.startModelsAPIRefreshLocked(key, true, accountID, mode)
		s.modelsAPICacheMu.Unlock()
		return cloneModelList(entry.models), nil
	}
	refreshing := s.startModelsAPIRefreshLocked(key, refresh, accountID, mode)
	s.modelsAPICacheMu.Unlock()
	<-refreshing.done
	if refreshing.err != nil {
		return nil, refreshing.err
	}
	return cloneModelList(refreshing.models), nil
}

func (s *Server) startModelsAPIRefreshLocked(key string, force bool, accountID string, mode catalogMode) *modelsAPIRefresh {
	if s.modelsAPIRefresh == nil {
		s.modelsAPIRefresh = map[string]*modelsAPIRefresh{}
	}
	if refreshing, ok := s.modelsAPIRefresh[key]; ok {
		return refreshing
	}
	refreshing := &modelsAPIRefresh{done: make(chan struct{})}
	s.modelsAPIRefresh[key] = refreshing
	go func() {
		models, err := s.fetchWorkerModelsForMode(force, accountID, mode)
		refreshing.models = models
		refreshing.err = err
		if err == nil {
			s.modelsAPICacheMu.Lock()
			if s.modelsAPICache == nil {
				s.modelsAPICache = map[string]modelsAPICacheEntry{}
			}
			s.modelsAPICache[key] = modelsAPICacheEntry{models: cloneModelList(models), at: time.Now()}
			s.modelsAPICacheMu.Unlock()
		}
		s.modelsAPICacheMu.Lock()
		delete(s.modelsAPIRefresh, key)
		s.modelsAPICacheMu.Unlock()
		close(refreshing.done)
	}()
	return refreshing
}
