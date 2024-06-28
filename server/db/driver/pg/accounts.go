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

// Account retrieves the account pubkey, active bonds, and if the account has a
// legacy registration fee address and transaction recorded. If the account does
// not exist or there is in an error retrieving any data, a nil *account.Account
// is returned.
func (a *Archiver) Account(aid account.AccountID, bondExpiry time.Time) (acct *account.Account, bonds []*db.Bond) {
	acct, err := getAccount(a.db, a.tables.accounts, aid)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return nil, nil
	case err == nil:
	default:
		log.Errorf("getAccount error: %v", err)
		return nil, nil
	}

	bonds, err = getBondsForAccount(a.db, a.tables.bonds, aid, bondExpiry.Unix())
	switch {
	case errors.Is(err, sql.ErrNoRows):
		bonds = nil
	case err == nil:
	default:
		log.Errorf("getBondsForAccount error: %v", err)
		return nil, nil
	}

	return acct, bonds
}

// AccountInfo returns data for an account.
func (a *Archiver) AccountInfo(aid account.AccountID) (*db.Account, error) {
	// bondExpiry time.Time and bonds return needed?
	stmt := fmt.Sprintf(internal.SelectAccountInfo, a.tables.accounts)
	acct := new(db.Account)
	if err := a.db.QueryRow(stmt, aid).Scan(&acct.AccountID, &acct.Pubkey); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			err = db.ArchiveError{Code: db.ErrAccountUnknown}
		}
		return nil, err
	}
	return acct, nil
}

// CreateAccountWithBond creates a new account with a fidelity bond.
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

	err = createAccountForBond(dbTx, a.tables.accounts, acct)
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

func (a *Archiver) DeleteBond(assetID uint32, coinID []byte) error {
	return deleteBond(a.db, a.tables.bonds, assetID, coinID)
}

func (a *Archiver) FetchPrepaidBond(coinID []byte) (strength uint32, lockTime int64, err error) {
	stmt := fmt.Sprintf(internal.SelectPrepaidBond, prepaidBondsTableName)
	err = a.db.QueryRow(stmt, coinID).Scan(&strength, &lockTime)
	return
}

func (a *Archiver) DeletePrepaidBond(coinID []byte) (err error) {
	stmt := fmt.Sprintf(internal.DeletePrepaidBond, prepaidBondsTableName)
	_, err = a.db.ExecContext(a.ctx, stmt, coinID)
	return
}

func (a *Archiver) StorePrepaidBonds(coinIDs [][]byte, strength uint32, lockTime int64) error {
	stmt := fmt.Sprintf(internal.InsertPrepaidBond, prepaidBondsTableName)
	for i := range coinIDs {
		if _, err := a.db.ExecContext(a.ctx, stmt, coinIDs[i], strength, lockTime); err != nil {
			return err
		}
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

// getAccount gets retrieves the account details, including the pubkey, a flag
// indicating if the account was created with a legacy fee address (not a
// fidelity bond), and a flag indicating if that legacy fee was paid.
func getAccount(dbe sqlQueryer, tableName string, aid account.AccountID) (acct *account.Account, err error) {
	var pubkey []byte
	stmt := fmt.Sprintf(internal.SelectAccount, tableName)
	err = dbe.QueryRow(stmt, aid).Scan(&pubkey)
	if err != nil {
		return
	}
	acct, err = account.NewAccountFromPubKey(pubkey)
	if err != nil {
		return
	}
	return
}

// createAccountForBond creates an entry for the account in the accounts table.
func createAccountForBond(dbe sqlExecutor, tableName string, acct *account.Account) error {
	stmt := fmt.Sprintf(internal.CreateAccountForBond, tableName)
	_, err := dbe.Exec(stmt, acct.ID, acct.PubKey.SerializeCompressed())
	return err
}

func addBond(dbe sqlExecutor, tableName string, aid account.AccountID, bond *db.Bond) error {
	stmt := fmt.Sprintf(internal.AddBond, tableName)
	_, err := dbe.Exec(stmt, bond.Version, bond.CoinID, bond.AssetID, aid,
		bond.Amount, bond.Strength, bond.LockTime)
	return err
}

func deleteBond(dbe sqlExecutor, tableName string, assetID uint32, coinID []byte) error {
	stmt := fmt.Sprintf(internal.DeleteBond, tableName)
	_, err := dbe.Exec(stmt, coinID, assetID)
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
		err = rows.Scan(&bond.Version, &bond.CoinID, &bond.AssetID,
			&bond.Amount, &bond.Strength, &bond.LockTime)
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
