// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package bch

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"path/filepath"
	"time"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/client/asset/btc"
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/config"
	dexbch "decred.org/dcrdex/dex/networks/bch"
	dexbtc "decred.org/dcrdex/dex/networks/btc"
	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/btcsuite/btcd/txscript"
	"github.com/btcsuite/btcd/wire"
	"github.com/dcrlabs/bchwallet/wallet"
	"github.com/gcash/bchd/bchec"
	bchchaincfg "github.com/gcash/bchd/chaincfg"
	bchtxscript "github.com/gcash/bchd/txscript"
	bchwire "github.com/gcash/bchd/wire"
)

const (
	version = 0

	// BipID is the Bip 44 coin ID for Bitcoin Cash.
	BipID = 145
	// The default fee is passed to the user as part of the asset.WalletInfo
	// structure.
	defaultFee         = 100
	minNetworkVersion  = 270000 // v27.0.0-49ad6a9a9
	walletTypeRPC      = "bitcoindRPC"
	walletTypeSPV      = "SPV"
	walletTypeLegacy   = ""
	walletTypeElectrum = "electrumRPC"
)

var (
	netPorts = dexbtc.NetPorts{
		Mainnet: "8332",
		Testnet: "28332",
		Simnet:  "18443",
	}

	rpcWalletDefinition = &asset.WalletDefinition{
		Type:              walletTypeRPC,
		Tab:               "External",
		Description:       "Connect to bitcoind",
		DefaultConfigPath: dexbtc.SystemConfigPath("bitcoin"), // Same as bitcoin. That's dumb.
		ConfigOpts:        append(btc.RPCConfigOpts("Bitcoin Cash", ""), btc.CommonConfigOpts("BCH", true)...),
		MultiFundingOpts:  btc.MultiFundingOpts,
	}
	spvWalletDefinition = &asset.WalletDefinition{
		Type:             walletTypeSPV,
		Tab:              "Native",
		Description:      "Use the built-in SPV wallet",
		ConfigOpts:       btc.CommonConfigOpts("BCH", true),
		Seeded:           true,
		MultiFundingOpts: btc.MultiFundingOpts,
	}

	electrumWalletDefinition = &asset.WalletDefinition{
		Type:        walletTypeElectrum,
		Tab:         "Electron Cash  (external)",
		Description: "Use an external Electron Cash (BCH Electrum fork) Wallet",
		// json: DefaultConfigPath: filepath.Join(btcutil.AppDataDir("electrom-cash", false), "config"), // maybe?
		ConfigOpts:       btc.CommonConfigOpts("BCH", true),
		MultiFundingOpts: btc.MultiFundingOpts,
	}

	// WalletInfo defines some general information about a Bitcoin Cash wallet.
	WalletInfo = &asset.WalletInfo{
		Name:              "Bitcoin Cash",
		SupportedVersions: []uint32{version},
		// Same as bitcoin. That's dumb.
		UnitInfo: dexbch.UnitInfo,
		AvailableWallets: []*asset.WalletDefinition{
			// spvWalletDefinition,
			rpcWalletDefinition,
			// electrumWalletDefinition, // getinfo RPC needs backport: https://github.com/Electron-Cash/Electron-Cash/pull/2399
		},
	}

	externalFeeRate = btc.BitcoreRateFetcher("BCH")
)

func init() {
	asset.Register(BipID, &Driver{})
	asset.RegisterSPVWithdrawFunc(BipID, WithdrawSPVFunds)
}

// Driver implements asset.Driver.
type Driver struct{}

// Check that Driver implements asset.Driver.
var _ asset.Driver = (*Driver)(nil)

// Open creates the BCH exchange wallet. Start the wallet with its Run method.
func (d *Driver) Open(cfg *asset.WalletConfig, logger dex.Logger, network dex.Network) (asset.Wallet, error) {
	if cfg.Type == walletTypeSPV {
		return nil, asset.ErrWalletTypeDisabled
	}
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

// Exists checks the existence of the wallet. Part of the Creator interface, so
// only used for wallets with WalletDefinition.Seeded = true.
func (d *Driver) Exists(walletType, dataDir string, settings map[string]string, net dex.Network) (bool, error) {
	if walletType != walletTypeSPV {
		return false, fmt.Errorf("no Bitcoin Cash wallet of type %q available", walletType)
	}

	chainParams, err := parseChainParams(net)
	if err != nil {
		return false, err
	}
	walletDir := filepath.Join(dataDir, chainParams.Name)
	// recoverWindow argument borrowed from bchwallet directly.
	loader := wallet.NewLoader(chainParams, walletDir, true, 250)
	return loader.WalletExists()
}

// Create creates a new SPV wallet.
func (d *Driver) Create(params *asset.CreateWalletParams) error {
	if params.Type != walletTypeSPV {
		return fmt.Errorf("SPV is the only seeded wallet type. required = %q, requested = %q", walletTypeSPV, params.Type)
	}
	if len(params.Seed) == 0 {
		return errors.New("wallet seed cannot be empty")
	}
	if len(params.DataDir) == 0 {
		return errors.New("must specify wallet data directory")
	}
	chainParams, err := parseChainParams(params.Net)
	if err != nil {
		return fmt.Errorf("error parsing chain: %w", err)
	}

	walletCfg := new(btc.WalletConfig)
	err = config.Unmapify(params.Settings, walletCfg)
	if err != nil {
		return err
	}

	recoveryCfg := new(btc.RecoveryCfg)
	err = config.Unmapify(params.Settings, recoveryCfg)
	if err != nil {
		return err
	}

	bday := btc.DefaultWalletBirthday
	if params.Birthday != 0 {
		bday = time.Unix(int64(params.Birthday), 0)
	}

	walletDir := filepath.Join(params.DataDir, chainParams.Name)
	return createSPVWallet(params.Pass, params.Seed, bday, walletDir,
		params.Logger, recoveryCfg.NumExternalAddresses, recoveryCfg.NumInternalAddresses, chainParams)
}

// MinLotSize calculates the minimum bond size for a given fee rate that avoids
// dust outputs on the swap and refund txs, assuming the maxFeeRate doesn't
// change.
func (d *Driver) MinLotSize(maxFeeRate uint64) uint64 {
	return dexbtc.MinLotSize(maxFeeRate, false)
}

// NewWallet is the exported constructor by which the DEX will import the
// exchange wallet.
func NewWallet(cfg *asset.WalletConfig, logger dex.Logger, network dex.Network) (asset.Wallet, error) {
	cloneParams := parseCloneParams(network)
	if cloneParams == nil {
		return nil, fmt.Errorf("unknown network ID %v", network)
	}

	// Designate the clone ports. These will be overwritten by any explicit
	// settings in the configuration file. Bitcoin Cash uses the same default
	// ports as Bitcoin.
	cloneCFG := &btc.BTCCloneCFG{
		WalletCFG:            cfg,
		MinNetworkVersion:    minNetworkVersion,
		WalletInfo:           WalletInfo,
		Symbol:               "bch",
		Logger:               logger,
		Network:              network,
		ChainParams:          cloneParams,
		Ports:                netPorts,
		DefaultFallbackFee:   defaultFee,
		Segwit:               false,
		InitTxSizeBase:       dexbtc.InitTxSizeBase,
		InitTxSize:           dexbtc.InitTxSize,
		ExternalFeeEstimator: externalFeeRate,
		LegacyBalance:        cfg.Type != walletTypeSPV,
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
		// Bitcoin Cash don't take a change_type argument in their options
		// unlike Bitcoin Core.
		OmitAddressType: true,
		AssetID:         BipID,
	}

	switch cfg.Type {
	case walletTypeRPC, walletTypeLegacy:
		// Bitcoin Cash uses estimatefee instead of estimatesmartfee, and even
		// then, they modified it from the old Bitcoin Core estimatefee by
		// removing the confirmation target argument.
		cloneCFG.FeeEstimator = estimateFee
		return btc.BTCCloneWallet(cloneCFG)
	// case walletTypeElectrum:
	// 	logger.Warnf("\n\nUNTESTED Bitcoin Cash ELECTRUM WALLET IMPLEMENTATION! DO NOT USE ON mainnet!\n\n")
	// 	cloneCFG.FeeEstimator = nil        // Electrum can do it, use the feeRate method
	// 	cloneCFG.LegacyBalance = false
	// 	cloneCFG.Ports = dexbtc.NetPorts{} // no default ports for Electrum wallet
	// 	return btc.ElectrumWallet(cloneCFG)
	case walletTypeSPV:
		return btc.OpenSPVWallet(cloneCFG, openSPVWallet)
	}
	return nil, fmt.Errorf("wallet type %q not known", cfg.Type)
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

	return bchtxscript.RawTxInECDSASignature(bchTx, idx, subScript, bchtxscript.SigHashType(uint32(hashType)), bchKey, vals[idx])
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
func estimateFee(ctx context.Context, node btc.RawRequester, confTarget uint64) (uint64, error) {
	resp, err := node.RawRequest(ctx, "estimatefee", nil)
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

func parseCloneParams(net dex.Network) *chaincfg.Params {
	switch net {
	case dex.Mainnet:
		return dexbch.MainNetParams
	case dex.Testnet:
		return dexbch.TestNet4Params
	case dex.Regtest:
		return dexbch.RegressionNetParams
	}
	return nil
}

func parseChainParams(net dex.Network) (*bchchaincfg.Params, error) {
	switch net {
	case dex.Mainnet:
		return &bchchaincfg.MainNetParams, nil
	case dex.Testnet:
		return &bchchaincfg.TestNet4Params, nil
	case dex.Regtest:
		return &bchchaincfg.RegressionNetParams, nil
	}
	return nil, fmt.Errorf("unknown network ID %v", net)
}

// WithdrawSPVFunds is a function to generate a tx that spends all funds from a
// deprecated SPV wallet.
func WithdrawSPVFunds(ctx context.Context, walletPW []byte, recipient, dataDir string, net dex.Network, log dex.Logger) ([]byte, error) {
	cloneParams := parseCloneParams(net)
	if cloneParams == nil {
		return nil, fmt.Errorf("unknown net %v", net)
	}
	addr, err := dexbch.DecodeCashAddress(recipient, cloneParams)
	if err != nil {
		return nil, fmt.Errorf("error decoding address %q: %w", recipient, err)
	}
	c := make(chan asset.WalletNotification, 16)
	cfg := &asset.WalletConfig{
		Type:        walletTypeSPV,
		Emit:        asset.NewWalletEmitter(c, BipID, log),
		PeersChange: func(u uint32, err error) {},
		DataDir:     dataDir,
		Settings: map[string]string{
			"apifeefallback": "true",
			"fallbackfee":    "0.001", // = defaultFee in BCH/kB
		},
	}
	wi, err := NewWallet(cfg, log, net)
	if err != nil {
		return nil, fmt.Errorf("error constructing wallet: %w", err)
	}
	w := wi.(*btc.ExchangeWalletSPV)

	btcTx, err := w.WithdrawTx(ctx, walletPW, addr)
	if err != nil {
		return nil, fmt.Errorf("error generating withdraw tx: %w", err)
	}

	bchTx, err := translateTx(btcTx)
	if err != nil {
		return nil, fmt.Errorf("btc->bch wire.MsgTx translation error: %v", err)
	}

	buf := bytes.NewBuffer(make([]byte, 0, bchTx.SerializeSize()))
	if err = bchTx.Serialize(buf); err != nil {
		return nil, fmt.Errorf("error serializing tx: %w", err)
	}
	return buf.Bytes(), nil
}
