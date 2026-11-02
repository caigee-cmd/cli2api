package codearts

import (
	"context"

	"github.com/caigee-cmd/cli2api/internal/providers"
)

// The console resolves credential import through Adapter.Credential
// (providers.CredentialImporter), so the codec carries Format and PrepareImport.
// The ImportExport slot keeps ValidateImport and Export only.
func (credentialCodec) Format() string { return CredentialFormat }

func (credentialCodec) PrepareImport(payload []byte) (providers.CredentialImport, error) {
	return PrepareImport(payload)
}

type importer struct{}

func (importer) ValidateImport(payload []byte) error { return ValidateCredential(payload) }

// Export is intentionally unsupported. A CodeArts credential contains the STS
// keys and the refresh token; exporting it would hand out the account.
func (importer) Export(context.Context, string) (map[string]any, error) {
	return nil, providers.ErrUnsupported
}
