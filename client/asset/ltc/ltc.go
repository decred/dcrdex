// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package ltc

import (
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/client/asset/btc"
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/config"
	dexbtc "decred.org/dcrdex/dex/networks/btc"
	dexltc "decred.org/dcrdex/dex/networks/ltc"
	"github.com/btcsuite/btcd/chaincfg"
	ltcchaincfg "github.com/ltcsuite/ltcd/chaincfg"
	"github.com/ltcsuite/ltcwallet/wallet"
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
	commonOpts = []*asset.ConfigOption{
		{
			Key:         "rpcuser",
			DisplayName: "JSON-RPC Username",
			Description: "Litecoin's 'rpcuser' setting",
		},
		{
			Key:         "rpcpassword",
			DisplayName: "JSON-RPC Password",
			Description: "Litecoin's 'rpcpassword' setting",
			NoEcho:      true,
		},
		{
			Key:          "rpcbind",
			DisplayName:  "JSON-RPC Address",
			Description:  "<addr> or <addr>:<port> (default 'localhost')",
			DefaultValue: "127.0.0.1",
		},
		{
			Key:          "rpcport",
			DisplayName:  "JSON-RPC Port",
			Description:  "Port for RPC connections (if not set in rpcbind)",
			DefaultValue: "9332",
		},
		{
			Key:          "fallbackfee",
			DisplayName:  "Fallback fee rate",
			Description:  "Litecoin's 'fallbackfee' rate. Units: LTC/kB",
			DefaultValue: defaultFee * 1000 / 1e8,
		},
		{
			Key:         "feeratelimit",
			DisplayName: "Highest acceptable fee rate",
			Description: "This is the highest network fee rate you are willing to " +
				"pay on swap transactions. If feeratelimit is lower than a market's " +
				"maxfeerate, you will not be able to trade on that market with this " +
				"wallet.  Units: LTC/kB",
			DefaultValue: defaultFeeRateLimit * 1000 / 1e8,
		},
		{
			Key:          "redeemconftarget",
			DisplayName:  "Redeem transaction confirmation target",
			Description:  "The target number of blocks for the redeem transaction to get a confirmation. Used to set the transaction's fee rate. (default: 2 blocks)",
			DefaultValue: 2,
		},
		{
			Key:         "txsplit",
			DisplayName: "Pre-size funding inputs",
			Description: "When placing an order, create a \"split\" transaction to fund the order without locking more of the wallet balance than " +
				"necessary. Otherwise, excess funds may be reserved to fund the order until the first swap contract is broadcast " +
				"during match settlement, or the order is canceled. This an extra transaction for which network mining fees are paid. " +
				"Used only for standing-type orders, e.g. limit orders without immediate time-in-force.",
			IsBoolean: true,
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
		ConfigOpts: btc.CommonConfigOpts("LTC", false),
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
		Name:     "Litecoin",
		Version:  version,
		UnitInfo: dexltc.UnitInfo,
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
	netDir := filepath.Join(dataDir, chainParams.Name, "spv")
	loader := wallet.NewLoader(chainParams, netDir, true, 60*time.Second, 250)
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

	return createSPVWallet(params.Pass, params.Seed, walletCfg.AdjustedBirthday(), params.DataDir,
		params.Logger, recoveryCfg.NumExternalAddresses, recoveryCfg.NumInternalAddresses, chainParams)
}

// NewWallet is the exported constructor by which the DEX will import the
// exchange wallet.
func NewWallet(cfg *asset.WalletConfig, logger dex.Logger, network dex.Network) (asset.Wallet, error) {
	var cloneParams *chaincfg.Params
	var ltcParams *ltcchaincfg.Params
	switch network {
	case dex.Mainnet:
		cloneParams = dexltc.MainNetParams
		ltcParams = &ltcchaincfg.MainNetParams
	case dex.Testnet:
		cloneParams = dexltc.TestNet4Params
		ltcParams = &ltcchaincfg.TestNet4Params
	case dex.Regtest:
		cloneParams = dexltc.RegressionNetParams
		ltcParams = &ltcchaincfg.RegressionNetParams
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
		BlockDeserializer:   dexltc.DeserializeBlockBytes,
	}

	switch cfg.Type {
	case walletTypeRPC, walletTypeLegacy:
		return btc.BTCCloneWallet(cloneCFG)
	case walletTypeSPV:
		return btc.OpenSPVWallet(cloneCFG, func(dir string, cfg *btc.WalletConfig, btcParams *chaincfg.Params, log dex.Logger) btc.BTCWallet {
			return openSPVWallet(dir, cfg, btcParams, ltcParams, log)
		})
	case walletTypeElectrum:
		cloneCFG.Ports = dexbtc.NetPorts{} // no default ports
		return btc.ElectrumWallet(cloneCFG)
	default:
		return nil, fmt.Errorf("unknown wallet type %q", cfg.Type)
	}
}
