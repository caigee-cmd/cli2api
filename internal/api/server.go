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
	mux       *http.ServeMux
}

func New(cfg config.Config) *Server {
	eps := endpoint.ResolveFromHome(cfg.QoderHome)
	s := &Server{
		cfg:       cfg,
		auth:      auth.Store{Home: cfg.QoderHome, PAT: cfg.QoderPAT},
		endpoints: eps,
		executor:  executor.NewChatExecutor(eps),
		mux:       http.NewServeMux(),
	}
	s.routes()
	return s
}

func (s *Server) Handler() http.Handler { return s.mux }

func (s *Server) routes() {
	s.mux.HandleFunc("/health", s.handleHealth)
	s.mux.HandleFunc("/api/overview", s.withAPIKey(s.handleOverview))
	s.mux.HandleFunc("/api/rewarm", s.withAPIKey(s.handleRewarm))
	s.mux.HandleFunc("/v1/models", s.withAPIKey(s.handleModels))
	s.mux.HandleFunc("/v1/chat/completions", s.withAPIKey(s.handleChatCompletions))
	s.mux.HandleFunc("/debug/auth-snapshot", s.withAPIKey(s.handleAuthSnapshot))
	s.mux.HandleFunc("/debug/endpoints", s.withAPIKey(s.handleEndpoints))

	s.mux.Handle("/assets/", webui.Handler())
	s.mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
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
	models := []map[string]any{
		{"id": "auto", "object": "model", "owned_by": "qoder"},
		{"id": "qwen3.7-plus", "object": "model", "owned_by": "qoder"},
		{"id": "glm-5.2", "object": "model", "owned_by": "qoder"},
	}
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
		"worker": worker,
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
		},
	})
}

func (s *Server) handleRewarm(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, "method_not_allowed", "POST only")
		return
	}
	workerURL := strings.TrimRight(firstNonEmpty(os.Getenv("QODER_WORKER_URL"), "http://127.0.0.1:3020"), "/")
	req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, workerURL+"/admin/rewarm", bytes.NewReader([]byte("{}")))
	if err != nil {
		writeErr(w, http.StatusBadGateway, "rewarm_failed", err.Error())
		return
	}
	req.Header.Set("Content-Type", "application/json")
	if key := strings.TrimSpace(firstNonEmpty(os.Getenv("QODER_WORKER_API_KEY"), s.cfg.ProxyAPIKey)); key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		writeErr(w, http.StatusBadGateway, "rewarm_failed", err.Error())
		return
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	_, _ = w.Write(body)
}

func (s *Server) fetchWorkerHealth() map[string]any {
	workerURL := strings.TrimRight(firstNonEmpty(os.Getenv("QODER_WORKER_URL"), "http://127.0.0.1:3020"), "/")
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
		"data": []map[string]any{
			{"id": "auto", "object": "model", "owned_by": "qoder"},
			{"id": "qwen3.7-plus", "object": "model", "owned_by": "qoder"},
			{"id": "glm-5.2", "object": "model", "owned_by": "qoder"},
		},
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

	if req.Stream {
		upstream, err := s.executor.ChatStreamProxy(r.Context(), req)
		if err != nil {
			writeErr(w, http.StatusServiceUnavailable, "upstream_not_ready", err.Error())
			return
		}
		defer upstream.Body.Close()
		w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.WriteHeader(http.StatusOK)
		if _, err := io.Copy(w, upstream.Body); err != nil {
			return
		}
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		return
	}

	res, err := s.executor.ChatNonStream(r.Context(), req)
	if err != nil {
		writeErr(w, http.StatusServiceUnavailable, "upstream_not_ready", err.Error())
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
		"usage": map[string]any{
			"prompt_tokens":     res.PromptTokens,
			"completion_tokens": res.CompletionTokens,
			"total_tokens":      res.PromptTokens + res.CompletionTokens,
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
