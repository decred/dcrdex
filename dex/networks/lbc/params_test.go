// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package lbc

import (
	"testing"

	"github.com/btcsuite/btcd/btcutil"
	"github.com/btcsuite/btcd/chaincfg"
)

func TestAddressEncoding(t *testing.T) {
	// P2PKH mainnet: PubKeyHashAddrID 0x55 → addresses typically start with 'b'
	pkHash := make([]byte, 20)
	for i := range pkHash {
		pkHash[i] = byte(i + 1)
	}

	addr, err := btcutil.NewAddressPubKeyHash(pkHash, MainNetParams)
	if err != nil {
		t.Fatalf("NewAddressPubKeyHash mainnet: %v", err)
	}
	decoded, err := btcutil.DecodeAddress(addr.EncodeAddress(), MainNetParams)
	if err != nil {
		t.Fatalf("DecodeAddress mainnet: %v", err)
	}
	if decoded.String() != addr.String() {
		t.Fatalf("round-trip mismatch: %s vs %s", decoded, addr)
	}

	// Bech32 mainnet
	witAddr, err := btcutil.NewAddressWitnessPubKeyHash(pkHash, MainNetParams)
	if err != nil {
		t.Fatalf("NewAddressWitnessPubKeyHash: %v", err)
	}
	if witAddr.EncodeAddress()[:3] != "lbc" {
		t.Fatalf("expected lbc bech32 prefix, got %s", witAddr.EncodeAddress())
	}

	// Regtest bech32
	witReg, err := btcutil.NewAddressWitnessPubKeyHash(pkHash, RegressionNetParams)
	if err != nil {
		t.Fatalf("regtest witness addr: %v", err)
	}
	if witReg.EncodeAddress()[:4] != "rlbc" {
		t.Fatalf("expected rlbc prefix, got %s", witReg.EncodeAddress())
	}

	// Ensure params registered
	for _, p := range []*chaincfg.Params{MainNetParams, TestNet3Params, RegressionNetParams} {
		if p.Name == "" {
			t.Fatal("empty network name")
		}
	}
}
