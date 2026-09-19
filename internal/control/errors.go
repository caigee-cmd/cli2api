package control

import "fmt"

type OperationError struct {
	Code string
	Err  error
}

func (e *OperationError) Error() string { return e.Err.Error() }
func (e *OperationError) Unwrap() error { return e.Err }
func operationError(code, message string) error {
	return &OperationError{Code: code, Err: fmt.Errorf("%s", message)}
}
