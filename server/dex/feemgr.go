package dex

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"decred.org/dcrdex/server/asset"
	"decred.org/dcrdex/server/market"
)

// FeeManager manages fee fetchers and a fee cache.
type FeeManager struct {
	fetchers map[uint32]*feeFetcher
}

var _ market.FeeSource = (*FeeManager)(nil)

// NewFeeManager is the constructor for a FeeManager.
func NewFeeManager() *FeeManager {
	return &FeeManager{
		fetchers: make(map[uint32]*feeFetcher),
	}
}

// AddFetcher adds a fee fetcher (a *BackedAsset) and primes the cache. The
// asset's MaxFeeRate are used to limit the rates returned by the LastRate
// method as well as the rates returned by child FeeFetchers.
func (m *FeeManager) AddFetcher(asset *asset.BackedAsset) {
	m.fetchers[asset.ID] = newFeeFetcher(asset)
}

// FeeFetcher creates and returns an asset-specific fetcher that satisfies
// market.FeeFetcher, implemented by *feeFetcher.
func (m *FeeManager) FeeFetcher(assetID uint32) market.FeeFetcher {
	return m.fetchers[assetID]
}

// LastRate is the last rate cached for the specified asset.
func (m *FeeManager) LastRate(assetID uint32) uint64 {
	return m.fetchers[assetID].LastRate()
}

// feeFetcher implements market.FeeFetcher and updates the last fee rate cache.
type feeFetcher struct {
	*asset.BackedAsset
	lastRate *uint64

	// Stash the old rate for a short time to avoid a race condition where
	// a client gets a rate right before the server gets a higher rate, then the
	// client tries to use the old rate and is rejected.
	stashedRate struct {
		sync.RWMutex
		rate  uint64
		stamp time.Time
	}
}

var _ market.FeeFetcher = (*feeFetcher)(nil)

// newFeeFetcher is the constructor for a *feeFetcher.
func newFeeFetcher(asset *asset.BackedAsset) *feeFetcher {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	r, err := asset.Backend.FeeRate(ctx)
	if err != nil {
		log.Warnf("Error priming fee cache for %s: %v", asset.Symbol, err)
	}
	if r > asset.MaxFeeRate {
		r = asset.MaxFeeRate
	}
	return &feeFetcher{
		BackedAsset: asset,
		lastRate:    &r,
	}
}

// Use the lower rate for a minute after a rate increase to avoid races.
const stashedRateExpiry = time.Minute

// FeeRate fetches a new fee rate and updates the cache.
func (f *feeFetcher) FeeRate(ctx context.Context) uint64 {
	r, err := f.Backend.FeeRate(ctx)
	if err != nil {
		log.Errorf("Error retrieving fee rate for %s: %v", f.Symbol, err)
		return 0 // Do not store as last rate.
	}
	if r <= 0 {
		return 0
	}
	if r > f.Asset.MaxFeeRate {
		r = f.Asset.MaxFeeRate
	}
	oldRate := atomic.SwapUint64(f.lastRate, r)
	if oldRate < r {
		f.stashedRate.Lock()
		f.stashedRate.rate = oldRate
		f.stashedRate.stamp = time.Now()
		f.stashedRate.Unlock()
		return oldRate
	}
	f.stashedRate.RLock()
	if time.Since(f.stashedRate.stamp) < stashedRateExpiry && f.stashedRate.rate < r {
		r = f.stashedRate.rate
	}
	f.stashedRate.RUnlock()
	return r
}

// LastRate is the last rate cached. This may be used as a fallback if FeeRate
// times out, or as a quick rate when rate freshness is not critical.
func (f *feeFetcher) LastRate() uint64 {
	r := atomic.LoadUint64(f.lastRate)
	f.stashedRate.RLock()
	if time.Since(f.stashedRate.stamp) < stashedRateExpiry && f.stashedRate.rate < r {
		r = f.stashedRate.rate
	}
	f.stashedRate.RUnlock()
	return r
}

// MaxFeeRate is a getter for the BackedAsset's dex.Asset.MaxFeeRate. This is
// provided so consumers that operate on the returned FeeRate can respect the
// configured limit e.g. ScaleFeeRate in (*Market).processReadyEpoch.
func (f *feeFetcher) MaxFeeRate() uint64 {
	return f.Asset.MaxFeeRate
}

// SwapFeeRate returns the tx fee that needs to be used to initiate a swap.
// This fee will be the max fee rate if the asset supports dynamic tx fees,
// and otherwise it will be the current market fee rate.
func (f *feeFetcher) SwapFeeRate(ctx context.Context) uint64 {
	if f.Backend.Info().SupportsDynamicTxFee {
		return f.MaxFeeRate()
	}
	return f.FeeRate(ctx)
}
