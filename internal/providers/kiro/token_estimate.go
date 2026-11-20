package kiro

import "math"

// Kiro never states output tokens on generateAssistantResponse, so they are
// estimated from the text the stream produced. The weights follow Kiro-Go
// (proxy/token_estimator.go estimateApproxTokens): ordinary ASCII about 4.5
// characters per token, digits 2, punctuation 1.5, and non-ASCII (CJK, emoji)
// 1.5. It is a usage magnitude for the request log, not a billing figure.

// tokenCounter accumulates character classes across stream fragments and
// rounds once at the end, so a reply split into many chunks is not rounded up
// once per chunk.
type tokenCounter struct {
	runes, ascii, digits, symbols, other int
}

func (c *tokenCounter) add(text string) {
	for _, r := range text {
		c.runes++
		switch {
		case r >= 0x80:
			c.other++
		case r >= '0' && r <= '9':
			c.digits++
		case (r >= '!' && r <= '/') || (r >= ':' && r <= '@') || (r >= '[' && r <= '`') || (r >= '{' && r <= '~'):
			c.symbols++
		default:
			c.ascii++
		}
	}
}

func (c *tokenCounter) total() int {
	if c.runes == 0 {
		return 0
	}
	if c.runes < 5 {
		return max(1, int(math.Ceil(float64(c.runes)/3)))
	}
	estimated := int(math.Ceil(float64(c.ascii)/4.5 + float64(c.digits)/2 + float64(c.symbols)/1.5 + float64(c.other)/1.5))
	return max(1, estimated)
}

// estimateTokens is the same estimate for one piece of text.
func estimateTokens(text string) int {
	var counter tokenCounter
	counter.add(text)
	return counter.total()
}
