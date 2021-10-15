// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package asset

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"

	"decred.org/dcrdex/dex"
	"golang.org/x/text/language"
)

var (
	driversMtx sync.RWMutex
	drivers    = make(map[uint32]Driver)
)

// CreateWalletParams are the parameters for internal wallet creation. The
// Settings provided should be the same wallet configuration settings passed to
// Open.
type CreateWalletParams struct {
	Type     string
	Seed     []byte
	Pass     []byte
	Settings map[string]string
	DataDir  string
	Net      dex.Network
	Logger   dex.Logger
}

// Driver is the interface required of all exchange wallets.
type Driver interface {
	Exists(walletType, dataDir string, settings map[string]string, net dex.Network) (bool, error)
	Create(*CreateWalletParams) error
	Open(*WalletConfig, dex.Logger, dex.Network) (Wallet, error)
	DecodeCoinID(coinID []byte) (string, error)
	Info() *WalletInfo
	Initialize(ctx context.Context, wg *sync.WaitGroup, logger dex.Logger, lang language.Tag)
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

// WalletExists will be true if the specified wallet exists.
func WalletExists(assetID uint32, walletType, dataDir string, settings map[string]string, net dex.Network) (exists bool, err error) {
	return exists, withDriver(assetID, func(drv Driver) error {
		exists, err = drv.Exists(walletType, dataDir, settings, net)
		return err
	})
}

// CreateWallet creates a new wallet. Only use Create for seeded wallet types.
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

var assetsInited uint32

// Initialize will initialize asset backends. This allows backends to setup
// teardown routines to synchronize shutdown with the caller.
func Initialize(ctx context.Context, wg *sync.WaitGroup, logger dex.Logger, lang language.Tag) {
	if !atomic.CompareAndSwapUint32(&assetsInited, 0, 1) {
		return
	}
	driversMtx.RLock()
	defer driversMtx.RUnlock()
	for assetID, drv := range drivers {
		drv.Initialize(ctx, wg, logger.SubLogger(dex.BipIDSymbol(assetID)), lang)
	}
}
