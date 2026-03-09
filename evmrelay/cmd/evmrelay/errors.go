// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package main

import (
	"errors"
	"fmt"
	"math/big"
	"time"
)

var (
	errDeadlineExpired         = errors.New("deadline expired")
	errDeadlineTooSoon         = errors.New("deadline too soon for relay")
	errFeeTooLow               = errors.New("fee too low")
	errFeeExceedsRedeemedValue = errors.New("fee exceeds total redeemed value")
	errGasEstimateTooHigh      = errors.New("gas estimate exceeds maximum expected gas")
)

type deadlineExpiredError struct {
	Deadline time.Time
	Now      time.Time
}

func (e *deadlineExpiredError) Error() string {
	return fmt.Sprintf("%s: deadline %s <= now %s",
		errDeadlineExpired,
		e.Deadline.UTC().Format(time.RFC3339),
		e.Now.UTC().Format(time.RFC3339),
	)
}

func (e *deadlineExpiredError) Is(target error) bool {
	return target == errDeadlineExpired
}

type deadlineTooSoonError struct {
	Deadline   time.Time
	ValidUntil time.Time
	Now        time.Time
}

func (e *deadlineTooSoonError) Error() string {
	return fmt.Sprintf("%s: deadline %s yields relay valid-until %s <= now %s",
		errDeadlineTooSoon,
		e.Deadline.UTC().Format(time.RFC3339),
		e.ValidUntil.UTC().Format(time.RFC3339),
		e.Now.UTC().Format(time.RFC3339),
	)
}

func (e *deadlineTooSoonError) Is(target error) bool {
	return target == errDeadlineTooSoon
}

type feeTooLowError struct {
	Need *big.Int
	Got  *big.Int
}

func (e *feeTooLowError) Error() string {
	return fmt.Sprintf("%s: need %s, got %s", errFeeTooLow, e.Need, e.Got)
}

func (e *feeTooLowError) Is(target error) bool {
	return target == errFeeTooLow
}

type feeExceedsRedeemedValueError struct {
	Fee   *big.Int
	Total *big.Int
}

func (e *feeExceedsRedeemedValueError) Error() string {
	return fmt.Sprintf("%s: fee %s exceeds total redeemed value %s", errFeeExceedsRedeemedValue, e.Fee, e.Total)
}

func (e *feeExceedsRedeemedValueError) Is(target error) bool {
	return target == errFeeExceedsRedeemedValue
}

type gasEstimateTooHighError struct {
	GasEstimate uint64
	ExpectedGas uint64
}

func (e *gasEstimateTooHighError) Error() string {
	return fmt.Sprintf("%s: gas estimate %d exceeds maximum expected gas %d (possible griefing attempt)",
		errGasEstimateTooHigh,
		e.GasEstimate,
		e.ExpectedGas,
	)
}

func (e *gasEstimateTooHighError) Is(target error) bool {
	return target == errGasEstimateTooHigh
}
