package workbuddy

import "encoding/json"
import "github.com/caigee-cmd/cli2api/internal/providers"

func (credentialCodec) Format() string { return CredentialFormat }
func (credentialCodec) PrepareImport(payload []byte) (providers.CredentialImport, error) {
	if err := ValidateCredential(payload); err != nil {
		return providers.CredentialImport{}, err
	}
	var credential struct {
		UID string `json:"uid"`
	}
	_ = json.Unmarshal(payload, &credential)
	return providers.CredentialImport{Payload: payload, Ready: credential.UID != ""}, nil
}
