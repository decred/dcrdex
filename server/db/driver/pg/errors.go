package pg

import (
	"errors"
)

// These errors are specific to the pg backend; they are not generic DEX
// archivist errors.
var (
	errNoRows      = errors.New("no rows")
	errTooManyRows = errors.New("too many rows")
)

// DetailedError pairs an Error with details.
type DetailedError struct {
	wrapped error
	detail  string
}

// Error satisfies the error interface, combining the wrapped error message with
// the details.
func (e DetailedError) Error() string {
	return e.wrapped.Error() + ": " + e.detail
}

// Unwrap returns the wrapped error, allowing errors.Is and errors.As to work.
func (e DetailedError) Unwrap() error {
	return e.wrapped
}

// NewDetailedError wraps the provided Error with details in a DetailedError,
// facilitating the use of errors.Is and errors.As via errors.Unwrap.
func NewDetailedError(err error, detail string) DetailedError {
	return DetailedError{
		wrapped: err,
		detail:  detail,
	}
}
