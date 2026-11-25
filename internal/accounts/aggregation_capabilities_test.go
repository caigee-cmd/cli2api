package accounts

import (
	"strings"
	"testing"
)

func capabilityTestCreate() CreateAggregation {
	return CreateAggregation{
		ID:      "pool",
		Name:    "Pool",
		Enabled: true,
		Members: []AggregationMember{{Provider: "qoder", Model: "glm-5.2"}},
	}
}

func TestPrepareCreateAggregationNormalizesCapabilities(t *testing.T) {
	input := capabilityTestCreate()
	input.ContextLength = 1_000_000
	input.MaxOutputTokens = 32_000
	input.ReasoningOptions = []string{"HIGH", "low", "max", "high"}
	input.ReasoningEffort = "HIGH"

	got, err := PrepareCreateAggregation(input)
	if err != nil {
		t.Fatal(err)
	}
	if got.ContextLength != 1_000_000 || got.MaxOutputTokens != 32_000 {
		t.Fatalf("numeric caps = %d/%d", got.ContextLength, got.MaxOutputTokens)
	}
	if len(got.ReasoningOptions) != 3 || got.ReasoningOptions[0] != "low" || got.ReasoningOptions[1] != "high" || got.ReasoningOptions[2] != "max" {
		t.Fatalf("options = %v", got.ReasoningOptions)
	}
	if got.ReasoningEffort != "high" {
		t.Fatalf("effort = %q", got.ReasoningEffort)
	}
}

func TestPrepareCreateAggregationRejectsInvalidCapabilities(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*CreateAggregation)
		want   string
	}{
		{"unknown level", func(in *CreateAggregation) { in.ReasoningOptions = []string{"turbo"} }, "unsupported reasoning level"},
		{"effort without options", func(in *CreateAggregation) { in.ReasoningEffort = "high" }, "requires reasoning_options"},
		{"effort outside options", func(in *CreateAggregation) {
			in.ReasoningOptions = []string{"low", "medium"}
			in.ReasoningEffort = "high"
		}, "must be one of the configured reasoning options"},
		{"tiny context", func(in *CreateAggregation) { in.ContextLength = 512 }, "context_length must be"},
		{"output above context", func(in *CreateAggregation) {
			in.ContextLength = 32_000
			in.MaxOutputTokens = 64_000
		}, "must not exceed context_length"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			input := capabilityTestCreate()
			tc.mutate(&input)
			_, err := PrepareCreateAggregation(input)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestApplyAggregationUpdateClearsAndRevalidatesCapabilities(t *testing.T) {
	current, err := PrepareCreateAggregation(func() CreateAggregation {
		input := capabilityTestCreate()
		input.ContextLength = 200_000
		input.MaxOutputTokens = 16_000
		input.ReasoningOptions = []string{"low", "high"}
		input.ReasoningEffort = "high"
		return input
	}())
	if err != nil {
		t.Fatal(err)
	}
	emptyOptions := []string{}
	emptyEffort := ""
	updated, err := ApplyAggregationUpdate(Aggregation{
		ID: current.ID, Name: current.Name, Enabled: current.Enabled,
		Strategy: current.Strategy, AllowFallbacks: current.AllowFallbacks, MaxAttempts: current.MaxAttempts,
		ContextLength: current.ContextLength, MaxOutputTokens: current.MaxOutputTokens,
		ReasoningOptions: current.ReasoningOptions, ReasoningEffort: current.ReasoningEffort,
		Members: current.Members,
	}, UpdateAggregation{ReasoningOptions: &emptyOptions, ReasoningEffort: &emptyEffort})
	if err != nil {
		t.Fatal(err)
	}
	if len(updated.ReasoningOptions) != 0 || updated.ReasoningEffort != "" {
		t.Fatalf("cleared reasoning = %v %q", updated.ReasoningOptions, updated.ReasoningEffort)
	}

	zeroContext := 0
	output := 32_000
	_, err = ApplyAggregationUpdate(Aggregation{ReasoningOptions: []string{"low"}}, UpdateAggregation{
		ContextLength: &zeroContext, MaxOutputTokens: &output,
	})
	if err != nil {
		t.Fatalf("output without a configured context should be allowed: %v", err)
	}
}
