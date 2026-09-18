package accounts

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/caigee-cmd/cli2api/internal/providers"
)

// Per-account catalog snapshots on Pool items. Qoder uses worker /admin/models;
// in-process providers use adapter.Models(). Does not merge with API display cache.

const modelCatalogTTL = 5 * time.Minute

// EnsureModelCatalogs refreshes per-account catalogs that are missing or
// older than the worker TTL. Failures leave the previous snapshot in place
// and never flip readiness or cooldown.
//
// Refreshes run concurrently in the background with a bounded semaphore so
// the first chat request is never blocked by N serial 15-second timeouts
// when multiple accounts are offline. The caller returns immediately; a
// nil Models slice means unknown (fail open) until the background refresh
// completes and subsequent requests use the fresh catalog.
func (m *Manager) EnsureModelCatalogs(ctx context.Context, force bool) {
	if m == nil || m.pool == nil {
		return
	}
	now := time.Now()
	var stale []Item
	for _, item := range m.pool.Items() {
		if !force && item.Models != nil && !item.ModelsAt.IsZero() && now.Sub(item.ModelsAt) < modelCatalogTTL {
			continue
		}
		stale = append(stale, item)
	}
	if len(stale) == 0 {
		return
	}
	// Fire background refreshes concurrently with a bounded semaphore.
	// Use m.runCtx so refreshes survive the caller's request context and
	// are canceled only on shutdown.
	if ctx.Err() != nil {
		return // caller context already cancelled — no point firing goroutines
	}
	const maxConcurrent = 4
	sem := make(chan struct{}, maxConcurrent)
	for _, item := range stale {
		go func(it Item) {
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
				m.fetchAccountModels(m.runCtx, it)
			case <-m.runCtx.Done():
			}
		}(item)
	}
}

func (m *Manager) fetchAccountModels(ctx context.Context, item Item) {
	if m == nil || m.pool == nil || item.ID == "" {
		return
	}
	if item.Runtime == string(providers.RuntimeInProcess) || strings.TrimSpace(item.URL) == "" {
		m.fetchProviderModels(ctx, item)
		return
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(item.URL, "/")+"/admin/models", nil)
	if err != nil {
		log.Printf("catalog refresh failed account=%s provider=%s stage=request: %v", item.ID, item.Provider, err)
		return
	}
	if m.config.ProxyAPIKey != "" {
		req.Header.Set("Authorization", "Bearer "+m.config.ProxyAPIKey)
	}
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("catalog refresh failed account=%s provider=%s stage=http: %v", item.ID, item.Provider, err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		log.Printf("catalog refresh failed account=%s provider=%s stage=status status=%d body=%q", item.ID, item.Provider, resp.StatusCode, strings.TrimSpace(string(body)))
		return
	}
	var parsed struct {
		Data []map[string]any `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		log.Printf("catalog refresh failed account=%s provider=%s stage=decode: %v", item.ID, item.Provider, err)
		return
	}
	m.pool.MergeModels(item.ID, catalogIDs(parsed.Data, nil))
}

func (m *Manager) fetchProviderModels(ctx context.Context, item Item) {
	if m.providers == nil {
		return
	}
	adapter, ok := m.providers.Get(item.Provider)
	if !ok || adapter.Models == nil {
		return
	}
	models, err := adapter.Models.Models(ctx, item.ID)
	if err != nil {
		log.Printf("catalog fetch failed account=%s provider=%s: %v", item.ID, item.Provider, err)
		return
	}
	ids := make([]string, 0, len(models)*2)
	for _, model := range models {
		ids = append(ids, model.PublicModel, model.NativeModel, model.DisplayName)
	}
	m.pool.MergeModels(item.ID, ids)
}

func catalogIDs(entries []map[string]any, extras []string) []string {
	ids := append([]string{}, extras...)
	for _, entry := range entries {
		for _, key := range []string{"id", "mapped_key", "native_model", "display_name"} {
			value, _ := entry[key].(string)
			if strings.TrimSpace(value) != "" {
				ids = append(ids, value)
			}
		}
	}
	return ids
}

// fetchQuota pulls the account quota snapshot from the worker daemon. Errors
