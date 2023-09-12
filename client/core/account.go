package core

import (
	"bytes"
	"encoding/hex"
	"errors"
	"fmt"
	"math"

	"decred.org/dcrdex/client/comms"
	"decred.org/dcrdex/client/db"
	"decred.org/dcrdex/dex/encode"
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

	// Don't try to create and import an account for a DEX that we already know,
	// but try to import missing bonds.
	if acctInfo, err := c.db.Account(host); err == nil {
		// Before importing bonds, make sure this is the same DEX (by public
		// key) and same account ID, otherwise the bonds do not apply. The user
		// can still refund by manually broadcasting the backup refund tx.
		if acct.DEXPubKey != hex.EncodeToString(acctInfo.DEXPubKey.SerializeCompressed()) {
			return errors.New("known dex host has different public key")
		}
		keyB, err := crypter.Decrypt(acctInfo.EncKey())
		if err != nil {
			return err
		}
		defer encode.ClearBytes(keyB)
		privKey := secp256k1.PrivKeyFromBytes(keyB)
		defer privKey.Zero()
		accountID := account.NewID(privKey.PubKey().SerializeCompressed())
		if acct.AccountID != accountID.String() {
			return errors.New("known dex account has different identity")
		}

		c.log.Infof("Found existing account for %s. Merging bonds...", host)
		haveBond := func(bond *db.Bond) *db.Bond {
			for _, knownBond := range acctInfo.Bonds {
				if bytes.Equal(knownBond.UniqueID(), bond.UniqueID()) {
					return knownBond
				}
			}
			return nil
		}
		var newLiveBonds int
		for _, bond := range bonds {
			have := haveBond(bond)
			if have != nil && have.KeyIndex != math.MaxUint32 {
				continue // we have this proper (not placeholder) bond already
			}
			if err = c.db.AddBond(host, bond); err != nil { // add OR update
				return fmt.Errorf("importing bond: %v", err)
			}
			if have == nil {
				acctInfo.Bonds = append(acctInfo.Bonds, bond)
			} else { // else this is the placeholder from Unknown active bond reported by server
				*have = *bond // update element in acctInfo.Bonds slice
			}
			if !bond.Refunded {
				newLiveBonds++
			}
		}
		if newLiveBonds == 0 {
			return nil
		}
		c.log.Infof("Imported %d new unspent bonds", newLiveBonds)
		if dc, connected, _ := c.dex(host); connected {
			c.disconnectDEX(dc)
			// TODO: less heavy handed approach to append or update
			// dc.acct.{bonds,pendingBonds,expiredBonds}, using server config...
		}
		dc, err := c.connectDEX(acctInfo)
		if err != nil {
			return err
		}
		c.addDexConnection(dc)
		c.initializeDEXConnections(crypter)
		return nil
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

	accountInfo.LegacyFeePaid = acct.FeeProofSig != "" && acct.FeeProofStamp != 0

	// Before we import the private key as LegacyEncKey, see if the account
	// derives from the app seed. Somewhat inconsequential except for logging
	// and use of the appropriate enc key field.
	privKey, err := hex.DecodeString(acct.PrivKey)
	if err != nil {
		return codedError(decodeErr, err)
	}
	encKey, err := crypter.Encrypt(privKey)
	if err != nil {
		return codedError(encryptionErr, err)
	}
	dcAcct := newDEXAccount(&accountInfo, false)
	creds := c.creds()
	const maxRecoveryIndex = 1000
	for keyIndex := uint32(0); keyIndex < maxRecoveryIndex; keyIndex++ {
		err := dcAcct.setupCryptoV2(creds, crypter, keyIndex)
		if err != nil {
			return newError(acctKeyErr, "setupCryptoV2 error: %w", err)
		}
		if bytes.Equal(privKey, dcAcct.privKey.Serialize()) {
			c.log.Debugf("Account derives from current application seed, with account key index %d", keyIndex)
			accountInfo.EncKeyV2 = encKey
			// Any unspent bonds for this account will refund using KeyIndex.
			break
		}
	}
	if len(accountInfo.EncKeyV2) == 0 {
		c.log.Warnf("Account with foreign key imported. " +
			"Any imported bonds will be refunded to the previous wallet!")
		accountInfo.LegacyEncKey = encKey
		// Any unspent bonds for this account will refund using the backup tx.
	}
	dcAcct.privKey.Zero()

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

	dc, err := c.connectDEX(&accountInfo)
	if err != nil {
		return err
	}
	c.addDexConnection(dc)
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
	// the map so it remains listed incase we need it in the interim.
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
func (c *Core) UpdateDEXHost(oldHost, newHost string, appPW []byte, certI any) (*Exchange, error) {
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
	if err != nil {
		return nil, err
	}

	defer func() {
		// Either disconnect or promote this connection.
		if !updatedHost {
			newDc.connMaster.Disconnect()
			return
		}
		c.upgradeConnection(newDc)
	}()

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
