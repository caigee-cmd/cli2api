package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/caigee-cmd/cli2api/internal/accounts"
	"github.com/caigee-cmd/cli2api/internal/providers"
)

var (
	errAccountNotRunning = errors.New("account is disabled or not running")
	errWorkerNotWarm     = errors.New("account worker is still starting")

	workerLoginReadyTimeout  = 90 * time.Second
	workerLoginReadyInterval = 200 * time.Millisecond
)

// workerBase returns the URL of the first running account session. Prefer
// workerForAccount with an explicit id.
func (s *Server) workerBase() string {
	if item, ok := s.pool.First(); ok {
		return item.URL
	}
	return ""
}

func (s *Server) workerForAccount(id string) string {
	if id != "" {
		if item, ok := s.pool.ByID(id); ok {
			return item.URL
		}
		// An explicit account ID that is not in the pool must not fall
		// back to the first running account — that would route a models
		// query (and potentially subsequent requests) to the wrong
		// account. Return empty so workerGet surfaces a clear error.
		return ""
	}
	return s.workerBase()
}

func (s *Server) requestedAccount(r *http.Request) string {
	id := strings.TrimSpace(r.URL.Query().Get("account"))
	if id == "" {
		id = strings.TrimSpace(r.Header.Get("X-Qoder-Account"))
	}
	return id
}

func (s *Server) selectedAccountID(r *http.Request) string {
	if id := s.requestedAccount(r); id != "" {
		return id
	}
	if item, ok := s.pool.First(); ok {
		return item.ID
	}
	return ""
}

// proxyAccountWorker forwards a console request to the per-account Node worker
// and optionally syncs the account auth_type after a successful login.
func (s *Server) proxyAccountWorker(w http.ResponseWriter, r *http.Request, accountID, path, syncAuth string) {
	waitForLogin := path == "/admin/login/device" || path == "/admin/login/pat"
	var workerURL string
	if waitForLogin {
		readyURL, err := s.waitForWorkerLogin(r.Context(), accountID)
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
		workerURL, ok = s.manager.AccountURL(accountID)
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
	req, err := http.NewRequestWithContext(r.Context(), r.Method, workerURL+path, bytes.NewReader(body))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "worker_request_failed", err.Error())
		return
	}
	if contentType := r.Header.Get("Content-Type"); contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	if key := s.cfg.ProxyAPIKey; key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	resp, err := (&http.Client{Timeout: 120 * time.Second}).Do(req)
	if err != nil {
		writeErr(w, http.StatusBadGateway, "worker_unavailable", err.Error())
		return
	}
	defer resp.Body.Close()
	responseBody, _ := io.ReadAll(resp.Body)
	for key, values := range resp.Header {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}
	if resp.StatusCode < 300 && syncAuth != "" {
		authType := syncAuth
		if syncAuth == "oauth_if_complete" {
			var status struct {
				Login struct {
					Status string `json:"status"`
				} `json:"login"`
			}
			if json.Unmarshal(responseBody, &status) != nil || status.Login.Status != "ok" {
				authType = ""
			} else {
				authType = "oauth"
			}
		}
		if authType != "" {
			if err := s.manager.SyncCredential(r.Context(), accountID, authType); err != nil {
				writeErr(w, http.StatusBadGateway, "credential_sync_failed", err.Error())
				return
			}
		}
	}
	w.WriteHeader(resp.StatusCode)
	_, _ = w.Write(responseBody)
}

func (s *Server) workerGet(path string, timeout time.Duration, accountID string) (*http.Response, error) {
	workerURL := s.workerForAccount(accountID)
	if workerURL == "" {
		return nil, fmt.Errorf("no running Qoder account")
	}
	req, err := http.NewRequest(http.MethodGet, workerURL+path, nil)
	if err != nil {
		return nil, err
	}
	if key := s.cfg.ProxyAPIKey; key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	if accountID != "" {
		req.Header.Set("X-Qoder-Account", accountID)
	}
	client := &http.Client{Timeout: timeout}
	return client.Do(req)
}

func (s *Server) fetchWorkerModels(refresh bool) []map[string]any {
	models, _ := s.fetchWorkerModelsFor(refresh, "")
	return models
}

// catalogMode controls how fetchProviderModels folds accounts that share a
// public model ID.
//
//   - catalogModeMerge: one entry per provider+model for OpenAI-compatible
//     /v1/models and the default console catalog. Regions are unioned;
//     capabilities intersect conservatively; credits/free are omitted when
//     source regions disagree so a single-region price is never shown as
//     universal.
//   - catalogModeExpand: one entry per provider+region+model for the
//     Providers page (?view=regional) so each row carries that region's
//     real credits, free flag, and capabilities.
type catalogMode int

const (
	catalogModeMerge catalogMode = iota
	catalogModeExpand
)

func (s *Server) fetchWorkerModelsFor(refresh bool, accountID string) ([]map[string]any, error) {
	return s.fetchWorkerModelsForMode(refresh, accountID, catalogModeMerge)
}

func (s *Server) fetchWorkerModelsForMode(refresh bool, accountID string, mode catalogMode) ([]map[string]any, error) {
	models, err := s.fetchProviderModels(refresh, accountID, mode)
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
		if _, ok := s.pool.ByID(accountID); !ok {
			return nil, fmt.Errorf("account %s not found", accountID)
		}
	}
	// Last-resort path for a lone Qoder worker with no in-process providers
	// and no pool URLs folded above. Stamp region the same way expand/merge
	// would, so Providers filters never treat CN catalogs as unlabeled.
	path := "/admin/models"
	if refresh {
		path += "?refresh=1"
	}
	resp, err := s.workerGet(path, 60*time.Second, accountID)
	if err != nil {
		if accountID != "" {
			return nil, err
		}
		return nil, nil
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var parsed struct {
		Data []map[string]any `json:"data"`
	}
	if json.Unmarshal(body, &parsed) != nil || len(parsed.Data) == 0 {
		return nil, nil
	}
	region := "global"
	if accountID != "" {
		if item, ok := s.pool.ByID(accountID); ok {
			region = accounts.NormalizeRegion(item.Region)
		}
	}
	for _, model := range parsed.Data {
		if model == nil {
			continue
		}
		if _, ok := model["provider"]; !ok {
			model["provider"] = "qoder"
		}
		if _, ok := model["owned_by"]; !ok {
			model["owned_by"] = "qoder"
		}
		if mode == catalogModeExpand {
			model["region"] = region
		} else {
			addModelRegion(model, region)
		}
	}
	return parsed.Data, nil
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
	for _, existing := range entryModelRegions(entry) {
		if existing == region {
			return
		}
	}
	entry["regions"] = append(entryModelRegions(entry), region)
}

func entryModelRegions(entry map[string]any) []string {
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
func (s *Server) fetchProviderModels(refresh bool, accountID string, mode catalogMode) ([]map[string]any, error) {
	var merged []map[string]any
	seen := map[string]map[string]any{}
	sawAny := false
	var lastErr error
	for _, item := range s.pool.Items() {
		if accountID != "" && item.ID != accountID {
			continue
		}
		if item.Provider == "" || item.Provider == "qoder" {
			continue
		}
		adapter, ok := s.providers.Get(item.Provider)
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
			if mode == catalogModeExpand {
				key += "@" + region
			}
			if existing, dup := seen[key]; dup {
				if mode == catalogModeMerge {
					addModelRegion(existing, region)
					mergeModelEntryCapabilities(existing, modelCapabilitiesEntry(model))
					mergeModelEntryPricing(existing, providerModelEntry(model, item.Provider))
				}
				continue
			}
			entry := providerModelEntry(model, item.Provider)
			if mode == catalogModeExpand {
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
	for _, item := range s.pool.Items() {
		if item.Provider != "qoder" || item.URL == "" {
			continue
		}
		if accountID != "" && item.ID != accountID {
			continue
		}
		qoderModels = append(qoderModels, s.fetchQoderModels(refresh, item.ID)...)
	}
	for _, model := range qoderModels {
		key, _ := model["id"].(string)
		region := qoderModelRegion(model)
		seenKey := key + "@qoder"
		if mode == catalogModeExpand {
			seenKey += "@" + region
		}
		if existing, dup := seen[seenKey]; dup {
			if mode == catalogModeMerge {
				addModelRegion(existing, region)
				mergeModelEntryCapabilities(existing, model)
				mergeModelEntryPricing(existing, model)
			}
			continue
		}
		seen[seenKey] = model
		model["provider"] = "qoder"
		model["owned_by"] = "qoder"
		if mode == catalogModeExpand {
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

func (s *Server) fetchQoderModels(refresh bool, accountID string) []map[string]any {
	region := "global"
	if item, ok := s.pool.ByID(accountID); ok {
		region = accounts.NormalizeRegion(item.Region)
	}
	path := "/admin/models"
	if refresh {
		path += "?refresh=1"
	}
	resp, err := s.workerGet(path, 60*time.Second, accountID)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var parsed struct {
		Data []map[string]any `json:"data"`
	}
	if json.Unmarshal(body, &parsed) != nil || len(parsed.Data) == 0 {
		return nil
	}
	for _, model := range parsed.Data {
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
	return parsed.Data
}

func (s *Server) waitForWorkerLogin(ctx context.Context, accountID string) (string, error) {
	workerURL, ok := s.manager.AccountURL(accountID)
	if !ok || strings.TrimSpace(workerURL) == "" {
		return "", errAccountNotRunning
	}
	return waitForWorkerAuthManager(ctx, func() (string, bool) {
		return workerURL, true
	}, workerLoginReadyTimeout, workerLoginReadyInterval)
}

func waitForWorkerAuthManager(ctx context.Context, lookup func() (string, bool), timeout, interval time.Duration) (string, error) {
	if timeout <= 0 {
		timeout = 90 * time.Second
	}
	if interval <= 0 {
		interval = 200 * time.Millisecond
	}
	deadline := time.Now().Add(timeout)
	client := &http.Client{Timeout: 2 * time.Second}
	var lastErr error
	for {
		if err := ctx.Err(); err != nil {
			if lastErr != nil {
				return "", lastErr
			}
			return "", err
		}
		workerURL, ok := lookup()
		workerURL = strings.TrimRight(strings.TrimSpace(workerURL), "/")
		if !ok || workerURL == "" {
			lastErr = errAccountNotRunning
		} else {
			ready, err := workerReportsAuthManager(ctx, client, workerURL)
			if err != nil {
				lastErr = err
			} else if ready {
				return workerURL, nil
			} else {
				lastErr = errWorkerNotWarm
			}
		}
		if !time.Now().Before(deadline) {
			if lastErr == nil {
				lastErr = errWorkerNotWarm
			}
			if errors.Is(lastErr, errAccountNotRunning) {
				return "", lastErr
			}
			return "", fmt.Errorf("%w: %v", errWorkerNotWarm, lastErr)
		}
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			if lastErr != nil {
				return "", lastErr
			}
			return "", ctx.Err()
		case <-timer.C:
		}
	}
}

func workerReportsAuthManager(ctx context.Context, client *http.Client, workerURL string) (bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, workerURL+"/health", nil)
	if err != nil {
		return false, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 400 {
		return false, fmt.Errorf("worker health status %d", resp.StatusCode)
	}
	var health struct {
		HasAuthManager bool `json:"hasAuthManager"`
	}
	if err := json.Unmarshal(body, &health); err != nil {
		return false, err
	}
	return health.HasAuthManager, nil
}
