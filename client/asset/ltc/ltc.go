// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package ltc

import (
	"fmt"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/client/asset/btc"
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/config"
	dexbtc "decred.org/dcrdex/dex/networks/btc"
	dexltc "decred.org/dcrdex/dex/networks/ltc"
	"github.com/btcsuite/btcd/chaincfg"
)

const (
	BipID = 2
	// The default fee is passed to the user as part of the asset.WalletInfo
	// structure.
	defaultWithdrawalFee = 1
	minNetworkVersion    = 180100
)

var (
	// walletInfo defines some general information about a Litecoin wallet.
	walletInfo = &asset.WalletInfo{
		Name:              "Litecoin",
		Units:             "Litoshi",
		DefaultConfigPath: dexbtc.SystemConfigPath("litecoin"),
		ConfigOpts:        config.Options(&dexbtc.Config{}),
		DefaultFeeRate:    defaultWithdrawalFee,
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
	return walletInfo
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
		WalletCFG:         cfg,
		MinNetworkVersion: minNetworkVersion,
		WalletInfo:        walletInfo,
		Symbol:            "btc",
		Logger:            logger,
		Network:           network,
		ChainParams:       params,
		Ports:             ports,
	}
	return btc.BTCCloneWallet(cloneCFG)
}
