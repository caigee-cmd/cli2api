package translate

import (
	"encoding/json"
	"testing"
)

func TestRepairToolCallArguments(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want string
		ok   bool
	}{
		{name: "valid passthrough", raw: `{"path":"a.go"}`, want: `{"path":"a.go"}`, ok: true},
		{name: "blank becomes object", raw: "   ", want: "{}", ok: true},
		{name: "trailing comma", raw: `{"path":"a.go",}`, want: `{"path":"a.go"}`, ok: true},
		{name: "missing closing brace", raw: `{"path":"a.go"`, want: `{"path":"a.go"}`, ok: true},
		{name: "missing nested closers", raw: `{"pattern":"x","paths":["a","b"`, want: `{"pattern":"x","paths":["a","b"]}`, ok: true},
		{name: "bare quote inside value", raw: `{"cmd":"echo "hi""}`, want: `{"cmd":"echo \"hi\""}`, ok: true},
		{name: "prose after object", raw: `{"path":"a.go"} and then explain`, want: `{"path":"a.go"}`, ok: true},
		{name: "cut mid string", raw: `{"cmd":"rm -rf /tmp/fo`, ok: false},
		{name: "cut mid key", raw: `{"pa`, ok: false},
		{name: "not json at all", raw: `please read the file`, ok: false},
	}
	for _, tc := range cases {
		got, ok := RepairToolCallArguments(tc.raw)
		if ok != tc.ok {
			t.Fatalf("%s: ok = %v, want %v (got %q)", tc.name, ok, tc.ok, got)
		}
		if !ok {
			continue
		}
		if got != tc.want {
			t.Fatalf("%s: got %q, want %q", tc.name, got, tc.want)
		}
		if !json.Valid([]byte(got)) {
			t.Fatalf("%s: repaired value is not valid JSON: %q", tc.name, got)
		}
	}
}
