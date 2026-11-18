package kiro

import (
	"regexp"
	"strings"

	"github.com/caigee-cmd/cli2api/internal/accounts"
	"github.com/caigee-cmd/cli2api/internal/providers"
)

var bearerPattern = regexp.MustCompile(`(?i)bearer\s+[A-Za-z0-9._\-]+`)

func redact(text string) string {
	if text == "" {
		return text
	}
	return bearerPattern.ReplaceAllString(text, "Bearer [redacted]")
}

// Classify maps Kiro HTTP failures onto the executor taxonomy.
// USER_REQUEST_RATE_EXCEEDED is a rate limit, not an auth failure.
func Classify(status int, body string) providers.ClassifiedError {
	safe := redact(strings.TrimSpace(body))
	text := strings.ToLower(safe)
	switch {
	case status == 401 || status == 403 || strings.Contains(text, "unauthorized") || strings.Contains(text, "expiredtoken") || strings.Contains(text, "invalid token"):
		return providers.ClassifiedError{Kind: accounts.KindAuth, Status: statusOr(status, 401), Message: firstNonEmpty(safe, "kiro authentication failed")}
	case status == 429 || strings.Contains(text, "user_request_rate_exceeded") || strings.Contains(text, "throttl") || strings.Contains(text, "too many requests"):
		return providers.ClassifiedError{Kind: accounts.KindRateLimit, Status: statusOr(status, 429), Message: firstNonEmpty(safe, "kiro rate limited")}
	case strings.Contains(text, "credit") || strings.Contains(text, "quota") || strings.Contains(text, "usage limit"):
		return providers.ClassifiedError{Kind: accounts.KindQuota, Status: statusOr(status, 402), Message: firstNonEmpty(safe, "kiro credits exhausted")}
	case status == 404 || strings.Contains(text, "unknown model") || strings.Contains(text, "model is not"):
		return providers.ClassifiedError{Kind: accounts.KindModelNotAvailable, Status: statusOr(status, 404), Message: firstNonEmpty(safe, "kiro model not available")}
	case status == 400 || status == 422 || strings.Contains(text, "validation"):
		return providers.ClassifiedError{Kind: accounts.KindInvalidRequest, Status: statusOr(status, 400), Message: safe}
	default:
		if status >= 400 {
			return providers.ClassifiedError{Kind: accounts.KindUnavailable, Status: status, Message: firstNonEmpty(safe, "kiro upstream error")}
		}
		return providers.ClassifiedError{}
	}
}

func statusOr(status, fallback int) int {
	if status >= 400 {
		return status
	}
	return fallback
}

func newProviderError(status int, body string) error {
	classified := Classify(status, body)
	if classified.Kind == "" {
		classified = providers.ClassifiedError{Kind: accounts.KindUnavailable, Status: statusOr(status, 502), Message: firstNonEmpty(redact(strings.TrimSpace(body)), "kiro upstream error")}
	}
	return &providers.Error{Kind: classified.Kind, Status: classified.Status, Message: classified.Message}
}
