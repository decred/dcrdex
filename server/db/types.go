// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package db

import (
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/server/account"
)

// Account holds data returned by Accounts.
type Account struct {
	AccountID  account.AccountID `json:"accountid"`
	Pubkey     dex.Bytes         `json:"pubkey"`
	FeeAsset   uint32            `json:"feeasset"`
	FeeAddress string            `json:"feeaddress"` // DEPRECATED
	FeeCoin    dex.Bytes         `json:"feecoin"`    // DEPRECATED
}
