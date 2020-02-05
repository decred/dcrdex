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

// Driver is the interface required of all exchange wallets.
type Driver interface {
	Setup(*WalletConfig, dex.Logger, dex.Network) (Wallet, error)
	Info() *WalletInfo
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
	drivers[assetID] = driver
}

// Setup sets up the asset, returning the exchange wallet.
func Setup(assetID uint32, cfg *WalletConfig, logger dex.Logger, network dex.Network) (Wallet, error) {
	driversMtx.Lock()
	drv, ok := drivers[assetID]
	driversMtx.Unlock()
	if !ok {
		return nil, fmt.Errorf("asset: unknown asset driver %d", assetID)
	}
	return drv.Setup(cfg, logger, network)
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

// Info retrieves the asset's *WalletInfo. A nil value will be returned for an
// unknown asset.
func Info(assetID uint32) *WalletInfo {
	driversMtx.RLock()
	defer driversMtx.RUnlock()
	driver := drivers[assetID]
	if driver == nil {
		return nil
	}
	return driver.Info()
}
