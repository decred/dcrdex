// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package btc

import (
	"bytes"
	"fmt"

	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcutil"
)

// A UTXO is information regarding an unspent transaction output. It must
// satisfy the asset.UTXO interface to be DEX-compatible.
type UTXO struct {
	// Because a UTXO's validity and block info can change after creation, keep a
	// btcBackend around to query the state of the tx and update the block info.
	btc *BTCBackend
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
				utxo.height = uint32(blk.height)
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

// PaysToPubkeys checks that the provided pubkeys can spend the UTXO.
func (utxo *UTXO) PaysToPubkeys(pubkeys [][]byte) (bool, error) {
	// If there are not enough pubkeys, no reason to check anything.
	if len(pubkeys) < utxo.numSigs {
		return false, fmt.Errorf("not enough signatures for utxo %s:%d. expected %d, got %d",
			utxo.txHash, utxo.vout, utxo.numSigs, len(pubkeys))
	}
	// Extract the addresses from the pubkey scripts and redeem scripts.
	evalScript := utxo.pkScript
	if utxo.scriptType.isP2SH() {
		evalScript = utxo.redeemScript
	}
	scriptAddrs, err := extractScriptAddrs(evalScript, utxo.btc.chainParams)
	if err != nil {
		return false, err
	}
	// Sanity check that the required signature count matches the count parsed
	// during UTXO initialization.
	if scriptAddrs.nRequired != utxo.numSigs {
		return false, fmt.Errorf("signature requirement mismatch for utxo %s:%d. %d != %d",
			utxo.txHash, utxo.vout, scriptAddrs.nRequired, utxo.numSigs)
	}
	// For unhashsed pubkeys, just compare the lists directly.
	numMatches := countMatches(pubkeys, scriptAddrs.pubkeys, nil)
	// For pubkey hash addresses, compare the hash160.
	numMatches += countMatches(pubkeys, scriptAddrs.pkHashes, btcutil.Hash160)
	if numMatches < utxo.numSigs {
		return false, fmt.Errorf("not enough pubkey matches to satisfy the script for utxo %s:%d. expected %d, got %d",
			utxo.txHash, utxo.vout, utxo.numSigs, numMatches)
	}
	return true, nil
}

// countMatches looks through a set of addresses and a set of pubkeys and counts
// the matches.
func countMatches(pubkeys [][]byte, addrs []btcutil.Address, hasher func([]byte) []byte) int {
	var numMatches int
	if hasher == nil {
		hasher = func(a []byte) []byte { return a }
	}
	matches := make(map[string]struct{})
	for _, addr := range addrs {
		for _, pubkey := range pubkeys {
			if bytes.Equal(addr.ScriptAddress(), hasher(pubkey)) {
				addrStr := addr.String()
				_, alreadyFound := matches[addrStr]
				if alreadyFound {
					continue
				}
				matches[addrStr] = struct{}{}
				numMatches++
				break
			}
		}
	}
	return numMatches
}

// ScriptSize returns the UTXO's maximum sigScript byte count.
func (utxo *UTXO) ScriptSize() uint32 {
	return utxo.spendSize
}

// TxHash is a byte-slice of the UTXO's transaction hash.
func (utxo *UTXO) TxHash() []byte {
	return utxo.txHash.CloneBytes()
}

// TxID is a string identifier for the transaction, typically a hexadecimal
// representation of the byte-reversed transaction hash. Should always return
// the same value as the txid argument passed to (DEXAsset).UTXO.
func (utxo *UTXO) TxID() string {
	return utxo.txHash.String()
}

// Vout is the output index of the UTXO.
func (utxo *UTXO) Vout() uint32 {
	return utxo.vout
}
