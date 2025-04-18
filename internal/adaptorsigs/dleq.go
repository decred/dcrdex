package adaptorsigs

import (
	"bytes"
	"fmt"

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
// secret.
func ProveDLEQ(secret []byte) ([]byte, error) {
	if len(secret) != 32 {
		return nil, fmt.Errorf("secret must be 32 bytes")
	}

	secretCopy := make([]byte, len(secret))
	copy(secretCopy, secret)
	utils.ReverseSlice(secretCopy)
	secretB := [32]byte{}
	copy(secretB[:], secretCopy)

	proof, err := dleq.NewProof(dleqEdwards.NewCurve(), dleqSecp.NewCurve(), secretB)
	if err != nil {
		return nil, err
	}

	return proof.Serialize(), nil
}

// edwardsPointsEqual checks equality of edwards curve points in the dcrec
// and go-dleq libraries.
func edwardsPointsEqual(dcrPK *dcrEdwards.PublicKey, dleqPK *dleqEdwards.PointImpl) bool {
	xB := dcrPK.GetX().Bytes()
	yB := dcrPK.GetY().Bytes()
	utils.ReverseSlice(xB)
	utils.ReverseSlice(yB)

	x := new(field.Element)
	y := new(field.Element)
	z := new(field.Element)
	t := new(field.Element)

	x.SetBytes(xB)
	y.SetBytes(yB)
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
