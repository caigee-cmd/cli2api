package accounts

import (
	"context"
	"time"
)

// PoolState is the persistable account-wide slice of a live pool item.
// Store and AccountStore stay free of executor types; runtime maps Item onto
// this DTO before writing.
type PoolState struct {
	ID            string
	DownUntil     time.Time
	LastError     string
	LastErrorKind string
}

// PoolStateStore is the cooldown persistence surface used by the pool
// observer drainer. Pool and executor never see the SQLite type; they emit
// snapshots, and Manager writes through this interface.
type PoolStateStore interface {
	RecordPoolState(ctx context.Context, state PoolState) error
	SaveCooldowns(ctx context.Context, accountID string, rows []CooldownRow) error
	LoadCooldowns(ctx context.Context) ([]CooldownRow, error)
}

// AccountStore is the persistence surface Manager and console HTTP use.
// The SQLite implementation lives in internal/store; this package must not
// import it.
type AccountStore interface {
	PoolStateStore
	Close() error
	Backup(ctx context.Context, directory string, keep int) (Backup, error)
	Create(ctx context.Context, input CreateAccount) (Account, error)
	Get(ctx context.Context, id string) (Account, error)
	List(ctx context.Context) ([]Account, error)
	// ListRaw returns the stored account rows without overlaying provider
	// policies. Console code uses it to compare the old effective policy with a
	// new one; runtime and account views must keep using List/Get.
	ListRaw(ctx context.Context) ([]Account, error)
	Update(ctx context.Context, id string, input UpdateAccount) error
	Delete(ctx context.Context, id string) error
	SaveCredential(ctx context.Context, accountID, authType string, credential NativeCredential) error
	LoadCredential(ctx context.Context, accountID string) (NativeCredential, error)
	SaveCredentialPayload(ctx context.Context, accountID, format string, payload []byte) error
	// SetCredentialAuthType records the credential kind ("pat"/"oauth") on the
	// account without rewriting the blob, so a PAT login shows the right type.
	SetCredentialAuthType(ctx context.Context, accountID, authType string) error
	LoadCredentialPayload(ctx context.Context, accountID string) (string, []byte, error)
	Observe(ctx context.Context, id, remoteUID, status, lastError, lastKind string) error
	// ObserveHealth updates the probe-derived fields (remote_uid, last_error,
	// last_error_kind) without touching the account status. It is used for
	// disabled accounts that have no pool item, where a probe must not promote
	// the account out of disabled.
	ObserveHealth(ctx context.Context, id, remoteUID, lastError, lastKind string) error
	SaveQuota(ctx context.Context, id string, quota *QuotaSnapshot) error
	RecordCheckin(ctx context.Context, id, status, msg string, at time.Time) error
	RecordCheckinStatus(ctx context.Context, id string, update CheckinStatusUpdate) error
	ListCheckinRecords(ctx context.Context, accountID string, limit int) ([]CheckinRecord, error)
	GetSecret(ctx context.Context, name string) (string, bool, error)
	SetSecret(ctx context.Context, name, value string) error
	SetSecretOrEmpty(ctx context.Context, name, value string) error
	ListProviderPolicies(ctx context.Context) ([]ProviderPolicy, error)
	GetProviderPolicy(ctx context.Context, provider, region string) (ProviderPolicy, bool, error)
	SaveProviderPolicy(ctx context.Context, policy ProviderPolicy) error
	WorkBuddyCheckinTimeDefault(ctx context.Context) string
	GetModelContext(ctx context.Context, modelID string) (int, bool, error)
	SetModelContext(ctx context.Context, modelID string, contextLength int) error
	ListModelContexts(ctx context.Context) (map[string]int, error)
	GetProviderModelSetting(ctx context.Context, provider, modelID string) (ProviderModelSetting, error)
	SetProviderModelSetting(ctx context.Context, provider, modelID string, setting ProviderModelSetting) error
	InsertAPIKey(ctx context.Context, key StoredAPIKey) (APIKey, error)
	SaveAPIKey(ctx context.Context, key StoredAPIKey) (APIKey, error)
	ListAPIKeys(ctx context.Context) ([]APIKey, error)
	GetAPIKey(ctx context.Context, id string) (APIKey, error)
	LookupAPIKey(ctx context.Context, secret string) (APIKey, bool, error)
	DeleteAPIKey(ctx context.Context, id string) error
	TouchAPIKey(ctx context.Context, id string) error
	InsertAggregation(ctx context.Context, agg Aggregation) (Aggregation, error)
	SaveAggregation(ctx context.Context, agg Aggregation) (Aggregation, error)
	ListAggregations(ctx context.Context) ([]Aggregation, error)
	GetAggregation(ctx context.Context, id string) (Aggregation, error)
	DeleteAggregation(ctx context.Context, id string) error
}
