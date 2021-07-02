// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package pg

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	"decred.org/dcrdex/server/account"
	"decred.org/dcrdex/server/db"
	"decred.org/dcrdex/server/db/driver/pg/internal"
	"github.com/decred/dcrd/dcrutil/v4" // TODO: consider a move to "crypto/sha256" instead of dcrutil.Hash160
)

// Account retrieves the account pubkey, active bonds, and if the account is has
// a legacy registration fee address and transaction recorded. If the account
// does not exists or their in an error retrieving any data, a nil
// *account.Account is returned.
func (a *Archiver) Account(aid account.AccountID, bondExpiry time.Time) (acct *account.Account, bonds []*db.Bond, legacy, legacyPaid bool) {
	acct, legacy, legacyPaid, err := getAccount(a.db, a.tables.accounts, aid)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return nil, nil, false, false
	case err == nil:
	default:
		log.Errorf("getAccount error: %v", err)
		return nil, nil, false, false
	}

	bonds, err = getBondsForAccount(a.db, a.tables.bonds, aid, bondExpiry.Unix())
	switch {
	case errors.Is(err, sql.ErrNoRows):
		bonds = nil
	case err == nil:
	default:
		log.Errorf("getBondsForAccount error: %v", err)
		return nil, nil, false, false
	}

	return acct, bonds, legacy, legacyPaid
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
		err = rows.Scan(&a.AccountID, &a.Pubkey, &feeAsset, &feeAddress, &a.FeeCoin)
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
	// bondExpiry time.Time and bonds return needed?
	stmt := fmt.Sprintf(internal.SelectAccountInfo, a.tables.accounts)
	acct := new(db.Account)
	var feeAddress sql.NullString
	var feeAsset sql.NullInt32
	if err := a.db.QueryRow(stmt, aid).Scan(&acct.AccountID, &acct.Pubkey, &feeAsset,
		&feeAddress, &acct.FeeCoin); err != nil {
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
// DEPRECATED: See CreateAccountWithBond. (V0PURGE)
func (a *Archiver) CreateAccount(acct *account.Account, assetID uint32, regAddr string) error {
	ai, err := a.AccountInfo(acct.ID)
	if err == nil {
		if ai.FeeAddress != regAddr || ai.FeeAsset != assetID {
			return db.ArchiveError{Code: db.ErrAccountBadFeeInfo}
		}
		// Account exists. Respond with either a fee address, or their (paid)
		// fee coin.
		if len(ai.FeeCoin) == 0 {
			return nil // fee address and asset match, just unpaid
		}
		return &db.ArchiveError{Code: db.ErrAccountExists, Detail: ai.FeeCoin.String()}
		// With tiers, "ErrAccountSuspended" is no more. Instead, the caller
		// should check the user's tier to decide if the caller (the legacy
		// 'register' handler) should respond with msgjson.AccountSuspendedError
		// or msgjson.AccountExistsError.
	}
	if !db.IsErrAccountUnknown(err) {
		log.Errorf("AccountInfo error for ID %s: %v", acct.ID, err)
		return db.ArchiveError{Code: db.ErrGeneralFailure}
	}

	// ErrAccountUnknown, so create the account.
	return createAccount(a.db, a.tables.accounts, acct, assetID, regAddr)
}

// CreateAccountWithTx creates a new account with a fidelity bond.
func (a *Archiver) CreateAccountWithBond(acct *account.Account, bond *db.Bond) error {
	dbTx, err := a.db.BeginTx(a.ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		if err == nil || errors.Is(err, sql.ErrTxDone) {
			return
		}
		if errR := dbTx.Rollback(); errR != nil {
			log.Errorf("Rollback failed: %v", errR)
		}
	}()

	err = createAccount(dbTx, a.tables.accounts, acct, 0, "") // no reg fee addr
	if err != nil {
		return err
	}
	err = addBond(dbTx, a.tables.bonds, acct.ID, bond)
	if err != nil {
		return err
	}

	err = dbTx.Commit() // for the defer
	return err
}

// AddBond stores a new Bond for an existing account.
func (a *Archiver) AddBond(aid account.AccountID, bond *db.Bond) error {
	return addBond(a.db, a.tables.bonds, aid, bond)
}

// ActivateBond flags an existing bond as active / not pending.
func (a *Archiver) ActivateBond(acctID account.AccountID, assetID uint32, coinID []byte) error {
	stmt := fmt.Sprintf(internal.ActivateBond, a.tables.bonds)
	N, err := sqlExec(a.db, stmt, assetID, coinID)
	if err != nil {
		return err
	}
	if N != 1 {
		return fmt.Errorf("failed to active 1 bond, updated %d", N)
	}
	return nil
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
func createAccountTables(db sqlQueryExecutor) error {
	for _, c := range createAccountTableStatements {
		created, err := createTable(db, publicSchema, c.name)
		if err != nil {
			return err
		}
		if created {
			log.Tracef("Table %s created", c.name)
		}
	}

	for _, c := range createBondIndexesStatements {
		err := createIndexStmt(db, c.stmt, c.idxName, bondsTableName)
		if err != nil {
			return err
		}
	}

	return nil
}

// getAccount gets the account pubkey, whether the account has been
// registered, and whether the account is still open, in that order.
func getAccount(dbe sqlQueryer, tableName string, aid account.AccountID) (acct *account.Account, legacy, legacyPaid bool, err error) {
	var pubkey []byte
	stmt := fmt.Sprintf(internal.SelectAccount, tableName)
	err = dbe.QueryRow(stmt, aid).Scan(&pubkey, &legacy, &legacyPaid)
	if err != nil {
		return
	}
	acct, err = account.NewAccountFromPubKey(pubkey)
	if err != nil {
		return
	}
	return
}

// createAccount creates an entry for the account in the accounts table.
func createAccount(dbe sqlExecutor, tableName string, acct *account.Account, feeAsset uint32, regAddr string) error {
	stmt := fmt.Sprintf(internal.CreateAccount, tableName)
	_, err := dbe.Exec(stmt, acct.ID, acct.PubKey.SerializeCompressed(), feeAsset, regAddr)
	return err
}

func addBond(dbe sqlExecutor, tableName string, aid account.AccountID, bond *db.Bond) error {
	stmt := fmt.Sprintf(internal.AddBond, tableName)
	_, err := dbe.Exec(stmt, bond.CoinID, bond.AssetID, aid, bond.Script, bond.Amount, bond.LockTime, bond.Pending)
	return err
}

func getBondsForAccount(dbe sqlQueryer, tableName string, acct account.AccountID, bondExpiryTime int64) ([]*db.Bond, error) {
	stmt := fmt.Sprintf(internal.SelectActiveBondsForUser, tableName)
	rows, err := dbe.Query(stmt, acct, bondExpiryTime)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var bonds []*db.Bond
	for rows.Next() {
		var bond db.Bond
		err = rows.Scan(&bond.CoinID, &bond.AssetID, &bond.Script, &bond.Amount, &bond.LockTime, &bond.Pending)
		if err != nil {
			return nil, err
		}
		bonds = append(bonds, &bond)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	return bonds, nil
}

// accountRegAddr gets the registration fee address and its asset ID created for
// the specified account.
func accountRegAddr(dbe sqlQueryer, tableName string, aid account.AccountID) (string, uint32, error) {
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
func payAccount(dbe sqlExecutor, tableName string, aid account.AccountID, coinID []byte) (bool, error) {
	stmt := fmt.Sprintf(internal.SetRegOutput, tableName)
	res, err := dbe.Exec(stmt, coinID, aid)
	if err != nil {
		return false, err
	}
	rows, err := res.RowsAffected()
	return rows > 0, err
}
