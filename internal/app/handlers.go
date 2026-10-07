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
		Chat:      a.gatewayHandler().HandleChatCompletions,
		Resources: a.systemResources,
	}
	return h
}

// systemResources adapts the runtime manager's process snapshot to the
// console-facing type and decorates workers with account display names.
func (a *App) systemResources() *appconsole.SystemResources {
	if a == nil || a.Manager == nil {
		return nil
	}
	snap := a.Manager.Resources()
	labels := map[string]string{}
	if a.Control != nil && a.Control.Accounts != nil {
		if views, err := a.Control.Accounts.List(context.Background(), false); err == nil {
			for _, v := range views {
				labels[v.ID] = v.Name
			}
		}
	}
	out := &appconsole.SystemResources{
		SampledAt: snap.SampledAt.Format("2006-01-02T15:04:05Z07:00"),
		Server: appconsole.SystemProcessResources{
			PID:        snap.Server.PID,
			RSSBytes:   snap.Server.RSSBytes,
			HeapBytes:  snap.Server.HeapBytes,
			Goroutines: snap.Server.Goroutines,
			CPUPercent: snap.Server.CPUPercent,
		},
		TotalRSS: snap.Server.RSSBytes,
	}
	for _, w := range snap.Workers {
		out.Workers = append(out.Workers, appconsole.SystemWorkerResources{
			AccountID:  w.AccountID,
			Label:      labels[w.AccountID],
			PID:        w.PID,
			RSSBytes:   w.RSSBytes,
			CPUPercent: w.CPUPercent,
		})
		out.TotalRSS += w.RSSBytes
	}
	return out
}
