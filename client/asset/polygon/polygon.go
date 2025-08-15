// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package polygon

import (
	"fmt"
	"os/user"
	"path/filepath"
	"strconv"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/client/asset/eth"
	"decred.org/dcrdex/dex"
	dexeth "decred.org/dcrdex/dex/networks/eth"
	dexpolygon "decred.org/dcrdex/dex/networks/polygon"
	"github.com/ethereum/go-ethereum/common"
)

func init() {
	dexpolygon.MaybeReadSimnetAddrs()
}

func registerToken(tokenID uint32, desc string, nets ...dex.Network) {
	token, found := dexpolygon.Tokens[tokenID]
	if !found {
		panic("token " + strconv.Itoa(int(tokenID)) + " not known")
	}
	netAddrs := make(map[dex.Network]string)
	netVersions := make(map[dex.Network][]uint32, 3)
	for net, netToken := range token.NetTokens {
		netAddrs[net] = netToken.Address.String()
		netVersions[net] = make([]uint32, 0, 1)
		for ver := range netToken.SwapContracts {
			netVersions[net] = append(netVersions[net], ver)
		}
	}
	asset.RegisterToken(tokenID, token.Token, &asset.WalletDefinition{
		Type:        walletTypeToken,
		Tab:         "Polygon token",
		Description: desc,
	}, netAddrs, netVersions)
}

func init() {
	asset.Register(BipID, &Driver{})
	registerToken(usdcTokenID, "The USDC Ethereum ERC20 token.", dex.Mainnet, dex.Testnet, dex.Simnet)
	registerToken(usdtTokenID, "The USDT Ethereum ERC20 token.", dex.Mainnet, dex.Testnet, dex.Simnet)
	registerToken(wbtcTokenID, "Wrapped BTC.", dex.Mainnet)
	registerToken(wethTokenID, "Wrapped ETH.", dex.Mainnet)
}

const (
	// BipID is the BIP-0044 asset ID for Polygon.
	BipID              = 966
	defaultGasFeeLimit = 1000
	walletTypeRPC      = "rpc"
	walletTypeToken    = "token"
)

var (
	usdcTokenID, _ = dex.BipSymbolID("usdc.polygon")
	usdtTokenID, _ = dex.BipSymbolID("usdt.polygon")
	wethTokenID, _ = dex.BipSymbolID("weth.polygon")
	wbtcTokenID, _ = dex.BipSymbolID("wbtc.polygon")
	// WalletInfo defines some general information about a Polygon Wallet(EVM
	// Compatible).

	walletOpts = []*asset.ConfigOption{
		{
			Key:         "gasfeelimit",
			DisplayName: "Gas Fee Limit",
			Description: "This is the highest network fee rate you are willing to " +
				"pay on swap transactions. If gasfeelimit is lower than a market's " +
				"maxfeerate, you will not be able to trade on that market with this " +
				"wallet.  Units: gwei / gas",
			DefaultValue: strconv.FormatUint(defaultGasFeeLimit, 10),
		},
	}
	WalletInfo = asset.WalletInfo{
		Name:              "Polygon",
		SupportedVersions: []uint32{0, 1},
		UnitInfo:          dexpolygon.UnitInfo,
		AvailableWallets: []*asset.WalletDefinition{
			{
				Type:        walletTypeRPC,
				Tab:         "External",
				Description: "Infrastructure providers (e.g. Infura) or local nodes",
				ConfigOpts:  append(eth.RPCOpts, walletOpts...),
				Seeded:      true,
				NoAuth:      true,
			},
		},
		IsAccountBased: true,
	}
)

type Driver struct{}

// Open opens the Polygon exchange wallet. Start the wallet with its Run method.
func (d *Driver) Open(cfg *asset.WalletConfig, logger dex.Logger, net dex.Network) (asset.Wallet, error) {
	chainCfg, err := ChainConfig(net)
	if err != nil {
		return nil, fmt.Errorf("failed to locate Polygon genesis configuration for network %s", net)
	}
	compat, err := NetworkCompatibilityData(net)
	if err != nil {
		return nil, fmt.Errorf("failed to locate Polygon compatibility data: %s", net)
	}
	contracts := make(map[uint32]common.Address, 1)
	for ver, netAddrs := range dexpolygon.ContractAddresses {
		for netw, addr := range netAddrs {
			if netw == net {
				contracts[ver] = addr
				break
			}
		}
	}

	var defaultProviders []string
	switch net {
	case dex.Simnet:
		u, _ := user.Current()
		defaultProviders = []string{filepath.Join(u.HomeDir, "dextest", "polygon", "alpha", "bor", "bor.ipc")}
	case dex.Testnet:
		defaultProviders = []string{
			"https://rpc-amoy.polygon.technology",
			"wss://polygon-amoy-bor-rpc.publicnode.com",
			"https://polygon-amoy.blockpi.network/v1/rpc/public",
		}
	case dex.Mainnet:
		defaultProviders = []string{
			"https://1rpc.io/matic",
			"https://rpc.ankr.com/polygon",
			"https://polygon-mainnet.public.blastapi.io",
			"https://polygon.blockpi.network/v1/rpc/public",
			"https://polygon.llamarpc.com",
			"https://endpoints.omniatech.io/v1/matic/mainnet/public",
			"https://rpc-mainnet.matic.quiknode.pro",
			"https://gateway.tenderly.co/public/polygon",
		}
	}

	// BipID, chainCfg, cfg, &t, dexpolygon.VersionedGases, dexpolygon.Tokens, logger, net
	evmWallet, err := eth.NewEVMWallet(&eth.EVMWalletConfig{
		BaseChainID:        BipID,
		ChainCfg:           chainCfg,
		AssetCfg:           cfg,
		CompatData:         &compat,
		VersionedGases:     dexpolygon.VersionedGases,
		Tokens:             dexpolygon.Tokens,
		FinalizeConfs:      64,
		Logger:             logger,
		BaseChainContracts: contracts,
		MultiBalAddress:    dexpolygon.MultiBalanceAddresses[net],
		WalletInfo:         WalletInfo,
		Net:                net,
		DefaultProviders:   defaultProviders,
		MaxTxFeeGwei:       1000 * dexeth.GweiFactor, // 1000 POL
	})
	if err != nil {
		return nil, err
	}

	return evmWallet, nil
}

func (d *Driver) DecodeCoinID(coinID []byte) (string, error) {
	return (&eth.Driver{}).DecodeCoinID(coinID)
}

func (d *Driver) Info() *asset.WalletInfo {
	wi := WalletInfo
	return &wi
}

func (d *Driver) Exists(walletType, dataDir string, settings map[string]string, net dex.Network) (bool, error) {
	if walletType != walletTypeRPC {
		return false, fmt.Errorf("unknown wallet type %q", walletType)
	}
	return (&eth.Driver{}).Exists(walletType, dataDir, settings, net)
}

func (d *Driver) Create(cfg *asset.CreateWalletParams) error {
	compat, err := NetworkCompatibilityData(cfg.Net)
	if err != nil {
		return fmt.Errorf("error finding compatibility data: %v", err)
	}
	return eth.CreateEVMWallet(dexpolygon.ChainIDs[cfg.Net], cfg, &compat, false)
}
