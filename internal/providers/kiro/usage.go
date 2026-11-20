package kiro

import (
	"math"
	"regexp"
	"strconv"
	"strings"
)

// Kiro does not send token counts on generateAssistantResponse. It sends
// contextUsageEvent (the share of the input window the request filled) and
// meteringEvent (credits). Input tokens are that share of the model's window;
// output tokens are estimated from what the stream produced. This follows
// Kiro-Go (proxy/kiro.go getContextWindowSize, estimateClaudeOutputTokens).

var claudeVersion = regexp.MustCompile(`claude-(?:opus|sonnet|haiku)-(\d+)(?:[.-](\d+))?`)

// contextWindow is the input window Kiro measures contextUsagePercentage
// against: 1M for Claude 4.6 and newer, 200K for 4.5 and earlier.
func contextWindow(model string) int {
	match := claudeVersion.FindStringSubmatch(strings.ToLower(model))
	if match == nil {
		return 200_000
	}
	major, err := strconv.Atoi(match[1])
	if err != nil {
		return 200_000
	}
	minor := 0
	if match[2] != "" {
		if minor, err = strconv.Atoi(match[2]); err != nil {
			return 200_000
		}
	}
	if major > 4 || (major == 4 && minor >= 6) {
		return 1_000_000
	}
	return 200_000
}

// inputFromContext turns a context percentage into input tokens.
func inputFromContext(model string, percentage float64) int {
	if percentage <= 0 {
		return 0
	}
	return int(math.Round(percentage / 100 * float64(contextWindow(model))))
}

// resolveUsage fills in tokens Kiro did not send. Counts the upstream stated
// are kept. A missing input count comes from the context percentage and a
// missing output count from the produced text; either one marks the source
// as an estimate.
func resolveUsage(model string, input, output int, contextPct float64, produced int) (int, int, bool) {
	estimated := false
	if input <= 0 {
		if derived := inputFromContext(model, contextPct); derived > 0 {
			input, estimated = derived, true
		}
	}
	if output <= 0 && produced > 0 {
		output, estimated = produced, true
	}
	return input, output, estimated
}

// streamUsage is the usage object on the final OpenAI chunk. It is empty only
// when Kiro sent nothing at all to count. prompt_tokens keeps OpenAI meaning:
// it includes the cache read and creation shares estimated in cache.
func streamUsage(model string, input, output int, contextPct float64, produced int, credits float64, sawCredits bool, cache *promptCacheUsage) map[string]any {
	input, output, estimated := resolveUsage(model, input, output, contextPct, produced)
	if input <= 0 && output <= 0 && !sawCredits {
		return map[string]any{}
	}
	usage := map[string]any{"prompt_tokens": input, "completion_tokens": output, "total_tokens": input + output}
	if cache != nil && input > 0 {
		read, write := cache.scaled(input)
		usage["cache_read_tokens"] = read
		usage["cache_write_tokens"] = write
		usage["prompt_tokens_details"] = map[string]any{"cached_tokens": read}
	}
	if sawCredits {
		usage["credits"] = credits
	}
	switch {
	case estimated:
		usage["source"] = "estimate"
	case sawCredits:
		usage["source"] = "provider"
	}
	return usage
}
