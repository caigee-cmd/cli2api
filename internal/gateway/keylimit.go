package gateway

import (
	"net/http"
	"sync"

	"github.com/caigee-cmd/cli2api/internal/auth"
)

// KeyInFlight tracks concurrent chat requests per named API key. The cap is
// read from the authenticated identity on each acquire, so an edit takes
// effect on the next request without restarting. Console traffic is not
// counted. Counts live in memory and reset on process restart, same as
// account in-flight slots.
type KeyInFlight struct {
	mu    sync.Mutex
	count map[string]int
}

func (k *KeyInFlight) acquire(identity auth.Identity) bool {
	if identity.Kind != auth.KindKey || identity.MaxInFlight <= 0 || identity.KeyID == "" {
		return true
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.count == nil {
		k.count = map[string]int{}
	}
	if k.count[identity.KeyID] >= identity.MaxInFlight {
		return false
	}
	k.count[identity.KeyID]++
	return true
}

func (k *KeyInFlight) release(identity auth.Identity) {
	if identity.Kind != auth.KindKey || identity.MaxInFlight <= 0 || identity.KeyID == "" {
		return
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.count == nil {
		return
	}
	if k.count[identity.KeyID] <= 1 {
		delete(k.count, identity.KeyID)
		return
	}
	k.count[identity.KeyID]--
}

// reserveKeySlot takes one concurrency slot for a named key. A false result
// is a 429 the caller should write and return from. The returned function
// releases the slot and is safe to call when nothing was reserved.
func (h *Handler) reserveKeySlot(r *http.Request) (func(), bool) {
	identity := h.requestIdentity(r)
	if h == nil || h.KeyLimits == nil || !h.KeyLimits.acquire(identity) {
		if h == nil || h.KeyLimits == nil {
			return func() {}, true
		}
		return func() {}, false
	}
	var once sync.Once
	return func() { once.Do(func() { h.KeyLimits.release(identity) }) }, true
}

func writeKeyConcurrencyLimited(w http.ResponseWriter) {
	w.Header().Set("Retry-After", "1")
	writeErr(w, http.StatusTooManyRequests, "rate_limit", "API key concurrency limit reached")
}

func writeAnthropicKeyConcurrencyLimited(w http.ResponseWriter) {
	w.Header().Set("Retry-After", "1")
	writeAnthropicError(w, http.StatusTooManyRequests, "rate_limit_error", "API key concurrency limit reached")
}
