// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package main

import (
	"errors"
	"fmt"
	"math/rand"
	"strings"
	"sync"
	"time"

	"decred.org/dcrdex/client/core"
	"decred.org/dcrdex/dex/msgjson"
	"decred.org/dcrdex/dex/order"
)

// whale erradicaly pushes the market one way or another randomly with huge buys
// and sells moving the mid gap very quickly.
type whale struct {
	// active is true for only one whale. whales are broken up in order to stay
	// under trading limits.
	active bool
	// cycles since last whaling.
	cycles int
}

const (
	numWhale  = 10
	maxOrders = 10
)

var (
	_           Trader = (*whale)(nil)
	wMantlesMtx sync.Mutex
	wMantles    []*Mantle
)

func newWhale(n int) *whale {
	t := &whale{
		active: n == 0,
		cycles: numWhale + 7, // Ready to whale.
	}
	return t
}

func runWhale() {
	var wg sync.WaitGroup
	wg.Add(numWhale)
	for i := 0; i < numWhale; i++ {
		n := i
		go func() {
			defer wg.Done()
			runTrader(newWhale(n), fmt.Sprintf("WHALE:%d", n))
		}()
		time.Sleep(time.Second)
	}
	wg.Wait()
}

// SetupWallets is part of the Trader interface.
func (*whale) SetupWallets(m *Mantle) {
	numCoins := 20
	minBaseQty, maxBaseQty, minQuoteQty, maxQuoteQty := symmetricWalletConfig(numCoins, uint64(defaultMidGap*float64(rateEncFactor)+float64(rateStep)*(1+whalePercent*2 /* twice for buffering */)))
	m.createWallet(baseSymbol, alpha, minBaseQty, maxBaseQty, numCoins)
	m.createWallet(quoteSymbol, alpha, minQuoteQty, maxQuoteQty, numCoins)

	m.log.Infof("Whale has been initialized with  %s to %s %s balance, and %s to %s %s balance, %d initial funding coins",
		valString(minBaseQty, baseSymbol), valString(maxBaseQty, baseSymbol), baseSymbol,
		valString(minQuoteQty, quoteSymbol), valString(maxQuoteQty, quoteSymbol), quoteSymbol, numCoins)

}

func (*whale) cancelOrders(currentEpoch uint64) {
	for _, m := range wMantles {
		xcs := m.Exchanges()
		xc := xcs[hostAddr]
		mkt := xc.Markets[market]
		// Make sure to give the order one epoch so we are not penalized
		// for canceling.
		for _, ord := range mkt.Orders {
			if ord.Status != order.OrderStatusBooked || ord.Cancelling || ord.Epoch+1 >= currentEpoch {
				continue
			}
			err := m.Cancel(ord.ID)
			if err != nil {
				// Be permissive of cancel misses.
				var msgErr *msgjson.Error
				if errors.As(err, &msgErr) && msgErr.Code == msgjson.OrderParameterError &&
					strings.Contains(err.Error(), missedCancelErrStr) {
					continue
				}
				m.fatalError("error canceling order for overloaded side: %v", err)
			}
		}
	}
}

// HandleNotification is part of the Trader interface.
func (w *whale) HandleNotification(m *Mantle, note core.Notification) {
	switch n := note.(type) {
	case *core.BondPostNote:
		// Once registration is complete, register for a book feed.
		if n.Topic() == core.TopicAccountRegistered {
			book := m.book()
			rate := midGap(book)
			if !liveMidGap {
				rate += uint64(float64(rate) * whalePercent)
			}
			minBaseQty, maxBaseQty, minQuoteQty, maxQuoteQty := symmetricWalletConfig(20, rate)
			wmm := walletMinMax{
				baseID:  {min: minBaseQty, max: maxBaseQty},
				quoteID: {min: minQuoteQty, max: maxQuoteQty},
			}
			m.replenishBalances(wmm)
			wMantlesMtx.Lock()
			wMantles = append(wMantles, m)
			wMantlesMtx.Unlock()
		}
	case *core.EpochNotification:
		// The bot at index 0 controls them all.
		if !w.active {
			return
		}
		wMantlesMtx.Lock()
		nMan := len(wMantles)
		wMantlesMtx.Unlock()
		if nMan != numWhale {
			m.log.Debug("whale waiting for other mantles")
			return
		}
		if n.MarketID != market {
			return
		}
		const coolDownPeriod = 5
		c := w.cycles
		w.cycles++
		switch {
		// Refresh balances one epoch at a time.
		case c < numWhale:
			book := m.book()
			rate := midGap(book)
			if !liveMidGap {
				rate += uint64(float64(rate) * whalePercent)
			}
			minBaseQty, maxBaseQty, minQuoteQty, maxQuoteQty := symmetricWalletConfig(20, rate)
			wmm := walletMinMax{
				baseID:  {min: minBaseQty, max: maxBaseQty},
				quoteID: {min: minQuoteQty, max: maxQuoteQty},
			}
			wMantles[c].replenishBalances(wmm)
		// Give a small cooldown. Then cancel all orders.
		case c == numWhale+coolDownPeriod:
			w.cancelOrders(n.Epoch)
		// Any other epoch cycle before our threshold does nothing.
		case c < numWhale+coolDownPeriod+2:
		// We've reached enough cycles. Randomly whale.
		case rand.Intn(int(whaleFrequency)) == 0:
			go w.whale(m)
			w.cycles = 0
		}
	case *core.BalanceNote:
		m.log.Infof("whale balance: %s = %d available, %d locked", unbip(n.AssetID), n.Balance.Available, n.Balance.Locked)
	}
}

// whale picks a random target and tries its hardest to push the market there.
// Only use once wMantles have all been set up.
func (*whale) whale(m *Mantle) {
	book := m.book()
	midGap := midGap(book)
	var target uint64
	// If we are trying to stay close to the mid gap, have the whale push
	// us there. Otherwise set a random target.
	if liveMidGap {
		r, err := liveRate()
		if err != nil {
			m.log.Errorf("error retrieving live rates: %v", err)
			target = midGap
		} else {
			target = truncate(int64(r*float64(rateEncFactor)), int64(rateStep))
		}
	} else {
		tweak := rand.Float64() * float64(1-(2*rand.Intn(2))) * float64(midGap) * whalePercent
		target = rateStep
		if tweak > 0 || int64(midGap)+int64(tweak) > int64(rateStep) {
			target = truncate(int64(midGap)+int64(tweak), int64(rateStep))
		}
	}
	conversionRatio := float64(conversionFactors[quoteSymbol]) / float64(conversionFactors[baseSymbol])
	m.log.Infof("TS whaling at %v rate.", float64(target)/float64(rateEncFactor)/conversionRatio)
	sell := true
	if target > midGap {
		sell = false
	}

	side := book.Sells
	if sell {
		side = book.Buys
	}
	var qty uint64
	for _, o := range side {
		if (sell && o.MsgRate > target) ||
			(!sell && o.MsgRate < target) {
			qty += o.QtyAtomic
		}
	}
	for _, o := range book.Epoch {
		if o.Sell != sell {
			if (sell && o.MsgRate > target) ||
				(!sell && o.MsgRate < target) {
				qty += o.QtyAtomic
			}
		}
	}

	qty = truncate(int64((130*qty/100)/numWhale), int64(lotSize))

	lots := qty / lotSize
	nMaxOrd := lots / maxOrderLots
	rem := lots % maxOrderLots
	if nMaxOrd > maxOrders {
		nMaxOrd = maxOrders
		rem = 0
	}

	m.log.Infof("Whaling with %d lots at %v", lots, float64(target)/float64(rateEncFactor)/conversionRatio)

	for _, man := range wMantles {
		for i := 0; i < int(nMaxOrd); i++ {
			man.order(sell, maxOrderLots*lotSize, target)
		}
		if rem > 0 {
			man.order(sell, rem*lotSize, target)
		}
	}
}
