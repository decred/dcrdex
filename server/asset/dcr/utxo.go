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
	dcr       *dcrBackend
	height    uint32
	blockHash chainhash.Hash
	txHash    chainhash.Hash
	vout      uint32
	maturity  int32
	pkScript  []byte
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
			return -1, fmt.Errorf("no mainchain block for tx %s at height %d", utxo.txHash, utxo.height)
		}
		// If the UTXO's block has been orphaned, check for a new containing block.
		if mainchainBlock.hash != utxo.blockHash {
			// See if we can find the utxo in another block.
			newUtxo, err := dcr.utxo(&utxo.txHash, utxo.vout)
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
		// If the block is set, check for stakeholder invalidation.
		if mainchainBlock != nil && !mainchainBlock.txInStakeTree(&utxo.txHash) {
			nextBlock, found := dcr.blockCache.atHeight(utxo.height + 1)
			if found && !nextBlock.vote {
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

// PaysToPubkey verifies that the utxo pays to the supplied public key. This is
// an asset.DEXAsset method. The second argument is for additional data as would
// be needed if P2SH support was enabled.
func (utxo *UTXO) PaysToPubkey(pubkey, _ []byte) bool {
	pkHash := dcrutil.Hash160(pubkey)
	extracted := extractPubKeyHash(utxo.pkScript)
	return extracted != nil && bytes.Equal(extracted, pkHash)
}
