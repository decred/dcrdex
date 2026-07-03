package dex

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"decred.org/dcrdex/server/asset"
	"decred.org/dcrdex/server/market"
)

// FeeManager manages FeeFetchers.
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

// AddFetcher adds a FeeFetcher for an asset.
func (m *FeeManager) AddFetcher(asset *asset.BackedAsset) {
	m.fetchers[asset.ID] = newFeeFetcher(asset)
}

// FeeFetcher returns the specified asset's FeeFetcher.
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

	// erosionWarning rate-limits the operator warning that is logged when
	// the live fee estimate approaches the configured maxFeeRate.
	erosionWarning struct {
		sync.Mutex
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

// erosionWarningInterval limits how often the maxFeeRate erosion warning is
// logged per asset.
const erosionWarningInterval = 10 * time.Minute

// warnErosion logs an operator warning, at most once per
// erosionWarningInterval, that the live fee estimate is approaching the
// configured maxFeeRate. Once the estimate exceeds maxFeeRate, assigned swap
// fee rates are capped there, and swap transactions may not mine promptly.
func (f *feeFetcher) warnErosion(rate uint64) {
	f.erosionWarning.Lock()
	defer f.erosionWarning.Unlock()
	if time.Since(f.erosionWarning.stamp) < erosionWarningInterval {
		return
	}
	f.erosionWarning.stamp = time.Now()
	log.Warnf("Live %s fee rate estimate (%d) is above half of the configured maxFeeRate (%d). "+
		"If the estimate exceeds maxFeeRate, assigned swap fee rates will be capped there and swap "+
		"transactions may not mine promptly. Consider raising the %s maxFeeRate.",
		f.Symbol, rate, f.Asset.MaxFeeRate, f.Symbol)
}

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
	if r > f.Asset.MaxFeeRate/2 {
		f.warnErosion(r)
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

// SwapFeeRate returns the tx fee rate assigned to a match's swap transactions:
// the current market fee rate, capped at the configured maxFeeRate. The
// configured maxFeeRate is a reserve bound, not the assigned value.
//
// Historically, assets that support dynamic tx fees (EIP-1559) were assigned
// maxFeeRate itself, since an overshooting fee cap is refunded on-chain and a
// maximal cap made the assigned rate immune to fee movement between match
// time and broadcast time. But the maximal cap has off-chain costs that grow
// with the configured value: clients reserve funds per lot at this rate, gate
// order placement on it, and lock it into resting orders, so operators are
// pressured to configure it low, and a network fee spike above the configured
// value renders every swap transaction unminable. Assigning a live rate frees
// maxFeeRate to be set high as pure insurance. Fee movement after match time
// is instead handled client-side: the wallet may raise its fee cap above the
// assigned rate, up to its order reserves, when the base fee demands
// (ValidateFeeRate only enforces the assigned rate as a floor).
func (f *feeFetcher) SwapFeeRate(ctx context.Context) uint64 {
	return f.FeeRate(ctx)
}
