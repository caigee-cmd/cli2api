package logs

import (
	"sync/atomic"
	"testing"
	"time"
)

func TestRequestRecorderCloseDrainsAcceptedWrites(t *testing.T) {
	r := NewRequestRecorder(persistOnly{})
	var ran atomic.Int32
	for i := 0; i < 100; i++ {
		r.enqueue(func() { ran.Add(1) })
	}
	r.Close()
	if got := ran.Load(); got != 100 {
		t.Fatalf("drained writes = %d, want 100", got)
	}
}

func TestRequestRecorderDropsOnlyAfterQueueGrace(t *testing.T) {
	// No loop: the first write fills the buffer and the second must wait out the
	// short grace period before being counted as dropped.
	r := &RequestRecorder{
		queue: make(chan func(), 1),
		stop:  make(chan struct{}),
		done:  make(chan struct{}),
	}
	r.enqueue(func() {})
	started := time.Now()
	r.enqueue(func() {})
	if elapsed := time.Since(started); elapsed < requestLogQueueWait {
		t.Fatalf("enqueue returned after %v, want at least %v", elapsed, requestLogQueueWait)
	}
	if got := r.DroppedWrites(); got != 1 {
		t.Fatalf("dropped = %d, want 1", got)
	}
	if got := r.QueueDepth(); got != 1 {
		t.Fatalf("queue depth = %d, want 1", got)
	}
}
