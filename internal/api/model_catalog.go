package api

import (
	"net/http"

	"github.com/caigee-cmd/cli2api/internal/auth"
	"github.com/caigee-cmd/cli2api/internal/control"
	"github.com/caigee-cmd/cli2api/internal/executor"
)

func (s *Server) handleModels(w http.ResponseWriter, r *http.Request) {
	models, err := s.fetchDisplayModels(false, "", control.CatalogModeMerge)
	if err != nil {
		models = nil
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"object": "list",
		"data":   s.decorateModelsWithContext(r.Context(), s.filterModelsForIdentity(r, models)),
	})
}

func providerPrefix(model string) string {
	return executor.ProviderPrefix(model)
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
