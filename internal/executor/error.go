package executor

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/caigee-cmd/cli2api/internal/accounts"
	"github.com/caigee-cmd/cli2api/internal/providers"
)

// ExecutionError is the executor's final classified result. HTTP layers format
// it; they must not re-run Classify or invent failover/cooldown policy.
type ExecutionError struct {
	Classified Classified
	Err        error
}

func (e *ExecutionError) Error() string {
	if e == nil {
		return ""
	}
	if e.Err != nil {
		return e.Err.Error()
	}
	if e.Classified.Message != "" {
		return e.Classified.Message
	}
	return e.Classified.Code
}

func (e *ExecutionError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func NewExecutionError(classified Classified, err error) *ExecutionError {
	if classified.RetryAfter <= 0 {
		classified.RetryAfter = classified.Cooldown
	}
	return &ExecutionError{Classified: classified, Err: err}
}

// ClassifyUpstreamBody classifies an upstream HTTP/SSE error body once.
func ClassifyUpstreamBody(status int, body string) error {
	return NewExecutionError(Classify(status, body, "", "", ""), nil)
}

// StreamIncompleteError is a mid-body stream that ended without [DONE].
func StreamIncompleteError() error {
	return newUnavailableStreamError("upstream_stream_incomplete", "stream ended before [DONE]", 502, nil)
}

// StreamReadError wraps a mid-body stream read failure.
func StreamReadError(err error) error {
	if err == nil {
		return StreamIncompleteError()
	}
	var execErr *ExecutionError
	if errors.As(err, &execErr) && execErr != nil {
		return err
	}
	var providerErr *providers.Error
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &providerErr) && providerErr != nil) {
		return NewExecutionError(ClassifyError(err), err)
	}
	return newUnavailableStreamError("upstream_stream_interrupted", "stream read error: "+err.Error(), 502, err)
}

func newUnavailableStreamError(code, message string, status int, cause error) error {
	classified := Classify(status, message, "", accounts.KindUnavailable, "")
	classified.Code = code
	classified.Message = message
	classified.Status = status
	classified.Type = "api_error"
	return NewExecutionError(classified, cause)
}

// ClassifyError is the single adapter-error policy used by execution and HTTP reporting.
func ClassifyError(err error) Classified {
	if err == nil {
		return Classify(0, "", "", accounts.KindUnavailable, "")
	}
	var execErr *ExecutionError
	if errors.As(err, &execErr) && execErr != nil {
		return execErr.Classified
	}
	// A streaming first-byte timeout is an upstream hang, not a client cancel:
	// the parent request is still alive, the upstream just never answered. Cool
	// the account briefly and fail over so the next eligible account serves the
	// turn instead of hanging until the client gives up.
	if errors.Is(err, providers.ErrFirstByteTimeout) {
		return Classified{
			Kind: accounts.KindUnavailable, Status: http.StatusBadGateway,
			Failover: true, Cooldown: 10 * time.Second, RetryAfter: 10 * time.Second,
			Code: "first_byte_timeout", Message: err.Error(),
		}
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return Classified{
			Kind: accounts.KindCanceled, Status: 499, Failover: false,
			Code: "request_canceled", Message: err.Error(),
		}
	}
	if isTransportFailure(err) {
		// The request never reached the upstream: a dial, TLS handshake, or
		// reset failure says nothing about this account's credential, quota,
		// or rate limit. Cooling it with the generic unavailable default (15s)
		// just parks a healthy account, and in a single-account route one
		// handshake timeout turns into "all accounts are cooling down" for
		// every request that follows. Surface the transport failure and let
		// the client retry; the account stays routable.
		return Classified{
			Kind: accounts.KindUnavailable, Status: http.StatusBadGateway,
			Failover: false, Code: "upstream_unreachable", Type: "api_error",
			Message: err.Error(),
		}
	}
	if providers.IsTLSVerifyError(err) {
		// Certificate verification is deterministic and shared by every
		// account: no failover, no cooldown, surface the hint as-is.
		return Classified{
			Kind: accounts.KindUnavailable, Status: http.StatusBadGateway,
			Failover: false, Code: providers.CodeTLSVerifyFailed,
			Type: "api_error", Message: err.Error(),
		}
	}
	var providerErr *providers.Error
	if !errors.As(err, &providerErr) || providerErr == nil {
		return Classify(0, err.Error(), "", "", "")
	}
	message := strings.TrimSpace(providerErr.Message)
	if message == "" {
		message = providerErr.Error()
	}
	classified := Classify(providerErr.Status, message, "", providerErr.Kind, "")
	if providerErr.Code != "" {
		classified.Code = providerErr.Code
	}
	if providerErr.Type != "" {
		classified.Type = providerErr.Type
	}
	if providerErr.Message != "" {
		classified.Message = providerErr.Message
	}
	switch providerErr.Code {
	case providers.CodeUpstreamQueued:
		// Model-level queue: the account is fine and another account would
		// queue the same way (and break session affinity). No cooldown, no
		// failover; pass the upstream retryAfterSeconds through.
		classified.Kind = accounts.KindRateLimit
		classified.Status = http.StatusTooManyRequests
		classified.Failover = false
		classified.Cooldown = 0
		classified.RetryAfter = providerErr.RetryAfter
		return classified
	case providers.CodeClientVersionTooLow:
		classified.Failover = false
		classified.Cooldown = 0
		classified.RetryAfter = 0
		return classified
	}
	if providerErr.Code == "upstream_empty" {
		// A heartbeat-only upstream stream says nothing about the account's
		// health: the credential, quota, and rate-limit state are all fine, and
		// the same account may succeed on the very next request. Fail over so
		// another account serves the turn, but never cool this one down.
		classified.Failover = true
		classified.Cooldown = 0
	}
	switch classified.Kind {
	case accounts.KindRateLimit:
		switch {
		case providerErr.Code == "4011" && providerErr.RetryAfter <= 0:
			classified.Cooldown = 5 * time.Minute
		case providerErr.RetryAfter > 0:
			classified.Cooldown = providerErr.RetryAfter
			if classified.Cooldown < 30*time.Second {
				classified.Cooldown = 30 * time.Second
			}
		}
	case accounts.KindQuota:
		// A parsed upstream reset replaces the local-midnight fallback.
		// Quota stays account-wide and does not fail over mid-request.
		if providerErr.RetryAfter > 0 {
			classified.Cooldown = providerErr.RetryAfter
		}
	}
	classified.RetryAfter = classified.Cooldown
	return classified
}

// isTransportFailure reports whether err is a connection-level failure (dial,
// TLS handshake, reset) rather than an answer from the upstream. http.Client
// reports every such failure as *url.Error; a response that arrived comes back
// with no error at all, so an upstream 502 whose body merely quotes
// "TLS handshake timeout" is not mistaken for one.
func isTransportFailure(err error) bool {
	var urlErr *url.Error
	return errors.As(err, &urlErr)
}
