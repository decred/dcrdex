// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org

package ltc

import (
	"bytes"
	"fmt"
	"io"

	"github.com/btcsuite/btcd/wire"
)

const (
	// mwebVer is the bit of the block header's version that indicates the
	// presence of a MWEB.
	mwebVer = 0x20000000 // 1 << 29
)

func parseMWEB(blk io.Reader) error {
	dec := newDecoder(blk)
	// src/mweb/mweb_models.h - struct Block
	// "A convenience wrapper around a possibly-null extension block.""
	// OptionalPtr around a mw::Block. Read the option byte:
	hasMWEB, err := dec.readByte()
	if err != nil {
		return fmt.Errorf("failed to check MWEB option byte: %w", err)
	}
	if hasMWEB == 0 {
		return nil
	}

	// src/libmw/include/mw/models/block/Block.h - class Block
	// (1) Header and (2) TxBody

	// src/libmw/include/mw/models/block/Header.h - class Header
	// height
	if _, err = dec.readVLQ(); err != nil {
		return fmt.Errorf("failed to decode MWEB height: %w", err)
	}

	// 3x Hash + 2x BlindingFactor
	if err = dec.discardBytes(32*3 + 32*2); err != nil {
		return fmt.Errorf("failed to decode MWEB junk: %w", err)
	}

	// Number of TXOs: outputMMRSize
	if _, err = dec.readVLQ(); err != nil {
		return fmt.Errorf("failed to decode TXO count: %w", err)
	}

	// Number of kernels: kernelMMRSize
	if _, err = dec.readVLQ(); err != nil {
		return fmt.Errorf("failed to decode kernel count: %w", err)
	}

	// TxBody
	_, err = dec.readMWTXBody()
	if err != nil {
		return fmt.Errorf("failed to decode MWEB tx: %w", err)
	}
	// if len(kern0) > 0 {
	// 	mwebTxID := chainhash.Hash(blake3.Sum256(kern0))
	// 	fmt.Println(mwebTxID.String())
	// }

	return nil
}

// DeserializeBlock decodes the bytes of a serialized Litecoin block. This
// function exists because MWEB changes both the block and transaction
// serializations. Blocks may have a MW "extension block" for "peg-out"
// transactions, and this EB is after all the transactions in the regular LTC
// block. After the canonical transactions in the regular block, there may be
// zero or more "peg-in" transactions followed by one integration transaction
// (also known as a HogEx transaction), all still in the regular LTC block. The
// peg-in txns decode correctly, but the integration tx is a special transaction
// with the witness tx flag with bit 3 set (8), which prevents correct
// wire.MsgTx deserialization.
// Refs:
// https://github.com/litecoin-project/lips/blob/master/lip-0002.mediawiki#PegOut_Transactions
// https://github.com/litecoin-project/lips/blob/master/lip-0003.mediawiki#Specification
// https://github.com/litecoin-project/litecoin/commit/9d1f530a5fa6d16871fdcc3b506be42b593d3ce4
// https://github.com/litecoin-project/litecoin/commit/8c82032f45e644f413ec5c91e121a31c993aa831
// (src/libmw/include/mw/models/tx/Transaction.h for the `mweb_tx` field of the
// `CTransaction` in the "primitives" commit).
func DeserializeBlock(blk io.Reader) (*wire.MsgBlock, error) {
	// Block header
	hdr := &wire.BlockHeader{}
	err := hdr.Deserialize(blk)
	if err != nil {
		return nil, fmt.Errorf("failed to deserialize block header: %w", err)
	}

	// This block's transactions
	txnCount, err := wire.ReadVarInt(blk, 0)
	if err != nil {
		return nil, fmt.Errorf("failed to parse transaction count: %w", err)
	}

	// We can only decode the canonical txns, not the mw peg-in txs in the EB.
	var hasHogEx bool
	txns := make([]*wire.MsgTx, 0, int(txnCount))
	for i := 0; i < cap(txns); i++ {
		msgTx, err := DeserializeTx(blk)
		if err != nil {
			return nil, fmt.Errorf("failed to deserialize transaction %d of %d in block %v: %w",
				i+1, txnCount, hdr.BlockHash(), err)
		}
		txns = append(txns, msgTx.MsgTx) // txns = append(txns, msgTx)
		hasHogEx = msgTx.IsHogEx         // hogex is the last txn
	}

	// The mwebVer mask indicates it may contain a MWEB after a HogEx.
	// src/primitives/block.h: SERIALIZE_NO_MWEB
	if hdr.Version&mwebVer != 0 && hasHogEx {
		if err = parseMWEB(blk); err != nil {
			return nil, err
		}
	}

	return &wire.MsgBlock{
		Header:       *hdr,
		Transactions: txns,
	}, nil
}

// DeserializeBlockBytes wraps DeserializeBlock using bytes.NewReader for
// convenience.
func DeserializeBlockBytes(blk []byte) (*wire.MsgBlock, error) {
	return DeserializeBlock(bytes.NewReader(blk))
}
