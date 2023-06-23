// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package polygon

import (
	"fmt"
	"time"

	"decred.org/dcrdex/dex"
	dexpolygon "decred.org/dcrdex/dex/networks/polygon"
	"decred.org/dcrdex/server/asset"
	"decred.org/dcrdex/server/asset/eth"
)

var registeredTokens = make(map[uint32]*eth.VersionedToken)

func registerToken(assetID uint32, ver uint32) {
	token, exists := dexpolygon.Tokens[assetID]
	if !exists {
		panic(fmt.Sprintf("no token constructor for asset ID %d", assetID))
	}
	asset.RegisterToken(assetID, &eth.TokenDriver{
		DriverBase: eth.DriverBase{
			Ver: ver,
			UI:  token.UnitInfo,
		},
		Token: token.Token,
	})
	registeredTokens[assetID] = &eth.VersionedToken{
		Token: token,
		Ver:   ver,
	}
}

func init() {
	asset.Register(BipID, &Driver{eth.Driver{
		DriverBase: eth.DriverBase{
			Ver: version,
			UI:  dexpolygon.UnitInfo,
		},
	}})

	registerToken(testTokenID, 0)
	// registerToken(usdcID, 0)

	if blockPollIntervalStr != "" {
		blockPollInterval, _ = time.ParseDuration(blockPollIntervalStr)
		if blockPollInterval < time.Second {
			panic(fmt.Sprintf("invalid value for blockPollIntervalStr: %q", blockPollIntervalStr))
		}
	}
}

const (
	BipID              = 966
	ethContractVersion = 0
	version            = 0
)

var (
	testTokenID, _ = dex.BipSymbolID("dextt.polygon")

	// blockPollInterval is the delay between calls to bestBlockHash to check
	// for new blocks. Modify at compile time via blockPollIntervalStr:
	// go build -tags lgpl -ldflags "-X 'decred.org/dcrdex/server/asset/polygon.blockPollIntervalStr=10s'"
	blockPollInterval    = time.Second
	blockPollIntervalStr string
)

type Driver struct {
	eth.Driver
}

// Setup creates the ETH backend. Start the backend with its Run method.
func (d *Driver) Setup(configPath string, logger dex.Logger, net dex.Network) (asset.Backend, error) {
	return eth.NewEVMBackend(BipID, configPath, logger, dexpolygon.ContractAddresses, registeredTokens, net)
}
