// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package txfee

import (
	"context"
	"math"
	"sort"
	"sync"
	"time"

	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/utils"
)

// FeeFetchFunc is a function that fetches a fee rate. If an error is
// encountered the FeeFetchFunc will indicate how long to wait until trying
// again.
type FeeFetchFunc func(ctx context.Context) (rate uint64, errDelay time.Duration, err error)

// SourceConfig defines a fee rate source.
type SourceConfig struct {
	F      FeeFetchFunc
	Name   string
	Period time.Duration
	// Rank controls which priority group the source is in. Lower Rank is higher
	// priority. Fee rates from similarly-ranked sources are grouped together
	// for a composite rate. Lower-ranked groups are not considered at all
	// until all sources from higher ranked groups are error or expired.
	Rank uint
}

type feeFetchSource struct {
	*SourceConfig

	log       dex.Logger
	rate      uint64
	stamp     time.Time
	failUntil time.Time
}

type FeeFetcher struct {
	log     dex.Logger
	c       chan uint64
	sources [][]*feeFetchSource
}

func groupedSources(sources []*SourceConfig, log dex.Logger) [][]*feeFetchSource {
	srcs := make([]*feeFetchSource, len(sources))
	for i, cfg := range sources {
		srcs[i] = &feeFetchSource{SourceConfig: cfg, log: log.SubLogger(cfg.Name)}
	}
	return groupSources(srcs)
}

func groupSources(sources []*feeFetchSource) [][]*feeFetchSource {
	groupedSources := make([][]*feeFetchSource, 0)
next:
	for _, src := range sources {
		for i, group := range groupedSources {
			if group[0].Rank == src.Rank {
				groupedSources[i] = append(group, src)
				continue next
			}
		}
		groupedSources = append(groupedSources, []*feeFetchSource{src})
	}
	sort.Slice(groupedSources, func(i, j int) bool {
		return groupedSources[i][0].Rank < groupedSources[j][0].Rank
	})
	return groupedSources
}

// NewFeeFetcher creates and returns a new FeeFetcher.
func NewFeeFetcher(sources []*SourceConfig, log dex.Logger) *FeeFetcher {
	return &FeeFetcher{
		log:     log,
		sources: groupedSources(sources, log),
		c:       make(chan uint64, 1),
	}
}

const (
	feeFetchTimeout       = time.Second * 10
	feeFetchDefaultTick   = time.Minute
	minFeeFetchErrorDelay = time.Minute
	// Fees are fully weighted for 5 minutes, then the weight decays to zero
	// over the next 10 minutes. Fee rates older than 15 minutes are not
	// considered valid.
	feeFetchFullValidityPeriod  = time.Minute * 5
	feeFetchValidityDecayPeriod = time.Minute * 10
	feeExpiration               = feeFetchFullValidityPeriod + feeFetchValidityDecayPeriod
)

func prioritizedFeeRate(sources [][]*feeFetchSource) uint64 {
	for _, group := range sources {
		var weightedRate float64
		var weight float64
		for _, src := range group {
			age := time.Since(src.stamp)
			if !src.failUntil.IsZero() || age >= feeExpiration {
				continue
			}
			if age < feeFetchFullValidityPeriod {
				weight += 1
				weightedRate += float64(src.rate)
				continue
			}
			decayAge := age - feeFetchFullValidityPeriod
			w := 1 - (float64(decayAge) / float64(feeFetchValidityDecayPeriod))
			weight += w
			weightedRate += w * float64(src.rate)
		}
		if weightedRate != 0 {
			return utils.Max(1, uint64(math.Round(weightedRate/weight)))
		}
	}
	return 0
}

func nextSource(sources [][]*feeFetchSource) (src *feeFetchSource, delay time.Duration) {
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

func (f *FeeFetcher) Connect(ctx context.Context) (*sync.WaitGroup, error) {
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

	updateSource := func(src *feeFetchSource) bool {
		ctx, cancel := context.WithTimeout(ctx, feeFetchTimeout)
		defer cancel()
		r, errDelay, err := src.F(ctx)
		if err != nil {
			src.log.Meter("fetch-error", time.Minute*30).Errorf("Fetch error: %v", err)
			src.failUntil = time.Now().Add(utils.Max(minFeeFetchErrorDelay, errDelay))
			return false
		}
		if r == 0 {
			src.log.Meter("zero-rate", time.Minute*30).Error("Fee rate of zero")
			src.failUntil = time.Now().Add(minFeeFetchErrorDelay)
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
				timeout = time.NewTimer(feeFetchDefaultTick)
			} else {
				timeout = time.NewTimer(utils.Max(0, delay))
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

func (f *FeeFetcher) Next() <-chan uint64 {
	return f.c
}
