// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package pg

import (
	"database/sql"
	"errors"
	"fmt"

	"decred.org/dcrdex/server/account"
	"decred.org/dcrdex/server/db"
	"decred.org/dcrdex/server/db/driver/pg/internal"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/dcrutil/v3"
	"github.com/decred/dcrd/hdkeychain/v3"
)

// CloseAccount closes the account by setting the value of the rule column to
// the passed rule.
func (a *Archiver) CloseAccount(aid account.AccountID, rule account.Rule) error {
	return a.setAccountRule(aid, rule)
}

// RestoreAccount reopens the account by setting the value of the rule column
// to account.NoRule.
func (a *Archiver) RestoreAccount(aid account.AccountID) error {
	return a.setAccountRule(aid, account.NoRule)
}

// setAccountRule closes or restores the account by setting the value of the
// rule column.
func (a *Archiver) setAccountRule(aid account.AccountID, rule account.Rule) error {
	err := setRule(a.db, a.tables.accounts, aid, rule)
	if err != nil {
		// fatal unless 0 matching rows found.
		if !errors.Is(err, errNoRows) {
			a.fatalBackendErr(err)
		}
		return fmt.Errorf("error setting account rule %s: %w", aid, err)
	}
	return nil
}

// Account retrieves the account pubkey, whether the account is paid, and
// whether the account is open, in that order.
func (a *Archiver) Account(aid account.AccountID) (*account.Account, bool, bool) {
	acct, isPaid, isOpen, err := getAccount(a.db, a.tables.accounts, aid)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return nil, false, false
	case err == nil:
	default:
		log.Errorf("getAccount error: %v", err)
		return nil, false, false
	}
	return acct, isPaid, isOpen
}

// Accounts returns data for all accounts.
func (a *Archiver) Accounts() ([]*db.Account, error) {
	stmt := fmt.Sprintf(internal.SelectAllAccounts, a.tables.accounts)
	rows, err := a.db.Query(stmt)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var accts []*db.Account
	var feeAddress sql.NullString
	for rows.Next() {
		a := new(db.Account)
		err = rows.Scan(&a.AccountID, &a.Pubkey, &feeAddress, &a.FeeCoin, &a.BrokenRule)
		if err != nil {
			return nil, err
		}
		a.FeeAddress = feeAddress.String
		accts = append(accts, a)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	return accts, nil
}

// AccountInfo returns data for an account.
func (a *Archiver) AccountInfo(aid account.AccountID) (*db.Account, error) {
	stmt := fmt.Sprintf(internal.SelectAccountInfo, a.tables.accounts)
	acct := new(db.Account)
	var feeAddress sql.NullString
	if err := a.db.QueryRow(stmt, aid).Scan(&acct.AccountID, &acct.Pubkey, &feeAddress,
		&acct.FeeCoin, &acct.BrokenRule); err != nil {
		return nil, err
	}
	acct.FeeAddress = feeAddress.String
	return acct, nil
}

// CreateAccount creates an entry for a new account in the accounts table. A
// DCR registration fee address is created and returned.
func (a *Archiver) CreateAccount(acct *account.Account) (string, error) {
	regAddr, err := a.getNextAddress()
	if err != nil {
		return "", fmt.Errorf("error creating registration address: %w", err)
	}
	err = createAccount(a.db, a.tables.accounts, acct, regAddr)
	if err != nil {
		return "", err
	}
	return regAddr, nil
}

// CreateKeyEntry creates an entry for the pubkey (hash) if one doesn't already
// exist.
func (a *Archiver) CreateKeyEntry(keyHash []byte) error {
	return createKeyEntry(a.db, a.tables.feeKeys, keyHash)
}

// AccountRegAddr retrieves the registration fee address created for the
// the specified account.
func (a *Archiver) AccountRegAddr(aid account.AccountID) (string, error) {
	return accountRegAddr(a.db, a.tables.accounts, aid)
}

// PayAccount sets the registration fee payment details for the account,
// effectively completing the registration process.
func (a *Archiver) PayAccount(aid account.AccountID, coinID []byte) error {
	// This check is fine for now. If support for an asset with a longer coin ID
	// is implemented, this restriction would need to be loosened.
	if len(coinID) != chainhash.HashSize+4 {
		return fmt.Errorf("incorrect length transaction ID %x. wanted %d, got %d",
			coinID, chainhash.MaxHashStringSize+4, len(coinID))
	}
	ok, err := payAccount(a.db, a.tables.accounts, aid, coinID)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("no accounts updated")
	}
	return nil
}

// Get the next address for the current master pubkey.
func (a *Archiver) getNextAddress() (string, error) {
	stmt := fmt.Sprintf(internal.IncrementKey, feeKeysTableName)
	var childExtKey *hdkeychain.ExtendedKey
out:
	for {
		var child uint32
		err := a.db.QueryRow(stmt, a.keyHash).Scan(&child)
		if err != nil {
			return "", err
		}
		childExtKey, err = a.feeKeyBranch.Child(child)
		switch {
		case errors.Is(err, hdkeychain.ErrInvalidChild):
			continue
		case err == nil:
			break out
		default:
			log.Errorf("error creating child key: %v", err)
			return "", fmt.Errorf("error generating fee address")
		}
	}

	pubKeyBytes := childExtKey.SerializedPubKey()
	addr, err := dcrutil.NewAddressSecpPubKey(pubKeyBytes, a.keyParams)
	if err != nil {
		log.Errorf("error creating new AddressSecpPubKey: %v", err)
		return "", fmt.Errorf("error encoding fee address")
	}

	return addr.Address(), nil
}

// createAccountTables creates the accounts and fee_keys tables.
func createAccountTables(db *sql.DB) error {
	for _, c := range createAccountTableStatements {
		created, err := CreateTable(db, publicSchema, c.name)
		if err != nil {
			return err
		}
		if created {
			log.Tracef("Table %s created", c.name)
		}
	}
	return nil
}

// setRule sets the rule column value.
func setRule(dbe sqlExecutor, tableName string, aid account.AccountID, rule account.Rule) error {
	stmt := fmt.Sprintf(internal.CloseAccount, tableName)
	N, err := sqlExec(dbe, stmt, rule, aid)
	if err != nil {
		return err
	}
	switch N {
	case 0:
		return errNoRows
	case 1:
		return nil
	default:
		return NewDetailedError(errTooManyRows, fmt.Sprint(N, "rows updated instead of 1"))
	}
}

// getAccount gets the account pubkey, whether the account has been
// registered, and whether the account is still open, in that order.
func getAccount(dbe *sql.DB, tableName string, aid account.AccountID) (*account.Account, bool, bool, error) {
	var coinID, pubkey []byte
	var rule uint8
	stmt := fmt.Sprintf(internal.SelectAccount, tableName)
	err := dbe.QueryRow(stmt, aid).Scan(&pubkey, &coinID, &rule)
	if err != nil {
		return nil, false, false, err
	}
	acct, err := account.NewAccountFromPubKey(pubkey)
	return acct, len(coinID) > 1, rule == 0, err
}

// createAccount creates an entry for the account in the accounts table.
func createAccount(dbe sqlExecutor, tableName string, acct *account.Account, regAddr string) error {
	stmt := fmt.Sprintf(internal.CreateAccount, tableName)
	_, err := dbe.Exec(stmt, acct.ID, acct.PubKey.SerializeCompressed(), regAddr)
	return err
}

// accountRegAddr gets the registration fee address created for the specified
// account.
func accountRegAddr(dbe *sql.DB, tableName string, aid account.AccountID) (string, error) {
	var addr string
	stmt := fmt.Sprintf(internal.SelectRegAddress, tableName)
	err := dbe.QueryRow(stmt, aid).Scan(&addr)
	if err != nil {
		return "", err
	}
	return addr, nil
}

// payAccount sets the registration fee payment details.
func payAccount(dbe *sql.DB, tableName string, aid account.AccountID, coinID []byte) (bool, error) {
	stmt := fmt.Sprintf(internal.SetRegOutput, tableName)
	res, err := dbe.Exec(stmt, coinID, aid)
	if err != nil {
		return false, err
	}
	rows, err := res.RowsAffected()
	return rows > 0, err
}

// createKeyEntry creates an entry for the pubkey (hash) if it doesn't already
// exist.
func createKeyEntry(db *sql.DB, tableName string, keyHash []byte) error {
	stmt := fmt.Sprintf(internal.InsertKeyIfMissing, tableName)
	_, err := db.Exec(stmt, keyHash)
	return err
}
