package kiro

import "time"

// Upstream facts pinned from the Kiro IDE client and kiro-reverse-api.
// CreateResponse is the chat path newer accounts answer; generateAssistantResponse
// remains the fallback when the account's feature flag is not create_response.
const (
	CredentialFormat = "kiro-oauth-v1"

	DefaultRegion = "us-east-1"

	Origin    = "AI_EDITOR"
	AgentMode = "vibe"

	// IDEVersion is the fallback advertised until the stable update feed answers.
	// Kiro's version gate rejects older builds with FEATURE_NOT_SUPPORTED, so the
	// fallback stays at the current stable release (1.5.37, published 2026-11-24
	// on prod.download.desktop.kiro.dev/stable). A fetched build older than this
	// is ignored.
	// AgentVersion is the bundled Kiro agent server (KAS) release. The package
	// ships @kiro/agent 0.66.123 inside IDE 1.5.37 (kiro.kiro-agent 1.1.1168,
	// product.json commit 03cd03a, 2026-11-24), which is what the IDE appends
	// as `KAS/<version>` to its user agent.
	IDEVersion   = "1.5.37"
	AgentVersion = "0.66.123"

	// nodeVersion is the Node runtime the IDE names in the AWS SDK user agent.
	nodeVersion = "22.22.0"

	// StreamingSDKVersion is the aws-sdk-js build the IDE names on the legacy host.
	StreamingSDKVersion = "1.0.34"
	RuntimeSDKVersion   = "1.0.0"

	ResponsesAttempts = 3
	legacyAttempts    = 2

	// maxModelPages bounds a broken upstream cursor without silently returning
	// a partial catalog. Kiro normally needs one or two pages.
	maxModelPages = 100

	// RefreshSkewSeconds refreshes an access token this long before expiry.
	RefreshSkewSeconds = 120

	maxEventStreamMessage = 16 << 20
	maxResponsesSSELine   = 4 << 20
	maxUpstreamErrorBytes = 64 << 10

	// maxToolNameLen is the longest tool name Kiro accepts. Longer client
	// names are shortened and mapped back on the way out.
	maxToolNameLen = 64
	// maxToolDescLen is the longest tool description Kiro accepts.
	maxToolDescLen = 10237

	webSearchClientName   = "web_search"
	webSearchUpstreamName = "webSearch"

	// builderIDProfileArn is the fixed profile every Builder ID client names.
	// Builder ID owns no profile, and ListAvailableProfiles answers 403, but
	// clients from 0.11.133 on reject a request that names none. It is never stored.
	builderIDProfileArn = "arn:aws:codewhisperer:us-east-1:638616132270:profile/AAAACCCCXXXX"

	featureConfigSalt     = "kiro-feature-config-key-salt-3e9b1d7a"
	createResponseFlag    = "create_response"
	featureFlagTTL        = 15 * time.Minute
	featureFlagRetry      = time.Minute
	featureFlagTimeout    = 5 * time.Second
	featureFlagRecheckGap = 30 * time.Second

	kiroMetadataURL     = "https://prod.download.desktop.kiro.dev/stable/metadata-linux-x64-stable.json"
	versionRecheckGap   = 10 * time.Minute
	versionRefreshEvery = 24 * time.Hour

	streamRetryBackoff = 700 * time.Millisecond

	socialRefreshURL = "https://prod.us-east-1.auth.desktop.kiro.dev/refreshToken"
	restAPIBase      = "https://codewhisperer.us-east-1.amazonaws.com"
	modelAPIBase     = "https://q.us-east-1.amazonaws.com"

	// builderIDStartURL is the issuer every Builder ID client registers with.
	builderIDStartURL = "https://view.awsapps.com/start"
	// ssoPortalBase is where an imported SSO bearer is exchanged for a device
	// session. It only exists in us-east-1.
	ssoPortalBase = "https://portal.sso.us-east-1.amazonaws.com"

	// microsoftSignInURL opens Kiro's own sign-in page, which redirects back
	// with the enterprise IdP descriptor. The port and paths are part of the
	// redirect Kiro registered, so they cannot move.
	microsoftSignInURL    = "https://app.kiro.dev/signin"
	microsoftLoopbackBase = "http://localhost:3128"
	microsoftPortalPath   = "/signin/callback"
	microsoftOAuthPath    = "/oauth/callback"

	// idcCallbackPath is the loopback path an Identity Center login redirects
	// to. The port is chosen per login; only the path is fixed.
	idcCallbackPath = "/oauth/callback"

	loginTTL = 10 * time.Minute
)

// kiroOIDCScopes are the scopes every Kiro OIDC client registers.
var kiroOIDCScopes = []string{
	"codewhisperer:completions",
	"codewhisperer:analysis",
	"codewhisperer:conversations",
	"codewhisperer:transformations",
	"codewhisperer:taskassist",
}

// serviceRegions are the data planes Kiro actually serves. Other AWS region
// codes name hosts that do not resolve.
var serviceRegions = []string{DefaultRegion, "eu-central-1"}
