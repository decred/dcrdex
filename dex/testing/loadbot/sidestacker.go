// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package main

import (
	"errors"
	"math/rand"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"decred.org/dcrdex/client/core"
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/calc"
	"decred.org/dcrdex/dex/msgjson"
	"decred.org/dcrdex/dex/order"
	"decred.org/dcrdex/dex/utils"
)

const stackerSpread = 50.0

// The sideStacker is a Trader that attempts to attain order book depth for one
// side before submitting taker orders for the other. There is some built-in
// randomness to the rates and quantities of the sideStacker's orders.
type sideStacker struct {
	log     dex.Logger
	seller  bool
	metered bool
	// numStanding is the targeted number of standing limit orders. If there
	// are fewer than numStanding orders on the book more are placed. If
	// less some are canceled. Some are also canceled when the market is
	// moving in order to place new orders close to the current mid gap.
	numStanding int
	// ordsPerEpoch is the exact number of orders the sideStacker will place
	// each epoch. If there are already enough standing limit orders,
	// sideStacker will place orders to match immediately.
	ordsPerEpoch int
	// overLimit tracks whether the previous epoch hit the match limit.
	// When true, matchers are skipped for one epoch to allow both sides
	// to replenish standing orders before crossing the spread again.
	overLimit atomic.Bool
	// oscillator will cause the side stacker rate to move up and down over
	// time. Used for a sideways market. Shared between side stackers so
	// that they push in the same direction. Rate moves up from
	// [0,oscInterval/2) and down from [oscInterval/2,oscInterval). One is
	// added to the oscillator ever epoch and it is reset to zero once it
	// reaches oscInterval.
	oscillator *uint64 // atomic
	// oscillatorWrite designates a side stacker that can write to the
	// oscillator.
	oscillatorWrite bool
}

var _ Trader = (*sideStacker)(nil)

func newSideStacker(numStanding, ordsPerEpoch int, seller, metered, oscillatorWrite bool,
	oscillator *uint64, log dex.Logger) *sideStacker {

	return &sideStacker{
		log:             log,
		seller:          seller,
		metered:         metered,
		numStanding:     numStanding,
		ordsPerEpoch:    ordsPerEpoch,
		oscillatorWrite: oscillatorWrite,
		oscillator:      oscillator,
	}
}

func runSideStacker(numStanding, ordsPerEpoch int) {
	if numStanding == 0 {
		numStanding = 10
	}
	if ordsPerEpoch == 0 {
		ordsPerEpoch = 5
	}
	if numStanding < ordsPerEpoch {
		panic("numStanding must be >= minPerEpoch")
	}
	var oscillator uint64
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		seller, metered, oscillatorWrite := true, false, true
		runTrader(newSideStacker(numStanding, ordsPerEpoch, seller, metered, oscillatorWrite,
			&oscillator, log.SubLogger("STACKER:0")), "STACKER:0")
	}()
	go func() {
		defer wg.Done()
		seller, metered, oscillatorWrite := false, false, false
		runTrader(newSideStacker(numStanding, ordsPerEpoch, seller, metered, oscillatorWrite,
			&oscillator, log.SubLogger("STACKER:1")), "STACKER:1")
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
	baseCoins, quoteCoins, minBaseQty, maxBaseQty, minQuoteQty, maxQuoteQty := walletConfig(maxOrderLots, maxActiveOrders, s.seller, uint64(defaultMidGap*rateEncFactor))
	m.createWallet(baseSymbol, minBaseQty, maxBaseQty, baseCoins)
	m.createWallet(quoteSymbol, minQuoteQty, maxQuoteQty, quoteCoins)
	s.log.Infof("Side Stacker has been initialized with %d target standing orders, %d orders "+
		"per epoch, %s to %s %s balance, and %s to %s %s balance, %d initial %s coins, %d initial %s coins",
		s.numStanding, s.ordsPerEpoch, fmtAtoms(minBaseQty, baseSymbol), fmtAtoms(maxBaseQty, baseSymbol), baseSymbol,
		fmtAtoms(minQuoteQty, quoteSymbol), fmtAtoms(maxQuoteQty, quoteSymbol), quoteSymbol, baseCoins, baseSymbol, quoteCoins, quoteSymbol)
}

// HandleNotification is part of the Trader interface.
func (s *sideStacker) HandleNotification(m *Mantle, note core.Notification) {
	switch n := note.(type) {
	case *core.EpochNotification:
		s.log.Debugf("Epoch note received: %s", mustJSON(note))
		if n.MarketID == market {
			// delay the orders, since the epoch note comes before the order
			// book updates associated with the last epoch. Ideally, we want a
			// notification telling us when we have received all order book
			// updates associated with the previous epoch's match cycle.
			go func() {
				select {
				// TODO: This delay is a little arbitrary. Maybe we shouldn't
				// delay at all or the delay should be randomized.
				case <-time.After(time.Duration(epochDuration/4) * time.Millisecond):
				case <-ctx.Done():
					return
				}
				s.stack(m, n.Epoch)
			}()
			maxActiveOrders := 2 * (s.numStanding + s.ordsPerEpoch)
			book := m.book()
			midGap := midGap(book)
			_, _, minBaseQty, maxBaseQty, minQuoteQty, maxQuoteQty := walletConfig(maxOrderLots, maxActiveOrders, s.seller, midGap)
			wmm := walletMinMax{
				baseID:  {min: minBaseQty, max: maxBaseQty},
				quoteID: {min: minQuoteQty, max: maxQuoteQty},
			}
			m.replenishBalances(wmm)
		}
	case *core.BalanceNote:
		s.log.Infof("Balance: %s = %d available, %d locked", unbip(n.AssetID), n.Balance.Available, n.Balance.Locked)
	}
}

// func (s *sideStacker) HandleBookNote(_ *Mantle, note *core.BookUpdate) {
// 	// log.Infof("sideStacker got a book note: %s", mustJSON(note))
// }

func (s *sideStacker) stack(m *Mantle, currentEpoch uint64) {
	book := m.book()
	midGap := midGap(book)
	xcs := m.Exchanges()
	xc := xcs[hostAddr]
	mkt := xc.Markets[market]
	minRate := mkt.MinimumRate
	worstBuys, worstSells := s.cancellableOrders(m, currentEpoch)
	activeBuys, activeSells := len(worstBuys), len(worstSells)
	cancelOrds := func(ords []*core.Order) {
		for _, o := range ords {
			err := m.Cancel(o.ID)
			if err != nil {
				// Be permissive of cancel misses and duplicate
				// cancel attempts that can arise from stale
				// Exchanges() snapshots.
				var msgErr *msgjson.Error
				if errors.As(err, &msgErr) && msgErr.Code == msgjson.OrderParameterError &&
					strings.Contains(err.Error(), missedCancelErrStr) {
					continue
				}
				if strings.Contains(err.Error(), "only one cancel order") {
					continue
				}
				m.fatalError("error canceling order for overloaded side: %v", err)
			}
		}
	}
	oscillator := atomic.LoadUint64(s.oscillator)
	// Cancel stale standing orders that are far from the current price.
	// When oscillating, cancel more aggressively to keep the book fresh
	// as the midGap moves each epoch.
	oscCancels := 0
	if oscillate {
		oscCancels = s.ordsPerEpoch / 2
		if oscCancels < 1 {
			oscCancels = 1
		}
	}
	if s.seller {
		numCancels := activeSells - s.numStanding
		if numCancels < oscCancels &&
			(rateIncrease < 0 ||
				(oscillate && oscillator >= oscInterval/2)) {
			numCancels = oscCancels
		}
		if numCancels > activeSells {
			numCancels = activeSells
		}
		if numCancels > 0 {
			cancelOrds(worstSells[:numCancels])
		}
	} else {
		numCancels := activeBuys - s.numStanding
		if numCancels < oscCancels &&
			(rateIncrease > 0 ||
				(oscillate && oscillator < oscInterval/2)) {
			numCancels = oscCancels
		}
		if numCancels > activeBuys {
			numCancels = activeBuys
		}
		if numCancels > 0 {
			cancelOrds(worstBuys[:numCancels])
		}
	}
	var neg int64 = -1
	numNewStanding := s.numStanding - activeBuys
	activeOrders := activeBuys
	if s.seller {
		activeOrders = activeSells
		numNewStanding = s.numStanding - activeSells
		neg = 1
	}
	numNewStanding = utils.Clamp(numNewStanding, 0, s.ordsPerEpoch)
	numMatchers := s.ordsPerEpoch - numNewStanding
	if s.overLimit.Load() {
		s.log.Infof("Seller = %t skipping matchers to replenish book depth after over-limit", s.seller)
		numMatchers = 0
		// Reset so we only skip for one epoch. If the condition
		// persists, it will be set again when orders are placed.
		s.overLimit.Store(false)
	}
	// Don't place matchers if the opposite side of the book is thin.
	// Without this check, one side's matchers consume the other side's
	// standers faster than they can be replenished, leaving the book
	// one-sided.
	oppositeSide := len(book.Buys)
	if !s.seller {
		oppositeSide = len(book.Sells)
	}
	if numMatchers > 0 && oppositeSide < s.numStanding/2 {
		s.log.Infof("Seller = %t skipping %d matchers, opposite book side thin (%d orders)",
			s.seller, numMatchers, oppositeSide)
		numMatchers = 0
	}
	s.log.Infof("Seller = %t placing %d standers and %d matchers. Currently %d active orders",
		s.seller, numNewStanding, numMatchers, activeOrders)

	qty := func() uint64 {
		return uint64(rand.Intn(maxOrderLots-1)+1) * lotSize
	}

	if oscillate {
		if randomOsc && s.oscillatorWrite {
			// Randomly flip the direction about four times per
			// interval.
			if rand.Intn(int(oscInterval/4)) == 0 {
				oscillator += oscInterval / 2
				oscillator %= oscInterval
			}
		}
		// Compute a centered triangle wave displacement from the
		// oscillator counter. The price oscillates symmetrically above
		// and below defaultMidGap. oscStep controls amplitude as a
		// percentage of midGap (e.g. 50 = ±25% centered oscillation).
		halfInterval := int64(oscInterval / 2)
		pos := int64(oscillator % oscInterval)
		peak := int64(midGap) / 100 * int64(oscStep)
		var displacement int64
		if pos < halfInterval {
			displacement = peak * pos / halfInterval
		} else {
			displacement = peak * (int64(oscInterval) - pos) / halfInterval
		}
		// Center the wave around zero.
		displacement -= peak / 2
		shifted := int64(midGap) + displacement
		if shifted < int64(rateStep) {
			shifted = int64(rateStep)
		}
		midGap = uint64(shifted)
		if s.oscillatorWrite {
			oscillator += 1
			oscillator %= oscInterval
			atomic.StoreUint64(s.oscillator, oscillator)
		}
	}
	rate := func(doMatch bool) uint64 {
		ne := neg
		// For the stagnant market, flip matches across the mid gap to
		// match. Moving markets will match naturally.
		if doMatch && rateIncrease == 0 {
			ne = -ne
		}
		rateTweak := int64(rand.Float64() * stackerSpread * float64(rateStep))
		rate := float64(int64(midGap) + ne*rateTweak)
		// Standing matches for the moving market can be ensured by not
		// applying the rate Increase.
		if doMatch {
			rate += float64(rateIncrease)
		}
		if rate < 0 {
			rate = float64(rateStep)
		}
		r := truncate(int64(rate), int64(rateStep))
		if r < minRate {
			// Round minRate up to the nearest rateStep multiple.
			r = ((minRate + rateStep - 1) / rateStep) * rateStep
		}
		return r
	}

	ords := make([]*orderReq, 0, numNewStanding+numMatchers)
	for i := 0; i < numNewStanding; i++ {
		ords = append(ords, &orderReq{
			sell: s.seller,
			qty:  qty(),
			rate: rate(false),
		})
	}
	for i := 0; i < numMatchers; i++ {
		ords = append(ords, &orderReq{
			sell: s.seller,
			qty:  qty(),
			rate: rate(true),
		})
	}

	if s.metered {
		m.orderMetered(ords, time.Duration(epochDuration)*time.Millisecond, &s.overLimit)
	} else {
		for _, ord := range ords {
			err := m.order(s.seller, ord.qty, ord.rate)
			if err != nil {
				if isRecoverableOrderError(err) {
					s.overLimit.Store(true)
					break
				}
			} else {
				s.overLimit.Store(false)
			}
		}
	}
}

func (s *sideStacker) cancellableOrders(m *Mantle, currentEpoch uint64) (
	worstBuys, worstSells []*core.Order) {

	xcs := m.Exchanges()
	xc := xcs[hostAddr]
	mkt := xc.Markets[market]
	// Make sure to give the order one epoch so we are not penalized
	// for canceling.
	epoch := atomic.LoadUint64(&currentEpoch)
	for _, ord := range mkt.Orders {
		if ord.Status != order.OrderStatusBooked || ord.Cancelling || ord.Epoch+1 >= epoch {
			continue
		}
		if ord.Sell {
			worstSells = append(worstSells, ord)
		} else {
			worstBuys = append(worstBuys, ord)
		}
	}
	sort.Slice(worstSells, func(i, j int) bool { return worstSells[i].Rate > worstSells[j].Rate })
	sort.Slice(worstBuys, func(i, j int) bool { return worstBuys[i].Rate < worstBuys[j].Rate })
	return
}

func walletConfig(maxLots, maxActiveOrds int, sell bool, midGap uint64) (baseCoins, quoteCoins int, minBaseQty, maxBaseQty, minQuoteQty, maxQuoteQty uint64) {
	numCoins := maxActiveOrds
	maxOrders := uint64(maxLots) * uint64(maxActiveOrds)
	minBaseQty = maxOrders * lotSize
	minQuoteQty = calc.BaseToQuote(midGap, minBaseQty)
	// Ensure enough for registration fees.
	minBaseQty += 50e8
	minQuoteQty += 50e8
	quoteCoins, baseCoins = 1, 1
	if sell {
		baseCoins = numCoins
		// eth fee estimation calls for more reserves. Refunds and
		// redeems also need reserves.
		// TODO: polygon and tokens
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
