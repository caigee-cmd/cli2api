package qwenwork

import (
	"strings"

	"github.com/caigee-cmd/cli2api/internal/providers"
)

func (credentialCodec) Format() string { return CredentialFormat }

// PrepareImport accepts the canonical snake_case payload and the camelCase
// account shape exported by qwenwork2api. An access token is required; a
// missing uid imports not-ready until Probe or the next refresh fills it.
func (credentialCodec) PrepareImport(payload []byte) (providers.CredentialImport, error) {
	credential, err := DecodeCredential(payload)
	if err != nil {
		return providers.CredentialImport{}, err
	}
	if strings.TrimSpace(credential.MachineID) == "" {
		credential.MachineID = newID()
	}
	encoded, err := credential.Encode()
	if err != nil {
		return providers.CredentialImport{}, err
	}
	return providers.CredentialImport{Payload: encoded, Ready: credential.Ready()}, nil
}
