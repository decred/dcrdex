// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package db

// Account holds data returned by Accounts. Byte array fields in the database
// are encoded as hex strings.
type Account struct {
	AccountID  string
	Pubkey     string
	FeeAddress string
	FeeCoin    string
	BrokenRule byte
}
