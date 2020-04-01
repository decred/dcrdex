package asset

import (
	"fmt"
	"sync"

	"decred.org/dcrdex/dex"
)

var (
	driversMtx sync.Mutex
	drivers    = make(map[string]Driver)
)

// Driver is the interface required of all assets. Setup should create a
// Backend, but not start the backend connection.
type Driver interface {
	Setup(configPath string, logger dex.Logger, network dex.Network) (Backend, error)
	DecodeCoinID(coinID []byte) (string, error)
}

// DecodeCoinID creates a human-readable representation of a coin ID for a named
// asset with a corresponding driver registered with this package.
func DecodeCoinID(name string, coinID []byte) (string, error) {
	driversMtx.Lock()
	drv, ok := drivers[name]
	driversMtx.Unlock()
	if !ok {
		return "", fmt.Errorf("db: unknown asset driver %q", name)
	}
	return drv.DecodeCoinID(coinID)
}

// Register should be called by the init function of an asset's package.
func Register(name string, driver Driver) {
	driversMtx.Lock()
	defer driversMtx.Unlock()

	if driver == nil {
		panic("asset: Register driver is nil")
	}
	if _, dup := drivers[name]; dup {
		panic("asset: Register called twice for asset driver " + name)
	}
	drivers[name] = driver
}

// Setup sets up the named asset. The RPC connection parameters are obtained
// from the asset's configuration file located at configPath.
func Setup(name, configPath string, logger dex.Logger, network dex.Network) (Backend, error) {
	driversMtx.Lock()
	drv, ok := drivers[name]
	driversMtx.Unlock()
	if !ok {
		return nil, fmt.Errorf("asset: unknown asset driver %q", name)
	}
	return drv.Setup(configPath, logger, network)
}
