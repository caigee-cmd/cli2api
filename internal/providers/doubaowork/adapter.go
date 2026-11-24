package doubaowork

import "github.com/caigee-cmd/cli2api/internal/providers"

// Adapter wires the capability surface. Login is absent: the only auth is a
// pasted session Cookie, exposed through the import/PAT path.
func (c *Client) Adapter() providers.Adapter {
	return providers.Adapter{
		ID:            "doubaowork",
		Credential:    credentialCodec{},
		Chat:          c,
		Models:        c,
		Classifier:    classifier{},
		ImportExport:  importer{},
		Prober:        c,
		Checkin:       c,
		CheckinStatus: c,
	}
}

type classifier struct{}

func (classifier) Classify(status int, body string) providers.ClassifiedError {
	return Classify(status, body)
}
