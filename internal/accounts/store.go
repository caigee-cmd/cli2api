package accounts

import (
	"context"
	"time"
)

// PoolStateStore is the cooldown persistence surface used by the pool
// observer drainer. Pool and executor never see the SQLite type; they emit
// Item snapshots, and Manager writes through this interface.
type PoolStateStore interface {
	RecordPoolState(ctx context.Context, item Item) error
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
	Update(ctx context.Context, id string, input UpdateAccount) error
	Delete(ctx context.Context, id string) error
	SaveCredential(ctx context.Context, accountID, authType string, credential NativeCredential) error
	LoadCredential(ctx context.Context, accountID string) (NativeCredential, error)
	SaveCredentialPayload(ctx context.Context, accountID, format string, payload []byte) error
	LoadCredentialPayload(ctx context.Context, accountID string) (string, []byte, error)
	Observe(ctx context.Context, id, remoteUID, status, lastError, lastKind string) error
	SaveQuota(ctx context.Context, id string, quota *QuotaSnapshot) error
	RecordCheckin(ctx context.Context, id, status, msg string, at time.Time) error
	ListCheckinRecords(ctx context.Context, accountID string, limit int) ([]CheckinRecord, error)
	GetSecret(ctx context.Context, name string) (string, bool, error)
	SetSecret(ctx context.Context, name, value string) error
	SetSecretOrEmpty(ctx context.Context, name, value string) error
	WorkBuddyCheckinTimeDefault(ctx context.Context) string
	GetModelContext(ctx context.Context, modelID string) (int, bool, error)
	SetModelContext(ctx context.Context, modelID string, contextLength int) error
	ListModelContexts(ctx context.Context) (map[string]int, error)
	GetProviderModelSetting(ctx context.Context, provider, modelID string) (ProviderModelSetting, error)
	SetProviderModelSetting(ctx context.Context, provider, modelID string, setting ProviderModelSetting) error
	CreateAPIKey(ctx context.Context, input CreateAPIKey) (APIKey, error)
	ListAPIKeys(ctx context.Context) ([]APIKey, error)
	GetAPIKey(ctx context.Context, id string) (APIKey, error)
	LookupAPIKey(ctx context.Context, secret string) (APIKey, bool, error)
	UpdateAPIKey(ctx context.Context, id string, input UpdateAPIKey) (APIKey, error)
	DeleteAPIKey(ctx context.Context, id string) error
	TouchAPIKey(ctx context.Context, id string) error
}
