package store

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/caigee-cmd/cli2api/internal/accounts"
)

// kahanSum mirrors SQLite's compensated floating-point SUM closely enough to
// keep JSON totals byte-for-byte comparable with the old SQL implementation.
type kahanSum struct {
	sum          float64
	compensation float64
}

func (s *kahanSum) add(value float64) {
	next := s.sum + value
	if math.Abs(s.sum) >= math.Abs(value) {
		s.compensation += (s.sum - next) + value
	} else {
		s.compensation += (value - next) + s.sum
	}
	s.sum = next
}

func (s kahanSum) value() float64 { return s.sum + s.compensation }

// statsNamedAgg accumulates one row of a model/account/provider ranking while
// the single-pass query streams the window. Latencies are kept only as ints;
// percentiles need the values, but the rest of the row is reduced immediately.
type statsNamedAgg struct {
	count        int
	ok           int
	errorCount   int
	prompt       int64
	completion   int64
	cacheRead    int64
	cacheWrite   int64
	credits      kahanSum
	latencySum   int64
	latencyCount int
	latencies    []int
}

func (agg *statsNamedAgg) add(status string, latency, prompt, completion, cacheRead, cacheWrite sql.NullInt64, credit sql.NullFloat64) {
	agg.count++
	switch status {
	case accounts.RequestStatusOK:
		agg.ok++
	case accounts.RequestStatusError:
		agg.errorCount++
	}
	addNullableInt(&agg.prompt, prompt)
	addNullableInt(&agg.completion, completion)
	addNullableInt(&agg.cacheRead, cacheRead)
	addNullableInt(&agg.cacheWrite, cacheWrite)
	if credit.Valid {
		agg.credits.add(credit.Float64)
	}
	if latency.Valid {
		agg.latencySum += latency.Int64
		agg.latencyCount++
		agg.latencies = append(agg.latencies, int(latency.Int64))
	}
}

func addNullableInt(dst *int64, value sql.NullInt64) {
	if value.Valid {
		*dst += value.Int64
	}
}

// summarizeRequestLogsSinglePass is the hot-path implementation behind
// SummarizeRequestLogsWith. The old implementation ran ~10 independent
// scans/queries over the same window (totals, status, errors, three named
// groups, three p95 passes and the series). This streams the window once and
// reduces it in Go, so the scheduler pays one table pass instead of ten.
func (s *Store) summarizeRequestLogsSinglePass(ctx context.Context, opts accounts.RequestStatsOptions) (accounts.RequestStats, error) {
	from, to := opts.From, opts.To
	limit := opts.EffectiveLimit()
	if from.IsZero() {
		from = time.Now().UTC().Add(-24 * time.Hour)
	}
	if to.IsZero() {
		to = time.Now().UTC()
	}
	from = from.UTC()
	to = to.UTC()
	if !to.After(from) {
		to = from.Add(time.Hour)
	}
	hours := int(to.Sub(from).Round(time.Hour) / time.Hour)
	if hours < 1 {
		hours = 1
	}

	filter := accounts.RequestLogFilter{From: &from, To: &to, APIKeyID: opts.APIKeyID}
	where, args := buildRequestLogWhere(filter)
	plan := newStatsSeriesPlan(from, to)
	stats := accounts.RequestStats{
		Window:    accounts.RequestStatsWindow{From: from, To: to, Hours: hours},
		Status:    make([]accounts.RequestStatsBucket, 0),
		Errors:    make([]accounts.RequestStatsBucket, 0),
		Models:    make([]accounts.RequestStatsNamed, 0),
		Accounts:  make([]accounts.RequestStatsNamed, 0),
		Providers: make([]accounts.RequestStatsNamed, 0),
		Series:    plan.points,
	}

	var models, accountRows, providers map[string]*statsNamedAgg
	if opts.WantsDim(accounts.RequestStatsDimModel) {
		models = make(map[string]*statsNamedAgg)
	}
	if opts.WantsDim(accounts.RequestStatsDimAccount) {
		accountRows = make(map[string]*statsNamedAgg)
	}
	if opts.WantsDim(accounts.RequestStatsDimProvider) {
		providers = make(map[string]*statsNamedAgg)
	}

	statusCounts := make(map[string]int)
	errorCounts := make(map[string]int)
	var totalLatencies []int
	seriesLatencies := make([][]int, len(plan.points))
	var latencySum, ttfbSum int64
	var latencyCount, ttfbCount int
	var creditSum kahanSum

	rows, err := s.db.QueryContext(ctx, `
SELECT created_at,
       status,
       error_kind,
       COALESCE(NULLIF(requested_model, ''), '(unknown)'),
       COALESCE(NULLIF(account_id, ''), '(unassigned)'),
       `+providerKeyExpr+`,
       stream,
       latency_ms, ttfb_ms,
       prompt_tokens, completion_tokens, cache_read_tokens, cache_write_tokens,
       `+creditExprNullable+`
FROM request_logs`+where, args...)
	if err != nil {
		return accounts.RequestStats{}, fmt.Errorf("summarize request logs: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			created                    string
			status, errorKind          string
			modelKey, accountKey, prov string
			stream                     int
			latency, ttfb              sql.NullInt64
			prompt, completion         sql.NullInt64
			cacheRead, cacheWrite      sql.NullInt64
			credit                     sql.NullFloat64
		)
		if err := rows.Scan(
			&created, &status, &errorKind, &modelKey, &accountKey, &prov, &stream,
			&latency, &ttfb, &prompt, &completion, &cacheRead, &cacheWrite, &credit,
		); err != nil {
			return accounts.RequestStats{}, fmt.Errorf("scan request stats row: %w", err)
		}

		stats.Totals.Requests++
		switch status {
		case accounts.RequestStatusOK:
			stats.Totals.OK++
		case accounts.RequestStatusIncomplete:
			stats.Totals.Incomplete++
		case accounts.RequestStatusError:
			stats.Totals.Error++
		case accounts.RequestStatusCanceled:
			stats.Totals.Canceled++
		}
		if stream != 0 {
			stats.Totals.Streaming++
		}
		addNullableInt(&stats.Tokens.Prompt, prompt)
		addNullableInt(&stats.Tokens.Completion, completion)
		addNullableInt(&stats.Tokens.CacheRead, cacheRead)
		addNullableInt(&stats.Tokens.CacheWrite, cacheWrite)
		if credit.Valid {
			creditSum.add(credit.Float64)
		}
		if latency.Valid {
			latencySum += latency.Int64
			latencyCount++
			totalLatencies = append(totalLatencies, int(latency.Int64))
		}
		if ttfb.Valid {
			ttfbSum += ttfb.Int64
			ttfbCount++
		}

		statusCounts[status]++
		if errorKind != "" {
			errorCounts[errorKind]++
		}
		if models != nil {
			addNamed(models, modelKey, status, latency, prompt, completion, cacheRead, cacheWrite, credit)
		}
		if accountRows != nil {
			addNamed(accountRows, accountKey, status, latency, prompt, completion, cacheRead, cacheWrite, credit)
		}
		if providers != nil {
			addNamed(providers, prov, status, latency, prompt, completion, cacheRead, cacheWrite, credit)
		}

		createdAt := parseTime(created).UTC()
		if index, ok := plan.index[plan.key(createdAt)]; ok {
			point := &stats.Series[index]
			point.Requests++
			switch status {
			case accounts.RequestStatusOK:
				point.OK++
			case accounts.RequestStatusError:
				point.Error++
			}
			addNullableInt(&point.PromptTokens, prompt)
			addNullableInt(&point.CompletionTokens, completion)
			addNullableInt(&point.CacheReadTokens, cacheRead)
			if latency.Valid {
				seriesLatencies[index] = append(seriesLatencies[index], int(latency.Int64))
			}
		}
	}
	if err := rows.Err(); err != nil {
		return accounts.RequestStats{}, err
	}

	stats.Tokens.Total = stats.Tokens.Prompt + stats.Tokens.Completion
	stats.Totals.Credits = creditSum.value()
	if stats.Totals.Requests > 0 {
		stats.Totals.SuccessRate = float64(stats.Totals.OK) / float64(stats.Totals.Requests)
	}
	stats.Latency.AvgMs = averagePtr(latencySum, latencyCount)
	stats.Latency.TTFBAvgMs = averagePtr(ttfbSum, ttfbCount)
	if len(totalLatencies) > 0 {
		sort.Ints(totalLatencies)
		stats.Latency.P50Ms = roundedInt(percentileNearestRank(totalLatencies, 50))
		stats.Latency.P95Ms = roundedInt(percentileNearestRank(totalLatencies, 95))
	}
	stats.Status = statsBucketsFromCounts(statusCounts, 0)
	stats.Errors = statsBucketsFromCounts(errorCounts, 8)
	stats.Models = statsNamedFromAggs(models, limit)
	stats.Accounts = statsNamedFromAggs(accountRows, limit)
	stats.Providers = statsNamedFromAggs(providers, limit)
	for i := range stats.Series {
		if len(seriesLatencies[i]) == 0 {
			continue
		}
		sort.Ints(seriesLatencies[i])
		stats.Series[i].LatencyP50Ms = roundedInt(percentileNearestRank(seriesLatencies[i], 50))
		stats.Series[i].LatencyP95Ms = roundedInt(percentileNearestRank(seriesLatencies[i], 95))
	}
	return stats, nil
}

func addNamed(rows map[string]*statsNamedAgg, key, status string, latency, prompt, completion, cacheRead, cacheWrite sql.NullInt64, credit sql.NullFloat64) {
	agg := rows[key]
	if agg == nil {
		agg = &statsNamedAgg{}
		rows[key] = agg
	}
	agg.add(status, latency, prompt, completion, cacheRead, cacheWrite, credit)
}

func statsBucketsFromCounts(counts map[string]int, limit int) []accounts.RequestStatsBucket {
	out := make([]accounts.RequestStatsBucket, 0, len(counts))
	for key, count := range counts {
		if strings.TrimSpace(key) == "" {
			continue
		}
		out = append(out, accounts.RequestStatsBucket{Key: key, Count: count})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Key < out[j].Key
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

func statsNamedFromAggs(rows map[string]*statsNamedAgg, limit int) []accounts.RequestStatsNamed {
	out := make([]accounts.RequestStatsNamed, 0, len(rows))
	for key, agg := range rows {
		out = append(out, accounts.RequestStatsNamed{
			Key:              key,
			Count:            agg.count,
			OK:               agg.ok,
			Error:            agg.errorCount,
			LatencyAvgMs:     averagePtr(agg.latencySum, agg.latencyCount),
			PromptTokens:     agg.prompt,
			CompletionTokens: agg.completion,
			CacheReadTokens:  agg.cacheRead,
			CacheWriteTokens: agg.cacheWrite,
			Credits:          agg.credits.value(),
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Key < out[j].Key
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	for i := range out {
		agg := rows[out[i].Key]
		if agg == nil || len(agg.latencies) == 0 {
			continue
		}
		sort.Ints(agg.latencies)
		out[i].LatencyP95Ms = roundedInt(percentileNearestRank(agg.latencies, 95))
	}
	return out
}

func averagePtr(sum int64, count int) *int {
	if count <= 0 {
		return nil
	}
	value := int(float64(sum)/float64(count) + 0.5)
	return &value
}

type statsSeriesPlan struct {
	points  []accounts.RequestStatsPoint
	index   map[string]int
	daily   bool
	quarter bool
}

func newStatsSeriesPlan(from, to time.Time) statsSeriesPlan {
	span := to.Sub(from)
	daily := span > 48*time.Hour
	quarter := !daily && span <= 2*time.Hour
	step := time.Hour
	trunc := from.Truncate(time.Hour)
	end := to.Truncate(time.Hour)
	if daily {
		step = 24 * time.Hour
		trunc = time.Date(from.Year(), from.Month(), from.Day(), 0, 0, 0, 0, time.UTC)
		end = time.Date(to.Year(), to.Month(), to.Day(), 0, 0, 0, 0, time.UTC)
	} else if quarter {
		step = 15 * time.Minute
		trunc = from.Truncate(15 * time.Minute)
		end = to.Truncate(15 * time.Minute)
	}
	if to.After(end) {
		end = end.Add(step)
	}
	points := make([]accounts.RequestStatsPoint, 0)
	index := map[string]int{}
	for cursor := trunc; cursor.Before(end); cursor = cursor.Add(step) {
		index[seriesKey(cursor, daily, quarter)] = len(points)
		points = append(points, accounts.RequestStatsPoint{At: cursor.UTC()})
	}
	return statsSeriesPlan{points: points, index: index, daily: daily, quarter: quarter}
}

func (plan statsSeriesPlan) key(at time.Time) string {
	return seriesKey(at, plan.daily, plan.quarter)
}

func seriesKey(value time.Time, daily, quarter bool) string {
	utc := value.UTC()
	if daily {
		return utc.Format("2006-01-02")
	}
	if quarter {
		return utc.Truncate(15 * time.Minute).Format("2006-01-02T15:04")
	}
	return utc.Format("2006-01-02T15")
}
