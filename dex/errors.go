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

// ErrorCloser is used to synchronize shutdown when an error is encountered in a
// multi-step process. After each successful step, a shutdown routine can be
// scheduled with Add. If Success is not signaled before Done, the shutdown
// routines will be run in the reverse order that they are added.
type ErrorCloser struct {
	closers []func() error
}

// NewErrorCloser creates a new ErrorCloser.
func NewErrorCloser() *ErrorCloser {
	return &ErrorCloser{
		closers: make([]func() error, 0, 3),
	}
}

// Add adds a new function to the queue. If Success is not called before Done,
// the Add'ed functions will be run in the reverse order that they were added.
func (e *ErrorCloser) Add(closer func() error) {
	e.closers = append(e.closers, closer)
}

// Success cancels the running of any Add'ed functions.
func (e *ErrorCloser) Success() {
	e.closers = nil
}

// Done signals that the ErrorClose can run its registered functions if success
// has not yet been flagged.
func (e *ErrorCloser) Done(log Logger) {
	for i := len(e.closers) - 1; i >= 0; i-- {
		if err := e.closers[i](); err != nil {
			log.Errorf("error running shutdown function %d: %v", i, err)
		}
	}
}

// Copy creates a shallow copy of the ErrorCloser.
func (e *ErrorCloser) Copy() *ErrorCloser {
	return &ErrorCloser{
		closers: e.closers,
	}
}
