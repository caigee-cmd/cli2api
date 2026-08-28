package accounts

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	RequestStatusStarted   = "started"
	RequestStatusStreaming = "streaming"
	RequestStatusOK        = "ok"
	RequestStatusError     = "error"
	RequestStatusCanceled  = "canceled"

	AttemptStatusStarted  = "started"
	AttemptStatusOK       = "ok"
	AttemptStatusError    = "error"
	AttemptStatusFailover = "failover"
)

var ErrRequestLogNotFound = errors.New("request log not found")

type RequestLog struct {
	ID               string           `json:"id"`
	CreatedAt        time.Time        `json:"created_at"`
	FinishedAt       *time.Time       `json:"finished_at,omitempty"`
	Stream           bool             `json:"stream"`
	Status           string           `json:"status"`
	RequestedModel   string           `json:"requested_model"`
	MappedModel      string           `json:"mapped_model,omitempty"`
	AccountID        string           `json:"account_id,omitempty"`
	PromptTokens     *int             `json:"prompt_tokens,omitempty"`
	CompletionTokens *int             `json:"completion_tokens,omitempty"`
	CacheReadTokens  *int             `json:"cache_read_tokens,omitempty"`
	CacheWriteTokens *int             `json:"cache_write_tokens,omitempty"`
	UsageSource      string           `json:"usage_source,omitempty"`
	Credits          *float64         `json:"credits,omitempty"`
	LatencyMs        *int             `json:"latency_ms,omitempty"`
	TTFBMs           *int             `json:"ttfb_ms,omitempty"`
	ErrorKind        string           `json:"error_kind,omitempty"`
	ErrorCode        string           `json:"error_code,omitempty"`
	ErrorMessage     string           `json:"error_message,omitempty"`
	AttemptCount     int              `json:"attempt_count"`
	Attempts         []RequestAttempt `json:"attempts,omitempty"`
}

type RequestAttempt struct {
	ID               string     `json:"id"`
	RequestID        string     `json:"request_id"`
	AttemptIndex     int        `json:"attempt_index"`
	AccountID        string     `json:"account_id,omitempty"`
	StartedAt        time.Time  `json:"started_at"`
	FinishedAt       *time.Time `json:"finished_at,omitempty"`
	Status           string     `json:"status"`
	HTTPStatus       *int       `json:"http_status,omitempty"`
	ErrorKind        string     `json:"error_kind,omitempty"`
	ErrorMessage     string     `json:"error_message,omitempty"`
	LatencyMs        *int       `json:"latency_ms,omitempty"`
	PromptTokens     *int       `json:"prompt_tokens,omitempty"`
	CompletionTokens *int       `json:"completion_tokens,omitempty"`
	UsageSource      string     `json:"usage_source,omitempty"`
}

type RequestLogFilter struct {
	AccountID string
	Status    string
	Stream    *bool
	ErrorKind string
	Model     string
	Query     string
	From      *time.Time
	To        *time.Time
	Limit     int
	Offset    int
}

type RequestLogList struct {
	Items  []RequestLog `json:"items"`
	Total  int          `json:"total"`
	Limit  int          `json:"limit"`
	Offset int          `json:"offset"`
}

type RequestStats struct {
	Window   RequestStatsWindow   `json:"window"`
	Totals   RequestStatsTotals   `json:"totals"`
	Latency  RequestStatsLatency  `json:"latency"`
	Tokens   RequestStatsTokens   `json:"tokens"`
	Status   []RequestStatsBucket `json:"status"`
	Errors   []RequestStatsBucket `json:"errors"`
	Models   []RequestStatsNamed  `json:"models"`
	Accounts []RequestStatsNamed  `json:"accounts"`
	Series   []RequestStatsPoint  `json:"series"`
}

type RequestStatsWindow struct {
	From  time.Time `json:"from"`
	To    time.Time `json:"to"`
	Hours int       `json:"hours"`
}

type RequestStatsTotals struct {
	Requests    int     `json:"requests"`
	OK          int     `json:"ok"`
	Error       int     `json:"error"`
	Canceled    int     `json:"canceled"`
	Streaming   int     `json:"streaming"`
	SuccessRate float64 `json:"success_rate"`
}

type RequestStatsLatency struct {
	AvgMs     *int `json:"avg_ms,omitempty"`
	P50Ms     *int `json:"p50_ms,omitempty"`
	P95Ms     *int `json:"p95_ms,omitempty"`
	TTFBAvgMs *int `json:"ttfb_avg_ms,omitempty"`
}

type RequestStatsTokens struct {
	Prompt     int64 `json:"prompt"`
	Completion int64 `json:"completion"`
	CacheRead  int64 `json:"cache_read"`
	Total      int64 `json:"total"`
}

type RequestStatsBucket struct {
	Key   string `json:"key"`
	Count int    `json:"count"`
}

type RequestStatsNamed struct {
	Key          string `json:"key"`
	Count        int    `json:"count"`
	OK           int    `json:"ok"`
	Error        int    `json:"error"`
	LatencyAvgMs *int   `json:"latency_avg_ms,omitempty"`
}

type RequestStatsPoint struct {
	At       time.Time `json:"at"`
	Requests int       `json:"requests"`
	OK       int       `json:"ok"`
	Error    int       `json:"error"`
}

func NewRequestID() string {
	raw := make([]byte, 8)
	if _, err := rand.Read(raw); err != nil {
		return fmt.Sprintf("req_%d", time.Now().UnixNano())
	}
	return "req_" + hex.EncodeToString(raw)
}

func NewAttemptID() string {
	raw := make([]byte, 6)
	if _, err := rand.Read(raw); err != nil {
		return fmt.Sprintf("att_%d", time.Now().UnixNano())
	}
	return "att_" + hex.EncodeToString(raw)
}

func (s *Store) InsertRequestLog(ctx context.Context, log RequestLog) error {
	if strings.TrimSpace(log.ID) == "" {
		return fmt.Errorf("request log id required")
	}
	if strings.TrimSpace(log.Status) == "" {
		log.Status = RequestStatusStarted
	}
	if log.CreatedAt.IsZero() {
		log.CreatedAt = time.Now().UTC()
	}
	var finished any
	if log.FinishedAt != nil {
		finished = formatTime(*log.FinishedAt)
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO request_logs (
  id, created_at, finished_at, stream, status, requested_model, mapped_model, account_id,
  prompt_tokens, completion_tokens, cache_read_tokens, cache_write_tokens, usage_source, credits,
  latency_ms, ttfb_ms, error_kind, error_code, error_message, attempt_count
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		log.ID, formatTime(log.CreatedAt), finished, boolToInt(log.Stream), log.Status,
		log.RequestedModel, log.MappedModel, nullIfEmpty(log.AccountID),
		nullableInt(log.PromptTokens), nullableInt(log.CompletionTokens),
		nullableInt(log.CacheReadTokens), nullableInt(log.CacheWriteTokens),
		log.UsageSource, nullableFloat(log.Credits), nullableInt(log.LatencyMs), nullableInt(log.TTFBMs),
		log.ErrorKind, log.ErrorCode, log.ErrorMessage, log.AttemptCount,
	)
	if err != nil {
		return fmt.Errorf("insert request log: %w", err)
	}
	return nil
}

func (s *Store) UpdateRequestLog(ctx context.Context, log RequestLog) error {
	if strings.TrimSpace(log.ID) == "" {
		return fmt.Errorf("request log id required")
	}
	var finished any
	if log.FinishedAt != nil {
		finished = formatTime(*log.FinishedAt)
	}
	result, err := s.db.ExecContext(ctx, `
UPDATE request_logs SET
  finished_at = ?, status = ?, requested_model = ?, mapped_model = ?, account_id = ?,
  prompt_tokens = ?, completion_tokens = ?, cache_read_tokens = ?, cache_write_tokens = ?,
  usage_source = ?, credits = ?, latency_ms = ?, ttfb_ms = ?,
  error_kind = ?, error_code = ?, error_message = ?, attempt_count = ?
WHERE id = ?`,
		finished, log.Status, log.RequestedModel, log.MappedModel, nullIfEmpty(log.AccountID),
		nullableInt(log.PromptTokens), nullableInt(log.CompletionTokens),
		nullableInt(log.CacheReadTokens), nullableInt(log.CacheWriteTokens),
		log.UsageSource, nullableFloat(log.Credits), nullableInt(log.LatencyMs), nullableInt(log.TTFBMs),
		log.ErrorKind, log.ErrorCode, log.ErrorMessage, log.AttemptCount, log.ID,
	)
	if err != nil {
		return fmt.Errorf("update request log: %w", err)
	}
	changed, _ := result.RowsAffected()
	if changed == 0 {
		return ErrRequestLogNotFound
	}
	return nil
}

func (s *Store) InsertRequestAttempt(ctx context.Context, attempt RequestAttempt) error {
	if strings.TrimSpace(attempt.ID) == "" {
		return fmt.Errorf("request attempt id required")
	}
	if strings.TrimSpace(attempt.RequestID) == "" {
		return fmt.Errorf("request id required")
	}
	if attempt.StartedAt.IsZero() {
		attempt.StartedAt = time.Now().UTC()
	}
	var finished any
	if attempt.FinishedAt != nil {
		finished = formatTime(*attempt.FinishedAt)
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO request_attempts (
  id, request_id, attempt_index, account_id, started_at, finished_at, status, http_status,
  error_kind, error_message, latency_ms, prompt_tokens, completion_tokens, usage_source
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		attempt.ID, attempt.RequestID, attempt.AttemptIndex, attempt.AccountID,
		formatTime(attempt.StartedAt), finished, attempt.Status, nullableInt(attempt.HTTPStatus),
		attempt.ErrorKind, attempt.ErrorMessage, nullableInt(attempt.LatencyMs),
		nullableInt(attempt.PromptTokens), nullableInt(attempt.CompletionTokens), attempt.UsageSource,
	)
	if err != nil {
		return fmt.Errorf("insert request attempt: %w", err)
	}
	return nil
}

func (s *Store) ListRequestLogs(ctx context.Context, filter RequestLogFilter) (RequestLogList, error) {
	limit := filter.Limit
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	offset := filter.Offset
	if offset < 0 {
		offset = 0
	}

	where, args := buildRequestLogWhere(filter)
	countQuery := "SELECT COUNT(*) FROM request_logs" + where
	var total int
	if err := s.db.QueryRowContext(ctx, countQuery, args...).Scan(&total); err != nil {
		return RequestLogList{}, fmt.Errorf("count request logs: %w", err)
	}

	query := `
SELECT id, created_at, finished_at, stream, status, requested_model, mapped_model, account_id,
       prompt_tokens, completion_tokens, cache_read_tokens, cache_write_tokens, usage_source, credits,
       latency_ms, ttfb_ms, error_kind, error_code, error_message, attempt_count
FROM request_logs` + where + ` ORDER BY created_at DESC, id DESC LIMIT ? OFFSET ?`
	args = append(args, limit, offset)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return RequestLogList{}, fmt.Errorf("list request logs: %w", err)
	}
	defer rows.Close()

	items := make([]RequestLog, 0, limit)
	for rows.Next() {
		item, err := scanRequestLog(rows)
		if err != nil {
			return RequestLogList{}, fmt.Errorf("scan request log: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return RequestLogList{}, err
	}
	return RequestLogList{Items: items, Total: total, Limit: limit, Offset: offset}, nil
}

func (s *Store) SummarizeRequestLogs(ctx context.Context, from, to time.Time) (RequestStats, error) {
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

	filter := RequestLogFilter{From: &from, To: &to}
	where, args := buildRequestLogWhere(filter)
	stats := RequestStats{
		Window:   RequestStatsWindow{From: from, To: to, Hours: hours},
		Status:   make([]RequestStatsBucket, 0),
		Errors:   make([]RequestStatsBucket, 0),
		Models:   make([]RequestStatsNamed, 0),
		Accounts: make([]RequestStatsNamed, 0),
		Series:   make([]RequestStatsPoint, 0, hours),
	}

	row := s.db.QueryRowContext(ctx, `
SELECT
  COUNT(*),
  COALESCE(SUM(CASE WHEN status = 'ok' THEN 1 ELSE 0 END), 0),
  COALESCE(SUM(CASE WHEN status = 'error' THEN 1 ELSE 0 END), 0),
  COALESCE(SUM(CASE WHEN status = 'canceled' THEN 1 ELSE 0 END), 0),
  COALESCE(SUM(CASE WHEN stream = 1 THEN 1 ELSE 0 END), 0),
  AVG(CASE WHEN latency_ms IS NOT NULL THEN latency_ms END),
  AVG(CASE WHEN ttfb_ms IS NOT NULL THEN ttfb_ms END),
  COALESCE(SUM(COALESCE(prompt_tokens, 0)), 0),
  COALESCE(SUM(COALESCE(completion_tokens, 0)), 0),
  COALESCE(SUM(COALESCE(cache_read_tokens, 0)), 0)
FROM request_logs`+where, args...)
	var avgLatency, avgTTFB sql.NullFloat64
	if err := row.Scan(
		&stats.Totals.Requests, &stats.Totals.OK, &stats.Totals.Error, &stats.Totals.Canceled, &stats.Totals.Streaming,
		&avgLatency, &avgTTFB, &stats.Tokens.Prompt, &stats.Tokens.Completion, &stats.Tokens.CacheRead,
	); err != nil {
		return RequestStats{}, fmt.Errorf("summarize request logs: %w", err)
	}
	stats.Tokens.Total = stats.Tokens.Prompt + stats.Tokens.Completion
	if stats.Totals.Requests > 0 {
		stats.Totals.SuccessRate = float64(stats.Totals.OK) / float64(stats.Totals.Requests)
	}
	stats.Latency.AvgMs = roundedNullInt(avgLatency)
	stats.Latency.TTFBAvgMs = roundedNullInt(avgTTFB)
	stats.Latency.P50Ms, stats.Latency.P95Ms = s.requestLatencyPercentiles(ctx, where, args)

	statusRows, err := s.db.QueryContext(ctx, `
SELECT status, COUNT(*) FROM request_logs`+where+` GROUP BY status ORDER BY COUNT(*) DESC, status ASC`, args...)
	if err != nil {
		return RequestStats{}, fmt.Errorf("summarize request status: %w", err)
	}
	stats.Status, err = scanCountBuckets(statusRows)
	if err != nil {
		return RequestStats{}, err
	}

	errorRows, err := s.db.QueryContext(ctx, `
SELECT error_kind, COUNT(*) FROM request_logs`+whereAnd(where, "error_kind <> ''")+`
GROUP BY error_kind ORDER BY COUNT(*) DESC, error_kind ASC LIMIT 8`, args...)
	if err != nil {
		return RequestStats{}, fmt.Errorf("summarize request errors: %w", err)
	}
	stats.Errors, err = scanCountBuckets(errorRows)
	if err != nil {
		return RequestStats{}, err
	}

	modelRows, err := s.db.QueryContext(ctx, `
SELECT COALESCE(NULLIF(requested_model, ''), '(unknown)'), COUNT(*),
       COALESCE(SUM(CASE WHEN status = 'ok' THEN 1 ELSE 0 END), 0),
       COALESCE(SUM(CASE WHEN status = 'error' THEN 1 ELSE 0 END), 0),
       AVG(CASE WHEN latency_ms IS NOT NULL THEN latency_ms END)
FROM request_logs`+where+` GROUP BY 1 ORDER BY COUNT(*) DESC, 1 ASC LIMIT 6`, args...)
	if err != nil {
		return RequestStats{}, fmt.Errorf("summarize request models: %w", err)
	}
	stats.Models, err = scanNamedBuckets(modelRows)
	if err != nil {
		return RequestStats{}, err
	}

	accountRows, err := s.db.QueryContext(ctx, `
SELECT COALESCE(NULLIF(account_id, ''), '(unassigned)'), COUNT(*),
       COALESCE(SUM(CASE WHEN status = 'ok' THEN 1 ELSE 0 END), 0),
       COALESCE(SUM(CASE WHEN status = 'error' THEN 1 ELSE 0 END), 0),
       AVG(CASE WHEN latency_ms IS NOT NULL THEN latency_ms END)
FROM request_logs`+where+` GROUP BY 1 ORDER BY COUNT(*) DESC, 1 ASC LIMIT 6`, args...)
	if err != nil {
		return RequestStats{}, fmt.Errorf("summarize request accounts: %w", err)
	}
	stats.Accounts, err = scanNamedBuckets(accountRows)
	if err != nil {
		return RequestStats{}, err
	}

	series, err := s.requestSeries(ctx, from, to, where, args)
	if err != nil {
		return RequestStats{}, err
	}
	stats.Series = series
	return stats, nil
}

func (s *Store) requestLatencyPercentiles(ctx context.Context, where string, args []any) (*int, *int) {
	rows, err := s.db.QueryContext(ctx, `SELECT latency_ms FROM request_logs`+whereAnd(where, "latency_ms IS NOT NULL")+` ORDER BY latency_ms ASC`, args...)
	if err != nil {
		return nil, nil
	}
	defer rows.Close()
	values := make([]int, 0, 128)
	for rows.Next() {
		var value int
		if err := rows.Scan(&value); err != nil {
			return nil, nil
		}
		values = append(values, value)
	}
	if len(values) == 0 {
		return nil, nil
	}
	return roundedInt(percentileNearestRank(values, 50)), roundedInt(percentileNearestRank(values, 95))
}

func (s *Store) requestSeries(ctx context.Context, from, to time.Time, where string, args []any) ([]RequestStatsPoint, error) {
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
	points := make([]RequestStatsPoint, 0)
	index := map[string]int{}
	for cursor := trunc; cursor.Before(end); cursor = cursor.Add(step) {
		index[seriesKey(cursor, daily, quarter)] = len(points)
		points = append(points, RequestStatsPoint{At: cursor.UTC()})
	}
	if len(points) == 0 {
		return points, nil
	}

	expr := "substr(created_at, 1, 13)"
	if daily {
		expr = "substr(created_at, 1, 10)"
	} else if quarter {
		expr = "substr(created_at, 1, 14) || printf('%02d', (CAST(substr(created_at, 15, 2) AS INTEGER) / 15) * 15)"
	}
	rows, err := s.db.QueryContext(ctx, fmt.Sprintf(`
SELECT %s, COUNT(*),
       COALESCE(SUM(CASE WHEN status = 'ok' THEN 1 ELSE 0 END), 0),
       COALESCE(SUM(CASE WHEN status = 'error' THEN 1 ELSE 0 END), 0)
FROM request_logs%s GROUP BY 1 ORDER BY 1 ASC`, expr, where), args...)
	if err != nil {
		return nil, fmt.Errorf("summarize request series: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var bucket string
		var requests, okCount, errorCount int
		if err := rows.Scan(&bucket, &requests, &okCount, &errorCount); err != nil {
			return nil, fmt.Errorf("scan request series: %w", err)
		}
		if i, exists := index[normalizeSeriesBucket(bucket, daily, quarter)]; exists {
			points[i].Requests = requests
			points[i].OK = okCount
			points[i].Error = errorCount
		}
	}
	return points, rows.Err()
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

func normalizeSeriesBucket(bucket string, daily, quarter bool) string {
	text := strings.TrimSpace(bucket)
	if daily {
		if len(text) >= 10 {
			return text[:10]
		}
		return text
	}
	if quarter {
		if len(text) >= 16 {
			parsed, err := time.Parse("2006-01-02T15:04", text[:16])
			if err == nil {
				return parsed.UTC().Truncate(15 * time.Minute).Format("2006-01-02T15:04")
			}
			return text[:16]
		}
		return text
	}
	if len(text) >= 13 {
		return text[:13]
	}
	return text
}

func scanCountBuckets(rows *sql.Rows) ([]RequestStatsBucket, error) {
	defer rows.Close()
	items := make([]RequestStatsBucket, 0)
	for rows.Next() {
		var item RequestStatsBucket
		if err := rows.Scan(&item.Key, &item.Count); err != nil {
			return nil, fmt.Errorf("scan request bucket: %w", err)
		}
		if strings.TrimSpace(item.Key) == "" {
			continue
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func scanNamedBuckets(rows *sql.Rows) ([]RequestStatsNamed, error) {
	defer rows.Close()
	items := make([]RequestStatsNamed, 0)
	for rows.Next() {
		var item RequestStatsNamed
		var avg sql.NullFloat64
		if err := rows.Scan(&item.Key, &item.Count, &item.OK, &item.Error, &avg); err != nil {
			return nil, fmt.Errorf("scan named request bucket: %w", err)
		}
		item.LatencyAvgMs = roundedNullInt(avg)
		items = append(items, item)
	}
	return items, rows.Err()
}

func whereAnd(where, extra string) string {
	if strings.TrimSpace(where) == "" {
		return " WHERE " + extra
	}
	return where + " AND " + extra
}

func roundedNullInt(value sql.NullFloat64) *int {
	if !value.Valid {
		return nil
	}
	rounded := int(value.Float64 + 0.5)
	return &rounded
}

func roundedInt(value int) *int {
	return &value
}

func percentileNearestRank(sorted []int, percentile int) int {
	if len(sorted) == 0 {
		return 0
	}
	if percentile <= 0 {
		return sorted[0]
	}
	if percentile >= 100 {
		return sorted[len(sorted)-1]
	}
	rank := int((float64(percentile) / 100) * float64(len(sorted)))
	if rank >= len(sorted) {
		rank = len(sorted) - 1
	}
	return sorted[rank]
}

func (s *Store) GetRequestLog(ctx context.Context, id string) (RequestLog, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT id, created_at, finished_at, stream, status, requested_model, mapped_model, account_id,
       prompt_tokens, completion_tokens, cache_read_tokens, cache_write_tokens, usage_source, credits,
       latency_ms, ttfb_ms, error_kind, error_code, error_message, attempt_count
FROM request_logs WHERE id = ?`, strings.TrimSpace(id))
	log, err := scanRequestLog(row)
	if errors.Is(err, sql.ErrNoRows) {
		return RequestLog{}, ErrRequestLogNotFound
	}
	if err != nil {
		return RequestLog{}, fmt.Errorf("get request log: %w", err)
	}
	attempts, err := s.listRequestAttempts(ctx, log.ID)
	if err != nil {
		return RequestLog{}, err
	}
	log.Attempts = attempts
	return log, nil
}

func (s *Store) listRequestAttempts(ctx context.Context, requestID string) ([]RequestAttempt, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id, request_id, attempt_index, account_id, started_at, finished_at, status, http_status,
       error_kind, error_message, latency_ms, prompt_tokens, completion_tokens, usage_source
FROM request_attempts WHERE request_id = ? ORDER BY attempt_index ASC, started_at ASC`, requestID)
	if err != nil {
		return nil, fmt.Errorf("list request attempts: %w", err)
	}
	defer rows.Close()
	items := make([]RequestAttempt, 0)
	for rows.Next() {
		item, err := scanRequestAttempt(rows)
		if err != nil {
			return nil, fmt.Errorf("scan request attempt: %w", err)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) ClearRequestLogs(ctx context.Context) (int64, error) {
	result, err := s.db.ExecContext(ctx, `DELETE FROM request_logs`)
	if err != nil {
		return 0, fmt.Errorf("clear request logs: %w", err)
	}
	changed, _ := result.RowsAffected()
	return changed, nil
}

func (s *Store) PurgeRequestLogs(ctx context.Context, olderThan time.Duration, maxRows int) (int64, error) {
	if olderThan <= 0 {
		olderThan = 7 * 24 * time.Hour
	}
	if maxRows <= 0 {
		maxRows = 20_000
	}
	cutoff := formatTime(time.Now().UTC().Add(-olderThan))
	result, err := s.db.ExecContext(ctx, `DELETE FROM request_logs WHERE created_at < ?`, cutoff)
	if err != nil {
		return 0, fmt.Errorf("purge old request logs: %w", err)
	}
	deleted, _ := result.RowsAffected()

	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM request_logs`).Scan(&total); err != nil {
		return deleted, fmt.Errorf("count request logs for cap: %w", err)
	}
	if total <= maxRows {
		return deleted, nil
	}
	overflow := total - maxRows
	result, err = s.db.ExecContext(ctx, `
DELETE FROM request_logs WHERE id IN (
  SELECT id FROM request_logs ORDER BY created_at ASC, id ASC LIMIT ?
)`, overflow)
	if err != nil {
		return deleted, fmt.Errorf("purge excess request logs: %w", err)
	}
	extra, _ := result.RowsAffected()
	return deleted + extra, nil
}

func buildRequestLogWhere(filter RequestLogFilter) (string, []any) {
	clauses := make([]string, 0, 8)
	args := make([]any, 0, 8)
	if account := strings.TrimSpace(filter.AccountID); account != "" {
		clauses = append(clauses, "account_id = ?")
		args = append(args, account)
	}
	if status := strings.TrimSpace(filter.Status); status != "" {
		clauses = append(clauses, "status = ?")
		args = append(args, status)
	}
	if filter.Stream != nil {
		clauses = append(clauses, "stream = ?")
		args = append(args, boolToInt(*filter.Stream))
	}
	if kind := strings.TrimSpace(filter.ErrorKind); kind != "" {
		clauses = append(clauses, "error_kind = ?")
		args = append(args, kind)
	}
	if model := strings.TrimSpace(filter.Model); model != "" {
		clauses = append(clauses, "(requested_model = ? OR mapped_model = ?)")
		args = append(args, model, model)
	}
	if query := strings.TrimSpace(filter.Query); query != "" {
		like := "%" + query + "%"
		clauses = append(clauses, "(id LIKE ? OR requested_model LIKE ? OR mapped_model LIKE ? OR account_id LIKE ? OR error_message LIKE ?)")
		args = append(args, like, like, like, like, like)
	}
	if filter.From != nil && !filter.From.IsZero() {
		clauses = append(clauses, "substr(created_at, 1, 19) >= ?")
		args = append(args, filter.From.UTC().Format("2006-01-02T15:04:05"))
	}
	if filter.To != nil && !filter.To.IsZero() {
		clauses = append(clauses, "substr(created_at, 1, 19) <= ?")
		args = append(args, filter.To.UTC().Format("2006-01-02T15:04:05"))
	}
	if len(clauses) == 0 {
		return "", args
	}
	return " WHERE " + strings.Join(clauses, " AND "), args
}

func scanRequestLog(row rowScanner) (RequestLog, error) {
	var (
		log                   RequestLog
		finished, accountID   sql.NullString
		stream                int
		prompt, completion    sql.NullInt64
		cacheRead, cacheWrite sql.NullInt64
		credits               sql.NullFloat64
		latency, ttfb         sql.NullInt64
		created               string
	)
	err := row.Scan(
		&log.ID, &created, &finished, &stream, &log.Status, &log.RequestedModel, &log.MappedModel, &accountID,
		&prompt, &completion, &cacheRead, &cacheWrite, &log.UsageSource, &credits,
		&latency, &ttfb, &log.ErrorKind, &log.ErrorCode, &log.ErrorMessage, &log.AttemptCount,
	)
	if err != nil {
		return RequestLog{}, err
	}
	log.CreatedAt = parseTime(created)
	log.Stream = stream != 0
	if finished.Valid && finished.String != "" {
		parsed := parseTime(finished.String)
		log.FinishedAt = &parsed
	}
	if accountID.Valid {
		log.AccountID = accountID.String
	}
	log.PromptTokens = nullIntPtr(prompt)
	log.CompletionTokens = nullIntPtr(completion)
	log.CacheReadTokens = nullIntPtr(cacheRead)
	log.CacheWriteTokens = nullIntPtr(cacheWrite)
	log.LatencyMs = nullIntPtr(latency)
	log.TTFBMs = nullIntPtr(ttfb)
	if credits.Valid {
		value := credits.Float64
		log.Credits = &value
	}
	return log, nil
}

func scanRequestAttempt(row rowScanner) (RequestAttempt, error) {
	var (
		attempt                     RequestAttempt
		finished                    sql.NullString
		httpStatus                  sql.NullInt64
		latency, prompt, completion sql.NullInt64
		started                     string
	)
	err := row.Scan(
		&attempt.ID, &attempt.RequestID, &attempt.AttemptIndex, &attempt.AccountID, &started, &finished,
		&attempt.Status, &httpStatus, &attempt.ErrorKind, &attempt.ErrorMessage, &latency, &prompt, &completion, &attempt.UsageSource,
	)
	if err != nil {
		return RequestAttempt{}, err
	}
	attempt.StartedAt = parseTime(started)
	if finished.Valid && finished.String != "" {
		parsed := parseTime(finished.String)
		attempt.FinishedAt = &parsed
	}
	attempt.HTTPStatus = nullIntPtr(httpStatus)
	attempt.LatencyMs = nullIntPtr(latency)
	attempt.PromptTokens = nullIntPtr(prompt)
	attempt.CompletionTokens = nullIntPtr(completion)
	return attempt, nil
}

func boolToInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func nullIfEmpty(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}

func nullableInt(value *int) any {
	if value == nil {
		return nil
	}
	return *value
}

func nullableFloat(value *float64) any {
	if value == nil {
		return nil
	}
	return *value
}

func nullIntPtr(value sql.NullInt64) *int {
	if !value.Valid {
		return nil
	}
	converted := int(value.Int64)
	return &converted
}
