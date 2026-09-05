package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/caigee-cmd/cli2api/internal/accounts"
	"github.com/caigee-cmd/cli2api/internal/executor"
)

const (
	crossProviderModelPoolSecret = "cross_provider_model_pool"
	routingStrategySecret        = "routing_strategy"
)

type systemSettings struct {
	CrossProviderModelPool bool                          `json:"cross_provider_model_pool"`
	RoutingStrategy        string                        `json:"routing_strategy"`
	SessionAffinity        executor.SessionAffinityStats `json:"session_affinity"`
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

func ensureRoutingStrategy(ctx context.Context, store *accounts.Store) (string, error) {
	value, ok, err := store.GetSecret(ctx, routingStrategySecret)
	if err != nil {
		return "", err
	}
	if !ok || strings.TrimSpace(value) == "" {
		value = accounts.RoutingStrategyRoundRobin
		if err := store.SetSecret(ctx, routingStrategySecret, value); err != nil {
			return "", fmt.Errorf("initialize routing strategy: %w", err)
		}
	}
	return accounts.NormalizeRoutingStrategy(value), nil
}

func (s *Server) currentSystemSettings() systemSettings {
	return systemSettings{
		CrossProviderModelPool: s.crossProviderModelPool.Load(),
		RoutingStrategy:        s.pool.RoutingStrategy(),
		SessionAffinity:        s.executor.SessionAffinity.Stats(),
	}
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
		writeJSON(w, http.StatusOK, s.currentSystemSettings())
	case http.MethodPatch:
		var input struct {
			CrossProviderModelPool *bool   `json:"cross_provider_model_pool"`
			RoutingStrategy        *string `json:"routing_strategy"`
		}
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			writeErr(w, http.StatusBadRequest, "invalid_request", err.Error())
			return
		}
		if input.CrossProviderModelPool == nil && input.RoutingStrategy == nil {
			writeErr(w, http.StatusBadRequest, "invalid_request", "a system setting is required")
			return
		}
		var strategy string
		if input.RoutingStrategy != nil {
			rawStrategy := strings.ToLower(strings.TrimSpace(*input.RoutingStrategy))
			if rawStrategy != accounts.RoutingStrategyRoundRobin && rawStrategy != accounts.RoutingStrategyWeightedRoundRobin && rawStrategy != accounts.RoutingStrategyFillFirst {
				writeErr(w, http.StatusBadRequest, "invalid_routing_strategy", "routing_strategy must be round-robin, weighted-round-robin, or fill-first")
				return
			}
			strategy = accounts.NormalizeRoutingStrategy(rawStrategy)
		}

		s.settingsMu.Lock()
		defer s.settingsMu.Unlock()
		if input.CrossProviderModelPool != nil {
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
		}
		if input.RoutingStrategy != nil {
			if err := s.manager.Store().SetSecret(r.Context(), routingStrategySecret, strategy); err != nil {
				writeErr(w, http.StatusInternalServerError, "system_settings_save_failed", err.Error())
				return
			}
			s.pool.SetRoutingStrategy(strategy)
		}
		writeJSON(w, http.StatusOK, s.currentSystemSettings())
	default:
		writeErr(w, http.StatusMethodNotAllowed, "method_not_allowed", "GET or PATCH only")
	}
}
