// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package lbc

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"

	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/server/account"
	"github.com/btcsuite/btcd/btcutil"
	"github.com/lbryio/lbcd/btcec"
	"github.com/lbryio/lbcd/chaincfg"
	"github.com/lbryio/lbcd/txscript"
	"github.com/lbryio/lbcd/txscript/stdaddr"
	"github.com/lbryio/lbcd/txscript/stdscript"
	"github.com/lbryio/lbcd/wire"
	"github.com/lbryio/lbcwallet/wallet/txsizes"
	"golang.org/x/crypto/ripemd160"
)

const (
	// MaxCLTVScriptNum is the largest usable value for a CLTV lockTime. This
	// will actually be stored in a 5-byte ScriptNum since they have a sign bit,
	// however, it is not 2^39-1 since the spending transaction's nLocktime is
	// an unsigned 32-bit integer and it must be at least the CLTV value. This
	// establishes a maximum lock time of February 7, 2106. Any later requires
	// using a block height instead of a unix epoch time stamp.
	MaxCLTVScriptNum = 1<<32 - 1 // 0xffff_ffff a.k.a. 2^32-1

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

	// PrivateSwapContractSize is the size of a private swap contract.
	PrivateSwapContractSize = 62

	// TxInOverhead is the overhead for a wire.TxIn with a scriptSig length <
	// 254. prefix (41 bytes) + ValueIn (8 bytes) + BlockHeight (4 bytes) +
	// BlockIndex (4 bytes) + sig script var int (at least 1 byte)
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

	// MsgTxOverhead is 4 bytes version (lower 2 bytes for the real transaction
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

	// SchnorrSigLength is the length of a Schnorr signature.
	SchnorrSigLength = 65

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
	RedeemSwapSigScriptSize = 1 + DERSigLength + 1 + pubkeyLength + 1 + 32 + 1 + 2 + SwapContractSize // 241

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
	RefundSigScriptSize = 1 + DERSigLength + 1 + pubkeyLength + 1 + 2 + SwapContractSize // 208

	// RedeemPrivateSwapSigScriptSize is the size of a transaction input script that
	// redeems a private swap output.
	// It is calculated as:
	//
	//   - OP_DATA_64
	//   - 64 bytes Schnorr signature
	//   - OP_DATA_33
	//   - 33 bytes serialized compressed pubkey
	//   - OP_DATA_64
	//   - 64 bytes Schnorr signature
	//   - OP_DATA_33
	//   - 33 bytes serialized compressed pubkey
	//   - OP_1
	//   - OP_DATA_62
	//   - 62 bytes contract script
	RedeemPrivateSwapSigScriptSize = 2*(1+SchnorrSigLength+1+pubkeyLength) + 1 + PrivateSwapContractSize // 264

	// RefundPrivateScriptSigSize is the size of a transaction input script that
	// refunds a private swap output.
	// It is calculated as:
	//   - OP_DATA_64
	//   - 64 bytes Schnorr signature
	//   - OP_DATA_33
	//   - 33 bytes serialized compressed pubkey
	//   - OP_0
	//   - OP_DATA_62
	//   - 62 bytes contract script
	RefundPrivateScriptSigSize = 1 + SchnorrSigLength + 1 + pubkeyLength + 1 + 1 + PrivateSwapContractSize // 164

	// BondScriptSize is the maximum size of a DEX time-locked fidelity bond
	// output script to which a bond P2SH pays:
	//   OP_DATA_4/5 (4/5 bytes lockTime) OP_CHECKLOCKTIMEVERIFY OP_DROP OP_DUP OP_HASH160 OP_DATA_20 (20-byte pubkey hash160) OP_EQUALVERIFY OP_CHECKSIG
	BondScriptSize = 1 + 5 + 1 + 1 + 1 + 1 + 1 + 20 + 1 + 1 // 33

	// RedeemBondSigScriptSize is the worst case size of a fidelity bond
	// signature script that spends a bond output. It includes a signature, a
	// compressed pubkey, and the bond script. Each of said data pushes use an
	// OP_DATA_ code.
	RedeemBondSigScriptSize = 1 + DERSigLength + 1 + pubkeyLength + 1 + BondScriptSize // 142

	// BondPushDataSize is the size of the nulldata in a bond commitment output:
	//  OP_RETURN <pushData: ver[2] | account_id[32] | lockTime[4] | pkh[20]>
	BondPushDataSize = 2 + account.HashSize + 4 + 20
)

// redeemP2SHTxSize calculates the size of the redeeming transaction for a
// P2SH transaction with the given sigScipt (or witness data) size.
func redeemP2SHTxSize(redeemSigScriptSize uint64) uint64 {
	inputSize := TxInOverhead + redeemSigScriptSize
	return MsgTxOverhead + inputSize + P2PKHOutputSize
}

// redeemSwapTxSize returns the size of a swap refund tx.
func redeemSwapTxSize() uint64 {
	return redeemP2SHTxSize(RedeemSwapSigScriptSize)
}

// refundBondTxSize returns the size of a bond refund tx.
func refundBondTxSize() uint64 {
	return redeemP2SHTxSize(RedeemBondSigScriptSize)
}

// minHTLCValue calculates the minimum value for the output of a chained
// P2SH -> P2WPKH transaction pair where the spending tx size is known.
func minHTLCValue(maxFeeRate, redeemTxSize uint64) uint64 {
	// Reversing IsDustVal.
	// totalSize adds some buffer for the spending transaction.
	var outputSize uint64 = P2PKHOutputSize // larger of bonds p2sh output and refund's p2pkh output.
	totalSize := outputSize + 165
	minInitTxValue := maxFeeRate * totalSize * 3

	// The minInitTxValue would get the bond tx accepted, but when we go to
	// refund, we need that output to pass too, So let's add the fees for the
	// refund transaction.
	redeemFees := redeemTxSize * maxFeeRate
	return minInitTxValue + redeemFees
}

// MinBondSize is the minimum bond size that avoids dust for a given max network
// fee rate.
func MinBondSize(maxFeeRate uint64) uint64 {
	refundTxSize := refundBondTxSize()
	return minHTLCValue(maxFeeRate, refundTxSize)
}

// MinLotSize is the minimum lot size that avoids dust for a given max network
// fee rate.
func MinLotSize(maxFeeRate uint64) uint64 {
	redeemSize := redeemSwapTxSize()
	return minHTLCValue(maxFeeRate, redeemSize)
}

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
func AddressSigType(addr stdaddr.Address) (sigType btcec.SignatureType, err error) {
	switch addr.(type) {
	case *stdaddr.AddressPubKeyEcdsaSecp256k1V0:
		sigType = btcec.STEcdsaSecp256k1
	case *stdaddr.AddressPubKeyHashEcdsaSecp256k1V0:
		sigType = btcec.STEcdsaSecp256k1

	case *stdaddr.AddressPubKeyEd25519V0:
		sigType = btcec.STEd25519
	case *stdaddr.AddressPubKeyHashEd25519V0:
		sigType = btcec.STEd25519

	case *stdaddr.AddressPubKeySchnorrSecp256k1V0:
		sigType = btcec.STSchnorrSecp256k1
	case *stdaddr.AddressPubKeyHashSchnorrSecp256k1V0:
		sigType = btcec.STSchnorrSecp256k1

	default:
		err = fmt.Errorf("unsupported signature type")
	}
	return sigType, err
}

// MakePrivateContract creates a private swap contract that can be redeemed
// using both the redeemer and refunder's signatures, or just the refunder's
// signature if the locktime has been reached.
func MakePrivateContract(redeemPKH, refundPKH []byte, locktime int64) ([]byte, error) {
	if len(redeemPKH) != 20 || len(refundPKH) != 20 {
		return nil, fmt.Errorf("invalid PKH size")
	}

	b := txscript.NewScriptBuilder()

	b.AddOp(txscript.OP_IF) // Normal redeem path
	{
		b.AddOp(txscript.OP_DUP)
		b.AddOp(txscript.OP_HASH160)
		b.AddData(redeemPKH[:])
		b.AddOp(txscript.OP_EQUALVERIFY)
		b.AddOp(txscript.OP_2)
		b.AddOp(txscript.OP_CHECKSIGALTVERIFY)
	}
	b.AddOp(txscript.OP_ELSE) // Refund path
	{
		b.AddInt64(locktime)
		b.AddOp(txscript.OP_CHECKLOCKTIMEVERIFY)
		b.AddOp(txscript.OP_DROP)
	}
	b.AddOp(txscript.OP_ENDIF)
	b.AddOp(txscript.OP_DUP)
	b.AddOp(txscript.OP_HASH160)
	b.AddData(refundPKH[:])
	b.AddOp(txscript.OP_EQUALVERIFY)
	b.AddOp(txscript.OP_2)
	b.AddOp(txscript.OP_CHECKSIGALT)

	script, err := b.Script()
	if err != nil {
		return nil, err
	}

	return script, nil
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

// RedeemP2SHPrivateContract returns the signature script using both the
// redeemer and refunder's signatures. This function assumes P2SH and appends
// the contract as the final data push.
func RedeemP2SHPrivateContract(contract, redeemPK, redeemSig, refundPK, refundSig []byte) ([]byte, error) {
	return txscript.NewScriptBuilder().
		AddData(refundSig).
		AddData(refundPK).
		AddData(redeemSig).
		AddData(redeemPK).
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

// OP_RETURN <pushData: ver[2] | account_id[32] | lockTime[4] | pkh[20]>
func extractBondCommitDataV0(pushData []byte) (acct account.AccountID, lockTime uint32, pubkeyHash [20]byte, err error) {
	if len(pushData) < 2 {
		err = errors.New("invalid data")
		return
	}
	ver := binary.BigEndian.Uint16(pushData)
	if ver != 0 {
		err = fmt.Errorf("unexpected bond commitment version %d, expected 0", ver)
		return
	}

	if len(pushData) != BondPushDataSize {
		err = fmt.Errorf("invalid bond commitment output script length: %d", len(pushData))
		return
	}

	pushData = pushData[2:] // pop off ver

	copy(acct[:], pushData)
	pushData = pushData[account.HashSize:]

	lockTime = binary.BigEndian.Uint32(pushData)
	pushData = pushData[4:]

	copy(pubkeyHash[:], pushData)

	return
}

// ExtractBondCommitDataV0 parses a v0 bond commitment output script. This is
// the OP_RETURN output, not the P2SH bond output. Use ExtractBondDetailsV0 to
// parse the P2SH bond output's redeem script.
//
// If the decoded commitment data indicates a version other than 0, an error is
// returned.
func ExtractBondCommitDataV0(scriptVer uint16, pkScript []byte) (acct account.AccountID, lockTime uint32, pubkeyHash [20]byte, err error) {
	tokenizer := txscript.MakeScriptTokenizer(scriptVer, pkScript)
	if !tokenizer.Next() {
		err = tokenizer.Err()
		return
	}

	if tokenizer.Opcode() != txscript.OP_RETURN {
		err = errors.New("not a null data output")
		return
	}

	if !tokenizer.Next() {
		err = tokenizer.Err()
		return
	}

	pushData := tokenizer.Data()
	acct, lockTime, pubkeyHash, err = extractBondCommitDataV0(pushData)
	if err != nil {
		return
	}

	if !tokenizer.Done() {
		err = errors.New("script has extra opcodes")
		return
	}

	return
}

// MakeBondScript constructs a versioned bond output script for the provided
// lock time and pubkey hash. Only version 0 is supported at present. The lock
// time must be less than 2^32-1 so that it uses at most 5 bytes. The lockTime
// is also required to use at least 4 bytes (time stamp, not block time).
func MakeBondScript(ver uint16, lockTime uint32, pubkeyHash []byte) ([]byte, error) {
	if ver != 0 {
		return nil, errors.New("only version 0 bonds supported")
	}
	if lockTime >= MaxCLTVScriptNum { // == should be OK, but let's not
		return nil, errors.New("invalid lock time")
	}
	lockTimeBytes := txscript.ScriptNum(lockTime).Bytes()
	if n := len(lockTimeBytes); n < 4 || n > 5 {
		return nil, errors.New("invalid lock time")
	}
	if len(pubkeyHash) != 20 {
		return nil, errors.New("invalid pubkey hash")
	}
	return txscript.NewScriptBuilder().
		AddData(lockTimeBytes).
		AddOp(txscript.OP_CHECKLOCKTIMEVERIFY).
		AddOp(txscript.OP_DROP).
		AddOp(txscript.OP_DUP).
		AddOp(txscript.OP_HASH160).
		AddData(pubkeyHash).
		AddOp(txscript.OP_EQUALVERIFY).
		AddOp(txscript.OP_CHECKSIG).
		Script()
}

// RefundBondScript builds the signature script to refund a time-locked fidelity
// bond in a P2SH output paying to the provided P2PKH bondScript.
func RefundBondScript(bondScript, sig, pubkey []byte) ([]byte, error) {
	return txscript.NewScriptBuilder().
		AddData(sig).
		AddData(pubkey).
		AddData(bondScript).
		Script()
}

// ExtractBondDetailsV0 validates the provided bond redeem script, extracting
// the lock time and pubkey. The V0 format of the script must be as follows:
//
//	<lockTime> OP_CHECKLOCKTIMEVERIFY OP_DROP OP_DUP OP_HASH160 <pubkeyhash[20]> OP_EQUALVERIFY OP_CHECKSIG
//
// The script version refers to the pkScript version, not bond version, which
// pertains to DEX's version of the bond script.
func ExtractBondDetailsV0(scriptVersion uint16, bondScript []byte) (lockTime uint32, pkh []byte, err error) {
	type templateMatch struct {
		expectInt     bool
		maxIntBytes   int
		opcode        byte
		extractedInt  int64
		extractedData []byte
	}
	var template = [...]templateMatch{
		{expectInt: true, maxIntBytes: txscript.CltvMaxScriptNumLen}, // extractedInt
		{opcode: txscript.OP_CHECKLOCKTIMEVERIFY},
		{opcode: txscript.OP_DROP},
		{opcode: txscript.OP_DUP},
		{opcode: txscript.OP_HASH160},
		{opcode: txscript.OP_DATA_20}, // extractedData
		{opcode: txscript.OP_EQUALVERIFY},
		{opcode: txscript.OP_CHECKSIG},
	}

	var templateOffset int
	tokenizer := txscript.MakeScriptTokenizer(scriptVersion, bondScript)
	for tokenizer.Next() {
		if templateOffset >= len(template) {
			return 0, nil, errors.New("too many script elements")
		}

		op, data := tokenizer.Opcode(), tokenizer.Data()
		tplEntry := &template[templateOffset]
		if tplEntry.expectInt {
			switch {
			case data != nil:
				val, err := txscript.MakeScriptNum(data, tplEntry.maxIntBytes)
				if err != nil {
					return 0, nil, err
				}
				tplEntry.extractedInt = int64(val)

			case txscript.IsSmallInt(op): // not expected for our lockTimes, but it is an integer
				tplEntry.extractedInt = int64(txscript.AsSmallInt(op))

			default:
				return 0, nil, errors.New("expected integer")
			}
		} else {
			if op != tplEntry.opcode {
				return 0, nil, fmt.Errorf("expected opcode %v, got %v", tplEntry.opcode, op)
			}

			tplEntry.extractedData = data
		}

		templateOffset++
	}
	if err := tokenizer.Err(); err != nil {
		return 0, nil, err
	}
	if !tokenizer.Done() || templateOffset != len(template) {
		return 0, nil, errors.New("incorrect script length")
	}

	// The script matches in structure. Now validate the two pushes.

	lockTime64 := template[0].extractedInt
	if lockTime64 <= 0 || lockTime64 > MaxCLTVScriptNum {
		return 0, nil, fmt.Errorf("invalid locktime %d", lockTime64)
	}
	lockTime = uint32(lockTime64)

	const pubkeyHashLen = 20
	bondPubKeyHash := template[5].extractedData
	if len(bondPubKeyHash) != pubkeyHashLen {
		err = errors.New("missing or invalid pubkeyhash data")
		return
	}
	pkh = make([]byte, pubkeyHashLen)
	copy(pkh, bondPubKeyHash)

	return
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

// ExtractPrivateSwapDetails extracts the pub key hashes of the refunder and
// redeemer, and the locktime from a private swap contract.
func ExtractPrivateSwapDetails(script []byte) (refunder, redeemer [ripemd160.Size]byte, locktime int64, err error) {
	if len(script) != 62 {
		err = fmt.Errorf("invalid swap contract: length = %v", len(script))
		return
	}

	if script[0] == txscript.OP_IF &&
		script[1] == txscript.OP_DUP &&
		script[2] == txscript.OP_HASH160 &&
		script[3] == txscript.OP_DATA_20 &&
		// creator's pkh (20 bytes)
		script[24] == txscript.OP_EQUALVERIFY &&
		script[25] == txscript.OP_2 &&
		script[26] == txscript.OP_CHECKSIGALTVERIFY &&
		script[27] == txscript.OP_ELSE &&
		script[28] == txscript.OP_DATA_4 &&
		// lockTime (4 bytes)
		script[33] == txscript.OP_CHECKLOCKTIMEVERIFY &&
		script[34] == txscript.OP_DROP &&
		script[35] == txscript.OP_ENDIF &&
		script[36] == txscript.OP_DUP &&
		script[37] == txscript.OP_HASH160 &&
		script[38] == txscript.OP_DATA_20 &&
		// participant's pkh (20 bytes)
		script[59] == txscript.OP_EQUALVERIFY &&
		script[60] == txscript.OP_2 &&
		script[61] == txscript.OP_CHECKSIGALT {
		copy(redeemer[:], script[4:24])
		copy(refunder[:], script[39:59])
		locktime = int64(binary.LittleEndian.Uint32(script[29:33]))
	} else {
		err = fmt.Errorf("invalid swap contract")
	}

	return
}

// IsDust returns whether or not the passed transaction output amount is
// considered dust or not based on the passed minimum transaction relay fee.
// Dust is defined in terms of the minimum transaction relay fee.
// See lbcd/mempool/policy isDust for further documentation, though this version
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
func IsDustVal(txOutSize, amt, minRelayTxFee uint64) bool {
	totalSize := txOutSize + 165
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

// IsRefundScript checks if the signature script is of the expected format for
// the standard swap contract refund. The signature and pubkey data pushes are
// not validated other than ensuring they are data pushes. The provided contract
// must correspond to the final data push in the sigScript, but it is otherwise
// not validated either. Both the HTLC and atomic signature swaps have the same
// refund script format, just the signature type and contracts are different.
func IsRefundScript(scriptVersion uint16, sigScript, contract []byte, privateRefund bool) bool {
	tokenizer := txscript.MakeScriptTokenizer(scriptVersion, sigScript)
	// sig
	if !tokenizer.Next() {
		return false
	}
	if tokenizer.Data() == nil {
		return false // should be a der signature for HTLC swaps or a schnorr signature for atomic signature swaps
	}

	// pubkey
	if !tokenizer.Next() {
		return false
	}
	if tokenizer.Data() == nil {
		return false // should be a pubkey
	}

	// OP_0
	if !tokenizer.Next() {
		return false
	}
	if tokenizer.Opcode() != txscript.OP_0 {
		return false
	}

	// contract script
	if !tokenizer.Next() {
		return false
	}
	expContractLength := SwapContractSize
	if privateRefund {
		expContractLength = PrivateSwapContractSize
	}
	push := tokenizer.Data()
	if len(push) != expContractLength {
		return false
	}
	if !bytes.Equal(push, contract) {
		return false
	}

	return tokenizer.Done()
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
			h := btcutil.Hash160(push)
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
