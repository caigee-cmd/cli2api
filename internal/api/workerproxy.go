package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// workerBase returns the URL of the first running account session. Prefer
// workerForAccount with an explicit id.
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

// proxyAccountWorker forwards a console request to the per-account Node worker
// and optionally syncs the account auth_type after a successful login.
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
	if key := s.cfg.ProxyAPIKey; key != "" {
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

func (s *Server) workerGet(path string, timeout time.Duration, accountID string) (*http.Response, error) {
	workerURL := s.workerForAccount(accountID)
	if workerURL == "" {
		return nil, fmt.Errorf("no running Qoder account")
	}
	req, err := http.NewRequest(http.MethodGet, workerURL+path, nil)
	if err != nil {
		return nil, err
	}
	if key := s.cfg.ProxyAPIKey; key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	if accountID != "" {
		req.Header.Set("X-Qoder-Account", accountID)
	}
	client := &http.Client{Timeout: timeout}
	return client.Do(req)
}

var fallbackModels = []map[string]any{
	{"id": "auto", "object": "model", "owned_by": "qoder"},
	{"id": "qwen3.7-plus", "object": "model", "owned_by": "qoder"},
	{"id": "glm-5.2", "object": "model", "owned_by": "qoder"},
	{"id": "MiniMax-M3", "mapped_key": "mmodel", "object": "model", "owned_by": "qoder"},
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
		return fallbackModels
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var parsed struct {
		Data []map[string]any `json:"data"`
	}
	if json.Unmarshal(body, &parsed) != nil || len(parsed.Data) == 0 {
		return fallbackModels
	}
	return parsed.Data
}
