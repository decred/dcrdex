// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package test

import (
	"testing"

	"decred.org/dcrdex/dex/networks/btc"

	"github.com/btcsuite/btcd/chaincfg"
	"github.com/btcsuite/btcd/txscript"
)

// CompatibilityItems are a set of pubkey scripts and corresponding
// string-encoded addresses checked in CompatibilityTest. They should be taken
// from existing on-chain data.
type CompatibilityItems struct {
	P2PKHScript  []byte
	PKHAddr      string
	P2WPKHScript []byte
	WPKHAddr     string
	P2SHScript   []byte
	SHAddr       string
	P2WSHScript  []byte
	WSHAddr      string
}

// CompatibilityCheck checks various scripts' compatibility with the Backend.
// If a fork's CompatibilityItems can pass the CompatibilityCheck, the node
// can likely use NewBTCClone to create a DEX-compatible backend.
func CompatibilityCheck(t *testing.T, items *CompatibilityItems, chainParams *chaincfg.Params) {
	t.Helper()
	checkAddr := func(pkScript []byte, addr string) {
		_, addrs, _, err := txscript.ExtractPkScriptAddrs(pkScript, chainParams)
		if err != nil {
			t.Fatalf("ExtractPkScriptAddrs error: %v", err)
		}
		if len(addrs) == 0 {
			t.Fatalf("no addresses extracted from script %x", pkScript)
		}
		if addrs[0].String() != addr {
			t.Fatalf("address mismatch %s != %s", addrs[0].String(), addr)
		}
		if !addrs[0].IsForNet(chainParams) {
			t.Fatalf("IsForNet rejected address %v for net %v", addrs[0], chainParams.Name)
		}
	}

	// P2PKH
	pkh := btc.ExtractPubKeyHash(items.P2PKHScript)
	if pkh == nil {
		t.Fatalf("incompatible P2PKH script")
	}
	checkAddr(items.P2PKHScript, items.PKHAddr)

	// P2WPKH
	// A clone doesn't necessarily need segwit, so a nil value here will skip
	// the test.
	if items.P2WPKHScript != nil {
		scriptClass := txscript.GetScriptClass(items.P2WPKHScript)
		if scriptClass != txscript.WitnessV0PubKeyHashTy {
			t.Fatalf("incompatible P2WPKH script")
		}
		checkAddr(items.P2WPKHScript, items.WPKHAddr)
	}

	// P2SH
	sh := btc.ExtractScriptHash(items.P2SHScript)
	if sh == nil {
		t.Fatalf("incompatible P2SH script")
	}
	checkAddr(items.P2SHScript, items.SHAddr)

	// P2WSH
	if items.P2WSHScript != nil {
		scriptClass := txscript.GetScriptClass(items.P2WSHScript)
		if scriptClass != txscript.WitnessV0ScriptHashTy {
			t.Fatalf("incompatible P2WPKH script")
		}
		wsh := btc.ExtractScriptHash(items.P2WSHScript)
		if wsh == nil {
			t.Fatalf("incompatible P2WSH script")
		}
		checkAddr(items.P2WSHScript, items.WSHAddr)
	}
}
