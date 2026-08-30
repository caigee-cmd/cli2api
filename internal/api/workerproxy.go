package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"
)

var (
	errAccountNotRunning = errors.New("account is disabled or not running")
	errWorkerNotWarm     = errors.New("account worker is still starting")

	workerLoginReadyTimeout  = 90 * time.Second
	workerLoginReadyInterval = 200 * time.Millisecond
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
	waitForLogin := path == "/admin/login/device" || path == "/admin/login/pat"
	var workerURL string
	if waitForLogin {
		readyURL, err := s.waitForWorkerLogin(r.Context(), accountID)
		if err != nil {
			if errors.Is(err, errAccountNotRunning) {
				writeErr(w, http.StatusConflict, "account_not_running", errAccountNotRunning.Error())
				return
			}
			writeErr(w, http.StatusServiceUnavailable, "not_ready", err.Error())
			return
		}
		workerURL = readyURL
	} else {
		var ok bool
		workerURL, ok = s.manager.AccountURL(accountID)
		if !ok {
			writeErr(w, http.StatusConflict, "account_not_running", "account is disabled or not running")
			return
		}
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

func (s *Server) fetchWorkerModels(refresh bool) []map[string]any {
	models, _ := s.fetchWorkerModelsFor(refresh, "")
	return models
}

func (s *Server) fetchWorkerModelsFor(refresh bool, accountID string) ([]map[string]any, error) {
	models, err := s.fetchProviderModels(refresh, accountID)
	if err != nil {
		return nil, err
	}
	if models != nil {
		return models, nil
	}
	path := "/admin/models"
	if refresh {
		path += "?refresh=1"
	}
	resp, err := s.workerGet(path, 60*time.Second, accountID)
	if err != nil {
		if accountID != "" {
			return nil, err
		}
		return nil, nil
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var parsed struct {
		Data []map[string]any `json:"data"`
	}
	if json.Unmarshal(body, &parsed) != nil || len(parsed.Data) == 0 {
		return nil, nil
	}
	for _, model := range parsed.Data {
		if model == nil {
			continue
		}
		if _, ok := model["provider"]; !ok {
			model["provider"] = "qoder"
		}
		if _, ok := model["owned_by"]; !ok {
			model["owned_by"] = "qoder"
		}
	}
	return parsed.Data, nil
}

// fetchProviderModels merges catalogs across accounts. Qoder models come from
// worker daemons; in-process providers come from their adapters. Each entry is
// tagged with the provider that actually serves it.
func (s *Server) fetchProviderModels(refresh bool, accountID string) ([]map[string]any, error) {
	var merged []map[string]any
	seen := map[string]struct{}{}
	sawAny := false
	var lastErr error
	for _, item := range s.pool.Items() {
		if accountID != "" && item.ID != accountID {
			continue
		}
		if item.Provider == "" || item.Provider == "qoder" {
			continue
		}
		adapter, ok := s.providers.Get(item.Provider)
		if !ok || adapter.Models == nil {
			continue
		}
		models, err := adapter.Models.Models(context.Background(), item.ID)
		if err != nil {
			log.Printf("catalog fetch failed account=%s provider=%s: %v", item.ID, item.Provider, err)
			lastErr = err
			if accountID != "" {
				return nil, err
			}
			continue
		}
		sawAny = true
		for _, model := range models {
			key := model.NativeModel + "@" + item.Provider
			if _, dup := seen[key]; dup {
				continue
			}
			seen[key] = struct{}{}
			entry := map[string]any{
				"id": model.PublicModel, "object": "model", "owned_by": item.Provider,
				"provider": item.Provider, "native_model": model.NativeModel,
			}
			if strings.TrimSpace(model.DisplayName) != "" {
				entry["display_name"] = model.DisplayName
			}
			if model.Capabilities.ContextWindow > 0 {
				entry["catalog_context_length"] = model.Capabilities.ContextWindow
			}
			if model.Capabilities.ContextWindowMax > 0 {
				entry["catalog_context_length_max"] = model.Capabilities.ContextWindowMax
			}
			if model.Capabilities.MaxOutput > 0 {
				entry["max_output_tokens"] = model.Capabilities.MaxOutput
			}
			if model.Capabilities.PromptMaxTokens > 0 {
				entry["prompt_max_tokens"] = model.Capabilities.PromptMaxTokens
			}
			if model.Capabilities.MaxMode {
				entry["supports_max_mode"] = true
			}
			if len(model.Capabilities.ReasoningOptions) > 0 {
				entry["reasoning_options"] = model.Capabilities.ReasoningOptions
			}
			if model.Capabilities.ReasoningDefault != "" {
				entry["reasoning_default"] = model.Capabilities.ReasoningDefault
			}
			if model.Capabilities.ReasoningType != "" {
				entry["reasoning_type"] = model.Capabilities.ReasoningType
			}
			if model.Capabilities.CanDisableThinking {
				entry["can_disable_thinking"] = true
			}
			merged = append(merged, entry)
		}
	}
	if !sawAny {
		if accountID != "" && lastErr != nil {
			return nil, lastErr
		}
		return nil, nil
	}
	// Merge Qoder daemon models alongside in-process providers.
	var qoderModels []map[string]any
	for _, item := range s.pool.Items() {
		if item.Provider != "qoder" || item.URL == "" {
			continue
		}
		if accountID != "" && item.ID != accountID {
			continue
		}
		qoderModels = append(qoderModels, s.fetchQoderModels(refresh, item.ID)...)
	}
	for _, model := range qoderModels {
		key, _ := model["id"].(string)
		if _, dup := seen[key+"@qoder"]; dup {
			continue
		}
		seen[key+"@qoder"] = struct{}{}
		model["provider"] = "qoder"
		model["owned_by"] = "qoder"
		merged = append(merged, model)
	}
	return merged, nil
}

func (s *Server) fetchQoderModels(refresh bool, accountID string) []map[string]any {
	path := "/admin/models"
	if refresh {
		path += "?refresh=1"
	}
	resp, err := s.workerGet(path, 60*time.Second, accountID)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var parsed struct {
		Data []map[string]any `json:"data"`
	}
	if json.Unmarshal(body, &parsed) != nil {
		return nil
	}
	return parsed.Data
}

func (s *Server) waitForWorkerLogin(ctx context.Context, accountID string) (string, error) {
	workerURL, ok := s.manager.AccountURL(accountID)
	if !ok || strings.TrimSpace(workerURL) == "" {
		return "", errAccountNotRunning
	}
	return waitForWorkerAuthManager(ctx, func() (string, bool) {
		return workerURL, true
	}, workerLoginReadyTimeout, workerLoginReadyInterval)
}

func waitForWorkerAuthManager(ctx context.Context, lookup func() (string, bool), timeout, interval time.Duration) (string, error) {
	if timeout <= 0 {
		timeout = 90 * time.Second
	}
	if interval <= 0 {
		interval = 200 * time.Millisecond
	}
	deadline := time.Now().Add(timeout)
	client := &http.Client{Timeout: 2 * time.Second}
	var lastErr error
	for {
		if err := ctx.Err(); err != nil {
			if lastErr != nil {
				return "", lastErr
			}
			return "", err
		}
		workerURL, ok := lookup()
		workerURL = strings.TrimRight(strings.TrimSpace(workerURL), "/")
		if !ok || workerURL == "" {
			lastErr = errAccountNotRunning
		} else {
			ready, err := workerReportsAuthManager(ctx, client, workerURL)
			if err != nil {
				lastErr = err
			} else if ready {
				return workerURL, nil
			} else {
				lastErr = errWorkerNotWarm
			}
		}
		if !time.Now().Before(deadline) {
			if lastErr == nil {
				lastErr = errWorkerNotWarm
			}
			if errors.Is(lastErr, errAccountNotRunning) {
				return "", lastErr
			}
			return "", fmt.Errorf("%w: %v", errWorkerNotWarm, lastErr)
		}
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			if lastErr != nil {
				return "", lastErr
			}
			return "", ctx.Err()
		case <-timer.C:
		}
	}
}

func workerReportsAuthManager(ctx context.Context, client *http.Client, workerURL string) (bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, workerURL+"/health", nil)
	if err != nil {
		return false, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 400 {
		return false, fmt.Errorf("worker health status %d", resp.StatusCode)
	}
	var health struct {
		HasAuthManager bool `json:"hasAuthManager"`
	}
	if err := json.Unmarshal(body, &health); err != nil {
		return false, err
	}
	return health.HasAuthManager, nil
}
