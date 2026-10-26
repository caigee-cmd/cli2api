package console

import (
	"testing"

	"github.com/caigee-cmd/cli2api/internal/control"
)

// TestModelSettingResponseKimiCarriesReasoningOnly pins the PATCH/GET payload
// shape for Kimi Code: the console writes and reads a reasoning default, and
// there is no max-mode tier to report.
func TestModelSettingResponseKimiCarriesReasoningOnly(t *testing.T) {
	response := modelSettingResponse(control.ModelSetting{
		Provider:        "kimi",
		Model:           "kimi-for-coding",
		ReasoningEffort: "max",
		ContextCustom:   true,
	})
	if response["reasoning_effort"] != "max" {
		t.Fatalf("reasoning_effort = %#v", response["reasoning_effort"])
	}
	if response["context_custom"] != true {
		t.Fatalf("context_custom = %#v", response["context_custom"])
	}
	if response["provider"] != "kimi" || response["model"] != "kimi-for-coding" {
		t.Fatalf("identity = %#v", response)
	}
	if _, ok := response["max_mode"]; ok {
		t.Fatal("kimi has no max-mode switch to report")
	}
}
