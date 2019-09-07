// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package dcr

import (
	"bytes"
	"fmt"

	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/dcrutil/v2"
	"github.com/decred/dcrdex/server/asset"
)

// Tx is information about a transaction. It must satisfy the asset.DEXTx
// interface to be DEX-compatible.
type Tx struct {
	// Because a Tx's validity and block info can change after creation, keep a
	// dcrBackend around to query the state of the tx and update the block info.
	dcr *dcrBackend
	// The height and hash of the transaction's best known block.
	blockHash chainhash.Hash
	height    int64
	// The transaction hash.
	hash chainhash.Hash
	// Transaction inputs and outputs.
	ins  []txIn
	outs []txOut
	// Whether the transaction is a stake-related transaction.
	isStake bool
	// Used to conditionally skip block lookups on mempool transactions during
	// calls to Confirmations.
	lastLookup *chainhash.Hash
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
func newTransaction(dcr *dcrBackend, txHash, blockHash, lastLookup *chainhash.Hash, blockHeight int64,
	isStake bool, ins []txIn, outs []txOut) *Tx {
	// Set a nil blockHash to the zero hash.
	hash := blockHash
	if hash == nil {
		hash = &zeroHash
	}
	return &Tx{
		dcr:        dcr,
		blockHash:  *hash,
		height:     blockHeight,
		hash:       *txHash,
		ins:        ins,
		outs:       outs,
		isStake:    isStake,
		lastLookup: lastLookup,
	}
}

// Confirmations is an asset.DEXAsset method that returns the number of
// confirmations for a transaction. Because it is possible for a tx that was
// once considered valid to later be considered invalid, Confirmations can
// return an error to indicate the tx is no longer valid.
func (tx *Tx) Confirmations() (int64, error) {
	// A zeroed block hash means this is a mempool transaction. Check if it has
	// been mined.
	tipHash := tx.dcr.blockCache.tipHash()
	if tx.blockHash == zeroHash && (tx.lastLookup == nil || *tx.lastLookup != tipHash) {
		tx.lastLookup = &tipHash
		verboseTx, err := tx.dcr.node.GetRawTransactionVerbose(&tx.hash)
		if err != nil {
			return -1, fmt.Errorf("GetRawTransactionVerbose for txid %s: %v", tx.hash, err)
		}
		if verboseTx.BlockHash != "" {
			blk, err := tx.dcr.getBlockInfo(verboseTx.BlockHash)
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
	mainchainBlock, found := tx.dcr.blockCache.atHeight(uint32(tx.height))
	if !found {
		return -1, fmt.Errorf("no mainchain block for tx %s at height %d", tx.hash, tx.height)
	}
	if tx.blockHash != mainchainBlock.hash {
		// The transaction's block has been orphaned. See if we can find the tx in
		// mempool or in another block.
		newTx, err := tx.dcr.transaction(&tx.hash)
		if err != nil {
			return -1, fmt.Errorf("tx %s has been lost from mainchain", tx.hash)
		}
		*tx = *newTx
		if tx.height == 0 {
			mainchainBlock = nil
		} else {
			mainchainBlock, found = tx.dcr.blockCache.atHeight(uint32(tx.height))
			if !found || mainchainBlock.hash != tx.blockHash {
				return -1, fmt.Errorf("new block not found for utxo moved from orphaned block")
			}
		}
	}
	// If it's a regular transaction, check stakeholder validation.
	if mainchainBlock != nil && !tx.isStake {
		nextBlock, err := tx.dcr.getMainchainDcrBlock(uint32(tx.height) + 1)
		if err != nil {
			return -1, fmt.Errorf("error retreiving approving block for tx %s: %v", tx.hash, err)
		}
		if nextBlock != nil && !nextBlock.vote {
			return -1, fmt.Errorf("tx's block has been voted as invalid")
		}
	}
	return int64(tx.dcr.blockCache.tipHeight()) - tx.height + 1, nil
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
// hash specified in the output at the indicated vout. The output value
// (in atoms) and receiving address are returned if no error is encountered.
func (tx *Tx) AuditContract(vout uint32, contract []byte) (string, uint64, error) {
	if len(tx.outs) <= int(vout) {
		return "", 0, fmt.Errorf("invalid index %d for transaction %s", vout, tx.hash)
	}
	output := tx.outs[int(vout)]
	scriptHash := extractScriptHash(output.pkScript)
	if scriptHash == nil {
		return "", 0, fmt.Errorf("specified output %s:%d is not P2SH", tx.hash, vout)
	}
	if !bytes.Equal(dcrutil.Hash160(contract), scriptHash) {
		return "", 0, fmt.Errorf("swap contract hash mismatch for %s:%d", tx.hash, vout)
	}
	_, receiver, err := extractSwapAddresses(contract)
	if err != nil {
		return "", 0, fmt.Errorf("error extracting address from swap contract for %s:%d", tx.hash, vout)
	}
	return receiver, output.value, nil
}
