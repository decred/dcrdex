// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package libsecp256k1

/*
#cgo CFLAGS: -g -Wall
#cgo LDFLAGS: -L. -l:secp256k1/.libs/libsecp256k1.a
#include "secp256k1/include/secp256k1_dleag.h"
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
)

const (
	proofLength = 48893
)

// Ed25519DleagProve creates a proof for checking a discrete logarithm is equal
// across the secp256k1 and ed25519 curves.
func Ed25519DleagProve(privKey *edwards.PrivateKey) (proof [proofLength]byte, err error) {
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
	plen := C.ulong(proofLength)
	p := (*C.uchar)(unsafe.Pointer(&proof))
	res := C.secp256k1_ed25519_dleag_prove(secpCtx, p, &plen, k, *nb, n)
	if int(res) != 1 {
		return [proofLength]byte{}, errors.New("C.secp256k1_ed25519_dleag_prove exited with error")
	}
	return proof, nil
}

// Ed25519DleagVerify verifies that a descrete logarithm is equal across the
// secp256k1 and ed25519 curves.
func Ed25519DleagVerify(proof [proofLength]byte) bool {
	secpCtx := C._ctx()
	defer C.free(unsafe.Pointer(secpCtx))
	pl := C.ulong(proofLength)
	p := (*C.uchar)(unsafe.Pointer(&proof))
	res := C.secp256k1_ed25519_dleag_verify(secpCtx, p, pl)
	return res == 1
}
