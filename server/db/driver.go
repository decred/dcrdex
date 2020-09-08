// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package db

import (
	"context"
	"fmt"
	"sync"

	"decred.org/dcrdex/dex"
)

var (
	driversMtx sync.Mutex
	drivers    = make(map[string]Driver)
)

// Driver is the interface required of all DB drivers. Open should create a
// DEXArchivist and verify connectivity with the asset's chain server.
type Driver interface {
	Open(ctx context.Context, cfg interface{}) (DEXArchivist, error)
	UseLogger(logger dex.Logger)
}

// Register should be called by the init function of an DB driver's package.
func Register(name string, driver Driver) {
	driversMtx.Lock()
	defer driversMtx.Unlock()

	if driver == nil {
		panic("db: Register driver is nil")
	}
	if _, dup := drivers[name]; dup {
		panic("db: Register called twice for asset driver " + name)
	}
	drivers[name] = driver
}

// Open loads the named DB driver with the provided configuration.
func Open(ctx context.Context, name string, cfg interface{}) (DEXArchivist, error) {
	driversMtx.Lock()
	drv, ok := drivers[name]
	driversMtx.Unlock()
	if !ok {
		return nil, fmt.Errorf("db: unknown database driver %q", name)
	}
	return drv.Open(ctx, cfg)
}

// UseLogger sets the logger to use for all of the DB Drivers.
func UseLogger(logger dex.Logger) {
	driversMtx.Lock()
	for _, drv := range drivers {
		drv.UseLogger(logger)
	}
	driversMtx.Unlock()
}
