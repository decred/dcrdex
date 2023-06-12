package dash

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

	// Mainnet
	mainnet_items := &btctest.CompatibilityItems{
		// Tx: e7169629845406e63e7eafe23f416ecfa47ed6b0dca6a10082a6c850ea9e7dbc
		P2PKHScript:  fromHex("76a914b2dc6a6c880c18fe5c541da9fe44d3fdd1c291d888ac"),
		PKHAddr:      "XrzaA9mCcNFAVa8bV7c52ZUSVArK1jxgqj",
		P2WPKHScript: nil,
		WPKHAddr:     "no segwit",
		P2SHScript:   fromHex("a9144d991c53701252c517564c5b3d081abf27db24ff87"),
		SHAddr:       "7ZUxCpiVEwrKZvAhBDeZgfzxmLeNUsf1qg",
		P2WSHScript:  nil,
		WSHAddr:      "no segwit",
	}
	btctest.CompatibilityCheck(t, mainnet_items, MainNetParams)

	// Testnet (v3)
	testnet_items := &btctest.CompatibilityItems{
		P2PKHScript:  fromHex("76a914b20b6f0f99b631ed085da83575674496600e70ea88ac"),
		PKHAddr:      "ycYrpnWSn8xdZDHFxAgngPacbXhHsFdCoF",
		P2WPKHScript: nil,
		WPKHAddr:     "no segwit",
		P2SHScript:   fromHex("a914fd1feb80b56322a33ea590a996f806e358c7f6f287"),
		SHAddr:       "93VruS4TQUGdnVEyX8HumMcP9fyCcbW3Zi", // "8xx" or "9xx"
		P2WSHScript:  nil,
		WSHAddr:      "no segwit",
	}
	btctest.CompatibilityCheck(t, testnet_items, TestNetParams)
}
