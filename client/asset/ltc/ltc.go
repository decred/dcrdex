// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package ltc

import (
	"context"
	"fmt"
	"time"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/client/asset/btc"
	"decred.org/dcrdex/dex"
	dexbtc "decred.org/dcrdex/dex/networks/btc"
	dexltc "decred.org/dcrdex/dex/networks/ltc"
	"github.com/btcsuite/btcd/chaincfg"
)

const (
	version = 0
	// BipID is the BIP-0044 asset ID.
	BipID = 2
	// defaultFee is the default value for the fallbackfee.
	defaultFee = 10
	// defaultFeeRateLimit is the default value for the feeratelimit.
	defaultFeeRateLimit = 100
	minNetworkVersion   = 180100
	walletTypeRPC       = "litecoindRPC"
	walletTypeLegacy    = ""
)

var (
	NetPorts = dexbtc.NetPorts{
		Mainnet: "9332",
		Testnet: "19332",
		Simnet:  "19443",
	}
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
			Key:         "feeratelimit",
			DisplayName: "Highest acceptable fee rate",
			Description: "This is the highest network fee rate you are willing to " +
				"pay on swap transactions. If feeratelimit is lower than a market's " +
				"maxfeerate, you will not be able to trade on that market with this " +
				"wallet.  Units: LTC/kB",
			DefaultValue: defaultFeeRateLimit * 1000 / 1e8,
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
		Name:     "Litecoin",
		Version:  version,
		UnitInfo: dexltc.UnitInfo,
		AvailableWallets: []*asset.WalletDefinition{{
			Type:              walletTypeRPC,
			Tab:               "External",
			Description:       "Connect to litecoind",
			DefaultConfigPath: dexbtc.SystemConfigPath("litecoin"),
			ConfigOpts:        configOpts,
		}},
	}
)

func init() {
	asset.Register(BipID, &Driver{})
}

// Driver implements asset.Driver.
type Driver struct{}

// Exists checks the existence of the wallet. For the RPC wallet, this attempts
// to connect and request getnetworkinfo to verify existence.
func (d *Driver) Exists(walletType, dataDir string, settings map[string]string, net dex.Network) (bool, error) {
	switch walletType {
	case walletTypeLegacy, walletTypeRPC:
		_, client, err := btc.ParseRPCWalletConfig(settings, "ltc", net, NetPorts)
		if err != nil {
			return false, err
		}
		ctx, cancel := context.WithTimeout(context.Background(), time.Second*3)
		defer cancel()
		_, err = client.RawRequest(ctx, "getnetworkinfo", nil)
		return err == nil, nil
	}

	return false, fmt.Errorf("no Bitcoin Cash wallet of type %q available", walletType)
}

func (d *Driver) Create(*asset.CreateWalletParams) error {
	return fmt.Errorf("no creatable wallet types")
}

// Open creates the LTC exchange wallet. Start the wallet with its Run method.
func (d *Driver) Open(cfg *asset.WalletConfig, logger dex.Logger, network dex.Network) (asset.Wallet, error) {
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
	cloneCFG := &btc.BTCCloneCFG{
		WalletCFG:           cfg,
		MinNetworkVersion:   minNetworkVersion,
		WalletInfo:          WalletInfo,
		Symbol:              "ltc",
		Logger:              logger,
		Network:             network,
		ChainParams:         params,
		Ports:               NetPorts,
		DefaultFallbackFee:  defaultFee,
		DefaultFeeRateLimit: defaultFeeRateLimit,
		LegacyBalance:       true,
		LegacyRawFeeLimit:   true,
		Segwit:              false,
	}

	return btc.BTCCloneWallet(cloneCFG)
}
