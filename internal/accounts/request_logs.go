package accounts

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"time"
)

const (
	RequestStatusStarted    = "started"
	RequestStatusStreaming  = "streaming"
	RequestStatusOK         = "ok"
	RequestStatusIncomplete = "incomplete"
	RequestStatusError      = "error"
	RequestStatusCanceled   = "canceled"

	AttemptStatusStarted  = "started"
	AttemptStatusOK       = "ok"
	AttemptStatusError    = "error"
	AttemptStatusFailover = "failover"
)

var ErrRequestLogNotFound = errors.New("request log not found")

type RequestLog struct {
	ID                  string                   `json:"id"`
	CreatedAt           time.Time                `json:"created_at"`
	FinishedAt          *time.Time               `json:"finished_at,omitempty"`
	Stream              bool                     `json:"stream"`
	Status              string                   `json:"status"`
	RequestedModel      string                   `json:"requested_model"`
	MappedModel         string                   `json:"mapped_model,omitempty"`
	RequestedReasoning  string                   `json:"requested_reasoning,omitempty"`
	ResolvedReasoning   string                   `json:"resolved_reasoning,omitempty"`
	AccountID           string                   `json:"account_id,omitempty"`
	APIKeyID            string                   `json:"api_key_id,omitempty"` // console API key id; "" for console-key traffic and legacy rows
	Provider            string                   `json:"provider,omitempty"`
	Routing             string                   `json:"routing,omitempty"`
	PromptTokens        *int                     `json:"prompt_tokens,omitempty"`
	CompletionTokens    *int                     `json:"completion_tokens,omitempty"`
	CacheReadTokens     *int                     `json:"cache_read_tokens,omitempty"`
	CacheWriteTokens    *int                     `json:"cache_write_tokens,omitempty"`
	UsageSource         string                   `json:"usage_source,omitempty"`
	Credits             *float64                 `json:"credits,omitempty"`
	LatencyMs           *int                     `json:"latency_ms,omitempty"`
	TTFBMs              *int                     `json:"ttfb_ms,omitempty"`
	ErrorKind           string                   `json:"error_kind,omitempty"`
	ErrorCode           string                   `json:"error_code,omitempty"`
	ErrorMessage        string                   `json:"error_message,omitempty"`
	AttemptCount        int                      `json:"attempt_count"`
	MessageCount        int                      `json:"message_count,omitempty"`
	EmptyMessageIndexes []int                    `json:"empty_message_indexes,omitempty"`
	MessageRoles        []string                 `json:"message_roles,omitempty"`
	Attempts            []RequestAttempt         `json:"attempts,omitempty"`
	StreamDiagnostic    *RequestStreamDiagnostic `json:"stream_diagnostic,omitempty"`
	UsageDetail         *RequestUsageDetail      `json:"usage_detail,omitempty"`
}

type RequestUsageDetail struct {
	RequestID string    `json:"request_id"`
	CreatedAt time.Time `json:"created_at"`
	Provider  string    `json:"provider,omitempty"`
	Credit    *float64  `json:"credit,omitempty"`
	Unit      string    `json:"unit,omitempty"`
}

type RequestStreamDiagnostic struct {
	RequestID          string     `json:"request_id"`
	CreatedAt          time.Time  `json:"created_at"`
	FinishedAt         *time.Time `json:"finished_at,omitempty"`
	UpstreamStatus     *int       `json:"upstream_status,omitempty"`
	UpstreamRequestID  string     `json:"upstream_request_id,omitempty"`
	ContextErr         string     `json:"context_err,omitempty"`
	CancellationSource string     `json:"cancellation_source,omitempty"`
	RelayError         string     `json:"relay_error,omitempty"`
	SSEEventCount      int        `json:"sse_event_count"`
	BytesRead          int64      `json:"bytes_read"`
	ContentLength      int        `json:"content_length"`
	LastEvent          string     `json:"last_event,omitempty"`
	SawDone            bool       `json:"saw_done"`
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
	// APIKeyID narrows the window to requests issued by one console API key.
	APIKeyID  string
	Status    string
	Stream    *bool
	ErrorKind string
	Model     string
	// Provider matches the logged provider, falling back to the account's
	// provider for rows written before provider was recorded.
	Provider string
	ID       string
	Query    string
	From     *time.Time
	To       *time.Time
	Limit    int
	Offset   int
}

type RequestLogList struct {
	Items  []RequestLog `json:"items"`
	Total  int          `json:"total"`
	Limit  int          `json:"limit"`
	Offset int          `json:"offset"`
}

type RequestStats struct {
	Window    RequestStatsWindow   `json:"window"`
	Totals    RequestStatsTotals   `json:"totals"`
	Latency   RequestStatsLatency  `json:"latency"`
	Tokens    RequestStatsTokens   `json:"tokens"`
	Status    []RequestStatsBucket `json:"status"`
	Errors    []RequestStatsBucket `json:"errors"`
	Models    []RequestStatsNamed  `json:"models"`
	Accounts  []RequestStatsNamed  `json:"accounts"`
	Providers []RequestStatsNamed  `json:"providers"`
	Series    []RequestStatsPoint  `json:"series"`
}

type RequestStatsWindow struct {
	From  time.Time `json:"from"`
	To    time.Time `json:"to"`
	Hours int       `json:"hours"`
}

type RequestStatsTotals struct {
	Requests    int     `json:"requests"`
	OK          int     `json:"ok"`
	Incomplete  int     `json:"incomplete"`
	Error       int     `json:"error"`
	Canceled    int     `json:"canceled"`
	Streaming   int     `json:"streaming"`
	SuccessRate float64 `json:"success_rate"`
	Credits     float64 `json:"credits"`
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
	CacheWrite int64 `json:"cache_write"`
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
	LatencyP95Ms *int   `json:"latency_p95_ms,omitempty"`
	// Usage totals for the group.
	PromptTokens     int64   `json:"prompt_tokens"`
	CompletionTokens int64   `json:"completion_tokens"`
	CacheReadTokens  int64   `json:"cache_read_tokens"`
	CacheWriteTokens int64   `json:"cache_write_tokens"`
	Credits          float64 `json:"credits"`
}

type RequestStatsPoint struct {
	At       time.Time `json:"at"`
	Requests int       `json:"requests"`
	OK       int       `json:"ok"`
	Error    int       `json:"error"`
	// Per-bucket usage and latency for the traffic chart's metric switch.
	PromptTokens     int64 `json:"prompt_tokens"`
	CompletionTokens int64 `json:"completion_tokens"`
	CacheReadTokens  int64 `json:"cache_read_tokens"`
	LatencyP50Ms     *int  `json:"latency_p50_ms,omitempty"`
	LatencyP95Ms     *int  `json:"latency_p95_ms,omitempty"`
}

// Stats grouping dimensions accepted by RequestStatsOptions.Dims.
const (
	RequestStatsDimModel    = "model"
	RequestStatsDimAccount  = "account"
	RequestStatsDimProvider = "provider"

	// RequestStatsDefaultLimit is the per-dimension row cap when no limit is
	// given (the overview rankings); RequestStatsMaxLimit bounds any request.
	RequestStatsDefaultLimit = 6
	RequestStatsMaxLimit     = 500
)

// RequestStatsOptions selects the window, which grouped lists to compute and
// how many rows each may return. Empty Dims means all three dimensions.
// APIKeyID narrows every aggregate to one console API key, so the usage view
// can report a single key's traffic; empty means all keys.
type RequestStatsOptions struct {
	From     time.Time
	To       time.Time
	Dims     []string
	Limit    int
	APIKeyID string
}

// WantsDim reports whether dim's grouped list should be computed.
func (o RequestStatsOptions) WantsDim(dim string) bool {
	if len(o.Dims) == 0 {
		return true
	}
	for _, d := range o.Dims {
		if d == dim {
			return true
		}
	}
	return false
}

// EffectiveLimit clamps Limit to [1, RequestStatsMaxLimit], defaulting to
// RequestStatsDefaultLimit.
func (o RequestStatsOptions) EffectiveLimit() int {
	if o.Limit <= 0 {
		return RequestStatsDefaultLimit
	}
	if o.Limit > RequestStatsMaxLimit {
		return RequestStatsMaxLimit
	}
	return o.Limit
}

// RequestUsage is the token / credit breakdown behind the Overview usage
// panels. It aggregates request_logs (tokens, prefix-cache hits, latency,
// credits) into per-account / per-model / per-realm rows plus two credit
// ledgers. It is a superset of RequestStats aimed at the "usage detail" and
// "credit deduction history" views rather than the traffic chart.
type RequestUsage struct {
	Window          RequestStatsWindow      `json:"window"`
	Totals          RequestUsageTotals      `json:"totals"`
	ByAccount       []RequestUsageRow       `json:"by_account"`
	ByModel         []RequestUsageRow       `json:"by_model"`
	ByRealm         []RequestUsageRow       `json:"by_realm"`
	CreditByAccount []RequestUsageCreditRow `json:"credit_by_account"`
	CreditByModel   []RequestUsageCreditRow `json:"credit_by_model"`
	// Series is retained for JSON compatibility but left empty; Overview
	// traffic uses /api/logs/stats series instead.
	Series []RequestStatsPoint `json:"series"`
}

type RequestUsageTotals struct {
	TotalTokens        int64    `json:"total_tokens"`
	PromptTokens       int64    `json:"prompt_tokens"`
	CompletionTokens   int64    `json:"completion_tokens"`
	CacheHitTokens     int64    `json:"cache_hit_tokens"`
	CacheMissTokens    int64    `json:"cache_miss_tokens"`
	Requests           int      `json:"requests"`
	Errors             int      `json:"errors"`
	AvgLatencyMs       *int     `json:"avg_latency_ms,omitempty"`
	AvgTokensPerSecond *float64 `json:"avg_tokens_per_second,omitempty"`
	Credits            float64  `json:"credits"`
	CreditTokens       int64    `json:"credit_tokens"`
	CreditSamples      int      `json:"credit_samples"`
	CreditsPer1MTokens *float64 `json:"credits_per_1m_tokens,omitempty"`
	SuccessRate        float64  `json:"success_rate"`
}

type RequestUsageRow struct {
	Key                string   `json:"key"`
	Realm              string   `json:"realm,omitempty"`
	Extra              string   `json:"extra,omitempty"`
	TotalTokens        int64    `json:"total_tokens"`
	PromptTokens       int64    `json:"prompt_tokens"`
	CompletionTokens   int64    `json:"completion_tokens"`
	CacheHitTokens     int64    `json:"cache_hit_tokens"`
	CacheMissTokens    int64    `json:"cache_miss_tokens"`
	Requests           int      `json:"requests"`
	Errors             int      `json:"errors"`
	AvgLatencyMs       *int     `json:"avg_latency_ms,omitempty"`
	AvgTokensPerSecond *float64 `json:"avg_tokens_per_second,omitempty"`
}

type RequestUsageCreditRow struct {
	Key                string   `json:"key"`
	Nickname           string   `json:"nickname,omitempty"`
	Realm              string   `json:"realm,omitempty"`
	Rate               *float64 `json:"rate,omitempty"`
	Requests           int      `json:"requests"`
	CreditSamples      int      `json:"credit_samples"`
	Credits            float64  `json:"credits"`
	CreditTokens       int64    `json:"credit_tokens"`
	CreditsPer1MTokens *float64 `json:"credits_per_1m_tokens,omitempty"`
	CacheHitTokens     int64    `json:"cache_hit_tokens"`
	CacheMissTokens    int64    `json:"cache_miss_tokens"`
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
