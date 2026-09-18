package api

import (
	"net/http"
	"strings"

	"github.com/caigee-cmd/cli2api/internal/accounts"
	"github.com/caigee-cmd/cli2api/internal/auth"
	"github.com/caigee-cmd/cli2api/internal/translate"
)

func (s *Server) handleModels(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"object": "list",
		"data":   s.decorateModelsWithContext(r.Context(), s.filterModelsForIdentity(r, s.fetchWorkerModels(false))),
	})
}

func providerPrefix(model string) string {
	model = strings.TrimSpace(model)
	for _, prefix := range []string{"qoder/", "workbuddy/", "trae/", "devin/"} {
		if strings.HasPrefix(model, prefix) {
			return strings.TrimSuffix(prefix, "/")
		}
	}
	return ""
}

// resolveProviderFilter enforces public-model ID rules. Prefixed IDs pin one
// provider family. Bare IDs are rejected when the cross-provider model pool
// setting is disabled; when enabled, the filter is empty for a shared route
// pool.
func (s *Server) rejectsBareModel(model string) bool {
	return strings.TrimSpace(model) != "" && !s.crossProviderModelPool.Load() && providerPrefix(model) == ""
}

func (s *Server) resolveProviderFilter(req *translate.ChatRequest) string {
	model := strings.TrimSpace(req.Model)
	if model == "" {
		return ""
	}
	if prefix := providerPrefix(model); prefix != "" {
		req.Model = strings.TrimPrefix(model, prefix+"/")
		return prefix
	}
	if s != nil && s.crossProviderModelPool.Load() {
		return ""
	}
	return "qoder"
}

// applyPinnedProviderFilter lets an explicit account pin select its provider
// family for bare model IDs. Prefixed model IDs keep their forced family so a
// mismatched pin still fails closed inside PickRoute.
func (s *Server) applyPinnedProviderFilter(providerFilter, publicModel, prefer string) string {
	if prefer == "" || s == nil || s.pool == nil {
		return providerFilter
	}
	if providerPrefix(publicModel) != "" {
		return providerFilter
	}
	item, ok := s.pool.ByID(prefer)
	if !ok {
		return providerFilter
	}
	pinned := accounts.NormalizeProviderFamily(item.Provider)
	if providerFilter == "" || providerFilter == pinned {
		return providerFilter
	}
	return pinned
}

// filterModelsForIdentity narrows a model catalog to what the request's
// identity may actually call. /v1/models answers "can this key call this
// public model": a merged entry stays when the key's grants cover the
// provider family and at least one of the regions that serve it. Console
// /api/models rows are already one region each and use the same check via
// the singular region field. Unrestricted identities (empty allowlist) keep
// every entry. This filter runs after the /api/models cache lookup and must
// not be folded into the cached snapshot — the snapshot is shared across keys.
func (s *Server) filterModelsForIdentity(r *http.Request, models []map[string]any) []map[string]any {
	identity := s.requestIdentity(r)
	if len(identity.AllowedProviders) == 0 {
		return models
	}
	filtered := make([]map[string]any, 0, len(models))
	for _, model := range models {
		provider, _ := model["provider"].(string)
		if provider == "" {
			provider, _ = model["owned_by"].(string)
		}
		if !identity.AllowsProvider(provider) {
			continue
		}
		if !identityAllowsAnyModelRegion(identity, model) {
			continue
		}
		filtered = append(filtered, model)
	}
	return filtered
}

// identityAllowsAnyModelRegion checks a catalog entry's region set against
// the identity's grants. Merged /v1 entries expose regions[]; expanded
// console entries expose a singular region. Entries without region
// information (e.g. a legacy single-worker catalog) fall back to the
// family-level decision.
func identityAllowsAnyModelRegion(identity auth.Identity, model map[string]any) bool {
	regions := entryModelRegions(model)
	if len(regions) == 0 {
		return true
	}
	provider, _ := model["provider"].(string)
	if provider == "" {
		provider, _ = model["owned_by"].(string)
	}
	for _, region := range regions {
		if identity.AllowsProviderRegion(provider, region) {
			return true
		}
	}
	return false
}
