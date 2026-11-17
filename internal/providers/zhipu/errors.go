package zhipu

import (
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

// modelEntitlementText lists wordings meaning the plan does not include the
// requested model. These are model-level, not auth and not quota: the shared
// catalog can advertise a model this account cannot actually use.
var modelEntitlementText = []string{
	"does not have access to",
	"not available for your plan",
	"upgrade your plan",
	"upgrade to higher-tier",
	"upgrade to a higher",
	"model is not included",
	"not included in your plan",
	"无权限使用该模型",
	"套餐不包含",
}

// Classify maps Zhipu HTTP errors onto the executor taxonomy. 401/403 cool
// the account as auth. 429 is a rate limit. 402 and an explicit balance or
// quota message are quota. Other 4xx are the request, not the account.
func Classify(status int, body string) providers.ClassifiedError {
	safe := redactSecrets(strings.TrimSpace(body))
	text := strings.ToLower(safe)
	switch {
	case containsAnyText(text, modelEntitlementText):
		return providers.ClassifiedError{
			Kind:    accounts.KindModelNotAvailable,
			Status:  firstStatus(status, 404),
			Message: safe,
		}
	case status == 401 || status == 403 ||
		strings.Contains(text, "invalid api key") ||
		strings.Contains(text, "unauthorized") ||
		strings.Contains(text, "authentication"):
		return providers.ClassifiedError{
			Kind:    accounts.KindAuth,
			Status:  firstStatus(status, 401),
			Message: firstNonEmpty(safe, "invalid zhipu api key"),
		}
	case status == 429 || strings.Contains(text, "rate limit") || strings.Contains(text, "too many requests"):
		return providers.ClassifiedError{
			Kind:    accounts.KindRateLimit,
			Status:  firstStatus(status, 429),
			Message: firstNonEmpty(safe, "zhipu rate limited"),
		}
	case status == 402 ||
		strings.Contains(text, "insufficient quota") ||
		strings.Contains(text, "insufficient balance") ||
		strings.Contains(text, "quota exceeded") ||
		strings.Contains(text, "quota exhausted") ||
		strings.Contains(text, "余额不足") ||
		strings.Contains(text, "额度已用尽") ||
		strings.Contains(text, "额度不足"):
		return providers.ClassifiedError{
			Kind:    accounts.KindQuota,
			Status:  firstStatus(status, 402),
			Message: firstNonEmpty(safe, "zhipu quota exhausted"),
		}
	case status == 404:
		return providers.ClassifiedError{Kind: accounts.KindModelNotAvailable, Status: 404, Message: safe}
	case status == 400 || status == 422 || accounts.IsInvalidRequestText(safe) || accounts.IsPromptLimitText(safe):
		return providers.ClassifiedError{Kind: accounts.KindInvalidRequest, Status: firstStatus(status, 400), Message: safe}
	case status == 499 || strings.Contains(text, "canceled"):
		return providers.ClassifiedError{Kind: accounts.KindCanceled, Status: 499, Message: safe}
	case status >= 500 || status == 0:
		return providers.ClassifiedError{
			Kind:    accounts.KindUnavailable,
			Status:  firstStatus(status, 502),
			Message: firstNonEmpty(safe, "zhipu upstream unavailable"),
		}
	}
	if safe != "" && status >= 400 {
		return providers.ClassifiedError{Kind: accounts.KindUnavailable, Status: status, Message: safe}
	}
	return providers.ClassifiedError{}
}

func newProviderError(status int, body string) *providers.Error {
	classified := Classify(status, body)
	return &providers.Error{
		Kind:    classified.Kind,
		Status:  classified.Status,
		Message: classified.Message,
	}
}

func firstStatus(status, fallback int) int {
	if status > 0 {
		return status
	}
	return fallback
}

func containsAnyText(text string, markers []string) bool {
	for _, marker := range markers {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}
