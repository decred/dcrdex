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
	chainIDs       []uint32
	sources        []*feeEstimateSource
	feeMtx         sync.RWMutex
	txFeeEstimates map[uint32]*Estimate

	listenersMtx sync.RWMutex
	listeners    map[string]chan<- map[uint32]*Estimate
}

// NewOracle returns a new instance of *Oracle.
func NewOracle(net dex.Network, cfg Config, chainsIDs []uint32) (*Oracle, error) {
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
		listeners:      map[string]chan<- map[uint32]*Estimate{},
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

// AddFeeListener adds a new tx fee listener. If the uniqueID already exists, it
// will be overridden by the new feeChan. feeChan MUST never be closed by
// callers, use RemoveFeeListener instead.
func (o *Oracle) AddFeeListener(uniqueID string, txFeeChan chan<- map[uint32]*Estimate) {
	o.listenersMtx.Lock()
	defer o.listenersMtx.Unlock()
	o.listeners[uniqueID] = txFeeChan
}

// RemoveFeeListener removes a tx fee listener and closes the channel.
func (o *Oracle) RemoveFeeListener(uniqueID string) {
	o.listenersMtx.Lock()
	defer o.listenersMtx.Unlock()
	feeChan, ok := o.listeners[uniqueID]
	if !ok {
		return
	}

	close(feeChan)
	delete(o.listeners, uniqueID)
}

// calculateAverage calculates the average fee estimates and distributes the
// result to all listeners. Returns indexes of newly reactivated sources that we
// need to fetch fee estimate from.
func (o *Oracle) calculateAverage() []int {
	var reActivatedSourceIndexes []int
	totalFeeEstimates := make(map[uint32]*FeeSourceCount)
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
				totalFeeEstimates[chainID] = new(FeeSourceCount)
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
		o.notifyListeners(broadCastTxFees)
	}

	return reActivatedSourceIndexes
}

func (o *Oracle) notifyListeners(feeEstimates map[uint32]*Estimate) {
	o.listenersMtx.RLock()
	defer o.listenersMtx.RUnlock()
	for _, feeChan := range o.listeners {
		feeChan <- feeEstimates
	}
}

// Run starts the tx fee oracle and block until the provided context is
// canceled.
func (o *Oracle) Run(ctx context.Context, log dex.Logger) {
	var wg sync.WaitGroup
	for i := range o.sources {
		source := o.sources[i]
		if source.isDisabled() {
			continue
		}

		o.fetchFromSource(ctx, log, source, &wg)

		estimates, err := source.getFeeEstimate(ctx, log, o.chainIDs)
		if err != nil {
			if isAuthError(err) {
				source.deactivate(false)
				log.Errorf("%s has been deactivated and cannot be auto reactivated due to %v", source.name, err)
			} else {
				log.Errorf("%s.getFeeEstimate error: %v", source.name, err)
			}
			continue
		}

		source.mtx.Lock()
		source.feeEstimates = estimates
		source.mtx.Unlock()
	}

	o.calculateAverage()

	wg.Add(1)
	go func() {
		defer wg.Done()

		ticker := time.NewTicker(defaultFeeRefreshInterval + 10*time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				reActivatedSourceIndexes := o.calculateAverage()
				if len(reActivatedSourceIndexes) > 0 {
					for _, index := range reActivatedSourceIndexes {
						// Start a new goroutine for this source.
						o.fetchFromSource(ctx, log, o.sources[index], &wg)
					}
				}
			}
		}
	}()

	wg.Wait()
}

func (o *Oracle) fetchFromSource(ctx context.Context, log dex.Logger, s *feeEstimateSource, wg *sync.WaitGroup) {
	wg.Add(1)
	go func() {
		defer wg.Done()

		ticker := time.NewTicker(s.refreshInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if s.isDisabled() {
					return
				}

				if s.hasFeeEstimates() && s.isExpired() {
					s.deactivate(true)
					log.Errorf("Fee estimate source %q has been disabled due to lack of fresh data. It will be re-enabled after %0.f hours.",
						s.name, reactivationDuration.Hours())
					return
				}

				estimates, err := s.getFeeEstimate(ctx, log, o.chainIDs)
				if err != nil {
					if isRateLimitError(err) {
						s.deactivate(true)
						log.Errorf("Fee estimate source %q has been disabled (Reason: %v). It will be re-enabled after %0.f hours.",
							s.name, err, reactivationDuration.Hours())
					} else {
						log.Errorf("%s.getFeeEstimate error: %v", s.name, err)
					}
					continue
				}

				s.mtx.Lock()
				s.feeEstimates = estimates
				s.lastRefresh = time.Now()
				s.mtx.Unlock()
			}
		}
	}()
}
