// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package polygon

import (
	"fmt"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/client/asset/eth"
	"decred.org/dcrdex/dex"
	dexpolygon "decred.org/dcrdex/dex/networks/polygon"
)

func init() {
	asset.Register(BipID, &Driver{})
}

const (
	// BipID is the BIP-0044 asset ID for Polygon.
	BipID         = 966
	version       = 0
	walletTypeRPC = "rpc"
)

var (
	// WalletInfo defines some general information about a Ethereum wallet.
	WalletInfo = &asset.WalletInfo{
		Name:    "Polygon",
		Version: version,
		// SupportedVersions: For Ethereum, the server backend maintains a
		// single protocol version, so tokens and ETH have the same set of
		// supported versions. Even though the SupportedVersions are made
		// accessible for tokens via (*TokenWallet).Info, the versions are not
		// exposed though any Driver methods or assets/driver functions. Use the
		// parent wallet's WalletInfo via (*Driver).Info if you need a token's
		// supported versions before a wallet is available.
		SupportedVersions: []uint32{version},
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

	chainIDs = map[dex.Network]int64{
		dex.Mainnet: 137,
		dex.Testnet: 80001, // Mumbai
		dex.Simnet:  90001, // See dex/testing/polygon/genesis.json
	}
)

type Driver struct{}

// Open opens the Polygon exchange wallet. Start the wallet with its Run method.
func (d *Driver) Open(cfg *asset.WalletConfig, logger dex.Logger, net dex.Network) (asset.Wallet, error) {
	return eth.NewEVMWallet(BipID, chainIDs[net], cfg, logger, net)
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
	return eth.CreateEVMWallet(chainIDs[cfg.Net], cfg, false)
}
