package doge

import (
	"encoding/hex"
	"testing"

	dexdoge "decred.org/dcrdex/dex/networks/doge"
	"decred.org/dcrdex/server/asset/btc"
)

func TestCompatibility(t *testing.T) {
	fromHex := func(str string) []byte {
		b, err := hex.DecodeString(str)
		if err != nil {
			t.Fatalf("error decoding %s: %v", str, err)
		}
		return b
	}
	// These scripts and addresses are just copy-pasted from random
	// getrawtransaction output.
	items := &btc.CompatibilityItems{
		P2PKHScript: fromHex("76a914f8f0813eb71c0c5c0a8677b6e8f1e4bb870935fc88ac"),
		PKHAddr:     "DTqNEQLjhn2hf8vK46py9nDohkci2BeAt1",
		P2SHScript:  fromHex("a9140aa26a002d22f88c1f83d35298dc64769fe6e81a87"),
		SHAddr:      "9sQVz2zRbhCAMdXb4NtoLRYzi84qAkGD5r",
	}
	btc.CompatibilityCheck(items, dexdoge.MainNetParams, t)
}
