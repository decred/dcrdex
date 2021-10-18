// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package asset

import (
	"fmt"
	"sync"

	"decred.org/dcrdex/dex"
)

var (
	driversMtx sync.RWMutex
	drivers    = make(map[uint32]Driver)
)

// CreateWalletParams are the parameters for internal wallet creation. The
// Settings provided should be the same wallet configuration settings passed to
// Open.
type CreateWalletParams struct {
	Seed     []byte
	Pass     []byte
	Settings map[string]string
	DataDir  string
	Net      dex.Network
}

// Driver is the interface required of all exchange wallets.
type Driver interface {
	Create(*CreateWalletParams) error
	Open(*WalletConfig, dex.Logger, dex.Network) (Wallet, error)
	DecodeCoinID(coinID []byte) (string, error)
	Info() *WalletInfo
}

func withDriver(assetID uint32, f func(Driver) error) error {
	driversMtx.Lock()
	defer driversMtx.Unlock()
	drv, ok := drivers[assetID]
	if !ok {
		return fmt.Errorf("asset: unknown driver asset %d", assetID)
	}
	return f(drv)
}

// Register should be called by the init function of an asset's package.
func Register(assetID uint32, driver Driver) {
	driversMtx.Lock()
	defer driversMtx.Unlock()

	if driver == nil {
		panic("asset: Register driver is nil")
	}
	if _, dup := drivers[assetID]; dup {
		panic(fmt.Sprint("asset: Register called twice for asset driver ", assetID))
	}
	if driver.Info().UnitInfo.Conventional.ConversionFactor == 0 {
		panic(fmt.Sprint("asset: Registered driver doesn't have a conventional conversion factor set in the wallet info ", assetID))
	}
	drivers[assetID] = driver
}

// CreateWallet creates a new wallet. This method should only be used once to create a
// seeded wallet, after which OpenWallet should be used to load and access the wallet.
func CreateWallet(assetID uint32, seedParams *CreateWalletParams) error {
	return withDriver(assetID, func(drv Driver) error {
		return drv.Create(seedParams)
	})
}

// OpenWallet sets up the asset, returning the exchange wallet.
func OpenWallet(assetID uint32, cfg *WalletConfig, logger dex.Logger, net dex.Network) (w Wallet, err error) {
	return w, withDriver(assetID, func(drv Driver) error {
		w, err = drv.Open(cfg, logger, net)
		return err
	})
}

// DecodeCoinID creates a human-readable representation of a coin ID for a named
// asset with a corresponding driver registered with this package.
func DecodeCoinID(assetID uint32, coinID []byte) (cid string, err error) {
	return cid, withDriver(assetID, func(drv Driver) error {
		cid, err = drv.DecodeCoinID(coinID)
		return err
	})
}

// A registered asset is information about a supported asset.
type RegisteredAsset struct {
	ID     uint32
	Symbol string
	Info   *WalletInfo
}

// Assets returns a list of information about supported assets.
func Assets() map[uint32]RegisteredAsset {
	driversMtx.RLock()
	defer driversMtx.RUnlock()
	assets := make(map[uint32]RegisteredAsset, len(drivers))
	for assetID, driver := range drivers {
		assets[assetID] = RegisteredAsset{
			ID:     assetID,
			Symbol: dex.BipIDSymbol(assetID),
			Info:   driver.Info(),
		}
	}
	return assets
}

// Info returns the WalletInfo for the specified asset, if supported.
func Info(assetID uint32) (*WalletInfo, error) {
	driversMtx.RLock()
	drv, ok := drivers[assetID]
	driversMtx.RUnlock()
	if !ok {
		return nil, fmt.Errorf("asset: unsupported asset %d", assetID)
	}
	return drv.Info(), nil
}
