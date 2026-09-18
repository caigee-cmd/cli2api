package devin

import (
	"log"
	"regexp"
	"strings"
	"time"

	"github.com/caigee-cmd/cli2api/internal/accounts"
	"github.com/caigee-cmd/cli2api/internal/providers"
)

var (
	sessionTokenPattern = regexp.MustCompile(`(?i)devin-session-token\$[A-Za-z0-9._\-+/=]+`)
	jwtLikePattern      = regexp.MustCompile(`\beyJ[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\b`)
)

func redactSecrets(text string) string {
	if text == "" {
		return text
	}
	out := sessionTokenPattern.ReplaceAllString(text, "devin-session-token$[redacted]")
	out = jwtLikePattern.ReplaceAllString(out, "[redacted-jwt]")
	return out
}

// Classify maps HTTP / Connect trailer / body errors onto the internal taxonomy.
func Classify(status int, body string) providers.ClassifiedError {
	safeBody := redactSecrets(strings.TrimSpace(body))
	text := strings.ToLower(safeBody)
	switch {
	case isDevinMCPConfigDenial(text):
		// Codex/Desktop MCP tool dumps make Devin return permission_denied.
		// That is a request-shape problem, not a dead session — do not cool
		// the account as auth.
		return providers.ClassifiedError{
			Kind:    accounts.KindInvalidRequest,
			Status:  400,
			Message: firstNonEmpty(safeBody, "devin rejected MCP/hosted tools in the request"),
		}
	case status == 401 ||
		strings.Contains(text, "unauthenticated") ||
		strings.Contains(text, "unauthorized") ||
		strings.Contains(text, "session dead"):
		return providers.ClassifiedError{
			Kind:    accounts.KindAuth,
			Status:  firstNonEmptyStatus(status, 401),
			Message: firstNonEmpty(safeBody, "session dead; re-login required"),
		}
	case status == 429 ||
		strings.Contains(text, "resource_exhausted") ||
		strings.Contains(text, "quota") ||
		strings.Contains(text, "credit") ||
		strings.Contains(text, "exhausted") ||
		strings.Contains(text, "rate limit") ||
		strings.Contains(text, "too many requests"):
		kind := accounts.KindRateLimit
		if strings.Contains(text, "quota") || strings.Contains(text, "credit") || strings.Contains(text, "exhausted") && !strings.Contains(text, "resource_exhausted") {
			kind = accounts.KindQuota
		}
		if strings.Contains(text, "resource_exhausted") && (strings.Contains(text, "quota") || strings.Contains(text, "credit") || strings.Contains(text, "acu")) {
			kind = accounts.KindQuota
		}
		return providers.ClassifiedError{
			Kind:    kind,
			Status:  429,
			Message: firstNonEmpty(safeBody, "rate limited"),
		}
	case status == 400 || status == 422 || status == 403 ||
		strings.Contains(text, "permission_denied") ||
		strings.Contains(text, "invalid_argument") ||
		strings.Contains(text, "failed_precondition") ||
		accounts.IsInvalidRequestText(safeBody) ||
		accounts.IsPromptLimitText(safeBody):
		return providers.ClassifiedError{
			Kind:    accounts.KindInvalidRequest,
			Status:  firstNonEmptyStatus(status, 400),
			Message: safeBody,
		}
	case status == 404:
		return providers.ClassifiedError{Kind: accounts.KindUnavailable, Status: 404, Message: safeBody}
	case status == 499 || strings.Contains(text, `"code":"canceled"`) || strings.Contains(text, "canceled"):
		return providers.ClassifiedError{Kind: accounts.KindCanceled, Status: 499, Message: safeBody}
	case status >= 500 || status == 0:
		return providers.ClassifiedError{
			Kind:    accounts.KindUnavailable,
			Status:  firstNonEmptyStatus(status, 502),
			Message: firstNonEmpty(safeBody, "upstream unavailable"),
		}
	}
	if safeBody != "" && status >= 400 {
		return providers.ClassifiedError{Kind: accounts.KindUnavailable, Status: status, Message: safeBody}
	}
	return providers.ClassifiedError{}
}

func classifiedErrorWithToolsDiag(status int, body, toolsDiag string) error {
	classified := Classify(status, body)
	if classified.Kind == "" {
		classified = providers.ClassifiedError{
			Kind:    accounts.KindUnavailable,
			Status:  firstNonEmptyStatus(status, 502),
			Message: firstNonEmpty(redactSecrets(strings.TrimSpace(body)), "upstream error"),
		}
	}
	message := classified.Message
	if toolsDiag != "" && isDevinMCPConfigDenial(strings.ToLower(message)+" "+strings.ToLower(body)) {
		log.Printf("devin mcp configuration denial tools_diag=%s", toolsDiag)
		message = appendToolsDiag(message, toolsDiag)
	}
	failover := classified.Kind != accounts.KindInvalidRequest
	return &providers.Error{
		Kind:     classified.Kind,
		Status:   classified.Status,
		Message:  message,
		Cooldown: classifiedCooldown(classified.Kind),
		Failover: &failover,
	}
}

func appendToolsDiag(message, toolsDiag string) string {
	message = strings.TrimSpace(message)
	toolsDiag = strings.TrimSpace(toolsDiag)
	if toolsDiag == "" {
		return message
	}
	suffix := "tools_diag=" + toolsDiag
	if message == "" {
		return suffix
	}
	if strings.Contains(message, "tools_diag=") {
		return message
	}
	return message + " | " + suffix
}

func classifiedCooldown(kind string) time.Duration {
	switch kind {
	case accounts.KindQuota:
		return accounts.NextLocalMidnightCooldown()
	case accounts.KindAuth:
		return 30 * time.Minute
	case accounts.KindRateLimit:
		return time.Minute
	default:
		return 0
	}
}

func isDevinMCPConfigDenial(text string) bool {
	if text == "" {
		return false
	}
	if strings.Contains(text, "mcp configuration") {
		return true
	}
	return strings.Contains(text, "permission_denied") && strings.Contains(text, "mcp")
}

func firstNonEmptyStatus(status, fallback int) int {
	if status >= 400 {
		return status
	}
	return fallback
}
