// BIP-340 Schnorr adaptor signatures.
//
// This is the Schnorr adaptor construction of Aumayr et al. instantiated
// over BIP-340 (x-only pubkeys, tagged hashes, s = k + e*d convention).
//
// The on-wire format is the same 97-byte encoding as the DCRv0 adaptor in
// adaptor.go. Callers must know which scheme they are using; the scheme is
// not encoded in the signature. Decrypt with the DCRv0 Decrypt method will
// silently return garbage on a BIP-340 adaptor and vice-versa.

package adaptorsigs

import (
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/btcsuite/btcd/btcec/v2"
	btcschnorr "github.com/btcsuite/btcd/btcec/v2/schnorr"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
)

// maxBIP340NonceIters caps the number of nonce-retry iterations inside
// PublicKeyTweakedAdaptorSigBIP340 before giving up. Each iteration fails
// with probability 1/2 (R+T has odd y) or negligibly (k=0 or k>=n), so 128
// is astronomically safe.
const maxBIP340NonceIters = 128

// PublicKeyTweakedAdaptorSigBIP340 creates a public-key-tweaked adaptor
// signature under BIP-340 Schnorr. The party creating this signature knows
// privKey but not the hidden scalar t for which T = t*G. The recipient,
// knowing t, can complete the adaptor to a standard BIP-340 signature using
// DecryptBIP340.
func PublicKeyTweakedAdaptorSigBIP340(privKey *btcec.PrivateKey, hash []byte,
	T *btcec.JacobianPoint) (*AdaptorSignature, error) {

	if len(hash) != scalarSize {
		return nil, fmt.Errorf("hash must be %d bytes, got %d",
			scalarSize, len(hash))
	}

	// BIP-340 step 3: fail if d = 0.
	if privKey.Key.IsZero() {
		return nil, errors.New("private key is zero")
	}

	// Step 4-5: P = d*G; negate d if P.y is odd so the x-only form is
	// canonical. Work on a copy so the caller's key is untouched.
	d := privKey.Key
	var P btcec.JacobianPoint
	btcec.ScalarBaseMultNonConst(&d, &P)
	P.ToAffine()
	if P.Y.IsOdd() {
		d.Negate()
		P.Y.Negate(1)
		P.Y.Normalize()
	}

	var pXBytes [scalarSize]byte
	P.X.PutBytes(&pXBytes)

	var dBytes [scalarSize]byte
	d.PutBytes(&dBytes)
	defer zeroArray(&dBytes)

	// Normalize T to affine once for storage. T itself is unchanged modulo
	// the equivalence class.
	Taff := new(btcec.JacobianPoint)
	Taff.Set(T)
	Taff.ToAffine()

	// Iterate: BIP-340 nonce derivation parameterized by an aux_rand that
	// incorporates an iteration counter. Retry when R+T has odd y - the
	// adaptor requires the final R+T point to satisfy BIP-340's even-y
	// rule, analogous to how DCRv0 adaptor retries in adaptor.go.
	for iter := uint32(0); iter < maxBIP340NonceIters; iter++ {
		k, err := bip340Nonce(dBytes[:], pXBytes[:], hash, iter)
		if err != nil {
			continue
		}
		sig, ok := bip340EncryptedSign(&d, k, hash, pXBytes[:], Taff)
		k.Zero()
		if ok {
			return sig, nil
		}
	}
	return nil, errors.New("bip340 adaptor: no valid nonce found")
}

// bip340Nonce wraps bip340NonceFromAux with an iteration-derived aux_rand,
// used inside the adaptor retry loop when R+T has odd y.
func bip340Nonce(dBytes, pXBytes, msg []byte, iter uint32) (*btcec.ModNScalar, error) {
	var auxRand [32]byte
	binary.BigEndian.PutUint32(auxRand[28:], iter)
	return bip340NonceFromAux(dBytes, pXBytes, msg, auxRand[:])
}

// bip340NonceFromAux computes the BIP-340 deterministic nonce k given the
// canonical inputs: the normalized secret key bytes, the x-only pubkey,
// the 32-byte message, and the aux_rand value. Implements BIP-340 steps
// 6-9.
func bip340NonceFromAux(dBytes, pXBytes, msg, auxRand []byte) (*btcec.ModNScalar, error) {
	// t = d XOR tagged_hash("BIP0340/aux", aux_rand)
	auxHash := chainhash.TaggedHash(chainhash.TagBIP0340Aux, auxRand)
	var tBytes [32]byte
	for i := 0; i < scalarSize; i++ {
		tBytes[i] = dBytes[i] ^ auxHash[i]
	}

	// rand = tagged_hash("BIP0340/nonce", t || P || msg)
	randHash := chainhash.TaggedHash(chainhash.TagBIP0340Nonce,
		tBytes[:], pXBytes, msg)
	for i := range tBytes {
		tBytes[i] = 0
	}

	var k btcec.ModNScalar
	if overflow := k.SetBytes((*[32]byte)(randHash)); overflow != 0 {
		return nil, errors.New("nonce overflow")
	}
	if k.IsZero() {
		return nil, errors.New("nonce is zero")
	}
	return &k, nil
}

// signBIP340Standard produces a textbook BIP-340 Schnorr signature. This
// function is NOT used by the dcrdex adaptor-swap protocol, which always
// signs via PublicKeyTweakedAdaptorSigBIP340. It exists so that the
// shared primitives - nonce derivation, challenge hash, signing equation
// s = k + e*d - can be cross-checked against the canonical BIP-340 test
// vectors. If this function passes the vectors, the same primitives used
// inside the adaptor path are vector-validated.
func signBIP340Standard(privKey *btcec.PrivateKey, hash, auxRand []byte) (*btcschnorr.Signature, error) {
	if len(hash) != scalarSize {
		return nil, fmt.Errorf("hash must be %d bytes, got %d", scalarSize, len(hash))
	}
	if len(auxRand) != scalarSize {
		return nil, fmt.Errorf("auxRand must be %d bytes, got %d", scalarSize, len(auxRand))
	}
	if privKey.Key.IsZero() {
		return nil, errors.New("private key is zero")
	}

	// Normalize d so P = d*G has even y.
	d := privKey.Key
	var P btcec.JacobianPoint
	btcec.ScalarBaseMultNonConst(&d, &P)
	P.ToAffine()
	if P.Y.IsOdd() {
		d.Negate()
		P.Y.Negate(1)
		P.Y.Normalize()
	}

	var pXBytes [scalarSize]byte
	P.X.PutBytes(&pXBytes)

	var dBytes [scalarSize]byte
	d.PutBytes(&dBytes)
	defer zeroArray(&dBytes)

	k, err := bip340NonceFromAux(dBytes[:], pXBytes[:], hash, auxRand)
	if err != nil {
		return nil, err
	}

	// R = k*G; if R.y is odd, negate k so the final R has even y
	// (BIP-340 step 11).
	var R btcec.JacobianPoint
	btcec.ScalarBaseMultNonConst(k, &R)
	R.ToAffine()
	if R.Y.IsOdd() {
		k.Negate()
	}

	// e = tagged_hash("BIP0340/challenge", R.x || P.x || m) mod n
	var rBytes [scalarSize]byte
	R.X.PutBytes(&rBytes)
	eHash := chainhash.TaggedHash(chainhash.TagBIP0340Challenge,
		rBytes[:], pXBytes[:], hash)
	var e btcec.ModNScalar
	e.SetBytes((*[scalarSize]byte)(eHash))

	// s = k + e*d mod n
	s := new(btcec.ModNScalar).Mul2(&e, &d).Add(k)
	k.Zero()

	return btcschnorr.NewSignature(&R.X, s), nil
}

// bip340EncryptedSign is the inner signing loop of BIP-340 adaptor sign. It
// returns (sig, false) when R+T has odd y, signaling the caller to retry
// with a new nonce.
func bip340EncryptedSign(d, k *btcec.ModNScalar, hash, pXBytes []byte,
	Taff *btcec.JacobianPoint) (*AdaptorSignature, bool) {

	// R = k*G
	var R btcec.JacobianPoint
	btcec.ScalarBaseMultNonConst(k, &R)

	// R_adapt = R + T
	var Radapt btcec.JacobianPoint
	btcec.AddNonConst(&R, Taff, &Radapt)
	if (Radapt.X.IsZero() && Radapt.Y.IsZero()) || Radapt.Z.IsZero() {
		return nil, false
	}
	Radapt.ToAffine()
	if Radapt.Y.IsOdd() {
		return nil, false
	}

	// e = tagged_hash("BIP0340/challenge", R_adapt.x || P.x || m) mod n
	var rBytes [scalarSize]byte
	Radapt.X.PutBytes(&rBytes)
	eHash := chainhash.TaggedHash(chainhash.TagBIP0340Challenge,
		rBytes[:], pXBytes, hash)
	var e btcec.ModNScalar
	e.SetBytes((*[scalarSize]byte)(eHash))

	// s = k + e*d mod n  (BIP-340 convention; contrast DCRv0 which uses
	// s = k - e*d).
	s := new(btcec.ModNScalar).Mul2(&e, d).Add(k)

	return &AdaptorSignature{
		r:           Radapt.X,
		s:           *s,
		t:           affinePoint{x: Taff.X, y: Taff.Y},
		pubKeyTweak: true,
	}, true
}

// PrivateKeyTweakedAdaptorSigBIP340 builds a private-key-tweaked adaptor
// signature from an already-valid BIP-340 signature and the scalar tweak.
// The signer knows both the signature and t; the resulting adaptor cannot
// be used by a party who lacks t. The counterparty can later complete it
// by learning t (typically by observing a completed pub-key-tweaked
// adaptor on-chain via RecoverTweakBIP340).
//
// The provided sig must be a valid BIP-340 signature produced by a
// canonical signer (e.g. btcschnorr.Sign); the returned adaptor inherits
// its implicit R point's even-y convention.
func PrivateKeyTweakedAdaptorSigBIP340(sig *btcschnorr.Signature,
	tweak *btcec.ModNScalar) *AdaptorSignature {

	var T btcec.JacobianPoint
	btcec.ScalarBaseMultNonConst(tweak, &T)
	T.ToAffine()

	// btcschnorr.Signature does not expose r and s as accessors; recover
	// them from the serialized form.
	ser := sig.Serialize()
	var r btcec.FieldVal
	r.SetByteSlice(ser[0:32])
	var s btcec.ModNScalar
	s.SetBytes((*[scalarSize]byte)(ser[32:64]))

	// s' = s + t (mod n). Decrypt subtracts t to recover the original s.
	sPrime := new(btcec.ModNScalar).Add2(&s, tweak)

	return &AdaptorSignature{
		r:           r,
		s:           *sPrime,
		t:           affinePoint{x: T.X, y: T.Y},
		pubKeyTweak: false,
	}
}

// VerifyBIP340 verifies an adaptor signature under BIP-340. Works for both
// public-key-tweaked and private-key-tweaked adaptors. The verifier does
// not need the tweak scalar - the stored T and the sig fields are
// sufficient.
//
// For pub-key-tweaked adaptors, a successful verify means that decrypting
// with the correct tweak will produce a valid BIP-340 signature. For
// private-key-tweaked adaptors, it means the signer already possesses a
// valid BIP-340 signature that only lacks subtraction of the tweak.
func (sig *AdaptorSignature) VerifyBIP340(hash []byte, pubKey *btcec.PublicKey) error {
	if len(hash) != scalarSize {
		return fmt.Errorf("bip340 verify: hash must be %d bytes, got %d",
			scalarSize, len(hash))
	}

	// Normalize P to canonical BIP-340 (even y).
	var P btcec.JacobianPoint
	pubKey.AsJacobian(&P)
	P.ToAffine()
	if P.Y.IsOdd() {
		P.Y.Negate(1)
		P.Y.Normalize()
	}
	var pXBytes [scalarSize]byte
	P.X.PutBytes(&pXBytes)

	// e = tagged_hash("BIP0340/challenge", r || P || m) mod n.
	var rBytes [scalarSize]byte
	sig.r.PutBytes(&rBytes)
	eHash := chainhash.TaggedHash(chainhash.TagBIP0340Challenge,
		rBytes[:], pXBytes[:], hash)
	var e btcec.ModNScalar
	e.SetBytes((*[scalarSize]byte)(eHash))
	e.Negate()

	// Reconstruct the expected R, varying sign of T by scheme:
	//   pubKeyTweak:  s*G - e*P + T = R_adapt  (with x == sig.r, y even)
	//   privKeyTweak: s*G - e*P - T = R        (with x == sig.r, y even)
	var sG, eP, sum, result btcec.JacobianPoint
	btcec.ScalarBaseMultNonConst(&sig.s, &sG)
	btcec.ScalarMultNonConst(&e, &P, &eP)
	btcec.AddNonConst(&sG, &eP, &sum)

	T := *sig.t.asJacobian()
	if !sig.pubKeyTweak {
		T.Y.Negate(1)
		T.Y.Normalize()
	}
	btcec.AddNonConst(&sum, &T, &result)

	if (result.X.IsZero() && result.Y.IsZero()) || result.Z.IsZero() {
		return errors.New("bip340 verify: R is point at infinity")
	}
	result.ToAffine()
	if result.Y.IsOdd() {
		return errors.New("bip340 verify: R has odd y")
	}
	if !sig.r.Equals(&result.X) {
		return errors.New("bip340 verify: r mismatch")
	}
	return nil
}

// DecryptBIP340 completes an adaptor signature into a standard BIP-340
// signature. Works for both public-key-tweaked and private-key-tweaked
// adaptors. The provided tweak is checked against the stored T; a
// mismatch returns an error rather than a garbage signature.
func (sig *AdaptorSignature) DecryptBIP340(tweak *btcec.ModNScalar) (*btcschnorr.Signature, error) {
	var expectedT btcec.JacobianPoint
	btcec.ScalarBaseMultNonConst(tweak, &expectedT)
	if !expectedT.EquivalentNonConst(sig.t.asJacobian()) {
		return nil, errors.New("bip340 decrypt: tweak does not match stored T")
	}

	//   pubKeyTweak:  s_final = s + t (mod n)
	//   privKeyTweak: s_final = s - t (mod n)
	sFinal := new(btcec.ModNScalar).Set(tweak)
	if !sig.pubKeyTweak {
		sFinal.Negate()
	}
	sFinal.Add(&sig.s)
	return btcschnorr.NewSignature(&sig.r, sFinal), nil
}

// RecoverTweakBIP340 extracts the tweak scalar t given an adaptor signature
// and the corresponding completed BIP-340 signature (e.g. observed on-chain).
// The recovered tweak is validated against the stored T before being
// returned.
//
// The completed signature is accepted in serialized form (64 bytes, BIP-340)
// because btcec's schnorr.Signature does not expose its s scalar directly.
func (sig *AdaptorSignature) RecoverTweakBIP340(valid *btcschnorr.Signature) (*btcec.ModNScalar, error) {
	if !sig.pubKeyTweak {
		return nil, errors.New("bip340 recover: only pub-key-tweaked adaptors")
	}
	ser := valid.Serialize()
	if len(ser) != btcschnorr.SignatureSize {
		return nil, fmt.Errorf("bip340 recover: unexpected signature size %d", len(ser))
	}
	var vs btcec.ModNScalar
	vs.SetBytes((*[scalarSize]byte)(ser[32:64]))
	tweak := new(btcec.ModNScalar).NegateVal(&sig.s).Add(&vs)

	var expected btcec.JacobianPoint
	btcec.ScalarBaseMultNonConst(tweak, &expected)
	if !expected.EquivalentNonConst(sig.t.asJacobian()) {
		return nil, errors.New("bip340 recover: recovered tweak does not match stored T")
	}
	return tweak, nil
}
