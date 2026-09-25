package translate

import (
	"encoding/json"
	"strings"
	"testing"
)

const nativeFixture = `{
  "model": "codex/gpt-5.5",
  "stream": true,
  "instructions": "be brief",
  "input": [
    {"type":"message","role":"developer","content":[{"type":"input_text","text":"dev rules"}]},
    {"type":"message","role":"user","content":[{"type":"input_text","text":"find files"}]},
    {"type":"reasoning","id":"rs_1","summary":[],"encrypted_content":"gAAAA-opaque"},
    {"type":"function_call","id":"fc_1","call_id":"call_1","namespace":"mcp__fastctx","name":"glob","arguments":"{\"pattern\":\"*.go\"}"},
    {"type":"function_call_output","call_id":"call_1","output":"a.go"}
  ],
  "tools": [{"type":"namespace","name":"mcp__fastctx","tools":[{"type":"function","name":"glob","parameters":{"type":"object"}}]}],
  "reasoning": {"effort":"high","summary":"auto"},
  "include": ["reasoning.encrypted_content"],
  "store": false,
  "prompt_cache_key": "client-key",
  "x_future_field": {"nested": [1, 2.50, 3]}
}`

func TestParseNativeResponsesKeepsEveryTopLevelMember(t *testing.T) {
	request, err := ParseNativeResponses([]byte(nativeFixture))
	if err != nil {
		t.Fatal(err)
	}
	if request.Model() != "codex/gpt-5.5" || !request.Stream() {
		t.Fatalf("model=%q stream=%v", request.Model(), request.Stream())
	}
	fields := request.Fields()
	for _, key := range []string{"instructions", "input", "tools", "reasoning", "include", "store", "prompt_cache_key", "x_future_field"} {
		if _, ok := fields[key]; !ok {
			t.Errorf("member %q dropped", key)
		}
	}
	// Numbers keep their original text; nothing was round-tripped through float64.
	if string(fields["x_future_field"]) != `{"nested": [1, 2.50, 3]}` {
		t.Fatalf("unknown member rewritten: %s", fields["x_future_field"])
	}
	// Item identity survives: id, encrypted_content, namespace, call_id.
	input := string(fields["input"])
	for _, want := range []string{`"id":"rs_1"`, `"encrypted_content":"gAAAA-opaque"`, `"namespace":"mcp__fastctx"`, `"id":"fc_1"`, `"role":"developer"`} {
		if !strings.Contains(input, want) {
			t.Errorf("input lost %s", want)
		}
	}
	if request.RequestedReasoningEffort() != "high" {
		t.Fatalf("effort=%q", request.RequestedReasoningEffort())
	}
}

func TestNativeResponsesFieldsAreIsolatedCopies(t *testing.T) {
	request, err := ParseNativeResponses([]byte(nativeFixture))
	if err != nil {
		t.Fatal(err)
	}
	first := request.Fields()
	first["model"] = json.RawMessage(`"rewritten"`)
	first["input"][0] = 'X'
	delete(first, "tools")

	second := request.Fields()
	if string(second["model"]) != `"codex/gpt-5.5"` {
		t.Fatalf("model leaked across attempts: %s", second["model"])
	}
	if second["input"][0] != '[' {
		t.Fatal("input bytes aliased across attempts")
	}
	if _, ok := second["tools"]; !ok {
		t.Fatal("deleted member leaked across attempts")
	}
}

func TestNativeResponsesRequestOwnsItsBytes(t *testing.T) {
	body := []byte(`{"model":"m","input":"hi"}`)
	request, err := ParseNativeResponses(body)
	if err != nil {
		t.Fatal(err)
	}
	copy(body, []byte(`{"model":"x"`))
	if request.Model() != "m" || string(request.Fields()["model"]) != `"m"` {
		t.Fatal("request aliases the caller's buffer")
	}
}

func TestParseNativeResponsesRejectsMalformedInput(t *testing.T) {
	cases := map[string]string{
		"array body":        `[{"model":"m","input":"hi"}]`,
		"trailing value":    `{"model":"m","input":"hi"} {"x":1}`,
		"empty":             ``,
		"missing model":     `{"input":"hi"}`,
		"blank model":       `{"model":"  ","input":"hi"}`,
		"missing input":     `{"model":"m"}`,
		"empty input":       `{"model":"m","input":[]}`,
		"blank input":       `{"model":"m","input":"  "}`,
		"scalar item":       `{"model":"m","input":["hi"]}`,
		"number input":      `{"model":"m","input":3}`,
		"bad stream":        `{"model":"m","input":"hi","stream":"yes"}`,
		"previous response": `{"model":"m","input":"hi","previous_response_id":"resp_1"}`,
		"conversation":      `{"model":"m","input":"hi","conversation":{"id":"c"}}`,
		"tools object":      `{"model":"m","input":"hi","tools":{}}`,
		"reasoning string":  `{"model":"m","input":"hi","reasoning":"high"}`,
		"instructions num":  `{"model":"m","input":"hi","instructions":1}`,
	}
	for name, body := range cases {
		if _, err := ParseNativeResponses([]byte(body)); err == nil {
			t.Errorf("%s: accepted %s", name, body)
		}
	}
}

func TestNativeResponsesCompatMatchesLegacyTranslation(t *testing.T) {
	body := `{"model":"m","stream":true,"instructions":"sys","input":[{"role":"user","content":"hi"}],"tools":[{"type":"namespace","name":"mcp__fastctx","tools":[{"type":"function","name":"glob"}]}]}`
	request, err := ParseNativeResponses([]byte(body))
	if err != nil {
		t.Fatal(err)
	}
	native, err := request.Compat()
	if err != nil {
		t.Fatal(err)
	}
	var source ResponsesRequest
	if err := json.Unmarshal([]byte(body), &source); err != nil {
		t.Fatal(err)
	}
	legacy, err := TranslateResponsesRequest(source)
	if err != nil {
		t.Fatal(err)
	}
	a, _ := json.Marshal(native.Chat)
	b, _ := json.Marshal(legacy.Chat)
	if string(a) != string(b) || len(native.ToolNames) != len(legacy.ToolNames) {
		t.Fatalf("compat drifted from legacy:\n%s\n%s", a, b)
	}
}

// Parse accepts what the chat form cannot carry; only Compat reports it.
func TestNativeResponsesAcceptsInputTheCompatPathRejects(t *testing.T) {
	request, err := ParseNativeResponses([]byte(`{"model":"m","input":[{"type":"input_file","file_id":"f"},{"role":"user","content":"hi"}]}`))
	if err != nil {
		t.Fatalf("native parse must accept: %v", err)
	}
	if _, err := request.Compat(); err == nil {
		t.Fatal("compat path should still reject input_file")
	}
}

func TestNativeResponsesSessionSeedIsStableAcrossTurns(t *testing.T) {
	turn1, err := ParseNativeResponses([]byte(`{"model":"M","instructions":"sys","input":[{"role":"user","content":"start"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	turn2, err := ParseNativeResponses([]byte(`{"model":"m","instructions":"sys","input":[{"role":"user","content":"start"},{"type":"reasoning","encrypted_content":"x"},{"role":"assistant","content":"ok"},{"role":"user","content":"next"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if turn1.SessionSeed() == "" || turn1.SessionSeed() != turn2.SessionSeed() {
		t.Fatalf("seed moved across turns: %q vs %q", turn1.SessionSeed(), turn2.SessionSeed())
	}
	other, _ := ParseNativeResponses([]byte(`{"model":"m","instructions":"sys","input":[{"role":"user","content":"different"}]}`))
	if other.SessionSeed() == turn1.SessionSeed() {
		t.Fatal("different conversations share a seed")
	}
	noUser, _ := ParseNativeResponses([]byte(`{"model":"m","input":[{"role":"developer","content":"only rules"}]}`))
	if noUser.SessionSeed() != "" {
		t.Fatal("no user anchor must not fabricate a seed")
	}
}
