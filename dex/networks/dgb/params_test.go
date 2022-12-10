package dgb

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
	// These scripts and addresses are just copy-pasted from random
	// getrawtransaction output.
	items := &btctest.CompatibilityItems{
		P2PKHScript:  fromHex("76a914621b33eb176c7800f5da7b71765d091b1592e58788ac"),
		PKHAddr:      "DE5qM9Bepj1bra5cQE8wZfgGik2e11YhGz",
		P2WPKHScript: fromHex("0014289593f256d5e258f8baa9f9f309187cb4c982d0"),
		WPKHAddr:     "dgb1q9z2e8ujk6h39379648ulxzgc0j6vnqksyznpa9",
		P2SHScript:   fromHex("a914473219ec32e225dfed8e21d96c2405626ed8981087"),
		SHAddr:       "STnT2ZLedFDsrBZpohp7HrTFyGzXdJMoV4",
		P2WSHScript:  fromHex("0020dd779dad5ed7f478e8d41600a9e9ade3904f8113796207322fc829107d353a78"),
		WSHAddr:      "dgb1qm4memt276l6836x5zcq2n6dduwgylqgn093qwv30eq53qlf48fuq5jjcje",
	}
	btctest.CompatibilityCheck(t, items, MainNetParams)
}
