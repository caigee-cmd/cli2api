package providers

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"strings"
)

// CodeTLSVerifyFailed marks a certificate/hostname verification failure.
// The executor never retries or fails over on it: every account shares the
// same trust chain, so another attempt only repeats the failed handshake.
const CodeTLSVerifyFailed = "tls_verify_failed"

// IsTLSVerifyError reports whether err is a certificate or hostname
// verification failure (as opposed to a transient TLS reset or timeout).
func IsTLSVerifyError(err error) bool {
	if err == nil {
		return false
	}
	var pe *Error
	if errors.As(err, &pe) && pe != nil && pe.Code == CodeTLSVerifyFailed {
		return true
	}
	var verifyErr *tls.CertificateVerificationError
	var unknownAuthority x509.UnknownAuthorityError
	var invalid x509.CertificateInvalidError
	var hostname x509.HostnameError
	if errors.As(err, &verifyErr) || errors.As(err, &unknownAuthority) ||
		errors.As(err, &invalid) || errors.As(err, &hostname) {
		return true
	}
	lower := strings.ToLower(err.Error())
	for _, marker := range []string{
		"x509: certificate signed by unknown authority",
		"x509: certificate has expired",
		"x509: certificate is valid for",
		"x509: certificate is not valid",
		"tls: failed to verify certificate",
		"certificate_verify_failed",
		"certificate verify failed",
	} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

// CodeUpstreamQueued marks a model-level upstream queue rejection (Qoder
// 10605). The account is healthy: no cooldown, no failover, Retry-After only.
const CodeUpstreamQueued = "upstream_queued"

// CodeClientVersionTooLow marks an upstream "upgrade required" hard gate
// (Qoder HTTP 430). Every account shares it, so it is never retried.
const CodeClientVersionTooLow = "client_version_too_low"
