// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package core

import (
	"errors"
	"fmt"
)

// Error codes here are used on the frontend.
const (
	walletErr = iota
	walletAuthErr
	noAuthError
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
	accountStatusUpdateErr
	suspendedAcctErr
	existenceCheckErr
	createWalletErr
	activeOrdersErr
	newAddrErr
	bondAmtErr
	bondTimeErr
	bondAssetErr
	bondPostErr // TODO
)

// Error is an error code and a wrapped error.
type Error struct {
	code int
	err  error
}

// Error returns the error string. Satisfies the error interface.
func (e *Error) Error() string {
	return e.err.Error()
}

// Code returns the error code.
func (e *Error) Code() *int {
	return &e.code
}

// Unwrap returns the underlying wrapped error.
func (e *Error) Unwrap() error {
	return e.err
}

// newError is a constructor for a new Error.
func newError(code int, s string, a ...any) error {
	return &Error{
		code: code,
		err:  fmt.Errorf(s, a...), // s may contain a %w verb to wrap an error
	}
}

// codedError converts the error to an Error with the specified code.
func codedError(code int, err error) error {
	return &Error{
		code: code,
		err:  err,
	}
}

// errorHasCode checks whether the error is an Error and has the specified code.
func errorHasCode(err error, code int) bool {
	var e *Error
	return errors.As(err, &e) && e.code == code
}

// UnwrapErr returns the result of calling the Unwrap method on err,
// until it returns a non-wrapped error.
func UnwrapErr(err error) error {
	InnerErr := errors.Unwrap(err)
	if InnerErr == nil {
		return err
	}
	return UnwrapErr(InnerErr)
}

var (
	ErrAccountSuspended = errors.New("may not trade while account is suspended")
)

// WalletNoPeersError should be returned when a wallet has no network peers.
type WalletNoPeersError struct {
	AssetID uint32
}

func (e *WalletNoPeersError) Error() string {
	return fmt.Sprintf("%s wallet has no network peers (check your network or firewall)", unbip(e.AssetID))
}

// WalletSyncError should be returned when a wallet is still syncing.
type WalletSyncError struct {
	AssetID  uint32
	Progress float32
}

func (e *WalletSyncError) Error() string {
	return fmt.Sprintf("%s still syncing. progress = %.2f%%", unbip(e.AssetID), e.Progress*100)
}
