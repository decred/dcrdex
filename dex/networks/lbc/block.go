// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package lbc

import (
	"bytes"
	"fmt"
	"io"
	"time"

	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcd/wire"
)

// LBC block headers are 112 bytes: the usual Bitcoin fields plus a ClaimTrie
// hash (32 bytes) after MerkleRoot.
//
// Layout: Version | PrevBlock | MerkleRoot | ClaimTrie | Timestamp | Bits | Nonce
const (
	claimTrieSize     = 32
	lbcBlockHeaderLen = 112
)

// DeserializeBlock decodes a serialized LBC block into a btcsuite MsgBlock.
// Transactions use standard Bitcoin wire encoding (including SegWit).
//
// NOTE: The returned Header does not retain ClaimTrie. Consequently
// Header.BlockHash() / MsgBlock.BlockHash() are NOT the LBC block hash.
// Callers that need the real hash should use node RPC (getblockhash, etc.).
// PrevBlock, MerkleRoot, Timestamp, Version, Bits, and Nonce are correct.
func DeserializeBlock(blk []byte) (*wire.MsgBlock, error) {
	return DeserializeBlockBytes(blk)
}

// DeserializeBlockBytes is an alias used by some clone backends.
func DeserializeBlockBytes(blk []byte) (*wire.MsgBlock, error) {
	r := bytes.NewReader(blk)

	hdr, err := deserializeHeader(r)
	if err != nil {
		return nil, fmt.Errorf("failed to deserialize LBC block header: %w", err)
	}

	txnCount, err := wire.ReadVarInt(r, 0)
	if err != nil {
		return nil, fmt.Errorf("failed to parse transaction count: %w", err)
	}

	txns := make([]*wire.MsgTx, int(txnCount))
	for i := range txns {
		msgTx := &wire.MsgTx{}
		if err = msgTx.Deserialize(r); err != nil {
			return nil, fmt.Errorf("failed to deserialize transaction %d of %d: %w",
				i+1, txnCount, err)
		}
		txns[i] = msgTx
	}

	return &wire.MsgBlock{
		Header:       *hdr,
		Transactions: txns,
	}, nil
}

func deserializeHeader(r io.Reader) (*wire.BlockHeader, error) {
	var (
		version                    int32
		prevBlock, merkle, claimTr chainhash.Hash
		timestamp                  uint32
		bits, nonce                uint32
	)

	if err := readElements(r,
		&version,
		&prevBlock,
		&merkle,
		&claimTr, // discarded for wire.BlockHeader
		&timestamp,
		&bits,
		&nonce,
	); err != nil {
		return nil, err
	}
	_ = claimTr

	return &wire.BlockHeader{
		Version:    version,
		PrevBlock:  prevBlock,
		MerkleRoot: merkle,
		Timestamp:  time.Unix(int64(timestamp), 0),
		Bits:       bits,
		Nonce:      nonce,
	}, nil
}

// readElements reads successive binary little-endian fields from r.
func readElements(r io.Reader, elements ...interface{}) error {
	for _, el := range elements {
		switch e := el.(type) {
		case *int32:
			var b [4]byte
			if _, err := io.ReadFull(r, b[:]); err != nil {
				return err
			}
			*e = int32(uint32(b[0]) | uint32(b[1])<<8 | uint32(b[2])<<16 | uint32(b[3])<<24)
		case *uint32:
			var b [4]byte
			if _, err := io.ReadFull(r, b[:]); err != nil {
				return err
			}
			*e = uint32(b[0]) | uint32(b[1])<<8 | uint32(b[2])<<16 | uint32(b[3])<<24
		case *chainhash.Hash:
			if _, err := io.ReadFull(r, e[:]); err != nil {
				return err
			}
		default:
			return fmt.Errorf("unsupported element type %T", el)
		}
	}
	return nil
}
