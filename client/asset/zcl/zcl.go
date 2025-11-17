// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package zcl

import (
	"context"
	"fmt"
	"math"
	"strconv"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/client/asset/btc"
	"decred.org/dcrdex/dex"
	dexbtc "decred.org/dcrdex/dex/networks/btc"
	dexzcl "decred.org/dcrdex/dex/networks/zcl"
	dexzec "decred.org/dcrdex/dex/networks/zec"
	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcd/btcec/v2/ecdsa"
	"github.com/btcsuite/btcd/btcutil"
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcd/txscript"
	"github.com/btcsuite/btcd/wire"
)

const (
	version = 0
	BipID   = 147
	// The default fee is passed to the user as part of the asset.WalletInfo
	// structure.
	defaultFee          = 10
	defaultFeeRateLimit = 1000
	minNetworkVersion   = 2010159 // v2.1.1-9-f7bff4ff3-dirty
	walletTypeRPC       = "zclassicdRPC"

	transparentAddressType = "p2pkh"
	orchardAddressType     = "orchard"
	saplingAddressType     = "sapling"
)

var (
	configOpts = []*asset.ConfigOption{
		{
			Key:         "rpcuser",
			DisplayName: "JSON-RPC Username",
			Description: "Zclassic's 'rpcuser' setting",
		},
		{
			Key:         "rpcpassword",
			DisplayName: "JSON-RPC Password",
			Description: "Zclassic's 'rpcpassword' setting",
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
			Key:          "fallbackfee",
			DisplayName:  "Fallback fee rate",
			Description:  "Zclassic's 'fallbackfee' rate. Units: ZEC/kB",
			DefaultValue: strconv.FormatFloat(defaultFee*1000/1e8, 'f', -1, 64),
		},
		{
			Key:         "feeratelimit",
			DisplayName: "Highest acceptable fee rate",
			Description: "This is the highest network fee rate you are willing to " +
				"pay on swap transactions. If feeratelimit is lower than a market's " +
				"maxfeerate, you will not be able to trade on that market with this " +
				"wallet.  Units: BTC/kB",
			DefaultValue: strconv.FormatFloat(defaultFeeRateLimit*1000/1e8, 'f', -1, 64),
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
	}
	// WalletInfo defines some general information about a Zcash wallet.
	WalletInfo = &asset.WalletInfo{
		Name:              "Zclassic",
		SupportedVersions: []uint32{version},
		UnitInfo:          dexzcl.UnitInfo,
		AvailableWallets: []*asset.WalletDefinition{{
			Type:              walletTypeRPC,
			Tab:               "External",
			Description:       "Connect to zclassicd",
			DefaultConfigPath: dexbtc.SystemConfigPath("zclassic"),
			ConfigOpts:        configOpts,
			NoAuth:            true,
		}},
	}
)

func init() {
	asset.Register(BipID, &Driver{})
}

// Driver implements asset.Driver.
type Driver struct{}

// Open creates the ZEC exchange wallet. Start the wallet with its Run method.
func (d *Driver) Open(cfg *asset.WalletConfig, logger dex.Logger, network dex.Network) (asset.Wallet, error) {
	return NewWallet(cfg, logger, network)
}

// DecodeCoinID creates a human-readable representation of a coin ID for
// Zcash.
func (d *Driver) DecodeCoinID(coinID []byte) (string, error) {
	// Zcash shielded transactions don't have transparent outputs, so the coinID
	// will just be the tx hash.
	if len(coinID) == chainhash.HashSize {
		var txHash chainhash.Hash
		copy(txHash[:], coinID)
		return txHash.String(), nil
	}
	// For transparent transactions, Zcash and Bitcoin have the same tx hash
	// and output format.
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
// system location of the zcashd config file is assumed.
func NewWallet(cfg *asset.WalletConfig, logger dex.Logger, net dex.Network) (asset.Wallet, error) {
	var btcParams *chaincfg.Params
	var addrParams *dexzec.AddressParams
	switch net {
	case dex.Mainnet:
		btcParams = dexzcl.MainNetParams
		addrParams = dexzec.MainNetAddressParams
	case dex.Testnet:
		btcParams = dexzcl.TestNet4Params
		addrParams = dexzec.TestNet4AddressParams
	case dex.Regtest:
		btcParams = dexzcl.RegressionNetParams
		addrParams = dexzec.RegressionNetAddressParams
	default:
		return nil, fmt.Errorf("unknown network ID %v", net)
	}

	// Designate the clone ports. These will be overwritten by any explicit
	// settings in the configuration file.
	ports := dexbtc.NetPorts{
		Mainnet: "8023",
		Testnet: "18023",
		Simnet:  "35768", // zclassic uses 18023 for regtest too. Using our alpha harness port instead.
	}

	var w *btc.ExchangeWalletNoAuth
	cloneCFG := &btc.BTCCloneCFG{
		WalletCFG:           cfg,
		MinNetworkVersion:   minNetworkVersion,
		WalletInfo:          WalletInfo,
		Symbol:              "zcl",
		Logger:              logger,
		Network:             net,
		ChainParams:         btcParams,
		Ports:               ports,
		DefaultFallbackFee:  defaultFee,
		DefaultFeeRateLimit: defaultFeeRateLimit,
		LegacyRawFeeLimit:   true,
		BalanceFunc: func(ctx context.Context, locked uint64) (*asset.Balance, error) {
			var bal float64
			// args: "(dummy)" minconf includeWatchonly
			if err := w.CallRPC("getbalance", []any{"", 0, false}, &bal); err != nil {
				return nil, err
			}
			return &asset.Balance{
				Available: toSatoshi(bal) - locked,
				Locked:    locked,
				Other:     make(map[asset.BalanceCategory]asset.CustomBalance),
			}, nil
		},
		Segwit: false,
		// InitTxSize from zec still looks right to me for Zclassic.
		InitTxSize:               dexzec.InitTxSize,
		InitTxSizeBase:           dexzec.InitTxSizeBase,
		OmitAddressType:          true,
		LegacySignTxRPC:          true,
		NumericGetRawRPC:         true,
		LegacyValidateAddressRPC: true,
		SingularWallet:           true,
		UnlockSpends:             true,
		FeeEstimator: func(_ context.Context, _ btc.RawRequester, nBlocks uint64) (uint64, error) {
			var r float64
			if err := w.CallRPC("estimatefee", []any{nBlocks}, &r); err != nil {
				return 0, fmt.Errorf("error calling 'estimatefee': %v", err)
			}
			if r < 0 {
				return 0, nil
			}
			return toSatoshi(r), nil
		},
		AddressDecoder: func(addr string, net *chaincfg.Params) (btcutil.Address, error) {
			return dexzec.DecodeAddress(addr, addrParams, btcParams)
		},
		AddressStringer: func(addr btcutil.Address, btcParams *chaincfg.Params) (string, error) {
			return dexzec.EncodeAddress(addr, addrParams)
		},
		TxSizeCalculator: dexzec.CalcTxSize,
		NonSegwitSigner:  signTx,
		TxDeserializer: func(b []byte) (*wire.MsgTx, error) {
			zecTx, err := dexzec.DeserializeTx(b)
			if err != nil {
				return nil, err
			}
			return zecTx.MsgTx, nil
		},
		BlockDeserializer: func(b []byte) (*wire.MsgBlock, error) {
			zecBlock, err := dexzec.DeserializeBlock(b)
			if err != nil {
				return nil, err
			}
			return &zecBlock.MsgBlock, nil
		},
		TxSerializer: func(btcTx *wire.MsgTx) ([]byte, error) {
			return zecTx(btcTx).Bytes()
		},
		TxHasher: func(tx *wire.MsgTx) *chainhash.Hash {
			h := zecTx(tx).TxHash()
			return &h
		},
		TxVersion: func() int32 {
			return dexzec.VersionSapling
		},
		// https://github.com/zcash/zcash/pull/6005
		ManualMedianTime:  true,
		OmitRPCOptionsArg: true,
		AssetID:           BipID,
	}
	w, err := btc.BTCCloneWalletNoAuth(cloneCFG)
	return w, err
}

// TODO: Implement ShieldedWallet
// type zecWallet struct {
// 	*btc.ExchangeWalletNoAuth
// 	log         dex.Logger
// 	lastAddress atomic.Value // "string"
// }

// var _ asset.ShieldedWallet = (*zecWallet)(nil)

func zecTx(tx *wire.MsgTx) *dexzec.Tx {
	return dexzec.NewTxFromMsgTx(tx, dexzec.MaxExpiryHeight)
}

// signTx signs the transaction input with Zcash's BLAKE-2B sighash digest.
// Won't work with shielded or blended transactions.
func signTx(
	btcTx *wire.MsgTx, idx int, pkScript []byte, hashType txscript.SigHashType,
	key *btcec.PrivateKey, amts []int64, prevScripts [][]byte,
) ([]byte, error) {

	tx := zecTx(btcTx)
	sigHash, err := tx.SignatureDigest(idx, hashType, pkScript, amts, prevScripts)
	if err != nil {
		return nil, fmt.Errorf("sighash calculation error: %v", err)
	}

	return append(ecdsa.Sign(key, sigHash[:]).Serialize(), byte(hashType)), nil
}

func toSatoshi(v float64) uint64 {
	const conventionalConversionFactor = 1e8
	return uint64(math.Round(v * conventionalConversionFactor))
}
