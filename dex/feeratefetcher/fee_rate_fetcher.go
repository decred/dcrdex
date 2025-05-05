// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package feeratefetcher

import (
	"context"
	"math"
	"sort"
	"sync"
	"time"

	"decred.org/dcrdex/dex"
)

// FeeRateFetchFunc is a function that fetches a fee rate. If an error is
// encountered the FeeRateFetchFunc will indicate how long to wait until trying
// again.
type FeeRateFetchFunc func(ctx context.Context) (rate uint64, errDelay time.Duration, err error)

// SourceConfig defines a fee rate source.
type SourceConfig struct {
	F      FeeRateFetchFunc
	Name   string
	Period time.Duration
	// Rank controls which priority group the source is in. Lower Rank is higher
	// priority. Fee rates from similarly-ranked sources are grouped together
	// for a composite rate. Lower-ranked groups are not considered at all
	// until all sources from higher ranked groups are error or expired.
	Rank uint
}

type feeRateFetchSource struct {
	*SourceConfig

	log       dex.Logger
	rate      uint64
	stamp     time.Time
	failUntil time.Time
}

type FeeRateFetcher struct {
	log     dex.Logger
	c       chan uint64
	sources [][]*feeRateFetchSource
}

func groupedSources(sources []*SourceConfig, log dex.Logger) [][]*feeRateFetchSource {
	srcs := make([]*feeRateFetchSource, len(sources))
	for i, cfg := range sources {
		srcs[i] = &feeRateFetchSource{SourceConfig: cfg, log: log.SubLogger(cfg.Name)}
	}
	return groupSources(srcs)
}

func groupSources(sources []*feeRateFetchSource) [][]*feeRateFetchSource {
	groupedSources := make([][]*feeRateFetchSource, 0)
next:
	for _, src := range sources {
		for i, group := range groupedSources {
			if group[0].Rank == src.Rank {
				groupedSources[i] = append(group, src)
				continue next
			}
		}
		groupedSources = append(groupedSources, []*feeRateFetchSource{src})
	}
	sort.Slice(groupedSources, func(i, j int) bool {
		return groupedSources[i][0].Rank < groupedSources[j][0].Rank
	})
	return groupedSources
}

// NewFeeRateFetcher creates and returns a new FeeRateFetcher.
func NewFeeRateFetcher(sources []*SourceConfig, log dex.Logger) *FeeRateFetcher {
	return &FeeRateFetcher{
		log:     log,
		sources: groupedSources(sources, log),
		c:       make(chan uint64, 1),
	}
}

const (
	feeRateFetchTimeout       = time.Second * 10
	feeRateFetchDefaultTick   = time.Minute
	minFeeRateFetchErrorDelay = time.Minute
	// Fee Rates are fully weighted for 5 minutes, then the weight decays to zero
	// over the next 10 minutes. Fee rates older than 15 minutes are not
	// considered valid.
	feeRateFetchFullValidityPeriod  = time.Minute * 5
	feeRateFetchValidityDecayPeriod = time.Minute * 10
	feeRateExpiration               = feeRateFetchFullValidityPeriod + feeRateFetchValidityDecayPeriod
)

func prioritizedFeeRate(sources [][]*feeRateFetchSource) uint64 {
	for _, group := range sources {
		var weightedRate float64
		var weight float64
		for _, src := range group {
			age := time.Since(src.stamp)
			if !src.failUntil.IsZero() || age >= feeRateExpiration {
				continue
			}
			if age < feeRateFetchFullValidityPeriod {
				weight += 1
				weightedRate += float64(src.rate)
				continue
			}
			decayAge := age - feeRateFetchFullValidityPeriod
			w := 1 - (float64(decayAge) / float64(feeRateFetchValidityDecayPeriod))
			weight += w
			weightedRate += w * float64(src.rate)
		}
		if weightedRate != 0 {
			return max(1, uint64(math.Round(weightedRate/weight)))
		}
	}
	return 0
}

func nextSource(sources [][]*feeRateFetchSource) (src *feeRateFetchSource, delay time.Duration) {
	delay = time.Duration(math.MaxInt64)
	for _, group := range sources {
		for _, s := range group {
			if !s.failUntil.IsZero() {
				if until := time.Until(s.failUntil); until < delay {
					delay = until
					src = s
				}
			} else if until := s.Period - time.Since(s.stamp); until < delay {
				delay = until
				src = s
			}
		}
		if src != nil {
			return src, delay
		}
	}
	return
}

func (f *FeeRateFetcher) Connect(ctx context.Context) (*sync.WaitGroup, error) {
	reportCompositeRate := func() {
		r := prioritizedFeeRate(f.sources)
		if r == 0 {
			// Probably not possible if we have things right below.
			f.log.Critical("Goose egg for prioritized fee rate")
			return
		}
		select {
		case f.c <- r:
		default:
			f.log.Meter("blocking-channel", time.Minute*5).Errorf("Blocking report channel")
		}
	}

	updateSource := func(src *feeRateFetchSource) bool {
		ctx, cancel := context.WithTimeout(ctx, feeRateFetchTimeout)
		defer cancel()
		r, errDelay, err := src.F(ctx)
		if err != nil {
			src.log.Meter("fetch-error", time.Minute*30).Errorf("Fetch error: %v", err)
			src.failUntil = time.Now().Add(max(minFeeRateFetchErrorDelay, errDelay))
			return false
		}
		if r == 0 {
			src.log.Meter("zero-rate", time.Minute*30).Error("Fee rate of zero")
			src.failUntil = time.Now().Add(minFeeRateFetchErrorDelay)
			return false
		}
		src.failUntil = time.Time{}
		src.stamp = time.Now()
		src.rate = r
		src.log.Tracef("New rate %d", r)
		return true
	}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		// Prime the sources.
		var any bool
		for _, group := range f.sources {
			for _, src := range group {
				any = updateSource(src) || any
			}
		}
		if any {
			reportCompositeRate()
		}

		// Start the fetch loop.
		defer wg.Done()
		for {
			if ctx.Err() != nil {
				return
			}
			src, delay := nextSource(f.sources)
			var timeout *time.Timer
			if src == nil {
				f.log.Meter("all-failed", time.Minute*10).Error("All sources failed")
				timeout = time.NewTimer(feeRateFetchDefaultTick)
			} else {
				timeout = time.NewTimer(max(0, delay))
			}
			select {
			case <-timeout.C:
				if src == nil || !updateSource(src) {
					continue
				}
				reportCompositeRate()
			case <-ctx.Done():
				return
			}
		}
	}()

	return &wg, nil
}

func (f *FeeRateFetcher) Next() <-chan uint64 {
	return f.c
}
