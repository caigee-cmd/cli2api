package kiro

import (
	"errors"
	"testing"
)

var errUpstreamTest = errors.New("upstream reset")

func TestEstimateTokens(t *testing.T) {
	cases := map[string]int{
		"":             0,
		"ok":           1,
		"abcd":         2,
		"hello world":  3, // 11 ascii incl. space / 4.5
		"1234567890":   5,
		"{}[]()":       4,
		"你好世界你好":       4, // 6 non-ASCII / 1.5
		"hello, 世界123": 5, // 6 ascii incl. space/4.5 + 1 comma/1.5 + 2/1.5 + 3/2 = 4.83
	}
	for text, want := range cases {
		if got := estimateTokens(text); got != want {
			t.Errorf("estimateTokens(%q) = %d, want %d", text, got, want)
		}
	}
}

// Rounding happens once, so many small fragments add up to the same estimate
// as the joined text.
func TestTokenCounterRoundsOnce(t *testing.T) {
	var counter tokenCounter
	for range 9 {
		counter.add("ab")
	}
	if got, want := counter.total(), estimateTokens("ababababababababab"); got != want {
		t.Fatalf("total = %d, want %d", got, want)
	}
}
