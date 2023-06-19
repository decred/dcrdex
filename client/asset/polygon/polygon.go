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
	walletTypeRPC = "rpc"
)

var (
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
	return eth.NewEVMWallet(BipID, dexpolygon.ChainIDs[net], cfg, logger, net)
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
	return eth.CreateEVMWallet(dexpolygon.ChainIDs[cfg.Net], cfg, false)
}
