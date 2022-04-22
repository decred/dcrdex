// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package doge

import (
	"bytes"
	"errors"
	"fmt"

	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcd/wire"
)

func readMerkleBranch(buf *bytes.Buffer) error {
	// Merkle branch for parent coinbase <-> parent Merkle root
	branchLen, err := wire.ReadVarInt(buf, 0)
	if err != nil {
		return err
	}
	for i := 0; i < int(branchLen); i++ {
		_, err := chainhash.NewHash(buf.Next(32))
		if err != nil {
			return err
		}
	}
	if buf.Len() < 4 {
		return errors.New("out of bytes")
	}
	buf.Next(4) // int32 branch side mask / Merkle tree index
	return nil
}

// DeserializeBlock decodes the bytes of a Dogecoin block. The block header and
// transaction serializations are identical to Bitcoin, but there is an optional
// "AuxPOW" section to support merged mining. The AuxPOW section is after the
// nonce field of the header, but before the block's transactions. This function
// decodes and discards the AuxPOW section if it exists.
//
// Refs:
// https://en.bitcoin.it/wiki/Merged_mining_specification#Aux_proof-of-work_block
// https://github.com/dogecoin/dogecoin/blob/31afd133119dd2e15862d46530cb99424cf564b0/src/primitives/block.h#L41-L46
// https://github.com/dogecoin/dogecoin/blob/31afd133119dd2e15862d46530cb99424cf564b0/src/auxpow.h#L155-L160
func DeserializeBlock(blk []byte) (*wire.MsgBlock, error) {

	blkBuf := bytes.NewBuffer(blk)

	// Block header
	hdr := &wire.BlockHeader{}
	err := hdr.Deserialize(blkBuf)
	if err != nil {
		return nil, fmt.Errorf("failed to deserialize block header: %w", err)
	}

	// AuxPOW region (optional)
	isAuxPow := hdr.Version&(1<<8) != 0
	if isAuxPow {
		// Coinbase tx of parent block on other chain (i.e. LTC)
		err = new(wire.MsgTx).Deserialize(blkBuf)
		if err != nil {
			return nil, fmt.Errorf("failed to deserialize AuxPOW>coinbase_txn: %w", err)
		}

		// Parent block hash
		_, err = chainhash.NewHash(blkBuf.Next(32))
		if err != nil {
			return nil, fmt.Errorf("failed to deserialize AuxPOW>parent_block_hash: %w", err)
		}
		if blkBuf.Len() == 0 {
			return nil, errors.New("out of bytes in AuxPOW section")
		}

		// Merkle branch for parent coinbase <-> parent Merkle root
		err = readMerkleBranch(blkBuf)
		if err != nil {
			return nil, fmt.Errorf("failed to deserialize AuxPOW>coinbase_branch: %w", err)
		}
		// Merkle branch for parent chain <-> other chains
		err = readMerkleBranch(blkBuf)
		if err != nil {
			return nil, fmt.Errorf("failed to deserialize AuxPOW>blockchain_branch: %w", err)
		}

		// Parent block header
		err = new(wire.BlockHeader).Deserialize(blkBuf)
		if err != nil {
			return nil, fmt.Errorf("failed to deserialize AuxPOW>parent_block_header: %w", err)
		}
	}

	// This block's transactions
	txnCount, err := wire.ReadVarInt(blkBuf, 0)
	if err != nil {
		return nil, fmt.Errorf("failed to parse transaction count: %w", err)
	}

	txns := make([]*wire.MsgTx, int(txnCount))

	for i := range txns {
		msgTx := &wire.MsgTx{}
		err = msgTx.Deserialize(blkBuf)
		if err != nil {
			return nil, fmt.Errorf("failed to deserialize transaction %d of %d in block %v: %w",
				i+1, txnCount, hdr.BlockHash(), err)
		}
		txns[i] = msgTx
	}

	return &wire.MsgBlock{
		Header:       *hdr,
		Transactions: txns,
	}, nil
}
