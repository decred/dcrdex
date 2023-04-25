// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package firo

import (
	"errors"
	"fmt"
	"strings"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/client/asset/btc"
	"decred.org/dcrdex/dex"
	dexbtc "decred.org/dcrdex/dex/networks/btc"
	dexfiro "decred.org/dcrdex/dex/networks/firo"

	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcd/btcutil"
	"github.com/btcsuite/btcd/chaincfg"
)

const (
	version           = 0
	BipID             = 136    // Zcoin XZC
	minNetworkVersion = 141201 // bitcoin 0.14 base
	walletTypeRPC     = "firodRPC"
)

var (
	configOpts = append(btc.RPCConfigOpts("Firo", "8168"), []*asset.ConfigOption{
		{
			Key:          "fallbackfee",
			DisplayName:  "Fallback fee rate",
			Description:  "Firo's 'fallbackfee' rate. Units: FIRO/kB",
			DefaultValue: dexfiro.DefaultFee * 1000 / 1e8,
		},
		{
			Key:         "feeratelimit",
			DisplayName: "Highest acceptable fee rate",
			Description: "This is the highest network fee rate you are willing to " +
				"pay on swap transactions. If feeratelimit is lower than a market's " +
				"maxfeerate, you will not be able to trade on that market with this " +
				"wallet.  Units: FIRO/kB",
			DefaultValue: dexfiro.DefaultFeeRateLimit * 1000 / 1e8,
		},
		{
			Key:         "txsplit",
			DisplayName: "Pre-split funding inputs",
			Description: "When placing an order, create a \"split\" transaction to fund the order without locking more of the wallet balance than " +
				"necessary. Otherwise, excess funds may be reserved to fund the order until the first swap contract is broadcast " +
				"during match settlement, or the order is canceled. This an extra transaction for which network mining fees are paid. " +
				"Used only for standing-type orders, e.g. limit orders without immediate time-in-force.",
			IsBoolean: true,
			// DefaultValue is false
		},
	}...)
	// WalletInfo defines some general information about a Firo wallet.
	WalletInfo = &asset.WalletInfo{
		Name:              "Firo",
		Version:           version,
		SupportedVersions: []uint32{version},
		UnitInfo:          dexfiro.UnitInfo,
		AvailableWallets: []*asset.WalletDefinition{{
			Type:              walletTypeRPC,
			Tab:               "External",
			Description:       "Connect to firod",
			DefaultConfigPath: dexbtc.SystemConfigPath("firo"),
			ConfigOpts:        configOpts,
		}},
	}
)

func init() {
	asset.Register(BipID, &Driver{})
}

// Driver implements asset.Driver.
type Driver struct{}

// Open creates the FIRO exchange wallet. Start the wallet with its Run method.
func (d *Driver) Open(cfg *asset.WalletConfig, logger dex.Logger, network dex.Network) (asset.Wallet, error) {
	return NewWallet(cfg, logger, network)
}

// DecodeCoinID creates a human-readable representation of a coin ID
// for Firo.
func (d *Driver) DecodeCoinID(coinID []byte) (string, error) {
	// Firo and Bitcoin have the same tx hash and output format.
	return (&btc.Driver{}).DecodeCoinID(coinID)
}

// Info returns basic information about the wallet and asset.
func (d *Driver) Info() *asset.WalletInfo {
	return WalletInfo
}

// NewWallet is the exported constructor by which the DEX will import the
// exchange wallet. The wallet will shut down when the provided context is
// canceled. The configPath can be an empty string, in which case the standard
// system location of the firod config file is assumed.
func NewWallet(cfg *asset.WalletConfig, logger dex.Logger, network dex.Network) (asset.Wallet, error) {
	var params *chaincfg.Params
	switch network {
	case dex.Mainnet:
		params = dexfiro.MainNetParams
	case dex.Testnet:
		params = dexfiro.TestNetParams
	case dex.Regtest:
		params = dexfiro.RegressionNetParams
	default:
		return nil, fmt.Errorf("unknown network ID %v", network)
	}

	// Designate the clone ports. These will be overwritten by any explicit
	// settings in the configuration file.
	ports := dexbtc.NetPorts{
		Mainnet: "8168",
		Testnet: "18168",
		Simnet:  "18444", // Regtest
	}

	var w *btc.ExchangeWalletFullNode

	cloneCFG := &btc.BTCCloneCFG{
		WalletCFG:                cfg,
		MinNetworkVersion:        minNetworkVersion,
		WalletInfo:               WalletInfo,
		Symbol:                   "firo",
		Logger:                   logger,
		Network:                  network,
		ChainParams:              params,
		Ports:                    ports,
		DefaultFallbackFee:       dexfiro.DefaultFee,
		DefaultFeeRateLimit:      dexfiro.DefaultFeeRateLimit,
		Segwit:                   false,
		InitTxSize:               dexbtc.InitTxSize,
		InitTxSizeBase:           dexbtc.InitTxSizeBase,
		LegacyBalance:            true,
		LegacyRawFeeLimit:        true,  // sendrawtransaction Has single arg allowhighfees
		ArglessChangeAddrRPC:     true,  // getrawchangeaddress has No address-type arg
		OmitAddressType:          true,  // getnewaddress has No address-type arg
		LegacySignTxRPC:          true,  // No signrawtransactionwithwallet RPC
		BooleanGetBlockRPC:       true,  // Use bool true/false text for verbose param
		NumericGetRawRPC:         false, // getrawtransaction uses either 0/1 Or true/false
		LegacyValidateAddressRPC: true,  // use validateaddress to read 'ismine' bool
		SingularWallet:           true,  // one wallet/node
		UnlockSpends:             false, // checked after sendtoaddress
		AssetID:                  BipID,
		FeeEstimator:             btc.NoLocalFeeRate,
		PrivKeyFunc: func(addr string) (*btcec.PrivateKey, error) {
			return privKeyForAddress(w, addr)
		},
	}

	var err error
	w, err = btc.BTCCloneWallet(cloneCFG)
	return w, err
}

// rpcCaller is satisfied by ExchangeWalletFullNode (baseWallet), providing
// direct RPC requests.
type rpcCaller interface {
	CallRPC(method string, args []interface{}, thing interface{}) error
}

func privKeyForAddress(c rpcCaller, addr string) (*btcec.PrivateKey, error) {
	const methodDumpPrivKey = "dumpprivkey"
	var privkeyStr string
	err := c.CallRPC(methodDumpPrivKey, []interface{}{addr}, &privkeyStr)
	if err == nil { // really, expect an error...
		return nil, errors.New("firo dumpprivkey: no authorization challenge")
	}

	errStr := err.Error()
	searchStr := "authorization code is: "
	i0 := strings.Index(errStr, searchStr) // TODO: use CutPrefix when Go 1.20 is min
	if i0 == -1 {
		return nil, err
	}
	i := i0 + len(searchStr)
	auth := errStr[i : i+4]
	/// fmt.Printf("OTA: %s\n", auth)

	err = c.CallRPC(methodDumpPrivKey, []interface{}{addr, auth}, &privkeyStr)
	if err != nil {
		return nil, err
	}

	wif, err := btcutil.DecodeWIF(privkeyStr)
	if err != nil {
		return nil, err
	}

	return wif.PrivKey, nil
}
