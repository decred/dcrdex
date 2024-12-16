package firo

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"testing"

	dexfiro "decred.org/dcrdex/dex/networks/firo"
	"github.com/btcsuite/btcd/btcutil"
)

const (
	exxAddress           = "EXXKcAcVWXeG7S9aiXXGuGNZkWdB9XuSbJ1z"
	scriptAddress        = "386ed39285803b1782d0e363897f1a81a5b87421"
	encodedAsPKH         = "a5rrM1DY9XTRucbNrJQDtDc6GiEbcX7jRd"
	testnetExtAddress    = "EXTSnBDP57YoFRzLwHQoP1grxh9j52FKmRBY"
	testnetScriptAddress = "963f2fd5ee2ee37d0b327794fc915d01343a4891"
	testnetEncodedAsPKH  = "TPfe48h75oMJ2LqXZtYjodumPjMUx64PGK"
)

///////////////////////////////////////////////////////////////////////////////
// Mainnet
///////////////////////////////////////////////////////////////////////////////

func TestDecodeExxAddress(t *testing.T) {
	addr, err := decodeExxAddress(exxAddress, dexfiro.MainNetParams)
	if err != nil {
		t.Fatalf("addr=%v - %v", addr, err)
	}

	switch ty := addr.(type) {
	case btcutil.Address:
		fmt.Printf("type=%T\n", ty)
	default:
		t.Fatalf("invalid type=%T", ty)
	}

	if !addr.IsForNet(dexfiro.MainNetParams) {
		t.Fatalf("IsForNet failed")
	}
	scriptAddressB, err := hex.DecodeString(scriptAddress)
	if err != nil {
		t.Fatalf("hex decode error: %v", err)
	}
	if !bytes.Equal(addr.ScriptAddress(), scriptAddressB) {
		t.Fatalf("ScriptAddress failed")
	}
	s := addr.String()
	if s != encodedAsPKH {
		t.Fatalf("String failed expected %s got %s", encodedAsPKH, s)
	}
}

func TestBuildExxPayToScript(t *testing.T) {
	addr, err := decodeExxAddress(exxAddress, dexfiro.MainNetParams)
	if err != nil {
		t.Fatalf("addr=%v - %v", addr, err)
	}
	script, err := buildExxPayToScript(addr, exxAddress)
	if err != nil {
		t.Fatal(err)
	}
	if len(script) != SCRIPT_LEN {
		t.Fatalf("wrong script length - expected %d got %d", SCRIPT_LEN, len(script))
	}
}

///////////////////////////////////////////////////////////////////////////////
// Testnet
///////////////////////////////////////////////////////////////////////////////

func TestDecodeExtAddress(t *testing.T) {
	addr, err := decodeExxAddress(testnetExtAddress, dexfiro.TestNetParams)
	if err != nil {
		t.Fatalf("testnet - addr=%v - %v", addr, err)
	}

	switch ty := addr.(type) {
	case btcutil.Address:
		fmt.Printf("testnet - type=%T\n", ty)
	default:
		t.Fatalf("testnet - invalid type=%T", ty)
	}

	if !addr.IsForNet(dexfiro.TestNetParams) {
		t.Fatalf("testnet - IsForNet failed")
	}
	testnetScriptAddressB, err := hex.DecodeString(testnetScriptAddress)
	if err != nil {
		t.Fatalf("testnet - hex decode error: %v", err)
	}
	if !bytes.Equal(addr.ScriptAddress(), testnetScriptAddressB) {
		t.Fatalf("testnet - ScriptAddress failed")
	}
	s := addr.String()
	if s != testnetEncodedAsPKH {
		t.Fatalf("testnet - String failed expected %s got %s", encodedAsPKH, s)
	}
}

func TestBuildExtPayToScript(t *testing.T) {
	addr, err := decodeExxAddress(testnetExtAddress, dexfiro.TestNetParams)
	if err != nil {
		t.Fatalf("testnet - addr=%v - %v", addr, err)
	}
	script, err := buildExxPayToScript(addr, testnetExtAddress)
	if err != nil {
		t.Fatal(err)
	}
	if len(script) != SCRIPT_LEN {
		t.Fatalf("testnet - wrong script length - expected %d got %d", SCRIPT_LEN, len(script))
	}
}
