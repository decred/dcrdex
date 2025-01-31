package adaptorsigs

import (
	"testing"

	"decred.org/dcrdex/dex/encode"
	"github.com/decred/dcrd/dcrec/edwards/v2"
	"github.com/decred/dcrd/dcrec/secp256k1/v4"
)

func TestDleqProof(t *testing.T) {
	pubKeysFromSecret := func(secret [32]byte) (*edwards.PublicKey, *secp256k1.PublicKey) {
		epk, _, err := edwards.PrivKeyFromScalar(secret[:])
		if err != nil {
			t.Fatalf("PrivKeyFromScalar error: %v", err)
		}

		scalarSecret := new(secp256k1.ModNScalar)
		overflow := scalarSecret.SetBytes(&secret)
		if overflow > 0 {
			t.Fatalf("overflow: %d", overflow)
		}
		spk := secp256k1.NewPrivateKey(scalarSecret)

		return epk.PubKey(), spk.PubKey()
	}

	var secret [32]byte
	copy(secret[1:], encode.RandomBytes(31))

	epk, spk := pubKeysFromSecret(secret)
	proof, err := ProveDLEQ(secret[:])
	if err != nil {
		t.Fatalf("ProveDLEQ error: %v", err)
	}
	err = VerifyDLEQ(spk, epk, proof)
	if err != nil {
		t.Fatalf("VerifyDLEQ error: %v", err)
	}

	secret[31] += 1
	badEpk, badSpk := pubKeysFromSecret(secret)
	err = VerifyDLEQ(badSpk, epk, proof)
	if err == nil {
		t.Fatalf("badSpk should not verify")
	}
	err = VerifyDLEQ(spk, badEpk, proof)
	if err == nil {
		t.Fatalf("badEpk should not verify")
	}

	extractedSecp, err := ExtractSecp256k1PubKeyFromProof(proof)
	if err != nil {
		t.Fatalf("ExtractSecp256k1PubKeyFromProof error: %v", err)
	}

	if !extractedSecp.IsEqual(spk) {
		t.Fatalf("extractedSecp != spk")
	}
}
