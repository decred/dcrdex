// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package mesh

import "testing"

func TestNewSecp256k1Signer(t *testing.T) {
	t.Run("nil private key", func(t *testing.T) {
		signer, err := newSecp256k1Signer(nil)
		if err == nil {
			t.Fatalf("expected error for nil private key")
		}
		if signer != nil {
			t.Fatalf("expected nil signer for nil private key")
		}
	})

	t.Run("sign and verify", func(t *testing.T) {
		signer, err := newSecp256k1Signer(testPrivKey())
		if err != nil {
			t.Fatalf("newSecp256k1Signer error: %v", err)
		}

		data := []byte("mesh hello payload")
		sig, err := signer.sign(data)
		if err != nil {
			t.Fatalf("sign error: %v", err)
		}
		if len(sig) == 0 {
			t.Fatalf("empty signature")
		}
		if !signer.verify(sig, data) {
			t.Fatalf("signature did not verify")
		}
		if signer.verify(sig, []byte("mesh hello payload modified")) {
			t.Fatalf("signature verified modified data")
		}
		if signer.verify([]byte("not a der signature"), data) {
			t.Fatalf("malformed signature verified")
		}
	})
}
