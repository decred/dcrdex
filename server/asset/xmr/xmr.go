package xmr

// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

import (
	"encoding/hex"
	"fmt"

	"decred.org/dcrdex/dex"
	dexbtc "decred.org/dcrdex/dex/networks/btc"
	dexxmr "decred.org/dcrdex/dex/networks/xmr"
	"decred.org/dcrdex/server/asset"
)

func init() {
	asset.Register(BipID, &Driver{})
}

const (
	version   = 0
	BipID     = 128
	assetName = "xmr"
)

const KeyLen = 32

// Driver implements asset.Driver.
type Driver struct{}

// Setup creates the XMR backend. Start the backend with its Run method.
func (d *Driver) Setup(cfg *asset.BackendConfig) (asset.Backend, error) {
	return NewBackend(cfg)
}

// DecodeCoinID creates a human-readable representation of a coin ID for Monero.
func (d *Driver) DecodeCoinID(coinID []byte) (string, error) {
	// Monero transactions have outputs but no amounts so the coinID
	// will just be the tx hash for now; representing the full output
	// amount sent. Change is unknown but the fee is known.
	if len(coinID) != KeyLen {
		return "", fmt.Errorf("bad tx_hash size")
	}
	return hex.EncodeToString(coinID), nil
}

// Version returns the Backend implementation's version number.
func (d *Driver) Version() uint32 {
	return version
}

// UnitInfo returns the dex.UnitInfo for the asset.
func (d *Driver) UnitInfo() dex.UnitInfo {
	return dexxmr.UnitInfo
}

// Name is the asset's name.
func (d *Driver) Name() string {
	return "Monero"
}

// MinBondSize calculates the minimum bond size for a given fee rate that avoids
// dust outputs on the bond and refund txs, assuming the maxFeeRate doesn't
// change.
func (d *Driver) MinBondSize(maxFeeRate uint64) uint64 {
	return dexbtc.MinBondSize(maxFeeRate, false)
}

// MinLotSize calculates the minimum bond size for a given fee rate that avoids
// dust outputs on the swap and refund txs, assuming the maxFeeRate doesn't
// change.
func (d *Driver) MinLotSize(maxFeeRate uint64) uint64 {
	return dexbtc.MinLotSize(maxFeeRate, false)
}
