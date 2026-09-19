package qoder

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/caigee-cmd/cli2api/internal/providers"
)

func (client *Client) Checkin(ctx context.Context, accountID string) (providers.CheckinResult, error) {
	workerURL, err := client.lookup(accountID)
	if err != nil {
		return providers.CheckinResult{}, err
	}
	client.mu.RLock()
	httpClient := client.adminHTTP
	client.mu.RUnlock()
	status, _, body, err := client.accountWorker(httpClient, accountID).Admin(ctx, workerURL, http.MethodPost, "/admin/checkin", "application/json", []byte("{}"))
	if err != nil {
		return providers.CheckinResult{}, err
	}
	var payload struct {
		OK bool `json:"ok"`
		providers.CheckinResult
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return providers.CheckinResult{}, fmt.Errorf("invalid worker check-in response")
	}
	if status != http.StatusOK || !payload.OK {
		if payload.Error.Message != "" {
			return providers.CheckinResult{}, fmt.Errorf("%s", payload.Error.Message)
		}
		return providers.CheckinResult{}, fmt.Errorf("worker check-in failed (HTTP %d)", status)
	}
	if !payload.CheckinResult.Valid() {
		return providers.CheckinResult{}, fmt.Errorf("invalid worker check-in status")
	}
	return payload.CheckinResult, nil
}
