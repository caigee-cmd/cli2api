package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/caigee-cmd/cli2api/internal/accounts"
)

const crossProviderModelPoolSecret = "cross_provider_model_pool"

type systemSettings struct {
	CrossProviderModelPool bool `json:"cross_provider_model_pool"`
}

func ensureCrossProviderModelPool(ctx context.Context, store *accounts.Store) (bool, error) {
	value, ok, err := store.GetSecret(ctx, crossProviderModelPoolSecret)
	if err != nil {
		return false, err
	}
	if !ok || strings.TrimSpace(value) == "" {
		if err := store.SetSecret(ctx, crossProviderModelPoolSecret, "1"); err != nil {
			return false, fmt.Errorf("initialize system settings: %w", err)
		}
		return true, nil
	}

	enabled, err := parseSettingBool(value)
	if err != nil {
		return false, fmt.Errorf("invalid %s setting: %w", crossProviderModelPoolSecret, err)
	}
	return enabled, nil
}

func parseSettingBool(value string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "on", "yes":
		return true, nil
	case "0", "false", "off", "no":
		return false, nil
	default:
		return false, fmt.Errorf("expected 0 or 1, got %q", value)
	}
}

func (s *Server) handleSystemSettings(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, systemSettings{
			CrossProviderModelPool: s.crossProviderModelPool.Load(),
		})
	case http.MethodPatch:
		var input struct {
			CrossProviderModelPool *bool `json:"cross_provider_model_pool"`
		}
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			writeErr(w, http.StatusBadRequest, "invalid_request", err.Error())
			return
		}
		if input.CrossProviderModelPool == nil {
			writeErr(w, http.StatusBadRequest, "invalid_request", "cross_provider_model_pool is required")
			return
		}

		s.settingsMu.Lock()
		defer s.settingsMu.Unlock()
		enabled := *input.CrossProviderModelPool
		value := "0"
		if enabled {
			value = "1"
		}
		if err := s.manager.Store().SetSecret(r.Context(), crossProviderModelPoolSecret, value); err != nil {
			writeErr(w, http.StatusInternalServerError, "system_settings_save_failed", err.Error())
			return
		}
		s.crossProviderModelPool.Store(enabled)
		writeJSON(w, http.StatusOK, systemSettings{CrossProviderModelPool: enabled})
	default:
		writeErr(w, http.StatusMethodNotAllowed, "method_not_allowed", "GET or PATCH only")
	}
}
