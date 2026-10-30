package codearts

import (
	"net/http"
	"regexp"
	"strings"

	"github.com/caigee-cmd/cli2api/internal/accounts"
	"github.com/caigee-cmd/cli2api/internal/providers"
)

var secretPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(x-auth-token|x-security-token|authorization)\s*[:=]\s*\S+`),
	regexp.MustCompile(`(?i)Access=[^,\s]+`),
	regexp.MustCompile(`(?i)Signature=[0-9a-f]+`),
	regexp.MustCompile(`(?i)bearer\s+\S+`),
}

func redactSecrets(text string) string {
	safe := text
	for _, pattern := range secretPatterns {
		safe = pattern.ReplaceAllString(safe, "[redacted]")
	}
	return safe
}

var modelUnavailableMarkers = []string{
	"inferhub.002002009",
	"not registered",
	"4004.200",
	"benefit not found",
	"model is not available",
	"model not found",
	"does not have access",
}

var invalidRequestMarkers = []string{
	"inferhub.001001005",
	"request param is invalid",
	"invalid request",
	"tool_choice",
}

var quotaMarkers = []string{
	"insufficient quota",
	"quota exceeded",
	"quota exhausted",
	"insufficient balance",
	"余额不足",
	"积分不足",
}

var rateLimitMarkers = []string{
	"tm.00001041",
	"too many requests",
	"rate limit",
	"concurrent session",
	"并发会话",
	"tpm",
	"queue",
}

// Classify maps CodeArts error bodies onto the executor taxonomy. A model the
// account cannot use is model_not_available and keeps the account ready; an
// expired STS token is auth and asks for a refresh/login.
func Classify(status int, body string) providers.ClassifiedError {
	safe := redactSecrets(strings.TrimSpace(body))
	lower := strings.ToLower(safe)
	message := firstNonEmpty(safe, "codearts upstream error")
	switch {
	case containsAny(lower, modelUnavailableMarkers):
		return providers.ClassifiedError{Kind: accounts.KindModelNotAvailable, Status: http.StatusNotFound, Message: message}
	case containsAny(lower, quotaMarkers) || status == http.StatusPaymentRequired:
		return providers.ClassifiedError{Kind: accounts.KindQuota, Status: http.StatusPaymentRequired, Message: message}
	case status == http.StatusUnauthorized || status == http.StatusForbidden ||
		strings.Contains(lower, "apig.0301") || strings.Contains(lower, "x-auth-token") ||
		strings.Contains(lower, "token expired") || strings.Contains(lower, "invalid token"):
		return providers.ClassifiedError{Kind: accounts.KindAuth, Status: http.StatusUnauthorized, Message: message}
	case containsAny(lower, rateLimitMarkers) || status == http.StatusTooManyRequests:
		return providers.ClassifiedError{Kind: accounts.KindRateLimit, Status: http.StatusTooManyRequests, Message: message}
	case containsAny(lower, invalidRequestMarkers) || status == http.StatusBadRequest || status == http.StatusUnprocessableEntity:
		return providers.ClassifiedError{Kind: accounts.KindInvalidRequest, Status: http.StatusBadRequest, Message: message}
	case status == http.StatusNotFound:
		return providers.ClassifiedError{Kind: accounts.KindModelNotAvailable, Status: http.StatusNotFound, Message: message}
	case status == 499 || strings.Contains(lower, "canceled") || strings.Contains(lower, "cancelled"):
		return providers.ClassifiedError{Kind: accounts.KindCanceled, Status: 499, Message: message}
	case status >= 500 || status == 0:
		return providers.ClassifiedError{Kind: accounts.KindUnavailable, Status: http.StatusBadGateway, Message: message}
	}
	if status >= 400 {
		return providers.ClassifiedError{Kind: accounts.KindUnavailable, Status: status, Message: message}
	}
	return providers.ClassifiedError{}
}

func containsAny(text string, markers []string) bool {
	for _, marker := range markers {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}

// newProviderError turns an HTTP status/body into the provider error the
// executor classifies. The message is always redacted.
func newProviderError(status int, body string) *providers.Error {
	classified := Classify(status, body)
	kind := classified.Kind
	if kind == "" {
		kind = accounts.KindUnavailable
	}
	code := http.StatusBadGateway
	if classified.Status != 0 {
		code = classified.Status
	}
	message := classified.Message
	if message == "" {
		message = "codearts upstream error"
	}
	return &providers.Error{Kind: kind, Status: code, Message: message}
}

type classifier struct{}

func (classifier) Classify(status int, body string) providers.ClassifiedError {
	return Classify(status, body)
}
