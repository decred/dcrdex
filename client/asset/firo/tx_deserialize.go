package firo

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"

	"github.com/btcsuite/btcd/wire"
)

var byteOrder = binary.LittleEndian

const (
	// protocol version 0
	pver uint32 = 0
	// wire.maxTxInPerMessage
	maxTxInPerMessage = wire.MaxMessagePayload/41 + 1
	// wire.maxTxOutPerMessage
	maxTxOutPerMessage = wire.MaxMessagePayload/wire.MinTxOutPayload + 1
)

type TxType int

const (
	// Transaction types
	TransactionNormal                  = 0
	TransactionProviderRegister        = 1
	TransactionProviderUpdateService   = 2
	TransactionProviderUpdateRegistrar = 3
	TransactionProviderUpdateRevoke    = 4
	TransactionCoinbase                = 5
	TransactionQuorumCommitment        = 6
	TransactionSpork                   = 7
	TransactionLelantus                = 8
	TransactionSpark                   = 9
	// TRANSACTION_ALIAS is a regular spark spend transaction, but contains
	// additional info about a created/modified spark name.
	TransactionAlias = 10
)

type decoder struct {
	buf [8]byte
	rd  io.Reader
	tee *bytes.Buffer // anything read from rd is Written to tee
}

func newDecoder(r io.Reader) *decoder {
	return &decoder{rd: r}
}

func (d *decoder) Read(b []byte) (n int, err error) {
	n, err = d.rd.Read(b)
	if err != nil {
		return 0, err
	}
	if d.tee != nil {
		d.tee.Write(b)
	}
	return n, nil
}

func (d *decoder) readByte() (byte, error) {
	b := d.buf[:1]
	if _, err := io.ReadFull(d, b); err != nil {
		return 0, err
	}
	return b[0], nil
}

func (d *decoder) readUint16() (uint16, error) {
	b := d.buf[:2]
	if _, err := io.ReadFull(d, b); err != nil {
		return 0, err
	}
	return byteOrder.Uint16(b), nil
}

func (d *decoder) readUint32() (uint32, error) {
	b := d.buf[:4]
	if _, err := io.ReadFull(d, b); err != nil {
		return 0, err
	}
	return byteOrder.Uint32(b), nil
}

func (d *decoder) readUint64() (uint64, error) {
	b := d.buf[:]
	if _, err := io.ReadFull(d, b); err != nil {
		return 0, err
	}
	return byteOrder.Uint64(b), nil
}

// readOutPoint reads the next sequence of bytes from r as an OutPoint.
func (d *decoder) readOutPoint(op *wire.OutPoint) error {
	_, err := io.ReadFull(d, op.Hash[:])
	if err != nil {
		return err
	}

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

	to.Value = int64(v)
	to.PkScript = pkScript

	return nil
}

// readVarBytes reads the next sequence of bytes from r and returns them.
func (d *decoder) readVarBytes() ([]byte, error) {
	byteArray, err := wire.ReadVarBytes(d, pver, wire.MaxMessagePayload, "extra")
	if err != nil {
		return nil, err
	}
	return byteArray, nil
}

type txn struct {
	msgTx  *wire.MsgTx
	txType TxType
}

// deserializeTransaction deserializes a transaction
func deserializeTransaction(r io.Reader) (*txn, error) {
	tx := txn{
		msgTx:  &wire.MsgTx{},
		txType: 0,
	}

	dec := newDecoder(r)

	fullVersion, err := dec.readUint32()
	if err != nil {
		return nil, err
	}
	ver := fullVersion & 0x0000ffff
	typ := (fullVersion >> 16) & 0x0000ffff

	tx.msgTx.Version = int32(ver)
	tx.txType = TxType(typ)

	switch tx.txType {
	case TransactionNormal,
		TransactionProviderRegister,
		TransactionProviderUpdateService,
		TransactionProviderUpdateRegistrar,
		TransactionProviderUpdateRevoke,
		TransactionCoinbase,
		TransactionLelantus,
		TransactionSpark,
		TransactionAlias:
		{
			deserializeTx(dec, &tx, tx.txType)
		}
	case TransactionQuorumCommitment:
		err = deserializeNonSpendingTx(dec, &tx, tx.txType)
	case TransactionSpork:
		err = deserializeNonSpendingTx(dec, &tx, tx.txType)
	default:
		err = fmt.Errorf("unknown transaction type %d", tx.txType)
	}

	return &tx, err
}

// deserializeTx deserializes a transaction
func deserializeTx(dec *decoder, tx *txn, txType TxType) error {
	count, err := dec.readCompactSize()
	if err != nil {
		return err
	}

	if count == 0 {
		return fmt.Errorf("input count is 0 -- but no segwit transactions for Firo")
	}

	if count > maxTxInPerMessage {
		return fmt.Errorf("too many transaction inputs to fit into "+
			"max message size [count %d, max %d]", count, maxTxInPerMessage)
	}

	tx.msgTx.TxIn = make([]*wire.TxIn, count)
	for i := range tx.msgTx.TxIn {
		txIn := &wire.TxIn{}
		err = dec.readTxIn(txIn)
		if err != nil {
			return err
		}
		tx.msgTx.TxIn[i] = txIn
	}

	count, err = dec.readCompactSize()
	if err != nil {
		return err
	}
	if count > maxTxOutPerMessage {
		return fmt.Errorf("too many transactions outputs to fit into "+
			"max message size [count %d, max %d]", count, maxTxOutPerMessage)
	}

	tx.msgTx.TxOut = make([]*wire.TxOut, count)
	for i := range tx.msgTx.TxOut {
		txOut := &wire.TxOut{}
		err = dec.readTxOut(txOut)
		if err != nil {
			return err
		}
		tx.msgTx.TxOut[i] = txOut
	}

	tx.msgTx.LockTime, err = dec.readUint32()
	if err != nil {
		return err
	}

	if txType == TransactionNormal {
		return nil
	}

	// read vExtraPayload
	_, err = dec.readVarBytes()
	if err != nil {
		return err
	}

	return nil
}

// deserializeNonSpendingTx deserializes spork, quorum txs which have no inputs and
// no outputs
func deserializeNonSpendingTx(dec *decoder, tx *txn, txType TxType) error {
	count, err := dec.readCompactSize()
	if err != nil {
		return err
	}

	if count != 0 {
		return fmt.Errorf("tx (type=%d) expected 0 txins - got %d", txType, count)
	}

	count, err = dec.readCompactSize()
	if err != nil {
		return err
	}

	if count != 0 {
		return fmt.Errorf("tx (type=%d) expected 0 txouts - got %d", txType, count)
	}

	tx.msgTx.LockTime, err = dec.readUint32()
	if err != nil {
		return err
	}

	_, err = dec.readVarBytes()
	if err != nil {
		return err
	}

	return nil
}
