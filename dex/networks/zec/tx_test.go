package zec

import (
	"bytes"
	_ "embed"
	"encoding/hex"
	"testing"
)

var (
	//go:embed shielded_sapling_tx.dat
	shieldedSaplingTx []byte
	//go:embed unshielded_sapling_tx.dat
	unshieldedSaplingTx []byte
)

func TestTxDeserialize(t *testing.T) {
	tx, err := DeserializeTx(unshieldedSaplingTx)
	if err != nil {
		t.Fatalf("error decoding tx: %v", err)
	}

	if len(tx.TxIn) != 1 {
		t.Fatalf("expected 1 input, got %d", len(tx.TxIn))
	}

	txIn := tx.TxIn[0]
	if txIn.PreviousOutPoint.Hash.String() != "df0547ac001d335441bd621779e8946a1224949a014871b5f2a349352a270d69" {
		t.Fatal("wrong previous outpoint hash")
	}
	if txIn.PreviousOutPoint.Index != 0 {
		t.Fatal("wrong previous outpoint index")
	}
	if hex.EncodeToString(txIn.SignatureScript) != "47304402201656a4834651f39ac52eb866042ca7ede052ac843a914da4790573122c8e2ab302200af617e856abf4f8fb8d8086825dc63766943b4866ad3d6b4c8f222017c9b402012102d547eb1c5672a4d212de3c797a87b2b8fe731c2b502db6d7ad044850fe11d78f" {
		t.Fatal("wrong signature script")
	}
	if txIn.Sequence != 4294967295 {
		t.Fatalf("wrong sequence")
	}

	if len(tx.TxOut) != 2 {
		t.Fatalf("expected 2 outputs, got %d", len(tx.TxOut))
	}
	txOut := tx.TxOut[1]
	if txOut.Value != 41408718 {
		t.Fatal("wrong value")
	}
	if hex.EncodeToString(txOut.PkScript) != "76a9144ff496917bae33309a8ad70bec81355bbf92988988ac" {
		t.Fatalf("wrong pk script")
	}

	if sz := CalcTxSize(tx.MsgTx); sz != uint64(len(unshieldedSaplingTx)) {
		t.Fatalf("wrong calculated tx size. wanted %d, got %d", len(unshieldedSaplingTx), sz)
	}

	serializedTx, err := tx.Bytes()
	if err != nil {
		t.Fatalf("error re-serializing: %v", err)
	}
	if !bytes.Equal(serializedTx, unshieldedSaplingTx) {
		t.Fatalf("re-encoding does not match original")
	}
}

func TestShieldedTx(t *testing.T) {
	// Just make sure it doesn't error.
	if _, err := DeserializeTx(shieldedSaplingTx); err != nil {
		t.Fatalf("error decoding tx: %v", err)
	}
}
