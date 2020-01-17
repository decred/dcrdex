package asset

import (
	"fmt"
	"sync"

	"decred.org/dcrdex/dex"
)

var (
	driversMtx sync.Mutex
	drivers    = make(map[uint32]Driver)
)

// Driver is the interface required of all assets. Setup should create a
// Backend, but not start the backend connection.
type Driver interface {
	Setup(*WalletConfig, dex.Logger, dex.Network) (Wallet, error)
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

// Setup sets up the named asset. The RPC connection parameters are obtained
// from the asset's configuration file located at configPath.
func Setup(assetID uint32, cfg *WalletConfig, logger dex.Logger, network dex.Network) (Wallet, error) {
	driversMtx.Lock()
	drv, ok := drivers[assetID]
	driversMtx.Unlock()
	if !ok {
		return nil, fmt.Errorf("asset: unknown asset driver %d", assetID)
	}
	return drv.Setup(cfg, logger, network)
}

// Supported creates and returns a slice of registered asset IDs.
func Supported() []uint32 {
	ids := make([]uint32, 0, len(drivers))
	for id := range drivers {
		ids = append(ids, id)
	}
	return ids
}
