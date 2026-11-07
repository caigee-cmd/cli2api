package qoder

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

const userSample = `[assistant 请求调用工具] <tool_call> Bash {"command":"cd /tmp && echo "=== 1. 示例目录结构对等 ===" && for d in examples/*/; do n=$(basename d); printf \"%-18s\" \"n"; done", "description":"Verify example structure parity"} </tool_call>`

func TestParseToolCallTagsUserSample(t *testing.T) {
	allowed := map[string]struct{}{"Bash": {}}
	calls, clean, ok := parseToolCallTags(userSample, allowed)
	if !ok || len(calls) != 1 {
		t.Fatalf("ok=%v calls=%d", ok, len(calls))
	}
	if calls[0].Function.Name != "Bash" {
		t.Fatalf("name=%q", calls[0].Function.Name)
	}
	var args map[string]any
	if err := json.Unmarshal([]byte(calls[0].Function.Arguments), &args); err != nil {
		t.Fatalf("args not JSON: %v (%s)", err, calls[0].Function.Arguments)
	}
	if _, ok := args["command"].(string); !ok {
		t.Fatalf("command missing: %#v", args)
	}
	if args["description"] != "Verify example structure parity" {
		t.Fatalf("description=%v", args["description"])
	}
	if clean != "" {
		t.Fatalf("clean=%q", clean)
	}
}

func TestParseToolCallTagsShapes(t *testing.T) {
	cases := []struct {
		name string
		text string
		allow map[string]struct{}
		wantName string
	}{
		{"bare", `<tool_call>Bash {"command":"ls -la"}</tool_call>`, map[string]struct{}{"Bash": {}}, "Bash"},
		{"object", `<tool_call>{"name":"Read","arguments":{"file_path":"a.js"}}</tool_call>`, map[string]struct{}{"Read": {}}, "Read"},
		{"parens", `<tool_call>Bash({"command":"pwd"})</tool_call>`, map[string]struct{}{"Bash": {}}, "Bash"},
		{"name-only", `<tool_call>Bash</tool_call>`, map[string]struct{}{"Bash": {}}, "Bash"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			calls, _, ok := parseToolCallTags(tc.text, tc.allow)
			if !ok || len(calls) != 1 || calls[0].Function.Name != tc.wantName {
				t.Fatalf("ok=%v calls=%#v", ok, calls)
			}
		})
	}
	multi := `<tool_call>Read {"file_path":"a"}</tool_call>
<tool_call>Bash {"command":"pwd"}</tool_call>`
	calls, _, ok := parseToolCallTags(multi, map[string]struct{}{"Read": {}, "Bash": {}})
	if !ok || len(calls) != 2 {
		t.Fatalf("multi ok=%v n=%d", ok, len(calls))
	}
}

func TestParseToolCallTagsGuards(t *testing.T) {
	prose := `例如可以写成 <tool_call>Bash {"command":"ls"}</tool_call> 这样，注意闭标签不能少。`
	if calls, clean, ok := parseToolCallTags(prose, map[string]struct{}{"Bash": {}}); ok || calls != nil || clean != prose {
		t.Fatalf("prose must stay content: ok=%v clean=%q", ok, clean)
	}
	if calls, _, ok := parseToolCallTags(`<tool_call>Hack {"x":1}</tool_call>`, map[string]struct{}{"Bash": {}}); ok || calls != nil {
		t.Fatal("undeclared name must not recover")
	}
	if calls, _, ok := parseToolCallTags(`<tool_call>Bash {"command":"ls"}</tool_call>`, nil); ok || calls != nil {
		t.Fatal("no tools declared must not recover")
	}
	if calls, _, ok := parseToolCallTags(`<tool_call>Bash {"command": ls}</tool_call>`, map[string]struct{}{"Bash": {}}); ok || calls != nil {
		t.Fatal("illegal args must not recover")
	}
	if calls, _, ok := parseToolCallTags("没有标签的普通回复", map[string]struct{}{"Bash": {}}); ok || calls != nil {
		t.Fatal("plain text must not recover")
	}
}

func TestRepairArgsJSON(t *testing.T) {
	fixed, ok := repairArgsJSON(`{"command":"echo "hi" && ls"}`)
	if !ok {
		t.Fatal("expected repair")
	}
	var args map[string]any
	if err := json.Unmarshal([]byte(fixed), &args); err != nil {
		t.Fatal(err)
	}
	if args["command"] != `echo "hi" && ls` {
		t.Fatalf("command=%v", args["command"])
	}
	fixed, ok = repairArgsJSON(`{"a":1,}`)
	if !ok {
		t.Fatal("trailing comma")
	}
	if err := json.Unmarshal([]byte(fixed), &args); err != nil || args["a"] != float64(1) {
		t.Fatalf("trailing comma result=%s err=%v", fixed, err)
	}
	if got, ok := repairArgsJSON(`{"a":1}`); !ok || got != `{"a":1}` {
		t.Fatalf("valid passthrough got=%q ok=%v", got, ok)
	}
	if _, ok := repairArgsJSON("{bad"); ok {
		t.Fatal("unrepairable must fail")
	}
	if got, ok := repairArgsJSON(""); !ok || got != "" {
		t.Fatalf("empty got=%q ok=%v", got, ok)
	}
}

func TestRepairToolCallArgumentsJSON(t *testing.T) {
	raw := json.RawMessage(`[{"id":"c1","type":"function","function":{"name":"Bash","arguments":"{\"command\":\"echo \"hi\" && ls\"}"}}]`)
	out := repairToolCallArgumentsJSON(raw)
	var calls []struct {
		Function struct {
			Arguments string `json:"arguments"`
		} `json:"function"`
	}
	if err := json.Unmarshal(out, &calls); err != nil {
		t.Fatal(err)
	}
	var args map[string]any
	if err := json.Unmarshal([]byte(calls[0].Function.Arguments), &args); err != nil {
		t.Fatalf("still invalid: %v (%s)", err, calls[0].Function.Arguments)
	}
	if args["command"] != `echo "hi" && ls` {
		t.Fatalf("command=%v", args["command"])
	}
}

func TestAllowedToolNames(t *testing.T) {
	raw := json.RawMessage(`[{"type":"function","function":{"name":"Bash"}},{"type":"function","function":{"name":"Read"}}]`)
	names := allowedToolNames(raw)
	if _, ok := names["Bash"]; !ok {
		t.Fatal("missing Bash")
	}
	if _, ok := names["Read"]; !ok {
		t.Fatal("missing Read")
	}
	if allowedToolNames(nil) != nil {
		t.Fatal("nil tools")
	}
}

func TestRecoverToolTagsFromContent(t *testing.T) {
	tools := json.RawMessage(`[{"type":"function","function":{"name":"Bash"}}]`)
	content, outTools, ok := recoverToolTagsFromContent(userSample, tools, nil)
	if !ok || content != "" || len(outTools) == 0 {
		t.Fatalf("ok=%v content=%q tools=%s", ok, content, outTools)
	}
	if !strings.Contains(string(outTools), `"Bash"`) {
		t.Fatalf("tools=%s", outTools)
	}
}

func TestRecoverToolTagStreamRewritesFakeXML(t *testing.T) {
	allowed := map[string]struct{}{"Bash": {}}
	upstream := "data: " + `{"body":{"choices":[{"delta":{"content":"[assistant 请求调用工具] <tool_call> Bash {\"command\":\"pwd\"} </tool_call>"}}]}}` + "\n\n" +
		"data: " + `{"body":{"choices":[{"delta":{},"finish_reason":"stop"}]}}` + "\n\n"
	var out strings.Builder
	if err := writeOpenAIChunks(&out, recoverToolTagStream(strings.NewReader(upstream), allowed), "m"); err != nil {
		t.Fatal(err)
	}
	body := out.String()
	if !strings.Contains(body, `"tool_calls"`) || !strings.Contains(body, `"Bash"`) {
		t.Fatalf("missing recovered tool_calls: %s", body)
	}
	if strings.Contains(body, "<tool_call>") {
		t.Fatalf("protocol leaked to client: %s", body)
	}
	if !strings.Contains(body, `"finish_reason":"tool_calls"`) {
		t.Fatalf("finish_reason: %s", body)
	}
}

func TestUpstreamStatusErrorMapsImageConversionTimeout(t *testing.T) {
	err := upstreamStatusError(http.StatusBadGateway, []byte(`{"message":"Image conversion timeout"}`))
	if !isImageConversionTimeoutErr(err) {
		t.Fatalf("status error not classified: %v", err)
	}
}
