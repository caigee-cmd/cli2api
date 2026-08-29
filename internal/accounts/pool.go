package accounts

import (
	"strings"
	"sync"
	"time"
)

// QuotaSnapshot is the display-only quota state for one account.
// A zero value means "unknown"; it never influences routing or cooldown.
type QuotaSnapshot struct {
	Used       float64 `json:"used"`
	Total      float64 `json:"total"`
	Remaining  float64 `json:"remaining"`
	Percentage float64 `json:"percentage"`
	Unit       string  `json:"unit"`
	Exceeded   bool    `json:"exceeded"`
	HasAddOn   bool    `json:"has_add_on"`
	AddOnUsed  float64 `json:"add_on_used"`
	AddOnTotal float64 `json:"add_on_total"`
	AddOnUnit  string  `json:"add_on_unit"`
	FetchedAt  string  `json:"fetched_at"`
}

type Item struct {
	ID        string
	URL       string
	Provider  string
	Region    string
	Runtime   string
	DownUntil time.Time
	LastError string
	LastKind  string
	Ready     *bool
	Hot       *bool
	InFlight  int
	Restarts  int
	Quota     *QuotaSnapshot
	// Models is the last successful per-account catalog snapshot. A nil
	// slice means unknown (fail open); a non-nil slice, including empty,
	// is used to filter PublicModel. Entries keep the provider-native
	// spelling so Trae config_name case is preserved.
	Models   []string
	ModelsAt time.Time
	// DropSystemPrompt mirrors the stored account flag so the executor can
	// sanitize requests per account without a store lookup per chat.
	DropSystemPrompt bool
}

// RouteQuery selects candidates for one public model request. Empty fields are
// not filtered; excluded account IDs are honored before anything else.
type RouteQuery struct {
	PublicModel    string
	PreferAccount  string
	ProviderFilter string
	RegionFilter   string
	Excluded       map[string]struct{}
}

func itemRegion(item Item) string {
	region := strings.ToLower(strings.TrimSpace(item.Region))
	if region == "" {
		return "global"
	}
	return region
}

func CanonicalModelID(model string) string {
	key := strings.ToLower(strings.TrimSpace(model))
	key = strings.NewReplacer("_", "-", " ", "-").Replace(key)
	if key == "" {
		return "auto"
	}
	return key
}

func routeModel(model string) string {
	id := CanonicalModelID(model)
	if id == "" || id == "auto" {
		return ""
	}
	return id
}

func itemHasModel(item Item, publicModel string) bool {
	want := routeModel(publicModel)
	if want == "" || item.Models == nil {
		return true
	}
	for _, model := range item.Models {
		if CanonicalModelID(model) == want {
			return true
		}
	}
	return false
}

// NativeModelID returns the provider-native catalog spelling for a public
// model. Trae config_name is case-sensitive; routing matches on the
// canonical form, but the upstream request must keep the original ID.
func NativeModelID(item Item, publicModel string) string {
	want := routeModel(publicModel)
	if want == "" {
		return strings.TrimSpace(publicModel)
	}
	for _, model := range item.Models {
		if CanonicalModelID(model) == want {
			return model
		}
	}
	return strings.TrimSpace(publicModel)
}

func routeMatches(item Item, q RouteQuery) bool {
	if _, skip := q.Excluded[item.ID]; skip {
		return false
	}
	if q.ProviderFilter != "" && item.Provider != q.ProviderFilter {
		return false
	}
	if q.RegionFilter != "" && itemRegion(item) != strings.ToLower(strings.TrimSpace(q.RegionFilter)) {
		return false
	}
	if !itemHasModel(item, q.PublicModel) {
		return false
	}
	return true
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
		items = append(items, Item{
			ID: id, URL: url, Provider: "qoder", Runtime: "child_process",
		})
	}
	return &Pool{items: items}
}

func (p *Pool) Len() int {
	return p.LenRoute(RouteQuery{})
}

// LenRoute counts the candidate set for one route query, so retry attempts
// match the current pool instead of every registered account.
func (p *Pool) LenRoute(q RouteQuery) int {
	if p == nil {
		return 0
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	count := 0
	for _, item := range p.items {
		if routeMatches(item, q) {
			count++
		}
	}
	return count
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
	return p.PickRoute(RouteQuery{PreferAccount: prefer, Excluded: excluded})
}

// PickRoute picks one item. Provider filtering, cooldown, pin, exclusion, and
// round-robin are applied in that order. The fallback path returns the item
// with the earliest cooldown so callers can surface a classified error.
func (p *Pool) PickRoute(q RouteQuery) (Item, bool) {
	if p == nil || len(p.items) == 0 {
		return Item{}, false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	now := time.Now()
	eligible := make([]int, 0, len(p.items))
	for i, item := range p.items {
		if routeMatches(item, q) {
			eligible = append(eligible, i)
		}
	}
	if q.PreferAccount != "" {
		var pinned *Item
		for i := range p.items {
			if p.items[i].ID != q.PreferAccount {
				continue
			}
			copy := p.items[i]
			pinned = &copy
			break
		}
		if pinned != nil {
			if _, skip := q.Excluded[pinned.ID]; !skip {
				if routeModel(q.PublicModel) != "" && pinned.Models != nil && !itemHasModel(*pinned, q.PublicModel) {
					return Item{}, false
				}
				if routeMatches(*pinned, q) && !itemDown(*pinned, now) {
					return *pinned, true
				}
			}
		}
		// Historical Qoder clients may pin an unknown account label; fall back
		// to normal scheduling like the pre-provider pool did. A cooling pin
		// sticky-escapes among eligible accounts that still serve the model.
	}
	n := len(p.items)
	for i := 0; i < n; i++ {
		idx := (p.next + i) % n
		item := p.items[idx]
		if !routeMatches(item, q) || itemDown(item, now) {
			continue
		}
		p.next = (idx + 1) % n
		return item, true
	}
	var best Item
	found := false
	for _, i := range eligible {
		item := p.items[i]
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
	if p == nil || id == "" || c.Kind == KindModelNotAvailable {
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

// SetDropSystemPrompt updates only the request-sanitization flag on a live
// item. Routing and runtime state stay untouched so the change applies to the
// next request without disturbing cooldowns or health.
func (p *Pool) SetDropSystemPrompt(id string, drop bool) {
	if p == nil || id == "" {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	for i := range p.items {
		if p.items[i].ID == id {
			p.items[i].DropSystemPrompt = drop
			return
		}
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
		p.items[i].LastError = lastError
		if lastError == "" {
			p.items[i].LastKind = ""
		}
		return
	}
}

func (p *Pool) MergeModels(id string, models []string) {
	if p == nil || id == "" {
		return
	}
	copied := make([]string, 0, len(models))
	seen := map[string]struct{}{}
	for _, model := range models {
		native := strings.TrimSpace(model)
		canonical := CanonicalModelID(native)
		if canonical == "" || canonical == "auto" || native == "" {
			continue
		}
		if _, ok := seen[canonical]; ok {
			continue
		}
		seen[canonical] = struct{}{}
		copied = append(copied, native)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	for i := range p.items {
		if p.items[i].ID == id {
			p.items[i].Models = copied
			p.items[i].ModelsAt = time.Now()
			return
		}
	}
}

// RemoveModel drops one public ID from a cached catalog so a stale snapshot
// cannot keep sending traffic to an account that no longer serves the model.
func (p *Pool) RemoveModel(id, model string) {
	want := routeModel(model)
	if p == nil || id == "" || want == "" {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	for i := range p.items {
		if p.items[i].ID != id || p.items[i].Models == nil {
			continue
		}
		next := make([]string, 0, len(p.items[i].Models))
		for _, existing := range p.items[i].Models {
			if CanonicalModelID(existing) != want {
				next = append(next, existing)
			}
		}
		p.items[i].Models = next
		return
	}
}

func (p *Pool) MergeQuota(id string, quota *QuotaSnapshot) {
	if p == nil || id == "" {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	for i := range p.items {
		if p.items[i].ID == id {
			p.items[i].Quota = quota
			return
		}
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
			"provider":   item.Provider,
			"region":     item.Region,
			"runtime":    item.Runtime,
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
	if p == nil || item.ID == "" {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	for i := range p.items {
		if p.items[i].ID == item.ID {
			item.DownUntil = p.items[i].DownUntil
			item.LastError = p.items[i].LastError
			item.LastKind = p.items[i].LastKind
			if item.Ready == nil {
				item.Ready = p.items[i].Ready
			}
			if item.Hot == nil {
				item.Hot = p.items[i].Hot
			}
			if item.Quota == nil {
				item.Quota = p.items[i].Quota
			}
			if item.Models == nil {
				item.Models = p.items[i].Models
				item.ModelsAt = p.items[i].ModelsAt
			}
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
