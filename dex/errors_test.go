package dex

import (
	"errors"
	"testing"
)

type testErr string

func (te testErr) Error() string {
	return string(te)
}

const (
	err0 = testErr("other error")
	err1 = ErrorKind("error 1")
	err2 = ErrorKind("error 2")
)

func TestDexError(t *testing.T) {

	dexErr := NewError(err1, "stuff")

	var err1Back ErrorKind
	if !errors.As(dexErr, &err1Back) {
		t.Errorf("it wasn't an ErrorKind")
	}
	wantErrStr := err1.Error() + ": " + "stuff"
	if dexErr.Error() != wantErrStr {
		t.Errorf("incorrect error string %v, want %v", dexErr.Error(), wantErrStr)
	}

	if !errors.Is(dexErr, err1) {
		t.Errorf("it wasn't err1")
	}
	if errors.Is(dexErr, err2) {
		t.Errorf("it was err2")
	}

	otherErr := NewError(err0, "other stuff")
	var err0Back testErr
	if !errors.As(otherErr, &err0Back) {
		t.Errorf("it wasn't an testErr")
	}
}
