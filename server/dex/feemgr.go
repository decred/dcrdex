package dex

import (
	"context"
	"strconv"
	"sync/atomic"
	"time"

	"decred.org/dcrdex/server/asset"
	"decred.org/dcrdex/server/market"
)

// FeeManager manages fee fetchers and a fee cache.
type FeeManager struct {
	assets map[uint32]*asset.BackedAsset
	cache  map[uint32]*uint64
}

var _ market.FeeSource = (*FeeManager)(nil)

// NewFeeManager is the constructor for a FeeManager.
func NewFeeManager() *FeeManager {
	return &FeeManager{
		assets: make(map[uint32]*asset.BackedAsset),
		cache:  make(map[uint32]*uint64),
	}
}

// AddFetcher adds a fee fetcher (a *BackedAsset) and primes the cache. The
// asset's MaxFeeRate are used to limit the rates returned by the LastRate
// method as well as the rates returned by child FeeFetchers.
func (m *FeeManager) AddFetcher(asset *asset.BackedAsset) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	rate, err := asset.Backend.FeeRate(ctx)
	if err != nil {
		log.Warnf("Error priming fee cache for %s: %v", asset.Symbol, err)
	}
	if rate > asset.MaxFeeRate {
		rate = asset.MaxFeeRate
	}
	m.cache[asset.ID] = &rate
	m.assets[asset.ID] = asset
}

// FeeFetcher creates and returns an asset-specific fetcher that satisfies
// market.FeeFetcher, implemented by *feeFetcher.
func (m *FeeManager) FeeFetcher(assetID uint32) market.FeeFetcher {
	asset := m.assets[assetID]
	if asset == nil {
		panic("no fetcher for " + strconv.Itoa(int(assetID)))
	}
	return newFeeFetcher(asset, m.cache[assetID])
}

// LastRate is the last rate cached for the specified asset.
func (m *FeeManager) LastRate(assetID uint32) uint64 {
	r := m.cache[assetID]
	if r == nil {
		return 0
	}
	return atomic.LoadUint64(r)
}

// feeFetcher implements market.FeeFetcher and updates the last fee rate cache.
type feeFetcher struct {
	*asset.BackedAsset
	lastRate *uint64
}

var _ market.FeeFetcher = (*feeFetcher)(nil)

// newFeeFetcher is the constructor for a *feeFetcher.
func newFeeFetcher(asset *asset.BackedAsset, lastRate *uint64) *feeFetcher {
	return &feeFetcher{
		BackedAsset: asset,
		lastRate:    lastRate,
	}
}

// FeeRate fetches a new fee rate and updates the cache.
func (f *feeFetcher) FeeRate(ctx context.Context) uint64 {
	r, err := f.Backend.FeeRate(ctx)
	if err != nil {
		log.Errorf("Error retrieving fee rate for %s: %v", f.Symbol, err)
		return 0 // Do not store as last rate.
	}
	if r > f.Asset.MaxFeeRate {
		r = f.Asset.MaxFeeRate
	}
	atomic.StoreUint64(f.lastRate, r)
	return r
}

// LastRate is the last rate cached. This may be used as a fallback if FeeRate
// times out, or as a quick rate when rate freshness is not critical.
func (f *feeFetcher) LastRate() uint64 {
	return atomic.LoadUint64(f.lastRate)
}

// MaxFeeRate is a getter for the BackedAsset's dex.Asset.MaxFeeRate. This is
// provided so consumers that operate on the returned FeeRate can respect the
// configured limit e.g. ScaleFeeRate in (*Market).processReadyEpoch.
func (f *feeFetcher) MaxFeeRate() uint64 {
	return f.Asset.MaxFeeRate
}
