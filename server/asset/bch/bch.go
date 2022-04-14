// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package bch

import (
	"fmt"

	"decred.org/dcrdex/dex"
	dexbch "decred.org/dcrdex/dex/networks/bch"
	dexbtc "decred.org/dcrdex/dex/networks/btc"
	"decred.org/dcrdex/server/asset"
	"decred.org/dcrdex/server/asset/btc"
	"github.com/btcsuite/btcd/chaincfg"
)

// Driver implements asset.Driver.
type Driver struct{}

// Setup creates the BCH backend. Start the backend with its Run method.
func (d *Driver) Setup(configPath string, logger dex.Logger, network dex.Network) (asset.Backend, error) {
	return NewBackend(configPath, logger, network)
}

// Version returns the Backend implementation's version number.
func (d *Driver) Version() uint32 {
	return version
}

// DecodeCoinID creates a human-readable representation of a coin ID for
// Bitcoin Cash.
func (d *Driver) DecodeCoinID(coinID []byte) (string, error) {
	// Bitcoin Cash and Bitcoin have the same tx hash and output format.
	return (&btc.Driver{}).DecodeCoinID(coinID)
}

// UnitInfo returns the dex.UnitInfo for the asset.
func (d *Driver) UnitInfo() dex.UnitInfo {
	return dexbch.UnitInfo
}

func init() {
	asset.Register(BipID, &Driver{})
}

const (
	version   = 0
	BipID     = 145
	assetName = "bch"
)

// NewBackend generates the network parameters and creates a bch backend as a
// btc clone using an asset/btc helper function.
func NewBackend(configPath string, logger dex.Logger, network dex.Network) (asset.Backend, error) {
	var params *chaincfg.Params
	switch network {
	case dex.Mainnet:
		params = dexbch.MainNetParams
	case dex.Testnet:
		params = dexbch.TestNet4Params
	case dex.Regtest:
		params = dexbch.RegressionNetParams
	default:
		return nil, fmt.Errorf("unknown network ID %v", network)
	}

	// Designate the clone ports. These will be overwritten by any explicit
	// settings in the configuration file. Bitcoin Cash uses the same default
	// ports as Bitcoin.
	ports := dexbtc.NetPorts{
		Mainnet: "8332",
		Testnet: "28332",
		Simnet:  "18443",
	}

	if configPath == "" {
		configPath = dexbtc.SystemConfigPath("bitcoin") // Yes, Bitcoin Cash's default config path is the same as bitcoin.
	}

	be, err := btc.NewBTCClone(&btc.BackendCloneConfig{
		Name:             assetName,
		Segwit:           false,
		ConfigPath:       configPath,
		AddressDecoder:   dexbch.DecodeCashAddress,
		Logger:           logger,
		Net:              network,
		ChainParams:      params,
		Ports:            ports,
		DumbFeeEstimates: true,
		// Bitcoin cash actually has getblockstats, but the RPC returns floats
		// in units of BCH/byte.
		ManualMedianFee:      true,
		NoCompetitionFeeRate: 2,
		ArglessFeeEstimates:  true,
	})
	if err != nil {
		return nil, err
	}

	return &BCHBackend{
		Backend: be,
	}, nil
}

// BCHBackend embeds *btc.Backend and re-implements the Contract method to deal
// with Cash Address translation.
type BCHBackend struct {
	*btc.Backend
}

// Contract returns the output from embedded Backend's Contract method, but
// with the SwapAddress field converted to Cash Address encoding.
func (bch *BCHBackend) Contract(coinID []byte, redeemScript []byte) (*asset.Contract, error) { // Contract.SwapAddress
	contract, err := bch.Backend.Contract(coinID, redeemScript)
	if err != nil {
		return nil, err
	}
	contract.SwapAddress, err = dexbch.RecodeCashAddress(contract.SwapAddress, bch.Net())
	if err != nil {
		return nil, err
	}
	return contract, nil
}
