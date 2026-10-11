package devin

import (
	"log"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/caigee-cmd/cli2api/internal/accounts"
	"github.com/caigee-cmd/cli2api/internal/providers"
)

var (
	sessionTokenPattern = regexp.MustCompile(`(?i)devin-session-token\$[A-Za-z0-9._\-+/=]+`)
	jwtLikePattern      = regexp.MustCompile(`\beyJ[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\b`)
	// resetInPattern matches relative hints such as "reset in 13 minutes" or
	// "reset in 5 hours 30 minutes". Devin puts the quota reset in the body and
	// does not send Retry-After.
	resetInPattern = regexp.MustCompile(`(?i)reset in\s+(?:(\d+)\s*hours?)?\s*(?:(\d+)\s*minutes?)?\s*(?:(\d+)\s*seconds?)?`)
	resetAtPattern = regexp.MustCompile(`(?i)\(at\s+(\d{1,2}):(\d{2})\s*UTC\)`)
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
		if devinQuotaScoped(text) {
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
	return classifiedErrorWithHeaders(status, nil, body, toolsDiag)
}

func classifiedErrorWithHeaders(status int, headers http.Header, body, toolsDiag string) error {
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
	err := &providers.Error{
		Kind:    classified.Kind,
		Status:  classified.Status,
		Message: message,
	}
	if classified.Kind == accounts.KindRateLimit || classified.Kind == accounts.KindQuota {
		err.RetryAfter = devinRetryAfter(headers, body, time.Now())
	}
	return err
}

// devinQuotaScoped reports an account-level quota exhaustion. A per-model rate
// limit stays model-scoped so the rest of the account keeps serving.
func devinQuotaScoped(text string) bool {
	if strings.Contains(text, "usage quota") {
		return true
	}
	if strings.Contains(text, "quota") || strings.Contains(text, "credit") || strings.Contains(text, "acu") {
		return true
	}
	return strings.Contains(text, "exhausted") && !strings.Contains(text, "resource_exhausted")
}

// devinRetryAfter prefers Retry-After, then the reset hint embedded in the
// body. Returns 0 when neither is present so the executor keeps its fallback.
func devinRetryAfter(headers http.Header, body string, now time.Time) time.Duration {
	if delay := retryAfterHeader(headers, now); delay > 0 {
		return delay
	}
	return resetHintFromMessage(body, now)
}

func retryAfterHeader(headers http.Header, now time.Time) time.Duration {
	if headers == nil {
		return 0
	}
	raw := strings.TrimSpace(headers.Get("Retry-After"))
	if raw == "" {
		return 0
	}
	if seconds, err := strconv.ParseInt(raw, 10, 64); err == nil && seconds > 0 {
		return time.Duration(seconds) * time.Second
	}
	if deadline, err := http.ParseTime(raw); err == nil {
		if delay := deadline.Sub(now); delay > 0 {
			return delay
		}
	}
	return 0
}

func resetHintFromMessage(msg string, now time.Time) time.Duration {
	if match := resetInPattern.FindStringSubmatch(msg); match != nil {
		seconds := int64(0)
		if value, err := strconv.ParseInt(match[1], 10, 64); err == nil {
			seconds += value * 3600
		}
		if value, err := strconv.ParseInt(match[2], 10, 64); err == nil {
			seconds += value * 60
		}
		if value, err := strconv.ParseInt(match[3], 10, 64); err == nil {
			seconds += value
		}
		if seconds > 0 {
			return time.Duration(seconds) * time.Second
		}
	}
	match := resetAtPattern.FindStringSubmatch(msg)
	if match == nil {
		return 0
	}
	hour, errH := strconv.Atoi(match[1])
	minute, errM := strconv.Atoi(match[2])
	if errH != nil || errM != nil || hour > 23 || minute > 59 {
		return 0
	}
	utc := now.UTC()
	reset := time.Date(utc.Year(), utc.Month(), utc.Day(), hour, minute, 0, 0, time.UTC)
	delay := reset.Sub(utc)
	if delay <= 0 {
		delay += 24 * time.Hour
	}
	return delay
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
