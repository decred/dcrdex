// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

//go:build lgpl

package eth

import (
	"math"
	"math/big"
	"reflect"
	"testing"
)

func TestConversions(t *testing.T) {
	newBigIntFromStr := func(str string) *big.Int {
		t.Helper()
		bi, ok := new(big.Int).SetString(str, 10)
		if !ok {
			t.Fatalf("bad int str %v", str)
		}
		return bi
	}
	tests := []struct {
		name string
		gwei uint64
		wei  *big.Int
	}{
		{
			"ok",
			12,
			newBigIntFromStr("12000000000"),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if gotWei := GweiToWei(tt.gwei); !reflect.DeepEqual(gotWei, tt.wei) {
				t.Errorf("GweiToWei() = %v, want %v", gotWei, tt.wei)
			}
			if gotGwei := WeiToGwei(tt.wei); !reflect.DeepEqual(gotGwei, tt.gwei) {
				t.Errorf("WeiToGwei() = %v, want %v", gotGwei, tt.wei)
			}
		})

	}
}

func TestVersionedGases(t *testing.T) {
	type test struct {
		ver            uint32
		expRedeemGases []uint64
		expInitGases   []uint64
		expRefundGas   uint64
	}

	tests := []*test{
		{
			ver: 0,
			expInitGases: []uint64{
				0,
				v0Gases.Swap,
				v0Gases.Swap + v0Gases.SwapAdd,
				v0Gases.Swap + v0Gases.SwapAdd*2,
			},
			expRedeemGases: []uint64{
				0,
				v0Gases.Redeem,
				v0Gases.Redeem + v0Gases.RedeemAdd,
				v0Gases.Redeem + v0Gases.RedeemAdd*2,
			},
			expRefundGas: v0Gases.Refund,
		},
		{
			ver:            1,
			expInitGases:   []uint64{0, math.MaxUint64},
			expRedeemGases: []uint64{0, math.MaxUint64},
			expRefundGas:   math.MaxUint64,
		},
	}

	for _, tt := range tests {
		for n, expGas := range tt.expInitGases {
			gas := InitGas(n, tt.ver)
			if gas != expGas {
				t.Fatalf("wrong gas for %d inits, version %d. wanted %d, got %d", n, tt.ver, expGas, gas)
			}
		}
		for n, expGas := range tt.expRedeemGases {
			gas := RedeemGas(n, tt.ver)
			if gas != expGas {
				t.Fatalf("wrong gas for %d redeems, version %d. wanted %d, got %d", n, tt.ver, expGas, gas)
			}
		}
		gas := RefundGas(tt.ver)
		if gas != tt.expRefundGas {
			t.Fatalf("wrong gas for refund, version %d. wanted %d, got %d", tt.ver, tt.expRefundGas, gas)
		}
	}
}
