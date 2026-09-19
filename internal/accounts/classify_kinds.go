package accounts

import (
	"strings"
)

const (
	KindQuota             = "quota"
	KindRateLimit         = "rate_limit"
	KindAuth              = "auth"
	KindNotReady          = "not_ready"
	KindUnavailable       = "unavailable"
	KindInvalidRequest    = "invalid_request"
	KindModelNotAvailable = "model_not_available"
	KindCanceled          = "canceled"
)

// BackoffMaxLevel caps the exponential backoff ladder applied to repeated
// failures of the same kind. The classifier owns the duration math; this
// bound is shared with persist/restore so a restart cannot resume past it.
const BackoffMaxLevel = 8

// ClampBackoffLevel bounds a persisted or restored backoff ladder value.
func ClampBackoffLevel(level int) int {
	if level < 0 {
		return 0
	}
	if level > BackoffMaxLevel {
		return BackoffMaxLevel
	}
	return level
}

func promptLimitLike(lower string) bool {
	return strings.Contains(lower, "token-limit") ||
		strings.Contains(lower, "#token-limit") ||
		strings.Contains(lower, "oversized prompt") ||
		strings.Contains(lower, "prompt too large") ||
		strings.Contains(lower, "prompt too long") ||
		strings.Contains(lower, "context length") ||
		strings.Contains(lower, "local precheck rejected")
}

func IsPromptLimitText(text string) bool {
	return promptLimitLike(strings.ToLower(text))
}

// IsInvalidRequestText reports whether an error body looks like an upstream
// content-screening rejection: the request itself is the problem, so no
// account should fail over or cool down. Quota and prompt-limit shapes are
// matched earlier in Classify and never reach this check.
func IsInvalidRequestText(text string) bool {
	lower := strings.ToLower(text)
	return strings.Contains(lower, "sensitive") ||
		strings.Contains(lower, "敏感") ||
		strings.Contains(lower, "违规") ||
		strings.Contains(lower, "风险") ||
		strings.Contains(lower, "拦截") ||
		strings.Contains(lower, "moderation") ||
		strings.Contains(lower, "content filter") ||
		strings.Contains(lower, "content_filter")
}
