package errs

import (
	"errors"
	"fmt"
)

const (
	ExitFailure     = 1
	ExitUsage       = 2
	ExitDependency  = 3
	ExitConflict    = 4
	ExitNotReady    = 5
	ExitPartial     = 6
	ExitInterrupted = 130
)

type Error struct {
	Code       int
	Diagnostic string
	Message    string
	Cause      error
}

func (e *Error) Error() string {
	if e.Cause == nil {
		return e.Message
	}
	return fmt.Sprintf("%s: %v", e.Message, e.Cause)
}

func (e *Error) Unwrap() error { return e.Cause }

func New(code int, diagnostic, message string) error {
	return &Error{Code: code, Diagnostic: diagnostic, Message: message}
}

func Wrap(code int, diagnostic, message string, cause error) error {
	return &Error{Code: code, Diagnostic: diagnostic, Message: message, Cause: cause}
}

func Code(err error) int {
	if err == nil {
		return 0
	}
	var target *Error
	if errors.As(err, &target) {
		return target.Code
	}
	return ExitFailure
}

func Diagnostic(err error) string {
	var target *Error
	if errors.As(err, &target) {
		return target.Diagnostic
	}
	return "RG000"
}
