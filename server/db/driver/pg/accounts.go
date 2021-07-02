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
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/hdkeychain/v3"
	"github.com/decred/dcrd/txscript/v4/stdaddr"
)

// Account retrieves the account pubkey, active bonds, and if the account is has
// a legacy registration fee transaction recorded.
func (a *Archiver) Account(aid account.AccountID, bondExpiry time.Time) (acct *account.Account, bonds []*db.Bond, legacyFeePaid bool) {
	acct, legacyFeePaid, err := getAccount(a.db, a.tables.accounts, aid)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return nil, nil, false
	case err == nil:
	default:
		log.Errorf("getAccount error: %v", err)
		return nil, nil, false
	}

	bonds, err = getBondsForAccount(a.db, a.tables.bonds, aid, bondExpiry.Unix())
	switch {
	case errors.Is(err, sql.ErrNoRows):
		bonds = nil
	case err == nil:
	default:
		log.Errorf("getBondsForAccount error: %v", err)
		return nil, nil, false
	}

	return acct, bonds, legacyFeePaid
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
		err = rows.Scan(&a.AccountID, &a.Pubkey, &feeAddress, &a.FeeCoin)
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
		&acct.FeeCoin); err != nil {
		return nil, err
	}

	acct.FeeAddress = feeAddress.String
	return acct, nil
}

// CreateAccount creates an entry for a new account in the accounts table. A
// DCR registration fee address is created and returned.
// DEPRECATED: See CreateAccountWithBond.
func (a *Archiver) CreateAccount(acct *account.Account) (string, error) {
	ai, err := a.AccountInfo(acct.ID)
	if err == nil {
		// Account exists. Respond with either a fee address, or their (paid)
		// fee coin.
		if len(ai.FeeCoin) == 0 {
			return ai.FeeAddress, nil

		}
		return "", &db.ArchiveError{Code: db.ErrAccountExists, Detail: ai.FeeCoin.String()}
		// with tiers, this is no more: &db.ArchiveError{Code: db.ErrAccountSuspended}
	}
	if !errors.Is(err, sql.ErrNoRows) {
		log.Errorf("AccountInfo error for ID %s: %v", acct.ID, err)
		return "", db.ArchiveError{Code: db.ErrGeneralFailure}
	}

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

	err = createAccount(dbTx, a.tables.accounts, acct, "") // no reg fee addr
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

// createKeyEntry creates an entry for the pubkey hash if one doesn't already
// exist.
func (a *Archiver) createKeyEntry(keyHash []byte) error {
	err := createKeyEntry(a.db, a.tables.feeKeys, keyHash)
	if errors.Is(err, sql.ErrNoRows) {
		err = nil
	}
	return err
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
	addr, err := stdaddr.NewAddressPubKeyHashEcdsaSecp256k1V0(stdaddr.Hash160(pubKeyBytes), a.keyParams)
	if err != nil {
		log.Errorf("Failed to create creating new pubkey hash address: %v", err)
		return "", fmt.Errorf("error encoding fee address: %w", err)
	}

	return addr.String(), nil
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
func getAccount(dbe sqlQueryer, tableName string, aid account.AccountID) (acct *account.Account, legacyFeePaid bool, err error) {
	var coinID, pubkey []byte
	stmt := fmt.Sprintf(internal.SelectAccount, tableName)
	err = dbe.QueryRow(stmt, aid).Scan(&pubkey, &coinID)
	if err != nil {
		return
	}
	acct, err = account.NewAccountFromPubKey(pubkey)
	if err != nil {
		return
	}
	legacyFeePaid = len(coinID) > 1
	return
}

// createAccount creates an entry for the account in the accounts table.
func createAccount(dbe sqlExecutor, tableName string, acct *account.Account, regAddr string) error {
	stmt := fmt.Sprintf(internal.CreateAccount, tableName)
	_, err := dbe.Exec(stmt, acct.ID, acct.PubKey.SerializeCompressed(), regAddr)
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

// accountRegAddr gets the registration fee address created for the specified
// account.
func accountRegAddr(dbe sqlQueryer, tableName string, aid account.AccountID) (string, error) {
	var addr string
	stmt := fmt.Sprintf(internal.SelectRegAddress, tableName)
	err := dbe.QueryRow(stmt, aid).Scan(&addr)
	if err != nil {
		return "", err
	}
	return addr, nil
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

// createKeyEntry creates an entry for the pubkey (hash) if it doesn't already
// exist.
func createKeyEntry(db *sql.DB, tableName string, keyHash []byte) error {
	stmt := fmt.Sprintf(internal.InsertKeyIfMissing, tableName)
	_, err := db.Exec(stmt, keyHash)
	return err
}
