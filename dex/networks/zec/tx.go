// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org

package zec

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"

	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcd/wire"
)

const (
	VersionPreOverwinter = 2
	VersionOverwinter    = 3
	VersionSapling       = 4
	VersionNU5           = 5
	MaxExpiryHeight      = 499999999 // https://zips.z.cash/zip-0203

	versionSaplingGroupID = 0x892f2085
	versionNU5GroupID     = 0x26A7270A

	overwinterMask = ^uint32(1 << 31)
	pver           = 0
)

var (
	// Little-endian encoded CONSENSUS_BRANCH_IDs.
	// https://zcash.readthedocs.io/en/latest/rtd_pages/nu_dev_guide.html#canopy
	ConsensusBranchNoUpgrade  = [4]byte{0x00, 0x00, 0x00, 0x00} // 0
	ConsensusBranchOverwinter = [4]byte{0x19, 0x1B, 0xA8, 0x5B} // 207500
	ConsensusBranchSapling    = [4]byte{0xBB, 0x09, 0xB8, 0x76} // 280000
	ConsensusBranchBlossom    = [4]byte{0x60, 0x0E, 0xB4, 0x2B} // 653600
	ConsensusBranchHeartwood  = [4]byte{0x0B, 0x23, 0xB9, 0xF5} // 903000
	ConsensusBranchCanopy     = [4]byte{0xA6, 0x75, 0xFF, 0xE9} // 1046400, tesnet: 1028500
	ConsensusBranchNU5        = [4]byte{0xB4, 0xD0, 0xD6, 0xC2} // 1687104, testnet: 1842420
)

// Tx is a ZCash-adapted MsgTx.
type Tx struct {
	*wire.MsgTx
	ExpiryHeight uint32
}

// NewTxFromMsgTx creates a Tx embedding the MsgTx, and adding ZCash-specific
// fields.
func NewTxFromMsgTx(tx *wire.MsgTx, expiryHeight uint32) *Tx {
	zecTx := &Tx{
		MsgTx:        tx,
		ExpiryHeight: expiryHeight,
	}
	return zecTx
}

// TxHash generates the Hash for the transaction.
func (tx *Tx) TxHash() chainhash.Hash {
	b, _ := tx.Bytes()
	return chainhash.DoubleHashH(b)
}

// Bytes encodes the receiver to w using the bitcoin protocol encoding.
// This is part of the Message interface implementation.
// See Serialize for encoding transactions to be stored to disk, such as in a
// database, as opposed to encoding transactions for the wire.
// msg.Version must be 4 or 5.
func (tx *Tx) Bytes() (txB []byte, err error) {
	w := new(bytes.Buffer)
	if tx.Version != VersionSapling && tx.Version != VersionNU5 {
		return nil, fmt.Errorf("only version 4 (sapling) & 5 (NU5) supported")
	}

	if err = putUint32(w, uint32(tx.Version)|(1<<31)); err != nil {
		return nil, fmt.Errorf("error writing version: %w", err)
	}

	var groupID uint32 = versionSaplingGroupID
	if tx.Version == VersionNU5 {
		groupID = versionNU5GroupID
	}

	// nVersionGroupId
	if err = putUint32(w, groupID); err != nil {
		return nil, fmt.Errorf("error writing nVersionGroupId: %w", err)
	}

	if tx.Version == VersionNU5 {
		// nConsensusBranchId
		if _, err = w.Write(ConsensusBranchNU5[:]); err != nil {
			return nil, fmt.Errorf("error writing nConsensusBranchId: %w", err)
		}

		// lock_time
		if err = putUint32(w, tx.LockTime); err != nil {
			return nil, fmt.Errorf("error writing lock_time: %w", err)
		}

		// nExpiryHeight
		if err = putUint32(w, tx.ExpiryHeight); err != nil {
			return nil, fmt.Errorf("error writing nExpiryHeight: %w", err)
		}
	}

	// tx_in_count
	if err = wire.WriteVarInt(w, pver, uint64(len(tx.MsgTx.TxIn))); err != nil {
		return nil, fmt.Errorf("error writing tx_in_count: %w", err)
	}

	// tx_in
	for vin, ti := range tx.TxIn {
		if err = writeTxIn(w, tx.Version, ti); err != nil {
			return nil, fmt.Errorf("error writing tx_in %d: %w", vin, err)
		}
	}

	// tx_out_count
	if err = wire.WriteVarInt(w, pver, uint64(len(tx.TxOut))); err != nil {
		return nil, fmt.Errorf("error writing tx_out_count: %w", err)
	}

	// tx_out
	for vout, to := range tx.TxOut {
		if err = wire.WriteTxOut(w, pver, tx.Version, to); err != nil {
			return nil, fmt.Errorf("error writing tx_out %d: %w", vout, err)
		}
	}

	if tx.Version == VersionSapling {
		// lock_time
		if err = putUint32(w, tx.LockTime); err != nil {
			return nil, fmt.Errorf("error writing lock_time: %w", err)
		}

		// nExpiryHeight
		if err = putUint32(w, tx.ExpiryHeight); err != nil {
			return nil, fmt.Errorf("error writing nExpiryHeight: %w", err)
		}

		// valueBalanceSapling
		if err = putUint64(w, 0); err != nil {
			return nil, fmt.Errorf("error writing valueBalanceSapling: %w", err)
		}
	}

	// nSpendsSapling
	if err = wire.WriteVarInt(w, pver, 0); err != nil {
		return nil, fmt.Errorf("error writing nSpendsSapling: %w", err)
	}

	// nOutputsSapling
	if err = wire.WriteVarInt(w, pver, 0); err != nil {
		return nil, fmt.Errorf("error writing nOutputsSapling: %w", err)
	}

	if tx.Version == VersionSapling {
		// nJoinSplit
		if err = wire.WriteVarInt(w, pver, 0); err != nil {
			return nil, fmt.Errorf("error writing nJoinSplit: %w", err)
		}
		return w.Bytes(), nil
	}

	// NU 5

	// valueBalanceSapling for NU5. Sapling was above.
	if err = putUint64(w, 0); err != nil {
		return nil, fmt.Errorf("error writing NU5 valueBalanceSapling: %w", err)
	}

	// no anchorSapling, because nSpendsSapling = 0
	// no bindingSigSapling, because nSpendsSapling + nOutputsSapling = 0

	// nActionsOrchard
	if err = wire.WriteVarInt(w, pver, 0); err != nil {
		return nil, fmt.Errorf("error writing nActionsOrchard: %w", err)
	}

	// vActionsOrchard, flagsOrchard, valueBalanceOrchard, anchorOrchard,
	// sizeProofsOrchard, proofsOrchard, vSpendAuthSigsOrchard, and
	// bindingSigOrchard are all empty, because nActionsOrchard = 0.

	return w.Bytes(), nil
}

// see https://zips.z.cash/protocol/protocol.pdf section 7.1
func DeserializeTx(b []byte) (*Tx, error) {
	tx := &Tx{MsgTx: new(wire.MsgTx)}
	r := bytes.NewReader(b)
	if err := tx.ZecDecode(r); err != nil {
		return nil, err
	}
	return tx, nil
}

// ZecDecode reads the serialized transaction from the reader and populates the
// *Tx's fields.
func (tx *Tx) ZecDecode(r io.Reader) (err error) {
	ver, err := readUint32(r)
	if err != nil {
		return fmt.Errorf("error reading version: %w", err)
	}

	// overWintered := (ver & (1 << 31)) > 0
	ver &= overwinterMask // Clear the overwinter bit
	tx.Version = int32(ver)

	if ver > VersionNU5 {
		return fmt.Errorf("unsupported tx version %d > 4", ver)
	}

	if ver >= VersionSapling {
		// nVersionGroupId uint32
		if err = discardBytes(r, 4); err != nil {
			return fmt.Errorf("error reading nVersionGroupId: %w", err)
		}
	}

	if ver == VersionNU5 {
		// nConsensusBranchId uint32
		if err = discardBytes(r, 4); err != nil {
			return fmt.Errorf("error reading nConsensusBranchId: %w", err)
		}
		// lock_time
		if tx.LockTime, err = readUint32(r); err != nil {
			return fmt.Errorf("error reading lock_time: %w", err)
		}
		// nExpiryHeight
		if tx.ExpiryHeight, err = readUint32(r); err != nil {
			return fmt.Errorf("error reading nExpiryHeight: %w", err)
		}
	}

	txInCount, err := wire.ReadVarInt(r, pver)
	if err != nil {
		return err
	}

	tx.TxIn = make([]*wire.TxIn, 0, txInCount)
	for i := 0; i < int(txInCount); i++ {
		ti := new(wire.TxIn)
		if err = readTxIn(r, ti); err != nil {
			return err
		}
		tx.TxIn = append(tx.TxIn, ti)
	}

	txOutCount, err := wire.ReadVarInt(r, pver)
	if err != nil {
		return err
	}

	tx.TxOut = make([]*wire.TxOut, 0, txOutCount)
	for i := 0; i < int(txOutCount); i++ {
		to := new(wire.TxOut)
		if err = readTxOut(r, to); err != nil {
			return err
		}
		tx.TxOut = append(tx.TxOut, to)
	}

	if ver < VersionNU5 {
		// lock_time
		if tx.LockTime, err = readUint32(r); err != nil {
			return fmt.Errorf("error reading lock_time: %w", err)
		}
	}

	if ver == VersionOverwinter || ver == VersionSapling {
		// nExpiryHeight
		if tx.ExpiryHeight, err = readUint32(r); err != nil {
			return fmt.Errorf("error reading nExpiryHeight: %w", err)
		}
	}

	// That's it for pre-overwinter.
	if ver < 2 {
		return nil
	}

	var bindingSigRequired bool
	if ver == VersionSapling {
		// valueBalanceSpending uint64
		if err = discardBytes(r, 8); err != nil {
			return fmt.Errorf("error reading valueBalanceSpending: %w", err)
		}

		if nSpendsSapling, err := wire.ReadVarInt(r, pver); err != nil {
			return fmt.Errorf("error reading nSpendsSapling: %w", err)
		} else if nSpendsSapling > 0 {
			// vSpendsSapling - discard
			bindingSigRequired = true
			if err = discardBytes(r, int64(nSpendsSapling*384)); err != nil {
				return fmt.Errorf("error reading vSpendsSapling: %w", err)
			}
		}

		if nOutputsSapling, err := wire.ReadVarInt(r, pver); err != nil {
			return fmt.Errorf("error reading nOutputsSapling: %w", err)
		} else if nOutputsSapling > 0 {
			// vOutputsSapling - discard
			bindingSigRequired = true
			if err = discardBytes(r, int64(nOutputsSapling*948)); err != nil {
				return fmt.Errorf("error reading vOutputsSapling: %w", err)
			}
		}
	}

	if ver < VersionNU5 {
		if nJoinSplit, err := wire.ReadVarInt(r, pver); err != nil {
			return fmt.Errorf("error reading nJoinSplit: %w", err)
		} else if nJoinSplit > 0 {
			// vJoinSplit - discard
			sz := 1802 * nJoinSplit
			if ver == 4 {
				sz = 1698 * nJoinSplit
			}
			if err = discardBytes(r, int64(sz)); err != nil {
				return fmt.Errorf("error reading vJoinSplit: %w", err)
			}
			// joinSplitPubKey
			if err = discardBytes(r, 32); err != nil {
				return fmt.Errorf("error reading joinSplitPubKey: %w", err)
			}

			// joinSplitSig
			if err = discardBytes(r, 64); err != nil {
				return fmt.Errorf("error reading joinSplitSig: %w", err)
			}
		}
	} else { // NU5
		// nSpendsSapling
		nSpendsSapling, err := wire.ReadVarInt(r, pver)
		if err != nil {
			return fmt.Errorf("error reading nSpendsSapling: %w", err)
		} else if nSpendsSapling > 0 {
			// vSpendsSapling - discard
			bindingSigRequired = true
			if err = discardBytes(r, int64(nSpendsSapling*96)); err != nil {
				return fmt.Errorf("error reading vSpendsSapling: %w", err)
			}
		}

		// nOutputsSapling
		if nOutputsSapling, err := wire.ReadVarInt(r, pver); err != nil {
			return fmt.Errorf("error reading nSpendsSapling: %w", err)
		} else if nOutputsSapling > 0 {
			// vOutputsSapling - discard
			bindingSigRequired = true
			if err = discardBytes(r, int64(nOutputsSapling*756)); err != nil {
				return fmt.Errorf("error reading vOutputsSapling: %w", err)
			}
		}
		// valueBalanceSpending uint64
		if err = discardBytes(r, 8); err != nil {
			return fmt.Errorf("error reading valueBalanceSpending: %w", err)
		}

		if nSpendsSapling > 0 {
			// anchorSapling
			if err = discardBytes(r, 32); err != nil {
				return fmt.Errorf("error reading anchorSapling: %w", err)
			}
			// vSpendProofsSapling
			if err = discardBytes(r, int64(nSpendsSapling*192)); err != nil {
				return fmt.Errorf("error reading vSpendProofsSapling: %w", err)
			}
			// vSpendAuthSigsSapling
			if err = discardBytes(r, int64(nSpendsSapling*64)); err != nil {
				return fmt.Errorf("error reading vSpendAuthSigsSapling: %w", err)
			}
			// vOutputProofsSapling
			if err = discardBytes(r, int64(nSpendsSapling*192)); err != nil {
				return fmt.Errorf("error reading vOutputProofsSapling: %w", err)
			}
		}
	}

	if bindingSigRequired {
		// bindingSigSapling
		if err = discardBytes(r, 64); err != nil {
			return fmt.Errorf("error reading bindingSigSapling: %w", err)
		}
	}

	// pre-NU5 is done now.
	if ver < VersionNU5 {
		return nil
	}

	// NU5-only fields below.

	// nActionsOrchard
	nActionsOrchard, err := wire.ReadVarInt(r, pver)
	if err != nil {
		return fmt.Errorf("error reading bindingSigSapling: %w", err)
	}

	if nActionsOrchard == 0 {
		return nil
	}

	// vActionsOrchard
	if err = discardBytes(r, int64(nActionsOrchard*820)); err != nil {
		return fmt.Errorf("error reading vActionsOrchard: %w", err)
	}

	// flagsOrchard
	if err = discardBytes(r, 1); err != nil {
		return fmt.Errorf("error reading flagsOrchard: %w", err)
	}

	// valueBalanceOrchard uint64
	if err = discardBytes(r, 8); err != nil {
		return fmt.Errorf("error reading valueBalanceOrchard: %w", err)
	}

	// anchorOrchard
	if err = discardBytes(r, 32); err != nil {
		return fmt.Errorf("error reading anchorOrchard: %w", err)
	}

	// sizeProofsOrchard
	sizeProofsOrchard, err := wire.ReadVarInt(r, pver)
	if err != nil {
		return fmt.Errorf("error reading sizeProofsOrchard: %w", err)
	}

	// proofsOrchard
	if err = discardBytes(r, int64(sizeProofsOrchard)); err != nil {
		return fmt.Errorf("error reading proofsOrchard: %w", err)
	}

	// vSpendAuthSigsOrchard
	if err = discardBytes(r, int64(nActionsOrchard*64)); err != nil {
		return fmt.Errorf("error reading vSpendAuthSigsOrchard: %w", err)
	}

	// bindingSigOrchard
	if err = discardBytes(r, 64); err != nil {
		return fmt.Errorf("error reading bindingSigOrchard: %w", err)
	}

	return nil
}

// writeTxIn encodes ti to the bitcoin protocol encoding for a transaction
// input (TxIn) to w.
func writeTxIn(w io.Writer, version int32, ti *wire.TxIn) error {
	err := writeOutPoint(w, version, &ti.PreviousOutPoint)
	if err != nil {
		return err
	}

	err = wire.WriteVarBytes(w, pver, ti.SignatureScript)
	if err != nil {
		return err
	}

	return putUint32(w, ti.Sequence)
}

// writeOutPoint encodes op to the bitcoin protocol encoding for an OutPoint
// to w.
func writeOutPoint(w io.Writer, version int32, op *wire.OutPoint) error {
	_, err := w.Write(op.Hash[:])
	if err != nil {
		return err
	}
	return putUint32(w, op.Index)
}

// putUint32 writes a little-endian encoded uint32 to the Writer.
func putUint32(w io.Writer, v uint32) error {
	b := make([]byte, 4)
	binary.LittleEndian.PutUint32(b, v)
	_, err := w.Write(b)
	return err
}

// putUint32 writes a little-endian encoded uint64 to the Writer.
func putUint64(w io.Writer, v uint64) error {
	b := make([]byte, 8)
	binary.LittleEndian.PutUint64(b, v)
	_, err := w.Write(b)
	return err
}

// readUint32 reads a little-endian encoded uint32 from the Reader.
func readUint32(r io.Reader) (uint32, error) {
	b := make([]byte, 4)
	if _, err := io.ReadFull(r, b); err != nil {
		return 0, err
	}
	return binary.LittleEndian.Uint32(b), nil
}

// readUint64 reads a little-endian encoded uint64 from the Reader.
func readUint64(r io.Reader) (uint64, error) {
	b := make([]byte, 8)
	if _, err := io.ReadFull(r, b); err != nil {
		return 0, err
	}
	return binary.LittleEndian.Uint64(b), nil
}

// readTxIn reads the next sequence of bytes from r as a transaction input.
func readTxIn(r io.Reader, ti *wire.TxIn) error {
	err := readOutPoint(r, &ti.PreviousOutPoint)
	if err != nil {
		return err
	}

	ti.SignatureScript, err = readScript(r)
	if err != nil {
		return err
	}

	ti.Sequence, err = readUint32(r)
	return err
}

// readTxOut reads the next sequence of bytes from r as a transaction output.
func readTxOut(r io.Reader, to *wire.TxOut) error {
	v, err := readUint64(r)
	if err != nil {
		return err
	}
	to.Value = int64(v)

	to.PkScript, err = readScript(r)
	return err
}

// readOutPoint reads the next sequence of bytes from r as an OutPoint.
func readOutPoint(r io.Reader, op *wire.OutPoint) error {
	_, err := io.ReadFull(r, op.Hash[:])
	if err != nil {
		return err
	}

	op.Index, err = readUint32(r)
	return err
}

// readScript reads a variable length byte array. Copy of unexported
// btcd/wire.readScript.
func readScript(r io.Reader) ([]byte, error) {
	count, err := wire.ReadVarInt(r, pver)
	if err != nil {
		return nil, err
	}
	if count > uint64(wire.MaxMessagePayload) {
		return nil, fmt.Errorf("larger than the max allowed size "+
			"[count %d, max %d]", count, wire.MaxMessagePayload)
	}
	b := make([]byte, count)
	_, err = io.ReadFull(r, b)
	if err != nil {
		return nil, err
	}
	return b, nil
}

func discardBytes(r io.Reader, n int64) error {
	m, err := io.CopyN(io.Discard, r, n)
	if err != nil {
		return err
	}
	if m != n {
		return fmt.Errorf("only discarded %d of %d bytes", m, n)
	}
	return nil
}

// CalcTxSize calculates the size of a ZCash transparent transaction. CalcTxSize
// won't return accurate results for shielded or blended transactions.
func CalcTxSize(tx *wire.MsgTx) uint64 {
	var sz uint64 = 4 // header
	ver := tx.Version
	sz += uint64(wire.VarIntSerializeSize(uint64(len(tx.TxIn)))) // tx_in_count
	for _, txIn := range tx.TxIn {                               // tx_in
		sz += 32 /* prev hash */ + 4 /* prev index */ + 4 /* sequence */
		sz += uint64(wire.VarIntSerializeSize(uint64(len(txIn.SignatureScript))) + len(txIn.SignatureScript))
	}
	sz += uint64(wire.VarIntSerializeSize(uint64(len(tx.TxOut)))) // tx_out_count
	for _, txOut := range tx.TxOut {                              // tx_out
		sz += 8 /* value */
		sz += uint64(wire.VarIntSerializeSize(uint64(len(txOut.PkScript))) + len(txOut.PkScript))
	}
	sz += 4 // lockTime
	if ver >= VersionOverwinter {
		sz += 4 // nExpiryHeight
		if ver < VersionNU5 {
			sz++ // nJoinSplit. removed in NU5
		}
	}
	if ver >= VersionSapling {
		sz += 4 // nVersionGroupId
		sz += 8 // valueBalanceSapling
		sz++    // nSpendsSapling varint
		sz++    // nOutputsSapling varint
	}
	if ver == VersionNU5 {
		// With nSpendsSapling = 0 and nOutputsSapling = 0
		sz += 4 // nConsensusBranchId
		sz++    // nActionsOrchard
	}
	return sz
}
