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
	// See extractSwapAddresses for a breakdown of the bytes.
	SwapContractSize = 97

	// DERSigLength is the maximum length of a DER encoded signature.
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
	//   - 97 bytes secret key
	RedeemSwapSigScriptSize = 1 + DERSigLength + 1 + 33 + 1 + 32 + 1 + 1 + 97

	// RefundSigScriptSize is the worst case (largest) serialize size
	// of a transaction input script that refunds a compressed P2PKH output.
	// It is calculated as:
	//
	//   - OP_DATA_73
	//   - 72 bytes DER signature + 1 byte sighash
	//   - OP_DATA_33
	//   - 33 bytes serialized compressed pubkey
	//   - OP_0
	//   - varint 97
	//   - 97 bytes contract
	RefundSigScriptSize = 1 + DERSigLength + 1 + 33 + 1 + 1 + 97

	// Overhead for a wire.TxIn. See wire.TxIn.SerializeSize.
	// hash 32 bytes + index 4 bytes + sequence 4 bytes.
	TxInOverhead = 32 + 4 + 4

	// TxOutOverhead is the overhead associated with a transaction output.
	// 8 bytes value + at least 1 byte varint script size
	TxOutOverhead = 8 + 1

	// RedeemP2PKHSigScriptSize is the worst case (largest) serialize size
	// of a transaction input script that redeems a compressed P2PKH output.
	// It is calculated as:
	//
	//   - OP_DATA_73
	//   - 72 bytes DER signature + 1 byte sighash
	//   - OP_DATA_33
	//   - 33 bytes serialized compressed pubkey
	RedeemP2PKHSigScriptSize = 1 + DERSigLength + 1 + 33

	// P2PKHPkScriptSize is the size of a transaction output script that
	// pays to a compressed pubkey hash.  It is calculated as:
	//
	//   - OP_DUP
	//   - OP_HASH160
	//   - OP_DATA_20
	//   - 20 bytes pubkey hash
	//   - OP_EQUALVERIFY
	//   - OP_CHECKSIG
	P2PKHPkScriptSize = 1 + 1 + 1 + 20 + 1 + 1

	// P2PKHOutputSize is the size of the serialized P2PKH output.
	P2PKHOutputSize = TxOutOverhead + P2PKHPkScriptSize

	// P2SHPkScriptSize is the size of a transaction output script that
	// pays to a redeem script.  It is calculated as:
	//
	//   - OP_HASH160
	//   - OP_DATA_20
	//   - 20 bytes pubkey hash
	//   - OP_EQUAL
	P2SHPkScriptSize = 1 + 1 + 20 + 1

	// P2SHOutputSize is the size of the serialized P2SH output.
	P2SHOutputSize = TxOutOverhead + P2SHPkScriptSize

	// RedeemP2PKHInputSize is the worst case (largest) serialize size of a
	// transaction input redeeming a compressed P2PKH output.  It is
	// calculated as:
	//
	//   - 32 bytes previous tx
	//   - 4 bytes output index
	//   - 1 byte compact int encoding value 107
	//   - 107 bytes signature script
	//   - 4 bytes sequence
	RedeemP2PKHInputSize = TxInOverhead + RedeemP2PKHSigScriptSize

	// RedeemP2WPKHInputSize is the worst case size of a transaction
	// input redeeming a P2WPKH output. This does not account for witness data,
	// which is considered at a lower weight for fee calculations. It is
	// calculated as
	//
	//   - 32 bytes previous tx
	//   - 4 bytes output index
	//   - 1 byte encoding empty redeem script
	//   - 0 bytes redeem script
	//   - 4 bytes sequence
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
	RedeemP2WPKHInputWitnessWeight = 1 + DERSigLength + 1 + 33

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
	P2WPKHOutputSize = 8 + 1 + P2WPKHPkScriptSize

	// MimimumTxOverhead is the size of an empty transaction.
	// 4 bytes version + 4 bytes locktime + 2 bytes of varints for the number of
	// transaction inputs and outputs
	MimimumTxOverhead = 4 + 4 + 1 + 1

	// InitTxSize is the size of a standard serialized atomic swap initialization
	// transaction with one change output.
	// MsgTx overhead is 4 bytes version + 4 bytes locktime + 2 bytes of varints
	// for the number of transaction inputs and outputs (1 each here) + 1 P2PKH
	// input (25) + 2 outputs (8 + 1 bytes overhead each): 1 P2PKH script (25) +
	// 1 P2SH script (23).
	InitTxSize = MimimumTxOverhead + RedeemP2PKHInputSize + P2PKHOutputSize + P2SHOutputSize
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

// MakeContract creates an atomic swap contract.
func MakeContract(recipient, sender string, secretHash []byte, lockTime int64, chainParams *chaincfg.Params) ([]byte, error) {
	rAddr, err := btcutil.DecodeAddress(recipient, chainParams)
	if err != nil {
		return nil, fmt.Errorf("error decoding recipient address %s: %v", recipient, err)
	}
	_, ok := rAddr.(*btcutil.AddressPubKeyHash)
	if !ok {
		return nil, fmt.Errorf("recipient address %s is not a pubkey-hash address", recipient)
	}
	sAddr, err := btcutil.DecodeAddress(sender, chainParams)
	if err != nil {
		return nil, fmt.Errorf("error decoding sender address %s: %v", sender, err)
	}
	_, ok = sAddr.(*btcutil.AddressPubKeyHash)
	if !ok {
		return nil, fmt.Errorf("sender address %s is not a pubkey-hash address", recipient)
	}
	if len(secretHash) != SecretHashSize {
		return nil, fmt.Errorf("secret hash of length %d not supported", len(secretHash))
	}

	return txscript.NewScriptBuilder().
		AddOps([]byte{
			txscript.OP_IF,
			txscript.OP_SIZE,
		}).AddInt64(SecretHashSize).
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
// Based on btcutil/policy isDust. See btcutil/policy for further documentation.
func IsDust(txOut *wire.TxOut, minRelayTxFee uint64) bool {
	if txscript.IsUnspendable(txOut.PkScript) {
		return true
	}
	totalSize := txOut.SerializeSize() + 41
	if txscript.IsWitnessProgram(txOut.PkScript) {
		totalSize += (107 / blockchain.WitnessScaleFactor)
	} else {
		totalSize += 107
	}
	return txOut.Value*1000/(3*int64(totalSize)) < int64(minRelayTxFee)
}

// BtcScriptAddrs is information about the pubkeys or pubkey hashes present in
// a scriptPubKey (and the redeem script, for p2sh). This information can be
// used to estimate the spend script size, e.g. pubkeys in a redeem script don't
// require pubkeys in the witness/scriptSig, but pubkey hashes do.
type BtcScriptAddrs struct {
	PubKeys   []btcutil.Address
	NumPK     int
	PKHashes  []btcutil.Address
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
		return nil, nonStandard, fmt.Errorf("ExtractScriptAddrs: %v", err)
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
		PKHashes:  pkHashes,
		NumPKH:    len(pkHashes),
		NRequired: numRequired,
	}, false, nil
}

// ExtractSwapDetails extacts the sender and receiver addresses from a swap
// contract. If the provided script is not a swap contract, an error will be
// returned.
func ExtractSwapDetails(pkScript []byte, chainParams *chaincfg.Params) (
	sender btcutil.Address, receiver btcutil.Address, lockTime uint64, secretHash []byte, err error) {
	// A swap redemption sigScript is <pubkey> <secret> and satisfies the
	// following swap contract.
	//
	// OP_IF
	//  OP_SIZE hashSize OP_EQUALVERIFY OP_SHA256 OP_DATA_32 secretHash OP_EQUALVERIFY OP_DUP OP_HASH160 OP_DATA20 pkHashReceiver
	//     1   +   2    +      1       +    1    +   1      +   32     +      1       +   1  +   1      +    1    +    20
	// OP_ELSE
	//  OP_DATA4 locktime OP_CHECKLOCKTIMEVERIFY OP_DROP OP_DUP OP_HASH160 OP_DATA_20 pkHashSender
	//     1    +    4   +           1          +   1   +  1   +    1     +   1      +    20
	// OP_ENDIF
	// OP_EQUALVERIFY
	// OP_CHECKSIG
	//
	// 5 bytes if-else-endif-equalverify-checksig
	// 1 + 2 + 1 + 1 + 1 + 32 + 1 + 1 + 1 + 1 + 20 = 62 bytes for redeem block
	// 1 + 4 + 1 + 1 + 1 + 1 + 1 + 20 = 30 bytes for refund block
	// 5 + 62 + 30 = 97 bytes
	if len(pkScript) != SwapContractSize {
		return nil, nil, 0, nil, fmt.Errorf("incorrect swap contract length. expected %d, got %d", SwapContractSize, len(pkScript))
	}

	if pkScript[0] == txscript.OP_IF &&
		pkScript[1] == txscript.OP_SIZE &&
		// secret key hash size (2 bytes)
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
		// time (4 bytes)
		pkScript[69] == txscript.OP_CHECKLOCKTIMEVERIFY &&
		pkScript[70] == txscript.OP_DROP &&
		pkScript[71] == txscript.OP_DUP &&
		pkScript[72] == txscript.OP_HASH160 &&
		pkScript[73] == txscript.OP_DATA_20 &&
		// sender's pkh (20 bytes)
		pkScript[94] == txscript.OP_ENDIF &&
		pkScript[95] == txscript.OP_EQUALVERIFY &&
		pkScript[96] == txscript.OP_CHECKSIG {

		receiverAddr, err := btcutil.NewAddressPubKeyHash(pkScript[43:63], chainParams)
		if err != nil {
			return nil, nil, 0, nil, fmt.Errorf("error decoding address from recipient's pubkey hash")
		}

		senderAddr, err := btcutil.NewAddressPubKeyHash(pkScript[74:94], chainParams)
		if err != nil {
			return nil, nil, 0, nil, fmt.Errorf("error decoding address from sender's pubkey hash")
		}

		return senderAddr, receiverAddr, uint64(binary.LittleEndian.Uint32(pkScript[65:69])), pkScript[7:39], nil
	}
	return nil, nil, 0, nil, fmt.Errorf("invalid swap contract")
}

// redeemP2SHContract returns the signature script to redeem a contract output
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

// refundP2SHContract returns the signature script to refund a contract output
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
	// Add 3 before dividing witness weight to force rounding up.
	return nfo.SigScriptSize + (nfo.WitnessSize+3)/4 + TxInOverhead
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
		return nil, fmt.Errorf("error parsing script addresses: %v", err)
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
		// Start with the signatures.
		sigScriptSize = (DERSigLength + 1) * scriptAddrs.NRequired // 73 max for sig, 1 for push code
		// If there are pubkey-hash addresses, they'll need pubkeys.
		if scriptAddrs.NumPKH > 0 {
			sigScriptSize += scriptAddrs.NRequired * (PubKeyLength + 1)
		}
		// Then add the OP_0 + script_length(1) + len(script).
		sigScriptSize += len(redeemScript) + 2
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
// push.
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
		if len(keyHash) == 0 && len(push) != SwapContractSize {
			continue
		}
		h := btcutil.Hash160(push)
		if bytes.Equal(h, contractHash) {
			_, _, _, keyHash, err = ExtractSwapDetails(push, chainParams)
			if err != nil {
				return nil, fmt.Errorf("error extracting atomic swap details: %v", err)
			}
			continue
		}
		// If we've found the keyhash, starting hashing the push to find the key.
		if len(keyHash) > 0 {
			if len(push) != SecretKeySize {
				continue
			}
			h := sha256.Sum256(push)
			if bytes.Equal(h[:], keyHash) {
				return push, nil
			}
		}
	}
	return nil, fmt.Errorf("key not found")
}

// ExtractContractHash extracts the redeem script hash from the P2SH script.
func ExtractContractHash(scriptHex string, chainParams *chaincfg.Params) ([]byte, error) {
	pkScript, err := hex.DecodeString(scriptHex)
	if err != nil {
		return nil, fmt.Errorf("error decoding scriptPubKey '%s': %v",
			scriptHex, err)
	}
	return ExtractScriptHash(pkScript, chainParams)
}

// ExtractScriptHash attempts to extract the redeem script hash from pkScript.
// The pkScript must be a a P2SH script, requiring only 1 pkh address, which
// must be a script hash address.
func ExtractScriptHash(pkScript []byte, chainParams *chaincfg.Params) ([]byte, error) {
	scriptAddrs, _, err := ExtractScriptAddrs(pkScript, chainParams)
	if err != nil {
		return nil, fmt.Errorf("error extracting contract address: %v", err)
	}
	if scriptAddrs.NRequired != 1 || scriptAddrs.NumPKH != 1 {
		return nil, fmt.Errorf("contract output has wrong number of required sigs(%d) or addresses(%d)",
			scriptAddrs.NRequired, scriptAddrs.NumPKH)
	}
	contractAddr := scriptAddrs.PKHashes[0]
	_, ok := contractAddr.(*btcutil.AddressScriptHash)
	if !ok {
		return nil, fmt.Errorf("wrong contract address type %s: %T", contractAddr, contractAddr)
	}
	// ScriptAddress is really a.hash[:], not the encoded address.
	return contractAddr.ScriptAddress(), nil
}
