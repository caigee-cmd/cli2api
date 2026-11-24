package accounts

import (
	"strings"
	"testing"
)

func TestValidAggregationIDAcceptsOnlyClientSafeChars(t *testing.T) {
	for _, id := range []string{"my-claude", "deepseek-v4.1-flash-1017", "gpt-4.1", "deepseek/v4-flash-1017", "glm-5", "a1"} {
		if !validAggregationID(id) {
			t.Fatalf("%q should be valid", id)
		}
	}
	for _, id := range []string{"", "DeepSeek-V4.1", "with space", "under_score", "colon:name"} {
		if validAggregationID(id) {
			t.Fatalf("%q should be invalid", id)
		}
	}
}

func TestPrepareCreateAggregationRejectsUnsupportedCharsWithActionableMessage(t *testing.T) {
	_, err := PrepareCreateAggregation(CreateAggregation{ID: "deep seek"})
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "may only contain lowercase letters, digits, '.', '-' and '/'") {
		t.Fatalf("message = %q", err.Error())
	}
}

func TestPrepareCreateAggregationRequiresAnID(t *testing.T) {
	_, err := PrepareCreateAggregation(CreateAggregation{ID: "  "})
	if err == nil || err.Error() != "aggregation id required" {
		t.Fatalf("err = %v", err)
	}
}

func TestPrepareCreateAggregationDefaultsNameToID(t *testing.T) {
	input := CreateAggregation{
		ID:      "DeepSeek-V4.1-Flash",
		Members: []AggregationMember{{Provider: "qoder", Region: "cn", Model: "glm-5", Weight: 100}},
	}
	prepared, err := PrepareCreateAggregation(input)
	if err != nil {
		t.Fatal(err)
	}
	if prepared.ID != "deepseek-v4.1-flash" {
		t.Fatalf("id = %q", prepared.ID)
	}
	if prepared.Name != "deepseek-v4.1-flash" {
		t.Fatalf("name = %q", prepared.Name)
	}
}

func TestPrepareCreateAggregationKeepsExplicitName(t *testing.T) {
	prepared, err := PrepareCreateAggregation(CreateAggregation{
		ID:      "my-claude",
		Name:    "My Claude",
		Members: []AggregationMember{{Provider: "qoder", Region: "cn", Model: "glm-5", Weight: 100}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if prepared.Name != "My Claude" {
		t.Fatalf("name = %q", prepared.Name)
	}
}
