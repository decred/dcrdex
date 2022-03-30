// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org

package zec

import (
	"bytes"
	"fmt"
	"io"
	"time"

	"github.com/btcsuite/btcd/wire"
)

// Block extends a wire.MsgBlock to specify ZCash specific fields, or in the
// case of the Nonce, a type-variant.
type Block struct {
	wire.MsgBlock
	// Transactions and MsgBlock.Transactions should both be populated. Each
	// *Tx.MsgTx will be the same as the *MsgTx in MsgBlock.Transactions.
	Transactions         []*Tx
	HashBlockCommitments [32]byte // Using NU5 name
	Nonce                [32]byte // Bitcoin uses uint32
	Solution             []byte   // length 1344 on main and testnet, 36 on regtest
}

// DeserializeBlock deserializes the ZCash-encoded block.
func DeserializeBlock(b []byte) (*Block, error) {
	zecBlock := &Block{}

	// https://zips.z.cash/protocol/protocol.pdf section 7.6
	r := bytes.NewReader(b)

	if err := zecBlock.decodeBlockHeader(r); err != nil {
		return nil, err
	}

	txCount, err := wire.ReadVarInt(r, pver)
	if err != nil {
		return nil, err
	}

	// TODO: Limit txCount based on block size, header size, min tx size.

	zecBlock.MsgBlock.Transactions = make([]*wire.MsgTx, 0, txCount)
	zecBlock.Transactions = make([]*Tx, 0, txCount)
	for i := uint64(0); i < txCount; i++ {
		tx := &Tx{MsgTx: new(wire.MsgTx)}

		if err := tx.ZecDecode(r); err != nil {
			return nil, err
		}
		zecBlock.MsgBlock.Transactions = append(zecBlock.MsgBlock.Transactions, tx.MsgTx)
		zecBlock.Transactions = append(zecBlock.Transactions, tx)
	}

	return zecBlock, nil
}

// See github.com/zcash/zcash CBlockHeader -> SerializeOp
func (z *Block) decodeBlockHeader(r io.Reader) error {
	hdr := &z.MsgBlock.Header

	nVersion, err := readUint32(r)
	if err != nil {
		return err
	}
	hdr.Version = int32(nVersion)

	if err = readInternalByteOrder(r, hdr.PrevBlock[:]); err != nil {
		return err
	}

	if err := readInternalByteOrder(r, hdr.MerkleRoot[:]); err != nil {
		return err
	}

	_, err = io.ReadFull(r, z.HashBlockCommitments[:])
	if err != nil {
		return err
	}

	nTime, err := readUint32(r)
	if err != nil {
		return err
	}
	hdr.Timestamp = time.Unix(int64(nTime), 0)

	hdr.Bits, err = readUint32(r)
	if err != nil {
		return err
	}

	err = readInternalByteOrder(r, z.Nonce[:])
	if err != nil {
		return err
	}

	solSize, err := wire.ReadVarInt(r, pver)
	if err != nil {
		return err
	}
	if solSize != 1344 && solSize != 36 {
		return fmt.Errorf("wrong solution size %d", solSize)
	}
	z.Solution = make([]byte, solSize)

	_, err = io.ReadFull(r, z.Solution)
	if err != nil {
		return err
	}

	return nil
}

func readInternalByteOrder(r io.Reader, b []byte) error {
	if _, err := io.ReadFull(r, b); err != nil {
		return err
	}
	// Reverse the bytes
	for i, j := 0, len(b)-1; i < j; i, j = i+1, j-1 {
		b[i], b[j] = b[j], b[i]
	}
	return nil
}
