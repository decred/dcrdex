package core

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/dex/keygen"
	"github.com/decred/dcrd/dcrec/secp256k1/v4"
	"github.com/decred/dcrd/hdkeychain/v3"
)

func (c *Core) parsePaymentMultisigCVS(cvsFilePath string) (pm *asset.PaymentMultisig, header []byte, err error) {
	b, err := os.ReadFile(cvsFilePath)
	if err != nil {
		return nil, nil, fmt.Errorf("error reading file: %v", err)
	}
	// Find start of msg tx json array.
	txStartIdx := bytes.IndexRune(b, '{')
	hasTx := txStartIdx > 0
	var txB []byte
	if hasTx {
		txB = make([]byte, len(b)-txStartIdx)
		copy(txB, b[txStartIdx:])
		b = b[:txStartIdx]
	}
	lines := bytes.Split(b, []byte("\n"))
	i, nLines := 0, len(lines)
	nextLine := func() ([]byte, bool) {
		defer func() {
			i++
		}()
		for {
			if i >= nLines {
				return nil, false
			}
			if len(lines[i]) == 0 || bytes.Equal(lines[0], []byte(";")) {
				i++
				continue
			}
			return lines[i], true
		}
	}
	assetIDB, ok := nextLine()
	if !ok {
		return nil, nil, errors.New("unable to find asset id")
	}
	assetID, err := strconv.Atoi(string(assetIDB))
	if err != nil {
		return nil, nil, fmt.Errorf("unable to decode asset id: %v", err)
	}
	locktimeB, ok := nextLine()
	if !ok {
		return nil, nil, errors.New("unable to find locktime")
	}
	locktime, err := strconv.Atoi(string(locktimeB))
	if err != nil {
		return nil, nil, fmt.Errorf("unable to decode locktime: %v", err)
	}
	nRequiredB, ok := nextLine()
	if !ok {
		return nil, nil, errors.New("unable to find number of sigs required")
	}
	nRequired, err := strconv.Atoi(string(nRequiredB))
	if err != nil {
		return nil, nil, fmt.Errorf("unable to decode locktime: %v", err)
	}
	xPubsLineB, ok := nextLine()
	if !ok {
		return nil, nil, errors.New("unable to find xpubs")
	}
	var signerXpubs [][]byte
	for i, xPubB := range bytes.Split(xPubsLineB, []byte(",")) {
		pubKey, err := hex.DecodeString(string(xPubB))
		if err != nil {
			return nil, nil, fmt.Errorf("unable to decode xpub hex: %v", err)
		}
		if len(pubKey) != 33 || pubKey[0] != 1 {
			return nil, nil, fmt.Errorf("invalid pubkey at index %d", i)
		}
		signerXpubs = append(signerXpubs, pubKey)
	}
	addrToVal := make(map[string]float64)
	for avl, end := nextLine(); !end; avl, end = nextLine() {
		addrAmt := bytes.Split(avl, []byte(","))
		if len(addrAmt) != 2 {
			return nil, nil, errors.New("unable to find amt that goes with addr")
		}
		amt, err := strconv.ParseFloat(string(addrAmt[1]), 64)
		if err != nil {
			return nil, nil, fmt.Errorf("unable to decode amt: %v", err)
		}
		addrToVal[string(addrAmt[0])] = amt
	}
	tx := new(asset.PaymentMultisigTx)
	if hasTx {
		if err := json.Unmarshal(txB, tx); err != nil {
			return nil, nil, fmt.Errorf("unable to unmarshal transactions: %v", err)
		}
	}
	return &asset.PaymentMultisig{
		AssetID:     uint32(assetID),
		NRequired:   int64(nRequired),
		Locktime:    int64(locktime),
		SignerXpubs: signerXpubs,
		AddrToVal:   addrToVal,
		SpendingTx:  tx,
	}, b, nil
}

func deriveMultisigXPriv(seed []byte) (*hdkeychain.ExtendedKey, error) {
	return keygen.GenDeepChild(seed, []uint32{hdKeyPurposeMulti})
}

func deriveMultisigKey(multisigXPriv *hdkeychain.ExtendedKey, assetID, multisigIndex uint32) (*secp256k1.PrivateKey, error) {
	kids := []uint32{
		assetID + hdkeychain.HardenedKeyStart,
		multisigIndex,
	}
	extKey, err := keygen.GenDeepChildFromXPriv(multisigXPriv, kids)
	if err != nil {
		return nil, fmt.Errorf("GenDeepChild error: %w", err)
	}
	privB, err := extKey.SerializedPrivKey()
	if err != nil {
		return nil, fmt.Errorf("SerializedPrivKey error: %w", err)
	}
	priv := secp256k1.PrivKeyFromBytes(privB)
	return priv, nil
}

func (c *Core) multisigKeyIdx(assetID, idx uint32) (*secp256k1.PrivateKey, error) {
	c.loginMtx.Lock()
	defer c.loginMtx.Unlock()

	if c.multisigXPriv == nil {
		return nil, errors.New("not logged in")
	}

	return deriveMultisigKey(c.multisigXPriv, assetID, idx)
}

// nextMultisigKey generates the private key for the next multisig, incrementing a
// persistent multisig index counter. This method requires login to decrypt and set
// the multisig xpriv, so use the multisigKeysReady method to ensure it is ready first.
// The multisig key index is returned so the same key may be regenerated.
func (c *Core) nextMultisigKey(assetID uint32) (*secp256k1.PrivateKey, uint32, error) {
	c.loginMtx.Lock()
	defer c.loginMtx.Unlock()

	if c.multisigXPriv == nil {
		return nil, 0, errors.New("not logged in")
	}

	nextMultisigKeyIndex, err := c.db.NextMultisigKeyIndex(assetID)
	if err != nil {
		return nil, 0, fmt.Errorf("NextMultisigIndex: %v", err)
	}

	priv, err := deriveMultisigKey(c.multisigXPriv, assetID, nextMultisigKeyIndex)
	if err != nil {
		return nil, 0, fmt.Errorf("multisigKeyIdx: %v", err)
	}

	var pubkey [32]byte
	copy(pubkey[:], priv.PubKey().SerializeCompressed()[1:])
	if err := c.db.StoreMultisigIndexForPubkey(assetID, nextMultisigKeyIndex, pubkey); err != nil {
		return nil, 0, fmt.Errorf("unable to store pubkey for index: %v", err)
	}
	return priv, nextMultisigKeyIndex, nil
}

// PaymentMultisigPubkey returns the next multisig signing pubkey and stores its
// index in the db.
func (c *Core) PaymentMultisigPubkey(assetID uint32) (string, error) {
	priv, _, err := c.nextMultisigKey(assetID)
	if err != nil {
		return "", err
	}
	defer priv.Zero()
	return hex.EncodeToString(priv.PubKey().SerializeCompressed()), nil
}

func (c *Core) preparePaymentMultisig(cvsFilePath string) (pm *asset.PaymentMultisig, multisigner asset.Multisigner,
	writeToFile func(pmTx *asset.PaymentMultisigTx) error, err error) {
	pm, header, err := c.parsePaymentMultisigCVS(cvsFilePath)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("unable to parse cvs file: %v", err)
	}
	var found bool
	wallet, found := c.wallet(pm.AssetID)
	if !found || !wallet.connected() {
		return nil, nil, nil, fmt.Errorf("multisig asset wallet %v does not exist or is not connected", unbip(pm.AssetID))
	}
	multisigner, ok := wallet.Wallet.(asset.Multisigner)
	if !ok {
		return nil, nil, nil, fmt.Errorf("wallet %v is not an asset.Multisigner", unbip(pm.AssetID))
	}
	_, err = wallet.refreshUnlock()
	if err != nil {
		return nil, nil, nil, fmt.Errorf("multisig asset wallet %v is locked", unbip(pm.AssetID))
	}

	return pm, multisigner, func(pmTx *asset.PaymentMultisigTx) error {
		b, err := json.Marshal(pmTx)
		if err != nil {
			return fmt.Errorf("unable to marshal multisig cvs :%v", err)
		}
		header = append(header, b...)
		return os.WriteFile(cvsFilePath, header, 0644)
	}, nil
}

// SendFundsToMultisig sends amounts to a multisig address, then createsa
// transaction that spends those funds. Writes data back to cvsFilePath.
func (c *Core) SendFundsToMultisig(cvsFilePath string) error {
	pm, multisigner, writeToFile, err := c.preparePaymentMultisig(cvsFilePath)
	if err != nil {
		return err
	}
	pmTx, err := multisigner.SendFundsToMultisig(c.ctx, pm)
	if err != nil {
		if errors.Is(err, asset.ErrMultisigPartialSend) {
			if writeErr := writeToFile(pmTx); err != nil {
				// TODO: Do more to ensure logging of the redeem script.
				return fmt.Errorf("making multisig ended with partial error and we were unable "+
					"to write the multisig tx to file: %v AND %v", err, writeErr)
			}
			return fmt.Errorf("making multisig ended with partial error but the multisig was added to the file: %v", err)
		}
		return fmt.Errorf("unable to send multisig funds: %v", err)
	}

	return writeToFile(pmTx)
}

// SignMultisig signs the pmTx with the supplied privateKey and insertsthe
// signature at idx among the other signatures. Writes back to cvsFilePath.
func (c *Core) SignMultisig(cvsFilePath string, signIdx int) error {
	pm, multisigner, writeToFile, err := c.preparePaymentMultisig(cvsFilePath)
	if err != nil {
		return err
	}
	if len(pm.SignerXpubs) < signIdx {
		return fmt.Errorf("not enough signers %d for sign index of %d", len(pm.SignerXpubs), signIdx)
	}
	var pubkey [32]byte
	copy(pubkey[:], pm.SignerXpubs[signIdx][1:])
	idx, err := c.db.MultisigIndexForPubkey(pm.AssetID, pubkey)
	if err != nil {
		return fmt.Errorf("unable to find the index for pubkey %x and asset id %d. If this wallet was restored, "+
			"try creating signing keys with that asset until you see the used key returned.", pm.SignerXpubs[signIdx], pm.AssetID)
	}
	c.loginMtx.Lock()

	if c.multisigXPriv == nil {
		c.loginMtx.Unlock()
		return errors.New("not logged in")
	}

	priv, err := deriveMultisigKey(c.multisigXPriv, pm.AssetID, idx)
	c.loginMtx.Unlock()
	if err != nil {
		return fmt.Errorf("unable to derive multisig private key: %v", err)
	}

	pmTx, err := multisigner.SignMultisig(c.ctx, pm, priv.Serialize(), signIdx)
	if err != nil {
		return fmt.Errorf("unable to sign multisig: %v", err)
	}

	return writeToFile(pmTx)
}

// RefundMultisig refunds a multisig if it is after the locktime and we are the
// sender.
func (c *Core) RefundPaymentMultisig(cvsFilePath string, signIdx int) error {
	pm, multisigner, _, err := c.preparePaymentMultisig(cvsFilePath)
	if err != nil {
		return err
	}
	return multisigner.RefundMultisig(c.ctx, pm)
}
