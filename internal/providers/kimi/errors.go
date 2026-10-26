package kimi

import (
	"net/http"
	"regexp"
	"strings"

	"github.com/caigee-cmd/cli2api/internal/accounts"
	"github.com/caigee-cmd/cli2api/internal/providers"
)

var bearerPattern = regexp.MustCompile(`(?i)bearer\s+\S+`)

func redactSecrets(text string) string {
	if text == "" {
		return text
	}
	return bearerPattern.ReplaceAllString(text, "Bearer [redacted]")
}

// modelEntitlementText lists the wordings Kimi uses when the subscription tier
// does not include a model. The managed /models list is not plan-scoped, so an
// unentitled model answers with an authentication-shaped error; treating that as
// an account auth failure would cool a healthy account over one model.
var modelEntitlementText = []string{
	"does not have access to",
	"upgrade to higher-tier",
	"upgrade to a higher",
	"upgrade your plan",
	"not available for your plan",
}

// quotaExhaustedText lists the wordings Kimi uses when the subscription window
// is spent. Rate limiting is a different, retryable condition (429 without these
// markers), so the two must not collapse into one kind.
var quotaExhaustedText = []string{
	"exceeded_current_quota_error",
	"exceeded your current quota",
	"exceeded your current token quota",
	"check your account balance",
	"insufficient balance",
	"please recharge",
	"recharge your account",
	"account is in arrears",
	"额度",
	"余额不足",
}

// Classify maps Kimi HTTP errors onto the executor taxonomy. A model the plan
// does not include is model-not-available, not auth. 401/403 cool the account as
// auth. An explicit quota-exhaustion 429 cools it as quota; a plain 429 is a rate
// limit. 404 means the model is not served to this account.
func Classify(status int, body string) providers.ClassifiedError {
	safe := redactSecrets(strings.TrimSpace(body))
	text := strings.ToLower(safe)
	switch {
	case isModelEntitlement(text):
		return providers.ClassifiedError{
			Kind:    accounts.KindModelNotAvailable,
			Status:  firstStatus(status, http.StatusNotFound),
			Message: safe,
		}
	case status == 401 || status == 403 ||
		strings.Contains(text, "invalid api key") ||
		strings.Contains(text, "unauthorized") ||
		strings.Contains(text, "authentication"):
		return providers.ClassifiedError{
			Kind:    accounts.KindAuth,
			Status:  firstStatus(status, 401),
			Message: firstNonEmpty(safe, "invalid kimi credentials"),
		}
	case isQuotaExhausted(text) || status == 402:
		return providers.ClassifiedError{
			Kind:    accounts.KindQuota,
			Status:  firstStatus(status, 402),
			Message: firstNonEmpty(safe, "kimi quota exhausted"),
		}
	case status == 429 || strings.Contains(text, "rate limit") || strings.Contains(text, "too many requests"):
		return providers.ClassifiedError{
			Kind:    accounts.KindRateLimit,
			Status:  firstStatus(status, 429),
			Message: firstNonEmpty(safe, "kimi rate limited"),
		}
	case status == 404:
		return providers.ClassifiedError{Kind: accounts.KindModelNotAvailable, Status: 404, Message: safe}
	case status == 400 || status == 422 || accounts.IsInvalidRequestText(safe) || accounts.IsPromptLimitText(safe):
		return providers.ClassifiedError{Kind: accounts.KindInvalidRequest, Status: firstStatus(status, 400), Message: safe}
	case status == 499 || strings.Contains(text, "canceled") || strings.Contains(text, "cancelled"):
		return providers.ClassifiedError{Kind: accounts.KindCanceled, Status: 499, Message: safe}
	case status >= 500 || status == 0:
		return providers.ClassifiedError{
			Kind:    accounts.KindUnavailable,
			Status:  firstStatus(status, 502),
			Message: firstNonEmpty(safe, "kimi upstream unavailable"),
		}
	}
	if safe != "" && status >= 400 {
		return providers.ClassifiedError{Kind: accounts.KindUnavailable, Status: status, Message: safe}
	}
	return providers.ClassifiedError{}
}

func isQuotaExhausted(lower string) bool {
	for _, marker := range quotaExhaustedText {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func isModelEntitlement(lower string) bool {
	for _, marker := range modelEntitlementText {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func newProviderError(status int, body string) *providers.Error {
	classified := Classify(status, body)
	return &providers.Error{
		Kind:    classified.Kind,
		Status:  classified.Status,
		Message: classified.Message,
	}
}
