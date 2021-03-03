package core

import (
	"encoding/hex"
	"fmt"

	"decred.org/dcrdex/client/db"
	"github.com/decred/dcrd/dcrec/secp256k1/v3"
)

// AccountDisable is used to disable an account by given host and application
// password.
func (c *Core) AccountDisable(pw []byte, host string) error {
	// Validate password.
	_, err := c.encryptionKey(pw)
	if err != nil {
		return codedError(passwordErr, err)
	}

	// Parse address.
	addr, err := addrHost(host)
	if err != nil {
		return newError(addressParseErr, "error parsing address: %v", err)
	}

	// Get the dexConnection.
	c.connMtx.RLock()
	defer c.connMtx.RUnlock()
	dc, found := c.conns[addr]
	if !found {
		return newError(unknownDEXErr, "DEX: %s", addr)
	}

	// Check active orders.
	if dc.hasActiveOrders() {
		return fmt.Errorf("cannot disable account with active orders")
	}

	acctInfo, err := c.db.Account(dc.acct.host)
	if err != nil {
		return newError(accountRetrieveErr, "Error retrieving account: %v", err)
	}
	err = c.db.DisableAccount(acctInfo)
	if err != nil {
		return newError(accountDisableErr, "Error disabling account: %v", err)
	}
	// Close dexConnection books.
	for _, book := range dc.books {
		book.close()
	}
	// Disconnect and delete connection from map.
	dc.connMaster.Disconnect()
	delete(c.conns, addr)

	return nil
}

// AccountExport is used to retrieve account by host for export.
func (c *Core) AccountExport(pw []byte, host string) (*Account, error) {
	crypter, err := c.encryptionKey(pw)
	if err != nil {
		return nil, codedError(passwordErr, err)
	}
	host, err = addrHost(host)
	if err != nil {
		return nil, newError(addressParseErr, "error parsing address: %v", err)
	}

	// Get the dexConnection and the dex.Asset for each asset.
	c.connMtx.RLock()
	dc, found := c.conns[host]
	c.connMtx.RUnlock()
	if !found {
		return nil, newError(unknownDEXErr, "DEX: %s", host)
	}

	// Unlock account if it is locked so that account id and privKey can be retrieved.
	if err = dc.acct.unlock(crypter); err != nil {
		return nil, codedError(acctKeyErr, err)
	}
	dc.acct.keyMtx.RLock()
	accountID := dc.acct.id.String()
	privKey := hex.EncodeToString(dc.acct.privKey.Serialize())
	dc.acct.keyMtx.RUnlock()

	feeProofSig := ""
	var feeProofStamp uint64
	if dc.acct.isPaid {
		accountProof, err := c.db.AccountProof(host)
		if err != nil {
			return nil, codedError(accountProofErr, err)
		}
		feeProofSig = hex.EncodeToString(accountProof.Sig)
		feeProofStamp = accountProof.Stamp
	}

	// Account ID is exported for informational purposes only, it is not used during import.
	acct := &Account{
		Host:          host,
		AccountID:     accountID,
		PrivKey:       privKey,
		DEXPubKey:     hex.EncodeToString(dc.acct.dexPubKey.SerializeCompressed()),
		Cert:          hex.EncodeToString(dc.acct.cert),
		FeeCoin:       hex.EncodeToString(dc.acct.feeCoin),
		FeeProofSig:   feeProofSig,
		FeeProofStamp: feeProofStamp,
	}
	return acct, nil
}

// AccountImport is used import an existing account into the db.
func (c *Core) AccountImport(pw []byte, acct Account) error {
	crypter, err := c.encryptionKey(pw)
	if err != nil {
		return codedError(passwordErr, err)
	}

	host, err := addrHost(acct.Host)
	if err != nil {
		return newError(addressParseErr, "error parsing address: %v", err)
	}
	accountInfo := db.AccountInfo{Host: host}

	DEXpubKey, err := hex.DecodeString(acct.DEXPubKey)
	if err != nil {
		return codedError(decodeErr, err)
	}
	accountInfo.DEXPubKey, err = secp256k1.ParsePubKey(DEXpubKey)
	if err != nil {
		return codedError(parseKeyErr, err)
	}

	accountInfo.Cert, err = hex.DecodeString(acct.Cert)
	if err != nil {
		return codedError(decodeErr, err)
	}

	accountInfo.FeeCoin, err = hex.DecodeString(acct.FeeCoin)
	if err != nil {
		return codedError(decodeErr, err)
	}

	privKey, err := hex.DecodeString(acct.PrivKey)
	if err != nil {
		return codedError(decodeErr, err)
	}
	accountInfo.EncKey, err = crypter.Encrypt(privKey)
	if err != nil {
		return codedError(encryptionErr, err)
	}

	accountInfo.Paid = acct.FeeProofSig != "" && acct.FeeProofStamp != 0

	// verifyAccount makes a connection to the DEX.
	if !c.verifyAccount(&accountInfo) {
		return newError(accountVerificationErr, "Account not verified for host: %s err: %v", host, err)
	}

	err = c.db.CreateAccount(&accountInfo)
	if err != nil {
		return codedError(dbErr, err)
	}

	if accountInfo.Paid {
		sig, err := hex.DecodeString(acct.FeeProofSig)
		if err != nil {
			return codedError(decodeErr, err)
		}
		accountProof := db.AccountProof{
			Host:  host,
			Stamp: acct.FeeProofStamp,
			Sig:   sig,
		}
		err = c.db.AccountPaid(&accountProof)
		if err != nil {
			return codedError(dbErr, err)
		}
	}

	return nil
}
