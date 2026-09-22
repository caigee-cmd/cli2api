package trae

import (
	"encoding/json"
	"testing"

	"github.com/caigee-cmd/cli2api/internal/providers"
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
	models, err := parseCatalogModels(payload, PrimaryScene)
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

func TestParseCatalogModelsUsesLiveMaxAndContactReasoning(t *testing.T) {
	payload, _ := json.Marshal(map[string]any{
		"config_info_list": []map[string]any{
			{
				"config_name":            "glm-5.3",
				"context_window_tokens":  map[string]any{"dev": 200000},
				"display_config":         map[string]any{"display_name": "GLM-5.3", "max_mode": false},
				"display_contact_config": `{"reasoning":{"enable":true}}`,
				"model_detail_list": []map[string]any{{
					"max_tokens": 32000, "prompt_max_tokens": 168000,
					"model_extra_config": `{"v2_max_mode_enabled":true}`,
				}},
			},
			{
				"config_name":            "kimi-k2.7-code",
				"context_window_tokens":  map[string]any{"dev": 200000},
				"display_config":         map[string]any{"display_name": "Kimi-K2.7-Code"},
				"display_contact_config": map[string]any{"reasoning": map[string]any{"enable": true}},
				"model_detail_list": []map[string]any{{
					"model_extra_config": `{"Thinking":{"Type":"enabled"}}`,
				}},
			},
		},
	})
	models, err := parseCatalogModels(payload, PrimaryScene)
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 2 {
		t.Fatalf("models=%+v", models)
	}
	glm := models[0]
	if !glm.Capabilities.MaxMode || len(glm.Capabilities.ReasoningOptions) != 3 || glm.Capabilities.ReasoningDefault != "high" {
		t.Fatalf("glm live caps=%+v", glm.Capabilities)
	}
	kimi := models[1]
	if kimi.Capabilities.MaxMode || kimi.Capabilities.ReasoningType != "enabled" {
		t.Fatalf("kimi caps=%+v", kimi.Capabilities)
	}
}

func TestCatalogParsesConsumptionRate(t *testing.T) {
	payload := []byte(`{"config_info_list":[
		{"config_name":"glm-5.2","display_config":{"display_name":"GLM-5.2"},
		 "display_contact_config":"{\"consumption_rate\":{\"enable\":true,\"data\":{\"rate\":0.78}},\"reasoning\":{\"enable\":true}}"}
	]}`)
	models, err := parseCatalogModels(payload, PrimaryScene)
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 1 || models[0].Credits != "x0.78 credits" {
		t.Fatalf("models=%+v", models)
	}
}

func TestCatalogDedupesPreferringRate(t *testing.T) {
	payload := []byte(`{"config_info_list":[
		{"config_name":"glm-5.2","display_config":{"display_name":"GLM-5.2"},
		 "display_contact_config":"{\"consumption_rate\":{\"enable\":true,\"data\":{\"rate\":0.78}}}"},
		{"config_name":"glm-5.2","display_config":{"display_name":"GLM-5.2"},"display_contact_config":"{}"}
	]}`)
	models, err := parseCatalogModels(payload, PrimaryScene)
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 1 || models[0].Credits != "x0.78 credits" {
		t.Fatalf("models=%+v", models)
	}
}

func TestCatalogOverridesDeepSeekV41FlashDisplayName(t *testing.T) {
	payload := []byte(`{"config_info_list":[
		{"config_name":"deepseek-v4.1-flash","display_config":{"display_name":"DeepSeek-V4-Flash 正式版"}},
		{"config_name":"DeepSeek-V4-Flash-Official","display_config":{"display_name":"DeepSeek-V4-Flash 正式版"}}
	]}`)
	models, err := parseCatalogModels(payload, PrimaryScene)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, m := range models {
		got[m.NativeModel] = m.DisplayName
	}
	if got["deepseek-v4.1-flash"] != "DeepSeek-V4.1-Flash" {
		t.Fatalf("v4.1-flash display=%q", got["deepseek-v4.1-flash"])
	}
	if got["DeepSeek-V4-Flash-Official"] != "DeepSeek-V4-Flash 正式版" {
		t.Fatalf("official display=%q", got["DeepSeek-V4-Flash-Official"])
	}
}

// Models are served through the scene that lists them: a model both scenes
// carry (or that only the primary carries) uses the primary scene, while a
// model only the secondary scene lists keeps the secondary scene.
func TestMergeCatalogModelsAssignsServingScene(t *testing.T) {
	primary := []providers.ModelInfo{
		{NativeModel: "glm-5.3", PublicModel: "glm-5.3", Scene: PrimaryScene},
		{NativeModel: "deepseek-v4.1-flash", PublicModel: "deepseek-v4.1-flash", Scene: PrimaryScene},
	}
	extra := []providers.ModelInfo{
		{NativeModel: "glm-5.3", PublicModel: "glm-5.3", Scene: SecondaryScene},
		{NativeModel: "kimi-k2.6", PublicModel: "kimi-k2.6", Scene: SecondaryScene},
	}
	merged := mergeCatalogModels(primary, extra)
	byID := map[string]providers.ModelInfo{}
	for _, m := range merged {
		byID[m.NativeModel] = m
	}
	if byID["glm-5.3"].Scene != PrimaryScene {
		t.Fatalf("shared model must use the primary scene: %+v", byID["glm-5.3"])
	}
	if byID["deepseek-v4.1-flash"].Scene != PrimaryScene {
		t.Fatalf("primary-only model scene=%q", byID["deepseek-v4.1-flash"].Scene)
	}
	if byID["kimi-k2.6"].Scene != SecondaryScene {
		t.Fatalf("secondary-only model scene=%q", byID["kimi-k2.6"].Scene)
	}
	if got := preferredScene("", SecondaryScene); got != SecondaryScene {
		t.Fatalf("fallback scene=%q", got)
	}
}

func TestMergeCatalogModels(t *testing.T) {
	primary := []providers.ModelInfo{
		{NativeModel: "glm-5.3", PublicModel: "glm-5.3", Credits: "x0.78 credits"},
		{NativeModel: "kimi-k2.7-code", PublicModel: "kimi-k2.7-code"},
	}
	extra := []providers.ModelInfo{
		{NativeModel: "kimi-k2.7-code", PublicModel: "kimi-k2.7-code", Credits: "x0.83 credits"},
		{NativeModel: "kimi-k2.6", PublicModel: "kimi-k2.6", Credits: "x0.75 credits"},
		{NativeModel: "glm-5.3", PublicModel: "glm-5.3"},
	}
	merged := mergeCatalogModels(primary, extra)
	if len(merged) != 3 {
		t.Fatalf("merged len=%d %+v", len(merged), merged)
	}
	byID := map[string]providers.ModelInfo{}
	for _, m := range merged {
		byID[m.NativeModel] = m
	}
	if byID["kimi-k2.7-code"].Credits != "x0.83 credits" {
		t.Fatalf("kimi-k2.7-code=%+v", byID["kimi-k2.7-code"])
	}
	if byID["glm-5.3"].Credits != "x0.78 credits" {
		t.Fatalf("glm-5.3=%+v", byID["glm-5.3"])
	}
	if _, ok := byID["kimi-k2.6"]; !ok {
		t.Fatal("kimi-k2.6 from the extra scene must be merged in")
	}
}

func TestCatalogCapturesMaxTier(t *testing.T) {
	payload := []byte(`{"config_info_list":[{
		"config_name":"glm-5.3-flash","display_config":{"display_name":"GLM-5.3-Flash","max_mode":true},
		"context_window_tokens":{"dev":116000,"max":1000000},
		"model_detail_list":[
			{"max_tokens":16000,"prompt_max_tokens":100000},
			{"max_tokens":64000,"prompt_max_tokens":936000}
		]
	}]}`)
	models, err := parseCatalogModels(payload, PrimaryScene)
	if err != nil || len(models) != 1 {
		t.Fatalf("models=%+v err=%v", models, err)
	}
	caps := models[0].Capabilities
	if caps.MaxOutput != 16000 || caps.PromptMaxTokens != 100000 {
		t.Fatalf("default tier=%+v", caps)
	}
	if caps.MaxOutputMax != 64000 || caps.PromptMaxTokensMax != 936000 {
		t.Fatalf("max tier=%+v", caps)
	}
	if caps.ContextWindowMax != 1000000 || !caps.MaxMode {
		t.Fatalf("window/maxmode=%+v", caps)
	}
}

// A shared model carries different tiers per scene: the merge must keep the
// Max-capable entry (1M + max tier) while backfilling credits from the other.
func TestMergePrefersMaxCapableEntry(t *testing.T) {
	solo := providers.ModelInfo{
		NativeModel: "glm-5.3", PublicModel: "glm-5.3", Credits: "x0.78 credits",
		Capabilities: providers.ModelCapabilities{ContextWindow: 200000, MaxOutput: 32000, PromptMaxTokens: 168000},
	}
	code := providers.ModelInfo{
		NativeModel: "glm-5.3", PublicModel: "glm-5.3", Credits: "x0.78 credits",
		Capabilities: providers.ModelCapabilities{
			ContextWindow: 116000, ContextWindowMax: 1000000, MaxMode: true,
			MaxOutput: 16000, PromptMaxTokens: 100000, MaxOutputMax: 64000, PromptMaxTokensMax: 936000,
		},
	}
	merged := mergeCatalogModels([]providers.ModelInfo{solo}, []providers.ModelInfo{code})
	if len(merged) != 1 {
		t.Fatalf("merged=%+v", merged)
	}
	c := merged[0].Capabilities
	if c.ContextWindowMax != 1000000 || !c.MaxMode || c.MaxOutputMax != 64000 || c.PromptMaxTokensMax != 936000 {
		t.Fatalf("merge kept default tier: %+v", c)
	}
	if merged[0].Credits != "x0.78 credits" {
		t.Fatalf("credits backfill failed: %q", merged[0].Credits)
	}
}
