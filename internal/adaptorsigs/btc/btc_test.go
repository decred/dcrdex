package btc

import (
	"bytes"
	"testing"

	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcd/btcec/v2/schnorr"
	"github.com/btcsuite/btcd/txscript"
	"github.com/btcsuite/btcd/wire"
)

// TestScriptInputValidation covers the input-validation paths in
// LockLeafScript and PunishLeafScript.
func TestScriptInputValidation(t *testing.T) {
	validKey := make([]byte, 32)
	shortKey := make([]byte, 31)

	if _, err := LockLeafScript(shortKey, validKey); err == nil {
		t.Fatal("expected error for short kal")
	}
	if _, err := LockLeafScript(validKey, shortKey); err == nil {
		t.Fatal("expected error for short kaf")
	}

	if _, err := PunishLeafScript(shortKey, 10); err == nil {
		t.Fatal("expected error for short kaf in punish script")
	}
	if _, err := PunishLeafScript(validKey, 0); err == nil {
		t.Fatal("expected error for zero lockBlocks")
	}
	if _, err := PunishLeafScript(validKey, -1); err == nil {
		t.Fatal("expected error for negative lockBlocks")
	}
	// CSV block-range is 16 bits.
	if _, err := PunishLeafScript(validKey, 0x10000); err == nil {
		t.Fatal("expected error for lockBlocks > 0xFFFF")
	}
}

// TestUnspendableInternalKey checks that the NUMS point parses as a
// valid BIP-340 x-only pubkey.
func TestUnspendableInternalKey(t *testing.T) {
	k, err := UnspendableInternalKey()
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if k == nil || !k.IsOnCurve() {
		t.Fatal("internal key not on curve")
	}
	got := schnorr.SerializePubKey(k)
	if !bytes.Equal(got, numsInternalKeyX[:]) {
		t.Fatalf("round-trip mismatch: got %x want %x", got, numsInternalKeyX[:])
	}
}

// TestLockTxOutputSpend builds a lockTx output, funds it from a prior
// synthetic tx, spends it through the 2-of-2 tap leaf, and validates
// the witness via btcd's script engine.
func TestLockTxOutputSpend(t *testing.T) {
	alice, err := btcec.NewPrivateKey()
	if err != nil {
		t.Fatalf("alice key: %v", err)
	}
	bob, err := btcec.NewPrivateKey()
	if err != nil {
		t.Fatalf("bob key: %v", err)
	}
	kal := schnorr.SerializePubKey(bob.PubKey()) // initiator (BTC-holder)
	kaf := schnorr.SerializePubKey(alice.PubKey())

	lock, err := NewLockTxOutput(kal, kaf)
	if err != nil {
		t.Fatalf("lock output: %v", err)
	}

	const value = int64(100_000)
	fundTx := wire.NewMsgTx(wire.TxVersion)
	fundTx.AddTxIn(&wire.TxIn{}) // dummy coinbase-ish input
	fundTx.AddTxOut(&wire.TxOut{Value: value, PkScript: lock.PkScript})
	fundHash := fundTx.TxHash()

	spendTx := wire.NewMsgTx(2)
	spendTx.AddTxIn(&wire.TxIn{
		PreviousOutPoint: wire.OutPoint{Hash: fundHash, Index: 0},
	})
	dummyDest := []byte{txscript.OP_TRUE}
	spendTx.AddTxOut(&wire.TxOut{Value: value - 1000, PkScript: dummyDest})

	prevFetcher := txscript.NewCannedPrevOutputFetcher(lock.PkScript, value)
	sigHashes := txscript.NewTxSigHashes(spendTx, prevFetcher)

	kalSig, err := txscript.RawTxInTapscriptSignature(
		spendTx, sigHashes, 0, value, lock.PkScript,
		txscript.NewBaseTapLeaf(lock.LeafScript),
		txscript.SigHashDefault, bob,
	)
	if err != nil {
		t.Fatalf("bob sign: %v", err)
	}
	kafSig, err := txscript.RawTxInTapscriptSignature(
		spendTx, sigHashes, 0, value, lock.PkScript,
		txscript.NewBaseTapLeaf(lock.LeafScript),
		txscript.SigHashDefault, alice,
	)
	if err != nil {
		t.Fatalf("alice sign: %v", err)
	}

	ctrlSer, err := lock.ControlBlock.ToBytes()
	if err != nil {
		t.Fatalf("control block: %v", err)
	}
	// Witness stack for <kal> CHECKSIGVERIFY <kaf> CHECKSIG:
	// bottom -> top: sig_kaf, sig_kal, script, control_block.
	spendTx.TxIn[0].Witness = wire.TxWitness{
		kafSig, kalSig, lock.LeafScript, ctrlSer,
	}

	// Verify the witness against the taproot output.
	if err := runScriptEngine(spendTx, 0, lock.PkScript, value, prevFetcher); err != nil {
		t.Fatalf("lock spend engine: %v", err)
	}
}

// TestRefundTxOutputCoopSpend exercises the cooperative-refund branch
// of the refundTx output: two signatures, same 2-of-2 tapscript as the
// lockTx leaf.
func TestRefundTxOutputCoopSpend(t *testing.T) {
	alice, err := btcec.NewPrivateKey()
	if err != nil {
		t.Fatalf("alice key: %v", err)
	}
	bob, err := btcec.NewPrivateKey()
	if err != nil {
		t.Fatalf("bob key: %v", err)
	}
	kal := schnorr.SerializePubKey(bob.PubKey())
	kaf := schnorr.SerializePubKey(alice.PubKey())

	const lockBlocks = int64(2)
	refund, err := NewRefundTxOutput(kal, kaf, lockBlocks)
	if err != nil {
		t.Fatalf("refund output: %v", err)
	}

	const value = int64(100_000)
	fundTx := wire.NewMsgTx(wire.TxVersion)
	fundTx.AddTxIn(&wire.TxIn{})
	fundTx.AddTxOut(&wire.TxOut{Value: value, PkScript: refund.PkScript})
	fundHash := fundTx.TxHash()

	spendTx := wire.NewMsgTx(2)
	spendTx.AddTxIn(&wire.TxIn{
		PreviousOutPoint: wire.OutPoint{Hash: fundHash, Index: 0},
	})
	spendTx.AddTxOut(&wire.TxOut{Value: value - 1000, PkScript: []byte{txscript.OP_TRUE}})

	prevFetcher := txscript.NewCannedPrevOutputFetcher(refund.PkScript, value)
	sigHashes := txscript.NewTxSigHashes(spendTx, prevFetcher)

	coopLeaf := txscript.NewBaseTapLeaf(refund.CoopLeafScript)
	kalSig, err := txscript.RawTxInTapscriptSignature(
		spendTx, sigHashes, 0, value, refund.PkScript,
		coopLeaf, txscript.SigHashDefault, bob,
	)
	if err != nil {
		t.Fatalf("bob sign: %v", err)
	}
	kafSig, err := txscript.RawTxInTapscriptSignature(
		spendTx, sigHashes, 0, value, refund.PkScript,
		coopLeaf, txscript.SigHashDefault, alice,
	)
	if err != nil {
		t.Fatalf("alice sign: %v", err)
	}

	ctrlSer, err := refund.CoopControlBlock.ToBytes()
	if err != nil {
		t.Fatalf("control block: %v", err)
	}
	spendTx.TxIn[0].Witness = wire.TxWitness{
		kafSig, kalSig, refund.CoopLeafScript, ctrlSer,
	}

	if err := runScriptEngine(spendTx, 0, refund.PkScript, value, prevFetcher); err != nil {
		t.Fatalf("refund coop spend engine: %v", err)
	}
}

// TestRefundTxOutputPunishSpend exercises the punish branch of the
// refundTx output: Alice alone spends after the CSV locktime.
func TestRefundTxOutputPunishSpend(t *testing.T) {
	alice, err := btcec.NewPrivateKey()
	if err != nil {
		t.Fatalf("alice key: %v", err)
	}
	bob, err := btcec.NewPrivateKey()
	if err != nil {
		t.Fatalf("bob key: %v", err)
	}
	kal := schnorr.SerializePubKey(bob.PubKey())
	kaf := schnorr.SerializePubKey(alice.PubKey())

	const lockBlocks = int64(2)
	refund, err := NewRefundTxOutput(kal, kaf, lockBlocks)
	if err != nil {
		t.Fatalf("refund output: %v", err)
	}

	const value = int64(100_000)
	// The funding tx must be old enough for CSV to mature. The script
	// engine checks relative locktime against the fund-tx's inclusion
	// age; we pass the matured age via prevFetcher? Actually, btcd's
	// CSV check in script execution compares the input sequence to
	// the CSV operand and separately requires tx version >= 2 and the
	// input's nSequence to not have the DISABLE bit set. Testing the
	// script-level semantics here is sufficient; consensus-level block
	// maturity is enforced by the block template, not by the script
	// engine.
	fundTx := wire.NewMsgTx(wire.TxVersion)
	fundTx.AddTxIn(&wire.TxIn{})
	fundTx.AddTxOut(&wire.TxOut{Value: value, PkScript: refund.PkScript})
	fundHash := fundTx.TxHash()

	spendTx := wire.NewMsgTx(2)
	// Set sequence to satisfy CSV. LockBlocks blocks; low bits encode
	// the block count with DISABLE bit clear and type-flag 0 (blocks).
	spendTx.AddTxIn(&wire.TxIn{
		PreviousOutPoint: wire.OutPoint{Hash: fundHash, Index: 0},
		Sequence:         uint32(lockBlocks),
	})
	spendTx.AddTxOut(&wire.TxOut{Value: value - 1000, PkScript: []byte{txscript.OP_TRUE}})

	prevFetcher := txscript.NewCannedPrevOutputFetcher(refund.PkScript, value)
	sigHashes := txscript.NewTxSigHashes(spendTx, prevFetcher)

	punishLeaf := txscript.NewBaseTapLeaf(refund.PunishLeafScript)
	kafSig, err := txscript.RawTxInTapscriptSignature(
		spendTx, sigHashes, 0, value, refund.PkScript,
		punishLeaf, txscript.SigHashDefault, alice,
	)
	if err != nil {
		t.Fatalf("alice sign: %v", err)
	}

	ctrlSer, err := refund.PunishControlBlock.ToBytes()
	if err != nil {
		t.Fatalf("control block: %v", err)
	}
	// Punish witness: sig, script, control_block. Script pops just one
	// sig (only CHECKSIG on kaf).
	spendTx.TxIn[0].Witness = wire.TxWitness{
		kafSig, refund.PunishLeafScript, ctrlSer,
	}

	if err := runScriptEngine(spendTx, 0, refund.PkScript, value, prevFetcher); err != nil {
		t.Fatalf("refund punish spend engine: %v", err)
	}
}

// runScriptEngine executes the txscript engine against the spend at idx
// and reports script-level validation errors.
func runScriptEngine(tx *wire.MsgTx, idx int, pkScript []byte, value int64,
	prev txscript.PrevOutputFetcher) error {

	flags := txscript.ScriptBip16 | txscript.ScriptVerifyTaproot |
		txscript.ScriptVerifyWitness |
		txscript.ScriptVerifyCheckLockTimeVerify |
		txscript.ScriptVerifyCheckSequenceVerify
	engine, err := txscript.NewEngine(pkScript, tx, idx, flags, nil,
		txscript.NewTxSigHashes(tx, prev), value, prev)
	if err != nil {
		return err
	}
	return engine.Execute()
}
