package codex

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/caigee-cmd/cli2api/internal/accounts"
	"github.com/caigee-cmd/cli2api/internal/providers"
)

// Classify maps codex upstream errors onto the internal taxonomy. The ChatGPT
// backend returns OpenAI-style error JSON plus Cloudflare HTML on edge blocks.
func Classify(status int, body string) providers.ClassifiedError {
	text := strings.ToLower(body)
	// Cloudflare edge rejection (1010 etc.) is a body match, not a status.
	if strings.Contains(text, "cloudflare") || strings.Contains(text, "error 1010") || strings.Contains(text, "<!doctype html") {
		return providers.ClassifiedError{Kind: accounts.KindUnavailable, Status: 502, Message: "upstream edge block (cloudflare)"}
	}
	code, message := parseErrorBody(body)
	switch {
	case status == 401 || code == "invalid_api_key" || strings.Contains(text, "unauthorized"):
		return providers.ClassifiedError{Kind: accounts.KindAuth, Status: 401, Message: firstNonEmpty(message, "session dead; re-login required")}
	case code == "insufficient_quota" || strings.Contains(text, "quota") && status == 429:
		return providers.ClassifiedError{Kind: accounts.KindQuota, Status: 429, Message: firstNonEmpty(message, "usage quota exhausted")}
	case code == "rate_limit_exceeded" || status == 429 || strings.Contains(text, "rate limit"):
		return providers.ClassifiedError{Kind: accounts.KindRateLimit, Status: 429, Message: firstNonEmpty(message, strings.TrimSpace(body))}
	case code == "model_not_found" || status == 404:
		return providers.ClassifiedError{Kind: accounts.KindModelNotAvailable, Status: 404, Message: firstNonEmpty(message, "model not available")}
	case status == 400 || code == "invalid_request_error":
		return providers.ClassifiedError{Kind: accounts.KindInvalidRequest, Status: 400, Message: firstNonEmpty(message, strings.TrimSpace(body))}
	case status >= 500:
		return providers.ClassifiedError{Kind: accounts.KindUnavailable, Status: status, Message: firstNonEmpty(message, strings.TrimSpace(body))}
	}
	return providers.ClassifiedError{}
}

// parseErrorBody reads {"error":{"code","message"}} plus the flat
// {"code","detail"} shape some codex endpoints use.
func parseErrorBody(body string) (code, message string) {
	var env struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
			Type    string `json:"type"`
		} `json:"error"`
		Code    string `json:"code"`
		Detail  string `json:"detail"`
		Message string `json:"message"`
	}
	if json.Unmarshal([]byte(body), &env) != nil {
		return "", ""
	}
	code = firstNonEmpty(env.Error.Code, env.Error.Type, env.Code)
	message = firstNonEmpty(env.Error.Message, env.Detail, env.Message)
	return code, message
}

func classifiedErrorf(status int, body []byte, format string, args ...any) error {
	classified := Classify(status, string(body))
	return &providers.Error{
		Kind:    classified.Kind,
		Status:  classified.Status,
		Message: fmt.Sprintf(format, args...),
	}
}
