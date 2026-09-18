package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/caigee-cmd/cli2api/internal/accounts"
	"github.com/caigee-cmd/cli2api/internal/executor"
	"github.com/caigee-cmd/cli2api/internal/proxy"
)

const (
	crossProviderModelPoolSecret = "cross_provider_model_pool"
	routingStrategySecret        = "routing_strategy"
	proxyURLSecret               = "proxy_url"
)

type systemSettings struct {
	CrossProviderModelPool bool                          `json:"cross_provider_model_pool"`
	RoutingStrategy        string                        `json:"routing_strategy"`
	ProxyURL               string                        `json:"proxy_url"`
	WorkBuddyCheckinTime   string                        `json:"workbuddy_checkin_time"`
	SessionAffinity        executor.SessionAffinityStats `json:"session_affinity"`
}

func ensureProxyURL(ctx context.Context, store secretStore, bootstrap string) (string, error) {
	value, ok, err := store.GetSecret(ctx, proxyURLSecret)
	if err != nil {
		return "", err
	}
	if !ok {
		value = strings.TrimSpace(bootstrap)
		if value != "" {
			if err := proxy.ValidateHTTPOnly(value); err != nil {
				return "", fmt.Errorf("invalid %s setting: %w", proxyURLSecret, err)
			}
		}
		if value != "" {
			if err := store.SetSecret(ctx, proxyURLSecret, value); err != nil {
				return "", fmt.Errorf("initialize system settings: %w", err)
			}
		}
	}
	if err := proxy.ValidateHTTPOnly(value); err != nil {
		return "", fmt.Errorf("invalid %s setting: %w", proxyURLSecret, err)
	}
	return strings.TrimSpace(value), nil
}

func ensureCrossProviderModelPool(ctx context.Context, store secretStore) (bool, error) {
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

func ensureRoutingStrategy(ctx context.Context, store secretStore) (string, error) {
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

func ensureWorkBuddyCheckinTime(ctx context.Context, store secretStore) (string, error) {
	value, ok, err := store.GetSecret(ctx, accounts.WorkBuddyCheckinTimeSecret)
	if err != nil {
		return "", err
	}
	if !ok || strings.TrimSpace(value) == "" {
		value = accounts.DefaultWorkBuddyCheckinTime
		if err := store.SetSecret(ctx, accounts.WorkBuddyCheckinTimeSecret, value); err != nil {
			return "", fmt.Errorf("initialize workbuddy check-in time: %w", err)
		}
	}
	normalized, err := accounts.NormalizeWorkBuddyCheckinTime(value)
	if err != nil {
		return "", fmt.Errorf("invalid %s setting: %w", accounts.WorkBuddyCheckinTimeSecret, err)
	}
	if normalized != value {
		if err := store.SetSecret(ctx, accounts.WorkBuddyCheckinTimeSecret, normalized); err != nil {
			return "", fmt.Errorf("initialize workbuddy check-in time: %w", err)
		}
	}
	return normalized, nil
}

func (s *Server) currentSystemSettings() systemSettings {
	proxyURL, _, _ := s.control.Settings.GetSecret(context.Background(), proxyURLSecret)
	return systemSettings{
		CrossProviderModelPool: s.crossProviderModelPool.Load(),
		RoutingStrategy:        s.pool.RoutingStrategy(),
		ProxyURL:               proxy.Redact(proxyURL),
		WorkBuddyCheckinTime:   s.control.Settings.WorkBuddyCheckinTimeDefault(context.Background()),
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
			ProxyURL               *string `json:"proxy_url"`
			WorkBuddyCheckinTime   *string `json:"workbuddy_checkin_time"`
		}
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			writeErr(w, http.StatusBadRequest, "invalid_request", err.Error())
			return
		}
		if input.CrossProviderModelPool == nil && input.RoutingStrategy == nil && input.ProxyURL == nil && input.WorkBuddyCheckinTime == nil {
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
		var checkinTime string
		if input.WorkBuddyCheckinTime != nil {
			normalized, err := accounts.NormalizeWorkBuddyCheckinTime(*input.WorkBuddyCheckinTime)
			if err != nil || strings.TrimSpace(*input.WorkBuddyCheckinTime) == "" {
				writeErr(w, http.StatusBadRequest, "invalid_workbuddy_checkin_time", "workbuddy_checkin_time must use HH:mm")
				return
			}
			checkinTime = normalized
		}

		s.settingsMu.Lock()
		defer s.settingsMu.Unlock()
		if input.ProxyURL != nil {
			// Read the persisted value, resolve redacted re-submits, validate,
			// and compare inside the same critical section that saves and
			// reloads. Doing the read outside the lock would let two concurrent
			// PATCHes interleave: a request submitting the old value could
			// compute proxyChanged against a stale read and then skip the write
			// while switching the runtime back to the old proxy, leaving the
			// database and the running workers disagreeing.
			existing, _, err := s.control.Settings.GetSecret(r.Context(), proxyURLSecret)
			if err != nil {
				writeErr(w, http.StatusInternalServerError, "system_settings_read_failed", err.Error())
				return
			}
			proxyURL := proxy.Preserve(existing, *input.ProxyURL)
			if err := proxy.ValidateHTTPOnly(proxyURL); err != nil {
				writeErr(w, http.StatusBadRequest, "invalid_proxy_url", err.Error())
				return
			}
			proxyChanged := proxyURL != strings.TrimSpace(existing)

			// Persist clears as an explicit empty value (not a delete) so the
			// next boot distinguishes "user cleared it" from "never set" and
			// does not re-apply the environment bootstrap. An unchanged value
			// skips the write, but we still call ReloadProxyURL: whether the
			// workers actually need restarting is the Manager's call, which
			// knows if a previous reload failed.
			if proxyChanged {
				if err := s.control.Settings.SetSecretOrEmpty(r.Context(), proxyURLSecret, proxyURL); err != nil {
					writeErr(w, http.StatusInternalServerError, "system_settings_save_failed", err.Error())
					return
				}
			}
			if err := s.control.Accounts.ReloadProxyURL(r.Context(), proxyURL); err != nil {
				writeErr(w, http.StatusInternalServerError, "proxy_reload_failed", err.Error())
				return
			}
		}
		if input.CrossProviderModelPool != nil {
			enabled := *input.CrossProviderModelPool
			value := "0"
			if enabled {
				value = "1"
			}
			if err := s.control.Settings.SetSecret(r.Context(), crossProviderModelPoolSecret, value); err != nil {
				writeErr(w, http.StatusInternalServerError, "system_settings_save_failed", err.Error())
				return
			}
			s.crossProviderModelPool.Store(enabled)
		}
		if input.RoutingStrategy != nil {
			if err := s.control.Settings.SetSecret(r.Context(), routingStrategySecret, strategy); err != nil {
				writeErr(w, http.StatusInternalServerError, "system_settings_save_failed", err.Error())
				return
			}
			s.pool.SetRoutingStrategy(strategy)
		}
		if input.WorkBuddyCheckinTime != nil {
			if err := s.control.Settings.SetSecret(r.Context(), accounts.WorkBuddyCheckinTimeSecret, checkinTime); err != nil {
				writeErr(w, http.StatusInternalServerError, "system_settings_save_failed", err.Error())
				return
			}
		}
		writeJSON(w, http.StatusOK, s.currentSystemSettings())
	default:
		writeErr(w, http.StatusMethodNotAllowed, "method_not_allowed", "GET or PATCH only")
	}
}
