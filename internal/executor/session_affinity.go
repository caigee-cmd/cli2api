package executor

import (
	"container/list"
	"context"
	"strings"
	"sync"
	"time"
)

const (
	defaultSessionAffinityTTL      = time.Hour
	defaultSessionAffinityCapacity = 10_000
)

type sessionKeyContextKey struct{}

func WithSessionKey(ctx context.Context, key string) context.Context {
	if strings.TrimSpace(key) == "" {
		return ctx
	}
	return context.WithValue(ctx, sessionKeyContextKey{}, strings.TrimSpace(key))
}

func sessionKeyFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	key, _ := ctx.Value(sessionKeyContextKey{}).(string)
	return strings.TrimSpace(key)
}

type sessionBinding struct {
	key       string
	accountID string
	expiresAt time.Time
}

type SessionAffinityStats struct {
	TTLSeconds       int64  `json:"ttl_seconds"`
	Capacity         int    `json:"capacity"`
	Bindings         int    `json:"bindings"`
	Hits             uint64 `json:"hits"`
	Misses           uint64 `json:"misses"`
	Escapes          uint64 `json:"escapes"`
	Rebindings       uint64 `json:"rebindings"`
	LastEscapeAt     string `json:"last_escape_at,omitempty"`
	LastEscapeReason string `json:"last_escape_reason,omitempty"`
	LastMissReason   string `json:"last_miss_reason,omitempty"`
}

// SessionAffinity is a bounded process-local LRU. Session bindings are a
// routing cache, not durable account state: process restarts intentionally
// clear them.
type SessionAffinity struct {
	mu               sync.Mutex
	ttl              time.Duration
	capacity         int
	entries          map[string]*list.Element
	lru              *list.List
	hits             uint64
	misses           uint64
	escapes          uint64
	rebindings       uint64
	lastEscapeAt     time.Time
	lastEscapeReason string
	lastMissReason   string
}

func NewSessionAffinity(ttl time.Duration, capacity int) *SessionAffinity {
	if ttl <= 0 {
		ttl = defaultSessionAffinityTTL
	}
	if capacity <= 0 {
		capacity = defaultSessionAffinityCapacity
	}
	return &SessionAffinity{
		ttl:      ttl,
		capacity: capacity,
		entries:  make(map[string]*list.Element, capacity),
		lru:      list.New(),
	}
}

func (a *SessionAffinity) Get(key string) (string, bool) {
	if a == nil {
		return "", false
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return "", false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	element, ok := a.entries[key]
	if !ok {
		a.misses++
		a.lastMissReason = "not_found"
		return "", false
	}
	binding := element.Value.(sessionBinding)
	if !binding.expiresAt.After(time.Now()) {
		a.remove(element)
		a.misses++
		a.lastMissReason = "expired"
		return "", false
	}
	a.hits++
	a.lru.MoveToFront(element)
	return binding.accountID, true
}

func (a *SessionAffinity) Bind(key, accountID string) {
	if a == nil {
		return
	}
	key = strings.TrimSpace(key)
	accountID = strings.TrimSpace(accountID)
	if key == "" || accountID == "" {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if element, ok := a.entries[key]; ok {
		previous := element.Value.(sessionBinding)
		if previous.accountID != accountID {
			a.rebindings++
		}
		element.Value = sessionBinding{key: key, accountID: accountID, expiresAt: time.Now().Add(a.ttl)}
		a.lru.MoveToFront(element)
		return
	}
	element := a.lru.PushFront(sessionBinding{key: key, accountID: accountID, expiresAt: time.Now().Add(a.ttl)})
	a.entries[key] = element
	for a.lru.Len() > a.capacity {
		a.remove(a.lru.Back())
	}
}

func (a *SessionAffinity) RecordEscape(reason string) {
	if a == nil {
		return
	}
	a.mu.Lock()
	a.escapes++
	a.lastEscapeAt = time.Now().UTC()
	a.lastEscapeReason = strings.TrimSpace(reason)
	a.mu.Unlock()
}

func (a *SessionAffinity) Stats() SessionAffinityStats {
	if a == nil {
		return SessionAffinityStats{}
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	lastAt := ""
	if !a.lastEscapeAt.IsZero() {
		lastAt = a.lastEscapeAt.Format(time.RFC3339)
	}
	return SessionAffinityStats{
		TTLSeconds: int64(a.ttl / time.Second), Capacity: a.capacity,
		Bindings: a.lru.Len(), Hits: a.hits, Misses: a.misses,
		Escapes: a.escapes, Rebindings: a.rebindings,
		LastEscapeAt: lastAt, LastEscapeReason: a.lastEscapeReason,
		LastMissReason: a.lastMissReason,
	}
}

func (a *SessionAffinity) Forget(key string) {
	if a == nil {
		return
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if element, ok := a.entries[key]; ok {
		a.remove(element)
	}
}

func (a *SessionAffinity) remove(element *list.Element) {
	if element == nil {
		return
	}
	binding := element.Value.(sessionBinding)
	delete(a.entries, binding.key)
	a.lru.Remove(element)
}
