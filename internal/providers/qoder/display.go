package qoder

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"time"
)

// DisplayCatalog is the legacy worker HTTP catalog source. Lookup must not
// fall back to another account for an explicit unknown account ID.
type DisplayCatalog struct {
	Lookup func(string) (string, bool)
	Key    func() string
}

func (s DisplayCatalog) Models(ctx context.Context, id string, refresh bool) ([]map[string]any, error) {
	url, ok := s.Lookup(id)
	if !ok || url == "" {
		return nil, fmt.Errorf("no running Qoder account")
	}
	client := WorkerClient{HTTP: &http.Client{Timeout: 60 * time.Second}, ProxyAPIKey: s.Key(), AccountID: id}
	entries, _, _, err := client.Models(ctx, url, refresh)
	if err != nil {
		var transport TransportError
		if errors.As(err, &transport) {
			return nil, err
		}
		return nil, nil
	}
	for _, entry := range entries {
		ApplyModelPricing(entry)
		ApplyModelContext(entry)
	}
	return entries, nil
}

// ApplyModelContext writes the display catalog's context keys from the context
// metadata Qoder reports per model. The worker forwards `default_context_window`
// (the window the Qoder client selects by default) and `available_context_windows`
// (the selectable set); the console reads `catalog_context_length` /
// `catalog_context_length_max`. Without this the console fell back to a
// hardcoded window (180000) for every model, mis-sizing models such as
// glm-5.3-flash (1M) and deepseek-v4-pro (96K). Keys already present are left
// as-is, and nothing is written when Qoder reported no context metadata.
func ApplyModelContext(entry map[string]any) {
	if entry == nil {
		return
	}
	if _, ok := entry["catalog_context_length"]; !ok {
		if window, ok := qoderDefaultContextWindow(entry); ok {
			entry["catalog_context_length"] = window
		}
	}
	if _, ok := entry["catalog_context_length_max"]; !ok {
		if maxWindow, ok := qoderLargestContextWindow(entry); ok {
			entry["catalog_context_length_max"] = maxWindow
		}
	}
	if _, ok := entry["max_output_tokens"]; !ok {
		if output, ok := numberFieldValue(entry, "max_output_tokens"); ok && output > 0 {
			entry["max_output_tokens"] = output
		}
	}
}

// qoderDefaultContextWindow is the window the model runs at unless the operator
// picks another. PAT-backed accounts read the gateway's `/model/list` directly,
// where the selectable windows live in a nested `context_config` object
// (`{"200K":{"token_count":200000,"is_default":true},"400K":{...},"1M":{...}}`).
// The worker path (legacy CLI blobs) instead forwards flattened
// `default_context_window` / `context_length`, so both shapes are accepted.
func qoderDefaultContextWindow(entry map[string]any) (int, bool) {
	if window, _, ok := qoderContextConfigWindows(entry); ok {
		return window, true
	}
	if window, ok := numberFieldValue(entry, "default_context_window"); ok && window > 0 {
		return window, true
	}
	if window, ok := numberFieldValue(entry, "context_length"); ok && window > 0 {
		return window, true
	}
	if window, ok := numberFieldValue(entry, "max_input_tokens"); ok && window > 0 {
		return window, true
	}
	return 0, false
}

// qoderLargestContextWindow is the largest selectable window, when Qoder
// advertises more than the default.
func qoderLargestContextWindow(entry map[string]any) (int, bool) {
	if _, windows, ok := qoderContextConfigWindows(entry); ok {
		return windows[len(windows)-1], true
	}
	windows, ok := intSliceField(entry, "available_context_windows")
	if !ok {
		return 0, false
	}
	largest := 0
	for _, window := range windows {
		if window > largest {
			largest = window
		}
	}
	if largest <= 0 {
		return 0, false
	}
	return largest, true
}

// qoderContextConfigWindows reads the gateway's nested context metadata for
// PAT-backed accounts: /model/list reports the selectable windows as
// `context_config` ({"200K":{"token_count":200000,"is_default":true},...}).
// Returns the default window and every selectable window sorted ascending. The
// official client marks the default with `is_default` and falls back to the
// smallest selectable window, which is what this mirrors when nothing is
// marked.
func qoderContextConfigWindows(entry map[string]any) (int, []int, bool) {
	raw, exists := entry["context_config"]
	if !exists {
		return 0, nil, false
	}
	config, ok := raw.(map[string]any)
	if !ok || len(config) == 0 {
		return 0, nil, false
	}
	seen := make(map[int]bool, len(config))
	windows := make([]int, 0, len(config))
	defaultWindow := 0
	for _, value := range config {
		tier, ok := value.(map[string]any)
		if !ok {
			continue
		}
		window, ok := numberFieldValue(tier, "token_count")
		if !ok || window <= 0 {
			continue
		}
		if !seen[window] {
			seen[window] = true
			windows = append(windows, window)
		}
		if isDefault, _ := tier["is_default"].(bool); isDefault && defaultWindow == 0 {
			defaultWindow = window
		}
	}
	if len(windows) == 0 {
		return 0, nil, false
	}
	sort.Ints(windows)
	if defaultWindow == 0 {
		defaultWindow = windows[0]
	}
	return defaultWindow, windows, true
}

// ApplyModelPricing writes the display catalog's credits/free keys onto a raw
// Qoder worker model entry. The worker forwards `price_factor` (the multiplier
// the Qoder client renders as e.g. "1.50x") and `is_free`; the console and
// gateway read `credits`/`free`. Qoder reports the price per model, so no
// cross-region reconciliation is needed. Keys already present are left as-is,
// and nothing is written when Qoder reported no price, so an unpriced model is
// never shown as free.
func ApplyModelPricing(entry map[string]any) {
	if entry == nil {
		return
	}
	if _, ok := entry["credits"]; !ok {
		if credits := qoderEntryCredits(entry); credits != "" {
			entry["credits"] = credits
		}
	}
	if _, ok := entry["free"]; !ok {
		if qoderEntryFree(entry) {
			entry["free"] = true
		}
	}
}
