package core

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"testing"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/client/core/libxc"
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

type tArbEngineInputs struct {
	bidsVWAP        map[uint64]vwapResult
	asksVWAP        map[uint64]vwapResult
	lotSizeV        uint64
	maxSellV        *MaxOrderEstimate
	maxBuyV         *MaxOrderEstimate
	buys            []*Order
	sells           []*Order
	cancelledOrders []order.OrderID
	placeOrderID    order.OrderID
	lastOrderPlaced *dexOrder
	placeOrderErr   error
	maxSellErr      error
	maxBuyErr       error
	vwapErr         error
}

func newTArbEngineInputs() *tArbEngineInputs {
	return &tArbEngineInputs{
		bidsVWAP:        make(map[uint64]vwapResult),
		asksVWAP:        make(map[uint64]vwapResult),
		cancelledOrders: make([]order.OrderID, 0, 4),
		buys:            make([]*Order, 0, 4),
		sells:           make([]*Order, 0, 4),
	}
}

func (i *tArbEngineInputs) vwap(numLots uint64, sell bool) (avgRate uint64, extrema uint64, filled bool, err error) {
	if i.vwapErr != nil {
		return 0, 0, false, i.vwapErr
	}

	if sell {
		res, found := i.asksVWAP[numLots]
		if !found {
			return 0, 0, false, nil
		}
		return res.avg, res.extrema, true, nil
	}

	res, found := i.bidsVWAP[numLots]
	if !found {
		return 0, 0, false, nil
	}
	return res.avg, res.extrema, true, nil
}

func (i *tArbEngineInputs) lotSize() uint64 {
	return i.lotSizeV
}
func (i *tArbEngineInputs) maxBuy(rate uint64) (*MaxOrderEstimate, error) {
	return i.maxBuyV, i.maxBuyErr
}
func (i *tArbEngineInputs) maxSell() (*MaxOrderEstimate, error) {
	return i.maxSellV, i.maxSellErr
}
func (i *tArbEngineInputs) sortedOrders() (buys, sells []*Order) {
	return i.buys, i.sells
}
func (i *tArbEngineInputs) placeOrder(lots, rate uint64, sell bool) (order.OrderID, error) {
	if i.placeOrderErr != nil {
		return order.OrderID{}, i.placeOrderErr
	}
	i.lastOrderPlaced = &dexOrder{lots, rate, sell}
	return i.placeOrderID, nil
}
func (i *tArbEngineInputs) cancelOrder(oid order.OrderID) error {
	i.cancelledOrders = append(i.cancelledOrders, oid)
	return nil
}

var _ arbEngineInputs = (*tArbEngineInputs)(nil)

type cexOrder struct {
	baseSymbol, quoteSymbol string
	qty, rate               uint64
	sell                    bool
}
type tCEX struct {
	bidsVWAP   map[uint64]vwapResult
	asksVWAP   map[uint64]vwapResult
	vwapErr    error
	balances   map[string]*libxc.ExchangeBalance
	balanceErr error

	tradeID   string
	tradeErr  error
	lastTrade *cexOrder

	cancelledTrades []string
	cancelTradeErr  error

	tradeUpdates   <-chan *libxc.TradeUpdate
	tradeUpdatesID int
}

func newTCEX() *tCEX {
	return &tCEX{
		bidsVWAP:        make(map[uint64]vwapResult),
		asksVWAP:        make(map[uint64]vwapResult),
		balances:        make(map[string]*libxc.ExchangeBalance),
		cancelledTrades: make([]string, 0),
	}
}

func (c *tCEX) Connect(ctx context.Context) (*sync.WaitGroup, error) {
	return nil, nil
}
func (c *tCEX) Balances() (map[uint32]*libxc.ExchangeBalance, error) {
	return nil, nil
}
func (c *tCEX) Markets() ([]*libxc.Market, error) {
	return nil, nil
}
func (c *tCEX) Balance(symbol string) (*libxc.ExchangeBalance, error) {
	return c.balances[symbol], c.balanceErr
}
func (c *tCEX) GenerateTradeID() string {
	return c.tradeID
}
func (c *tCEX) Trade(baseSymbol, quoteSymbol string, sell bool, rate, qty uint64, updaterID int, orderID string) error {
	if c.tradeErr != nil {
		return c.tradeErr
	}
	c.lastTrade = &cexOrder{baseSymbol, quoteSymbol, qty, rate, sell}
	return nil
}
func (c *tCEX) CancelTrade(baseSymbol, quoteSymbol, tradeID string) error {
	if c.cancelTradeErr != nil {
		return c.cancelTradeErr
	}
	c.cancelledTrades = append(c.cancelledTrades, tradeID)
	return nil
}
func (c *tCEX) SubscribeMarket(baseSymbol, quoteSymbol string) error {
	return nil
}
func (c *tCEX) UnsubscribeMarket(baseSymbol, quoteSymbol string) error {
	return nil
}
func (c *tCEX) VWAP(baseSymbol, quoteSymbol string, sell bool, qty uint64) (vwap, extrema uint64, filled bool, err error) {
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
func (c *tCEX) SubscribeTradeUpdates() (<-chan *libxc.TradeUpdate, int) {
	return c.tradeUpdates, c.tradeUpdatesID
}
func (c *tCEX) SubscribeCEXUpdates() <-chan interface{} {
	return nil
}

var _ libxc.CEX = (*tCEX)(nil)

func TestArbRebalance(t *testing.T) {
	lotSize := uint64(40 * 1e8)

	orderIDs := make([]order.OrderID, 5)
	for i := 0; i < 5; i++ {
		copy(orderIDs[i][:], encode.RandomBytes(32))
	}

	cexTradeIDs := make([]string, 0, 5)
	for i := 0; i < 5; i++ {
		cexTradeIDs = append(cexTradeIDs, fmt.Sprintf("%x", encode.RandomBytes(32)))
	}

	var currEpoch uint64 = 100
	var numEpochsLeaveOpen uint32 = 10

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

	type test struct {
		name             string
		books            *testBooks
		dexMaxSell       *MaxOrderEstimate
		dexMaxBuy        *MaxOrderEstimate
		cexBalances      map[string]*libxc.ExchangeBalance
		existingBuys     []*Order
		existingSells    []*Order
		activeArbs       []*arbSequence
		cexTradeErr      error
		dexPlaceOrderErr error
		cexTradeID       string
		dexMaxSellErr    error
		dexMaxBuyErr     error
		dexVWAPErr       error
		cexVWAPErr       error

		expectedDexOrder   *dexOrder
		expectedCexOrder   *cexOrder
		expectedCEXCancels []string
		expectedDEXCancels []order.OrderID
	}

	tests := []test{
		{
			name:  "no arb",
			books: noArbBooks,
			dexMaxSell: &MaxOrderEstimate{
				Swap: &asset.SwapEstimate{
					Lots: 5,
				},
			},
			dexMaxBuy: &MaxOrderEstimate{
				Swap: &asset.SwapEstimate{
					Lots: 5,
				},
			},
			cexBalances: map[string]*libxc.ExchangeBalance{
				"btc": {Available: 1e19},
				"dcr": {Available: 1e19},
			},
		},
		{
			name:  "1 lot, buy on dex, sell on cex",
			books: arbBuyOnDEXBooks,
			dexMaxSell: &MaxOrderEstimate{
				Swap: &asset.SwapEstimate{
					Lots: 5,
				},
			},
			dexMaxBuy: &MaxOrderEstimate{
				Swap: &asset.SwapEstimate{
					Lots: 5,
				},
			},
			cexBalances: map[string]*libxc.ExchangeBalance{
				"btc": {Available: 1e19},
				"dcr": {Available: 1e19},
			},
			expectedDexOrder: &dexOrder{
				lots: 1,
				rate: 2e6,
				sell: false,
			},
			expectedCexOrder: &cexOrder{
				baseSymbol:  "dcr",
				quoteSymbol: "btc",
				qty:         lotSize,
				rate:        2.2e6,
				sell:        true,
			},
		},
		{
			name:  "1 lot, buy on dex, sell on cex, but cex base balance not enough",
			books: arbBuyOnDEXBooks,
			dexMaxSell: &MaxOrderEstimate{
				Swap: &asset.SwapEstimate{
					Lots: 5,
				},
			},
			dexMaxBuy: &MaxOrderEstimate{
				Swap: &asset.SwapEstimate{
					Lots: 5,
				},
			},
			cexBalances: map[string]*libxc.ExchangeBalance{
				"btc": {Available: 1e19},
				"dcr": {Available: lotSize / 2},
			},
		},
		{
			name:  "2 lot, buy on dex, sell on cex, but dex quote balance only enough for 1",
			books: arb2LotsBuyOnDEXBooks,
			dexMaxSell: &MaxOrderEstimate{
				Swap: &asset.SwapEstimate{
					Lots: 5,
				},
			},
			dexMaxBuy: &MaxOrderEstimate{
				Swap: &asset.SwapEstimate{
					Lots: 1,
				},
			},

			cexBalances: map[string]*libxc.ExchangeBalance{
				"btc": {Available: 1e19},
				"dcr": {Available: 1e19},
			},

			expectedDexOrder: &dexOrder{
				lots: 1,
				rate: 2e6,
				sell: false,
			},
			expectedCexOrder: &cexOrder{
				baseSymbol:  "dcr",
				quoteSymbol: "btc",
				qty:         lotSize,
				rate:        2.2e6,
				sell:        true,
			},
		},

		{
			name:  "1 lot, sell on dex, buy on cex",
			books: arbSellOnDEXBooks,
			dexMaxSell: &MaxOrderEstimate{
				Swap: &asset.SwapEstimate{
					Lots: 5,
				},
			},
			dexMaxBuy: &MaxOrderEstimate{
				Swap: &asset.SwapEstimate{
					Lots: 5,
				},
			},
			cexBalances: map[string]*libxc.ExchangeBalance{
				"btc": {Available: 1e19},
				"dcr": {Available: 1e19},
			},
			expectedDexOrder: &dexOrder{
				lots: 1,
				rate: 2.2e6,
				sell: true,
			},
			expectedCexOrder: &cexOrder{
				baseSymbol:  "dcr",
				quoteSymbol: "btc",
				qty:         lotSize,
				rate:        2e6,
				sell:        false,
			},
		},
		{
			name:  "2 lot, buy on cex, sell on dex, but cex quote balance only enough for 1",
			books: arb2LotsSellOnDEXBooks,
			dexMaxSell: &MaxOrderEstimate{
				Swap: &asset.SwapEstimate{
					Lots: 5,
				},
			},
			dexMaxBuy: &MaxOrderEstimate{
				Swap: &asset.SwapEstimate{
					Lots: 5,
				},
			},
			cexBalances: map[string]*libxc.ExchangeBalance{
				"btc": {Available: calc.BaseToQuote(2e6, lotSize*3/2)},
				"dcr": {Available: 1e19},
			},
			expectedDexOrder: &dexOrder{
				lots: 1,
				rate: 2.2e6,
				sell: true,
			},
			expectedCexOrder: &cexOrder{
				baseSymbol:  "dcr",
				quoteSymbol: "btc",
				qty:         lotSize,
				rate:        2e6,
				sell:        false,
			},
		},

		{
			name:  "self-match",
			books: arbSellOnDEXBooks,
			dexMaxSell: &MaxOrderEstimate{
				Swap: &asset.SwapEstimate{
					Lots: 5,
				},
			},
			dexMaxBuy: &MaxOrderEstimate{
				Swap: &asset.SwapEstimate{
					Lots: 5,
				},
			},

			cexBalances: map[string]*libxc.ExchangeBalance{
				"btc": {Available: 1e19},
				"dcr": {Available: 1e19},
			},

			existingBuys: []*Order{{Rate: 2.2e6}},
			activeArbs: []*arbSequence{{
				dexOrderID: orderIDs[0],
				cexOrderID: cexTradeIDs[0],
				sellOnDEX:  false,
				startEpoch: currEpoch - 2,
			}},

			expectedCEXCancels: []string{cexTradeIDs[0]},
			expectedDEXCancels: []order.OrderID{orderIDs[0]},
		},
		{
			name:  "remove expired active arbs",
			books: noArbBooks,
			dexMaxSell: &MaxOrderEstimate{
				Swap: &asset.SwapEstimate{
					Lots: 5,
				},
			},
			dexMaxBuy: &MaxOrderEstimate{
				Swap: &asset.SwapEstimate{
					Lots: 5,
				},
			},
			activeArbs: []*arbSequence{
				{
					dexOrderID: orderIDs[0],
					cexOrderID: cexTradeIDs[0],
					sellOnDEX:  false,
					startEpoch: currEpoch - 2,
				},
				{
					dexOrderID: orderIDs[1],
					cexOrderID: cexTradeIDs[1],
					sellOnDEX:  false,
					startEpoch: currEpoch - (uint64(numEpochsLeaveOpen) + 2),
				},
				{
					dexOrderID:     orderIDs[2],
					cexOrderID:     cexTradeIDs[2],
					sellOnDEX:      false,
					cexOrderFilled: true,
					startEpoch:     currEpoch - (uint64(numEpochsLeaveOpen) + 2),
				},
				{
					dexOrderID:     orderIDs[3],
					cexOrderID:     cexTradeIDs[3],
					sellOnDEX:      false,
					dexOrderFilled: true,
					startEpoch:     currEpoch - (uint64(numEpochsLeaveOpen) + 2),
				},
			},
			expectedCEXCancels: []string{cexTradeIDs[1], cexTradeIDs[3]},
			expectedDEXCancels: []order.OrderID{orderIDs[1], orderIDs[2]},
			cexBalances: map[string]*libxc.ExchangeBalance{
				"btc": {Available: 1e19},
				"dcr": {Available: 1e19},
			},
		},
		{
			name:  "already max active arbs",
			books: arbBuyOnDEXBooks,
			dexMaxSell: &MaxOrderEstimate{
				Swap: &asset.SwapEstimate{
					Lots: 5,
				},
			},
			dexMaxBuy: &MaxOrderEstimate{
				Swap: &asset.SwapEstimate{
					Lots: 5,
				},
			},
			cexBalances: map[string]*libxc.ExchangeBalance{
				"btc": {Available: 1e19},
				"dcr": {Available: 1e19},
			},
			activeArbs: []*arbSequence{
				{
					dexOrderID: orderIDs[0],
					cexOrderID: cexTradeIDs[0],
					sellOnDEX:  false,
					startEpoch: currEpoch - 1,
				},
				{
					dexOrderID: orderIDs[1],
					cexOrderID: cexTradeIDs[2],
					sellOnDEX:  false,
					startEpoch: currEpoch - 2,
				},
				{
					dexOrderID: orderIDs[2],
					cexOrderID: cexTradeIDs[2],
					sellOnDEX:  false,
					startEpoch: currEpoch - 3,
				},
				{
					dexOrderID: orderIDs[3],
					cexOrderID: cexTradeIDs[3],
					sellOnDEX:  false,
					startEpoch: currEpoch - 4,
				},
				{
					dexOrderID: orderIDs[4],
					cexOrderID: cexTradeIDs[4],
					sellOnDEX:  false,
					startEpoch: currEpoch - 5,
				},
			},
		},
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
			dexMaxSell: &MaxOrderEstimate{
				Swap: &asset.SwapEstimate{
					Lots: 5,
				},
			},
			dexMaxBuy: &MaxOrderEstimate{
				Swap: &asset.SwapEstimate{
					Lots: 5,
				},
			},

			cexBalances: map[string]*libxc.ExchangeBalance{
				"btc": {Available: 1e19},
				"dcr": {Available: 1e19},
			},
		},
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
			dexMaxSell: &MaxOrderEstimate{
				Swap: &asset.SwapEstimate{
					Lots: 5,
				},
			},
			dexMaxBuy: &MaxOrderEstimate{
				Swap: &asset.SwapEstimate{
					Lots: 5,
				},
			},

			cexBalances: map[string]*libxc.ExchangeBalance{
				"btc": {Available: 1e19},
				"dcr": {Available: 1e19},
			},
		},
		{
			name:  "cex trade error",
			books: arbBuyOnDEXBooks,
			dexMaxSell: &MaxOrderEstimate{
				Swap: &asset.SwapEstimate{
					Lots: 5,
				},
			},
			dexMaxBuy: &MaxOrderEstimate{
				Swap: &asset.SwapEstimate{
					Lots: 5,
				},
			},
			cexBalances: map[string]*libxc.ExchangeBalance{
				"btc": {Available: 1e19},
				"dcr": {Available: 1e19},
			},
			cexTradeErr: errors.New(""),
		},
		{
			name:  "dex place order error",
			books: arbBuyOnDEXBooks,
			dexMaxSell: &MaxOrderEstimate{
				Swap: &asset.SwapEstimate{
					Lots: 5,
				},
			},
			dexMaxBuy: &MaxOrderEstimate{
				Swap: &asset.SwapEstimate{
					Lots: 5,
				},
			},
			cexBalances: map[string]*libxc.ExchangeBalance{
				"btc": {Available: 1e19},
				"dcr": {Available: 1e19},
			},
			expectedCexOrder: &cexOrder{
				baseSymbol:  "dcr",
				quoteSymbol: "btc",
				qty:         lotSize,
				rate:        2.2e6,
				sell:        true,
			},
			cexTradeID:         cexTradeIDs[1],
			expectedCEXCancels: []string{cexTradeIDs[1]},
			dexPlaceOrderErr:   errors.New(""),
		},
		{
			name:  "dex max sell error",
			books: arbSellOnDEXBooks,
			dexMaxBuy: &MaxOrderEstimate{
				Swap: &asset.SwapEstimate{
					Lots: 5,
				},
			},
			cexBalances: map[string]*libxc.ExchangeBalance{
				"btc": {Available: 1e19},
				"dcr": {Available: 1e19},
			},
			dexMaxSellErr: errors.New(""),
		},
		{
			name:  "dex max buy error",
			books: arbBuyOnDEXBooks,
			dexMaxSell: &MaxOrderEstimate{
				Swap: &asset.SwapEstimate{
					Lots: 5,
				},
			},
			cexBalances: map[string]*libxc.ExchangeBalance{
				"btc": {Available: 1e19},
				"dcr": {Available: 1e19},
			},
			dexMaxBuyErr: errors.New(""),
		},
		{
			name:  "dex vwap error",
			books: arbBuyOnDEXBooks,
			dexMaxBuy: &MaxOrderEstimate{
				Swap: &asset.SwapEstimate{
					Lots: 5,
				},
			},
			dexMaxSell: &MaxOrderEstimate{
				Swap: &asset.SwapEstimate{
					Lots: 5,
				},
			},
			cexBalances: map[string]*libxc.ExchangeBalance{
				"btc": {Available: 1e19},
				"dcr": {Available: 1e19},
			},
			dexVWAPErr: errors.New(""),
		},
		{
			name:  "cex vwap error",
			books: arbBuyOnDEXBooks,
			dexMaxBuy: &MaxOrderEstimate{
				Swap: &asset.SwapEstimate{
					Lots: 5,
				},
			},
			dexMaxSell: &MaxOrderEstimate{
				Swap: &asset.SwapEstimate{
					Lots: 5,
				},
			},
			cexBalances: map[string]*libxc.ExchangeBalance{
				"btc": {Available: 1e19},
				"dcr": {Available: 1e19},
			},
			cexVWAPErr: errors.New(""),
		},
	}

	for _, test := range tests {
		inputs := newTArbEngineInputs()
		cex := newTCEX()
		arbEngine := &arbEngine{
			inputs:     inputs,
			log:        log,
			cex:        cex,
			base:       42,
			quote:      0,
			activeArbs: test.activeArbs,
		}
		arbEngine.cfgV.Store(&ArbEngineCfg{
			ProfitTrigger:      0.01,
			MaxActiveArbs:      5,
			NumEpochsLeaveOpen: numEpochsLeaveOpen,
		})

		for i := range test.books.dexBidsAvg {
			inputs.bidsVWAP[uint64(i+1)] = vwapResult{test.books.dexBidsAvg[i], test.books.dexBidsExtrema[i]}
		}
		for i := range test.books.dexAsksAvg {
			inputs.asksVWAP[uint64(i+1)] = vwapResult{test.books.dexAsksAvg[i], test.books.dexAsksExtrema[i]}
		}
		for i := range test.books.cexBidsAvg {
			cex.bidsVWAP[uint64(i+1)*lotSize] = vwapResult{test.books.cexBidsAvg[i], test.books.cexBidsExtrema[i]}
		}
		for i := range test.books.cexAsksAvg {
			cex.asksVWAP[uint64(i+1)*lotSize] = vwapResult{test.books.cexAsksAvg[i], test.books.cexAsksExtrema[i]}
		}

		inputs.buys = test.existingBuys
		inputs.sells = test.existingSells
		inputs.lotSizeV = lotSize
		inputs.maxSellV = test.dexMaxSell
		inputs.maxBuyV = test.dexMaxBuy
		inputs.placeOrderErr = test.dexPlaceOrderErr
		inputs.maxBuyErr = test.dexMaxBuyErr
		inputs.maxSellErr = test.dexMaxSellErr
		inputs.vwapErr = test.dexVWAPErr

		cex.balances = test.cexBalances
		cex.tradeID = test.cexTradeID
		cex.tradeErr = test.cexTradeErr
		cex.vwapErr = test.cexVWAPErr

		arbEngine.rebalance(currEpoch)

		// Check dex trade
		if (test.expectedDexOrder == nil) != (inputs.lastOrderPlaced == nil) {
			t.Fatalf("%s: expected dex order %v but got %v", test.name, (test.expectedDexOrder != nil), (inputs.lastOrderPlaced != nil))
		}
		if inputs.lastOrderPlaced != nil &&
			*inputs.lastOrderPlaced != *test.expectedDexOrder {
			t.Fatalf("%s: dex order %+v != expected %+v", test.name, inputs.lastOrderPlaced, test.expectedDexOrder)
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
		if len(inputs.cancelledOrders) != len(test.expectedDEXCancels) {
			t.Fatalf("%s expected %d dex cancels but got %d", test.name, len(test.expectedDEXCancels), len(inputs.cancelledOrders))
		}
		sort.Slice(inputs.cancelledOrders, func(i, j int) bool {
			return bytes.Compare(inputs.cancelledOrders[i][:], inputs.cancelledOrders[j][:]) < 0
		})
		sort.Slice(test.expectedDEXCancels, func(i, j int) bool {
			return bytes.Compare(test.expectedDEXCancels[i][:], test.expectedDEXCancels[j][:]) < 0
		})
		for i := range test.expectedDEXCancels {
			if !bytes.Equal(test.expectedDEXCancels[i][:], inputs.cancelledOrders[i][:]) {
				fmt.Printf("expected: %+v\n actual: %+v\n", test.expectedDEXCancels, inputs.cancelledOrders)
				t.Fatalf("%s: dex cancelled order %s != expected %s", test.name, inputs.cancelledOrders[i], test.expectedDEXCancels[i])
			}
		}

		// Check cex cancels
		if len(cex.cancelledTrades) != len(test.expectedCEXCancels) {
			t.Fatalf("%s expected %d cex cancels but got %d", test.name, len(test.expectedCEXCancels), len(cex.cancelledTrades))
		}
		sort.Slice(test.expectedCEXCancels, func(i, j int) bool {
			return strings.Compare(test.expectedCEXCancels[i][:], test.expectedCEXCancels[j][:]) < 0
		})
		sort.Slice(cex.cancelledTrades, func(i, j int) bool {
			return strings.Compare(cex.cancelledTrades[i][:], cex.cancelledTrades[j][:]) < 0
		})
		for i := range cex.cancelledTrades {
			if test.expectedCEXCancels[i][:] != cex.cancelledTrades[i][:] {
				t.Fatalf("%s: cex cancelled order %s != expected %s", test.name, cex.cancelledTrades[i], test.expectedCEXCancels[i])
			}
		}
	}
}

func TestArbEngineDEXOrderUpdate(t *testing.T) {
	orderIDs := make([]order.OrderID, 5)
	for i := 0; i < 5; i++ {
		copy(orderIDs[i][:], encode.RandomBytes(32))
	}

	cexTradeIDs := make([]string, 0, 5)
	for i := 0; i < 5; i++ {
		cexTradeIDs = append(cexTradeIDs, fmt.Sprintf("%x", encode.RandomBytes(32)))
	}

	tests := []struct {
		name               string
		activeArbs         []*arbSequence
		updatedOrderID     []byte
		updatedOrderStatus order.OrderStatus
		expectedActiveArbs []*arbSequence
	}{
		{
			name: "dex order still booked",
			activeArbs: []*arbSequence{
				{
					dexOrderID: orderIDs[0],
					cexOrderID: cexTradeIDs[0],
				},
			},
			updatedOrderID:     orderIDs[0][:],
			updatedOrderStatus: order.OrderStatusBooked,
			expectedActiveArbs: []*arbSequence{
				{
					dexOrderID: orderIDs[0],
					cexOrderID: cexTradeIDs[0],
				},
			},
		},
		{
			name: "dex order executed, but cex not yet filled",
			activeArbs: []*arbSequence{
				{
					dexOrderID: orderIDs[0],
					cexOrderID: cexTradeIDs[0],
				},
			},
			updatedOrderID:     orderIDs[0][:],
			updatedOrderStatus: order.OrderStatusExecuted,
			expectedActiveArbs: []*arbSequence{
				{
					dexOrderID:     orderIDs[0],
					cexOrderID:     cexTradeIDs[0],
					dexOrderFilled: true,
				},
			},
		},
		{
			name: "dex order executed, but cex already filled",
			activeArbs: []*arbSequence{
				{
					dexOrderID:     orderIDs[0],
					cexOrderID:     cexTradeIDs[0],
					cexOrderFilled: true,
				},
			},
			updatedOrderID:     orderIDs[0][:],
			updatedOrderStatus: order.OrderStatusExecuted,
			expectedActiveArbs: []*arbSequence{},
		},
	}

	for _, test := range tests {
		inputs := newTArbEngineInputs()
		cex := newTCEX()
		arbEngine := &arbEngine{
			inputs:     inputs,
			log:        log,
			cex:        cex,
			base:       42,
			quote:      0,
			activeArbs: test.activeArbs,
		}
		arbEngine.cfgV.Store(&ArbEngineCfg{
			ProfitTrigger:      0.01,
			MaxActiveArbs:      5,
			NumEpochsLeaveOpen: 10,
		})

		arbEngine.notify(orderUpdateEngineNote(&Order{
			Status: test.updatedOrderStatus,
			ID:     test.updatedOrderID,
		}))

		if len(test.expectedActiveArbs) != len(arbEngine.activeArbs) {
			t.Fatalf("%s: expected %d active arbs but got %d", test.name, len(test.expectedActiveArbs), len(arbEngine.activeArbs))
		}

		for i := range test.expectedActiveArbs {
			if *arbEngine.activeArbs[i] != *test.expectedActiveArbs[i] {
				t.Fatalf("%s: active arb %+v != expected active arb %+v", test.name, arbEngine.activeArbs[i], test.expectedActiveArbs[i])
			}
		}
	}
}
