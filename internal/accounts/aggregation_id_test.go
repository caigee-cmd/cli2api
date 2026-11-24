package accounts

import (
	"strings"
	"testing"
)

func TestValidAggregationIDAcceptsOnlyClientSafeChars(t *testing.T) {
	for _, id := range []string{"my-claude", "deepseek/v4-flash-1017", "glm-5", "a1"} {
		if !validAggregationID(id) {
			t.Fatalf("%q should be valid", id)
		}
	}
	for _, id := range []string{"", "DeepSeek-V4.1", "with space", "under_score", "dot.id"} {
		if validAggregationID(id) {
			t.Fatalf("%q should be invalid", id)
		}
	}
}

func TestPrepareCreateAggregationRejectsDotsWithActionableMessage(t *testing.T) {
	_, err := PrepareCreateAggregation(CreateAggregation{ID: "deepseek-v4.1-flash"})
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "may only contain lowercase letters, digits, '-' and '/'") {
		t.Fatalf("message = %q", err.Error())
	}
}

func TestPrepareCreateAggregationRequiresAnID(t *testing.T) {
	_, err := PrepareCreateAggregation(CreateAggregation{ID: "  "})
	if err == nil || err.Error() != "aggregation id required" {
		t.Fatalf("err = %v", err)
	}
}
