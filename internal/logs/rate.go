package logs

import (
	"sync"
	"time"
)

// rateWindowSeconds is the sliding window RateCounter reports over.
const rateWindowSeconds = 60

// RateCounter counts events over the last 60 seconds with one bucket per
// second. It is in-memory only (resets on restart) and costs O(1) per event,
// so the live strip can poll it without touching SQLite.
type RateCounter struct {
	mu      sync.Mutex
	buckets [rateWindowSeconds]int
	stamps  [rateWindowSeconds]int64
	now     func() time.Time
}

func NewRateCounter() *RateCounter { return &RateCounter{now: time.Now} }

// Add records one event at the current time.
func (c *RateCounter) Add() {
	if c == nil {
		return
	}
	sec := c.now().Unix()
	i := int(sec % rateWindowSeconds)
	c.mu.Lock()
	if c.stamps[i] != sec {
		c.stamps[i] = sec
		c.buckets[i] = 0
	}
	c.buckets[i]++
	c.mu.Unlock()
}

// Count returns events in the trailing 60 seconds (including the current one).
func (c *RateCounter) Count() int {
	if c == nil {
		return 0
	}
	sec := c.now().Unix()
	total := 0
	c.mu.Lock()
	for i := range c.buckets {
		if sec-c.stamps[i] < rateWindowSeconds {
			total += c.buckets[i]
		}
	}
	c.mu.Unlock()
	return total
}
