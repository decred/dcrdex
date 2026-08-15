// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package lbc

import (
	"bytes"
	"encoding/binary"
	"testing"
	"time"

	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcd/wire"
)

// build a minimal LBC block (header with ClaimTrie + 1 coinbase-like empty-vin tx)
func fakeLBCBlock() []byte {
	var buf bytes.Buffer
	// header
	_ = binary.Write(&buf, binary.LittleEndian, int32(1)) // version
	prev := chainhash.Hash{}
	merkle := chainhash.Hash{1, 2, 3}
	claim := chainhash.Hash{9, 9, 9}
	buf.Write(prev[:])
	buf.Write(merkle[:])
	buf.Write(claim[:])
	_ = binary.Write(&buf, binary.LittleEndian, uint32(time.Now().Unix()))
	_ = binary.Write(&buf, binary.LittleEndian, uint32(0x1f00ffff))
	_ = binary.Write(&buf, binary.LittleEndian, uint32(42))

	// one transaction
	tx := wire.NewMsgTx(wire.TxVersion)
	tx.AddTxIn(&wire.TxIn{
		PreviousOutPoint: wire.OutPoint{Hash: chainhash.Hash{}, Index: 0xffffffff},
		SignatureScript:  []byte{0x00},
		Sequence:         0xffffffff,
	})
	tx.AddTxOut(&wire.TxOut{Value: 50 * 1e8, PkScript: []byte{0x51}}) // OP_TRUE
	_ = wire.WriteVarInt(&buf, 0, 1)
	_ = tx.Serialize(&buf)
	return buf.Bytes()
}

func TestDeserializeBlock(t *testing.T) {
	raw := fakeLBCBlock()
	if len(raw) < lbcBlockHeaderLen {
		t.Fatalf("test block too short: %d", len(raw))
	}
	msg, err := DeserializeBlock(raw)
	if err != nil {
		t.Fatalf("DeserializeBlock: %v", err)
	}
	if msg.Header.Version != 1 {
		t.Fatalf("version %d", msg.Header.Version)
	}
	if msg.Header.Nonce != 42 {
		t.Fatalf("nonce %d", msg.Header.Nonce)
	}
	if len(msg.Transactions) != 1 {
		t.Fatalf("tx count %d", len(msg.Transactions))
	}
	if msg.Transactions[0].TxOut[0].Value != 50*1e8 {
		t.Fatalf("unexpected value %d", msg.Transactions[0].TxOut[0].Value)
	}
	// PrevBlock should be zero hash
	if !msg.Header.PrevBlock.IsEqual(&chainhash.Hash{}) {
		t.Fatalf("prev block not zero")
	}
}
