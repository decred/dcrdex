// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package asset

import (
	"fmt"
	"sync"

	"decred.org/dcrdex/dex"
)

var (
	driversMtx  sync.Mutex // Can we get rid of this?
	drivers     = make(map[uint32]Driver)
	tokens      = make(map[uint32]TokenDriver)
	childTokens = make(map[uint32]map[uint32]*dex.Token)
)

// AddresserFactory describes a type that can construct new Addressers.
type AddresserFactory interface {
	NewAddresser(acctXPub string, keyIndexer KeyIndexer, network dex.Network) (Addresser, uint32, error)
}

// driverBase defines a base set of driver methods common to both base-chain and
// degenerate assets.
type driverBase interface {
	DecodeCoinID(coinID []byte) (string, error)
	// Version returns the Backend's version number, which is used to signal
	// when major changes are made to internal details such as coin ID encoding
	// and contract structure that must be common to a client's.
	Version() uint32
	// UnitInfo returns the dex.UnitInfo for the asset.
	UnitInfo() dex.UnitInfo
}

// Driver is the interface required of all base chain assets.
type Driver interface {
	driverBase
	// Setup should create a Backend, but not start the backend connection.
	Setup(configPath string, logger dex.Logger, network dex.Network) (Backend, error)
}

// TokenDriver is the interface required of all token assets.
type TokenDriver interface {
	driverBase
	TokenInfo() *dex.Token
}

func baseDriver(assetID uint32) (driverBase, bool) {
	driversMtx.Lock()
	defer driversMtx.Unlock()
	if drv, found := drivers[assetID]; found {
		return drv, true
	}
	if drv, found := tokens[assetID]; found {
		return drv, true
	}
	return nil, false
}

// DecodeCoinID creates a human-readable representation of a coin ID for a named
// asset with a corresponding driver registered with this package.
func DecodeCoinID(assetID uint32, coinID []byte) (string, error) {
	drv, ok := baseDriver(assetID)
	if !ok {
		return "", fmt.Errorf("unknown asset driver %d", assetID)
	}
	return drv.DecodeCoinID(coinID)
}

// asset with a corresponding driver registered with this package.
func UnitInfo(assetID uint32) (dex.UnitInfo, error) {
	drv, ok := baseDriver(assetID)
	if !ok {
		return dex.UnitInfo{}, fmt.Errorf("unknown asset %d (%q) in UnitInfo", assetID, dex.BipIDSymbol(assetID))
	}
	return drv.UnitInfo(), nil

}

// NewAddresser creates an Addresser for a named asset for deriving addresses
// for the given extended public key on a certain network while maintaining the
// address index in an external HDKeyIndex.
func NewAddresser(assetID uint32, acctXPub string, keyIndexer KeyIndexer, network dex.Network) (Addresser, uint32, error) {
	driversMtx.Lock()
	drv, ok := drivers[assetID]
	driversMtx.Unlock()
	if !ok {
		return nil, 0, fmt.Errorf("unknown asset driver %d", assetID)
	}
	af, ok := drv.(AddresserFactory)
	if !ok {
		return nil, 0, fmt.Errorf("asset does not support NewAddresser")
	}
	return af.NewAddresser(acctXPub, keyIndexer, network)
}

// Register should be called by the init function of an asset's package.
func Register(assetID uint32, drv Driver) {
	if drv == nil {
		panic("asset: Register driver is nil")
	}
	if _, ok := baseDriver(assetID); ok {
		panic(fmt.Sprintf("asset: Register called twice for asset driver %d", assetID))
	}
	if drv.UnitInfo().Conventional.ConversionFactor == 0 {
		panic(fmt.Sprintf("asset: Driver registered with unit conversion factor = 0: %q", assetID))
	}
	drivers[assetID] = drv
}

// RegisterToken is called to register a token. The parent asset should be
// registered first.
func RegisterToken(assetID uint32, drv TokenDriver) {
	driversMtx.Lock()
	defer driversMtx.Unlock()
	if drv == nil {
		panic("asset: Register driver is nil")
	}
	if _, ok := tokens[assetID]; ok {
		panic(fmt.Sprintf("asset: RegisterToken called twice for asset driver %d", assetID))
	}
	token := drv.TokenInfo()
	if token == nil {
		panic(fmt.Sprintf("nil *Token for asset %d", assetID))
	}
	if _, exists := drivers[token.ParentID]; !exists {
		panic(fmt.Sprintf("no parent (%d) registered for %s", token.ParentID, token.Name))
	}
	children := childTokens[token.ParentID]
	if children == nil {
		children = make(map[uint32]*dex.Token, 1)
		childTokens[token.ParentID] = children
	}
	children[assetID] = token
	tokens[assetID] = drv
}

// Setup sets up the named asset. The RPC connection parameters are obtained
// from the asset's configuration file located at configPath. Setup is only
// called for base chain assets, not tokens.
func Setup(assetID uint32, configPath string, logger dex.Logger, network dex.Network) (Backend, error) {
	driversMtx.Lock()
	drv, ok := drivers[assetID]
	driversMtx.Unlock()
	if !ok {
		return nil, fmt.Errorf("asset: unknown asset driver %d", assetID)
	}
	return drv.Setup(configPath, logger, network)
}

// Version retrieves the version of the named asset's Backend implementation.
func Version(assetID uint32) (uint32, error) {
	drv, ok := baseDriver(assetID)
	if !ok {
		return 0, fmt.Errorf("asset: unknown asset driver %d", assetID)
	}
	return drv.Version(), nil
}

// IsTokens checks if the asset ID is for a token and returns the token's parent
// ID.
func IsToken(assetID uint32) (is bool, parentID uint32) {
	token, is := tokens[assetID]
	if !is {
		return
	}
	return true, token.TokenInfo().ParentID
}

// Tokens returns the child tokens registered for a base chain asset.
func Tokens(assetID uint32) map[uint32]*dex.Token {
	m := make(map[uint32]*dex.Token, len(childTokens[assetID]))
	for k, v := range childTokens[assetID] {
		m[k] = v
	}
	return m
}
