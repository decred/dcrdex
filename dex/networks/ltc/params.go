// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org

package ltc

import (
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/networks/btc"
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
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
		AtomicUnit: "litoshi",
		Conventional: dex.Denomination{
			Unit:             "LTC",
			ConversionFactor: 1e8,
		},
	}
	// MainNetParams are the clone parameters for mainnet.
	MainNetParams = btc.ReadCloneParams(&btc.CloneParams{
		Name:             "mainnet",
		PubKeyHashAddrID: 0x30, // starts with L
		ScriptHashAddrID: 0x32, // starts with M
		Bech32HRPSegwit:  "ltc",
		CoinbaseMaturity: 100,
		Net:              0xdbb6c0fb,
		HDPrivateKeyID:   [4]byte{0x04, 0x88, 0xad, 0xe4}, // starts with xprv
		HDPublicKeyID:    [4]byte{0x04, 0x88, 0xb2, 0x1e}, // starts with xpub
		GenesisHash:      mustHash("12a765e31ffd4059bada1e25190f6e98c99d9714d334efa41a195a7e7e04bfe2"),
	})
	// TestNet4Params are the clone parameters for testnet.
	TestNet4Params = btc.ReadCloneParams(&btc.CloneParams{
		Name:             "testnet4",
		PubKeyHashAddrID: 0x6f, // starts with m or n
		ScriptHashAddrID: 0x3a, // starts with Q
		Bech32HRPSegwit:  "tltc",
		CoinbaseMaturity: 100,
		Net:              0xf1c8d2fd,
		HDPrivateKeyID:   [4]byte{0x04, 0x35, 0x83, 0x94}, // starts with tprv
		HDPublicKeyID:    [4]byte{0x04, 0x35, 0x87, 0xcf}, // starts with tpub
		GenesisHash:      mustHash("4966625a4b2851d9fdee139e56211a0d88575f59ed816ff5e6a63deb4e3e29a0"),
	})
	// RegressionNetParams are the clone parameters for simnet.
	RegressionNetParams = btc.ReadCloneParams(&btc.CloneParams{
		Name:             "regtest",
		PubKeyHashAddrID: 0x6f, // starts with m or n
		ScriptHashAddrID: 0x3a, // starts with Q
		Bech32HRPSegwit:  "rltc",
		CoinbaseMaturity: 100,
		// Net is not the standard for LTC simnet, since they never changed it
		// from the BTC value. The only place we currently use Net is in
		// btcd/chaincfg.Register, where it is checked to prevent duplicate
		// registration, so our only requirement is that it is unique. This one
		// was just generated with a prng.
		Net:            0x9acb0442,
		HDPrivateKeyID: [4]byte{0x04, 0x35, 0x83, 0x94}, // starts with tprv
		HDPublicKeyID:  [4]byte{0x04, 0x35, 0x87, 0xcf}, // starts with tpub
		GenesisHash:    mustHash("530827f38f93b43ed12af0b3ad25a288dc02ed74d6d7857862df51fc56c416f9"),
	})
)

func init() {
	for _, params := range []*chaincfg.Params{MainNetParams, TestNet4Params, RegressionNetParams} {
		err := chaincfg.Register(params)
		if err != nil {
			panic("failed to register ltc parameters: " + err.Error())
		}
	}
}
