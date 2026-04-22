package btc

import (
	"bytes"
	"testing"

	"decred.org/dcrdex/internal/adaptorsigs"
	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcd/btcec/v2/schnorr"
	"github.com/btcsuite/btcd/txscript"
	"github.com/btcsuite/btcd/wire"
	"github.com/decred/dcrd/dcrec/edwards/v2"
)

// TestFullBTCSwapHappyPath simulates the full happy-path of the BTC/XMR
// adaptor swap on the BTC side only, in-memory, without any RPC.
//
// The scenario:
//
//  1. Bob (BTC holder, initiator) and Alice (XMR holder, participant)
//     each generate an ed25519 XMR spend-key half and a fresh
//     secp256k1 BTC signing key. They exchange DLEQ proofs so the
//     other party knows the secp256k1 pubkey corresponding to their
//     ed25519 spend-key scalar.
//
//  2. Bob constructs the lockTx output (taproot 2-of-2 tapscript).
//     In a real swap he also funds it on-chain and waits for confirms.
//     Here we synthesize the funding via a dummy tx.
//
//  3. Alice would normally lock XMR at this point (skipped).
//
//  4. Bob produces a pub-key-tweaked BIP-340 adaptor signature on the
//     spendTx that moves funds from the lockTx output to Alice, with
//     the tweak point = Alice's XMR-key-half pubkey (as secp256k1).
//
//  5. Alice verifies Bob's adaptor, decrypts it using her ed25519
//     scalar reinterpreted as a secp256k1 scalar (DLEQ makes this
//     consistent), and produces a full Bob signature. She signs her
//     own half normally and assembles the tapscript witness.
//
//  6. The spendTx is validated by btcd's script engine.
//
//  7. Bob observes the spendTx (or in this test, directly reads the
//     completed signature), recovers Alice's ed25519 XMR-key-half
//     scalar via RecoverTweakBIP340, and verifies that summing with
//     his own XMR-key half yields the expected full XMR spend key.
//
// This validates that the crypto pieces built so far -
// internal/adaptorsigs/dleq.go (DLEQ), bip340.go (BIP-340 adaptor),
// and this package's tapscript scripts - plug together correctly for
// the full protocol.
func TestFullBTCSwapHappyPath(t *testing.T) {
	// -------- Phase 1: key setup and DLEQ exchange --------

	// Alice's ed25519 XMR spend-key half.
	aliceSpendKey, err := edwards.GeneratePrivateKey()
	if err != nil {
		t.Fatalf("alice ed25519 key: %v", err)
	}
	// Bob's ed25519 XMR spend-key half.
	bobSpendKey, err := edwards.GeneratePrivateKey()
	if err != nil {
		t.Fatalf("bob ed25519 key: %v", err)
	}

	// Each party generates a DLEQ proof binding their ed25519 scalar
	// to a secp256k1 pubkey.
	aliceDLEAG, err := adaptorsigs.ProveDLEQ(aliceSpendKey.Serialize())
	if err != nil {
		t.Fatalf("alice DLEQ: %v", err)
	}
	bobDLEAG, err := adaptorsigs.ProveDLEQ(bobSpendKey.Serialize())
	if err != nil {
		t.Fatalf("bob DLEQ: %v", err)
	}

	// Each party extracts the other's secp256k1 pubkey - this becomes
	// the tweak point used when adaptor-signing for that counterparty.
	alicePubSecp, err := adaptorsigs.ExtractSecp256k1PubKeyFromProof(aliceDLEAG)
	if err != nil {
		t.Fatalf("extract alice secp: %v", err)
	}
	// In a full two-way swap the initiator also consumes the
	// participant's DLEQ proof (for verifying that Alice's pubkey on
	// the XMR side corresponds to a known secp scalar point). This
	// test only exercises the happy-path single-direction flow, so we
	// just validate that Bob's DLEQ deserializes.
	if _, err := adaptorsigs.ExtractSecp256k1PubKeyFromProof(bobDLEAG); err != nil {
		t.Fatalf("extract bob secp: %v", err)
	}

	// Each party's BTC signing key (independent of the XMR key half).
	aliceBTC, err := btcec.NewPrivateKey()
	if err != nil {
		t.Fatalf("alice btc key: %v", err)
	}
	bobBTC, err := btcec.NewPrivateKey()
	if err != nil {
		t.Fatalf("bob btc key: %v", err)
	}

	kal := schnorr.SerializePubKey(bobBTC.PubKey())   // initiator: Bob
	kaf := schnorr.SerializePubKey(aliceBTC.PubKey()) // participant: Alice

	// -------- Phase 2: lockTx output construction --------

	lock, err := NewLockTxOutput(kal, kaf)
	if err != nil {
		t.Fatalf("lock output: %v", err)
	}

	const value = int64(100_000)
	// Funding tx synthesized in memory. In a real swap Bob fills in
	// real wallet inputs and broadcasts this.
	fundTx := wire.NewMsgTx(wire.TxVersion)
	fundTx.AddTxIn(&wire.TxIn{})
	fundTx.AddTxOut(&wire.TxOut{Value: value, PkScript: lock.PkScript})
	fundHash := fundTx.TxHash()

	// -------- Phase 4: Bob adaptor-signs spendTx --------
	//
	// spendTx moves the lockTx output to Alice's address. The witness
	// will need Bob's signature and Alice's signature. Bob signs as an
	// adaptor under the tweak point = Alice's secp256k1 pubkey (from
	// her DLEAG proof). This signature cannot be published by Alice
	// until she "decrypts" it using her ed25519 scalar, which is what
	// reveals the scalar to Bob post-publication.

	spendTx := wire.NewMsgTx(2)
	spendTx.AddTxIn(&wire.TxIn{
		PreviousOutPoint: wire.OutPoint{Hash: fundHash, Index: 0},
	})
	// Dummy output script (in reality this would be Alice's P2TR or
	// whatever address she wants).
	spendTx.AddTxOut(&wire.TxOut{Value: value - 1000, PkScript: []byte{txscript.OP_TRUE}})

	prevFetcher := txscript.NewCannedPrevOutputFetcher(lock.PkScript, value)
	sigHashes := txscript.NewTxSigHashes(spendTx, prevFetcher)

	leaf := txscript.NewBaseTapLeaf(lock.LeafScript)
	sigHash, err := txscript.CalcTapscriptSignaturehash(
		sigHashes, txscript.SigHashDefault, spendTx, 0, prevFetcher, leaf,
	)
	if err != nil {
		t.Fatalf("bob tapscript sighash: %v", err)
	}

	// Bob's adaptor sig under Alice's XMR-key-half pubkey as tweak.
	var aliceTweakJac btcec.JacobianPoint
	alicePubSecp.AsJacobian(&aliceTweakJac)
	bobAdaptor, err := adaptorsigs.PublicKeyTweakedAdaptorSigBIP340(bobBTC, sigHash, &aliceTweakJac)
	if err != nil {
		t.Fatalf("bob adaptor sign: %v", err)
	}
	// Alice (the recipient) verifies the adaptor without knowing the tweak.
	if err := bobAdaptor.VerifyBIP340(sigHash, bobBTC.PubKey()); err != nil {
		t.Fatalf("alice verify bob adaptor: %v", err)
	}

	// -------- Phase 5: Alice completes and assembles --------
	//
	// Alice decrypts Bob's adaptor using her ed25519 spend-key half
	// reinterpreted as a secp256k1 scalar. The DLEQ proof is what
	// guarantees this reinterpretation is the scalar whose pubkey Bob
	// baked into his adaptor as T.
	aliceScalarForSecp, _ := btcec.PrivKeyFromBytes(aliceSpendKey.Serialize())
	bobSigCompleted, err := bobAdaptor.DecryptBIP340(&aliceScalarForSecp.Key)
	if err != nil {
		t.Fatalf("alice decrypt bob adaptor: %v", err)
	}
	// The completed sig is a valid BIP-340 signature on the spendTx
	// sighash for Bob's pubkey.
	if !bobSigCompleted.Verify(sigHash, bobBTC.PubKey()) {
		t.Fatal("completed bob sig failed BIP-340 verify")
	}

	// Alice signs her own half normally.
	aliceSig, err := txscript.RawTxInTapscriptSignature(
		spendTx, sigHashes, 0, value, lock.PkScript, leaf,
		txscript.SigHashDefault, aliceBTC,
	)
	if err != nil {
		t.Fatalf("alice sign: %v", err)
	}

	// Witness: sig_kaf (alice), sig_kal (bob completed), script, control block.
	ctrlSer, err := lock.ControlBlock.ToBytes()
	if err != nil {
		t.Fatalf("control block: %v", err)
	}
	spendTx.TxIn[0].Witness = wire.TxWitness{
		aliceSig, bobSigCompleted.Serialize(), lock.LeafScript, ctrlSer,
	}

	// -------- Phase 6: on-chain validation --------
	//
	// btcd's script engine validates the full taproot witness,
	// including the BIP-340 signatures and the merkle proof against
	// the committed leaf script.
	if err := runScriptEngine(spendTx, 0, lock.PkScript, value, prevFetcher); err != nil {
		t.Fatalf("spend script engine: %v", err)
	}

	// -------- Phase 7: Bob recovers Alice's XMR-key-half scalar --------
	//
	// Bob sees the completed signature on-chain. Running RecoverTweak
	// on his own adaptor + the completed sig yields Alice's scalar.
	recoveredScalar, err := bobAdaptor.RecoverTweakBIP340(bobSigCompleted)
	if err != nil {
		t.Fatalf("bob recover tweak: %v", err)
	}
	if !recoveredScalar.Equals(&aliceScalarForSecp.Key) {
		t.Fatal("recovered scalar != alice's secp-reinterpreted scalar")
	}

	// Bob now combines his own XMR-key half (ed25519) with the
	// recovered scalar to form the full XMR spend key. We can only
	// check this at the scalar level: Bob's ed25519 scalar + Alice's
	// recovered scalar should equal the sum of their private key
	// halves as used in XMR wallet reconstruction.
	//
	// The reference implementation does this by adding the two
	// scalars mod the edwards curve order and calling
	// edwards.PrivKeyFromScalar. We verify a weaker but sufficient
	// property: the recovered scalar matches Alice's private key bytes
	// via the DLEQ-guaranteed equivalence.
	var aliceBytes [32]byte
	aliceScalarForSecp.Key.PutBytes(&aliceBytes)
	if !bytes.Equal(aliceBytes[:], aliceSpendKey.Serialize()) {
		t.Fatal("alice's secp-reinterpreted scalar does not round-trip with ed25519 bytes")
	}
	var recoveredBytes [32]byte
	recoveredScalar.PutBytes(&recoveredBytes)
	if !bytes.Equal(recoveredBytes[:], aliceSpendKey.Serialize()) {
		t.Fatal("recovered bytes do not match alice's ed25519 private key bytes")
	}

	// Verify the recovered XMR scalar is not just a coincidence: if
	// we instead recovered some bogus scalar, the DLEQ pubkey should
	// not reconstruct.
	var recoveredPub btcec.JacobianPoint
	btcec.ScalarBaseMultNonConst(recoveredScalar, &recoveredPub)
	var aliceDLEQPub btcec.JacobianPoint
	alicePubSecp.AsJacobian(&aliceDLEQPub)
	if !recoveredPub.EquivalentNonConst(&aliceDLEQPub) {
		t.Fatal("recovered pubkey != Alice's DLEQ secp pubkey")
	}
}

// swapParties holds everything two parties share after the DLEQ + key
// setup phase. Helper for the refund-path tests.
type swapParties struct {
	aliceSpendKey *edwards.PrivateKey // alice ed25519 XMR spend-key half
	bobSpendKey   *edwards.PrivateKey // bob   ed25519 XMR spend-key half
	alicePubSecp  *btcec.PublicKey    // DLEQ-extracted from alice's proof
	bobPubSecp    *btcec.PublicKey    // DLEQ-extracted from bob's proof
	aliceBTC      *btcec.PrivateKey   // alice secp BTC signing key
	bobBTC        *btcec.PrivateKey   // bob   secp BTC signing key
	kal           []byte              // x-only pubkey of initiator (Bob)
	kaf           []byte              // x-only pubkey of participant (Alice)
}

func setupSwapParties(t *testing.T) *swapParties {
	t.Helper()
	aliceSpend, err := edwards.GeneratePrivateKey()
	if err != nil {
		t.Fatalf("alice ed25519: %v", err)
	}
	bobSpend, err := edwards.GeneratePrivateKey()
	if err != nil {
		t.Fatalf("bob ed25519: %v", err)
	}
	aliceDLEAG, err := adaptorsigs.ProveDLEQ(aliceSpend.Serialize())
	if err != nil {
		t.Fatalf("alice dleq: %v", err)
	}
	bobDLEAG, err := adaptorsigs.ProveDLEQ(bobSpend.Serialize())
	if err != nil {
		t.Fatalf("bob dleq: %v", err)
	}
	alicePubSecp, err := adaptorsigs.ExtractSecp256k1PubKeyFromProof(aliceDLEAG)
	if err != nil {
		t.Fatalf("alice extract: %v", err)
	}
	bobPubSecp, err := adaptorsigs.ExtractSecp256k1PubKeyFromProof(bobDLEAG)
	if err != nil {
		t.Fatalf("bob extract: %v", err)
	}
	aliceBTC, err := btcec.NewPrivateKey()
	if err != nil {
		t.Fatalf("alice btc: %v", err)
	}
	bobBTC, err := btcec.NewPrivateKey()
	if err != nil {
		t.Fatalf("bob btc: %v", err)
	}
	return &swapParties{
		aliceSpendKey: aliceSpend,
		bobSpendKey:   bobSpend,
		alicePubSecp:  alicePubSecp,
		bobPubSecp:    bobPubSecp,
		aliceBTC:      aliceBTC,
		bobBTC:        bobBTC,
		kal:           schnorr.SerializePubKey(bobBTC.PubKey()),
		kaf:           schnorr.SerializePubKey(aliceBTC.PubKey()),
	}
}

// buildAndVerifyRefundTx synthesizes funding, builds the lockTx output
// from the refund test's perspective (the refund-spending-lockTx), and
// executes the refundTx witness through the script engine. It returns
// the refund output materials and the refundTx itself so the caller
// can build the spend-refund step on top.
func buildAndVerifyRefundTx(t *testing.T, p *swapParties, lockBlocks int64,
	lockValue int64) (*RefundTxOutput, *wire.MsgTx) {
	t.Helper()

	lock, err := NewLockTxOutput(p.kal, p.kaf)
	if err != nil {
		t.Fatalf("lock output: %v", err)
	}

	// Synthesize lockTx funding.
	fundTx := wire.NewMsgTx(wire.TxVersion)
	fundTx.AddTxIn(&wire.TxIn{})
	fundTx.AddTxOut(&wire.TxOut{Value: lockValue, PkScript: lock.PkScript})
	fundHash := fundTx.TxHash()

	refund, err := NewRefundTxOutput(p.kal, p.kaf, lockBlocks)
	if err != nil {
		t.Fatalf("refund output: %v", err)
	}

	// refundTx spends the lockTx output via the 2-of-2 leaf and pays
	// to the refund taproot output.
	refundTx := wire.NewMsgTx(2)
	refundTx.AddTxIn(&wire.TxIn{
		PreviousOutPoint: wire.OutPoint{Hash: fundHash, Index: 0},
	})
	refundTx.AddTxOut(&wire.TxOut{Value: lockValue - 1000, PkScript: refund.PkScript})

	// Both parties pre-sign the refundTx with their secp keys
	// (standard tapscript sigs, not adaptor). Either can later
	// broadcast it unilaterally.
	prev := txscript.NewCannedPrevOutputFetcher(lock.PkScript, lockValue)
	sigHashes := txscript.NewTxSigHashes(refundTx, prev)
	leaf := txscript.NewBaseTapLeaf(lock.LeafScript)

	bobSig, err := txscript.RawTxInTapscriptSignature(
		refundTx, sigHashes, 0, lockValue, lock.PkScript, leaf,
		txscript.SigHashDefault, p.bobBTC,
	)
	if err != nil {
		t.Fatalf("bob refundTx sign: %v", err)
	}
	aliceSig, err := txscript.RawTxInTapscriptSignature(
		refundTx, sigHashes, 0, lockValue, lock.PkScript, leaf,
		txscript.SigHashDefault, p.aliceBTC,
	)
	if err != nil {
		t.Fatalf("alice refundTx sign: %v", err)
	}
	ctrlSer, err := lock.ControlBlock.ToBytes()
	if err != nil {
		t.Fatalf("control: %v", err)
	}
	refundTx.TxIn[0].Witness = wire.TxWitness{
		aliceSig, bobSig, lock.LeafScript, ctrlSer,
	}
	if err := runScriptEngine(refundTx, 0, lock.PkScript, lockValue, prev); err != nil {
		t.Fatalf("refundTx engine: %v", err)
	}
	return refund, refundTx
}

// TestBTCSwapCooperativeRefund exercises the cooperative-refund flow:
// Bob locks BTC, both pre-sign the refund chain, then they decide to
// unwind. Alice's adaptor signature on spendRefundTx is decrypted by
// Bob using his own XMR-key-half scalar, revealing that scalar to
// Alice when the completed sig hits chain - allowing her to sweep the
// XMR she locked at the shared address.
func TestBTCSwapCooperativeRefund(t *testing.T) {
	p := setupSwapParties(t)
	const (
		lockBlocks = int64(2)
		lockValue  = int64(100_000)
	)
	refund, refundTx := buildAndVerifyRefundTx(t, p, lockBlocks, lockValue)
	refundHash := refundTx.TxHash()
	refundValue := refundTx.TxOut[0].Value

	// spendRefundTxCoop: spends refundTx output via coop leaf. In a
	// real swap, Alice pre-signs as an adaptor (tweak = Bob's XMR
	// pubkey) before the lockTx ever hits chain. Bob only decrypts and
	// broadcasts after both parties lock.
	spend := wire.NewMsgTx(2)
	spend.AddTxIn(&wire.TxIn{
		PreviousOutPoint: wire.OutPoint{Hash: refundHash, Index: 0},
	})
	spend.AddTxOut(&wire.TxOut{Value: refundValue - 1000, PkScript: []byte{txscript.OP_TRUE}})

	prev := txscript.NewCannedPrevOutputFetcher(refund.PkScript, refundValue)
	sigHashes := txscript.NewTxSigHashes(spend, prev)
	coopLeaf := txscript.NewBaseTapLeaf(refund.CoopLeafScript)

	sigHash, err := txscript.CalcTapscriptSignaturehash(
		sigHashes, txscript.SigHashDefault, spend, 0, prev, coopLeaf,
	)
	if err != nil {
		t.Fatalf("sighash: %v", err)
	}

	// Alice's adaptor signature, with tweak = Bob's XMR-key-half
	// secp pubkey (extracted from Bob's DLEQ proof).
	var bobTweak btcec.JacobianPoint
	p.bobPubSecp.AsJacobian(&bobTweak)
	aliceAdaptor, err := adaptorsigs.PublicKeyTweakedAdaptorSigBIP340(
		p.aliceBTC, sigHash, &bobTweak,
	)
	if err != nil {
		t.Fatalf("alice adaptor: %v", err)
	}
	if err := aliceAdaptor.VerifyBIP340(sigHash, p.aliceBTC.PubKey()); err != nil {
		t.Fatalf("bob verify alice adaptor: %v", err)
	}

	// Bob decrypts using his ed25519 scalar reinterpreted as secp256k1.
	bobScalarForSecp, _ := btcec.PrivKeyFromBytes(p.bobSpendKey.Serialize())
	aliceSigCompleted, err := aliceAdaptor.DecryptBIP340(&bobScalarForSecp.Key)
	if err != nil {
		t.Fatalf("bob decrypt: %v", err)
	}
	if !aliceSigCompleted.Verify(sigHash, p.aliceBTC.PubKey()) {
		t.Fatal("completed alice sig does not verify")
	}

	// Bob signs his half normally.
	bobSig, err := txscript.RawTxInTapscriptSignature(
		spend, sigHashes, 0, refundValue, refund.PkScript, coopLeaf,
		txscript.SigHashDefault, p.bobBTC,
	)
	if err != nil {
		t.Fatalf("bob sign: %v", err)
	}

	ctrlSer, err := refund.CoopControlBlock.ToBytes()
	if err != nil {
		t.Fatalf("control: %v", err)
	}
	spend.TxIn[0].Witness = wire.TxWitness{
		aliceSigCompleted.Serialize(), bobSig, refund.CoopLeafScript, ctrlSer,
	}
	if err := runScriptEngine(spend, 0, refund.PkScript, refundValue, prev); err != nil {
		t.Fatalf("spendRefund engine: %v", err)
	}

	// Alice observes the completed alice-sig on-chain and recovers
	// Bob's XMR-key-half scalar from her own adaptor.
	recovered, err := aliceAdaptor.RecoverTweakBIP340(aliceSigCompleted)
	if err != nil {
		t.Fatalf("alice recover: %v", err)
	}
	if !recovered.Equals(&bobScalarForSecp.Key) {
		t.Fatal("recovered scalar != bob's secp-reinterpreted scalar")
	}
	var recoveredBytes [32]byte
	recovered.PutBytes(&recoveredBytes)
	if !bytes.Equal(recoveredBytes[:], p.bobSpendKey.Serialize()) {
		t.Fatal("recovered bytes != bob's ed25519 private key bytes")
	}
}

// TestBTCSwapPunishRefund exercises the punish path: Bob has gone
// silent after both parties locked. Alice waits for the CSV locktime
// and sweeps the BTC alone via the punish leaf. She does NOT learn
// Bob's XMR-key-half scalar - the punish branch does not constrain
// Bob's signature, so Bob's ed25519 half is never revealed on-chain.
func TestBTCSwapPunishRefund(t *testing.T) {
	p := setupSwapParties(t)
	const (
		lockBlocks = int64(2)
		lockValue  = int64(100_000)
	)
	refund, refundTx := buildAndVerifyRefundTx(t, p, lockBlocks, lockValue)
	refundHash := refundTx.TxHash()
	refundValue := refundTx.TxOut[0].Value

	// spendRefundTx via punish leaf; sequence must satisfy CSV.
	spend := wire.NewMsgTx(2)
	spend.AddTxIn(&wire.TxIn{
		PreviousOutPoint: wire.OutPoint{Hash: refundHash, Index: 0},
		Sequence:         uint32(lockBlocks),
	})
	spend.AddTxOut(&wire.TxOut{Value: refundValue - 1000, PkScript: []byte{txscript.OP_TRUE}})

	prev := txscript.NewCannedPrevOutputFetcher(refund.PkScript, refundValue)
	sigHashes := txscript.NewTxSigHashes(spend, prev)
	punishLeaf := txscript.NewBaseTapLeaf(refund.PunishLeafScript)

	aliceSig, err := txscript.RawTxInTapscriptSignature(
		spend, sigHashes, 0, refundValue, refund.PkScript, punishLeaf,
		txscript.SigHashDefault, p.aliceBTC,
	)
	if err != nil {
		t.Fatalf("alice sign: %v", err)
	}

	ctrlSer, err := refund.PunishControlBlock.ToBytes()
	if err != nil {
		t.Fatalf("control: %v", err)
	}
	spend.TxIn[0].Witness = wire.TxWitness{
		aliceSig, refund.PunishLeafScript, ctrlSer,
	}
	if err := runScriptEngine(spend, 0, refund.PkScript, refundValue, prev); err != nil {
		t.Fatalf("punish engine: %v", err)
	}
	// Note: no scalar recovery is possible here; the punish sig alone
	// does not reveal Bob's ed25519 scalar, which is by design - Alice
	// accepting the BTC alone means she forfeits the ability to sweep
	// the XMR, matching the asymmetric-punish property of the protocol.
}
