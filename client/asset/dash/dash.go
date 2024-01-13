// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package dash

import (
	"fmt"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/client/asset/btc"
	"decred.org/dcrdex/dex"
	dexbtc "decred.org/dcrdex/dex/networks/btc"
	dexdash "decred.org/dcrdex/dex/networks/dash"

	"github.com/btcsuite/btcd/chaincfg"
)

const (
	version                 = 0
	BipID                   = 5
	minNetworkVersion       = 200000 // Dash v20.0.0, proto: 70230. Breaking change from 190200
	walletTypeRPC           = "dashdRPC"
	defaultRedeemConfTarget = 2
)

var (
	configOpts = append(btc.RPCConfigOpts("Dash", "9998"), []*asset.ConfigOption{
		{
			Key:          "fallbackfee",
			DisplayName:  "Fallback fee rate",
			Description:  "Dash's 'fallbackfee' rate. Units: DASH/kB",
			DefaultValue: dexdash.DefaultFee * 1000 / 1e8,
		},
		{
			Key:         "feeratelimit",
			DisplayName: "Highest acceptable fee rate",
			Description: "This is the highest network fee rate you are willing to " +
				"pay on swap transactions. If feeratelimit is lower than a market's " +
				"maxfeerate, you will not be able to trade on that market with this " +
				"wallet.  Units: DASH/kB",
			DefaultValue: dexdash.DefaultFeeRateLimit * 1000 / 1e8,
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
			DefaultValue: true,
		},
	}...)

	// WalletInfo defines some general information about a Dash wallet.
	WalletInfo = &asset.WalletInfo{
		Name:              "Dash",
		Version:           version,
		SupportedVersions: []uint32{version},
		UnitInfo:          dexdash.UnitInfo,
		AvailableWallets: []*asset.WalletDefinition{
			{
				Type:              walletTypeRPC,
				Tab:               "Dash Core (external)",
				Description:       "Connect to dashd",
				DefaultConfigPath: dexbtc.SystemConfigPath("dash"),
				ConfigOpts:        configOpts,
			},
		},
	}
)

func init() {
	asset.Register(BipID, &Driver{})
}

// Driver implements asset.Driver.
type Driver struct{}

// Open creates the Dash exchange wallet. Start the wallet with its Run method.
func (d *Driver) Open(cfg *asset.WalletConfig, logger dex.Logger, network dex.Network) (asset.Wallet, error) {
	return newWallet(cfg, logger, network)
}

// DecodeCoinID creates a human-readable representation of a coin ID for Dash
func (d *Driver) DecodeCoinID(coinID []byte) (string, error) {
	// Dash and Bitcoin have the same tx hash and output format.
	return (&btc.Driver{}).DecodeCoinID(coinID)
}

// Info returns basic information about the wallet and asset.
func (d *Driver) Info() *asset.WalletInfo {
	return WalletInfo
}

// newWallet constructs a new client wallet for Dash based on the WalletDefinition.Type
func newWallet(cfg *asset.WalletConfig, logger dex.Logger, network dex.Network) (asset.Wallet, error) {
	var params *chaincfg.Params
	switch network {
	case dex.Mainnet:
		params = dexdash.MainNetParams
	case dex.Testnet:
		params = dexdash.TestNetParams
	case dex.Regtest:
		params = dexdash.RegressionNetParams
	default:
		return nil, fmt.Errorf("unknown network ID %v", network)
	}

	// Designate the clone ports.
	ports := dexbtc.NetPorts{
		Mainnet: "9998",
		Testnet: "19998",
		Simnet:  "19898",
	}

	cloneCFG := &btc.BTCCloneCFG{
		WalletCFG:           cfg,
		MinNetworkVersion:   minNetworkVersion,
		WalletInfo:          WalletInfo,
		Symbol:              "dash",
		Logger:              logger,
		Network:             network,
		ChainParams:         params,
		Ports:               ports,
		DefaultFallbackFee:  dexdash.DefaultFee,
		DefaultFeeRateLimit: dexdash.DefaultFeeRateLimit,
		LegacyBalance:       false,
		Segwit:              false,
		// Dash v19.1.0 has a breaking change from the true/false 'allowhighfees'
		// to 'maxfeerate' in DASH/kB, the same as btc.
		LegacyRawFeeLimit:        false,
		InitTxSize:               dexbtc.InitTxSize,
		InitTxSizeBase:           dexbtc.InitTxSizeBase,
		PrivKeyFunc:              nil,
		AddressDecoder:           nil,
		AddressStringer:          nil,
		BlockDeserializer:        nil,
		ArglessChangeAddrRPC:     true, // getrawchangeaddress has No address-type arg
		NonSegwitSigner:          nil,
		FeeEstimator:             nil, // estimatesmartfee + getblockstats
		ExternalFeeEstimator:     nil,
		OmitAddressType:          true,  // getnewaddress has No address-type arg
		LegacySignTxRPC:          false, // Has signrawtransactionwithwallet RPC
		BooleanGetBlockRPC:       false, // Use 0/1 for verbose param
		LegacyValidateAddressRPC: false, // getaddressinfo
		SingularWallet:           false, // wallet can have "" as a path but also a name like "gamma"
		UnlockSpends:             false,
		ConstantDustLimit:        0,
		AssetID:                  BipID,
	}

	return btc.BTCCloneWallet(cloneCFG)
}
