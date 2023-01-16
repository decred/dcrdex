package zec

import (
	"encoding/hex"
	"testing"

	btctest "decred.org/dcrdex/dex/networks/btc/test"
)

func TestCompatibility(t *testing.T) {
	fromHex := func(str string) []byte {
		b, err := hex.DecodeString(str)
		if err != nil {
			t.Fatalf("error decoding %s: %v", str, err)
		}
		return b
	}

	pkhAddr := "t1SqYLhzHyGoWwatRNGrTt4ueqivKdJpFY4"
	btcPkhAddr, err := DecodeAddress(pkhAddr, MainNetAddressParams, MainNetParams)
	if err != nil {
		t.Fatalf("error decoding p2pkh address: %v", err)
	}

	shAddr := "t3ZJCdehVh9MTm6BaKWZmWy5Hsw7PhJxmTc"
	btcShAddr, err := DecodeAddress(shAddr, MainNetAddressParams, MainNetParams)
	if err != nil {
		t.Fatalf("error decoding p2sh address: %v", err)
	}

	items := &btctest.CompatibilityItems{
		P2PKHScript: fromHex("76a91462553d6a85afe7753cbe8dc57c7f34f6a8efd79f88ac"),
		PKHAddr:     btcPkhAddr.String(),
		P2SHScript:  fromHex("a914a19f5d7d23bbbff0695363f932c8d67c0169963f87"),
		SHAddr:      btcShAddr.String(),
	}
	btctest.CompatibilityCheck(t, items, MainNetParams)
}
