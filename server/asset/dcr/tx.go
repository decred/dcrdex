// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package dcr

import (
	"github.com/decred/dcrd/chaincfg/chainhash"
)

// Tx is information about a transaction. It must satisfy the asset.DEXTx
// interface to be DEX-compatible.
type Tx struct {
	// The height and hash of the transaction's best known block.
	blockHash chainhash.Hash
	height    int64
	// The transaction hash.
	hash chainhash.Hash
	// Transaction inputs and outputs.
	ins  []txIn
	outs []txOut
	// Whether the transaction is a stake-related transaction.
	isStake    bool
	isCoinbase bool
	// Used to conditionally skip block lookups on mempool transactions during
	// calls to Confirmations.
	lastLookup *chainhash.Hash
	// The calculated transaction fee rate, in atoms/byte
	feeRate uint64
}

// A txIn holds information about a transaction input, mainly to verify which
// previous outpoint is being spent.
type txIn struct {
	prevTx chainhash.Hash
	vout   uint32
	value  uint64
}

// A txOut holds information about a transaction output.
type txOut struct {
	value    uint64
	pkScript []byte
}

// A getter for a new Tx.
func newTransaction(txHash, blockHash, lastLookup *chainhash.Hash, blockHeight int64,
	isStake, isCoinbase bool, ins []txIn, outs []txOut, feeRate uint64) *Tx {
	// Set a nil blockHash to the zero hash.
	hash := blockHash
	if hash == nil {
		hash = &zeroHash
	}
	return &Tx{
		blockHash:  *hash,
		height:     blockHeight,
		hash:       *txHash,
		ins:        ins,
		outs:       outs,
		isStake:    isStake,
		isCoinbase: isCoinbase,
		lastLookup: lastLookup,
		feeRate:    feeRate,
	}
}
