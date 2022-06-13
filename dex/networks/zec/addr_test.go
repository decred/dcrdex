package zec

import (
	"bytes"
	"encoding/hex"
	"testing"
)

func TestAddress(t *testing.T) {
	pkHash, _ := hex.DecodeString("0ca584c97d2c84ea296524ac89f2febcf1094347")
	addr := "t1K2UQ5VzGHGC1ZPqGJXXSocxtjo5s6peSJ"

	btcAddr, err := DecodeAddress(addr, MainNetAddressParams, MainNetParams)
	if err != nil {
		t.Fatalf("DecodeAddress error: %v", err)
	}

	if !bytes.Equal(btcAddr.ScriptAddress(), pkHash) {
		t.Fatalf("wrong script address")
	}

	reAddr, err := RecodeAddress(btcAddr.String(), MainNetAddressParams, MainNetParams)
	if err != nil {
		t.Fatalf("RecodeAddress error: %v", err)
	}

	if reAddr != addr {
		t.Fatalf("wrong recoded address. expected %s, got %s", addr, reAddr)
	}
}
