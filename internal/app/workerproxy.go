package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/caigee-cmd/cli2api/internal/accounts"
	"github.com/caigee-cmd/cli2api/internal/providers"
	"github.com/caigee-cmd/cli2api/internal/providers/qoder"
)

var (
	errAccountNotRunning = qoder.ErrAccountNotRunning
	ErrWorkerNotWarm     = qoder.ErrWorkerNotWarm
	errWorkerNotWarm     = qoder.ErrWorkerNotWarm

	workerLoginReadyTimeout  = 90 * time.Second
	workerLoginReadyInterval = 200 * time.Millisecond
)

// workerBase returns the URL of the first running account session. Prefer
// workerForAccount with an explicit id.
func (a *App) workerBase() string {
	if item, ok := a.Pool.First(); ok {
		return item.URL
	}
	return ""
}

func (a *App) workerForAccount(id string) string {
	if id != "" {
		if item, ok := a.Pool.ByID(id); ok {
			return item.URL
		}
		// An explicit account ID that is not in the pool must not fall
		// back to the first running account — that would route a models
		// query (and potentially subsequent requests) to the wrong
		// account. Return empty so workerModels surfaces a clear error.
		return ""
	}
	return a.workerBase()
}

func (a *App) RequestedAccount(r *http.Request) string {
	id := strings.TrimSpace(r.URL.Query().Get("account"))
	if id == "" {
		id = strings.TrimSpace(r.Header.Get("X-Qoder-Account"))
	}
	return id
}

func (a *App) selectedAccountID(r *http.Request) string {
	if id := a.RequestedAccount(r); id != "" {
		return id
	}
	if item, ok := a.Pool.First(); ok {
		return item.ID
	}
	return ""
}

// proxyAccountWorker forwards a console request to the per-account Node worker
// and optionally syncs the account auth_type after a successful login.
func (a *App) proxyAccountWorker(w http.ResponseWriter, r *http.Request, accountID, path, syncAuth string) {
	waitForLogin := path == "/admin/login/device" || path == "/admin/login/pat"
	var workerURL string
	if waitForLogin {
		readyURL, err := a.waitForWorkerLogin(r.Context(), accountID)
		if err != nil {
			if errors.Is(err, errAccountNotRunning) {
				writeErr(w, http.StatusConflict, "account_not_running", errAccountNotRunning.Error())
				return
			}
			writeErr(w, http.StatusServiceUnavailable, "not_ready", err.Error())
			return
		}
		workerURL = readyURL
	} else {
		var ok bool
		workerURL, ok = a.Manager.AccountURL(accountID)
		if !ok {
			writeErr(w, http.StatusConflict, "account_not_running", "account is disabled or not running")
			return
		}
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	client := qoder.WorkerClient{
		HTTP:        &http.Client{Timeout: 120 * time.Second},
		ProxyAPIKey: a.Cfg.ProxyAPIKey,
	}
	statusCode, header, responseBody, err := client.Admin(r.Context(), workerURL, r.Method, path, r.Header.Get("Content-Type"), body)
	if err != nil {
		var transport qoder.TransportError
		if errors.As(err, &transport) {
			writeErr(w, http.StatusBadGateway, "worker_unavailable", err.Error())
			return
		}
		writeErr(w, http.StatusInternalServerError, "worker_request_failed", err.Error())
		return
	}
	for key, values := range header {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}
	if statusCode < 300 {
		if authType := qoder.LoginCompleteAuthType(syncAuth, responseBody); authType != "" {
			if err := a.Manager.SyncCredential(r.Context(), accountID, authType); err != nil {
				writeErr(w, http.StatusBadGateway, "credential_sync_failed", err.Error())
				return
			}
		}
	}
	w.WriteHeader(statusCode)
	_, _ = w.Write(responseBody)
}

func (a *App) workerModels(timeout time.Duration, accountID string, refresh bool) ([]map[string]any, error) {
	workerURL := a.workerForAccount(accountID)
	if workerURL == "" {
		return nil, fmt.Errorf("no running Qoder account")
	}
	client := qoder.WorkerClient{
		HTTP:        &http.Client{Timeout: timeout},
		ProxyAPIKey: a.Cfg.ProxyAPIKey,
		AccountID:   accountID,
	}
	entries, _, _, err := client.Models(context.Background(), workerURL, refresh)
	if err != nil {
		var transport qoder.TransportError
		if errors.As(err, &transport) {
			return nil, err
		}
		return nil, nil
	}
	return entries, nil
}

func (a *App) fetchWorkerModels(refresh bool) []map[string]any {
	models, _ := a.FetchWorkerModelsFor(refresh, "")
	return models
}

// CatalogMode controls how fetchProviderModels folds accounts that share a
// public model ID.
//
//   - CatalogModeMerge: one entry per provider+model for OpenAI-compatible
//     /v1/models and the default console catalog. Regions are unioned;
//     capabilities intersect conservatively; credits/free are omitted when
//     source regions disagree so a single-region price is never shown as
//     universal.
//   - CatalogModeExpand: one entry per provider+region+model for the
//     Providers page (?view=regional) so each row carries that region's
//     real credits, free flag, and capabilities.
type CatalogMode int

const (
	CatalogModeMerge CatalogMode = iota
	CatalogModeExpand
)

func (a *App) FetchWorkerModelsFor(refresh bool, accountID string) ([]map[string]any, error) {
	return a.FetchWorkerModelsForMode(refresh, accountID, CatalogModeMerge)
}

func (a *App) FetchWorkerModelsForMode(refresh bool, accountID string, mode CatalogMode) ([]map[string]any, error) {
	models, err := a.fetchProviderModels(refresh, accountID, mode)
	if err != nil {
		return nil, err
	}
	if models != nil {
		return models, nil
	}
	// An explicit account ID that is not in the pool must not silently
	// fall back to another account's catalog — that misleads the client
	// and can route subsequent requests to the wrong account.
	if accountID != "" {
		if _, ok := a.Pool.ByID(accountID); !ok {
			return nil, fmt.Errorf("account %s not found", accountID)
		}
	}
	// Last-resort path for a lone Qoder worker with no in-process providers
	// and no pool URLs folded above. Stamp region the same way expand/merge
	// would, so Providers filters never treat CN catalogs as unlabeled.
	parsed, err := a.workerModels(60*time.Second, accountID, refresh)
	if err != nil {
		if accountID != "" {
			return nil, err
		}
		return nil, nil
	}
	if len(parsed) == 0 {
		return nil, nil
	}
	region := "global"
	if accountID != "" {
		if item, ok := a.Pool.ByID(accountID); ok {
			region = accounts.NormalizeRegion(item.Region)
		}
	}
	for _, model := range parsed {
		if model == nil {
			continue
		}
		if _, ok := model["provider"]; !ok {
			model["provider"] = "qoder"
		}
		if _, ok := model["owned_by"]; !ok {
			model["owned_by"] = "qoder"
		}
		if mode == CatalogModeExpand {
			model["region"] = region
		} else {
			addModelRegion(model, region)
		}
	}
	return parsed, nil
}

// modelsNumericCapFields / modelsBoolCapFields list the capability fields the
// merged catalog intersects conservatively when the same public model is
// served by accounts of different regions: numeric fields take the minimum,
// boolean fields are ANDed. Other fields (display name, reasoning options,
// credits) keep the first-seen value.
var modelsNumericCapFields = []string{
	"catalog_context_length", "catalog_context_length_max",
	"max_output_tokens", "prompt_max_tokens",
}

var modelsBoolCapFields = []string{"supports_max_mode", "can_disable_thinking"}

func addModelRegion(entry map[string]any, region string) {
	region = strings.TrimSpace(region)
	if region == "" {
		return
	}
	for _, existing := range EntryModelRegions(entry) {
		if existing == region {
			return
		}
	}
	entry["regions"] = append(EntryModelRegions(entry), region)
}

func EntryModelRegions(entry map[string]any) []string {
	if raw, ok := entry["regions"].([]string); ok {
		return raw
	}
	if region, ok := entry["region"].(string); ok {
		region = strings.TrimSpace(region)
		if region != "" {
			return []string{region}
		}
	}
	return nil
}

// mergeModelEntryCapabilities folds a later source entry into the merged one
// conservatively: numeric capability fields take the minimum of the two,
// boolean capability fields are ANDed. A field missing from either side keeps
// the merged value (missing means "unknown", not "zero").
func mergeModelEntryCapabilities(merged, incoming map[string]any) {
	for _, field := range modelsNumericCapFields {
		mergedValue, ok1 := numericFieldValue(merged[field])
		incomingValue, ok2 := numericFieldValue(incoming[field])
		if ok1 && ok2 && incomingValue < mergedValue {
			merged[field] = incomingValue
		}
	}
	for _, field := range modelsBoolCapFields {
		if value, ok := incoming[field].(bool); ok && !value {
			merged[field] = false
		}
	}
}

func modelEntryCredits(entry map[string]any) string {
	credits, _ := entry["credits"].(string)
	return strings.TrimSpace(credits)
}

func modelEntryFree(entry map[string]any) (bool, bool) {
	free, ok := entry["free"].(bool)
	return free, ok
}

// mergeModelEntryPricing drops credits/free from a merged entry when source
// regions disagree. Keeping the first-seen price would present one region's
// catalog rate as if it applied everywhere.
func mergeModelEntryPricing(merged, incoming map[string]any) {
	if modelEntryCredits(merged) != modelEntryCredits(incoming) {
		delete(merged, "credits")
		delete(merged, "free")
		return
	}
	mergedFree, hasMergedFree := modelEntryFree(merged)
	incomingFree, hasIncomingFree := modelEntryFree(incoming)
	if hasMergedFree != hasIncomingFree || mergedFree != incomingFree {
		delete(merged, "free")
	}
}

func numericFieldValue(value any) (float64, bool) {
	switch v := value.(type) {
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	case float64:
		return v, true
	default:
		return 0, false
	}
}

// modelCapabilitiesEntry renders one adapter ModelInfo into the same entry
// shape used in the merged catalog, so capability intersection works on
// duplicate sources of the same public model.
func modelCapabilitiesEntry(model providers.ModelInfo) map[string]any {
	entry := map[string]any{}
	if model.Capabilities.ContextWindow > 0 {
		entry["catalog_context_length"] = model.Capabilities.ContextWindow
	}
	if model.Capabilities.ContextWindowMax > 0 {
		entry["catalog_context_length_max"] = model.Capabilities.ContextWindowMax
	}
	if model.Capabilities.MaxOutput > 0 {
		entry["max_output_tokens"] = model.Capabilities.MaxOutput
	}
	if model.Capabilities.PromptMaxTokens > 0 {
		entry["prompt_max_tokens"] = model.Capabilities.PromptMaxTokens
	}
	if model.Capabilities.MaxMode {
		entry["supports_max_mode"] = true
	}
	if model.Capabilities.CanDisableThinking {
		entry["can_disable_thinking"] = true
	}
	return entry
}

func providerModelEntry(model providers.ModelInfo, provider string) map[string]any {
	entry := map[string]any{
		"id": model.PublicModel, "object": "model", "owned_by": provider,
		"provider": provider, "native_model": model.NativeModel,
	}
	if strings.TrimSpace(model.DisplayName) != "" {
		entry["display_name"] = model.DisplayName
	}
	if credits := strings.TrimSpace(model.Credits); credits != "" {
		entry["credits"] = credits
	}
	if model.Free {
		entry["free"] = true
	}
	if model.Capabilities.ContextWindow > 0 {
		entry["catalog_context_length"] = model.Capabilities.ContextWindow
	}
	if model.Capabilities.ContextWindowMax > 0 {
		entry["catalog_context_length_max"] = model.Capabilities.ContextWindowMax
	}
	if model.Capabilities.MaxOutput > 0 {
		entry["max_output_tokens"] = model.Capabilities.MaxOutput
	}
	if model.Capabilities.PromptMaxTokens > 0 {
		entry["prompt_max_tokens"] = model.Capabilities.PromptMaxTokens
	}
	if model.Capabilities.MaxMode {
		entry["supports_max_mode"] = true
	}
	if len(model.Capabilities.ReasoningOptions) > 0 {
		entry["reasoning_options"] = model.Capabilities.ReasoningOptions
	}
	if model.Capabilities.ReasoningDefault != "" {
		entry["reasoning_default"] = model.Capabilities.ReasoningDefault
	}
	if model.Capabilities.ReasoningType != "" {
		entry["reasoning_type"] = model.Capabilities.ReasoningType
	}
	if model.Capabilities.CanDisableThinking {
		entry["can_disable_thinking"] = true
	}
	return entry
}

// fetchProviderModels builds the in-process + Qoder catalog. Merge mode is for
// OpenAI-compatible clients: one public entry per provider+model. Expand mode
// is for the console: one entry per provider+region+model with that region's
// credits/free/capabilities intact.
func (a *App) fetchProviderModels(refresh bool, accountID string, mode CatalogMode) ([]map[string]any, error) {
	var merged []map[string]any
	seen := map[string]map[string]any{}
	sawAny := false
	var lastErr error
	for _, item := range a.Pool.Items() {
		if accountID != "" && item.ID != accountID {
			continue
		}
		if item.Provider == "" || item.Provider == "qoder" {
			continue
		}
		adapter, ok := a.Providers.Get(item.Provider)
		if !ok || adapter.Models == nil {
			continue
		}
		models, err := adapter.Models.Models(context.Background(), item.ID)
		if err != nil {
			log.Printf("catalog fetch failed account=%s provider=%s: %v", item.ID, item.Provider, err)
			lastErr = err
			if accountID != "" {
				return nil, err
			}
			continue
		}
		sawAny = true
		region := accounts.NormalizeRegion(item.Region)
		for _, model := range models {
			// Dedup on the public model ID (what clients request and what the
			// entry exposes as "id"), not the upstream native ID. Two entries
			// may legitimately share a native model — e.g. a WorkBuddy alias
			// where NativeModel=deep-model and PublicModel=deepseek-v4.1-flash
			// alongside the native deep-model entry. Keying on the native ID
			// would drop the alias from the merged catalog.
			publicKey := strings.TrimSpace(model.PublicModel)
			if publicKey == "" {
				publicKey = strings.TrimSpace(model.NativeModel)
			}
			key := publicKey + "@" + item.Provider
			if mode == CatalogModeExpand {
				key += "@" + region
			}
			if existing, dup := seen[key]; dup {
				if mode == CatalogModeMerge {
					addModelRegion(existing, region)
					mergeModelEntryCapabilities(existing, modelCapabilitiesEntry(model))
					mergeModelEntryPricing(existing, providerModelEntry(model, item.Provider))
				}
				continue
			}
			entry := providerModelEntry(model, item.Provider)
			if mode == CatalogModeExpand {
				entry["region"] = region
			} else {
				addModelRegion(entry, region)
			}
			seen[key] = entry
			merged = append(merged, entry)
		}
	}
	// Fold Qoder daemon models alongside in-process providers. Do this even
	// when no WorkBuddy/Trae accounts exist; returning nil here used to skip
	// region stamping and send pure-Qoder pools through the unlabeled fallback.
	var qoderModels []map[string]any
	for _, item := range a.Pool.Items() {
		if item.Provider != "qoder" || item.URL == "" {
			continue
		}
		if accountID != "" && item.ID != accountID {
			continue
		}
		qoderModels = append(qoderModels, a.fetchQoderModels(refresh, item.ID)...)
	}
	for _, model := range qoderModels {
		key, _ := model["id"].(string)
		region := qoderModelRegion(model)
		seenKey := key + "@qoder"
		if mode == CatalogModeExpand {
			seenKey += "@" + region
		}
		if existing, dup := seen[seenKey]; dup {
			if mode == CatalogModeMerge {
				addModelRegion(existing, region)
				mergeModelEntryCapabilities(existing, model)
				mergeModelEntryPricing(existing, model)
			}
			continue
		}
		seen[seenKey] = model
		model["provider"] = "qoder"
		model["owned_by"] = "qoder"
		if mode == CatalogModeExpand {
			model["region"] = region
		} else {
			addModelRegion(model, region)
		}
		merged = append(merged, model)
		sawAny = true
	}
	if !sawAny {
		if accountID != "" && lastErr != nil {
			return nil, lastErr
		}
		return nil, nil
	}
	return merged, nil
}

// qoderModelRegion returns the region stamped on a per-account Qoder worker
// catalog entry (see fetchQoderModels), defaulting to global.
func qoderModelRegion(model map[string]any) string {
	region, _ := model["_qoder_region"].(string)
	delete(model, "_qoder_region")
	return accounts.NormalizeRegion(region)
}

func (a *App) fetchQoderModels(refresh bool, accountID string) []map[string]any {
	region := "global"
	if item, ok := a.Pool.ByID(accountID); ok {
		region = accounts.NormalizeRegion(item.Region)
	}
	parsed, err := a.workerModels(60*time.Second, accountID, refresh)
	if err != nil || len(parsed) == 0 {
		return nil
	}
	for _, model := range parsed {
		if model == nil {
			continue
		}
		if _, ok := model["provider"]; !ok {
			model["provider"] = "qoder"
		}
		if _, ok := model["owned_by"]; !ok {
			model["owned_by"] = "qoder"
		}
		// Internal marker consumed by fetchProviderModels; stripped from the
		// output before it reaches any client.
		model["_qoder_region"] = region
	}
	return parsed
}

func (a *App) waitForWorkerLogin(ctx context.Context, accountID string) (string, error) {
	workerURL, ok := a.Manager.AccountURL(accountID)
	if !ok || strings.TrimSpace(workerURL) == "" {
		return "", errAccountNotRunning
	}
	return WaitForWorkerAuthManager(ctx, func() (string, bool) {
		return workerURL, true
	}, workerLoginReadyTimeout, workerLoginReadyInterval)
}

func WaitForWorkerAuthManager(ctx context.Context, lookup func() (string, bool), timeout, interval time.Duration) (string, error) {
	return qoder.WaitForAuthManager(ctx, lookup, timeout, interval)
}
