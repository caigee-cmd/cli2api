package api

import (
	"context"
	"net/http"

	"github.com/caigee-cmd/cli2api/internal/auth"
	appconsole "github.com/caigee-cmd/cli2api/internal/console"
	"github.com/caigee-cmd/cli2api/internal/control"
	appupdate "github.com/caigee-cmd/cli2api/internal/update"
)

func (s *Server) newUpdateCoordinator() *appupdate.Coordinator {
	if s == nil {
		return &appupdate.Coordinator{}
	}
	coord := &appupdate.Coordinator{
		Checker: s.updateChecker,
		Agent:   s.updateAgent,
		DataDir: s.cfg.DataDir,
	}
	if s.control != nil && s.control.Backup != nil {
		coord.Backup = s.control.Backup.Snapshot
	}
	return coord
}

func (s *Server) newConsole() *appconsole.Handler {
	if s == nil {
		return &appconsole.Handler{}
	}
	h := &appconsole.Handler{
		Control:           s.control,
		Cfg:               &s.cfg,
		Executor:          &s.executor,
		Pool:              s.pool,
		Providers:         s.providers,
		Recorder:          s.recorder,
		Ring:              s.ring,
		CrossProviderPool: &s.crossProviderModelPool,
		SettingsMu:        &s.settingsMu,
		RequestedAccount:  s.requestedAccount,
		FilterModels:      s.filterModelsForIdentity,
		FetchWorkerModels: s.fetchWorkerModels,
		FetchDisplayModels: func(refresh bool, accountID string, mode control.CatalogMode) ([]map[string]any, error) {
			return s.fetchDisplayModels(refresh, accountID, mode)
		},
		ProxyAccountWorker: s.proxyAccountWorker,
		GenerateAPIKey:     generateAPIKey,
		OnConsoleKeyRotated: func(secret string) {
			s.cfg.ProxyAPIKey = secret
			store := s.control.Accounts.Store()
			s.auth = auth.NewVerifier(secret, store)
			s.executor.WorkerKey = secret
		},
		DecorateModels: func(ctx context.Context, models []map[string]any) []map[string]any {
			return s.decorateModelsWithContext(ctx, models)
		},
		Chat: s.gatewayHandler().HandleChatCompletions,
	}
	return h
}

func (s *Server) consoleHandler() *appconsole.Handler {
	if s == nil {
		return &appconsole.Handler{}
	}
	if s.console == nil {
		s.console = s.newConsole()
	}
	if s.console.Update == nil {
		s.console.Update = s.updater()
	}
	if s.console.Chat == nil {
		s.console.Chat = s.gatewayHandler().HandleChatCompletions
	}
	return s.console
}

func (s *Server) updater() *appupdate.Coordinator {
	if s == nil {
		return &appupdate.Coordinator{}
	}
	if s.update == nil {
		s.update = s.newUpdateCoordinator()
	}
	s.update.Checker = s.updateChecker
	s.update.Agent = s.updateAgent
	if s.update.DataDir == "" {
		s.update.DataDir = s.cfg.DataDir
	}
	if s.update.Backup == nil && s.control != nil && s.control.Backup != nil {
		s.update.Backup = s.control.Backup.Snapshot
	}
	return s.update
}

func (s *Server) handleOverview(w http.ResponseWriter, r *http.Request) {
	s.consoleHandler().HandleOverview(w, r)
}

func (s *Server) handleOverviewSummary(w http.ResponseWriter, r *http.Request) {
	s.consoleHandler().HandleOverviewSummary(w, r)
}

func (s *Server) handleLogs(w http.ResponseWriter, r *http.Request) {
	s.consoleHandler().HandleLogs(w, r)
}

func (s *Server) handleProviders(w http.ResponseWriter, r *http.Request) {
	s.consoleHandler().HandleProviders(w, r)
}

func (s *Server) handleAccounts(w http.ResponseWriter, r *http.Request) {
	s.consoleHandler().HandleAccounts(w, r)
}

func (s *Server) handleAccountImport(w http.ResponseWriter, r *http.Request) {
	s.consoleHandler().HandleAccountImport(w, r)
}

func (s *Server) handleAccountByID(w http.ResponseWriter, r *http.Request) {
	s.consoleHandler().HandleAccountByID(w, r)
}

func (s *Server) handleAPIKeys(w http.ResponseWriter, r *http.Request) {
	s.consoleHandler().HandleAPIKeys(w, r)
}

func (s *Server) handleAPIKeyByID(w http.ResponseWriter, r *http.Request) {
	s.consoleHandler().HandleAPIKeyByID(w, r)
}

func (s *Server) handleConsoleKey(w http.ResponseWriter, r *http.Request) {
	s.consoleHandler().HandleConsoleKey(w, r)
}

func (s *Server) handleModelsAPI(w http.ResponseWriter, r *http.Request) {
	s.consoleHandler().HandleModelsAPI(w, r)
}

func (s *Server) handleModelSetting(w http.ResponseWriter, r *http.Request) {
	s.consoleHandler().HandleModelSetting(w, r)
}

func (s *Server) handleSystemSettings(w http.ResponseWriter, r *http.Request) {
	s.consoleHandler().HandleSystemSettings(w, r)
}

func (s *Server) handleSystemUpdate(w http.ResponseWriter, r *http.Request) {
	s.consoleHandler().HandleSystemUpdate(w, r)
}

func (s *Server) handleSystemUpdatePrepare(w http.ResponseWriter, r *http.Request) {
	s.consoleHandler().HandleSystemUpdatePrepare(w, r)
}

func (s *Server) handleSystemUpdateConfirm(w http.ResponseWriter, r *http.Request) {
	s.consoleHandler().HandleSystemUpdateConfirm(w, r)
}

func (s *Server) handleSystemUpdateCancel(w http.ResponseWriter, r *http.Request) {
	s.consoleHandler().HandleSystemUpdateCancel(w, r)
}

func (s *Server) handleSystemUpdateRollback(w http.ResponseWriter, r *http.Request) {
	s.consoleHandler().HandleSystemUpdateRollback(w, r)
}

func (s *Server) handleConsoleChat(w http.ResponseWriter, r *http.Request) {
	s.consoleHandler().HandleChat(w, r)
}

func (s *Server) handleChatCompletions(w http.ResponseWriter, r *http.Request) {
	s.gatewayHandler().HandleChatCompletions(w, r)
}

func (s *Server) snapshotUpdateJob() *appupdate.Job {
	return s.updater().Snapshot()
}

func (s *Server) finishUpdateJob(jobID, state, message string, finished bool) {
	s.updater().Finish(jobID, state, message, finished)
}
