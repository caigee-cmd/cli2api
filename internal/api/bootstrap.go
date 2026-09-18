package api

import (
	"context"

	"github.com/caigee-cmd/cli2api/internal/app"
)

func ensureProxyAPIKey(ctx context.Context, store app.SecretStore, bootstrap string) (string, bool, error) {
	return app.EnsureProxyAPIKey(ctx, store, bootstrap)
}

func generateAPIKey() (string, error) {
	return app.GenerateAPIKey()
}
