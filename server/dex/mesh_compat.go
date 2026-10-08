// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package dex

import (
	"fmt"
	"strings"

	dexpkg "decred.org/dcrdex/dex"
	"decred.org/dcrdex/server/mesh"
	"decred.org/dcrdex/server/meshevents"
)

// buildMeshCompatSnapshot converts DexConf into the configuration used by mesh
// to determine whether two nodes are compatible.
func buildMeshCompatSnapshot(cfg *DexConf) (*mesh.CompatSnapshot, error) {
	compatCfg := mesh.CompatConfig{
		Network:            cfg.Network.String(),
		APIVersion:         uint16(APIVersion),
		EventSchemaVersion: meshevents.EventSchemaVersion,
		BroadcastTimeoutMS: uint64(cfg.BroadcastTimeout.Milliseconds()),
		TxWaitExpirationMS: uint64(cfg.TxWaitExpiration.Milliseconds()),
		CancelThreshold:    cfg.CancelThreshold,
		FreeCancels:        cfg.FreeCancels,
		PenaltyThreshold:   cfg.PenaltyThreshold,
		Assets:             make([]mesh.CompatAsset, 0, len(cfg.Assets)),
		Markets:            make([]mesh.CompatMarket, 0, len(cfg.Markets)),
	}

	for _, assetConf := range cfg.Assets {
		symbol := strings.ToLower(assetConf.Symbol)
		assetID, found := dexpkg.BipSymbolID(symbol)
		if !found {
			return nil, fmt.Errorf("asset symbol %q unrecognized", assetConf.Symbol)
		}
		compatCfg.Assets = append(compatCfg.Assets, mesh.CompatAsset{
			ID:         assetID,
			Symbol:     symbol,
			MaxFeeRate: assetConf.MaxFeeRate,
			SwapConf:   assetConf.SwapConf,
			RegFee:     assetConf.RegFee,
			RegConfs:   assetConf.RegConfs,
			RegXPub:    assetConf.RegXPub,
			BondAmt:    assetConf.BondAmt,
			BondConfs:  assetConf.BondConfs,
		})
	}

	for _, mkt := range cfg.Markets {
		compatCfg.Markets = append(compatCfg.Markets, mesh.CompatMarket{
			Name:                   strings.ToLower(mkt.Name),
			Base:                   mkt.Base,
			Quote:                  mkt.Quote,
			LotSize:                mkt.LotSize,
			ParcelSize:             mkt.ParcelSize,
			RateStep:               mkt.RateStep,
			EpochDuration:          mkt.EpochDuration,
			MarketBuyBuffer:        mkt.MarketBuyBuffer,
			MaxUserCancelsPerEpoch: mkt.MaxUserCancelsPerEpoch,
		})
	}

	return mesh.NewCompatSnapshot(compatCfg)
}
