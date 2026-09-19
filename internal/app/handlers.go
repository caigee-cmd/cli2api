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
	var settings *control.Settings
	var accounts *control.Accounts
	var keys *control.Keys
	if a.Control != nil {
		settings, accounts, keys = a.Control.Settings, a.Control.Accounts, a.Control.Keys
		if accounts != nil {
			accounts.Providers = a.Providers
		}
	}
	h := &appconsole.Handler{
		System:            &control.System{Settings: settings, Accounts: accounts, Pool: a.Pool, Executor: &a.Executor, CrossProviderPool: &a.CrossProviderModelPool, Mu: &a.SettingsMu},
		KeyRotation:       &control.KeyRotation{Keys: keys, Accounts: accounts, Mu: &a.SettingsMu, Generate: control.GenerateAPIKey, Publish: a.Auth.SetConsoleKey},
		Control:           a.Control,
		Cfg:               &a.Cfg,
		Executor:          &a.Executor,
		Pool:              a.Pool,
		Recorder:          a.Recorder,
		Ring:              a.Ring,
		CrossProviderPool: &a.CrossProviderModelPool,
		RequestedAccount:  a.RequestedAccount,
		FilterModels:      a.filterModelsForIdentity,
		FetchWorkerModels: a.fetchWorkerModels,
		FetchDisplayModels: func(refresh bool, accountID string, mode control.CatalogMode) ([]map[string]any, error) {
			return a.fetchDisplayModels(refresh, accountID, mode)
		},
		ConsoleKey: a.Auth.ConsoleKey,
		DecorateModels: func(ctx context.Context, models []map[string]any) []map[string]any {
			return a.decorateModelsWithContext(ctx, models)
		},
		Chat: a.gatewayHandler().HandleChatCompletions,
	}
	return h
}
