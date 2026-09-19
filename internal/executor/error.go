package executor

import (
	"context"
	"errors"
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
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return Classified{
			Kind: accounts.KindCanceled, Status: 499, Failover: false,
			Code: "request_canceled", Message: err.Error(),
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
	if classified.Kind == accounts.KindRateLimit {
		switch {
		case providerErr.Code == "4011" && providerErr.RetryAfter <= 0:
			classified.Cooldown = 5 * time.Minute
		case providerErr.RetryAfter > 0:
			classified.Cooldown = providerErr.RetryAfter
			if classified.Cooldown < 30*time.Second {
				classified.Cooldown = 30 * time.Second
			}
		}
	}
	classified.RetryAfter = classified.Cooldown
	return classified
}
