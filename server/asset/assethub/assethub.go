// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

// Package assethub is the server backend for KSM and its tokens on Kusama
// Asset Hub's Ethereum side. It is the eth backend with Asset Hub's
// parameters.
package assethub

import (
	"fmt"

	dexah "decred.org/dcrdex/dex/networks/assethub"
	dexeth "decred.org/dcrdex/dex/networks/eth"
	"decred.org/dcrdex/server/asset"
	"decred.org/dcrdex/server/asset/eth"
)

const BipID = dexah.KsmBipID

var registeredTokens = make(map[uint32]*eth.VersionedToken)

func registerToken(assetID uint32, protocolVersion dexeth.ProtocolVersion) {
	token, exists := dexah.Tokens[assetID]
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
			UI:              dexah.UnitInfo,
			Nam:             "Kusama Asset Hub",
		},
	}})
	registerToken(dexah.DusdTokenID, eth.ProtocolVersion(dexah.DusdTokenID))
}

type Driver struct {
	eth.Driver
}

func (d *Driver) Setup(cfg *asset.BackendConfig) (asset.Backend, error) {
	return eth.NewEVMBackend(cfg, uint64(dexah.ChainIDs[cfg.Net]), dexah.ContractAddresses, registeredTokens)
}
