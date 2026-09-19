package trae

import "github.com/caigee-cmd/cli2api/internal/providers"

func (credentialCodec) Format() string { return CredentialFormat }
func (credentialCodec) PrepareImport(payload []byte) (providers.CredentialImport, error) {
	if err := ValidateCredential(payload); err != nil {
		return providers.CredentialImport{}, err
	}
	credential, err := DecodeCredential(payload)
	if err != nil {
		return providers.CredentialImport{}, err
	}
	credential = EnsureDevice(credential)
	encoded, err := credential.Encode()
	return providers.CredentialImport{Payload: encoded, Ready: credential.UID != ""}, err
}
