package qoder

import (
	"context"
	"errors"
	"fmt"
	"net/http"
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
// picks another: Qoder's `default_context_window`, else the legacy
// `context_length` (the worker's max_input_tokens).
func qoderDefaultContextWindow(entry map[string]any) (int, bool) {
	if window, ok := numberFieldValue(entry, "default_context_window"); ok && window > 0 {
		return window, true
	}
	if window, ok := numberFieldValue(entry, "context_length"); ok && window > 0 {
		return window, true
	}
	return 0, false
}

// qoderLargestContextWindow is the largest selectable window, when Qoder
// advertises more than the default.
func qoderLargestContextWindow(entry map[string]any) (int, bool) {
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
