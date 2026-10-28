package meta

import (
	"encoding/json"
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

// Classify maps Meta HTTP errors onto the executor taxonomy.
// Subscription quota / 429+resets_at → KindQuota; plain 429 → KindRateLimit.
func Classify(status int, body string) providers.ClassifiedError {
	safe := redactSecrets(strings.TrimSpace(body))
	text := strings.ToLower(safe)
	code, message := parseErrorFields(safe)
	combined := strings.ToLower(code + " " + message + " " + text)

	switch {
	case status == 401 || status == 403 ||
		strings.Contains(combined, "invalid api key") ||
		strings.Contains(combined, "unauthorized") ||
		strings.Contains(combined, "authentication"):
		return providers.ClassifiedError{
			Kind:    accounts.KindAuth,
			Status:  firstStatus(status, 401),
			Message: firstNonEmpty(safe, "invalid meta credentials"),
		}
	case isMetaSubscriptionQuota(status, combined, safe):
		return providers.ClassifiedError{
			Kind:    accounts.KindQuota,
			Status:  firstStatus(status, 429),
			Message: firstNonEmpty(safe, "meta subscription quota exhausted"),
		}
	case status == 429 || strings.Contains(combined, "rate limit") || strings.Contains(combined, "too many requests"):
		return providers.ClassifiedError{
			Kind:    accounts.KindRateLimit,
			Status:  firstStatus(status, 429),
			Message: firstNonEmpty(safe, "meta rate limited"),
		}
	case status == 404:
		return providers.ClassifiedError{Kind: accounts.KindModelNotAvailable, Status: 404, Message: safe}
	case status == 400 || status == 422 || accounts.IsInvalidRequestText(safe) || accounts.IsPromptLimitText(safe):
		return providers.ClassifiedError{Kind: accounts.KindInvalidRequest, Status: firstStatus(status, 400), Message: safe}
	case status == 499 || strings.Contains(combined, "canceled") || strings.Contains(combined, "cancelled"):
		return providers.ClassifiedError{Kind: accounts.KindCanceled, Status: 499, Message: safe}
	case status >= 500 || status == 0:
		return providers.ClassifiedError{
			Kind:    accounts.KindUnavailable,
			Status:  firstStatus(status, 502),
			Message: firstNonEmpty(safe, "meta upstream unavailable"),
		}
	}
	if safe != "" && status >= 400 {
		return providers.ClassifiedError{Kind: accounts.KindUnavailable, Status: status, Message: safe}
	}
	return providers.ClassifiedError{}
}

func isMetaSubscriptionQuota(status int, combined, raw string) bool {
	if strings.Contains(combined, "subscription quota") || strings.Contains(combined, "quota exhausted") {
		return true
	}
	if status != http.StatusTooManyRequests {
		return false
	}
	code, _ := parseErrorFields(raw)
	codeLower := strings.ToLower(code)
	if (codeLower == "rate_limit_exceeded" || strings.Contains(codeLower, "quota")) && strings.Contains(raw, "resets_at") {
		return true
	}
	return false
}

func parseErrorFields(body string) (code, message string) {
	var envelope struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
			Type    string `json:"type"`
		} `json:"error"`
	}
	if json.Unmarshal([]byte(body), &envelope) == nil {
		return firstNonEmpty(envelope.Error.Code, envelope.Error.Type), envelope.Error.Message
	}
	return "", ""
}

func firstStatus(status, fallback int) int {
	if status > 0 {
		return status
	}
	return fallback
}
