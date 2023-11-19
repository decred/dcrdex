// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package btc

import (
	"decred.org/dcrdex/dex"
	"github.com/btcsuite/btcd/btcjson"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
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
	ins        []txIn
	outs       []txOut
	isCoinbase bool
	// Used to conditionally skip block lookups on mempool transactions during
	// calls to Confirmations.
	lastLookup *chainhash.Hash
	// The calculated transaction fee rate, in satoshis/vbyte
	feeRate  uint64
	inputSum uint64
	// raw is the raw tx bytes.
	raw []byte
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

// JoinSplit represents a Zcash JoinSplit.
// https://zips.z.cash/protocol/canopy.pdf section 4.11
type JoinSplit struct {
	// Old = input
	Old uint64 `json:"vpub_oldZat"`
	// New = output
	New uint64 `json:"vpub_newZat"`
}

// VerboseTxExtended is a subset of *btcjson.TxRawResult, with the addition of
// some asset-specific fields.
type VerboseTxExtended struct {
	Raw           dex.Bytes `json:"hex"`
	Hex           string
	Txid          string          `json:"txid"`
	Size          int32           `json:"size,omitempty"`
	Vsize         int32           `json:"vsize,omitempty"`
	Vin           []*btcjson.Vin  `json:"vin"`
	Vout          []*btcjson.Vout `json:"vout"`
	BlockHash     string          `json:"blockhash,omitempty"`
	Confirmations uint64          `json:"confirmations,omitempty"`

	// Zcash-specific fields.

	VJoinSplit          []*JoinSplit `json:"vjoinsplit"`
	ValueBalanceSapling int64        `json:"valueBalanceZat"` // Sapling pool
	// ValueBalanceOrchard is disabled until zcashd encodes valueBalanceOrchard.
	ValueBalanceOrchard int64 `json:"valueBalanceOrchardZat"` // Orchard pool

	// Other fields that could be used but aren't right now.

	// Hash      string `json:"hash,omitempty"`
	// Weight    int32  `json:"weight,omitempty"`
	// Version   uint32 `json:"version"`
	// LockTime  uint32 `json:"locktime"`
	// Time      int64  `json:"time,omitempty"`
	// Blocktime int64  `json:"blocktime,omitempty"`
}

// Currently disabled because the verbose getrawtransaction results for Zcash
// do not include the valueBalanceOrchard yet.
// https://github.com/zcash/zcash/pull/5969
// // ShieldedIO sums the Zcash shielded pool inputs and outputs. Will return
// // zeros for non-Zcash-protocol transactions.
// func (tx *VerboseTxExtended) ShieldedIO() (in, out uint64) {
// 	for _, js := range tx.VJoinSplit {
// 		in += js.New
// 		out += js.Old
// 	}
// 	if tx.ValueBalanceSapling > 0 {
// 		in += uint64(tx.ValueBalanceSapling)
// 	} else if tx.ValueBalanceSapling < 0 {
// 		out += uint64(-1 * tx.ValueBalanceSapling)
// 	}
// 	if tx.ValueBalanceOrchard > 0 {
// 		in += uint64(tx.ValueBalanceOrchard)
// 	} else if tx.ValueBalanceOrchard < 0 {
// 		out += uint64(-1 * tx.ValueBalanceOrchard)
// 	}
// 	return
// }
