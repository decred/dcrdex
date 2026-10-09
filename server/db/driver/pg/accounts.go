// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package pg

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"decred.org/dcrdex/server/account"
	"decred.org/dcrdex/server/db"
	"decred.org/dcrdex/server/db/driver/pg/internal"
	"decred.org/dcrdex/server/meshevents"
)

// Account returns the account and bonds whose lock time is at least
// lockTimeThresh. It returns a nil account and nil error if the account
// does not exist.
func (a *Archiver) Account(ctx context.Context, aid account.AccountID, lockTimeThresh time.Time) (acct *account.Account, bonds []*db.Bond, err error) {
	acct, err = getAccount(ctx, a.db, a.tables.accounts, aid)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return nil, nil, nil
	case err != nil:
		return nil, nil, fmt.Errorf("getAccount error: %w", err)
	}

	bonds, err = getBondsForAccount(ctx, a.db, a.tables.bonds, aid, lockTimeThresh.Unix())
	if err != nil {
		return nil, nil, fmt.Errorf("getBondsForAccount error: %w", err)
	}

	return acct, bonds, nil
}

// ApplyBondPostedEvent stores the account and bond changes with the bond_posted
// event log entry in one transaction, and returns the account's updated bonds
// and latest reputation outcomes, up to the specified limits.
func (a *Archiver) ApplyBondPostedEvent(ctx context.Context, meta *db.EventLogMeta, event *meshevents.BondPostedEvent, pimgSz, matchSz, orderSz int) (*db.BondPostedResult, error) {
	txData, err := event.EventTxData()
	if err != nil {
		return nil, err
	}
	acct, err := event.PostedAccount()
	if err != nil {
		return nil, err
	}
	bond := &db.Bond{
		Version:  event.Bond.Version,
		AssetID:  event.Bond.AssetID,
		CoinID:   event.Bond.CoinID,
		Amount:   event.Bond.Amount,
		Strength: event.Bond.Strength,
		LockTime: event.Bond.LockTime,
	}

	prepaid := bond.AssetID == account.PrepaidBondID
	result := new(db.BondPostedResult)
	loadReputationInputs := func(tx *sql.Tx) error {
		var err error
		result.Bonds, err = getBondsForAccount(ctx, tx, a.tables.bonds, acct.ID, time.Time{}.Unix())
		if err != nil {
			return err
		}
		stmt := tx.StmtContext(ctx, a.queries.selectPoints)
		defer stmt.Close()
		result.Preimages, result.Matches, result.Orders, err = getUserReputationData(ctx, stmt, acct.ID, pimgSz, matchSz, orderSz)
		return err
	}
	logEntry, err := a.applyEventTx(ctx, meta, meshevents.EventKindBondPosted, txData, func(dbTx *sql.Tx) error {
		storedAcct, err := getAccount(ctx, dbTx, a.tables.accounts, acct.ID)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			if err := createAccountForBond(dbTx, a.tables.accounts, acct); err != nil {
				return err
			}
		case err != nil:
			return err
		case storedAcct.PubKey == nil || !bytes.Equal(storedAcct.PubKey.SerializeCompressed(), acct.PubKey.SerializeCompressed()):
			return fmt.Errorf("bond_posted account pubkey mismatch for %v", acct.ID)
		}

		bondOwner, err := getBondAccount(dbTx, a.tables.bonds, bond.AssetID, bond.CoinID)
		switch {
		case err == nil:
			if bondOwner != acct.ID {
				return fmt.Errorf("bond_posted bond %x asset %d already belongs to account %v",
					bond.CoinID, bond.AssetID, bondOwner)
			}
			// A retry must succeed even if the prepaid token was already consumed.
			return loadReputationInputs(dbTx)
		case !errors.Is(err, sql.ErrNoRows):
			return err
		}

		if prepaid {
			tokenStrength, tokenLockTime, err := getPrepaidBond(dbTx, a.tables.prepaidBonds, bond.CoinID)
			if errors.Is(err, sql.ErrNoRows) {
				return fmt.Errorf("bond_posted pre-paid bond %x not found", bond.CoinID)
			}
			if err != nil {
				return err
			}
			if tokenStrength != bond.Strength {
				return fmt.Errorf("bond_posted pre-paid bond %x strength mismatch: got %d, want %d",
					bond.CoinID, bond.Strength, tokenStrength)
			}
			if tokenLockTime != bond.LockTime {
				return fmt.Errorf("bond_posted pre-paid bond %x lock time mismatch: got %d, want %d",
					bond.CoinID, bond.LockTime, tokenLockTime)
			}
		}

		if err := addBond(dbTx, a.tables.bonds, acct.ID, bond); err != nil {
			return err
		}
		if prepaid {
			if err := deletePrepaidBond(dbTx, a.tables.prepaidBonds, bond.CoinID); err != nil {
				return err
			}
		}
		result.BondAdded = true
		return loadReputationInputs(dbTx)
	})
	a.notifyRepInputsOnCommit(err, acct.ID)
	if err != nil {
		return nil, err
	}
	result.Log = logEntry
	return result, nil
}

// ApplyPrepaidBondsCreatedEvent stores the prepaid bond tokens and the
// prepaid_bonds_created event log entry in one transaction.
func (a *Archiver) ApplyPrepaidBondsCreatedEvent(ctx context.Context, meta *db.EventLogMeta, event *meshevents.PrepaidBondsCreatedEvent) (*db.EventLogEntry, error) {
	if err := event.Validate(); err != nil {
		return nil, err
	}
	txData, err := event.EventTxData()
	if err != nil {
		return nil, err
	}

	return a.applyEventTx(ctx, meta, meshevents.EventKindPrepaidBondsCreated, txData, func(dbTx *sql.Tx) error {
		for _, bond := range event.Bonds {
			if err := insertPrepaidBond(dbTx, a.tables.prepaidBonds, bond); err != nil {
				return err
			}
		}
		return nil
	})
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

func (a *Archiver) FetchPrepaidBond(coinID []byte) (strength uint32, lockTime int64, err error) {
	return getPrepaidBond(a.db, a.tables.prepaidBonds, coinID)
}

// createAccountTables creates the account-related tables.
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

// getAccount retrieves an account from its stored public key.
func getAccount(ctx context.Context, dbe sqlQueryer, tableName string, aid account.AccountID) (acct *account.Account, err error) {
	var pubkey []byte
	stmt := fmt.Sprintf(internal.SelectAccount, tableName)
	err = dbe.QueryRowContext(ctx, stmt, aid).Scan(&pubkey)
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

func getBondsForAccount(ctx context.Context, dbe sqlQueryer, tableName string, acct account.AccountID, bondExpiryTime int64) ([]*db.Bond, error) {
	stmt := fmt.Sprintf(internal.SelectActiveBondsForUser, tableName)
	rows, err := dbe.QueryContext(ctx, stmt, acct, bondExpiryTime)
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

func getBondAccount(dbe sqlQueryer, tableName string, assetID uint32, coinID []byte) (acct account.AccountID, err error) {
	stmt := fmt.Sprintf(internal.SelectBondAccount, tableName)
	err = dbe.QueryRow(stmt, coinID, assetID).Scan(&acct)
	return
}

func getPrepaidBond(dbe sqlQueryer, tableName string, coinID []byte) (strength uint32, lockTime int64, err error) {
	stmt := fmt.Sprintf(internal.SelectPrepaidBond, tableName)
	err = dbe.QueryRow(stmt, coinID).Scan(&strength, &lockTime)
	return
}

func deletePrepaidBond(dbe sqlExecutor, tableName string, coinID []byte) error {
	stmt := fmt.Sprintf(internal.DeletePrepaidBond, tableName)
	_, err := dbe.Exec(stmt, coinID)
	return err
}

func insertPrepaidBond(dbe sqlExecutor, tableName string, bond *meshevents.PrepaidBond) error {
	stmt := fmt.Sprintf(internal.InsertPrepaidBond, tableName)
	_, err := dbe.Exec(stmt, bond.CoinID, bond.Strength, bond.LockTime)
	return err
}
