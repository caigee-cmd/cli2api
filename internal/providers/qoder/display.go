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
	return entries, nil
}
