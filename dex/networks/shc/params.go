// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org

package shc

import (
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/networks/btc"
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
)

const (
	// DefaultFee and DefaultFeeRateLimit are estimates, not yet verified
	// against a real fee market - Sharecoin has no exchange-priced fee
	// history yet. Units: sats/kvB, same convention as the other BTC-clone
	// packages in this directory. Revisit once real trading data exists.
	DefaultFee          = 1000
	DefaultFeeRateLimit = 25000
)

func mustHash(hash string) *chainhash.Hash {
	h, err := chainhash.NewHashFromStr(hash)
	if err != nil {
		panic(err.Error())
	}
	return h
}

var (
	UnitInfo = dex.UnitInfo{
		AtomicUnit: "satoshi",
		Conventional: dex.Denomination{
			Unit:             "SHC",
			ConversionFactor: 1e8,
		},
		FeeRateDenom: "vB",
	}

	// MainNetParams are the clone parameters for Sharecoin mainnet.
	// Values read directly from this project's own
	// bitcoin-source/src/kernel/chainparams.cpp (CMainParams), and the
	// genesis hash cross-checked against a live mainnet node's own
	// `getblockhash 0` RPC response, 2026-08-09 - not guessed or derived.
	MainNetParams = btc.ReadCloneParams(&btc.CloneParams{
		Name:             "mainnet",
		PubKeyHashAddrID: 63, // base58Prefixes[PUBKEY_ADDRESS]
		ScriptHashAddrID: 18, // base58Prefixes[SCRIPT_ADDRESS]
		Bech32HRPSegwit:  "shc",
		CoinbaseMaturity: 100, // untouched Bitcoin default (consensus/consensus.h)
		// pchMessageStart = {0x53,0x48,0x43,0x31} ("SHC1" in
		// chainparams.cpp), read little-endian - same byte-order
		// convention as Bitcoin's own pchMessageStart 0xf9beb4d9
		// becoming wire.MainNet 0xd9b4bef9.
		Net:         0x31434853,
		GenesisHash: mustHash("f1f36342dfeca02a77bb5de429c39f9cbac8794275b603c318c79bcab145fc6e"),
		// HD key prefixes - not required by the client, included for
		// completeness / server-side fee-address derivation. From
		// base58Prefixes[EXT_PUBLIC_KEY] / [EXT_SECRET_KEY].
		HDPublicKeyID:  [4]byte{0x04, 0x2C, 0x03, 0xE3},
		HDPrivateKeyID: [4]byte{0x04, 0x2C, 0x08, 0x1E},
	})

	// Regtest params are deliberately not included here. This project's
	// real, live network is mainnet only (see SHARECOINADMIN.md topology)
	// - the point of this integration is real people trading real SHC on
	// Sharecoin's actual live mainnet. Regtest support was built and
	// verified separately during development (a full local swap was
	// completed against it, see NOTES.md) but is not part of what gets
	// submitted upstream.
)

func init() {
	if err := chaincfg.Register(MainNetParams); err != nil {
		panic("failed to register shc parameters: " + err.Error())
	}
}
