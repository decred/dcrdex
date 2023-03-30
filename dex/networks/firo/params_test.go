package firo

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

	// These scripts and addresses are just copy-pasted from random getrawtransaction output
	//
	// Mainnet
	items := &btctest.CompatibilityItems{
		P2PKHScript:  fromHex("76a9145d1dc660d9f4c526f21e62dae503c9f5f18d11ac88ac"),
		PKHAddr:      "a9CpFYRZYrdGc2MgD5bQAgEXXAJNyxe12j",
		P2WPKHScript: nil,
		WPKHAddr:     "no segwit",
		P2SHScript:   fromHex("a9148d1ba9c8def4ac369f7b7162f68b1ba764bea91387"),
		SHAddr:       "43ELJtBmtwxZcscErd4S3QFqPVbs4b6a87",
		P2WSHScript:  nil,
		WSHAddr:      "no segwit",
	}
	btctest.CompatibilityCheck(t, items, MainNetParams)

	// Testnet3
	testnet_items := &btctest.CompatibilityItems{
		P2PKHScript:  fromHex("76a914c6d9103ea51374752bfe8360b6a49d548ce0ca2b88ac"),
		PKHAddr:      "TU6cpsgsrgxfVEpTBoQsdeZppTUZ4K3o3h",
		P2WPKHScript: nil,
		WPKHAddr:     "no segwit",
		P2SHScript:   fromHex("a9145988a1ddfb12a5b4a7701fa327d262212ac642b587"),
		SHAddr:       "2EmKn5nDjAbU7jHPCXxqUBE4e8owhXGFfbB",
		P2WSHScript:  nil,
		WSHAddr:      "no segwit",
	}
	btctest.CompatibilityCheck(t, testnet_items, TestNetParams)
}
