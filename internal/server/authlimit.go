package server

import (
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	// Only failed authentication is counted. Ten bad keys in a minute from one
	// client is already a scanner, not a person mistyping a console key.
	authFailureLimit      = 10
	authFailureWindow     = time.Minute
	authFailureBlock      = time.Minute
	authFailureMaxEntries = 4096
)

type authFailureEntry struct {
	windowStart  time.Time
	failures     int
	blockedUntil time.Time
}

// authFailureLimiter is deliberately in-memory: it protects the SQLite lookup
// and the 401 path, not a durable security audit log. A restart clearing the
// counters is acceptable; an unbounded map is not.
type authFailureLimiter struct {
	mu      sync.Mutex
	now     func() time.Time
	entries map[string]authFailureEntry
}

func newAuthFailureLimiter(now func() time.Time) *authFailureLimiter {
	if now == nil {
		now = time.Now
	}
	return &authFailureLimiter{now: now, entries: make(map[string]authFailureEntry)}
}

// blocked reports whether the client is already in a failure cooldown and how
// long remains. It runs before any key lookup.
func (l *authFailureLimiter) blocked(client string) (bool, time.Duration) {
	if l == nil {
		return false, 0
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	e, ok := l.entries[client]
	if !ok {
		return false, 0
	}
	now := l.now()
	if !e.blockedUntil.IsZero() && e.blockedUntil.After(now) {
		return true, e.blockedUntil.Sub(now)
	}
	if e.blockedUntil.IsZero() && now.Sub(e.windowStart) >= authFailureWindow {
		delete(l.entries, client)
	}
	return false, 0
}

// failure records a rejected key. The request that reaches the limit is itself
// answered with 429 so a scanner cannot keep probing at full speed.
func (l *authFailureLimiter) failure(client string) (bool, time.Duration) {
	if l == nil {
		return false, 0
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	e, ok := l.entries[client]
	if !ok || now.Sub(e.windowStart) >= authFailureWindow || (!e.blockedUntil.IsZero() && !now.Before(e.blockedUntil)) {
		e = authFailureEntry{windowStart: now}
	}
	e.failures++
	if e.failures >= authFailureLimit {
		e.blockedUntil = now.Add(authFailureBlock)
	}
	l.entries[client] = e
	l.pruneLocked(now)
	if !e.blockedUntil.IsZero() && e.blockedUntil.After(now) {
		return true, e.blockedUntil.Sub(now)
	}
	return false, 0
}

func (l *authFailureLimiter) success(client string) {
	if l == nil {
		return
	}
	l.mu.Lock()
	delete(l.entries, client)
	l.mu.Unlock()
}

func (l *authFailureLimiter) pruneLocked(now time.Time) {
	if len(l.entries) < authFailureMaxEntries/4 {
		return
	}
	for client, e := range l.entries {
		expiredWindow := now.Sub(e.windowStart) >= authFailureWindow
		expiredBlock := e.blockedUntil.IsZero() || !now.Before(e.blockedUntil)
		if expiredWindow && expiredBlock {
			delete(l.entries, client)
		}
	}
	if len(l.entries) > authFailureMaxEntries {
		// A spoofed-IP flood must not turn into unbounded memory. Clearing is
		// safe: it only forfeits counters, never authentication state.
		l.entries = make(map[string]authFailureEntry)
	}
}

func clientIP(r *http.Request) string {
	if r == nil {
		return "unknown"
	}
	addr := strings.TrimSpace(r.RemoteAddr)
	if host, _, err := net.SplitHostPort(addr); err == nil {
		return host
	}
	if addr == "" {
		return "unknown"
	}
	return addr
}

// credentialsPresented separates an unauthenticated page load from a wrong
// key guess. The login screen may refresh data without a key; that must not
// consume the failure budget meant for scanners.
func credentialsPresented(r *http.Request) bool {
	if r == nil {
		return false
	}
	if secret := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")); secret != "" {
		return true
	}
	return strings.TrimSpace(r.Header.Get("x-api-key")) != ""
}

func retryAfterSeconds(d time.Duration) int {
	seconds := int((d + time.Second - 1) / time.Second)
	if seconds < 1 {
		return 1
	}
	return seconds
}

func writeAuthBlocked(w http.ResponseWriter, retry time.Duration) {
	w.Header().Set("Retry-After", strconv.Itoa(retryAfterSeconds(retry)))
	writeErr(w, http.StatusTooManyRequests, "too_many_auth_failures", "Too many invalid API key attempts")
}
