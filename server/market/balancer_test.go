// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package market

import (
	"testing"

	"decred.org/dcrdex/dex/calc"
)

func TestBalancer(t *testing.T) {
	const lotSize = 1e10
	const assetID = 123
	assetInfo := &assetETH.Asset
	perRedeemCost := assetInfo.RedeemSize * assetInfo.MaxFeeRate

	swapper := &tMatchNegotiator{}
	tunnel := &TMarketTunnel{}
	backend := &tAccountBackend{}

	balancer := &DEXBalancer{
		tunnels: map[string]MarketTunnel{
			"abc_xyz": tunnel,
		},
		assets: map[uint32]*backedBalancer{
			assetID: {
				balancer:  backend,
				assetInfo: assetInfo,
			},
		},
		matchNegotiator: swapper,
	}

	type qlr struct {
		qty, lots uint64
		redeems   int
	}

	type test struct {
		name    string
		pass    bool
		bal     uint64
		new     qlr
		mkt     qlr
		swapper qlr
	}

	tests := []*test{
		{
			name: "no existing - 1 new lot - pass",
			new:  qlr{lotSize, 1, 0},
			bal:  calc.RequiredOrderFunds(lotSize, 0, 1, assetInfo),
			pass: true,
		}, {
			name: "no existing - 1 new lot - fail",
			new:  qlr{lotSize, 1, 0},
			bal:  calc.RequiredOrderFunds(lotSize, 0, 1, assetInfo) - 1,
		}, {
			name: "no existing - 1 new redeem - pass",
			new:  qlr{0, 0, 1},
			bal:  perRedeemCost,
			pass: true,
		}, {
			name: "no existing - 1 new redeem - fail",
			new:  qlr{0, 0, 1},
			bal:  perRedeemCost - 1,
		}, {
			name:    "1 each swapper - 1 new lot - pass",
			new:     qlr{lotSize, 1, 0},
			swapper: qlr{lotSize, 1, 1},
			bal:     calc.RequiredOrderFunds(lotSize*2, 0, 2, assetInfo) + perRedeemCost,
			pass:    true,
		}, {
			name: "1 each market - 1 new lot - pass",
			new:  qlr{lotSize, 1, 0},
			mkt:  qlr{lotSize, 1, 1},
			bal:  calc.RequiredOrderFunds(lotSize*2, 0, 2, assetInfo) + perRedeemCost,
			pass: true,
		},
		{
			name: "1 each market - 1 new lot - fail",
			new:  qlr{lotSize, 1, 0},
			mkt:  qlr{lotSize, 1, 1},
			bal:  calc.RequiredOrderFunds(lotSize*2, 0, 2, assetInfo) + perRedeemCost - 1,
		},
		{
			name:    "mix it up - pass",
			new:     qlr{lotSize * 2, 2, 0},
			mkt:     qlr{lotSize * 3, 3, 2},
			swapper: qlr{lotSize * 4, 4, 3},
			bal:     calc.RequiredOrderFunds(lotSize*9, 0, 9, assetInfo) + perRedeemCost*5,
			pass:    true,
		},
		{
			name:    "mix it up - fail",
			new:     qlr{lotSize * 2, 2, 0},
			mkt:     qlr{lotSize * 3, 3, 2},
			swapper: qlr{lotSize * 4, 4, 3},
			bal:     calc.RequiredOrderFunds(lotSize*9, 0, 9, assetInfo) + perRedeemCost*5 - 1,
		},
	}

	for _, tt := range tests {
		tunnel.acctLots = tt.mkt.lots
		tunnel.acctQty = tt.mkt.qty
		tunnel.acctRedeems = tt.mkt.redeems
		swapper.swaps = tt.swapper.lots
		swapper.qty = tt.swapper.qty
		swapper.redeems = tt.swapper.redeems
		backend.bal = tt.bal

		if balancer.CheckBalance("a", assetID, tt.new.qty, tt.new.lots, tt.new.redeems) != tt.pass {
			t.Fatalf("%s: expected %t, got %t", tt.name, tt.pass, !tt.pass)
		}
	}
}
