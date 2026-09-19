package app

import (
	"context"
	"net/http"

	"github.com/caigee-cmd/cli2api/internal/control"
	apigateway "github.com/caigee-cmd/cli2api/internal/gateway"
)

func (a *App) newGateway() *apigateway.Handler {
	if a == nil {
		return &apigateway.Handler{}
	}
	h := &apigateway.Handler{
		Executor:          a.Executor,
		Pool:              a.Pool,
		Recorder:          a.Recorder,
		CrossProviderPool: &a.CrossProviderModelPool,
		RequestedAccount:  a.RequestedAccount,
		FilterModels:      a.filterModelsForIdentity,
		DecorateModels: func(r *http.Request, models []map[string]any) []map[string]any {
			ctx := context.Background()
			if r != nil {
				ctx = r.Context()
			}
			return a.decorateModelsWithContext(ctx, models)
		},
		Models: func(refresh bool, accountID string) ([]map[string]any, error) {
			return a.fetchDisplayModels(refresh, accountID, control.CatalogModeMerge)
		},
	}
	if a.Control != nil {
		h.ModelContexts = a.Control.Settings
	}
	if a.Manager != nil {
		h.Catalogs = a.Manager
	}
	if a.Recorder != nil {
		h.Logs = a.Recorder
	}
	return h
}

func (a *App) gatewayHandler() *apigateway.Handler {
	if a == nil {
		return &apigateway.Handler{}
	}
	if a.Gateway == nil {
		a.Gateway = a.newGateway()
	}
	return a.Gateway
}
