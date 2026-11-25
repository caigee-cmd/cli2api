package translate

import (
	"encoding/json"
	"testing"
)

func TestNormalizeToolCallArgumentsNeverReturnsInvalidJSON(t *testing.T) {
	cases := []struct {
		name       string
		raw        string
		want       string
		wantChange bool
	}{
		{name: "valid", raw: `{"path":"a.go"}`, want: `{"path":"a.go"}`},
		{name: "blank", raw: "  ", want: "{}", wantChange: true},
		{name: "trailing comma", raw: `{"path":"a.go",}`, want: `{"path":"a.go"}`, wantChange: true},
		{name: "missing brace", raw: `{"path":"a.go"`, want: `{"path":"a.go"}`, wantChange: true},
		{name: "truncated last field dropped", raw: `{"path":"a.go","body":"partial`, want: `{"path":"a.go"}`, wantChange: true},
		{name: "truncated only field becomes empty", raw: `{"command":"rm -rf /tmp/fo`, want: "{}", wantChange: true},
		{name: "truncated nested field dropped", raw: `{"path":"a.go","edits":[{"old":"x","new":"y`, want: `{"path":"a.go"}`, wantChange: true},
		{name: "not json at all", raw: `please read the file`, want: "{}", wantChange: true},
	}
	for _, tc := range cases {
		got, changed := NormalizeToolCallArguments(tc.raw)
		if !json.Valid([]byte(got)) {
			t.Fatalf("%s: result is not valid JSON: %q", tc.name, got)
		}
		if got != tc.want {
			t.Fatalf("%s: got %q, want %q", tc.name, got, tc.want)
		}
		if changed != tc.wantChange {
			t.Fatalf("%s: changed = %v, want %v", tc.name, changed, tc.wantChange)
		}
	}
}
