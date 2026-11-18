package kiro

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// ideVersion is the Kiro IDE build advertised on every request. The stable
// update feed replaces the fallback; a failed read keeps whatever is current.
var ideVersion atomic.Value

func init() { ideVersion.Store(IDEVersion) }

func currentIDEVersion() string {
	value, _ := ideVersion.Load().(string)
	if strings.TrimSpace(value) == "" {
		return IDEVersion
	}
	return value
}

var versionOnce sync.Once
var lastVersionRecheck atomic.Int64

// ensureIDEVersion starts one background read of the stable feed. Chat does
// not wait for it; the fallback version is used until the feed answers.
func ensureIDEVersion(client *http.Client) {
	versionOnce.Do(func() {
		go refreshIDEVersion(client)
	})
}

func refreshIDEVersion(client *http.Client) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	version, err := fetchIDEVersion(ctx, client)
	if err != nil || version == "" {
		return
	}
	ideVersion.Store(version)
}

func fetchIDEVersion(ctx context.Context, client *http.Client) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, kiroMetadataURL, nil)
	if err != nil {
		return "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", errStatus(resp.StatusCode)
	}
	var feed struct {
		CurrentRelease string `json:"currentRelease"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&feed); err != nil {
		return "", err
	}
	return strings.TrimSpace(feed.CurrentRelease), nil
}

// noteVersionGate reads the feed again when Kiro refused the advertised IDE
// build. At most once per versionRecheckGap, and never on the request path
// beyond starting the read.
func noteVersionGate(client *http.Client, body string) {
	if !strings.Contains(body, "FEATURE_NOT_SUPPORTED") {
		return
	}
	now := time.Now().UnixNano()
	last := lastVersionRecheck.Load()
	if now-last < int64(versionRecheckGap) || !lastVersionRecheck.CompareAndSwap(last, now) {
		return
	}
	go refreshIDEVersion(client)
}

type statusError int

func (s statusError) Error() string { return http.StatusText(int(s)) }

func errStatus(code int) error { return statusError(code) }
