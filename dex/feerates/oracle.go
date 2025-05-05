package feerates

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"decred.org/dcrdex/dex"
)

// Oracle provides fee rates for all configured assets from external sources.
// Fee rate estimate values are in atoms for dcr, gwei for ethereum, satoshis
// for bitcoin and bitcoin clone blockchains (per byte sat), or the lowest
// non-divisible unit in other non-Bitcoin blockchains.
type Oracle struct {
	chainIDs []uint32
	sources  []*feeRateEstimateSource

	feeRateMtx       sync.RWMutex
	feeRateEstimates map[uint32]*Estimate

	listener chan<- map[uint32]*Estimate
}

// NewOracle returns a new instance of *Oracle.
func NewOracle(net dex.Network, cfg Config, chainsIDs []uint32, listener chan<- map[uint32]*Estimate) (*Oracle, error) {
	if len(chainsIDs) == 0 {
		return nil, errors.New("provide chainIDs to fetch fee rate estimate for")
	}

	if net != dex.Mainnet && net != dex.Testnet {
		return nil, errors.New("fee rate estimate oracle is available for only mainnet and testnet")
	}

	o := &Oracle{
		chainIDs:         chainsIDs,
		sources:          feeRateEstimateSources(net, cfg),
		feeRateEstimates: make(map[uint32]*Estimate),
		listener:         listener,
	}

	for _, chainID := range chainsIDs {
		if sym := dex.BipIDSymbol(chainID); sym == "" {
			return nil, fmt.Errorf("chainID %d is invalid", chainID)
		}

		// Init chain.
		o.feeRateEstimates[chainID] = new(Estimate)
	}

	return o, nil
}

// FeeRateEstimates retrieves the current fee rate estimates.
func (o *Oracle) FeeRateEstimates() map[uint32]*Estimate {
	o.feeRateMtx.RLock()
	defer o.feeRateMtx.RUnlock()
	feeRateEstimates := make(map[uint32]*Estimate, len(o.feeRateEstimates))
	for chainID, feeEstimate := range o.feeRateEstimates {
		if feeEstimate.Value > 0 && time.Since(feeEstimate.LastUpdated) < FeeRateEstimateExpiry {
			fe := *feeEstimate
			feeRateEstimates[chainID] = &fe
		}
	}
	return feeRateEstimates
}

// calculateAverage calculates the average fee rate estimates and distributes the
// result to all listeners. Returns indexes of newly reactivated sources that we
// need to fetch fee rate estimate from.
func (o *Oracle) calculateAverage() []int {
	var reActivatedSourceIndexes []int
	totalFeeEstimates := make(map[uint32]*feeRateSourceCount)
	for i := range o.sources {
		source := o.sources[i]
		if source.isDisabled() {
			if source.checkIfSourceCanReactivate() {
				reActivatedSourceIndexes = append(reActivatedSourceIndexes, i)
			}
			continue
		}

		source.mtx.Lock()
		estimates := source.feeRateEstimates
		source.mtx.Unlock()

		for chainID, feeEstimate := range estimates {
			if feeEstimate == 0 {
				continue
			}

			if _, ok := totalFeeEstimates[chainID]; !ok {
				totalFeeEstimates[chainID] = new(feeRateSourceCount)
			}

			totalFeeEstimates[chainID].totalSource++
			totalFeeEstimates[chainID].totalFee += feeEstimate
		}
	}

	now := time.Now()
	o.feeRateMtx.Lock()
	broadCastFeeRates := make(map[uint32]*Estimate, len(o.feeRateEstimates))
	for chainID := range o.feeRateEstimates {
		if rateInfo := totalFeeEstimates[chainID]; rateInfo != nil {
			fee := rateInfo.totalFee / uint64(rateInfo.totalSource)
			if fee > 0 {
				o.feeRateEstimates[chainID].Value = fee
				o.feeRateEstimates[chainID].LastUpdated = now
				estimate := *o.feeRateEstimates[chainID]
				broadCastFeeRates[chainID] = &estimate
			}
		}
	}
	o.feeRateMtx.Unlock()

	// Notify all listeners if we have rates to broadcast.
	if len(broadCastFeeRates) > 0 {
		o.listener <- broadCastFeeRates
	}

	return reActivatedSourceIndexes
}

// Run starts the fee rates oracle and blocks until the provided context is
// canceled.
func (o *Oracle) Run(ctx context.Context, log dex.Logger) {
	nSuccessfulSources := o.fetchFromAllSource(ctx, log)
	if nSuccessfulSources == 0 {
		log.Errorf("Failed to retrieve fee rate estimate, exiting fee rate estimate oracle...")
		return
	}
	o.calculateAverage()

	ticker := time.NewTicker(defaultFeeRefreshInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return

		case <-ticker.C:
			o.fetchFromAllSource(ctx, log)
			o.calculateAverage()
		}
	}
}

// fetchFromAllSource retrieves fee rate estimates from all fee rate estimate sources and
// returns the number of sources that successfully returned a fee rate estimate.
func (o *Oracle) fetchFromAllSource(ctx context.Context, log dex.Logger) int {
	var nSuccessfulSources int
	for i := range o.sources {
		source := o.sources[i]
		if source.isDisabled() {
			continue
		}

		if source.hasFeeRateEstimates() && source.isExpired() {
			source.deactivate(true)
			log.Errorf("fee rate estimate source %q has been disabled due to lack of fresh data. It will be re-enabled after %0.f hours.",
				source.name, reactivationDuration.Hours())
			continue
		}

		estimates, err := source.getFeeRateEstimate(ctx, log, o.chainIDs)
		if err != nil {
			if isAuthError(err) {
				source.deactivate(false)
				log.Errorf("%s has been deactivated and cannot be auto reactivated due to %v", source.name, err)
			} else if isRateLimitError(err) {
				source.deactivate(true)
				log.Errorf("fee rate estimate source %q has been disabled (Reason: %v). It will be re-enabled after %0.f hours.",
					source.name, err, reactivationDuration.Hours())
			} else {
				log.Errorf("%s.getFeeEstimate error: %v", source.name, err)
			}
			continue
		}

		nSuccessfulSources++
		source.mtx.Lock()
		source.feeRateEstimates = estimates
		source.lastRefresh = time.Now()
		source.mtx.Unlock()
	}

	return nSuccessfulSources
}
