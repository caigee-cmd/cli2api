package meta

import (
	"runtime"
	"time"
)

// ProviderID is the canonical family id. Alias "muse" resolves to this at
// registry Get / NormalizeProviderFamily only; it is not a second family.
const ProviderID = "meta"

// Credential formats. OAuth Muse Code is the MVP login path; PAT lets an
// operator paste a minted Meta Model API key (same wire Bearer as the mint).
const (
	CredentialFormatOAuth = "meta-oauth-v1"
	CredentialFormatKey   = "meta-key-v1"
)

// Wire constants aligned with CPA v8.0.40 (internal/auth/meta + meta_executor).
const (
	ClientID = "1031625952748946"
	AuthHost = "https://auth.meta.com"
	ChatBase = "https://api.meta.ai/v1"
	MintURL  = "https://api.meta.ai/muse-code/key"

	DeviceAuthorizationPath = "/oidc/device/authorization/"
	TokenPath               = "/oidc/device/token/"
	DeviceGrantType         = "urn:ietf:params:oauth:grant-type:device_code"

	// AuthUserAgent is sent on device/token/mint and has not moved with the
	// CLI (CPA and every community client still send muse-code/1.0.2).
	AuthUserAgent = "muse-code/1.0.2"
	// ClientIDHeader marks the caller as a non-interactive Muse Code client.
	// `tbh:exec` is what the gateway-shaped clients send (CLIProxyAPI v8.0.63,
	// magpie-muse-auth); `tbh:tui` belongs to the interactive terminal.
	ClientIDHeader = "tbh:exec"

	ResponsesPath = "/responses"

	// MuseCodeVersion / MuseCodeBuild are the client version and build hash the
	// official CLI reported on 2026-11-16 (Muse Code 1.4.14-R7309.1, captured
	// by magpie-muse-auth and shipped by CLIProxyAPI v8.0.63). They must move
	// together: a new version with an old build hash is an inconsistent
	// fingerprint, and Meta can tell. The SDK line is already at 1.4.25
	// (2026-11-21), but no published build hash for it exists yet, so the
	// verified pair stays pinned until one does.
	MuseCodeVersion = "1.4.14"
	MuseCodeBuild   = "dec7d7497fecdad135ec5835a8161dbc92e55b58"
)

// ChatUserAgent is the muse-build UA for Responses calls. Platform comes from
// the host, the way CLIProxyAPI builds it from runtime.GOOS/GOARCH.
var ChatUserAgent = museBuildUserAgent()

func museBuildUserAgent() string {
	osName := runtime.GOOS
	switch osName {
	case "darwin":
		osName = "macos"
	}
	arch := runtime.GOARCH
	switch arch {
	case "amd64":
		arch = "x86_64"
	case "arm64":
		arch = "aarch64"
	}
	return "muse-build/" + MuseCodeVersion + " (non-interactive; " + osName + "-" + arch + "; build " + MuseCodeBuild + ")"
}

const (
	deviceLoginTimeout = 15 * time.Minute
	pollFloor          = 5 * time.Second
	pollSlowDownStep   = 5 * time.Second
	maxPollInterval    = 60 * time.Second
	metadataTimeout    = 20 * time.Second
)
