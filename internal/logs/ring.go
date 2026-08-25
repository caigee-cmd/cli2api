package logs

import (
	"bytes"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type Entry struct {
	ID        uint64    `json:"id"`
	Time      time.Time `json:"time"`
	Level     string    `json:"level"`
	AccountID string    `json:"account_id,omitempty"`
	Source    string    `json:"source,omitempty"`
	Message   string    `json:"message"`
}

type Ring struct {
	mu      sync.RWMutex
	entries []Entry
	size    int
	nextID  atomic.Uint64
}

var (
	accountPrefixRe = regexp.MustCompile(`(?i)\[account=([^\]]+)\]`)
	bearerRe        = regexp.MustCompile(`(?i)(Bearer\s+)[A-Za-z0-9\-._~+/]+=*`)
	apiKeyAssignRe  = regexp.MustCompile(`(?i)((?:PROXY_API_KEY|api[_-]?key|token)\s*[=:]\s*)(\S+)`)
	initializedKeyRe = regexp.MustCompile(`(?i)(initialized API key(?: and stored it in SQLite)?:\s*)(\S+)`)
)

func NewRing(size int) *Ring {
	if size <= 0 {
		size = 2000
	}
	return &Ring{
		entries: make([]Entry, 0, size),
		size:    size,
	}
}

func (r *Ring) Write(p []byte) (int, error) {
	if r == nil {
		return len(p), nil
	}
	text := string(p)
	if text == "" {
		return 0, nil
	}
	for _, line := range splitLines(text) {
		r.appendLine(line)
	}
	return len(p), nil
}

func (r *Ring) Append(message string) Entry {
	return r.appendLine(message)
}

func (r *Ring) Snapshot(afterID uint64, limit int, level, query string) []Entry {
	if r == nil {
		return nil
	}
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	level = strings.ToLower(strings.TrimSpace(level))
	query = strings.TrimSpace(query)

	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Entry, 0, min(limit, len(r.entries)))
	for _, entry := range r.entries {
		if entry.ID <= afterID {
			continue
		}
		if level != "" && level != "all" && entry.Level != level {
			continue
		}
		if query != "" && !strings.Contains(strings.ToLower(entry.Message), strings.ToLower(query)) &&
			!strings.Contains(strings.ToLower(entry.AccountID), strings.ToLower(query)) {
			continue
		}
		out = append(out, entry)
		if len(out) >= limit {
			break
		}
	}
	return out
}

func (r *Ring) Latest(limit int) []Entry {
	return r.Snapshot(0, limit, "", "")
}

func (r *Ring) appendLine(line string) Entry {
	line = strings.TrimRight(line, "\r\n")
	if strings.TrimSpace(line) == "" {
		return Entry{}
	}
	accountID := ""
	if match := accountPrefixRe.FindStringSubmatch(line); len(match) == 2 {
		accountID = strings.TrimSpace(match[1])
	}
	entry := Entry{
		ID:        r.nextID.Add(1),
		Time:      time.Now().UTC(),
		Level:     detectLevel(line),
		AccountID: accountID,
		Source:    detectSource(line),
		Message:   redactSecrets(line),
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.entries) >= r.size {
		copy(r.entries, r.entries[1:])
		r.entries[len(r.entries)-1] = entry
		r.entries = r.entries[:r.size]
	} else {
		r.entries = append(r.entries, entry)
	}
	return entry
}

type PrefixWriter struct {
	Prefix string
	Next   interface{ Write([]byte) (int, error) }
	buf    []byte
}

func (w *PrefixWriter) Write(p []byte) (int, error) {
	if w == nil || w.Next == nil {
		return len(p), nil
	}
	w.buf = append(w.buf, p...)
	for {
		idx := bytes.IndexByte(w.buf, '\n')
		if idx < 0 {
			break
		}
		line := append([]byte(nil), w.buf[:idx+1]...)
		w.buf = w.buf[idx+1:]
		prefixed := append([]byte(w.Prefix), line...)
		if _, err := w.Next.Write(prefixed); err != nil {
			return len(p), err
		}
	}
	return len(p), nil
}

func splitLines(text string) []string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	parts := strings.Split(text, "\n")
	out := make([]string, 0, len(parts))
	for i, part := range parts {
		if i == len(parts)-1 && part == "" {
			continue
		}
		out = append(out, part)
	}
	return out
}

func detectLevel(line string) string {
	lower := strings.ToLower(line)
	switch {
	case strings.Contains(lower, " error") || strings.Contains(lower, "[error]") || strings.Contains(lower, "failed") || strings.Contains(lower, "panic"):
		return "error"
	case strings.Contains(lower, " warn") || strings.Contains(lower, "[warn]") || strings.Contains(lower, "warning"):
		return "warn"
	default:
		return "info"
	}
}

func detectSource(line string) string {
	switch {
	case strings.Contains(line, "[daemon]"):
		return "daemon"
	case strings.Contains(line, "[sse]"):
		return "sse"
	case strings.Contains(line, "[security]"):
		return "security"
	default:
		return "proxy"
	}
}

func redactSecrets(line string) string {
	line = bearerRe.ReplaceAllString(line, "${1}***")
	line = apiKeyAssignRe.ReplaceAllString(line, "${1}***")
	line = initializedKeyRe.ReplaceAllString(line, "${1}***")
	return line
}
