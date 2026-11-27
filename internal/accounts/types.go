package accounts

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

var ErrAccountNotFound = errors.New("account not found")
var ErrSecretNotFound = errors.New("secret not found")
var ErrAPIKeyNotFound = errors.New("api key not found")
var ErrAggregationNotFound = errors.New("aggregation not found")

const (
	DefaultWorkBuddyCheckinTime = "09:00"
	WorkBuddyCheckinTimeSecret  = "workbuddy_checkin_time"
)

type Account struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	RemoteUID      string `json:"remote_uid,omitempty"`
	Provider       string `json:"provider"`
	ProviderRegion string `json:"region"`
	AuthType       string `json:"auth_type"`
	Enabled        bool   `json:"enabled"`
	MaxInFlight    int    `json:"max_inflight"`
	Priority       int    `json:"priority"`
	// CreditFloor is the per-account credit floor override. Nil means "not
	// set" and defers to the global CLI2API_CREDIT_FLOOR; a non-nil value
	// (including 0, which disables the floor for this account) overrides it.
	// No omitempty: an explicit 0 must survive API responses so a caller can
	// distinguish "disable this account's floor" from "inherit the global floor".
	CreditFloor *float64 `json:"credit_floor"`
	// PercentFloor is the per-account floor for percent-denominated quota.
	// Nil defers to the global CLI2API_PERCENT_FLOOR; a non-nil value (including
	// 0, which disables the percent floor for this account) overrides it.
	PercentFloor *float64 `json:"percent_floor"`
	// DropSystemPrompt drops caller system prompts before provider-native chat.
	DropSystemPrompt bool `json:"drop_system_prompt"`
	// ModelAliases maps a client-facing name to this account's real catalog
	// model. Routing matches the alias; the request is rewritten to the real
	// model only after this account is picked.
	ModelAliases map[string]string `json:"model_aliases,omitempty"`
	// EnabledModels limits which catalog models this account may serve.
	// ModelsRestricted reports whether the operator saved a list; an empty
	// list then serves nothing, while an unset list serves the catalog.
	EnabledModels    []string `json:"enabled_models,omitempty"`
	ModelsRestricted bool     `json:"models_restricted,omitempty"`
	// WorkBuddyAutoCheckin opts into scheduled daily check-in (default off).
	WorkBuddyAutoCheckin bool `json:"workbuddy_auto_checkin"`
	// WorkBuddyCheckinTime is the process-local daily check-in time.
	WorkBuddyCheckinTime string `json:"workbuddy_checkin_time"`
	AutoCheckin          bool   `json:"auto_checkin"`
	CheckinTime          string `json:"checkin_time"`
	ProxyURL             string `json:"-"`
	// LastCheckin* are display-only WorkBuddy ops results.
	LastCheckinAt     string `json:"last_checkin_at,omitempty"`
	LastCheckinMsg    string `json:"last_checkin_msg,omitempty"`
	LastCheckinStatus string `json:"last_checkin_status,omitempty"`
	// LastCheckinSource is "local" for a real check-in attempt and "upstream"
	// for a status-only read. LastCheckinSyncedAt is when the upstream status
	// was last verified, independent of when the check-in happened.
	LastCheckinSource   string `json:"last_checkin_source,omitempty"`
	LastCheckinSyncedAt string `json:"last_checkin_synced_at,omitempty"`
	// CheckinAutoTime is the upstream daily window start + 1 hour, stored as
	// HH:MM in the provider timezone. Empty means use the provider fallback.
	CheckinAutoTime string         `json:"checkin_auto_time,omitempty"`
	Status          string         `json:"status"`
	LastError       string         `json:"last_error,omitempty"`
	LastErrorKind   string         `json:"last_error_kind,omitempty"`
	CooldownUntil   *time.Time     `json:"cooldown_until,omitempty"`
	Quota           *QuotaSnapshot `json:"-"`
	CreatedAt       time.Time      `json:"created_at"`
	UpdatedAt       time.Time      `json:"updated_at"`
}

type CheckinAllResult struct {
	AccountID string `json:"account_id"`
	Provider  string `json:"provider"`
	Region    string `json:"region"`
	Status    string `json:"status"`
	Message   string `json:"message,omitempty"`
	// RewardCredits is the real upstream grant amount reported by the provider
	// claim response. Zero/omitted means the provider did not report an amount.
	RewardCredits float64 `json:"reward_credits,omitempty"`
	// Code and Kind carry the real upstream error classification when the
	// check-in failed; they are never synthesized.
	Code       string `json:"code,omitempty"`
	Kind       string `json:"kind,omitempty"`
	HTTPStatus int    `json:"http_status,omitempty"`
	// QuotaRefreshed reports whether the post-check-in upstream quota fetch
	// completed and persisted a real snapshot.
	QuotaRefreshed bool `json:"quota_refreshed,omitempty"`
}

// AccountRefreshResult is the per-account outcome of a bulk refresh pass.
// Every field reflects the real probe/quota outcome; Message carries the
// upstream or probe error verbatim when OK is false.
type AccountRefreshResult struct {
	AccountID  string         `json:"account_id"`
	Provider   string         `json:"provider"`
	Region     string         `json:"region"`
	OK         bool           `json:"ok"`
	Status     string         `json:"status,omitempty"`
	Code       string         `json:"code,omitempty"`
	Kind       string         `json:"kind,omitempty"`
	Message    string         `json:"message,omitempty"`
	HTTPStatus int            `json:"http_status,omitempty"`
	Quota      *QuotaSnapshot `json:"quota,omitempty"`
}

// CheckinStatusUpdate persists an upstream status read. CheckinAt is only set
// when upstream confirms today's check-in; AutoTime is the derived HH:MM slot.
type CheckinStatusUpdate struct {
	Status    string
	Message   string
	Source    string
	CheckinAt string
	AutoTime  string
	SyncedAt  time.Time
}

type CreateAccount struct {
	AutoCheckin          *bool
	CheckinTime          string
	Name                 string
	Provider             string
	Region               string
	Enabled              bool
	MaxInFlight          int
	Priority             int
	CreditFloor          *float64
	PercentFloor         *float64
	DropSystemPrompt     *bool
	WorkBuddyAutoCheckin *bool
	WorkBuddyCheckinTime string
	ProxyURL             string
	ModelAliases         map[string]string
	EnabledModels        []string
	SetModelRouting      bool
}

type UpdateAccount struct {
	AutoCheckin *bool
	CheckinTime *string
	Name        string
	Enabled     *bool
	MaxInFlight *int
	Priority    *int
	// CreditFloor overrides the global floor when non-nil. A nil pointer
	// leaves the stored value unchanged on PATCH; a non-nil value (including
	// 0, which disables the floor for this account) is stored as the override.
	CreditFloor  *float64
	PercentFloor *float64
	// SetCreditFloor/SetPercentFloor let a caller explicitly clear an override
	// (store NULL) while leaving PATCH fields absent untouched. Without these,
	// a JSON null would be indistinguishable from an omitted field in *float64.
	SetCreditFloor       bool
	SetPercentFloor      bool
	DropSystemPrompt     *bool
	WorkBuddyAutoCheckin *bool
	WorkBuddyCheckinTime *string
	ProxyURL             *string
	// ModelAliases and EnabledModels replace the stored routing when
	// SetModelRouting is true. Nil aliases clear the map; nil enabled models
	// mean the account can serve its whole catalog.
	ModelAliases    map[string]string
	EnabledModels   []string
	SetModelRouting bool
}

type NativeCredential struct {
	UserBlob  []byte `json:"-"`
	MachineID string `json:"machine_id"`
}

type ImportAccount struct {
	AutoCheckin          *bool
	CheckinTime          string
	Name                 string
	Provider             string
	Region               string
	Enabled              bool
	MaxInFlight          int
	Priority             int
	CreditFloor          *float64
	PercentFloor         *float64
	DropSystemPrompt     *bool
	WorkBuddyAutoCheckin *bool
	WorkBuddyCheckinTime string
	ProxyURL             string
	Credential           NativeCredential
}

type AccountView struct {
	Account
	Ready    bool `json:"ready"`
	Hot      bool `json:"hot"`
	InFlight int  `json:"in_flight"`
	// InFlightSince is RFC3339 when the pool slot went busy (0→1); empty when idle.
	InFlightSince       string            `json:"in_flight_since,omitempty"`
	Restarts            int               `json:"restarts"`
	RuntimeState        string            `json:"runtime_state,omitempty"`
	NextRestartAt       string            `json:"next_restart_at,omitempty"`
	RestartBackoffLevel int               `json:"restart_backoff_level,omitempty"`
	DownUntil           string            `json:"down_until,omitempty"`
	ModelCooldowns      map[string]string `json:"model_cooldowns,omitempty"`
	Quota               *QuotaSnapshot    `json:"quota,omitempty"`
	// QuotaError carries the live upstream quota-read failure of a console
	// page load. When it is set, Quota is nil on purpose: a stale snapshot must
	// never be presented as the current balance.
	QuotaError string `json:"quota_error,omitempty"`
	ProxyURL   string `json:"proxy_url,omitempty"`
	// ForceReadyOverride marks the local/dev force-ready marker active for
	// this account: routing ignores the hard-zero balance, but Quota above
	// still shows the real upstream snapshot.
	ForceReadyOverride bool `json:"force_ready_override,omitempty"`
}

type ProviderModelSetting struct {
	MaxMode         bool
	ReasoningEffort string
}

type CheckinRecord struct {
	ID        string    `json:"id"`
	AccountID string    `json:"account_id"`
	Status    string    `json:"status"`
	Message   string    `json:"message"`
	CreatedAt time.Time `json:"created_at"`
}

// CooldownRow is one persisted cooldown: account-wide when Model is empty,
// scoped to a single canonical model otherwise. ModelKind holds the
// per-model previous failure kind (empty for the account-wide row) so the
// backoff ladder can resume after a restart without cross-model confusion.
type CooldownRow struct {
	AccountID    string
	Model        string
	DownUntil    time.Time
	BackoffLevel int
	Kind         string
	Message      string
	ModelKind    string
}

func NormalizeWorkBuddyCheckinTime(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return DefaultWorkBuddyCheckinTime, nil
	}
	parsed, err := time.Parse("15:04", value)
	if err != nil || parsed.Format("15:04") != value {
		return "", fmt.Errorf("workbuddy_checkin_time must use HH:mm")
	}
	return value, nil
}
