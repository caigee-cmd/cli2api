package control

import (
	"context"

	"github.com/caigee-cmd/cli2api/internal/accounts"
)

// Runtime is the account lifecycle surface control calls. Implemented by
// *accountruntime.Manager; control does not reimplement process start/stop.
type Runtime interface {
	Create(ctx context.Context, input accounts.CreateAccount) (accounts.Account, error)
	Update(ctx context.Context, id string, input accounts.UpdateAccount) error
	Delete(ctx context.Context, id string) error
	Import(ctx context.Context, input accounts.ImportAccount) (accounts.Account, error)
	AccountView(ctx context.Context, id string) (accounts.AccountView, error)
	Accounts(ctx context.Context) ([]accounts.AccountView, error)
	RefreshAccount(ctx context.Context, id string, forceQuota bool) error
	RefreshAll(ctx context.Context, forceQuota bool) error
	CheckinAccount(ctx context.Context, accountID string) (accounts.Account, error)
	ReloadProxyURL(ctx context.Context, value string) error
	ReplaceProxyAPIKey(ctx context.Context, key string) error
	Store() accounts.AccountStore
}

// Accounts orchestrates console account operations through Runtime.
// HTTP handlers keep decoding and error-code mapping.
type Accounts struct {
	runtime Runtime
}

func NewAccounts(runtime Runtime) *Accounts {
	if runtime == nil {
		return nil
	}
	return &Accounts{runtime: runtime}
}

func (a *Accounts) List(ctx context.Context, refresh bool) ([]accounts.AccountView, error) {
	if refresh {
		_ = a.runtime.RefreshAll(ctx, true)
	}
	return a.runtime.Accounts(ctx)
}

func (a *Accounts) Get(ctx context.Context, id string) (accounts.AccountView, error) {
	return a.runtime.AccountView(ctx, id)
}

func (a *Accounts) Create(ctx context.Context, input accounts.CreateAccount) (accounts.Account, error) {
	return a.runtime.Create(ctx, input)
}

func (a *Accounts) Update(ctx context.Context, id string, input accounts.UpdateAccount) (accounts.Account, error) {
	if err := a.runtime.Update(ctx, id, input); err != nil {
		return accounts.Account{}, err
	}
	account, _ := a.runtime.Store().Get(ctx, id)
	return account, nil
}

func (a *Accounts) Delete(ctx context.Context, id string) error {
	return a.runtime.Delete(ctx, id)
}

func (a *Accounts) ImportNative(ctx context.Context, input accounts.ImportAccount) (accounts.Account, error) {
	return a.runtime.Import(ctx, input)
}

// ImportCredentialPayload creates a disabled in-process account, writes the
// provider credential blob, then optionally enables it. Credential write
// failure deletes the account, matching the previous HTTP handler. A Get
// after a successful write is best-effort, also matching that handler.
func (a *Accounts) ImportCredentialPayload(ctx context.Context, input accounts.CreateAccount, format string, payload []byte, enable bool) (accounts.Account, error) {
	input.Enabled = false
	account, err := a.runtime.Create(ctx, input)
	if err != nil {
		return accounts.Account{}, err
	}
	if err := a.runtime.Store().SaveCredentialPayload(ctx, account.ID, format, payload); err != nil {
		_ = a.runtime.Delete(ctx, account.ID)
		return accounts.Account{}, err
	}
	if enable {
		enabled := true
		if err := a.runtime.Update(ctx, account.ID, accounts.UpdateAccount{Enabled: &enabled}); err != nil {
			return accounts.Account{}, err
		}
	}
	imported, _ := a.runtime.Store().Get(ctx, account.ID)
	return imported, nil
}

func (a *Accounts) RefreshAll(ctx context.Context, forceQuota bool) error {
	return a.runtime.RefreshAll(ctx, forceQuota)
}

func (a *Accounts) RefreshAccount(ctx context.Context, id string, forceQuota bool) error {
	return a.runtime.RefreshAccount(ctx, id, forceQuota)
}

func (a *Accounts) GetStored(ctx context.Context, id string) (accounts.Account, error) {
	return a.runtime.Store().Get(ctx, id)
}

func (a *Accounts) ListCheckins(ctx context.Context, id string, limit int) ([]accounts.CheckinRecord, error) {
	return a.runtime.Store().ListCheckinRecords(ctx, id, limit)
}

func (a *Accounts) Checkin(ctx context.Context, id string) (accounts.Account, error) {
	return a.runtime.CheckinAccount(ctx, id)
}

func (a *Accounts) LoadCredentialPayload(ctx context.Context, id string) (string, []byte, error) {
	return a.runtime.Store().LoadCredentialPayload(ctx, id)
}

func (a *Accounts) LoadNativeCredential(ctx context.Context, id string) (accounts.NativeCredential, error) {
	return a.runtime.Store().LoadCredential(ctx, id)
}

func (a *Accounts) ReloadProxyURL(ctx context.Context, value string) error {
	return a.runtime.ReloadProxyURL(ctx, value)
}

func (a *Accounts) ReplaceProxyAPIKey(ctx context.Context, key string) error {
	return a.runtime.ReplaceProxyAPIKey(ctx, key)
}

func (a *Accounts) Store() accounts.AccountStore {
	if a == nil || a.runtime == nil {
		return nil
	}
	return a.runtime.Store()
}
