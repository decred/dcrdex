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
	minBaseQty, maxBaseQty, minQuoteQty, maxQuoteQty := symmetricWalletConfig(numCoins, uint64(defaultMidGap*rateEncFactor))
	m.createWallet(baseSymbol, alpha, minBaseQty, maxBaseQty, numCoins)
	m.createWallet(quoteSymbol, alpha, minQuoteQty, maxQuoteQty, numCoins)

	m.log.Infof("Sniper has been initialized with %d max orders per epoch"+
		"per epoch, %s to %s %s balance, and %s to %s %s balance, %d initial funding coins",
		s.maxOrdsPerEpoch, valString(minBaseQty, baseSymbol), valString(maxBaseQty, baseSymbol), baseSymbol,
		valString(minQuoteQty, quoteSymbol), valString(maxQuoteQty, quoteSymbol), quoteSymbol, numCoins)

}

// HandleNotification is part of the Trader interface.
func (s *sniper) HandleNotification(m *Mantle, note core.Notification) {
	switch n := note.(type) {
	case *core.EpochNotification:
		if n.MarketID == market {
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
			numCoins := 3 * s.maxOrdsPerEpoch
			book := m.book()
			midGap := midGap(book)
			minBaseQty, maxBaseQty, minQuoteQty, maxQuoteQty := symmetricWalletConfig(numCoins, midGap)
			wmm := walletMinMax{
				baseID:  {min: minBaseQty, max: maxBaseQty},
				quoteID: {min: minQuoteQty, max: maxQuoteQty},
			}
			m.replenishBalances(wmm)
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
			qty = calc.BaseToQuote(ord.MsgRate, qty)
			qty = uint64(float64(qty) * marketBuyBuffer)
		}
		m.marketOrder(sell, qty)
	}
}
