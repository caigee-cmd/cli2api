package app

import (
	"context"

	"github.com/caigee-cmd/cli2api/internal/executor"
	"github.com/caigee-cmd/cli2api/internal/translate"
)

func (a *App) ApplyModelContextDefaults(ctx context.Context, req *translate.ChatRequest, providerFilter string) error {
	var store executor.ModelContextStore
	if a != nil && a.Control != nil {
		store = a.Control.Settings
	}
	return executor.ApplyModelContextDefaults(ctx, store, req, providerFilter)
}
