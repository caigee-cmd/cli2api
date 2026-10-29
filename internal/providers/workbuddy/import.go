package workbuddy

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/caigee-cmd/cli2api/internal/providers"
)

func (credentialCodec) Format() string { return CredentialFormat }

// SplitImport implements providers.CredentialBatchExpander and unwraps a
// workbuddy-accounts hub package into one raw account JSON per entry.
func (credentialCodec) SplitImport(payload []byte) ([][]byte, error) {
	return SplitImport(payload)
}

// PrepareImport accepts canonical snake_case, nested {account,auth}, flat
// camelCase, and a one-account workbuddy-accounts hub package. Multi-account
// packages belong to batch import via SplitImport.
func (credentialCodec) PrepareImport(payload []byte) (providers.CredentialImport, error) {
	chunks, err := SplitImport(payload)
	if err != nil {
		return providers.CredentialImport{}, err
	}
	if len(chunks) != 1 {
		return providers.CredentialImport{}, fmt.Errorf("workbuddy-accounts package has %d accounts; use batch import", len(chunks))
	}
	return prepareOne(chunks[0])
}

// ExpandImport unwraps and prepares every entry. Prefer SplitImport + per-item
// PrepareImport in control when partial success is required.
func ExpandImport(payload []byte) ([]providers.CredentialImport, error) {
	chunks, err := SplitImport(payload)
	if err != nil {
		return nil, err
	}
	out := make([]providers.CredentialImport, 0, len(chunks))
	for i, raw := range chunks {
		prepared, err := prepareOne(raw)
		if err != nil {
			return nil, fmt.Errorf("accounts[%d]: %w", i, err)
		}
		out = append(out, prepared)
	}
	return out, nil
}

// normalizeAccountsFormat maps format / exportType aliases onto AccountsFormat
// (case-insensitive). Values that do not alias stay unchanged.
func normalizeAccountsFormat(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	if strings.EqualFold(trimmed, AccountsFormat) {
		return AccountsFormat
	}
	return trimmed
}

// SplitImport returns one raw credential JSON per imported account.
// Accepts hub packs with format "workbuddy-accounts", official client packs
// with exportType "WorkBuddy-accounts" (any casing), or a bare accounts[]
// array with an empty format field.
func SplitImport(payload []byte) ([][]byte, error) {
	trimmed := strings.TrimSpace(string(payload))
	if trimmed == "" {
		return nil, fmt.Errorf("workbuddy credential requires access_token")
	}
	var envelope struct {
		Format     string            `json:"format"`
		ExportType string            `json:"exportType"`
		Accounts   []json.RawMessage `json:"accounts"`
	}
	if err := json.Unmarshal(payload, &envelope); err != nil {
		return [][]byte{append([]byte(nil), payload...)}, nil
	}

	normalizedFormat := normalizeAccountsFormat(envelope.Format)
	normalizedExport := normalizeAccountsFormat(envelope.ExportType)
	isAccountsHint := normalizedFormat == AccountsFormat || normalizedExport == AccountsFormat

	if !isAccountsHint && len(envelope.Accounts) == 0 {
		return [][]byte{append([]byte(nil), payload...)}, nil
	}

	// Explicit non-accounts format field still rejects even when accounts[] is present.
	// exportType-only packs (including casing aliases) are accepted; empty format +
	// accounts[] is accepted.
	if envelope.Format != "" && normalizedFormat != AccountsFormat {
		return nil, fmt.Errorf("unsupported workbuddy package format %q", envelope.Format)
	}

	if len(envelope.Accounts) == 0 {
		return nil, fmt.Errorf("workbuddy-accounts package has no accounts")
	}
	out := make([][]byte, 0, len(envelope.Accounts))
	for _, raw := range envelope.Accounts {
		out = append(out, append([]byte(nil), raw...))
	}
	return out, nil
}

func prepareOne(payload []byte) (providers.CredentialImport, error) {
	credential, err := DecodeCredential(payload)
	if err != nil {
		return providers.CredentialImport{}, err
	}
	if strings.TrimSpace(credential.AccessToken) == "" {
		return providers.CredentialImport{}, fmt.Errorf("workbuddy credential requires accessToken")
	}
	encoded, err := credential.Encode()
	if err != nil {
		return providers.CredentialImport{}, err
	}
	return providers.CredentialImport{Payload: encoded, Ready: credential.Ready()}, nil
}
