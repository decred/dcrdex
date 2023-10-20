// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package polygon

import (
	"fmt"

	"decred.org/dcrdex/dex"
	dexeth "decred.org/dcrdex/dex/networks/eth"
	dexpolygon "decred.org/dcrdex/dex/networks/polygon"
	"decred.org/dcrdex/server/asset"
	"decred.org/dcrdex/server/asset/eth"
)

var registeredTokens = make(map[uint32]*eth.VersionedToken)

func registerToken(assetID uint32, protocolVersion dexeth.ProtocolVersion) {
	token, exists := dexpolygon.Tokens[assetID]
	if !exists {
		panic(fmt.Sprintf("no token constructor for asset ID %d", assetID))
	}
	asset.RegisterToken(assetID, &eth.TokenDriver{
		DriverBase: eth.DriverBase{
			ProtocolVersion: protocolVersion,
			UI:              token.UnitInfo,
			Nam:             token.Name,
		},
		Token: token.Token,
	})
	registeredTokens[assetID] = &eth.VersionedToken{
		Token:           token,
		ContractVersion: protocolVersion.ContractVersion(),
	}
}

func init() {
	asset.Register(BipID, &Driver{eth.Driver{
		DriverBase: eth.DriverBase{
			ProtocolVersion: eth.ProtocolVersion(BipID),
			UI:              dexpolygon.UnitInfo,
			Nam:             "Polygon",
		},
	}})

	registerToken(usdcID, eth.ProtocolVersion(usdcID))
	registerToken(usdtID, eth.ProtocolVersion(usdtID))
	registerToken(wethTokenID, eth.ProtocolVersion(wethTokenID))
	registerToken(wbtcTokenID, eth.ProtocolVersion(wbtcTokenID))
}

const (
	BipID = 966
)

var (
	usdcID, _      = dex.BipSymbolID("usdc.polygon")
	usdtID, _      = dex.BipSymbolID("usdt.polygon")
	wethTokenID, _ = dex.BipSymbolID("weth.polygon")
	wbtcTokenID, _ = dex.BipSymbolID("wbtc.polygon")
)

type Driver struct {
	eth.Driver
}

// Setup creates the ETH backend. Start the backend with its Run method.
func (d *Driver) Setup(cfg *asset.BackendConfig) (asset.Backend, error) {
	var chainID uint64
	switch cfg.Net {
	case dex.Mainnet:
		chainID = dexpolygon.BorMainnetChainConfig.ChainID.Uint64()
	case dex.Testnet:
		chainID = dexpolygon.AmoyChainConfig.ChainID.Uint64()
	default:
		chainID = 90001
	}

	return eth.NewEVMBackend(cfg, chainID, dexpolygon.ContractAddresses, registeredTokens)
}
