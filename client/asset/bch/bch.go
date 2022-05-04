// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package bch

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/client/asset/btc"
	"decred.org/dcrdex/dex"
	dexbch "decred.org/dcrdex/dex/networks/bch"
	dexbtc "decred.org/dcrdex/dex/networks/btc"
	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/btcsuite/btcd/txscript"
	"github.com/btcsuite/btcd/wire"
	"github.com/gcash/bchd/bchec"
	bchscript "github.com/gcash/bchd/txscript"
	bchwire "github.com/gcash/bchd/wire"
)

const (
	version = 0

	// BipID is the Bip 44 coin ID for Bitcoin Cash.
	BipID = 145
	// The default fee is passed to the user as part of the asset.WalletInfo
	// structure.
	defaultFee         = 100
	minNetworkVersion  = 221100
	walletTypeRPC      = "bitcoindRPC"
	walletTypeLegacy   = ""
	walletTypeElectrum = "electrumRPC"
)

var (
	netPorts = dexbtc.NetPorts{
		Mainnet: "8332",
		Testnet: "28332",
		Simnet:  "18443",
	}
	fallbackFeeKey = "fallbackfee"
	walletNameOpt  = []*asset.ConfigOption{ // slice for easy appends
		{
			Key:         "walletname",
			DisplayName: "Wallet Name",
			Description: "The wallet name",
		},
	}
	commonOpts = []*asset.ConfigOption{
		{
			Key:         "rpcuser",
			DisplayName: "JSON-RPC Username",
			Description: "Bitcoin Cash 'rpcuser' setting",
		},
		{
			Key:         "rpcpassword",
			DisplayName: "JSON-RPC Password",
			Description: "Bitcoin Cash 'rpcpassword' setting",
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
			Description:  "Bitcoin Cash 'fallbackfee' rate. Units: BCH/kB",
			DefaultValue: defaultFee * 1000 / 1e8,
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
	rpcWalletDefinition = &asset.WalletDefinition{
		Type:              walletTypeRPC,
		Tab:               "Bitcoin Cash Node (external)",
		Description:       "Connect to bitcoind (the BCH fork)",
		DefaultConfigPath: dexbtc.SystemConfigPath("bitcoin"), // Same as bitcoin. That's dumb.
		ConfigOpts:        append(walletNameOpt, commonOpts...),
	}
	electrumWalletDefinition = &asset.WalletDefinition{
		Type:        walletTypeElectrum,
		Tab:         "Electron Cash  (external)",
		Description: "Use an external Electron Cash (BCH Electrum fork) Wallet",
		// json: DefaultConfigPath: filepath.Join(btcutil.AppDataDir("electrom-cash", false), "config"), // maybe?
		ConfigOpts: commonOpts,
	}
	// WalletInfo defines some general information about a Bitcoin Cash wallet.
	WalletInfo = &asset.WalletInfo{
		Name:    "Bitcoin Cash",
		Version: version,
		// Same as bitcoin. That's dumb.
		UnitInfo: dexbch.UnitInfo,
		AvailableWallets: []*asset.WalletDefinition{
			rpcWalletDefinition,
			// electrumWalletDefinition, // getinfo RPC needs backport: https://github.com/Electron-Cash/Electron-Cash/pull/2399
		},
	}
)

func init() {
	asset.Register(BipID, &Driver{})
}

// Driver implements asset.Driver.
type Driver struct{}

// Check that Driver implements asset.Driver.
var _ asset.Driver = (*Driver)(nil)

// Open creates the BCH exchange wallet. Start the wallet with its Run method.
func (d *Driver) Open(cfg *asset.WalletConfig, logger dex.Logger, network dex.Network) (asset.Wallet, error) {
	return NewWallet(cfg, logger, network)
}

// DecodeCoinID creates a human-readable representation of a coin ID for
// Bitcoin Cash.
func (d *Driver) DecodeCoinID(coinID []byte) (string, error) {
	// Bitcoin Cash and Bitcoin have the same tx hash and output format.
	return (&btc.Driver{}).DecodeCoinID(coinID)
}

// Info returns basic information about the wallet and asset.
func (d *Driver) Info() *asset.WalletInfo {
	return WalletInfo
}

// NewWallet is the exported constructor by which the DEX will import the
// exchange wallet.
func NewWallet(cfg *asset.WalletConfig, logger dex.Logger, network dex.Network) (asset.Wallet, error) {
	var params *chaincfg.Params
	switch network {
	case dex.Mainnet:
		params = dexbch.MainNetParams
	case dex.Testnet:
		params = dexbch.TestNet4Params
	case dex.Regtest:
		params = dexbch.RegressionNetParams
	default:
		return nil, fmt.Errorf("unknown network ID %v", network)
	}

	// Designate the clone ports. These will be overwritten by any explicit
	// settings in the configuration file. Bitcoin Cash uses the same default
	// ports as Bitcoin.
	cloneCFG := &btc.BTCCloneCFG{
		WalletCFG:          cfg,
		MinNetworkVersion:  minNetworkVersion,
		WalletInfo:         WalletInfo,
		Symbol:             "bch",
		Logger:             logger,
		Network:            network,
		ChainParams:        params,
		Ports:              netPorts,
		DefaultFallbackFee: defaultFee,
		Segwit:             false,
		LegacyBalance:      true,
		// Bitcoin Cash uses the Cash Address encoding, which is Bech32, but not
		// indicative of segwit. We provide a custom encoder and decode to go
		// to/from a btcutil.Address and a string.
		AddressDecoder:  dexbch.DecodeCashAddress,
		AddressStringer: dexbch.EncodeCashAddress,
		// Bitcoin Cash has a custom signature hash algorithm. Since they don't
		// have segwit, Bitcoin Cash implemented a variation of the withdrawn
		// BIP0062 that utilizes Schnorr signatures.
		// https://gist.github.com/markblundeberg/a3aba3c9d610e59c3c49199f697bc38b#making-unmalleable-smart-contracts
		// https://github.com/bitcoin/bips/blob/master/bip-0062.mediawiki
		NonSegwitSigner: rawTxInSigner,
		OmitAddressType: true,
		// Bitcoin Cash uses estimatefee instead of estimatesmartfee, and even
		// then, they modified it from the old Bitcoin Core estimatefee by
		// removing the confirmation target argument.
		FeeEstimator: estimateFee,
	}

	switch cfg.Type {
	case walletTypeRPC, walletTypeLegacy:
		return btc.BTCCloneWallet(cloneCFG)
	// case walletTypeElectrum:
	// 	logger.Warnf("\n\nUNTESTED Bitcoin Cash ELECTRUM WALLET IMPLEMENTATION! DO NOT USE ON mainnet!\n\n")
	// 	cloneCFG.FeeEstimator = nil        // Electrum can do it, use the feeRate method
	// 	cloneCFG.LegacyBalance = false
	// 	cloneCFG.Ports = dexbtc.NetPorts{} // no default ports for Electrum wallet
	// 	return btc.ElectrumWallet(cloneCFG)
	default:
		return nil, fmt.Errorf("unknown wallet type %q", cfg.Type)
	}
}

// rawTxSigner signs the transaction using Bitcoin Cash's custom signature
// hash and signing algorithm.
func rawTxInSigner(btcTx *wire.MsgTx, idx int, subScript []byte, hashType txscript.SigHashType,
	btcKey *btcec.PrivateKey, vals []int64, _ [][]byte) ([]byte, error) {

	bchTx, err := translateTx(btcTx)
	if err != nil {
		return nil, fmt.Errorf("btc->bch wire.MsgTx translation error: %v", err)
	}

	bchKey, _ := bchec.PrivKeyFromBytes(bchec.S256(), btcKey.Serialize())

	return bchscript.RawTxInECDSASignature(bchTx, idx, subScript, bchscript.SigHashType(uint32(hashType)), bchKey, vals[idx])
}

// serializeBtcTx serializes the wire.MsgTx.
func serializeBtcTx(msgTx *wire.MsgTx) ([]byte, error) {
	buf := bytes.NewBuffer(make([]byte, 0, msgTx.SerializeSize()))
	err := msgTx.Serialize(buf)
	if err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// estimateFee uses Bitcoin Cash's estimatefee RPC, since estimatesmartfee
// is not implemented.
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

// translateTx converts the btcd/*wire.MsgTx into a bchd/*wire.MsgTx.
func translateTx(btcTx *wire.MsgTx) (*bchwire.MsgTx, error) {
	txB, err := serializeBtcTx(btcTx)
	if err != nil {
		return nil, err
	}

	bchTx := new(bchwire.MsgTx)
	err = bchTx.Deserialize(bytes.NewBuffer(txB))
	if err != nil {
		return nil, err
	}

	return bchTx, nil
}
