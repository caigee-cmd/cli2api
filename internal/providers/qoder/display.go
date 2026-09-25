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
	}
	return entries, nil
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
