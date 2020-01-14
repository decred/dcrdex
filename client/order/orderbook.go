package order

import (
	"fmt"
	"sync"
	"sync/atomic"

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
}

// NewOrderBook creates a new order book.
func NewOrderBook() *OrderBook {
	ob := &OrderBook{
		noteQueue: make([]*cachedOrderNote, 0, defaultQueueCapacity),
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

// cacheOrderNote caches an order note.
func (ob *OrderBook) cacheOrderNote(route string, entry interface{}) error {
	note := new(cachedOrderNote)

	switch route {
	case msgjson.BookOrderRoute, msgjson.UnbookOrderRoute:
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

		default:
			return fmt.Errorf("unknown cached note route "+
				" provided: %s", entry.Route)
		}
	}

	return nil
}

// Sync updates a client tracked order book with an order
// book snapshot.
func (ob *OrderBook) Sync(snapshot *msgjson.OrderBook) error {
	if ob.isSynced() {
		return fmt.Errorf("order book is already synced")
	}

	atomic.StoreUint64(&ob.seq, snapshot.Seq)
	ob.marketID = snapshot.MarketID
	ob.orders = make(map[order.OrderID]*Order)
	ob.buys = NewBookSide(descending)
	ob.sells = NewBookSide(ascending)

	for _, o := range snapshot.Orders {
		if len(o.OrderID) != order.OrderIDSize {
			return fmt.Errorf("order id length is not %v", order.OrderIDSize)
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

	// Discard a note if the order book is synced past it.
	if ob.seq > note.Seq {
		return nil
	}

	seq := atomic.AddUint64(&ob.seq, 1)
	if seq != note.Seq {
		return fmt.Errorf("order book out of sync, %d < %d", seq, note.Seq)
	}

	if len(note.OrderID) != order.OrderIDSize {
		return fmt.Errorf("order id length is not %d", order.OrderIDSize)
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

	// Discard a note if the order book is synced past it.
	if ob.seq > note.Seq {
		return nil
	}

	seq := atomic.AddUint64(&ob.seq, 1)
	if seq != note.Seq {
		return fmt.Errorf("order book out of sync, %d < %d", seq, note.Seq)
	}

	if len(note.OrderID) != order.OrderIDSize {
		return fmt.Errorf("order id length is not %d", order.OrderIDSize)
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
func (ob *OrderBook) BestNOrders(n uint64, side uint8) ([]*Order, error) {
	if !ob.isSynced() {
		return nil, fmt.Errorf("order book is unsynced")
	}

	switch side {
	case msgjson.BuyOrderNum:
		return ob.buys.BestNOrders(n)

	case msgjson.SellOrderNum:
		return ob.sells.BestNOrders(n)

	default:
		return nil, fmt.Errorf("unknown side provided: %d", side)
	}
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
