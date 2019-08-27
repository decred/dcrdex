// Copyright (c) 2019, The Decred developers
// See LICENSE for details.

package dcr

import (
	"bytes"
	"fmt"

	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/dcrutil/v2"
	"github.com/decred/dcrdex/server/asset"
)

// A UTXO is information regarding an unspent transaction output. It must
// satisfy the asset.UTXO interface to be DEX-compatible.
type UTXO struct {
	// Because a UTXO's validity and block info can change after creation, keep a
	// dcrBackend around to query the state of the tx and update the block info.
	dcr          *dcrBackend
	height       uint32
	blockHash    chainhash.Hash
	txHash       chainhash.Hash
	vout         uint32
	maturity     int32
	scriptType   dcrScriptType
	pkScript     []byte
	redeemScript []byte
	numSigs      int
	size         uint32
}

// Check that UTXO satisfies the asset.UTXO interface
var _ asset.UTXO = (*UTXO)(nil)

// Confirmations is an asset.DEXAsset method that returns the number of
// confirmations for a UTXO. Because it is possible for a UTXO that was once
// considered valid to later be considered invalid, Confirmations can return
// an error to indicate the UTXO is no longer valid. The definition of UTXO
// validity should not be confused with the validity of regular tree
// transactions that is voted on by stakeholders. While stakeholder approval is
// a part for UTXO validity, there are other considerations as well.
func (utxo *UTXO) Confirmations() (int64, error) {
	dcr := utxo.dcr
	// If the UTXO was in a mempool transaction, check if it has been confirmed.
	if utxo.height == 0 {
		txOut, verboseTx, _, err := dcr.getTxOutInfo(&utxo.txHash, utxo.vout)
		if err != nil {
			return -1, err
		}
		// More than zero confirmations would indicate that the transaction has
		// been mined. Collect the block info and update the utxo fields.
		if txOut.Confirmations > 0 {
			blk, err := dcr.getBlockInfo(verboseTx.BlockHash)
			if err != nil {
				return -1, err
			}
			utxo.height = uint32(blk.height)
			utxo.blockHash = blk.hash
		}
	} else {
		// The UTXO was included in a block, but make sure that the utxo's block has
		// not been orphaned or voted as invalid.
		mainchainBlock, found := dcr.blockCache.atHeight(utxo.height)
		if !found {
			return -1, fmt.Errorf("no mainchain block for tx %s at height %d", utxo.txHash.String(), utxo.height)
		}
		// If the UTXO's block has been orphaned, check for a new containing block.
		if mainchainBlock.hash != utxo.blockHash {
			// See if we can find the utxo in another block.
			newUtxo, err := dcr.utxo(&utxo.txHash, utxo.vout, utxo.redeemScript)
			if err != nil {
				return -1, fmt.Errorf("utxo block is not mainchain")
			}
			*utxo = *newUtxo
			if utxo.height == 0 {
				mainchainBlock = nil
			} else {
				mainchainBlock, found = dcr.blockCache.atHeight(utxo.height)
				if !found {
					return -1, fmt.Errorf("new block not found for utxo moved from orphaned block")
				}
			}
		}
		// If the block is set, check for stakeholder invalidation. Stakeholders
		// can only invalidate a regular-tree transaction.
		if mainchainBlock != nil && !utxo.scriptType.isStake() {
			nextBlock, err := dcr.getMainchainDcrBlock(utxo.height + 1)
			if err != nil {
				return -1, fmt.Errorf("error retreiving approving block for utxo %s:%d: %v", utxo.txHash, utxo.vout, err)
			}
			if nextBlock != nil && !nextBlock.vote {
				return -1, fmt.Errorf("utxo's block has been voted as invalid")
			}
		}
	}
	// If the height is still 0, this is a mempool transaction.
	if utxo.height == 0 {
		return 0, nil
	}
	// Otherwise just check that there hasn't been a reorg which would render the
	// output immature. This would be exceedingly rare (impossible?).
	confs := int32(dcr.blockCache.tipHeight()) - int32(utxo.height) + 1
	if confs < utxo.maturity {
		return -1, fmt.Errorf("transaction %s became immature", utxo.txHash)
	}
	return int64(confs), nil
}

// PaysToPubkeys verifies that the utxo pays to the supplied public key(s). This
// is an asset.DEXAsset method.
func (utxo *UTXO) PaysToPubkeys(pubkeys [][]byte) (bool, error) {
	if len(pubkeys) < utxo.numSigs {
		return false, fmt.Errorf("not enough signatures for utxo %s:%d. expected %d, got %d", utxo.txHash, utxo.vout, utxo.numSigs, len(pubkeys))
	}
	scriptAddrs, err := extractScriptAddrs(utxo.scriptType, utxo.pkScript, utxo.redeemScript)
	if err != nil {
		return false, err
	}
	if scriptAddrs.nRequired != utxo.numSigs {
		return false, fmt.Errorf("signature requirement mismatch for utxo %s:%d. %d != %d", utxo.txHash, utxo.vout, scriptAddrs.nRequired, utxo.numSigs)
	}
	numMatches := countMatches(pubkeys, scriptAddrs.pubkeys, nil)
	numMatches += countMatches(pubkeys, scriptAddrs.pkHashes, dcrutil.Hash160)
	if numMatches < utxo.numSigs {
		return false, fmt.Errorf("not enough pubkey matches to satisfy the script for utxo %s:%d. expected %d, got %d", utxo.txHash, utxo.vout, utxo.numSigs, numMatches)
	}
	return true, nil
}

// countMatches looks through a set of addresses and a set of pubkeys and counts
// the matches.
func countMatches(pubkeys [][]byte, addrs []dcrutil.Address, hasher func([]byte) []byte) int {
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

// ScriptSize returns the maximum spend script size of the UTXO, in bytes.
// This is a method of the asset.UTXO interface.
func (utxo *UTXO) ScriptSize() uint32 {
	return utxo.size
}

// TxHash is the transaction hash. TxHash is a method of the asset.UTXO
// interface.
func (utxo *UTXO) TxHash() []byte {
	return utxo.txHash.CloneBytes()
}

// Vout is the output index. Vout is a method of the asset.UTXO interface.
func (utxo *UTXO) Vout() uint32 {
	return utxo.vout
}

// TxID is the txid, which is the hex-encoded byte-reversed transaction hash.
// TxID is a method of the asset.UTXO interface.
func (utxo *UTXO) TxID() string {
	return utxo.txHash.String()
}
