package control

import (
	"context"
	"fmt"
	"github.com/caigee-cmd/cli2api/internal/accounts"
	"github.com/caigee-cmd/cli2api/internal/providers"
	"log"
	"strings"
)

// CatalogAccount is a display source snapshot, not a scheduler item.
type CatalogAccount struct {
	ID, Provider, Region string
	Worker               bool
}

// CatalogSource aggregates provider catalogs through injected data sources.
// Health is optional and records one outcome per provider request.
type CatalogSource struct {
	Accounts     func() []CatalogAccount
	Providers    *providers.Registry
	WorkerModels func(context.Context, string, bool) ([]map[string]any, error)
	Health       *CatalogHealth
}

// recordAttempt, recordSuccess and recordFailure are no-ops when no health
// recorder is wired, so a source built by a test needs no extra setup.
func (a *CatalogSource) recordAttempt(provider string) {
	if a != nil && a.Health != nil {
		a.Health.Attempt(provider)
	}
}

func (a *CatalogSource) recordSuccess(provider string, models int) {
	if a != nil && a.Health != nil {
		a.Health.Success(provider, models)
	}
}

func (a *CatalogSource) recordFailure(provider string, err error) {
	if a != nil && a.Health != nil {
		a.Health.Failure(provider, err)
	}
}

func (a *CatalogSource) byID(id string) (CatalogAccount, bool) {
	for _, item := range a.Accounts() {
		if item.ID == id {
			return item, true
		}
	}
	return CatalogAccount{}, false
}
func (a *CatalogSource) Fetch(refresh bool, accountID string, mode CatalogMode) ([]map[string]any, error) {
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
		if _, ok := a.byID(accountID); !ok {
			return nil, fmt.Errorf("account %s not found", accountID)
		}
	}
	// Last-resort path for a lone Qoder worker with no in-process providers
	// and no pool URLs folded above. Stamp region the same way expand/merge
	// would, so Providers filters never treat CN catalogs as unlabeled.
	a.recordAttempt("qoder")
	parsed, err := a.WorkerModels(context.Background(), accountID, refresh)
	if err != nil {
		a.recordFailure("qoder", err)
		if accountID != "" {
			return nil, err
		}
		return nil, nil
	}
	a.recordSuccess("qoder", len(parsed))
	if len(parsed) == 0 {
		return nil, nil
	}
	region := "global"
	if accountID != "" {
		if item, ok := a.byID(accountID); ok {
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
			AddModelRegion(model, region)
		}
	}
	return parsed, nil
}

func (a *CatalogSource) fetchProviderModels(refresh bool, accountID string, mode CatalogMode) ([]map[string]any, error) {
	var merged []map[string]any
	seen := map[string]map[string]any{}
	sawAny := false
	var lastErr error
	for _, item := range a.Accounts() {
		if accountID != "" && item.ID != accountID {
			continue
		}
		if item.Provider == "" || (item.Provider == "qoder" && item.Worker) {
			continue
		}
		adapter, ok := a.Providers.Get(item.Provider)
		if !ok || adapter.Models == nil {
			continue
		}
		a.recordAttempt(item.Provider)
		models, err := adapter.Models.Models(context.Background(), item.ID)
		if err != nil {
			log.Printf("catalog fetch failed account=%s provider=%s: %v", item.ID, item.Provider, err)
			a.recordFailure(item.Provider, err)
			lastErr = err
			if accountID != "" {
				return nil, err
			}
			continue
		}
		a.recordSuccess(item.Provider, len(models))
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
					AddModelRegion(existing, region)
					MergeModelEntryCapabilities(existing, ModelCapabilitiesEntry(model))
					MergeModelEntryPricing(existing, ProviderModelEntry(model, item.Provider))
				}
				continue
			}
			entry := ProviderModelEntry(model, item.Provider)
			if mode == CatalogModeExpand {
				entry["region"] = region
			} else {
				AddModelRegion(entry, region)
			}
			seen[key] = entry
			merged = append(merged, entry)
		}
	}
	// Fold Qoder daemon models alongside in-process providers. Do this even
	// when no WorkBuddy/Trae accounts exist; returning nil here used to skip
	// region stamping and send pure-Qoder pools through the unlabeled fallback.
	var qoderModels []map[string]any
	for _, item := range a.Accounts() {
		if item.Provider != "qoder" || !item.Worker {
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
				AddModelRegion(existing, region)
				MergeModelEntryCapabilities(existing, model)
				MergeModelEntryPricing(existing, model)
			}
			continue
		}
		seen[seenKey] = model
		model["provider"] = "qoder"
		model["owned_by"] = "qoder"
		if mode == CatalogModeExpand {
			model["region"] = region
		} else {
			AddModelRegion(model, region)
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

func (a *CatalogSource) fetchQoderModels(refresh bool, accountID string) []map[string]any {
	region := "global"
	if item, ok := a.byID(accountID); ok {
		region = accounts.NormalizeRegion(item.Region)
	}
	a.recordAttempt("qoder")
	parsed, err := a.WorkerModels(context.Background(), accountID, refresh)
	if err != nil {
		a.recordFailure("qoder", err)
		return nil
	}
	a.recordSuccess("qoder", len(parsed))
	if len(parsed) == 0 {
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
		// Qoder treats every catalog model as vision-capable: the in-process
		// adapter always sends is_vl=true in model_config and ModelInfos reports
		// Images for the same reason. The worker catalog sometimes carries the
		// upstream is_vl flag, so honor an explicit answer and fall back to the
		// adapter's declared behavior when the flag is absent.
		if vision, ok := model["is_vl"].(bool); ok {
			model["supports_images"] = vision
		} else {
			model["supports_images"] = true
		}
		// Internal marker consumed by fetchProviderModels; stripped from the
		// output before it reaches any client.
		model["_qoder_region"] = region
	}
	return parsed
}
