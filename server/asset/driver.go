// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package asset

import (
	"fmt"

	"decred.org/dcrdex/dex"
)

var (
	drivers     = make(map[uint32]Driver)
	tokens      = make(map[uint32]TokenDriver)
	childTokens = make(map[uint32]map[uint32]*dex.Token)
)

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
	// Name is the name for the asset.
	Name() string
}

// minValuer can be implemented by assets with minimum dust sizes that vary
// with fee rate. If an asset driver does not implement minValuer, the min
// value is assumed to be 1 atom for both bond and lot sizes.
type minValuer interface {
	MinBondSize(maxFeeRate uint64) uint64
	MinLotSize(maxFeeRate uint64) uint64
}

// Driver is the interface required of all base chain assets.
type Driver interface {
	driverBase
	// Setup should create a Backend, but not start the backend connection.
	Setup(*BackendConfig) (Backend, error)
}

// TokenDriver is the interface required of all token assets.
type TokenDriver interface {
	driverBase
	TokenInfo() *dex.Token
}

func baseDriver(assetID uint32) (driverBase, bool) {
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
	if drv.UnitInfo().Conventional.ConversionFactor == 0 {
		panic(fmt.Sprintf("asset: TokenDriver registered with unit conversion factor = 0: %q", assetID))
	}
	children := childTokens[token.ParentID]
	if children == nil {
		children = make(map[uint32]*dex.Token, 1)
		childTokens[token.ParentID] = children
	}
	children[assetID] = token
	tokens[assetID] = drv
}

type BackendConfig struct {
	AssetID    uint32
	ConfigPath string
	Logger     dex.Logger
	Net        dex.Network
	RelayAddr  string
}

// Setup sets up the named asset. The RPC connection parameters are obtained
// from the asset's configuration file located at configPath. Setup is only
// called for base chain assets, not tokens.
func Setup(cfg *BackendConfig) (Backend, error) {
	drv, ok := drivers[cfg.AssetID]
	if !ok {
		return nil, fmt.Errorf("asset: unknown asset driver %d", cfg.AssetID)
	}
	return drv.Setup(cfg)
}

// Version retrieves the version of the named asset's Backend implementation.
func Version(assetID uint32) (uint32, error) {
	drv, ok := baseDriver(assetID)
	if !ok {
		return 0, fmt.Errorf("asset: unknown asset driver %d", assetID)
	}
	return drv.Version(), nil
}

// IsToken checks if the asset ID is for a token and returns the token's parent
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

// Minimums returns the minimimum lot size and bond size for a registered asset.
func Minimums(assetID uint32, maxFeeRate uint64) (minLotSize, minBondSize uint64, found bool) {
	baseChainID := assetID
	if token, is := tokens[assetID]; is {
		baseChainID = token.TokenInfo().ParentID
	}
	drv, found := drivers[baseChainID]
	if !found {
		return 0, 0, false
	}
	m, is := drv.(minValuer)
	if !is {
		return 1, 1, true
	}
	return m.MinLotSize(maxFeeRate), m.MinBondSize(maxFeeRate), true
}

// RegisteredAsset is information about a registered asset.
type RegisteredAsset struct {
	AssetID  uint32
	Symbol   string
	Name     string
	UnitInfo dex.UnitInfo
}

// Assets returns a information about registered assets.
func Assets() []*RegisteredAsset {
	assets := make([]*RegisteredAsset, 0, len(drivers)+len(tokens))
	for assetID, drv := range drivers {
		assets = append(assets, &RegisteredAsset{
			AssetID:  assetID,
			Symbol:   dex.BipIDSymbol(assetID),
			Name:     drv.Name(),
			UnitInfo: drv.UnitInfo(),
		})
	}
	for assetID, drv := range tokens {
		assets = append(assets, &RegisteredAsset{
			AssetID:  assetID,
			Symbol:   dex.BipIDSymbol(assetID),
			Name:     drv.Name(),
			UnitInfo: drv.UnitInfo(),
		})
	}
	return assets
}
