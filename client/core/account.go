package core

import (
	"encoding/hex"
	"errors"
	"fmt"

	"decred.org/dcrdex/client/db"
	"github.com/decred/dcrd/dcrec/secp256k1/v4"
)

// disconnectDEX unsubscribes from the dex's orderbooks, ends the connection
// with the dex, and removes it from the connection map.
func (c *Core) disconnectDEX(dc *dexConnection) {
	// Stop dexConnection books.
	dc.cfgMtx.RLock()
	for _, m := range dc.cfg.Markets {
		// Empty bookie's feeds map, close feeds' channels & stop close timers.
		dc.booksMtx.Lock()
		if b, found := dc.books[m.Name]; found {
			b.closeFeeds()
			if b.closeTimer != nil {
				b.closeTimer.Stop()
			}
		}
		dc.booksMtx.Unlock()
		dc.stopBook(m.Base, m.Quote)
	}
	dc.cfgMtx.RUnlock()
	// Disconnect and delete connection from map.
	dc.connMaster.Disconnect()
	c.connMtx.Lock()
	delete(c.conns, dc.acct.host)
	c.connMtx.Unlock()
}

// AccountDisable is used to disable an account by given host and application
// password.
func (c *Core) AccountDisable(pw []byte, addr string) error {
	// Validate password.
	_, err := c.encryptionKey(pw)
	if err != nil {
		return codedError(passwordErr, err)
	}

	// Get dex connection by host.
	dc, _, err := c.dex(addr)
	if err != nil {
		return newError(unknownDEXErr, "error retrieving dex conn: %w", err)
	}

	// Check active orders.
	if dc.hasActiveOrders() {
		return fmt.Errorf("cannot disable account with active orders")
	}

	err = c.db.DisableAccount(dc.acct.host)
	if err != nil {
		return newError(accountDisableErr, "error disabling account: %w", err)
	}

	c.disconnectDEX(dc)

	return nil
}

// AccountExport is used to retrieve account by host for export.
func (c *Core) AccountExport(pw []byte, host string) (*Account, error) {
	crypter, err := c.encryptionKey(pw)
	if err != nil {
		return nil, codedError(passwordErr, err)
	}
	defer crypter.Close()
	host, err = addrHost(host)
	if err != nil {
		return nil, newError(addressParseErr, "error parsing address: %w", err)
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
		Host:      host,
		AccountID: dc.acct.id.String(),
		// PrivKey: Note that we don't differentiate between legacy and
		// hierarchical private keys here. On import, all keys are treated as
		// legacy keys.
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
		return newError(addressParseErr, "error parsing address: %w", err)
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
	accountInfo.LegacyEncKey, err = crypter.Encrypt(privKey)
	if err != nil {
		return codedError(encryptionErr, err)
	}

	accountInfo.Paid = acct.FeeProofSig != "" && acct.FeeProofStamp != 0

	// Make a connection to the DEX.
	if dc, connected := c.connectAccount(&accountInfo); !connected {
		if dc != nil {
			dc.connMaster.Disconnect() // stop reconnect loop
			c.connMtx.Lock()
			delete(c.conns, dc.acct.host)
			c.connMtx.Unlock()
		}
		return newError(accountVerificationErr, "Account not verified for host: %s", host)
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

// UpdateCert attempts to connect to a server using a new TLS certificate. If
// the connection is successful, then the cert in the database is updated.
func (c *Core) UpdateCert(host string, cert []byte) error {
	accountInfo, err := c.db.Account(host)
	if err != nil {
		return err
	}

	accountInfo.Cert = cert

	_, connected := c.connectAccount(accountInfo)
	if !connected {
		return errors.New("failed to connect using new cert")
	}

	err = c.db.UpdateAccountInfo(accountInfo)
	if err != nil {
		return fmt.Errorf("failed to update account info: %w", err)
	}

	return nil
}

// UpdateDEXHost updates the host for a connection to a dex. The dex at oldHost
// and newHost must be the same dex, which means that the dex at both hosts use
// the same public key.
func (c *Core) UpdateDEXHost(oldHost, newHost string, appPW []byte, certI interface{}) (*Exchange, error) {
	if oldHost == newHost {
		return nil, errors.New("old host and new host are the same")
	}

	crypter, err := c.encryptionKey(appPW)
	if err != nil {
		return nil, codedError(passwordErr, err)
	}

	oldDc, _, err := c.dex(oldHost)
	if err != nil {
		return nil, err
	}

	if oldDc.hasActiveOrders() {
		return nil, fmt.Errorf("cannot update host while dex has active orders")
	}

	if oldDc.acct.dexPubKey == nil {
		return nil, fmt.Errorf("cannot update host if dex public key is nil")
	}

	if oldDc.getPendingFee() != nil {
		// The notify fee code will have to be completely refactored to allow
		// this.
		return nil, fmt.Errorf("host cannot be updated while registration fee is pending")
	}

	var updatedHost bool
	newDc, err := c.tempDexConnection(newHost, certI)
	if newDc != nil { // (re)connect loop may be running even if err != nil
		defer func() {
			// Either disconnect or promote this connection.
			if !updatedHost {
				newDc.connMaster.Disconnect()
				return
			}
			c.upgradeConnection(newDc)
		}()
	}
	if err != nil {
		return nil, err
	}

	if !newDc.acct.dexPubKey.IsEqual(oldDc.acct.dexPubKey) {
		return nil, fmt.Errorf("the dex at %s does not have the same public key as %s",
			oldHost, newHost)
	}

	c.disconnectDEX(oldDc)

	_, err = c.discoverAccount(newDc, crypter)
	if err != nil {
		return nil, err
	}

	err = c.db.DisableAccount(oldDc.acct.host)
	if err != nil {
		return nil, newError(accountDisableErr, "error disabling account: %w", err)
	}

	updatedHost = true
	return newDc.exchangeInfo(), nil
}
