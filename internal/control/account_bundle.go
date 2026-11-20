package control

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/caigee-cmd/cli2api/internal/accounts"
)

const (
	// AccountBundleFormat identifies a full console account migration file.
	AccountBundleFormat = "cli2api-accounts"
	// AccountBundleVersion is bumped when the JSON shape changes incompatibly.
	AccountBundleVersion = 1
)

// AccountBundleCredential is the portable credential material for one account.
// Native Qoder accounts carry user_blob/machine_id; all other providers carry
// an already-normalized credential payload.
type AccountBundleCredential struct {
	Format     string          `json:"format"`
	Credential json.RawMessage `json:"credential,omitempty"`
	UserBlob   string          `json:"user_blob,omitempty"`
	MachineID  string          `json:"machine_id,omitempty"`
}

// AccountBundleEntry is one account plus its settings and credential.
type AccountBundleEntry struct {
	Name                 string                  `json:"name"`
	Provider             string                  `json:"provider"`
	Region               string                  `json:"region"`
	AuthType             string                  `json:"auth_type,omitempty"`
	Enabled              bool                    `json:"enabled"`
	MaxInFlight          int                     `json:"max_inflight"`
	Priority             int                     `json:"priority"`
	DropSystemPrompt     *bool                   `json:"drop_system_prompt,omitempty"`
	ModelAliases         map[string]string       `json:"model_aliases,omitempty"`
	EnabledModels        []string                `json:"enabled_models,omitempty"`
	ModelsRestricted     bool                    `json:"models_restricted,omitempty"`
	AutoCheckin          bool                    `json:"auto_checkin,omitempty"`
	CheckinTime          string                  `json:"checkin_time,omitempty"`
	WorkBuddyAutoCheckin bool                    `json:"workbuddy_auto_checkin,omitempty"`
	WorkBuddyCheckinTime string                  `json:"workbuddy_checkin_time,omitempty"`
	ProxyURL             string                  `json:"proxy_url,omitempty"`
	Credential           *AccountBundleCredential `json:"credential,omitempty"`
}

// AccountBundle is the full export/import file. Accounts without a stored
// credential are still exported so their routing settings survive migration.
type AccountBundle struct {
	Format     string               `json:"format"`
	Version    int                  `json:"version"`
	ExportedAt string               `json:"exported_at"`
	Count      int                  `json:"count"`
	Accounts   []AccountBundleEntry `json:"accounts"`
}

// ExportBundle builds a portable migration file from every stored account.
func (a *Accounts) ExportBundle(ctx context.Context) (AccountBundle, error) {
	stored, err := a.store().List(ctx)
	if err != nil {
		return AccountBundle{}, err
	}
	bundle := AccountBundle{
		Format:     AccountBundleFormat,
		Version:    AccountBundleVersion,
		ExportedAt: time.Now().UTC().Format(time.RFC3339),
		Accounts:   make([]AccountBundleEntry, 0, len(stored)),
	}
	for _, account := range stored {
		entry := AccountBundleEntry{
			Name:                 account.Name,
			Provider:             account.Provider,
			Region:               account.ProviderRegion,
			AuthType:             account.AuthType,
			Enabled:              account.Enabled,
			MaxInFlight:          account.MaxInFlight,
			Priority:             account.Priority,
			ModelAliases:         account.ModelAliases,
			EnabledModels:        account.EnabledModels,
			ModelsRestricted:     account.ModelsRestricted,
			AutoCheckin:          account.AutoCheckin,
			CheckinTime:          account.CheckinTime,
			WorkBuddyAutoCheckin: account.WorkBuddyAutoCheckin,
			WorkBuddyCheckinTime: account.WorkBuddyCheckinTime,
			ProxyURL:             account.ProxyURL,
		}
		drop := account.DropSystemPrompt
		entry.DropSystemPrompt = &drop
		// A missing credential is not fatal: keep the account configuration so
		// the operator can sign in again after the move.
		if exported, exportErr := a.Export(ctx, account.ID); exportErr == nil {
			credential := &AccountBundleCredential{Format: exported.Format}
			if exported.Format == "qoder-native-v1" {
				credential.UserBlob = exported.UserBlob
				credential.MachineID = exported.MachineID
			} else {
				credential.Credential = json.RawMessage(exported.Credential)
			}
			entry.Credential = credential
		}
		bundle.Accounts = append(bundle.Accounts, entry)
	}
	bundle.Count = len(bundle.Accounts)
	return bundle, nil
}

// ImportBundle restores every account in a migration file. Import is additive:
// existing accounts and IDs are never touched; new accounts get fresh IDs.
// Failure policy matches ImportAll: valid entries are kept, failures reported.
func (a *Accounts) ImportBundle(ctx context.Context, bundle AccountBundle) (AccountImportResult, error) {
	if !strings.EqualFold(strings.TrimSpace(bundle.Format), AccountBundleFormat) {
		return AccountImportResult{}, operationError("unsupported_format", "unsupported account bundle format")
	}
	if bundle.Version > AccountBundleVersion {
		return AccountImportResult{}, operationError("unsupported_format", fmt.Sprintf("unsupported account bundle version %d", bundle.Version))
	}
	result := AccountImportResult{Object: "import_result", Data: make([]accounts.Account, 0, len(bundle.Accounts))}
	for i, entry := range bundle.Accounts {
		account, err := a.importBundleEntry(ctx, entry)
		if err != nil {
			result.Failed++
			result.Errors = append(result.Errors, AccountImportItemError{
				Index: i, Name: entry.Name, Message: err.Error(),
			})
			continue
		}
		result.Succeeded++
		result.Data = append(result.Data, account)
	}
	if result.Succeeded == 0 {
		msg := "account bundle import failed"
		if len(result.Errors) > 0 {
			msg = result.Errors[0].Message
		}
		return result, operationError("account_import_failed", msg)
	}
	return result, nil
}

func (a *Accounts) importBundleEntry(ctx context.Context, entry AccountBundleEntry) (accounts.Account, error) {
	create := accounts.CreateAccount{
		Name:                 strings.TrimSpace(entry.Name),
		Provider:             strings.TrimSpace(entry.Provider),
		Region:               strings.TrimSpace(entry.Region),
		Enabled:              false,
		MaxInFlight:          entry.MaxInFlight,
		Priority:             entry.Priority,
		DropSystemPrompt:     entry.DropSystemPrompt,
		WorkBuddyAutoCheckin: boolPtr(entry.WorkBuddyAutoCheckin),
		WorkBuddyCheckinTime: entry.WorkBuddyCheckinTime,
		AutoCheckin:          boolPtr(entry.AutoCheckin),
		CheckinTime:          entry.CheckinTime,
		ProxyURL:             entry.ProxyURL,
	}
	account, err := a.store().Create(ctx, create)
	if err != nil {
		return accounts.Account{}, err
	}
	cleanup := func() {
		_ = a.store().Delete(ctx, account.ID)
		_ = a.runtime.RemoveAccount(account.ID)
	}
	if credential := entry.Credential; credential != nil {
		if credential.Format == "qoder-native-v1" {
			blob, err := base64.StdEncoding.DecodeString(credential.UserBlob)
			if err != nil {
				cleanup()
				return accounts.Account{}, fmt.Errorf("user_blob must be base64: %w", err)
			}
			if err := a.store().SaveCredential(ctx, account.ID, "native", accounts.NativeCredential{
				UserBlob: blob, MachineID: credential.MachineID,
			}); err != nil {
				cleanup()
				return accounts.Account{}, err
			}
		} else if len(credential.Credential) > 0 {
			if err := a.store().SaveCredentialPayload(ctx, account.ID, credential.Format, credential.Credential); err != nil {
				cleanup()
				return accounts.Account{}, err
			}
			if authType := strings.TrimSpace(entry.AuthType); authType != "" && authType != "none" {
				_ = a.store().SetCredentialAuthType(ctx, account.ID, authType)
			}
		}
	}
	// Apply routing and the final enabled state in one update. Create does not
	// persist model routing, so SetModelRouting must be explicit here.
	enabled := entry.Enabled
	update := accounts.UpdateAccount{
		Enabled:         &enabled,
		ModelAliases:    entry.ModelAliases,
		EnabledModels:   entry.EnabledModels,
		SetModelRouting: true,
	}
	if err := a.store().Update(ctx, account.ID, update); err != nil {
		cleanup()
		return accounts.Account{}, err
	}
	account, err = a.store().Get(ctx, account.ID)
	if err != nil {
		return accounts.Account{}, err
	}
	if enabled {
		if err := a.runtime.StartAccount(ctx, account); err != nil {
			return account, err
		}
	}
	return a.store().Get(ctx, account.ID)
}

func boolPtr(value bool) *bool {
	return &value
}
