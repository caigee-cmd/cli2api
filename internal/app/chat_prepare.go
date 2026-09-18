package app

import (
	"context"
	"net/http"

	"github.com/caigee-cmd/cli2api/internal/auth"
	"github.com/caigee-cmd/cli2api/internal/executor"
	apigateway "github.com/caigee-cmd/cli2api/internal/gateway"
	"github.com/caigee-cmd/cli2api/internal/translate"
)

type chatHTTPError = executor.PrepareError
type chatExecution = apigateway.Execution
type compatibilityExecution = apigateway.Execution

func (a *App) rejectsBareModel(model string) bool {
	poolOn := a != nil && a.CrossProviderModelPool.Load()
	return executor.RejectsBareModel(model, poolOn)
}

func (a *App) resolveProviderFilter(req *translate.ChatRequest) string {
	poolOn := a != nil && a.CrossProviderModelPool.Load()
	return executor.ResolveProviderFilter(req, poolOn)
}

func (a *App) applyPinnedProviderFilter(providerFilter, publicModel, prefer string) string {
	if a == nil {
		return providerFilter
	}
	return executor.ApplyPinnedProviderFilter(a.Pool, providerFilter, publicModel, prefer)
}

func (a *App) ApplyModelContextDefaults(ctx context.Context, req *translate.ChatRequest, providerFilter string) error {
	var store executor.ModelContextStore
	if a != nil && a.Control != nil {
		store = a.Control.Settings
	}
	return executor.ApplyModelContextDefaults(ctx, store, req, providerFilter)
}

func requestSessionKey(r *http.Request, identity auth.Identity, req translate.ChatRequest) string {
	header := ""
	if r != nil {
		header = r.Header.Get("X-CLI2API-Session")
	}
	return executor.SessionKeyFor(header, identity, req)
}
