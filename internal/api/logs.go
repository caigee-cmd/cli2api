package api

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/caigee-cmd/cli2api/internal/accounts"
)

func (s *Server) handleLogs(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/logs")
	path = strings.Trim(path, "/")
	switch {
	case path == "requests" && r.Method == http.MethodGet:
		s.handleListRequestLogs(w, r)
	case path == "requests" && r.Method == http.MethodDelete:
		s.handleClearRequestLogs(w, r)
	case strings.HasPrefix(path, "requests/") && r.Method == http.MethodGet:
		id := strings.TrimPrefix(path, "requests/")
		s.handleGetRequestLog(w, r, id)
	case path == "runtime" && r.Method == http.MethodGet:
		s.handleRuntimeLogs(w, r)
	default:
		writeErr(w, http.StatusNotFound, "not_found", "unknown logs endpoint")
	}
}

func (s *Server) handleListRequestLogs(w http.ResponseWriter, r *http.Request) {
	if s.recorder == nil || s.recorder.Store() == nil {
		writeErr(w, http.StatusServiceUnavailable, "logs_unavailable", "request logs unavailable")
		return
	}
	filter := accounts.RequestLogFilter{
		AccountID: r.URL.Query().Get("account"),
		Status:    r.URL.Query().Get("status"),
		ErrorKind: r.URL.Query().Get("error_kind"),
		Query:     r.URL.Query().Get("q"),
	}
	if raw := strings.TrimSpace(r.URL.Query().Get("stream")); raw != "" {
		value := raw == "1" || strings.EqualFold(raw, "true")
		filter.Stream = &value
	}
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil {
			filter.Limit = n
		}
	}
	if raw := strings.TrimSpace(r.URL.Query().Get("offset")); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil {
			filter.Offset = n
		}
	}
	list, err := s.recorder.Store().ListRequestLogs(r.Context(), filter)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "list_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleGetRequestLog(w http.ResponseWriter, r *http.Request, id string) {
	if s.recorder == nil || s.recorder.Store() == nil {
		writeErr(w, http.StatusServiceUnavailable, "logs_unavailable", "request logs unavailable")
		return
	}
	item, err := s.recorder.Store().GetRequestLog(r.Context(), id)
	if errors.Is(err, accounts.ErrRequestLogNotFound) {
		writeErr(w, http.StatusNotFound, "not_found", "request log not found")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "get_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) handleClearRequestLogs(w http.ResponseWriter, r *http.Request) {
	if s.recorder == nil || s.recorder.Store() == nil {
		writeErr(w, http.StatusServiceUnavailable, "logs_unavailable", "request logs unavailable")
		return
	}
	deleted, err := s.recorder.Store().ClearRequestLogs(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "clear_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "deleted": deleted})
}

func (s *Server) handleRuntimeLogs(w http.ResponseWriter, r *http.Request) {
	if s.ring == nil {
		writeErr(w, http.StatusServiceUnavailable, "logs_unavailable", "runtime logs unavailable")
		return
	}
	afterID := uint64(0)
	if raw := strings.TrimSpace(r.URL.Query().Get("after")); raw != "" {
		if n, err := strconv.ParseUint(raw, 10, 64); err == nil {
			afterID = n
		}
	}
	limit := 200
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil {
			limit = n
		}
	}
	entries := s.ring.Snapshot(afterID, limit, r.URL.Query().Get("level"), r.URL.Query().Get("q"))
	writeJSON(w, http.StatusOK, map[string]any{
		"items": entries,
		"count": len(entries),
	})
}
