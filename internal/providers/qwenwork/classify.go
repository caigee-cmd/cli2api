package qwenwork

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/caigee-cmd/cli2api/internal/accounts"
	"github.com/caigee-cmd/cli2api/internal/providers"
)

// quotaCode is the account-level quota error the QwenWork gateway embeds in
// error frames.
const quotaCode = "14018"

// Classify maps a QwenWork HTTP status and error body onto the internal
// taxonomy. Text checks run before plain status checks because the gateway
// reports model and quota failures inside 400-class frames: "Model is not
// available for this user" and quota code 14018 must not be folded into
// invalid_request. Model-level misses stay model_not_available so the
// executor cools the model, never the whole account.
func Classify(status int, body string) providers.ClassifiedError {
	text := strings.TrimSpace(body)
	message, code := errorTextAndCode(text)
	kind := classifyKind(status, message, code)
	if kind == "" {
		return providers.ClassifiedError{}
	}
	out := providers.ClassifiedError{Kind: kind, Status: status, Message: message}
	switch kind {
	case accounts.KindModelNotAvailable:
		out.Status = firstStatus(status, http.StatusBadRequest)
	case accounts.KindQuota:
		out.Status = http.StatusPaymentRequired
	case accounts.KindAuth:
		if out.Status != http.StatusUnauthorized && out.Status != http.StatusForbidden {
			out.Status = http.StatusUnauthorized
		}
	case accounts.KindRateLimit:
		out.Status = http.StatusTooManyRequests
	case accounts.KindInvalidRequest, accounts.KindUnavailable:
		out.Status = firstStatus(status, http.StatusBadGateway)
	}
	if out.Message == "" {
		out.Message = "qwenwork upstream status " + strconv.Itoa(out.Status)
	}
	return out
}

// classifyKind picks the taxonomy kind. The known error texts and body-level
// codes win over the frame status they ride in: "Model is not available for
// this user" arrives in a 400-class frame and must not fold into
// invalid_request.
func classifyKind(status int, message, code string) string {
	lower := strings.ToLower(message + " " + code)
	if strings.Contains(lower, "model is not available") {
		return accounts.KindModelNotAvailable
	}
	// Body-level 4xx codes: the desktop error convention sends
	// {"code":"401","message":"…"} even inside 200-class frames.
	switch code {
	case "401", "403":
		return accounts.KindAuth
	case "402":
		return accounts.KindQuota
	case "429":
		return accounts.KindRateLimit
	}
	switch {
	case status == http.StatusPaymentRequired || code == quotaCode || quotaLike(lower):
		return accounts.KindQuota
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		return accounts.KindAuth
	case status == http.StatusTooManyRequests:
		return accounts.KindRateLimit
	case status == http.StatusBadRequest || status == http.StatusRequestEntityTooLarge || status == http.StatusUnprocessableEntity:
		return accounts.KindInvalidRequest
	case status >= 500 || status == http.StatusNotFound:
		return accounts.KindUnavailable
	}
	return ""
}

func quotaLike(lower string) bool {
	return strings.Contains(lower, "quota") ||
		strings.Contains(lower, "insufficient credit") ||
		strings.Contains(lower, "积分不足") ||
		strings.Contains(lower, "额度")
}

// errorTextAndCode extracts the human message and machine code from a
// QwenWork error body. Both the flat gateway shape ({"code":14018,"message":
// "…"}) and the OpenAI error shape ({"error":{"message":"…"}}) are accepted;
// anything else keeps the trimmed body as the message.
func errorTextAndCode(body string) (message, code string) {
	var flat struct {
		Code    any    `json:"code"`
		Message string `json:"message"`
		Msg     string `json:"msg"`
	}
	if json.Unmarshal([]byte(body), &flat) == nil {
		code = scalarCode(flat.Code)
		message = strings.TrimSpace(flat.Message)
		if message == "" {
			message = strings.TrimSpace(flat.Msg)
		}
	}
	if message == "" {
		var openAI struct {
			Error struct {
				Message string `json:"message"`
				Code    any    `json:"code"`
			} `json:"error"`
		}
		if json.Unmarshal([]byte(body), &openAI) == nil && openAI.Error.Message != "" {
			message = strings.TrimSpace(openAI.Error.Message)
			if code == "" {
				code = scalarCode(openAI.Error.Code)
			}
		}
	}
	if message == "" {
		message = body
	}
	return message, code
}

// scalarCode renders a JSON code value (number or string) as text.
func scalarCode(value any) string {
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case float64:
		if typed == float64(int64(typed)) {
			return strconv.FormatInt(int64(typed), 10)
		}
		return strconv.FormatFloat(typed, 'f', -1, 64)
	}
	return ""
}

func firstStatus(status, fallback int) int {
	if status >= 400 {
		return status
	}
	return fallback
}

// classifiedError wraps Classify for the HTTP error path. Bodies that carry
// no known marker still fail over as unavailable instead of passing a raw
// upstream page to the client.
func classifiedError(status int, body []byte) error {
	classified := Classify(status, string(body))
	if classified.Kind == "" {
		classified = providers.ClassifiedError{
			Kind:    accounts.KindUnavailable,
			Status:  firstStatus(status, http.StatusBadGateway),
			Message: truncateBody(body),
		}
	}
	return &providers.Error{Kind: classified.Kind, Status: classified.Status, Message: classified.Message}
}

// truncateBody keeps error bodies bounded and never empty.
func truncateBody(body []byte) string {
	text := strings.TrimSpace(string(body))
	if text == "" {
		return ""
	}
	if len(text) > 240 {
		return text[:240]
	}
	return text
}
