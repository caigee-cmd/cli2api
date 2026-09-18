package api

import (
	"context"
	"net/http"

	"github.com/caigee-cmd/cli2api/internal/control"
	apigateway "github.com/caigee-cmd/cli2api/internal/gateway"
	"github.com/caigee-cmd/cli2api/internal/translate"
)

func (s *Server) newGateway() *apigateway.Handler {
	if s == nil {
		return &apigateway.Handler{}
	}
	h := &apigateway.Handler{
		Executor:          s.executor,
		Recorder:          s.recorder,
		Pool:              s.pool,
		CrossProviderPool: &s.crossProviderModelPool,
		RequestedAccount:  s.requestedAccount,
		FilterModels:      s.filterModelsForIdentity,
		DecorateModels: func(r *http.Request, models []map[string]any) []map[string]any {
			ctx := context.Background()
			if r != nil {
				ctx = r.Context()
			}
			return s.decorateModelsWithContext(ctx, models)
		},
		Models: func(refresh bool, accountID string) ([]map[string]any, error) {
			return s.fetchDisplayModels(refresh, accountID, control.CatalogModeMerge)
		},
	}
	if s.control != nil {
		h.ModelContexts = s.control.Settings
	}
	if s.manager != nil {
		h.Catalogs = s.manager
	}
	if s.recorder != nil {
		h.Logs = s.recorder
	}
	return h
}

func (s *Server) gatewayHandler() *apigateway.Handler {
	if s == nil {
		return &apigateway.Handler{}
	}
	if s.gateway == nil {
		s.gateway = s.newGateway()
	}
	return s.gateway
}

func (s *Server) handleAnthropicMessages(w http.ResponseWriter, r *http.Request) {
	s.gatewayHandler().HandleAnthropicMessages(w, r)
}

func (s *Server) handleResponses(w http.ResponseWriter, r *http.Request) {
	s.gatewayHandler().HandleResponses(w, r)
}

func (s *Server) handleModels(w http.ResponseWriter, r *http.Request) {
	s.gatewayHandler().HandleModels(w, r)
}

func (s *Server) prepareChatExecution(r *http.Request, request translate.ChatRequest) (chatExecution, error) {
	return s.gatewayHandler().PrepareChatExecution(r, request)
}

func (s *Server) prepareCompatibilityExecution(r *http.Request, request translate.ChatRequest) (compatibilityExecution, error) {
	return s.gatewayHandler().PrepareCompatibilityExecution(r, request)
}
