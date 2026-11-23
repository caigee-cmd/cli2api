package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/caigee-cmd/cli2api/internal/accounts"
)

func TestAggregationMemberAccountIDRoundTrip(t *testing.T) {
	s, err := OpenStore(filepath.Join(t.TempDir(), "aggregation.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	agg := accounts.Aggregation{
		ID: "logical", Name: "Logical", Enabled: true,
		Strategy: accounts.AggregationPriority, AllowFallbacks: true, MaxAttempts: 2,
		CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
		Members: []accounts.AggregationMember{
			{Provider: "qoder", Region: "global", Model: "model-a", AccountID: "q2", Priority: 0, Weight: 100},
			{Provider: "workbuddy", Region: "cn", Model: "model-b", Priority: 1, Weight: 100},
		},
	}
	saved, err := s.InsertAggregation(context.Background(), agg)
	if err != nil {
		t.Fatal(err)
	}
	if len(saved.Members) != 2 || saved.Members[0].AccountID != "q2" || saved.Members[1].AccountID != "" {
		t.Fatalf("members = %+v", saved.Members)
	}
}
