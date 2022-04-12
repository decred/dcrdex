// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package dcr

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"fmt"

	"decred.org/dcrdex/dex"
	"decred.org/dcrwallet/v2/wallet/txsizes"
	"github.com/decred/dcrd/chaincfg/v3"
	"github.com/decred/dcrd/dcrec"
	"github.com/decred/dcrd/dcrutil/v4"
	"github.com/decred/dcrd/txscript/v4"
	"github.com/decred/dcrd/txscript/v4/stdaddr"
	"github.com/decred/dcrd/txscript/v4/stdscript"
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

	// Overhead for a wire.TxIn with a scriptSig length < 254.
	// prefix (41 bytes) + ValueIn (8 bytes) + BlockHeight (4 bytes)
	// + BlockIndex (4 bytes) + sig script var int (at least 1 byte)
	TxInOverhead = 41 + 8 + 4 + 4 // 57 + at least 1 more

	P2PKSigScriptSize = txsizes.RedeemP2PKSigScriptSize

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

// ScriptType is a bitmask with information about a pubkey script and
// possibly its redeem script.
type ScriptType uint8

const (
	ScriptP2PKH ScriptType = 1 << iota
	ScriptP2PK
	ScriptP2SH
	ScriptStake
	ScriptMultiSig
	ScriptSigEdwards
	ScriptSigSchnorr
	ScriptUnsupported
)

// ParseScriptType creates a ScriptType bitmask for a pkScript.
func ParseScriptType(scriptVersion uint16, pkScript []byte) ScriptType {
	st := stdscript.DetermineScriptType(scriptVersion, pkScript)
	return convertScriptType(st)
}

// IsP2SH will return boolean true if the script is a P2SH script.
func (s ScriptType) IsP2SH() bool {
	return s&ScriptP2SH != 0
}

// IsStake will return boolean true if the pubkey script it tagged with a stake
// opcode.
func (s ScriptType) IsStake() bool {
	return s&ScriptStake != 0
}

// IsP2PK will return boolean true if the script is a P2PK script.
func (s ScriptType) IsP2PK() bool {
	return s&ScriptP2PK != 0
}

// IsP2PKH will return boolean true if the script is a P2PKH script.
func (s ScriptType) IsP2PKH() bool {
	return s&ScriptP2PKH != 0
}

// IsMultiSig is whether the pkscript references a multi-sig redeem script.
// Since the DEX will know the redeem script, we can say whether it's multi-sig.
func (s ScriptType) IsMultiSig() bool {
	return s&ScriptMultiSig != 0
}

func convertScriptType(st stdscript.ScriptType) ScriptType {
	var scriptType ScriptType
	switch st {
	// P2PK
	case stdscript.STPubKeyEcdsaSecp256k1:
		scriptType = ScriptP2PK
	case stdscript.STPubKeyEd25519:
		scriptType = ScriptP2PK | ScriptSigEdwards
	case stdscript.STPubKeySchnorrSecp256k1:
		scriptType = ScriptP2PK | ScriptSigSchnorr
	// P2PKH
	case stdscript.STPubKeyHashEcdsaSecp256k1:
		scriptType = ScriptP2PKH
	case stdscript.STPubKeyHashEd25519:
		scriptType = ScriptP2PKH | ScriptSigEdwards
	case stdscript.STPubKeyHashSchnorrSecp256k1:
		scriptType = ScriptP2PKH | ScriptSigSchnorr
	case stdscript.STStakeSubmissionPubKeyHash, stdscript.STStakeChangePubKeyHash,
		stdscript.STStakeGenPubKeyHash, stdscript.STStakeRevocationPubKeyHash,
		stdscript.STTreasuryGenPubKeyHash:
		scriptType = ScriptP2PKH | ScriptStake
	// P2SH
	case stdscript.STScriptHash:
		scriptType = ScriptP2SH
	case stdscript.STStakeChangeScriptHash, stdscript.STStakeGenScriptHash,
		stdscript.STStakeRevocationScriptHash, stdscript.STStakeSubmissionScriptHash,
		stdscript.STTreasuryGenScriptHash:
		scriptType = ScriptP2SH | ScriptStake
	// P2MS
	case stdscript.STMultiSig:
		scriptType = ScriptMultiSig
	default: // STNonStandard, STNullData, STTreasuryAdd, etc
		return ScriptUnsupported
	}
	return scriptType
}

// AddressScript returns the raw bytes of the address to be used when inserting
// the address into a txout's script. This is currently the address' pubkey or
// script hash160. Other address types are unsupported.
func AddressScript(addr stdaddr.Address) ([]byte, error) {
	switch addr := addr.(type) {
	case stdaddr.SerializedPubKeyer:
		return addr.SerializedPubKey(), nil
	case stdaddr.Hash160er:
		return addr.Hash160()[:], nil
	default:
		return nil, fmt.Errorf("unsupported address type %T", addr)
	}
}

// AddressSigType returns the digital signature algorithm for the specified
// address.
func AddressSigType(addr stdaddr.Address) (sigType dcrec.SignatureType, err error) {
	switch addr.(type) {
	case *stdaddr.AddressPubKeyEcdsaSecp256k1V0:
		sigType = dcrec.STEcdsaSecp256k1
	case *stdaddr.AddressPubKeyHashEcdsaSecp256k1V0:
		sigType = dcrec.STEcdsaSecp256k1

	case *stdaddr.AddressPubKeyEd25519V0:
		sigType = dcrec.STEd25519
	case *stdaddr.AddressPubKeyHashEd25519V0:
		sigType = dcrec.STEd25519

	case *stdaddr.AddressPubKeySchnorrSecp256k1V0:
		sigType = dcrec.STSchnorrSecp256k1
	case *stdaddr.AddressPubKeyHashSchnorrSecp256k1V0:
		sigType = dcrec.STSchnorrSecp256k1

	default:
		err = fmt.Errorf("unsupported signature type")
	}
	return sigType, err
}

// MakeContract creates an atomic swap contract. The secretHash MUST be computed
// from a secret of length SecretKeySize bytes or the resulting contract will be
// invalid.
func MakeContract(recipient, sender string, secretHash []byte, lockTime int64, chainParams *chaincfg.Params) ([]byte, error) {
	parseAddress := func(address string) (*stdaddr.AddressPubKeyHashEcdsaSecp256k1V0, error) {
		addr, err := stdaddr.DecodeAddress(address, chainParams)
		if err != nil {
			return nil, fmt.Errorf("error decoding address %s: %w", address, err)
		}
		if addr, ok := addr.(*stdaddr.AddressPubKeyHashEcdsaSecp256k1V0); ok {
			return addr, nil
		} else {
			return nil, fmt.Errorf("address %s is not a pubkey-hash address or "+
				"signature algorithm is unsupported", address)
		}
	}

	rAddr, err := parseAddress(recipient)
	if err != nil {
		return nil, fmt.Errorf("invalid recipient address: %w", err)
	}
	sAddr, err := parseAddress(sender)
	if err != nil {
		return nil, fmt.Errorf("invalid sender address: %w", err)
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
		}).AddData(rAddr.Hash160()[:]).
		AddOp(txscript.OP_ELSE).
		AddInt64(lockTime).
		AddOps([]byte{
			txscript.OP_CHECKLOCKTIMEVERIFY,
			txscript.OP_DROP,
			txscript.OP_DUP,
			txscript.OP_HASH160,
		}).AddData(sAddr.Hash160()[:]).
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
	sender, receiver stdaddr.Address, lockTime uint64, secretHash []byte, err error) {
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

		receiver, err = stdaddr.NewAddressPubKeyHashEcdsaSecp256k1V0(pkScript[43:63], chainParams)
		if err != nil {
			return nil, nil, 0, nil, fmt.Errorf("error decoding address from recipient's pubkey hash")
		}

		sender, err = stdaddr.NewAddressPubKeyHashEcdsaSecp256k1V0(pkScript[74:94], chainParams)
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

// ExtractScriptHash extracts the script hash from a P2SH pkScript of a
// particular script version.
func ExtractScriptHash(version uint16, script []byte) []byte {
	switch version {
	case 0:
		return ExtractScriptHashV0(script)
	}
	return nil
}

// ExtractScriptHashV0 extracts the script hash from a P2SH pkScript. This is
// valid only for version 0 scripts.
func ExtractScriptHashV0(script []byte) []byte {
	if h := stdscript.ExtractScriptHashV0(script); h != nil {
		return h
	}
	return stdscript.ExtractStakeScriptHashV0(script)
}

// ExtractScriptData extracts script type, addresses, and required signature
// count from a pkScript. Non-standard scripts are not an error; non-nil errors
// are only returned if the script cannot be parsed. See also InputInfo for
// additional signature script size data
func ExtractScriptData(version uint16, script []byte, chainParams *chaincfg.Params) (ScriptType, []string, int) {
	class, addrs := stdscript.ExtractAddrs(version, script, chainParams)
	if class == stdscript.STNonStandard {
		return ScriptUnsupported, nil, 0
	}
	numRequired := stdscript.DetermineRequiredSigs(version, script)
	scriptType := convertScriptType(class)
	addresses := make([]string, len(addrs))
	for i, addr := range addrs {
		addresses[i] = addr.String()
	}

	return scriptType, addresses, int(numRequired)
}

// ScriptAddrs is information about the pubkeys or pubkey hashes present in a
// scriptPubKey (and the redeem script, for p2sh). This information can be used
// to estimate the spend script size, e.g. pubkeys in a redeem script don't
// require pubkeys in the scriptSig, but pubkey hashes do.
type ScriptAddrs struct {
	PubKeys   []stdaddr.Address
	PkHashes  []stdaddr.Address
	NRequired int
}

// ExtractScriptAddrs extracts the addresses from script. Addresses are
// separated into pubkey and pubkey hash, where the pkh addresses are actually a
// catch all for non-P2PK addresses. As such, this function is not intended for
// use on P2SH pkScripts. Rather, the corresponding redeem script should be
// processed with ExtractScriptAddrs. The returned bool indicates if the script
// is non-standard.
func ExtractScriptAddrs(version uint16, script []byte, chainParams *chaincfg.Params) (ScriptType, *ScriptAddrs) {
	pubkeys := make([]stdaddr.Address, 0)
	pkHashes := make([]stdaddr.Address, 0)
	class, addrs := stdscript.ExtractAddrs(version, script, chainParams)
	if class == stdscript.STNonStandard {
		return ScriptUnsupported, &ScriptAddrs{}
	}
	for _, addr := range addrs {
		// If the address is an unhashed public key, is won't need a pubkey as
		// part of its sigScript, so count them separately.
		_, isPubkey := addr.(stdaddr.SerializedPubKeyer)
		if isPubkey {
			pubkeys = append(pubkeys, addr)
		} else {
			pkHashes = append(pkHashes, addr)
		}
	}
	numRequired := stdscript.DetermineRequiredSigs(version, script)
	scriptType := convertScriptType(class)
	return scriptType, &ScriptAddrs{
		PubKeys:   pubkeys,
		PkHashes:  pkHashes,
		NRequired: int(numRequired),
	}
}

// SpendInfo is information about an input and it's previous outpoint.
type SpendInfo struct {
	SigScriptSize     uint32
	ScriptAddrs       *ScriptAddrs
	ScriptType        ScriptType
	NonStandardScript bool // refers to redeem script for P2SH ScriptType
}

// Size is the serialized size of the input.
func (nfo *SpendInfo) Size() uint32 {
	return TxInOverhead + uint32(wire.VarIntSerializeSize(uint64(nfo.SigScriptSize))) + nfo.SigScriptSize
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

// InputInfo is some basic information about the input required to spend an
// output. Only P2PKH and P2SH pkScripts are supported. If the pubkey script
// parses as P2SH, the redeem script must be provided.
func InputInfo(version uint16, pkScript, redeemScript []byte, chainParams *chaincfg.Params) (*SpendInfo, error) {
	scriptType, scriptAddrs := ExtractScriptAddrs(version, pkScript, chainParams)
	switch {
	case scriptType == ScriptUnsupported:
		return nil, dex.UnsupportedScriptError
	case scriptType.IsP2PK():
		return &SpendInfo{
			SigScriptSize: P2PKSigScriptSize,
			ScriptAddrs:   scriptAddrs,
			ScriptType:    scriptType,
		}, nil
	case scriptType.IsP2PKH():
		return &SpendInfo{
			SigScriptSize: P2PKHSigScriptSize,
			ScriptAddrs:   scriptAddrs,
			ScriptType:    scriptType,
		}, nil
	case scriptType.IsP2SH():
	default:
		return nil, fmt.Errorf("unsupported pkScript: %d", scriptType) // dex.UnsupportedScriptError too?
	}

	// P2SH
	if len(redeemScript) == 0 {
		return nil, fmt.Errorf("no redeem script provided for P2SH pubkey script")
	}

	var redeemScriptType ScriptType
	redeemScriptType, scriptAddrs = ExtractScriptAddrs(version, redeemScript, chainParams)
	if redeemScriptType == ScriptUnsupported {
		return &SpendInfo{
			// SigScriptSize cannot be determined, leave zero.
			ScriptAddrs:       scriptAddrs,
			ScriptType:        scriptType, // still P2SH
			NonStandardScript: true,       // but non-standard redeem script (e.g. contract)
		}, nil
	}

	// NOTE: scriptAddrs are now from the redeemScript, and we add the multisig
	// bit to scriptType.
	if redeemScriptType.IsMultiSig() {
		scriptType |= ScriptMultiSig
	}

	// If it's a P2SH, with a standard redeem script, compute the sigScript size
	// for the expected number of signatures, pubkeys, and the redeem script
	// itself. Start with the signatures.
	sigScriptSize := (1 + DERSigLength) * scriptAddrs.NRequired // 73 max for sig, 1 for push code
	// If there are pubkey-hash addresses, they'll need pubkeys.
	if len(scriptAddrs.PkHashes) > 0 {
		sigScriptSize += scriptAddrs.NRequired * (pubkeyLength + 1)
	}
	// Then add the length of the script and the push opcode byte(s).
	sigScriptSize += len(redeemScript) + int(DataPrefixSize(redeemScript)) // push data length op code might be >1 byte
	return &SpendInfo{
		SigScriptSize: uint32(sigScriptSize),
		ScriptAddrs:   scriptAddrs,
		ScriptType:    scriptType,
	}, nil
}

// FindKeyPush attempts to extract the secret key from the signature script. The
// contract must be provided for the search algorithm to verify the correct data
// push. Only contracts of length SwapContractSize that can be validated by
// ExtractSwapDetails are recognized.
func FindKeyPush(scriptVersion uint16, sigScript, contractHash []byte, chainParams *chaincfg.Params) ([]byte, error) {
	tokenizer := txscript.MakeScriptTokenizer(scriptVersion, sigScript)

	// The contract is pushed after the key, find the contract starting with the
	// first data push and record all data pushes encountered before the contract
	// push. One of those preceding pushes should be the key push.
	var dataPushesUpTillContract [][]byte
	var keyHash []byte
	var err error
	for tokenizer.Next() {
		push := tokenizer.Data()

		// Only hash if ExtractSwapDetails will recognize it.
		if len(push) == SwapContractSize {
			h := dcrutil.Hash160(push)
			if bytes.Equal(h, contractHash) {
				_, _, _, keyHash, err = ExtractSwapDetails(push, chainParams)
				if err != nil {
					return nil, fmt.Errorf("error extracting atomic swap details: %w", err)
				}
				break // contract is pushed after the key, if we've encountered the contract, we must have just passed the key
			}
		}

		// Save this push as preceding the contract push.
		if push != nil {
			dataPushesUpTillContract = append(dataPushesUpTillContract, push)
		}
	}
	if tokenizer.Err() != nil {
		return nil, tokenizer.Err()
	}

	if len(keyHash) > 0 {
		// The key push should be the data push immediately preceding the contract
		// push, but iterate through all of the preceding pushes backwards to ensure
		// it is not hidden behind some non-standard script.
		for i := len(dataPushesUpTillContract) - 1; i >= 0; i-- {
			push := dataPushesUpTillContract[i]

			// We have the key hash from the contract. See if this is the key.
			h := sha256.Sum256(push)
			if bytes.Equal(h[:], keyHash) {
				return push, nil
			}
		}
	}

	return nil, fmt.Errorf("key not found")
}
