// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package btc

import (
	"bytes"
	"crypto/sha256"
	"fmt"

	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcutil"
)

// A UTXO is information regarding an unspent transaction output. It must
// satisfy the asset.UTXO interface to be DEX-compatible.
type UTXO struct {
	// Because a UTXO's validity and block info can change after creation, keep a
	// Backend around to query the state of the tx and update the block info.
	btc *Backend
	tx  *Tx
	// The height and hash of the transaction's best known block.
	height    uint32
	blockHash chainhash.Hash
	txHash    chainhash.Hash
	vout      uint32
	// The number of confirmations needed for maturity. For outputs of a coinbase
	// transaction, this will be set to chaincfg.Params.CoinbaseMaturity (100 for
	// BTC). For other supported script types, this will be zero.
	maturity int32
	// A bitmask for script type information.
	scriptType btcScriptType
	// The output's scriptPubkey.
	pkScript []byte
	// If the pubkey script is P2SH or P2WSH, the UTXO will only be generated if
	// the redeem script is supplied and the script-hash validated. For P2PKH and
	// P2WPKH pubkey scripts, the redeem script should be nil.
	redeemScript []byte
	// numSigs is the number of signatures required to spend this output.
	numSigs int
	// spendSize stores the best estimate of the size (bytes) of the serialized
	// transaction input that spends this UTXO.
	spendSize uint32
	// The output value.
	value uint64
	// While the utxo's tx is still in mempool, the tip hash will be stored.
	// This enables an optimization in the Confirmations method to return zero
	// without extraneous block lookups.
	lastLookup *chainhash.Hash
}

// Confirmations returns the number of confirmations for a UTXO's transaction.
// Because a UTXO can become invalid after once being considered valid, validity
// should be verified again on every call. An error will be returned if this
// UTXO is no longer ready to spend. An unmined transaction should have zero
// confirmations. A transaction in the current best block should have one
// confirmation. The value -1 will be returned with any error.
func (utxo *UTXO) Confirmations() (int64, error) {
	btc := utxo.btc
	tipHash := btc.blockCache.tipHash()
	// If the UTXO was in a mempool transaction, check if it has been confirmed.
	if utxo.height == 0 {
		// If the tip hasn't changed, don't do anything here.
		if utxo.lastLookup == nil || *utxo.lastLookup != tipHash {
			utxo.lastLookup = &tipHash
			txOut, verboseTx, _, err := btc.getTxOutInfo(&utxo.txHash, utxo.vout)
			if err != nil {
				return -1, err
			}
			// More than zero confirmations would indicate that the transaction has
			// been mined. Collect the block info and update the utxo fields.
			if txOut.Confirmations > 0 {
				blk, err := btc.getBlockInfo(verboseTx.BlockHash)
				if err != nil {
					return -1, err
				}
				utxo.height = blk.height
				utxo.blockHash = blk.hash
			}
		}
	} else {
		// The UTXO was included in a block, but make sure that the utxo's block has
		// not been orphaned.
		mainchainBlock, found := btc.blockCache.atHeight(utxo.height)
		if !found {
			return -1, fmt.Errorf("no mainchain block for tx %s at height %d", utxo.txHash.String(), utxo.height)
		}
		// If the UTXO's block has been orphaned, check for a new containing block.
		if mainchainBlock.hash != utxo.blockHash {
			// See if we can find the utxo in another block.
			newUtxo, err := btc.utxo(&utxo.txHash, utxo.vout, utxo.redeemScript)
			if err != nil {
				return -1, fmt.Errorf("utxo block is not mainchain")
			}
			*utxo = *newUtxo
		}
	}
	// If the height is still 0, this is a mempool transaction.
	if utxo.height == 0 {
		return 0, nil
	}
	// Otherwise just check that there hasn't been a reorg which would render the
	// output immature. This would be exceedingly rare (impossible?).
	confs := int32(btc.blockCache.tipHeight()) - int32(utxo.height) + 1
	if confs < utxo.maturity {
		return -1, fmt.Errorf("transaction %s became immature", utxo.txHash)
	}
	return int64(confs), nil
}

// Auth verifies that the utxo pays to the supplied public key(s). This is an
// asset.DEXAsset method.
func (utxo *UTXO) Auth(pubkeys, sigs [][]byte, msg []byte) error {
	// If there are not enough pubkeys, no reason to check anything.
	if len(pubkeys) < utxo.numSigs {
		return fmt.Errorf("not enough signatures for utxo %s:%d. expected %d, got %d",
			utxo.txHash, utxo.vout, utxo.numSigs, len(pubkeys))
	}
	// Extract the addresses from the pubkey scripts and redeem scripts.
	evalScript := utxo.pkScript
	if utxo.scriptType.isP2SH() {
		evalScript = utxo.redeemScript
	}
	scriptAddrs, err := extractScriptAddrs(evalScript, utxo.btc.chainParams)
	if err != nil {
		return err
	}
	// Sanity check that the required signature count matches the count parsed
	// during UTXO initialization.
	if scriptAddrs.nRequired != utxo.numSigs {
		return fmt.Errorf("signature requirement mismatch for utxo %s:%d. %d != %d",
			utxo.txHash, utxo.vout, scriptAddrs.nRequired, utxo.numSigs)
	}
	matches := append(pkMatches(pubkeys, scriptAddrs.pubkeys, nil),
		pkMatches(pubkeys, scriptAddrs.pkHashes, btcutil.Hash160)...)
	if len(matches) < utxo.numSigs {
		return fmt.Errorf("not enough pubkey matches to satisfy the script for utxo %s:%d. expected %d, got %d",
			utxo.txHash, utxo.vout, utxo.numSigs, len(matches))
	}
	for _, match := range matches {
		err := checkSig(msg, match.pubkey, sigs[match.idx])
		if err != nil {
			return err
		}
	}
	return nil
}

type pkMatch struct {
	pubkey []byte
	idx    int
}

// pkMatches looks through a set of addresses and a returns a set of match
// structs with details about the match.
func pkMatches(pubkeys [][]byte, addrs []btcutil.Address, hasher func([]byte) []byte) []pkMatch {
	matches := make([]pkMatch, 0, len(pubkeys))
	if hasher == nil {
		hasher = func(a []byte) []byte { return a }
	}
	matchIndex := make(map[string]struct{})
	for _, addr := range addrs {
		for i, pubkey := range pubkeys {
			if bytes.Equal(addr.ScriptAddress(), hasher(pubkey)) {
				addrStr := addr.String()
				_, alreadyFound := matchIndex[addrStr]
				if alreadyFound {
					continue
				}
				matchIndex[addrStr] = struct{}{}
				matches = append(matches, pkMatch{
					pubkey: pubkey,
					idx:    i,
				})
				break
			}
		}
	}
	return matches
}

// AuditContract checks that UTXO is a swap contract and extracts the
// receiving address and contract value on success.
func (utxo *UTXO) AuditContract() (string, uint64, error) {
	tx := utxo.tx
	if len(tx.outs) <= int(utxo.vout) {
		return "", 0, fmt.Errorf("invalid index %d for transaction %s", utxo.vout, tx.hash)
	}
	output := tx.outs[int(utxo.vout)]

	// If it's a pay-to-script-hash, extract the script hash and check it against
	// the hash of the user-supplied redeem script.
	scriptType := parseScriptType(output.pkScript, utxo.redeemScript)
	if scriptType == scriptUnsupported {
		return "", 0, fmt.Errorf("specified output %s:%d is not P2SH", tx.hash, utxo.vout)
	}
	var scriptHash, hashed []byte
	if scriptType.isP2SH() {
		if scriptType.isSegwit() {
			scriptHash = extractWitnessScriptHash(output.pkScript)
			shash := sha256.Sum256(utxo.redeemScript)
			hashed = shash[:]
		} else {
			scriptHash = extractScriptHash(output.pkScript)
			hashed = btcutil.Hash160(utxo.redeemScript)
		}
	}
	if scriptHash == nil {
		return "", 0, fmt.Errorf("specified output %s:%d is not P2SH", tx.hash, utxo.vout)
	}
	if !bytes.Equal(hashed, scriptHash) {
		return "", 0, fmt.Errorf("swap contract hash mismatch for %s:%d", tx.hash, utxo.vout)
	}
	_, receiver, err := extractSwapAddresses(utxo.redeemScript, utxo.btc.chainParams)
	if err != nil {
		return "", 0, fmt.Errorf("error extracting address from swap contract for %s:%d: %v", tx.hash, utxo.redeemScript, err)
	}
	return receiver, output.value, nil
}

// SpendsUTXO checks whether a particular coin is spent in this coin's tx.
func (utxo *UTXO) SpendsCoin(coinID []byte) (bool, error) {
	txHash, vout, err := decodeCoinID(coinID)
	if err != nil {
		return false, fmt.Errorf("error decoding coin ID %x: %v", coinID, err)
	}
	for _, txIn := range utxo.tx.ins {
		if txIn.prevTx == *txHash && txIn.vout == vout {
			return true, nil
		}
	}
	return false, nil
}

// FeeRate returns the transaction fee rate, in satoshi/vbyte.
func (utxo *UTXO) FeeRate() uint64 {
	return utxo.tx.feeRate
}

// SpendSize returns the maximum size of the serialized TxIn that spends this
// UTXO, in bytes. This is a method of the asset.UTXO interface.
func (utxo *UTXO) SpendSize() uint32 {
	return utxo.spendSize
}

// ID returns the coin ID.
func (utxo *UTXO) ID() []byte {
	return toCoinID(&utxo.tx.hash, utxo.vout)
}

// TxID is a string identifier for the transaction, typically a hexadecimal
// representation of the byte-reversed transaction hash. Should always return
// the same value as the txid argument passed to (DEXAsset).UTXO.
func (utxo *UTXO) TxID() string {
	return utxo.txHash.String()
}

// Value is the output value, in atoms.
func (utxo *UTXO) Value() uint64 {
	return utxo.value
}
