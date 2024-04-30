// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package ltc

import (
	"errors"
	"fmt"
	"path/filepath"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/client/asset/btc"
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/config"
	dexbtc "decred.org/dcrdex/dex/networks/btc"
	dexltc "decred.org/dcrdex/dex/networks/ltc"
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/dcrlabs/ltcwallet/wallet"
	ltcchaincfg "github.com/ltcsuite/ltcd/chaincfg"
)

const (
	version = 1
	// BipID is the BIP-0044 asset ID.
	BipID = 2
	// defaultFee is the default value for the fallbackfee.
	defaultFee = 10
	// defaultFeeRateLimit is the default value for the feeratelimit.
	defaultFeeRateLimit = 100
	minNetworkVersion   = 210201
	walletTypeRPC       = "litecoindRPC"
	walletTypeSPV       = "SPV"
	walletTypeLegacy    = ""
	walletTypeElectrum  = "electrumRPC"
)

var (
	NetPorts = dexbtc.NetPorts{
		Mainnet: "9332",
		Testnet: "19332",
		Simnet:  "19443",
	}
	walletNameOpt = []*asset.ConfigOption{ // slice for easy appends
		{
			Key:         "walletname",
			DisplayName: "Wallet Name",
			Description: "The wallet name",
		},
	}
	rpcWalletDefinition = &asset.WalletDefinition{
		Type:              walletTypeRPC,
		Tab:               "Litecoin Core (external)",
		Description:       "Connect to litecoind",
		DefaultConfigPath: dexbtc.SystemConfigPath("litecoin"),
		ConfigOpts:        append(btc.RPCConfigOpts("Litecoin", "9332"), btc.CommonConfigOpts("LTC", false)...),
	}
	electrumWalletDefinition = &asset.WalletDefinition{
		Type:        walletTypeElectrum,
		Tab:         "Electrum-LTC (external)",
		Description: "Use an external Electrum-LTC Wallet",
		// json: DefaultConfigPath: filepath.Join(btcutil.AppDataDir("electrum-ltc", false), "config"), // e.g. ~/.electrum-ltc/config		ConfigOpts:        append(rpcOpts, commonOpts...),
		ConfigOpts: append(btc.ElectrumConfigOpts, btc.CommonConfigOpts("LTC", false)...),
	}
	spvWalletDefinition = &asset.WalletDefinition{
		Type:        walletTypeSPV,
		Tab:         "Native",
		Description: "Use the built-in SPV wallet",
		ConfigOpts:  append(btc.SPVConfigOpts("LTC"), btc.CommonConfigOpts("LTC", false)...),
		Seeded:      true,
	}
	// WalletInfo defines some general information about a Litecoin wallet.
	WalletInfo = &asset.WalletInfo{
		Name:              "Litecoin",
		Version:           version,
		SupportedVersions: []uint32{version},
		UnitInfo:          dexltc.UnitInfo,
		AvailableWallets: []*asset.WalletDefinition{
			spvWalletDefinition,
			rpcWalletDefinition,
			electrumWalletDefinition,
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

// MinLotSize calculates the minimum bond size for a given fee rate that avoids
// dust outputs on the swap and refund txs, assuming the maxFeeRate doesn't
// change.
func (d *Driver) MinLotSize(maxFeeRate uint64) uint64 {
	return dexbtc.MinLotSize(maxFeeRate, true)
}

// Exists checks the existence of the wallet. Part of the Creator interface, so
// only used for wallets with WalletDefinition.Seeded = true.
func (d *Driver) Exists(walletType, dataDir string, settings map[string]string, net dex.Network) (bool, error) {
	if walletType != walletTypeSPV {
		return false, fmt.Errorf("no Bitcoin wallet of type %q available", walletType)
	}

	chainParams, err := parseChainParams(net)
	if err != nil {
		return false, err
	}
	walletDir := filepath.Join(dataDir, chainParams.Name)
	loader := wallet.NewLoader(chainParams, walletDir, true, dbTimeout, 250)
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

	walletDir := filepath.Join(params.DataDir, chainParams.Name)
	return createSPVWallet(params.Pass, params.Seed, walletCfg.AdjustedBirthday(), walletDir,
		params.Logger, recoveryCfg.NumExternalAddresses, recoveryCfg.NumInternalAddresses, chainParams)
}

// customSPVWalletConstructors are functions for setting up custom
// implementations of the btc.BTCWallet interface that may be used by the
// ExchangeWalletSPV instead of the default spv implementation.
var customSPVWalletConstructors = map[string]btc.CustomSPVWalletConstructor{}

// RegisterCustomSPVWallet registers a function that should be used in creating
// a btc.BTCWallet implementation that the ExchangeWalletSPV will use in place
// of the default spv wallet implementation. External consumers can use this
// function to provide alternative btc.BTCWallet implementations, and must do so
// before attempting to create an ExchangeWalletSPV instance of this type. It'll
// panic if callers try to register a wallet twice.
func RegisterCustomSPVWallet(constructor btc.CustomSPVWalletConstructor, def *asset.WalletDefinition) {
	for _, availableWallets := range WalletInfo.AvailableWallets {
		if def.Type == availableWallets.Type {
			panic(fmt.Sprintf("wallet type (%q) is already registered", def.Type))
		}
	}
	customSPVWalletConstructors[def.Type] = constructor
	WalletInfo.AvailableWallets = append(WalletInfo.AvailableWallets, def)
}

// NewWallet is the exported constructor by which the DEX will import the
// exchange wallet.
func NewWallet(cfg *asset.WalletConfig, logger dex.Logger, network dex.Network) (asset.Wallet, error) {
	var cloneParams *chaincfg.Params
	switch network {
	case dex.Mainnet:
		cloneParams = dexltc.MainNetParams
	case dex.Testnet:
		cloneParams = dexltc.TestNet4Params
	case dex.Regtest:
		cloneParams = dexltc.RegressionNetParams
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
		ChainParams:         cloneParams,
		Ports:               NetPorts,
		DefaultFallbackFee:  defaultFee,
		DefaultFeeRateLimit: defaultFeeRateLimit,
		LegacyBalance:       false,
		LegacyRawFeeLimit:   false,
		Segwit:              true,
		InitTxSize:          dexbtc.InitTxSizeSegwit,
		InitTxSizeBase:      dexbtc.InitTxSizeBaseSegwit,
		BlockDeserializer:   dexltc.DeserializeBlockBytes,
		AssetID:             BipID,
	}

	switch cfg.Type {
	case walletTypeRPC, walletTypeLegacy:
		return btc.BTCCloneWallet(cloneCFG)
	case walletTypeSPV:
		return btc.OpenSPVWallet(cloneCFG, openSPVWallet)
	case walletTypeElectrum:
		cloneCFG.Ports = dexbtc.NetPorts{} // no default ports
		return btc.ElectrumWallet(cloneCFG)
	default:
		makeCustomWallet, ok := customSPVWalletConstructors[cfg.Type]
		if !ok {
			return nil, fmt.Errorf("unknown wallet type %q", cfg.Type)
		}

		// Create custom wallet first and return early if we encounter any
		// error.
		ltcWallet, err := makeCustomWallet(cfg.Settings, cloneCFG.ChainParams)
		if err != nil {
			return nil, fmt.Errorf("custom wallet setup error: %v", err)
		}

		walletConstructor := func(_ string, _ *btc.WalletConfig, _ *chaincfg.Params, _ dex.Logger) btc.BTCWallet {
			return ltcWallet
		}
		return btc.OpenSPVWallet(cloneCFG, walletConstructor)
	}
}

func parseChainParams(net dex.Network) (*ltcchaincfg.Params, error) {
	switch net {
	case dex.Mainnet:
		return &ltcchaincfg.MainNetParams, nil
	case dex.Testnet:
		return &ltcchaincfg.TestNet4Params, nil
	case dex.Regtest:
		return &ltcchaincfg.RegressionNetParams, nil
	}
	return nil, fmt.Errorf("unknown network ID %v", net)
}
