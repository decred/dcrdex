package pg

import (
	"errors"
	"testing"
)

func TestDetailedError(t *testing.T) {
	// Ensure that DetailedError.Unwrap is working via errors.Is.
	detail := "blah"
	detailed := NewDetailedError(errTooManyRows, detail)
	if !errors.Is(detailed, errTooManyRows) {
		t.Errorf("Failed to recognize this NewDetailedError as errTooManyRows.")
	}

	expectedErr := errTooManyRows.Error() + ": " + detail
	if detailed.Error() != expectedErr {
		t.Errorf("Wrong error message. Got %s, expected %s", detailed.Error(), expectedErr)
	}
}
