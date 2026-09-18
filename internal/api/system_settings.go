package api

import (
	"context"

	"github.com/caigee-cmd/cli2api/internal/app"
)

const (
	crossProviderModelPoolSecret = "cross_provider_model_pool"
	routingStrategySecret        = "routing_strategy"
	proxyURLSecret               = "proxy_url"
)

func ensureProxyURL(ctx context.Context, store app.SecretStore, bootstrap string) (string, error) {
	return app.EnsureProxyURL(ctx, store, bootstrap)
}

func ensureCrossProviderModelPool(ctx context.Context, store app.SecretStore) (bool, error) {
	return app.EnsureCrossProviderModelPool(ctx, store)
}

func ensureRoutingStrategy(ctx context.Context, store app.SecretStore) (string, error) {
	return app.EnsureRoutingStrategy(ctx, store)
}

func ensureWorkBuddyCheckinTime(ctx context.Context, store app.SecretStore) (string, error) {
	return app.EnsureWorkBuddyCheckinTime(ctx, store)
}
