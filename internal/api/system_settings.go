package api

import (
	"context"
	"fmt"
	"strings"

	"github.com/caigee-cmd/cli2api/internal/accounts"
	"github.com/caigee-cmd/cli2api/internal/proxy"
)

const (
	crossProviderModelPoolSecret = "cross_provider_model_pool"
	routingStrategySecret        = "routing_strategy"
	proxyURLSecret               = "proxy_url"
)

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
