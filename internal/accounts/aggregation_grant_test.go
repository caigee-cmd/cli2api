package accounts

import "testing"

func TestAggregationGrantParseAndNormalize(t *testing.T) {
	grant, err := ParseProviderGrant("aggregation")
	if err != nil || grant.Provider != AggregationProvider || grant.Region != "" {
		t.Fatalf("parse aggregation: %+v err=%v", grant, err)
	}
	if _, err := ParseProviderGrant("aggregation:cn"); err == nil {
		t.Fatal("aggregation:cn must be rejected (no regions for sentinel)")
	}
	normalized, err := NormalizeAPIKeyProviders([]string{"aggregation"})
	if err != nil || len(normalized) != 1 || normalized[0] != "aggregation" {
		t.Fatalf("normalize: %v err=%v", normalized, err)
	}
}

func TestIsAggregationOnly(t *testing.T) {
	cases := []struct {
		allowed []string
		want    bool
	}{
		{[]string{"aggregation"}, true},
		{[]string{"aggregation", "aggregation"}, true},
		{[]string{"aggregation", "qoder"}, false},
		{[]string{"qoder"}, false},
		{nil, false},
	}
	for _, tc := range cases {
		if got := IsAggregationOnly(tc.allowed); got != tc.want {
			t.Errorf("IsAggregationOnly(%v)=%v want %v", tc.allowed, got, tc.want)
		}
	}
}

func TestAggregationGrantDoesNotAllowRealProvider(t *testing.T) {
	allowed := []string{"aggregation"}
	if ProviderAllowed("qoder", allowed) {
		t.Fatal("aggregation-only key must not authorize real provider qoder")
	}
	if !ProviderAllowed("qoder", nil) {
		t.Fatal("empty allowlist must allow all providers")
	}
}
