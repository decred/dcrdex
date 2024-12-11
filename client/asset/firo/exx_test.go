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
	exxAddress = "EXXKcAcVWXeG7S9aiXXGuGNZkWdB9XuSbJ1z"
	decoded    = "386ed39285803b1782d0e363897f1a81a5b87421"
	//            386ed39285803b1782d0e363897f1a81a5b87421
	//  e0 76a914 386ed39285803b1782d0e363897f1a81a5b87421 88ac
	// validateaddress firo-qt, electrum-firo just says 'true'
	encodedAsPKH = "a5rrM1DY9XTRucbNrJQDtDc6GiEbcX7jRd"
)

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
	decodedB, err := hex.DecodeString(decoded)
	if err != nil {
		t.Fatalf("hex decode error: %v", err)
	}
	if !bytes.Equal(addr.ScriptAddress(), decodedB) {
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
