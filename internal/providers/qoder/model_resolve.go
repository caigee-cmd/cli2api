package qoder

import (
	"context"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/caigee-cmd/cli2api/internal/accounts"
	"github.com/caigee-cmd/cli2api/internal/providers"
	"github.com/caigee-cmd/cli2api/internal/translate"
)

// coldCatalogTimeout bounds the one synchronous model/list fetch a request
// may trigger when the account has no cached catalog yet.
const coldCatalogTimeout = 5 * time.Second

// catalogEntryKey is the gateway routing key of a model/list entry.
func catalogEntryKey(entry map[string]any) string {
	return strings.TrimSpace(stringField(entry, "mapped_key", "key", "native_model"))
}

// findCatalogEntry matches a client model name against the catalog: gateway
// key (exact), then id / display name (case-insensitive), then the canonical
// lowercase form used for routing elsewhere.
func findCatalogEntry(entries []map[string]any, model string) map[string]any {
	want := strings.TrimSpace(model)
	if want == "" {
		return nil
	}
	usable := func(e map[string]any) bool {
		if e == nil || catalogEntryKey(e) == "" {
			return false
		}
		enable, ok := e["enable"].(bool)
		return !ok || enable
	}
	for _, e := range entries {
		if usable(e) && catalogEntryKey(e) == want {
			return e
		}
	}
	for _, e := range entries {
		if !usable(e) {
			continue
		}
		for _, name := range []string{stringField(e, "id"), stringField(e, "display_name"), stringField(e, "mapped_key")} {
			if name != "" && strings.EqualFold(strings.TrimSpace(name), want) {
				return e
			}
		}
	}
	canon := accounts.CanonicalModelID(want)
	for _, e := range entries {
		if !usable(e) {
			continue
		}
		for _, name := range []string{stringField(e, "id"), stringField(e, "display_name"), catalogEntryKey(e)} {
			if name != "" && accounts.CanonicalModelID(name) == canon {
				return e
			}
		}
	}
	return nil
}

// resolveCatalogModel rewrites req.Model to the account's gateway key. The
// cached catalog is used without a network call; a cold cache gets one
// bounded, single-flighted fetch. An unresolvable name against a loaded
// catalog is model_not_found; an unloadable catalog keeps the old behavior.
func resolveCatalogModel(ctx context.Context, catalogOf func(context.Context) ([]map[string]any, error), req translate.ChatRequest) (translate.ChatRequest, map[string]any, error) {
	model := strings.TrimSpace(req.Model)
	fetchCtx, cancel := context.WithTimeout(ctx, coldCatalogTimeout)
	entries, err := catalogOf(fetchCtx)
	cancel()
	if err != nil || len(entries) == 0 {
		if model != "" && !strings.EqualFold(model, "auto") {
			log.Printf("[qoder-direct] WARN model catalog unavailable; sending requested model %q unresolved", model)
		}
		return req, nil, nil
	}
	if model == "" || strings.EqualFold(model, "auto") {
		// auto routes server-side; use its entry only for metadata.
		if entry := findCatalogEntry(entries, "auto"); entry != nil {
			req.Model = catalogEntryKey(entry)
			return req, entry, nil
		}
		return req, nil, nil
	}
	entry := findCatalogEntry(entries, model)
	if entry == nil {
		return req, nil, &providers.Error{
			Kind:    accounts.KindModelNotAvailable,
			Status:  http.StatusBadRequest,
			Code:    "model_not_found",
			Type:    "invalid_request_error",
			Message: "qoder model " + model + " is not in this account's model catalog",
		}
	}
	req.Model = catalogEntryKey(entry)
	return req, entry, nil
}

// applyCatalogEntry fills model_config from the matched catalog entry.
// Reasoning: an explicit request level wins ("none" forces off, any other
// level forces on, unchanged from before); with no level the catalog's
// is_reasoning decides instead of the previous hard false.
func applyCatalogEntry(plain map[string]any, req translate.ChatRequest, entry map[string]any) {
	if entry == nil {
		return
	}
	cfg, _ := plain["model_config"].(map[string]any)
	if cfg == nil {
		return
	}
	if name := stringField(entry, "display_name"); name != "" {
		cfg["display_name"] = name
	}
	if v, ok := entry["is_vl"].(bool); ok {
		cfg["is_vl"] = v
	}
	if n, ok := rawNumber(req.MaxInputTokens); !ok || n <= 0 {
		if n, ok := numberFieldValue(entry, "max_input_tokens"); ok && n > 0 {
			cfg["max_input_tokens"] = n
		}
	}
	if resolvedReasoningLevel(req) == "" {
		if v, ok := entry["is_reasoning"].(bool); ok {
			cfg["is_reasoning"] = v
			if cc, _ := plain["chat_context"].(map[string]any); cc != nil {
				if extra, _ := cc["extra"].(map[string]any); extra != nil {
					if mc, _ := extra["modelConfig"].(map[string]any); mc != nil {
						mc["is_reasoning"] = v
					}
				}
			}
		}
	}
}
