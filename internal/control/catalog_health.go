package control

import (
	"log"
	"sort"
	"strings"
	"sync"
	"time"
)

// CatalogProviderHealth is one provider's upstream model-catalog sync state.
// It is display-only: a failure never removes models from the shared catalog.
type CatalogProviderHealth struct {
	Provider      string `json:"provider"`
	Models        int    `json:"models"`
	LastAttemptAt string `json:"last_attempt_at,omitempty"`
	LastSuccessAt string `json:"last_success_at,omitempty"`
	LastError     string `json:"last_error,omitempty"`
	// Stale is true while the last attempt failed and the model list below
	// still describes the last successful sync.
	Stale bool `json:"stale"`
}

// CatalogStatus is the whole display catalog's sync state. SyncedAt is the last
// successful full refresh of the merged view that backs /v1/models.
type CatalogStatus struct {
	SyncedAt        string                  `json:"synced_at,omitempty"`
	Stale           bool                    `json:"stale"`
	Refreshing      bool                    `json:"refreshing"`
	LastError       string                  `json:"last_error,omitempty"`
	IntervalSeconds int                     `json:"interval_seconds"`
	Providers       []CatalogProviderHealth `json:"providers"`
}

// CatalogHealth records per-provider catalog outcomes. Providers discover their
// own model list, so the merged catalog can be half fresh: recording per
// provider is what lets the console and /v1/models say which upstream is behind
// instead of only that "the catalog" failed.
type CatalogHealth struct {
	mu        sync.Mutex
	providers map[string]*CatalogProviderHealth
	now       func() time.Time
}

func NewCatalogHealth() *CatalogHealth {
	return &CatalogHealth{providers: map[string]*CatalogProviderHealth{}, now: time.Now}
}

func (h *CatalogHealth) clock() time.Time {
	if h == nil || h.now == nil {
		return time.Now()
	}
	return h.now()
}

func (h *CatalogHealth) entry(provider string) *CatalogProviderHealth {
	key := strings.ToLower(strings.TrimSpace(provider))
	if key == "" {
		key = "unknown"
	}
	entry, ok := h.providers[key]
	if !ok {
		entry = &CatalogProviderHealth{Provider: key}
		h.providers[key] = entry
	}
	return entry
}

// Attempt marks the start of one upstream catalog request.
func (h *CatalogHealth) Attempt(provider string) {
	if h == nil {
		return
	}
	at := h.clock().UTC().Format(time.RFC3339)
	h.mu.Lock()
	defer h.mu.Unlock()
	h.entry(provider).LastAttemptAt = at
}

// Success records a served catalog and clears the provider's failure state.
func (h *CatalogHealth) Success(provider string, models int) {
	if h == nil {
		return
	}
	at := h.clock().UTC().Format(time.RFC3339)
	h.mu.Lock()
	defer h.mu.Unlock()
	entry := h.entry(provider)
	entry.Models = models
	entry.LastSuccessAt = at
	entry.LastError = ""
	entry.Stale = false
}

// Failure records a failed catalog request. The previous model count and last
// success time stay in place so a failure never looks like an empty catalog.
func (h *CatalogHealth) Failure(provider string, err error) {
	if h == nil || err == nil {
		return
	}
	at := h.clock().UTC().Format(time.RFC3339)
	h.mu.Lock()
	defer h.mu.Unlock()
	entry := h.entry(provider)
	entry.LastAttemptAt = at
	entry.LastError = strings.TrimSpace(err.Error())
	entry.Stale = true
}

// Snapshot returns a stable, provider-sorted copy for display.
func (h *CatalogHealth) Snapshot() []CatalogProviderHealth {
	if h == nil {
		return nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]CatalogProviderHealth, 0, len(h.providers))
	for _, entry := range h.providers {
		out = append(out, *entry)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Provider < out[j].Provider })
	return out
}

// Restore seeds health from a persisted snapshot. Timestamps are kept verbatim;
// a provider whose last attempt failed stays stale until the next success.
func (h *CatalogHealth) Restore(entries []CatalogProviderHealth) {
	if h == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, entry := range entries {
		if strings.TrimSpace(entry.Provider) == "" {
			continue
		}
		copied := entry
		h.providers[strings.ToLower(copied.Provider)] = &copied
	}
}

// logFailure prints one line per failed provider request so an operator can see
// which upstream is behind without opening the console.
func logFailure(provider string, err error) {
	if err == nil {
		return
	}
	log.Printf("catalog sync failed provider=%s: %v", provider, err)
}
