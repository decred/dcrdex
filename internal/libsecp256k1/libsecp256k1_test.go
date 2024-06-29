//go:build libsecp256k1

package libsecp256k1

import (
	"testing"

	"github.com/decred/dcrd/dcrec/edwards/v2"
)

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
			_, err = Ed25519DleagProve(pk)
			if err != nil {
				t.Fatal(err)
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
		proof [proofLength]byte
		ok    bool
	}{{
		name:  "ok",
		proof: proof,
		ok:    true,
	}, {
		name: "bad proof",
		proof: func() (p [proofLength]byte) {
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
