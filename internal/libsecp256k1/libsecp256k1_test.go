//go:build libsecp256k1

package libsecp256k1

import (
	"bytes"
	"math/rand"
	"testing"

	"github.com/decred/dcrd/dcrec/edwards/v2"
	"github.com/decred/dcrd/dcrec/secp256k1/v4"
)

func randBytes(n int) []byte {
	b := make([]byte, n)
	rand.Read(b)
	return b
}

func TestEd25519DleagProve(t *testing.T) {
	tests := []struct {
		name string
	}{{
		name: "ok",
	}}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			pk, err := edwards.GeneratePrivateKey()
			if err != nil {
				t.Fatal(err)
			}
			sPk := secp256k1.PrivKeyFromBytes(pk.Serialize())
			proof, err := Ed25519DleagProve(pk)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(sPk.PubKey().SerializeCompressed(), proof[:33]) {
				t.Fatal("first 33 bytes of proof not equal to secp256k1 pubkey")
			}
		})
	}
}

func TestEd25519DleagVerify(t *testing.T) {
	pk, err := edwards.GeneratePrivateKey()
	if err != nil {
		panic(err)
	}
	proof, err := Ed25519DleagProve(pk)
	if err != nil {
		panic(err)
	}
	tests := []struct {
		name  string
		proof [ProofLen]byte
		ok    bool
	}{{
		name:  "ok",
		proof: proof,
		ok:    true,
	}, {
		name: "bad proof",
		proof: func() (p [ProofLen]byte) {
			copy(p[:], proof[:])
			p[0] ^= p[0]
			return p
		}(),
	}}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ok := Ed25519DleagVerify(test.proof)
			if ok != test.ok {
				t.Fatalf("want %v but got %v", test.ok, ok)
			}
		})
	}
}

func TestEcdsaotvesEncSign(t *testing.T) {
	tests := []struct {
		name string
	}{{
		name: "ok",
	}}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			signPk, err := secp256k1.GeneratePrivateKey()
			if err != nil {
				t.Fatal(err)
			}
			encPk, err := secp256k1.GeneratePrivateKey()
			if err != nil {
				t.Fatal(err)
			}
			h := randBytes(32)
			var hash [32]byte
			copy(hash[:], h)
			_, err = EcdsaotvesEncSign(signPk, encPk.PubKey(), hash)
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestEcdsaotvesEncVerify(t *testing.T) {
	signPk, err := secp256k1.GeneratePrivateKey()
	if err != nil {
		t.Fatal(err)
	}
	encPk, err := secp256k1.GeneratePrivateKey()
	if err != nil {
		t.Fatal(err)
	}
	h := randBytes(32)
	var hash [32]byte
	copy(hash[:], h)
	ct, err := EcdsaotvesEncSign(signPk, encPk.PubKey(), hash)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		ok   bool
		ct   [196]byte
	}{{
		name: "ok",
		ct:   ct,
		ok:   true,
	}, {
		name: "bad sig",
		ct: func() (c [CTLen]byte) {
			copy(c[:], ct[:])
			c[0] ^= c[0]
			return c
		}(),
	}}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ok := EcdsaotvesEncVerify(signPk.PubKey(), encPk.PubKey(), hash, test.ct)
			if ok != test.ok {
				t.Fatalf("want %v but got %v", test.ok, ok)
			}
		})
	}
}

func TestEcdsaotvesDecSig(t *testing.T) {
	signPk, err := secp256k1.GeneratePrivateKey()
	if err != nil {
		t.Fatal(err)
	}
	encPk, err := secp256k1.GeneratePrivateKey()
	if err != nil {
		t.Fatal(err)
	}
	h := randBytes(32)
	var hash [32]byte
	copy(hash[:], h)
	ct, err := EcdsaotvesEncSign(signPk, encPk.PubKey(), hash)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
	}{{
		name: "ok",
	}}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := EcdsaotvesDecSig(encPk, ct)
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestEcdsaotvesRecEncKey(t *testing.T) {
	signPk, err := secp256k1.GeneratePrivateKey()
	if err != nil {
		t.Fatal(err)
	}
	encPk, err := secp256k1.GeneratePrivateKey()
	if err != nil {
		t.Fatal(err)
	}
	h := randBytes(32)
	var hash [32]byte
	copy(hash[:], h)
	ct, err := EcdsaotvesEncSign(signPk, encPk.PubKey(), hash)
	if err != nil {
		t.Fatal(err)
	}
	sig, err := EcdsaotvesDecSig(encPk, ct)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
	}{{
		name: "ok",
	}}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			pk, err := EcdsaotvesRecEncKey(encPk.PubKey(), ct, sig)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(pk.Serialize(), encPk.Serialize()) {
				t.Fatal("private keys not equal")
			}
		})
	}
}
