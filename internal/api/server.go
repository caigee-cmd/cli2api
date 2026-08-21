package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/caigee-cmd/cli2api/internal/auth"
	"github.com/caigee-cmd/cli2api/internal/config"
	"github.com/caigee-cmd/cli2api/internal/endpoint"
	"github.com/caigee-cmd/cli2api/internal/executor"
	"github.com/caigee-cmd/cli2api/internal/translate"
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
	s.mux.HandleFunc("/v1/models", s.withAPIKey(s.handleModels))
	s.mux.HandleFunc("/v1/chat/completions", s.withAPIKey(s.handleChatCompletions))
	s.mux.HandleFunc("/debug/auth-snapshot", s.withAPIKey(s.handleAuthSnapshot))
	s.mux.HandleFunc("/debug/endpoints", s.withAPIKey(s.handleEndpoints))
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
		"service":  "qoder-api-proxy",
		"phase":    "C-preview",
		"chat_url": s.endpoints.ChatURL(),
		"time":     time.Now().UTC().Format(time.RFC3339),
	})
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
