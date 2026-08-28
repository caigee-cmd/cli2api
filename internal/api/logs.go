package api

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

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
	case path == "stats" && r.Method == http.MethodGet:
		s.handleRequestStats(w, r)
	default:
		writeErr(w, http.StatusNotFound, "not_found", "unknown logs endpoint")
	}
}

func (s *Server) handleRequestStats(w http.ResponseWriter, r *http.Request) {
	if s.recorder == nil || s.recorder.Store() == nil {
		writeErr(w, http.StatusServiceUnavailable, "logs_unavailable", "request logs unavailable")
		return
	}
	now := time.Now().UTC()
	hours := 24
	if raw := strings.TrimSpace(r.URL.Query().Get("hours")); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil {
			hours = n
		}
	}
	if hours != 1 && hours != 24 && hours != 168 {
		hours = 24
	}
	to := parseQueryTime(r.URL.Query().Get("to"), true)
	if to == nil {
		value := now
		to = &value
	}
	from := parseQueryTime(r.URL.Query().Get("from"), false)
	if from == nil {
		value := to.Add(-time.Duration(hours) * time.Hour)
		from = &value
	}
	stats, err := s.recorder.Store().SummarizeRequestLogs(r.Context(), *from, *to)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "stats_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, stats)
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
		Model:     r.URL.Query().Get("model"),
		Query:     r.URL.Query().Get("q"),
		From:      parseQueryTime(r.URL.Query().Get("from"), false),
		To:        parseQueryTime(r.URL.Query().Get("to"), true),
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
	offset := 0
	if raw := strings.TrimSpace(r.URL.Query().Get("offset")); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil {
			offset = n
		}
	}
	entries, total := s.ring.Snapshot(afterID, limit, offset, r.URL.Query().Get("level"), r.URL.Query().Get("q"), r.URL.Query().Get("account"))
	writeJSON(w, http.StatusOK, map[string]any{
		"items":  entries,
		"count":  len(entries),
		"total":  total,
		"limit":  clampRuntimeLimit(limit),
		"offset": clampRuntimeOffset(offset),
	})
}

func clampRuntimeLimit(limit int) int {
	if limit <= 0 || limit > 500 {
		return 200
	}
	return limit
}

func clampRuntimeOffset(offset int) int {
	if offset < 0 {
		return 0
	}
	return offset
}

func parseQueryTime(raw string, endOfDay bool) *time.Time {
	text := strings.TrimSpace(raw)
	if text == "" {
		return nil
	}
	layouts := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02T15:04:05Z07:00",
		"2006-01-02T15:04:05",
		"2006-01-02T15:04",
		"2006-01-02 15:04:05",
		"2006-01-02",
	}
	for _, layout := range layouts {
		parsed, err := time.Parse(layout, text)
		if err != nil {
			continue
		}
		if layout == "2006-01-02" && endOfDay {
			parsed = parsed.Add(24*time.Hour - time.Nanosecond)
		}
		utc := parsed.UTC()
		return &utc
	}
	return nil
}
