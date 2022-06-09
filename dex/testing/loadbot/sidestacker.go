// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package main

import (
	"errors"
	"math/rand"
	"sort"
	"sync"
	"time"

	"decred.org/dcrdex/client/core"
	"decred.org/dcrdex/dex/calc"
	"decred.org/dcrdex/dex/msgjson"
	"decred.org/dcrdex/dex/order"
)

const stackerSpread = 0.05

// The sideStacker is a Trader that attempts to attain order book depth for one
// side before submitting taker orders for the other. There is some built-in
// randomness to the rates and quantities of the sideStacker's orders.
type sideStacker struct {
	node    string
	seller  bool
	metered bool
	// numStanding is the targeted number of standing limit orders. If there are
	// fewer than numStanding orders on the book
	numStanding int
	// ordsPerEpoch is the exact number of orders the sideStacker will place
	// each epoch. If there are already enough standing limit orders,
	// sideStacker will place orders to match immediately.
	ordsPerEpoch int
}

var _ Trader = (*sideStacker)(nil)

func newSideStacker(seller bool, numStanding, ordsPerEpoch int, node string, metered bool) *sideStacker {
	return &sideStacker{
		node:         node,
		seller:       seller,
		metered:      metered,
		numStanding:  numStanding,
		ordsPerEpoch: ordsPerEpoch,
	}
}

func runSideStacker(numStanding, ordsPerEpoch int) {
	if numStanding < ordsPerEpoch {
		panic("numStanding must be >= minPerEpoch")
	}
	go blockEvery2()

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		runTrader(newSideStacker(true, numStanding, ordsPerEpoch, alpha, false), "STACKER:0")
	}()
	go func() {
		defer wg.Done()
		runTrader(newSideStacker(false, numStanding, ordsPerEpoch, alpha, false), "STACKER:1")
	}()
	wg.Wait()

}

var setupWalletsMtx sync.Mutex

// SetupWallets is part of the Trader interface.
func (s *sideStacker) SetupWallets(m *Mantle) {
	// Only allow this to run one at a time, and mine a block at the end to
	// prevent hitting unused address limit when running multiple sideStackers.
	setupWalletsMtx.Lock()
	defer setupWalletsMtx.Unlock()
	maxActiveOrders := 2 * (s.numStanding + s.ordsPerEpoch)
	baseCoins, quoteCoins, minBaseQty, maxBaseQty, minQuoteQty, maxQuoteQty := walletConfig(maxOrderLots, maxActiveOrders, s.seller)
	m.createWallet(baseSymbol, s.node, minBaseQty, maxBaseQty, baseCoins)
	m.createWallet(quoteSymbol, s.node, minQuoteQty, maxQuoteQty, quoteCoins)
	m.log.Infof("Side Stacker has been initialized with %d target standing orders, %d orders "+
		"per epoch, %s to %s %s balance, and %s to %s %s balance, %d initial %s coins, %d initial %s coins",
		s.numStanding, s.ordsPerEpoch, valString(minBaseQty, baseSymbol), valString(maxBaseQty, baseSymbol), baseSymbol,
		valString(minQuoteQty, quoteSymbol), valString(maxQuoteQty, quoteSymbol), quoteSymbol, baseCoins, baseSymbol, quoteCoins, quoteSymbol)
	<-mine(baseSymbol, alpha)
	<-mine(quoteSymbol, alpha)
}

// HandleNotification is part of the Trader interface.
func (s *sideStacker) HandleNotification(m *Mantle, note core.Notification) {
	switch n := note.(type) {
	case *core.EpochNotification:
		m.log.Debugf("Epoch note received: %s", mustJSON(note))
		if n.MarketID == market {
			// delay the orders, since the epoch note comes before the order
			// book updates associated with the last epoch. Ideally, we want a
			// notification telling us when we have received all order book
			// updates associated with the previous epoch's match cycle.
			go func() {
				select {
				case <-time.After(time.Duration(epochDuration/4) * time.Millisecond):
				case <-ctx.Done():
					return
				}
				s.stack(m)
			}()
			m.replenishBalances()
		}
	case *core.BalanceNote:
		// log.Infof("balance for %s = %d available, %d locked", unbip(n.AssetID), n.Balance.Available, n.Balance.Locked)
	}
}

// func (s *sideStacker) HandleBookNote(_ *Mantle, note *core.BookUpdate) {
// 	// log.Infof("sideStacker got a book note: %s", mustJSON(note))
// }

func (s *sideStacker) stack(m *Mantle) {
	book := m.book()
	midGap := midGap(book)
	rateTweak := func() int64 {
		return int64(rand.Float64() * stackerSpread * float64(midGap))
	}
	worstBuys, worstSells := s.cancellableOrders(m)
	activeBuys, activeSells := len(worstBuys), len(worstSells)
	if activeBuys+activeSells > 2*s.numStanding {
		// If we have a lot of standing orders, cancel at least one, but more
		// if this sidestacker is configured for a high numStanding.
		numCancels := 1 + s.numStanding/3
		ords := worstBuys
		if s.seller {
			ords = worstSells
			activeSells -= numCancels
		} else {
			activeBuys -= numCancels
		}
		for i := 0; i < numCancels; i++ {
			err := m.Cancel(pass, ords[i].ID)
			if err != nil {
				// Be permissive of cancel misses.
				var msgErr *msgjson.Error
				if errors.As(err, &msgErr) && msgErr.Code == msgjson.OrderParameterError {
					continue
				}
				m.fatalError("error canceling order for overloaded side: %v", err)
			}
		}
	}

	var neg int64 = 1
	numNewStanding := s.numStanding - activeBuys
	activeOrders := activeBuys
	if s.seller {
		activeOrders = activeSells
		numNewStanding = s.numStanding - activeSells
		neg = -1
	}
	numNewStanding = clamp(numNewStanding, 0, s.ordsPerEpoch)
	numMatchers := s.ordsPerEpoch - numNewStanding
	m.log.Infof("Side Stacker (seller = %t) placing %d standers and %d matchers. Currently %d active orders",
		s.seller, numNewStanding, numMatchers, activeOrders)

	qty := func() uint64 {
		return uint64(rand.Intn(maxOrderLots-1)+1) * lotSize
	}

	ords := make([]*orderReq, 0, numNewStanding+numMatchers)
	for i := 0; i < numNewStanding; i++ {
		ords = append(ords, &orderReq{
			sell: s.seller,
			qty:  qty(),
			rate: truncate(int64(midGap)-neg*rateTweak(), int64(rateStep)),
		})
	}
	for i := 0; i < numMatchers; i++ {
		ords = append(ords, &orderReq{
			sell: s.seller,
			qty:  qty(),
			rate: truncate(int64(midGap)+neg*rateTweak(), int64(rateStep)),
		})
	}

	if s.metered {
		m.orderMetered(ords, time.Duration(epochDuration)*time.Millisecond)
	} else {
		for _, ord := range ords {
			m.order(s.seller, ord.qty, ord.rate)
		}
	}
}

func (s *sideStacker) cancellableOrders(m *Mantle) (
	worstBuys, worstSells []*core.Order) {

	xcs := m.Exchanges()
	xc := xcs[hostAddr]
	mkt := xc.Markets[market]
	for _, ord := range mkt.Orders {
		cancellable := ord.Status == order.OrderStatusBooked && !ord.Cancelling
		if ord.Status < order.OrderStatusExecuted && cancellable {
			if ord.Sell {
				worstSells = append(worstSells, ord)
			} else {
				worstBuys = append(worstBuys, ord)
			}
		}
	}
	sort.Slice(worstSells, func(i, j int) bool { return worstSells[i].Rate > worstSells[j].Rate })
	sort.Slice(worstBuys, func(i, j int) bool { return worstBuys[i].Rate > worstBuys[j].Rate })
	return
}

func walletConfig(maxLots, maxActiveOrds int, sell bool) (baseCoins, quoteCoins int, minBaseQty, maxBaseQty, minQuoteQty, maxQuoteQty uint64) {
	numCoins := maxActiveOrds
	maxOrders := uint64(maxLots) * uint64(maxActiveOrds)
	minBaseQty = maxOrders * lotSize
	minQuoteQty = calc.BaseToQuote(uint64(defaultMidGap*rateEncFactor), minBaseQty)
	// Ensure enough for registration fees.
	if minBaseQty < 2e8 {
		minBaseQty = 2e8
	}
	if minQuoteQty < 2e8 {
		minQuoteQty = 2e8
	}
	quoteCoins, baseCoins = 1, 1
	if sell {
		baseCoins = numCoins
		// eth fee estimation calls for more reserves. Refunds and
		// redeems also need reserves.
		if quoteSymbol == eth {
			quoteCoins = numCoins
			add := ethRedeemFee * maxOrders
			minQuoteQty += add
		}
		if baseSymbol == eth {
			add := ethInitFee * maxOrders
			minBaseQty += add
		}
	} else {
		quoteCoins = numCoins
		// eth fee estimation calls for more reserves. Refunds and
		// redeems also need reserves.
		if baseSymbol == eth {
			baseCoins = numCoins
			add := ethRedeemFee * maxOrders
			minBaseQty += add
		}
		if quoteSymbol == eth {
			add := ethInitFee * maxOrders
			minQuoteQty += add
		}
	}
	maxBaseQty, maxQuoteQty = 2*minBaseQty, 2*minQuoteQty
	return
}
