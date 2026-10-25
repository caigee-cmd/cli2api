package control

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

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
	AutoCheckin          *bool           `json:"auto_checkin"`
	CheckinTime          string          `json:"checkin_time"`
	ProxyURL             string          `json:"proxy_url"`
	UserBlob             string          `json:"user_blob"`
	MachineID            string          `json:"machine_id"`
	Credential           json.RawMessage `json:"credential"`
}

// AccountImportItemError records one failed entry inside a multi-account package.
type AccountImportItemError struct {
	Index   int    `json:"index"`
	Name    string `json:"name,omitempty"`
	Message string `json:"message"`
}

// AccountImportResult is the import outcome. Single-credential imports still
// populate Data with one account; packages may partially succeed.
type AccountImportResult struct {
	Object    string                   `json:"object"`
	Succeeded int                      `json:"succeeded"`
	Failed    int                      `json:"failed"`
	Data      []accounts.Account       `json:"data"`
	Errors    []AccountImportItemError `json:"errors,omitempty"`
}

func (a *Accounts) Import(ctx context.Context, input AccountImportInput, raw []byte) (accounts.Account, error) {
	result, err := a.ImportAll(ctx, input, raw)
	if err != nil {
		return accounts.Account{}, err
	}
	if result.Succeeded == 0 {
		msg := "account import failed"
		if len(result.Errors) > 0 {
			msg = result.Errors[0].Message
		}
		return accounts.Account{}, operationError("account_import_failed", msg)
	}
	return result.Data[0], nil
}

// ImportAll imports one credential or every entry in a multi-account package.
// Failure policy is partial success: valid entries are kept, failures are
// summarized in Errors instead of rolling back successes.
func (a *Accounts) ImportAll(ctx context.Context, input AccountImportInput, raw []byte) (AccountImportResult, error) {
	if input.Format == "qoder-native-v1" {
		blob, err := base64.StdEncoding.DecodeString(input.UserBlob)
		if err != nil {
			return AccountImportResult{}, operationError("invalid_user_blob", "user_blob must be base64")
		}
		account, err := a.ImportNative(ctx, accounts.ImportAccount{
			AutoCheckin: input.AutoCheckin, CheckinTime: input.CheckinTime,
			Name: input.Name, Provider: input.Provider, Region: input.Region, Enabled: input.Enabled,
			MaxInFlight: input.MaxInFlight, Priority: input.Priority, DropSystemPrompt: input.DropSystemPrompt,
			WorkBuddyAutoCheckin: input.WorkBuddyAutoCheckin, WorkBuddyCheckinTime: input.WorkBuddyCheckinTime, ProxyURL: input.ProxyURL,
			Credential: accounts.NativeCredential{UserBlob: blob, MachineID: input.MachineID},
		})
		if err != nil {
			return AccountImportResult{}, operationError("account_import_failed", err.Error())
		}
		return AccountImportResult{Object: "import_result", Succeeded: 1, Data: []accounts.Account{account}}, nil
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
		if !ok || !credentialFormatMatches(descriptor, importer, input.Format) {
			continue
		}
		chunks, err := splitCredentialImports(importer, payload)
		if err != nil {
			return AccountImportResult{}, operationError("invalid_credential", err.Error())
		}
		if len(chunks) == 0 {
			return AccountImportResult{}, operationError("invalid_credential", "credential package is empty")
		}
		storeFormat := importer.Format()
		result := AccountImportResult{Object: "import_result", Data: make([]accounts.Account, 0, len(chunks))}
		for i, chunk := range chunks {
			prepared, err := importer.PrepareImport(chunk)
			if err != nil {
				result.Failed++
				result.Errors = append(result.Errors, AccountImportItemError{
					Index: i, Message: fmt.Sprintf("accounts[%d]: %s", i, err.Error()),
				})
				continue
			}
			name := importAccountName(input.Name, prepared.Payload, i, len(chunks))
			account, err := a.ImportCredentialPayload(ctx, accounts.CreateAccount{
				AutoCheckin: input.AutoCheckin, CheckinTime: input.CheckinTime,
				Name: name, Provider: descriptor.ID, Region: input.Region,
				MaxInFlight: input.MaxInFlight, Priority: input.Priority, DropSystemPrompt: input.DropSystemPrompt,
				WorkBuddyAutoCheckin: input.WorkBuddyAutoCheckin, WorkBuddyCheckinTime: input.WorkBuddyCheckinTime, ProxyURL: input.ProxyURL,
			}, storeFormat, prepared.Payload, prepared.Ready && input.Enabled)
			if err != nil {
				result.Failed++
				result.Errors = append(result.Errors, AccountImportItemError{
					Index: i, Name: name, Message: err.Error(),
				})
				continue
			}
			result.Succeeded++
			result.Data = append(result.Data, account)
		}
		if result.Succeeded == 0 {
			msg := "account import failed"
			if len(result.Errors) > 0 {
				msg = result.Errors[0].Message
			}
			return result, operationError("account_import_failed", msg)
		}
		return result, nil
	}
	return AccountImportResult{}, operationError("unsupported_format", "unsupported account import format")
}

func credentialFormatMatches(descriptor providers.ProviderDescriptor, importer providers.CredentialImporter, format string) bool {
	if importer.Format() == format {
		return true
	}
	// Accept registry aliases (e.g. workbuddy-accounts → workbuddy-oauth-v1).
	return descriptor.ID != "" && descriptor.SupportsCredentialFormat(format) &&
		descriptor.SupportsCredentialFormat(importer.Format())
}

func splitCredentialImports(importer providers.CredentialImporter, payload []byte) ([][]byte, error) {
	if expander, ok := importer.(providers.CredentialBatchExpander); ok {
		return expander.SplitImport(payload)
	}
	return [][]byte{payload}, nil
}

func importAccountName(base string, payload []byte, index, total int) string {
	base = strings.TrimSpace(base)
	nickname := credentialDisplayName(payload)
	if total <= 1 {
		if base != "" {
			return base
		}
		if nickname != "" {
			return nickname
		}
		return ""
	}
	if base != "" && nickname != "" && !strings.EqualFold(base, nickname) {
		return fmt.Sprintf("%s · %s", base, nickname)
	}
	if nickname != "" {
		return nickname
	}
	if base != "" {
		return fmt.Sprintf("%s #%d", base, index+1)
	}
	return fmt.Sprintf("account #%d", index+1)
}

func credentialDisplayName(payload []byte) string {
	var meta struct {
		Nickname string `json:"nickname"`
		UID      string `json:"uid"`
	}
	_ = json.Unmarshal(payload, &meta)
	if name := strings.TrimSpace(meta.Nickname); name != "" {
		return name
	}
	return strings.TrimSpace(meta.UID)
}
