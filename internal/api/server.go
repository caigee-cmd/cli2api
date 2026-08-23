package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/caigee-cmd/cli2api/internal/accounts"
	"github.com/caigee-cmd/cli2api/internal/auth"
	"github.com/caigee-cmd/cli2api/internal/config"
	"github.com/caigee-cmd/cli2api/internal/endpoint"
	"github.com/caigee-cmd/cli2api/internal/executor"
	"github.com/caigee-cmd/cli2api/internal/webui"
)

type Server struct {
	cfg      config.Config
	auth     auth.Verifier
	executor executor.ChatExecutor
	pool     *accounts.Pool
	manager  *accounts.Manager
	mux      *http.ServeMux
}

func New(cfg config.Config) *Server {
	dataDir := cfg.DataDir
	if dataDir == "" {
		dataDir = filepath.Join(cfg.QoderHome, ".proxy-data")
	}
	store, err := accounts.OpenStore(filepath.Join(dataDir, "qoder.db"))
	if err != nil {
		panic(err)
	}
	if _, err := accounts.ImportLegacyHome(context.Background(), store, cfg.QoderHome); err != nil {
		panic(err)
	}
	manager := accounts.NewManager(accounts.ManagerConfig{
		DataDir: dataDir, BasePort: cfg.WorkerBasePort, NodeBinary: cfg.NodeBinary,
		DaemonPath: cfg.WorkerDaemonPath, QoderCLIPath: cfg.QoderCLIPath,
		TemplatePath: cfg.PlainTemplatePath, ProxyAPIKey: cfg.ProxyAPIKey,
	}, store, nil)
	if err := manager.Start(context.Background()); err != nil {
		panic(err)
	}
	pool := manager.Pool()
	s := &Server{
		cfg:      cfg,
		auth:     auth.NewVerifier(cfg.ProxyAPIKey),
		executor: executor.NewChatExecutor(pool, cfg.ProxyAPIKey),
		pool:     pool,
		manager:  manager,
		mux:      http.NewServeMux(),
	}
	s.routes()
	return s
}

func (s *Server) Handler() http.Handler { return s.mux }

func (s *Server) Close() error {
	return errors.Join(s.manager.Close(), s.manager.Store().Close())
}

func (s *Server) routes() {
	s.mux.HandleFunc(endpoint.HealthPath, s.handleHealth)
	s.mux.HandleFunc("/api/overview", s.withAPIKey(s.handleOverview))
	s.mux.HandleFunc("/api/models", s.withAPIKey(s.handleModelsAPI))
	s.mux.HandleFunc("/api/accounts", s.withAPIKey(s.handleAccounts))
	s.mux.HandleFunc("/api/accounts/import", s.withAPIKey(s.handleAccountImport))
	s.mux.HandleFunc("/api/accounts/", s.withAPIKey(s.handleAccountByID))
	s.mux.HandleFunc("/api/chat", s.withAPIKey(s.handleChatCompletions))
	s.mux.HandleFunc(endpoint.ModelsPath, s.withAPIKey(s.handleModels))
	s.mux.HandleFunc(endpoint.ChatCompletionsPath, s.withAPIKey(s.handleChatCompletions))

	ui := webui.Handler()
	s.mux.Handle("/assets/", ui)
	s.mux.Handle("/favicon.svg", ui)
	s.mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" &&
			!strings.HasPrefix(r.URL.Path, "/assets/") &&
			r.URL.Path != "/favicon.svg" {
			switch r.URL.Path {
			case "/login", "/auth", "/providers", "/access", "/accounts":
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
		if !s.auth.Authorized(r) {
			writeErr(w, http.StatusUnauthorized, "invalid_api_key", "Missing/invalid PROXY_API_KEY")
			return
		}
		next(w, r)
	}
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":       true,
		"service":  "cli2api",
		"provider": "qoder",
		"phase":    "ui-preview",
		"chat_url": endpoint.ChatCompletionsPath,
		"time":     time.Now().UTC().Format(time.RFC3339),
	})
}

func (s *Server) handleOverview(w http.ResponseWriter, r *http.Request) {
	_ = s.manager.RefreshAll(r.Context())
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
	models := s.fetchWorkerModels(false)
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":   true,
		"time": time.Now().Format(time.RFC3339),
		"proxy": map[string]any{
			"ok": true, "service": "cli2api", "provider": "qoder", "port": s.cfg.Port,
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
			"hint": "Console APIs and /v1 both require PROXY_API_KEY when it is set.",
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
