package kiro

import (
	"crypto/sha256"
	"encoding/json"
	"math"
	"strings"
	"sync"
	"time"
)

// Kiro reports neither cache reads nor cache writes, so they are estimated
// locally the way Kiro-Go does (proxy/cache_tracker.go): each request is cut
// into a prelude (model, system, tools) and one block per turn, every block
// end is fingerprinted with a running SHA-256, and a later request on the same
// account that repeats a stored prefix reads it from "cache". Fingerprints
// live in memory for the Anthropic default TTL of five minutes. The figures
// are an estimate for the request log and the client, not a billing number.

const (
	promptCacheTTL = 5 * time.Minute
	// Anthropic does not cache prefixes shorter than this.
	minCacheableTokens     = 1024
	opusMinCacheableTokens = 4096
	// The newest content in a request is never all served from cache.
	maxCacheReadShare = 0.85
	// Media is counted at a flat size instead of by its base64 length.
	mediaTokenEstimate = 1600
)

// promptCacheUsage is one request's estimate. Total is the local estimate of
// the whole prompt that Read and Write are measured against.
type promptCacheUsage struct {
	Read, Write, Total int
}

// scaled re-expresses the estimate against input, the prompt size the usage
// chunk reports (Kiro's context share), so the shares stay the same while
// read plus write never exceeds input.
func (u *promptCacheUsage) scaled(input int) (int, int) {
	if u == nil || input <= 0 || u.Total <= 0 {
		return 0, 0
	}
	ratio := float64(input) / float64(u.Total)
	read := min(int(math.Round(float64(u.Read)*ratio)), input)
	write := min(int(math.Round(float64(u.Write)*ratio)), input-read)
	return read, write
}

type cacheBreakpoint struct {
	fingerprint [32]byte
	tokens      int // cumulative estimate up to and including this block
}

type cacheProfile struct {
	model       string
	breakpoints []cacheBreakpoint
	total       int
}

// buildCacheProfile fingerprints a conversation block by block. A user turn
// hashes the same whether it is the current turn or a replayed history turn,
// so this request's full prompt is the prefix the next turn looks up.
func buildCacheProfile(c conversation) *cacheProfile {
	hasher := sha256.New()
	profile := &cacheProfile{model: c.Model}
	add := func(value any, tokens int) {
		encoded, err := json.Marshal(value)
		if err != nil {
			return
		}
		hasher.Write(encoded)
		hasher.Write([]byte{0})
		profile.total += tokens
		var fingerprint [32]byte
		copy(fingerprint[:], hasher.Sum(nil))
		profile.breakpoints = append(profile.breakpoints, cacheBreakpoint{fingerprint: fingerprint, tokens: profile.total})
	}
	prelude := struct {
		Model, System string
		Tools         []toolSpec
	}{c.Model, c.System, c.Tools}
	add(prelude, preludeTokens(c))
	for _, turn := range c.History {
		switch {
		case turn.User != nil:
			add(turn.User, userTokens(*turn.User))
		case turn.Assistant != nil:
			add(turn.Assistant, assistantTokens(*turn.Assistant))
		}
	}
	add(c.Current, userTokens(c.Current))
	if len(profile.breakpoints) == 0 || profile.total <= 0 {
		return nil
	}
	return profile
}

func preludeTokens(c conversation) int {
	total := estimateTokens(c.System)
	for _, tool := range c.Tools {
		total += estimateTokens(tool.Name) + estimateTokens(tool.Description)
		if schema, err := json.Marshal(tool.Parameters); err == nil {
			total += estimateTokens(string(schema))
		}
	}
	return total
}

func userTokens(turn userTurn) int {
	total := estimateTokens(turn.Text) + mediaTokenEstimate*(len(turn.Images)+len(turn.Documents))
	for _, result := range turn.Results {
		total += estimateTokens(result.Output)
	}
	return total
}

func assistantTokens(turn assistantTurn) int {
	total := estimateTokens(turn.Text)
	for _, block := range turn.Reasoning {
		total += estimateTokens(block.Text)
	}
	for _, call := range turn.ToolUses {
		total += estimateTokens(call.Name)
		if input, err := json.Marshal(call.Input); err == nil {
			total += estimateTokens(string(input))
		}
	}
	return total
}

func minCacheableFor(model string) int {
	if strings.Contains(strings.ToLower(model), "opus") {
		return opusMinCacheableTokens
	}
	return minCacheableTokens
}

// promptCache holds stored prefix fingerprints per account. The zero of a nil
// *promptCache is valid and estimates nothing.
type promptCache struct {
	mu      sync.Mutex
	entries map[string]map[[32]byte]time.Time // account -> fingerprint -> expiry
	now     func() time.Time
}

func newPromptCache() *promptCache {
	return &promptCache{entries: map[string]map[[32]byte]time.Time{}, now: time.Now}
}

// compute estimates cache use for profile without storing it. The first
// cacheable request on an account writes the whole prompt. Later requests read
// the longest stored prefix, capped so part of the prompt stays uncached, and
// write the rest up to that cap.
func (t *promptCache) compute(account string, profile *cacheProfile) promptCacheUsage {
	usage := promptCacheUsage{Total: profile.total}
	minTokens := minCacheableFor(profile.model)
	if profile.total < minTokens {
		return usage
	}
	now := t.now()
	t.mu.Lock()
	defer t.mu.Unlock()
	t.pruneLocked(now)
	entries := t.entries[account]
	if len(entries) == 0 {
		usage.Write = profile.total
		return usage
	}
	cacheable := int(float64(profile.total) * maxCacheReadShare)
	matched := 0
	for i := len(profile.breakpoints) - 1; i >= 0; i-- {
		point := profile.breakpoints[i]
		if point.tokens < minTokens {
			break
		}
		if _, ok := entries[point.fingerprint]; ok {
			matched = min(point.tokens, cacheable)
			break
		}
	}
	usage.Read = matched
	usage.Write = max(cacheable-matched, 0)
	return usage
}

// update stores every cacheable prefix of profile for the TTL.
func (t *promptCache) update(account string, profile *cacheProfile) {
	minTokens := minCacheableFor(profile.model)
	now := t.now()
	t.mu.Lock()
	defer t.mu.Unlock()
	t.pruneLocked(now)
	entries := t.entries[account]
	if entries == nil {
		entries = map[[32]byte]time.Time{}
		t.entries[account] = entries
	}
	for _, point := range profile.breakpoints {
		if point.tokens >= minTokens {
			entries[point.fingerprint] = now.Add(promptCacheTTL)
		}
	}
	if len(entries) == 0 {
		delete(t.entries, account)
	}
}

func (t *promptCache) pruneLocked(now time.Time) {
	for account, entries := range t.entries {
		for fingerprint, expires := range entries {
			if !expires.After(now) {
				delete(entries, fingerprint)
			}
		}
		if len(entries) == 0 {
			delete(t.entries, account)
		}
	}
}

// cacheTurn is one request's estimate. It is computed before the request is
// sent and stored only after the stream completes, so a failed request never
// seeds a hit for the next one.
type cacheTurn struct {
	tracker *promptCache
	account string
	profile *cacheProfile
	usage   promptCacheUsage
}

func (t *promptCache) begin(account string, c conversation) *cacheTurn {
	if t == nil || account == "" {
		return nil
	}
	profile := buildCacheProfile(c)
	if profile == nil {
		return nil
	}
	return &cacheTurn{tracker: t, account: account, profile: profile, usage: t.compute(account, profile)}
}

func (c *cacheTurn) estimate() *promptCacheUsage {
	if c == nil {
		return nil
	}
	usage := c.usage
	return &usage
}

func (c *cacheTurn) commit() {
	if c == nil {
		return
	}
	c.tracker.update(c.account, c.profile)
}
