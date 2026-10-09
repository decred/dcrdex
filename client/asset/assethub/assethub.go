// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

// Package assethub is the client wallet for KSM and its tokens on Kusama
// Asset Hub, through pallet-revive's Ethereum RPC. It is the eth wallet with
// Asset Hub's parameters, the way the Base and Polygon wallets are.
package assethub

import (
	"fmt"
	"math/big"
	"slices"
	"strconv"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/client/asset/eth"
	"decred.org/dcrdex/dex"
	dexah "decred.org/dcrdex/dex/networks/assethub"
	dexeth "decred.org/dcrdex/dex/networks/eth"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/params"
)

const (
	BipID           = dexah.KsmBipID
	walletTypeRPC   = "rpc"
	walletTypeToken = "token"
	// revive's base fee has sat at a flat 1,000 gwei on Polkadot Hub TestNet
	// and 10,000 gwei on Kusama Asset Hub (2026-10-09). Leave room above it.
	defaultGasFeeLimit = 25_000
)

var (
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
		Name:              "Kusama Asset Hub",
		SupportedVersions: []uint32{1},
		UnitInfo:          dexah.UnitInfo,
		AvailableWallets: []*asset.WalletDefinition{
			{
				Type:        walletTypeRPC,
				Tab:         "External",
				Description: "Asset Hub's Ethereum RPC (pallet-revive eth-rpc)",
				ConfigOpts:  append(eth.RPCOpts, walletOpts...),
				Seeded:      true,
				NoAuth:      true,
			},
		},
		BlockchainClass: asset.BlockchainClassEVM,
	}

	// Compatibility data: things the wallet asks every provider for before it
	// trusts it.
	mainnetCompatibilityData = eth.CompatibilityData{
		Addr:      common.HexToAddress("0x49b850c6552bd0884a0a9071dd449e3826debf64"),
		TokenAddr: common.HexToAddress("0x02faf08200000000000000000000000001200000"), // dUSD
		TxHash:    common.HexToHash("0x51fd4d64d5656833591da501d057a7be07c3509edc4e3b6d6ff95d7886413b1f"),
		BlockHash: common.HexToHash("0xe2ce846fc9683f908325afc1da2a5fb41717536c675ff0fc50af1ab3be528409"),
	}
	testnetCompatibilityData = eth.CompatibilityData{
		Addr:      common.HexToAddress("0x216Db17653C22dCb9a4D387B1e45f57102A30cA7"),
		TokenAddr: common.HexToAddress("0x010ca163c417fabf7223f23a1ba7aeaca8ca571e"), // mock dUSD
		TxHash:    common.HexToHash("0x85d49c9e7d0e19a4dcb39c4f3bd4d2343b3cfcc83f64c969924f95699eb2b75c"),
		BlockHash: common.HexToHash("0xd1283f0c9b42ecf59f05aced17a5e19906b09059a4917a33913ff86c363f30cc"),
	}
)

func registerToken(tokenID uint32, desc string, allowedNets ...dex.Network) {
	token, found := dexah.Tokens[tokenID]
	if !found {
		panic("token " + strconv.Itoa(int(tokenID)) + " not known")
	}
	netAddrs := make(map[dex.Network]string)
	netVersions := make(map[dex.Network][]uint32, 3)
	for net, netToken := range token.NetTokens {
		if !slices.Contains(allowedNets, net) {
			continue
		}
		netAddrs[net] = netToken.Address.String()
		for ver := range netToken.SwapContracts {
			netVersions[net] = append(netVersions[net], ver)
		}
	}
	asset.RegisterToken(tokenID, token.Token, &asset.WalletDefinition{
		Type:        walletTypeToken,
		Tab:         "Asset Hub token",
		Description: desc,
	}, netAddrs, netVersions)
}

func init() {
	asset.Register(BipID, &Driver{})
	registerToken(dexah.DusdTokenID, "dUSD, a dollar stablecoin on Kusama Asset Hub.", dex.Mainnet, dex.Testnet)
}

func NetworkCompatibilityData(net dex.Network) (eth.CompatibilityData, error) {
	switch net {
	case dex.Mainnet:
		return mainnetCompatibilityData, nil
	case dex.Testnet:
		return testnetCompatibilityData, nil
	}
	return eth.CompatibilityData{}, fmt.Errorf("no compatibility data for network %s", net)
}

func ChainConfig(net dex.Network) (*params.ChainConfig, error) {
	id, ok := dexah.ChainIDs[net]
	if !ok {
		return nil, fmt.Errorf("no chain id for network %s", net)
	}
	// The wallet predicts the next base fee with go-ethereum's EIP-1559
	// formula, which returns Ethereum's 1 gwei starting fee unless London is
	// active. revive's base fee is a flat 1,000 gwei, so without this every
	// transaction is capped below it and refused as "Invalid Transaction".
	// With it, the formula reads the header's fee (it never fills a block, so
	// it predicts 7/8 of it) and the cap of twice that clears.
	return &params.ChainConfig{ChainID: big.NewInt(id), LondonBlock: big.NewInt(0)}, nil
}

type Driver struct{}

func (d *Driver) Open(cfg *asset.WalletConfig, logger dex.Logger, net dex.Network) (asset.Wallet, error) {
	chainCfg, err := ChainConfig(net)
	if err != nil {
		return nil, err
	}
	compat, err := NetworkCompatibilityData(net)
	if err != nil {
		return nil, err
	}
	contracts := make(map[uint32]common.Address, 1)
	for ver, netAddrs := range dexah.ContractAddresses {
		if addr, ok := netAddrs[net]; ok && addr != (common.Address{}) {
			contracts[ver] = addr
		}
	}
	var defaultProviders []string
	switch net {
	case dex.Testnet:
		defaultProviders = []string{
			"https://eth-rpc-testnet.polkadot.io",
		}
	case dex.Mainnet:
		defaultProviders = []string{
			"https://kusama-rpc.laissez-faire.trade",
			"https://eth-rpc-kusama.polkadot.io",
		}
	}
	return eth.NewEVMWallet(&eth.EVMWalletConfig{
		BaseChainID:        BipID,
		ChainCfg:           chainCfg,
		AssetCfg:           cfg,
		CompatData:         &compat,
		VersionedGases:     dexah.VersionedGases,
		Tokens:             dexah.Tokens,
		FinalizeConfs:      3,
		Logger:             logger,
		BaseChainContracts: contracts,
		WalletInfo:         WalletInfo,
		Net:                net,
		DefaultProviders:   defaultProviders,
		MaxTxFeeGwei:       dexeth.GweiFactor, // 1 KSM
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
		return err
	}
	return eth.CreateEVMWallet(dexah.ChainIDs[cfg.Net], cfg, &compat, false)
}
