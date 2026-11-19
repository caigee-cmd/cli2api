package kiro

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"runtime"
	"strconv"
	"strings"
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

var (
	lastVersionRecheck atomic.Int64
	lastVersionRefresh atomic.Int64
)

// ensureIDEVersion starts one background read of the stable feed, then one
// more per day. Chat does not wait for it; the fallback version is used until
// the feed answers with a build at least as new as the fallback.
func ensureIDEVersion(client *http.Client) {
	go refreshIDEVersion(client)
}

func refreshIDEVersion(client *http.Client) {
	now := time.Now().UnixNano()
	last := lastVersionRefresh.Load()
	fresh := last != 0 && now-last < int64(versionRefreshEvery)
	if fresh && currentIDEVersion() != IDEVersion {
		return
	}
	if !lastVersionRefresh.CompareAndSwap(last, now) {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	version, err := fetchIDEVersion(ctx, client)
	if err != nil || !newerIDEVersion(version, IDEVersion) {
		return
	}
	current, _ := ideVersion.Load().(string)
	if current != "" && !newerIDEVersion(version, current) {
		return
	}
	ideVersion.Store(version)
}

// newerIDEVersion reports whether a is a dotted numeric version above b.
// A version that does not parse is never newer, so a bad feed cannot move
// the advertised build backwards.
func newerIDEVersion(a, b string) bool {
	pa, ok := parseIDEVersion(a)
	if !ok {
		return false
	}
	pb, ok := parseIDEVersion(b)
	if !ok {
		return true
	}
	n := len(pa)
	if len(pb) > n {
		n = len(pb)
	}
	for i := 0; i < n; i++ {
		var x, y int
		if i < len(pa) {
			x = pa[i]
		}
		if i < len(pb) {
			y = pb[i]
		}
		if x != y {
			return x > y
		}
	}
	return false
}

func parseIDEVersion(v string) ([]int, bool) {
	parts := strings.Split(strings.TrimSpace(v), ".")
	if len(parts) < 2 || len(parts) > 4 {
		return nil, false
	}
	nums := make([]int, len(parts))
	for i, part := range parts {
		if part == "" || len(part) > 5 {
			return nil, false
		}
		n, err := strconv.Atoi(part)
		if err != nil || n < 0 || strconv.Itoa(n) != part {
			return nil, false
		}
		nums[i] = n
	}
	return nums, true
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

// systemFingerprint is the os/<name>#<release> the IDE puts in the SDK user
// agent. The release is the one the Kiro client advertises for that OS, not
// the host's: a newer kernel release is not a version the upstream has shipped.
func systemFingerprint() string {
	switch runtime.GOOS {
	case "windows":
		return "win32#10.0.22631"
	case "darwin":
		return "darwin#24.6.0"
	default:
		return "linux#6.6.87"
	}
}
