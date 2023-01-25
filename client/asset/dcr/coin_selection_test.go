package dcr

import (
	"math/rand"
	"reflect"
	"testing"

	walletjson "decred.org/dcrwallet/v2/rpc/jsonrpc/types"
)

func Test_subsetLargeBias(t *testing.T) {
	amt := uint64(10e8)
	newU := func(amt float64) *compositeUTXO {
		return &compositeUTXO{
			rpc: &walletjson.ListUnspentResult{Amount: amt},
		}
	}
	tests := []struct {
		name  string
		utxos []*compositeUTXO
		want  []*compositeUTXO
	}{
		{
			"1,3 exact",
			[]*compositeUTXO{newU(1), newU(8), newU(9)},
			[]*compositeUTXO{newU(1), newU(9)},
		},
		{
			"subset large bias",
			[]*compositeUTXO{newU(1), newU(3), newU(6), newU(7)},
			[]*compositeUTXO{newU(3), newU(7)},
		},
		{
			"subset large bias",
			[]*compositeUTXO{newU(1), newU(3), newU(5), newU(7), newU(8)},
			[]*compositeUTXO{newU(3), newU(8)},
		},
		{
			"insufficient",
			[]*compositeUTXO{newU(1), newU(8)},
			nil,
		},
		{
			"all exact",
			[]*compositeUTXO{newU(1), newU(9)},
			[]*compositeUTXO{newU(1), newU(9)},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := subsetLargeBias(amt, tt.utxos); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("subset() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_subsetSmallBias(t *testing.T) {
	amt := uint64(10e8)
	newU := func(amt float64) *compositeUTXO {
		return &compositeUTXO{
			rpc: &walletjson.ListUnspentResult{Amount: amt},
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
			[]*compositeUTXO{newU(8), newU(9)},
		},
		{
			"subset",
			[]*compositeUTXO{newU(1), newU(9), newU(11)},
			[]*compositeUTXO{newU(1), newU(9)},
		},
		{
			"subset small bias",
			[]*compositeUTXO{newU(1), newU(3), newU(6), newU(7)},
			[]*compositeUTXO{newU(1), newU(3), newU(6)},
		},
		{
			"subset small bias",
			[]*compositeUTXO{newU(1), newU(3), newU(5), newU(7), newU(8)},
			[]*compositeUTXO{newU(5), newU(7)},
		},
		{
			"ok nil",
			[]*compositeUTXO{newU(1), newU(8)},
			nil,
		},
		{
			"two, over",
			[]*compositeUTXO{newU(5), newU(7), newU(11)},
			[]*compositeUTXO{newU(5), newU(7)},
		},
		{
			"insufficient",
			[]*compositeUTXO{newU(1), newU(8)},
			nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := subsetSmallBias(amt, tt.utxos); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("subset() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_leastOverFund(t *testing.T) {
	amt := uint64(10e8)
	newU := func(amt float64) *compositeUTXO {
		return &compositeUTXO{
			rpc: &walletjson.ListUnspentResult{Amount: amt},
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
			[]*compositeUTXO{newU(1), newU(3), newU(6), newU(7)},
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
			if got := leastOverFund(amt, tt.utxos); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("subset() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Fuzz_leastOverFund(f *testing.F) {
	seeds := []struct {
		amt uint64
		n   int
	}{{
		amt: 200,
		n:   2,
	}, {
		amt: 20,
		n:   1,
	}, {
		amt: 20,
		n:   20,
	}, {
		amt: 2,
		n:   40,
	}}

	for _, seed := range seeds {
		f.Add(seed.amt, seed.n)
	}

	newU := func(amt float64) *compositeUTXO {
		return &compositeUTXO{
			rpc: &walletjson.ListUnspentResult{Amount: amt},
		}
	}

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
		leastOverFund(amt*1e8, utxos)
	})
}

func Test_utxoSetDiff(t *testing.T) {
	newU := func(amt float64) *compositeUTXO {
		return &compositeUTXO{
			rpc: &walletjson.ListUnspentResult{Amount: amt},
		}
	}

	all := []*compositeUTXO{newU(1), newU(3), newU(6), newU(7), newU(12)}

	sub := func(inds []int) []*compositeUTXO {
		out := make([]*compositeUTXO, len(inds))
		for i, ind := range inds {
			out[i] = all[ind]
		}
		return out
	}

	checkPtrs := func(a, b []*compositeUTXO) bool {
		if len(a) != len(b) {
			return false
		}
		for i := range a {
			if a[i] != b[i] {
				return false
			}
		}
		return true
	}

	tests := []struct {
		name string
		set  []*compositeUTXO
		sub  []*compositeUTXO
		want []*compositeUTXO
	}{
		{
			"one",
			sub([]int{0, 1}),
			sub([]int{1}),
			sub([]int{0}),
		}, {
			"none",
			sub([]int{0, 1}),
			sub([]int{2}),
			sub([]int{0, 1}),
		}, {
			"some",
			sub([]int{0, 1}),
			sub([]int{1, 2}),
			sub([]int{0}),
		}, {
			"both all / nil",
			sub([]int{0, 1}),
			sub([]int{0, 1}),
			nil,
		}, {
			"one all / nil",
			sub([]int{0}),
			sub([]int{0}),
			nil,
		}, {
			"bigger sub, all",
			sub([]int{0, 1}),
			sub([]int{0, 1, 2}),
			nil,
		}, {
			"nil set",
			nil,
			sub([]int{0, 1, 2}),
			nil,
		}, {
			"nil sub",
			sub([]int{0, 1, 2}),
			nil,
			sub([]int{0, 1, 2}),
		}, {
			"nil both",
			nil,
			nil,
			nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := utxoSetDiff(tt.set, tt.sub)
			if !checkPtrs(got, tt.want) {
				t.Errorf("utxoSetDiff() = %v, want %v", got, tt.want)
			}
		})
	}
}
