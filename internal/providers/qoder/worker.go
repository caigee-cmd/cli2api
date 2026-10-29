package qoder

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/caigee-cmd/cli2api/internal/accounts"
	"github.com/caigee-cmd/cli2api/internal/endpoint"
	"github.com/caigee-cmd/cli2api/internal/providers"
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

// defaultFirstByteTimeout caps how long a streaming chat request will wait for
// the upstream response head (status + headers). Once the head arrives, the
// timer is stopped and the SSE body streams under the parent context's lifetime,
// so long thinking-model output is not cut off. A stuck upstream that never
// sends a first byte surfaces as ErrFirstByteTimeout so the executor can fail
// over to the next account instead of hanging forever. 60s covers slow
// cold-starts and thinking-model first-token latency while keeping a
// stuck-upstream failover inside one round-trip of the client's patience.
//
// The effective value is resolved once at construction (see
// resolveFirstByteTimeout) and can be tuned per deployment with
// QODER_STREAM_TTFB_MS, mirroring the worker daemon's own knob.
const defaultFirstByteTimeout = 60 * time.Second

// resolveFirstByteTimeout reads QODER_STREAM_TTFB_MS, falling back to
// defaultFirstByteTimeout for an absent or invalid value. The floor matches the
// worker daemon's so the two deadlines cannot be configured to disagree wildly.
func resolveFirstByteTimeout() time.Duration {
	raw := strings.TrimSpace(os.Getenv("QODER_STREAM_TTFB_MS"))
	if raw == "" {
		return defaultFirstByteTimeout
	}
	ms, err := strconv.Atoi(raw)
	if err != nil || ms <= 0 {
		return defaultFirstByteTimeout
	}
	d := time.Duration(ms) * time.Millisecond
	if d < 5*time.Second {
		return 5 * time.Second
	}
	return d
}

// doWithFirstByteTimeout sends req with client, returning a response once the
// response head is received. If the head does not arrive within timeout it
// returns an error wrapping providers.ErrFirstByteTimeout; if the parent
// context was already cancelled it surfaces that error unchanged so the
// executor classifies it as a client cancellation rather than a timeout.
//
// The TTFB context must NOT be cancelled once the head arrives: Go's HTTP
// client ties the response body to the request context, so cancelling would
// kill the body mid-stream (the SSE read then fails with "context canceled"
// after the head was already delivered). Only the error path cancels; on
// success the deadline goroutine is stopped and the body lives under the
// parent context, with the response body's Close as the eventual release.
func doWithFirstByteTimeout(client *http.Client, req *http.Request, timeout time.Duration) (*http.Response, error) {
	if timeout <= 0 {
		timeout = defaultFirstByteTimeout
	}
	parent := req.Context()
	ttfbCtx, cancelTTFB := context.WithCancel(parent)
	timer := time.AfterFunc(timeout, cancelTTFB)
	resp, err := client.Do(req.WithContext(ttfbCtx))
	if err == nil {
		// Head arrived: stop the timer but keep ttfbCtx alive so the body
		// remains readable. Tie the cancel to Body.Close so the derived context
		// is released exactly when the caller is done with the stream, while
		// the parent context still governs its lifetime (client disconnect /
		// shutdown). Cancelling here instead would kill the body mid-stream.
		timer.Stop()
		resp.Body = &cancelOnCloseBody{ReadCloser: resp.Body, cancel: cancelTTFB}
		return resp, nil
	}
	timer.Stop()
	// TTFB fired only when the parent context is still alive: a parent cancel
	// (client disconnect, shutdown) must stay a plain cancel so the executor
	// does not treat it as an upstream failure worth cooling down. Decide this
	// before cancelTTFB below, which would otherwise make ttfbCtx.Err() non-nil
	// regardless of the real cause.
	timedOut := parent.Err() == nil && ttfbCtx.Err() != nil && !errors.Is(parent.Err(), context.Canceled)
	cancelTTFB()
	if timedOut {
		return nil, fmt.Errorf("%w: no response head within %s", providers.ErrFirstByteTimeout, timeout)
	}
	return nil, TransportError{Err: err}
}

// cancelOnCloseBody releases the TTFB-derived context when the caller closes
// the response body. The body itself is tied to that context by Go's HTTP
// client, so cancelling earlier would truncate the stream; cancelling on Close
// frees the timer goroutine's context exactly when the stream is finished.
type cancelOnCloseBody struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (b *cancelOnCloseBody) Close() error {
	err := b.ReadCloser.Close()
	if b.cancel != nil {
		b.cancel()
		b.cancel = nil
	}
	return err
}

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

type AdminSpec struct {
	Path               string
	WaitForAuthManager bool
	SyncAuth           string
}

func AdminAction(action string) (AdminSpec, bool) {
	switch strings.TrimSpace(action) {
	case "login/device":
		return AdminSpec{Path: "/admin/login/device", WaitForAuthManager: true}, true
	case "login/status":
		return AdminSpec{Path: "/admin/login/status", SyncAuth: "oauth_if_complete"}, true
	case "login/pat":
		return AdminSpec{Path: "/admin/login/pat", WaitForAuthManager: true, SyncAuth: "pat"}, true
	case "rewarm":
		return AdminSpec{Path: "/admin/rewarm"}, true
	default:
		return AdminSpec{}, false
	}
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
