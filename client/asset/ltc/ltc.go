// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package ltc

import (
	"fmt"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/client/asset/btc"
	"decred.org/dcrdex/dex"
	dexbtc "decred.org/dcrdex/dex/networks/btc"
	dexltc "decred.org/dcrdex/dex/networks/ltc"
	"github.com/btcsuite/btcd/chaincfg"
)

const (
	BipID = 2
	// The default fee is passed to the user as part of the asset.WalletInfo
	// structure.
	defaultFee        = 10
	minNetworkVersion = 180100
)

var (
	configOpts = []*asset.ConfigOption{
		{
			Key:         "walletname",
			DisplayName: "Wallet Name",
			Description: "The wallet name",
		},
		{
			Key:         "rpcuser",
			DisplayName: "JSON-RPC Username",
			Description: "Litecoin's 'rpcuser' setting",
		},
		{
			Key:         "rpcpassword",
			DisplayName: "JSON-RPC Password",
			Description: "Litecoin's 'rpcpassword' setting",
			NoEcho:      true,
		},
		{
			Key:          "rpcbind",
			DisplayName:  "JSON-RPC Address",
			Description:  "<addr> or <addr>:<port> (default 'localhost')",
			DefaultValue: "127.0.0.1",
		},
		{
			Key:          "rpcport",
			DisplayName:  "JSON-RPC Port",
			Description:  "Port for RPC connections (if not set in rpcbind)",
			DefaultValue: "9332",
		},
		{
			Key:          "fallbackfee",
			DisplayName:  "Fallback fee rate",
			Description:  "Litecoin's 'fallbackfee' rate. Units: LTC/kB",
			DefaultValue: defaultFee * 1000 / 1e8,
		},
		{
			Key:          "redeemconftarget",
			DisplayName:  "Redeem transaction confirmation target",
			Description:  "The target number of blocks for the redeem transaction to get a confirmation. Used to set the transaction's fee rate. (default: 2 blocks)",
			DefaultValue: 2,
		},
		{
			Key:         "txsplit",
			DisplayName: "Pre-size funding inputs",
			Description: "When placing an order, create a \"split\" transaction to fund the order without locking more of the wallet balance than " +
				"necessary. Otherwise, excess funds may be reserved to fund the order until the first swap contract is broadcast " +
				"during match settlement, or the order is canceled. This an extra transaction for which network mining fees are paid. " +
				"Used only for standing-type orders, e.g. limit orders without immediate time-in-force.",
			IsBoolean: true,
		},
	}
	// WalletInfo defines some general information about a Litecoin wallet.
	WalletInfo = &asset.WalletInfo{
		Name:              "Litecoin",
		Units:             "Litoshi",
		DefaultConfigPath: dexbtc.SystemConfigPath("litecoin"),
		ConfigOpts:        configOpts,
	}
)

func init() {
	asset.Register(BipID, &Driver{})
}

// Driver implements asset.Driver.
type Driver struct{}

// Setup creates the LTC exchange wallet. Start the wallet with its Run method.
func (d *Driver) Setup(cfg *asset.WalletConfig, logger dex.Logger, network dex.Network) (asset.Wallet, error) {
	return NewWallet(cfg, logger, network)
}

// DecodeCoinID creates a human-readable representation of a coin ID for
// Litecoin.
func (d *Driver) DecodeCoinID(coinID []byte) (string, error) {
	// Litecoin and Bitcoin have the same tx hash and output format.
	return (&btc.Driver{}).DecodeCoinID(coinID)
}

// Info returns basic information about the wallet and asset.
func (d *Driver) Info() *asset.WalletInfo {
	return WalletInfo
}

// NewWallet is the exported constructor by which the DEX will import the
// exchange wallet. The wallet will shut down when the provided context is
// canceled. The configPath can be an empty string, in which case the standard
// system location of the litecoind config file is assumed.
func NewWallet(cfg *asset.WalletConfig, logger dex.Logger, network dex.Network) (asset.Wallet, error) {
	var params *chaincfg.Params
	switch network {
	case dex.Mainnet:
		params = dexltc.MainNetParams
	case dex.Testnet:
		params = dexltc.TestNet4Params
	case dex.Regtest:
		params = dexltc.RegressionNetParams
	default:
		return nil, fmt.Errorf("unknown network ID %v", network)
	}

	// Designate the clone ports. These will be overwritten by any explicit
	// settings in the configuration file.
	ports := dexbtc.NetPorts{
		Mainnet: "9332",
		Testnet: "19332",
		Simnet:  "19443",
	}
	cloneCFG := &btc.BTCCloneCFG{
		WalletCFG:          cfg,
		MinNetworkVersion:  minNetworkVersion,
		WalletInfo:         WalletInfo,
		Symbol:             "ltc",
		Logger:             logger,
		Network:            network,
		ChainParams:        params,
		Ports:              ports,
		DefaultFallbackFee: defaultFee,
		LegacyBalance:      true,
		Segwit:             false,
	}

	return btc.BTCCloneWallet(cloneCFG)
}
