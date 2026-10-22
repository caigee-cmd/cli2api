package console

import (
	"reflect"
	"testing"
)

func TestParseStatsDims(t *testing.T) {
	cases := map[string][]string{
		"":                         nil,
		"model":                    {"model"},
		" Provider , model,model ": {"provider", "model"},
		"bogus,account":            {"account"},
		"bogus":                    nil,
	}
	for raw, want := range cases {
		if got := ParseStatsDims(raw); !reflect.DeepEqual(got, want) {
			t.Fatalf("ParseStatsDims(%q) = %#v, want %#v", raw, got, want)
		}
	}
}
