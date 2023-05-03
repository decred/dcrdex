// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package zec

import (
	"context"
	"fmt"
	"strings"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/client/asset/btc"
	"decred.org/dcrdex/dex"
	dexbtc "decred.org/dcrdex/dex/networks/btc"
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
	BipID   = 133
	// The default fee is passed to the user as part of the asset.WalletInfo
	// structure.
	defaultFee          = 10
	defaultFeeRateLimit = 1000
	minNetworkVersion   = 5040250 // v5.4.2
	walletTypeRPC       = "zcashdRPC"

	mainnetNU5ActivationHeight        = 1687104
	testnetNU5ActivationHeight        = 1842420
	testnetSaplingActivationHeight    = 280000
	testnetOverwinterActivationHeight = 207500

	transparentAddressType = "p2pkh"
	orchardAddressType     = "orchard"
)

var (
	conventionalConversionFactor = float64(dexzec.UnitInfo.Conventional.ConversionFactor)
	fallbackFeeKey               = "fallbackfee"

	configOpts = []*asset.ConfigOption{
		{
			Key:         "rpcuser",
			DisplayName: "JSON-RPC Username",
			Description: "Zcash's 'rpcuser' setting",
		},
		{
			Key:         "rpcpassword",
			DisplayName: "JSON-RPC Password",
			Description: "Zcash's 'rpcpassword' setting",
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
			Description:  "Zcash's 'fallbackfee' rate. Units: ZEC/kB",
			DefaultValue: defaultFee * 1000 / 1e8,
		},
		{
			Key:         "feeratelimit",
			DisplayName: "Highest acceptable fee rate",
			Description: "This is the highest network fee rate you are willing to " +
				"pay on swap transactions. If feeratelimit is lower than a market's " +
				"maxfeerate, you will not be able to trade on that market with this " +
				"wallet.  Units: BTC/kB",
			DefaultValue: defaultFeeRateLimit * 1000 / 1e8,
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
		Name:              "Zcash",
		Version:           version,
		SupportedVersions: []uint32{version},
		UnitInfo:          dexzec.UnitInfo,
		AvailableWallets: []*asset.WalletDefinition{{
			Type:              walletTypeRPC,
			Tab:               "External",
			Description:       "Connect to zcashcoind",
			DefaultConfigPath: dexbtc.SystemConfigPath("zcash"),
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
	// Zcash and Bitcoin have the same tx hash and output format.
	return (&btc.Driver{}).DecodeCoinID(coinID)
}

// Info returns basic information about the wallet and asset.
func (d *Driver) Info() *asset.WalletInfo {
	return WalletInfo
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
		btcParams = dexzec.MainNetParams
		addrParams = dexzec.MainNetAddressParams
	case dex.Testnet:
		btcParams = dexzec.TestNet4Params
		addrParams = dexzec.TestNet4AddressParams
	case dex.Regtest:
		btcParams = dexzec.RegressionNetParams
		addrParams = dexzec.RegressionNetAddressParams
	default:
		return nil, fmt.Errorf("unknown network ID %v", net)
	}

	// Designate the clone ports. These will be overwritten by any explicit
	// settings in the configuration file.
	ports := dexbtc.NetPorts{
		Mainnet: "8232",
		Testnet: "18232",
		Simnet:  "18232",
	}
	var w *btc.ExchangeWalletFullNode
	cloneCFG := &btc.BTCCloneCFG{
		WalletCFG:                cfg,
		MinNetworkVersion:        minNetworkVersion,
		WalletInfo:               WalletInfo,
		Symbol:                   "zec",
		Logger:                   logger,
		Network:                  net,
		ChainParams:              btcParams,
		Ports:                    ports,
		DefaultFallbackFee:       defaultFee,
		DefaultFeeRateLimit:      defaultFeeRateLimit,
		LegacyRawFeeLimit:        true,
		ZECStyleBalance:          true,
		Segwit:                   false,
		InitTxSize:               dexzec.InitTxSize,
		InitTxSizeBase:           dexzec.InitTxSizeBase,
		OmitAddressType:          true,
		LegacySignTxRPC:          true,
		NumericGetRawRPC:         true,
		LegacyValidateAddressRPC: true,
		SingularWallet:           true,
		UnlockSpends:             true,
		FeeEstimator:             estimateFee,
		ConnectFunc: func() error {
			return connect(w)
		},
		AddrFunc: func() (btcutil.Address, error) {
			return transparentAddress(w, addrParams, btcParams)
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
			return dexzec.VersionNU5
		},
		// https://github.com/zcash/zcash/pull/6005
		ManualMedianTime:  true,
		OmitRPCOptionsArg: true,
		AssetID:           BipID,
	}

	var err error
	w, err = btc.BTCCloneWallet(cloneCFG)
	return w, err
}

type rpcCaller interface {
	CallRPC(method string, args []interface{}, thing interface{}) error
}

func transparentAddress(c rpcCaller, addrParams *dexzec.AddressParams, btcParams *chaincfg.Params) (btcutil.Address, error) {
	const zerothAccount = 0
	// One of the address types MUST be shielded.
	addrRes, err := zGetAddressForAccount(c, zerothAccount, []string{transparentAddressType, orchardAddressType})
	if err != nil {
		return nil, err
	}
	receivers, err := zGetUnifiedReceivers(c, addrRes.Address)
	if err != nil {
		return nil, err
	}
	return dexzec.DecodeAddress(receivers.Transparent, addrParams, btcParams)
}

// connect is Zcash's BTCCloneCFG.ConnectFunc. Ensures that accounts are set
// up correctly.
func connect(c rpcCaller) error {
	// Make sure we have zeroth and first account or are able to create them.
	accts, err := zListAccounts(c)
	if err != nil {
		return fmt.Errorf("error listing Zcash accounts: %w", err)
	}

	createAccount := func(n uint32) error {
		for _, acct := range accts {
			if acct.Number == n {
				return nil
			}
		}
		acctNumber, err := zGetNewAccount(c)
		if err != nil {
			if strings.Contains(err.Error(), "zcashd-wallet-tool") {
				return fmt.Errorf("account %d does not exist and cannot be created because wallet seed backup has not been acknowledged with the zcashd-wallet-tool utility", n)
			}
			return fmt.Errorf("error creating account %d: %w", n, err)
		}
		if acctNumber != n {
			return fmt.Errorf("no account %d found and newly created account has unexpected account number %d", n, acctNumber)
		}
		return nil
	}
	if err := createAccount(0); err != nil {
		return err
	}
	// When shielded pools are implemented, we'll use a separate, dedicated
	// account that uses unified addresses with only and Orchard receiver type.
	// if err := createAccount(1); err != nil {
	// 	return err
	// }
	return nil
}

func zecTx(tx *wire.MsgTx) *dexzec.Tx {
	return dexzec.NewTxFromMsgTx(tx, dexzec.MaxExpiryHeight)
}

// estimateFee uses ZCash's estimatefee RPC, since estimatesmartfee
// ZCash's fee estimation is pretty crappy. Full nodes can take hours to
func estimateFee(context.Context, btc.RawRequester, uint64) (uint64, error) {
	return dexzec.LegacyFeeRate, nil
}

// signTx signs the transaction input with Zcash's BLAKE-2B sighash digest.
// Won't work with shielded or blended transactions.
func signTx(btcTx *wire.MsgTx, idx int, pkScript []byte, hashType txscript.SigHashType,
	key *btcec.PrivateKey, amts []int64, prevScripts [][]byte) ([]byte, error) {

	tx := zecTx(btcTx)

	sigHash, err := tx.SignatureDigest(idx, hashType, amts, prevScripts)
	if err != nil {
		return nil, fmt.Errorf("sighash calculation error: %v", err)
	}

	return append(ecdsa.Sign(key, sigHash[:]).Serialize(), byte(hashType)), nil
}
