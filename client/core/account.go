package core

import (
	"bytes"
	"encoding/hex"
	"errors"
	"fmt"

	"decred.org/dcrdex/client/comms"
	"decred.org/dcrdex/client/db"
	"decred.org/dcrdex/server/account"
	"github.com/decred/dcrd/dcrec/secp256k1/v4"
)

// disconnectDEX unsubscribes from the dex's orderbooks, ends the connection
// with the dex, and removes it from the connection map.
func (c *Core) disconnectDEX(dc *dexConnection) {
	// Stop dexConnection books.
	dc.cfgMtx.RLock()
	if dc.cfg != nil {
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

	// Check active orders or bonds.
	if dc.hasActiveOrders() {
		return fmt.Errorf("cannot disable account with active orders")
	}
	if dc.hasUnspentBond() {
		return fmt.Errorf("cannot disable account with unspent bonds")
	}

	err = c.db.DisableAccount(dc.acct.host)
	if err != nil {
		return newError(accountDisableErr, "error disabling account: %w", err)
	}

	c.disconnectDEX(dc)

	return nil
}

// AccountExport is used to retrieve account by host for export.
func (c *Core) AccountExport(pw []byte, host string) (*Account, []*db.Bond, error) {
	crypter, err := c.encryptionKey(pw)
	if err != nil {
		return nil, nil, codedError(passwordErr, err)
	}
	defer crypter.Close()
	host, err = addrHost(host)
	if err != nil {
		return nil, nil, newError(addressParseErr, "error parsing address: %w", err)
	}

	// Load account info, including all bonds, from DB.
	acctInf, err := c.db.Account(host)
	if err != nil {
		return nil, nil, newError(unknownDEXErr, "dex db load error: %w", err)
	}

	keyB, err := crypter.Decrypt(acctInf.EncKey())
	if err != nil {
		return nil, nil, err
	}
	privKey := secp256k1.PrivKeyFromBytes(keyB)
	pubKey := privKey.PubKey()
	accountID := account.NewID(pubKey.SerializeCompressed())

	var feeProofSig string
	var feeProofStamp uint64
	if acctInf.LegacyFeePaid {
		accountProof, err := c.db.AccountProof(host)
		if err != nil {
			return nil, nil, codedError(accountProofErr, err)
		}
		feeProofSig = hex.EncodeToString(accountProof.Sig)
		feeProofStamp = accountProof.Stamp
	}

	// Account ID is exported for informational purposes only, it is not used during import.
	acct := &Account{
		Host:      host,
		AccountID: accountID.String(),
		// PrivKey: Note that we don't differentiate between legacy and
		// hierarchical private keys here. On import, all keys are treated as
		// legacy keys.
		PrivKey:       hex.EncodeToString(keyB),
		DEXPubKey:     hex.EncodeToString(acctInf.DEXPubKey.SerializeCompressed()),
		Cert:          hex.EncodeToString(acctInf.Cert),
		FeeCoin:       hex.EncodeToString(acctInf.LegacyFeeCoin),
		FeeProofSig:   feeProofSig,
		FeeProofStamp: feeProofStamp,
	}
	return acct, acctInf.Bonds, nil
}

// AccountImport is used import an existing account into the db.
func (c *Core) AccountImport(pw []byte, acct *Account, bonds []*db.Bond) error {
	crypter, err := c.encryptionKey(pw)
	if err != nil {
		return codedError(passwordErr, err)
	}

	host, err := addrHost(acct.Host)
	if err != nil {
		return newError(addressParseErr, "error parsing address: %w", err)
	}

	// Don't try to create and import an account for a DEX that we are already
	// connected to.
	c.connMtx.RLock()
	_, connected := c.conns[host]
	c.connMtx.RUnlock()
	if connected {
		return errors.New("already connected")
	}
	_, err = c.db.Account(host) // may just not be in the conns map
	if err == nil {
		return errors.New("account already exists")
	}

	accountInfo := db.AccountInfo{
		Host:  host,
		Bonds: bonds,
	}

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

	accountInfo.LegacyFeeCoin, err = hex.DecodeString(acct.FeeCoin)
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

	accountInfo.LegacyFeePaid = acct.FeeProofSig != "" && acct.FeeProofStamp != 0

	err = c.db.CreateAccount(&accountInfo)
	if err != nil {
		return codedError(dbErr, err)
	}

	if accountInfo.LegacyFeePaid {
		sig, err := hex.DecodeString(acct.FeeProofSig)
		if err != nil {
			return codedError(decodeErr, err)
		}
		accountProof := db.AccountProof{
			Host:  host,
			Stamp: acct.FeeProofStamp,
			Sig:   sig,
		}
		err = c.db.StoreAccountProof(&accountProof)
		if err != nil {
			return codedError(dbErr, err)
		}
	}

	c.initializeDEXConnections(crypter)
	return nil
}

// UpdateCert attempts to connect to a server using a new TLS certificate. If
// the connection is successful, then the cert in the database is updated.
// Updating cert for already connected dex will return an error.
func (c *Core) UpdateCert(host string, cert []byte) error {
	c.connMtx.RLock()
	dc, found := c.conns[host]
	c.connMtx.RUnlock()
	if found && dc.status() == comms.Connected {
		return errors.New("dex is already connected")
	}

	acct, err := c.db.Account(host)
	if err != nil {
		return err
	}

	// Ensure user provides a new cert.
	if bytes.Equal(acct.Cert, cert) {
		return errors.New("provided cert is the same with the old cert")
	}

	// Stop reconnect retry for previous dex connection first but leave it in
	// the map so it remains listed in case we need it in the interim.
	if found {
		dc.connMaster.Disconnect()
		dc.acct.lock()
		dc.booksMtx.Lock()
		for m, b := range dc.books {
			b.closeFeeds()
			if b.closeTimer != nil {
				b.closeTimer.Stop()
			}
			delete(dc.books, m)
		}
		dc.booksMtx.Unlock()
	}

	acct.Cert = cert
	dc, err = c.connectDEX(acct)
	if err != nil {
		if dc != nil {
			dc.connMaster.Disconnect() // stop any retry loop for this new connection.
		}
		return fmt.Errorf("failed to connect using new cert (will attempt to restore old connection): %v", err)
	}

	err = c.db.UpdateAccountInfo(acct)
	if err != nil {
		return fmt.Errorf("failed to update account info: %w", err)
	}

	c.addDexConnection(dc)

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
	defer crypter.Close()

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

	if !oldDc.acct.isViewOnly() { // view-only dc should not discoverAcct
		_, err = c.discoverAccount(newDc, crypter)
		if err != nil {
			return nil, err
		}
	}

	err = c.db.DisableAccount(oldDc.acct.host)
	if err != nil {
		return nil, newError(accountDisableErr, "error disabling account: %w", err)
	}

	updatedHost = true
	return newDc.exchangeInfo(), nil
}
