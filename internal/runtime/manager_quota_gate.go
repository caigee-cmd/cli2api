package runtime

import (
	"context"
	"sync"
)

// quotaFetchGate serializes quota samples for one account. Every entry point
// (boot probe, manual/console refresh, maintenance sweep, check-in, and the
// FEFO after-use refresher) funnels through fetchProviderQuota, so an older
// sample that started first cannot be persisted after a newer one.
type quotaFetchGate struct {
	ch chan struct{}
	// refs counts the holder plus all waiters. The map entry is dropped when
	// the last participant leaves, keeping the table bounded by active probes.
	refs int
}

// acquireQuotaFetch blocks until this account's gate is free or ctx ends.
// The returned release must be called exactly once after the fetch completes.
func (m *Manager) acquireQuotaFetch(ctx context.Context, accountID string) (func(), bool) {
	m.quotaFetchMu.Lock()
	if m.quotaFetchGates == nil {
		m.quotaFetchGates = make(map[string]*quotaFetchGate)
	}
	gate := m.quotaFetchGates[accountID]
	if gate == nil {
		gate = &quotaFetchGate{ch: make(chan struct{}, 1)}
		m.quotaFetchGates[accountID] = gate
	}
	gate.refs++
	m.quotaFetchMu.Unlock()

	select {
	case gate.ch <- struct{}{}:
		var once sync.Once
		return func() {
			once.Do(func() {
				<-gate.ch
				m.releaseQuotaFetchRef(accountID, gate)
			})
		}, true
	case <-ctx.Done():
		m.releaseQuotaFetchRef(accountID, gate)
		return func() {}, false
	}
}

func (m *Manager) releaseQuotaFetchRef(accountID string, gate *quotaFetchGate) {
	m.quotaFetchMu.Lock()
	gate.refs--
	if gate.refs == 0 {
		if current, ok := m.quotaFetchGates[accountID]; ok && current == gate {
			delete(m.quotaFetchGates, accountID)
		}
	}
	m.quotaFetchMu.Unlock()
}
