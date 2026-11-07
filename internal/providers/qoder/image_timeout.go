package qoder

import (
	"errors"
	"net/http"
	"regexp"
	"strings"

	"github.com/caigee-cmd/cli2api/internal/accounts"
	"github.com/caigee-cmd/cli2api/internal/providers"
)

// CodeUpstreamImageConversionTimeout marks a provider-side image conversion
// timeout (hub cdd95ef / v1.8.0). Distinct from first-byte / worker hang:
// one same-account bounded retry is useful; account rotation is not.
var imageConversionTimeoutRE = regexp.MustCompile(`(?i)image[ _-]+conversion[ _-]+(?:timeout|timed[ _-]+out)`)

func isImageConversionTimeoutMessage(detail string) bool {
	return imageConversionTimeoutRE.MatchString(detail)
}

func imageConversionTimeoutError(detail string) error {
	message := strings.TrimSpace(detail)
	if message == "" {
		message = "image conversion timeout"
	}
	return &providers.Error{
		Kind:    accounts.KindUnavailable,
		Status:  http.StatusBadGateway,
		Code:    providers.CodeUpstreamImageConversionTimeout,
		Message: message,
	}
}

func isImageConversionTimeoutErr(err error) bool {
	if err == nil {
		return false
	}
	var providerErr *providers.Error
	if errors.As(err, &providerErr) && providerErr != nil {
		if providerErr.Code == providers.CodeUpstreamImageConversionTimeout {
			return true
		}
		return isImageConversionTimeoutMessage(providerErr.Message)
	}
	return isImageConversionTimeoutMessage(err.Error())
}
