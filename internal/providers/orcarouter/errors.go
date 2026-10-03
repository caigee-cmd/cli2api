package orcarouter

import (
	"regexp"
	"strings"

	"github.com/caigee-cmd/cli2api/internal/accounts"
	"github.com/caigee-cmd/cli2api/internal/providers"
)

var (
	orcaKeyPattern   = regexp.MustCompile(`sk-orca-[A-Za-z0-9._\-]{4,}`)
	bearerPattern    = regexp.MustCompile(`(?i)bearer\s+[A-Za-z0-9._\-]+`)
	verifierPattern  = regexp.MustCompile(`(?i)"?code_verifier"?\s*[:=]\s*"?[A-Za-z0-9._\-]+`)
	challengePattern = regexp.MustCompile(`(?i)"?code_challenge"?\s*[:=]\s*"?[A-Za-z0-9._\-]+`)
	statePattern     = regexp.MustCompile(`(?i)([?&]|\b)state["']?\s*[:=]\s*["']?[A-Za-z0-9._\-]{4,}`)
	secretPattern    = regexp.MustCompile(`(?i)"?client_secret"?\s*[:=]\s*"?[^\s,"}]+`)
)

// redactSecrets removes anything that must never reach a log, an error message,
// a telemetry event, or a screenshot: API keys, bearer tokens, and the PKCE
// verifier/challenge/state values.
func redactSecrets(text string) string {
	if text == "" {
		return text
	}
	out := orcaKeyPattern.ReplaceAllString(text, "sk-orca-[redacted]")
	out = bearerPattern.ReplaceAllString(out, "Bearer [redacted]")
	out = verifierPattern.ReplaceAllString(out, "code_verifier=[redacted]")
	out = challengePattern.ReplaceAllString(out, "code_challenge=[redacted]")
	out = statePattern.ReplaceAllString(out, "state=[redacted]")
	out = secretPattern.ReplaceAllString(out, "client_secret=[redacted]")
	return out
}

// Redact is the exported redactor used by tests and by console error mapping.
func Redact(text string) string { return redactSecrets(text) }

// Classify maps OrcaRouter HTTP status/body onto the internal taxonomy. A 401
// means the durable key was revoked: that is a terminal reauthentication
// requirement (accounts.KindAuth), never a retry loop and never a refresh.
func Classify(status int, body string) providers.ClassifiedError {
	safeBody := redactSecrets(strings.TrimSpace(body))
	text := strings.ToLower(safeBody)
	code := errorCode(safeBody)
	switch {
	case status == 401 ||
		code == "invalid_api_key" ||
		code == "unauthorized" ||
		strings.Contains(text, "invalid api key") ||
		strings.Contains(text, "invalid api_key") ||
		strings.Contains(text, "unauthorized"):
		return providers.ClassifiedError{
			Kind:    accounts.KindAuth,
			Status:  firstNonZeroStatus(status, 401),
			Message: firstNonEmpty(safeBody, "orcarouter key rejected; reconnect the account"),
		}
	case status == 403:
		return providers.ClassifiedError{
			Kind:    accounts.KindAuth,
			Status:  403,
			Message: firstNonEmpty(safeBody, "orcarouter refused the credential or scope"),
		}
	case status == 429 ||
		code == "rate_limit_exceeded" ||
		strings.Contains(text, "rate limit") ||
		strings.Contains(text, "too many requests"):
		return providers.ClassifiedError{
			Kind:    accounts.KindRateLimit,
			Status:  firstNonZeroStatus(status, 429),
			Message: firstNonEmpty(safeBody, "orcarouter rate limited"),
		}
	case status == 402 || code == "insufficient_quota" || code == "insufficient_credits" ||
		strings.Contains(text, "insufficient") || strings.Contains(text, "quota"):
		return providers.ClassifiedError{
			Kind:    accounts.KindQuota,
			Status:  firstNonZeroStatus(status, 402),
			Message: firstNonEmpty(safeBody, "orcarouter balance exhausted"),
		}
	case status == 404 || code == "model_not_found":
		return providers.ClassifiedError{Kind: accounts.KindModelNotAvailable, Status: 404, Message: safeBody}
	case status == 400 || status == 422 ||
		code == "invalid_request_error" ||
		accounts.IsInvalidRequestText(safeBody) ||
		accounts.IsPromptLimitText(safeBody):
		return providers.ClassifiedError{
			Kind:    accounts.KindInvalidRequest,
			Status:  firstNonZeroStatus(status, 400),
			Message: safeBody,
		}
	case status == 499 || strings.Contains(text, "canceled") || strings.Contains(text, "cancelled"):
		return providers.ClassifiedError{Kind: accounts.KindCanceled, Status: 499, Message: safeBody}
	case status >= 500 || status == 0 ||
		strings.Contains(text, "no available channel") ||
		strings.Contains(text, "no available channels"):
		return providers.ClassifiedError{
			Kind:    accounts.KindUnavailable,
			Status:  firstNonZeroStatus(status, 502),
			Message: firstNonEmpty(safeBody, "orcarouter upstream unavailable"),
		}
	}
	if safeBody != "" && status >= 400 {
		return providers.ClassifiedError{Kind: accounts.KindUnavailable, Status: status, Message: safeBody}
	}
	return providers.ClassifiedError{}
}

// errorCode extracts error.code from the OpenAI-style {"error":{...,"code":…}}
// envelope without deserializing the whole body.
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

func firstNonZeroStatus(status, fallback int) int {
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
			Status:  firstNonZeroStatus(status, 502),
			Message: firstNonEmpty(redactSecrets(strings.TrimSpace(body)), "orcarouter upstream error"),
		}
	}
	return &providers.Error{
		Kind:    classified.Kind,
		Status:  classified.Status,
		Message: classified.Message,
	}
}

// classifyCredentialError reports whether a stored credential must be marked
// needs_reauth after a failed request: only a terminal auth rejection qualifies.
func classifyCredentialError(status int, body string) bool {
	return Classify(status, body).Kind == accounts.KindAuth
}
