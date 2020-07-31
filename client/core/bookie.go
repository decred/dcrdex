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
		C:     make(chan *BookUpdate, 256),
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
	close      func() // e.g. dexConnection.Unsub
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
		// If Stop returns true, the timer did not fire. If false, the timer
		// already fired and the close func was called. The caller of feed()
		// must be OK with that, or the close func must be able to detect when
		// new feeds exist and abort. To solve the race, the caller of feed()
		// must synchronize with the close func. e.g. Sync locks bookMtx before
		// creating new feeds, and Unsub locks bookMtx to check for feeds before
		// unsubscribing.
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
	// If that was the last BookFeed, set a timer to unsubscribe w/ server.
	if beforeLen == 1 && len(b.feeds) == 0 {
		if b.closeTimer != nil {
			b.closeTimer.Stop()
		}
		b.closeTimer = time.AfterFunc(bookFeedTimeout, func() {
			b.mtx.Lock()
			defer b.mtx.Unlock()
			// Note that it is possible that the timer fired as b.feed() was
			// about to stop it before inserting a new BookFeed. If feed() got
			// the mutex first, there will be a feed to prevent b.close below.
			// If closeFeed() got the mutex first, feed() will fail to stop the
			// timer but still register a new BookFeed. The caller of feed()
			// must synchronize with the close func to prevent this.

			// Call the close func if there are no more feeds.
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
func (dc *dexConnection) Sync(base, quote uint32) (*OrderBook, *BookFeed, error) {
	dc.booksMtx.Lock()
	defer dc.booksMtx.Unlock()
	return dc.sync(base, quote)
}

// sync is the same as Sync but the booksMtx is not locked, so this is not
// thread-safe; the caller must lock it.
func (dc *dexConnection) sync(base, quote uint32) (*OrderBook, *BookFeed, error) {
	mkt := marketName(base, quote)
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

	// Subscribe via the 'orderbook' request.
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

	booky = newBookie(func() { dc.Unsub(mkt) })
	err = booky.Sync(result)
	if err != nil {
		return nil, nil, err
	}
	dc.books[mkt] = booky

	return booky.book(), booky.feed(), nil
}

// Unsub is the close callback passed to the bookie, and will be called when
// there are no more subscribers and the close delay period has expired.
func (dc *dexConnection) Unsub(mkt string) {
	dc.booksMtx.Lock()
	defer dc.booksMtx.Unlock() // hold it locked until unsubscribe request is completed

	// Abort the unsubscribe if feeds exist for the bookie. This can happen if a
	// bookie's close func is called while a new BookFeed is generated via Sync.
	if booky, found := dc.books[mkt]; found {
		booky.mtx.Lock()
		numFeeds := len(booky.feeds)
		booky.mtx.Unlock()
		if numFeeds > 0 {
			log.Debugf("Aborting unsubscribe for market %s with active feeds", mkt)
			return
		}
		// No BookFeeds, delete the bookie.
		delete(dc.books, mkt)
	}

	log.Debugf("Unsubscribing from %s", mkt)
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

// Sync subscribes to the order book and returns the book and a BookFeed to
// receive order book updates. The BookFeed must be Close()d when it is no
// longer in use.
func (c *Core) Sync(host string, base, quote uint32) (*OrderBook, *BookFeed, error) {
	c.connMtx.RLock()
	dc, found := c.conns[host]
	c.connMtx.RUnlock()
	if !found {
		return nil, nil, fmt.Errorf("unknown DEX '%s'", host)
	}

	return dc.Sync(base, quote)
}

// Book fetches the order book. If a subscription doesn't exist, one will be
// attempted and immediately closed.
func (c *Core) Book(dex string, base, quote uint32) (*OrderBook, error) {
	dex = addrHost(dex)
	c.connMtx.RLock()
	dc, found := c.conns[dex]
	c.connMtx.RUnlock()
	if !found {
		return nil, fmt.Errorf("no DEX %s", dex)
	}

	mkt := marketName(base, quote)
	dc.booksMtx.RLock()
	defer dc.booksMtx.RUnlock() // hold it locked until any transient sub/unsub is completed
	book, found := dc.books[mkt]
	// If not found, attempt to make a temporary subscription and return the
	// initial book.
	if !found {
		book, bookFeed, err := c.Sync(dex, base, quote)
		if err != nil {
			return nil, fmt.Errorf("unable to sync book: %v", err)
		}
		bookFeed.Close()
		return book, nil
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
			Qty:   float64(o.Quantity) / conversionFactor,
			Rate:  float64(o.Rate) / conversionFactor,
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
		Action:   msg.Route,
		Host:     dc.acct.host,
		MarketID: note.MarketID,
		Payload:  minifyOrder(note.OrderID, &note.TradeNote, 0),
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
		Action:   msg.Route,
		Host:     dc.acct.host,
		MarketID: note.MarketID,
		Payload:  &MiniOrder{Token: token(note.OrderID)},
	})

	return nil
}

// handleUpdateRemainingMsg is called when an update_remaining notification is
// received.
func handleUpdateRemainingMsg(_ *Core, dc *dexConnection, msg *msgjson.Message) error {
	note := new(msgjson.UpdateRemainingNote)
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
	err = book.UpdateRemaining(note)
	if err != nil {
		return err
	}
	book.send(&BookUpdate{
		Action:   msg.Route,
		Host:     dc.acct.host,
		MarketID: note.MarketID,
		Payload: &RemainingUpdate{
			Token: token(note.OrderID),
			Qty:   float64(note.Remaining) / conversionFactor,
		},
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

	if dc.setEpoch(note.MarketID, note.Epoch) {
		c.refreshUser()
	}

	dc.booksMtx.RLock()
	defer dc.booksMtx.RUnlock()

	book, ok := dc.books[note.MarketID]
	if !ok {
		return fmt.Errorf("no order book found with market id %q",
			note.MarketID)
	}

	err = book.Enqueue(note)
	if err != nil {
		return fmt.Errorf("failed to Enqueue epoch order: %v", err)
	}

	// Send a mini-order for book updates.
	book.send(&BookUpdate{
		Action:   msg.Route,
		Host:     dc.acct.host,
		MarketID: note.MarketID,
		Payload:  minifyOrder(note.OrderID, &note.TradeNote, note.Epoch),
	})

	return nil
}

// minifyOrder creates a MiniOrder from a TradeNote. The epoch and order ID must
// be supplied.
func minifyOrder(oid dex.Bytes, trade *msgjson.TradeNote, epoch uint64) *MiniOrder {
	return &MiniOrder{
		Qty:   float64(trade.Quantity) / conversionFactor,
		Rate:  float64(trade.Rate) / conversionFactor,
		Sell:  trade.Side == msgjson.SellOrderNum,
		Token: token(oid),
		Epoch: &epoch,
	}
}
