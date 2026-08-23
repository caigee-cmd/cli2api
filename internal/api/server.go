package api

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/caigee-cmd/cli2api/internal/accounts"
	"github.com/caigee-cmd/cli2api/internal/auth"
	"github.com/caigee-cmd/cli2api/internal/config"
	"github.com/caigee-cmd/cli2api/internal/endpoint"
	"github.com/caigee-cmd/cli2api/internal/executor"
	"github.com/caigee-cmd/cli2api/internal/translate"
	"github.com/caigee-cmd/cli2api/internal/webui"
)

type Server struct {
	cfg       config.Config
	auth      auth.Store
	endpoints endpoint.Endpoints
	executor  executor.ChatExecutor
	pool      *accounts.Pool
	manager   *accounts.Manager
	mux       *http.ServeMux
}

func New(cfg config.Config) *Server {
	eps := endpoint.ResolveFromHome(cfg.QoderHome)
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
		cfg:       cfg,
		auth:      auth.Store{Home: cfg.QoderHome, PAT: cfg.QoderPAT},
		endpoints: eps,
		executor:  executor.NewChatExecutor(eps, pool),
		pool:      pool,
		manager:   manager,
		mux:       http.NewServeMux(),
	}
	s.routes()
	return s
}

func (s *Server) Handler() http.Handler { return s.mux }

func (s *Server) Close() error {
	return errors.Join(s.manager.Close(), s.manager.Store().Close())
}

func (s *Server) routes() {
	s.mux.HandleFunc("/health", s.handleHealth)
	s.mux.HandleFunc("/api/overview", s.withAPIKey(s.handleOverview))
	s.mux.HandleFunc("/api/models", s.withAPIKey(s.handleModelsAPI))
	s.mux.HandleFunc("/api/accounts", s.withAPIKey(s.handleAccounts))
	s.mux.HandleFunc("/api/accounts/import", s.withAPIKey(s.handleAccountImport))
	s.mux.HandleFunc("/api/accounts/", s.withAPIKey(s.handleAccountByID))
	s.mux.HandleFunc("/api/login/status", s.withAPIKey(s.handleLoginStatus))
	s.mux.HandleFunc("/api/login/device", s.withAPIKey(s.handleLoginDevice))
	s.mux.HandleFunc("/api/login/pat", s.withAPIKey(s.handleLoginPAT))
	s.mux.HandleFunc("/api/rewarm", s.withAPIKey(s.handleRewarm))
	s.mux.HandleFunc("/api/chat", s.withAPIKey(s.handleChatCompletions))
	s.mux.HandleFunc("/v1/models", s.withAPIKey(s.handleModels))
	s.mux.HandleFunc("/v1/chat/completions", s.withAPIKey(s.handleChatCompletions))
	s.mux.HandleFunc("/debug/auth-snapshot", s.withAPIKey(s.handleAuthSnapshot))
	s.mux.HandleFunc("/debug/endpoints", s.withAPIKey(s.handleEndpoints))

	ui := webui.Handler()
	s.mux.Handle("/assets/", ui)
	s.mux.Handle("/favicon.svg", ui)
	s.mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// SPA fallback for React Router pages.
		if r.URL.Path != "/" &&
			!strings.HasPrefix(r.URL.Path, "/assets/") &&
			r.URL.Path != "/favicon.svg" {
			// Only treat likely page routes as SPA; keep unknown API-like paths 404.
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
		if s.cfg.ProxyAPIKey == "" {
			next(w, r)
			return
		}
		got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if got == "" {
			got = r.Header.Get("x-api-key")
		}
		if got != s.cfg.ProxyAPIKey {
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
		"chat_url": s.endpoints.ChatURL(),
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
			"chat_url": s.endpoints.ChatURL(), "endpoints": s.endpoints,
		},
		"worker": map[string]any{
			"ok": readyCount > 0, "hot": hotCount > 0, "ready_count": readyCount,
			"hot_count": hotCount, "account_count": len(accountViews),
		},
		"accounts": accountViews,
		"models":   models,
		"access": map[string]any{
			"openai_base_url": "/v1", "chat_completions": "/v1/chat/completions",
			"models": "/v1/models", "health": "/health",
			"hint": "Console APIs and /v1 both require PROXY_API_KEY when it is set.",
		},
		"ui": map[string]any{
			"needs_api_key_for_chat":        s.cfg.ProxyAPIKey != "",
			"proxy_api_key_required_for_v1": s.cfg.ProxyAPIKey != "",
		},
	})
}

func (s *Server) handleRewarm(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, "method_not_allowed", "POST only")
		return
	}
	accountID := s.selectedAccountID(r)
	if accountID == "" {
		writeErr(w, http.StatusConflict, "account_not_running", "no running account")
		return
	}
	s.proxyAccountWorker(w, r, accountID, "/admin/rewarm", "")
}

func (s *Server) workerBase() string {
	if item, ok := s.pool.First(); ok {
		return item.URL
	}
	return ""
}

func (s *Server) workerForAccount(id string) string {
	if id != "" {
		if item, ok := s.pool.ByID(id); ok {
			return item.URL
		}
	}
	return s.workerBase()
}

func (s *Server) requestedAccount(r *http.Request) string {
	id := strings.TrimSpace(r.URL.Query().Get("account"))
	if id == "" {
		id = strings.TrimSpace(r.Header.Get("X-Qoder-Account"))
	}
	return id
}

func (s *Server) selectedAccountID(r *http.Request) string {
	if id := s.requestedAccount(r); id != "" {
		return id
	}
	if item, ok := s.pool.First(); ok {
		return item.ID
	}
	return ""
}

func (s *Server) handleAccounts(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		_ = s.manager.RefreshAll(r.Context())
		items, err := s.manager.Accounts(r.Context())
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "account_list_failed", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"object": "list", "data": items})
	case http.MethodPost:
		var input struct {
			Name        string `json:"name"`
			Enabled     bool   `json:"enabled"`
			MaxInFlight int    `json:"max_inflight"`
		}
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			writeErr(w, http.StatusBadRequest, "invalid_request", err.Error())
			return
		}
		account, err := s.manager.Create(r.Context(), accounts.CreateAccount{Name: input.Name, Enabled: input.Enabled, MaxInFlight: input.MaxInFlight})
		if err != nil {
			writeErr(w, http.StatusBadRequest, "account_create_failed", err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, account)
	default:
		writeErr(w, http.StatusMethodNotAllowed, "method_not_allowed", "GET or POST only")
	}
}

func (s *Server) handleAccountImport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, "method_not_allowed", "POST only")
		return
	}
	var input struct {
		Format    string `json:"format"`
		Name      string `json:"name"`
		Enabled   bool   `json:"enabled"`
		UserBlob  string `json:"user_blob"`
		MachineID string `json:"machine_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	if input.Format != "qoder-native-v1" {
		writeErr(w, http.StatusBadRequest, "unsupported_credential_format", "format must be qoder-native-v1")
		return
	}
	blob, err := base64.StdEncoding.DecodeString(input.UserBlob)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_user_blob", "user_blob must be base64")
		return
	}
	account, err := s.manager.Import(r.Context(), accounts.ImportAccount{Name: input.Name, Enabled: input.Enabled, Credential: accounts.NativeCredential{UserBlob: blob, MachineID: input.MachineID}})
	if err != nil {
		writeErr(w, http.StatusBadRequest, "account_import_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, account)
}

func (s *Server) handleAccountByID(w http.ResponseWriter, r *http.Request) {
	relative := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/accounts/"), "/")
	parts := strings.Split(relative, "/")
	if len(parts) == 0 || parts[0] == "" {
		writeErr(w, http.StatusNotFound, "account_not_found", "account id required")
		return
	}
	accountID := parts[0]
	if len(parts) == 1 {
		switch r.Method {
		case http.MethodGet:
			account, err := s.manager.Store().Get(r.Context(), accountID)
			if err != nil {
				writeErr(w, http.StatusNotFound, "account_not_found", err.Error())
				return
			}
			writeJSON(w, http.StatusOK, account)
		case http.MethodPatch:
			var input struct {
				Name        string `json:"name"`
				Enabled     *bool  `json:"enabled"`
				MaxInFlight *int   `json:"max_inflight"`
				Priority    *int   `json:"priority"`
			}
			if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
				writeErr(w, http.StatusBadRequest, "invalid_request", err.Error())
				return
			}
			err := s.manager.Update(r.Context(), accountID, accounts.UpdateAccount{
				Name: input.Name, Enabled: input.Enabled, MaxInFlight: input.MaxInFlight, Priority: input.Priority,
			})
			if err != nil {
				writeErr(w, http.StatusBadRequest, "account_update_failed", err.Error())
				return
			}
			account, _ := s.manager.Store().Get(r.Context(), accountID)
			writeJSON(w, http.StatusOK, account)
		case http.MethodDelete:
			if err := s.manager.Delete(r.Context(), accountID); err != nil {
				writeErr(w, http.StatusBadRequest, "account_delete_failed", err.Error())
				return
			}
			w.WriteHeader(http.StatusNoContent)
		default:
			writeErr(w, http.StatusMethodNotAllowed, "method_not_allowed", "GET, PATCH or DELETE only")
		}
		return
	}

	action := strings.Join(parts[1:], "/")
	switch action {
	case "export":
		if r.Method != http.MethodGet {
			writeErr(w, http.StatusMethodNotAllowed, "method_not_allowed", "GET only")
			return
		}
		account, err := s.manager.Store().Get(r.Context(), accountID)
		if err != nil {
			writeErr(w, http.StatusNotFound, "account_not_found", err.Error())
			return
		}
		credential, err := s.manager.Store().LoadCredential(r.Context(), accountID)
		if err != nil {
			writeErr(w, http.StatusNotFound, "credential_not_found", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"format": "qoder-native-v1", "name": account.Name,
			"user_blob":  base64.StdEncoding.EncodeToString(credential.UserBlob),
			"machine_id": credential.MachineID,
		})
	case "login/device":
		s.proxyAccountWorker(w, r, accountID, "/admin/login/device", "")
	case "login/status":
		s.proxyAccountWorker(w, r, accountID, "/admin/login/status", "oauth_if_complete")
	case "login/pat":
		s.proxyAccountWorker(w, r, accountID, "/admin/login/pat", "pat")
	case "rewarm":
		s.proxyAccountWorker(w, r, accountID, "/admin/rewarm", "")
	default:
		writeErr(w, http.StatusNotFound, "not_found", "unknown account action")
	}
}

func (s *Server) proxyAccountWorker(w http.ResponseWriter, r *http.Request, accountID, path, syncAuth string) {
	workerURL, ok := s.manager.AccountURL(accountID)
	if !ok {
		writeErr(w, http.StatusConflict, "account_not_running", "account is disabled or not running")
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	req, err := http.NewRequestWithContext(r.Context(), r.Method, workerURL+path, bytes.NewReader(body))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "worker_request_failed", err.Error())
		return
	}
	if contentType := r.Header.Get("Content-Type"); contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	if key := s.workerAPIKey(); key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	resp, err := (&http.Client{Timeout: 120 * time.Second}).Do(req)
	if err != nil {
		writeErr(w, http.StatusBadGateway, "worker_unavailable", err.Error())
		return
	}
	defer resp.Body.Close()
	responseBody, _ := io.ReadAll(resp.Body)
	for key, values := range resp.Header {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}
	if resp.StatusCode < 300 && syncAuth != "" {
		authType := syncAuth
		if syncAuth == "oauth_if_complete" {
			var status struct {
				Login struct {
					Status string `json:"status"`
				} `json:"login"`
			}
			if json.Unmarshal(responseBody, &status) != nil || status.Login.Status != "ok" {
				authType = ""
			} else {
				authType = "oauth"
			}
		}
		if authType != "" {
			if err := s.manager.SyncCredential(r.Context(), accountID, authType); err != nil {
				writeErr(w, http.StatusBadGateway, "credential_sync_failed", err.Error())
				return
			}
		}
	}
	w.WriteHeader(resp.StatusCode)
	_, _ = w.Write(responseBody)
}

func (s *Server) workerAPIKey() string {
	return firstNonEmpty(os.Getenv("QODER_WORKER_API_KEY"), s.cfg.ProxyAPIKey)
}

func (s *Server) workerGet(path string, timeout time.Duration, accountID string) (*http.Response, error) {
	workerURL := s.workerForAccount(accountID)
	if workerURL == "" {
		return nil, fmt.Errorf("no running Qoder account")
	}
	req, err := http.NewRequest(http.MethodGet, workerURL+path, nil)
	if err != nil {
		return nil, err
	}
	if key := s.workerAPIKey(); key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	if accountID != "" {
		req.Header.Set("X-Qoder-Account", accountID)
	}
	client := &http.Client{Timeout: timeout}
	return client.Do(req)
}

func (s *Server) fetchWorkerModels(refresh bool) []map[string]any {
	return s.fetchWorkerModelsFor(refresh, "")
}

func (s *Server) fetchWorkerModelsFor(refresh bool, accountID string) []map[string]any {
	path := "/admin/models"
	if refresh {
		path += "?refresh=1"
	}
	resp, err := s.workerGet(path, 60*time.Second, accountID)
	if err != nil {
		return []map[string]any{
			{"id": "auto", "object": "model", "owned_by": "qoder"},
			{"id": "qwen3.7-plus", "object": "model", "owned_by": "qoder"},
			{"id": "glm-5.2", "object": "model", "owned_by": "qoder"},
		}
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var parsed struct {
		Data []map[string]any `json:"data"`
	}
	if json.Unmarshal(body, &parsed) != nil || len(parsed.Data) == 0 {
		return []map[string]any{
			{"id": "auto", "object": "model", "owned_by": "qoder"},
			{"id": "qwen3.7-plus", "object": "model", "owned_by": "qoder"},
			{"id": "glm-5.2", "object": "model", "owned_by": "qoder"},
		}
	}
	return parsed.Data
}

func (s *Server) handleModelsAPI(w http.ResponseWriter, r *http.Request) {
	refresh := r.URL.Query().Get("refresh") == "1"
	writeJSON(w, http.StatusOK, map[string]any{
		"object": "list",
		"data":   s.fetchWorkerModelsFor(refresh, s.requestedAccount(r)),
	})
}

func (s *Server) handleLoginStatus(w http.ResponseWriter, r *http.Request) {
	accountID := s.selectedAccountID(r)
	if accountID == "" {
		writeErr(w, http.StatusConflict, "account_not_running", "no running account")
		return
	}
	s.proxyAccountWorker(w, r, accountID, "/admin/login/status", "oauth_if_complete")
}

func (s *Server) handleLoginDevice(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, "method_not_allowed", "POST only")
		return
	}
	accountID := s.selectedAccountID(r)
	if accountID == "" {
		writeErr(w, http.StatusConflict, "account_not_running", "no running account")
		return
	}
	s.proxyAccountWorker(w, r, accountID, "/admin/login/device", "")
}

func (s *Server) handleLoginPAT(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, "method_not_allowed", "POST only")
		return
	}
	accountID := s.selectedAccountID(r)
	if accountID == "" {
		writeErr(w, http.StatusConflict, "account_not_running", "no running account")
		return
	}
	s.proxyAccountWorker(w, r, accountID, "/admin/login/pat", "pat")
}

func (s *Server) handleModels(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"object": "list",
		"data":   s.fetchWorkerModels(false),
	})
}

func (s *Server) handleAuthSnapshot(w http.ResponseWriter, r *http.Request) {
	snap, err := s.auth.Snapshot()
	if err != nil {
		writeErr(w, http.StatusBadRequest, "auth_missing", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, snap)
}

func (s *Server) handleEndpoints(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.endpoints)
}

func (s *Server) handleChatCompletions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, "method_not_allowed", "POST only")
		return
	}
	var req translate.ChatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	if len(req.Messages) == 0 {
		writeErr(w, http.StatusBadRequest, "invalid_request", "messages required")
		return
	}

	prefer := s.requestedAccount(r)
	if req.Stream {
		upstream, accountID, err := s.executor.ChatStreamProxy(r.Context(), req, prefer)
		if err != nil {
			writeClassifiedErr(w, err)
			return
		}
		defer upstream.Body.Close()
		w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		if accountID != "" {
			w.Header().Set("X-Qoder-Account", accountID)
		}
		w.WriteHeader(http.StatusOK)
		if _, err := io.Copy(w, upstream.Body); err != nil {
			return
		}
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		return
	}

	res, err := s.executor.ChatNonStream(r.Context(), req, prefer)
	if err != nil {
		writeClassifiedErr(w, err)
		return
	}
	message := map[string]any{
		"role":    "assistant",
		"content": res.Content,
	}
	if res.Reasoning != "" {
		message["reasoning_content"] = res.Reasoning
	}
	if len(res.ToolCalls) > 0 && string(res.ToolCalls) != "null" {
		message["tool_calls"] = json.RawMessage(res.ToolCalls)
		if res.Content == "" {
			message["content"] = nil
		}
	}
	finishReason := res.FinishReason
	if finishReason == "" {
		if len(res.ToolCalls) > 0 && string(res.ToolCalls) != "null" {
			finishReason = "tool_calls"
		} else {
			finishReason = "stop"
		}
	}
	if res.AccountID != "" {
		w.Header().Set("X-Qoder-Account", res.AccountID)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"id":      fmt.Sprintf("chatcmpl-%d", time.Now().UnixMilli()),
		"object":  "chat.completion",
		"created": time.Now().Unix(),
		"model":   res.Model,
		"choices": []map[string]any{{
			"index":         0,
			"message":       message,
			"finish_reason": finishReason,
		}},
		"usage": func() map[string]any {
			out := map[string]any{
				"prompt_tokens":     res.PromptTokens,
				"completion_tokens": res.CompletionTokens,
				"total_tokens":      res.PromptTokens + res.CompletionTokens,
				"source":            firstNonEmpty(res.UsageSource, "estimate"),
			}
			if res.Credits != nil {
				out["credits"] = *res.Credits
			}
			return out
		}(),
	})
}

func writeClassifiedErr(w http.ResponseWriter, err error) {
	if err == nil {
		writeErr(w, http.StatusServiceUnavailable, "upstream_not_ready", "upstream not ready")
		return
	}
	classified := accounts.Classify(0, err.Error(), "", "", "")
	if classified.RetryAfter > 0 {
		w.Header().Set("Retry-After", fmt.Sprintf("%d", int(classified.RetryAfter.Seconds())))
	}
	w.Header().Set("X-Qoder-Error-Kind", classified.Kind)
	if classified.Failover {
		w.Header().Set("X-Qoder-Failover", "1")
	} else {
		w.Header().Set("X-Qoder-Failover", "0")
	}
	writeErr(w, classified.Status, classified.Code, classified.Message)
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
