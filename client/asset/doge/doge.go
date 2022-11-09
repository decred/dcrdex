// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package doge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"time"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/client/asset/btc"
	"decred.org/dcrdex/dex"
	dexbtc "decred.org/dcrdex/dex/networks/btc"
	dexdoge "decred.org/dcrdex/dex/networks/doge"
	"github.com/btcsuite/btcd/chaincfg"
)

const (
	version = 0
	BipID   = 3

	dustLimit = 1_000_000 // sats => 0.01 DOGE, the "soft" limit (DEFAULT_DUST_LIMIT)

	minNetworkVersion = 1140500
	walletTypeRPC     = "dogecoindRPC"
	feeConfs          = 10
	// externalApiUrl is the URL of the external API in case of fallback.
	externalApiUrl = "https://api.bitcore.io/api/DOGE/mainnet/"
	// testnetExternalApiUrl is the URL of the testnet external API in case of
	// fallback.
	testnetExternalApiUrl = "https://api.bitcore.io/api/DOGE/testnet/"
)

var (
	fallbackFeeKey = "fallbackfee"
	configOpts     = []*asset.ConfigOption{
		{
			Key:         "rpcuser",
			DisplayName: "JSON-RPC Username",
			Description: "Dogecoin's 'rpcuser' setting",
		},
		{
			Key:         "rpcpassword",
			DisplayName: "JSON-RPC Password",
			Description: "Dogecoin's 'rpcpassword' setting",
			NoEcho:      true,
		},
		{
			Key:         "rpcbind",
			DisplayName: "JSON-RPC Address",
			Description: "<addr> or <addr>:<port> (default 'localhost')",
		},
		{
			Key:         "rpcport",
			DisplayName: "JSON-RPC Port",
			Description: "Port for RPC connections (if not set in Address)",
		},
		{
			Key:          fallbackFeeKey,
			DisplayName:  "Fallback fee rate",
			Description:  "Dogecoin's 'fallbackfee' rate. Units: DOGE/kB",
			DefaultValue: dexdoge.DefaultFee * 1000 / 1e8,
		},
		{
			Key:         "feeratelimit",
			DisplayName: "Highest acceptable fee rate",
			Description: "This is the highest network fee rate you are willing to " +
				"pay on swap transactions. If feeratelimit is lower than a market's " +
				"maxfeerate, you will not be able to trade on that market with this " +
				"wallet.  Units: BTC/kB",
			DefaultValue: dexdoge.DefaultFeeRateLimit * 1000 / 1e8,
		},
		{
			Key:         "txsplit",
			DisplayName: "Pre-split funding inputs",
			Description: "When placing an order, create a \"split\" transaction to fund the order without locking more of the wallet balance than " +
				"necessary. Otherwise, excess funds may be reserved to fund the order until the first swap contract is broadcast " +
				"during match settlement, or the order is canceled. This an extra transaction for which network mining fees are paid. " +
				"Used only for standing-type orders, e.g. limit orders without immediate time-in-force.",
			IsBoolean: true,
		},
		{
			Key:         "apifeefallback",
			DisplayName: "External fee rate estimates",
			Description: "Allow fee rate estimation from a block explorer API. " +
				"This is useful as a fallback for SPV wallets and RPC wallets " +
				"that have recently been started.",
			IsBoolean:    true,
			DefaultValue: true,
		},
	}
	// WalletInfo defines some general information about a Dogecoin wallet.
	WalletInfo = &asset.WalletInfo{
		Name:              "Dogecoin",
		Version:           version,
		SupportedVersions: []uint32{version},
		UnitInfo:          dexdoge.UnitInfo,
		AvailableWallets: []*asset.WalletDefinition{{
			Type:              walletTypeRPC,
			Tab:               "External",
			Description:       "Connect to dogecoind",
			DefaultConfigPath: dexbtc.SystemConfigPath("dogecoin"),
			ConfigOpts:        configOpts,
		}},
	}
)

func init() {
	asset.Register(BipID, &Driver{})
}

// Driver implements asset.Driver.
type Driver struct{}

// Open creates the DOGE exchange wallet. Start the wallet with its Run method.
func (d *Driver) Open(cfg *asset.WalletConfig, logger dex.Logger, network dex.Network) (asset.Wallet, error) {
	return NewWallet(cfg, logger, network)
}

// DecodeCoinID creates a human-readable representation of a coin ID for
// Dogecoin.
func (d *Driver) DecodeCoinID(coinID []byte) (string, error) {
	// Dogecoin and Bitcoin have the same tx hash and output format.
	return (&btc.Driver{}).DecodeCoinID(coinID)
}

// Info returns basic information about the wallet and asset.
func (d *Driver) Info() *asset.WalletInfo {
	return WalletInfo
}

// NewWallet is the exported constructor by which the DEX will import the
// exchange wallet. The wallet will shut down when the provided context is
// canceled. The configPath can be an empty string, in which case the standard
// system location of the dogecoind config file is assumed.
func NewWallet(cfg *asset.WalletConfig, logger dex.Logger, network dex.Network) (asset.Wallet, error) {
	var params *chaincfg.Params
	switch network {
	case dex.Mainnet:
		params = dexdoge.MainNetParams
	case dex.Testnet:
		params = dexdoge.TestNet4Params
	case dex.Regtest:
		params = dexdoge.RegressionNetParams
	default:
		return nil, fmt.Errorf("unknown network ID %v", network)
	}

	// Designate the clone ports. These will be overwritten by any explicit
	// settings in the configuration file.
	ports := dexbtc.NetPorts{
		Mainnet: "22555",
		Testnet: "44555",
		Simnet:  "18332",
	}
	cloneCFG := &btc.BTCCloneCFG{
		WalletCFG:                cfg,
		MinNetworkVersion:        minNetworkVersion,
		WalletInfo:               WalletInfo,
		Symbol:                   "doge",
		Logger:                   logger,
		Network:                  network,
		ChainParams:              params,
		Ports:                    ports,
		DefaultFallbackFee:       dexdoge.DefaultFee,
		DefaultFeeRateLimit:      dexdoge.DefaultFeeRateLimit,
		LegacyBalance:            true,
		Segwit:                   false,
		InitTxSize:               dexbtc.InitTxSize,
		InitTxSizeBase:           dexbtc.InitTxSizeBase,
		OmitAddressType:          true,
		LegacySignTxRPC:          true,
		LegacySendToAddr:         true,
		LegacyValidateAddressRPC: true,
		BooleanGetBlockRPC:       true,
		SingularWallet:           true,
		UnlockSpends:             true,
		ConstantDustLimit:        dustLimit,
		FeeEstimator: func(ctx context.Context, cl btc.RawRequester, _ uint64, allowExternal bool, net dex.Network) (uint64, error) {
			confArg, err := json.Marshal(feeConfs)
			if err != nil {
				return 0, err
			}
			resp, err := cl.RawRequest("estimatefee", []json.RawMessage{confArg})
			if err != nil {
				if !allowExternal {
					return 0, err
				}
				return externalFeeEstimator(ctx, net)
			}
			var feeRate float64
			err = json.Unmarshal(resp, &feeRate)
			if err != nil {
				return 0, err
			}
			if feeRate <= 0 {
				if !allowExternal {
					return 0, nil
				}
				return externalFeeEstimator(ctx, net)
			}
			// estimatefee is f#$%ed
			// https://github.com/decred/dcrdex/pull/1558#discussion_r850061882
			if feeRate > dexdoge.DefaultFeeRateLimit/1e5 {
				return dexdoge.DefaultFee, nil
			}
			return uint64(math.Round(feeRate * 1e5)), nil
		},
		ExternalFeeEstimator: func(ctx context.Context, net dex.Network) (uint64, error) {
			var url string
			if net == dex.Testnet {
				url = "https://api.bitcore.io/api/DOGE/testnet/fee/1"
			} else {
				url = "https://api.bitcore.io/api/DOGE/mainnet/fee/1"
			}
			ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
			defer cancel()
			r, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
			if err != nil {
				return 0, err
			}
			httpResponse, err := http.DefaultClient.Do(r)
			if err != nil {
				return 0, err
			}
			var resp map[string]float64
			reader := io.LimitReader(httpResponse.Body, 1<<20)
			err = json.NewDecoder(reader).Decode(&resp)
			if err != nil {
				return 0, err
			}
			httpResponse.Body.Close()

			dogePerKb, ok := resp["feerate"]
			if !ok {
				return 0, errors.New("no fee rate found")
			}
			// estimatefee is f#$%ed
			// https://github.com/decred/dcrdex/pull/1558#discussion_r850061882
			if dogePerKb <= 0 || dogePerKb > dexdoge.DefaultFeeRateLimit/1e5 {
				return dexdoge.DefaultFee, nil
			}
			feeRate := toSatoshi(dogePerKb)
			return feeRate, nil
		},
		BlockDeserializer: dexdoge.DeserializeBlock,
	}

	return btc.BTCCloneWallet(cloneCFG)
}

var conventionalConversionFactor = float64(dexbtc.UnitInfo.Conventional.ConversionFactor)

func toSatoshi(v float64) uint64 {
	return uint64(math.Round(v * conventionalConversionFactor))
}
