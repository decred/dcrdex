// Copyright (c) 2019, The Decred developers
// See LICENSE for details.

package dcr

import (
	"fmt"

	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrdex/server/asset"
)

// UTXO is information about a transaction. It must satisfy the asset.DEXTx
// interface to be DEX-compatible.
type Tx struct {
	dcr       *dcrBackend
	blockHash chainhash.Hash
	height    int64
	hash      chainhash.Hash
	ins       []txIn
	outs      []txOut
}

// Check that DEXTx satisfies the asset.DEXTx interface
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
func newTransaction(dcr *dcrBackend, txHash, blockHash *chainhash.Hash, blockHeight int64, ins []txIn, outs []txOut) *Tx {
	// Set a nil blockHash to the zero hash.
	hash := blockHash
	if hash == nil {
		hash = &zeroHash
	}
	return &Tx{
		dcr:       dcr,
		blockHash: *hash,
		height:    blockHeight,
		hash:      *txHash,
		ins:       ins,
		outs:      outs,
	}
}

// Confirmations is an asset.DEXAsset method that returns the number of
// confirmations for a transaction. Because it is possible for a tx that was
// once considered valid to later be considered invalid, Confirmations can
// return an error to indicate the tx is no longer valid.
func (tx *Tx) Confirmations() (int64, error) {
	// A zeroed block hash means this is a mempool transaction. Check if it has
	// been mined.
	if tx.blockHash == zeroHash {
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
			tx.height = blk.height
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
			if !found {
				return -1, fmt.Errorf("new block not found for utxo moved from orphaned block")
			}
		}
	}
	// If it's a regular transaction, check stakeholder validation.
	if mainchainBlock != nil && !mainchainBlock.txInStakeTree(&tx.hash) {
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
func (tx *Tx) SpendsUTXO(txid string, vout uint32) bool {
	txHash, err := chainhash.NewHashFromStr(txid)
	if err != nil {
		log.Warnf("error decoding txid %s: %v", txid, err)
		return false
	}
	for _, txIn := range tx.ins {
		if txIn.prevTx == *txHash && txIn.vout == vout {
			return true
		}
	}
	return false
}

// SwapDetails returns information about a swap contract at the specified
// transaction output. If the specified output is not a swap contract, an
// error will be returned. The returned data is sender address, receiver
// address, and contract value, in atoms.
func (tx *Tx) SwapDetails(vout uint32) (string, string, uint64, error) {
	if len(tx.outs) <= int(vout) {
		return "", "", 0, fmt.Errorf("invalid index %d for transaction %s", vout, tx.hash)
	}
	output := tx.outs[int(vout)]
	sender, receiver, err := extractSwapAddresses(output.pkScript)
	return sender, receiver, output.value, err
}
