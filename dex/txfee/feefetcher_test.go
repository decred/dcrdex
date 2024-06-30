package txfee

import (
	"testing"
	"time"

	"decred.org/dcrdex/dex"
)

func TestGroupedSources(t *testing.T) {
	log := dex.StdOutLogger("T", dex.LevelTrace)
	checkSort := func(sources []*SourceConfig, groupSizes ...int) {
		t.Helper()
		groups := groupedSources(sources, log)
		if len(groups) != len(groupSizes) {
			t.Fatalf("expected %d groups, got %d", len(groupSizes), len(groups))
		}
		for i, group := range groups {
			if len(group) != groupSizes[i] {
				t.Fatalf("%d'th group has %d members. expected %d", i, len(group), groupSizes[i])
			}
		}
	}
	sources := func(ranks ...uint) (srcs []*SourceConfig) {
		for _, rank := range ranks {
			srcs = append(srcs, &SourceConfig{Rank: rank})
		}
		return
	}
	checkSort(sources(1, 1), 2)
	checkSort(sources(1, 2), 1, 1)
	checkSort(sources(1, 1, 2), 2, 1)
	checkSort(sources(4, 4, 4, 2, 1, 1), 2, 1, 3)
}

func TestPrioritizedFeeRate(t *testing.T) {
	feeFetchers := func(priorityRates [][2]uint) [][]*feeFetchSource {
		sources := make([]*feeFetchSource, len(priorityRates))
		for i, rr := range priorityRates {
			rank, rate := rr[0], rr[1]
			sources[i] = &feeFetchSource{
				SourceConfig: &SourceConfig{Rank: rank},
				rate:         uint64(rate),
				stamp:        time.Now(),
			}
		}
		return groupSources(sources)
	}

	var fetchers [][]*feeFetchSource
	checkRate := func(expRate uint64) {
		t.Helper()
		if r := prioritizedFeeRate(fetchers); r != expRate {
			t.Fatalf("expected rate %d, got %d", expRate, r)
		}
	}

	// just one
	fetchers = feeFetchers([][2]uint{
		{1, 10},
	})
	checkRate(10)

	// good until expired
	f0 := fetchers[0][0]
	f0.stamp = time.Now().Add(-feeExpiration + time.Second*10)
	checkRate(10)
	// expired
	f0.stamp = time.Now().Add(-feeExpiration)
	checkRate(0)

	// two at different priorities
	fetchers = feeFetchers([][2]uint{
		{1, 10},
		{2, 20},
	})
	// Only first is considered if not expired or failed.
	checkRate(10)
	// expire first
	f0, f1 := fetchers[0][0], fetchers[1][0]
	f0.stamp = time.Now().Add(-feeExpiration)
	checkRate(20)
	// first failed
	f0.stamp = time.Now()
	f0.failUntil = time.Now()
	checkRate(20)
	// both failed
	f1.failUntil = time.Now()
	checkRate(0)

	// first group has multiple
	fetchers = feeFetchers([][2]uint{
		{1, 10}, {1, 30},
		{2, 250},
	})
	checkRate(20)
	// expire second from group 1
	f0, f1 = fetchers[0][0], fetchers[0][1]
	f1.failUntil = time.Now()
	checkRate(10)
	// second from group 1 half-decayed
	f1.failUntil = time.Time{}
	f1.stamp = time.Now().Add(-1 * (feeFetchFullValidityPeriod + (feeFetchValidityDecayPeriod / 2)))
	checkRate(17) // (10 + (0.5 * 30)) / 1.5 = 16.6666
	// first from group 1  decayed by 75%
	f1.stamp = time.Now()
	f0.stamp = time.Now().Add(-1 * (feeFetchFullValidityPeriod + (feeFetchValidityDecayPeriod * 3 / 4)))
	checkRate(26) // (30 + (0.25 * 10)) / 1.25 = 26
	// group 1 unusable
	f0.failUntil = time.Now()
	f1.failUntil = time.Now()
	checkRate(250)
}
