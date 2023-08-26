// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package main

import (
	"strconv"
	"sync"

	"decred.org/dcrdex/client/core"
)

// The pingPonger is a Trader that simply sends single-lot, mid-gap rate orders
// that alternate between buys and sells. A new order is sent every time an
// "audit" request is seen by the client
type pingPonger struct{}

var _ Trader = (*pingPonger)(nil)

func runPingPong(n int) {
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			runTrader(&pingPonger{}, "PINGPONG:"+strconv.Itoa(i))
		}(i)
	}
	wg.Wait()
}

// SetupWallets is part of the Trader interface.
func (p *pingPonger) SetupWallets(m *Mantle) {
	numCoins := 4
	minBaseQty, maxBaseQty, minQuoteQty, maxQuoteQty := symmetricWalletConfig(numCoins, uint64(defaultMidGap*rateEncFactor))
	m.createWallet(baseSymbol, alpha, minBaseQty, maxBaseQty, numCoins)
	m.createWallet(quoteSymbol, alpha, minQuoteQty, maxQuoteQty, numCoins)
	m.log.Infof("Ping Ponger has been initialized with %s to %s %s balance, "+
		"and %s to %s %s balance, %d initial funding coins",
		valString(minBaseQty, baseSymbol), valString(maxBaseQty, baseSymbol), baseSymbol,
		valString(minQuoteQty, quoteSymbol), valString(maxQuoteQty, quoteSymbol), quoteSymbol, numCoins)
}

// HandleNotification is part of the Trader interface.
func (p *pingPonger) HandleNotification(m *Mantle, note core.Notification) {
	switch n := note.(type) {
	case *core.BondPostNote:
		if n.Topic() == core.TopicAccountRegistered {
			p.buy(m)
			p.sell(m)
		}
	case *core.MatchNote:
		switch n.Topic() {
		case core.TopicAudit:
			ord, err := m.Order(n.OrderID)
			if err != nil {
				m.fatalError("Error fetching order for match: %v", err)
				return
			}
			if ord.Sell {
				p.sell(m)
			} else {
				p.buy(m)
			}
		}
	case *core.EpochNotification:
		if n.MarketID == market {
			numCoins := 4
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
		log.Infof("pingponger balance: %s = %d available, %d locked", unbip(n.AssetID), n.Balance.Available, n.Balance.Locked)
	}
}

// func (p *pingPonger) HandleBookNote(m *Mantle, note *core.BookUpdate) {
// 	log.Infof("pingPonger got a book note: %s", mustJSON(note))
// }

func (p *pingPonger) sell(m *Mantle) {
	m.order(true, lotSize, m.truncatedMidGap())
}

func (p *pingPonger) buy(m *Mantle) {
	m.order(false, lotSize, m.truncatedMidGap())
}
