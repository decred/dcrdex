// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package btc

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"

	"decred.org/dcrdex/dex"
	"github.com/btcsuite/btcd/blockchain"
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/btcsuite/btcd/txscript"
	"github.com/btcsuite/btcd/wire"
	"github.com/btcsuite/btcutil"
)

const (
	// SecretHashSize is the byte-length of the hash of the secret key used in an
	// atomic swap.
	SecretHashSize = 32

	// SecretKeySize is the byte-length of the secret key used in an atomic swap.
	SecretKeySize = 32

	// ContractHashSize is the size of the script-hash for an atomic swap
	// contract.
	ContractHashSize = 20

	// PubKeyLength is the length of a serialized compressed public key.
	PubKeyLength = 33

	// 4 bytes version + 4 bytes locktime + 2 bytes of varints for the number of
	// transaction inputs and outputs
	MinimumBlockOverHead = 10

	// SwapContractSize is the worst case scenario size for a swap contract,
	// which is the pk-script of the non-change output of an initialization
	// transaction as used in execution of an atomic swap.
	// See ExtractSwapDetails for a breakdown of the bytes.
	SwapContractSize = 97

	// DERSigLength is the maximum length of a DER encoded signature with a
	// sighash type byte.
	DERSigLength = 73

	// RedeemSwapSigScriptSize is the worst case (largest) serialize size
	// of a transaction signature script that redeems atomic swap output contract.
	// It is calculated as:
	//
	//   - OP_DATA_73
	//   - 72 bytes DER signature + 1 byte sighash
	//   - OP_DATA_33
	//   - 33 bytes serialized compressed pubkey
	//   - OP_DATA_32
	//   - 32 bytes secret key
	//   - OP_1
	//   - varint 97
	//   - 97 bytes redeem script
	RedeemSwapSigScriptSize = 1 + DERSigLength + 1 + 33 + 1 + 32 + 1 + 2 + 97

	// RefundSigScriptSize is the worst case (largest) serialize size
	// of a transaction input script that refunds a compressed P2PKH output.
	// It is calculated as:
	//
	//   - OP_DATA_73
	//   - 72 bytes DER signature + 1 byte sighash
	//   - OP_DATA_33
	//   - 33 bytes serialized compressed pubkey
	//   - OP_0
	//   - varint 97 => OP_PUSHDATA1(0x4c) + 0x61
	//   - 97 bytes contract
	RefundSigScriptSize = 1 + DERSigLength + 1 + 33 + 1 + 2 + 97

	// Overhead for a wire.TxIn. See wire.TxIn.SerializeSize.
	// hash 32 bytes + index 4 bytes + sequence 4 bytes.
	TxInOverhead = 32 + 4 + 4 // 40

	// TxOutOverhead is the overhead associated with a transaction output.
	// 8 bytes value + at least 1 byte varint script size
	TxOutOverhead = 8 + 1

	RedeemP2PKSigScriptSize = 1 + DERSigLength

	// RedeemP2PKHSigScriptSize is the worst case (largest) serialize size
	// of a transaction input script that redeems a compressed P2PKH output.
	// It is calculated as:
	//
	//   - OP_DATA_73
	//   - 72 bytes DER signature + 1 byte sighash
	//   - OP_DATA_33
	//   - 33 bytes serialized compressed pubkey
	RedeemP2PKHSigScriptSize = 1 + DERSigLength + 1 + 33 // 108

	// RedeemP2SHSigScriptSize does not include the redeem script.
	//RedeemP2SHSigScriptSize = 1 + DERSigLength + 1 + 1 + 33 + 1 // + redeem script!

	// P2PKHPkScriptSize is the size of a transaction output script that
	// pays to a compressed pubkey hash.  It is calculated as:
	//
	//   - OP_DUP
	//   - OP_HASH160
	//   - OP_DATA_20
	//   - 20 bytes pubkey hash
	//   - OP_EQUALVERIFY
	//   - OP_CHECKSIG
	P2PKHPkScriptSize = 1 + 1 + 1 + 20 + 1 + 1 // 25

	// P2PKHOutputSize is the size of the serialized P2PKH output.
	P2PKHOutputSize = TxOutOverhead + P2PKHPkScriptSize // 9 + 25 = 34

	// P2SHPkScriptSize is the size of a transaction output script that
	// pays to a redeem script.  It is calculated as:
	//
	//   - OP_HASH160
	//   - OP_DATA_20
	//   - 20 bytes redeem script hash
	//   - OP_EQUAL
	P2SHPkScriptSize = 1 + 1 + 20 + 1

	// P2SHOutputSize is the size of the serialized P2SH output.
	P2SHOutputSize = TxOutOverhead + P2SHPkScriptSize // 9 + 23 = 32

	// P2WSHPkScriptSize is the size of a segwit transaction output script that
	// pays to a redeem script.  It is calculated as:
	//
	//   - OP_0
	//   - OP_DATA_32
	//   - 32 bytes redeem script hash
	P2WSHPkScriptSize = 1 + 1 + 32

	// P2WSHOutputSize is the size of the serialized P2WSH output.
	P2WSHOutputSize = TxOutOverhead + P2WSHPkScriptSize // 9 + 34 = 43

	// RedeemP2PKHInputSize is the worst case (largest) serialize size of a
	// transaction input redeeming a compressed P2PKH output.  It is
	// calculated as:
	//
	//   - 32 bytes previous tx
	//   - 4 bytes output index
	//   - 4 bytes sequence
	//   - 1 byte compact int encoding value 108
	//   - 108 bytes signature script
	RedeemP2PKHInputSize = TxInOverhead + 1 + RedeemP2PKHSigScriptSize // 40 + 1 + 108 = 149

	// RedeemP2WPKHInputSize is the worst case size of a transaction
	// input redeeming a P2WPKH output. This does not account for witness data,
	// which is considered at a lower weight for fee calculations. It is
	// calculated as
	//
	//   - 32 bytes previous tx
	//   - 4 bytes output index
	//   - 4 bytes sequence
	//   - 1 byte encoding empty redeem script
	//   - 0 bytes signature script
	RedeemP2WPKHInputSize = TxInOverhead + 1

	// RedeemP2WPKHInputWitnessWeight is the worst case weight of
	// a witness for spending P2WPKH and nested P2WPKH outputs. It
	// is calculated as:
	//
	//   - 1 wu compact int encoding value 2 (number of items)
	//   - 1 wu compact int encoding value 73
	//   - 72 wu DER signature + 1 wu sighash
	//   - 1 wu compact int encoding value 33
	//   - 33 wu serialized compressed pubkey
	// NOTE: witness data is not script.
	RedeemP2WPKHInputWitnessWeight = 1 + 1 + DERSigLength + 1 + 33 // 109

	// RedeemP2WSHInputWitnessWeight depends on the number of redeem scrpit and
	// number of signatures.
	//  version + signatures + length of redeem script + redeemscript
	// RedeemP2WSHInputWitnessWeight = 1 + N*DERSigLength + 1 + (redeem script bytes)

	// P2WPKHPkScriptSize is the size of a transaction output script that
	// pays to a witness pubkey hash. It is calculated as:
	//
	//   - OP_0
	//   - OP_DATA_20
	//   - 20 bytes pubkey hash
	P2WPKHPkScriptSize = 1 + 1 + 20

	// P2WPKHOutputSize is the serialize size of a transaction output with a
	// P2WPKH output script. It is calculated as:
	//
	//   - 8 bytes output value
	//   - 1 byte compact int encoding value 22
	//   - 22 bytes P2PKH output script
	P2WPKHOutputSize = TxOutOverhead + P2WPKHPkScriptSize // 31

	// MinimumTxOverhead is the size of an empty transaction.
	// 4 bytes version + 4 bytes locktime + 2 bytes of varints for the number of
	// transaction inputs and outputs
	MinimumTxOverhead = 4 + 4 + 1 + 1 // 10

	// InitTxSizeBase is the size of a standard serialized atomic swap
	// initialization transaction with one change output and no inputs. This is
	// MsgTx overhead + 1 P2PKH change output + 1 P2SH contract output. However,
	// the change output might be P2WPKH, in which case it would be smaller.
	InitTxSizeBase = MinimumTxOverhead + P2PKHOutputSize + P2SHOutputSize // 10 + 34 + 32 = 76
	// leaner with P2WPKH+P2SH outputs: 10 + 31 + 32 = 73

	// InitTxSize is InitTxBaseSize + 1 P2PKH input
	InitTxSize = InitTxSizeBase + RedeemP2PKHInputSize // 76 + 149 = 225
	// Varies greatly with some other input types, e.g nested witness (p2sh with
	// p2wpkh redeem script): 23 byte scriptSig + 108 byte (75 vbyte) witness = ~50

	// InitTxSizeBaseSegwit is the size of a standard serialized atomic swap
	// initialization transaction with one change output and no inputs. The
	// change output is assumed to be segwit. 10 + 31 + 43 = 84
	InitTxSizeBaseSegwit = MinimumTxOverhead + P2WPKHOutputSize + P2WSHOutputSize

	// InitTxSizeSegwit is InitTxSizeSegwit + 1 P2WPKH input.
	// 84 vbytes base tx
	// 41 vbytes base tx input
	// 109wu witness +  2wu segwit marker and flag = 28 vbytes
	// total = 153 vbytes
	InitTxSizeSegwit = InitTxSizeBaseSegwit + RedeemP2WPKHInputSize + ((RedeemP2WPKHInputWitnessWeight + 2 + 3) / 4)

	witnessWeight = blockchain.WitnessScaleFactor
)

// BTCScriptType holds details about a pubkey script and possibly it's redeem
// script.
type BTCScriptType uint8

const (
	ScriptP2PKH = 1 << iota
	ScriptP2SH
	ScriptTypeSegwit
	ScriptMultiSig
	ScriptUnsupported
)

// IsP2SH will return boolean true if the script is a P2SH script.
func (s BTCScriptType) IsP2SH() bool {
	return s&ScriptP2SH != 0 && s&ScriptTypeSegwit == 0
}

// IsP2WSH will return boolean true if the script is a P2WSH script.
func (s BTCScriptType) IsP2WSH() bool {
	return s&ScriptP2SH != 0 && s&ScriptTypeSegwit != 0
}

// IsP2PKH will return boolean true if the script is a P2PKH script.
func (s BTCScriptType) IsP2PKH() bool {
	return s&ScriptP2PKH != 0 && s&ScriptTypeSegwit == 0
}

// IsP2WPKH will return boolean true if the script is a P2WPKH script.
func (s BTCScriptType) IsP2WPKH() bool {
	return s&ScriptP2PKH != 0 && s&ScriptTypeSegwit != 0
}

// IsSegwit will return boolean true if the script is a P2WPKH or P2WSH script.
func (s BTCScriptType) IsSegwit() bool {
	return s&ScriptTypeSegwit != 0
}

// IsMultiSig is whether the pkscript references a multi-sig redeem script.
// Since the DEX will know the redeem script, we can say whether it's multi-sig.
func (s BTCScriptType) IsMultiSig() bool {
	return s&ScriptMultiSig != 0
}

// ParseScriptType creates a BTCScriptType bitmap for the script type. A script
// type will be some combination of pay-to-pubkey-hash, pay-to-script-hash,
// and stake. If a script type is P2SH, it may or may not be mutli-sig.
func ParseScriptType(pkScript, redeemScript []byte) BTCScriptType {
	var scriptType BTCScriptType
	class := txscript.GetScriptClass(pkScript)

	switch class {
	case txscript.PubKeyHashTy:
		scriptType |= ScriptP2PKH
	case txscript.WitnessV0PubKeyHashTy:
		scriptType |= ScriptP2PKH | ScriptTypeSegwit
	case txscript.ScriptHashTy:
		scriptType |= ScriptP2SH
	case txscript.WitnessV0ScriptHashTy:
		scriptType |= ScriptP2SH | ScriptTypeSegwit
	default:
		return ScriptUnsupported
	}
	if scriptType.IsP2SH() || scriptType.IsP2WSH() {
		scriptClass := txscript.GetScriptClass(redeemScript)
		if scriptClass == txscript.MultiSigTy {
			scriptType |= ScriptMultiSig
		}
		if scriptClass == txscript.ScriptHashTy || scriptClass == txscript.WitnessV0ScriptHashTy {
			// nested p2sh-pswsh, p2sh-p2sh, etc are not supported.
			return ScriptUnsupported
		}
	}
	return scriptType
}

// MakeContract creates a segwit atomic swap contract. The secretHash MUST
// be computed from a secret of length SecretKeySize bytes or the resulting
// contract will be invalid.
func MakeContract(recipient, sender string, secretHash []byte, lockTime int64, segwit bool, chainParams *chaincfg.Params) ([]byte, error) {
	rAddr, err := btcutil.DecodeAddress(recipient, chainParams)
	if err != nil {
		return nil, fmt.Errorf("error decoding recipient address %s: %w", recipient, err)
	}
	sAddr, err := btcutil.DecodeAddress(sender, chainParams)
	if err != nil {
		return nil, fmt.Errorf("error decoding sender address %s: %w", sender, err)
	}
	if segwit {
		_, ok := rAddr.(*btcutil.AddressWitnessPubKeyHash)
		if !ok {
			return nil, fmt.Errorf("recipient address %s is not a witness-pubkey-hash address", recipient)
		}
		_, ok = sAddr.(*btcutil.AddressWitnessPubKeyHash)
		if !ok {
			return nil, fmt.Errorf("sender address %s is not a witness-pubkey-hash address", recipient)
		}
	} else {
		_, ok := rAddr.(*btcutil.AddressPubKeyHash)
		if !ok {
			return nil, fmt.Errorf("recipient address %s is not a witness-pubkey-hash address", recipient)
		}
		_, ok = sAddr.(*btcutil.AddressPubKeyHash)
		if !ok {
			return nil, fmt.Errorf("sender address %s is not a witness-pubkey-hash address", recipient)
		}
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
		AddInt64(lockTime).AddOps([]byte{
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

// IsDust returns whether or not the passed transaction output amount is
// considered dust or not based on the passed minimum transaction relay fee.
// Dust is defined in terms of the minimum transaction relay fee.
// Based on btcd/policy isDust. See btcd/policy for further documentation.
func IsDust(txOut *wire.TxOut, minRelayTxFee uint64) bool {
	if txscript.IsUnspendable(txOut.PkScript) {
		return true
	}
	totalSize := txOut.SerializeSize() + 41
	if txscript.IsWitnessProgram(txOut.PkScript) {
		// This function is taken from btcd, but noting here that we are not
		// rounding up and probably should be.
		totalSize += (107 / witnessWeight)
	} else {
		totalSize += 107
	}
	return txOut.Value/(3*int64(totalSize)) < int64(minRelayTxFee)
}

// BtcScriptAddrs is information about the pubkeys or pubkey hashes present in
// a scriptPubKey (and the redeem script, for p2sh). This information can be
// used to estimate the spend script size, e.g. pubkeys in a redeem script don't
// require pubkeys in the witness/scriptSig, but pubkey hashes do.
type BtcScriptAddrs struct {
	PubKeys   []btcutil.Address
	NumPK     int
	PkHashes  []btcutil.Address
	NumPKH    int
	NRequired int // num sigs needed
}

// ExtractScriptAddrs extracts the addresses from the pubkey script, or the
// redeem script if the pubkey script is P2SH. The returned bool indicates if
// the script is non-standard.
func ExtractScriptAddrs(script []byte, chainParams *chaincfg.Params) (*BtcScriptAddrs, bool, error) {
	pubkeys := make([]btcutil.Address, 0)
	pkHashes := make([]btcutil.Address, 0)
	// For P2SH and non-P2SH multi-sig, pull the addresses from the pubkey script.
	class, addrs, numRequired, err := txscript.ExtractPkScriptAddrs(script, chainParams)
	nonStandard := class == txscript.NonStandardTy
	if err != nil {
		return nil, nonStandard, fmt.Errorf("ExtractScriptAddrs: %w", err)
	}
	if nonStandard {
		return &BtcScriptAddrs{}, nonStandard, nil
	}
	for _, addr := range addrs {
		// If the address is an unhashed public key, is won't need a pubkey as part
		// of its sigScript, so count them separately.
		_, isPubkey := addr.(*btcutil.AddressPubKey)
		if isPubkey {
			pubkeys = append(pubkeys, addr)
		} else {
			pkHashes = append(pkHashes, addr)
		}
	}
	return &BtcScriptAddrs{
		PubKeys:   pubkeys,
		NumPK:     len(pubkeys),
		PkHashes:  pkHashes,
		NumPKH:    len(pkHashes),
		NRequired: numRequired,
	}, false, nil
}

// ExtractSwapDetails extacts the sender and receiver addresses from a swap
// contract. If the provided script is not a swap contract, an error will be
// returned.
func ExtractSwapDetails(pkScript []byte, segwit bool, chainParams *chaincfg.Params) (
	sender, receiver btcutil.Address, lockTime uint64, secretHash []byte, err error) {
	// A swap redemption sigScript is <pubkey> <secret> and satisfies the
	// following swap contract.
	//
	// OP_IF
	//  OP_SIZE OP_DATA_1 secretSize OP_EQUALVERIFY OP_SHA256 OP_DATA_32 secretHash OP_EQUALVERIFY OP_DUP OP_HASH160 OP_DATA20 pkHashReceiver
	//     1   +   1     +    1     +      1       +    1    +    1     +   32     +      1       +   1  +    1     +    1    +    20
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

		if segwit {
			receiver, err = btcutil.NewAddressWitnessPubKeyHash(pkScript[43:63], chainParams)
			if err != nil {
				return nil, nil, 0, nil, fmt.Errorf("error decoding address from recipient's pubkey hash")
			}

			sender, err = btcutil.NewAddressWitnessPubKeyHash(pkScript[74:94], chainParams)
			if err != nil {
				return nil, nil, 0, nil, fmt.Errorf("error decoding address from sender's pubkey hash")
			}
		} else {
			receiver, err = btcutil.NewAddressPubKeyHash(pkScript[43:63], chainParams)
			if err != nil {
				return nil, nil, 0, nil, fmt.Errorf("error decoding address from recipient's pubkey hash")
			}

			sender, err = btcutil.NewAddressPubKeyHash(pkScript[74:94], chainParams)
			if err != nil {
				return nil, nil, 0, nil, fmt.Errorf("error decoding address from sender's pubkey hash")
			}
		}

		lockTime = uint64(binary.LittleEndian.Uint32(pkScript[65:69]))
		secretHash = pkScript[7:39]

		return
	}

	err = fmt.Errorf("invalid swap contract")
	return
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

// RedeemP2WSHContract returns the witness script to redeem a contract output
// using the redeemer's signature and the initiator's secret.  This function
// assumes P2WSH and appends the contract as the final data push.
func RedeemP2WSHContract(contract, sig, pubkey, secret []byte) [][]byte {
	return [][]byte{
		sig,
		pubkey,
		secret,
		{0x01},
		contract,
	}
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

// MsgTxVBytes returns the transaction's virtual size, which accounts for the
// segwit input weighting.
func MsgTxVBytes(msgTx *wire.MsgTx) uint64 {
	baseSize := msgTx.SerializeSizeStripped()
	totalSize := msgTx.SerializeSize()
	txWeight := baseSize*(witnessWeight-1) + totalSize
	// vbytes is ceil(tx_weight/4)
	return uint64(txWeight+(witnessWeight-1)) / witnessWeight // +3 before / 4 to round up
	// NOTE: This is the same as doing the following:
	// tx := btcutil.NewTx(msgTx)
	// return uint64(blockchain.GetTransactionWeight(tx)+(blockchain.WitnessScaleFactor-1)) /
	// 	blockchain.WitnessScaleFactor // mempool.GetTxVirtualSize(tx)
}

// RefundP2WSHContract returns the witness to refund a contract output
// using the contract author's signature after the locktime has been reached.
// This function assumes P2WSH and appends the contract as the final data push.
func RefundP2WSHContract(contract, sig, pubkey []byte) [][]byte {
	return [][]byte{
		sig,
		pubkey,
		{},
		contract,
	}
}

// SpendInfo is information about an input and it's previous outpoint.
type SpendInfo struct {
	SigScriptSize     uint32
	WitnessSize       uint32
	ScriptAddrs       *BtcScriptAddrs
	ScriptType        BTCScriptType
	NonStandardScript bool
}

// VBytes is the virtual bytes of the spending transaction input. Miners use
// weight/4 = vbytes to calculate contribution to block size limit and therefore
// transaction fees.
func (nfo *SpendInfo) VBytes() uint32 {
	return TxInOverhead + uint32(wire.VarIntSerializeSize(uint64(nfo.SigScriptSize))) +
		nfo.SigScriptSize + (nfo.WitnessSize+(witnessWeight-1))/witnessWeight // Add 3 before dividing witness weight to force rounding up.
}

// InputInfo is some basic information about the input required to spend an
// output. The pubkey script of the output is provided. If the pubkey script
// parses as P2SH or P2WSH, the redeem script must be provided.
func InputInfo(pkScript, redeemScript []byte, chainParams *chaincfg.Params) (*SpendInfo, error) {
	// Get information about the signatures and pubkeys needed to spend the utxo.
	scriptType := ParseScriptType(pkScript, redeemScript)
	if scriptType == ScriptUnsupported {
		return nil, dex.UnsupportedScriptError
	}
	evalScript := pkScript
	if scriptType.IsP2SH() || scriptType.IsP2WSH() {
		if len(redeemScript) == 0 {
			return nil, fmt.Errorf("no redeem script provided for p2sh or p2wsh address")
		}
		evalScript = redeemScript
	}
	scriptAddrs, nonStandard, err := ExtractScriptAddrs(evalScript, chainParams)
	if err != nil {
		return nil, fmt.Errorf("error parsing script addresses: %w", err)
	}
	if nonStandard {
		return &SpendInfo{
			// SigScriptSize and WitnessSizecannot be determined, leave zero.
			ScriptAddrs:       scriptAddrs,
			ScriptType:        scriptType,
			NonStandardScript: true,
		}, nil
	}

	// Start with standard P2PKH.
	var sigScriptSize, witnessWeight int
	switch {
	case scriptType.IsP2PKH():
		sigScriptSize = RedeemP2PKHSigScriptSize
		witnessWeight = 0
	case scriptType.IsP2WPKH():
		sigScriptSize = 0
		witnessWeight = RedeemP2WPKHInputWitnessWeight
	case scriptType.IsP2SH():
		// If it's a P2SH, the size must be calculated based on other factors.

		// If the redeem script is P2SM, there is a leading OP_0 in Bitcoin.
		if scriptType.IsMultiSig() {
			sigScriptSize++
		}

		// The signatures.
		sigScriptSize += (DERSigLength + 1) * scriptAddrs.NRequired // 73 max for sig, 1 for push code

		// If there are pubkey-hash addresses, they'll need pubkeys.
		if scriptAddrs.NumPKH > 0 {
			sigScriptSize += scriptAddrs.NRequired * (PubKeyLength + 1)
		}

		// Then add the script_length(1) + len(script).
		sigScriptSize += len(redeemScript) + 1
	case scriptType.IsP2WSH():
		witnessWeight = (DERSigLength + 1) * scriptAddrs.NRequired
		if scriptAddrs.NumPKH > 0 {
			witnessWeight += scriptAddrs.NRequired * (PubKeyLength + 1)
		}
		witnessWeight += len(redeemScript) + 1
	default:
		return nil, fmt.Errorf("error calculating size. unknown script type %d?", scriptType)
	}
	return &SpendInfo{
		SigScriptSize: uint32(sigScriptSize),
		WitnessSize:   uint32(witnessWeight),
		ScriptAddrs:   scriptAddrs,
		ScriptType:    scriptType,
	}, nil
}

// FindKeyPush attempts to extract the secret key from the signature script. The
// contract must be provided for the search algorithm to verify the correct data
// push. Only contracts of length SwapContractSize that can be validated by
// ExtractSwapDetails are recognized.
func FindKeyPush(txIn *wire.TxIn, contractHash []byte, segwit bool, chainParams *chaincfg.Params) ([]byte, error) {
	var redeemScript, secret []byte
	var hasher func([]byte) []byte
	if segwit {
		if len(txIn.Witness) != 5 {
			return nil, fmt.Errorf("sigScript should contain 5 data pushes. Found %d", len(txIn.Witness))
		}
		secret, redeemScript = txIn.Witness[2], txIn.Witness[4]
		hasher = func(b []byte) []byte {
			h := sha256.Sum256(b)
			return h[:]
		}
	} else {
		pushes, err := txscript.PushedData(txIn.SignatureScript)
		if err != nil {
			return nil, fmt.Errorf("sigScript PushedData(%x): %w", txIn.SignatureScript, err)
		}
		if len(pushes) != 4 {
			return nil, fmt.Errorf("sigScript should contain 4 data pushes. Found %d", len(pushes))
		}
		secret, redeemScript = pushes[2], pushes[3]
		hasher = btcutil.Hash160
	}
	return findKeyPush(redeemScript, secret, contractHash, chainParams, hasher)
}

func findKeyPush(redeemScript, secret, contractHash []byte, chainParams *chaincfg.Params, hasher func([]byte) []byte) ([]byte, error) {
	// Doesn't matter what we pass for the bool segwit argument, since we're not
	// using the addresses, and the contract's 20-byte pubkey hashes are
	// compatible with both address types.
	_, _, _, keyHash, err := ExtractSwapDetails(redeemScript, true, chainParams)
	if err != nil {
		return nil, fmt.Errorf("error extracting atomic swap details: %w", err)
	}
	if !bytes.Equal(hasher(redeemScript), contractHash) {
		return nil, fmt.Errorf("redeemScript is not correct for provided contract hash")
	}
	h := sha256.Sum256(secret)
	if !bytes.Equal(h[:], keyHash) {
		return nil, fmt.Errorf("incorrect secret")
	}
	return secret, nil
}

// ExtractPubKeyHash extracts the pubkey hash from the passed script if it is a
// standard pay-to-pubkey-hash script.  It will return nil otherwise.
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

// ExtractContractHash extracts the contract P2SH or P2WSH address from a
// pkScript. A non-nil error is returned if the hexadecimal encoding of the
// pkScript is invalid, or if the pkScript is not a P2SH or P2WSH script.
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

// ExtractScriptHash attempts to extract the redeem script hash from a pkScript.
// If it is not a P2SH or P2WSH pkScript, a nil slice is returned.
func ExtractScriptHash(script []byte) []byte {
	// A pay-to-script-hash pkScript is of the form:
	//  OP_HASH160 <20-byte scripthash> OP_EQUAL
	if len(script) == 23 &&
		script[0] == txscript.OP_HASH160 &&
		script[1] == txscript.OP_DATA_20 &&
		script[22] == txscript.OP_EQUAL {

		return script[2:22]
	}
	// A pay-to-witness-script-hash pkScript is of the form:
	//  OP_0 <32-byte scripthash>
	if len(script) == 34 &&
		script[0] == txscript.OP_0 &&
		script[1] == txscript.OP_DATA_32 {
		return script[2:34]
	}
	return nil
}
