package logs

import (
	"testing"
	"time"

	"github.com/caigee-cmd/cli2api/internal/accounts"
)

func TestRateCounterSlidingWindow(t *testing.T) {
	base := time.Unix(1_700_000_000, 0)
	cur := base
	c := NewRateCounter()
	c.now = func() time.Time { return cur }
	c.Add()
	c.Add()
	cur = base.Add(30 * time.Second)
	c.Add()
	if got := c.Count(); got != 3 {
		t.Fatalf("count=%d want 3", got)
	}
	cur = base.Add(60 * time.Second)
	if got := c.Count(); got != 1 {
		t.Fatalf("after 60s count=%d want 1", got)
	}
	// Same slot reused after a full wrap must reset.
	cur = base.Add(120 * time.Second)
	c.Add()
	if got := c.Count(); got != 1 {
		t.Fatalf("after wrap count=%d want 1", got)
	}
	cur = base.Add(10 * time.Minute)
	if got := c.Count(); got != 0 {
		t.Fatalf("idle count=%d want 0", got)
	}
}

func TestRateCounterNilSafe(t *testing.T) {
	var c *RateCounter
	c.Add()
	if c.Count() != 0 {
		t.Fatal("nil counter should report 0")
	}
}

func TestRecorderStartCountsRate(t *testing.T) {
	r := &RequestRecorder{closed: true}
	r.Start(accounts.RequestLog{})
	r.Start(accounts.RequestLog{})
	if got := r.RecentRequests(); got != 2 {
		t.Fatalf("recent=%d want 2", got)
	}
	var nilRec *RequestRecorder
	if nilRec.RecentRequests() != 0 {
		t.Fatal("nil recorder should report 0")
	}
}
