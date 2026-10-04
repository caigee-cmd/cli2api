package zhipu

// Official Zhipu endpoints. Pay-as-you-go and Coding Plan share the request
// shape; only the base URL changes. Quota is a separate monitor host, not a
// path under the chat base.
const (
	// CredentialFormat is the canonical stored credential format.
	CredentialFormat = "zhipu-key-v1"

	// PayGBaseURL is the pay-as-you-go OpenAI-compatible origin, already
	// including the /v4 version segment.
	PayGBaseURL = "https://open.bigmodel.cn/api/paas/v4"
	// CodingBaseURL is the Coding Plan OpenAI-compatible origin.
	CodingBaseURL = "https://open.bigmodel.cn/api/coding/paas/v4"

	// CNQuotaHost and GlobalQuotaHost serve GET /api/monitor/usage/quota/limit.
	// The quota host follows the chat host: bigmodel.cn stays on the CN site,
	// api.z.ai stays on the international site.
	CNQuotaHost     = "https://open.bigmodel.cn"
	GlobalQuotaHost = "https://api.z.ai"
	QuotaPath       = "/api/monitor/usage/quota/limit"

	// ModePayG and ModeCoding are the only accepted account modes.
	ModePayG   = "payg"
	ModeCoding = "coding"

	// QuotaUnit labels token-denominated Coding Plan windows. The upstream
	// reports a used percentage, not a raw token balance.
	QuotaUnit = "percent"
)
