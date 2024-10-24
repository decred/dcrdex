// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package libsecp256k1

/*
#cgo CFLAGS: -g -Wall
#cgo LDFLAGS: -L. -l:secp256k1/.libs/libsecp256k1.a
#include "secp256k1/include/secp256k1_dleag.h"
#include "secp256k1/include/secp256k1_ecdsaotves.h"
#include <stdlib.h>

secp256k1_context* _ctx() {
	return secp256k1_context_create(SECP256K1_CONTEXT_SIGN | SECP256K1_CONTEXT_VERIFY);
}
*/
import "C"
import (
	"errors"
	"unsafe"

	"decred.org/dcrdex/dex/encode"
	"github.com/decred/dcrd/dcrec/edwards/v2"
	"github.com/decred/dcrd/dcrec/secp256k1/v4"
)

const (
	ProofLen  = 48893
	CTLen     = 196
	maxSigLen = 72 // The actual size is variable.
)

// Ed25519DleagProve creates a proof for checking a discrete logarithm is equal
// across the secp256k1 and ed25519 curves.
func Ed25519DleagProve(privKey *edwards.PrivateKey) (proof [ProofLen]byte, err error) {
	secpCtx := C._ctx()
	defer C.free(unsafe.Pointer(secpCtx))
	nonce := [32]byte{}
	copy(nonce[:], encode.RandomBytes(32))
	key := [32]byte{}
	copy(key[:], privKey.Serialize())
	n := (*C.uchar)(unsafe.Pointer(&nonce))
	k := (*C.uchar)(unsafe.Pointer(&key))
	nBits := uint64(252)
	nb := (*C.ulong)(unsafe.Pointer(&nBits))
	plen := C.ulong(ProofLen)
	p := (*C.uchar)(unsafe.Pointer(&proof))
	res := C.secp256k1_ed25519_dleag_prove(secpCtx, p, &plen, k, *nb, n)
	if int(res) != 1 {
		return [ProofLen]byte{}, errors.New("C.secp256k1_ed25519_dleag_prove exited with error")
	}
	return proof, nil
}

// Ed25519DleagVerify verifies that a descrete logarithm is equal across the
// secp256k1 and ed25519 curves.
func Ed25519DleagVerify(proof [ProofLen]byte) bool {
	secpCtx := C._ctx()
	defer C.free(unsafe.Pointer(secpCtx))
	pl := C.ulong(ProofLen)
	p := (*C.uchar)(unsafe.Pointer(&proof))
	res := C.secp256k1_ed25519_dleag_verify(secpCtx, p, pl)
	return res == 1
}

// EcdsaotvesEncSign signs the hash and returns an encrypted signature.
func EcdsaotvesEncSign(signPriv *secp256k1.PrivateKey, encPub *secp256k1.PublicKey, hash [32]byte) (cyphertext [CTLen]byte, err error) {
	secpCtx := C._ctx()
	defer C.free(unsafe.Pointer(secpCtx))
	privBytes := [32]byte{}
	copy(privBytes[:], signPriv.Serialize())
	priv := (*C.uchar)(unsafe.Pointer(&privBytes))
	pubBytes := [33]byte{}
	copy(pubBytes[:], encPub.SerializeCompressed())
	pub := (*C.uchar)(unsafe.Pointer(&pubBytes))
	h := (*C.uchar)(unsafe.Pointer(&hash))
	s := (*C.uchar)(unsafe.Pointer(&cyphertext))
	res := C.ecdsaotves_enc_sign(secpCtx, s, priv, pub, h)
	if int(res) != 1 {
		return [CTLen]byte{}, errors.New("C.ecdsaotves_enc_sign exited with error")
	}
	return cyphertext, nil
}

// EcdsaotvesEncVerify verifies the encrypted signature.
func EcdsaotvesEncVerify(signPub, encPub *secp256k1.PublicKey, hash [32]byte, cyphertext [CTLen]byte) bool {
	secpCtx := C._ctx()
	defer C.free(unsafe.Pointer(secpCtx))
	signBytes := [33]byte{}
	copy(signBytes[:], signPub.SerializeCompressed())
	sp := (*C.uchar)(unsafe.Pointer(&signBytes))
	encBytes := [33]byte{}
	copy(encBytes[:], encPub.SerializeCompressed())
	ep := (*C.uchar)(unsafe.Pointer(&encBytes))
	h := (*C.uchar)(unsafe.Pointer(&hash))
	c := (*C.uchar)(unsafe.Pointer(&cyphertext))
	res := C.ecdsaotves_enc_verify(secpCtx, sp, ep, h, c)
	return res == 1
}

// EcdsaotvesDecSig retrieves the signature.
func EcdsaotvesDecSig(encPriv *secp256k1.PrivateKey, cyphertext [CTLen]byte) ([]byte, error) {
	secpCtx := C._ctx()
	defer C.free(unsafe.Pointer(secpCtx))
	encBytes := [32]byte{}
	copy(encBytes[:], encPriv.Serialize())
	ep := (*C.uchar)(unsafe.Pointer(&encBytes))
	ct := (*C.uchar)(unsafe.Pointer(&cyphertext))
	var sig [maxSigLen]byte
	s := (*C.uchar)(unsafe.Pointer(&sig))
	slen := C.ulong(maxSigLen)
	res := C.ecdsaotves_dec_sig(secpCtx, s, &slen, ep, ct)
	if int(res) != 1 {
		return nil, errors.New("C.ecdsaotves_dec_sig exited with error")
	}
	sigCopy := make([]byte, maxSigLen)
	copy(sigCopy, sig[:])
	// Remove trailing zeros.
	for i := maxSigLen - 1; i >= 0; i-- {
		if sigCopy[i] != 0 {
			break
		}
		sigCopy = sigCopy[:i]
	}
	return sigCopy, nil
}

// EcdsaotvesRecEncKey retrieves the encoded private key from signature and
// cyphertext.
func EcdsaotvesRecEncKey(encPub *secp256k1.PublicKey, cyphertext [CTLen]byte, sig []byte) (encPriv *secp256k1.PrivateKey, err error) {
	secpCtx := C._ctx()
	defer C.free(unsafe.Pointer(secpCtx))
	pubBytes := [33]byte{}
	copy(pubBytes[:], encPub.SerializeCompressed())
	ep := (*C.uchar)(unsafe.Pointer(&pubBytes))
	ct := (*C.uchar)(unsafe.Pointer(&cyphertext))
	sigCopy := [maxSigLen]byte{}
	copy(sigCopy[:], sig)
	s := (*C.uchar)(unsafe.Pointer(&sigCopy))
	varSigLen := len(sig)
	slen := C.ulong(varSigLen)
	pkBytes := [32]byte{}
	pk := (*C.uchar)(unsafe.Pointer(&pkBytes))
	res := C.ecdsaotves_rec_enc_key(secpCtx, pk, ep, ct, s, slen)
	if int(res) != 1 {
		return nil, errors.New("C.ecdsaotves_rec_enc_key exited with error")
	}
	return secp256k1.PrivKeyFromBytes(pkBytes[:]), nil
}
