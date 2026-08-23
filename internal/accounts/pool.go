package accounts

import (
	"sync"
	"time"
)

type Item struct {
	ID        string
	URL       string
	DownUntil time.Time
	LastError string
	LastKind  string
	Ready     *bool
	Hot       *bool
	InFlight  int
	Restarts  int
}

type Pool struct {
	mu       sync.Mutex
	items    []Item
	next     int
	observer func(Item)
}

func NewPool(urls []string, ids []string) *Pool {
	items := make([]Item, 0, len(urls))
	for i, url := range urls {
		id := "default"
		if i < len(ids) && ids[i] != "" {
			id = ids[i]
		} else if len(urls) > 1 {
			id = "worker-" + itoa(i+1)
		}
		items = append(items, Item{ID: id, URL: url})
	}
	return &Pool{items: items}
}

func (p *Pool) Len() int {
	if p == nil {
		return 0
	}
	return len(p.items)
}

func (p *Pool) First() (Item, bool) {
	if p == nil || len(p.items) == 0 {
		return Item{}, false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.items[0], true
}

func (p *Pool) ByID(id string) (Item, bool) {
	if p == nil {
		return Item{}, false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, item := range p.items {
		if item.ID == id {
			return item, true
		}
	}
	return Item{}, false
}

func (p *Pool) Pick(prefer string, excluded map[string]struct{}) (Item, bool) {
	if p == nil || len(p.items) == 0 {
		return Item{}, false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	now := time.Now()
	if prefer != "" {
		for _, item := range p.items {
			if _, skip := excluded[item.ID]; item.ID == prefer && !itemDown(item, now) && !skip {
				return item, true
			}
		}
	}
	n := len(p.items)
	for i := 0; i < n; i++ {
		idx := (p.next + i) % n
		item := p.items[idx]
		if _, skip := excluded[item.ID]; skip || itemDown(item, now) {
			continue
		}
		p.next = (idx + 1) % n
		return item, true
	}
	var best Item
	found := false
	for _, item := range p.items {
		if _, skip := excluded[item.ID]; skip {
			continue
		}
		if !found || item.DownUntil.Before(best.DownUntil) {
			best = item
			found = true
		}
	}
	return best, found
}

func (p *Pool) MarkDown(id string, d time.Duration, err string) {
	p.MarkClassified(id, Classified{Kind: KindUnavailable, Cooldown: d, Message: err})
}

func (p *Pool) MarkClassified(id string, c Classified) {
	if p == nil || id == "" {
		return
	}
	var changed *Item
	p.mu.Lock()
	for i := range p.items {
		if p.items[i].ID != id {
			continue
		}
		p.items[i].LastError = c.Message
		p.items[i].LastKind = c.Kind
		if c.Cooldown > 0 && (c.Failover || c.Kind != KindQuota) {
			p.items[i].DownUntil = time.Now().Add(c.Cooldown)
		}
		copy := p.items[i]
		changed = &copy
		break
	}
	observer := p.observer
	p.mu.Unlock()
	if changed != nil && observer != nil {
		observer(*changed)
	}
}
func (p *Pool) MarkOK(id string) {
	if p == nil || id == "" {
		return
	}
	var changed *Item
	p.mu.Lock()
	for i := range p.items {
		if p.items[i].ID == id {
			p.items[i].DownUntil = time.Time{}
			p.items[i].LastError = ""
			p.items[i].LastKind = ""
			copy := p.items[i]
			changed = &copy
			break
		}
	}
	observer := p.observer
	p.mu.Unlock()
	if changed != nil && observer != nil {
		observer(*changed)
	}
}
func (p *Pool) MergeHealth(id string, ready, hot bool, inFlight, restarts int, lastError string) {
	if p == nil || id == "" {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	for i := range p.items {
		if p.items[i].ID != id {
			continue
		}
		r, h := ready, hot
		p.items[i].Ready = &r
		p.items[i].Hot = &h
		p.items[i].InFlight = inFlight
		p.items[i].Restarts = restarts
		if lastError != "" {
			p.items[i].LastError = lastError
		}
		return
	}
}

func (p *Pool) Snapshot() []map[string]any {
	if p == nil {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	now := time.Now()
	out := make([]map[string]any, 0, len(p.items))
	for _, item := range p.items {
		ready := !itemDown(item, now)
		if item.Ready != nil {
			ready = *item.Ready && ready
		}
		hot := false
		if item.Hot != nil {
			hot = *item.Hot
		}
		out = append(out, map[string]any{
			"id":         item.ID,
			"url":        item.URL,
			"ready":      ready,
			"hot":        hot,
			"in_flight":  item.InFlight,
			"restarts":   item.Restarts,
			"kind":       item.LastKind,
			"down_until": nullableTime(item.DownUntil, now),
			"last_error": item.LastError,
		})
	}
	return out
}

func itemDown(item Item, now time.Time) bool {
	return !item.DownUntil.IsZero() && now.Before(item.DownUntil)
}

func nullableTime(t time.Time, now time.Time) any {
	if t.IsZero() || !now.Before(t) {
		return nil
	}
	return t.UTC().Format(time.RFC3339)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [12]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

func (p *Pool) Upsert(item Item) {
	if p == nil || item.ID == "" || item.URL == "" {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	for i := range p.items {
		if p.items[i].ID == item.ID {
			item.DownUntil = p.items[i].DownUntil
			item.LastError = p.items[i].LastError
			item.LastKind = p.items[i].LastKind
			p.items[i] = item
			return
		}
	}
	p.items = append(p.items, item)
}

func (p *Pool) Remove(id string) {
	if p == nil || id == "" {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	for i := range p.items {
		if p.items[i].ID != id {
			continue
		}
		p.items = append(p.items[:i], p.items[i+1:]...)
		if len(p.items) == 0 {
			p.next = 0
		} else if p.next >= len(p.items) {
			p.next %= len(p.items)
		}
		return
	}
}

func (p *Pool) Items() []Item {
	if p == nil {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	items := make([]Item, len(p.items))
	copy(items, p.items)
	return items
}

func (p *Pool) SetObserver(observer func(Item)) {
	if p == nil {
		return
	}
	p.mu.Lock()
	p.observer = observer
	p.mu.Unlock()
}
