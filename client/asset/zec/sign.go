package zec

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"

	dexzec "decred.org/dcrdex/dex/networks/zec"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcd/txscript"
	"github.com/btcsuite/btcd/wire"
	"github.com/dchest/blake2b"
)

const (
	// ZIP 143 personalization keys.
	blake2BSigHash              = "ZcashSigHash"
	prevoutsHashPersonalization = "ZcashPrevoutHash"
	sequenceHashPersonalization = "ZcashSequencHash"
	outputsHashPersonalization  = "ZcashOutputsHash"

	sigHashMask = 0x1f

	// versionOverwinterGroupID uint32 = 0x3C48270
	versionSaplingGroupID = 0x892f2085
	versionNU5GroupID     = 0x26A7270A
)

var (
	zeroHash chainhash.Hash
)

// signatureHash creates a hash for a transparent input.
// This function will not work with transactions with shielded i/o.
// See https://github.com/zcash/librustzcash/blob/master/zcash_primitives/src/transaction/sighash_v4.rs
// Specifications:
//   https://zips.z.cash/protocol/canopy.pdf section 4.9 points to
//   https://github.com/zcash/zips/blob/main/zip-0243.rst#specification
//   which extends https://zips.z.cash/zip-0143#specification
// See also implementation @
//	https://github.com/iqoption/zecutil/blob/master/sign.go
func signatureHash(subScript []byte, sigHashes *txscript.TxSigHashes, hashType txscript.SigHashType,
	tx *dexzec.Tx, idx int, amt int64, cver [4]byte) (preimage, sHash []byte, err error) {

	if tx.Version < dexzec.VersionSapling {
		return nil, nil, fmt.Errorf("version %d transactions unsupported", tx.Version)
	}

	if idx > len(tx.TxIn)-1 {
		return nil, nil, fmt.Errorf("index %d out of range for %d inputs", idx, len(tx.TxIn))
	}

	sigHash := new(bytes.Buffer)

	// header is tx.Version with the overwintered flag set.
	writeUint32(sigHash, uint32(tx.Version)|(1<<31))

	// nVersionGroupId
	var groupID uint32 = versionSaplingGroupID
	if tx.Version == dexzec.VersionNU5 {
		groupID = versionNU5GroupID
	}
	writeUint32(sigHash, groupID)

	// hashPrevouts
	if hashType&txscript.SigHashAnyOneCanPay == 0 {
		sigHash.Write(sigHashes.HashPrevOuts[:])
	} else {
		sigHash.Write(zeroHash[:])
	}

	// hashSequence
	// We'll apply the bitmask here to filter unused bits, per specification
	// pre-NU5. Technically, from >= NU5, the unused bits must be zero.
	if tx.Version >= dexzec.VersionNU5 && !validateSigHashType(hashType) {
		return nil, nil, fmt.Errorf("NU5 sighash type must be one of 0x01, 0x02, 0x03, 0x81, 0x82, or 0x83")
	} else {
		// Pre NU5, ignore leading bits.
		hashType = hashType & sigHashMask
	}
	if hashType != txscript.SigHashAnyOneCanPay &&
		hashType != txscript.SigHashSingle &&
		hashType != txscript.SigHashNone {

		sigHash.Write(sigHashes.HashSequence[:])
	} else {
		sigHash.Write(zeroHash[:])
	}

	// hashOutputs
	if hashType != txscript.SigHashSingle && hashType != txscript.SigHashNone {
		sigHash.Write(sigHashes.HashOutputs[:])
	} else if hashType == txscript.SigHashSingle && idx < len(tx.TxOut) {
		writeOutput(sigHash, tx.TxOut[idx])
	} else {
		sigHash.Write(zeroHash[:])
	}

	// hashJoinSplits
	sigHash.Write(zeroHash[:])

	// hashShieldedSpends
	if tx.Version >= dexzec.VersionSapling {
		sigHash.Write(zeroHash[:])
	}

	// hashShieldedOutputs
	if tx.Version >= dexzec.VersionSapling {
		sigHash.Write(zeroHash[:])
	}

	// nLockTime
	writeUint32(sigHash, tx.LockTime)

	// nExpiryHeight
	writeUint32(sigHash, tx.ExpiryHeight)

	// valueBalance
	if tx.Version >= dexzec.VersionSapling {
		writeUint64(sigHash, 0)
	}

	// hash type
	writeUint32(sigHash, uint32(hashType))

	// outpoint
	sigHash.Write(tx.TxIn[idx].PreviousOutPoint.Hash[:])
	writeUint32(sigHash, tx.TxIn[idx].PreviousOutPoint.Index)

	// scriptCode
	wire.WriteVarBytes(sigHash, 0, subScript)

	// value
	writeUint64(sigHash, uint64(amt))

	// nSequence
	writeUint32(sigHash, tx.TxIn[idx].Sequence)

	preImage := sigHash.Bytes()
	sigHashKey := append([]byte(blake2BSigHash), cver[:]...)
	h, err := blake2bHash(preImage, sigHashKey)
	if err != nil {
		return nil, nil, err
	}

	return preImage, h[:], nil
}

func writeUint32(w io.Writer, v uint32) {
	var i [4]byte
	binary.LittleEndian.PutUint32(i[:], v)
	w.Write(i[:])
}

func writeUint64(w io.Writer, v uint64) {
	var i [8]byte
	binary.LittleEndian.PutUint64(i[:], v)
	w.Write(i[:])
}

func writeOutput(w io.Writer, op *wire.TxOut) error {
	b := new(bytes.Buffer)
	if err := wire.WriteTxOut(b, 0, 0, op); err != nil {
		return err
	}

	h, err := blake2bHash(b.Bytes(), []byte(outputsHashPersonalization))
	if err != nil {
		return err
	}
	w.Write(h[:])
	return nil
}

func validateSigHashType(v txscript.SigHashType) bool {
	return v == txscript.SigHashAll || v == txscript.SigHashNone || v == txscript.SigHashSingle ||
		v == txscript.SigHashAnyOneCanPay || v == 0x82 || v == 0x83
}

// blake2bHash is a BLAKE-2B hash of the data with the specified personalization
// key.
func blake2bHash(data, personalizationKey []byte) (h chainhash.Hash, err error) {
	bHash, err := blake2b.New(&blake2b.Config{Size: 32, Person: personalizationKey})
	if err != nil {
		return h, err
	}

	if _, err = bHash.Write(data); err != nil {
		return h, err
	}

	err = h.SetBytes(bHash.Sum(nil))
	return h, err
}

// NewTxSigHashes is like btcd/txscript.NewTxSigHashes, except the underlying
// hash function is BLAKE-2B instead of double-SHA256.
func NewTxSigHashes(tx *dexzec.Tx) (h *txscript.TxSigHashes, err error) {
	h = &txscript.TxSigHashes{}
	if h.HashPrevOuts, err = calcHashPrevOuts(tx); err != nil {
		return
	}
	if h.HashSequence, err = calcHashSequence(tx); err != nil {
		return
	}
	if h.HashOutputs, err = calcHashOutputs(tx); err != nil {
		return
	}
	return
}

// calcHashPrevOuts is btcd/txscxript.calcHashPrevOuts, but with  BLAKE-2B
// instead of double-SHA256. The personalization key is defined in ZIP 143.
func calcHashPrevOuts(tx *dexzec.Tx) (chainhash.Hash, error) {
	var b bytes.Buffer
	for _, in := range tx.TxIn {
		b.Write(in.PreviousOutPoint.Hash[:])
		var buf [4]byte
		binary.LittleEndian.PutUint32(buf[:], in.PreviousOutPoint.Index)
		b.Write(buf[:])
	}
	return blake2bHash(b.Bytes(), []byte(prevoutsHashPersonalization))
}

// calcHashSequence is btcd/txscxript.calcHashSequence, but with  BLAKE-2B
// instead of double-SHA256. The personalization key is defined in ZIP 143.
func calcHashSequence(tx *dexzec.Tx) (chainhash.Hash, error) {
	var b bytes.Buffer
	for _, in := range tx.TxIn {
		var buf [4]byte
		binary.LittleEndian.PutUint32(buf[:], in.Sequence)
		b.Write(buf[:])
	}
	return blake2bHash(b.Bytes(), []byte(sequenceHashPersonalization))
}

// calcHashOutputs is btcd/txscxript.calcHashOutputs, but with  BLAKE-2B
// instead of double-SHA256. The personalization key is defined in ZIP 143.
func calcHashOutputs(tx *dexzec.Tx) (_ chainhash.Hash, err error) {
	var b bytes.Buffer
	for _, out := range tx.TxOut {
		if err = wire.WriteTxOut(&b, 0, 0, out); err != nil {
			return chainhash.Hash{}, err
		}
	}
	return blake2bHash(b.Bytes(), []byte(outputsHashPersonalization))
}
