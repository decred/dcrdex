// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package main

import (
	"math"
	"math/rand"
	"time"

	"decred.org/dcrdex/client/core"
	"decred.org/dcrdex/dex/calc"
)

// sniper is a Trader that will randomly place orders targeting existing orders.
type sniper struct {
	// maxOrdsPerEpoch is the maximum number of orders that can be placed in a
	// single epoch. The actual number will be a random number in the range
	// [0, maxOrdsPerEpoch].
	maxOrdsPerEpoch int
}

var _ Trader = (*sniper)(nil)

func newSniper(maxOrdsPerEpoch int) *sniper {
	return &sniper{
		maxOrdsPerEpoch: maxOrdsPerEpoch,
	}
}

// SetupWallets is part of the Trader interface.
func (s *sniper) SetupWallets(m *Mantle) {
	numCoins := 3 * s.maxOrdsPerEpoch
	minBaseQty, maxBaseQty, minQuoteQty, maxQuoteQty := symmetricWalletConfig(numCoins)
	m.createWallet(dcr, alpha, minBaseQty, maxBaseQty, numCoins)
	m.createWallet(btc, alpha, minQuoteQty, maxQuoteQty, numCoins)

	m.log.Infof("Sniper has been initialized with %d max orders per epoch"+
		"per epoch, %s to %s dcr balance, and %s to %s btc balance, %d initial funding coins",
		s.maxOrdsPerEpoch, valString(minBaseQty), valString(maxBaseQty),
		valString(minQuoteQty), valString(maxQuoteQty), numCoins)

}

// HandleNotification is part of the Trader interface.
func (s *sniper) HandleNotification(m *Mantle, note core.Notification) {
	switch n := note.(type) {
	case *core.EpochNotification:
		if n.MarketID == dcrBtcMarket {
			// delay the sniper, since the epoch note comes before the order
			// book updates associated with the last epoch.
			go func() {
				select {
				case <-time.After(time.Duration(epochDuration/4) * time.Millisecond):
				case <-ctx.Done():
					return
				}
				s.snipe(m)
			}()
		}
	case *core.BalanceNote:
		log.Infof("sniper balance: %s = %d available, %d locked", unbip(n.AssetID), n.Balance.Available, n.Balance.Locked)
	}
}

// func (s *sniper) HandleBookNote(_ *Mantle, note *core.BookUpdate) {
// 	// log.Infof("sideStacker got a book note: %s", mustJSON(note))
// }

// snipe picks a random order and places a market order in an attempt to match
// it exactly.
func (s *sniper) snipe(m *Mantle) {
	book := m.book()

	var sell bool
	if len(book.Sells) == 0 {
		if len(book.Buys) == 0 {
			log.Infof("Sniper skipping ordering for empty book")
			return
		}
		sell = true
	} else if len(book.Buys) > 0 {
		sell = rand.Float32() > 0.5
	}

	maxOrders := int(math.Round(float64(s.maxOrdsPerEpoch) * rand.Float64()))
	targets := book.Sells[:clamp(maxOrders, 0, len(book.Sells))]
	if sell {
		targets = book.Buys[:clamp(maxOrders, 0, len(book.Buys))]
	}
	for _, ord := range targets {
		qty := ord.QtyAtomic
		if !sell {
			if qty/lotSize == 1 {
				qty *= 2
			}
			qty = calc.BaseToQuote(encodeRate(ord.Rate), qty)

		}
		m.marketOrder(sell, qty)
	}
}

func symmetricWalletConfig(numCoins int) (
	minBaseQty, maxBaseQty, minQuoteQty, maxQuoteQty uint64) {

	maxBaseQty = maxOrderLots * uint64(numCoins) * lotSize
	minBaseQty = maxBaseQty / 2
	defaultRate := truncate(defaultBtcPerDcr*1e8, int64(rateStep))
	maxQuoteQty = calc.BaseToQuote(defaultRate, maxBaseQty)
	minQuoteQty = maxQuoteQty / 2
	return
}
