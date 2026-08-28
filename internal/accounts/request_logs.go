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
