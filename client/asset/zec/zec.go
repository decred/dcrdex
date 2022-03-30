// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package zec

import (
	"encoding/json"
	"fmt"
	"math"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/client/asset/btc"
	"decred.org/dcrdex/dex"
	dexbtc "decred.org/dcrdex/dex/networks/btc"
	dexzec "decred.org/dcrdex/dex/networks/zec"
	"github.com/btcsuite/btcd/btcec"
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcd/txscript"
	"github.com/btcsuite/btcd/wire"
	"github.com/btcsuite/btcutil"
)

const (
	version = 0
	BipID   = 133
	// The default fee is passed to the user as part of the asset.WalletInfo
	// structure.
	defaultFee          = 10
	defaultFeeRateLimit = 1000
	minNetworkVersion   = 4060051
	walletTypeRPC       = "zcashdRPC"
)

var (
	fallbackFeeKey = "fallbackfee"
	configOpts     = []*asset.ConfigOption{
		{
			Key:         "rpcuser",
			DisplayName: "JSON-RPC Username",
			Description: "ZCash's 'rpcuser' setting",
		},
		{
			Key:         "rpcpassword",
			DisplayName: "JSON-RPC Password",
			Description: "ZCash's 'rpcpassword' setting",
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
			Description:  "ZCash's 'fallbackfee' rate. Units: ZEC/kB",
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
	// WalletInfo defines some general information about a ZCash wallet.
	WalletInfo = &asset.WalletInfo{
		Name:     "ZCash",
		Version:  version,
		UnitInfo: dexzec.UnitInfo,
		AvailableWallets: []*asset.WalletDefinition{{
			Type:              walletTypeRPC,
			Tab:               "External",
			Description:       "Connect to zcashcoind",
			DefaultConfigPath: dexbtc.SystemConfigPath("zcash"),
			ConfigOpts:        configOpts,
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
// ZCash.
func (d *Driver) DecodeCoinID(coinID []byte) (string, error) {
	// ZCash and Bitcoin have the same tx hash and output format.
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
	nu5Activated := net == dex.Simnet
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
		OmitAddressType:          true,
		LegacySignTxRPC:          true,
		NumericGetRawRPC:         true,
		LegacyValidateAddressRPC: true,
		SingularWallet:           true,
		FeeEstimator:             estimateFee,
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
			const mainnetNU5ActivationHeight = 1687104
			const testnetNU5ActivationHeight = 1842420

			if nu5Activated {
				return dexzec.VersionNU5
			}

			bestHeight, err := w.GetBestBlockHeight()
			if err != nil {
				logger.Errorf("Error getting best height for tx version calculation: %v", err)
				return dexzec.VersionNU5
			}

			h := bestHeight + 1

			switch net {
			case dex.Mainnet:
				if h >= mainnetNU5ActivationHeight {
					nu5Activated = true
				}
			case dex.Testnet:
				if h >= testnetNU5ActivationHeight {
					nu5Activated = true
				}
			}
			if nu5Activated {
				return dexzec.VersionNU5
			}
			return dexzec.VersionSapling
		},
	}

	var err error
	w, err = btc.BTCCloneWallet(cloneCFG)
	return w, err
}

func zecTx(tx *wire.MsgTx) *dexzec.Tx {
	return dexzec.NewTxFromMsgTx(tx, dexzec.MaxExpiryHeight)
}

// estimateFee uses ZCash's estimatefee RPC, since estimatesmartfee
// is not implemented.
// ZCash's fee estimation is pretty crappy. Full nodes can take hours to
// get up to speed, and forget about simnet.
// See https://github.com/zcash/zcash/issues/2552
func estimateFee(node btc.RawRequester, confTarget uint64) (uint64, error) {
	resp, err := node.RawRequest("estimatefee", nil)
	if err != nil {
		return 0, err
	}
	var feeRate float64
	err = json.Unmarshal(resp, &feeRate)
	if err != nil {
		return 0, err
	}
	if feeRate <= 0 {
		return 0, fmt.Errorf("fee could not be estimated")
	}
	return uint64(math.Round(feeRate * 1e5)), nil
}

// signTx signs the transaction input with ZCash's BLAKE-2B sighash digest.
// Won't work with shielded or blended transactions.
func signTx(btcTx *wire.MsgTx, idx int, pkScript []byte, hashType txscript.SigHashType, key *btcec.PrivateKey, amt uint64) ([]byte, error) {
	tx := zecTx(btcTx)
	cache, err := NewTxSigHashes(tx)
	if err != nil {
		return nil, fmt.Errorf("NewTxSigHashes error: %v", err)
	}

	// Compare with zcash/zcash TransactionSignatureCreator::CreateSig
	// also zcash_transaction_transparent_signature_digest()

	_, sigHash, err := signatureHash(pkScript, cache, hashType, tx, idx, int64(amt), dexzec.ConsensusBranchCanopy)
	if err != nil {
		return nil, fmt.Errorf("sighash calculation error: %v", err)
	}

	signature, err := key.Sign(sigHash)
	if err != nil {
		return nil, fmt.Errorf("cannot sign tx input: %s", err)
	}

	return append(signature.Serialize(), byte(hashType)), nil
}
