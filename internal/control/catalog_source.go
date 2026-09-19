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
type CatalogSource struct {
	Accounts     func() []CatalogAccount
	Providers    *providers.Registry
	WorkerModels func(context.Context, string, bool) ([]map[string]any, error)
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
	parsed, err := a.WorkerModels(context.Background(), accountID, refresh)
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
	parsed, err := a.WorkerModels(context.Background(), accountID, refresh)
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
