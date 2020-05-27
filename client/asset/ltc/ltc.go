// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package ltc

import (
	"fmt"
	"os"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/client/asset/btc"
	"decred.org/dcrdex/dex"
	dexbtc "decred.org/dcrdex/dex/btc"
	"decred.org/dcrdex/dex/config"
	"decred.org/dcrdex/server/asset/ltc"
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/btcsuite/btcd/wire"
)

const (
	BipID = 2
	// The default fee is passed to the user as part of the asset.WalletInfo
	// structure.
	defaultWithdrawalFee = 1
	minNetworkVersion    = 170100
	// Litecoin Net parameters in terms of bitcoin's wire.
	MainNet  wire.BitcoinNet = 0xdbb6c0fb
	TestNet4 wire.BitcoinNet = 0xf1c8d2fd
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
	var params *dexbtc.CloneParams
	switch network {
	case dex.Mainnet:
		params = ltc.MainNetParams
	case dex.Testnet:
		params = ltc.TestNet4Params
	case dex.Regtest:
		params = ltc.RegressionNetParams
	default:
		return nil, fmt.Errorf("unknown network ID %v", network)
	}

	// Convert the ltcd params to btcd params.
	btcParams := dexbtc.ReadCloneParams(params)

	// ltc Net parameters are exactly the same as btc for everything but
	// mainnet and testnet4 and so must be changed in order to register.
	if btcParams.Net != MainNet && btcParams.Net != TestNet4 {
		btcParams.Net = wire.BitcoinNet(BipID)
	}

	// In order to populate some maps inside of chaincfg, new params must
	// be registered.
	if err := chaincfg.Register(btcParams); err != nil {
		// An error is expected when testing and several wallets are
		// created.
		fmt.Fprintf(os.Stderr, "err registering ltc params: %v", err)
	}

	// Designate the clone ports. These will be overwritten by any explicit
	// settings in the configuration file.
	ports := dexbtc.NetPorts{
		Mainnet: "9333",
		Testnet: "19335",
		Simnet:  "18444",
	}
	cloneCFG := &btc.BTCCloneCFG{
		WalletCFG:         cfg,
		MinNetworkVersion: minNetworkVersion,
		WalletInfo:        walletInfo,
		Symbol:            "btc",
		Logger:            logger,
		Network:           network,
		ChainParams:       btcParams,
		Ports:             ports,
	}
	return btc.BTCCloneWallet(cloneCFG)
}
