// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package base

import (
	"errors"
	"fmt"
	"strconv"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/client/asset/eth"
	"decred.org/dcrdex/dex"
	dexbase "decred.org/dcrdex/dex/networks/base"
	dexeth "decred.org/dcrdex/dex/networks/eth"
	"github.com/ethereum/go-ethereum/common"
)

func registerToken(tokenID uint32, desc string, nets ...dex.Network) {
	token, found := dexbase.Tokens[tokenID]
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
		Tab:         "Base token",
		Description: desc,
	}, netAddrs, netVersions)
}

func init() {
	asset.Register(BipID, &Driver{})
	registerToken(usdcTokenID, "The USDC Base ERC20 token.", dex.Mainnet, dex.Testnet)
}

const (
	// BipID is our custom BIP-0044 asset ID for Base weth.
	BipID              = 8453
	defaultGasFeeLimit = 1000
	walletTypeRPC      = "rpc"
	walletTypeToken    = "token"
)

var (
	usdcTokenID, _ = dex.BipSymbolID("usdc.base")
	// WalletInfo defines some general information about a Base Wallet(EVM
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
		Name:              "Base",
		SupportedVersions: []uint32{},
		UnitInfo:          dexbase.UnitInfo,
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
		BlockchainClass: asset.BlockchainClassEVM,
	}
)

type Driver struct{}

// Open opens the Polygon exchange wallet. Start the wallet with its Run method.
func (d *Driver) Open(cfg *asset.WalletConfig, logger dex.Logger, net dex.Network) (asset.Wallet, error) {
	if net == dex.Simnet {
		return nil, errors.New("simnet not implemented yet")
	}
	chainCfg, err := ChainConfig(net)
	if err != nil {
		return nil, fmt.Errorf("failed to locate Base genesis configuration for network %s", net)
	}
	compat, err := NetworkCompatibilityData(net)
	if err != nil {
		return nil, fmt.Errorf("failed to locate Base compatibility data: %s", net)
	}
	contracts := make(map[uint32]common.Address, 1)
	for ver, netAddrs := range dexbase.ContractAddresses {
		for netw, addr := range netAddrs {
			if netw == net {
				contracts[ver] = addr
				break
			}
		}
	}

	var defaultProviders []string
	switch net {
	case dex.Testnet:
		defaultProviders = []string{
			"https://base-sepolia-rpc.publicnode.com",
			"https://sepolia.base.org",
			"https://base-sepolia.drpc.org",
			"https://base-sepolia.api.onfinality.io/public", // heavily rate limited, mainnet seems fine
			"https://base-sepolia.gateway.tenderly.co",
		}
	case dex.Mainnet:
		defaultProviders = []string{
			"https://base-rpc.publicnode.com", // not for production use
			"https://mainnet.base.org",
			"https://base.drpc.org",
			"https://base.llamarpc.com",
			"https://base.api.onfinality.io/public",
		}
	}

	return eth.NewEVMWallet(&eth.EVMWalletConfig{
		BaseChainID:        BipID,
		ChainCfg:           chainCfg,
		AssetCfg:           cfg,
		CompatData:         &compat,
		VersionedGases:     dexbase.VersionedGases,
		Tokens:             dexbase.Tokens,
		FinalizeConfs:      3,
		Logger:             logger,
		BaseChainContracts: contracts,
		WalletInfo:         WalletInfo,
		Net:                net,
		DefaultProviders:   defaultProviders,
		MaxTxFeeGwei:       dexeth.GweiFactor, // 1 ETH
	})
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
	return eth.CreateEVMWallet(dexbase.ChainIDs[cfg.Net], cfg, &compat, false)
}
