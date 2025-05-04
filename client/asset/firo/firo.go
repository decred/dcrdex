// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package firo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/client/asset/btc"
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/dexnet"
	dexbtc "decred.org/dcrdex/dex/networks/btc"
	dexfiro "decred.org/dcrdex/dex/networks/firo"

	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcd/btcutil"
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/btcsuite/btcd/wire"
)

const (
	version = 0
	// Zcoin XZC
	BipID = 136
	// Consensus changes v0.14.14.0
	minNetworkVersion   = 141400
	walletTypeRPC       = "firodRPC"
	walletTypeElectrum  = "electrumRPC"
	estimateFeeConfs    = 2 // 2 blocks should be enough
	needElectrumVersion = "4.1.5"
)

var (
	configOpts = append(btc.RPCConfigOpts("Firo", "8888"), []*asset.ConfigOption{
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
		SupportedVersions: []uint32{version},
		UnitInfo:          dexfiro.UnitInfo,
		AvailableWallets: []*asset.WalletDefinition{
			{
				Type:              walletTypeRPC,
				Tab:               "Firo Core (external)",
				Description:       "Connect to firod",
				DefaultConfigPath: dexbtc.SystemConfigPath("firo"),
				ConfigOpts:        configOpts,
				MultiFundingOpts:  btc.MultiFundingOpts,
			},
			{
				Type:             walletTypeElectrum,
				Tab:              "Electrum-Firo (external)",
				Description:      "Use an external Electrum-Firo Wallet",
				ConfigOpts:       append(btc.ElectrumConfigOpts, btc.CommonConfigOpts("FIRO", true)...),
				MultiFundingOpts: btc.MultiFundingOpts,
			},
		},
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

// MinLotSize calculates the minimum bond size for a given fee rate that avoids
// dust outputs on the swap and refund txs, assuming the maxFeeRate doesn't
// change.
func (d *Driver) MinLotSize(maxFeeRate uint64) uint64 {
	return dexbtc.MinLotSize(maxFeeRate, false)
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

	cloneCFG := &btc.BTCCloneCFG{
		WalletCFG:                cfg,
		MinNetworkVersion:        minNetworkVersion,
		WalletInfo:               WalletInfo,
		Symbol:                   "firo",
		Logger:                   logger,
		Network:                  network,
		ChainParams:              params,
		Ports:                    dexfiro.NetPorts,
		DefaultFallbackFee:       dexfiro.DefaultFee,
		DefaultFeeRateLimit:      dexfiro.DefaultFeeRateLimit,
		Segwit:                   false,
		InitTxSize:               dexbtc.InitTxSize,
		InitTxSizeBase:           dexbtc.InitTxSizeBase,
		LegacyBalance:            cfg.Type == walletTypeRPC,
		LegacyRawFeeLimit:        true, // sendrawtransaction Has single arg allowhighfees
		ArglessChangeAddrRPC:     true, // getrawchangeaddress has No address-type arg
		OmitAddressType:          true, // getnewaddress has No address-type arg
		LegacySignTxRPC:          true, // No signrawtransactionwithwallet RPC
		BooleanGetBlockRPC:       true, // Use bool true/false text for verbose param
		LegacyValidateAddressRPC: true, // use validateaddress to read 'ismine' bool
		SingularWallet:           true, // one wallet/node
		UnlockSpends:             true, // Firo chain wallet does Not unlock coins after sendrawtransaction
		AssetID:                  BipID,
		FeeEstimator:             estimateFee,
		ExternalFeeEstimator:     externalFeeRate,
		AddressDecoder:           decodeAddress,
		PrivKeyFunc:              nil, // set only for walletTypeRPC below
		BlockDeserializer:        nil, // set only for walletTypeRPC below
	}

	switch cfg.Type {
	case walletTypeRPC:
		var exw *btc.ExchangeWalletFullNode
		cloneCFG.PrivKeyFunc = func(addr string) (*btcec.PrivateKey, error) {
			return privKeyForAddress(exw, addr)
		}
		cloneCFG.BlockDeserializer = func(blk []byte) (*wire.MsgBlock, error) {
			return deserializeBlock(params, blk)
		}
		var err error
		exw, err = btc.BTCCloneWallet(cloneCFG)
		return exw, err
	case walletTypeElectrum:
		// override Ports - no default ports
		cloneCFG.Ports = dexbtc.NetPorts{}
		ver, err := dex.SemverFromString(needElectrumVersion)
		if err != nil {
			return nil, err
		}
		cloneCFG.MinElectrumVersion = *ver
		return btc.ElectrumWallet(cloneCFG)
	default:
		return nil, fmt.Errorf("unknown wallet type %q for firo", cfg.Type)
	}
}

/******************************************************************************
                             Helper Functions
******************************************************************************/

// decodeAddress decodes a Firo address. For normal transparent addresses this
// just uses btcd: btcutil.DecodeAddress.
func decodeAddress(address string, net *chaincfg.Params) (btcutil.Address, error) {
	if isExxAddress(address) {
		return decodeExxAddress(address, net)
	}
	decAddr, err := btcutil.DecodeAddress(address, net)
	if err != nil {
		return nil, err
	}
	if !decAddr.IsForNet(net) {
		return nil, errors.New("wrong network")
	}
	return decAddr, nil
}

// rpcCaller is satisfied by ExchangeWalletFullNode (baseWallet), providing
// direct RPC requests.
type rpcCaller interface {
	CallRPC(method string, args []any, thing any) error
}

// privKeyForAddress is Firo's dumpprivkey RPC which calls dumpprivkey once
// to get a One Time Authorization (OTA) which is appended to a second call
// for the same address to authorize the caller.
func privKeyForAddress(c rpcCaller, addr string) (*btcec.PrivateKey, error) {
	const methodDumpPrivKey = "dumpprivkey"
	var privkeyStr string
	err := c.CallRPC(methodDumpPrivKey, []any{addr}, &privkeyStr)
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

	err = c.CallRPC(methodDumpPrivKey, []any{addr, auth}, &privkeyStr)
	if err != nil {
		return nil, err
	}

	wif, err := btcutil.DecodeWIF(privkeyStr)
	if err != nil {
		return nil, err
	}

	return wif.PrivKey, nil
}

// NOTE: btc.(*baseWallet).feeRate calls the local and external fee estimators
// in sequence, applying the limits configured in baseWallet.

func estimateFee(ctx context.Context, rr btc.RawRequester, _ uint64) (uint64, error) {
	confArg, err := json.Marshal(estimateFeeConfs)
	if err != nil {
		return 0, err
	}
	resp, err := rr.RawRequest(ctx, "estimatefee", []json.RawMessage{confArg})
	if err != nil {
		return 0, err
	}
	var feeRate float64
	err = json.Unmarshal(resp, &feeRate)
	if err != nil {
		return 0, err
	}
	if feeRate <= 0 {
		return 0, nil
	}
	// Keep this check
	if feeRate > dexfiro.DefaultFeeRateLimit/1e5 {
		return dexfiro.DefaultFee, nil
	}
	return uint64(math.Round(feeRate * 1e5)), nil
}

// externalFeeRate returns a fee rate for the network. If an error is
// encountered fetching the testnet fee rate, we will try to return the
// mainnet fee rate.
func externalFeeRate(ctx context.Context, net dex.Network) (uint64, error) {
	const mainnetURI = "https://explorer.firo.org/insight-api-zcoin/utils/estimatefee"
	var uri string
	if net == dex.Testnet {
		uri = "https://testexplorer.firo.org/insight-api-zcoin/utils/estimatefee"
	} else {
		uri = "https://explorer.firo.org/insight-api-zcoin/utils/estimatefee"
	}
	feeRate, err := fetchExternalFee(ctx, uri)
	if err == nil || net != dex.Testnet {
		return feeRate, err
	}
	return fetchExternalFee(ctx, mainnetURI)
}

// fetchExternalFee calls 'estimatefee' API on Firo block explorer for
// the network. API returned float value is converted into sats/byte.
func fetchExternalFee(ctx context.Context, uri string) (uint64, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var resp map[string]float64
	if err := dexnet.Get(ctx, uri, &resp); err != nil {
		return 0, err
	}
	if resp == nil {
		return 0, errors.New("null response")
	}

	firoPerKilobyte, ok := resp["2"] // field '2': n.nnnn
	if !ok {
		return 0, errors.New("no fee rate in response")
	}
	if firoPerKilobyte <= 0 {
		return 0, fmt.Errorf("zero or negative fee rate")
	}
	return uint64(math.Round(firoPerKilobyte * 1e5)), nil // FIRO/kB => firo-sat/B
}
