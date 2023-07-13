package btc

import (
	"math/rand"
	"reflect"
	"sort"
	"testing"
	"time"

	dexbtc "decred.org/dcrdex/dex/networks/btc"
)

func Test_leastOverFund(t *testing.T) {
	enough := func(_, sum uint64) (bool, uint64) {
		return sum >= 10e8, 0
	}
	newU := func(amt float64) *compositeUTXO {
		return &compositeUTXO{
			utxo: &utxo{
				amount: uint64(amt) * 1e8,
			},
			input: &dexbtc.SpendInfo{},
		}
	}
	tests := []struct {
		name  string
		utxos []*compositeUTXO
		want  []*compositeUTXO
	}{
		{
			"1,3",
			[]*compositeUTXO{newU(1), newU(8), newU(9)},
			[]*compositeUTXO{newU(1), newU(9)},
		},
		{
			"1,2",
			[]*compositeUTXO{newU(1), newU(9)},
			[]*compositeUTXO{newU(1), newU(9)},
		},
		{
			"1,2++",
			[]*compositeUTXO{newU(2), newU(9)},
			[]*compositeUTXO{newU(2), newU(9)},
		},
		{
			"2,3++",
			[]*compositeUTXO{newU(0), newU(2), newU(9)},
			[]*compositeUTXO{newU(2), newU(9)},
		},
		{
			"3",
			[]*compositeUTXO{newU(0), newU(2), newU(10)},
			[]*compositeUTXO{newU(10)},
		},
		{
			"subset",
			[]*compositeUTXO{newU(1), newU(9), newU(11)},
			[]*compositeUTXO{newU(1), newU(9)},
		},
		{
			"subset small bias",
			[]*compositeUTXO{newU(3), newU(6), newU(7)},
			[]*compositeUTXO{newU(3), newU(7)},
		},
		{
			"single exception",
			[]*compositeUTXO{newU(5), newU(7), newU(11)},
			[]*compositeUTXO{newU(11)},
		},
		{
			"1 of 1",
			[]*compositeUTXO{newU(10)},
			[]*compositeUTXO{newU(10)},
		},
		{
			"ok nil",
			[]*compositeUTXO{newU(1), newU(8)},
			nil,
		},
		{
			"ok",
			[]*compositeUTXO{newU(1)},
			nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := leastOverFund(enough, tt.utxos)
			sort.Slice(got, func(i int, j int) bool {
				return got[i].amount < got[j].amount
			})
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("subset() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_leastOverFundWithLimit(t *testing.T) {
	enough := func(_, sum uint64) (bool, uint64) {
		return sum >= 10e8, 0
	}
	newU := func(amt float64) *compositeUTXO {
		return &compositeUTXO{
			utxo: &utxo{
				amount: uint64(amt) * 1e8,
			},
			input: &dexbtc.SpendInfo{},
		}
	}
	tests := []struct {
		name  string
		limit uint64
		utxos []*compositeUTXO
		want  []*compositeUTXO
	}{
		{
			"1,3",
			10e8,
			[]*compositeUTXO{newU(1), newU(8), newU(9)},
			[]*compositeUTXO{newU(1), newU(9)},
		},
		{
			"max fund too low",
			9e8,
			[]*compositeUTXO{newU(1), newU(8), newU(9)},
			nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := leastOverFundWithLimit(enough, tt.limit, tt.utxos)
			sort.Slice(got, func(i int, j int) bool {
				return got[i].amount < got[j].amount
			})
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("subset() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Fuzz_leastOverFund(f *testing.F) {
	type seed struct {
		amt uint64
		n   int
	}

	rnd := rand.New(rand.NewSource(1))

	seeds := make([]seed, 0, 40)
	for i := 0; i < 10; i++ {
		seeds = append(seeds, seed{
			amt: uint64(rnd.Intn(40)),
			n:   rnd.Intn(20000),
		})
	}

	for _, seed := range seeds {
		f.Add(seed.amt, seed.n)
	}

	newU := func(amt float64) *compositeUTXO {
		return &compositeUTXO{
			utxo: &utxo{
				amount: uint64(amt * 1e8),
			},
			input: &dexbtc.SpendInfo{},
		}
	}

	var totalDuration time.Duration
	var totalUTXO int64

	f.Fuzz(func(t *testing.T, amt uint64, n int) {
		if n < 1 || n > 65535 || amt == 0 || amt > 21e6 {
			t.Skip()
		}
		m := 2 * amt / uint64(n)
		utxos := make([]*compositeUTXO, n)
		for i := range utxos {
			var v float64
			if rand.Intn(2) > 0 {
				v = rand.Float64()
			}
			if m != 0 {
				v += float64(rand.Int63n(int64(m)))
			}
			if v > 40000 {
				t.Skip()
			}
			utxos[i] = newU(v)
		}
		startTime := time.Now()
		enough := func(_, sum uint64) (bool, uint64) {
			return sum >= amt*1e8, 0
		}
		leastOverFund(enough, utxos)
		totalDuration += time.Since(startTime)
		totalUTXO += int64(n)
	})

	f.Logf("leastOverFund: average duration: %v, with average number of UTXOs: %v\n", totalDuration/100, totalUTXO/100)
}

func BenchmarkLeastOverFund(b *testing.B) {
	// Same amounts every time.
	rnd := rand.New(rand.NewSource(1))
	utxos := make([]*compositeUTXO, 2_000)
	for i := range utxos {
		utxo := &compositeUTXO{
			utxo: &utxo{
				amount: uint64(rnd.Int31n(100) * 1e8),
			},
			input: &dexbtc.SpendInfo{},
		}
		utxos[i] = utxo
	}
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		enough := func(_, sum uint64) (bool, uint64) {
			return sum >= 10_000*1e8, 0
		}
		leastOverFund(enough, utxos)
	}
}
