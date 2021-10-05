// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package core

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/client/db"
	"decred.org/dcrdex/client/orderbook"
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/calc"
	"decred.org/dcrdex/dex/candles"
	"decred.org/dcrdex/dex/encode"
	"decred.org/dcrdex/dex/msgjson"
	"decred.org/dcrdex/dex/order"
	"github.com/decred/dcrd/dcrec/secp256k1/v4"
)

var (
	feederID        uint32
	bookFeedTimeout = time.Minute

	outdatedClientErr = errors.New("outdated client")
)

// BookFeed manages a channel for receiving order book updates. It is imperative
// that the feeder (BookFeed).Close() when no longer using the feed.
type BookFeed interface {
	Next() <-chan *BookUpdate
	Close()
	Candles(dur string) error
}

// bookFeed implements BookFeed.
type bookFeed struct {
	// c is the update channel. Access to c is synchronized by the bookie's
	// feedMtx.
	c      chan *BookUpdate
	bookie *bookie
	id     uint32
}

// Next returns the channel for receiving updates.
func (f *bookFeed) Next() <-chan *BookUpdate {
	return f.c
}

// Close the BookFeed.
func (f *bookFeed) Close() {
	f.bookie.closeFeed(f.id)
}

// Candles subscribes to the candlestick duration and sends the initial set
// of sticks over the update channel.
func (f *bookFeed) Candles(durStr string) error {
	return f.bookie.candles(durStr, f.id)
}

// candleCache adds synchronization and an on/off switch to *candles.Cache.
type candleCache struct {
	*candles.Cache
	candleMtx sync.RWMutex
	on        uint32
}

// copy creates a copy of the candles.
func (c *candleCache) copy() []candles.Candle {
	var cands []candles.Candle
	// The last candle can be modified after creation. Make a copy.
	c.candleMtx.RLock()
	defer c.candleMtx.RUnlock()
	if len(c.Candles) > 0 {
		cands = make([]candles.Candle, len(c.Candles))
		copy(cands, c.Candles)
	}
	return cands
}

// init resets the candles with the supplied set.
func (c *candleCache) init(in []*msgjson.Candle) {
	c.candleMtx.Lock()
	defer c.candleMtx.Unlock()
	c.Reset()
	for _, candle := range in {
		c.Add(candle)
	}
}

// addCandle adds the candle using candles.Cache.Add.
func (c *candleCache) addCandle(msgCandle *msgjson.Candle) *msgjson.Candle {
	if atomic.LoadUint32(&c.on) == 0 {
		return nil
	}
	c.candleMtx.Lock()
	defer c.candleMtx.Unlock()
	c.Add(msgCandle)
	return c.Last()
}

// bookie is a BookFeed manager. bookie will maintain any number of order book
// subscribers. When the number of subscribers goes to zero, book feed does
// not immediately close(). Instead, a timer is set and if no more feeders
// subscribe before the timer expires, then the bookie will invoke it's caller
// supplied close() callback.
type bookie struct {
	*orderbook.OrderBook
	dc           *dexConnection
	candleCaches map[string]*candleCache
	log          dex.Logger

	feedsMtx sync.RWMutex
	feeds    map[uint32]*bookFeed

	timerMtx   sync.Mutex
	closeTimer *time.Timer

	base, quote           uint32
	baseUnits, quoteUnits dex.UnitInfo
}

func defaultUnitInfo(symbol string) dex.UnitInfo {
	return dex.UnitInfo{
		AtomicUnit: "atoms",
		Conventional: dex.Denomination{
			ConversionFactor: 1e8,
			Unit:             symbol,
		},
	}
}

// newBookie is a constructor for a bookie. The caller should provide a callback
// function to be called when there are no subscribers and the close timer has
// expired.
func newBookie(dc *dexConnection, base, quote uint32, binSizes []string, logger dex.Logger) *bookie {
	candleCaches := make(map[string]*candleCache, len(binSizes))
	for _, durStr := range binSizes {
		dur, err := time.ParseDuration(durStr)
		if err != nil {
			logger.Errorf("failed to ParseDuration(%q)", durStr)
			continue
		}
		candleCaches[durStr] = &candleCache{
			Cache: candles.NewCache(candles.CacheSize, uint64(dur.Milliseconds())),
		}
	}

	parseUnitInfo := func(assetID uint32) dex.UnitInfo {
		assetInfo, _ := asset.Info(assetID)
		if assetInfo != nil {
			return assetInfo.UnitInfo
		} else {
			dexAsset := dc.assets[assetID]
			if dexAsset == nil {
				dc.log.Errorf("DEX market has no %d asset. Is this even possible?", base)
				return defaultUnitInfo("XYZ")
			} else {
				unitInfo := dexAsset.UnitInfo
				if unitInfo.Conventional.ConversionFactor == 0 {
					return defaultUnitInfo(dexAsset.Symbol)
				}
				return unitInfo
			}
		}
	}

	return &bookie{
		OrderBook:    orderbook.NewOrderBook(logger.SubLogger("book")),
		dc:           dc,
		candleCaches: candleCaches,
		log:          logger,
		feeds:        make(map[uint32]*bookFeed, 1),
		base:         base,
		quote:        quote,
		baseUnits:    parseUnitInfo(base),
		quoteUnits:   parseUnitInfo(quote),
	}
}

// logEpochReport handles the epoch candle in the epoch_report message.
func (b *bookie) logEpochReport(note *msgjson.EpochReportNote) error {
	err := b.LogEpochReport(note)
	if err != nil {
		return err
	}
	if note.Candle.EndStamp == 0 {
		return fmt.Errorf("epoch report has zero-valued candle end stamp")
	}

	for durStr, cache := range b.candleCaches {
		c := cache.addCandle(&note.Candle)
		if c == nil {
			continue
		}
		dur, _ := time.ParseDuration(durStr)
		b.send(&BookUpdate{
			Action:   CandleUpdateAction,
			Host:     b.dc.acct.host,
			MarketID: marketName(b.base, b.quote),
			Payload: CandleUpdate{
				Dur:          durStr,
				DurMilliSecs: uint64(dur.Milliseconds()),
				Candle:       c,
			},
		})
	}

	return nil
}

// newFeed gets a new *bookFeed and cancels the close timer. feed must be called
// with the bookie.mtx locked. The feed is primed with the provided *BookUpdate.
func (b *bookie) newFeed(u *BookUpdate) *bookFeed {
	b.timerMtx.Lock()
	if b.closeTimer != nil {
		// If Stop returns true, the timer did not fire. If false, the timer
		// already fired and the close func was called. The caller of feed()
		// must be OK with that, or the close func must be able to detect when
		// new feeds exist and abort. To solve the race, the caller of feed()
		// must synchronize with the close func. e.g. Sync locks bookMtx before
		// creating new feeds, and StopBook locks bookMtx to check for feeds
		// before unsubscribing.
		b.closeTimer.Stop()
		b.closeTimer = nil
	}
	b.timerMtx.Unlock()
	feed := &bookFeed{
		c:      make(chan *BookUpdate, 256),
		bookie: b,
		id:     atomic.AddUint32(&feederID, 1),
	}
	feed.c <- u
	b.feedsMtx.Lock()
	b.feeds[feed.id] = feed
	b.feedsMtx.Unlock()
	return feed
}

// closeFeeds closes the bookie's book feeds and resets the feeds map.
func (b *bookie) closeFeeds() {
	b.feedsMtx.Lock()
	defer b.feedsMtx.Unlock()
	for _, f := range b.feeds {
		close(f.c)
	}
	b.feeds = make(map[uint32]*bookFeed, 1)

}

// candles fetches the candle set from the server and activates the candle
// cache.
func (b *bookie) candles(durStr string, feedID uint32) error {
	cache := b.candleCaches[durStr]
	if cache == nil {
		return fmt.Errorf("no candles for %s-%s %q", unbip(b.base), unbip(b.quote), durStr)
	}
	var err error
	defer func() {
		if err != nil {
			return
		}
		b.feedsMtx.RLock()
		defer b.feedsMtx.RUnlock()
		f, ok := b.feeds[feedID]
		if !ok {
			// Feed must have been closed in another thread.
			return
		}
		dur, _ := time.ParseDuration(durStr)
		f.c <- &BookUpdate{
			Action:   FreshCandlesAction,
			Host:     b.dc.acct.host,
			MarketID: marketName(b.base, b.quote),
			Payload: &CandlesPayload{
				Dur:          durStr,
				DurMilliSecs: uint64(dur.Milliseconds()),
				Candles:      cache.copy(),
			},
		}
	}()
	if atomic.LoadUint32(&cache.on) == 1 {
		return nil
	}
	// Subscribe to the feed.
	payload := &msgjson.CandlesRequest{
		BaseID:     b.base,
		QuoteID:    b.quote,
		BinSize:    durStr,
		NumCandles: candles.CacheSize,
	}
	wireCandles := new(msgjson.WireCandles)
	err = sendRequest(b.dc.WsConn, msgjson.CandlesRoute, payload, wireCandles, DefaultResponseTimeout)
	if err != nil {
		return err
	}
	cache.init(wireCandles.Candles())
	atomic.StoreUint32(&cache.on, 1)
	return nil
}

// closeFeed closes the specified feed, and if no more feeds are open, sets a
// close timer to disconnect from the market feed.
func (b *bookie) closeFeed(feedID uint32) {
	b.feedsMtx.Lock()
	delete(b.feeds, feedID)
	numFeeds := len(b.feeds)
	b.feedsMtx.Unlock()

	// If that was the last BookFeed, set a timer to unsubscribe w/ server.
	if numFeeds == 0 {
		b.timerMtx.Lock()
		if b.closeTimer != nil {
			b.closeTimer.Stop()
		}
		b.closeTimer = time.AfterFunc(bookFeedTimeout, func() {
			b.feedsMtx.RLock()
			numFeeds := len(b.feeds)
			b.feedsMtx.RUnlock() // cannot be locked for b.close
			// Note that it is possible that the timer fired as b.feed() was
			// about to stop it before inserting a new BookFeed. If feed() got
			// the mutex first, there will be a feed to prevent b.close below.
			// If closeFeed() got the mutex first, feed() will fail to stop the
			// timer but still register a new BookFeed. The caller of feed()
			// must synchronize with the close func to prevent this.

			// Call the close func if there are no more feeds.
			if numFeeds == 0 {
				b.dc.stopBook(b.base, b.quote)
			}
		})
		b.timerMtx.Unlock()
	}
}

// send sends a *BookUpdate to all subscribers.
func (b *bookie) send(u *BookUpdate) {
	b.feedsMtx.Lock()
	defer b.feedsMtx.Unlock()
	for fid, feed := range b.feeds {
		select {
		case feed.c <- u:
		default:
			b.log.Warnf("bookie %p: Closing book update feed %d with no receiver. "+
				"The receiver should have closed the feed before going away.", b, fid)
			go b.closeFeed(feed.id) // delete it and maybe start a delayed bookie close
		}
	}
}

// book returns the bookie's current order book.
func (b *bookie) book() *OrderBook {
	buys, sells, epoch := b.Orders()
	return &OrderBook{
		Buys:  b.translateBookSide(buys),
		Sells: b.translateBookSide(sells),
		Epoch: b.translateBookSide(epoch),
	}
}

// minifyOrder creates a MiniOrder from a TradeNote. The epoch and order ID must
// be supplied.
func (b *bookie) minifyOrder(oid dex.Bytes, trade *msgjson.TradeNote, epoch uint64) *MiniOrder {
	return &MiniOrder{
		Qty:       float64(trade.Quantity) / float64(b.baseUnits.Conventional.ConversionFactor),
		QtyAtomic: trade.Quantity,
		Rate:      calc.ConventionalRate(trade.Rate, b.baseUnits, b.quoteUnits),
		MsgRate:   trade.Rate,
		Sell:      trade.Side == msgjson.SellOrderNum,
		Token:     token(oid),
		Epoch:     epoch,
	}
}

// bookie gets the bookie for the market, if it exists, else nil.
func (dc *dexConnection) bookie(marketID string) *bookie {
	dc.booksMtx.RLock()
	defer dc.booksMtx.RUnlock()
	return dc.books[marketID]
}

// syncBook subscribes to the order book and returns the book and a BookFeed to
// receive order book updates. The BookFeed must be Close()d when it is no
// longer in use. Use stopBook to unsubscribed and clean up the feed.
func (dc *dexConnection) syncBook(base, quote uint32) (BookFeed, error) {
	dc.cfgMtx.RLock()
	cfg := dc.cfg
	dc.cfgMtx.RUnlock()

	dc.booksMtx.Lock()
	defer dc.booksMtx.Unlock()

	mktID := marketName(base, quote)
	booky, found := dc.books[mktID]
	if !found {
		// Make sure the market exists.
		if dc.marketConfig(mktID) == nil {
			return nil, fmt.Errorf("unknown market %s", mktID)
		}

		obRes, err := dc.subscribe(base, quote)
		if err != nil {
			return nil, err
		}

		booky = newBookie(dc, base, quote, cfg.BinSizes, dc.log.SubLogger(mktID))
		err = booky.Sync(obRes)
		if err != nil {
			return nil, err
		}
		dc.books[mktID] = booky
	}

	// Get the feed and the book under a single lock to make sure the first
	// message is the book.
	feed := booky.newFeed(&BookUpdate{
		Action:   FreshBookAction,
		Host:     dc.acct.host,
		MarketID: mktID,
		Payload: &MarketOrderBook{
			Base:  base,
			Quote: quote,
			Book:  booky.book(),
		},
	})

	return feed, nil
}

// subscribe subscribes to the given market's order book via the 'orderbook'
// request. The response, which includes book's snapshot, is returned. Proper
// synchronization is required by the caller to ensure that order feed messages
// aren't processed before they are prepared to handle this subscription.
func (dc *dexConnection) subscribe(base, quote uint32) (*msgjson.OrderBook, error) {
	mkt := marketName(base, quote)
	// Subscribe via the 'orderbook' request.
	dc.log.Debugf("Subscribing to the %v order book for %v", mkt, dc.acct.host)
	req, err := msgjson.NewRequest(dc.NextID(), msgjson.OrderBookRoute, &msgjson.OrderBookSubscription{
		Base:  base,
		Quote: quote,
	})
	if err != nil {
		return nil, fmt.Errorf("error encoding 'orderbook' request: %w", err)
	}
	errChan := make(chan error, 1)
	result := new(msgjson.OrderBook)
	err = dc.RequestWithTimeout(req, func(msg *msgjson.Message) {
		errChan <- msg.UnmarshalResult(result)
	}, DefaultResponseTimeout, func() {
		errChan <- fmt.Errorf("timed out waiting for '%s' response", msgjson.OrderBookRoute)
	})
	if err != nil {
		return nil, fmt.Errorf("error subscribing to %s orderbook: %w", mkt, err)
	}
	err = <-errChan
	if err != nil {
		return nil, err
	}
	return result, nil
}

// stopBook is the close callback passed to the bookie, and will be called when
// there are no more subscribers and the close delay period has expired.
func (dc *dexConnection) stopBook(base, quote uint32) {
	mkt := marketName(base, quote)
	dc.booksMtx.Lock()
	defer dc.booksMtx.Unlock() // hold it locked until unsubscribe request is completed

	// Abort the unsubscribe if feeds exist for the bookie. This can happen if a
	// bookie's close func is called while a new BookFeed is generated elsewhere.
	if booky, found := dc.books[mkt]; found {
		booky.feedsMtx.Lock()
		numFeeds := len(booky.feeds)
		booky.feedsMtx.Unlock()
		if numFeeds > 0 {
			dc.log.Warnf("Aborting booky %p unsubscribe for market %s with active feeds", booky, mkt)
			return
		}
		// No BookFeeds, delete the bookie.
		delete(dc.books, mkt)
	}

	if err := dc.unsubscribe(base, quote); err != nil {
		dc.log.Error(err)
	}
}

// unsubscribe unsubscribes from to the given market's order book.
func (dc *dexConnection) unsubscribe(base, quote uint32) error {
	mkt := marketName(base, quote)
	dc.log.Debugf("Unsubscribing from the %v order book for %v", mkt, dc.acct.host)
	req, err := msgjson.NewRequest(dc.NextID(), msgjson.UnsubOrderBookRoute, &msgjson.UnsubOrderBook{
		MarketID: mkt,
	})
	if err != nil {
		return fmt.Errorf("unsub_orderbook message encoding error: %w", err)
	}
	// Request to unsubscribe. NOTE: does not wait for response.
	err = dc.Request(req, func(msg *msgjson.Message) {
		var res bool
		_ = msg.UnmarshalResult(&res) // res==false if unmarshal fails
		if !res {
			dc.log.Errorf("error unsubscribing from %s", mkt)
		}
	})
	if err != nil {
		return fmt.Errorf("request error unsubscribing from %s orderbook: %w", mkt, err)
	}
	return nil
}

// SyncBook subscribes to the order book and returns the book and a BookFeed to
// receive order book updates. The BookFeed must be Close()d when it is no
// longer in use.
func (c *Core) SyncBook(host string, base, quote uint32) (BookFeed, error) {
	c.connMtx.RLock()
	dc, found := c.conns[host]
	c.connMtx.RUnlock()
	if !found {
		return nil, fmt.Errorf("unknown DEX '%s'", host)
	}

	return dc.syncBook(base, quote)
}

// Book fetches the order book. If a subscription doesn't exist, one will be
// attempted and immediately closed.
func (c *Core) Book(dex string, base, quote uint32) (*OrderBook, error) {
	dex, err := addrHost(dex)
	if err != nil {
		return nil, newError(addressParseErr, "error parsing address: %v", err)
	}
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
	var ob *orderbook.OrderBook
	// If not found, attempt to make a temporary subscription and return the
	// initial book.
	if !found {
		snap, err := dc.subscribe(base, quote)
		if err != nil {
			return nil, fmt.Errorf("unable to subscribe to book: %w", err)
		}
		err = dc.unsubscribe(base, quote)
		if err != nil {
			c.log.Errorf("Failed to unsubscribe to %q book: %v", mkt, err)
		}
		ob = orderbook.NewOrderBook(c.log.SubLogger(mkt))
		if err = ob.Sync(snap); err != nil {
			return nil, fmt.Errorf("unable to sync book: %w", err)
		}
	} else {
		ob = book.OrderBook
	}

	buys, sells, epoch := ob.Orders()
	return &OrderBook{
		Buys:  book.translateBookSide(buys),
		Sells: book.translateBookSide(sells),
		Epoch: book.translateBookSide(epoch),
	}, nil
}

// translateBookSide translates from []*orderbook.Order to []*MiniOrder.
func (b *bookie) translateBookSide(ins []*orderbook.Order) (outs []*MiniOrder) {
	for _, o := range ins {
		outs = append(outs, &MiniOrder{
			Qty:       float64(o.Quantity) / float64(b.baseUnits.Conventional.ConversionFactor),
			QtyAtomic: o.Quantity,
			Rate:      calc.ConventionalRate(o.Rate, b.baseUnits, b.quoteUnits),
			MsgRate:   o.Rate,
			Sell:      o.Side == msgjson.SellOrderNum,
			Token:     token(o.OrderID[:]),
			Epoch:     o.Epoch,
		})
	}
	return
}

// handleBookOrderMsg is called when a book_order notification is received.
func handleBookOrderMsg(_ *Core, dc *dexConnection, msg *msgjson.Message) error {
	note := new(msgjson.BookOrderNote)
	err := msg.Unmarshal(note)
	if err != nil {
		return fmt.Errorf("book order note unmarshal error: %w", err)
	}

	book := dc.bookie(note.MarketID)
	if book == nil {
		return fmt.Errorf("no order book found with market id '%v'",
			note.MarketID)
	}
	err = book.Book(note)
	if err != nil {
		return err
	}
	book.send(&BookUpdate{
		Action:   BookOrderAction,
		Host:     dc.acct.host,
		MarketID: note.MarketID,
		Payload:  book.minifyOrder(note.OrderID, &note.TradeNote, 0),
	})
	return nil
}

// findMarketConfig searches the stored ConfigResponse for the named market.
// This must be called with cfgMtx at least read locked.
func (dc *dexConnection) findMarketConfig(name string) *msgjson.Market {
	if dc.cfg == nil {
		return nil
	}
	for _, mktConf := range dc.cfg.Markets {
		if mktConf.Name == name {
			return mktConf
		}
	}
	return nil
}

// setMarketStartEpoch revises the StartEpoch field of the named market in the
// stored ConfigResponse. It optionally zeros FinalEpoch and Persist, which
// should only be done at start time.
func (dc *dexConnection) setMarketStartEpoch(name string, startEpoch uint64, clearFinal bool) {
	dc.cfgMtx.Lock()
	defer dc.cfgMtx.Unlock()
	mkt := dc.findMarketConfig(name)
	if mkt == nil {
		return
	}
	mkt.StartEpoch = startEpoch
	// NOTE: should only clear these if starting now.
	if clearFinal {
		mkt.FinalEpoch = 0
		mkt.Persist = nil
	}
}

// setMarketFinalEpoch revises the FinalEpoch and Persist fields of the named
// market in the stored ConfigResponse.
func (dc *dexConnection) setMarketFinalEpoch(name string, finalEpoch uint64, persist bool) {
	dc.cfgMtx.Lock()
	defer dc.cfgMtx.Unlock()
	mkt := dc.findMarketConfig(name)
	if mkt == nil {
		return
	}
	mkt.FinalEpoch = finalEpoch
	mkt.Persist = &persist
}

// handleTradeSuspensionMsg is called when a trade suspension notification is
// received. This message may come in advance of suspension, in which case it
// has a SuspendTime set, or at the time of suspension if subscribed to the
// order book, in which case it has a Seq value set.
func handleTradeSuspensionMsg(c *Core, dc *dexConnection, msg *msgjson.Message) error {
	var sp msgjson.TradeSuspension
	err := msg.Unmarshal(&sp)
	if err != nil {
		return fmt.Errorf("trade suspension unmarshal error: %w", err)
	}

	// Ensure the provided market exists for the dex.
	mkt := dc.marketConfig(sp.MarketID)
	if mkt == nil {
		return fmt.Errorf("no market found with ID %s", sp.MarketID)
	}

	// Update the data in the stored ConfigResponse.
	dc.setMarketFinalEpoch(sp.MarketID, sp.FinalEpoch, sp.Persist)

	// SuspendTime == 0 means suspending now.
	if sp.SuspendTime != 0 {
		// This is just a warning about a scheduled suspension.
		suspendTime := encode.UnixTimeMilli(int64(sp.SuspendTime))
		subject, detail := c.formatDetails(TopicMarketSuspendScheduled, sp.MarketID, dc.acct.host, suspendTime)
		c.notify(newServerNotifyNote(TopicMarketSuspendScheduled, subject, detail, db.WarningLevel))
		return nil
	}

	topic := TopicMarketSuspended
	if !sp.Persist {
		topic = TopicMarketSuspendedWithPurge
	}
	subject, detail := c.formatDetails(topic, sp.MarketID, dc.acct.host)
	c.notify(newServerNotifyNote(topic, subject, detail, db.WarningLevel))

	if sp.Persist {
		// No book changes. Just wait for more order notes.
		return nil
	}

	// Clear the book and unbook/revoke own orders.
	book := dc.bookie(sp.MarketID)
	if book == nil {
		return fmt.Errorf("no order book found with market id '%s'", sp.MarketID)
	}

	err = book.Reset(&msgjson.OrderBook{
		MarketID: sp.MarketID,
		Seq:      sp.Seq,        // forces seq reset, but should be in seq with previous
		Epoch:    sp.FinalEpoch, // unused?
		// Orders is nil
	})
	// Return any non-nil error, but still revoke purged orders.

	// Revoke all active orders of the suspended market for the dex.
	c.log.Warnf("Revoking all active orders for market %s at %s.", sp.MarketID, dc.acct.host)
	updatedAssets := make(assetMap)
	dc.tradeMtx.RLock()
	for _, tracker := range dc.trades {
		if tracker.Order.Base() == mkt.Base && tracker.Order.Quote() == mkt.Quote &&
			tracker.metaData.Host == dc.acct.host && tracker.metaData.Status == order.OrderStatusBooked {
			// Locally revoke the purged book order.
			tracker.revoke()
			subject, details := c.formatDetails(TopicOrderAutoRevoked, tracker.token(), sp.MarketID, dc.acct.host)
			c.notify(newOrderNote(TopicOrderAutoRevoked, subject, details, db.WarningLevel, tracker.coreOrderInternal()))
			updatedAssets.count(tracker.fromAssetID)
		}
	}
	dc.tradeMtx.RUnlock()

	// Clear the book.
	book.send(&BookUpdate{
		Action:   FreshBookAction,
		Host:     dc.acct.host,
		MarketID: sp.MarketID,
		Payload: &MarketOrderBook{
			Base:  mkt.Base,
			Quote: mkt.Quote,
			Book:  book.book(), // empty
		},
	})

	if len(updatedAssets) > 0 {
		c.updateBalances(updatedAssets)
	}

	return err
}

// handleTradeResumptionMsg is called when a trade resumption notification is
// received. This may be an orderbook message at the time of resumption, or a
// notification of a newly-schedule resumption while the market is suspended
// (prior to the market resume).
func handleTradeResumptionMsg(c *Core, dc *dexConnection, msg *msgjson.Message) error {
	var rs msgjson.TradeResumption
	err := msg.Unmarshal(&rs)
	if err != nil {
		return fmt.Errorf("trade resumption unmarshal error: %w", err)
	}

	// Ensure the provided market exists for the dex.
	if dc.marketConfig(rs.MarketID) == nil {
		return fmt.Errorf("no market at %v found with ID %s", dc.acct.host, rs.MarketID)
	}

	// rs.ResumeTime == 0 means resume now.
	if rs.ResumeTime != 0 {
		// This is just a notice about a scheduled resumption.
		dc.setMarketStartEpoch(rs.MarketID, rs.StartEpoch, false) // set the start epoch, leaving any final/persist data
		resTime := encode.UnixTimeMilli(int64(rs.ResumeTime))
		subject, detail := c.formatDetails(TopicMarketResumeScheduled, rs.MarketID, dc.acct.host, resTime)
		c.notify(newServerNotifyNote(TopicMarketResumeScheduled, subject, detail, db.WarningLevel))
		return nil
	}

	// Update the market's status and mark the new epoch.
	dc.setMarketStartEpoch(rs.MarketID, rs.StartEpoch, true) // and clear the final/persist data
	dc.epochMtx.Lock()
	dc.epoch[rs.MarketID] = rs.StartEpoch
	dc.epochMtx.Unlock()

	// TODO: Server config change without restart is not implemented on the
	// server, but it would involve either getting the config response or adding
	// the entire market config to the TradeResumption payload.
	//
	// Fetch the updated DEX configuration.
	// dc.refreshServerConfig()

	subject, detail := c.formatDetails(TopicMarketResumed, rs.MarketID, dc.acct.host, rs.StartEpoch)
	c.notify(newServerNotifyNote(TopicMarketResumed, subject, detail, db.Success))

	// Book notes may resume at any time. Seq not set since no book changes.

	return nil
}

// refreshServerConfig fetches and replaces server configuration data. It also
// initially checks that a server's API version is one of serverAPIVers.
func (dc *dexConnection) refreshServerConfig() error {
	// Fetch the updated DEX configuration.
	cfg := new(msgjson.ConfigResult)
	err := sendRequest(dc.WsConn, msgjson.ConfigRoute, nil, cfg, DefaultResponseTimeout)
	if err != nil {
		return fmt.Errorf("unable to fetch server config: %w", err)
	}

	// Check that we are able to communicate with this DEX.
	apiVer := atomic.LoadInt32(&dc.apiVer)
	cfgAPIVer := int32(cfg.APIVersion)

	if apiVer != cfgAPIVer {
		if found := func() bool {
			for _, version := range serverAPIVers {
				ver := int32(version)
				if cfgAPIVer == ver {
					dc.log.Debugf("Setting server api version to %v.", ver)
					atomic.StoreInt32(&dc.apiVer, ver)
					return true
				}
			}
			return false
		}(); !found {
			err := fmt.Errorf("unknown server API version: %v", cfgAPIVer)
			if cfgAPIVer > apiVer {
				err = fmt.Errorf("%v: %w", err, outdatedClientErr)
			}
			return err
		}
	}

	bTimeout := time.Millisecond * time.Duration(cfg.BroadcastTimeout)
	tickInterval := bTimeout / tickCheckDivisions
	dc.log.Debugf("Server %v broadcast timeout %v. Tick interval %v", dc.acct.host, bTimeout, tickInterval)
	if dc.ticker.Dur() != tickInterval {
		dc.ticker.Reset(tickInterval)
	}
	getAsset := func(id uint32) *msgjson.Asset {
		for _, asset := range cfg.Assets {
			if id == asset.ID {
				return asset
			}
		}
		return nil
	}

	// Attempt to patch the ConfigResponse.Markets with zero LotSize and
	// RateStep from the Asset configs. This may be the case with an old server.
	for _, mkt := range cfg.Markets {
		if mkt.LotSize == 0 {
			if asset := getAsset(mkt.Base); asset != nil {
				mkt.LotSize = asset.LotSize
			} else {
				dc.log.Warnf("Market %s with zero lot size and no configured asset %d", mkt.Name, mkt.Base)
			}
		}
		if mkt.RateStep == 0 {
			if asset := getAsset(mkt.Quote); asset != nil {
				mkt.RateStep = asset.RateStep
			} else {
				dc.log.Warnf("Market %s with zero rate step and no configured asset %d", mkt.Name, mkt.Quote)
			}
		}
	}

	// Patch ConfigResponse.RegFees if no entry for DCR is there, meaning it is
	// likely an older server using cfg.Fee and cfg.RegFeeConfirms.
	if dcrAsset := cfg.RegFees["dcr"]; dcrAsset == nil {
		dc.log.Warnf("Legacy server %v does not provide a regFees map.", dc.acct.host)
		if cfg.RegFees == nil {
			cfg.RegFees = make(map[string]*msgjson.FeeAsset)
		}
		if cfg.Fee > 0 {
			cfg.RegFees["dcr"] = &msgjson.FeeAsset{ // v0 is only DCR
				ID:    42,
				Confs: uint32(cfg.RegFeeConfirms),
				Amt:   cfg.Fee,
			}
		} else {
			dc.log.Warnf("Server %v does not support DCR for registration", dc.acct.host)
		}
	} else {
		if cfg.Fee > 0 && dcrAsset.Amt != cfg.Fee {
			dc.log.Warnf("Inconsistent DCR fee amount: %d != %d", dcrAsset.Amt, cfg.Fee)
		}
		if dcrAsset.Confs != uint32(cfg.RegFeeConfirms) {
			dc.log.Warnf("Inconsistent DCR fee confirmation requirement: %d != %d",
				dcrAsset.Confs, cfg.RegFeeConfirms)
		}
	}

	// Update the dex connection with the new config details, including
	// StartEpoch and FinalEpoch, and rebuild the market data maps.
	dc.cfgMtx.Lock()
	defer dc.cfgMtx.Unlock()
	dc.cfg = cfg

	assets, epochs, err := generateDEXMaps(dc.acct.host, cfg)
	if err != nil {
		return fmt.Errorf("Inconsistent 'config' response: %w", err)
	}

	// Update dc.{marketMap,epoch,assets}
	dc.assetsMtx.Lock()
	dc.assets = assets
	dc.assetsMtx.Unlock()

	// If we're fetching config and the server sends the pubkey in config, set
	// the dexPubKey now. We also know that we're fetching the config for the
	// first time (via connectDEX), and the dexConnection has not been assigned
	// to dc.conns yet, so we can still update the acct.dexPubKey field without
	// a data race.
	if dc.acct.dexPubKey == nil && len(cfg.DEXPubKey) > 0 {
		dc.acct.dexPubKey, err = secp256k1.ParsePubKey(cfg.DEXPubKey)
		if err != nil {
			return fmt.Errorf("error decoding secp256k1 PublicKey from bytes: %w", err)
		}
	}

	dc.epochMtx.Lock()
	dc.epoch = epochs
	dc.epochMtx.Unlock()

	return nil
}

// handleUnbookOrderMsg is called when an unbook_order notification is
// received.
func handleUnbookOrderMsg(_ *Core, dc *dexConnection, msg *msgjson.Message) error {
	note := new(msgjson.UnbookOrderNote)
	err := msg.Unmarshal(note)
	if err != nil {
		return fmt.Errorf("unbook order note unmarshal error: %w", err)
	}

	book := dc.bookie(note.MarketID)
	if book == nil {
		return fmt.Errorf("no order book found with market id %q",
			note.MarketID)
	}
	err = book.Unbook(note)
	if err != nil {
		return err
	}
	book.send(&BookUpdate{
		Action:   UnbookOrderAction,
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
		return fmt.Errorf("book order note unmarshal error: %w", err)
	}

	book := dc.bookie(note.MarketID)
	if book == nil {
		return fmt.Errorf("no order book found with market id '%v'",
			note.MarketID)
	}
	err = book.UpdateRemaining(note)
	if err != nil {
		return err
	}
	book.send(&BookUpdate{
		Action:   UpdateRemainingAction,
		Host:     dc.acct.host,
		MarketID: note.MarketID,
		Payload: &RemainderUpdate{
			Token:     token(note.OrderID),
			Qty:       float64(note.Remaining) / float64(book.baseUnits.Conventional.ConversionFactor),
			QtyAtomic: note.Remaining,
		},
	})
	return nil
}

// handleEpochReportMsg is called when an epoch_report notification is received.
func handleEpochReportMsg(_ *Core, dc *dexConnection, msg *msgjson.Message) error {
	note := new(msgjson.EpochReportNote)
	err := msg.Unmarshal(note)
	if err != nil {
		return fmt.Errorf("epoch report note unmarshal error: %w", err)
	}
	book := dc.bookie(note.MarketID)
	if book == nil {
		return fmt.Errorf("no order book found with market id '%v'",
			note.MarketID)
	}
	err = book.logEpochReport(note)
	if err != nil {
		return fmt.Errorf("error logging epoch report: %w", err)
	}
	return nil
}

// handleEpochOrderMsg is called when an epoch_order notification is
// received.
func handleEpochOrderMsg(c *Core, dc *dexConnection, msg *msgjson.Message) error {
	note := new(msgjson.EpochOrderNote)
	err := msg.Unmarshal(note)
	if err != nil {
		return fmt.Errorf("epoch order note unmarshal error: %w", err)
	}

	book := dc.bookie(note.MarketID)
	if book == nil {
		return fmt.Errorf("no order book found with market id %q",
			note.MarketID)
	}

	err = book.Enqueue(note)
	if err != nil {
		return fmt.Errorf("failed to Enqueue epoch order: %w", err)
	}

	// Send a MiniOrder for book updates.
	book.send(&BookUpdate{
		Action:   EpochOrderAction,
		Host:     dc.acct.host,
		MarketID: note.MarketID,
		Payload:  book.minifyOrder(note.OrderID, &note.TradeNote, note.Epoch),
	})

	return nil
}
