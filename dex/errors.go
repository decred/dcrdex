// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package dex

// ErrorKind identifies a kind of error that can be used to define new errors
// via const SomeError = dex.ErrorKind("something").
type ErrorKind string

// Error satisfies the error interface and prints human-readable errors.
func (e ErrorKind) Error() string {
	return string(e)
}

// Error pairs an error with details.
type Error struct {
	wrapped error
	detail  string
}

// Error satisfies the error interface, combining the wrapped error message with
// the details.
func (e Error) Error() string {
	return e.wrapped.Error() + ": " + e.detail
}

// Unwrap returns the wrapped error, allowing errors.Is and errors.As to work.
func (e Error) Unwrap() error {
	return e.wrapped
}

// NewError wraps the provided Error with details in a Error, facilitating the
// use of errors.Is and errors.As via errors.Unwrap.
func NewError(err error, detail string) Error {
	return Error{
		wrapped: err,
		detail:  detail,
	}
}
