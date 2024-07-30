package txfee

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"decred.org/dcrdex/dex"
)

// Oracle provides transaction fees for all configured assets from external
// sources. Fee estimate values are in atoms for dcr, gwei for ethereum,
// satoshis for bitcoin and bitcoin clone blockchains (per byte sat), or the
// lowest non-divisible unit in other non-Bitcoin blockchains.
type Oracle struct {
	chainIDs []uint32
	sources  []*feeEstimateSource

	feeMtx         sync.RWMutex
	txFeeEstimates map[uint32]*Estimate

	listener chan<- map[uint32]*Estimate
}

// NewOracle returns a new instance of *Oracle.
func NewOracle(net dex.Network, cfg Config, chainsIDs []uint32, listener chan<- map[uint32]*Estimate) (*Oracle, error) {
	if len(chainsIDs) == 0 {
		return nil, errors.New("provide chainIDs to fetch fee estimate for")
	}

	if net != dex.Mainnet && net != dex.Testnet {
		return nil, errors.New("fee estimate oracle is available for only mainnet and testnet")
	}

	o := &Oracle{
		chainIDs:       chainsIDs,
		sources:        feeEstimateSources(net, cfg),
		txFeeEstimates: make(map[uint32]*Estimate),
		listener:       listener,
	}

	for _, chainID := range chainsIDs {
		if sym := dex.BipIDSymbol(chainID); sym == "" {
			return nil, fmt.Errorf("chainID %d is invalid", chainID)
		}

		// Init chain.
		o.txFeeEstimates[chainID] = new(Estimate)
	}

	return o, nil
}

// FeeEstimates retrieves the current fee estimates.
func (o *Oracle) FeeEstimates() map[uint32]*Estimate {
	o.feeMtx.RLock()
	defer o.feeMtx.RUnlock()
	feeEstimates := make(map[uint32]*Estimate, len(o.txFeeEstimates))
	for chainID, feeEstimate := range o.txFeeEstimates {
		if feeEstimate.Value > 0 && time.Since(feeEstimate.LastUpdated) < FeeEstimateExpiry {
			fe := *feeEstimate
			feeEstimates[chainID] = &fe
		}
	}
	return feeEstimates
}

// calculateAverage calculates the average fee estimates and distributes the
// result to all listeners. Returns indexes of newly reactivated sources that we
// need to fetch fee estimate from.
func (o *Oracle) calculateAverage() []int {
	var reActivatedSourceIndexes []int
	totalFeeEstimates := make(map[uint32]*feeSourceCount)
	for i := range o.sources {
		source := o.sources[i]
		if source.isDisabled() {
			if source.checkIfSourceCanReactivate() {
				reActivatedSourceIndexes = append(reActivatedSourceIndexes, i)
			}
			continue
		}

		source.mtx.Lock()
		estimates := source.feeEstimates
		source.mtx.Unlock()

		for chainID, feeEstimate := range estimates {
			if feeEstimate == 0 {
				continue
			}

			if _, ok := totalFeeEstimates[chainID]; !ok {
				totalFeeEstimates[chainID] = new(feeSourceCount)
			}

			totalFeeEstimates[chainID].totalSource++
			totalFeeEstimates[chainID].totalFee += feeEstimate
		}
	}

	now := time.Now()
	o.feeMtx.Lock()
	broadCastTxFees := make(map[uint32]*Estimate, len(o.txFeeEstimates))
	for chainID := range o.txFeeEstimates {
		if rateInfo := totalFeeEstimates[chainID]; rateInfo != nil {
			fee := rateInfo.totalFee / uint64(rateInfo.totalSource)
			if fee > 0 {
				o.txFeeEstimates[chainID].Value = fee
				o.txFeeEstimates[chainID].LastUpdated = now
				estimate := *o.txFeeEstimates[chainID]
				broadCastTxFees[chainID] = &estimate
			}
		}
	}
	o.feeMtx.Unlock()

	// Notify all listeners if we have rates to broadcast.
	if len(broadCastTxFees) > 0 {
		o.listener <- broadCastTxFees
	}

	fmt.Println(broadCastTxFees)

	return reActivatedSourceIndexes
}

// Run starts the tx fee oracle and blocks until the provided context is
// canceled.
func (o *Oracle) Run(ctx context.Context, log dex.Logger) {
	nSuccessfulSources := o.fetchFromAllSource(ctx, log)
	if nSuccessfulSources == 0 {
		log.Errorf("Failed to retrieve fee estimate, exiting fee estimate oracle...")
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

// fetchFromAllSource retrieves fee estimates from all fee estimate sources and
// returns the number of sources that successfully returned a fee estimate.
func (o *Oracle) fetchFromAllSource(ctx context.Context, log dex.Logger) int {
	var nSuccessfulSources int
	for i := range o.sources {
		source := o.sources[i]
		if source.isDisabled() {
			continue
		}

		if source.hasFeeEstimates() && source.isExpired() {
			source.deactivate(true)
			log.Errorf("Fee estimate source %q has been disabled due to lack of fresh data. It will be re-enabled after %0.f hours.",
				source.name, reactivationDuration.Hours())
			continue
		}

		estimates, err := source.getFeeEstimate(ctx, log, o.chainIDs)
		if err != nil {
			if isAuthError(err) {
				source.deactivate(false)
				log.Errorf("%s has been deactivated and cannot be auto reactivated due to %v", source.name, err)
			} else if isRateLimitError(err) {
				source.deactivate(true)
				log.Errorf("Fee estimate source %q has been disabled (Reason: %v). It will be re-enabled after %0.f hours.",
					source.name, err, reactivationDuration.Hours())
			} else {
				log.Errorf("%s.getFeeEstimate error: %v", source.name, err)
			}
			continue
		}

		nSuccessfulSources++
		source.mtx.Lock()
		source.feeEstimates = estimates
		source.lastRefresh = time.Now()
		source.mtx.Unlock()
	}

	return nSuccessfulSources
}
