package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/caigee-cmd/cli2api/internal/accounts"
	"github.com/caigee-cmd/cli2api/internal/executor"
	"github.com/caigee-cmd/cli2api/internal/providers"
)

func buildChatUsage(res executor.ChatResult) map[string]any {
	out := map[string]any{
		"prompt_tokens":     res.PromptTokens,
		"completion_tokens": res.CompletionTokens,
		"total_tokens":      res.PromptTokens + res.CompletionTokens,
		"source":            firstNonEmpty(res.UsageSource, "estimate"),
	}
	if res.CacheReadTokens != nil {
		out["cache_read_tokens"] = *res.CacheReadTokens
	}
	if res.CacheWriteTokens != nil {
		out["cache_write_tokens"] = *res.CacheWriteTokens
	}
	cachedTokens := res.CachedTokens
	if cachedTokens == nil {
		cachedTokens = res.CacheReadTokens
	}
	if cachedTokens != nil {
		out["prompt_tokens_details"] = map[string]any{"cached_tokens": *cachedTokens}
	}
	if res.Credits != nil {
		out["credits"] = *res.Credits
	}
	return out
}

func classifyAPIError(err error) accounts.Classified {
	if err == nil {
		return accounts.Classify(0, "", "", accounts.KindUnavailable, "")
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return accounts.Classified{
			Kind: accounts.KindCanceled, Status: 499, Failover: false,
			Code: "request_canceled", Message: err.Error(),
		}
	}
	var classifiedErr *providers.Error
	if errors.As(err, &classifiedErr) && classifiedErr != nil {
		message := strings.TrimSpace(classifiedErr.Message)
		if message == "" {
			message = classifiedErr.Error()
		}
		raw := strings.TrimSpace(strings.Join([]string{message, classifiedErr.Code, classifiedErr.Type}, " "))
		failoverHint := ""
		if classifiedErr.Failover != nil {
			if *classifiedErr.Failover {
				failoverHint = "1"
			} else {
				failoverHint = "0"
			}
		}
		classified := accounts.Classify(classifiedErr.Status, raw, "", classifiedErr.Kind, failoverHint)
		if classifiedErr.Code != "" {
			classified.Code = classifiedErr.Code
		}
		if classifiedErr.Type != "" {
			classified.Type = classifiedErr.Type
		}
		if classifiedErr.Message != "" {
			classified.Message = classifiedErr.Message
		}
		providerRetryAfter := classifiedErr.RetryAfter
		if providerRetryAfter <= 0 {
			providerRetryAfter = classifiedErr.Cooldown
		}
		if providerRetryAfter > 0 {
			classified.Cooldown = providerRetryAfter
			if classified.Kind == accounts.KindRateLimit && classified.Cooldown < 30*time.Second {
				classified.Cooldown = 30 * time.Second
			}
		}
		classified.RetryAfter = classified.Cooldown
		return classified
	}
	return accounts.Classify(0, err.Error(), "", "", "")
}

func writeClassifiedErr(w http.ResponseWriter, err error) {
	if err == nil {
		writeErr(w, http.StatusServiceUnavailable, "upstream_not_ready", "upstream not ready")
		return
	}
	classified := classifyAPIError(err)
	if classified.RetryAfter > 0 {
		seconds := int(classified.RetryAfter / time.Second)
		if classified.RetryAfter%time.Second != 0 {
			seconds++
		}
		if seconds < 1 {
			seconds = 1
		}
		w.Header().Set("Retry-After", fmt.Sprintf("%d", seconds))
	}
	w.Header().Set("X-Qoder-Error-Kind", classified.Kind)
	if classified.Failover {
		w.Header().Set("X-Qoder-Failover", "1")
	} else {
		w.Header().Set("X-Qoder-Failover", "0")
	}
	writeErr(w, classified.Status, classified.Code, classified.Message)
}
