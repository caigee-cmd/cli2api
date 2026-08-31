package accounts

import (
	"sort"
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
	ID       string
	URL      string
	Provider string
	Region   string
	Runtime  string
	// Weight is the per-account scheduling weight (1..100). Accounts share
	// the default, so the pool stays a plain round-robin until an operator
	// differentiates them.
	Weight    int
	DownUntil time.Time
	LastError string
	LastKind  string
	Ready     *bool
	Hot       *bool
	InFlight  int
	// MaxInFlight caps concurrent requests routed to this account. Zero
	// means unknown and does not block routing.
	MaxInFlight int
	Restarts    int
	Quota       *QuotaSnapshot
	// Models is the last successful per-account catalog snapshot. A nil
	// slice means unknown (fail open); a non-nil slice, including empty,
	// is used to filter PublicModel. Entries keep the provider-native
	// spelling so Trae config_name case is preserved.
	Models   []string
	ModelsAt time.Time
	// ModelDownUntil is per-model cooldown. One model hitting a limit must
	// not take the whole account offline for other models.
	ModelDownUntil map[string]time.Time
	// BackoffLevel climbs on repeated failures of the same kind and resets
	// on success, so a persistently failing account backs off instead of
	// being retried at a fixed interval.
	BackoffLevel int
	// DropSystemPrompt mirrors the stored account flag so the executor can
	// sanitize requests per account without a store lookup per chat.
	DropSystemPrompt bool
}

// RouteQuery selects candidates for one public model request. Empty fields are
// not filtered; excluded account IDs are honored before anything else.
type RouteQuery struct {
	PublicModel      string
	PreferAccount    string
	ProviderFilter   string
	RegionFilter     string
	AllowedProviders []string
	Excluded         map[string]struct{}
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

func providerAllowed(provider string, allowed []string) bool {
	if len(allowed) == 0 {
		return true
	}
	family := strings.ToLower(strings.TrimSpace(provider))
	if family == "" {
		family = "qoder"
	}
	for _, item := range allowed {
		if strings.ToLower(strings.TrimSpace(item)) == family {
			return true
		}
	}
	return false
}

// NormalizeWeight maps a stored priority onto a scheduling weight. The
// console exposes 1..100 with 50 as the default; anything outside the range
// falls back to the default so a bad row cannot distort rotation.
func NormalizeWeight(priority int) int {
	if priority < 1 || priority > 100 {
		return defaultWeight
	}
	return priority
}

const defaultWeight = 50

// itemWeight is the effective scheduling weight. Every account at the default
// keeps plain round-robin; differentiated accounts are picked proportionally.
func itemWeight(item Item) int {
	return NormalizeWeight(item.Weight)
}

func routeMatches(item Item, q RouteQuery) bool {
	if _, skip := q.Excluded[item.ID]; skip {
		return false
	}
	if !providerAllowed(item.Provider, q.AllowedProviders) {
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
	mu    sync.Mutex
	items []Item
	// lastPicked tracks the rotation cursor per route, keyed by
	// provider|region|model, as the *ID* of the previous pick.
	//
	// A numeric index into a shrinking candidate set silently re-seats the
	// rotation: candidates drop out whenever a retry excludes an account, an
	// account enters cooldown, or a model becomes unavailable. Indexing a
	// monotonic counter into that changing slice starves some accounts and
	// hammers others. Keying on the previous pick's ID resumes rotation at
	// the account that follows it, whatever happened in between.
	lastPicked map[string]string
	// lastRegion remembers the region a route was last served from. Request
	// handlers pin the region from whichever account is picked first, so an
	// unpinned pick decides the region for the whole request. Without this a
	// mixed-region pool would flip between regions as rotation advances.
	lastRegion map[string]string
	// weightCounter holds smooth-WRR running counters per route, keyed like
	// lastPicked and then by account ID. Only used when weights differ.
	weightCounter map[string]map[string]int64
	observer      func(Item)
}

// rotationLimit bounds the cursor map so long-tailed model IDs cannot grow it
// without bound. Hitting the limit resets every route's cursor, which costs
// one rotation reseed rather than unbounded memory.
const rotationLimit = 4096

func rotationKey(q RouteQuery) string {
	model := routeModel(q.PublicModel)
	return providerFilterKey(q) + "|" + itemRegion(Item{Region: q.RegionFilter}) + "|" + model
}

func providerFilterKey(q RouteQuery) string {
	if q.ProviderFilter != "" {
		return strings.ToLower(strings.TrimSpace(q.ProviderFilter))
	}
	return "*"
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
	// Candidates that are ready right now. Sorted by ID so the rotation
	// cursor can resume from the previous pick even as the set changes.
	available := make([]Item, 0, len(eligible))
	for _, i := range eligible {
		item := p.items[i]
		if !itemDown(item, now) && !itemModelDown(item, q, now) && !itemSaturated(item) {
			available = append(available, item)
		}
	}
	if len(available) == 0 {
		// Everything is cooling or saturated. Surface the item that frees up
		// soonest so the caller can report a classified error with a retry
		// hint, rather than silently picking an arbitrary account.
		var best Item
		found := false
		for _, i := range eligible {
			item := p.items[i]
			if !found || resumeAt(item, q).Before(resumeAt(best, q)) {
				best = item
				found = true
			}
		}
		return best, found
	}
	sort.Slice(available, func(i, j int) bool { return available[i].ID < available[j].ID })
	key := rotationKey(q)
	p.ensureRotationKey(key)
	// When the caller has not pinned a region, keep serving the region this
	// route used last (if it still has a candidate). Request handlers pin the
	// region from the first account picked, so an unpinned pick decides the
	// region for the whole request; letting rotation decide it would flip
	// regions as rotation advances. On a cold route there is no history, so
	// pick the region with the most candidates and remember it — starting at
	// the alphabetically first account would otherwise let one small region
	// capture the route.
	if q.RegionFilter == "" {
		if previous, ok := p.lastRegion[key]; ok {
			if subset := inRegion(available, previous); len(subset) > 0 {
				available = subset
			}
		}
		if _, ok := p.lastRegion[key]; !ok {
			if preferred := largestRegion(available); preferred != "" {
				p.lastRegion[key] = preferred
				if subset := inRegion(available, preferred); len(subset) > 0 {
					available = subset
				}
			}
		}
	} else {
		p.lastRegion[key] = itemRegion(available[0])
	}
	if weighted, ok := p.pickWeighted(available, key); ok {
		// Differentiated weights: smooth weighted round-robin keeps the load
		// proportional without bursting one high-weight account first.
		p.lastPicked[key] = weighted.ID
		p.lastRegion[key] = itemRegion(weighted)
		return weighted, true
	}
	picked := available[successorIndex(available, p.lastPicked[key])]
	p.lastPicked[key] = picked.ID
	p.lastRegion[key] = itemRegion(picked)
	return picked, true
}

// pickWeighted runs smooth weighted round-robin when candidates have differing
// weights. It reports false when every candidate shares one weight, which
// leaves plain round-robin in charge: with a uniform pool the two are
// equivalent, and the ID cursor is what keeps rotation stable across a
// changing candidate set.
func (p *Pool) pickWeighted(available []Item, key string) (Item, bool) {
	total := int64(0)
	uniform := true
	first := itemWeight(available[0])
	for _, item := range available {
		weight := int64(itemWeight(item))
		total += weight
		if weight != int64(first) {
			uniform = false
		}
	}
	if uniform || total <= 0 {
		return Item{}, false
	}
	if p.weightCounter == nil {
		p.weightCounter = map[string]map[string]int64{}
	}
	if p.weightCounter[key] == nil {
		p.weightCounter[key] = map[string]int64{}
	}
	// Smooth WRR: add each candidate's weight to its running counter and take
	// the largest, then subtract the total from the winner. This spreads picks
	// evenly across a window instead of front-loading the heaviest candidate.
	best := -1
	var bestCurrent int64
	for i := range available {
		current := p.weightCounter[key][available[i].ID] + int64(itemWeight(available[i]))
		p.weightCounter[key][available[i].ID] = current
		if best < 0 || current > bestCurrent {
			best = i
			bestCurrent = current
		}
	}
	if best < 0 {
		return Item{}, false
	}
	p.weightCounter[key][available[best].ID] -= total
	return available[best], true
}

// successorIndex returns the position of the first candidate ordered after
// lastID, wrapping to the head. Candidates are sorted by ID, so this resumes
// rotation at the account following the previous pick even when candidates
// were filtered out in between. An empty lastID starts at the head.
func successorIndex(available []Item, lastID string) int {
	if lastID == "" {
		return 0
	}
	index := sort.Search(len(available), func(i int) bool { return available[i].ID > lastID })
	if index >= len(available) {
		return 0
	}
	return index
}

// ensureRotationKey keeps the cursor map bounded. Must be called with p.mu held.
func (p *Pool) ensureRotationKey(key string) {
	if p.lastPicked == nil {
		p.lastPicked = make(map[string]string)
	}
	if p.lastRegion == nil {
		p.lastRegion = make(map[string]string)
	}
	if _, ok := p.lastPicked[key]; !ok && len(p.lastPicked) >= rotationLimit {
		p.lastPicked = make(map[string]string)
	}
}

// largestRegion returns the region with the most candidates, breaking ties by
// name so the choice is stable across restarts rather than map-order random.
func largestRegion(items []Item) string {
	counts := map[string]int{}
	for _, item := range items {
		counts[itemRegion(item)]++
	}
	best := ""
	bestCount := 0
	for region, count := range counts {
		if count > bestCount || (count == bestCount && (best == "" || region < best)) {
			best = region
			bestCount = count
		}
	}
	return best
}

// inRegion narrows candidates to one region. An empty pool means that region
// has nothing available right now and the caller should fall back to all.
func inRegion(items []Item, region string) []Item {
	out := make([]Item, 0, len(items))
	for _, item := range items {
		if itemRegion(item) == region {
			out = append(out, item)
		}
	}
	return out
}

// itemSaturated reports whether the account already serves its configured
// concurrency limit. max_inflight was stored but never enforced, so a single
// account could absorb every concurrent request during a burst.
func itemSaturated(item Item) bool {
	if item.MaxInFlight <= 0 {
		return false
	}
	return item.InFlight >= item.MaxInFlight
}

// itemModelDown applies the per-model cooldown: one model hitting a limit
// must not take the whole account offline for other models.
func itemModelDown(item Item, q RouteQuery, now time.Time) bool {
	model := routeModel(q.PublicModel)
	if model == "" {
		return false
	}
	until, ok := item.ModelDownUntil[model]
	return ok && now.Before(until)
}

// resumeAt is the moment an item becomes usable again for this route,
// considering both account-level and model-level cooldowns.
func resumeAt(item Item, q RouteQuery) time.Time {
	next := item.DownUntil
	model := routeModel(q.PublicModel)
	if model != "" {
		if until, ok := item.ModelDownUntil[model]; ok && (next.IsZero() || until.After(next)) {
			next = until
		}
	}
	return next
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
		if c.Kind != KindInvalidRequest {
			p.items[i].LastKind = c.Kind
		}
		if c.Cooldown > 0 && (c.Failover || c.Kind != KindQuota) {
			// Repeated failures of the same kind back off exponentially so a
			// persistently broken account stops consuming attempts at a fixed
			// interval. A new failure kind starts the ladder over.
			if p.items[i].LastKind == c.Kind && c.Kind != KindInvalidRequest {
				p.items[i].BackoffLevel++
			}
			cooldown := c.Cooldown
			if p.items[i].BackoffLevel > 1 {
				cooldown, _ = nextBackoffCooldown(c.Cooldown, p.items[i].BackoffLevel-1)
			}
			until := time.Now().Add(cooldown)
			if model := routeModel(c.Model); model != "" {
				// Scope to one model: a rate limit on glm-5.3 must not take
				// the account offline for deepseek-v4-flash.
				if p.items[i].ModelDownUntil == nil {
					p.items[i].ModelDownUntil = map[string]time.Time{}
				}
				p.items[i].ModelDownUntil[model] = until
			} else {
				p.items[i].DownUntil = until
			}
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
			// Success clears the backoff ladder and any model-scoped
			// cooldowns: the account proved it can serve traffic again.
			p.items[i].BackoffLevel = 0
			p.items[i].ModelDownUntil = nil
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
			// Carry runtime state forward, but only when the caller did not
			// supply a value. Restoring persisted cooldowns needs to write
			// these fields; the old unconditional copy silently discarded
			// them.
			if item.DownUntil.IsZero() {
				item.DownUntil = p.items[i].DownUntil
			}
			if item.LastError == "" {
				item.LastError = p.items[i].LastError
			}
			if item.LastKind == "" {
				item.LastKind = p.items[i].LastKind
			}
			if item.BackoffLevel == 0 {
				item.BackoffLevel = p.items[i].BackoffLevel
			}
			if item.ModelDownUntil == nil {
				item.ModelDownUntil = p.items[i].ModelDownUntil
			}
			if item.Weight == 0 {
				item.Weight = p.items[i].Weight
			}
			if item.MaxInFlight == 0 {
				item.MaxInFlight = p.items[i].MaxInFlight
			}
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
		// The rotation cursor stores the previous pick's ID, so a removed
		// account is skipped naturally; only a cursor pointing at it needs
		// clearing.
		p.items = append(p.items[:i], p.items[i+1:]...)
		p.dropRotationCursor(id)
		return
	}
}

// dropRotationCursor clears any route whose cursor points at a removed
// account, so rotation restarts at the head instead of resuming from an ID
// that no longer exists. Must be called with p.mu held.
func (p *Pool) dropRotationCursor(id string) {
	for key, picked := range p.lastPicked {
		if picked == id {
			delete(p.lastPicked, key)
		}
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
