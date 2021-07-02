package core

import (
	"encoding/hex"
	"fmt"

	"decred.org/dcrdex/client/db"
	"decred.org/dcrdex/server/account"
	"github.com/decred/dcrd/dcrec/secp256k1/v3"
)

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
		return newError(unknownDEXErr, "error retrieving dex conn: %v", err)
	}

	// Check active orders.
	if dc.hasActiveOrders() {
		return fmt.Errorf("cannot disable account with active orders")
	}

	err = c.db.DisableAccount(dc.acct.host)
	if err != nil {
		return newError(accountDisableErr, "error disabling account: %v", err)
	}
	// Stop dexConnection books.
	dc.cfgMtx.RLock()
	for _, m := range dc.cfg.Markets {
		// Empty bookie's feeds map, close feeds' channels & stop close timers.
		dc.booksMtx.Lock()
		if b, found := dc.books[m.Name]; found {
			b.mtx.Lock()
			for _, f := range b.feeds {
				close(f.C)
			}
			b.feeds = make(map[uint32]*BookFeed, 1)
			if b.closeTimer != nil {
				b.closeTimer.Stop()
			}
			b.mtx.Unlock()
		}
		dc.booksMtx.Unlock()

		dc.stopBook(m.Base, m.Quote)
	}
	dc.cfgMtx.RUnlock()
	// Disconnect and delete connection from map.
	dc.connMaster.Disconnect()
	delete(c.conns, dc.acct.host)

	return nil
}

// AccountExport is used to retrieve account by host for export.
func (c *Core) AccountExport(pw []byte, host string) (*Account, []*db.Bond, error) {
	crypter, err := c.encryptionKey(pw)
	if err != nil {
		return nil, nil, codedError(passwordErr, err)
	}
	host, err = addrHost(host)
	if err != nil {
		return nil, nil, newError(addressParseErr, "error parsing address: %v", err)
	}

	// Load account info, including all bonds, from DB.
	acctInf, err := c.db.Account(host)
	if err != nil {
		return nil, nil, newError(unknownDEXErr, "dex db load error: %v", err)
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
		return newError(addressParseErr, "error parsing address: %v", err)
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

	// Make a connection to the DEX.
	if dc, connected := c.connectAccount(&accountInfo); !connected {
		if dc != nil {
			dc.connMaster.Disconnect() // stop reconnect loop
			c.connMtx.Lock()
			delete(c.conns, dc.acct.host)
			c.connMtx.Unlock()
		}
		return newError(accountVerificationErr, "Account not verified for host: %s err: %v", host, err)
	}

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

	return nil
}
