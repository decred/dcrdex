// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package asset

import (
	"fmt"
	"sync"

	"decred.org/dcrdex/dex"
)

// nettedToken is a Token with its available networks. Saving the valid
// networks enables filtering out tokens via the package's SetNetwork.
type nettedToken struct {
	*Token
	nets []dex.Network
}

var (
	driversMtx sync.RWMutex
	drivers    = make(map[uint32]Driver)
	tokens     = make(map[uint32]*nettedToken)
)

// CreateWalletParams are the parameters for internal wallet creation. The
// Settings provided should be the same wallet configuration settings passed to
// OpenWallet.
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
	Open(*WalletConfig, dex.Logger, dex.Network) (Wallet, error)
	DecodeCoinID(coinID []byte) (string, error)
	Info() *WalletInfo
}

// Creator defines methods for Drivers that will be called to initialize seeded
// wallets during CreateWallet. Only assets that provide seeded wallets need to
// implement Creator.
type Creator interface {
	Exists(walletType, dataDir string, settings map[string]string, net dex.Network) (bool, error)
	Create(*CreateWalletParams) error
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

// RegisterToken should be called to register tokens. If no nets are specified
// the token will be registered for all networks. The user must invoke
// SetNetwork to enable net-based filtering of package function output.
func RegisterToken(tokenID uint32, token *dex.Token, walletDef *WalletDefinition, nets ...dex.Network) {
	driversMtx.Lock()
	defer driversMtx.Unlock()
	if _, exists := tokens[tokenID]; exists {
		panic(fmt.Sprintf("token %d already exists", tokenID))
	}
	_, exists := drivers[token.ParentID]
	if !exists {
		panic(fmt.Sprintf("token %d's parent asset %d isn't registered", tokenID, token.ParentID))
	}
	tokens[tokenID] = &nettedToken{
		Token: &Token{
			Token:      token,
			Definition: walletDef,
		},
		nets: nets,
	}
}

// WalletExists will be true if the specified wallet exists.
func WalletExists(assetID uint32, walletType, dataDir string, settings map[string]string, net dex.Network) (exists bool, err error) {
	return exists, withDriver(assetID, func(drv Driver) error {
		creator, is := drv.(Creator)
		if !is {
			return fmt.Errorf("driver has no Exists method")
		}
		exists, err = creator.Exists(walletType, dataDir, settings, net)
		return err
	})
}

// CreateWallet creates a new wallet. Only use Create for seeded wallet types.
func CreateWallet(assetID uint32, seedParams *CreateWalletParams) error {
	return withDriver(assetID, func(drv Driver) error {
		creator, is := drv.(Creator)
		if !is {
			return fmt.Errorf("driver has no Create method")
		}
		return creator.Create(seedParams)
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
// asset with a corresponding driver registered with this package. For now,
// token id decoding is deferred to the parent asset.
func DecodeCoinID(assetID uint32, coinID []byte) (cid string, err error) {
	driversMtx.Lock()
	defer driversMtx.Unlock()
	drv, ok := drivers[assetID]
	if ok {
		return drv.DecodeCoinID(coinID)
	}
	tkn, ok := tokens[assetID]
	if !ok {
		return "", fmt.Errorf("no driver or token info for asset %d (%s)", assetID, dex.BipIDSymbol(assetID))
	}
	drv, ok = drivers[tkn.ParentID]
	if ok {
		return drv.DecodeCoinID(coinID)
	}
	return "", fmt.Errorf("no %d (%s) parent asset driver for token %d (%s)",
		tkn.ParentID, dex.BipIDSymbol(tkn.ParentID), assetID, dex.BipIDSymbol(assetID))
}

// A registered asset is information about a supported asset.
type RegisteredAsset struct {
	ID     uint32
	Symbol string
	Info   *WalletInfo
	Tokens map[uint32]*Token
}

// Assets returns a list of information about supported assets.
func Assets() map[uint32]*RegisteredAsset {
	driversMtx.RLock()
	defer driversMtx.RUnlock()
	assets := make(map[uint32]*RegisteredAsset, len(drivers))
	for assetID, driver := range drivers {
		assets[assetID] = &RegisteredAsset{
			ID:     assetID,
			Symbol: dex.BipIDSymbol(assetID),
			Info:   driver.Info(),
		}
	}

	for tokenID, token := range tokens {
		parent, found := assets[token.ParentID]
		if !found {
			// should be impossible.
			fmt.Println("parentless token", tokenID, token.Name)
			continue
		}
		if parent.Tokens == nil {
			parent.Tokens = make(map[uint32]*Token, 1)
		}
		parent.Tokens[tokenID] = token.Token
	}
	return assets
}

// Asset gets the RegisteredAsset for the specified asset ID. Asset is for
// base chain assets, not tokens.
func Asset(assetID uint32) *RegisteredAsset {
	driversMtx.RLock()
	defer driversMtx.RUnlock()
	drv := drivers[assetID]
	if drv == nil {
		return nil
	}
	ra := &RegisteredAsset{
		ID:     assetID,
		Symbol: dex.BipIDSymbol(assetID),
		Info:   drv.Info(),
	}
	for tokenID, token := range tokens {
		if token.ParentID == assetID {
			if ra.Tokens == nil {
				ra.Tokens = make(map[uint32]*Token, 1)
			}
			ra.Tokens[tokenID] = token.Token
		}
	}
	return ra
}

// TokenInfo returns *Token for a registered token, or nil if the token is
// unknown.
func TokenInfo(assetID uint32) *Token {
	driversMtx.RLock()
	defer driversMtx.RUnlock()
	nt := tokens[assetID]
	if nt == nil {
		return nil
	}
	return nt.Token
}

// Info returns the WalletInfo for the specified asset, if supported. Info only
// returns WalletInfo for base chain assets, not tokens.
func Info(assetID uint32) (*WalletInfo, error) {
	driversMtx.RLock()
	drv, ok := drivers[assetID]
	driversMtx.RUnlock()
	if !ok {
		return nil, fmt.Errorf("asset: unsupported asset %d", assetID)
	}
	return drv.Info(), nil
}

// UnitInfo returns the dex.UnitInfo for the asset or token.
func UnitInfo(assetID uint32) (dex.UnitInfo, error) {
	driversMtx.RLock()
	defer driversMtx.RUnlock()
	drv, ok := drivers[assetID]
	if ok {
		return drv.Info().UnitInfo, nil
	}
	token, ok := tokens[assetID]
	if ok {
		return token.UnitInfo, nil
	}
	return dex.UnitInfo{}, fmt.Errorf("asset: unsupported asset %d", assetID)
}

// SetNetwork will filter registered assets for those available on the specified
// network. SetNetwork need only be called once during initialization.
func SetNetwork(net dex.Network) {
nextasset:
	for assetID, nt := range tokens {
		if len(nt.nets) == 0 {
			continue
		}
		for _, allowedNet := range nt.nets {
			if net == allowedNet {
				continue nextasset
			}
		}
		// This network is not supported for this asset.
		delete(tokens, assetID)
	}
}
