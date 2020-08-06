package order

import (
	"bytes"
	"fmt"
	"sync"

	"decred.org/dcrdex/dex/msgjson"
	"decred.org/dcrdex/dex/order"
)

var (
	// defaultQueueCapacity represents the default capacity of
	// the order note queue.
	defaultQueueCapacity = 10
)

// Order represents an ask or bid.
type Order struct {
	OrderID  order.OrderID
	Side     uint8
	Quantity uint64
	Rate     uint64
	Time     uint64
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
	seqMtx       sync.Mutex
	seq          uint64
	marketID     string
	noteQueue    []*cachedOrderNote
	noteQueueMtx sync.Mutex
	orders       map[order.OrderID]*Order
	ordersMtx    sync.Mutex
	buys         *bookSide
	sells        *bookSide
	synced       bool
	syncedMtx    sync.Mutex
	epochQueue   *EpochQueue
}

// NewOrderBook creates a new order book.
func NewOrderBook() *OrderBook {
	ob := &OrderBook{
		noteQueue:  make([]*cachedOrderNote, 0, defaultQueueCapacity),
		orders:     make(map[order.OrderID]*Order),
		buys:       NewBookSide(descending),
		sells:      NewBookSide(ascending),
		epochQueue: NewEpochQueue(),
	}
	return ob
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
		log.Errorf("notification received out of sync. %d != %d - 1", ob.seq, seq)
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

	for len(ob.noteQueue) > 0 {
		var entry *cachedOrderNote
		entry, ob.noteQueue = ob.noteQueue[0], ob.noteQueue[1:]

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

// Sync updates a client tracked order book with an order book snapshot.
func (ob *OrderBook) Sync(snapshot *msgjson.OrderBook) error {
	if ob.isSynced() {
		return fmt.Errorf("order book is already synced")
	}

	// Don't use setSeq here, since this message is the seed and is not expected
	// to be 1 more than the current seq value.
	ob.seqMtx.Lock()
	ob.seq = snapshot.Seq
	ob.seqMtx.Unlock()

	ob.marketID = snapshot.MarketID

	// Clear all orders, if any.
	ob.orders = make(map[order.OrderID]*Order)
	ob.buys = NewBookSide(descending)
	ob.sells = NewBookSide(ascending)

	for _, o := range snapshot.Orders {
		if len(o.OrderID) != order.OrderIDSize {
			return fmt.Errorf("expected order id length of %d, got %d",
				order.OrderIDSize, len(o.OrderID))
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

		ob.ordersMtx.Lock()
		ob.orders[order.OrderID] = order
		ob.ordersMtx.Unlock()

		// Append the order to the order book.
		switch o.Side {
		case msgjson.BuyOrderNum:
			ob.buys.Add(order)

		case msgjson.SellOrderNum:
			ob.sells.Add(order)

		default:
			return fmt.Errorf("unknown order side provided: %d", o.Side)
		}
	}

	// Process cached order notes.
	err := ob.processCachedNotes()
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

	ord := ob.sells.UpdateRemaining(oid, note.Remaining)
	if ord != nil {
		return nil
	}

	ord = ob.buys.UpdateRemaining(oid, note.Remaining)
	if ord != nil {
		return nil
	}

	return fmt.Errorf("update_remaining order %s not found", oid)
}

// UpdateRemaining updates the remaining quantity of a booked order.
func (ob *OrderBook) UpdateRemaining(note *msgjson.UpdateRemainingNote) error {
	return ob.updateRemaining(note, false)
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

	order, ok := ob.orders[oid]
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
// unsorted epoch orders.
func (ob *OrderBook) Orders() ([]*Order, []*Order, []*Order) {
	return ob.buys.orders(), ob.sells.orders(), ob.epochQueue.Orders()
}

// BestFIll returns the best fill for a quantity from the provided side.
func (ob *OrderBook) BestFill(qty uint64, side uint8) ([]*fill, error) {
	if !ob.isSynced() {
		return nil, fmt.Errorf("order book is unsynced")
	}

	switch side {
	case msgjson.BuyOrderNum:
		return ob.buys.BestFill(qty)

	case msgjson.SellOrderNum:
		return ob.sells.BestFill(qty)

	default:
		return nil, fmt.Errorf("unknown side provided: %d", side)
	}
}

// ResetEpoch clears the orderbook's epoch queue. This should be called when
// a new epoch begins.
func (ob *OrderBook) ResetEpoch() {
	ob.epochQueue.Reset()
}

// Enqueue appends the provided order note to the orderbook's epoch queue.
func (ob *OrderBook) Enqueue(note *msgjson.EpochOrderNote) error {
	ob.setSeq(note.Seq)
	return ob.epochQueue.Enqueue(note)
}

// EpochSize returns the number of entries in the orderbook's epoch queue.
func (ob *OrderBook) EpochSize() int {
	return ob.epochQueue.Size()
}

// IsEpochEntry checks if the provided order id is in the orderbook's epoch queue.
func (ob *OrderBook) IsEpochEntry(oid order.OrderID) bool {
	return ob.epochQueue.Exists(oid)
}

// ValidateMatchProof ensures the match proof data provided is correct by
// comparing it to a locally generated proof from the same epoch queue.
func (ob *OrderBook) ValidateMatchProof(note msgjson.MatchProofNote) error {
	localSize := ob.epochQueue.Size()
	noteSize := len(note.Preimages) + len(note.Misses)
	if noteSize > 0 {
		log.Debugf("Validating match proof note with %d preimages and %d misses.",
			len(note.Preimages), len(note.Misses))
	}
	if noteSize != localSize {
		return fmt.Errorf("match proof note references %d orders, but local epoch queue has %d",
			noteSize, localSize)
	}

	if len(note.Preimages) == 0 {
		if noteSize > 0 {
			log.Debugf("Match proof note contains only misses (%d) for %v epoch %v",
				noteSize, note.MarketID, note.Epoch)
		}
		return nil
	}

	pimgs := make([]order.Preimage, 0, len(note.Preimages))
	for _, entry := range note.Preimages {
		var pimg order.Preimage
		copy(pimg[:], entry[:order.PreimageSize])
		pimgs = append(pimgs, pimg)
	}

	misses := make([]order.OrderID, 0, len(note.Misses))
	for _, entry := range note.Misses {
		var miss order.OrderID
		copy(miss[:], entry[:order.OrderIDSize])
		misses = append(misses, miss)
	}

	seed, csum, err := ob.epochQueue.GenerateMatchProof(note.Epoch, pimgs, misses)
	if err != nil {
		return fmt.Errorf("unable to generate match proof for epoch %d: %v",
			note.Epoch, err)
	}

	if !bytes.Equal(seed, note.Seed) {
		return fmt.Errorf("match proof seed mismatch for epoch %d: "+
			"expected %s, got %s", note.Epoch, note.Seed, seed)
	}

	if !bytes.Equal(csum, note.CSum) {
		return fmt.Errorf("match proof csum mismatch for epoch %d: "+
			"expected %s, got %s", note.Epoch, note.CSum, csum)
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
