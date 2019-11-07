// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package pg

import (
	"database/sql"
	"fmt"

	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/dcrutil/v2"
	"github.com/decred/dcrd/hdkeychain/v2"
	"github.com/decred/dcrdex/server/account"
	"github.com/decred/dcrdex/server/db/driver/pg/internal"
)

// CloseAccount closes the account by setting the value of the rule column.
func (a *Archiver) CloseAccount(aid account.AccountID, rule account.Rule) {
	err := closeAccount(a.db, a.tables.accounts, aid, rule)
	if err != nil {
		log.Errorf("error closing account %s (rule %d): %v", aid, rule, err)
	}
}

// Account retreives the account pubkey, whether the account is paid, and
// whether the account is open, in that order.
func (a *Archiver) Account(aid account.AccountID) (*account.Account, bool, bool) {
	acct, isPaid, isOpen, err := getAccount(a.db, a.tables.accounts, aid)
	switch err {
	case sql.ErrNoRows:
		return nil, false, false
	case nil:
	default:
		log.Errorf("getAccount error: %v", err)
		return nil, false, false
	}
	return acct, isPaid, isOpen
}

// CreateAccount creates an entry for a new account in the accounts table. A
// DCR registration fee address is created and returned.
func (a *Archiver) CreateAccount(acct *account.Account) (string, error) {
	regAddr, err := a.getNextAddress()
	if err != nil {
		return "", fmt.Errorf("error creating registration address: %v", err)
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
func (a *Archiver) PayAccount(aid account.AccountID, txid string, vout uint32) error {
	if len(txid) != chainhash.MaxHashStringSize {
		return fmt.Errorf("incorrect length transaction ID %s", txid)
	}
	txHash, err := chainhash.NewHashFromStr(txid)
	if err != nil {
		return err
	}
	ok, err := payAccount(a.db, a.tables.accounts, aid, txHash, vout)
	if !ok {
		return fmt.Errorf("no accounts updated")
	}
	return err
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
		switch err {
		case hdkeychain.ErrInvalidChild:
			continue
		case nil:
			break out
		default:
			log.Errorf("error creating child key: %v", err)
			return "", fmt.Errorf("error generating fee address")
		}
	}
	pubKey, err := childExtKey.ECPubKey()
	if err != nil {
		log.Errorf("error getting PublicKey from child ExtendedKey: %v", err)
		return "", fmt.Errorf("error creating fee address")
	}
	addr, err := dcrutil.NewAddressSecpPubKey(pubKey.Serialize(), a.keyParams)
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
			log.Infof("Table %s created", c.name)
		}
	}
	return nil
}

// closeAccount closes the account by setting the rule column value.
func closeAccount(dbe sqlExecutor, tableName string, aid account.AccountID, rule account.Rule) error {
	stmt := fmt.Sprintf(internal.CloseAccount, tableName)
	_, err := dbe.Exec(stmt, rule, aid)
	return err
}

// getAccount gets the account pubkey, whether the account has been
// registered, and whether the account is still open, in that order.
func getAccount(dbe *sql.DB, tableName string, aid account.AccountID) (*account.Account, bool, bool, error) {
	var txid, pubkey []byte
	var rule uint8
	stmt := fmt.Sprintf(internal.SelectAccount, tableName)
	err := dbe.QueryRow(stmt, aid).Scan(&pubkey, &txid, &rule)
	if err != nil {
		return nil, false, false, err
	}
	acct, err := account.NewAccountFromPubKey(pubkey)
	return acct, len(txid) > 1, rule == 0, err
}

// createAccount creates an entry for the account in the accounts table.
func createAccount(dbe sqlExecutor, tableName string, acct *account.Account, regAddr string) error {
	stmt := fmt.Sprintf(internal.CreateAccount, tableName)
	_, err := dbe.Exec(stmt, acct.ID, acct.PubKey.Serialize(), regAddr)
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
func payAccount(dbe *sql.DB, tableName string, aid account.AccountID, txHash *chainhash.Hash, vout uint32) (bool, error) {
	stmt := fmt.Sprintf(internal.SetRegOutput, tableName)
	res, err := dbe.Exec(stmt, txHash[:], vout, aid)
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
