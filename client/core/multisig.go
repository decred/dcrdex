package core

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"time"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/dex/keygen"
	"github.com/decred/dcrd/dcrec/secp256k1/v4"
	"github.com/decred/dcrd/hdkeychain/v3"
)

const csvVersion = 0

func (c *Core) parsePaymentMultisigCVS(csvFilePath string) (pm *asset.PaymentMultisig, header []byte, err error) {
	b, err := os.ReadFile(csvFilePath)
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
			if len(lines[i]) == 0 || lines[i][0] == ';' {
				i++
				continue
			}
			return lines[i], true
		}
	}
	version, ok := nextLine()
	if !ok {
		return nil, nil, errors.New("unable to find asset id")
	}
	ver, err := strconv.Atoi(string(version))
	if err != nil {
		return nil, nil, fmt.Errorf("unable to decode version: %v", err)
	}
	if ver != csvVersion {
		return nil, nil, fmt.Errorf("can only parse version %d csv file", csvVersion)
	}
	assetIDB, ok := nextLine()
	if !ok {
		return nil, nil, errors.New("unable to find asset id")
	}
	assetID, err := strconv.Atoi(string(assetIDB))
	if err != nil {
		return nil, nil, fmt.Errorf("unable to decode asset id: %v", err)
	}
	durB, ok := nextLine()
	if !ok {
		return nil, nil, errors.New("unable to find locktime duration")
	}
	lockDuration, err := time.ParseDuration(string(durB))
	if err != nil {
		return nil, nil, fmt.Errorf("unable to decode duration: %v", err)
	}
	locktime := time.Now().Add(lockDuration).Unix()
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
		if len(pubKey) != 33 {
			return nil, nil, fmt.Errorf("invalid pubkey at index %d", i)
		}
		signerXpubs = append(signerXpubs, pubKey)
	}
	if nRequired > len(signerXpubs) {
		return nil, nil, errors.New("more sigs required than signers")
	}
	addrToVal := make(map[string]float64)
	for avl, ok := nextLine(); ok; avl, ok = nextLine() {
		addrAmt := bytes.Split(avl, []byte(","))
		if len(addrAmt) != 2 {
			return nil, nil, errors.New("unable to find amt that goes with addr")
		}
		amt, err := strconv.ParseFloat(string(addrAmt[1]), 64)
		if err != nil {
			return nil, nil, fmt.Errorf("unable to decode amt: %v", err)
		}
		if amt < 0 {
			return nil, nil, errors.New("only positive values allowed")
		}
		addrToVal[string(addrAmt[0])] = amt
	}
	var tx *asset.PaymentMultisigTx
	if hasTx {
		tx = new(asset.PaymentMultisigTx)
		if err := json.Unmarshal(txB, tx); err != nil {
			return nil, nil, fmt.Errorf("unable to unmarshal transactions: %v", err)
		}
	}
	return &asset.PaymentMultisig{
		AssetID:     uint32(assetID),
		NRequired:   int64(nRequired),
		Locktime:    locktime,
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

	var pubkey [33]byte
	copy(pubkey[:], priv.PubKey().SerializeCompressed())
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

func (c *Core) preparePaymentMultisig(csvFilePath string) (pm *asset.PaymentMultisig, multisigner asset.Multisigner, xcwallet *xcWallet,
	writeToFile func(pmTx *asset.PaymentMultisigTx) error, err error) {
	pm, header, err := c.parsePaymentMultisigCVS(csvFilePath)
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("unable to parse csv file: %v", err)
	}
	var found bool
	wallet, found := c.wallet(pm.AssetID)
	if !found || !wallet.connected() {
		return nil, nil, nil, nil, fmt.Errorf("multisig asset wallet %v does not exist or is not connected", unbip(pm.AssetID))
	}
	multisigner, ok := wallet.Wallet.(asset.Multisigner)
	if !ok {
		return nil, nil, nil, nil, fmt.Errorf("wallet %v is not an asset.Multisigner", unbip(pm.AssetID))
	}

	return pm, multisigner, wallet, func(pmTx *asset.PaymentMultisigTx) error {
		b, err := json.Marshal(pmTx)
		if err != nil {
			return fmt.Errorf("unable to marshal multisig csv :%v", err)
		}
		header = append(header, b...)
		return os.WriteFile(csvFilePath, header, 0644)
	}, nil
}

// SendFundsToMultisig sends amounts to a multisig address, then creates a
// transaction that spends those funds. Writes data back to csvFilePath.
func (c *Core) SendFundsToMultisig(csvFilePath string) error {
	pm, multisigner, wallet, writeToFile, err := c.preparePaymentMultisig(csvFilePath)
	if err != nil {
		return err
	}
	if pm.SpendingTx != nil {
		return errors.New("it appears funds have already been sent. spending tx not nil")
	}
	_, err = wallet.refreshUnlock()
	if err != nil {
		return fmt.Errorf("multisig asset wallet %v is locked", unbip(pm.AssetID))
	}
	pmTx, err := multisigner.SendFundsToMultisig(c.ctx, pm)
	if err != nil {
		if errors.Is(err, asset.ErrMultisigPartialSend) {
			if writeErr := writeToFile(pmTx); err != nil {
				return fmt.Errorf("making multisig ended with partial error and we were unable "+
					"to write the multisig tx to file: %v AND %v", err, writeErr)
			}
			return fmt.Errorf("making multisig ended with partial error but some info was added to the csv file: %v", err)
		}
		return fmt.Errorf("unable to send multisig funds: %v", err)
	}

	return writeToFile(pmTx)
}

// SignMultisig signs the pmTx with the supplied privateKey and inserts the
// signature at idx among the other signatures. Writes back to csvFilePath.
func (c *Core) SignMultisig(csvFilePath string, signIdx int) error {
	pm, multisigner, wallet, writeToFile, err := c.preparePaymentMultisig(csvFilePath)
	if err != nil {
		return err
	}
	if pm.SpendingTx == nil {
		return errors.New("no tx to sign")
	}
	_, err = wallet.refreshUnlock()
	if err != nil {
		return fmt.Errorf("multisig asset wallet %v is locked", unbip(pm.AssetID))
	}
	if len(pm.SignerXpubs) <= signIdx {
		return fmt.Errorf("not enough signers %d for sign index of %d", len(pm.SignerXpubs), signIdx)
	}
	var pubkey [33]byte
	copy(pubkey[:], pm.SignerXpubs[signIdx])
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

	pmTx, err := multisigner.SignMultisig(c.ctx, pm.SpendingTx, priv.Serialize())
	if err != nil {
		return fmt.Errorf("unable to sign multisig: %v", err)
	}

	return writeToFile(pmTx)
}

// RefundPaymentMultisig refunds a multisig if it is after the locktime and we are the
// sender.
func (c *Core) RefundPaymentMultisig(csvFilePath string) (string, error) {
	pm, multisigner, wallet, _, err := c.preparePaymentMultisig(csvFilePath)
	if err != nil {
		return "", err
	}
	if pm.SpendingTx == nil {
		return "", errors.New("no tx to refund")
	}
	_, err = wallet.refreshUnlock()
	if err != nil {
		return "", fmt.Errorf("multisig asset wallet %v is locked", unbip(pm.AssetID))
	}
	return multisigner.RefundMultisig(c.ctx, pm.SpendingTx)
}

// ViewPaymentMultisig returns a json encoding of the spending tx for human
// viewing.
func (c *Core) ViewPaymentMultisig(csvFilePath string) (string, error) {
	pm, multisigner, _, _, err := c.preparePaymentMultisig(csvFilePath)
	if err != nil {
		return "", err
	}
	if pm.SpendingTx == nil {
		return "", errors.New("no tx to view")
	}
	return multisigner.ViewPaymentMultisig(pm.SpendingTx)
}

// SendPaymentMultisig sends the multisig hex. Will not error if there aren't
// enough signatures if using spv.
func (c *Core) SendPaymentMultisig(csvFilePath string) (string, error) {
	pm, multisigner, _, _, err := c.preparePaymentMultisig(csvFilePath)
	if err != nil {
		return "", err
	}
	if pm.SpendingTx == nil {
		return "", errors.New("no tx to send")
	}
	txB, err := hex.DecodeString(pm.SpendingTx.TxHex)
	if err != nil {
		return "", err
	}
	coinID, err := multisigner.SendTransaction(txB)
	if err != nil {
		return "", err
	}
	return coinIDString(pm.AssetID, coinID), nil
}
