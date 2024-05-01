// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org

package dash

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

const (
	DefaultFee          = 1  // 1 sat/byte
	DefaultFeeRateLimit = 20 // 20 sats/byte
)

var (
	UnitInfo = dex.UnitInfo{
		AtomicUnit: "Sats",
		Conventional: dex.Denomination{
			Unit:             "DASH",
			ConversionFactor: 1e8,
		},
		Alternatives: []dex.Denomination{
			{
				Unit:             "mDASH", // Casino usage
				ConversionFactor: 1e5,
			},
		},
		FeeRateUnit: "Sats/B",
	}

	// MainNetParams are the clone parameters for mainnet.
	MainNetParams = btc.ReadCloneParams(&btc.CloneParams{
		Name:             "mainnet",
		PubKeyHashAddrID: 0x4c, // (76) starts with X
		ScriptHashAddrID: 0x10, // (16) starts with 7
		Bech32HRPSegwit:  "",   // no segwit
		CoinbaseMaturity: 100,
		Net:              0xbf0c6bbd,
		HDPrivateKeyID:   [4]byte{0x04, 0x88, 0xAD, 0xE4}, // starts with xprv
		HDPublicKeyID:    [4]byte{0x04, 0x88, 0xB2, 0x1E}, // starts with xpub
		GenesisHash:      mustHash("00000ffd590b1485b3caadc19b22e6379c733355108f107a430458cdf3407ab6"),
	})
	// TestNetParams are the clone parameters for testnet.
	TestNetParams = btc.ReadCloneParams(&btc.CloneParams{
		Name:             "testnet3",
		PubKeyHashAddrID: 0x8c, // (140) starts with y
		ScriptHashAddrID: 0x13, // (19) starts with 6 or 9
		Bech32HRPSegwit:  "",   // no segwit
		CoinbaseMaturity: 100,
		// The real dash value 0xcee2caff is duplicated with several coins. This
		// is pseudo-ransomly generated and is currently unique across dcrdex netids.
		Net:            0x881823,
		HDPrivateKeyID: [4]byte{0x04, 0x35, 0x83, 0x94}, // starts with tprv
		HDPublicKeyID:  [4]byte{0x04, 0x35, 0x87, 0xCF}, // starts with tpub
		GenesisHash:    mustHash("00000bafbc94add76cb75e2ec92894837288a481e5c005f6563d91623bf8bc2c"),
	})
	// RegressionNetParams are the clone parameters for simnet.
	RegressionNetParams = btc.ReadCloneParams(&btc.CloneParams{
		Name:             "regtest",
		PubKeyHashAddrID: 0x8c, // (140) starts with y
		ScriptHashAddrID: 0x13, // (19) starts with 6 or 9
		Bech32HRPSegwit:  "",   // no segwit
		CoinbaseMaturity: 100,
		// The real dash value 0xfcc1b7dc is duplicated with several coins. This
		// is pseudo-ransomly generated and is currently unique across dcrdex netids.
		// See Also:
		// https://github.com/dan-da/coinparams/blob/master/coinparams.json
		Net:            0x848a5332,
		HDPrivateKeyID: [4]byte{0x04, 0x35, 0x83, 0x94}, // starts with tprv
		HDPublicKeyID:  [4]byte{0x04, 0x35, 0x87, 0xcf}, // starts with tpub
		GenesisHash:    mustHash("000008ca1832a4baf228eb1553c03d3a2c8e02399550dd6ea8d65cec3ef23d2e"),
	})
)

func init() {
	for _, params := range []*chaincfg.Params{MainNetParams, TestNetParams, RegressionNetParams} {
		err := chaincfg.Register(params)
		if err != nil {
			panic("failed to register dash parameters: " + err.Error())
		}
	}
}
