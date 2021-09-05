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
	// For this program, we'll want to mine a block about every epoch or so.
	go moreThanOneBlockPer()

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
	minBaseQty, maxBaseQty, minQuoteQty, maxQuoteQty := symmetricWalletConfig(numCoins)
	m.createWallet(dcr, alpha, minBaseQty, maxBaseQty, numCoins)
	m.createWallet(btc, alpha, minQuoteQty, maxQuoteQty, numCoins)
	m.log.Infof("Ping Ponger has been initialized with %s to %s dcr balance, "+
		"and %s to %s btc balance, %d initial funding coins",
		valString(minBaseQty), valString(maxBaseQty),
		valString(minQuoteQty), valString(maxQuoteQty), numCoins)
}

// HandleNotification is part of the Trader interface.
func (p *pingPonger) HandleNotification(m *Mantle, note core.Notification) {
	switch n := note.(type) {
	case *core.FeePaymentNote:
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
