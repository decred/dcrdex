// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package dgb

import (
	"fmt"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/client/asset/btc"
	"decred.org/dcrdex/dex"
	dexbtc "decred.org/dcrdex/dex/networks/btc"
	dexdgb "decred.org/dcrdex/dex/networks/dgb"

	"github.com/btcsuite/btcd/chaincfg"
)

const (
	version = 0
	BipID   = 20

	// dustLimit = 1_000_000 // sats => 0.01 DGB, the "soft" limit (DEFAULT_DUST_LIMIT) **TODO check for dgb**

	minNetworkVersion       = 7170300
	walletTypeRPC           = "digibytedRPC"
	defaultRedeemConfTarget = 2
)

var (
	configOpts = append(btc.RPCConfigOpts("DigiByte", "14022"), []*asset.ConfigOption{
		{
			Key:          "fallbackfee",
			DisplayName:  "Fallback fee rate",
			Description:  "DigiByte's 'fallbackfee' rate. Units: DGB/kB",
			DefaultValue: dexdgb.DefaultFee * 1000 / 1e8, // higher than BTC default
		},
		{
			Key:         "feeratelimit",
			DisplayName: "Highest acceptable fee rate",
			Description: "This is the highest network fee rate you are willing to " +
				"pay on swap transactions. If feeratelimit is lower than a market's " +
				"maxfeerate, you will not be able to trade on that market with this " +
				"wallet.  Units: BTC/kB",
			DefaultValue: dexdgb.DefaultFeeRateLimit * 1000 / 1e8, // higher than BTC default
		},
		{
			Key:         "redeemconftarget",
			DisplayName: "Redeem confirmation target",
			Description: "The target number of blocks for the redeem transaction " +
				"to be mined. Used to set the transaction's fee rate. " +
				"(default: 2 blocks)",
			DefaultValue: defaultRedeemConfTarget,
		},
		{
			Key:         "txsplit",
			DisplayName: "Pre-split funding inputs",
			Description: "When placing an order, create a \"split\" transaction to fund the order without locking more of the wallet balance than " +
				"necessary. Otherwise, excess funds may be reserved to fund the order until the first swap contract is broadcast " +
				"during match settlement, or the order is canceled. This an extra transaction for which network mining fees are paid. " +
				"Used only for standing-type orders, e.g. limit orders without immediate time-in-force.",
			IsBoolean:    true,
			DefaultValue: true, // low fee, fast chain
		}, // no ExternalFeeEstimator, so no apifeefallback option
	}...)
	// WalletInfo defines some general information about a DigiByte wallet.
	WalletInfo = &asset.WalletInfo{
		Name:              "DigiByte",
		Version:           version,
		SupportedVersions: []uint32{version},
		UnitInfo:          dexdgb.UnitInfo,
		AvailableWallets: []*asset.WalletDefinition{{
			Type:              walletTypeRPC,
			Tab:               "External",
			Description:       "Connect to digibyted",
			DefaultConfigPath: dexbtc.SystemConfigPath("digibyte"),
			ConfigOpts:        configOpts,
		}},
	}
)

func init() {
	asset.Register(BipID, &Driver{})
}

// Driver implements asset.Driver.
type Driver struct{}

// Open creates the DGB exchange wallet. Start the wallet with its Run method.
func (d *Driver) Open(cfg *asset.WalletConfig, logger dex.Logger, network dex.Network) (asset.Wallet, error) {
	return NewWallet(cfg, logger, network)
}

// DecodeCoinID creates a human-readable representation of a coin ID for
// DigiByte.
func (d *Driver) DecodeCoinID(coinID []byte) (string, error) {
	// DigiByte and Bitcoin have the same tx hash and output format.
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
// system location of the digibyted config file is assumed.
func NewWallet(cfg *asset.WalletConfig, logger dex.Logger, network dex.Network) (asset.Wallet, error) {
	var params *chaincfg.Params
	switch network {
	case dex.Mainnet:
		params = dexdgb.MainNetParams
	case dex.Testnet:
		params = dexdgb.TestNetParams
	case dex.Regtest:
		params = dexdgb.RegressionNetParams
	default:
		return nil, fmt.Errorf("unknown network ID %v", network)
	}

	// See https://digibyte.org/docs/integrationguide.pdf

	// Designate the clone ports. These will be overwritten by any explicit
	// settings in the configuration file.
	ports := dexbtc.NetPorts{
		Mainnet: "14022",
		Testnet: "14023",
		Simnet:  "18443",
	}
	cloneCFG := &btc.BTCCloneCFG{
		WalletCFG:           cfg,
		MinNetworkVersion:   minNetworkVersion,
		WalletInfo:          WalletInfo,
		Symbol:              "dgb",
		Logger:              logger,
		Network:             network,
		ChainParams:         params,
		Ports:               ports,
		DefaultFallbackFee:  dexdgb.DefaultFee,
		DefaultFeeRateLimit: dexdgb.DefaultFeeRateLimit,
		LegacyBalance:       true,
		Segwit:              true,
		InitTxSize:          dexbtc.InitTxSizeSegwit,
		InitTxSizeBase:      dexbtc.InitTxSizeBaseSegwit,
		// UnlockSpends:        false, // check listlockunspent after a swap: https://github.com/decred/dcrdex/pull/1558#issuecomment-1096912520
		// ConstantDustLimit:   dustLimit, // commented to use dexbtc.IsDust, but investigate for DGB
		AssetID: BipID,
	}

	return btc.BTCCloneWallet(cloneCFG)
}
