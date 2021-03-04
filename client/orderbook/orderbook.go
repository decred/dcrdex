// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package orderbook

import (
	"bytes"
	"fmt"
	"sync"
	"sync/atomic"

	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/msgjson"
	"decred.org/dcrdex/dex/order"
)

// Order represents an ask or bid.
type Order struct {
	OrderID  order.OrderID
	Side     uint8
	Quantity uint64
	Rate     uint64
	Time     uint64
	// Epoch is only used in the epoch queue, otherwise it is ignored.
	Epoch uint64
}

// copy creates a copy. Note that the OrderID is not a deep copy.
func (o *Order) copy() *Order {
	return &(*o)
}

func (o *Order) sell() bool {
	return o.Side == msgjson.SellOrderNum
}

// RemoteOrderBook defines the functions a client tracked order book
// must implement.
type RemoteOrderBook interface {
	// Sync instantiates a client tracked order book with the
	// current order book snapshot.
	Sync(*msgjson.OrderBook)
	// Book adds a new order to the order book.
	Book(*msgjson.BookOrderNote)
	// Unbook removes an order from the order book.
	Unbook(*msgjson.UnbookOrderNote) error
}

// CachedOrderNote represents a cached order not entry.
type cachedOrderNote struct {
	Route     string
	OrderNote interface{}
}

// OrderBook represents a client tracked order book.
type OrderBook struct {
	log      dex.Logger
	seqMtx   sync.Mutex
	seq      uint64
	marketID string

	noteQueueMtx sync.Mutex
	noteQueue    []*cachedOrderNote

	ordersMtx sync.Mutex
	orders    map[order.OrderID]*Order

	buys  *bookSide
	sells *bookSide

	syncedMtx sync.Mutex
	synced    bool

	epochMtx     sync.Mutex
	currentEpoch uint64
	proofedEpoch uint64
	epochQueues  map[uint64]*EpochQueue

	// feeRates is a separate struct to account for atomic field alignment in
	// 32-bit systems. See also https://golang.org/pkg/sync/atomic/#pkg-note-BUG
	feeRates struct {
		base  uint64
		quote uint64
	}
}

// NewOrderBook creates a new order book.
func NewOrderBook(logger dex.Logger) *OrderBook {
	ob := &OrderBook{
		log:         logger,
		noteQueue:   make([]*cachedOrderNote, 0, 16),
		orders:      make(map[order.OrderID]*Order),
		buys:        newBookSide(descending),
		sells:       newBookSide(ascending),
		epochQueues: make(map[uint64]*EpochQueue),
	}
	return ob
}

// BaseFeeRate is the last reported base asset fee rate.
func (ob *OrderBook) BaseFeeRate() uint64 {
	return atomic.LoadUint64(&ob.feeRates.base)
}

// QuoteFeeRate is the last reported quote asset fee rate.
func (ob *OrderBook) QuoteFeeRate() uint64 {
	return atomic.LoadUint64(&ob.feeRates.quote)
}

// setSynced sets the synced state of the order book.
func (ob *OrderBook) setSynced(value bool) {
	ob.syncedMtx.Lock()
	ob.synced = value
	ob.syncedMtx.Unlock()
}

// isSynced returns the synced state of the order book.
func (ob *OrderBook) isSynced() bool {
	ob.syncedMtx.Lock()
	defer ob.syncedMtx.Unlock()
	return ob.synced
}

// setSeq should be called whenever a sequenced message is received. If seq is
// out of sequence, an error is logged.
func (ob *OrderBook) setSeq(seq uint64) {
	ob.seqMtx.Lock()
	defer ob.seqMtx.Unlock()
	if seq != ob.seq+1 {
		ob.log.Errorf("notification received out of sync. %d != %d - 1", ob.seq, seq)
	}
	if seq > ob.seq {
		ob.seq = seq
	}
}

// cacheOrderNote caches an order note.
func (ob *OrderBook) cacheOrderNote(route string, entry interface{}) error {
	note := new(cachedOrderNote)

	switch route {
	case msgjson.BookOrderRoute, msgjson.UnbookOrderRoute, msgjson.UpdateRemainingRoute:
		note.Route = route
		note.OrderNote = entry

		ob.noteQueueMtx.Lock()
		ob.noteQueue = append(ob.noteQueue, note)
		ob.noteQueueMtx.Unlock()

		return nil

	default:
		return fmt.Errorf("unknown route provided %s", route)
	}
}

// processCachedNotes processes all cached notes, each processed note is
// removed from the cache.
func (ob *OrderBook) processCachedNotes() error {
	ob.noteQueueMtx.Lock()
	defer ob.noteQueueMtx.Unlock()

	ob.log.Debugf("Processing %d cached order notes", len(ob.noteQueue))
	for len(ob.noteQueue) > 0 {
		var entry *cachedOrderNote
		entry, ob.noteQueue = ob.noteQueue[0], ob.noteQueue[1:] // so much for preallocating

		switch entry.Route {
		case msgjson.BookOrderRoute:
			note, ok := entry.OrderNote.(*msgjson.BookOrderNote)
			if !ok {
				panic("failed to cast cached book order note" +
					" as a BookOrderNote")
			}
			err := ob.book(note, true)
			if err != nil {
				return err
			}

		case msgjson.UnbookOrderRoute:
			note, ok := entry.OrderNote.(*msgjson.UnbookOrderNote)
			if !ok {
				panic("failed to cast cached unbook order note" +
					" as an UnbookOrderNote")
			}
			err := ob.unbook(note, true)
			if err != nil {
				return err
			}

		case msgjson.UpdateRemainingRoute:
			note, ok := entry.OrderNote.(*msgjson.UpdateRemainingNote)
			if !ok {
				panic("failed to cast cached update_remaining note" +
					" as an UnbookOrderNote")
			}
			err := ob.updateRemaining(note, true)
			if err != nil {
				return err
			}

		default:
			return fmt.Errorf("unknown cached note route "+
				" provided: %s", entry.Route)
		}
	}

	return nil
}

// Sync updates a client tracked order book with an order book snapshot. It is
// an error if the the OrderBook is already synced.
func (ob *OrderBook) Sync(snapshot *msgjson.OrderBook) error {
	if ob.isSynced() {
		return fmt.Errorf("order book is already synced")
	}
	return ob.Reset(snapshot)
}

// Reset forcibly updates a client tracked order book with an order book
// snapshot. This resets the sequence.
// TODO: eliminate this and half of the mutexes!
func (ob *OrderBook) Reset(snapshot *msgjson.OrderBook) error {
	// Don't use setSeq here, since this message is the seed and is not expected
	// to be 1 more than the current seq value.
	ob.seqMtx.Lock()
	ob.seq = snapshot.Seq
	ob.seqMtx.Unlock()

	atomic.StoreUint64(&ob.feeRates.base, snapshot.BaseFeeRate)
	atomic.StoreUint64(&ob.feeRates.quote, snapshot.QuoteFeeRate)

	ob.marketID = snapshot.MarketID

	err := func() error { // Using a function for mutex management with defer.
		ob.ordersMtx.Lock()
		defer ob.ordersMtx.Unlock()

		ob.orders = make(map[order.OrderID]*Order)
		ob.buys = newBookSide(descending)
		ob.sells = newBookSide(ascending)
		for _, o := range snapshot.Orders {
			if len(o.OrderID) != order.OrderIDSize {
				return fmt.Errorf("expected order id length of %d, got %d", order.OrderIDSize, len(o.OrderID))
			}

			var oid order.OrderID
			copy(oid[:], o.OrderID)
			order := &Order{
				OrderID:  oid,
				Side:     o.Side,
				Quantity: o.Quantity,
				Rate:     o.Rate,
				Time:     o.Time,
			}

			ob.orders[order.OrderID] = order

			// Append the order to the order book.
			switch o.Side {
			case msgjson.BuyOrderNum:
				ob.buys.Add(order)

			case msgjson.SellOrderNum:
				ob.sells.Add(order)

			default:
				ob.log.Errorf("unknown order side provided: %d", o.Side)
			}
		}
		return nil
	}()
	if err != nil {
		return err
	}

	// Process cached order notes.
	err = ob.processCachedNotes()
	if err != nil {
		return err
	}

	ob.setSynced(true)

	return nil
}

// book is the workhorse of the exported Book function. It allows booking
// cached and uncached order notes.
func (ob *OrderBook) book(note *msgjson.BookOrderNote, cached bool) error {
	if ob.marketID != note.MarketID {
		return fmt.Errorf("invalid note market id %s", note.MarketID)
	}

	if !cached {
		// Cache the note if the order book is not synced.
		if !ob.isSynced() {
			return ob.cacheOrderNote(msgjson.BookOrderRoute, note)
		}
	}

	ob.setSeq(note.Seq)

	if len(note.OrderID) != order.OrderIDSize {
		return fmt.Errorf("expected order id length of %d, got %d",
			order.OrderIDSize, len(note.OrderID))
	}

	var oid order.OrderID
	copy(oid[:], note.OrderID)

	order := &Order{
		OrderID:  oid,
		Side:     note.Side,
		Quantity: note.Quantity,
		Rate:     note.Rate,
	}

	ob.ordersMtx.Lock()
	ob.orders[order.OrderID] = order
	ob.ordersMtx.Unlock()

	// Add the order to its associated books side.
	switch order.Side {
	case msgjson.BuyOrderNum:
		ob.buys.Add(order)

	case msgjson.SellOrderNum:
		ob.sells.Add(order)

	default:
		return fmt.Errorf("unknown order side provided: %d", order.Side)
	}

	return nil
}

// Book adds a new order to the order book.
func (ob *OrderBook) Book(note *msgjson.BookOrderNote) error {
	return ob.book(note, false)
}

// updateRemaining is the workhorse of the exported UpdateRemaining function. It
// allows updating cached and uncached orders.
func (ob *OrderBook) updateRemaining(note *msgjson.UpdateRemainingNote, cached bool) error {
	if ob.marketID != note.MarketID {
		return fmt.Errorf("invalid update_remaining note market id %s", note.MarketID)
	}

	if !cached {
		// Cache the note if the order book is not synced.
		if !ob.isSynced() {
			return ob.cacheOrderNote(msgjson.UpdateRemainingRoute, note)
		}
	}

	ob.setSeq(note.Seq)

	if len(note.OrderID) != order.OrderIDSize {
		return fmt.Errorf("expected order id length of %d, got %d",
			order.OrderIDSize, len(note.OrderID))
	}

	var oid order.OrderID
	copy(oid[:], note.OrderID)

	ob.ordersMtx.Lock()
	ord, found := ob.orders[oid]
	var newOrder *Order
	if found {
		newOrder = ord.copy()
		newOrder.Quantity = note.Remaining
		ob.orders[oid] = newOrder
	}
	ob.ordersMtx.Unlock()
	if !found {
		return fmt.Errorf("update_remaining order %s not found", oid)
	}

	if ord.sell() {
		ob.sells.ReplaceOrder(newOrder)
	} else {
		ob.buys.ReplaceOrder(newOrder)
	}
	return nil
}

// UpdateRemaining updates the remaining quantity of a booked order.
func (ob *OrderBook) UpdateRemaining(note *msgjson.UpdateRemainingNote) error {
	return ob.updateRemaining(note, false)
}

// LogEpochReport just checks the notification sequence.
func (ob *OrderBook) LogEpochReport(note *msgjson.EpochReportNote) error {
	ob.setSeq(note.Seq)
	atomic.StoreUint64(&ob.feeRates.base, note.BaseFeeRate)
	atomic.StoreUint64(&ob.feeRates.quote, note.QuoteFeeRate)
	return nil
}

// unbook is the workhorse of the exported Unbook function. It allows unbooking
// cached and uncached order notes.
func (ob *OrderBook) unbook(note *msgjson.UnbookOrderNote, cached bool) error {
	if ob.marketID != note.MarketID {
		return fmt.Errorf("invalid note market id %s", note.MarketID)
	}

	if !cached {
		// Cache the note if the order book is not synced.
		if !ob.isSynced() {
			return ob.cacheOrderNote(msgjson.UnbookOrderRoute, note)
		}
	}

	ob.setSeq(note.Seq)

	if len(note.OrderID) != order.OrderIDSize {
		return fmt.Errorf("expected order id length of %d, got %d",
			order.OrderIDSize, len(note.OrderID))
	}

	var oid order.OrderID
	copy(oid[:], note.OrderID)

	ob.ordersMtx.Lock()
	order, ok := ob.orders[oid]
	ob.ordersMtx.Unlock()
	if !ok {
		return fmt.Errorf("no order found with id %s", oid.String())
	}

	// Remove the order from its associated book side.
	switch order.Side {
	case msgjson.BuyOrderNum:
		err := ob.buys.Remove(order)
		if err != nil {
			return err
		}

	case msgjson.SellOrderNum:
		err := ob.sells.Remove(order)
		if err != nil {
			return err
		}

	default:
		return fmt.Errorf("unknown order side provided: %d", order.Side)
	}

	ob.ordersMtx.Lock()
	delete(ob.orders, oid)
	ob.ordersMtx.Unlock()

	return nil
}

// Unbook removes an order from the order book.
func (ob *OrderBook) Unbook(note *msgjson.UnbookOrderNote) error {
	return ob.unbook(note, false)
}

// BestNOrders returns the best n orders from the provided side.
func (ob *OrderBook) BestNOrders(n int, side uint8) ([]*Order, bool, error) {
	if !ob.isSynced() {
		return nil, false, fmt.Errorf("order book is unsynced")
	}

	var orders []*Order
	var filled bool
	switch side {
	case msgjson.BuyOrderNum:
		orders, filled = ob.buys.BestNOrders(n)

	case msgjson.SellOrderNum:
		orders, filled = ob.sells.BestNOrders(n)

	default:
		return nil, false, fmt.Errorf("unknown side provided: %d", side)
	}

	return orders, filled, nil
}

// Orders is the full order book, as slices of sorted buys and sells, and
// unsorted epoch orders in the current epoch.
func (ob *OrderBook) Orders() ([]*Order, []*Order, []*Order) {
	ob.epochMtx.Lock()
	eq := ob.epochQueues[ob.currentEpoch]
	ob.epochMtx.Unlock()
	var epochOrders []*Order
	if eq != nil {
		// NOTE: This epoch is either (1) open or (2) closed but awaiting a
		// match_proof and with no orders for a subsequent epoch yet.
		epochOrders = eq.Orders()
	}
	return ob.buys.orders(), ob.sells.orders(), epochOrders
}

// Enqueue appends the provided order note to the corresponding epoch's queue.
func (ob *OrderBook) Enqueue(note *msgjson.EpochOrderNote) error {
	ob.setSeq(note.Seq)
	idx := note.Epoch
	ob.epochMtx.Lock()
	defer ob.epochMtx.Unlock()
	eq, have := ob.epochQueues[idx]
	if !have {
		eq = NewEpochQueue()
		ob.epochQueues[idx] = eq // NOTE: trusting server here a bit not to flood us with fake epochs
		if idx > ob.currentEpoch {
			ob.currentEpoch = idx
		} else {
			ob.log.Errorf("epoch order note received for epoch %d but current epoch is %d", idx, ob.currentEpoch)
		}
	}

	return eq.Enqueue(note)
}

// ValidateMatchProof ensures the match proof data provided is correct by
// comparing it to a locally generated proof from the same epoch queue.
func (ob *OrderBook) ValidateMatchProof(note msgjson.MatchProofNote) error {
	idx := note.Epoch
	noteSize := len(note.Preimages) + len(note.Misses)

	// Extract the EpochQueue in a closure for clean epochMtx handling.
	var firstProof bool
	extractEpochQueue := func() (*EpochQueue, error) {
		ob.epochMtx.Lock()
		defer ob.epochMtx.Unlock()
		firstProof = ob.proofedEpoch == 0
		ob.proofedEpoch = idx
		if eq := ob.epochQueues[idx]; eq != nil {
			delete(ob.epochQueues, idx) // there will be no more additions to this epoch
			return eq, nil
		}
		// This is expected for an empty match proof or if we started mid-epoch.
		if noteSize == 0 || firstProof {
			return nil, nil
		}
		return nil, fmt.Errorf("epoch %d match proof note references %d orders, but local epoch queue is empty",
			idx, noteSize)
	}
	eq, err := extractEpochQueue()
	if eq == nil /* includes err != nil */ {
		return err
	}

	if noteSize > 0 {
		ob.log.Tracef("Validating match proof note for epoch %d (%s) with %d preimages and %d misses.",
			idx, note.MarketID, len(note.Preimages), len(note.Misses))
	}
	if localSize := eq.Size(); noteSize != localSize {
		if firstProof && localSize < noteSize {
			return nil // we only saw part of the epoch
		}
		// Since match_proof lags epoch close by up to preimage request timeout,
		// this can still happen for multiple proofs after (re)connect.
		return fmt.Errorf("epoch %d match proof note references %d orders, but local epoch queue has %d",
			idx, noteSize, localSize)
	}
	if len(note.Preimages) == 0 {
		return nil
	}

	pimgs := make([]order.Preimage, len(note.Preimages))
	for i, entry := range note.Preimages {
		copy(pimgs[i][:], entry)
	}

	misses := make([]order.OrderID, len(note.Misses))
	for i, entry := range note.Misses {
		copy(misses[i][:], entry)
	}

	seed, csum, err := eq.GenerateMatchProof(pimgs, misses)
	if err != nil {
		return fmt.Errorf("unable to generate match proof for epoch %d: %w",
			idx, err)
	}

	if !bytes.Equal(seed, note.Seed) {
		return fmt.Errorf("match proof seed mismatch for epoch %d: "+
			"expected %s, got %s", idx, note.Seed, seed)
	}

	if !bytes.Equal(csum, note.CSum) {
		return fmt.Errorf("match proof csum mismatch for epoch %d: "+
			"expected %s, got %s", idx, note.CSum, csum)
	}

	return nil
}

// MidGap returns the mid-gap price for the market. If one market side is empty
// the bets rate from the other side will be used. If both sides are empty, an
// error will be returned.
func (ob *OrderBook) MidGap() (uint64, error) {
	s, senough := ob.sells.BestNOrders(1)
	b, benough := ob.buys.BestNOrders(1)
	if !senough {
		if !benough {
			return 0, fmt.Errorf("cannot calculate mid-gap from empty order book")
		}
		return b[0].Rate, nil
	}
	if !benough {
		return s[0].Rate, nil
	}
	return (s[0].Rate + b[0].Rate) / 2, nil
}

// BestFill is the best (rate, quantity) fill for an order of the type and
// quantity specified. BestFill should be used when the exact quantity of base asset
// is known, i.e. limit orders and market sell orders. For market buy orders,
// use BestFillMarketBuy.
func (ob *OrderBook) BestFill(sell bool, qty uint64) ([]*Fill, bool) {
	if sell {
		return ob.buys.BestFill(qty)
	}
	return ob.sells.BestFill(qty)
}

// BestFillMarketBuy is the best (rate, quantity) fill for a market buy order.
// The qty given will be in units of quote asset.
func (ob *OrderBook) BestFillMarketBuy(qty, lotSize uint64) ([]*Fill, bool) {
	return ob.sells.bestFill(qty, true, lotSize)
}
