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
	"github.com/decred/dcrd/dcrutil/v4" // TODO: consider a move to "crypto/sha256" instead of dcrutil.Hash160
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
	var feeAsset sql.NullInt32
	for rows.Next() {
		a := new(db.Account)
		err = rows.Scan(&a.AccountID, &a.Pubkey, &feeAsset, &feeAddress, &a.FeeCoin, &a.BrokenRule)
		if err != nil {
			return nil, err
		}
		a.FeeAsset = uint32(feeAsset.Int32)
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
	var feeAsset sql.NullInt32
	if err := a.db.QueryRow(stmt, aid).Scan(&acct.AccountID, &acct.Pubkey, &feeAsset,
		&feeAddress, &acct.FeeCoin, &acct.BrokenRule); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			err = db.ArchiveError{Code: db.ErrAccountUnknown}
		}
		return nil, err
	}

	acct.FeeAsset = uint32(feeAsset.Int32)
	acct.FeeAddress = feeAddress.String
	return acct, nil
}

// CreateAccount creates an entry for a new account in the accounts table.
func (a *Archiver) CreateAccount(acct *account.Account, assetID uint32, regAddr string) error {
	ai, err := a.AccountInfo(acct.ID)
	if err == nil {
		if ai.FeeAddress != regAddr || ai.FeeAsset != assetID {
			return db.ArchiveError{Code: db.ErrAccountBadFeeInfo}
		}
		if len(ai.FeeCoin) == 0 {
			return nil // fee address and asset match, just unpaid
		}
		if ai.BrokenRule == account.NoRule {
			return db.ArchiveError{Code: db.ErrAccountExists, Detail: ai.FeeCoin.String()}
		}
		return db.ArchiveError{Code: db.ErrAccountSuspended}
	}
	if !db.IsErrAccountUnknown(err) {
		log.Errorf("AccountInfo error for ID %s: %v", acct.ID, err)
		return db.ArchiveError{Code: db.ErrGeneralFailure}
	}

	// ErrAccountUnknown, so create the account.
	return createAccount(a.db, a.tables.accounts, acct, assetID, regAddr)
}

// AccountRegAddr retrieves the registration fee address and the corresponding
// asset ID created for the the specified account.
func (a *Archiver) AccountRegAddr(aid account.AccountID) (string, uint32, error) {
	return accountRegAddr(a.db, a.tables.accounts, aid)
}

// PayAccount sets the registration fee payment details for the account,
// effectively completing the registration process.
func (a *Archiver) PayAccount(aid account.AccountID, coinID []byte) error {
	ok, err := payAccount(a.db, a.tables.accounts, aid, coinID)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("no accounts updated")
	}
	return nil
}

// KeyIndex returns the current child index for the an xpub. If it is not
// known, this creates a new entry with index zero.
func (a *Archiver) KeyIndex(xpub string) (uint32, error) {
	keyHash := dcrutil.Hash160([]byte(xpub))

	var child uint32
	stmt := fmt.Sprintf(internal.CurrentKeyIndex, feeKeysTableName)
	err := a.db.QueryRow(stmt, keyHash).Scan(&child)
	switch {
	case errors.Is(err, sql.ErrNoRows): // continue to create new entry
	case err == nil:
		return child, nil
	default:
		return 0, err
	}

	log.Debugf("Inserting key entry for xpub %.40s..., hash160 = %x", xpub, keyHash)
	stmt = fmt.Sprintf(internal.InsertKeyIfMissing, feeKeysTableName)
	err = a.db.QueryRow(stmt, keyHash).Scan(&child)
	if err != nil {
		return 0, err
	}
	return child, nil
}

// SetKeyIndex records the child index for an xpub. An error is returned
// unless exactly 1 row is updated or created.
func (a *Archiver) SetKeyIndex(idx uint32, xpub string) error {
	keyHash := dcrutil.Hash160([]byte(xpub))
	log.Debugf("Recording new index %d for xpub %.40s... (%x)", idx, xpub, keyHash)
	stmt := fmt.Sprintf(internal.UpsertKeyIndex, feeKeysTableName)
	res, err := a.db.Exec(stmt, idx, keyHash)
	if err != nil {
		return err
	}
	N, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if N != 1 {
		return fmt.Errorf("updated %d rows, expected 1", N)
	}
	return nil
}

// createAccountTables creates the accounts and fee_keys tables.
func createAccountTables(db *sql.DB) error {
	for _, c := range createAccountTableStatements {
		created, err := createTable(db, publicSchema, c.name)
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
	var assetID sql.NullInt32
	var rule uint8
	stmt := fmt.Sprintf(internal.SelectAccount, tableName)
	err := dbe.QueryRow(stmt, aid).Scan(&pubkey, &assetID, &coinID, &rule)
	if err != nil {
		return nil, false, false, err
	}
	acct, err := account.NewAccountFromPubKey(pubkey)
	return acct, len(coinID) > 1, rule == 0, err
}

// createAccount creates an entry for the account in the accounts table.
func createAccount(dbe sqlExecutor, tableName string, acct *account.Account, feeAsset uint32, regAddr string) error {
	stmt := fmt.Sprintf(internal.CreateAccount, tableName)
	_, err := dbe.Exec(stmt, acct.ID, acct.PubKey.SerializeCompressed(), feeAsset, regAddr)
	return err
}

// accountRegAddr gets the registration fee address and its asset ID created for
// the specified account.
func accountRegAddr(dbe *sql.DB, tableName string, aid account.AccountID) (string, uint32, error) {
	var addr string
	var assetID sql.NullInt32
	stmt := fmt.Sprintf(internal.SelectRegAddress, tableName)
	err := dbe.QueryRow(stmt, aid).Scan(&assetID, &addr)
	if err != nil {
		return "", 0, err
	}
	return addr, uint32(assetID.Int32), nil
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
