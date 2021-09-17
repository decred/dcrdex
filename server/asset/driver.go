// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

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

// AddresserFactory describes a type that can construct new Addressers.
type AddresserFactory interface {
	NewAddresser(acctXPub string, keyIndexer HDKeyIndexer, network dex.Network) (Addresser, error)
}

// Driver is the interface required of all assets. A Driver may or may not also
// be an AddresserFactory.
type Driver interface {
	// Setup should create a Backend, but not start the backend connection.
	Setup(configPath string, logger dex.Logger, network dex.Network) (Backend, error)
	DecodeCoinID(coinID []byte) (string, error)
	// Version returns the Backend's version number, which is used to signal
	// when major changes are made to internal details such as coin ID encoding
	// and contract structure that must be common to a client's.
	Version() uint32
}

// DecodeCoinID creates a human-readable representation of a coin ID for a named
// asset with a corresponding driver registered with this package.
func DecodeCoinID(name string, coinID []byte) (string, error) {
	driversMtx.Lock()
	drv, ok := drivers[name]
	driversMtx.Unlock()
	if !ok {
		return "", fmt.Errorf("unknown asset driver %q", name)
	}
	return drv.DecodeCoinID(coinID)
}

// NewAddresser creates an Addresser for a named asset for deriving addresses
// for the given extended public key on a certain network while maintaining the
// address index in an external HDKeyIndex.
func NewAddresser(name string, acctXPub string, keyIndexer HDKeyIndexer, network dex.Network) (Addresser, error) {
	driversMtx.Lock()
	drv, ok := drivers[name]
	driversMtx.Unlock()
	if !ok {
		return nil, fmt.Errorf("unknown asset driver %q", name)
	}
	af, ok := drv.(AddresserFactory)
	if !ok {
		return nil, fmt.Errorf("asset does not support NewAddresser")
	}
	return af.NewAddresser(acctXPub, keyIndexer, network)
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

// Version retrieves the version of the named asset's Backend implementation.
func Version(name string) (uint32, error) {
	driversMtx.Lock()
	drv, ok := drivers[name]
	driversMtx.Unlock()
	if !ok {
		return 0, fmt.Errorf("asset: unknown asset driver %q", name)
	}
	return drv.Version(), nil
}
