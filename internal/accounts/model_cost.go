package accounts

import (
	"sort"
	"strings"
	"time"
)

// TimeBand splits a price list into the off-peak and peak rows DeepSeek (and
// most CN vendors) bill at. Prices are CNY per 1M tokens.
type ModelPriceBand struct {
	CacheHit  float64 `json:"cache_hit"`
	CacheMiss float64 `json:"cache_miss"`
	Output    float64 `json:"output"`
}

// ModelPrice carries the official list price for one model. Peak hours are the
// vendor's published peak window in China Standard Time (UTC+8); a zero window
// (empty PeakStart/PeakEnd) means the model has a flat price. When flat, both
// bands hold the same numbers and PeakWeekdaysOnly is irrelevant.
type ModelPrice struct {
	ID               string         `json:"id"`
	Label            string         `json:"label"`
	Vendor           string         `json:"vendor"`
	Currency         string         `json:"currency"`
	OffPeak          ModelPriceBand `json:"off_peak"`
	Peak             ModelPriceBand `json:"peak"`
	PeakStartHour    int            `json:"peak_start_hour"`
	PeakEndHour      int            `json:"peak_end_hour"`
	PeakWeekdaysOnly bool           `json:"peak_weekdays_only"`
	PeakNote         string         `json:"peak_note,omitempty"`
	Source           string         `json:"source,omitempty"`
	// Alias marks an id that shares another row's price (legacy rename). The
	// console hides alias rows from the price table but still matches them in
	// logs so old request rows price correctly.
	Alias bool `json:"alias,omitempty"`
	// CanonicalID, when set, folds this alias onto the canonical priced model so
	// cost totals and the breakdown do not split one model across two rows.
	CanonicalID string `json:"canonical_id,omitempty"`
}

// TokenUsage is the billable token split a request produced. CacheMiss is the
// portion of the prompt that was NOT served from cache (prompt - cache_read).
type TokenUsage struct {
	CacheHit  int64 `json:"cache_hit"`
	CacheMiss int64 `json:"cache_miss"`
	Output    int64 `json:"output"`
}

// Total returns all billable tokens together.
func (u TokenUsage) Total() int64 { return u.CacheHit + u.CacheMiss + u.Output }

// ModelCost is the monetary breakdown for one model over one window.
type ModelCost struct {
	ID           string  `json:"id"`
	Label        string  `json:"label"`
	Vendor       string  `json:"vendor"`
	Currency     string  `json:"currency"`
	Requests     int64   `json:"requests"`
	CacheHit     int64   `json:"cache_hit"`
	CacheMiss    int64   `json:"cache_miss"`
	Output       int64   `json:"output"`
	TotalTokens  int64   `json:"total_tokens"`
	Cost         float64 `json:"cost"`
	CostOffPeak  float64 `json:"cost_off_peak"`
	CostPeak     float64 `json:"cost_peak"`
	PeakRequests int64   `json:"peak_requests"`
	// Credits is the subscription quota this model consumed, as recorded by the
	// provider. It applies to every provider, priced or not, and is the honest
	// cost unit for subscription accounts where no list price exists.
	Credits float64 `json:"credits"`
	// Priced reports whether an official list price backs the Cost fields.
	// Unpriced models still contribute tokens and Credits.
	Priced bool `json:"priced"`
}

// CostBreakdown is the Usage Cost tab payload: quota and tokens for every
// model, plus an optional DeepSeek list-price CNY estimate for priced rows.
type CostBreakdown struct {
	Window   RequestStatsWindow `json:"window"`
	Currency string             `json:"currency"`
	Models   []ModelCost        `json:"models"`
	Totals   ModelCost          `json:"totals"`
	Series   []CostSeriesPoint  `json:"series"`
	Unpriced []string           `json:"unpriced,omitempty"`
}

// CostSeriesPoint is one time bucket for the stacked chart. PerModelCredits and
// PerModelTokens cover every model; PerModel holds DeepSeek list-price CNY for
// priced models only (optional estimate UI).
type CostSeriesPoint struct {
	At              time.Time          `json:"at"`
	PerModel        map[string]float64 `json:"per_model"`
	PerModelCredits map[string]float64 `json:"per_model_credits"`
	PerModelTokens  map[string]float64 `json:"per_model_tokens"`
	Tokens          int64              `json:"tokens"`
	Credits         float64            `json:"credits"`
}

// chinaLocation is UTC+8 with no DST — DeepSeek's published peak window is
// expressed in Beijing time.
var chinaLocation = time.FixedZone("CST", 8*60*60)

// deepSeekPrices returns the DeepSeek official list prices in CNY per 1M tokens.
// Source: https://api-docs.deepseek.com/zh-cn/quick_start/pricing (fetched
// 2026-10-16). The estimate uses Beijing time Mon–Fri
// 09:00–12:00 and 14:00–18:00; holidays and adjusted working days are not
// modeled. Off-peak is exactly half price. The midday
// break makes the peak window non-contiguous, so we honor the two segments in
// isPeakHour instead of a single [start,end) pair.
func deepSeekPrices() []ModelPrice {
	const src = "https://api-docs.deepseek.com/zh-cn/quick_start/pricing"
	return []ModelPrice{
		{
			ID: "deepseek-v4.1-flash", Label: "DeepSeek V4.1 Flash", Vendor: "DeepSeek", Currency: "CNY",
			OffPeak:       ModelPriceBand{CacheHit: 0.02, CacheMiss: 1, Output: 4},
			Peak:          ModelPriceBand{CacheHit: 0.04, CacheMiss: 2, Output: 8},
			PeakStartHour: 9, PeakEndHour: 18, PeakWeekdaysOnly: true, Source: src,
			PeakNote: "按北京时间周一至周五 09:00-12:00、14:00-18:00 估算高峰价；未处理节假日及调休",
		},
		{
			ID: "deepseek-v4-flash", Label: "DeepSeek V4.1 Flash（旧名）", Vendor: "DeepSeek", Currency: "CNY",
			OffPeak:       ModelPriceBand{CacheHit: 0.02, CacheMiss: 1, Output: 4},
			Peak:          ModelPriceBand{CacheHit: 0.04, CacheMiss: 2, Output: 8},
			PeakStartHour: 9, PeakEndHour: 18, PeakWeekdaysOnly: true, Source: src,
			PeakNote: "旧模型名，按 Flash 价格计费", Alias: true, CanonicalID: "deepseek-v4.1-flash",
		},
		{
			ID: "deepseek-flash", Label: "DeepSeek Flash", Vendor: "DeepSeek", Currency: "CNY",
			OffPeak:       ModelPriceBand{CacheHit: 0.02, CacheMiss: 1, Output: 4},
			Peak:          ModelPriceBand{CacheHit: 0.04, CacheMiss: 2, Output: 8},
			PeakStartHour: 9, PeakEndHour: 18, PeakWeekdaysOnly: true, Source: src,
			PeakNote: "官网主名，按 Flash 价格计费", Alias: true, CanonicalID: "deepseek-v4.1-flash",
		},
		{
			ID: "deepseek-v4-pro", Label: "DeepSeek V4 Pro", Vendor: "DeepSeek", Currency: "CNY",
			OffPeak:       ModelPriceBand{CacheHit: 0.15, CacheMiss: 4.5, Output: 13.5},
			Peak:          ModelPriceBand{CacheHit: 0.30, CacheMiss: 9.0, Output: 27.0},
			PeakStartHour: 9, PeakEndHour: 18, PeakWeekdaysOnly: true, Source: src,
			PeakNote: "按北京时间周一至周五 09:00-12:00、14:00-18:00 估算高峰价；未处理节假日及调休",
		},
	}
}

// ModelPriceCatalog is the full set of priced models, sorted by vendor then id.
func ModelPriceCatalog() []ModelPrice {
	prices := deepSeekPrices()
	sort.Slice(prices, func(i, j int) bool {
		if prices[i].Vendor != prices[j].Vendor {
			return prices[i].Vendor < prices[j].Vendor
		}
		return prices[i].ID < prices[j].ID
	})
	return prices
}

// PricedModelIDs returns the bare model ids that have a price row. The store
// uses this to filter request_logs without re-spelling the catalog.
func PricedModelIDs() []string {
	prices := deepSeekPrices()
	ids := make([]string, 0, len(prices))
	for _, p := range prices {
		ids = append(ids, p.ID)
	}
	return ids
}

// LookupModelPrice matches an upstream model id to a price row. Matching is
// case-insensitive, tolerates a provider prefix (workbuddy/...), and strips a
// trailing "-sg" / "_sg" regional suffix so WorkBuddy's Singapore aliases fold
// onto the same official price.
func LookupModelPrice(modelID string) (ModelPrice, bool) {
	id := normalizePriceModelID(modelID)
	if id == "" {
		return ModelPrice{}, false
	}
	for _, p := range deepSeekPrices() {
		if p.ID == id {
			return p, true
		}
	}
	return ModelPrice{}, false
}

func normalizePriceModelID(modelID string) string {
	id := strings.ToLower(strings.TrimSpace(modelID))
	if idx := strings.LastIndex(id, "/"); idx >= 0 {
		id = id[idx+1:]
	}
	for _, suffix := range []string{"-sg", "_sg"} {
		if strings.HasSuffix(id, suffix) {
			id = strings.TrimSuffix(id, suffix)
		}
	}
	return id
}

// isPeakHour reports whether the UTC timestamp falls in the vendor peak window.
func (p ModelPrice) isPeakHour(at time.Time) bool {
	if p.PeakStartHour == 0 && p.PeakEndHour == 0 {
		return false
	}
	local := at.In(chinaLocation)
	if p.PeakWeekdaysOnly && (local.Weekday() == time.Saturday || local.Weekday() == time.Sunday) {
		return false
	}
	hour := local.Hour()
	// DeepSeek's peak window is 09:00–12:00 and 14:00–18:00, not a single span.
	// Models with a single contiguous morning/afternoon window still fit the
	// [start,end) test below because the published midday break is the only gap.
	if p.PeakStartHour == 9 && p.PeakEndHour == 18 {
		return (hour >= 9 && hour < 12) || (hour >= 14 && hour < 18)
	}
	return hour >= p.PeakStartHour && hour < p.PeakEndHour
}

// bandCost prices a token split at the given band.
func bandCost(u TokenUsage, b ModelPriceBand) float64 {
	const million = 1_000_000
	return (float64(u.CacheHit)*b.CacheHit + float64(u.CacheMiss)*b.CacheMiss + float64(u.Output)*b.Output) / million
}

// Cost prices one request's usage; when peak is true it bills the whole split
// at the peak band. (Per-request granularity is the caller's; a request that
// straddles the boundary is billed by its start time, which matches how the
// provider stamps usage.)
func (p ModelPrice) Cost(u TokenUsage, peak bool) float64 {
	if peak {
		return bandCost(u, p.Peak)
	}
	return bandCost(u, p.OffPeak)
}

// CostUsageRow is one pre-aggregated slice the store hands back: token usage for
// one model in one whole-hour UTC bucket. Bucketing by hour keeps every row on
// one side of DeepSeek's peak boundary (which is set on Beijing whole hours), so
// the peak/off-peak split stays exact even when callers ask for a daily view.
type CostUsageRow struct {
	Model     string
	HourStart time.Time // UTC, truncated to the hour
	Requests  int64
	Usage     TokenUsage
	// Credits is the quota consumed in this bucket; it is provider-agnostic and
	// survives models that have no official list price.
	Credits float64
}

// accumulateUsage folds one bucket's tokens and quota into a target row. Cost is
// added by the caller because only priced rows carry it.
func accumulateUsage(target *ModelCost, row CostUsageRow, cost float64) {
	target.Requests += row.Requests
	target.CacheHit += row.Usage.CacheHit
	target.CacheMiss += row.Usage.CacheMiss
	target.Output += row.Usage.Output
	target.TotalTokens += row.Usage.Total()
	target.Credits += row.Credits
	target.Cost += cost
}

// BuildCostBreakdown prices pre-aggregated usage rows into the console payload.
// When daily is true the series is bucketed by UTC day, otherwise by hour.
func BuildCostBreakdown(rows []CostUsageRow, from, to time.Time, daily bool) CostBreakdown {
	from = from.UTC()
	to = to.UTC()
	hours := int(to.Sub(from).Round(time.Hour) / time.Hour)
	if hours < 1 {
		hours = 1
	}

	byModel := map[string]*ModelCost{}
	order := make([]string, 0, len(rows))
	seen := map[string]bool{}

	// Series buckets keyed by ISO-ish string; we keep the bucket start for sort.
	type seriesAcc struct {
		at              time.Time
		perModel        map[string]float64
		perModelCredits map[string]float64
		perModelTokens  map[string]float64
		tokens          int64
		credits         float64
	}
	series := map[string]*seriesAcc{}
	seriesOrder := make([]string, 0)

	bucketKey := func(t time.Time) (string, time.Time) {
		if daily {
			day := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
			return day.Format("2006-01-02"), day
		}
		h := t.Truncate(time.Hour)
		return h.Format("2006-01-02T15"), h
	}

	var total ModelCost
	unpricedSet := map[string]bool{}
	var unpriced []string
	for _, row := range rows {
		price, ok := LookupModelPrice(row.Model)
		if ok && price.CanonicalID != "" {
			if canonical, found := LookupModelPrice(price.CanonicalID); found {
				price = canonical
			}
		}
		if !ok {
			// No official list price, but the request still consumed quota and
			// tokens. Keep the row so the usage table and credit totals stay
			// complete; only the monetary columns stay zero.
			if row.Requests > 0 || row.Usage.Total() > 0 || row.Credits > 0 {
				key := strings.TrimSpace(row.Model)
				if key == "" {
					key = "(unknown)"
				}
				if !unpricedSet[key] {
					unpricedSet[key] = true
					unpriced = append(unpriced, key)
				}
			}
			mc := byModel[row.Model]
			if mc == nil {
				label := strings.TrimSpace(row.Model)
				if label == "" {
					label = "(unknown)"
				}
				mc = &ModelCost{ID: label, Label: label, Currency: ""}
				byModel[row.Model] = mc
			}
			if !seen[row.Model] {
				seen[row.Model] = true
				order = append(order, row.Model)
			}
			accumulateUsage(mc, row, 0)
			accumulateUsage(&total, row, 0)
			key, at := bucketKey(row.HourStart)
			acc := series[key]
			if acc == nil {
				acc = &seriesAcc{at: at, perModel: map[string]float64{}, perModelCredits: map[string]float64{}, perModelTokens: map[string]float64{}}
				series[key] = acc
				seriesOrder = append(seriesOrder, key)
			}
			acc.tokens += row.Usage.Total()
			acc.credits += row.Credits
			acc.perModelCredits[mc.ID] += row.Credits
			acc.perModelTokens[mc.ID] += float64(row.Usage.Total())
			continue
		}
		mc := byModel[price.ID]
		if mc == nil {
			mc = &ModelCost{ID: price.ID, Label: price.Label, Vendor: price.Vendor, Currency: price.Currency}
			byModel[price.ID] = mc
		}
		if !seen[price.ID] {
			seen[price.ID] = true
			order = append(order, price.ID)
		}
		peak := price.isPeakHour(row.HourStart)
		cost := price.Cost(row.Usage, peak)

		mc.Priced = true
		accumulateUsage(mc, row, cost)
		if peak {
			mc.CostPeak += cost
			mc.PeakRequests += row.Requests
		} else {
			mc.CostOffPeak += cost
		}

		accumulateUsage(&total, row, cost)
		if peak {
			total.CostPeak += cost
			total.PeakRequests += row.Requests
		} else {
			total.CostOffPeak += cost
		}

		key, at := bucketKey(row.HourStart)
		acc := series[key]
		if acc == nil {
			acc = &seriesAcc{at: at, perModel: map[string]float64{}, perModelCredits: map[string]float64{}, perModelTokens: map[string]float64{}}
			series[key] = acc
			seriesOrder = append(seriesOrder, key)
		}
		acc.perModel[price.ID] += cost
		acc.perModelCredits[price.ID] += row.Credits
		acc.perModelTokens[price.ID] += float64(row.Usage.Total())
		acc.tokens += row.Usage.Total()
		acc.credits += row.Credits
	}

	sort.Slice(order, func(i, j int) bool {
		a, b := byModel[order[i]], byModel[order[j]]
		if a.Credits != b.Credits {
			return a.Credits > b.Credits
		}
		if a.TotalTokens != b.TotalTokens {
			return a.TotalTokens > b.TotalTokens
		}
		return a.Cost > b.Cost
	})
	models := make([]ModelCost, 0, len(order))
	for _, id := range order {
		models = append(models, *byModel[id])
	}

	sort.Strings(seriesOrder)
	points := make([]CostSeriesPoint, 0, len(seriesOrder))
	for _, key := range seriesOrder {
		acc := series[key]
		points = append(points, CostSeriesPoint{
			At:              acc.at,
			PerModel:        acc.perModel,
			PerModelCredits: acc.perModelCredits,
			PerModelTokens:  acc.perModelTokens,
			Tokens:          acc.tokens,
			Credits:         acc.credits,
		})
	}

	sort.Strings(unpriced)
	return CostBreakdown{
		Window:   RequestStatsWindow{From: from, To: to, Hours: hours},
		Currency: "CNY",
		Models:   models,
		Totals:   total,
		Series:   points,
		Unpriced: unpriced,
	}
}
