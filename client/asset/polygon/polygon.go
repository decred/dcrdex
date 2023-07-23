// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package polygon

import (
	"fmt"
	"strconv"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/client/asset/eth"
	"decred.org/dcrdex/dex"
	dexpolygon "decred.org/dcrdex/dex/networks/polygon"
	"github.com/ethereum/go-ethereum/common"
)

func registerToken(tokenID uint32, desc string, nets ...dex.Network) {
	token, found := dexpolygon.Tokens[tokenID]
	if !found {
		panic("token " + strconv.Itoa(int(tokenID)) + " not known")
	}
	asset.RegisterToken(tokenID, token.Token, &asset.WalletDefinition{
		Type:        walletTypeToken,
		Tab:         "Polygon token",
		Description: desc,
	}, nets...)
}

func init() {
	asset.Register(BipID, &Driver{})
	registerToken(simnetTokenID, "A token wallet for the DEX test token. Used for testing DEX software.", dex.Simnet)
	registerToken(usdcTokenID, "The USDC Ethereum ERC20 token.", dex.Mainnet, dex.Testnet)
}

const (
	// BipID is the BIP-0044 asset ID for Polygon.
	BipID           = 966
	walletTypeRPC   = "rpc"
	walletTypeToken = "token"
	minerGasCeil    = 8_000_000 // config.Defaults.Miner.GasCeil
)

var (
	simnetTokenID, _ = dex.BipSymbolID("dextt.polygon")
	usdcTokenID, _   = dex.BipSymbolID("usdc.polygon")
	// WalletInfo defines some general information about a Polygon Wallet(EVM
	// Compatible).
	WalletInfo = &asset.WalletInfo{
		Name:              "Polygon",
		Version:           0,
		SupportedVersions: []uint32{0},
		UnitInfo:          dexpolygon.UnitInfo,
		AvailableWallets: []*asset.WalletDefinition{
			// {
			// 	Type:        walletTypeGeth,
			// 	Tab:         "Native",
			// 	Description: "Use the built-in DEX wallet (geth light node)",
			// 	ConfigOpts:  WalletOpts,
			// 	Seeded:      true,
			// },
			{
				Type:        walletTypeRPC,
				Tab:         "External",
				Description: "Infrastructure providers (e.g. Infura) or local nodes",
				ConfigOpts:  append(eth.RPCOpts, eth.WalletOpts...),
				Seeded:      true,
				NoAuth:      true,
			},
			// MaxSwapsInTx and MaxRedeemsInTx are set in (Wallet).Info, since
			// the value cannot be known until we connect and get network info.
		},
	}
)

type Driver struct{}

// Open opens the Polygon exchange wallet. Start the wallet with its Run method.
func (d *Driver) Open(cfg *asset.WalletConfig, logger dex.Logger, net dex.Network) (asset.Wallet, error) {
	chainCfg, err := ChainGenesis(net)
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
	wi := *WalletInfo
	// BipID, chainCfg, cfg, &t, dexpolygon.VersionedGases, dexpolygon.Tokens, logger, net
	return eth.NewEVMWallet(&eth.EVMWalletConfig{
		BaseChainID:        BipID,
		ChainCfg:           chainCfg,
		AssetCfg:           cfg,
		CompatData:         &compat,
		VersionedGases:     dexpolygon.VersionedGases,
		Tokens:             dexpolygon.Tokens,
		Logger:             logger,
		BaseChainContracts: contracts,
		MinerBlockGasCeil:  minerGasCeil,
		WalletInfo:         &wi,
		Net:                net,
	})
}

func (d *Driver) DecodeCoinID(coinID []byte) (string, error) {
	return (&eth.Driver{}).DecodeCoinID(coinID)
}

func (d *Driver) Info() *asset.WalletInfo {
	return WalletInfo
}

func (d *Driver) Exists(walletType, dataDir string, settings map[string]string, net dex.Network) (bool, error) {
	if walletType != walletTypeRPC {
		return false, fmt.Errorf("unknown wallet type %q", walletType)
	}
	return (&eth.Driver{}).Exists(walletType, dataDir, settings, net)
}

func (d *Driver) Create(cfg *asset.CreateWalletParams) error {
	t, err := NetworkCompatibilityData(cfg.Net)
	if err != nil {
		return fmt.Errorf("error finding compatibility data: %v", err)
	}
	return eth.CreateEVMWallet(dexpolygon.ChainIDs[cfg.Net], cfg, &t, false)
}
