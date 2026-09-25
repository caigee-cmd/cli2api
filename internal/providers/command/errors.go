package command

import (
	"regexp"
	"strings"

	"github.com/caigee-cmd/cli2api/internal/accounts"
	"github.com/caigee-cmd/cli2api/internal/providers"
)

var (
	userKeyPattern = regexp.MustCompile(`user_[A-Za-z0-9._\-]{8,}`)
	bearerPattern  = regexp.MustCompile(`(?i)bearer\s+[A-Za-z0-9._\-]+`)
)

func redactSecrets(text string) string {
	if text == "" {
		return text
	}
	out := userKeyPattern.ReplaceAllString(text, "user_[redacted]")
	out = bearerPattern.ReplaceAllString(out, "Bearer [redacted]")
	return out
}

// Classify maps Command Code HTTP / NDJSON error bodies onto the internal
// taxonomy. The Go plan's distinctive 403 upgrade_required is treated as a
// request-shape problem (this adapter must never call the Pro-gated
// /provider/v1/* paths), not as an auth failure — cooling the account would be
// wrong.
func Classify(status int, body string) providers.ClassifiedError {
	safeBody := redactSecrets(strings.TrimSpace(body))
	text := strings.ToLower(safeBody)
	code := errorCode(safeBody)
	switch {
	case isUpgradeRequired(code, text):
		return providers.ClassifiedError{
			Kind:    accounts.KindInvalidRequest,
			Status:  firstNonEmptyStatus(status, 403),
			Message: firstNonEmpty(safeBody, "command code plan does not include API access; /alpha/generate is required"),
		}
	case isInsufficientCredits(code, text):
		return providers.ClassifiedError{
			Kind:    accounts.KindQuota,
			Status:  firstNonEmptyStatus(status, 402),
			Message: firstNonEmpty(safeBody, "command code credits exhausted"),
		}
	case status == 401 ||
		code == "unauthorized" ||
		strings.Contains(text, "unauthorized") ||
		strings.Contains(text, "invalid 'authorization'") ||
		strings.Contains(text, "invalid api key"):
		return providers.ClassifiedError{
			Kind:    accounts.KindAuth,
			Status:  firstNonEmptyStatus(status, 401),
			Message: firstNonEmpty(safeBody, "invalid command code key; paste a fresh user_… key"),
		}
	case status == 429 ||
		code == "rate_limit_error" ||
		strings.Contains(text, "rate limit") ||
		strings.Contains(text, "too many requests"):
		return providers.ClassifiedError{
			Kind:    accounts.KindRateLimit,
			Status:  firstNonEmptyStatus(status, 429),
			Message: firstNonEmpty(safeBody, "rate limited"),
		}
	case status == 404:
		return providers.ClassifiedError{Kind: accounts.KindModelNotAvailable, Status: 404, Message: safeBody}
	case status == 400 || status == 403 || status == 422 ||
		code == "invalid_request_error" ||
		strings.Contains(text, "validation error") ||
		strings.Contains(text, "do not match the modelmessage") ||
		strings.Contains(text, "is not supported on this endpoint") ||
		accounts.IsInvalidRequestText(safeBody) ||
		accounts.IsPromptLimitText(safeBody):
		return providers.ClassifiedError{
			Kind:    accounts.KindInvalidRequest,
			Status:  firstNonEmptyStatus(status, 400),
			Message: safeBody,
		}
	case status == 499 || strings.Contains(text, "canceled"):
		return providers.ClassifiedError{Kind: accounts.KindCanceled, Status: 499, Message: safeBody}
	case status >= 500 || status == 0:
		return providers.ClassifiedError{
			Kind:    accounts.KindUnavailable,
			Status:  firstNonEmptyStatus(status, 502),
			Message: firstNonEmpty(safeBody, "command code upstream unavailable"),
		}
	}
	if safeBody != "" && status >= 400 {
		return providers.ClassifiedError{Kind: accounts.KindUnavailable, Status: status, Message: safeBody}
	}
	return providers.ClassifiedError{}
}

// errorCode extracts error.code from either the /alpha/generate envelope
// ({"success":false,"error":{"code","status","message"}}) or the OpenAI-style
// envelope ({"error":{"message","type","code"}}).
func errorCode(body string) string {
	lower := strings.ToLower(body)
	const marker = `"code"`
	idx := strings.Index(lower, marker)
	if idx < 0 {
		return ""
	}
	rest := body[idx+len(marker):]
	colon := strings.Index(rest, ":")
	if colon < 0 {
		return ""
	}
	rest = strings.TrimSpace(rest[colon+1:])
	if !strings.HasPrefix(rest, `"`) {
		return ""
	}
	rest = rest[1:]
	end := strings.Index(rest, `"`)
	if end < 0 {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(rest[:end]))
}

func isUpgradeRequired(code, lowerBody string) bool {
	return code == "upgrade_required" || strings.Contains(lowerBody, "upgrade_required") || strings.Contains(lowerBody, "doesn't include api access")
}

func isInsufficientCredits(code, lowerBody string) bool {
	return code == "insufficient_credits" ||
		strings.Contains(lowerBody, "insufficient credits") ||
		strings.Contains(lowerBody, "purchase more credits")
}

func firstNonEmptyStatus(status, fallback int) int {
	if status >= 400 {
		return status
	}
	return fallback
}

// newProviderError builds the executor-facing error used by the adapter.
func newProviderError(status int, body string) error {
	classified := Classify(status, body)
	if classified.Kind == "" {
		classified = providers.ClassifiedError{
			Kind:    accounts.KindUnavailable,
			Status:  firstNonEmptyStatus(status, 502),
			Message: firstNonEmpty(redactSecrets(strings.TrimSpace(body)), "command code upstream error"),
		}
	}
	return &providers.Error{
		Kind:    classified.Kind,
		Status:  classified.Status,
		Message: classified.Message,
	}
}
