package ltc

import (
	"encoding/hex"
	"testing"

	dexltc "decred.org/dcrdex/dex/networks/ltc"
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
		P2PKHScript:  fromHex("76a9146e137cab355e7a35d7546470dc6db403b7bd47ea88ac"),
		PKHAddr:      "LVFywJ1DHYbN2uYjWNCJGcLJJhL3boaiSy",
		P2WPKHScript: fromHex("00144820955c5ecf2fd7a0864d8ae7572f17b1e8fb91"),
		WPKHAddr:     "ltc1qfqsf2hz7euha0gyxfk9ww4e0z7c737u3gp8elg",
		P2SHScript:   fromHex("a914dec29f203dc46e81adbb5a999fcdf0932cd3125787"),
		SHAddr:       "MUD1PZWk9UBmFeLT7oksFzus416x8X1TZP",
		P2WSHScript:  fromHex("0020adb044cf4da15506e73c6d3928737229e64227f29cd86dcc34b7353c1f5560eb"),
		WSHAddr:      "ltc1q4kcyfn6d592sdeeud5ujsumj98nyyfljnnvxmnp5ku6nc864vr4sawj2gw",
	}
	btc.CompatibilityCheck(items, dexltc.MainNetParams, t)
}
