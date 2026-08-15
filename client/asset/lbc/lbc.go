// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package lbc

import (
	"context"
	"fmt"
	"math"
	"strconv"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/client/asset/btc"
	"decred.org/dcrdex/dex"
	dexbtc "decred.org/dcrdex/dex/networks/btc"
	dexlbc "decred.org/dcrdex/dex/networks/lbc"
	"github.com/btcsuite/btcd/chaincfg"
)

const (
	version = 0
	// BipID is the BIP-0044 / SLIP-0044 coin type for LBRY Credits.
	BipID = 140

	// minNetworkVersion is intentionally low: local/dev lbcd builds report a
	// small version.Numeric(). Operators of released binaries will still pass.
	minNetworkVersion = 0
	walletTypeRPC     = "lbcwalletRPC"
)

var (
	configOpts = append(btc.RPCConfigOpts("LBRY Credits", "9244"), []*asset.ConfigOption{
		{
			Key:          "fallbackfee",
			DisplayName:  "Fallback fee rate",
			Description:  "LBC's fallback fee rate. Units: LBC/kB",
			DefaultValue: strconv.FormatFloat(dexlbc.DefaultFee*1000/1e8, 'f', -1, 64),
		},
		{
			Key:         "feeratelimit",
			DisplayName: "Highest acceptable fee rate",
			Description: "This is the highest network fee rate you are willing to " +
				"pay on swap transactions. If feeratelimit is lower than a market's " +
				"maxfeerate, you will not be able to trade on that market with this " +
				"wallet. Units: LBC/kB",
			DefaultValue: strconv.FormatFloat(dexlbc.DefaultFeeRateLimit*1000/1e8, 'f', -1, 64),
		},
		{
			Key:         "txsplit",
			DisplayName: "Pre-split funding inputs",
			Description: "When placing an order, create a \"split\" transaction to fund the order without locking more of the wallet balance than " +
				"necessary. Otherwise, excess funds may be reserved to fund the order until the first swap contract is broadcast " +
				"during match settlement, or the order is canceled. This is an extra transaction for which network mining fees are paid. " +
				"Used only for standing-type orders, e.g. limit orders without immediate time-in-force.",
			IsBoolean:    true,
			DefaultValue: "true",
		},
	}...)
	// WalletInfo defines some general information about an LBC wallet.
	WalletInfo = &asset.WalletInfo{
		Name:              "LBRY Credits",
		SupportedVersions: []uint32{version},
		UnitInfo:          dexlbc.UnitInfo,
		AvailableWallets: []*asset.WalletDefinition{{
			Type:              walletTypeRPC,
			Tab:               "External",
			Description:       "Connect to lbcwallet (which must be connected to lbcd)",
			DefaultConfigPath: dexbtc.SystemConfigPath("lbcwallet"),
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

// Open creates the LBC exchange wallet.
func (d *Driver) Open(cfg *asset.WalletConfig, logger dex.Logger, network dex.Network) (asset.Wallet, error) {
	return NewWallet(cfg, logger, network)
}

// DecodeCoinID creates a human-readable representation of a coin ID for LBC.
func (d *Driver) DecodeCoinID(coinID []byte) (string, error) {
	return (&btc.Driver{}).DecodeCoinID(coinID)
}

// Info returns basic information about the wallet and asset.
func (d *Driver) Info() *asset.WalletInfo {
	return WalletInfo
}

// MinLotSize calculates the minimum lot size for a given fee rate.
func (d *Driver) MinLotSize(maxFeeRate uint64) uint64 {
	return dexbtc.MinLotSize(maxFeeRate, false)
}

func toSatoshi(v float64) uint64 {
	return uint64(math.Round(v * 1e8))
}

// NewWallet is the exported constructor by which the DEX will import the
// exchange wallet. Connect to lbcwallet's legacy JSON-RPC (default port 9244).
// lbcwallet must be connected to an lbcd node for chain RPCs (passthrough).
func NewWallet(cfg *asset.WalletConfig, logger dex.Logger, network dex.Network) (asset.Wallet, error) {
	var params *chaincfg.Params
	switch network {
	case dex.Mainnet:
		params = dexlbc.MainNetParams
	case dex.Testnet:
		params = dexlbc.TestNet3Params
	case dex.Regtest:
		params = dexlbc.RegressionNetParams
	default:
		return nil, fmt.Errorf("unknown network ID %v", network)
	}

	// Wallet RPC ports (lbcwallet), not node ports.
	ports := dexbtc.NetPorts{
		Mainnet: "9244",
		Testnet: "19244",
		Simnet:  "29244",
	}

	// w is closed over by BalanceFunc / FeeEstimator (same pattern as ZCL).
	var w *btc.ExchangeWalletFullNode
	cloneCFG := &btc.BTCCloneCFG{
		WalletCFG:           cfg,
		MinNetworkVersion:   minNetworkVersion,
		WalletInfo:          WalletInfo,
		Symbol:              "lbc",
		Logger:              logger,
		Network:             network,
		ChainParams:         params,
		Ports:               ports,
		DefaultFallbackFee:  dexlbc.DefaultFee,
		DefaultFeeRateLimit: dexlbc.DefaultFeeRateLimit,
		// lbcwallet has no getwalletinfo / getbalances; use getbalance.
		BalanceFunc: func(ctx context.Context, locked uint64) (*asset.Balance, error) {
			var bal float64
			// minconf=0 to include unconfirmed; account "" is default.
			if err := w.CallRPC("getbalance", []any{"*", 0}, &bal); err != nil {
				// Fallback: no-arg getbalance
				if err2 := w.CallRPC("getbalance", nil, &bal); err2 != nil {
					return nil, fmt.Errorf("getbalance: %v (fallback: %v)", err, err2)
				}
			}
			return &asset.Balance{
				Available: toSatoshi(bal) - locked,
				Locked:    locked,
				Other:     make(map[asset.BalanceCategory]asset.CustomBalance),
			}, nil
		},
		// Non-segwit for lbcwallet RPC compatibility: getrawchangeaddress takes
		// (account, addresstype); dcrdex would pass "bech32" as account. Legacy
		// P2SH swap contracts still work on LBC mainnet (SegWit is optional).
		// Follow-up: add AccountFirstChangeAddr support and enable Segwit.
		Segwit:                   false,
		InitTxSize:               dexbtc.InitTxSize,
		InitTxSizeBase:           dexbtc.InitTxSizeBase,
		OmitAddressType:          true,
		LegacySignTxRPC:          true,
		LegacyValidateAddressRPC: true,
		SingularWallet:           true,
		UnlockSpends:             true, // lbcwallet may not auto-unlock spent coins
		BlockDeserializer:        dexlbc.DeserializeBlock,
		AssetID:                  BipID,
		FeeEstimator: func(ctx context.Context, cl btc.RawRequester, confTarget uint64) (uint64, error) {
			// Prefer estimatesmartfee if lbcd provides it via passthrough.
			// Fall back to DefaultFee.
			return dexlbc.DefaultFee, nil
		},
	}

	var err error
	w, err = btc.BTCCloneWallet(cloneCFG)
	return w, err
}
