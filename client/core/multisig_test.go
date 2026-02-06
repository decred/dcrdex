package core

import (
	"bytes"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"decred.org/dcrdex/client/asset"
	"github.com/decred/dcrd/dcrec/secp256k1/v4"
)

func TestDeriveMultisigXPriv(t *testing.T) {
	seed := make([]byte, 32)
	for i := range seed {
		seed[i] = byte(i)
	}

	xPriv, err := deriveMultisigXPriv(seed)
	if err != nil {
		t.Fatalf("deriveMultisigXPriv error: %v", err)
	}
	if xPriv == nil {
		t.Fatal("expected non-nil xPriv")
	}

	// Derive again with same seed should give same result
	xPriv2, err := deriveMultisigXPriv(seed)
	if err != nil {
		t.Fatalf("deriveMultisigXPriv error on second call: %v", err)
	}

	if xPriv.String() != xPriv2.String() {
		t.Fatal("same seed should produce same xPriv")
	}

	// Different seed should produce different xPriv
	seed2 := make([]byte, 32)
	for i := range seed2 {
		seed2[i] = byte(i + 1)
	}
	xPriv3, err := deriveMultisigXPriv(seed2)
	if err != nil {
		t.Fatalf("deriveMultisigXPriv error with different seed: %v", err)
	}
	if xPriv.String() == xPriv3.String() {
		t.Fatal("different seed should produce different xPriv")
	}
}

func TestDeriveMultisigKey(t *testing.T) {
	seed := make([]byte, 32)
	for i := range seed {
		seed[i] = byte(i)
	}

	xPriv, err := deriveMultisigXPriv(seed)
	if err != nil {
		t.Fatalf("deriveMultisigXPriv error: %v", err)
	}

	assetID := uint32(42) // DCR
	multisigIndex := uint32(0)

	priv, err := deriveMultisigKey(xPriv, assetID, multisigIndex)
	if err != nil {
		t.Fatalf("deriveMultisigKey error: %v", err)
	}
	if priv == nil {
		t.Fatal("expected non-nil private key")
	}

	// Same inputs should give same key
	priv2, err := deriveMultisigKey(xPriv, assetID, multisigIndex)
	if err != nil {
		t.Fatalf("deriveMultisigKey error on second call: %v", err)
	}
	if !bytes.Equal(priv.Serialize(), priv2.Serialize()) {
		t.Fatal("same inputs should produce same key")
	}

	// Different index should give different key
	priv3, err := deriveMultisigKey(xPriv, assetID, multisigIndex+1)
	if err != nil {
		t.Fatalf("deriveMultisigKey error with different index: %v", err)
	}
	if bytes.Equal(priv.Serialize(), priv3.Serialize()) {
		t.Fatal("different index should produce different key")
	}

	// Different asset should give different key
	priv4, err := deriveMultisigKey(xPriv, assetID+1, multisigIndex)
	if err != nil {
		t.Fatalf("deriveMultisigKey error with different asset: %v", err)
	}
	if bytes.Equal(priv.Serialize(), priv4.Serialize()) {
		t.Fatal("different asset should produce different key")
	}
}

func TestParsePaymentMultisigCSV(t *testing.T) {
	tmpDir := t.TempDir()
	// Generate test pubkeys
	pk1 := secp256k1.PrivKeyFromBytes(make([]byte, 32)).PubKey()
	pk2Seed := make([]byte, 32)
	pk2Seed[0] = 1
	pk2 := secp256k1.PrivKeyFromBytes(pk2Seed).PubKey()

	pk1Hex := hex.EncodeToString(pk1.SerializeCompressed())
	pk2Hex := hex.EncodeToString(pk2.SerializeCompressed())

	tests := []struct {
		name        string
		csvContent  string
		wantErr     bool
		checkResult func(t *testing.T, pm *asset.PaymentMultisig)
	}{{
		name: "valid csv",
		csvContent: `0
42
1h
2
` + pk1Hex + `,` + pk2Hex + `
SsfLJbjXHTjGGqNd7uaNHL8EukmNgEQDNbg,40.1
`,
		wantErr: false,
		checkResult: func(t *testing.T, pm *asset.PaymentMultisig) {
			if pm.AssetID != 42 {
				t.Errorf("expected asset ID 42, got %d", pm.AssetID)
			}
			if pm.NRequired != 2 {
				t.Errorf("expected nRequired 2, got %d", pm.NRequired)
			}
			if len(pm.SignerXpubs) != 2 {
				t.Errorf("expected 2 xpubs, got %d", len(pm.SignerXpubs))
			}
			if len(pm.AddrToVal) != 1 {
				t.Errorf("expected 1 address, got %d", len(pm.AddrToVal))
			}
			if pm.AddrToVal["SsfLJbjXHTjGGqNd7uaNHL8EukmNgEQDNbg"] != 40.1 {
				t.Error("wrong amount for address")
			}
		},
	}, {
		name: "with comments",
		csvContent: `; This is a comment
0
; asset ID
42
; duration
2h
; nRequired
1
` + pk1Hex + `,` + pk2Hex + `
; addresses
SsfLJbjXHTjGGqNd7uaNHL8EukmNgEQDNbg,10
`,
		wantErr: false,
		checkResult: func(t *testing.T, pm *asset.PaymentMultisig) {
			if pm.NRequired != 1 {
				t.Errorf("expected nRequired 1, got %d", pm.NRequired)
			}
		},
	}, {
		name: "invalid version",
		csvContent: `1
42
1h
2
` + pk1Hex + `,` + pk2Hex + `
SsfLJbjXHTjGGqNd7uaNHL8EukmNgEQDNbg,40.1
`,
		wantErr: true,
	}, {
		name: "more sigs required than signers",
		csvContent: `0
42
1h
3
` + pk1Hex + `,` + pk2Hex + `
SsfLJbjXHTjGGqNd7uaNHL8EukmNgEQDNbg,40.1
`,
		wantErr: true,
	}, {
		name: "invalid pubkey",
		csvContent: `0
42
1h
1
invalidhex,` + pk2Hex + `
SsfLJbjXHTjGGqNd7uaNHL8EukmNgEQDNbg,40.1
`,
		wantErr: true,
	}, {
		name: "negative amount",
		csvContent: `0
42
1h
1
` + pk1Hex + `,` + pk2Hex + `
SsfLJbjXHTjGGqNd7uaNHL8EukmNgEQDNbg,-10
`,
		wantErr: true,
	}, {
		name:       "empty file",
		csvContent: ``,
		wantErr:    true,
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// Write test CSV file
			csvPath := filepath.Join(tmpDir, test.name+".csv")
			if err := os.WriteFile(csvPath, []byte(test.csvContent), 0644); err != nil {
				t.Fatalf("failed to write test file: %v", err)
			}

			// Create a minimal Core just to test parsing
			c := &Core{}
			pm, _, err := c.parsePaymentMultisigCVS(csvPath)

			if test.wantErr {
				if err == nil {
					t.Error("expected error but got none")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if pm == nil {
				t.Fatal("expected non-nil PaymentMultisig")
			}
			if test.checkResult != nil {
				test.checkResult(t, pm)
			}
		})
	}
}

func TestParsePaymentMultisigCSVWithTx(t *testing.T) {
	tmpDir := t.TempDir()

	pk1 := secp256k1.PrivKeyFromBytes(make([]byte, 32)).PubKey()
	pk2Seed := make([]byte, 32)
	pk2Seed[0] = 1
	pk2 := secp256k1.PrivKeyFromBytes(pk2Seed).PubKey()

	pk1Hex := hex.EncodeToString(pk1.SerializeCompressed())
	pk2Hex := hex.EncodeToString(pk2.SerializeCompressed())

	csvContent := `0
42
1h
2
` + pk1Hex + `,` + pk2Hex + `
SsfLJbjXHTjGGqNd7uaNHL8EukmNgEQDNbg,40.1
{"txhex":"0100","hassigs":[false,false]}`

	csvPath := filepath.Join(tmpDir, "with_tx.csv")
	if err := os.WriteFile(csvPath, []byte(csvContent), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	c := &Core{}
	pm, _, err := c.parsePaymentMultisigCVS(csvPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pm.SpendingTx == nil {
		t.Fatal("expected SpendingTx to be parsed")
	}
	if pm.SpendingTx.TxHex != "0100" {
		t.Errorf("expected TxHex '0100', got '%s'", pm.SpendingTx.TxHex)
	}
	if len(pm.SpendingTx.HasSigs) != 2 {
		t.Errorf("expected 2 HasSigs entries, got %d", len(pm.SpendingTx.HasSigs))
	}
}

func TestParsePaymentMultisigCSVFileNotFound(t *testing.T) {
	c := &Core{}
	_, _, err := c.parsePaymentMultisigCVS("/nonexistent/path/file.csv")
	if err == nil {
		t.Error("expected error for non-existent file")
	}
}
