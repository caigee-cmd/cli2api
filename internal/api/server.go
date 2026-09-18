package api

import (
	"context"
	"encoding/json"
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
	appsvc "github.com/caigee-cmd/cli2api/internal/control"
	"github.com/caigee-cmd/cli2api/internal/executor"
	apigateway "github.com/caigee-cmd/cli2api/internal/gateway"
	applogs "github.com/caigee-cmd/cli2api/internal/logs"
	"github.com/caigee-cmd/cli2api/internal/providers"
	"github.com/caigee-cmd/cli2api/internal/providers/devin"
	"github.com/caigee-cmd/cli2api/internal/providers/qoder"
	"github.com/caigee-cmd/cli2api/internal/providers/trae"
	"github.com/caigee-cmd/cli2api/internal/providers/workbuddy"
	accountruntime "github.com/caigee-cmd/cli2api/internal/runtime"
	sqlstore "github.com/caigee-cmd/cli2api/internal/store"
	control "github.com/caigee-cmd/cli2api/internal/update"
)

type Server struct {
	cfg                    config.Config
	auth                   auth.Verifier
	executor               executor.ChatExecutor
	pool                   *executor.Pool
	manager                *accountruntime.Manager
	control                *appsvc.Services
	providers              *providers.Registry
	recorder               *applogs.RequestRecorder
	ring                   *applogs.Ring
	stopLogs               chan struct{}
	mux                    *http.ServeMux
	updateChecker          updateChecker
	updateAgent            updateAgent
	settingsMu             sync.Mutex
	crossProviderModelPool atomic.Bool
	maintenance            atomic.Bool
	updateRunning          atomic.Bool
	updateMu               sync.Mutex
	updateJob              *systemUpdateJob
	statsCacheMu           sync.Mutex
	statsCache             map[string]statsCacheEntry
	gateway                *apigateway.Handler
}

func New(cfg config.Config) *Server {
	dataDir := cfg.DataDir
	if dataDir == "" {
		dataDir = filepath.Join(cfg.QoderHome, ".proxy-data")
	}
	cfg.DataDir = dataDir
	store, err := sqlstore.OpenStore(filepath.Join(dataDir, "qoder.db"))
	if err != nil {
		panic(err)
	}
	proxyAPIKey, initialized, err := ensureProxyAPIKey(context.Background(), store, cfg.ProxyAPIKey)
	if err != nil {
		panic(err)
	}
	if initialized {
		log.Printf("[security] initialized API key and stored it in SQLite: %s", proxyAPIKey)
	}
	proxyURL, err := ensureProxyURL(context.Background(), store, cfg.ProxyURL)
	if err != nil {
		panic(err)
	}
	crossProviderModelPool, err := ensureCrossProviderModelPool(context.Background(), store)
	if err != nil {
		panic(err)
	}
	routingStrategy, err := ensureRoutingStrategy(context.Background(), store)
	if err != nil {
		panic(err)
	}
	if _, err := ensureWorkBuddyCheckinTime(context.Background(), store); err != nil {
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
	qoderClient := qoder.NewClient()
	qoderClient.Bind(manager.AccountURL, manager.ProxyAPIKey)
	providerReg.Register(qoderClient.Adapter())
	manager.SetProviders(providerReg)
	manager.SetWorkBuddy(workbuddyClient)
	go manager.RefreshAll(context.Background(), false)
	recorder := applogs.NewRequestRecorder(store)
	stopLogs := make(chan struct{})
	go recorder.PurgeLoop(stopLogs, time.Hour)
	go manager.RunWorkBuddyMaintenanceLoop(stopLogs)
	checker := control.NewChecker(buildinfo.Version, control.NewGitHubReleaseSource("caigee-cmd/cli2api", cfg.UpdateGitHubToken))
	var agent control.Agent = control.NewUnixAgentClient(cfg.UpdateSocketPath)
	if strings.TrimSpace(cfg.UpdateAgentURL) != "" {
		agent = control.NewHTTPAgentClient(cfg.UpdateAgentURL, cfg.UpdateAgentToken)
	}
	chatExecutor := executor.NewChatExecutor(pool, proxyAPIKey)
	chatExecutor.MaxAttempts = cfg.MaxRetryAccounts
	chatExecutor.Providers = providerReg
	chatExecutor.OnAttempt = recorder.Attempt
	s := &Server{
		cfg:           cfg,
		auth:          auth.NewVerifier(proxyAPIKey, store),
		executor:      chatExecutor,
		pool:          pool,
		manager:       manager,
		control:       appsvc.New(manager),
		providers:     providerReg,
		recorder:      recorder,
		ring:          ring,
		stopLogs:      stopLogs,
		mux:           http.NewServeMux(),
		updateChecker: checker,
		updateAgent:   agent,
	}
	s.crossProviderModelPool.Store(crossProviderModelPool)
	s.control.Catalog = appsvc.NewCatalog(s.fetchCatalogModels)
	s.gateway = s.newGateway()
	s.routes()
	return s
}

func (s *Server) Close() error {
	if s.stopLogs != nil {
		close(s.stopLogs)
	}
	return errors.Join(s.manager.Close(), s.manager.Store().Close())
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, map[string]any{
		"error": map[string]any{
			"message": msg,
			"type":    "api_error",
			"code":    code,
		},
	})
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if s := strings.TrimSpace(v); s != "" {
			return s
		}
	}
	return ""
}
