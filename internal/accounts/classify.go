package accounts

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"
)

const (
	KindQuota             = "quota"
	KindRateLimit         = "rate_limit"
	KindAuth              = "auth"
	KindNotReady          = "not_ready"
	KindUnavailable       = "unavailable"
	KindInvalidRequest    = "invalid_request"
	KindModelNotAvailable = "model_not_available"
)

const maxRetryAfter = 10 * time.Minute

type Classified struct {
	Kind       string
	Status     int
	Failover   bool
	Cooldown   time.Duration
	Code       string
	Type       string
	Message    string
	RetryAfter time.Duration
	// Model scopes the cooldown to one public model. When set, only that
	// model is cooled; the account keeps serving everything else.
	Model string
}

// backoffFloor and backoffCeiling bound the exponential backoff applied to
// repeated failures of the same kind. A first failure still uses the
// classifier's own duration; backoff only extends it on repeats.
const (
	backoffFloor    = 30 * time.Second
	backoffCeiling  = 6 * time.Hour
	backoffMaxLevel = 8
)

// nextBackoffCooldown lengthens the cooldown for a repeatedly failing account.
// level is the count of consecutive failures of this kind; the returned level
// is the value to store for the next failure. Success resets it to zero.
func nextBackoffCooldown(base time.Duration, level int) (time.Duration, int) {
	if level < 0 {
		level = 0
	}
	if level >= backoffMaxLevel {
		return backoffCeiling, level
	}
	multiplier := time.Duration(1) << level
	next := base * multiplier
	if next < backoffFloor {
		next = backoffFloor
	}
	if next >= backoffCeiling {
		return backoffCeiling, level
	}
	return next, level + 1
}

func ParseRetryAfter(raw string, fallback time.Duration) time.Duration {
	text := strings.TrimSpace(raw)
	if text != "" {
		if sec, err := strconv.ParseFloat(text, 64); err == nil && sec > 0 {
			d := time.Duration(sec * float64(time.Second))
			if d > maxRetryAfter {
				return maxRetryAfter
			}
			return d
		}
		if t, err := time.Parse(time.RFC1123, text); err == nil {
			d := time.Until(t)
			if d > 0 {
				if d > maxRetryAfter {
					return maxRetryAfter
				}
				return d
			}
		}
	}
	if fallback < 0 {
		return 0
	}
	if fallback > maxRetryAfter {
		return maxRetryAfter
	}
	return fallback
}

func Classify(status int, body, retryAfter, kindHint, failoverHint string) Classified {
	msg, code, typ, nestedKind := extractError(body)
	kind := strings.TrimSpace(firstNonEmpty(kindHint, nestedKind))
	lower := strings.ToLower(msg + " " + code + " " + typ)
	if kind == "" {
		switch {
		case quotaLike(lower, code, typ):
			kind = KindQuota
		case modelNotAvailableLike(lower, code):
			kind = KindModelNotAvailable
		case notReadyLike(lower):
			kind = KindNotReady
		case IsInvalidRequestText(lower):
			kind = KindInvalidRequest
		case authLike(lower) && !quotaLike(lower, code, typ) && !rateLike(lower):
			kind = KindAuth
		case rateLike(lower) || status == 429:
			kind = KindRateLimit
		case status == 401 || status == 403:
			kind = KindAuth
		default:
			kind = KindUnavailable
		}
	}
	if kind == KindAuth && quotaLike(lower, code, typ) {
		kind = KindQuota
	}
	if kind == KindRateLimit && quotaLike(lower, code, typ) {
		kind = KindQuota
	}

	out := Classified{Kind: kind, Message: strings.TrimSpace(msg), Code: firstNonEmpty(code, kind), Type: firstNonEmpty(typ, "api_error")}
	switch kind {
	case KindQuota:
		out.Status = 429
		out.Failover = false
		out.Cooldown = 0
		out.Code = firstNonEmpty(code, "insufficient_quota")
		out.Type = "insufficient_quota"
	case KindRateLimit:
		out.Status = 429
		out.Failover = true
		out.Cooldown = ParseRetryAfter(retryAfter, 60*time.Second)
	case KindAuth:
		if status == 401 {
			out.Status = 401
		} else {
			out.Status = 403
		}
		out.Failover = true
		out.Cooldown = ParseRetryAfter(retryAfter, 30*time.Second)
		out.Code = firstNonEmpty(code, "unauthorized")
	case KindNotReady:
		out.Status = 503
		out.Failover = true
		out.Cooldown = ParseRetryAfter(retryAfter, 10*time.Second)
		out.Code = firstNonEmpty(code, "not_ready")
	case KindInvalidRequest:
		// Request content the upstream rejected; retrying on another account
		// cannot succeed, and the account itself is healthy.
		out.Status = status
		if out.Status < 400 {
			out.Status = 400
		}
		out.Failover = false
		out.Cooldown = 0
		out.Type = firstNonEmpty(typ, "invalid_request_error")
		out.Code = firstNonEmpty(code, "invalid_request")
	case KindModelNotAvailable:
		// The account is healthy; a stale catalog may still need a retry
		// on another account. Never cool the account down.
		out.Status = 400
		out.Failover = true
		out.Cooldown = 0
		out.Type = firstNonEmpty(typ, "invalid_request_error")
		out.Code = firstNonEmpty(code, "model_not_available")
	default:
		if status >= 500 {
			out.Status = status
		} else if status >= 400 {
			out.Status = status
		} else {
			out.Status = 502
		}
		out.Failover = true
		out.Cooldown = ParseRetryAfter(retryAfter, 15*time.Second)
		out.Code = firstNonEmpty(code, "upstream_error")
	}
	if failoverHint == "0" {
		out.Failover = false
	} else if failoverHint == "1" {
		out.Failover = true
	}
	out.RetryAfter = out.Cooldown
	if out.Message == "" {
		out.Message = out.Code
	}
	return out
}

func extractError(body string) (msg, code, typ, kind string) {
	text := strings.TrimSpace(body)
	if text == "" {
		return "", "", "", ""
	}
	var parsed map[string]any
	if json.Unmarshal([]byte(text), &parsed) == nil {
		if errObj, ok := parsed["error"].(map[string]any); ok {
			msg, _ = errObj["message"].(string)
			code = stringifyJSONCode(errObj["code"])
			typ, _ = errObj["type"].(string)
			kind, _ = errObj["kind"].(string)
			return msg, code, typ, kind
		}
		if m, ok := parsed["message"].(string); ok {
			msg = m
		}
		code = stringifyJSONCode(parsed["code"])
		if t, ok := parsed["type"].(string); ok {
			typ = t
		}
		if k, ok := parsed["kind"].(string); ok {
			kind = k
		}
		if msg != "" || code != "" {
			return msg, code, typ, kind
		}
	}
	return text, "", "", ""
}

func stringifyJSONCode(v any) string {
	switch c := v.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(c)
	case float64:
		return strconv.FormatFloat(c, 'f', -1, 64)
	case json.Number:
		return strings.TrimSpace(c.String())
	default:
		return ""
	}
}

func quotaLike(lower, code, typ string) bool {
	if code == "insufficient_quota" || typ == "insufficient_quota" || code == "1005" || code == "4008" {
		return true
	}
	return strings.Contains(lower, "insufficient_quota") ||
		strings.Contains(lower, "token-limit") ||
		strings.Contains(lower, "#token-limit") ||
		strings.Contains(lower, "exceeded your current quota") ||
		strings.Contains(lower, "oversized prompt") ||
		strings.Contains(lower, "local precheck rejected")
}

func rateLike(lower string) bool {
	return strings.Contains(lower, "too many requests") ||
		strings.Contains(lower, "rate limit") ||
		strings.Contains(lower, "rate-limit") ||
		strings.Contains(lower, "response code=429") ||
		strings.Contains(lower, "account busy") ||
		strings.Contains(lower, "in-flight")
}

func authLike(lower string) bool {
	return strings.Contains(lower, "null pointer") ||
		strings.Contains(lower, "forbidden") ||
		strings.Contains(lower, "duplicate request") ||
		strings.Contains(lower, "unauthorized") ||
		strings.Contains(lower, "401") ||
		strings.Contains(lower, "403") ||
		strings.Contains(lower, "credential") ||
		strings.Contains(lower, "refresh token") ||
		strings.Contains(lower, "access token")
}

func notReadyLike(lower string) bool {
	return strings.Contains(lower, "hot context not ready") ||
		strings.Contains(lower, "auth manager not captured") ||
		strings.Contains(lower, "not ready")
}

func modelNotAvailableLike(lower, code string) bool {
	if code == "model_not_available" || code == "model_catalog_unavailable" {
		return true
	}
	return strings.Contains(lower, "model_not_available") ||
		strings.Contains(lower, "is not available for this qoder account") ||
		strings.Contains(lower, "model_catalog_unavailable") ||
		strings.Contains(lower, "no accounts serve model")
}

// IsInvalidRequestText reports whether an error body looks like an upstream
// content-screening rejection: the request itself is the problem, so no
// account should fail over or cool down. Auth and quota shapes are matched
// earlier in Classify and never reach this check.
func IsInvalidRequestText(text string) bool {
	lower := strings.ToLower(text)
	return strings.Contains(lower, "sensitive") ||
		strings.Contains(lower, "敏感") ||
		strings.Contains(lower, "违规") ||
		strings.Contains(lower, "风险") ||
		strings.Contains(lower, "拦截") ||
		strings.Contains(lower, "moderation") ||
		strings.Contains(lower, "content filter") ||
		strings.Contains(lower, "content_filter")
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
