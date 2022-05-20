// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package zec

import (
	"fmt"

	"decred.org/dcrdex/dex"
	dexbtc "decred.org/dcrdex/dex/networks/btc"
	dexzec "decred.org/dcrdex/dex/networks/zec"
	"decred.org/dcrdex/server/asset"
	"decred.org/dcrdex/server/asset/btc"
	"github.com/btcsuite/btcd/btcutil"
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/btcsuite/btcd/wire"
)

// Driver implements asset.Driver.
type Driver struct{}

// Setup creates the ZCash backend. Start the backend with its Run method.
func (d *Driver) Setup(configPath string, logger dex.Logger, network dex.Network) (asset.Backend, error) {
	return NewBackend(configPath, logger, network)
}

// DecodeCoinID creates a human-readable representation of a coin ID for
// ZCash.
func (d *Driver) DecodeCoinID(coinID []byte) (string, error) {
	// ZCash and Bitcoin have the same tx hash and output format.
	return (&btc.Driver{}).DecodeCoinID(coinID)
}

// Version returns the Backend implementation's version number.
func (d *Driver) Version() uint32 {
	return version
}

// UnitInfo returns the dex.UnitInfo for the asset.
func (d *Driver) UnitInfo() dex.UnitInfo {
	return dexzec.UnitInfo
}

func init() {
	asset.Register(BipID, &Driver{})
}

const (
	version   = 0
	BipID     = 133
	assetName = "zec"
	feeConfs  = 10 // Block time is 75 seconds
)

// NewBackend generates the network parameters and creates a zec backend as a
// btc clone using an asset/btc helper function.
func NewBackend(configPath string, logger dex.Logger, network dex.Network) (asset.Backend, error) {
	var btcParams *chaincfg.Params
	var addrParams *dexzec.AddressParams
	switch network {
	case dex.Mainnet:
		btcParams = dexzec.MainNetParams
		addrParams = dexzec.MainNetAddressParams
	case dex.Testnet:
		btcParams = dexzec.TestNet4Params
		addrParams = dexzec.TestNet4AddressParams
	case dex.Regtest:
		btcParams = dexzec.RegressionNetParams
		addrParams = dexzec.RegressionNetAddressParams
	default:
		return nil, fmt.Errorf("unknown network ID %v", network)
	}

	// Designate the clone ports. These will be overwritten by any explicit
	// settings in the configuration file.
	ports := dexbtc.NetPorts{
		Mainnet: "8232",
		Testnet: "18232",
		Simnet:  "18232",
	}

	if configPath == "" {
		configPath = dexbtc.SystemConfigPath("zcash")
	}

	be, err := btc.NewBTCClone(&btc.BackendCloneConfig{
		Name:        assetName,
		Segwit:      false,
		ConfigPath:  configPath,
		Logger:      logger,
		Net:         network,
		ChainParams: btcParams,
		Ports:       ports,
		AddressDecoder: func(addr string, net *chaincfg.Params) (btcutil.Address, error) {
			return dexzec.DecodeAddress(addr, addrParams, btcParams)
		},
		DumbFeeEstimates: true,
		ManualMedianFee:  true,
		// NoCompetitionFeeRate is not based on zcashd DEFAULT_MIN_RELAY_TX_FEE,
		// because that rate is 0.1 zats/byte. Looking at a block explorer, it
		// appears that typical rates actually are very low, and sometimes under
		// 1 zat/byte, but not usually.
		NoCompetitionFeeRate: 2,
		TxDeserializer: func(txB []byte) (*wire.MsgTx, error) {
			zecTx, err := dexzec.DeserializeTx(txB)
			if err != nil {
				return nil, err
			}
			return zecTx.MsgTx, nil
		},
		InitTxSize:       dexzec.InitTxSize,
		InitTxSizeBase:   dexzec.InitTxSizeBase,
		NumericGetRawRPC: true,
	})
	if err != nil {
		return nil, err
	}

	return &ZECBackend{
		Backend:    be,
		addrParams: addrParams,
		btcParams:  btcParams,
	}, nil
}

// ZECBackend embeds *btc.Backend and re-implements the Contract method to deal
// with ZCash address translation.
type ZECBackend struct {
	*btc.Backend
	btcParams  *chaincfg.Params
	addrParams *dexzec.AddressParams
}

// Contract returns the output from embedded Backend's Contract method, but
// with the SwapAddress field converted to ZCash encoding.
// TODO: Drop this in favor of an AddressEncoder field in the
// BackendCloneConfig.
func (be *ZECBackend) Contract(coinID []byte, redeemScript []byte) (*asset.Contract, error) { // Contract.SwapAddress
	contract, err := be.Backend.Contract(coinID, redeemScript)
	if err != nil {
		return nil, err
	}
	contract.SwapAddress, err = dexzec.RecodeAddress(contract.SwapAddress, be.addrParams, be.btcParams)
	if err != nil {
		return nil, err
	}
	return contract, nil
}
