package firo

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"testing"

	"decred.org/dcrdex/client/asset/btc"
	dexfiro "decred.org/dcrdex/dex/networks/firo"
	"github.com/btcsuite/btcd/btcutil"
)

const (
	exxAddress    = "EXXKcAcVWXeG7S9aiXXGuGNZkWdB9XuSbJ1z"
	scriptAddress = "386ed39285803b1782d0e363897f1a81a5b87421"

	testnetExtAddress    = "EXTSnBDP57YoFRzLwHQoP1grxh9j52FKmRBY"
	testnetScriptAddress = "963f2fd5ee2ee37d0b327794fc915d01343a4891"

	// Example: e0 76a914 386ed39285803b1782d0e363897f1a81a5b87421 88ac
	scriptLenEXX = 1 + 3 + ripemd160HashSize + 2
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
	case btcutil.Address, *addressEXX:
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
	if s != exxAddress {
		t.Fatalf("String failed expected %s got %s", exxAddress, s)
	}
	enc := addr.EncodeAddress()
	if enc != exxAddress {
		t.Fatalf("EncodeAddress failed expected %s got %s", exxAddress, enc)
	}
}

func TestBuildExxPayToScript(t *testing.T) {
	addr, err := decodeExxAddress(exxAddress, dexfiro.MainNetParams)
	if err != nil {
		t.Fatalf("addr=%v - %v", addr, err)
	}
	var script []byte
	if scripter, is := addr.(btc.PaymentScripter); is {
		script, err = scripter.PaymentScript()
		if err != nil {
			t.Fatal(err)
		}
	} else {
		t.Fatal("addr does not implement btc.PaymentScripter")
	}
	if len(script) != scriptLenEXX {
		t.Fatalf("wrong script length - expected %d got %d", scriptLenEXX, len(script))
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
	if s != testnetExtAddress {
		t.Fatalf("testnet - String failed expected %s got %s", testnetExtAddress, s)
	}
	enc := addr.EncodeAddress()
	if enc != testnetExtAddress {
		t.Fatalf("EncodeAddress failed expected %s got %s", testnetExtAddress, enc)
	}
}

func TestBuildExtPayToScript(t *testing.T) {
	addr, err := decodeExxAddress(testnetExtAddress, dexfiro.TestNetParams)
	if err != nil {
		t.Fatalf("testnet - addr=%v - %v", addr, err)
	}
	var script []byte
	if scripter, is := addr.(btc.PaymentScripter); is {
		script, err = scripter.PaymentScript()
		if err != nil {
			t.Fatal(err)
		}
	} else {
		t.Fatal("addr does not implement btc.PaymentScripter")
	}
	if len(script) != scriptLenEXX {
		t.Fatalf("wrong script length - expected %d got %d", scriptLenEXX, len(script))
	}
}
