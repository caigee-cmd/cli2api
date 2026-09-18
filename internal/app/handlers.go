package app

import (
	"context"

	appconsole "github.com/caigee-cmd/cli2api/internal/console"
	"github.com/caigee-cmd/cli2api/internal/control"
	appupdate "github.com/caigee-cmd/cli2api/internal/update"
)

func (a *App) newUpdateCoordinator(checker appupdate.ReleaseChecker, agent appupdate.Agent) *appupdate.Coordinator {
	if a == nil {
		return &appupdate.Coordinator{}
	}
	coord := &appupdate.Coordinator{
		Checker: checker,
		Agent:   agent,
		DataDir: a.Cfg.DataDir,
	}
	if a.Control != nil && a.Control.Backup != nil {
		coord.Backup = a.Control.Backup.Snapshot
	}
	return coord
}

func (a *App) newConsole() *appconsole.Handler {
	if a == nil {
		return &appconsole.Handler{}
	}
	h := &appconsole.Handler{
		Control:           a.Control,
		Cfg:               &a.Cfg,
		Executor:          &a.Executor,
		Pool:              a.Pool,
		Providers:         a.Providers,
		Recorder:          a.Recorder,
		Ring:              a.Ring,
		CrossProviderPool: &a.CrossProviderModelPool,
		SettingsMu:        &a.SettingsMu,
		RequestedAccount:  a.RequestedAccount,
		FilterModels:      a.filterModelsForIdentity,
		FetchWorkerModels: a.fetchWorkerModels,
		FetchDisplayModels: func(refresh bool, accountID string, mode control.CatalogMode) ([]map[string]any, error) {
			return a.fetchDisplayModels(refresh, accountID, mode)
		},
		ProxyAccountWorker:  a.proxyAccountWorker,
		GenerateAPIKey:      GenerateAPIKey,
		ConsoleKey:          a.Auth.ConsoleKey,
		OnConsoleKeyRotated: a.Auth.SetConsoleKey,
		DecorateModels: func(ctx context.Context, models []map[string]any) []map[string]any {
			return a.decorateModelsWithContext(ctx, models)
		},
		Chat: a.gatewayHandler().HandleChatCompletions,
	}
	return h
}
