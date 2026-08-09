// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package shc

import (
	"fmt"
	"strconv"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/client/asset/btc"
	"decred.org/dcrdex/dex"
	dexbtc "decred.org/dcrdex/dex/networks/btc"
	dexshc "decred.org/dcrdex/dex/networks/shc"

	"github.com/btcsuite/btcd/chaincfg"
)

const (
	version = 0

	// BipID is not an officially-registered SLIP-44 index - Sharecoin has
	// none as of 2026-08-09 (confirmed against both this repo's own
	// dex/bip-id.go, 600 entries, and the upstream satoshilabs/slips
	// registry directly - zero hits either place). Self-assigned here
	// following the same convention already used by other self-registered
	// entries in that table (aqua, kusd, fluid, qkc all use large
	// arbitrary values rather than seeking a low reserved number).
	// Derived from Sharecoin's own real mainnet P2P port (8443).
	BipID = 8443000

	// Real value from a live mainnet node's `getnetworkinfo` "version"
	// field, 2026-08-09 (all four live nodes run this exact build as of
	// the 2026-08-07 LoadBlockIndexGuts-fix redeploy) - not guessed.
	minNetworkVersion    = 319900
	minDescriptorVersion = 319900

	walletTypeRPC           = "sharecoindRPC"
	defaultRedeemConfTarget = 2
)

var (
	configOpts = append(btc.RPCConfigOpts("Sharecoin", "8332"), []*asset.ConfigOption{
		{
			Key:          "fallbackfee",
			DisplayName:  "Fallback fee rate",
			Description:  "Sharecoin's 'fallbackfee' rate. Units: SHC/kB",
			DefaultValue: strconv.FormatFloat(dexshc.DefaultFee*1000/1e8, 'f', -1, 64),
		},
		{
			Key:         "feeratelimit",
			DisplayName: "Highest acceptable fee rate",
			Description: "This is the highest network fee rate you are willing to " +
				"pay on swap transactions. If feeratelimit is lower than a market's " +
				"maxfeerate, you will not be able to trade on that market with this " +
				"wallet.  Units: SHC/kB",
			DefaultValue: strconv.FormatFloat(dexshc.DefaultFeeRateLimit*1000/1e8, 'f', -1, 64),
		},
		{
			Key:         "redeemconftarget",
			DisplayName: "Redeem confirmation target",
			Description: "The target number of blocks for the redeem transaction " +
				"to be mined. Used to set the transaction's fee rate. " +
				"(default: 2 blocks)",
			DefaultValue: strconv.FormatUint(defaultRedeemConfTarget, 10),
		},
		{
			Key:         "txsplit",
			DisplayName: "Pre-split funding inputs",
			Description: "When placing an order, create a \"split\" transaction to fund the order without locking more of the wallet balance than " +
				"necessary. Otherwise, excess funds may be reserved to fund the order until the first swap contract is broadcast " +
				"during match settlement, or the order is canceled. This an extra transaction for which network mining fees are paid. " +
				"Used only for standing-type orders, e.g. limit orders without immediate time-in-force.",
			IsBoolean:    true,
			DefaultValue: "true", // low fee, fast (~120s) block time
		},
	}...)

	// WalletInfo defines some general information about a Sharecoin wallet.
	WalletInfo = &asset.WalletInfo{
		Name:              "Sharecoin",
		SupportedVersions: []uint32{version},
		UnitInfo:          dexshc.UnitInfo,
		AvailableWallets: []*asset.WalletDefinition{{
			Type:              walletTypeRPC,
			Tab:               "External",
			Description:       "Connect to sharecoind",
			DefaultConfigPath: dexbtc.SystemConfigPath("sharecoin"),
			ConfigOpts:        configOpts,
		}},
		BlockchainClass: asset.BlockchainClassUTXO,
	}
)

func init() {
	asset.Register(BipID, &Driver{})
}

// Driver implements asset.Driver.
type Driver struct{}

// Open creates the Sharecoin exchange wallet. Start the wallet with its Run method.
func (d *Driver) Open(cfg *asset.WalletConfig, logger dex.Logger, network dex.Network) (asset.Wallet, error) {
	return NewWallet(cfg, logger, network)
}

// DecodeCoinID creates a human-readable representation of a coin ID for
// Sharecoin.
func (d *Driver) DecodeCoinID(coinID []byte) (string, error) {
	// Sharecoin and Bitcoin have the same tx hash and output format -
	// Sharecoin only swaps the PoW algorithm (SHA-256d -> KawPow); the
	// rest of the Bitcoin Core RPC/wallet/script surface is untouched
	// upstream, confirmed in this project's own chainparams.cpp/patch
	// files.
	return (&btc.Driver{}).DecodeCoinID(coinID)
}

// Info returns basic information about the wallet and asset.
func (d *Driver) Info() *asset.WalletInfo {
	return WalletInfo
}

// MinLotSize calculates the minimum bond size for a given fee rate that avoids
// dust outputs on the swap and refund txs, assuming the maxFeeRate doesn't
// change.
func (d *Driver) MinLotSize(maxFeeRate uint64) uint64 {
	return dexbtc.MinLotSize(maxFeeRate, false)
}

// NewWallet is the exported constructor by which the DEX will import the
// exchange wallet. The wallet will shut down when the provided context is
// canceled. The configPath can be an empty string, in which case the standard
// system location of the sharecoind config file is assumed.
func NewWallet(cfg *asset.WalletConfig, logger dex.Logger, network dex.Network) (asset.Wallet, error) {
	var params *chaincfg.Params
	switch network {
	case dex.Mainnet:
		params = dexshc.MainNetParams
	default:
		return nil, fmt.Errorf("unsupported network for shc: %v (mainnet only)", network)
	}

	// Designate the clone ports. These will be overwritten by any explicit
	// settings in the configuration file.
	ports := dexbtc.NetPorts{
		Mainnet: "8332",
	}
	cloneCFG := &btc.BTCCloneCFG{
		WalletCFG:            cfg,
		MinNetworkVersion:    minNetworkVersion,
		MinDescriptorVersion: minDescriptorVersion,

		WalletInfo:          WalletInfo,
		Symbol:              "shc",
		Logger:              logger,
		Network:             network,
		ChainParams:         params,
		Ports:               ports,
		DefaultFallbackFee:  dexshc.DefaultFee,
		DefaultFeeRateLimit: dexshc.DefaultFeeRateLimit,
		// false, not DGB's true - confirmed live 2026-08-09 against a real
		// regtest node that `getbalances` (the modern RPC) works fine here;
		// `LegacyBalance: true` silently caused Balance() to always report 0
		// despite the underlying wallet holding real funds, since Sharecoin's
		// node (a very recent Bitcoin Core fork) doesn't need the legacy
		// `listunspent`-sum fallback older clones like DigiByte require.
		LegacyBalance:  false,
		Segwit:         true,
		InitTxSize:     dexbtc.InitTxSizeSegwit,
		InitTxSizeBase: dexbtc.InitTxSizeBaseSegwit,
		AssetID:        BipID,
	}

	return btc.BTCCloneWallet(cloneCFG)
}
