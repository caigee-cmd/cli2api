package console

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/caigee-cmd/cli2api/internal/accounts"
	appsvc "github.com/caigee-cmd/cli2api/internal/control"
)

type providerPolicyInput struct {
	Provider         string            `json:"provider"`
	Region           string            `json:"region"`
	CreditFloor      *float64          `json:"credit_floor"`
	PercentFloor     *float64          `json:"percent_floor"`
	ProxyURL         string            `json:"proxy_url"`
	DropSystemPrompt *bool             `json:"drop_system_prompt"`
	ModelAliases     map[string]string `json:"model_aliases"`
	EnabledModels    []string          `json:"enabled_models"`
	ModelsRestricted bool              `json:"models_restricted"`
	CheckinTime      string            `json:"checkin_time"`
	CheckinAuto      *bool             `json:"checkin_auto"`
}

func (input providerPolicyInput) policy() accounts.ProviderPolicy {
	return accounts.ProviderPolicy{
		Provider:         input.Provider,
		Region:           input.Region,
		CreditFloor:      input.CreditFloor,
		PercentFloor:     input.PercentFloor,
		ProxyURL:         input.ProxyURL,
		DropSystemPrompt: input.DropSystemPrompt,
		ModelAliases:     input.ModelAliases,
		EnabledModels:    input.EnabledModels,
		ModelsRestricted: input.ModelsRestricted,
		CheckinTime:      input.CheckinTime,
		CheckinAuto:      input.CheckinAuto,
	}
}

// HandleProviderSettings reads and replaces provider-wide routing settings.
// Saving persists the policy and refreshes matching live accounts; it never
// rewrites account rows.
func (h *Handler) HandleProviderSettings(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.Control == nil || h.Control.Provider == nil {
		writeErr(w, http.StatusServiceUnavailable, "provider_settings_unavailable", "provider settings are not configured")
		return
	}
	switch r.Method {
	case http.MethodGet:
		list, err := h.Control.Provider.List(r.Context())
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "provider_settings_failed", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"data": list})
	case http.MethodPatch:
		var input providerPolicyInput
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxConsoleBodyBytes)).Decode(&input); err != nil {
			writeErr(w, http.StatusBadRequest, "invalid_request", err.Error())
			return
		}
		policy, err := h.Control.Provider.Update(r.Context(), input.policy())
		if err != nil {
			writeErr(w, http.StatusBadRequest, "invalid_provider_settings", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"data": policy})
	default:
		writeErr(w, http.StatusMethodNotAllowed, "method_not_allowed", "GET or PATCH only")
	}
}

// HandleProviderCatalog returns the full model catalog for one provider
// region, bypassing account-level filters so a newly restricted provider can
// still be edited back open.
func (h *Handler) HandleProviderCatalog(w http.ResponseWriter, r *http.Request) {
	if h == nil {
		writeErr(w, http.StatusServiceUnavailable, "catalog_unavailable", "model catalog is not configured")
		return
	}
	provider := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("provider")))
	region := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("region")))
	if provider == "" {
		writeErr(w, http.StatusBadRequest, "invalid_provider", "provider is required")
		return
	}
	models, err := h.fetchDisplayModels(false, "", appsvc.CatalogModeExpand)
	if err != nil {
		writeErr(w, http.StatusServiceUnavailable, "catalog_failed", err.Error())
		return
	}
	type catalogEntry struct {
		ID          string `json:"id"`
		DisplayName string `json:"display_name,omitempty"`
	}
	data := make([]catalogEntry, 0, len(models))
	seen := map[string]struct{}{}
	for _, model := range models {
		if catalogProvider(model) != provider {
			continue
		}
		if region != "" && !catalogRegionMatches(model, region) {
			continue
		}
		id := textValue(model["id"])
		if id == "" {
			continue
		}
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		data = append(data, catalogEntry{ID: id, DisplayName: textValue(model["display_name"])})
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": data})
}

func catalogProvider(model map[string]any) string {
	provider := strings.ToLower(strings.TrimSpace(textValue(model["provider"])))
	if provider == "" {
		provider = strings.ToLower(strings.TrimSpace(textValue(model["owned_by"])))
	}
	return provider
}

func catalogRegionMatches(model map[string]any, region string) bool {
	value := strings.ToLower(strings.TrimSpace(textValue(model["region"])))
	if value != "" {
		return value == region
	}
	switch regions := model["regions"].(type) {
	case []any:
		for _, item := range regions {
			if strings.EqualFold(textValue(item), region) {
				return true
			}
		}
	case []string:
		for _, item := range regions {
			if strings.EqualFold(item, region) {
				return true
			}
		}
	}
	return false
}

func textValue(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case fmt.Stringer:
		return typed.String()
	case nil:
		return ""
	default:
		return fmt.Sprint(typed)
	}
}
