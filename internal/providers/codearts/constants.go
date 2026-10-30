package codearts

import "time"

// ProviderID is the provider family id registered in the control plane.
const ProviderID = "codearts"

// CredentialFormat is the canonical stored credential format.
const CredentialFormat = "codearts-oauth-v1"

// ClientID is the OAuth client used by the official CodeArts Agent extension.
// The refresh token is bound to this value; refreshing with a different id is
// rejected by STS.
const ClientID = "codearts-agent"

// Endpoints. The chat gateway and the token service are different hosts:
// chat is the snap-access regional gateway, while OAuth token exchange and
// refresh go through the regional STS endpoint.
const (
	CNBaseURL    = "https://snap-access.cn-north-4.myhuaweicloud.com"
	CNSTSHost    = "https://sts.cn-north-4.myhuaweicloud.com"
	CNPortalHost = "https://codearts.huaweicloud.com/portal"
	BenefitHost  = "https://opengw.developer.huaweicloud.com"
)

// API paths. Paths are relative to CNBaseURL unless noted.
const (
	chatPath           = "/api/v2/chat/completions"
	loginTicketPath    = "/snap-manager/v1/login/ticket"
	currentUserPath    = "/snap-manager/v1/current/user"
	statisticsPath     = "/snap-manager/v1/statistics/plugin"
	modelBuiltinPath   = "/v1/model/builtin"
	thinkingVariants   = "/v1/model/thinking-variants"
	agentListPath      = "/v1/agent-center/agents/useragents"
	agentDetailPath    = "/v1/agent-center/agents/detail"
	creditDeliveryPath = "/v1/ops/delivery"
	creditClaimPath    = "/v1/ops/claim"
	creditConfirmPath  = "/v1/ops/confirm"
)

// OAuth and benefit paths.
const (
	oauthTokensPath = "/v1/oauth2/tokens"
	benefitConfig   = "/api/v1/gateway/config"
	benefitClaim    = "/api/v1/benefit/claim"
	benefitBalance  = "/api/v1/user/tokens/balance"
)

// Client identity reported to the OAuth portal and gateway.
const (
	PluginName    = "snap_AIIDE"
	PluginVersion = "5.2.0"
	OAuthRedirect = "/oauth/callback"
	Language      = "zh-cn"
	AppID         = "CodeAgent3.0"
)

// Timing knobs.
const (
	// refreshLead refreshes the STS credential this long before expiry so a
	// long stream never starts on a credential that dies mid-response.
	refreshLead = 15 * time.Minute
	// metadataTimeout bounds model / statistics / identity calls.
	metadataTimeout = 30 * time.Second
	// chatTimeout bounds one streaming HTTP request when the caller does not
	// set a shorter context.
	chatTimeout = 10 * time.Minute
	// firstByteTimeout is the maximum wait for response headers plus the first
	// SSE event. It is returned as providers.ErrFirstByteTimeout, never as a
	// context deadline, so the executor can fail over.
	firstByteTimeout = 90 * time.Second
	// catalogTTL caps how long a discovered model list is reused.
	catalogTTL = 30 * time.Minute
	// loginTTL bounds one browser login round.
	loginTTL = 15 * time.Minute
	// maxBodyBytes caps upstream error bodies retained for classification.
	maxBodyBytes = 1 << 20
)

// HeaderMaasType routes a request to the limited-benefit model gateway.
const (
	HeaderMaasType = "maas_type"
	MaasBenefit    = "benefit"
)
