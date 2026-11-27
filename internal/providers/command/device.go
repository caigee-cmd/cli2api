package command

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"runtime"
	"strings"
	"sync"
)

// The official CLI announces itself on every request with a stable set of
// identity headers and, once per key per process, two best-effort telemetry
// calls (a device fingerprint and a lifecycle heartbeat). Command Code's docs
// call out proxying as a TOS violation that leads to account bans, and the
// reverse-engineered clients all reproduce this surface, so the adapter mimics
// it instead of sending a naked HTTP request.
//
// Everything here is derived deterministically from the API key: one key must
// keep reporting one device across restarts and goroutines, otherwise upstream
// sees a single account rotating through many "machines" - the exact signal
// those clients avoid.

// identityHeaders are the CLI's non-auth identity headers.
func setIdentityHeaders(req *http.Request, credential Credential) {
	// x-command-code-version / x-cli-environment / x-session-id are set by the
	// caller; these are the remaining ones the CLI always sends.
	req.Header.Set("x-cli-surface", "cli")
	req.Header.Set("x-taste-learning", "false")
	if slug := projectSlug(credential); slug != "" {
		req.Header.Set("x-project-slug", slug)
	}
}

// fingerprintOnce memoizes the per-key device record so the pre-request runs at
// most once per key per process, the way the CLI records it once per login.
var (
	fingerprintMu   sync.Mutex
	fingerprintSeen = map[string]bool{}
)

// projectSlug mirrors the CLI's x-project-slug, which is a slug of the fake
// working directory. A stable per-key value keeps the pseudo-device coherent
// with config.workingDir.
func projectSlug(credential Credential) string {
	sum := sha256.Sum256([]byte("command-code:project:" + credential.APIKey))
	return "proj-" + hex.EncodeToString(sum[:6])
}

// deviceComponents is the `components` object upstream receives on
// /alpha/fingerprint/record. Values are hashed so no real host detail leaks,
// but the shape matches what the CLI reports.
type deviceComponents struct {
	MachineIDHash string   `json:"machineIdHash"`
	MacHashes     []string `json:"macHashes"`
	OSUserHash    string   `json:"osUserHash"`
	HostnameHash  string   `json:"hostnameHash"`
	Platform      string   `json:"platform"`
	Arch          string   `json:"arch"`
	CPUModel      string   `json:"cpuModel"`
	CPUs          int      `json:"cpuCount"`
	MemGiB        int      `json:"memGiB"`
	IsContainer   bool     `json:"isContainer"`
	Runtime       string   `json:"runtime"`
	CollectorVer  string   `json:"collectorVersion"`
	OSRelease     string   `json:"osRelease,omitempty"`
}

// fingerprintSalt is the CLI's fingerprint namespace constant.
const fingerprintSalt = "command-code:device-fingerprint:v1"

// hashSignal mirrors the CLI's hashSignal: a salted SHA-256 hex digest. Empty
// input stays empty so absent signals are omitted rather than hashed as "".
func hashSignal(signal string) string {
	signal = strings.TrimSpace(signal)
	if signal == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(fingerprintSalt + "\x00" + signal))
	return hex.EncodeToString(sum[:])
}

// buildDeviceFingerprint derives a stable thumbmark + components from the key.
func buildDeviceFingerprint(credential Credential) (string, deviceComponents) {
	seed := credential.APIKey
	sum := sha256.Sum256([]byte(fingerprintSalt + "\x00device\x00" + seed))
	thumbmark := hex.EncodeToString(sum[:])

	// Two stable pseudo-MACs per key, the way a real host reports NICs.
	macs := make([]string, 0, 2)
	for i := 0; i < 2; i++ {
		h := sha256.Sum256([]byte(fmt.Sprintf("%s:mac:%d", seed, i)))
		macs = append(macs, strings.ToLower(fmt.Sprintf("%02x:%02x:%02x:%02x:%02x:%02x",
			h[0], h[1], h[2], h[3], h[4], h[5])))
	}
	osUser := ""
	if u := os.Getenv("USER"); u != "" {
		osUser = u
	}
	hostname, _ := os.Hostname()
	cpu := runtime.GOARCH
	return thumbmark, deviceComponents{
		MachineIDHash: hashSignal(seed),
		MacHashes:     []string{hashSignal(macs[0]), hashSignal(macs[1])},
		OSUserHash:    hashSignal(osUser),
		HostnameHash:  hashSignal(hostname),
		Platform:      runtime.GOOS,
		Arch:          runtime.GOARCH,
		CPUModel:      cpu,
		CPUs:          runtime.NumCPU(),
		IsContainer:   false,
		Runtime:       "cli",
		CollectorVer:  "command-code:device-fingerprint:v1",
	}
}

// recordFingerprint posts /alpha/fingerprint/record once per key per process.
// Best effort: any failure is ignored, exactly like the CLI's background call.
func (c *Client) recordFingerprint(ctx context.Context, credential Credential) {
	key := credential.APIKey
	fingerprintMu.Lock()
	if fingerprintSeen[key] {
		fingerprintMu.Unlock()
		return
	}
	fingerprintSeen[key] = true
	fingerprintMu.Unlock()

	thumbmark, components := buildDeviceFingerprint(credential)
	body, err := json.Marshal(map[string]any{
		"thumbmark":  thumbmark,
		"components": components,
	})
	if err != nil {
		return
	}
	c.postTelemetry(ctx, credential, PathFingerprint, body)
}

// recordLifecycle posts the CLI's cli_session_exists heartbeat. Best effort.
func (c *Client) recordLifecycle(ctx context.Context, credential Credential) {
	body, err := json.Marshal(map[string]any{
		"eventType": "cli_session_exists",
		"metadata": map[string]any{
			"sessionId":  newSessionID(),
			"cliVersion": c.cliVersion(),
			"mode":       "non-interactive",
			"os":         runtime.GOOS + "-" + runtime.GOARCH,
		},
	})
	if err != nil {
		return
	}
	c.postTelemetry(ctx, credential, PathLifecycle, body)
}

// postTelemetry sends one JSON body with the CLI's identity headers. Telemetry
// never gates a chat request, so the response is drained and discarded.
func (c *Client) postTelemetry(ctx context.Context, credential Credential, path string, body []byte) {
	client, err := c.httpClient(ctx, credential.APIKey)
	if err != nil || client == nil {
		return
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpointBase(credential)+path, strings.NewReader(string(body)))
	if err != nil {
		return
	}
	req.Header.Set("Authorization", "Bearer "+credential.APIKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-cli-environment", CLIEnvironment)
	req.Header.Set("x-command-code-version", c.cliVersion())
	req.Header.Set("x-session-id", newSessionID())
	req.Header.Set("User-Agent", UserAgent)
	setIdentityHeaders(req, credential)
	resp, err := client.Do(req)
	if err != nil {
		return
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
	resp.Body.Close()
}

// newTraceparent returns a W3C trace-context header value in the CLI's shape:
// 00-<trace-id>-<span-id>-01.
func newTraceparent() string {
	var t [24]byte
	if _, err := rand.Read(t[:]); err != nil {
		return ""
	}
	traceID := hex.EncodeToString(t[:16])
	spanID := hex.EncodeToString(t[16:24])
	return "00-" + traceID + "-" + spanID + "-01"
}

// ensureDeviceRegistration fires both telemetry calls in the background the
// first time a credential is used, mirroring the CLI's startup sequence.
func (c *Client) ensureDeviceRegistration(ctx context.Context, credential Credential) {
	if ctx == nil {
		ctx = context.Background()
	}
	go func() {
		defer func() { _ = recover() }()
		c.recordFingerprint(ctx, credential)
		c.recordLifecycle(ctx, credential)
	}()
}
