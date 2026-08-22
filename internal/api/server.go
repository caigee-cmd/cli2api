package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
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
	mux       *http.ServeMux
}

func New(cfg config.Config) *Server {
	eps := endpoint.ResolveFromHome(cfg.QoderHome)
	pool := accounts.LoadFromEnv()
	s := &Server{
		cfg:       cfg,
		auth:      auth.Store{Home: cfg.QoderHome, PAT: cfg.QoderPAT},
		endpoints: eps,
		executor:  executor.NewChatExecutor(eps, pool),
		pool:      pool,
		mux:       http.NewServeMux(),
	}
	s.routes()
	return s
}

func (s *Server) Handler() http.Handler { return s.mux }

func (s *Server) routes() {
	s.mux.HandleFunc("/health", s.handleHealth)
	s.mux.HandleFunc("/api/overview", s.withAPIKey(s.handleOverview))
	s.mux.HandleFunc("/api/models", s.withAPIKey(s.handleModelsAPI))
	s.mux.HandleFunc("/api/accounts", s.withAPIKey(s.handleAccounts))
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
	authSnap, authErr := s.auth.Snapshot()
	worker := s.fetchWorkerHealth()
	models := s.fetchWorkerModels(false)
	login := s.fetchLoginStatus()
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":   true,
		"time": time.Now().Format(time.RFC3339),
		"proxy": map[string]any{
			"ok":        true,
			"service":   "cli2api",
			"provider":  "qoder",
			"port":      s.cfg.Port,
			"chat_url":  s.endpoints.ChatURL(),
			"endpoints": s.endpoints,
		},
		"worker":   worker,
		"login":    login,
		"accounts": accountSnapshot(s.pool, worker),
		"auth": func() any {
			if authErr != nil {
				return map[string]any{
					"ok":      false,
					"error":   authErr.Error(),
					"home":    s.cfg.QoderHome,
					"has_pat": s.cfg.QoderPAT != "",
				}
			}
			return authSnap
		}(),
		"models": models,
		"access": map[string]any{
			"openai_base_url":  "/v1",
			"chat_completions": "/v1/chat/completions",
			"models":           "/v1/models",
			"health":           "/health",
			"hint":             "Console APIs and /v1 both require PROXY_API_KEY when it is set.",
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
	s.proxyWorker(w, r, "/admin/rewarm")
}

func (s *Server) workerBase() string {
	if s.pool != nil {
		if item, ok := s.pool.First(); ok {
			return item.URL
		}
	}
	return strings.TrimRight(firstNonEmpty(os.Getenv("QODER_WORKER_URL"), "http://127.0.0.1:3020"), "/")
}

func (s *Server) workerForAccount(id string) string {
	if s.pool != nil {
		if id != "" {
			if item, ok := s.pool.ByID(id); ok {
				return item.URL
			}
		}
		if item, ok := s.pool.First(); ok {
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

func (s *Server) handleAccounts(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"object": "list",
		"data":   accountSnapshot(s.pool, s.fetchWorkerHealth()),
	})
}

func (s *Server) workerAPIKey() string {
	return firstNonEmpty(os.Getenv("QODER_WORKER_API_KEY"), s.cfg.ProxyAPIKey)
}

func (s *Server) workerGet(path string, timeout time.Duration, accountID string) (*http.Response, error) {
	req, err := http.NewRequest(http.MethodGet, s.workerForAccount(accountID)+path, nil)
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

func (s *Server) fetchLoginStatus() map[string]any {
	return s.fetchLoginStatusFor("")
}

func (s *Server) fetchLoginStatusFor(accountID string) map[string]any {
	resp, err := s.workerGet("/admin/login/status", 5*time.Second, accountID)
	if err != nil {
		return map[string]any{"ok": false, "error": err.Error()}
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var raw map[string]any
	if json.Unmarshal(body, &raw) != nil {
		return map[string]any{"ok": false, "raw": string(body)}
	}
	return raw
}

func (s *Server) handleModelsAPI(w http.ResponseWriter, r *http.Request) {
	refresh := r.URL.Query().Get("refresh") == "1"
	writeJSON(w, http.StatusOK, map[string]any{
		"object": "list",
		"data":   s.fetchWorkerModelsFor(refresh, s.requestedAccount(r)),
	})
}

func (s *Server) handleLoginStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.fetchLoginStatusFor(s.requestedAccount(r)))
}

func (s *Server) handleLoginDevice(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, "method_not_allowed", "POST only")
		return
	}
	s.proxyWorker(w, r, "/admin/login/device")
}

func (s *Server) handleLoginPAT(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, "method_not_allowed", "POST only")
		return
	}
	s.proxyWorker(w, r, "/admin/login/pat")
}

func (s *Server) proxyWorker(w http.ResponseWriter, r *http.Request, path string) {
	body, _ := io.ReadAll(r.Body)
	if len(body) == 0 {
		body = []byte("{}")
	}
	accountID := s.requestedAccount(r)
	req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, s.workerForAccount(accountID)+path, bytes.NewReader(body))
	if err != nil {
		writeErr(w, http.StatusBadGateway, "worker_proxy_failed", err.Error())
		return
	}
	req.Header.Set("Content-Type", "application/json")
	if key := s.workerAPIKey(); key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	if accountID != "" {
		req.Header.Set("X-Qoder-Account", accountID)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		writeErr(w, http.StatusBadGateway, "worker_proxy_failed", err.Error())
		return
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(resp.Body)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	_, _ = w.Write(out)
}

func (s *Server) fetchWorkerHealth() map[string]any {
	workerURL := s.workerBase()
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(workerURL + "/health")
	if err != nil {
		return map[string]any{"ok": false, "error": err.Error(), "url": workerURL}
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var raw map[string]any
	if json.Unmarshal(body, &raw) != nil {
		return map[string]any{"ok": resp.StatusCode < 300, "raw": string(body), "url": workerURL}
	}
	raw["ok"] = resp.StatusCode < 300
	raw["url"] = workerURL
	return raw
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

func accountSnapshot(pool *accounts.Pool, worker map[string]any) []map[string]any {
	if worker != nil {
		if raw, ok := worker["accounts"].([]any); ok && len(raw) > 0 {
			out := make([]map[string]any, 0, len(raw))
			for _, item := range raw {
				m, ok := item.(map[string]any)
				if !ok {
					continue
				}
				delete(m, "home")
				id, _ := m["id"].(string)
				if id == "" {
					id, _ = m["accountId"].(string)
					if id != "" {
						m["id"] = id
					}
				}
				if pool != nil && id != "" {
					if pooled, ok := pool.ByID(id); ok {
						if m["url"] == nil || m["url"] == "" {
							m["url"] = pooled.URL
						}
						if !pooled.DownUntil.IsZero() && time.Now().Before(pooled.DownUntil) {
							m["down_until"] = pooled.DownUntil.UTC().Format(time.RFC3339)
							m["ready"] = false
						}
						if pooled.LastError != "" && m["last_error"] == nil && m["lastError"] == nil {
							m["last_error"] = pooled.LastError
						}
						if pooled.LastKind != "" && m["kind"] == nil {
							m["kind"] = pooled.LastKind
						}
					}
				}
				out = append(out, m)
			}
			if len(out) > 0 {
				return out
			}
		}
	}
	if pool != nil {
		return pool.Snapshot()
	}
	return nil
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
