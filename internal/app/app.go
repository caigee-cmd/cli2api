package app

import (
	"context"
	"errors"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/caigee-cmd/cli2api/internal/auth"
	"github.com/caigee-cmd/cli2api/internal/buildinfo"
	"github.com/caigee-cmd/cli2api/internal/config"
	appconsole "github.com/caigee-cmd/cli2api/internal/console"
	appsvc "github.com/caigee-cmd/cli2api/internal/control"
	"github.com/caigee-cmd/cli2api/internal/executor"
	apigateway "github.com/caigee-cmd/cli2api/internal/gateway"
	applogs "github.com/caigee-cmd/cli2api/internal/logs"
	"github.com/caigee-cmd/cli2api/internal/providers"
	"github.com/caigee-cmd/cli2api/internal/providers/codex"
	"github.com/caigee-cmd/cli2api/internal/providers/command"
	"github.com/caigee-cmd/cli2api/internal/providers/devin"
	"github.com/caigee-cmd/cli2api/internal/providers/qoder"
	"github.com/caigee-cmd/cli2api/internal/providers/trae"
	"github.com/caigee-cmd/cli2api/internal/providers/workbuddy"
	accountruntime "github.com/caigee-cmd/cli2api/internal/runtime"
	httpserver "github.com/caigee-cmd/cli2api/internal/server"
	sqlstore "github.com/caigee-cmd/cli2api/internal/store"
	appupdate "github.com/caigee-cmd/cli2api/internal/update"
)

// App owns process resources and assembles gateway/console/server.
// It is not a service locator for individual modules.
type App struct {
	Cfg                    config.Config
	Auth                   auth.Verifier
	Executor               executor.ChatExecutor
	Pool                   *executor.Pool
	Manager                *accountruntime.Manager
	Control                *appsvc.Services
	Providers              *providers.Registry
	Recorder               *applogs.RequestRecorder
	Ring                   *applogs.Ring
	stopLogs               chan struct{}
	stopLogsOnce           sync.Once
	SettingsMu             sync.Mutex
	CrossProviderModelPool atomic.Bool
	Gateway                *apigateway.Handler
	Console                *appconsole.Handler
	Update                 *appupdate.Coordinator
	HTTP                   *httpserver.Server
}

func New(cfg config.Config) *App {
	dataDir := cfg.DataDir
	if dataDir == "" {
		dataDir = filepath.Join(cfg.QoderHome, ".proxy-data")
	}
	cfg.DataDir = dataDir
	store, err := sqlstore.OpenStore(filepath.Join(dataDir, "qoder.db"))
	if err != nil {
		panic(err)
	}
	proxyAPIKey, initialized, err := EnsureProxyAPIKey(context.Background(), store, cfg.ProxyAPIKey)
	if err != nil {
		panic(err)
	}
	if initialized {
		log.Printf("[security] initialized API key and stored it in SQLite: %s", proxyAPIKey)
	}
	proxyURL, err := EnsureProxyURL(context.Background(), store, cfg.ProxyURL)
	if err != nil {
		panic(err)
	}
	crossProviderModelPool, err := EnsureCrossProviderModelPool(context.Background(), store)
	if err != nil {
		panic(err)
	}
	routingStrategy, err := EnsureRoutingStrategy(context.Background(), store)
	if err != nil {
		panic(err)
	}
	if _, err := EnsureWorkBuddyCheckinTime(context.Background(), store); err != nil {
		panic(err)
	}
	cfg.ProxyAPIKey = proxyAPIKey
	runtimeDir := cfg.RuntimeDir
	if runtimeDir == "" {
		runtimeDir = filepath.Join("/tmp", "cli2api-runtime")
	}
	ring := applogs.NewRing(2000)
	log.SetOutput(io.MultiWriter(os.Stderr, ring))
	manager := accountruntime.NewManager(accountruntime.ManagerConfig{
		DataDir: runtimeDir, BasePort: cfg.WorkerBasePort, NodeBinary: cfg.NodeBinary,
		DaemonPath: cfg.WorkerDaemonPath, QoderCLIPath: cfg.QoderCLIPath, QoderCNCLIPath: cfg.QoderCNCLIPath,
		TemplatePath: cfg.PlainTemplatePath, ProxyAPIKey: proxyAPIKey, ProxyURL: proxyURL,
		MaxLogWriters: io.MultiWriter(os.Stderr, ring),
	}, store, nil)
	if err := manager.Start(context.Background()); err != nil {
		panic(err)
	}
	pool := manager.Pool()
	pool.SetRoutingStrategy(routingStrategy)
	providerReg := providers.NewRegistry()
	workbuddyClient := workbuddy.NewClient(store)
	providerReg.Register(workbuddyClient.Adapter())
	providerReg.Register(trae.NewClient(store).Adapter())
	providerReg.Register(devin.NewClient(store).Adapter())
	providerReg.Register(command.NewClient(store).Adapter())
	providerReg.Register(codex.NewClient(store).Adapter())
	qoderClient := qoder.NewClient()
	qoderClient.Bind(manager.AccountURL, manager.ProxyAPIKey)
	providerReg.Register(qoderClient.Adapter())
	manager.SetProviders(providerReg)
	manager.SetWorkBuddy(workbuddyClient)
	go manager.RefreshAll(context.Background(), false)
	recorder := applogs.NewRequestRecorder(store)
	stopLogs := make(chan struct{})
	go recorder.PurgeLoop(stopLogs, time.Hour)
	go manager.RunMaintenanceLoop(stopLogs)
	checker := appupdate.NewChecker(buildinfo.Version, appupdate.NewGitHubReleaseSource("caigee-cmd/cli2api", cfg.UpdateGitHubToken))
	var agent appupdate.Agent = appupdate.NewUnixAgentClient(cfg.UpdateSocketPath)
	if strings.TrimSpace(cfg.UpdateAgentURL) != "" {
		agent = appupdate.NewHTTPAgentClient(cfg.UpdateAgentURL, cfg.UpdateAgentToken)
	}
	chatExecutor := executor.NewChatExecutor(pool, proxyAPIKey)
	chatExecutor.MaxAttempts = cfg.MaxRetryAccounts
	chatExecutor.Providers = providerReg
	chatExecutor.OnAttempt = recorder.Attempt
	a := &App{
		Cfg:       cfg,
		Auth:      auth.NewVerifier(proxyAPIKey, store),
		Executor:  chatExecutor,
		Pool:      pool,
		Manager:   manager,
		Control:   appsvc.New(manager),
		Providers: providerReg,
		Recorder:  recorder,
		Ring:      ring,
		stopLogs:  stopLogs,
	}
	a.CrossProviderModelPool.Store(crossProviderModelPool)
	a.Control.Catalog = appsvc.NewCatalog(a.FetchWorkerModelsForMode)
	if a.Control.Settings != nil {
		a.Control.Settings.BindCatalog(a.Control.Catalog)
	}
	// Auth copies and all executor copies read the same atomic live key.
	// Cfg.ProxyAPIKey and Executor.WorkerKey remain bootstrap snapshots.
	a.Executor.WorkerKeySource = a.Auth.ConsoleKey
	a.Update = a.newUpdateCoordinator(checker, agent)
	a.Gateway = a.newGateway()
	a.Console = a.newConsole()
	a.Console.Update = a.Update
	a.HTTP = a.newHTTP()
	return a
}

func (a *App) Close() error {
	if a.stopLogs != nil {
		a.stopLogsOnce.Do(func() { close(a.stopLogs) })
	}
	managerErr := a.Manager.Close()
	a.Recorder.Close()
	return errors.Join(managerErr, a.Manager.Store().Close())
}

func (a *App) Handler() http.Handler {
	if a == nil || a.HTTP == nil {
		return http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})
	}
	return a.HTTP.Handler()
}

func (a *App) RebuildHTTP() {
	if a == nil {
		return
	}
	if a.Gateway == nil {
		a.Gateway = a.newGateway()
	} else {
		a.Gateway.Executor = a.Executor
		a.Gateway.Pool = a.Pool
		a.Gateway.Recorder = a.Recorder
		a.Gateway.CrossProviderPool = &a.CrossProviderModelPool
		if a.Control != nil {
			a.Gateway.ModelContexts = a.Control.Settings
		}
		if a.Manager != nil {
			a.Gateway.Catalogs = a.Manager
		}
		if a.Recorder != nil {
			a.Gateway.Logs = a.Recorder
		}
	}
	if a.Console == nil {
		a.Console = a.newConsole()
	}
	a.Console.Update = a.Update
	if a.Console.Chat == nil && a.Gateway != nil {
		a.Console.Chat = a.Gateway.HandleChatCompletions
	}
	a.HTTP = a.newHTTP()
}

func (a *App) requestIdentity(r *http.Request) auth.Identity {
	identity, ok := auth.IdentityFrom(r.Context())
	if ok {
		return identity
	}
	return auth.Identity{Kind: auth.KindNone}
}

func (a *App) newHTTP() *httpserver.Server {
	return httpserver.New(httpserver.Server{
		Auth:          a.Auth,
		Gateway:       a.Gateway,
		Console:       a.Console,
		Update:        a.Update,
		CrossProvider: &a.CrossProviderModelPool,
		TouchKey: func(ctx context.Context, keyID string) {
			if a.Control != nil && a.Control.Keys != nil {
				_ = a.Control.Keys.Touch(ctx, keyID)
			}
		},
	})
}
