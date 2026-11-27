// Package events carries real console state changes from the runtime to the
// console SSE channel. Every event reflects an actual backend mutation (quota
// fetch, check-in record, cooldown/health merge, bulk-op progress); nothing is
// synthesized. The package is a leaf utility: runtime publishes, control
// exposes subscription, console streams — no boundary is crossed.
package events

import (
	"sync"
	"time"

	"github.com/caigee-cmd/cli2api/internal/accounts"
)

// Event is one real state change. Seq is monotonic per bus and doubles as the
// SSE id; Time is the actual emit instant.
type Event struct {
	Seq       uint64 `json:"seq"`
	Type      string `json:"type"`
	Time      string `json:"time"`
	AccountID string `json:"account_id,omitempty"`
	OpID      string `json:"op_id,omitempty"`
	Data      any    `json:"data,omitempty"`
}

// Event types.
const (
	TypeAccount  = "account"  // one account's real routing/health state changed
	TypeAccounts = "accounts" // structural change: create/enable/disable/remove
	TypeQuota    = "quota"    // a real upstream quota snapshot was persisted
	TypeCheckin  = "checkin"  // a real check-in outcome was recorded
	TypeRefresh  = "refresh"  // one account's real refresh outcome
	TypeBulk     = "bulk"     // bulk-op progress (start / per-account / done)
)

// AccountState is the real routing projection of one account after a health
// or cooldown merge.
type AccountState struct {
	AccountID     string `json:"account_id"`
	RuntimeState  string `json:"runtime_state,omitempty"`
	Ready         bool   `json:"ready"`
	CooldownUntil string `json:"cooldown_until,omitempty"`
	LastError     string `json:"last_error,omitempty"`
	LastKind      string `json:"last_kind,omitempty"`
}

// QuotaState carries the exact snapshot persisted from upstream.
type QuotaState struct {
	AccountID string                  `json:"account_id"`
	Quota     *accounts.QuotaSnapshot `json:"quota"`
}

// CheckinState carries the verbatim provider check-in outcome.
type CheckinState struct {
	AccountID     string  `json:"account_id"`
	Status        string  `json:"status"`
	Message       string  `json:"message,omitempty"`
	RewardCredits float64 `json:"reward_credits,omitempty"`
	At            string  `json:"at"`
}

// BulkProgress reports one bulk operation's real per-account progress.
// Phase is "start", "account", or "done"; Result carries the per-account
// outcome (accounts.CheckinAllResult / accounts.AccountRefreshResult) on
// "account" and "done" frames.
type BulkProgress struct {
	OpID      string `json:"op_id"`
	Kind      string `json:"kind"`
	Phase     string `json:"phase"`
	Total     int    `json:"total"`
	Done      int    `json:"done"`
	AccountID string `json:"account_id,omitempty"`
	Result    any    `json:"result,omitempty"`
}

const (
	ringSize  = 1024
	subBuffer = ringSize + 64
)

// Bus is a bounded in-memory fan-out. Publishers never block: a subscriber
// that cannot keep up is closed, and its SSE client reconnects with
// Last-Event-ID to replay from the ring.
type Bus struct {
	mu   sync.Mutex
	seq  uint64
	ring []Event
	subs map[chan Event]struct{}
}

func NewBus() *Bus {
	return &Bus{ring: make([]Event, 0, ringSize), subs: map[chan Event]struct{}{}}
}

// Publish records one real event and fans it out to every subscriber.
func (b *Bus) Publish(eventType, accountID, opID string, data any) {
	if b == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.seq++
	event := Event{
		Seq:       b.seq,
		Type:      eventType,
		Time:      time.Now().UTC().Format(time.RFC3339Nano),
		AccountID: accountID,
		OpID:      opID,
		Data:      data,
	}
	if len(b.ring) == ringSize {
		copy(b.ring, b.ring[1:])
		b.ring[len(b.ring)-1] = event
	} else {
		b.ring = append(b.ring, event)
	}
	for ch := range b.subs {
		select {
		case ch <- event:
		default:
			// Slow consumer: drop it. The client reconnects and replays.
			delete(b.subs, ch)
			close(ch)
		}
	}
}

// Subscribe replays ring events newer than lastSeq, then streams live events.
// The returned cancel deregisters and closes the channel exactly once.
func (b *Bus) Subscribe(lastSeq uint64) (<-chan Event, func()) {
	ch := make(chan Event, subBuffer)
	if b == nil {
		close(ch)
		return ch, func() {}
	}
	b.mu.Lock()
	b.subs[ch] = struct{}{}
	// A seq larger than the current high-water mark can only be a client from a
	// previous process (or a malformed ID). Replaying the whole ring is cheaper
	// than silently skipping every event until the numbers catch up again.
	if lastSeq > b.seq {
		lastSeq = 0
	}
	for _, event := range b.ring {
		if event.Seq > lastSeq {
			// Buffer is sized to hold the whole ring, so replay never drops.
			ch <- event
		}
	}
	b.mu.Unlock()
	var once sync.Once
	cancel := func() {
		once.Do(func() {
			b.mu.Lock()
			if _, ok := b.subs[ch]; ok {
				delete(b.subs, ch)
				close(ch)
			}
			b.mu.Unlock()
		})
	}
	return ch, cancel
}
