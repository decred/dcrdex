package adaptorsigs

import (
	"bytes"
	"fmt"
	"math/big"

	"decred.org/dcrdex/dex/utils"
	filipEdwards "filippo.io/edwards25519"
	"filippo.io/edwards25519/field"
	"github.com/athanorlabs/go-dleq"
	dleqEdwards "github.com/athanorlabs/go-dleq/ed25519"
	dleqSecp "github.com/athanorlabs/go-dleq/secp256k1"
	dcrEdwards "github.com/decred/dcrd/dcrec/edwards/v2"
	dcrSecp "github.com/decred/dcrd/dcrec/secp256k1/v4"
)

// ProveDLEQ generates a proof that the public keys generated from the provided
// secret on the secp256k1 and edwards25519 curves are derived from the same
// secret. Secret should be passed in as a 32-byte big-endian array, and should
// at most have 252 bits set.
func ProveDLEQ(secret []byte) ([]byte, error) {
	if len(secret) != 32 {
		return nil, fmt.Errorf("secret must be 32 bytes")
	}

	secretB := [32]byte{}
	copy(secretB[:], secret)
	utils.ReverseSlice(secretB[:])

	proof, err := dleq.NewProof(dleqEdwards.NewCurve(), dleqSecp.NewCurve(), secretB)
	if err != nil {
		return nil, err
	}

	return proof.Serialize(), nil
}

// bigIntToLittleEndian32 converts a big.Int to a 32-byte little-endian byte array.
func bigIntToLittleEndian32(i *big.Int) [32]byte {
	var p [32]byte
	// Get the big-endian bytes of the integer.
	b := i.Bytes()
	// Copy the bytes into our 32-byte array, right-aligned.
	// This effectively left-pads with zeros.
	copy(p[32-len(b):], b)
	// Reverse the bytes to convert from big-endian to little-endian.
	utils.ReverseSlice(p[:])
	return p
}

// edwardsPointsEqual checks equality of edwards curve points in the dcrec
// and go-dleq libraries.
func edwardsPointsEqual(dcrPK *dcrEdwards.PublicKey, dleqPK *dleqEdwards.PointImpl) bool {
	xB := bigIntToLittleEndian32(dcrPK.GetX())
	yB := bigIntToLittleEndian32(dcrPK.GetY())

	x := new(field.Element)
	y := new(field.Element)
	z := new(field.Element)
	t := new(field.Element)

	if _, err := x.SetBytes(xB[:]); err != nil {
		return false
	}

	if _, err := y.SetBytes(yB[:]); err != nil {
		return false
	}

	z.One()
	t.Multiply(x, y)

	point := filipEdwards.NewIdentityPoint()
	point.SetExtendedCoordinates(x, y, z, t)

	expDLEQ := dleqEdwards.NewPoint(point)
	return dleqPK.Equals(expDLEQ)
}

// VerifyDLEQ verifies a DLEQ proof that the public keys are generated from the
// same secret.
func VerifyDLEQ(spk *dcrSecp.PublicKey, epk *dcrEdwards.PublicKey, proofB []byte) error {
	edwardsCurve := dleqEdwards.NewCurve()
	secpCurve := dleqSecp.NewCurve()

	proof := new(dleq.Proof)
	if err := proof.Deserialize(edwardsCurve, secpCurve, proofB); err != nil {
		return err
	}
	if err := proof.Verify(edwardsCurve, secpCurve); err != nil {
		return err
	}

	edwardsPoint, ok := proof.CommitmentA.(*dleqEdwards.PointImpl)
	if !ok {
		return fmt.Errorf("expected ed25519.Point, got %T", proof.CommitmentA)
	}
	if !edwardsPointsEqual(epk, edwardsPoint) {
		return fmt.Errorf("ed25519 points do not match")
	}

	secpPoint, ok := proof.CommitmentB.(*dleqSecp.PointImpl)
	if !ok {
		return fmt.Errorf("expected secp256k1.Point, got %T", proof.CommitmentB)
	}
	if !bytes.Equal(secpPoint.Encode(), spk.SerializeCompressed()) {
		return fmt.Errorf("secp256k1 points do not match")
	}

	return nil
}

// ExtractSecp256k1PubKeyFromProof extracts the secp256k1 public key from a
// DLEQ proof.
func ExtractSecp256k1PubKeyFromProof(proofB []byte) (*dcrSecp.PublicKey, error) {
	proof := new(dleq.Proof)
	if err := proof.Deserialize(dleqEdwards.NewCurve(), dleqSecp.NewCurve(), proofB); err != nil {
		return nil, err
	}

	secpPoint, ok := proof.CommitmentB.(*dleqSecp.PointImpl)
	if !ok {
		return nil, fmt.Errorf("expected secp256k1.Point, got %T", proof.CommitmentB)
	}

	return dcrSecp.ParsePubKey(secpPoint.Encode())
}
