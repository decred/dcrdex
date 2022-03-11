package core

import (
	"errors"
	"fmt"
	"testing"
)

type testErr string

func (te testErr) Error() string {
	return string(te)
}

const (
	err0 = testErr("other error")
	err2 = testErr("Test error 2")
)

// TestCoreError tests the error output for the Error type.
func TestCoreError(t *testing.T) {
	tests := []struct {
		in   Error
		want string
	}{{
		Error{
			desc:    "wallet error",
			code:    walletErr,
			wrapped: err0,
		},
		"wallet error",
	}, {
		Error{
			desc:    "wallet auth error",
			code:    walletAuthErr,
			wrapped: err0,
		},
		"wallet auth error",
	}}

	for i, test := range tests {
		result := test.in.Error()
		if result != test.want {
			t.Errorf("#%d: got: %s want: %s", i, result, test.want)
			continue
		}
	}

	coreErr := newError(walletErr, "stuff: %v", err0)

	var err1 *Error
	if !errors.As(coreErr, &err1) {
		t.Errorf("it isn't a core.Error type")
	}
	if !errors.Is(coreErr, err0) {
		t.Errorf("it wasn't err0")
	}
	if errors.Is(coreErr, err2) {
		t.Errorf("it was err2")
	}
	otherErr := codedError(walletErr, err0)
	var err3 testErr
	if !errors.As(otherErr, &err3) {
		t.Errorf("it wasn't an testErr")
	}
}

func TestUnwrap(t *testing.T) {
	err1 := errors.New("1")
	err2 := fmt.Errorf("Error 2: %w", err1)
	err3 := fmt.Errorf("Not wrapped: %v, %d", "other string", 20)
	erra := newError(walletErr, "wrapped 1: %v", err1)

	testCases := []struct {
		err  error
		want error
	}{
		{newError(walletErr, "wrapped: %v", err2), err1},
		{erra, err1},
		{newError(walletErr, "Not wrapped: %v, %d", "other string", 20), err3},
		{newError(walletErr, "wrap 3: %v", err1), err1},
	}
	for i, tc := range testCases {
		if got := Unwrap(tc.err); got.Error() != tc.want.Error() {
			t.Errorf("#%d: Unwrap(%v) = %v, want %v", i, tc.err, got.Error(), tc.want.Error())
		}
	}
}
