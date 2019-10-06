// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package btc

import (
	"bytes"
	"crypto/sha256"
	"fmt"

	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcutil"
	"github.com/decred/dcrdex/server/asset"
)

// Tx is information about a transaction. It must satisfy the asset.DEXTx
// interface to be DEX-compatible.
type Tx struct {
	// Because a Tx's validity and block info can change after creation, keep a
	// Backend around to query the state of the tx and update the block info.
	btc *Backend
	// The height and hash of the transaction's best known block.
	blockHash chainhash.Hash
	height    int64
	// The transaction hash.
	hash chainhash.Hash
	// Transaction inputs and outputs.
	ins  []txIn
	outs []txOut
	// Used to conditionally skip block lookups on mempool transactions during
	// calls to Confirmations.
	lastLookup *chainhash.Hash
	// The calculated transaction fee rate, in satoshis/byte
	feeRate uint64
}

// Check that Tx satisfies the asset.DEXTx interface
var _ asset.DEXTx = (*Tx)(nil)

// A txIn holds information about a transaction input, mainly to verify which
// previous outpoint is being spent.
type txIn struct {
	prevTx chainhash.Hash
	vout   uint32
}

// A txOut holds information about a transaction output.
type txOut struct {
	value    uint64
	pkScript []byte
}

// A getter for a new Tx.
func newTransaction(btc *Backend, txHash, blockHash, lastLookup *chainhash.Hash,
	blockHeight int64, ins []txIn, outs []txOut, feeRate uint64) *Tx {
	// Set a nil blockHash to the zero hash.
	hash := blockHash
	if hash == nil {
		hash = &zeroHash
	}
	return &Tx{
		btc:        btc,
		blockHash:  *hash,
		height:     blockHeight,
		hash:       *txHash,
		ins:        ins,
		outs:       outs,
		lastLookup: lastLookup,
		feeRate:    feeRate,
	}
}

// Confirmations is an asset.DEXAsset method that returns the number of
// confirmations for a transaction. Because it is possible for a tx that was
// once considered valid to later be considered invalid, Confirmations can
// return an error to indicate the tx is no longer valid.
func (tx *Tx) Confirmations() (int64, error) {
	// A zeroed block hash means this is a mempool transaction. Check if it has
	// been mined
	// If the tip hasn't changed, there's no reason to look anything up.
	tipHash := tx.btc.blockCache.tipHash()
	if tx.blockHash == zeroHash && (tx.lastLookup == nil || *tx.lastLookup != tipHash) {
		tx.lastLookup = &tipHash
		verboseTx, err := tx.btc.node.GetRawTransactionVerbose(&tx.hash)
		if err != nil {
			return -1, fmt.Errorf("GetRawTransactionVerbose for txid %s: %v", tx.hash, err)
		}
		if verboseTx.BlockHash != "" {
			blk, err := tx.btc.getBlockInfo(verboseTx.BlockHash)
			if err != nil {
				return -1, err
			}
			tx.blockHash = blk.hash
			tx.height = int64(blk.height)
		}
	}
	// If there is still no block hash, it's a mempool transaction.
	if tx.blockHash == zeroHash {
		return 0, nil
	}
	// Get the block and check that this transaction is valid.
	mainchainBlock, found := tx.btc.blockCache.atHeight(uint32(tx.height))
	if !found {
		return -1, fmt.Errorf("no mainchain block for tx %s at height %d", tx.hash, tx.height)
	}
	if tx.blockHash != mainchainBlock.hash {
		// The transaction's block has been orphaned. See if we can find the tx in
		// mempool or in another block.
		newTx, err := tx.btc.transaction(&tx.hash)
		if err != nil {
			return -1, fmt.Errorf("tx %s has been lost from mainchain", tx.hash)
		}
		*tx = *newTx
		if tx.height == 0 {
			mainchainBlock = nil
		} else {
			mainchainBlock, found = tx.btc.blockCache.atHeight(uint32(tx.height))
			if !found || mainchainBlock.hash != tx.blockHash {
				return -1, fmt.Errorf("new block not found for utxo moved from orphaned block")
			}
		}
	}
	return int64(tx.btc.blockCache.tipHeight()) - tx.height + 1, nil
}

// SpendsUTXO checks whether a particular previous output is spent in this tx.
func (tx *Tx) SpendsUTXO(txid string, vout uint32) (bool, error) {
	txHash, err := chainhash.NewHashFromStr(txid)
	if err != nil {
		return false, fmt.Errorf("error decoding txid %s: %v", txid, err)
	}
	for _, txIn := range tx.ins {
		if txIn.prevTx == *txHash && txIn.vout == vout {
			return true, nil
		}
	}
	return false, nil
}

// AuditContract checks that the provided swap contract hashes to the script
// hash specified in the output. The output value (in satoshi) and receiving
// address are returned if no error is encountered.
func (tx *Tx) AuditContract(vout uint32, contract []byte) (string, uint64, error) {
	if len(tx.outs) <= int(vout) {
		return "", 0, fmt.Errorf("invalid index %d for transaction %s", vout, tx.hash)
	}
	output := tx.outs[int(vout)]

	// If it's a pay-to-script-hash, extract the script hash and check it against
	// the hash of the user-supplied redeem script.
	scriptType := parseScriptType(output.pkScript, contract)
	if scriptType == scriptUnsupported {
		return "", 0, asset.UnsupportedScriptError
	}
	var scriptHash, hashed []byte
	if scriptType.isP2SH() {
		if scriptType.isSegwit() {
			scriptHash = extractWitnessScriptHash(output.pkScript)
			shash := sha256.Sum256(contract)
			hashed = shash[:]
		} else {
			scriptHash = extractScriptHash(output.pkScript)
			hashed = btcutil.Hash160(contract)
		}
	}
	if scriptHash == nil {
		return "", 0, fmt.Errorf("specified output %s:%d is not P2SH", tx.hash, vout)
	}
	if !bytes.Equal(hashed, scriptHash) {
		return "", 0, fmt.Errorf("swap contract hash mismatch for %s:%d", tx.hash, vout)
	}
	_, receiver, err := extractSwapAddresses(contract, tx.btc.chainParams)
	if err != nil {
		return "", 0, fmt.Errorf("error extracting address from swap contract for %s:%d: %v", tx.hash, vout, err)
	}
	return receiver, output.value, nil
}

// FeeRate returns the transaction fee rate, in satoshis/byte.
func (tx *Tx) FeeRate() uint64 {
	return tx.feeRate
}
