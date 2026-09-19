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
	// DropSystemPrompt drops caller system prompts before provider-native chat.
	DropSystemPrompt bool `json:"drop_system_prompt"`
	// WorkBuddyAutoCheckin opts into scheduled daily check-in (default off).
	WorkBuddyAutoCheckin bool `json:"workbuddy_auto_checkin"`
	// WorkBuddyCheckinTime is the process-local daily check-in time.
	WorkBuddyCheckinTime string `json:"workbuddy_checkin_time"`
	AutoCheckin          bool   `json:"auto_checkin"`
	CheckinTime          string `json:"checkin_time"`
	ProxyURL             string `json:"-"`
	// LastCheckin* are display-only WorkBuddy ops results.
	LastCheckinAt     string         `json:"last_checkin_at,omitempty"`
	LastCheckinMsg    string         `json:"last_checkin_msg,omitempty"`
	LastCheckinStatus string         `json:"last_checkin_status,omitempty"`
	Status            string         `json:"status"`
	LastError         string         `json:"last_error,omitempty"`
	LastErrorKind     string         `json:"last_error_kind,omitempty"`
	CooldownUntil     *time.Time     `json:"cooldown_until,omitempty"`
	Quota             *QuotaSnapshot `json:"-"`
	CreatedAt         time.Time      `json:"created_at"`
	UpdatedAt         time.Time      `json:"updated_at"`
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
	DropSystemPrompt     *bool
	WorkBuddyAutoCheckin *bool
	WorkBuddyCheckinTime string
	ProxyURL             string
}

type UpdateAccount struct {
	AutoCheckin          *bool
	CheckinTime          *string
	Name                 string
	Enabled              *bool
	MaxInFlight          *int
	Priority             *int
	DropSystemPrompt     *bool
	WorkBuddyAutoCheckin *bool
	WorkBuddyCheckinTime *string
	ProxyURL             *string
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
	DropSystemPrompt     *bool
	WorkBuddyAutoCheckin *bool
	WorkBuddyCheckinTime string
	ProxyURL             string
	Credential           NativeCredential
}

type AccountView struct {
	Account
	Ready               bool              `json:"ready"`
	Hot                 bool              `json:"hot"`
	InFlight            int               `json:"in_flight"`
	Restarts            int               `json:"restarts"`
	RuntimeState        string            `json:"runtime_state,omitempty"`
	NextRestartAt       string            `json:"next_restart_at,omitempty"`
	RestartBackoffLevel int               `json:"restart_backoff_level,omitempty"`
	DownUntil           string            `json:"down_until,omitempty"`
	ModelCooldowns      map[string]string `json:"model_cooldowns,omitempty"`
	Quota               *QuotaSnapshot    `json:"quota,omitempty"`
	ProxyURL            string            `json:"proxy_url,omitempty"`
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
