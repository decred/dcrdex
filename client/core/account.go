package core

import (
	"encoding/hex"

	"decred.org/dcrdex/client/db"
	"github.com/decred/dcrd/dcrec/secp256k1/v3"
)

// AccountExport is used to retrieve account by host for export.
func (c *Core) AccountExport(pw []byte, host string) (*Account, error) {
	_, err := c.encryptionKey(pw)
	if err != nil {
		return nil, codedError(passwordErr, err)
	}
	_, err = addrHost(host)
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

	dc.acct.keyMtx.RLock()
	accountId := dc.acct.id.String()
	privKey := hex.EncodeToString(dc.acct.privKey.Serialize())
	dc.acct.keyMtx.RUnlock()

	feeProofSig := ""
	var feeProofStamp uint64 = 0
	if dc.acct.isPaid {
		accountProof, err := c.db.AccountProof(host)
		if err != nil {
			return nil, codedError(accountProofErr, err)
		} else {
			feeProofSig = hex.EncodeToString(accountProof.Sig)
			feeProofStamp = accountProof.Stamp
		}
	}

	// Account ID is exported for informational purposes only, it is not used during import.
	acct := &Account{
		Host:          host,
		AccountID:     accountId,
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

	_, err = addrHost(acct.Host)
	if err != nil {
		return newError(addressParseErr, "error parsing address: %v", err)
	}

	var accountInfo db.AccountInfo
	accountInfo.Host = acct.Host

	pubKey, err := hex.DecodeString(acct.DEXPubKey)
	if err != nil {
		return codedError(decodeErr, err)
	}
	accountInfo.DEXPubKey, err = secp256k1.ParsePubKey(pubKey)
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

	var accountProof db.AccountProof
	if acct.FeeProofSig != "" && acct.FeeProofStamp != 0 {
		accountProof.Sig, err = hex.DecodeString(acct.FeeProofSig)
		if err != nil {
			return codedError(decodeErr, err)
		}
		accountProof.Stamp = acct.FeeProofStamp
		accountProof.Host = acct.Host
		accountInfo.Paid = acct.FeeProofSig != "" && acct.FeeProofStamp != 0
	}

	// verifyAccount makes a connection to the DEX.
	if !c.verifyAccount(&accountInfo) {
		return newError(accountVerificationErr, "Account not verified for host: %s err: %v", acct.Host, err)
	}

	err = c.db.CreateAccount(&accountInfo)
	if err != nil {
		return codedError(dbErr, err)
	}

	err = c.db.AccountPaid(&accountProof)
	if err != nil {
		return codedError(dbErr, err)
	}

	c.refreshUser()
	return nil
}
