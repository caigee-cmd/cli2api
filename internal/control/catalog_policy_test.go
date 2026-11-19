package control

import "testing"

func TestMergeModelEntryCapabilitiesMergesUpstreamReasoningOptionValues(t *testing.T) {
	merged := map[string]any{
		"reasoning_option_values": []string{"off", "low"},
		"reasoning_options":       []string{"none", "low"},
	}
	incoming := map[string]any{
		"reasoning_option_values": []string{"medium", "max"},
		"reasoning_options":       []string{"medium", "max"},
	}
	MergeModelEntryCapabilities(merged, incoming)
	values, ok := merged["reasoning_option_values"].([]string)
	if !ok {
		t.Fatalf("reasoning_option_values = %#v", merged["reasoning_option_values"])
	}
	want := "off,low,medium,max"
	got := ""
	for i, value := range values {
		if i > 0 {
			got += ","
		}
		got += value
	}
	if got != want {
		t.Fatalf("reasoning_option_values = %q, want %q", got, want)
	}
	options, ok := merged["reasoning_options"].([]string)
	if !ok {
		t.Fatalf("reasoning_options = %#v", merged["reasoning_options"])
	}
	wantOptions := "none,low,medium,max"
	gotOptions := ""
	for i, value := range options {
		if i > 0 {
			gotOptions += ","
		}
		gotOptions += value
	}
	if gotOptions != wantOptions {
		t.Fatalf("reasoning_options = %q, want %q", gotOptions, wantOptions)
	}
}

func TestMergeModelEntryCapabilitiesFillsReasoningFromLaterSource(t *testing.T) {
	merged := map[string]any{}
	incoming := map[string]any{
		"reasoning_option_values": []string{"off", "medium", "max"},
		"reasoning_options":       []string{"none", "medium", "max"},
		"reasoning_default":       "medium",
	}
	MergeModelEntryCapabilities(merged, incoming)
	if _, ok := merged["reasoning_option_values"]; !ok {
		t.Fatal("later source reasoning options were not merged")
	}
	if merged["reasoning_default"] != "medium" {
		t.Fatalf("reasoning_default = %#v", merged["reasoning_default"])
	}
}
