package app

import (
	"context"

	"github.com/caigee-cmd/cli2api/internal/auth"
	appconsole "github.com/caigee-cmd/cli2api/internal/console"
	"github.com/caigee-cmd/cli2api/internal/control"
	appupdate "github.com/caigee-cmd/cli2api/internal/update"
)

func (a *App) newUpdateCoordinator() *appupdate.Coordinator {
	if a == nil {
		return &appupdate.Coordinator{}
	}
	coord := &appupdate.Coordinator{
		Checker: a.UpdateChecker,
		Agent:   a.UpdateAgent,
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
		ProxyAccountWorker: a.proxyAccountWorker,
		GenerateAPIKey:     GenerateAPIKey,
		OnConsoleKeyRotated: func(secret string) {
			a.Cfg.ProxyAPIKey = secret
			store := a.Control.Accounts.Store()
			a.Auth = auth.NewVerifier(secret, store)
			a.Executor.WorkerKey = secret
		},
		DecorateModels: func(ctx context.Context, models []map[string]any) []map[string]any {
			return a.decorateModelsWithContext(ctx, models)
		},
		Chat:             a.gatewayHandler().HandleChatCompletions,
		UpdateForRequest: a.SyncUpdate,
	}
	return h
}

func (a *App) consoleHandler() *appconsole.Handler {
	if a == nil {
		return &appconsole.Handler{}
	}
	if a.Console == nil {
		a.Console = a.newConsole()
	}
	if a.Console.Update == nil {
		a.Console.Update = a.updater()
	}
	if a.Console.Chat == nil {
		a.Console.Chat = a.gatewayHandler().HandleChatCompletions
	}
	return a.Console
}

func (a *App) updater() *appupdate.Coordinator {
	return a.SyncUpdate()
}

func (a *App) SyncUpdate() *appupdate.Coordinator {
	if a == nil {
		return &appupdate.Coordinator{}
	}
	if a.Update == nil {
		a.Update = a.newUpdateCoordinator()
	}
	a.Update.Checker = a.UpdateChecker
	a.Update.Agent = a.UpdateAgent
	if a.Update.DataDir == "" {
		a.Update.DataDir = a.Cfg.DataDir
	}
	if a.Update.Backup == nil && a.Control != nil && a.Control.Backup != nil {
		a.Update.Backup = a.Control.Backup.Snapshot
	}
	if a.Console != nil {
		a.Console.Update = a.Update
	}
	return a.Update
}

func (a *App) snapshotUpdateJob() *appupdate.Job {
	return a.updater().Snapshot()
}

func (a *App) finishUpdateJob(jobID, state, message string, finished bool) {
	a.updater().Finish(jobID, state, message, finished)
}
