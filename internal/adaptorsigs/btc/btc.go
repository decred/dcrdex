// BTC tapscript scripts and output-key helpers for the BTC side of a
// BTC/XMR adaptor swap.
//
// The BTC leg locks funds in a taproot output whose internal key is an
// unspendable (NUMS) point, forcing all spends to go through the script
// path. Two taproot outputs are used in the protocol:
//
//   - lockTx output: a single tap leaf with a plain 2-of-2 tapscript
//     (Bob's pubkey CHECKSIGVERIFY, Alice's pubkey CHECKSIG). The
//     spendTx (happy path, Alice redeems) spends through this leaf.
//
//   - refundTx output: a two-leaf script tree. The cooperative-refund
//     leaf is the same 2-of-2 tapscript (Bob's sig there is an adaptor
//     completion that leaks Bob's XMR-key half). The punish leaf is
//     <locktime> CSV DROP <kaf> CHECKSIG, spendable only by Alice after
//     the relative locktime matures.
//
// This package produces the scripts, the taproot output keys, and the
// control blocks needed for witness assembly. Signing, sighash
// computation, and transaction construction are the caller's
// responsibility; btcd's txscript package has all the needed helpers.
package btc

import (
	"errors"
	"fmt"

	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcd/btcec/v2/schnorr"
	"github.com/btcsuite/btcd/txscript"
)

// numsInternalKeyX is the x-coordinate of a secp256k1 point with no
// known discrete log. The value is the canonical BIP-341 NUMS point
// used in the reference test vectors, widely reused across Taproot
// implementations that disable key-path spending.
var numsInternalKeyX = [32]byte{
	0x50, 0x92, 0x9b, 0x74, 0xc1, 0xa0, 0x49, 0x54,
	0xb7, 0x8b, 0x4b, 0x60, 0x35, 0xe9, 0x7a, 0x5e,
	0x07, 0x8a, 0x5a, 0x0f, 0x28, 0xec, 0x96, 0xd5,
	0x47, 0xbf, 0xee, 0x9a, 0xce, 0x80, 0x3a, 0xc0,
}

// UnspendableInternalKey returns the NUMS internal pubkey used for
// taproot outputs whose key path should be unspendable.
func UnspendableInternalKey() (*btcec.PublicKey, error) {
	return schnorr.ParsePubKey(numsInternalKeyX[:])
}

// LockLeafScript builds the tap leaf script that gates the lockTx
// happy-path spend. Spending requires BIP-340 signatures from both kal
// (the initiator, holding BTC) and kaf (the participant, holding XMR).
// Both pubkey arguments must be 32-byte x-only BIP-340 pubkeys.
//
// The kal signature is typically produced as an adaptor by kal under
// kaf's XMR-key-half tweak; kaf completes it on-chain, revealing the
// tweak from which kal recovers kaf's XMR-key half.
func LockLeafScript(kal, kaf []byte) ([]byte, error) {
	if err := checkXOnlyPubKey(kal, "kal"); err != nil {
		return nil, err
	}
	if err := checkXOnlyPubKey(kaf, "kaf"); err != nil {
		return nil, err
	}
	return txscript.NewScriptBuilder().
		AddData(kal).
		AddOp(txscript.OP_CHECKSIGVERIFY).
		AddData(kaf).
		AddOp(txscript.OP_CHECKSIG).
		Script()
}

// PunishLeafScript builds the tap leaf script that gates the
// refundTx's punish path: Alice alone may spend, but only after
// lockBlocks relative blocks have elapsed since the refundTx
// confirmed.
func PunishLeafScript(kaf []byte, lockBlocks int64) ([]byte, error) {
	if err := checkXOnlyPubKey(kaf, "kaf"); err != nil {
		return nil, err
	}
	if lockBlocks <= 0 || lockBlocks > 0xFFFF {
		return nil, fmt.Errorf("lockBlocks out of CSV block-range: %d", lockBlocks)
	}
	return txscript.NewScriptBuilder().
		AddInt64(lockBlocks).
		AddOp(txscript.OP_CHECKSEQUENCEVERIFY).
		AddOp(txscript.OP_DROP).
		AddData(kaf).
		AddOp(txscript.OP_CHECKSIG).
		Script()
}

// LockTxOutput holds the materials needed to fund, spend, and verify
// the lockTx output.
type LockTxOutput struct {
	// OutputKey is the tweaked taproot output key. PkScript is
	// OP_1 <OutputKey serialized x-only>.
	OutputKey *btcec.PublicKey
	// PkScript is the full scriptPubKey for the lockTx output.
	PkScript []byte
	// LeafScript is the 2-of-2 tapscript committed to in the output.
	LeafScript []byte
	// LeafHash is the TapLeaf hash of LeafScript.
	LeafHash [32]byte
	// ControlBlock is the witness control block for spending through
	// the 2-of-2 leaf. Serialized with Serialize() for witness use.
	ControlBlock txscript.ControlBlock
	// InternalKey is the NUMS internal key used in the taproot output.
	InternalKey *btcec.PublicKey
}

// NewLockTxOutput derives the taproot output key, scriptPubKey, tap
// leaf, and control block for the lockTx output.
func NewLockTxOutput(kal, kaf []byte) (*LockTxOutput, error) {
	script, err := LockLeafScript(kal, kaf)
	if err != nil {
		return nil, err
	}
	leaf := txscript.NewBaseTapLeaf(script)
	tree := txscript.AssembleTaprootScriptTree(leaf)

	internalKey, err := UnspendableInternalKey()
	if err != nil {
		return nil, fmt.Errorf("nums internal key: %w", err)
	}
	rootHash := tree.RootNode.TapHash()
	outputKey := txscript.ComputeTaprootOutputKey(internalKey, rootHash[:])

	pkScript, err := txscript.PayToTaprootScript(outputKey)
	if err != nil {
		return nil, fmt.Errorf("pay-to-taproot: %w", err)
	}

	// A single-leaf tree has a zero-length merkle proof for the leaf.
	leafIdx, ok := tree.LeafProofIndex[leaf.TapHash()]
	if !ok {
		return nil, errors.New("leaf index not found in tree")
	}
	proof := tree.LeafMerkleProofs[leafIdx]
	ctrl := proof.ToControlBlock(internalKey)

	return &LockTxOutput{
		OutputKey:    outputKey,
		PkScript:     pkScript,
		LeafScript:   script,
		LeafHash:     leaf.TapHash(),
		ControlBlock: ctrl,
		InternalKey:  internalKey,
	}, nil
}

// RefundTxOutput holds the materials needed to fund, spend, and verify
// the refundTx output (the one with cooperative and punish branches).
type RefundTxOutput struct {
	OutputKey          *btcec.PublicKey
	PkScript           []byte
	CoopLeafScript     []byte
	CoopLeafHash       [32]byte
	CoopControlBlock   txscript.ControlBlock
	PunishLeafScript   []byte
	PunishLeafHash     [32]byte
	PunishControlBlock txscript.ControlBlock
	InternalKey        *btcec.PublicKey
	LockBlocks         int64
}

// NewRefundTxOutput derives the taproot output key, scriptPubKey, and
// both tap leaves (with their control blocks) for the refundTx output.
func NewRefundTxOutput(kal, kaf []byte, lockBlocks int64) (*RefundTxOutput, error) {
	coop, err := LockLeafScript(kal, kaf)
	if err != nil {
		return nil, fmt.Errorf("coop leaf: %w", err)
	}
	punish, err := PunishLeafScript(kaf, lockBlocks)
	if err != nil {
		return nil, fmt.Errorf("punish leaf: %w", err)
	}

	coopLeaf := txscript.NewBaseTapLeaf(coop)
	punishLeaf := txscript.NewBaseTapLeaf(punish)
	tree := txscript.AssembleTaprootScriptTree(coopLeaf, punishLeaf)

	internalKey, err := UnspendableInternalKey()
	if err != nil {
		return nil, fmt.Errorf("nums internal key: %w", err)
	}
	rootHash := tree.RootNode.TapHash()
	outputKey := txscript.ComputeTaprootOutputKey(internalKey, rootHash[:])
	pkScript, err := txscript.PayToTaprootScript(outputKey)
	if err != nil {
		return nil, fmt.Errorf("pay-to-taproot: %w", err)
	}

	coopIdx, ok := tree.LeafProofIndex[coopLeaf.TapHash()]
	if !ok {
		return nil, errors.New("coop leaf index not found")
	}
	punishIdx, ok := tree.LeafProofIndex[punishLeaf.TapHash()]
	if !ok {
		return nil, errors.New("punish leaf index not found")
	}

	coopCtrl := tree.LeafMerkleProofs[coopIdx].ToControlBlock(internalKey)
	punishCtrl := tree.LeafMerkleProofs[punishIdx].ToControlBlock(internalKey)

	return &RefundTxOutput{
		OutputKey:          outputKey,
		PkScript:           pkScript,
		CoopLeafScript:     coop,
		CoopLeafHash:       coopLeaf.TapHash(),
		CoopControlBlock:   coopCtrl,
		PunishLeafScript:   punish,
		PunishLeafHash:     punishLeaf.TapHash(),
		PunishControlBlock: punishCtrl,
		InternalKey:        internalKey,
		LockBlocks:         lockBlocks,
	}, nil
}

func checkXOnlyPubKey(k []byte, name string) error {
	if len(k) != 32 {
		return fmt.Errorf("%s must be 32-byte x-only pubkey, got %d bytes", name, len(k))
	}
	return nil
}
