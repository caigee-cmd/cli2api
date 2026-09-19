package app

import (
	"context"
	"github.com/caigee-cmd/cli2api/internal/control"
)

func (a *App) decorateModelsWithContext(ctx context.Context, models []map[string]any) []map[string]any {
	var settings *control.Settings
	if a.Control != nil {
		settings = a.Control.Settings
	}
	return control.DecorateModelsWithContext(ctx, settings, models)
}
