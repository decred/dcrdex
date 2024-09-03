package adaptorsigs

import (
	"errors"
	"fmt"

	"github.com/decred/dcrd/crypto/blake256"
	"github.com/decred/dcrd/dcrec/secp256k1/v4"
	"github.com/decred/dcrd/dcrec/secp256k1/v4/schnorr"
)

// AdaptorSignatureSize is the size of an encoded adaptor Schnorr signature.
const AdaptorSignatureSize = 97

// scalarSize is the size of an encoded big endian scalar.
const scalarSize = 32

var (
	// rfc6979ExtraDataV0 is the extra data to feed to RFC6979 when generating
	// the deterministic nonce for the EC-Schnorr-DCRv0 scheme.  This ensures
	// the same nonce is not generated for the same message and key as for other
	// signing algorithms such as ECDSA.
	//
	// It is equal to BLAKE-256([]byte("EC-Schnorr-DCRv0")).
	rfc6979ExtraDataV0 = [32]byte{
		0x0b, 0x75, 0xf9, 0x7b, 0x60, 0xe8, 0xa5, 0x76,
		0x28, 0x76, 0xc0, 0x04, 0x82, 0x9e, 0xe9, 0xb9,
		0x26, 0xfa, 0x6f, 0x0d, 0x2e, 0xea, 0xec, 0x3a,
		0x4f, 0xd1, 0x44, 0x6a, 0x76, 0x83, 0x31, 0xcb,
	}
)

type affinePoint struct {
	x secp256k1.FieldVal
	y secp256k1.FieldVal
}

func (p *affinePoint) asJacobian() *secp256k1.JacobianPoint {
	var result secp256k1.JacobianPoint
	result.X.Set(&p.x)
	result.Y.Set(&p.y)
	result.Z.SetInt(1)
	return &result
}

// AdaptorSignature is a signature with auxillary data that commits to a hidden
// value. When an adaptor signature is combined with a corresponding signature,
// the hidden value is revealed. Alternatively, when combined with a hidden
// value, the adaptor reveals the signature.
//
// An adaptor signature is created by either doing a public or private key
// tweak of a valid schnorr signature. A private key tweak can only be done by
// a party who knows the hidden value, and a public key tweak can be done by
// a party that only knows the point on the secp256k1 curve derived by the
// multiplying the hidden value by the generator point.
//
// Generally the workflow of using adaptor signatures is the following:
//  1. Party A randomly selects a hidden value and creates a private key
//     modified adaptor signature of something for which party B requires
//     a valid signature.
//  2. The Party B sees the PublicTweak in the adaptor signature, and creates
//     a public key tweaked adaptor signature for something that party A
//     requires a valid signature.
//  3. Since party A knows the hidden value, they can use the hidden value to
//     create a valid signature from the public key tweaked adaptor signature.
//  4. When the valid signature is revealed, by being posted to the blockchain,
//     party B can recover the tweak and use it to decrypt the private key
//     tweaked adaptor signature that party A originally sent them.
type AdaptorSignature struct {
	r           secp256k1.FieldVal
	s           secp256k1.ModNScalar
	t           affinePoint
	pubKeyTweak bool
}

// Serialize returns a serialized adaptor signature in the following format:
//
//	sig[0:32]  x coordinate of the point R, encoded as a big-endian uint256
//	sig[32:64] s, encoded also as big-endian uint256
//	sig[64:96] x coordinate of the point T, encoded as a big-endian uint256
//	sig[96] first bit is 1 if the signature is public key tweaked, second bit
//	        is 1 if the y coordinate of T is odd.
func (sig *AdaptorSignature) Serialize() []byte {
	var b [AdaptorSignatureSize]byte
	sig.r.PutBytesUnchecked(b[0:32])
	sig.s.PutBytesUnchecked(b[32:64])
	sig.t.x.PutBytesUnchecked(b[64:96])
	if sig.pubKeyTweak {
		b[96] = 1
	}
	b[96] |= byte(sig.t.y.IsOddBit()) << 1
	return b[:]
}

// ParseAdaptorSignature parses an adaptor signature from a serialized format.
func ParseAdaptorSignature(b []byte) (*AdaptorSignature, error) {
	if len(b) != AdaptorSignatureSize {
		str := fmt.Sprintf("malformed signature: wrong size: %d", len(b))
		return nil, errors.New(str)
	}

	var r secp256k1.FieldVal
	if overflow := r.SetBytes((*[32]byte)(b[0:32])); overflow > 0 {
		str := "invalid signature: r >= field prime"
		return nil, errors.New(str)
	}

	var s secp256k1.ModNScalar
	if overflow := s.SetBytes((*[32]byte)(b[32:64])); overflow > 0 {
		str := "invalid signature: s >= group order"
		return nil, errors.New(str)
	}

	var t affinePoint
	if overflow := t.x.SetBytes((*[32]byte)(b[64:96])); overflow > 0 {
		str := "invalid signature: t.x >= field prime"
		return nil, errors.New(str)
	}

	isOdd := (b[96]>>1)&1 == 1
	if valid := secp256k1.DecompressY(&t.x, isOdd, &t.y); !valid {
		str := "invalid signature: not for a valid curve point"
		return nil, errors.New(str)
	}
	t.y.Normalize()

	pubKeyTweak := b[96]&1 == 1

	return &AdaptorSignature{
		r:           r,
		s:           s,
		t:           t,
		pubKeyTweak: pubKeyTweak,
	}, nil
}

// IsEqual returns true if the adaptor signature is equal to another.
func (sig *AdaptorSignature) IsEqual(otherSig *AdaptorSignature) bool {
	return sig.r.Equals(&otherSig.r) &&
		sig.s.Equals(&otherSig.s) &&
		sig.t.x.Equals(&otherSig.t.x) &&
		sig.t.y.Equals(&otherSig.t.y) &&
		sig.pubKeyTweak == otherSig.pubKeyTweak
}

// schnorrAdaptorVerify verifies that the adaptor signature will result in a
// valid signature when decrypted with the tweak.
func schnorrAdaptorVerify(sig *AdaptorSignature, hash []byte, pubKey *secp256k1.PublicKey) error {
	// The algorithm for producing a EC-Schnorr-DCRv0 signature is as follows:
	// This deviates from the original algorithm in step 7.
	//
	// 1. Fail if m is not 32 bytes
	// 2. Fail if Q is not a point on the curve
	// 3. Fail if r >= p
	// 4. Fail if s >= n
	// 5. e = BLAKE-256(r || m) (Ensure r is padded to 32 bytes)
	// 6. Fail if e >= n
	// 7. R = s*G + e*Q - T
	// 8. Fail if R is the point at infinity
	// 9. Fail if R.y is odd
	// 10. Verified if R.x == r

	// Step 1.
	//
	// Fail if m is not 32 bytes
	if len(hash) != scalarSize {
		str := fmt.Sprintf("wrong size for message (got %v, want %v)",
			len(hash), scalarSize)
		return errors.New(str)
	}

	// Step 2.
	//
	// Fail if Q is not a point on the curve
	if !pubKey.IsOnCurve() {
		str := "pubkey point is not on curve"
		return errors.New(str)
	}

	// Step 3.
	//
	// Fail if r >= p
	//
	// Note this is already handled by the fact r is a field element.

	// Step 4.
	//
	// Fail if s >= n
	//
	// Note this is already handled by the fact s is a mod n scalar.

	// Step 5.
	//
	// e = BLAKE-256(r || m) (Ensure r is padded to 32 bytes)
	var commitmentInput [scalarSize * 2]byte
	sig.r.PutBytesUnchecked(commitmentInput[0:scalarSize])
	copy(commitmentInput[scalarSize:], hash[:])
	commitment := blake256.Sum256(commitmentInput[:])

	// Step 6.
	//
	// Fail if e >= n
	var e secp256k1.ModNScalar
	if overflow := e.SetBytes(&commitment); overflow != 0 {
		str := "hash of (R || m) too big"
		return errors.New(str)
	}

	// Step 7.
	//
	// R = s*G + e*Q - T
	var Q, R, sG, eQ, encryptedR secp256k1.JacobianPoint
	pubKey.AsJacobian(&Q)
	secp256k1.ScalarBaseMultNonConst(&sig.s, &sG)
	secp256k1.ScalarMultNonConst(&e, &Q, &eQ)
	secp256k1.AddNonConst(&sG, &eQ, &R)
	tInv := sig.t.asJacobian()
	tInv.Y.Negate(1)
	secp256k1.AddNonConst(&R, tInv, &encryptedR)

	// Step 8.
	//
	// Fail if R is the point at infinity
	if (encryptedR.X.IsZero() && encryptedR.Y.IsZero()) || encryptedR.Z.IsZero() {
		str := "calculated R point is the point at infinity"
		return errors.New(str)
	}

	// Step 9.
	//
	// Fail if R.y is odd
	//
	// Note that R must be in affine coordinates for this check.
	encryptedR.ToAffine()
	if encryptedR.Y.IsOdd() {
		str := "calculated encrypted R y-value is odd"
		return errors.New(str)
	}

	// Step 10.
	//
	// Verified if R.x == r
	//
	// Note that R must be in affine coordinates for this check.
	if !sig.r.Equals(&encryptedR.X) {
		str := "calculated R point was not given R"
		return errors.New(str)
	}

	return nil
}

// Verify checks that the adaptor signature, when decrypted using the tweak,
// will result in a valid schnorr signature for the given hash and public key.
func (sig *AdaptorSignature) Verify(hash []byte, pubKey *secp256k1.PublicKey) error {
	if sig.pubKeyTweak {
		return fmt.Errorf("only priv key tweaked adaptors can be verified")
	}

	return schnorrAdaptorVerify(sig, hash, pubKey)
}

// Decrypt returns a valid schnorr signature if the tweak is correct.
// This may not be a valid signature if the tweak is incorrect. The caller can
// use Verify to make sure it is a valid signature.
func (sig *AdaptorSignature) Decrypt(tweak *secp256k1.ModNScalar) (*schnorr.Signature, error) {
	var expectedT secp256k1.JacobianPoint
	secp256k1.ScalarBaseMultNonConst(tweak, &expectedT)
	expectedT.ToAffine()
	if !expectedT.X.Equals(&sig.t.x) {
		return nil, fmt.Errorf("tweak X does not match expected")
	}
	if !expectedT.Y.Equals(&sig.t.y) {
		return nil, fmt.Errorf("tweak Y does not match expected")
	}

	s := new(secp256k1.ModNScalar).Set(tweak)
	if !sig.pubKeyTweak {
		s.Negate()
	}
	s.Add(&sig.s)

	decryptedSig := schnorr.NewSignature(&sig.r, s)
	return decryptedSig, nil
}

// RecoverTweak recovers the tweak using the decrypted signature.
func (sig *AdaptorSignature) RecoverTweak(validSig *schnorr.Signature) (*secp256k1.ModNScalar, error) {
	if !sig.pubKeyTweak {
		return nil, fmt.Errorf("only pub key tweaked sigs can be recovered")
	}

	_, s := parseSig(validSig)

	t := new(secp256k1.ModNScalar).NegateVal(&sig.s).Add(s)

	// Verify the recovered tweak
	var expectedT secp256k1.JacobianPoint
	secp256k1.ScalarBaseMultNonConst(t, &expectedT)
	expectedT.ToAffine()
	if !expectedT.X.Equals(&sig.t.x) {
		return nil, fmt.Errorf("recovered tweak does not match expected")
	}
	if !expectedT.Y.Equals(&sig.t.y) {
		return nil, fmt.Errorf("recovered tweak does not match expected")
	}

	return t, nil
}

// PublicTweak returns the hidden value multiplied by the generator point.
func (sig *AdaptorSignature) PublicTweak() *secp256k1.JacobianPoint {
	return sig.t.asJacobian()
}

// schnorrEncryptedSign creates an adaptor signature by modifying the nonce in
// the commitment to be the sum of the nonce and the tweak. If the resulting
// signature is summed with the tweak, a valid signature is produced.
func schnorrEncryptedSign(privKey, nonce *secp256k1.ModNScalar, hash []byte, T *secp256k1.JacobianPoint) (*AdaptorSignature, error) {
	// The algorithm for producing an encrypted EC-Schnorr-DCRv0 is as follows:
	// The deviations from the original algorithm are in step 5 and 6.
	//
	// G = curve generator
	// n = curve order
	// d = private key
	// m = message
	// r, s = signature
	// t = hidden value
	// T = t * G
	//
	// 1. Fail if m is not 32 bytes
	// 2. Fail if d = 0 or d >= n
	// 3. Use RFC6979 to generate a deterministic nonce k in [1, n-1]
	//    parameterized by the private key, message being signed, extra data
	//    that identifies the scheme, and an iteration count
	// 4. R = kG
	// 5. Repeat from step 3 if R + T is odd
	// 6. r = (R + T).x (R.x is the x coordinate of the point R + T)
	// 7. e = BLAKE-256(r || m) (Ensure r is padded to 32 bytes)
	// 8. Repeat from step 3 (with iteration + 1) if e >= n
	// 9. s = k - e*d mod n
	// 10. Return (r, s)

	// Step 4,
	//
	// R = kG
	var R secp256k1.JacobianPoint
	k := *nonce
	secp256k1.ScalarBaseMultNonConst(&k, &R)

	// Step 5.
	//
	// Check if R + T is odd. If it is, we need to try again with a new nonce.
	R.ToAffine()
	var rPlusT secp256k1.JacobianPoint
	secp256k1.AddNonConst(T, &R, &rPlusT)
	rPlusT.ToAffine()
	if rPlusT.Y.IsOdd() {
		return nil, fmt.Errorf("need new nonce")
	}

	// Step 6.
	//
	// r = (R + T).x (R.x is the x coordinate of the point R + T)
	r := &rPlusT.X

	// Step 7.
	//
	// e = BLAKE-256(r + t || m) (Ensure r is padded to 32 bytes)
	var commitmentInput [scalarSize * 2]byte
	r.PutBytesUnchecked(commitmentInput[0:scalarSize])
	copy(commitmentInput[scalarSize:], hash[:])
	commitment := blake256.Sum256(commitmentInput[:])

	// Step 8.
	//
	// Repeat from step 1 (with iteration + 1) if e >= N
	var e secp256k1.ModNScalar
	if overflow := e.SetBytes(&commitment); overflow != 0 {
		k.Zero()
		str := "hash of (R || m) too big"
		return nil, errors.New(str)
	}

	// Step 9.
	//
	// s = k - e*d mod n
	s := new(secp256k1.ModNScalar).Mul2(&e, privKey).Negate().Add(&k)
	k.Zero()

	// Step 10.
	//
	// Return (r, s, T)
	affineT := new(secp256k1.JacobianPoint)
	affineT.Set(T)
	affineT.ToAffine()
	t := affinePoint{x: affineT.X, y: affineT.Y}
	return &AdaptorSignature{
		r:           *r,
		s:           *s,
		t:           t,
		pubKeyTweak: true}, nil
}

// zeroArray zeroes the memory of a scalar array.
func zeroArray(a *[scalarSize]byte) {
	for i := 0; i < scalarSize; i++ {
		a[i] = 0x00
	}
}

// PublicKeyTweakedAdaptorSig creates a public key tweaked adaptor signature.
// This is created by a party which does not know the hidden value, but knows
// the point on the secp256k1 curve derived by multiplying the hidden value by
// the generator point. The party that knows the hidden value can use it to
// create a valid signature from the adaptor signature. Then, the valid
// signature can be combined with the adaptor signature to reveal the hidden
// value.
func PublicKeyTweakedAdaptorSig(privKey *secp256k1.PrivateKey, hash []byte, T *secp256k1.JacobianPoint) (*AdaptorSignature, error) {
	// The algorithm for producing an encrypted EC-Schnorr-DCRv0 is as follows:
	// The deviations from the original algorithm are in step 5 and 6.
	//
	// G = curve generator
	// n = curve order
	// d = private key
	// m = message
	// r, s = signature
	// t = hidden value
	// T = t * G
	//
	// 1. Fail if m is not 32 bytes
	// 2. Fail if d = 0 or d >= n
	// 3. Use RFC6979 to generate a deterministic nonce k in [1, n-1]
	//    parameterized by the private key, message being signed, extra data
	//    that identifies the scheme, and an iteration count
	// 4. R = kG
	// 5. Repeat from step 3 if R + T is odd
	// 6. r = (R + T).x (R.x is the x coordinate of the point R + T)
	// 7. e = BLAKE-256(r || m) (Ensure r is padded to 32 bytes)
	// 8. Repeat from step 3 (with iteration + 1) if e >= n
	// 9. s = k - e*d mod n
	// 10. Return (r, s)

	// Step 1.
	//
	// Fail if m is not 32 bytes
	if len(hash) != scalarSize {
		str := fmt.Sprintf("wrong size for message hash (got %v, want %v)",
			len(hash), scalarSize)
		return nil, errors.New(str)
	}

	// Step 2.
	//
	// Fail if d = 0 or d >= n
	privKeyScalar := &privKey.Key
	if privKeyScalar.IsZero() {
		str := "private key is zero"
		return nil, errors.New(str)
	}

	var privKeyBytes [scalarSize]byte
	privKeyScalar.PutBytes(&privKeyBytes)
	defer zeroArray(&privKeyBytes)
	for iteration := uint32(0); ; iteration++ {
		// Step 3.
		//
		// Use RFC6979 to generate a deterministic nonce k in [1, n-1]
		// parameterized by the private key, message being signed, extra data
		// that identifies the scheme, and an iteration count
		k := secp256k1.NonceRFC6979(privKeyBytes[:], hash, rfc6979ExtraDataV0[:],
			nil, iteration)

		// Steps 4-10.
		sig, err := schnorrEncryptedSign(privKeyScalar, k, hash, T)
		k.Zero()
		if err != nil {
			// Try again with a new nonce.
			continue
		}

		return sig, nil
	}
}

func parseSig(sig *schnorr.Signature) (r *secp256k1.FieldVal, s *secp256k1.ModNScalar) {
	sigB := sig.Serialize()
	r, s = new(secp256k1.FieldVal), new(secp256k1.ModNScalar)
	r.SetBytes((*[32]byte)(sigB[0:32]))
	s.SetBytes((*[32]byte)(sigB[32:64]))
	return r, s
}

// PrivateKeyTweakedAdaptorSig creates a private key tweaked adaptor signature.
// This is created by a party which knows the hidden value.
func PrivateKeyTweakedAdaptorSig(sig *schnorr.Signature, pubKey *secp256k1.PublicKey, t *secp256k1.ModNScalar) *AdaptorSignature {
	T := new(secp256k1.JacobianPoint)
	secp256k1.ScalarBaseMultNonConst(t, T)
	T.ToAffine()

	r, s := parseSig(sig)
	tweakedS := new(secp256k1.ModNScalar).Add2(s, t)

	return &AdaptorSignature{
		r: *r,
		s: *tweakedS,
		t: affinePoint{x: T.X, y: T.Y},
	}
}
