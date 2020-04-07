// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package core

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	orderbook "decred.org/dcrdex/client/order"
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/msgjson"
)

var (
	feederID        uint32
	bookFeedTimeout = time.Minute
)

// BookFeed manages a channel for receiving order book updates. The only
// exported field, C, is a channel on which to receive the updates as a series
// of *BookUpdate. It is imperative that the feeder (*BookFeed).Close() when no
// longer using the feed.
type BookFeed struct {
	C     chan *BookUpdate
	off   chan struct{}
	id    uint32
	close func(*BookFeed)
}

// NewBookFeed is a constructor for a *BookFeed. The caller must Close() the
// feed when it's no longer being used.
func NewBookFeed(close func(feed *BookFeed)) *BookFeed {
	return &BookFeed{
		C:     make(chan *BookUpdate, 1),
		off:   make(chan struct{}),
		id:    atomic.AddUint32(&feederID, 1),
		close: close,
	}
}

// Close the BookFeed.
func (f *BookFeed) Close() {
	f.close(f)
	close(f.off)
}

// on is used internally to see whether the caller has Close()d the feed.
func (f *BookFeed) on() bool {
	select {
	case <-f.off:
		return false
	default:
	}
	return true
}

// bookie is a BookFeed manager. bookie will maintain any number of order book
// subscribers. When the number of subscribers goes to zero, book feed does
// not immediately close(). Instead, a timer is set and if no more feeders
// subscribe before the timer expires, then the bookie will invoke it's caller
// supplied close() callback.
type bookie struct {
	orderbook.OrderBook
	mtx        sync.Mutex
	feeds      map[uint32]*BookFeed
	close      func()
	closeTimer *time.Timer
}

// newBookie is a constructor for a bookie. The caller should provide a callback
// function to be called when there are no subscribers and the close timer has
// expired.
func newBookie(close func()) *bookie {
	return &bookie{
		OrderBook: *orderbook.NewOrderBook(),
		feeds:     make(map[uint32]*BookFeed, 1),
		close:     close,
	}
}

// feed gets a new *BookFeed and cancels the close timer.
func (b *bookie) feed() *BookFeed {
	b.mtx.Lock()
	defer b.mtx.Unlock()
	if b.closeTimer != nil {
		b.closeTimer.Stop()
		b.closeTimer = nil
	}
	feed := NewBookFeed(b.closeFeed)
	b.feeds[feed.id] = feed
	return feed
}

// closeFeed is ultimately called when the BookFeed subscriber closes the feed.
// If this was the last feed for this bookie aka market, set a timer to
// unsubscribe unless another feed is requested.
func (b *bookie) closeFeed(feed *BookFeed) {
	b.mtx.Lock()
	defer b.mtx.Unlock()
	beforeLen := len(b.feeds)
	delete(b.feeds, feed.id)
	if beforeLen == 1 && len(b.feeds) == 0 {
		if b.closeTimer != nil {
			b.closeTimer.Stop()
		}
		b.closeTimer = time.AfterFunc(bookFeedTimeout, func() {
			b.mtx.Lock()
			defer b.mtx.Unlock()
			if len(b.feeds) == 0 {
				b.close()
			}
		})
	}
}

// send sends a *BookUpdate to all subscribers.
func (b *bookie) send(u *BookUpdate) {
	b.mtx.Lock()
	defer b.mtx.Unlock()
	for fid, feed := range b.feeds {
		if !feed.on() {
			delete(b.feeds, fid)
			continue
		}
		select {
		case feed.C <- u:
		default:
			log.Errorf("closing blocking book update channel")
			delete(b.feeds, fid)
		}
	}
}

// book returns the bookie's current order book.
func (b *bookie) book() *OrderBook {
	buys, sells, epoch := b.Orders()
	return &OrderBook{
		Buys:  translateBookSide(buys),
		Sells: translateBookSide(sells),
		Epoch: translateBookSide(epoch),
	}
}

// Sync subscribes to the order book and returns the book and a BookFeed to
// receive order book updates. The BookFeed must be Close()d when it is no
// longer in use.
func (c *Core) Sync(url string, base, quote uint32) (*OrderBook, *BookFeed, error) {
	// Need to send the 'orderbook' message and parse the results.
	c.connMtx.RLock()
	dc, found := c.conns[url]
	c.connMtx.RUnlock()
	if !found {
		return nil, nil, fmt.Errorf("unkown DEX '%s'", url)
	}

	mkt := marketName(base, quote)
	dc.booksMtx.Lock()
	defer dc.booksMtx.Unlock()
	booky, found := dc.books[mkt]
	if found {
		return booky.book(), booky.feed(), nil
	}

	// Make sure the market exists.
	dc.marketMtx.RLock()
	_, found = dc.marketMap[mkt]
	dc.marketMtx.RUnlock()
	if !found {
		return nil, nil, fmt.Errorf("unknown market %s", mkt)
	}

	// Subscribe
	req, err := msgjson.NewRequest(dc.NextID(), msgjson.OrderBookRoute, &msgjson.OrderBookSubscription{
		Base:  base,
		Quote: quote,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("error encoding 'orderbook' request: %v", err)
	}
	errChan := make(chan error, 1)
	result := new(msgjson.OrderBook)
	err = dc.Request(req, func(msg *msgjson.Message) {
		errChan <- msg.UnmarshalResult(result)
	})
	if err != nil {
		return nil, nil, fmt.Errorf("error subscribing to %s orderbook: %v", mkt, err)
	}
	err = extractError(errChan, requestTimeout, msgjson.OrderBookRoute)
	if err != nil {
		return nil, nil, err
	}

	booky = newBookie(func() { c.unsub(dc, mkt) })
	err = booky.Sync(result)
	if err != nil {
		return nil, nil, err
	}
	dc.books[mkt] = booky

	return booky.book(), booky.feed(), nil
}

// unsub is the close callback passed to the bookie, and will be called when
// there are no more subscribers and the close delay period has expired.
func (c *Core) unsub(dc *dexConnection, mkt string) {
	log.Debugf("unsubscribing from %s", mkt)

	dc.booksMtx.Lock()
	delete(dc.books, mkt)
	dc.booksMtx.Unlock()

	req, err := msgjson.NewRequest(dc.NextID(), msgjson.UnsubOrderBookRoute, &msgjson.UnsubOrderBook{
		MarketID: mkt,
	})
	if err != nil {
		log.Errorf("unsub_orderbook message encoding error: %v", err)
		return
	}
	err = dc.Request(req, func(msg *msgjson.Message) {
		var res bool
		msg.UnmarshalResult(&res)
		if !res {
			log.Errorf("error unsubscribing from %s", mkt)
		}
	})
	if err != nil {
		log.Errorf("request error unsubscribing from %s orderbook: %v", mkt, err)
	}
}

// Book fetches the order book. Book must be called after Sync.
func (c *Core) Book(dex string, base, quote uint32) (*OrderBook, error) {
	c.connMtx.RLock()
	dc, found := c.conns[dex]
	c.connMtx.RUnlock()
	if !found {
		return nil, fmt.Errorf("no DEX %s", dex)
	}

	mkt := marketName(base, quote)
	dc.booksMtx.RLock()
	book, found := dc.books[mkt]
	dc.booksMtx.RUnlock()
	if !found {
		return nil, fmt.Errorf("no market %s", mkt)
	}

	buys, sells, epoch := book.Orders()

	return &OrderBook{
		Buys:  translateBookSide(buys),
		Sells: translateBookSide(sells),
		Epoch: translateBookSide(epoch),
	}, nil
}

// translateBookSide translates from []*orderbook.Order to []*MiniOrder.
func translateBookSide(ins []*orderbook.Order) (outs []*MiniOrder) {
	for _, o := range ins {
		outs = append(outs, &MiniOrder{
			Qty:   float64(o.Quantity) / 1e8,
			Rate:  float64(o.Rate) / 1e8,
			Sell:  o.Side == msgjson.SellOrderNum,
			Token: token(o.OrderID[:]),
		})
	}
	return
}

// handleBookOrderMsg is called when a book_order notification is received.
func handleBookOrderMsg(_ *Core, dc *dexConnection, msg *msgjson.Message) error {
	note := new(msgjson.BookOrderNote)
	err := msg.Unmarshal(note)
	if err != nil {
		return fmt.Errorf("book order note unmarshal error: %v", err)
	}

	dc.booksMtx.RLock()
	defer dc.booksMtx.RUnlock()

	book, ok := dc.books[note.MarketID]
	if !ok {
		return fmt.Errorf("no order book found with market id '%v'",
			note.MarketID)
	}
	err = book.Book(note)
	if err != nil {
		return err
	}
	book.send(&BookUpdate{
		Action: msg.Route,
		Order:  minifyOrder(note.OrderID, &note.TradeNote, 0),
	})
	return nil
}

// handleUnbookOrderMsg is called when an unbook_order notification is
// received.
func handleUnbookOrderMsg(_ *Core, dc *dexConnection, msg *msgjson.Message) error {
	note := new(msgjson.UnbookOrderNote)
	err := msg.Unmarshal(note)
	if err != nil {
		return fmt.Errorf("unbook order note unmarshal error: %v", err)
	}

	dc.booksMtx.RLock()
	defer dc.booksMtx.RUnlock()

	book, ok := dc.books[note.MarketID]
	if !ok {
		return fmt.Errorf("no order book found with market id %q",
			note.MarketID)
	}
	err = book.Unbook(note)
	if err != nil {
		return err
	}
	book.send(&BookUpdate{
		Action: msg.Route,
		Order:  &MiniOrder{Token: token(note.OrderID)},
	})

	return nil
}

// handleEpochOrderMsg is called when an epoch_order notification is
// received.
func handleEpochOrderMsg(c *Core, dc *dexConnection, msg *msgjson.Message) error {
	note := new(msgjson.EpochOrderNote)
	err := msg.Unmarshal(note)
	if err != nil {
		return fmt.Errorf("epoch order note unmarshal error: %v", err)
	}

	c.setEpoch(note.Epoch)

	dc.booksMtx.RLock()
	defer dc.booksMtx.RUnlock()

	book, ok := dc.books[note.MarketID]
	if !ok {
		return fmt.Errorf("no order book found with market id %q",
			note.MarketID)
	}

	err = book.Enqueue(note)
	if err != nil {
		return err
	}
	// Send a mini-order for book updates.
	book.send(&BookUpdate{
		Action: msg.Route,
		Order:  minifyOrder(note.OrderID, &note.TradeNote, note.Epoch),
	})

	return nil
}

// minifyOrder creates a MiniOrder from a TradeNote. The epoch and order ID must
// be supplied.
func minifyOrder(oid dex.Bytes, trade *msgjson.TradeNote, epoch uint64) *MiniOrder {
	return &MiniOrder{
		Qty:   float64(trade.Quantity) / 1e8,
		Rate:  float64(trade.Rate) / 1e8,
		Sell:  trade.Side == msgjson.SellOrderNum,
		Token: token(oid),
		Epoch: epoch,
	}
}
