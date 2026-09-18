package api

import (
	"context"
	"net/http"
	"time"

	"github.com/caigee-cmd/cli2api/internal/app"
	"github.com/caigee-cmd/cli2api/internal/auth"
	appconsole "github.com/caigee-cmd/cli2api/internal/console"
	"github.com/caigee-cmd/cli2api/internal/control"
	"github.com/caigee-cmd/cli2api/internal/executor"
	apigateway "github.com/caigee-cmd/cli2api/internal/gateway"
	"github.com/caigee-cmd/cli2api/internal/translate"
	appupdate "github.com/caigee-cmd/cli2api/internal/update"
)

type chatExecution = apigateway.Execution
type compatibilityExecution = apigateway.Execution
type systemUpdateJob = appupdate.Job
type catalogMode = control.CatalogMode

const (
	catalogModeMerge  = control.CatalogModeMerge
	catalogModeExpand = control.CatalogModeExpand
)

func parseQueryTime(raw string, endOfDay bool) *time.Time {
	return appconsole.ParseQueryTime(raw, endOfDay)
}

func (s *Server) updater() *appupdate.Coordinator {
	if s == nil || s.App == nil {
		return &appupdate.Coordinator{}
	}
	return s.App.SyncUpdate()
}

func (s *Server) consoleHandler() *appconsole.Handler {
	if s == nil || s.App == nil || s.Console == nil {
		return &appconsole.Handler{}
	}
	return s.Console
}

func (s *Server) snapshotUpdateJob() *appupdate.Job {
	return s.updater().Snapshot()
}

func (s *Server) finishUpdateJob(jobID, state, message string, finished bool) {
	s.updater().Finish(jobID, state, message, finished)
}

func (s *Server) prepareChatExecution(r *http.Request, request translate.ChatRequest) (chatExecution, error) {
	return s.ensureGateway().PrepareChatExecution(r, request)
}

func (s *Server) prepareCompatibilityExecution(r *http.Request, request translate.ChatRequest) (compatibilityExecution, error) {
	return s.ensureGateway().PrepareCompatibilityExecution(r, request)
}

func (s *Server) rejectsBareModel(model string) bool {
	poolOn := false
	if s != nil && s.App != nil {
		poolOn = s.CrossProviderModelPool.Load()
	}
	return executor.RejectsBareModel(model, poolOn)
}

func (s *Server) resolveProviderFilter(req *translate.ChatRequest) string {
	poolOn := false
	if s != nil && s.App != nil {
		poolOn = s.CrossProviderModelPool.Load()
	}
	return executor.ResolveProviderFilter(req, poolOn)
}

func (s *Server) applyPinnedProviderFilter(providerFilter, publicModel, prefer string) string {
	if s == nil || s.App == nil {
		return providerFilter
	}
	return executor.ApplyPinnedProviderFilter(s.Pool, providerFilter, publicModel, prefer)
}

func (s *Server) applyModelContextDefaults(ctx context.Context, req *translate.ChatRequest, providerFilter string) error {
	if s == nil || s.App == nil || s.Control == nil {
		return nil
	}
	return executor.ApplyModelContextDefaults(ctx, s.Control.Settings, req, providerFilter)
}

func (s *Server) fetchWorkerModelsFor(refresh bool, accountID string) ([]map[string]any, error) {
	if s == nil || s.App == nil {
		return nil, nil
	}
	return s.FetchWorkerModelsFor(refresh, accountID)
}

func (s *Server) fetchWorkerModelsForMode(refresh bool, accountID string, mode catalogMode) ([]map[string]any, error) {
	if s == nil || s.App == nil {
		return nil, nil
	}
	return s.FetchWorkerModelsForMode(refresh, accountID, mode)
}

func (s *Server) ensureApp() *app.App {
	if s == nil {
		return nil
	}
	if s.App == nil {
		s.App = &app.App{}
	}
	return s.App
}

func (s *Server) ensureGateway() *apigateway.Handler {
	application := s.ensureApp()
	if application == nil {
		return &apigateway.Handler{}
	}
	if application.Gateway == nil {
		application.Gateway = &apigateway.Handler{
			Executor:          application.Executor,
			Recorder:          application.Recorder,
			Pool:              application.Pool,
			CrossProviderPool: &application.CrossProviderModelPool,
			RequestedAccount:  application.RequestedAccount,
		}
		if application.Control != nil {
			application.Gateway.ModelContexts = application.Control.Settings
		}
		if application.Manager != nil {
			application.Gateway.Catalogs = application.Manager
		}
		if application.Recorder != nil {
			application.Gateway.Logs = application.Recorder
		}
	}
	return application.Gateway
}

func (s *Server) handleChatCompletions(w http.ResponseWriter, r *http.Request) {
	s.ensureGateway().HandleChatCompletions(w, r)
}

func (s *Server) handleAnthropicMessages(w http.ResponseWriter, r *http.Request) {
	s.ensureGateway().HandleAnthropicMessages(w, r)
}

func (s *Server) handleResponses(w http.ResponseWriter, r *http.Request) {
	s.ensureGateway().HandleResponses(w, r)
}

func requestSessionKey(r *http.Request, identity auth.Identity, req translate.ChatRequest) string {
	header := ""
	if r != nil {
		header = r.Header.Get("X-CLI2API-Session")
	}
	return executor.SessionKeyFor(header, identity, req)
}

func waitForWorkerAuthManager(ctx context.Context, lookup func() (string, bool), timeout, interval time.Duration) (string, error) {
	return app.WaitForWorkerAuthManager(ctx, lookup, timeout, interval)
}

func entryModelRegions(entry map[string]any) []string {
	return app.EntryModelRegions(entry)
}

var errWorkerNotWarm = app.ErrWorkerNotWarm
