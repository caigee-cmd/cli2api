package accounts

// QuotaWindow is one provider-native usage period, such as Devin daily
// and weekly included quota. The top-level QuotaSnapshot fields stay the
// tighter of these windows for routing.
type QuotaWindow struct {
	ID         string  `json:"id"`
	Label      string  `json:"label,omitempty"`
	Used       float64 `json:"used"`
	Total      float64 `json:"total"`
	Remaining  float64 `json:"remaining"`
	Percentage float64 `json:"percentage"`
	Unit       string  `json:"unit,omitempty"`
	ResetAt    string  `json:"reset_at,omitempty"`
	Exceeded   bool    `json:"exceeded,omitempty"`
}

// QuotaSnapshot is the quota state for one account.
// A zero value means "unknown"; only Exceeded influences routing.
type QuotaSnapshot struct {
	Used                     float64       `json:"used"`
	Total                    float64       `json:"total"`
	Remaining                float64       `json:"remaining"`
	Percentage               float64       `json:"percentage"`
	Unit                     string        `json:"unit"`
	Exceeded                 bool          `json:"exceeded"`
	Windows                  []QuotaWindow `json:"windows,omitempty"`
	HasAddOn                 bool          `json:"has_add_on"`
	AddOnUsed                float64       `json:"add_on_used"`
	AddOnTotal               float64       `json:"add_on_total"`
	AddOnRemaining           float64       `json:"add_on_remaining"`
	AddOnUnit                string        `json:"add_on_unit"`
	AddOnAvailable           *bool         `json:"add_on_available,omitempty"`
	HasResourcePackage       bool          `json:"has_resource_package"`
	ResourcePackageUsed      float64       `json:"resource_package_used"`
	ResourcePackageTotal     float64       `json:"resource_package_total"`
	ResourcePackageRemaining float64       `json:"resource_package_remaining"`
	ResourcePackageUnit      string        `json:"resource_package_unit"`
	ResourcePackageAvailable *bool         `json:"resource_package_available,omitempty"`
	FetchedAt                string        `json:"fetched_at"`
}
