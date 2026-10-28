package meta

import (
	"context"
	"encoding/json"
	"fmt"
)

type importer struct{}

func (importer) ValidateImport(payload []byte) error {
	return ValidateCredential(payload)
}

func (importer) Export(ctx context.Context, accountID string) (map[string]any, error) {
	_ = ctx
	_ = accountID
	return nil, fmt.Errorf("meta credential export is not supported; re-login or re-import")
}

// Format helpers used by detect paths.
func (credentialCodec) Format() string { return CredentialFormatOAuth }

func decodeImportMap(payload []byte) (map[string]any, error) {
	var out map[string]any
	if err := json.Unmarshal(payload, &out); err != nil {
		return nil, fmt.Errorf("meta credential is not valid json")
	}
	return out, nil
}
