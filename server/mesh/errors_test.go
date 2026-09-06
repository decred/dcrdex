// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package mesh

import (
	"errors"
	"fmt"
	"testing"

	"decred.org/dcrdex/dex/msgjson"
)

func TestClientError(t *testing.T) {
	msgErr := ClientError(fmt.Errorf("drain: %w", ErrUnavailable), msgjson.RPCInternalError, "internal server error")
	if msgErr.Code != msgjson.TryAgainLaterError {
		t.Fatalf("transient code = %d, want TryAgainLater", msgErr.Code)
	}
	// The wire message is fixed; the diagnostic chain stays out of it.
	if msgErr.Message != "mesh temporarily unavailable; retry the request" {
		t.Fatalf("transient message = %q", msgErr.Message)
	}

	deepWrapped := fmt.Errorf("wrap: %w",
		fmt.Errorf("event publisher unavailable for command %s: %w", "cmd-1", ErrUnavailable))
	if msgErr := ClientError(deepWrapped, msgjson.RPCInternalError, "x"); msgErr.Code != msgjson.TryAgainLaterError {
		t.Fatalf("deep-wrapped transient code = %d, want TryAgainLater", msgErr.Code)
	}

	msgErr = ClientError(errors.New("db down"), msgjson.RedemptionError, "match %d revoked", 5)
	if msgErr.Code != msgjson.RedemptionError {
		t.Fatalf("permanent code = %d, want RedemptionError", msgErr.Code)
	}
	if msgErr.Message != "match 5 revoked" {
		t.Fatalf("permanent message = %q", msgErr.Message)
	}
}
