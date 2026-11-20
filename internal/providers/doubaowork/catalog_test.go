package doubaowork

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestParseSsrModelsOfficeOnly(t *testing.T) {
	office := map[string]any{
		"modeSelectData": map[string]any{
			"menuConfV2": map[string]any{
				"mode_list": map[string]any{
					"item_list": []any{
						map[string]any{
							"default_conversation_mode": "chat",
							"model_list":                map[string]any{"item_list": []any{map[string]any{"model_item_key": "ignore-me", "name": "Chat"}}},
						},
						map[string]any{
							"default_conversation_mode": "office",
							"mode_id":                   "3",
							"agent_mode":                1,
							"model_list": map[string]any{"item_list": []any{
								map[string]any{
									"model_item_key":     "person-kimi-k3-261111",
									"name":               "Kimi K3",
									"model_extra_params": map[string]any{"total_window_size": "300000"},
									"reasoning_effort_config": map[string]any{
										"option_list":   []any{map[string]any{"level": 3}, map[string]any{"level": 5}},
										"default_level": 5,
									},
									"tag_list": []any{map[string]any{"text": "1.48x"}},
								},
								map[string]any{
									"model_item_key":   "person-qwen-4-max",
									"name":             "Qwen 4 Max",
									"subscribe_config": map[string]any{"need_upgrade": true},
								},
							}},
						},
					},
				},
			},
		},
	}
	raw, _ := json.Marshal(office)
	html := `<html><script>window.__DATA__ = ` + string(raw) + `;</script></html>`
	catalog := parseSsrModels(html)
	if catalog == nil {
		t.Fatal("expected catalog")
	}
	if _, ok := catalog["doubao-kimi-k3"]; !ok {
		t.Fatalf("expected doubao-kimi-k3, got keys %v", keysOf(catalog))
	}
	for id := range catalog {
		if strings.Contains(id, "qwen") {
			t.Fatalf("need_upgrade model must be hidden, found %q", id)
		}
	}
	entry := catalog["doubao-kimi-k3"]
	if entry.Context != 300000 {
		t.Fatalf("context = %d", entry.Context)
	}
	if entry.Rate != 1.48 || entry.RateText != "1.48x" {
		t.Fatalf("rate = %v (%q)", entry.Rate, entry.RateText)
	}
	info := toModelInfo(entry)
	if !strings.Contains(info.DisplayName, "1.48x") {
		t.Fatalf("display name must carry the rate: %q", info.DisplayName)
	}
	if len(info.Capabilities.ReasoningOptions) != 2 {
		t.Fatalf("reasoning options = %v", info.Capabilities.ReasoningOptions)
	}
}

func TestBaselineCatalogExcludesUpgradeAndNeverEmpty(t *testing.T) {
	baseline := baselineCatalog()
	if len(baseline) < 9 {
		t.Fatalf("baseline too small: %d", len(baseline))
	}
	for id := range baseline {
		if strings.Contains(id, "pro") && strings.Contains(id, "doubao-2-1-pro") {
			t.Fatalf("need_upgrade baseline model leaked: %q", id)
		}
	}
}

func TestMergeCatalogsNeverShrinks(t *testing.T) {
	baseline := baselineCatalog()
	online := map[string]doubaoModel{
		"doubao-new-model": {ID: "doubao-new-model", Name: "New", Native: "person-new"},
	}
	merged := mergeCatalogs(online, baseline)
	if len(merged) < len(baseline) {
		t.Fatalf("merge shrank below baseline: %d < %d", len(merged), len(baseline))
	}
	if _, ok := merged["doubao-new-model"]; !ok {
		t.Fatal("online new model dropped")
	}
	if _, ok := merged["doubao-auto"]; !ok {
		t.Fatal("baseline auto model dropped")
	}
}

func TestSelectModelResolution(t *testing.T) {
	catalog := baselineCatalog()
	if entry, ok := selectModel(catalog, "doubaowork/doubao-auto"); !ok || entry.ID != "doubao-auto" {
		t.Fatalf("resolve prefixed: %+v %v", entry, ok)
	}
	if entry, ok := selectModel(catalog, "doubao-auto"); !ok || entry.ID != "doubao-auto" {
		t.Fatalf("resolve plain: %+v %v", entry, ok)
	}
	// Unknown falls back to auto.
	if entry, ok := selectModel(catalog, "nonexistent"); !ok || entry.ID != "doubao-auto" {
		t.Fatalf("fallback to auto: %+v %v", entry, ok)
	}
}

func keysOf(m map[string]doubaoModel) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
