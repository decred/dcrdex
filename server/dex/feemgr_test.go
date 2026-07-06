// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package dex

import (
	"context"
	"errors"
	"testing"
	"time"

	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/server/asset"
)

// tFeeBackend implements only the asset.Backend methods that feeFetcher
// uses. Calls to anything else panic via the embedded nil interface.
type tFeeBackend struct {
	asset.Backend
	feeRate uint64
	feeErr  error
}

func (b *tFeeBackend) FeeRate(context.Context) (uint64, error) {
	return b.feeRate, b.feeErr
}

func tFeeFetcher(maxFeeRate, liveRate uint64) (*feeFetcher, *tFeeBackend) {
	be := &tFeeBackend{feeRate: liveRate}
	f := newFeeFetcher(&asset.BackedAsset{
		Asset: dex.Asset{
			Symbol:     "polygon",
			MaxFeeRate: maxFeeRate,
		},
		Backend: be,
	})
	return f, be
}

func TestSwapFeeRateLive(t *testing.T) {
	ctx := context.Background()

	// The assigned rate tracks the live estimate when it is below the
	// configured max, for all assets, including dynamic-tx-fee assets.
	f, be := tFeeFetcher(1000, 300)
	if r := f.SwapFeeRate(ctx); r != 300 {
		t.Fatalf("expected live rate 300, got %d", r)
	}

	// The configured max caps the assigned rate.
	be.feeRate = 5000
	// A rate increase is stashed: the pre-increase rate is returned once to
	// avoid racing clients that funded orders against the old rate.
	if r := f.SwapFeeRate(ctx); r != 300 {
		t.Fatalf("expected stashed rate 300 after increase, got %d", r)
	}
	// Age out the stash to observe the capped rate.
	f.stashedRate.Lock()
	f.stashedRate.stamp = time.Now().Add(-2 * stashedRateExpiry)
	f.stashedRate.Unlock()
	if r := f.SwapFeeRate(ctx); r != 1000 {
		t.Fatalf("expected capped rate 1000, got %d", r)
	}

	// A backend error yields zero, which the market's getFeeRate translates
	// to maxFeeRate.
	be.feeErr = errors.New("test error")
	if r := f.SwapFeeRate(ctx); r != 0 {
		t.Fatalf("expected 0 on backend error, got %d", r)
	}
	be.feeErr = nil

	// LastRate survives the error path from the last good fetch.
	if r := f.LastRate(); r != 1000 {
		t.Fatalf("expected last rate 1000, got %d", r)
	}
}
