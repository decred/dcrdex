package ltc

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"

	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcd/wire"

	"lukechampine.com/blake3"
)

var byteOrder = binary.LittleEndian

const (
	pver               uint32 = 0                             // only protocol version 0 supported
	maxTxInPerMessage         = wire.MaxMessagePayload/41 + 1 // wire.maxTxInPerMessage
	maxTxOutPerMessage        = wire.MaxMessagePayload/       // wire.maxTxOutPerMessage
		wire.MinTxOutPayload + 1
	maxWitnessItemsPerInput = 500000 // from wire
	maxWitnessItemSize      = 11000  // from wire
)

type decoder struct {
	buf [8]byte
	io.Reader
	tee *bytes.Buffer
}

func newDecoder(r io.Reader) *decoder {
	return &decoder{Reader: r}
}

func (d *decoder) mirror(b []byte) {
	if d.tee != nil {
		d.tee.Write(b)
	}
}

func (d *decoder) readByte() (byte, error) {
	b := d.buf[:1]
	if _, err := io.ReadFull(d, b); err != nil {
		return 0, err
	}
	d.mirror(b)
	return b[0], nil
}

func (d *decoder) readUint16() (uint16, error) {
	b := d.buf[:2]
	if _, err := io.ReadFull(d, b); err != nil {
		return 0, err
	}
	d.mirror(b)
	return byteOrder.Uint16(b), nil
}

func (d *decoder) readUint32() (uint32, error) {
	b := d.buf[:4]
	if _, err := io.ReadFull(d, b); err != nil {
		return 0, err
	}
	d.mirror(b)
	return byteOrder.Uint32(b), nil
}

func (d *decoder) readUint64() (uint64, error) {
	b := d.buf[:]
	if _, err := io.ReadFull(d, b); err != nil {
		return 0, err
	}
	d.mirror(b)
	return byteOrder.Uint64(b), nil
}

// readOutPoint reads the next sequence of bytes from r as an OutPoint.
func (d *decoder) readOutPoint(op *wire.OutPoint) error {
	_, err := io.ReadFull(d, op.Hash[:])
	if err != nil {
		return err
	}
	d.mirror(op.Hash[:])

	op.Index, err = d.readUint32()
	return err
}

// wire.ReadVarInt a.k.a. CompactSize, not VARINT
// https://en.bitcoin.it/wiki/Protocol_documentation#Variable_length_integer
func (d *decoder) readCompactSize() (uint64, error) {
	// Compact Size
	// size <  253        -- 1 byte
	// size <= USHRT_MAX  -- 3 bytes  (253 + 2 bytes)
	// size <= UINT_MAX   -- 5 bytes  (254 + 4 bytes)
	// size >  UINT_MAX   -- 9 bytes  (255 + 8 bytes)
	chSize, err := d.readByte()
	if err != nil {
		return 0, err
	}
	switch chSize {
	case 253:
		sz, err := d.readUint16()
		if err != nil {
			return 0, err
		}
		return uint64(sz), nil
	case 254:
		sz, err := d.readUint32()
		if err != nil {
			return 0, err
		}
		return uint64(sz), nil
	case 255:
		sz, err := d.readUint64()
		if err != nil {
			return 0, err
		}
		return sz, nil
	default: // < 253
		return uint64(chSize), nil
	}
}

// variable length quantity, not supposed to be part of the wire protocol.
// Not the same as CompactSize type in the C++ code, but rather VARINT.
// https://en.wikipedia.org/wiki/Variable-length_quantity
// This function is borrowed from dcrd.
func (d *decoder) readVLQ() (uint64, error) {
	var n uint64
	for {
		val, err := d.readByte()
		if err != nil {
			return 0, err
		}
		n = (n << 7) | uint64(val&0x7f)
		if val&0x80 != 0x80 {
			break
		}
		n++
	}

	return n, nil
}

// readTxIn reads the next sequence of bytes from r as a transaction input.
func (d *decoder) readTxIn(ti *wire.TxIn) error {
	err := d.readOutPoint(&ti.PreviousOutPoint)
	if err != nil {
		return err
	}

	ti.SignatureScript, err = wire.ReadVarBytes(d, pver, wire.MaxMessagePayload, "sigScript")
	if err != nil {
		return err
	}
	if d.tee != nil {
		_ = wire.WriteVarBytes(d.tee, pver, ti.SignatureScript)
	}

	ti.Sequence, err = d.readUint32()
	return err
}

// readTxOut reads the next sequence of bytes from r as a transaction output.
func (d *decoder) readTxOut(to *wire.TxOut) error {
	v, err := d.readUint64()
	if err != nil {
		return err
	}

	pkScript, err := wire.ReadVarBytes(d, pver, wire.MaxMessagePayload, "pkScript")
	if err != nil {
		return err
	}
	if d.tee != nil {
		_ = wire.WriteVarBytes(d.tee, pver, pkScript)
	}

	to.Value = int64(v)
	to.PkScript = pkScript

	return nil
}

func (d *decoder) discardBytes(n int64) error {
	w := io.Discard
	if d.tee != nil {
		w = d.tee
	}
	m, err := io.CopyN(w, d, n)
	if err != nil {
		return err
	}
	if m != n {
		return fmt.Errorf("only discarded %d of %d bytes", m, n)
	}
	return nil
}

func (d *decoder) discardVect() error {
	sz, err := d.readCompactSize()
	if err != nil {
		return err
	}
	return d.discardBytes(int64(sz))
}

func (d *decoder) readMWTX() ([]byte, bool, error) {
	// src/mweb/mweb_models.h - struct Tx
	// "A convenience wrapper around a possibly-null MWEB transcation."
	// Read the uint8_t is_set of OptionalPtr.
	haveMWTX, err := d.readByte()
	if err != nil {
		return nil, false, err
	}
	if haveMWTX == 0 {
		return nil, true, nil // HogEx - that's all folks
	}

	// src/libmw/include/mw/models/tx/Transaction.h - class Transaction
	// READWRITE(obj.m_kernelOffset); // class BlindingFactor
	// READWRITE(obj.m_stealthOffset); // class BlindingFactor
	// READWRITE(obj.m_body); // class TxBody

	// src/libmw/include/mw/models/crypto/BlindingFactor.h - class BlindingFactor
	// just 32 bytes:
	//  BigInt<32> m_value;
	//  READWRITE(obj.m_value);
	// x2 for both kernel_offset and stealth_offset BlindingFactor
	if err = d.discardBytes(64); err != nil {
		return nil, false, err
	}

	// TxBody
	kern0, err := d.readMWTXBody()
	if err != nil {
		return nil, false, err
	}
	return kern0, false, nil
}

func (d *decoder) readMWTXBody() ([]byte, error) {
	// src/libmw/include/mw/models/tx/TxBody.h - class TxBody
	//  READWRITE(obj.m_inputs, obj.m_outputs, obj.m_kernels);
	//  std::vector<Input> m_inputs;
	//  std::vector<Output> m_outputs;
	//  std::vector<Kernel> m_kernels;

	// inputs
	numIn, err := d.readCompactSize()
	if err != nil {
		return nil, err
	}
	for i := 0; i < int(numIn); i++ {
		// src/libmw/include/mw/models/tx/Input.h - class Input
		// - features uint8_t
		// - outputID mw::Hash - 32 bytes BigInt<32>
		// - commit Commitment - 33 bytes BigInt<SIZE> for SIZE 33
		// - outputPubKey PublicKey - 33 bytes BigInt<33>
		// (if features includes STEALTH_KEY_FEATURE_BIT) input_pubkey PublicKey
		// (if features includes EXTRA_DATA_FEATURE_BIT) extraData vector<uint8_t>
		// - signature Signature - 64 bytes BigInt<SIZE> for SIZE 64
		//
		// enum FeatureBit {
		//     STEALTH_KEY_FEATURE_BIT = 0x01,
		//     EXTRA_DATA_FEATURE_BIT = 0x02
		// };
		feats, err := d.readByte()
		if err != nil {
			return nil, err
		}
		if err = d.discardBytes(32 + 33 + 33); err != nil { // outputID, commitment, outputPubKey
			return nil, err
		}
		if feats&0x1 != 0 { // input pubkey
			if err = d.discardBytes(33); err != nil {
				return nil, err
			}
		}
		if feats&0x2 != 0 { // extraData
			if err = d.discardVect(); err != nil {
				return nil, err
			}
		}
		if err = d.discardBytes(64); err != nil { // sig
			return nil, err
		}
	} // inputs

	// outputs
	numOut, err := d.readCompactSize()
	if err != nil {
		return nil, err
	}
	for i := 0; i < int(numOut); i++ {
		// src/libmw/include/mw/models/tx/Output.h - class Output
		// commit
		// sender pubkey
		// receiver pubkey
		// message OutputMessage --- fuuuuuuuu
		// proof RangeProof
		// signature
		if err = d.discardBytes(33 + 33 + 33); err != nil { // commitment, sender pk, receiver pk
			return nil, err
		}

		// OutputMessage
		feats, err := d.readByte()
		if err != nil {
			return nil, err
		}
		// enum FeatureBit {
		// 	STANDARD_FIELDS_FEATURE_BIT = 0x01,
		// 	EXTRA_DATA_FEATURE_BIT = 0x02
		// };
		if feats&0x1 != 0 { // pubkey | view_tag uint8_t | masked_value uint64_t | nonce 16-bytes
			if err = d.discardBytes(33 + 1 + 8 + 16); err != nil {
				return nil, err
			}
		}
		if feats&0x2 != 0 { // extraData
			if err = d.discardVect(); err != nil {
				return nil, err
			}
		}

		// RangeProof "The proof itself, at most 675 bytes long."
		// std::vector<uint8_t> m_bytes; -- except it's actually used like a [675]byte...
		if err = d.discardBytes(675 + 64); err != nil { // proof + sig
			return nil, err
		}
	} // outputs

	// kernels
	numKerns, err := d.readCompactSize()
	if err != nil {
		return nil, err
	}
	// Capture the first kernel since pure MW txns (no canonical inputs or
	// outputs) that live in the MWEB but are also seen in mempool have their
	// hash computed as blake3_256(kernel0).
	var kern0 []byte
	d.tee = new(bytes.Buffer)
	for i := 0; i < int(numKerns); i++ {
		// src/libmw/include/mw/models/tx/Kernel.h - class Kernel

		// enum FeatureBit {
		//     FEE_FEATURE_BIT = 0x01,
		//     PEGIN_FEATURE_BIT = 0x02,
		//     PEGOUT_FEATURE_BIT = 0x04,
		//     HEIGHT_LOCK_FEATURE_BIT = 0x08,
		//     STEALTH_EXCESS_FEATURE_BIT = 0x10,
		//     EXTRA_DATA_FEATURE_BIT = 0x20,
		//     ALL_FEATURE_BITS = FEE_FEATURE_BIT | PEGIN...
		// };
		feats, err := d.readByte()
		if err != nil {
			return nil, err
		}
		if feats&0x1 != 0 { // fee
			_, err = d.readVLQ() // vlq for amount? in the wire protocol?!?
			if err != nil {
				return nil, err
			}
		}
		if feats&0x2 != 0 { // pegin amt
			_, err = d.readVLQ()
			if err != nil {
				return nil, err
			}
		}
		if feats&0x4 != 0 { // pegouts vector
			sz, err := d.readCompactSize()
			if err != nil {
				return nil, err
			}
			for i := uint64(0); i < sz; i++ {
				_, err = d.readVLQ() // pegout amt
				if err != nil {
					return nil, err
				}
				if err = d.discardVect(); err != nil { // pkScript
					return nil, err
				}
			}
		}
		if feats&0x8 != 0 { // lockHeight
			_, err = d.readVLQ()
			if err != nil {
				return nil, err
			}
		}
		if feats&0x10 != 0 { // stealth_excess pubkey
			if err = d.discardBytes(33); err != nil {
				return nil, err
			}
		}
		if feats&0x20 != 0 { // extraData vector
			if err = d.discardVect(); err != nil {
				return nil, err
			}
		}
		// "excess" commitment and signature
		if err = d.discardBytes(33 + 64); err != nil {
			return nil, err
		}

		if i == 0 {
			kern0 = d.tee.Bytes()
			d.tee = nil
		}
	} // kernels

	return kern0, nil
}

type Tx struct {
	*wire.MsgTx
	IsHogEx bool
	Kern0   []byte
}

func (tx *Tx) TxHash() chainhash.Hash {
	// A pure-MW tx can only be in mempool or the EB, not the canonical block.
	if len(tx.Kern0) > 0 && len(tx.TxIn) == 0 && len(tx.TxOut) == 0 {
		// CTransaction::ComputeHash in src/primitives/transaction.cpp.
		// Fortunately also a 32 byte hash so we can use chainhash.Hash.
		return blake3.Sum256(tx.Kern0)
	}
	return tx.MsgTx.TxHash()
}

// DeserializeTx
func DeserializeTx(r io.Reader) (*Tx, error) {
	// mostly taken from wire/msgtx.go
	msgTx := &wire.MsgTx{}
	dec := newDecoder(r)

	version, err := dec.readUint32()
	if err != nil {
		return nil, err
	}
	// if version != 0 {
	// 	return nil, fmt.Errorf("only tx version 0 supported, got %d", version)
	// }
	msgTx.Version = int32(version)

	count, err := dec.readCompactSize()
	if err != nil {
		return nil, err
	}

	// A count of zero (meaning no TxIn's to the uninitiated) means that the
	// value is a TxFlagMarker, and hence indicates the presence of a flag.
	var flag [1]byte
	if count == 0 {
		// The count varint was in fact the flag marker byte. Next, we need to
		// read the flag value, which is a single byte.
		if _, err = io.ReadFull(r, flag[:]); err != nil {
			return nil, err
		}

		// Flag bits 0 or 3 must be set.
		if flag[0]&0b1001 == 0 {
			return nil, fmt.Errorf("witness tx but flag byte is %x", flag)
		}

		// With the Segregated Witness specific fields decoded, we can
		// now read in the actual txin count.
		count, err = dec.readCompactSize()
		if err != nil {
			return nil, err
		}
	}

	if count > maxTxInPerMessage {
		return nil, fmt.Errorf("too many transaction inputs to fit into "+
			"max message size [count %d, max %d]", count, maxTxInPerMessage)
	}

	msgTx.TxIn = make([]*wire.TxIn, count)
	for i := range msgTx.TxIn {
		txIn := &wire.TxIn{}
		err = dec.readTxIn(txIn)
		if err != nil {
			return nil, err
		}
		msgTx.TxIn[i] = txIn
	}

	count, err = dec.readCompactSize()
	if err != nil {
		return nil, err
	}
	if count > maxTxOutPerMessage {
		return nil, fmt.Errorf("too many transactions outputs to fit into "+
			"max message size [count %d, max %d]", count, maxTxOutPerMessage)
	}

	msgTx.TxOut = make([]*wire.TxOut, count)
	for i := range msgTx.TxOut {
		txOut := &wire.TxOut{}
		err = dec.readTxOut(txOut)
		if err != nil {
			return nil, err
		}
		msgTx.TxOut[i] = txOut
	}

	if flag[0]&0x01 != 0 {
		for _, txIn := range msgTx.TxIn {
			witCount, err := dec.readCompactSize()
			if err != nil {
				return nil, err
			}
			if witCount > maxWitnessItemsPerInput {
				return nil, fmt.Errorf("too many witness items: %d > max %d",
					witCount, maxWitnessItemsPerInput)
			}
			txIn.Witness = make(wire.TxWitness, witCount)
			for j := range txIn.Witness {
				txIn.Witness[j], err = wire.ReadVarBytes(r, pver,
					maxWitnessItemSize, "script witness item")
				if err != nil {
					return nil, err
				}
			}
		}
	}

	// check for a MW tx based on flag 0x08
	// src/primitives/transaction.h - class CTransaction
	// Serialized like normal tx except with an optional MWEB::Tx after outputs
	// and before locktime.
	var kern0 []byte
	var isHogEx bool
	if flag[0]&0x08 != 0 {
		kern0, isHogEx, err = dec.readMWTX()
		if err != nil {
			return nil, err
		}
		if isHogEx && len(msgTx.TxOut) == 0 {
			return nil, errors.New("no outputs on HogEx txn")
		}
	}

	msgTx.LockTime, err = dec.readUint32()
	if err != nil {
		return nil, err
	}

	return &Tx{msgTx, isHogEx, kern0}, nil
}
