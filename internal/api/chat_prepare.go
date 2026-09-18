package api

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

func (s *Server) rejectsBareModel(model string) bool {
	poolOn := s != nil && s.crossProviderModelPool.Load()
	return executor.RejectsBareModel(model, poolOn)
}

func (s *Server) resolveProviderFilter(req *translate.ChatRequest) string {
	poolOn := s != nil && s.crossProviderModelPool.Load()
	return executor.ResolveProviderFilter(req, poolOn)
}

func (s *Server) applyPinnedProviderFilter(providerFilter, publicModel, prefer string) string {
	if s == nil {
		return providerFilter
	}
	return executor.ApplyPinnedProviderFilter(s.pool, providerFilter, publicModel, prefer)
}

func (s *Server) applyModelContextDefaults(ctx context.Context, req *translate.ChatRequest, providerFilter string) error {
	var store executor.ModelContextStore
	if s != nil && s.control != nil {
		store = s.control.Settings
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
