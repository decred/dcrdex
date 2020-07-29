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
	FeeAddress string            `json:"feeaddress"`
	FeeCoin    dex.Bytes         `json:"feecoin"`
	BrokenRule account.Rule      `json:"brokenrule"`
}

// Penalty holds data stored in the Penalty table.
type Penalty struct {
	ID         int64             `json:"id"`
	AccountID  account.AccountID `json:"accountid"`
	BrokenRule account.Rule      `json:"brokenrule"`
	Time       int64             `json:"timestamp"`
	Duration   int64             `json:"duration"`
	Details    string            `json:"details"`
	Forgiven   bool              `json:"forgiven"`
}
