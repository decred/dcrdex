// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package dcr

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"

	"decred.org/dcrdex/dex"
	"decred.org/dcrwallet/wallet/txsizes"
	"github.com/decred/dcrd/chaincfg/v3"
	"github.com/decred/dcrd/dcrec"
	"github.com/decred/dcrd/dcrutil/v3"
	"github.com/decred/dcrd/txscript/v3"
	"github.com/decred/dcrd/wire"
)

const (
	// SecretHashSize is the byte-length of the hash of the secret key used in an
	// atomic swap.
	SecretHashSize = 32

	// SecretKeySize is the byte-length of the secret key used in an atomic swap.
	SecretKeySize = 32

	// Size of serialized compressed public key.
	pubkeyLength = 33 // Length of a serialized compressed pubkey.

	// SwapContractSize is the worst case scenario size for a swap contract,
	// which is the pk-script of the non-change output of an initialization
	// transaction as used in execution of an atomic swap.
	// See ExtractSwapDetails for a breakdown of the bytes.
	SwapContractSize = 97

	// All pubkey scripts are assumed to be version 0.
	CurrentScriptVersion = 0

	// Overhead for a wire.TxIn with a scriptSig length < 254.
	// prefix (41 bytes) + ValueIn (8 bytes) + BlockHeight (4 bytes)
	// + BlockIndex (4 bytes) + sig script var int (at least 1 byte)
	TxInOverhead = 41 + 8 + 4 + 4 // 57 + at least 1 more

	P2PKHSigScriptSize = txsizes.RedeemP2PKHSigScriptSize
	P2PKHInputSize     = TxInOverhead + 1 + P2PKHSigScriptSize // 57 + 1 + 108 = 166

	// P2SHSigScriptSize and P2SHInputSize do not include the redeem script size
	// (unknown), which is concatenated on execution with the p2sh pkScript.
	//P2SHSigScriptSize = txsizes.RedeemP2SHSigScriptSize      // 110 + redeem script!
	//P2SHInputSize     = TxInOverhead + 1 + P2SHSigScriptSize // 57 + 1 + 110 = 168 + redeem script!

	// TxOutOverhead is the overhead associated with a transaction output.
	// 8 bytes value + 2 bytes version + at least 1 byte varint script size
	TxOutOverhead = 8 + 2 + 1

	P2PKHOutputSize = TxOutOverhead + txsizes.P2PKHPkScriptSize // 36
	P2SHOutputSize  = TxOutOverhead + txsizes.P2SHPkScriptSize  // 34

	// MsgTx overhead is 4 bytes version (lower 2 bytes for the real transaction
	// version and upper 2 bytes for the serialization type) + 4 bytes locktime
	// + 4 bytes expiry + 3 bytes of varints for the number of transaction
	// inputs (x2 for witness and prefix) and outputs
	MsgTxOverhead = 4 + 4 + 4 + 3 // 15

	// InitTxSizeBase is the size of a standard serialized atomic swap
	// initialization transaction with one change output and no inputs. MsgTx
	// overhead is 4 bytes version + 4 bytes locktime + 4 bytes expiry + 3 bytes
	// of varints for the number of transaction inputs (x2 for witness and
	// prefix) and outputs. There is one P2SH output with a 23 byte pkScript,
	// and one P2PKH change output with a 25 byte pkScript.
	InitTxSizeBase = MsgTxOverhead + P2PKHOutputSize + P2SHOutputSize // 15 + 36 + 34 = 85

	// InitTxSize is InitTxBaseSize + 1 P2PKH input
	InitTxSize = InitTxSizeBase + P2PKHInputSize // 85(83) + 166 = 251

	// DERSigLength is the maximum length of a DER encoded signature.
	DERSigLength = 73

	// RedeemSwapSigScriptSize is the worst case (largest) serialize size
	// of a transaction signature script that redeems atomic swap output contract.
	// It is calculated as:
	//
	//   - OP_DATA_73
	//   - 72 bytes DER signature + 1 byte sighash type
	//   - OP_DATA_33
	//   - 33 bytes serialized compressed pubkey
	//   - OP_DATA_32
	//   - 32 bytes secret key
	//   - OP_1
	//   - varint 97 => OP_PUSHDATA1(0x4c) + 0x61
	//   - 97 bytes contract script
	RedeemSwapSigScriptSize = 1 + DERSigLength + 1 + 33 + 1 + 32 + 1 + 2 + SwapContractSize // 241

	// RefundSigScriptSize is the worst case (largest) serialize size
	// of a transaction input script that refunds a compressed P2PKH output.
	// It is calculated as:
	//
	//   - OP_DATA_73
	//   - 72 bytes DER signature + 1 byte sighash type
	//   - OP_DATA_33
	//   - 33 bytes serialized compressed pubkey
	//   - OP_0
	//   - varint 97 => OP_PUSHDATA1(0x4c) + 0x61
	//   - 97 bytes contract script
	RefundSigScriptSize = 1 + DERSigLength + 1 + 33 + 1 + 2 + SwapContractSize // 208
)

// DCRScriptType is a bitmask with information about a pubkey script and
// possibly its redeem script.
type DCRScriptType uint8

const (
	ScriptP2PKH DCRScriptType = 1 << iota
	ScriptP2SH
	ScriptStake
	ScriptMultiSig
	ScriptSigEdwards
	ScriptSigSchnorr
	ScriptUnsupported
)

// ParseScriptType creates a dcrScriptType bitmask for the script type. A script
// type will be some combination of pay-to-pubkey-hash, pay-to-script-hash,
// and stake. If a script type is P2SH, it may or may not be mutli-sig.
func ParseScriptType(scriptVersion uint16, pkScript, redeemScript []byte) DCRScriptType {
	if scriptVersion != 0 {
		return ScriptUnsupported
	}
	var scriptType DCRScriptType
	switch {
	case IsPubKeyHashScript(pkScript):
		scriptType |= ScriptP2PKH
	case IsScriptHashScript(pkScript):
		scriptType |= ScriptP2SH
	case IsStakePubkeyHashScript(pkScript):
		scriptType |= ScriptP2PKH | ScriptStake
	case IsStakeScriptHashScript(pkScript):
		scriptType |= ScriptP2SH | ScriptStake
	case IsPubKeyHashAltScript(pkScript):
		scriptType |= ScriptP2PKH
		_, sigType := ExtractPubKeyHashAltDetails(pkScript)
		switch sigType {
		case dcrec.STEd25519:
			scriptType |= ScriptSigEdwards
		case dcrec.STSchnorrSecp256k1:
			scriptType |= ScriptSigSchnorr
		default:
			return ScriptUnsupported
		}
	default:
		return ScriptUnsupported
	}
	if scriptType.IsP2SH() && txscript.IsMultisigScript(redeemScript) {
		scriptType |= ScriptMultiSig
	}
	return scriptType
}

// IsP2SH will return boolean true if the script is a P2SH script.
func (s DCRScriptType) IsP2SH() bool {
	return s&ScriptP2SH != 0
}

// IsStake will return boolean true if the pubkey script it tagged with a stake
// opcode.
func (s DCRScriptType) IsStake() bool {
	return s&ScriptStake != 0
}

// IsP2PKH will return boolean true if the script is a P2PKH script.
func (s DCRScriptType) IsP2PKH() bool {
	return s&ScriptP2PKH != 0
}

// IsMultiSig is whether the pkscript references a multi-sig redeem script.
// Since the DEX will know the redeem script, we can say whether it's multi-sig.
func (s DCRScriptType) IsMultiSig() bool {
	return s&ScriptMultiSig != 0
}

// IsPubKeyHashScript returns whether or not the passed script is a standard
// pay-to-pubkey-hash script.
func IsPubKeyHashScript(script []byte) bool {
	return ExtractPubKeyHash(script) != nil
}

// IsScriptHashScript returns whether or not the passed script is a standard
// pay-to-script-hash script.
func IsScriptHashScript(script []byte) bool {
	return ExtractScriptHash(script) != nil
}

// isStakePubkeyHashScript returns whether or not the passed script is a
// stake-related P2PKH script. Script is assumed to be version 0.
func IsStakePubkeyHashScript(script []byte) bool {
	opcode := stakeOpcode(script)
	if opcode == 0 {
		return false
	}
	return ExtractStakePubKeyHash(script, opcode) != nil
}

// IsStakePubkeyHashScript returns whether or not the passed script is a
// stake-related P2SH script. Script is assumed to be version 0.
func IsStakeScriptHashScript(script []byte) bool {
	opcode := stakeOpcode(script)
	if opcode == 0 {
		return false
	}
	return ExtractStakeScriptHash(script, opcode) != nil
}

// IsPubKeyHashAltScript returns whether or not the passed script is a standard
// pay-to-alt-pubkey-hash script.
func IsPubKeyHashAltScript(script []byte) bool {
	pk, _ := ExtractPubKeyHashAltDetails(script)
	return pk != nil
}

// ExtractPubKeyHash extracts the pubkey hash from the passed script if it is a
// /standard pay-to-pubkey-hash script.  It will return nil otherwise.
func ExtractPubKeyHash(script []byte) []byte {
	// A pay-to-pubkey-hash script is of the form:
	//  OP_DUP OP_HASH160 <20-byte hash> OP_EQUALVERIFY OP_CHECKSIG
	if len(script) == 25 &&
		script[0] == txscript.OP_DUP &&
		script[1] == txscript.OP_HASH160 &&
		script[2] == txscript.OP_DATA_20 &&
		script[23] == txscript.OP_EQUALVERIFY &&
		script[24] == txscript.OP_CHECKSIG {

		return script[3:23]
	}

	return nil
}

// ExtractScriptHash extracts the script hash from the passed script if it is a
// standard pay-to-script-hash script. It will return nil otherwise. See
// ExtractStakeScriptHash for stake transaction p2sh outputs.
//
// NOTE: This function is only valid for version 0 opcodes.  Since the function
// does not accept a script version, the results are undefined for other script
// versions.
func ExtractScriptHash(script []byte) []byte {
	// A pay-to-script-hash script is of the form:
	//  OP_HASH160 <20-byte scripthash> OP_EQUAL
	if len(script) == 23 &&
		script[0] == txscript.OP_HASH160 &&
		script[1] == txscript.OP_DATA_20 &&
		script[22] == txscript.OP_EQUAL {

		return script[2:22]
	}
	return nil
}

// ExtractPubKeyHashAltDetails extracts the public key hash and signature type
// from the passed script if it is a standard pay-to-alt-pubkey-hash script.  It
// will return nil otherwise.
func ExtractPubKeyHashAltDetails(script []byte) ([]byte, dcrec.SignatureType) {
	// A pay-to-alt-pubkey-hash script is of the form:
	//  DUP HASH160 <20-byte hash> EQUALVERIFY SIGTYPE CHECKSIG
	//
	// The only two currently supported alternative signature types are ed25519
	// and schnorr + secp256k1 (with a compressed pubkey).
	//
	//  DUP HASH160 <20-byte hash> EQUALVERIFY <1-byte ed25519 sigtype> CHECKSIG
	//  DUP HASH160 <20-byte hash> EQUALVERIFY <1-byte schnorr+secp sigtype> CHECKSIG
	//
	//  Notice that OP_0 is not specified since signature type 0 disabled.

	if len(script) == 26 &&
		script[0] == txscript.OP_DUP &&
		script[1] == txscript.OP_HASH160 &&
		script[2] == txscript.OP_DATA_20 &&
		script[23] == txscript.OP_EQUALVERIFY &&
		isStandardAltSignatureType(script[24]) &&
		script[25] == txscript.OP_CHECKSIGALT {

		return script[3:23], dcrec.SignatureType(asSmallInt(script[24]))
	}

	return nil, 0
}

// Check if the opcode is one of a small number of acceptable stake opcodes that
// are prepended to P2PKH scripts. Return the opcode if it is, else OP_0.
func stakeOpcode(script []byte) byte {
	if len(script) == 0 {
		return 0
	}
	opcode := script[0]
	if opcode == txscript.OP_SSGEN || opcode == txscript.OP_SSRTX {
		return opcode
	}
	return 0
}

// ExtractStakePubKeyHash extracts a pubkey hash from the passed public key
// script if it is a standard pay-to-pubkey-hash script tagged with the provided
// stake opcode.  It will return nil otherwise.
func ExtractStakePubKeyHash(script []byte, stakeOpcode byte) []byte {
	if len(script) == 26 &&
		script[0] == stakeOpcode &&
		script[1] == txscript.OP_DUP &&
		script[2] == txscript.OP_HASH160 &&
		script[3] == txscript.OP_DATA_20 &&
		script[24] == txscript.OP_EQUALVERIFY &&
		script[25] == txscript.OP_CHECKSIG {

		return script[4:24]
	}

	return nil
}

// ExtractStakeScriptHash extracts a script hash from the passed public key
// script if it is a standard pay-to-script-hash script tagged with the provided
// stake opcode.  It will return nil otherwise.
func ExtractStakeScriptHash(script []byte, stakeOpcode byte) []byte {
	if len(script) == 24 &&
		script[0] == stakeOpcode &&
		script[1] == txscript.OP_HASH160 &&
		script[2] == txscript.OP_DATA_20 &&
		script[23] == txscript.OP_EQUAL {

		return script[3:23]
	}

	return nil
}

// MakeContract creates an atomic swap contract. The secretHash MUST be computed
// from a secret of length SecretKeySize bytes or the resulting contract will be
// invalid.
func MakeContract(recipient, sender string, secretHash []byte, lockTime int64, chainParams *chaincfg.Params) ([]byte, error) {
	rAddr, err := dcrutil.DecodeAddress(recipient, chainParams)
	if err != nil {
		return nil, fmt.Errorf("error decoding recipient address %s: %w", recipient, err)
	}
	a, ok := rAddr.(*dcrutil.AddressPubKeyHash)
	if !ok {
		return nil, fmt.Errorf("recipient address %s is not a pubkey-hash address", recipient)
	}
	if a.DSA() != dcrec.STEcdsaSecp256k1 {
		return nil, fmt.Errorf("recipient address signature algorithm unsupported")
	}
	sAddr, err := dcrutil.DecodeAddress(sender, chainParams)
	if err != nil {
		return nil, fmt.Errorf("error decoding sender address %s: %w", sender, err)
	}
	a, ok = sAddr.(*dcrutil.AddressPubKeyHash)
	if !ok {
		return nil, fmt.Errorf("sender address %s is not a pubkey-hash address", recipient)
	}
	if a.DSA() != dcrec.STEcdsaSecp256k1 {
		return nil, fmt.Errorf("sender address signature algorithm unsupported")
	}
	if len(secretHash) != SecretHashSize {
		return nil, fmt.Errorf("secret hash of length %d not supported", len(secretHash))
	}

	return txscript.NewScriptBuilder().
		AddOps([]byte{
			txscript.OP_IF,
			txscript.OP_SIZE,
		}).AddInt64(SecretKeySize).
		AddOps([]byte{
			txscript.OP_EQUALVERIFY,
			txscript.OP_SHA256,
		}).AddData(secretHash).
		AddOps([]byte{
			txscript.OP_EQUALVERIFY,
			txscript.OP_DUP,
			txscript.OP_HASH160,
		}).AddData(rAddr.ScriptAddress()).
		AddOp(txscript.OP_ELSE).
		AddInt64(lockTime).
		AddOps([]byte{
			txscript.OP_CHECKLOCKTIMEVERIFY,
			txscript.OP_DROP,
			txscript.OP_DUP,
			txscript.OP_HASH160,
		}).AddData(sAddr.ScriptAddress()).
		AddOps([]byte{
			txscript.OP_ENDIF,
			txscript.OP_EQUALVERIFY,
			txscript.OP_CHECKSIG,
		}).Script()
}

// RedeemP2SHContract returns the signature script to redeem a contract output
// using the redeemer's signature and the initiator's secret.  This function
// assumes P2SH and appends the contract as the final data push.
func RedeemP2SHContract(contract, sig, pubkey, secret []byte) ([]byte, error) {
	return txscript.NewScriptBuilder().
		AddData(sig).
		AddData(pubkey).
		AddData(secret).
		AddInt64(1).
		AddData(contract).
		Script()
}

// RefundP2SHContract returns the signature script to refund a contract output
// using the contract author's signature after the locktime has been reached.
// This function assumes P2SH and appends the contract as the final data push.
func RefundP2SHContract(contract, sig, pubkey []byte) ([]byte, error) {
	return txscript.NewScriptBuilder().
		AddData(sig).
		AddData(pubkey).
		AddInt64(0).
		AddData(contract).
		Script()
}

// ExtractSwapDetails extacts the sender and receiver addresses from a swap
// contract. If the provided script is not a swap contract, an error will be
// returned.
func ExtractSwapDetails(pkScript []byte, chainParams *chaincfg.Params) (
	sender, receiver dcrutil.Address, lockTime uint64, secretHash []byte, err error) {
	// A swap redemption sigScript is <pubkey> <secret> and satisfies the
	// following swap contract, allowing only for a secret of size
	//
	// OP_IF
	//  OP_SIZE OP_DATA_1 secretSize OP_EQUALVERIFY OP_SHA256 OP_DATA_32 secretHash OP_EQUALVERIFY OP_DUP OP_HASH160 OP_DATA20 pkHashReceiver
	//     1   +   1     +    1     +      1       +    1    +    1     +    32    +      1       +   1  +    1     +    1    +    20
	// OP_ELSE
	//  OP_DATA4 lockTime OP_CHECKLOCKTIMEVERIFY OP_DROP OP_DUP OP_HASH160 OP_DATA_20 pkHashSender
	//     1    +    4   +           1          +   1   +  1   +    1     +   1      +    20
	// OP_ENDIF
	// OP_EQUALVERIFY
	// OP_CHECKSIG
	//
	// 5 bytes if-else-endif-equalverify-checksig
	// 1 + 1 + 1 + 1 + 1 + 1 + 32 + 1 + 1 + 1 + 1 + 20 = 62 bytes for redeem block
	// 1 + 4 + 1 + 1 + 1 + 1 + 1 + 20 = 30 bytes for refund block
	// 5 + 62 + 30 = 97 bytes
	//
	// Note that this allows for a secret size of up to 75 bytes, but the secret
	// must be 32 bytes to be considered valid.
	if len(pkScript) != SwapContractSize {
		err = fmt.Errorf("incorrect swap contract length. expected %d, got %d",
			SwapContractSize, len(pkScript))
		return
	}

	if pkScript[0] == txscript.OP_IF &&
		pkScript[1] == txscript.OP_SIZE &&
		pkScript[2] == txscript.OP_DATA_1 &&
		// secretSize (1 bytes)
		pkScript[4] == txscript.OP_EQUALVERIFY &&
		pkScript[5] == txscript.OP_SHA256 &&
		pkScript[6] == txscript.OP_DATA_32 &&
		// secretHash (32 bytes)
		pkScript[39] == txscript.OP_EQUALVERIFY &&
		pkScript[40] == txscript.OP_DUP &&
		pkScript[41] == txscript.OP_HASH160 &&
		pkScript[42] == txscript.OP_DATA_20 &&
		// receiver's pkh (20 bytes)
		pkScript[63] == txscript.OP_ELSE &&
		pkScript[64] == txscript.OP_DATA_4 &&
		// lockTime (4 bytes)
		pkScript[69] == txscript.OP_CHECKLOCKTIMEVERIFY &&
		pkScript[70] == txscript.OP_DROP &&
		pkScript[71] == txscript.OP_DUP &&
		pkScript[72] == txscript.OP_HASH160 &&
		pkScript[73] == txscript.OP_DATA_20 &&
		// sender's pkh (20 bytes)
		pkScript[94] == txscript.OP_ENDIF &&
		pkScript[95] == txscript.OP_EQUALVERIFY &&
		pkScript[96] == txscript.OP_CHECKSIG {

		if ssz := pkScript[3]; ssz != SecretKeySize {
			return nil, nil, 0, nil, fmt.Errorf("invalid secret size %d", ssz)
		}

		receiver, err = dcrutil.NewAddressPubKeyHash(pkScript[43:63], chainParams, dcrec.STEcdsaSecp256k1)
		if err != nil {
			return nil, nil, 0, nil, fmt.Errorf("error decoding address from recipient's pubkey hash")
		}

		sender, err = dcrutil.NewAddressPubKeyHash(pkScript[74:94], chainParams, dcrec.STEcdsaSecp256k1)
		if err != nil {
			return nil, nil, 0, nil, fmt.Errorf("error decoding address from sender's pubkey hash")
		}

		lockTime = uint64(binary.LittleEndian.Uint32(pkScript[65:69]))
		secretHash = pkScript[7:39]

		return
	}

	err = fmt.Errorf("invalid swap contract")
	return
}

// IsDust returns whether or not the passed transaction output amount is
// considered dust or not based on the passed minimum transaction relay fee.
// Dust is defined in terms of the minimum transaction relay fee.
// See dcrd/mempool/policy isDust for further documentation, though this version
// accepts atoms/byte rather than atoms/kB.
func IsDust(txOut *wire.TxOut, minRelayTxFee uint64) bool {
	// Unspendable outputs are considered dust.
	if txscript.IsUnspendable(txOut.Value, txOut.PkScript) {
		return true
	}
	return IsDustVal(uint64(txOut.SerializeSize()), uint64(txOut.Value), minRelayTxFee)
}

// IsDustVal is like IsDust but it only needs the size of the serialized output
// and its amount.
func IsDustVal(sz, amt, minRelayTxFee uint64) bool {
	totalSize := sz + 165
	return amt/(3*totalSize) < minRelayTxFee
}

// isStandardAltSignatureType returns whether or not the provided opcode
// represents a push of a standard alt signature type.
func isStandardAltSignatureType(op byte) bool {
	if !isSmallInt(op) {
		return false
	}

	sigType := asSmallInt(op)
	return sigType == dcrec.STEd25519 || sigType == dcrec.STSchnorrSecp256k1
}

// asSmallInt returns the passed opcode, which must be true according to
// isSmallInt(), as an integer.
func asSmallInt(op byte) int {
	if op == txscript.OP_0 {
		return 0
	}

	return int(op - (txscript.OP_1 - 1))
}

// isSmallInt returns whether or not the opcode is considered a small integer,
// which is an OP_0, or OP_1 through OP_16.
//
// NOTE: This function is only valid for version 0 opcodes.  Since the function
// does not accept a script version, the results are undefined for other script
// versions.
func isSmallInt(op byte) bool {
	return op == txscript.OP_0 || (op >= txscript.OP_1 && op <= txscript.OP_16)
}

// Grab the script hash based on the dcrScriptType.
func ExtractScriptHashByType(scriptType DCRScriptType, pkScript []byte) ([]byte, error) {
	var redeemScript []byte
	// Stake related scripts will start with OP_SSGEN or OP_SSRTX.
	if scriptType.IsStake() {
		opcode := stakeOpcode(pkScript)
		if opcode == 0 {
			return nil, fmt.Errorf("unsupported stake opcode")
		}
		redeemScript = ExtractStakeScriptHash(pkScript, opcode)
	} else {
		redeemScript = ExtractScriptHash(pkScript)
	}
	if redeemScript == nil {
		return nil, fmt.Errorf("failed to parse p2sh script")
	}
	return redeemScript, nil
}

// ExtractScriptData extracts script type, addresses, and required signature
// count from a pkScript. Non-standard scripts are not necessarily an error;
// non-nil errors are only returned if the script cannot be parsed. See also
// InputInfo for additional signature script size data
func ExtractScriptData(script []byte, chainParams *chaincfg.Params) (DCRScriptType, []string, int, error) {
	class, addrs, numRequired, err := txscript.ExtractPkScriptAddrs(0, script, chainParams, false)
	if err != nil {
		return ScriptUnsupported, nil, 0, err
	}
	if class == txscript.NonStandardTy {
		return ScriptUnsupported, nil, 0, nil
	}

	// Could switch on class from ExtractPkScriptAddrs, but use ParseScriptType
	// for internal consistency.
	scriptType := ParseScriptType(CurrentScriptVersion, script, nil)

	addresses := make([]string, len(addrs))
	for i, addr := range addrs {
		addresses[i] = addr.String()
	}

	return scriptType, addresses, numRequired, nil
}

// DCRScriptAddrs is information about the pubkeys or pubkey hashes present in
// a scriptPubKey (and the redeem script, for p2sh). This information can be
// used to estimate the spend script size, e.g. pubkeys in a redeem script don't
// require pubkeys in the scriptSig, but pubkey hashes do.
type DCRScriptAddrs struct {
	PubKeys   []dcrutil.Address
	NumPK     int
	PkHashes  []dcrutil.Address
	NumPKH    int
	NRequired int
}

// ExtractScriptAddrs extracts the addresses from script. Addresses are
// separated into pubkey and pubkey hash, where the pkh addresses are actually a
// catch all for non-P2PK addresses. As such, this function is not intended for
// use on P2SH pkScripts. Rather, the corresponding redeem script should be
// processed with ExtractScriptAddrs. The returned bool indicates if the script
// is non-standard.
func ExtractScriptAddrs(script []byte, chainParams *chaincfg.Params) (*DCRScriptAddrs, bool, error) {
	pubkeys := make([]dcrutil.Address, 0)
	pkHashes := make([]dcrutil.Address, 0)
	// For P2SH and non-P2SH multi-sig, pull the addresses from the pubkey script.
	class, addrs, numRequired, err := txscript.ExtractPkScriptAddrs(0, script, chainParams, false)
	nonStandard := class == txscript.NonStandardTy
	if err != nil {
		return nil, nonStandard, fmt.Errorf("ExtractScriptAddrs: %w", err)
	}
	if nonStandard {
		return &DCRScriptAddrs{}, nonStandard, nil
	}
	for _, addr := range addrs {
		// If the address is an unhashed public key, is won't need a pubkey as
		// part of its sigScript, so count them separately.
		_, isPubkey := addr.(*dcrutil.AddressSecpPubKey)
		if isPubkey {
			pubkeys = append(pubkeys, addr)
		} else {
			pkHashes = append(pkHashes, addr)
		}
	}
	return &DCRScriptAddrs{
		PubKeys:   pubkeys,
		NumPK:     len(pubkeys),
		PkHashes:  pkHashes,
		NumPKH:    len(pkHashes),
		NRequired: numRequired,
	}, false, nil
}

// SpendInfo is information about an input and it's previous outpoint.
type SpendInfo struct {
	SigScriptSize     uint32
	ScriptAddrs       *DCRScriptAddrs
	ScriptType        DCRScriptType
	NonStandardScript bool
}

// DataPrefixSize returns the size of the size opcodes that would precede the
// data in a script. Certain data of length 0 and 1 is represented with OP_# or
// OP_1NEGATE codes directly without preceding OP_DATA_# OR OP_PUSHDATA# codes.
func DataPrefixSize(data []byte) uint8 {
	serSz := txscript.CanonicalDataSize(data)
	sz := len(data)
	prefixSize := serSz - sz
	if sz == 0 {
		prefixSize-- // empty array uses a byte
	}
	return uint8(prefixSize)
}

// Size is the serialized size of the input.
func (nfo *SpendInfo) Size() uint32 {
	return TxInOverhead + uint32(wire.VarIntSerializeSize(uint64(nfo.SigScriptSize))) + nfo.SigScriptSize
}

// InputInfo is some basic information about the input required to spend an
// output. The pubkey script of the output is provided. If the pubkey script
// parses as P2SH or P2WSH, the redeem script must be provided.
func InputInfo(pkScript, redeemScript []byte, chainParams *chaincfg.Params) (*SpendInfo, error) {
	scriptType := ParseScriptType(CurrentScriptVersion, pkScript, redeemScript)
	if scriptType == ScriptUnsupported {
		return nil, dex.UnsupportedScriptError
	}

	// Get information about the signatures and pubkeys needed to spend the utxo.
	evalScript := pkScript
	if scriptType.IsP2SH() {
		if len(redeemScript) == 0 {
			return nil, fmt.Errorf("no redeem script provided for P2SH pubkey script")
		}
		evalScript = redeemScript
	}
	scriptAddrs, nonStandard, err := ExtractScriptAddrs(evalScript, chainParams)
	if err != nil {
		return nil, fmt.Errorf("error parsing utxo script addresses")
	}
	if nonStandard {
		return &SpendInfo{
			// SigScriptSize cannot be determined, leave zero.
			ScriptAddrs:       scriptAddrs,
			ScriptType:        scriptType,
			NonStandardScript: true,
		}, nil
	}

	// Get the size of the signature script.
	sigScriptSize := P2PKHSigScriptSize
	// If it's a P2SH, the size must be calculated based on other factors.
	if scriptType.IsP2SH() {
		// Start with the signatures.
		sigScriptSize = 74 * scriptAddrs.NRequired // 73 max for sig, 1 for push code
		// If there are pubkey-hash addresses, they'll need pubkeys.
		if scriptAddrs.NumPKH > 0 {
			sigScriptSize += scriptAddrs.NRequired * (pubkeyLength + 1)
		}
		// Then add the length of the script and another push opcode byte.
		sigScriptSize += len(redeemScript) + int(DataPrefixSize(redeemScript)) // push data length op code might be >1 byte
	} else if !scriptType.IsP2PKH() {
		// Some defensive checks here.
		if scriptType.IsMultiSig() {
			// Should have been caught by ParseScriptType.
			return nil, fmt.Errorf("bare multisig is unsupported")
		}
		// P2PK also should have been caught by ParseScriptType.
		return nil, fmt.Errorf("unsupported pkScript: %d", scriptType)
	}
	return &SpendInfo{
		SigScriptSize: uint32(sigScriptSize),
		ScriptAddrs:   scriptAddrs,
		ScriptType:    scriptType,
	}, nil
}

// ExtractContractHash extracts the contract P2SH address from a pkScript. A
// non-nil error is returned if the hexadecimal encoding of the pkScript is
// invalid, or if the pkScript is not a P2SH script.
func ExtractContractHash(scriptHex string) ([]byte, error) {
	pkScript, err := hex.DecodeString(scriptHex)
	if err != nil {
		return nil, fmt.Errorf("error decoding scriptPubKey '%s': %w",
			scriptHex, err)
	}
	scriptHash := ExtractScriptHash(pkScript)
	if scriptHash == nil {
		return nil, fmt.Errorf("error extracting script hash")
	}
	return scriptHash, nil
}

// FindKeyPush attempts to extract the secret key from the signature script. The
// contract must be provided for the search algorithm to verify the correct data
// push. Only contracts of length SwapContractSize that can be validated by
// ExtractSwapDetails are recognized.
func FindKeyPush(sigScript, contractHash []byte, chainParams *chaincfg.Params) ([]byte, error) {
	dataPushes, err := txscript.PushedData(sigScript)
	if err != nil {
		return nil, err
	}
	if len(dataPushes) == 0 {
		return nil, fmt.Errorf("no data pushes in in the signature script")
	}
	// The key must be the last data push, but iterate through all of the pushes
	// backwards to ensure it not hidden behind some non-standard script.
	var keyHash []byte
	for i := len(dataPushes) - 1; i >= 0; i-- {
		push := dataPushes[i]

		// First locate the swap contract.
		if len(keyHash) == 0 {
			// Skip hashing if ExtractSwapDetails will not recognize it.
			if len(push) != SwapContractSize {
				continue
			}
			h := dcrutil.Hash160(push)
			if bytes.Equal(h, contractHash) {
				_, _, _, keyHash, err = ExtractSwapDetails(push, chainParams)
				if err != nil {
					return nil, fmt.Errorf("error extracting atomic swap details: %w", err)
				}
			}
			continue
		}

		// We have the key hash from the contract. See if this is the key.
		h := sha256.Sum256(push)
		if bytes.Equal(h[:], keyHash) {
			return push, nil
		}
	}
	return nil, fmt.Errorf("key not found")
}
