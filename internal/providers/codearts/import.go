package codearts

import (
	"context"

	"github.com/caigee-cmd/cli2api/internal/providers"
)

type importer struct{}

func (importer) Format() string { return CredentialFormat }

func (importer) ValidateImport(payload []byte) error { return ValidateCredential(payload) }

func (importer) PrepareImport(payload []byte) (providers.CredentialImport, error) {
	return PrepareImport(payload)
}

// Export is intentionally unsupported. A CodeArts credential contains the STS
// keys and the refresh token; exporting it would hand out the account.
func (importer) Export(context.Context, string) (map[string]any, error) {
	return nil, providers.ErrUnsupported
}
