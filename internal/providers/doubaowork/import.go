package doubaowork

import (
	"context"

	"github.com/caigee-cmd/cli2api/internal/providers"
)

// credentialCodec is the CredentialCodec plus the importer surface the console
// uses to turn a pasted Cookie into a stored credential.
type credentialCodec struct{}

func (credentialCodec) Validate(payload []byte) error { return ValidateCredential(payload) }

func (credentialCodec) Format() string { return CredentialFormat }

func (credentialCodec) PrepareImport(payload []byte) (providers.CredentialImport, error) {
	if err := ValidateCredential(payload); err != nil {
		return providers.CredentialImport{}, err
	}
	credential, err := DecodeCredential(payload)
	if err != nil {
		return providers.CredentialImport{}, err
	}
	encoded, err := credential.Encode()
	if err != nil {
		return providers.CredentialImport{}, err
	}
	return providers.CredentialImport{Payload: encoded, Ready: credential.Ready()}, nil
}

// importer is the ImportExporter capability. Export is unsupported: a stored
// doubaowork credential is a live session Cookie, not a portable blob.
type importer struct{}

func (importer) ValidateImport(payload []byte) error { return ValidateCredential(payload) }

func (importer) Export(context.Context, string) (map[string]any, error) {
	return nil, providers.ErrUnsupported
}
