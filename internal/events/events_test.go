package events

import (
	"testing"
	"time"
)

func TestBusPublishAssignsMonotonicSeqAndTime(t *testing.T) {
	bus := NewBus()
	bus.Publish(TypeAccounts, "acc-1", "", nil)
	bus.Publish(TypeQuota, "acc-1", "", QuotaState{AccountID: "acc-1"})
	stream, cancel := bus.Subscribe(0)
	defer cancel()
	first := <-stream
	second := <-stream
	if first.Seq != 1 || second.Seq != 2 {
		t.Fatalf("seq = %d,%d", first.Seq, second.Seq)
	}
	if first.Type != TypeAccounts || second.Type != TypeQuota {
		t.Fatalf("types = %s,%s", first.Type, second.Type)
	}
	if first.Time == "" {
		t.Fatal("event time must be the real emit instant")
	}
	if _, err := time.Parse(time.RFC3339Nano, first.Time); err != nil {
		t.Fatalf("event time not RFC3339Nano: %v", err)
	}
}

func TestBusReplaysOnlyEventsNewerThanLastSeq(t *testing.T) {
	bus := NewBus()
	for i := 0; i < 3; i++ {
		bus.Publish(TypeQuota, "acc", "", nil)
	}
	stream, cancel := bus.Subscribe(2)
	defer cancel()
	replayed := <-stream
	if replayed.Seq != 3 {
		t.Fatalf("replayed seq = %d want 3", replayed.Seq)
	}
	// Live events continue after the replay.
	bus.Publish(TypeCheckin, "acc", "", nil)
	live := <-stream
	if live.Seq != 4 || live.Type != TypeCheckin {
		t.Fatalf("live = %+v", live)
	}
}

func TestBusFansOutToEverySubscriber(t *testing.T) {
	bus := NewBus()
	a, cancelA := bus.Subscribe(0)
	defer cancelA()
	b, cancelB := bus.Subscribe(0)
	defer cancelB()
	bus.Publish(TypeAccount, "acc", "", AccountState{AccountID: "acc", Ready: true})
	if got := <-a; got.AccountID != "acc" {
		t.Fatalf("a = %+v", got)
	}
	if got := <-b; got.AccountID != "acc" {
		t.Fatalf("b = %+v", got)
	}
}

func TestBusClosesSlowConsumerWithoutBlockingPublisher(t *testing.T) {
	bus := NewBus()
	slow, cancel := bus.Subscribe(0)
	defer cancel()
	// Fill the subscriber buffer plus the ring, so the slow consumer overflows
	// and is closed while publishing stays non-blocking.
	for i := 0; i < subBuffer+ringSize+10; i++ {
		bus.Publish(TypeQuota, "acc", "", nil)
	}
	deadline := time.After(200 * time.Millisecond)
	for {
		select {
		case _, ok := <-slow:
			if !ok {
				return // closed as designed
			}
		case <-deadline:
			t.Fatal("slow consumer was not closed")
		}
	}
}

func TestBusCancelIsIdempotent(t *testing.T) {
	bus := NewBus()
	stream, cancel := bus.Subscribe(0)
	cancel()
	cancel() // must not panic or double-close
	if _, ok := <-stream; ok {
		t.Fatal("canceled subscription must be closed")
	}
}

func TestNilBusIsSafe(t *testing.T) {
	var bus *Bus
	bus.Publish(TypeQuota, "acc", "", nil)
	stream, cancel := bus.Subscribe(0)
	cancel()
	if _, ok := <-stream; ok {
		t.Fatal("nil bus subscription must be closed")
	}
}
