package kimi

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/caigee-cmd/cli2api/internal/providers"
)

func (credentialCodec) Format() string { return CredentialFormat }

// PrepareImport accepts either a pasted credential (the Kimi Code CLI file or
// this provider's export shape) or {"import_from":"local"}, which reads the
// login the official client already stored on this machine.
func (credentialCodec) PrepareImport(payload []byte) (providers.CredentialImport, error) {
	var hint struct {
		ImportFrom string `json:"import_from"`
	}
	_ = json.Unmarshal(payload, &hint)

	var credential Credential
	if strings.EqualFold(strings.TrimSpace(hint.ImportFrom), ImportFromLocal) {
		loaded, err := LoadLocalCredential()
		if err != nil {
			return providers.CredentialImport{}, err
		}
		credential = loaded
	} else {
		decoded, err := DecodeCredential(payload)
		if err != nil {
			return providers.CredentialImport{}, err
		}
		credential = decoded
	}
	if !credential.Ready() {
		return providers.CredentialImport{}, errNoAccessToken
	}
	if err := ValidateEndpoints(credential.AuthHost(), credential.ChatBase()); err != nil {
		return providers.CredentialImport{}, err
	}
	if credential.DeviceID == "" {
		credential.DeviceID = newDeviceID()
	}
	if credential.ExpiresAt <= 0 && credential.ExpiresIn > 0 {
		credential.ExpiresAt = time.Now().Unix() + credential.ExpiresIn
	}
	encoded, err := credential.Encode()
	if err != nil {
		return providers.CredentialImport{}, err
	}
	return providers.CredentialImport{Payload: encoded, Ready: true}, nil
}

type importer struct{}

func (importer) ValidateImport(payload []byte) error { return ValidateCredential(payload) }

// Export is not offered: a Kimi Code credential is a bearer token pair, and
// exporting it would hand out the account.
func (importer) Export(context.Context, string) (map[string]any, error) {
	return nil, providers.ErrUnsupported
}
