package doubaowork

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/caigee-cmd/cli2api/internal/accounts"
	"github.com/caigee-cmd/cli2api/internal/providers"
)

// Doubao gateway business codes. Login codes cool the account as auth; rate
// codes stay rate_limit; parameter and captcha codes are the request's problem,
// so the executor must not fail over or cool the account for them.
const (
	codeUserInvalid    = 710012000
	codeLoginExpired   = 710012001
	codeInvalidParam   = 710010202
	codeBizParamReject = 710020202
	codeCaptcha        = 710022004
	codeRateLimited    = 710012004
)

var cookiePattern = regexp.MustCompile(`(?i)(sessionid[^=;]*=)\S+`)

func redactSecrets(text string) string {
	if text == "" {
		return text
	}
	return cookiePattern.ReplaceAllString(text, "${1}[redacted]")
}

// Classify maps an upstream HTTP status and body onto the executor taxonomy by
// reading the Doubao business code first, then falling back to HTTP status.
func Classify(status int, body string) providers.ClassifiedError {
	safe := redactSecrets(strings.TrimSpace(body))
	code, message, subtype := extractUpstreamError(body)
	if message == "" {
		message = safe
	}
	if message == "" {
		message = defaultMessageForCode(code)
	}

	switch code {
	case codeUserInvalid, codeLoginExpired:
		return providers.ClassifiedError{Kind: accounts.KindAuth, Status: firstStatus(401), Message: withHint(message, "sign_in")}
	case codeRateLimited:
		return providers.ClassifiedError{Kind: accounts.KindRateLimit, Status: firstStatus(429), Message: withHint(message, "rate_limit")}
	case codeCaptcha:
		return providers.ClassifiedError{Kind: accounts.KindInvalidRequest, Status: firstStatus(403), Message: captchaHint(message, subtype)}
	case codeInvalidParam, codeBizParamReject:
		return providers.ClassifiedError{Kind: accounts.KindInvalidRequest, Status: firstStatus(400), Message: withHint(message, "invalid_param")}
	}

	// No known business code: classify by HTTP status.
	switch {
	case status == 401:
		return providers.ClassifiedError{Kind: accounts.KindAuth, Status: 401, Message: message}
	case status == 403:
		// Unknown 403 is a refusal, not a retryable rate limit.
		return providers.ClassifiedError{Kind: accounts.KindInvalidRequest, Status: 403, Message: message}
	case status == 429:
		return providers.ClassifiedError{Kind: accounts.KindRateLimit, Status: 429, Message: message}
	case status == 400 || status == 422 || accounts.IsInvalidRequestText(safe) || accounts.IsPromptLimitText(safe):
		return providers.ClassifiedError{Kind: accounts.KindInvalidRequest, Status: firstStatusInt(status, 400), Message: message}
	case status == 404:
		return providers.ClassifiedError{Kind: accounts.KindModelNotAvailable, Status: 404, Message: message}
	case status >= 500 || status == 0:
		return providers.ClassifiedError{Kind: accounts.KindUnavailable, Status: firstStatusInt(status, 502), Message: defaultNonEmpty(message, "doubao upstream unavailable")}
	}
	if status >= 400 {
		return providers.ClassifiedError{Kind: accounts.KindUnavailable, Status: status, Message: message}
	}
	return providers.ClassifiedError{}
}

// newProviderError builds the typed error the caller returns. The Kind drives
// executor cooldown/failover; invalid_request never fails over.
func newProviderError(status int, body string) *providers.Error {
	classified := Classify(status, body)
	return &providers.Error{
		Kind:    classified.Kind,
		Status:  classified.Status,
		Message: classified.Message,
	}
}

func newCodeError(code int, message string) *providers.Error {
	classified := Classify(statusForCode(code), fmt.Sprintf(`{"code":%d,"message":%q}`, code, message))
	return &providers.Error{Kind: classified.Kind, Status: classified.Status, Message: classified.Message}
}

func statusForCode(code int) int {
	switch code {
	case codeUserInvalid, codeLoginExpired:
		return 401
	case codeRateLimited:
		return 429
	case codeCaptcha:
		return 403
	case codeInvalidParam, codeBizParamReject:
		return 400
	}
	return 502
}

func withHint(message, kind string) string {
	hint := ""
	switch kind {
	case "sign_in":
		hint = "豆包工作的登录已失效，请在控制台重新导入 Cookie。"
	case "rate_limit":
		hint = "豆包侧限流，稍后再试。"
	case "invalid_param":
		hint = "豆包拒绝了请求参数。若反复出现，请把本次请求记录一并反馈。"
	}
	if hint == "" {
		return message
	}
	if message == "" {
		return hint
	}
	return message + "（" + hint + "）"
}

// captchaHint reads the decision subtype so the guidance matches the actual
// block: a missing device identity is not fixed by clicking a captcha.
func captchaHint(message, subtype string) string {
	lower := strings.ToLower(subtype)
	hint := "豆包要求人工验证：打开 https://www.doubao.com/chat/ 手动发一句话，过掉验证后再试。"
	switch {
	case strings.Contains(lower, "device"), strings.Contains(lower, "session"), strings.Contains(lower, "fingerprint"):
		hint = "这次请求缺设备标识，被判定为异常会话。重新导入一次 Cookie（粘贴完整 Cookie）通常即可，去网页点验证没用。"
	case strings.Contains(lower, "rate"), strings.Contains(lower, "frequen"), strings.Contains(lower, "speed"):
		hint = "请求过密被限流。停几分钟再用，期间不要反复重试——越试恢复越慢。"
	}
	if message == "" {
		return hint
	}
	return message + "（" + hint + "）"
}

func defaultMessageForCode(code int) string {
	switch code {
	case codeUserInvalid, codeLoginExpired:
		return "doubao work login invalid"
	case codeRateLimited:
		return "doubao work rate limited"
	case codeCaptcha:
		return "doubao work requires manual verification"
	case codeInvalidParam, codeBizParamReject:
		return "doubao work rejected the request parameters"
	}
	return ""
}

// upstreamErrorPayload is a lenient view over the several error envelopes the
// upstream uses.
type upstreamErrorPayload struct {
	Code      json.RawMessage `json:"code"`
	Message   string          `json:"message"`
	Msg       string          `json:"msg"`
	ErrorMsg  string          `json:"error_msg"`
	ErrorCode json.RawMessage `json:"error_code"`
	Data      json.RawMessage `json:"data"`
	Decision  json.RawMessage `json:"decision"`
	Extra     struct {
		Decision json.RawMessage `json:"decision"`
	} `json:"extra"`
	Error *struct {
		Code      json.RawMessage `json:"code"`
		Message   string          `json:"message"`
		ErrorCode json.RawMessage `json:"error_code"`
	} `json:"error"`
}

func extractUpstreamError(body string) (code int, message, subtype string) {
	trimmed := strings.TrimSpace(body)
	if trimmed == "" || !strings.HasPrefix(trimmed, "{") {
		return 0, "", ""
	}
	var payload upstreamErrorPayload
	if err := json.Unmarshal([]byte(trimmed), &payload); err != nil {
		return 0, "", ""
	}
	code = firstCode(payload.Code, payload.ErrorCode)
	message = firstNonEmpty(payload.Message, payload.ErrorMsg, payload.Msg)
	if payload.Error != nil {
		message = firstNonEmpty(payload.Error.Message, message)
		if ec := firstCode(payload.Error.Code, payload.Error.ErrorCode); ec != 0 {
			code = ec
		}
	}
	// Nested data.error_code is common on business rejections.
	if code == 0 && len(payload.Data) > 0 {
		var data struct {
			ErrorCode json.RawMessage `json:"error_code"`
			Code      json.RawMessage `json:"code"`
			Decision  json.RawMessage `json:"decision"`
		}
		if json.Unmarshal(payload.Data, &data) == nil {
			code = firstCode(data.ErrorCode, data.Code)
			subtype = decisionSubtype(data.Decision)
		}
	}
	if subtype == "" {
		subtype = decisionSubtype(firstRaw(payload.Extra.Decision, payload.Decision))
	}
	return code, message, subtype
}

func decisionSubtype(raw json.RawMessage) string {
	raw = trimJSON(raw)
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var obj struct {
		Subtype string `json:"subtype"`
	}
	if len(raw) > 0 && raw[0] == '"' {
		var text string
		if json.Unmarshal(raw, &text) == nil {
			if json.Unmarshal([]byte(text), &obj) == nil {
				return strings.TrimSpace(obj.Subtype)
			}
		}
		return ""
	}
	if json.Unmarshal(raw, &obj) == nil {
		return strings.TrimSpace(obj.Subtype)
	}
	return ""
}

func firstCode(values ...json.RawMessage) int {
	for _, raw := range values {
		raw = trimJSON(raw)
		if len(raw) == 0 || string(raw) == "null" {
			continue
		}
		var asInt int
		if json.Unmarshal(raw, &asInt) == nil && asInt != 0 {
			return asInt
		}
		var asString string
		if json.Unmarshal(raw, &asString) == nil {
			if parsed := atoi(asString); parsed != 0 {
				return parsed
			}
		}
	}
	return 0
}

func atoi(text string) int {
	text = strings.TrimSpace(text)
	if text == "" {
		return 0
	}
	value := 0
	for _, r := range text {
		if r < '0' || r > '9' {
			return 0
		}
		value = value*10 + int(r-'0')
	}
	return value
}

func firstRaw(values ...json.RawMessage) json.RawMessage {
	for _, raw := range values {
		if trimmed := trimJSON(raw); len(trimmed) > 0 && string(trimmed) != "null" {
			return trimmed
		}
	}
	return nil
}

func trimJSON(raw json.RawMessage) json.RawMessage {
	return json.RawMessage(strings.TrimSpace(string(raw)))
}

func firstStatus(status int) int {
	if status > 0 {
		return status
	}
	return 502
}

func firstStatusInt(status, fallback int) int {
	if status > 0 {
		return status
	}
	return fallback
}

func defaultNonEmpty(value, fallback string) string {
	if strings.TrimSpace(value) != "" {
		return value
	}
	return fallback
}
