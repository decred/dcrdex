// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package market

import (
	"testing"

	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/calc"
)

func TestBalancer(t *testing.T) {
	const lotSize = 1e10

	swapper := tNewMatchNegotiator()

	ethTunnel := &TMarketTunnel{base: assetETH.ID}
	ethBackend := &tAccountBackend{}
	ethRedeemCost := assetETH.RedeemSize * assetETH.MaxFeeRate

	tokenTunnel := &TMarketTunnel{quote: assetToken.ID}
	tokenBackend := &tAccountBackend{}
	// tokenRedeemCost := assetToken.RedeemSize * assetToken.MaxFeeRate

	ethBalancer := &backedBalancer{
		balancer:  ethBackend,
		assetInfo: &assetETH.Asset,
		markets: []PendingAccounter{
			ethTunnel,
		},
		feeFamily: map[uint32]*dex.Asset{
			assetToken.ID: &assetToken.Asset,
		},
	}

	balancer := &DEXBalancer{
		assets: map[uint32]*backedBalancer{
			assetETH.ID: ethBalancer,
			assetToken.ID: {
				balancer:  tokenBackend,
				assetInfo: &assetToken.Asset,
				markets: []PendingAccounter{
					tokenTunnel,
				},
				feeBalancer: ethBalancer,
				feeFamily: map[uint32]*dex.Asset{
					assetETH.ID: &assetETH.Asset,
				},
			},
		},
		matchNegotiator: swapper,
	}

	ethNineFive := calc.RequiredOrderFunds(lotSize*9, 0, 9, &assetETH.Asset) + ethRedeemCost*5

	type qlr struct {
		qty, lots uint64
		redeems   int
	}

	type assetParams struct {
		bal     uint64
		new     qlr
		mkt     qlr
		swapper qlr
	}

	type test struct {
		name     string
		pass     bool
		eth      assetParams
		token    assetParams
		useToken bool
		redeemID uint32
	}

	tests := []*test{
		{
			name: "no existing - 1 new lot - pass",
			eth: assetParams{
				new: qlr{lotSize, 1, 0},
				bal: calc.RequiredOrderFunds(lotSize, 0, 1, &assetETH.Asset),
			},
			pass: true,
		}, {
			name: "no existing - 1 new lot - fail",
			eth: assetParams{
				new: qlr{lotSize, 1, 0},
				bal: calc.RequiredOrderFunds(lotSize, 0, 1, &assetETH.Asset) - 1,
			},
		}, {
			name: "no existing - 1 new redeem - pass",
			eth: assetParams{
				new: qlr{0, 0, 1},
				bal: ethRedeemCost,
			},
			pass: true,
		}, {
			name: "no existing - 1 new redeem - fail",
			eth: assetParams{
				new: qlr{0, 0, 1},
				bal: ethRedeemCost - 1,
			},
		}, {
			name: "1 each swapper - 1 new lot - pass",
			eth: assetParams{
				new:     qlr{lotSize, 1, 0},
				swapper: qlr{lotSize, 1, 1},
				bal:     calc.RequiredOrderFunds(lotSize*2, 0, 2, &assetETH.Asset) + ethRedeemCost,
			},
			pass: true,
		}, {
			name: "1 each market - 1 new lot - pass",
			eth: assetParams{
				new: qlr{lotSize, 1, 0},
				mkt: qlr{lotSize, 1, 1},
				bal: calc.RequiredOrderFunds(lotSize*2, 0, 2, &assetETH.Asset) + ethRedeemCost,
			},
			pass: true,
		},
		{
			name: "1 each market - 1 new lot - fail",
			eth: assetParams{
				new: qlr{lotSize, 1, 0},
				mkt: qlr{lotSize, 1, 1},
				bal: calc.RequiredOrderFunds(lotSize*2, 0, 2, &assetETH.Asset) + ethRedeemCost - 1,
			},
		},
		{
			name: "mix it up - pass",
			eth: assetParams{
				new:     qlr{lotSize * 2, 2, 0},
				mkt:     qlr{lotSize * 3, 3, 2},
				swapper: qlr{lotSize * 4, 4, 3},
				bal:     ethNineFive,
			},
			pass: true,
		},
		{
			name: "mix it up - fail",
			eth: assetParams{
				new:     qlr{lotSize * 2, 2, 0},
				mkt:     qlr{lotSize * 3, 3, 2},
				swapper: qlr{lotSize * 4, 4, 3},
				bal:     ethNineFive - 1,
			},
		},
		{
			name: "mix it up - with tokens - pass",
			eth: assetParams{
				new:     qlr{lotSize * 2, 2, 0},
				mkt:     qlr{lotSize * 3, 3, 2},
				swapper: qlr{lotSize * 4, 4, 3},
				bal:     ethNineFive + assetToken.MaxFeeRate*(assetToken.SwapSize+assetToken.RedeemSize),
			},
			token: assetParams{
				mkt: qlr{lotSize * 5000, 1, 1}, // qty doesn't shouldn't matter.
			},
			pass: true,
		},
		{
			name: "mix it up - with tokens - fail",
			eth: assetParams{
				new:     qlr{lotSize * 2, 2, 0},
				mkt:     qlr{lotSize * 3, 3, 2},
				swapper: qlr{lotSize * 4, 4, 3},
				bal:     ethNineFive + assetToken.MaxFeeRate*(assetToken.SwapSize+assetToken.RedeemSize) - 1,
			},
			token: assetParams{
				mkt: qlr{lotSize * 5000, 1, 1},
			},
		}, {
			name:     "1 new lot token - pass",
			useToken: true,
			token: assetParams{
				new: qlr{lotSize, 1, 0},
				bal: lotSize,
			},
			eth: assetParams{
				bal: assetToken.SwapSize * assetToken.MaxFeeRate,
			},
			pass: true,
		}, {
			name:     "1 new lot token - fail insufficient fees",
			useToken: true,
			token: assetParams{
				new: qlr{lotSize, 1, 0},
				bal: lotSize,
			},
			eth: assetParams{
				bal: assetToken.SwapSize*assetToken.MaxFeeRate - 1,
			},
		}, {
			name:     "1 new lot token - fail insufficient balance",
			useToken: true,
			token: assetParams{
				new: qlr{lotSize, 1, 0},
				bal: lotSize - 1,
			},
			eth: assetParams{
				bal: assetToken.SwapSize * assetToken.MaxFeeRate,
			},
		}, {
			name:     "1 new lot token - family redeem - pass",
			useToken: true,
			token: assetParams{
				new: qlr{lotSize, 1, 0},
				bal: lotSize,
			},
			eth: assetParams{
				bal: assetToken.SwapSize*assetToken.MaxFeeRate + assetETH.RedeemSize*assetETH.MaxFeeRate,
			},
			redeemID: assetETH.ID,
			pass:     true,
		}, {
			name:     "1 new lot token - family redeem - fail insufficient fees",
			useToken: true,
			token: assetParams{
				new: qlr{2 * lotSize, 1, 0},
				bal: lotSize,
			},
			eth: assetParams{
				bal: assetToken.SwapSize*assetToken.MaxFeeRate + assetETH.RedeemSize*assetETH.MaxFeeRate - 1,
			},
			redeemID: assetETH.ID,
		}, {
			name:     "2 new lot token - family redeem + 2 pending token redeems - pass",
			useToken: true,
			token: assetParams{
				new: qlr{2 * lotSize, 2, 0},
				mkt: qlr{0, 0, 2},
				bal: 2 * lotSize,
			},
			eth: assetParams{
				bal: 2*(assetToken.SwapSize*assetToken.MaxFeeRate+assetETH.RedeemSize*assetETH.MaxFeeRate) +
					2*assetToken.RedeemSize*assetToken.MaxFeeRate,
			},
			redeemID: assetETH.ID,
			pass:     true,
		}, {
			name:     "2 new lot token - family redeem + 2 pending token redeems - fail insufficient fees",
			useToken: true,
			token: assetParams{
				new: qlr{2 * lotSize, 2, 0},
				mkt: qlr{0, 0, 2},
				bal: 2 * lotSize,
			},
			eth: assetParams{
				bal: 2*(assetToken.SwapSize*assetToken.MaxFeeRate+assetETH.RedeemSize*assetETH.MaxFeeRate) +
					2*assetToken.RedeemSize*assetToken.MaxFeeRate - 1,
			},
			redeemID: assetETH.ID,
		},
		{
			name:     "2 new lot token - family redeem + 2 pending token redeems - fail insufficient balance",
			useToken: true,
			token: assetParams{
				new: qlr{2 * lotSize, 2, 0},
				mkt: qlr{0, 0, 2},
				bal: 2*lotSize - 1,
			},
			eth: assetParams{
				bal: 2*(assetToken.SwapSize*assetToken.MaxFeeRate+assetETH.RedeemSize*assetETH.MaxFeeRate) +
					2*assetToken.RedeemSize*assetToken.MaxFeeRate,
			},
			redeemID: assetETH.ID,
		},
	}

	for _, tt := range tests {
		ethTunnel.acctLots = tt.eth.mkt.lots
		ethTunnel.acctQty = tt.eth.mkt.qty
		ethTunnel.acctRedeems = tt.eth.mkt.redeems
		swapper.swaps[assetETH.ID] = tt.eth.swapper.lots
		swapper.qty[assetETH.ID] = tt.eth.swapper.qty
		swapper.redeems[assetETH.ID] = tt.eth.swapper.redeems
		ethBackend.bal = tt.eth.bal

		tokenTunnel.acctLots = tt.token.mkt.lots
		tokenTunnel.acctQty = tt.token.mkt.qty
		tokenTunnel.acctRedeems = tt.token.mkt.redeems
		swapper.swaps[assetToken.ID] = tt.token.swapper.lots
		swapper.qty[assetToken.ID] = tt.token.swapper.qty
		swapper.redeems[assetToken.ID] = tt.token.swapper.redeems
		tokenBackend.bal = tt.token.bal

		assetID := assetETH.ID
		newQLR := tt.eth.new
		if tt.useToken {
			assetID = assetToken.ID
			newQLR = tt.token.new
		}

		if balancer.CheckBalance("a", assetID, tt.redeemID, newQLR.qty, newQLR.lots, newQLR.redeems) != tt.pass {
			t.Fatalf("%s: expected %t, got %t", tt.name, tt.pass, !tt.pass)
		}
	}
}
