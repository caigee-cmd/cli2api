package workbuddy

import (
	"context"
	"errors"

	"github.com/caigee-cmd/cli2api/internal/providers"
)

func (client *Client) Checkin(ctx context.Context, accountID string) (providers.CheckinResult, error) {
	message, err := client.DailyCheckin(ctx, accountID)
	var already AlreadyCheckedInError
	if errors.As(err, &already) {
		return providers.CheckinResult{Status: "already", Message: message}, nil
	}
	if err != nil {
		return providers.CheckinResult{}, err
	}
	return providers.CheckinResult{Status: "success", Message: message}, nil
}
