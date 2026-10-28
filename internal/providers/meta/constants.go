package meta

import "time"

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
	ClientID   = "1031625952748946"
	AuthHost   = "https://auth.meta.com"
	ChatBase   = "https://api.meta.ai/v1"
	MintURL    = "https://api.meta.ai/muse-code/key"

	DeviceAuthorizationPath = "/oidc/device/authorization/"
	TokenPath               = "/oidc/device/token/"
	DeviceGrantType         = "urn:ietf:params:oauth:grant-type:device_code"

	// AuthUserAgent is sent on device/token/mint (CPA muse-code/1.0.2).
	AuthUserAgent = "muse-code/1.0.2"
	// ChatUserAgent and ClientIDHeader match CPA meta_executor fingerprints.
	ChatUserAgent   = "muse-build/1.3.0 (interactive; macos-aarch64; build ac7280f2aca67769d1455a8847bb502b617d50f6)"
	ClientIDHeader  = "tbh:tui"
	ResponsesPath   = "/responses"
)

const (
	deviceLoginTimeout = 15 * time.Minute
	pollFloor          = 5 * time.Second
	pollSlowDownStep   = 5 * time.Second
	maxPollInterval    = 60 * time.Second
	metadataTimeout    = 20 * time.Second
)
