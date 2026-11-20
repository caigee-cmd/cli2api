package minimaxcode

import (
	"context"

	"github.com/caigee-cmd/cli2api/internal/providers"
)

func (credentialCodec) Format() string { return CredentialFormat }

func (credentialCodec) PrepareImport(payload []byte) (providers.CredentialImport, error) {
	credential, err := DecodeCredential(payload)
	if err != nil {
		return providers.CredentialImport{}, err
	}
	if !credential.Ready() {
		return providers.CredentialImport{}, errNoAccessToken
	}
	if err := ValidateEndpoints(credential.AccountBase, credential.AgentBase); err != nil {
		return providers.CredentialImport{}, err
	}
	encoded, err := credential.Encode()
	if err != nil {
		return providers.CredentialImport{}, err
	}
	return providers.CredentialImport{Payload: encoded, Ready: true, Region: credential.Site}, nil
}

type importer struct{}

func (importer) ValidateImport(payload []byte) error {
	return credentialCodec{}.Validate(payload)
}

func (importer) Export(context.Context, string) (map[string]any, error) {
	return nil, providers.ErrUnsupported
}
