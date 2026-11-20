package minimaxcode

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/caigee-cmd/cli2api/internal/accounts"
	"github.com/caigee-cmd/cli2api/internal/providers"
)

func Classify(status int, body string) providers.ClassifiedError {
	message := upstreamMessage(body)
	lower := strings.ToLower(message)
	switch {
	case status == http.StatusUnauthorized || strings.Contains(lower, "unauthorized") || strings.Contains(lower, "invalid token"):
		return providers.ClassifiedError{Kind: accounts.KindAuth, Status: http.StatusUnauthorized, Message: message}
	case status == http.StatusPaymentRequired || strings.Contains(lower, "insufficient balance") || strings.Contains(lower, "quota") || strings.Contains(message, "余额不足"):
		return providers.ClassifiedError{Kind: accounts.KindQuota, Status: http.StatusPaymentRequired, Message: message}
	case status == http.StatusTooManyRequests || strings.Contains(lower, "rate limit"):
		return providers.ClassifiedError{Kind: accounts.KindRateLimit, Status: http.StatusTooManyRequests, Message: message}
	case status == http.StatusNotFound || strings.Contains(lower, "model not found") || strings.Contains(lower, "not registered"):
		return providers.ClassifiedError{Kind: accounts.KindModelNotAvailable, Status: http.StatusNotFound, Message: message}
	case status == http.StatusBadRequest || status == http.StatusUnprocessableEntity || accounts.IsInvalidRequestText(message):
		return providers.ClassifiedError{Kind: accounts.KindInvalidRequest, Status: http.StatusBadRequest, Message: message}
	case status == 499 || strings.Contains(lower, "canceled") || strings.Contains(lower, "cancelled"):
		return providers.ClassifiedError{Kind: accounts.KindCanceled, Status: 499, Message: message}
	default:
		return providers.ClassifiedError{Kind: accounts.KindUnavailable, Status: http.StatusBadGateway, Message: message}
	}
}

func upstreamMessage(body string) string {
	var payload map[string]any
	if json.Unmarshal([]byte(body), &payload) == nil {
		if errObj, ok := payload["error"].(map[string]any); ok {
			if text := jsonStr(errObj, "message"); text != "" {
				return text
			}
		}
		if text := jsonStr(payload, "message"); text != "" {
			return text
		}
		if resp, ok := payload["base_resp"].(map[string]any); ok {
			if text := jsonStr(resp, "status_msg"); text != "" {
				return text
			}
		}
	}
	trimmed := strings.TrimSpace(body)
	if trimmed == "" {
		return "minimaxcode upstream error"
	}
	return trimmed
}

func newProviderError(status int, body string) error {
	classified := Classify(status, body)
	return &providers.Error{Kind: classified.Kind, Status: classified.Status, Message: fmt.Sprintf("minimaxcode upstream: %s", classified.Message)}
}
