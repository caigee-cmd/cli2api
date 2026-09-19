package providers

import "context"

type CheckinPolicy struct {
	Timezone string `json:"timezone"`
}

type CheckinResult struct {
	Status        string  `json:"status"`
	Message       string  `json:"message"`
	RewardCredits float64 `json:"reward_credits,omitempty"`
}

func (result CheckinResult) Valid() bool {
	return result.Status == "success" || result.Status == "already" || result.Status == "skipped"
}

type AccountCheckiner interface {
	Checkin(ctx context.Context, accountID string) (CheckinResult, error)
}

func CheckinFor(providerID, regionID string) (*CheckinPolicy, bool) {
	_, region, err := Resolve(providerID, regionID)
	return region.Checkin, err == nil && region.Checkin != nil
}

func (descriptor ProviderDescriptor) SupportsCheckin() bool {
	for _, region := range descriptor.Regions {
		if region.Checkin != nil {
			return true
		}
	}
	return false
}
