package control

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"github.com/caigee-cmd/cli2api/internal/accounts"
	"github.com/caigee-cmd/cli2api/internal/providers"
)

type AccountImportInput struct {
	Format               string          `json:"format"`
	Name                 string          `json:"name"`
	Provider             string          `json:"provider"`
	Region               string          `json:"region"`
	Enabled              bool            `json:"enabled"`
	MaxInFlight          int             `json:"max_inflight"`
	Priority             int             `json:"priority"`
	DropSystemPrompt     *bool           `json:"drop_system_prompt"`
	WorkBuddyAutoCheckin *bool           `json:"workbuddy_auto_checkin"`
	WorkBuddyCheckinTime string          `json:"workbuddy_checkin_time"`
	ProxyURL             string          `json:"proxy_url"`
	UserBlob             string          `json:"user_blob"`
	MachineID            string          `json:"machine_id"`
	Credential           json.RawMessage `json:"credential"`
}

func (a *Accounts) Import(ctx context.Context, input AccountImportInput, raw []byte) (accounts.Account, error) {
	if input.Format == "qoder-native-v1" {
		blob, err := base64.StdEncoding.DecodeString(input.UserBlob)
		if err != nil {
			return accounts.Account{}, operationError("invalid_user_blob", "user_blob must be base64")
		}
		account, err := a.ImportNative(ctx, accounts.ImportAccount{
			Name: input.Name, Provider: input.Provider, Region: input.Region, Enabled: input.Enabled,
			MaxInFlight: input.MaxInFlight, Priority: input.Priority, DropSystemPrompt: input.DropSystemPrompt,
			WorkBuddyAutoCheckin: input.WorkBuddyAutoCheckin, WorkBuddyCheckinTime: input.WorkBuddyCheckinTime, ProxyURL: input.ProxyURL,
			Credential: accounts.NativeCredential{UserBlob: blob, MachineID: input.MachineID},
		})
		if err != nil {
			return account, operationError("account_import_failed", err.Error())
		}
		return account, nil
	}
	payload := input.Credential
	if len(payload) == 0 {
		payload = raw
	}
	for _, descriptor := range providers.List() {
		adapter, ok := a.Providers.Get(descriptor.ID)
		if !ok {
			continue
		}
		importer, ok := adapter.Credential.(providers.CredentialImporter)
		if !ok || importer.Format() != input.Format {
			continue
		}
		prepared, err := importer.PrepareImport(payload)
		if err != nil {
			return accounts.Account{}, operationError("invalid_credential", err.Error())
		}
		account, err := a.ImportCredentialPayload(ctx, accounts.CreateAccount{
			Name: input.Name, Provider: descriptor.ID, Region: input.Region,
			MaxInFlight: input.MaxInFlight, Priority: input.Priority, DropSystemPrompt: input.DropSystemPrompt,
			WorkBuddyAutoCheckin: input.WorkBuddyAutoCheckin, WorkBuddyCheckinTime: input.WorkBuddyCheckinTime, ProxyURL: input.ProxyURL,
		}, input.Format, prepared.Payload, prepared.Ready && input.Enabled)
		if err != nil {
			return account, operationError("account_import_failed", err.Error())
		}
		return account, nil
	}
	return accounts.Account{}, operationError("unsupported_format", "unsupported account import format")
}
