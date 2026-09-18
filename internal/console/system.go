package console

import (
	"context"
	"encoding/json"
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

func (h *Handler) currentSystemSettings() systemSettings {
	var proxyURL string
	checkin := ""
	if h.Control != nil && h.Control.Settings != nil {
		proxyURL, _, _ = h.Control.Settings.GetSecret(context.Background(), proxyURLSecret)
		checkin = h.Control.Settings.WorkBuddyCheckinTimeDefault(context.Background())
	}
	settings := systemSettings{
		CrossProviderModelPool: h.crossProviderPoolOn(),
		ProxyURL:               proxy.Redact(proxyURL),
		WorkBuddyCheckinTime:   checkin,
	}
	if h.Pool != nil {
		settings.RoutingStrategy = h.Pool.RoutingStrategy()
	}
	if h.Executor != nil && h.Executor.SessionAffinity != nil {
		settings.SessionAffinity = h.Executor.SessionAffinity.Stats()
	}
	return settings
}

func (h *Handler) HandleSystemSettings(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, h.currentSystemSettings())
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

		if h.SettingsMu != nil {
			h.SettingsMu.Lock()
			defer h.SettingsMu.Unlock()
		}
		if input.ProxyURL != nil {
			// Read the persisted value, resolve redacted re-submits, validate,
			// and compare inside the same critical section that saves and
			// reloads. Doing the read outside the lock would let two concurrent
			// PATCHes interleave: a request submitting the old value could
			// compute proxyChanged against a stale read and then skip the write
			// while switching the runtime back to the old proxy, leaving the
			// database and the running workers disagreeing.
			existing, _, err := h.Control.Settings.GetSecret(r.Context(), proxyURLSecret)
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
				if err := h.Control.Settings.SetSecretOrEmpty(r.Context(), proxyURLSecret, proxyURL); err != nil {
					writeErr(w, http.StatusInternalServerError, "system_settings_save_failed", err.Error())
					return
				}
			}
			if err := h.Control.Accounts.ReloadProxyURL(r.Context(), proxyURL); err != nil {
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
			if err := h.Control.Settings.SetSecret(r.Context(), crossProviderModelPoolSecret, value); err != nil {
				writeErr(w, http.StatusInternalServerError, "system_settings_save_failed", err.Error())
				return
			}
			h.CrossProviderPool.Store(enabled)
		}
		if input.RoutingStrategy != nil {
			if err := h.Control.Settings.SetSecret(r.Context(), routingStrategySecret, strategy); err != nil {
				writeErr(w, http.StatusInternalServerError, "system_settings_save_failed", err.Error())
				return
			}
			h.Pool.SetRoutingStrategy(strategy)
		}
		if input.WorkBuddyCheckinTime != nil {
			if err := h.Control.Settings.SetSecret(r.Context(), accounts.WorkBuddyCheckinTimeSecret, checkinTime); err != nil {
				writeErr(w, http.StatusInternalServerError, "system_settings_save_failed", err.Error())
				return
			}
		}
		writeJSON(w, http.StatusOK, h.currentSystemSettings())
	default:
		writeErr(w, http.StatusMethodNotAllowed, "method_not_allowed", "GET or PATCH only")
	}
}
