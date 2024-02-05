package mm

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/client/core"
	"decred.org/dcrdex/client/mm/libxc"
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/calc"
	"decred.org/dcrdex/dex/encode"
	"decred.org/dcrdex/dex/order"
)

var log = dex.StdOutLogger("T", dex.LevelTrace)

type vwapResult struct {
	avg     uint64
	extrema uint64
}

type dexOrder struct {
	lots, rate uint64
	sell       bool
}

type cexOrder struct {
	baseID, quoteID uint32
	qty, rate       uint64
	sell            bool
}

type withdrawArgs struct {
	address string
	amt     uint64
	assetID uint32
}

type tCEX struct {
	bidsVWAP             map[uint64]vwapResult
	asksVWAP             map[uint64]vwapResult
	vwapErr              error
	balances             map[uint32]*libxc.ExchangeBalance
	balanceErr           error
	tradeID              string
	tradeErr             error
	lastTrade            *cexOrder
	cancelledTrades      []string
	cancelTradeErr       error
	tradeUpdates         chan *libxc.TradeUpdate
	tradeUpdatesID       int
	lastConfirmDepositTx string
	confirmDepositAmt    uint64
	depositConfirmed     bool
	depositAddress       string
	withdrawAmt          uint64
	withdrawTxID         string
	lastWithdrawArgs     *withdrawArgs
}

func newTCEX() *tCEX {
	return &tCEX{
		bidsVWAP:        make(map[uint64]vwapResult),
		asksVWAP:        make(map[uint64]vwapResult),
		balances:        make(map[uint32]*libxc.ExchangeBalance),
		cancelledTrades: make([]string, 0),
		tradeUpdates:    make(chan *libxc.TradeUpdate),
	}
}

var _ libxc.CEX = (*tCEX)(nil)

func (c *tCEX) Connect(ctx context.Context) (*sync.WaitGroup, error) {
	return &sync.WaitGroup{}, nil
}
func (c *tCEX) Balances() (map[uint32]*libxc.ExchangeBalance, error) {
	return nil, nil
}
func (c *tCEX) Markets(ctx context.Context) ([]*libxc.Market, error) {
	return nil, nil
}
func (c *tCEX) Balance(assetID uint32) (*libxc.ExchangeBalance, error) {
	return c.balances[assetID], c.balanceErr
}
func (c *tCEX) Trade(ctx context.Context, baseID, quoteID uint32, sell bool, rate, qty uint64, updaterID int) (string, error) {
	if c.tradeErr != nil {
		return "", c.tradeErr
	}
	c.lastTrade = &cexOrder{baseID, quoteID, qty, rate, sell}
	return c.tradeID, nil
}
func (c *tCEX) CancelTrade(ctx context.Context, seID, quoteID uint32, tradeID string) error {
	if c.cancelTradeErr != nil {
		return c.cancelTradeErr
	}
	c.cancelledTrades = append(c.cancelledTrades, tradeID)
	return nil
}
func (c *tCEX) SubscribeMarket(ctx context.Context, baseID, quoteID uint32) error {
	return nil
}
func (c *tCEX) UnsubscribeMarket(baseID, quoteID uint32) error {
	return nil
}
func (c *tCEX) VWAP(baseID, quoteID uint32, sell bool, qty uint64) (vwap, extrema uint64, filled bool, err error) {
	if c.vwapErr != nil {
		return 0, 0, false, c.vwapErr
	}

	if sell {
		res, found := c.asksVWAP[qty]
		if !found {
			return 0, 0, false, nil
		}
		return res.avg, res.extrema, true, nil
	}

	res, found := c.bidsVWAP[qty]
	if !found {
		return 0, 0, false, nil
	}
	return res.avg, res.extrema, true, nil
}
func (c *tCEX) SubscribeTradeUpdates() (<-chan *libxc.TradeUpdate, func(), int) {
	return c.tradeUpdates, func() {}, c.tradeUpdatesID
}
func (c *tCEX) SubscribeCEXUpdates() (<-chan interface{}, func()) {
	return nil, func() {}
}
func (c *tCEX) GetDepositAddress(ctx context.Context, assetID uint32) (string, error) {
	return c.depositAddress, nil
}

func (c *tCEX) Withdraw(ctx context.Context, assetID uint32, qty uint64, address string, onComplete func(uint64, string)) error {
	c.lastWithdrawArgs = &withdrawArgs{
		address: address,
		amt:     qty,
		assetID: assetID,
	}
	onComplete(c.withdrawAmt, c.withdrawTxID)
	return nil
}

func (c *tCEX) ConfirmDeposit(ctx context.Context, txID string, onConfirm func(bool, uint64)) {
	c.lastConfirmDepositTx = txID
	onConfirm(c.depositConfirmed, c.confirmDepositAmt)
}

type tWrappedCEX struct {
	bidsVWAP         map[uint64]vwapResult
	asksVWAP         map[uint64]vwapResult
	vwapErr          error
	balances         map[uint32]*libxc.ExchangeBalance
	balanceErr       error
	tradeID          string
	tradeErr         error
	lastTrade        *cexOrder
	cancelledTrades  []string
	cancelTradeErr   error
	tradeUpdates     chan *libxc.TradeUpdate
	lastWithdrawArgs *withdrawArgs
	lastDepositArgs  *withdrawArgs
	confirmDeposit   func()
	confirmWithdraw  func()
}

func newTWrappedCEX() *tWrappedCEX {
	return &tWrappedCEX{
		bidsVWAP:        make(map[uint64]vwapResult),
		asksVWAP:        make(map[uint64]vwapResult),
		balances:        make(map[uint32]*libxc.ExchangeBalance),
		cancelledTrades: make([]string, 0),
		tradeUpdates:    make(chan *libxc.TradeUpdate),
	}
}

var _ cex = (*tWrappedCEX)(nil)

func (c *tWrappedCEX) Balance(assetID uint32) (*libxc.ExchangeBalance, error) {
	return c.balances[assetID], c.balanceErr
}
func (c *tWrappedCEX) CancelTrade(ctx context.Context, baseID, quoteID uint32, tradeID string) error {
	if c.cancelTradeErr != nil {
		return c.cancelTradeErr
	}
	c.cancelledTrades = append(c.cancelledTrades, tradeID)
	return nil
}
func (c *tWrappedCEX) SubscribeMarket(ctx context.Context, baseID, quoteID uint32) error {
	return nil
}
func (c *tWrappedCEX) SubscribeTradeUpdates() (updates <-chan *libxc.TradeUpdate, unsubscribe func()) {
	return c.tradeUpdates, func() {}
}
func (c *tWrappedCEX) Trade(ctx context.Context, baseID, quoteID uint32, sell bool, rate, qty uint64) (string, error) {
	if c.tradeErr != nil {
		return "", c.tradeErr
	}
	c.lastTrade = &cexOrder{baseID, quoteID, qty, rate, sell}
	return c.tradeID, nil
}
func (c *tWrappedCEX) VWAP(baseID, quoteID uint32, sell bool, qty uint64) (vwap, extrema uint64, filled bool, err error) {
	if c.vwapErr != nil {
		return 0, 0, false, c.vwapErr
	}

	if sell {
		res, found := c.asksVWAP[qty]
		if !found {
			return 0, 0, false, nil
		}
		return res.avg, res.extrema, true, nil
	}

	res, found := c.bidsVWAP[qty]
	if !found {
		return 0, 0, false, nil
	}
	return res.avg, res.extrema, true, nil
}
func (c *tWrappedCEX) Deposit(ctx context.Context, assetID uint32, amount uint64, onConfirm func()) error {
	c.lastDepositArgs = &withdrawArgs{
		assetID: assetID,
		amt:     amount,
	}
	c.confirmDeposit = onConfirm
	return nil
}
func (c *tWrappedCEX) Withdraw(ctx context.Context, assetID uint32, amount uint64, onConfirm func()) error {
	c.lastWithdrawArgs = &withdrawArgs{
		assetID: assetID,
		amt:     amount,
	}
	c.confirmWithdraw = onConfirm
	return nil
}

func TestArbRebalance(t *testing.T) {
	mkt := &core.Market{
		LotSize: uint64(40 * 1e8),
	}

	orderIDs := make([]order.OrderID, 5)
	for i := 0; i < 5; i++ {
		copy(orderIDs[i][:], encode.RandomBytes(32))
	}

	cexTradeIDs := make([]string, 0, 5)
	for i := 0; i < 5; i++ {
		cexTradeIDs = append(cexTradeIDs, fmt.Sprintf("%x", encode.RandomBytes(32)))
	}

	log := dex.StdOutLogger("T", dex.LevelTrace)

	var currEpoch uint64 = 100
	var numEpochsLeaveOpen uint32 = 10
	var maxActiveArbs uint32 = 5
	var profitTrigger float64 = 0.01

	type testBooks struct {
		dexBidsAvg     []uint64
		dexBidsExtrema []uint64

		dexAsksAvg     []uint64
		dexAsksExtrema []uint64

		cexBidsAvg     []uint64
		cexBidsExtrema []uint64

		cexAsksAvg     []uint64
		cexAsksExtrema []uint64
	}

	noArbBooks := &testBooks{
		dexBidsAvg:     []uint64{1.8e6, 1.7e6},
		dexBidsExtrema: []uint64{1.7e6, 1.6e6},

		dexAsksAvg:     []uint64{2e6, 2.5e6},
		dexAsksExtrema: []uint64{2e6, 3e6},

		cexBidsAvg:     []uint64{1.9e6, 1.8e6},
		cexBidsExtrema: []uint64{1.85e6, 1.75e6},

		cexAsksAvg:     []uint64{2.1e6, 2.2e6},
		cexAsksExtrema: []uint64{2.2e6, 2.3e6},
	}

	arbBuyOnDEXBooks := &testBooks{
		dexBidsAvg:     []uint64{1.8e6, 1.7e6},
		dexBidsExtrema: []uint64{1.7e6, 1.6e6},

		dexAsksAvg:     []uint64{2e6, 2.5e6},
		dexAsksExtrema: []uint64{2e6, 3e6},

		cexBidsAvg:     []uint64{2.3e6, 2.1e6},
		cexBidsExtrema: []uint64{2.2e6, 1.9e6},

		cexAsksAvg:     []uint64{2.4e6, 2.6e6},
		cexAsksExtrema: []uint64{2.5e6, 2.7e6},
	}

	arbSellOnDEXBooks := &testBooks{
		cexBidsAvg:     []uint64{1.8e6, 1.7e6},
		cexBidsExtrema: []uint64{1.7e6, 1.6e6},

		cexAsksAvg:     []uint64{2e6, 2.5e6},
		cexAsksExtrema: []uint64{2e6, 3e6},

		dexBidsAvg:     []uint64{2.3e6, 2.1e6},
		dexBidsExtrema: []uint64{2.2e6, 1.9e6},

		dexAsksAvg:     []uint64{2.4e6, 2.6e6},
		dexAsksExtrema: []uint64{2.5e6, 2.7e6},
	}

	arb2LotsBuyOnDEXBooks := &testBooks{
		dexBidsAvg:     []uint64{1.8e6, 1.7e6},
		dexBidsExtrema: []uint64{1.7e6, 1.6e6},

		dexAsksAvg:     []uint64{2e6, 2e6, 2.5e6},
		dexAsksExtrema: []uint64{2e6, 2e6, 3e6},

		cexBidsAvg:     []uint64{2.3e6, 2.2e6, 2.1e6},
		cexBidsExtrema: []uint64{2.2e6, 2.2e6, 1.9e6},

		cexAsksAvg:     []uint64{2.4e6, 2.6e6},
		cexAsksExtrema: []uint64{2.5e6, 2.7e6},
	}

	arb2LotsSellOnDEXBooks := &testBooks{
		cexBidsAvg:     []uint64{1.8e6, 1.7e6},
		cexBidsExtrema: []uint64{1.7e6, 1.6e6},

		cexAsksAvg:     []uint64{2e6, 2e6, 2.5e6},
		cexAsksExtrema: []uint64{2e6, 2e6, 3e6},

		dexBidsAvg:     []uint64{2.3e6, 2.2e6, 2.1e6},
		dexBidsExtrema: []uint64{2.2e6, 2.2e6, 1.9e6},

		dexAsksAvg:     []uint64{2.4e6, 2.6e6},
		dexAsksExtrema: []uint64{2.5e6, 2.7e6},
	}

	// Arbing 2 lots worth would still be above profit trigger, but the
	// second lot on its own would not be.
	arb2LotsButOneWorth := &testBooks{
		dexBidsAvg:     []uint64{1.8e6, 1.7e6},
		dexBidsExtrema: []uint64{1.7e6, 1.6e6},

		dexAsksAvg:     []uint64{2e6, 2.1e6},
		dexAsksExtrema: []uint64{2e6, 2.2e6},

		cexBidsAvg:     []uint64{2.3e6, 2.122e6},
		cexBidsExtrema: []uint64{2.2e6, 2.1e6},

		cexAsksAvg:     []uint64{2.4e6, 2.6e6},
		cexAsksExtrema: []uint64{2.5e6, 2.7e6},
	}

	type assetAmt struct {
		assetID uint32
		amt     uint64
	}

	type test struct {
		name          string
		books         *testBooks
		dexMaxSell    *core.MaxOrderEstimate
		dexMaxBuy     *core.MaxOrderEstimate
		dexMaxSellErr error
		dexMaxBuyErr  error
		// The strategy uses maxSell/maxBuy to determine how much it can trade.
		// dexBalances is just used for auto rebalancing.
		dexBalances           map[uint32]uint64
		cexBalances           map[uint32]*libxc.ExchangeBalance
		dexVWAPErr            error
		cexVWAPErr            error
		cexTradeErr           error
		existingArbs          []*arbSequence
		pendingBaseRebalance  bool
		pendingQuoteRebalance bool

		autoRebalance *AutoRebalanceConfig

		expectedDexOrder   *dexOrder
		expectedCexOrder   *cexOrder
		expectedDEXCancels []dex.Bytes
		expectedCEXCancels []string
		expectedWithdrawal *assetAmt
		expectedDeposit    *assetAmt
	}

	tests := []test{
		// "no arb"
		{
			name:  "no arb",
			books: noArbBooks,
			dexMaxSell: &core.MaxOrderEstimate{
				Swap: &asset.SwapEstimate{
					Lots: 5,
				},
			},
			dexMaxBuy: &core.MaxOrderEstimate{
				Swap: &asset.SwapEstimate{
					Lots: 5,
				},
			},
			cexBalances: map[uint32]*libxc.ExchangeBalance{
				0:  {Available: 1e19},
				42: {Available: 1e19},
			},
		},
		// "1 lot, buy on dex, sell on cex"
		{
			name:  "1 lot, buy on dex, sell on cex",
			books: arbBuyOnDEXBooks,
			dexMaxSell: &core.MaxOrderEstimate{
				Swap: &asset.SwapEstimate{
					Lots: 5,
				},
			},
			dexMaxBuy: &core.MaxOrderEstimate{
				Swap: &asset.SwapEstimate{
					Lots: 5,
				},
			},
			cexBalances: map[uint32]*libxc.ExchangeBalance{
				0:  {Available: 1e19},
				42: {Available: 1e19},
			},
			expectedDexOrder: &dexOrder{
				lots: 1,
				rate: 2e6,
				sell: false,
			},
			expectedCexOrder: &cexOrder{
				baseID:  42,
				quoteID: 0,
				qty:     mkt.LotSize,
				rate:    2.2e6,
				sell:    true,
			},
		},
		// "1 lot, buy on dex, sell on cex, but dex base balance not enough"
		{
			name:  "1 lot, buy on dex, sell on cex, but cex base balance not enough",
			books: arbBuyOnDEXBooks,
			dexMaxSell: &core.MaxOrderEstimate{
				Swap: &asset.SwapEstimate{
					Lots: 5,
				},
			},
			dexMaxBuy: &core.MaxOrderEstimate{
				Swap: &asset.SwapEstimate{
					Lots: 5,
				},
			},
			cexBalances: map[uint32]*libxc.ExchangeBalance{
				0:  {Available: 1e19},
				42: {Available: mkt.LotSize / 2},
			},
		},
		// "2 lot, buy on dex, sell on cex, but dex quote balance only enough for 1"
		{
			name:  "2 lot, buy on dex, sell on cex, but dex quote balance only enough for 1",
			books: arb2LotsBuyOnDEXBooks,
			dexMaxSell: &core.MaxOrderEstimate{
				Swap: &asset.SwapEstimate{
					Lots: 5,
				},
			},
			dexMaxBuy: &core.MaxOrderEstimate{
				Swap: &asset.SwapEstimate{
					Lots: 1,
				},
			},

			cexBalances: map[uint32]*libxc.ExchangeBalance{
				0:  {Available: 1e19},
				42: {Available: 1e19},
			},

			expectedDexOrder: &dexOrder{
				lots: 1,
				rate: 2e6,
				sell: false,
			},
			expectedCexOrder: &cexOrder{
				baseID:  42,
				quoteID: 0,
				qty:     mkt.LotSize,
				rate:    2.2e6,
				sell:    true,
			},
		},
		// "1 lot, sell on dex, buy on cex"
		{
			name:  "1 lot, sell on dex, buy on cex",
			books: arbSellOnDEXBooks,
			dexMaxSell: &core.MaxOrderEstimate{
				Swap: &asset.SwapEstimate{
					Lots: 5,
				},
			},
			dexMaxBuy: &core.MaxOrderEstimate{
				Swap: &asset.SwapEstimate{
					Lots: 5,
				},
			},
			cexBalances: map[uint32]*libxc.ExchangeBalance{
				0:  {Available: 1e19},
				42: {Available: 1e19},
			},
			expectedDexOrder: &dexOrder{
				lots: 1,
				rate: 2.2e6,
				sell: true,
			},
			expectedCexOrder: &cexOrder{
				baseID:  42,
				quoteID: 0,
				qty:     mkt.LotSize,
				rate:    2e6,
				sell:    false,
			},
		},
		// "2 lot, buy on cex, sell on dex, but cex quote balance only enough for 1"
		{
			name:  "2 lot, buy on cex, sell on dex, but cex quote balance only enough for 1",
			books: arb2LotsSellOnDEXBooks,
			dexMaxSell: &core.MaxOrderEstimate{
				Swap: &asset.SwapEstimate{
					Lots: 5,
				},
			},
			dexMaxBuy: &core.MaxOrderEstimate{
				Swap: &asset.SwapEstimate{
					Lots: 5,
				},
			},
			cexBalances: map[uint32]*libxc.ExchangeBalance{
				0:  {Available: calc.BaseToQuote(2e6, mkt.LotSize*3/2)},
				42: {Available: 1e19},
			},
			expectedDexOrder: &dexOrder{
				lots: 1,
				rate: 2.2e6,
				sell: true,
			},
			expectedCexOrder: &cexOrder{
				baseID:  42,
				quoteID: 0,
				qty:     mkt.LotSize,
				rate:    2e6,
				sell:    false,
			},
		},
		// "1 lot, sell on dex, buy on cex"
		{
			name:  "1 lot, sell on dex, buy on cex",
			books: arbSellOnDEXBooks,
			dexMaxSell: &core.MaxOrderEstimate{
				Swap: &asset.SwapEstimate{
					Lots: 5,
				},
			},
			dexMaxBuy: &core.MaxOrderEstimate{
				Swap: &asset.SwapEstimate{
					Lots: 5,
				},
			},
			cexBalances: map[uint32]*libxc.ExchangeBalance{
				0:  {Available: 1e19},
				42: {Available: 1e19},
			},
			expectedDexOrder: &dexOrder{
				lots: 1,
				rate: 2.2e6,
				sell: true,
			},
			expectedCexOrder: &cexOrder{
				baseID:  42,
				quoteID: 0,
				qty:     mkt.LotSize,
				rate:    2e6,
				sell:    false,
			},
		},
		// "2 lots arb still above profit trigger, but second not worth it on its own"
		{
			name:  "2 lots arb still above profit trigger, but second not worth it on its own",
			books: arb2LotsButOneWorth,
			dexMaxSell: &core.MaxOrderEstimate{
				Swap: &asset.SwapEstimate{
					Lots: 5,
				},
			},
			dexMaxBuy: &core.MaxOrderEstimate{
				Swap: &asset.SwapEstimate{
					Lots: 5,
				},
			},
			cexBalances: map[uint32]*libxc.ExchangeBalance{
				0:  {Available: 1e19},
				42: {Available: 1e19},
			},
			expectedDexOrder: &dexOrder{
				lots: 1,
				rate: 2e6,
				sell: false,
			},
			expectedCexOrder: &cexOrder{
				baseID:  42,
				quoteID: 0,
				qty:     mkt.LotSize,
				rate:    2.2e6,
				sell:    true,
			},
		},
		// "2 lot, buy on cex, sell on dex, but cex quote balance only enough for 1"
		{
			name:  "2 lot, buy on cex, sell on dex, but cex quote balance only enough for 1",
			books: arb2LotsSellOnDEXBooks,
			dexMaxSell: &core.MaxOrderEstimate{
				Swap: &asset.SwapEstimate{
					Lots: 5,
				},
			},
			dexMaxBuy: &core.MaxOrderEstimate{
				Swap: &asset.SwapEstimate{
					Lots: 5,
				},
			},
			cexBalances: map[uint32]*libxc.ExchangeBalance{
				0:  {Available: calc.BaseToQuote(2e6, mkt.LotSize*3/2)},
				42: {Available: 1e19},
			},
			expectedDexOrder: &dexOrder{
				lots: 1,
				rate: 2.2e6,
				sell: true,
			},
			expectedCexOrder: &cexOrder{
				baseID:  42,
				quoteID: 0,
				qty:     mkt.LotSize,
				rate:    2e6,
				sell:    false,
			},
		},
		// "cex no asks"
		{
			name: "cex no asks",
			books: &testBooks{
				dexBidsAvg:     []uint64{1.8e6, 1.7e6},
				dexBidsExtrema: []uint64{1.7e6, 1.6e6},

				dexAsksAvg:     []uint64{2e6, 2.5e6},
				dexAsksExtrema: []uint64{2e6, 3e6},

				cexBidsAvg:     []uint64{1.9e6, 1.8e6},
				cexBidsExtrema: []uint64{1.85e6, 1.75e6},

				cexAsksAvg:     []uint64{},
				cexAsksExtrema: []uint64{},
			},
			dexMaxSell: &core.MaxOrderEstimate{
				Swap: &asset.SwapEstimate{
					Lots: 5,
				},
			},
			dexMaxBuy: &core.MaxOrderEstimate{
				Swap: &asset.SwapEstimate{
					Lots: 5,
				},
			},

			cexBalances: map[uint32]*libxc.ExchangeBalance{
				0:  {Available: 1e19},
				42: {Available: 1e19},
			},
		},
		// "dex no asks"
		{
			name: "dex no asks",
			books: &testBooks{
				dexBidsAvg:     []uint64{1.8e6, 1.7e6},
				dexBidsExtrema: []uint64{1.7e6, 1.6e6},

				dexAsksAvg:     []uint64{},
				dexAsksExtrema: []uint64{},

				cexBidsAvg:     []uint64{1.9e6, 1.8e6},
				cexBidsExtrema: []uint64{1.85e6, 1.75e6},

				cexAsksAvg:     []uint64{2.1e6, 2.2e6},
				cexAsksExtrema: []uint64{2.2e6, 2.3e6},
			},
			dexMaxSell: &core.MaxOrderEstimate{
				Swap: &asset.SwapEstimate{
					Lots: 5,
				},
			},
			dexMaxBuy: &core.MaxOrderEstimate{
				Swap: &asset.SwapEstimate{
					Lots: 5,
				},
			},

			cexBalances: map[uint32]*libxc.ExchangeBalance{
				0:  {Available: 1e19},
				42: {Available: 1e19},
			},
		},
		// "dex max sell error"
		{
			name:  "dex max sell error",
			books: arbSellOnDEXBooks,
			dexMaxBuy: &core.MaxOrderEstimate{
				Swap: &asset.SwapEstimate{
					Lots: 5,
				},
			},
			cexBalances: map[uint32]*libxc.ExchangeBalance{
				0:  {Available: 1e19},
				42: {Available: 1e19},
			},
			dexMaxSellErr: errors.New(""),
		},
		//  "dex max buy error"
		{
			name:  "dex max buy error",
			books: arbBuyOnDEXBooks,
			dexMaxSell: &core.MaxOrderEstimate{
				Swap: &asset.SwapEstimate{
					Lots: 5,
				},
			},
			cexBalances: map[uint32]*libxc.ExchangeBalance{
				0:  {Available: 1e19},
				42: {Available: 1e19},
			},
			dexMaxBuyErr: errors.New(""),
		},
		// "dex vwap error"
		{
			name:  "dex vwap error",
			books: arbBuyOnDEXBooks,
			dexMaxBuy: &core.MaxOrderEstimate{
				Swap: &asset.SwapEstimate{
					Lots: 5,
				},
			},
			dexMaxSell: &core.MaxOrderEstimate{
				Swap: &asset.SwapEstimate{
					Lots: 5,
				},
			},
			cexBalances: map[uint32]*libxc.ExchangeBalance{
				0:  {Available: 1e19},
				42: {Available: 1e19},
			},
			dexVWAPErr: errors.New(""),
		},
		// "cex vwap error"
		{
			name:  "cex vwap error",
			books: arbBuyOnDEXBooks,
			dexMaxBuy: &core.MaxOrderEstimate{
				Swap: &asset.SwapEstimate{
					Lots: 5,
				},
			},
			dexMaxSell: &core.MaxOrderEstimate{
				Swap: &asset.SwapEstimate{
					Lots: 5,
				},
			},
			cexBalances: map[uint32]*libxc.ExchangeBalance{
				0:  {Available: 1e19},
				42: {Available: 1e19},
			},
			cexVWAPErr: errors.New(""),
		},
		// "self-match"
		{
			name:  "self-match",
			books: arbSellOnDEXBooks,
			dexMaxSell: &core.MaxOrderEstimate{
				Swap: &asset.SwapEstimate{
					Lots: 5,
				},
			},
			dexMaxBuy: &core.MaxOrderEstimate{
				Swap: &asset.SwapEstimate{
					Lots: 5,
				},
			},

			cexBalances: map[uint32]*libxc.ExchangeBalance{
				0:  {Available: 1e19},
				42: {Available: 1e19},
			},

			existingArbs: []*arbSequence{{
				dexOrder: &core.Order{
					ID:   orderIDs[0][:],
					Rate: 2.2e6,
				},
				cexOrderID: cexTradeIDs[0],
				sellOnDEX:  false,
				startEpoch: currEpoch - 2,
			}},

			expectedCEXCancels: []string{cexTradeIDs[0]},
			expectedDEXCancels: []dex.Bytes{orderIDs[0][:]},
		},
		// "remove expired active arbs"
		{
			name:  "remove expired active arbs",
			books: noArbBooks,
			dexMaxSell: &core.MaxOrderEstimate{
				Swap: &asset.SwapEstimate{
					Lots: 5,
				},
			},
			dexMaxBuy: &core.MaxOrderEstimate{
				Swap: &asset.SwapEstimate{
					Lots: 5,
				},
			},
			existingArbs: []*arbSequence{
				{
					dexOrder: &core.Order{
						ID: orderIDs[0][:],
					},
					cexOrderID: cexTradeIDs[0],
					sellOnDEX:  false,
					startEpoch: currEpoch - 2,
				},
				{
					dexOrder: &core.Order{
						ID: orderIDs[1][:],
					},
					cexOrderID: cexTradeIDs[1],
					sellOnDEX:  false,
					startEpoch: currEpoch - (uint64(numEpochsLeaveOpen) + 2),
				},
				{
					dexOrder: &core.Order{
						ID: orderIDs[2][:],
					},
					cexOrderID:     cexTradeIDs[2],
					sellOnDEX:      false,
					cexOrderFilled: true,
					startEpoch:     currEpoch - (uint64(numEpochsLeaveOpen) + 2),
				},
				{
					dexOrder: &core.Order{
						ID: orderIDs[3][:],
					},
					cexOrderID:     cexTradeIDs[3],
					sellOnDEX:      false,
					dexOrderFilled: true,
					startEpoch:     currEpoch - (uint64(numEpochsLeaveOpen) + 2),
				},
			},
			expectedCEXCancels: []string{cexTradeIDs[1], cexTradeIDs[3]},
			expectedDEXCancels: []dex.Bytes{orderIDs[1][:], orderIDs[2][:]},
			cexBalances: map[uint32]*libxc.ExchangeBalance{
				0:  {Available: 1e19},
				42: {Available: 1e19},
			},
		},
		// "already max active arbs"
		{
			name:  "already max active arbs",
			books: arbBuyOnDEXBooks,
			dexMaxSell: &core.MaxOrderEstimate{
				Swap: &asset.SwapEstimate{
					Lots: 5,
				},
			},
			dexMaxBuy: &core.MaxOrderEstimate{
				Swap: &asset.SwapEstimate{
					Lots: 5,
				},
			},
			cexBalances: map[uint32]*libxc.ExchangeBalance{
				0:  {Available: 1e19},
				42: {Available: 1e19},
			},
			existingArbs: []*arbSequence{
				{
					dexOrder: &core.Order{
						ID: orderIDs[0][:],
					},
					cexOrderID: cexTradeIDs[0],
					sellOnDEX:  false,
					startEpoch: currEpoch - 1,
				},
				{
					dexOrder: &core.Order{
						ID: orderIDs[1][:],
					},
					cexOrderID: cexTradeIDs[2],
					sellOnDEX:  false,
					startEpoch: currEpoch - 2,
				},
				{
					dexOrder: &core.Order{
						ID: orderIDs[2][:],
					},
					cexOrderID: cexTradeIDs[2],
					sellOnDEX:  false,
					startEpoch: currEpoch - 3,
				},
				{
					dexOrder: &core.Order{
						ID: orderIDs[3][:],
					},
					cexOrderID: cexTradeIDs[3],
					sellOnDEX:  false,
					startEpoch: currEpoch - 4,
				},
				{
					dexOrder: &core.Order{
						ID: orderIDs[4][:],
					},
					cexOrderID: cexTradeIDs[4],
					sellOnDEX:  false,
					startEpoch: currEpoch - 5,
				},
			},
		},
		// "cex trade error"
		{
			name:  "cex trade error",
			books: arbBuyOnDEXBooks,
			dexMaxSell: &core.MaxOrderEstimate{
				Swap: &asset.SwapEstimate{
					Lots: 5,
				},
			},
			dexMaxBuy: &core.MaxOrderEstimate{
				Swap: &asset.SwapEstimate{
					Lots: 5,
				},
			},
			cexBalances: map[uint32]*libxc.ExchangeBalance{
				0:  {Available: 1e19},
				42: {Available: 1e19},
			},
			cexTradeErr: errors.New(""),
		},
		// "no arb, base needs withdrawal, quote needs deposit"
		{
			name:  "no arb, base needs withdrawal, quote needs deposit",
			books: noArbBooks,
			dexMaxSell: &core.MaxOrderEstimate{
				Swap: &asset.SwapEstimate{
					Lots: 5,
				},
			},
			dexMaxBuy: &core.MaxOrderEstimate{
				Swap: &asset.SwapEstimate{
					Lots: 5,
				},
			},
			dexBalances: map[uint32]uint64{
				42: 1e14,
				0:  1e17,
			},
			cexBalances: map[uint32]*libxc.ExchangeBalance{
				42: {Available: 1e19},
				0:  {Available: 1e10},
			},
			autoRebalance: &AutoRebalanceConfig{
				MinBaseAmt:  1e16,
				MinQuoteAmt: 1e12,
			},
			expectedWithdrawal: &assetAmt{
				assetID: 42,
				amt:     4.99995e18,
			},
			expectedDeposit: &assetAmt{
				assetID: 0,
				amt:     4.9999995e16,
			},
		},
		// "no arb, base needs withdrawal, quote needs deposit, edge of min transfer amount"
		{
			name:  "no arb, base needs withdrawal, quote needs deposit, edge of min transfer amount",
			books: noArbBooks,
			dexMaxSell: &core.MaxOrderEstimate{
				Swap: &asset.SwapEstimate{
					Lots: 5,
				},
			},
			dexMaxBuy: &core.MaxOrderEstimate{
				Swap: &asset.SwapEstimate{
					Lots: 5,
				},
			},
			dexBalances: map[uint32]uint64{
				42: 9.5e15,
				0:  1.1e12,
			},
			cexBalances: map[uint32]*libxc.ExchangeBalance{
				42: {Available: 1.1e16},
				0:  {Available: 9.5e11},
			},
			autoRebalance: &AutoRebalanceConfig{
				MinBaseAmt:       1e16,
				MinQuoteAmt:      1e12,
				MinBaseTransfer:  (1.1e16+9.5e15)/2 - 9.5e15,
				MinQuoteTransfer: (1.1e12+9.5e11)/2 - 9.5e11,
			},
			expectedWithdrawal: &assetAmt{
				assetID: 42,
				amt:     (1.1e16+9.5e15)/2 - 9.5e15,
			},
			expectedDeposit: &assetAmt{
				assetID: 0,
				amt:     (1.1e12+9.5e11)/2 - 9.5e11,
			},
		},
		// "no arb, base needs withdrawal, quote needs deposit, below min transfer amount"
		{
			name:  "no arb, base needs withdrawal, quote needs deposit, below min transfer amount",
			books: noArbBooks,
			dexMaxSell: &core.MaxOrderEstimate{
				Swap: &asset.SwapEstimate{
					Lots: 5,
				},
			},
			dexMaxBuy: &core.MaxOrderEstimate{
				Swap: &asset.SwapEstimate{
					Lots: 5,
				},
			},
			dexBalances: map[uint32]uint64{
				42: 9.5e15,
				0:  1.1e12,
			},
			cexBalances: map[uint32]*libxc.ExchangeBalance{
				42: {Available: 1.1e16},
				0:  {Available: 9.5e11},
			},
			autoRebalance: &AutoRebalanceConfig{
				MinBaseAmt:       1e16,
				MinQuoteAmt:      1e12,
				MinBaseTransfer:  (1.1e16+9.5e15)/2 - 9.5e15 + 1,
				MinQuoteTransfer: (1.1e12+9.5e11)/2 - 9.5e11 + 1,
			},
		},
		// "no arb, quote needs withdrawal, base needs deposit"
		{
			name:  "no arb, quote needs withdrawal, base needs deposit",
			books: noArbBooks,
			dexMaxSell: &core.MaxOrderEstimate{
				Swap: &asset.SwapEstimate{
					Lots: 5,
				},
			},
			dexMaxBuy: &core.MaxOrderEstimate{
				Swap: &asset.SwapEstimate{
					Lots: 5,
				},
			},
			dexBalances: map[uint32]uint64{
				42: 1e19,
				0:  1e10,
			},
			cexBalances: map[uint32]*libxc.ExchangeBalance{
				42: {Available: 1e14},
				0:  {Available: 1e17},
			},
			autoRebalance: &AutoRebalanceConfig{
				MinBaseAmt:  1e16,
				MinQuoteAmt: 1e12,
			},
			expectedWithdrawal: &assetAmt{
				assetID: 0,
				amt:     4.9999995e16,
			},
			expectedDeposit: &assetAmt{
				assetID: 42,
				amt:     4.99995e18,
			},
		},
		// "no arb, quote needs withdrawal, base needs deposit, already pending"
		{
			name:  "no arb, quote needs withdrawal, base needs deposit, already pending",
			books: noArbBooks,
			dexMaxSell: &core.MaxOrderEstimate{
				Swap: &asset.SwapEstimate{
					Lots: 5,
				},
			},
			dexMaxBuy: &core.MaxOrderEstimate{
				Swap: &asset.SwapEstimate{
					Lots: 5,
				},
			},
			dexBalances: map[uint32]uint64{
				42: 1e19,
				0:  1e10,
			},
			cexBalances: map[uint32]*libxc.ExchangeBalance{
				42: {Available: 1e14},
				0:  {Available: 1e17},
			},
			autoRebalance: &AutoRebalanceConfig{
				MinBaseAmt:  1e16,
				MinQuoteAmt: 1e12,
			},
			pendingBaseRebalance:  true,
			pendingQuoteRebalance: true,
		},
	}

	runTest := func(test *test) {
		cex := newTWrappedCEX()
		cex.vwapErr = test.cexVWAPErr
		cex.balances = test.cexBalances
		cex.tradeErr = test.cexTradeErr

		tCore := newTCore()
		tCore.maxBuyEstimate = test.dexMaxBuy
		tCore.maxSellEstimate = test.dexMaxSell
		tCore.maxSellErr = test.dexMaxSellErr
		tCore.maxBuyErr = test.dexMaxBuyErr
		tCore.setAssetBalances(test.dexBalances)
		if test.expectedDexOrder != nil {
			tCore.multiTradeResult = []*core.Order{
				{
					ID: encode.RandomBytes(32),
				},
			}
		}

		orderBook := &tOrderBook{
			bidsVWAP: make(map[uint64]vwapResult),
			asksVWAP: make(map[uint64]vwapResult),
			vwapErr:  test.dexVWAPErr,
		}
		for i := range test.books.dexBidsAvg {
			orderBook.bidsVWAP[uint64(i+1)] = vwapResult{test.books.dexBidsAvg[i], test.books.dexBidsExtrema[i]}
		}
		for i := range test.books.dexAsksAvg {
			orderBook.asksVWAP[uint64(i+1)] = vwapResult{test.books.dexAsksAvg[i], test.books.dexAsksExtrema[i]}
		}
		for i := range test.books.cexBidsAvg {
			cex.bidsVWAP[uint64(i+1)*mkt.LotSize] = vwapResult{test.books.cexBidsAvg[i], test.books.cexBidsExtrema[i]}
		}
		for i := range test.books.cexAsksAvg {
			cex.asksVWAP[uint64(i+1)*mkt.LotSize] = vwapResult{test.books.cexAsksAvg[i], test.books.cexAsksExtrema[i]}
		}

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		arbEngine := &simpleArbMarketMaker{
			ctx:        ctx,
			log:        log,
			cex:        cex,
			mkt:        mkt,
			baseID:     42,
			quoteID:    0,
			core:       tCore,
			activeArbs: test.existingArbs,
			cfg: &SimpleArbConfig{
				ProfitTrigger:      profitTrigger,
				MaxActiveArbs:      maxActiveArbs,
				NumEpochsLeaveOpen: numEpochsLeaveOpen,
				AutoRebalance:      test.autoRebalance,
			},
		}

		arbEngine.pendingBaseRebalance.Store(test.pendingBaseRebalance)
		arbEngine.pendingQuoteRebalance.Store(test.pendingQuoteRebalance)

		go arbEngine.run()

		dummyNote := &core.BookUpdate{}
		tCore.bookFeed.c <- dummyNote
		tCore.bookFeed.c <- dummyNote
		arbEngine.book = orderBook
		tCore.bookFeed.c <- &core.BookUpdate{
			Action: core.EpochMatchSummary,
			Payload: &core.EpochMatchSummaryPayload{
				Epoch: currEpoch - 1,
			},
		}
		tCore.bookFeed.c <- dummyNote
		tCore.bookFeed.c <- dummyNote

		// Check dex trade
		if test.expectedDexOrder == nil {
			if len(tCore.buysPlaced) > 0 || len(tCore.sellsPlaced) > 0 {
				t.Fatalf("%s: expected no dex order but got %d buys and %d sells", test.name, len(tCore.buysPlaced), len(tCore.sellsPlaced))
			}
		}
		if test.expectedDexOrder != nil {
			if test.expectedDexOrder.sell {
				if len(tCore.multiTradesPlaced[0].Placements) != 1 {
					t.Fatalf("%s: expected 1 sell order but got %d", test.name, len(tCore.sellsPlaced))
				}
				if !tCore.multiTradesPlaced[0].Sell {
					t.Fatalf("%s: expected sell order but got buy order", test.name)
				}
				if test.expectedDexOrder.rate != tCore.multiTradesPlaced[0].Placements[0].Rate {
					t.Fatalf("%s: expected sell order rate %d but got %d", test.name, test.expectedDexOrder.rate, tCore.sellsPlaced[0].Rate)
				}
				if test.expectedDexOrder.lots*mkt.LotSize != tCore.multiTradesPlaced[0].Placements[0].Qty {
					t.Fatalf("%s: expected sell order qty %d but got %d", test.name, test.expectedDexOrder.lots*mkt.LotSize, tCore.sellsPlaced[0].Qty)
				}
			}

			if !test.expectedDexOrder.sell {
				if len(tCore.multiTradesPlaced[0].Placements) != 1 {
					t.Fatalf("%s: expected 1 buy order but got %d", test.name, len(tCore.buysPlaced))
				}
				if tCore.multiTradesPlaced[0].Sell {
					t.Fatalf("%s: expected buy order but got sell order", test.name)
				}
				if test.expectedDexOrder.rate != tCore.multiTradesPlaced[0].Placements[0].Rate {
					t.Fatalf("%s: expected buy order rate %d but got %d", test.name, test.expectedDexOrder.rate, tCore.buysPlaced[0].Rate)
				}
				if test.expectedDexOrder.lots*mkt.LotSize != tCore.multiTradesPlaced[0].Placements[0].Qty {
					t.Fatalf("%s: expected buy order qty %d but got %d", test.name, test.expectedDexOrder.lots*mkt.LotSize, tCore.buysPlaced[0].Qty)
				}
			}
		}

		// Check cex trade
		if (test.expectedCexOrder == nil) != (cex.lastTrade == nil) {
			t.Fatalf("%s: expected cex order %v but got %v", test.name, (test.expectedCexOrder != nil), (cex.lastTrade != nil))
		}
		if cex.lastTrade != nil &&
			*cex.lastTrade != *test.expectedCexOrder {
			t.Fatalf("%s: cex order %+v != expected %+v", test.name, cex.lastTrade, test.expectedCexOrder)
		}

		// Check dex cancels
		if len(test.expectedDEXCancels) != len(tCore.cancelsPlaced) {
			t.Fatalf("%s: expected %d cancels but got %d", test.name, len(test.expectedDEXCancels), len(tCore.cancelsPlaced))
		}
		for i := range test.expectedDEXCancels {
			if !bytes.Equal(test.expectedDEXCancels[i], tCore.cancelsPlaced[i]) {
				t.Fatalf("%s: expected cancel %x but got %x", test.name, test.expectedDEXCancels[i], tCore.cancelsPlaced[i])
			}
		}

		// Check cex cancels
		if len(test.expectedCEXCancels) != len(cex.cancelledTrades) {
			t.Fatalf("%s: expected %d cex cancels but got %d", test.name, len(test.expectedCEXCancels), len(cex.cancelledTrades))
		}
		for i := range test.expectedCEXCancels {
			if test.expectedCEXCancels[i] != cex.cancelledTrades[i] {
				t.Fatalf("%s: expected cex cancel %s but got %s", test.name, test.expectedCEXCancels[i], cex.cancelledTrades[i])
			}
		}

		// Test auto rebalancing
		expectBasePending := test.pendingBaseRebalance
		expectQuotePending := test.pendingQuoteRebalance
		if test.expectedWithdrawal != nil {
			if cex.lastWithdrawArgs == nil {
				t.Fatalf("%s: expected withdrawal %+v but got none", test.name, test.expectedWithdrawal)
			}
			if test.expectedWithdrawal.assetID != cex.lastWithdrawArgs.assetID {
				t.Fatalf("%s: expected withdrawal asset %d but got %d", test.name, test.expectedWithdrawal.assetID, cex.lastWithdrawArgs.assetID)
			}
			if test.expectedWithdrawal.amt != cex.lastWithdrawArgs.amt {
				t.Fatalf("%s: expected withdrawal amt %d but got %d", test.name, test.expectedWithdrawal.amt, cex.lastWithdrawArgs.amt)
			}
			if test.expectedWithdrawal.assetID == arbEngine.baseID {
				expectBasePending = true
			} else {
				expectQuotePending = true
			}
		} else if cex.lastWithdrawArgs != nil {
			t.Fatalf("%s: expected no withdrawal but got %+v", test.name, cex.lastWithdrawArgs)
		}
		if test.expectedDeposit != nil {
			if cex.lastDepositArgs == nil {
				t.Fatalf("%s: expected deposit %+v but got none", test.name, test.expectedDeposit)
			}
			if test.expectedDeposit.assetID != cex.lastDepositArgs.assetID {
				t.Fatalf("%s: expected deposit asset %d but got %d", test.name, test.expectedDeposit.assetID, cex.lastDepositArgs.assetID)
			}
			if test.expectedDeposit.amt != cex.lastDepositArgs.amt {
				t.Fatalf("%s: expected deposit amt %d but got %d", test.name, test.expectedDeposit.amt, cex.lastDepositArgs.amt)
			}
			if test.expectedDeposit.assetID == arbEngine.baseID {
				expectBasePending = true
			} else {
				expectQuotePending = true
			}

		} else if cex.lastDepositArgs != nil {
			t.Fatalf("%s: expected no deposit but got %+v", test.name, cex.lastDepositArgs)
		}
		if expectBasePending != arbEngine.pendingBaseRebalance.Load() {
			t.Fatalf("%s: expected base pending %v but got %v", test.name, expectBasePending, !expectBasePending)
		}
		if expectQuotePending != arbEngine.pendingQuoteRebalance.Load() {
			t.Fatalf("%s: expected base pending %v but got %v", test.name, expectBasePending, !expectBasePending)
		}

		// Make sure that when withdraw/deposit is confirmed, the pending field
		// gets set back to false.
		if cex.confirmWithdraw != nil {
			cex.confirmWithdraw()
			if cex.lastWithdrawArgs.assetID == arbEngine.baseID && arbEngine.pendingBaseRebalance.Load() {
				t.Fatalf("%s: pending base rebalance was not reset after confirmation", test.name)
			}
			if cex.lastWithdrawArgs.assetID != arbEngine.baseID && arbEngine.pendingQuoteRebalance.Load() {
				t.Fatalf("%s: pending quote rebalance was not reset after confirmation", test.name)
			}
		}
		if cex.confirmDeposit != nil {
			cex.confirmDeposit()
			if cex.lastDepositArgs.assetID == arbEngine.baseID && arbEngine.pendingBaseRebalance.Load() {
				t.Fatalf("%s: pending base rebalance was not reset after confirmation", test.name)
			}
			if cex.lastDepositArgs.assetID != arbEngine.baseID && arbEngine.pendingQuoteRebalance.Load() {
				t.Fatalf("%s: pending quote rebalance was not reset after confirmation", test.name)
			}
		}
	}

	for _, test := range tests {
		runTest(&test)
	}
}

func TestArbDexTradeUpdates(t *testing.T) {
	orderIDs := make([]order.OrderID, 5)
	for i := 0; i < 5; i++ {
		copy(orderIDs[i][:], encode.RandomBytes(32))
	}

	cexTradeIDs := make([]string, 0, 5)
	for i := 0; i < 5; i++ {
		cexTradeIDs = append(cexTradeIDs, fmt.Sprintf("%x", encode.RandomBytes(32)))
	}

	type test struct {
		name               string
		activeArbs         []*arbSequence
		updatedOrderID     []byte
		updatedOrderStatus order.OrderStatus
		expectedActiveArbs []*arbSequence
	}

	dexOrder := &core.Order{
		ID: orderIDs[0][:],
	}

	tests := []*test{
		{
			name: "dex order still booked",
			activeArbs: []*arbSequence{
				{
					dexOrder:   dexOrder,
					cexOrderID: cexTradeIDs[0],
				},
			},
			updatedOrderID:     orderIDs[0][:],
			updatedOrderStatus: order.OrderStatusBooked,
			expectedActiveArbs: []*arbSequence{
				{
					dexOrder:   dexOrder,
					cexOrderID: cexTradeIDs[0],
				},
			},
		},
		{
			name: "dex order executed, but cex not yet filled",
			activeArbs: []*arbSequence{
				{
					dexOrder:   dexOrder,
					cexOrderID: cexTradeIDs[0],
				},
			},
			updatedOrderID:     orderIDs[0][:],
			updatedOrderStatus: order.OrderStatusExecuted,
			expectedActiveArbs: []*arbSequence{
				{
					dexOrder:       dexOrder,
					cexOrderID:     cexTradeIDs[0],
					dexOrderFilled: true,
				},
			},
		},
		{
			name: "dex order executed, but cex already filled",
			activeArbs: []*arbSequence{
				{
					dexOrder:       dexOrder,
					cexOrderID:     cexTradeIDs[0],
					cexOrderFilled: true,
				},
			},
			updatedOrderID:     orderIDs[0][:],
			updatedOrderStatus: order.OrderStatusExecuted,
			expectedActiveArbs: []*arbSequence{},
		},
	}

	runTest := func(test *test) {
		cex := newTWrappedCEX()
		tCore := newTCore()

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		arbEngine := &simpleArbMarketMaker{
			ctx:        ctx,
			log:        log,
			cex:        cex,
			baseID:     42,
			quoteID:    0,
			core:       tCore,
			activeArbs: test.activeArbs,
			cfg: &SimpleArbConfig{
				ProfitTrigger:      0.01,
				MaxActiveArbs:      5,
				NumEpochsLeaveOpen: 10,
			},
		}

		go arbEngine.run()

		tCore.noteFeed <- &core.OrderNote{
			Order: &core.Order{
				Status: test.updatedOrderStatus,
				ID:     test.updatedOrderID,
			},
		}
		dummyNote := &core.BondRefundNote{}
		tCore.noteFeed <- dummyNote

		if len(test.expectedActiveArbs) != len(arbEngine.activeArbs) {
			t.Fatalf("%s: expected %d active arbs but got %d", test.name, len(test.expectedActiveArbs), len(arbEngine.activeArbs))
		}

		for i := range test.expectedActiveArbs {
			if *arbEngine.activeArbs[i] != *test.expectedActiveArbs[i] {
				t.Fatalf("%s: active arb %+v != expected active arb %+v", test.name, arbEngine.activeArbs[i], test.expectedActiveArbs[i])
			}
		}
	}

	for _, test := range tests {
		runTest(test)
	}
}

func TestCexTradeUpdates(t *testing.T) {
	orderIDs := make([]order.OrderID, 5)
	for i := 0; i < 5; i++ {
		copy(orderIDs[i][:], encode.RandomBytes(32))
	}

	cexTradeIDs := make([]string, 0, 5)
	for i := 0; i < 5; i++ {
		cexTradeIDs = append(cexTradeIDs, fmt.Sprintf("%x", encode.RandomBytes(32)))
	}

	dexOrder := &core.Order{
		ID: orderIDs[0][:],
	}

	type test struct {
		name               string
		activeArbs         []*arbSequence
		updatedOrderID     string
		orderComplete      bool
		expectedActiveArbs []*arbSequence
	}

	tests := []*test{
		{
			name: "neither complete",
			activeArbs: []*arbSequence{
				{
					dexOrder:   dexOrder,
					cexOrderID: cexTradeIDs[0],
				},
			},
			updatedOrderID: cexTradeIDs[0],
			orderComplete:  false,
			expectedActiveArbs: []*arbSequence{
				{
					dexOrder:   dexOrder,
					cexOrderID: cexTradeIDs[0],
				},
			},
		},
		{
			name: "cex complete, but dex order not complete",
			activeArbs: []*arbSequence{
				{
					dexOrder:   dexOrder,
					cexOrderID: cexTradeIDs[0],
				},
			},
			updatedOrderID: cexTradeIDs[0],
			orderComplete:  true,
			expectedActiveArbs: []*arbSequence{
				{
					dexOrder:       dexOrder,
					cexOrderID:     cexTradeIDs[0],
					cexOrderFilled: true,
				},
			},
		},
		{
			name: "both complete",
			activeArbs: []*arbSequence{
				{
					dexOrder:       dexOrder,
					cexOrderID:     cexTradeIDs[0],
					dexOrderFilled: true,
				},
			},
			updatedOrderID: cexTradeIDs[0],
			orderComplete:  true,
		},
	}

	runTest := func(test *test) {
		cex := newTWrappedCEX()
		tCore := newTCore()

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		arbEngine := &simpleArbMarketMaker{
			ctx:        ctx,
			log:        log,
			cex:        cex,
			baseID:     42,
			quoteID:    0,
			core:       tCore,
			activeArbs: test.activeArbs,
			cfg: &SimpleArbConfig{
				ProfitTrigger:      0.01,
				MaxActiveArbs:      5,
				NumEpochsLeaveOpen: 10,
			},
		}

		go arbEngine.run()

		cex.tradeUpdates <- &libxc.TradeUpdate{
			TradeID:  test.updatedOrderID,
			Complete: test.orderComplete,
		}
		// send dummy update
		cex.tradeUpdates <- &libxc.TradeUpdate{
			TradeID: "",
		}

		if len(test.expectedActiveArbs) != len(arbEngine.activeArbs) {
			t.Fatalf("%s: expected %d active arbs but got %d", test.name, len(test.expectedActiveArbs), len(arbEngine.activeArbs))
		}
		for i := range test.expectedActiveArbs {
			if *arbEngine.activeArbs[i] != *test.expectedActiveArbs[i] {
				t.Fatalf("%s: active arb %+v != expected active arb %+v", test.name, arbEngine.activeArbs[i], test.expectedActiveArbs[i])
			}
		}
	}

	for _, test := range tests {
		runTest(test)
	}
}
