package accounts

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"
)

const (
	KindQuota       = "quota"
	KindRateLimit   = "rate_limit"
	KindAuth        = "auth"
	KindNotReady    = "not_ready"
	KindUnavailable = "unavailable"
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
		case notReadyLike(lower):
			kind = KindNotReady
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
			code, _ = errObj["code"].(string)
			typ, _ = errObj["type"].(string)
			kind, _ = errObj["kind"].(string)
			return msg, code, typ, kind
		}
		if m, ok := parsed["message"].(string); ok {
			msg = m
		}
		if c, ok := parsed["code"].(string); ok {
			code = c
		}
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

func quotaLike(lower, code, typ string) bool {
	if code == "insufficient_quota" || typ == "insufficient_quota" {
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
