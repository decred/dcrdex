// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package db

import "errors"

// TODO: Consider changing error types to Error and DetailedError for direct
// errors package unwrapping and tests with Is/As:

// Error is just a basic error.
//type Error string

// Error satisfies the error interface.
// func (e Error) Error() string {
// 	return string(e)
// }

// DetailedError pairs an Error with details.
// type DetailedError struct {
// 	wrapped Error
// 	detail  string
// }

// Error satisfies the error interface, combining the wrapped error message with
// the details.
// func (e DetailedError) Error() string {
// 	return e.wrapped.Error() + ": " + e.detail
// }

// NewDetailedError wraps the provided Error with details in a DetailedError,
// facilitating the use of errors.Is and errors.As via errors.Unwrap.
// func NewDetailedError(err Error, detail string) DetailedError {
// 	return DetailedError{
// 		wrapped: err,
// 		detail:  detail,
// 	}
// }

// ArchiveError is the error type used by archivist for certain recognized
// errors. Not all returned errors will be of this type.
type ArchiveError struct {
	Code   uint16
	Detail string
}

// The possible Code values in an ArchiveError.
const (
	ErrGeneralFailure uint16 = iota
	ErrUnknownMatch
	ErrUnknownOrder
	ErrUnsupportedMarket
	ErrInvalidOrder
	ErrReusedCommit
	ErrOrderNotExecuted
	ErrUpdateCount
)

func (ae ArchiveError) Error() string {
	desc := "unrecognized error"
	switch ae.Code {
	case ErrGeneralFailure:
		desc = "general failure"
	case ErrUnknownMatch:
		desc = "unknown match"
	case ErrUnknownOrder:
		desc = "unknown order"
	case ErrUnsupportedMarket:
		desc = "unsupported market"
	case ErrInvalidOrder:
		desc = "invalid order"
	case ErrReusedCommit:
		desc = "order commit reused"
	case ErrOrderNotExecuted:
		desc = "order not in executed status"
	case ErrUpdateCount:
		desc = "unexpected number of rows updated"
	}

	if ae.Detail == "" {
		return desc
	}
	return desc + ": " + ae.Detail
}

// SameErrorTypes checks for error equality or ArchiveError.Code equality if
// both errors are of type ArchiveError.
func SameErrorTypes(errA, errB error) bool {
	var arA, arB ArchiveError
	if errors.As(errA, &arA) && errors.As(errB, &arB) {
		return arA.Code == arB.Code
	}

	return errors.Is(errA, errB)
}

// IsErrGeneralFailure returns true if the error is of type ArchiveError and has
// code ErrGeneralFailure.
func IsErrGeneralFailure(err error) bool {
	var errA ArchiveError
	return errors.As(err, &errA) && errA.Code == ErrGeneralFailure
}

// IsErrOrderUnknown returns true if the error is of type ArchiveError and has
// code ErrUnknownOrder.
func IsErrOrderUnknown(err error) bool {
	var errA ArchiveError
	return errors.As(err, &errA) && errA.Code == ErrUnknownOrder
}

// IsErrMatchUnknown returns true if the error is of type ArchiveError and has
// code ErrUnknownMatch.
func IsErrMatchUnknown(err error) bool {
	var errA ArchiveError
	return errors.As(err, &errA) && errA.Code == ErrUnknownMatch
}

// IsErrMatchUnsupported returns true if the error is of type ArchiveError and
// has code ErrUnsupportedMarket.
func IsErrMatchUnsupported(err error) bool {
	var errA ArchiveError
	return errors.As(err, &errA) && errA.Code == ErrUnsupportedMarket
}

// IsErrInvalidOrder returns true if the error is of type ArchiveError and has
// code ErrInvalidOrder.
func IsErrInvalidOrder(err error) bool {
	var errA ArchiveError
	return errors.As(err, &errA) && errA.Code == ErrInvalidOrder
}

// IsErrReusedCommit returns true if the error is of type ArchiveError and has
// code ErrReusedCommit.
func IsErrReusedCommit(err error) bool {
	var errA ArchiveError
	return errors.As(err, &errA) && errA.Code == ErrReusedCommit
}

// IsErrOrderNotExecuted returns true if the error is of type ArchiveError and
// has code ErrOrderNotExecuted.
func IsErrOrderNotExecuted(err error) bool {
	var errA ArchiveError
	return errors.As(err, &errA) && errA.Code == ErrOrderNotExecuted
}

// IsErrUpdateCount returns true if the error is of type ArchiveError and has
// code ErrUpdateCount.
func IsErrUpdateCount(err error) bool {
	var errA ArchiveError
	return errors.As(err, &errA) && errA.Code == ErrUpdateCount
}
