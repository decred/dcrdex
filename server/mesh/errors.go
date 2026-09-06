// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package mesh

import (
	"errors"

	"decred.org/dcrdex/dex/msgjson"
)

var (
	// ErrUnavailable means the current state of the node does not allow the
	// requested operation. This is used to represent a temporary unavailability,
	// that can be retried later.
	ErrUnavailable = errors.New("mesh unavailable")

	// ErrClientNotConnected means the user is not connected to the node that was
	// asked to deliver a client message.
	ErrClientNotConnected = errors.New("client not connected")

	// ErrClientProxyUnavailable indicates that no active mesh peer can relay live
	// client messages.
	ErrClientProxyUnavailable = errors.New("mesh client proxy unavailable")
)

// ClientError maps a failed Emit or ApplyEvent to a client wire error:
// TryAgainLater for ErrUnavailable, the given permanent error otherwise.
func ClientError(err error, code int, format string, args ...any) *msgjson.Error {
	if errors.Is(err, ErrUnavailable) {
		return msgjson.NewError(msgjson.TryAgainLaterError,
			"mesh temporarily unavailable; retry the request")
	}
	return msgjson.NewError(code, format, args...)
}

// applyFailureLogger is the subset of slog/dex loggers used by LogApplyFailure.
type applyFailureLogger interface {
	Debugf(format string, args ...any)
	Errorf(format string, args ...any)
}

// LogApplyFailure logs a failed Emit or ApplyEvent at a client edge.
// ErrUnavailable is expected temporary unavailability (Debug); other
// failures are Error.
func LogApplyFailure(log applyFailureLogger, err error, format string, args ...any) {
	if errors.Is(err, ErrUnavailable) {
		log.Debugf(format, args...)
		return
	}
	log.Errorf(format, args...)
}
