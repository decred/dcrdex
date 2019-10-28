package db

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
	}

	if ae.Detail == "" {
		return desc
	}
	return desc + ": " + ae.Detail
}

// SameErrorTypes checks for error equality or ArchiveError.Code equality if
// both errors are of type ArchiveError.
func SameErrorTypes(errA, errB error) bool {
	if errA == errB {
		return true
	}
	if arA, ok := errA.(ArchiveError); ok {
		if arB, ok := errB.(ArchiveError); ok && arA.Code == arB.Code {
			return true
		}
	}
	return false
}

// IsErrOrderUnknown returns true if the error is of type ArchiveError and has
// code ErrUnknownOrder.
func IsErrOrderUnknown(err error) bool {
	errA, ok := err.(ArchiveError)
	return ok && errA.Code == ErrUnknownOrder
}

// IsErrMatchUnknown returns true if the error is of type ArchiveError and has
// code ErrUnknownMatch.
func IsErrMatchUnknown(err error) bool {
	errA, ok := err.(ArchiveError)
	return ok && errA.Code == ErrUnknownMatch
}

// IsErrMatchUnsupported returns true if the error is of type ArchiveError and
// has code ErrUnsupportedMarket.
func IsErrMatchUnsupported(err error) bool {
	errA, ok := err.(ArchiveError)
	return ok && errA.Code == ErrUnsupportedMarket
}

// IsErrInvalidOrder returns true if the error is of type ArchiveError and has
// code ErrInvalidOrder.
func IsErrInvalidOrder(err error) bool {
	errA, ok := err.(ArchiveError)
	return ok && errA.Code == ErrInvalidOrder
}
