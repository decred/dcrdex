// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package core

import (
	"errors"
	"fmt"
)

// errors used on client/webserver/site/js/constants.js
// need to be careful for not going out of sync.
const (
	walletErr = iota
	walletAuthErr
	walletBalanceErr
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
	decodeErr
	accountVerificationErr
	accountProofErr
	parseKeyErr
	marketErr
	addressParseErr
	addrErr
	fileReadErr
	unknownDEXErr
	accountRetrieveErr
	accountDisableErr
	suspendedAcctErr
	existenceCheckErr
	createWalletErr
	activeOrdersErr
)

// Error is an error message, an error code and a wrapped error.
type Error struct {
	desc    string
	code    int
	wrapped error
}

// Error returns the error string. Satisfies the error interface.
func (e *Error) Error() string {
	return e.desc
}

// Code returns the error code.
func (e *Error) Code() *int {
	return &e.code
}

// Unwrap returns the underlying wrapped error.
func (e *Error) Unwrap() error {
	return e.wrapped
}

// newError is a constructor for a new Error.
func newError(code int, s string, a ...interface{}) error {
	var err *Error
	for _, v := range a {
		e, ok := v.(error)
		if ok {
			err = &Error{
				desc:    fmt.Sprintf(s, a...),
				code:    code,
				wrapped: e,
			}
			return err
		}
	}
	err = &Error{
		desc:    fmt.Sprintf(s, a...),
		code:    code,
		wrapped: fmt.Errorf(s, a...),
	}
	return err
}

// codedError converts the error to an Error with the specified code.
func codedError(code int, err error) error {
	return &Error{
		desc:    err.Error(),
		code:    code,
		wrapped: err,
	}
}

// errorHasCode checks whether the error is an Error and has the specified code.
func errorHasCode(err error, code int) bool {
	var e *Error
	return errors.As(err, &e) && e.code == code
}

// Unwrap returns the result of calling the Unwrap method on err, if err's
// type contains an Unwrap method returning error.
// Otherwise, Unwrap returns err.
func Unwrap(err error) error {
	for {
		u, ok := err.(interface {
			Unwrap() error
		})
		if !ok {
			return err
		}
		err = u.Unwrap()
	}
}
