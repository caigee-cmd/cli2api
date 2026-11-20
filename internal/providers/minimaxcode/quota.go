package minimaxcode

import (
	"context"
	"strings"

	"github.com/caigee-cmd/cli2api/internal/providers"
)

// Quota reports a minimal status window. The MiniMax Code gateway does not
// expose a quota endpoint over the Claude-compatible surface, so no numeric
// balance is invented; the account label is shown instead.
func (c *Client) Quota(ctx context.Context, accountID string) (*providers.QuotaInfo, error) {
	credential, err := c.loadCredential(ctx, accountID)
	if err != nil {
		return nil, err
	}
	label := strings.TrimSpace(credential.Plan)
	if label == "" {
		label = "MiniMax Code"
	}
	info := providers.QuotaInfo{
		ProviderID: ProviderID,
		Unit:       "状态",
		Plan:       label,
		FetchedAt:  c.now().UTC().Format("2006-01-02T15:04:05Z"),
		Windows:    []providers.QuotaWindow{{ID: "plan", Label: label, Unit: "状态"}},
	}
	return &info, nil
}
