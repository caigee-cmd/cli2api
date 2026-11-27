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

func TestAggregationCapabilitiesRoundTrip(t *testing.T) {
	s, err := OpenStore(filepath.Join(t.TempDir(), "aggregation-capabilities.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	agg := accounts.Aggregation{
		ID: "logical", Name: "Logical", Enabled: true,
		Strategy: accounts.AggregationPriority, AllowFallbacks: true, MaxAttempts: 2,
		ContextLength: 1_000_000, MaxOutputTokens: 32_000,
		ReasoningOptions: []string{"low", "high", "max"}, ReasoningEffort: "high",
		CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
		Members: []accounts.AggregationMember{{Provider: "qoder", Model: "model-a", Priority: 0, Weight: 100}},
	}
	saved, err := s.InsertAggregation(context.Background(), agg)
	if err != nil {
		t.Fatal(err)
	}
	if saved.ContextLength != 1_000_000 || saved.MaxOutputTokens != 32_000 {
		t.Fatalf("numeric overrides = %d/%d", saved.ContextLength, saved.MaxOutputTokens)
	}
	if len(saved.ReasoningOptions) != 3 || saved.ReasoningOptions[0] != "low" || saved.ReasoningEffort != "high" {
		t.Fatalf("reasoning = %v %q", saved.ReasoningOptions, saved.ReasoningEffort)
	}

	saved.ContextLength = 0
	saved.MaxOutputTokens = 0
	saved.ReasoningOptions = nil
	saved.ReasoningEffort = ""
	saved.UpdatedAt = time.Now().UTC()
	cleared, err := s.SaveAggregation(context.Background(), saved)
	if err != nil {
		t.Fatal(err)
	}
	if cleared.ContextLength != 0 || cleared.MaxOutputTokens != 0 || len(cleared.ReasoningOptions) != 0 || cleared.ReasoningEffort != "" {
		t.Fatalf("cleared = %+v", cleared)
	}
}
