package qoder

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/caigee-cmd/cli2api/internal/accounts"
	"github.com/caigee-cmd/cli2api/internal/endpoint"
)

var (
	ErrAccountNotRunning = errors.New("account is disabled or not running")
	ErrWorkerNotWarm     = errors.New("account worker is still starting")
)

type Health struct {
	OK             bool   `json:"ok"`
	Ready          bool   `json:"ready"`
	Hot            bool   `json:"hot"`
	UID            string `json:"uid"`
	InFlight       int    `json:"inFlight"`
	LastError      string `json:"lastError"`
	HasAuthManager bool   `json:"hasAuthManager"`
}

type WorkerClient struct {
	HTTP        *http.Client
	ProxyAPIKey string
	AccountID   string
}

type TransportError struct {
	Err error
}

func (e TransportError) Error() string {
	if e.Err == nil {
		return "worker transport error"
	}
	return e.Err.Error()
}

func (e TransportError) Unwrap() error { return e.Err }

type HTTPStatusError struct {
	Op     string
	Status int
	Body   string
}

func (e HTTPStatusError) Error() string {
	if e.Body != "" {
		return fmt.Sprintf("worker %s status %d body=%q", e.Op, e.Status, e.Body)
	}
	return fmt.Sprintf("worker %s status %d", e.Op, e.Status)
}

func (c WorkerClient) client() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return http.DefaultClient
}

func (c WorkerClient) authorize(req *http.Request) {
	if c.ProxyAPIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.ProxyAPIKey)
	}
	if c.AccountID != "" {
		req.Header.Set("X-Qoder-Account", c.AccountID)
	}
}

func (c WorkerClient) Health(ctx context.Context, workerURL string) (Health, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(workerURL, "/")+"/health", nil)
	if err != nil {
		return Health{}, 0, err
	}
	resp, err := c.client().Do(req)
	if err != nil {
		return Health{}, 0, TransportError{Err: err}
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	var health Health
	if err := json.Unmarshal(body, &health); err != nil {
		return Health{}, resp.StatusCode, err
	}
	return health, resp.StatusCode, nil
}

func (c WorkerClient) Models(ctx context.Context, workerURL string, refresh bool) (data []map[string]any, status int, rawBody string, err error) {
	path := strings.TrimRight(workerURL, "/") + "/admin/models"
	if refresh {
		path += "?refresh=1"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, 0, "", err
	}
	c.authorize(req)
	resp, err := c.client().Do(req)
	if err != nil {
		return nil, 0, "", TransportError{Err: err}
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	rawBody = string(body)
	var parsed struct {
		Data []map[string]any `json:"data"`
	}
	decodeErr := json.Unmarshal(body, &parsed)
	if resp.StatusCode >= 300 {
		return parsed.Data, resp.StatusCode, rawBody, nil
	}
	if decodeErr != nil {
		return nil, resp.StatusCode, rawBody, fmt.Errorf("decode worker models: %w", decodeErr)
	}
	return parsed.Data, resp.StatusCode, rawBody, nil
}

func CatalogIDs(entries []map[string]any, extras []string) []string {
	ids := append([]string{}, extras...)
	for _, entry := range entries {
		for _, key := range []string{"id", "mapped_key", "native_model", "display_name"} {
			value, _ := entry[key].(string)
			if strings.TrimSpace(value) != "" {
				ids = append(ids, value)
			}
		}
	}
	return ids
}

func (c WorkerClient) Quota(ctx context.Context, workerURL string, force bool) (*accounts.QuotaSnapshot, error) {
	path := strings.TrimRight(workerURL, "/") + "/admin/quota"
	if force {
		path += "?refresh=1"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	c.authorize(req)
	resp, err := c.client().Do(req)
	if err != nil {
		return nil, TransportError{Err: err}
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return nil, HTTPStatusError{Op: "quota", Status: resp.StatusCode}
	}
	var payload struct {
		Quota *workerQuota `json:"quota"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil || payload.Quota == nil {
		return nil, fmt.Errorf("decode worker quota")
	}
	return payload.Quota.snapshot(), nil
}

func NewChatRequest(ctx context.Context, workerURL, accountID, requestID, workerKey string, payload []byte) (*http.Request, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(workerURL, "/")+endpoint.ChatCompletionsPath, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if workerKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+workerKey)
	}
	if accountID != "" {
		httpReq.Header.Set("X-Qoder-Account", accountID)
	}
	if requestID != "" {
		httpReq.Header.Set("X-Request-Id", requestID)
	}
	return httpReq, nil
}

func (c WorkerClient) Admin(ctx context.Context, workerURL, method, path, contentType string, body []byte) (status int, header http.Header, responseBody []byte, err error) {
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(workerURL, "/")+path, bytes.NewReader(body))
	if err != nil {
		return 0, nil, nil, err
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	c.authorize(req)
	resp, err := c.client().Do(req)
	if err != nil {
		return 0, nil, nil, TransportError{Err: err}
	}
	defer resp.Body.Close()
	responseBody, _ = io.ReadAll(resp.Body)
	return resp.StatusCode, resp.Header.Clone(), responseBody, nil
}

func LoginCompleteAuthType(syncAuth string, responseBody []byte) string {
	if syncAuth == "" {
		return ""
	}
	if syncAuth != "oauth_if_complete" {
		return syncAuth
	}
	var status struct {
		Login struct {
			Status string `json:"status"`
		} `json:"login"`
	}
	if json.Unmarshal(responseBody, &status) != nil || status.Login.Status != "ok" {
		return ""
	}
	return "oauth"
}

func WaitForAuthManager(ctx context.Context, lookup func() (string, bool), timeout, interval time.Duration) (string, error) {
	if timeout <= 0 {
		timeout = 90 * time.Second
	}
	if interval <= 0 {
		interval = 200 * time.Millisecond
	}
	deadline := time.Now().Add(timeout)
	probe := WorkerClient{HTTP: &http.Client{Timeout: 2 * time.Second}}
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
			lastErr = ErrAccountNotRunning
		} else {
			health, status, err := probe.Health(ctx, workerURL)
			if status >= 400 {
				lastErr = fmt.Errorf("worker health status %d", status)
			} else if err != nil {
				lastErr = err
			} else if health.HasAuthManager {
				return workerURL, nil
			} else {
				lastErr = ErrWorkerNotWarm
			}
		}
		if !time.Now().Before(deadline) {
			if lastErr == nil {
				lastErr = ErrWorkerNotWarm
			}
			if errors.Is(lastErr, ErrAccountNotRunning) {
				return "", lastErr
			}
			return "", fmt.Errorf("%w: %v", ErrWorkerNotWarm, lastErr)
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
