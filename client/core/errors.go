// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package core

import (
	"errors"
	"fmt"
)

const (
	walletErr = iota
	walletAuthErr
	dupeDEXErr
	assetSupportErr
	registerErr
	signatureErr
	zeroFeeErr
	feeMismatchErr
	feeSendErr
	passwordErr
	emptyHostErr
	connectionErr
	acctKeyErr
	unknownOrderErr
	orderParamsErr
	dbErr
	authErr
	connectWalletErr
	missingWalletErr
	encryptionErr
	marketErr
	addressParseErr
	addrErr
	fileReadErr
)

// Error is an error message and an error code.
type Error struct {
	s    string
	code int
}

// Error returns the error string. Satisfies the error interface.
func (e *Error) Error() string {
	return e.s
}

// newError is a constructor for a new Error.
func newError(code int, s string, a ...interface{}) error {
	return &Error{
		s:    fmt.Sprintf(s, a...),
		code: code,
	}
}

// codedError converts the error to an Error with the specified code.
func codedError(code int, err error) error {
	return &Error{
		s:    err.Error(),
		code: code,
	}
}

// errorHasCode checks whether the error is an Error and has the specified code.
func errorHasCode(err error, code int) bool {
	var e *Error
	return errors.As(err, &e) && e.code == code
}
