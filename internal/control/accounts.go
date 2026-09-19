package control

import (
	"context"
	"encoding/base64"

	"github.com/caigee-cmd/cli2api/internal/accounts"
	"github.com/caigee-cmd/cli2api/internal/providers"
)

// Runtime is the process lifecycle surface control calls. Persistence and
// enable/import order live here; Manager only starts, stops, and syncs the pool.
type Runtime interface {
	StartAccount(ctx context.Context, account accounts.Account) error
	StopAccount(id string) error
	RemoveAccount(id string) error
	SyncAccount(ctx context.Context, before, after accounts.Account) error
	AccountView(ctx context.Context, id string) (accounts.AccountView, error)
	Accounts(ctx context.Context) ([]accounts.AccountView, error)
	RefreshAccount(ctx context.Context, id string, forceQuota bool) error
	RefreshAll(ctx context.Context, forceQuota bool) error
	CheckinAccount(ctx context.Context, accountID string) (accounts.Account, error)
	ReloadProxyURL(ctx context.Context, value string) error
	ReplaceProxyAPIKey(ctx context.Context, key string) error
	WorkerAdmin(ctx context.Context, input providers.AdminRequest) (providers.AdminResponse, error)
	Store() accounts.AccountStore
}

// Accounts orchestrates console account operations through Store + Runtime.
// HTTP handlers keep decoding and error-code mapping.
type Accounts struct {
	Providers *providers.Registry
	runtime   Runtime
}

func NewAccounts(runtime Runtime) *Accounts {
	if runtime == nil {
		return nil
	}
	return &Accounts{runtime: runtime}
}

func (a *Accounts) store() accounts.AccountStore {
	return a.runtime.Store()
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
	account, err := a.store().Create(ctx, input)
	if err != nil {
		return accounts.Account{}, err
	}
	if account.Enabled {
		if err := a.runtime.StartAccount(ctx, account); err != nil {
			return account, err
		}
	}
	return account, nil
}

func (a *Accounts) Update(ctx context.Context, id string, input accounts.UpdateAccount) (accounts.Account, error) {
	before, err := a.store().Get(ctx, id)
	if err != nil {
		return accounts.Account{}, err
	}
	if err := a.store().Update(ctx, id, input); err != nil {
		return accounts.Account{}, err
	}
	after, err := a.store().Get(ctx, id)
	if err != nil {
		return accounts.Account{}, err
	}
	if err := a.runtime.SyncAccount(ctx, before, after); err != nil {
		return after, err
	}
	return after, nil
}

func (a *Accounts) Delete(ctx context.Context, id string) error {
	if err := a.runtime.StopAccount(id); err != nil {
		return err
	}
	if err := a.store().Delete(ctx, id); err != nil {
		return err
	}
	return a.runtime.RemoveAccount(id)
}

func (a *Accounts) ImportNative(ctx context.Context, input accounts.ImportAccount) (accounts.Account, error) {
	account, err := a.store().Create(ctx, accounts.CreateAccount{
		Name: input.Name, Provider: input.Provider, Region: input.Region, Enabled: false,
		MaxInFlight: input.MaxInFlight, Priority: input.Priority, DropSystemPrompt: input.DropSystemPrompt,
		WorkBuddyAutoCheckin: input.WorkBuddyAutoCheckin, WorkBuddyCheckinTime: input.WorkBuddyCheckinTime, ProxyURL: input.ProxyURL,
	})
	if err != nil {
		return accounts.Account{}, err
	}
	if err := a.store().SaveCredential(ctx, account.ID, "native", input.Credential); err != nil {
		_ = a.store().Delete(ctx, account.ID)
		return accounts.Account{}, err
	}
	if input.Enabled {
		enabled := true
		if err := a.store().Update(ctx, account.ID, accounts.UpdateAccount{Enabled: &enabled}); err != nil {
			return accounts.Account{}, err
		}
		account, err = a.store().Get(ctx, account.ID)
		if err != nil {
			return accounts.Account{}, err
		}
		if err := a.runtime.StartAccount(ctx, account); err != nil {
			return account, err
		}
	}
	return a.store().Get(ctx, account.ID)
}

// ImportCredentialPayload creates a disabled in-process account, writes the
// provider credential blob, then optionally enables it. Credential write
// failure deletes the account, matching the previous HTTP handler. A Get
// after a successful write is best-effort, also matching that handler.
func (a *Accounts) ImportCredentialPayload(ctx context.Context, input accounts.CreateAccount, format string, payload []byte, enable bool) (accounts.Account, error) {
	input.Enabled = false
	account, err := a.store().Create(ctx, input)
	if err != nil {
		return accounts.Account{}, err
	}
	if err := a.store().SaveCredentialPayload(ctx, account.ID, format, payload); err != nil {
		_ = a.store().Delete(ctx, account.ID)
		_ = a.runtime.RemoveAccount(account.ID)
		return accounts.Account{}, err
	}
	if enable {
		enabled := true
		if err := a.store().Update(ctx, account.ID, accounts.UpdateAccount{Enabled: &enabled}); err != nil {
			return accounts.Account{}, err
		}
		account, err = a.store().Get(ctx, account.ID)
		if err != nil {
			return accounts.Account{}, err
		}
		if err := a.runtime.StartAccount(ctx, account); err != nil {
			return account, err
		}
	}
	imported, _ := a.store().Get(ctx, account.ID)
	return imported, nil
}

func (a *Accounts) RefreshAll(ctx context.Context, forceQuota bool) error {
	return a.runtime.RefreshAll(ctx, forceQuota)
}

func (a *Accounts) RefreshAccount(ctx context.Context, id string, forceQuota bool) error {
	return a.runtime.RefreshAccount(ctx, id, forceQuota)
}

func (a *Accounts) GetStored(ctx context.Context, id string) (accounts.Account, error) {
	return a.store().Get(ctx, id)
}

func (a *Accounts) ListCheckins(ctx context.Context, id string, limit int) ([]accounts.CheckinRecord, error) {
	return a.store().ListCheckinRecords(ctx, id, limit)
}

func (a *Accounts) Checkin(ctx context.Context, id string) (accounts.Account, error) {
	account, err := a.GetStored(ctx, id)
	if err != nil {
		return accounts.Account{}, err
	}
	if account.Provider != "workbuddy" {
		return accounts.Account{}, operationError("provider_unsupported", "check-in is only available for WorkBuddy accounts")
	}
	return a.runtime.CheckinAccount(ctx, id)
}

func (a *Accounts) LoadCredentialPayload(ctx context.Context, id string) (string, []byte, error) {
	return a.store().LoadCredentialPayload(ctx, id)
}

func (a *Accounts) LoadNativeCredential(ctx context.Context, id string) (accounts.NativeCredential, error) {
	return a.store().LoadCredential(ctx, id)
}

func (a *Accounts) ReloadProxyURL(ctx context.Context, value string) error {
	return a.runtime.ReloadProxyURL(ctx, value)
}

func (a *Accounts) ReplaceProxyAPIKey(ctx context.Context, key string) error {
	return a.runtime.ReplaceProxyAPIKey(ctx, key)
}

type AccountExport struct {
	Format     string
	Name       string
	Provider   string
	Region     string
	Credential []byte
	UserBlob   string
	MachineID  string
}

func (a *Accounts) Export(ctx context.Context, id string) (AccountExport, error) {
	account, err := a.GetStored(ctx, id)
	if err != nil {
		return AccountExport{}, err
	}
	if account.Provider != "" && account.Provider != "qoder" {
		format, payload, err := a.LoadCredentialPayload(ctx, id)
		if err != nil {
			return AccountExport{}, err
		}
		return AccountExport{
			Format: format, Name: account.Name, Provider: account.Provider, Region: account.ProviderRegion, Credential: payload,
		}, nil
	}
	credential, err := a.LoadNativeCredential(ctx, id)
	if err != nil {
		return AccountExport{}, err
	}
	return AccountExport{
		Format: "qoder-native-v1", Name: account.Name, Provider: account.Provider, Region: account.ProviderRegion,
		UserBlob: base64.StdEncoding.EncodeToString(credential.UserBlob), MachineID: credential.MachineID,
	}, nil
}

type AccountAdminAction struct {
	AccountID   string
	Action      string
	Method      string
	ContentType string
	Body        []byte
	CallbackURL string
}

type AccountAdminResult struct {
	Kind        string
	Session     providers.LoginSession
	LoginDone   bool
	LoginStatus string
	LoginMsg    string
	Worker      providers.AdminResponse
}

func (a *Accounts) Admin(ctx context.Context, input AccountAdminAction) (AccountAdminResult, error) {
	account, storeErr := a.GetStored(ctx, input.AccountID)
	inProcess := storeErr == nil && account.Provider != "" && account.Provider != "qoder"
	switch input.Action {
	case "checkins":
		if storeErr != nil {
			return AccountAdminResult{}, storeErr
		}
		if account.Provider != "workbuddy" {
			return AccountAdminResult{}, operationError("provider_unsupported", "check-in is only available for WorkBuddy accounts")
		}
		return AccountAdminResult{Kind: "checkins"}, nil
	case "checkin":
		if storeErr != nil {
			return AccountAdminResult{}, storeErr
		}
		if account.Provider != "workbuddy" {
			return AccountAdminResult{}, operationError("provider_unsupported", "check-in is only available for WorkBuddy accounts")
		}
		return AccountAdminResult{Kind: "checkin"}, nil
	case "login/device":
		if inProcess {
			session, err := a.StartLogin(ctx, input.AccountID)
			if err != nil {
				return AccountAdminResult{}, err
			}
			return AccountAdminResult{Kind: "login_start", Session: session, LoginStatus: "pending"}, nil
		}
		return a.workerAdmin(ctx, input)
	case "login/status":
		if inProcess {
			done, message, err := a.PollLogin(ctx, input.AccountID)
			if err != nil {
				return AccountAdminResult{}, err
			}
			status := "pending"
			if done {
				status = "ok"
			}
			return AccountAdminResult{Kind: "login_status", LoginDone: done, LoginStatus: status, LoginMsg: message}, nil
		}
		return a.workerAdmin(ctx, input)
	case "login/callback":
		if !inProcess {
			return AccountAdminResult{}, operationError("not_found", "unknown account action")
		}
		if err := a.CompleteLogin(ctx, input.AccountID, input.CallbackURL); err != nil {
			return AccountAdminResult{}, err
		}
		return AccountAdminResult{Kind: "login_complete", LoginStatus: "ok", LoginMsg: "login complete"}, nil
	case "login/pat", "rewarm":
		if inProcess {
			return AccountAdminResult{}, operationError("not_found", "unknown account action")
		}
		return a.workerAdmin(ctx, input)
	default:
		return AccountAdminResult{}, operationError("not_found", "unknown account action")
	}
}

func (a *Accounts) workerAdmin(ctx context.Context, input AccountAdminAction) (AccountAdminResult, error) {
	worker, err := a.runtime.WorkerAdmin(ctx, providers.AdminRequest{
		AccountID: input.AccountID, Action: input.Action, Method: input.Method, ContentType: input.ContentType, Body: input.Body,
	})
	if err != nil {
		return AccountAdminResult{}, err
	}
	return AccountAdminResult{Kind: "worker", Worker: worker}, nil
}
