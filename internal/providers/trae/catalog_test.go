package trae

import (
	"encoding/json"
	"testing"
)

func TestParseCatalogModelsKeepsVisibleWindowsAndHidesInternal(t *testing.T) {
	payload, _ := json.Marshal(map[string]any{
		"config_info_list": []map[string]any{
			{
				"config_name":           "glm-5.2",
				"context_window_tokens": map[string]any{"dev": 200000},
				"display_config": map[string]any{
					"display_name":     "GLM-5.2",
					"model_capability": "reasoning_model",
				},
				"model_detail_list": []map[string]any{{
					"max_tokens": 32000, "prompt_max_tokens": 168000,
				}},
			},
			{
				"config_name":           "DeepSeek-V4-Pro-Official",
				"context_window_tokens": map[string]any{"dev": 200000, "max": 1000000},
				"display_config": map[string]any{
					"display_name": "DeepSeek-V4-Pro 正式版",
					"max_mode":     false,
				},
				"reasoning_effort_config": map[string]any{
					"support_thinking": true,
					"options":          []string{"low", "medium", "high", "xhigh"},
					"default_level":    "medium",
				},
			},
			{
				"config_name":          "seed-code-pro-0430",
				"is_invisible_to_user": true,
				"display_config":       map[string]any{"display_name": "Doubao-Seed-2.1-Pro", "max_mode": true},
			},
			{
				"config_name":           "custom_model_1M",
				"context_window_tokens": map[string]any{"dev": 1000000},
				"display_config":        map[string]any{"display_name": ""},
			},
			{
				"config_name":    "browser_use_subagent",
				"display_config": map[string]any{"display_name": "Browser"},
			},
		},
	})
	models, err := parseCatalogModels(payload)
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 2 {
		t.Fatalf("models=%+v", models)
	}
	if models[0].NativeModel != "glm-5.2" || models[0].Capabilities.ContextWindow != 200000 || models[0].Capabilities.MaxMode {
		t.Fatalf("glm=%+v", models[0])
	}
	ds := models[1]
	if ds.NativeModel != "DeepSeek-V4-Pro-Official" || !ds.Capabilities.MaxMode || ds.Capabilities.ContextWindowMax != 1000000 {
		t.Fatalf("deepseek=%+v", ds.Capabilities)
	}
	if len(ds.Capabilities.ReasoningOptions) != 4 || ds.Capabilities.ReasoningDefault != "medium" {
		t.Fatalf("reasoning=%+v", ds.Capabilities)
	}
}

func TestParseReasoningOptionsRejectsIncompleteConfig(t *testing.T) {
	options, def := parseReasoningOptions([]byte(`{"support_thinking":true,"options":["low"],"default_level":"medium"}`))
	if options != nil || def != "" {
		t.Fatalf("incomplete config leaked: %v %s", options, def)
	}
}
