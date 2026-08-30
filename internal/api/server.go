package api

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/caigee-cmd/cli2api/internal/accounts"
	"github.com/caigee-cmd/cli2api/internal/auth"
	"github.com/caigee-cmd/cli2api/internal/buildinfo"
	"github.com/caigee-cmd/cli2api/internal/config"
	"github.com/caigee-cmd/cli2api/internal/endpoint"
	"github.com/caigee-cmd/cli2api/internal/executor"
	applogs "github.com/caigee-cmd/cli2api/internal/logs"
	"github.com/caigee-cmd/cli2api/internal/providers"
	"github.com/caigee-cmd/cli2api/internal/providers/trae"
	"github.com/caigee-cmd/cli2api/internal/providers/workbuddy"
	control "github.com/caigee-cmd/cli2api/internal/update"
	"github.com/caigee-cmd/cli2api/internal/webui"
)

type Server struct {
	cfg           config.Config
	auth          auth.Verifier
	executor      executor.ChatExecutor
	pool          *accounts.Pool
	manager       *accounts.Manager
	providers     *providers.Registry
	recorder      *applogs.RequestRecorder
	ring          *applogs.Ring
	stopLogs      chan struct{}
	mux           *http.ServeMux
	updateChecker updateChecker
	updateAgent   updateAgent
	maintenance   atomic.Bool
	updateRunning atomic.Bool
}

func New(cfg config.Config) *Server {
	dataDir := cfg.DataDir
	if dataDir == "" {
		dataDir = filepath.Join(cfg.QoderHome, ".proxy-data")
	}
	cfg.DataDir = dataDir
	store, err := accounts.OpenStore(filepath.Join(dataDir, "qoder.db"))
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
	cfg.ProxyAPIKey = proxyAPIKey
	runtimeDir := cfg.RuntimeDir
	if runtimeDir == "" {
		runtimeDir = filepath.Join("/tmp", "cli2api-runtime")
	}
	ring := applogs.NewRing(2000)
	log.SetOutput(io.MultiWriter(os.Stderr, ring))
	manager := accounts.NewManager(accounts.ManagerConfig{
		DataDir: runtimeDir, BasePort: cfg.WorkerBasePort, NodeBinary: cfg.NodeBinary,
		DaemonPath: cfg.WorkerDaemonPath, QoderCLIPath: cfg.QoderCLIPath, QoderCNCLIPath: cfg.QoderCNCLIPath,
		TemplatePath: cfg.PlainTemplatePath, ProxyAPIKey: proxyAPIKey,
		MaxLogWriters: io.MultiWriter(os.Stderr, ring),
	}, store, nil)
	if err := manager.Start(context.Background()); err != nil {
		panic(err)
	}
	pool := manager.Pool()
	providerReg := providers.NewRegistry()
	workbuddyClient := workbuddy.NewClient(store)
	providerReg.Register(workbuddyClient.Adapter())
	providerReg.Register(trae.NewClient(store).Adapter())
	manager.SetProviders(providerReg)
	manager.SetWorkBuddy(workbuddyClient)
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
	chatExecutor.Providers = providerReg
	chatExecutor.OnAttempt = recorder.Attempt
	s := &Server{
		cfg:           cfg,
		auth:          auth.NewVerifier(proxyAPIKey, store),
		executor:      chatExecutor,
		pool:          pool,
		manager:       manager,
		providers:     providerReg,
		recorder:      recorder,
		ring:          ring,
		stopLogs:      stopLogs,
		mux:           http.NewServeMux(),
		updateChecker: checker,
		updateAgent:   agent,
	}
	s.routes()
	return s
}

const proxyAPIKeySecret = "proxy_api_key"

func ensureProxyAPIKey(ctx context.Context, store *accounts.Store, bootstrap string) (string, bool, error) {
	if value, ok, err := store.GetSecret(ctx, proxyAPIKeySecret); err != nil {
		return "", false, err
	} else if ok && strings.TrimSpace(value) != "" {
		return value, false, nil
	}

	key := strings.TrimSpace(bootstrap)
	if key == "" || key == "change-me" || key == "dev-key" {
		generated, err := generateAPIKey()
		if err != nil {
			return "", false, err
		}
		key = generated
	}
	if err := store.SetSecret(ctx, proxyAPIKeySecret, key); err != nil {
		return "", false, err
	}
	return key, true, nil
}

func generateAPIKey() (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("generate proxy api key: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(bytes), nil
}

func (s *Server) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.maintenance.Load() && blocksDuringUpdate(r.URL.Path) {
			writeErr(w, http.StatusServiceUnavailable, "service_updating", "Service update in progress")
			return
		}
		s.mux.ServeHTTP(w, r)
	})
}

func (s *Server) Close() error {
	if s.stopLogs != nil {
		close(s.stopLogs)
	}
	return errors.Join(s.manager.Close(), s.manager.Store().Close())
}

func (s *Server) routes() {
	s.mux.HandleFunc(endpoint.HealthPath, s.handleHealth)
	s.mux.HandleFunc("/api/overview", s.withConsoleKey(s.handleOverview))
	s.mux.HandleFunc("/api/system/update", s.withConsoleKey(s.handleSystemUpdate))
	s.mux.HandleFunc("/api/system/console-key", s.withConsoleKey(s.handleConsoleKey))
	s.mux.HandleFunc("/api/keys", s.withConsoleKey(s.handleAPIKeys))
	s.mux.HandleFunc("/api/keys/", s.withConsoleKey(s.handleAPIKeyByID))
	s.mux.HandleFunc("/api/models", s.withConsoleKey(s.handleModelsAPI))
	s.mux.HandleFunc("/api/models/", s.withConsoleKey(s.handleModelSetting))
	s.mux.HandleFunc("/api/providers", s.withConsoleKey(s.handleProviders))
	s.mux.HandleFunc("/api/accounts", s.withConsoleKey(s.handleAccounts))
	s.mux.HandleFunc("/api/accounts/import", s.withConsoleKey(s.handleAccountImport))
	s.mux.HandleFunc("/api/accounts/", s.withConsoleKey(s.handleAccountByID))
	s.mux.HandleFunc("/api/logs", s.withConsoleKey(s.handleLogs))
	s.mux.HandleFunc("/api/logs/", s.withConsoleKey(s.handleLogs))
	s.mux.HandleFunc("/api/chat", s.withConsoleKey(s.handleChatCompletions))
	s.mux.HandleFunc(endpoint.ModelsPath, s.withAPIKey(s.handleModels))
	s.mux.HandleFunc(endpoint.ChatCompletionsPath, s.withAPIKey(s.handleChatCompletions))

	ui := webui.Handler()
	s.mux.Handle("/assets/", ui)
	s.mux.Handle("/favicon.svg", ui)
	s.mux.Handle("/favicon-dark.svg", ui)
	s.mux.Handle("/apple-touch-icon.svg", ui)
	s.mux.Handle("/og-card.svg", ui)
	s.mux.Handle("/site.webmanifest", ui)
	s.mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" &&
			!strings.HasPrefix(r.URL.Path, "/assets/") &&
			r.URL.Path != "/favicon.svg" &&
			r.URL.Path != "/favicon-dark.svg" &&
			r.URL.Path != "/apple-touch-icon.svg" &&
			r.URL.Path != "/og-card.svg" &&
			r.URL.Path != "/site.webmanifest" {
			switch r.URL.Path {
			case "/login", "/auth", "/providers", "/access", "/accounts", "/system", "/logs", "/keys":
			default:
				http.NotFound(w, r)
				return
			}
		}
		data, err := webui.IndexHTML()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(data)
	})
}

func (s *Server) withAPIKey(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		identity, ok := s.auth.Authenticate(r.Context(), r)
		if !ok {
			writeErr(w, http.StatusUnauthorized, "invalid_api_key", "Missing/invalid API key")
			return
		}
		if identity.Kind == auth.KindKey && s.manager != nil {
			_ = s.manager.Store().TouchAPIKey(r.Context(), identity.KeyID)
		}
		next(w, r.WithContext(auth.WithIdentity(r.Context(), identity)))
	}
}

func (s *Server) withConsoleKey(next http.HandlerFunc) http.HandlerFunc {
	return s.withAPIKey(func(w http.ResponseWriter, r *http.Request) {
		identity, _ := auth.IdentityFrom(r.Context())
		if !identity.Console() {
			writeErr(w, http.StatusForbidden, "console_key_required", "This endpoint requires the console API key")
			return
		}
		next(w, r)
	})
}

func (s *Server) requestIdentity(r *http.Request) auth.Identity {
	identity, ok := auth.IdentityFrom(r.Context())
	if ok {
		return identity
	}
	return auth.Identity{Kind: auth.KindNone}
}

func providerIDs() []string {
	out := make([]string, 0, len(providers.List()))
	for _, d := range providers.List() {
		out = append(out, d.ID)
	}
	return out
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":                        true,
		"service":                   "cli2api",
		"providers":                 providerIDs(),
		"cross_provider_model_pool": s.cfg.CrossProviderModelPool,
		"phase":                     "ui-preview",
		"chat_url":                  endpoint.ChatCompletionsPath,
		"time":                      time.Now().UTC().Format(time.RFC3339),
		"version":                   buildinfo.Version,
		"commit":                    buildinfo.Commit,
		"maintenance":               s.maintenance.Load(),
	})
}

func (s *Server) handleOverview(w http.ResponseWriter, r *http.Request) {
	_ = s.manager.RefreshAll(r.Context(), r.URL.Query().Get("refresh") == "1")
	accountViews, _ := s.manager.Accounts(r.Context())
	readyCount := 0
	hotCount := 0
	for _, account := range accountViews {
		if account.Ready {
			readyCount++
		}
		if account.Hot {
			hotCount++
		}
	}
	models := s.decorateModelsWithContext(r.Context(), s.filterModelsForIdentity(r, s.fetchWorkerModels(false)))
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":   true,
		"time": time.Now().Format(time.RFC3339),
		"proxy": map[string]any{
			"ok": true, "service": "cli2api", "port": s.cfg.Port,
			"providers":                 providerIDs(),
			"cross_provider_model_pool": s.cfg.CrossProviderModelPool,
			"version":                   buildinfo.Version, "commit": buildinfo.Commit,
			"chat_url": "/v1/chat/completions",
		},
		"worker": map[string]any{
			"ok": readyCount > 0, "hot": hotCount > 0, "ready_count": readyCount,
			"hot_count": hotCount, "account_count": len(accountViews),
		},
		"accounts": accountViews,
		"models":   models,
		"access": map[string]any{
			"openai_base_url": "/v1", "chat_completions": endpoint.ChatCompletionsPath,
			"models": endpoint.ModelsPath, "health": endpoint.HealthPath,
			"hint": "Console APIs and /v1 require the API key stored in SQLite.",
		},
		"ui": map[string]any{
			"needs_api_key_for_chat":        s.cfg.ProxyAPIKey != "",
			"proxy_api_key_required_for_v1": s.cfg.ProxyAPIKey != "",
		},
	})
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
