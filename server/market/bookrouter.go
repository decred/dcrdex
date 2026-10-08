// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package market

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/msgjson"
	"decred.org/dcrdex/dex/order"
	"decred.org/dcrdex/server/comms"
	"decred.org/dcrdex/server/matcher"
	"decred.org/dcrdex/server/meshevents"
)

// epochReport carries the per-epoch statistics published to book subscribers
// and price feeders after an epoch_processed event.
type epochReport struct {
	epochIdx     int64
	epochDur     int64
	stats        *matcher.MatchCycleStats
	spot         *msgjson.Spot
	baseFeeRate  uint64
	quoteFeeRate uint64
	matches      [][2]int64
}

// BookSource provides a market's booked orders.
type BookSource interface {
	Book() (epoch int64, buys []*order.LimitOrder, sells []*order.LimitOrder)
	Base() uint32
	Quote() uint32
}

// subscribers is a manager for a map of subscribers and a sequence counter. The
// sequence counter should be incremented whenever the DEX accepts, books,
// removes, or modifies an order. The client is responsible for tracking the
// sequence ID to ensure all order updates are received. If an update appears to
// be missing, the client should re-subscribe to the market to synchronize the
// order book from scratch.
type subscribers struct {
	mtx   sync.RWMutex
	conns map[uint64]comms.Link
	seq   uint64
}

// add adds a new subscriber.
func (s *subscribers) add(conn comms.Link) {
	s.mtx.Lock()
	defer s.mtx.Unlock()
	s.conns[conn.ID()] = conn
}

func (s *subscribers) remove(id uint64) bool {
	s.mtx.Lock()
	defer s.mtx.Unlock()
	_, found := s.conns[id]
	if !found {
		return false
	}
	delete(s.conns, id)
	return true
}

// nextSeq gets the next sequence number by incrementing the counter. This
// should be used when the book and orders are modified. Currently this applies
// to the routes: book_order, unbook_order, update_remaining, and epoch_order,
// plus suspend if the book is also being purged (persist=false).
func (s *subscribers) nextSeq() uint64 {
	s.mtx.Lock()
	defer s.mtx.Unlock()
	s.seq++
	return s.seq
}

// lastSeq gets the last retrieved sequence number.
func (s *subscribers) lastSeq() uint64 {
	s.mtx.RLock()
	defer s.mtx.RUnlock()
	return s.seq
}

// msgBook is a local copy of the order book information. The orders are saved
// as msgjson.BookOrderNote structures.
type msgBook struct {
	name string
	// mtx guards running, orders, recentMatches, and epochIdx.
	mtx           sync.RWMutex
	running       bool // ready to serve book snapshots, even when the market is suspended
	orders        map[order.OrderID]*msgjson.BookOrderNote
	recentMatches [][3]int64
	epochIdx      int64
	subs          *subscribers
	source        BookSource
	baseID        uint32
	quoteID       uint32
}

func (book *msgBook) setEpoch(idx int64) {
	book.mtx.Lock()
	book.epochIdx = idx
	book.mtx.Unlock()
}

func (book *msgBook) addRecentMatches(matches [][3]int64) {
	book.mtx.Lock()
	defer book.mtx.Unlock()

	book.recentMatches = append(matches, book.recentMatches...)
	if len(book.recentMatches) > 100 {
		book.recentMatches = book.recentMatches[:100]
	}
}

func (book *msgBook) epoch() int64 {
	book.mtx.RLock()
	defer book.mtx.RUnlock()
	return book.epochIdx
}

// insert adds the information for a new order into the order book. If the order
// is already found, it is inserted, but an error is logged since update should
// be used in that case.
func (book *msgBook) insert(lo *order.LimitOrder) *msgjson.BookOrderNote {
	msgOrder := limitOrderToMsgOrder(lo, book.name)
	book.mtx.Lock()
	defer book.mtx.Unlock()
	if _, found := book.orders[lo.ID()]; found {
		log.Errorf("Found existing order %v in book router when inserting a new one. "+
			"Overwriting, but this should not happen.", lo.ID())
		//panic("bad insert")
	}
	book.orders[lo.ID()] = msgOrder
	return msgOrder
}

// update updates the order book with the new order information, such as when an
// order's filled amount changes. If the order is not found, it is inserted, but
// an error is logged since insert should be used in that case.
func (book *msgBook) update(lo *order.LimitOrder) *msgjson.BookOrderNote {
	msgOrder := limitOrderToMsgOrder(lo, book.name)
	book.mtx.Lock()
	defer book.mtx.Unlock()
	if _, found := book.orders[lo.ID()]; !found {
		log.Errorf("Did NOT find existing order %v in book router while attempting to update it. "+
			"Adding a new entry, but this should not happen", lo.ID())
		//panic("bad update")
	}
	book.orders[lo.ID()] = msgOrder
	return msgOrder
}

// remove removes an order from the cache and reports whether it was present.
func (book *msgBook) remove(lo *order.LimitOrder) bool {
	book.mtx.Lock()
	defer book.mtx.Unlock()
	if _, found := book.orders[lo.ID()]; !found {
		return false
	}
	delete(book.orders, lo.ID())
	return true
}

// BookRouter manages client order-book subscriptions, caches booked orders
// in message format for initial snapshots, and sends updates to subscribers.
// It also serves price-feed subscriptions and fee-rate requests.
type BookRouter struct {
	books     map[string]*msgBook
	feeSource FeeSource

	seedOnce sync.Once

	priceFeeders *subscribers
	spotsMtx     sync.RWMutex
	spots        map[string]*msgjson.Spot
}

// NewBookRouter creates a book router and registers its request handlers.
// sources maps market names to order book sources. Call SeedBooks afterward
// to initialize the cached books from the markets' restored state.
func NewBookRouter(sources map[string]BookSource, feeSource FeeSource, route func(route string, handler comms.MsgHandler)) *BookRouter {
	router := &BookRouter{
		books:     make(map[string]*msgBook),
		feeSource: feeSource,
		priceFeeders: &subscribers{
			conns: make(map[uint64]comms.Link),
		},
		spots: make(map[string]*msgjson.Spot),
	}
	for mkt, src := range sources {
		subs := &subscribers{
			conns: make(map[uint64]comms.Link),
		}
		book := &msgBook{
			name:    mkt,
			orders:  make(map[order.OrderID]*msgjson.BookOrderNote),
			subs:    subs,
			source:  src,
			baseID:  src.Base(),
			quoteID: src.Quote(),
		}
		router.books[mkt] = book
	}
	route(msgjson.OrderBookRoute, router.handleOrderBook)
	route(msgjson.UnsubOrderBookRoute, router.handleUnsubOrderBook)
	route(msgjson.FeeRateRoute, router.handleFeeRate)
	route(msgjson.PriceFeedRoute, router.handlePriceFeeder)

	return router
}

// Run waits for cancellation, then makes the cached books unavailable and clears them.
func (r *BookRouter) Run(ctx context.Context) {
	<-ctx.Done()

	for _, book := range r.books {
		book.mtx.Lock()
		book.running = false
		book.orders = make(map[order.OrderID]*msgjson.BookOrderNote)
		book.mtx.Unlock()
		log.Infof("Book router terminating for market %q", book.name)
	}
}

// SeedBooks initializes the cached books from the markets' restored state.
// Call it after loading market state and before serving subscriptions or
// applying replicated events.
// Subsequent calls have no effect.
func (r *BookRouter) SeedBooks() {
	r.seedOnce.Do(func() {
		for _, book := range r.books {
			r.seedBook(book)
		}
	})
}

// seedBook initializes a market's cached book and marks it ready for subscriptions.
func (r *BookRouter) seedBook(book *msgBook) {
	book.mtx.Lock()
	defer book.mtx.Unlock()

	epoch, buys, sells := book.source.Book()
	book.epochIdx = epoch
	book.orders = make(map[order.OrderID]*msgjson.BookOrderNote, len(buys)+len(sells))
	for _, orders := range [][]*order.LimitOrder{buys, sells} {
		for _, lo := range orders {
			book.orders[lo.ID()] = limitOrderToMsgOrder(lo, book.name)
		}
	}
	book.running = true
}

// UnbookOrder removes an order from a configured market's cached book and
// notifies subscribers. It does nothing if the order is already absent.
func (r *BookRouter) UnbookOrder(mktName string, lo *order.LimitOrder) {
	r.unbookOrder(r.books[mktName], lo)
}

func (r *BookRouter) applyMarketStartedEvent(book *msgBook, epochIdx int64, removed []*order.LimitOrder) {
	book.setEpoch(epochIdx)
	for _, lo := range removed {
		r.unbookOrder(book, lo)
	}
}

// Book creates a copy of the book as a *msgjson.OrderBook.
func (r *BookRouter) Book(mktName string) (*msgjson.OrderBook, error) {
	book := r.books[mktName]
	if book == nil {
		return nil, fmt.Errorf("market %s unknown", mktName)
	}
	msgOB := r.msgOrderBook(book)
	if msgOB == nil {
		return nil, fmt.Errorf("market %s not running", mktName)
	}
	return msgOB, nil
}

// sendBook encodes and sends the entire order book to the specified client.
func (r *BookRouter) sendBook(conn comms.Link, book *msgBook, msgID uint64) {
	msgOB := r.msgOrderBook(book)
	if msgOB == nil {
		conn.SendError(msgID, msgjson.NewError(msgjson.MarketNotRunningError, "market not running"))
		return
	}
	msg, err := msgjson.NewResponse(msgID, msgOB, nil)
	if err != nil {
		log.Errorf("error encoding 'orderbook' response: %v", err)
		return
	}

	err = conn.Send(msg) // consider a synchronous send here
	if err != nil {
		log.Debugf("error sending 'orderbook' response: %v", err)
	}
}

func (r *BookRouter) msgOrderBook(book *msgBook) *msgjson.OrderBook {
	book.mtx.RLock() // book.orders and book.running
	if !book.running {
		book.mtx.RUnlock()
		return nil
	}
	ords := make([]*msgjson.BookOrderNote, 0, len(book.orders))
	for _, o := range book.orders {
		ords = append(ords, o)
	}
	epochIdx := book.epochIdx // instead of book.epoch() while already locked

	recentMatches := make([][3]int64, len(book.recentMatches))
	copy(recentMatches, book.recentMatches)

	book.mtx.RUnlock()

	return &msgjson.OrderBook{
		Seq:           book.subs.lastSeq(),
		MarketID:      book.name,
		Epoch:         uint64(epochIdx),
		Orders:        ords,
		BaseFeeRate:   r.feeSource.LastRate(book.baseID), // MaxFeeRate applied inside feeSource
		QuoteFeeRate:  r.feeSource.LastRate(book.quoteID),
		RecentMatches: recentMatches,
	}
}

// handleOrderBook is the handler for the non-authenticated 'orderbook' route.
// A client sends a request to this route to start an order book subscription,
// downloading the existing order book and receiving updates as a feed of
// notifications.
func (r *BookRouter) handleOrderBook(conn comms.Link, msg *msgjson.Message) *msgjson.Error {
	sub := new(msgjson.OrderBookSubscription)
	err := msg.Unmarshal(&sub)
	if err != nil || sub == nil {
		return &msgjson.Error{
			Code:    msgjson.RPCParseError,
			Message: "error parsing orderbook request",
		}
	}
	mkt, err := dex.MarketName(sub.Base, sub.Quote)
	if err != nil {
		return &msgjson.Error{
			Code:    msgjson.UnknownMarket,
			Message: "market name error: " + err.Error(),
		}
	}
	book, found := r.books[mkt]
	if !found {
		return &msgjson.Error{
			Code:    msgjson.UnknownMarket,
			Message: "unknown market",
		}
	}
	book.subs.add(conn)
	r.sendBook(conn, book, msg.ID)
	return nil
}

// handleUnsubOrderBook is the handler for the non-authenticated
// 'unsub_orderbook' route. Clients use this route to unsubscribe from an
// order book.
func (r *BookRouter) handleUnsubOrderBook(conn comms.Link, msg *msgjson.Message) *msgjson.Error {
	unsub := new(msgjson.UnsubOrderBook)
	err := msg.Unmarshal(&unsub)
	if err != nil || unsub == nil {
		return &msgjson.Error{
			Code:    msgjson.RPCParseError,
			Message: "error parsing unsub_orderbook request",
		}
	}
	book := r.books[unsub.MarketID]
	if book == nil {
		return &msgjson.Error{
			Code:    msgjson.UnknownMarket,
			Message: "unknown market: " + unsub.MarketID,
		}
	}

	if !book.subs.remove(conn.ID()) {
		return &msgjson.Error{
			Code:    msgjson.NotSubscribedError,
			Message: "not subscribed to " + unsub.MarketID,
		}
	}

	ack, err := msgjson.NewResponse(msg.ID, true, nil)
	if err != nil {
		log.Errorf("failed to encode response payload = true?")
	}

	err = conn.Send(ack)
	if err != nil {
		log.Debugf("error sending unsub_orderbook response: %v", err)
	}

	return nil
}

// handleFeeRate handles a fee_rate request.
func (r *BookRouter) handleFeeRate(conn comms.Link, msg *msgjson.Message) *msgjson.Error {
	var assetID uint32
	err := msg.Unmarshal(&assetID)
	if err != nil {
		return &msgjson.Error{
			Code:    msgjson.RPCParseError,
			Message: "error parsing fee_rate request",
		}
	}

	// Note that MaxFeeRate is applied inside feeSource.
	resp, err := msgjson.NewResponse(msg.ID, r.feeSource.LastRate(assetID), nil)
	if err != nil {
		log.Errorf("failed to encode fee_rate response: %v", err)
	}
	err = conn.Send(resp)
	if err != nil {
		log.Debugf("error sending fee_rate response: %v", err)
	}
	return nil
}

func (r *BookRouter) handlePriceFeeder(conn comms.Link, msg *msgjson.Message) *msgjson.Error {
	r.spotsMtx.RLock()
	msg, err := msgjson.NewResponse(msg.ID, r.spots, nil)
	r.spotsMtx.RUnlock()
	if err != nil {
		return &msgjson.Error{
			Code:    msgjson.RPCInternal,
			Message: "encoding error",
		}
	}

	if err := conn.Send(msg); err == nil {
		r.priceFeeders.add(conn)
	} else {
		log.Debugf("error sending price_feed response: %v", err)
	}

	return nil
}

// sendNote sends a notification to the specified subscribers.
func (r *BookRouter) sendNote(route string, subs *subscribers, note any) {
	msg, err := msgjson.NewNotification(route, note)
	if err != nil {
		log.Errorf("error creating notification-type Message: %v", err)
		// Do I need to do some kind of resync here?
		return
	}

	// Marshal and send the bytes to avoid multiple marshals when sending.
	b, err := json.Marshal(msg)
	if err != nil {
		log.Errorf("unable to marshal notification-type Message: %v", err)
		return
	}

	var deletes []uint64
	subs.mtx.RLock()
	for _, conn := range subs.conns {
		err := conn.SendRaw(b)
		if err != nil {
			deletes = append(deletes, conn.ID())
		}
	}
	subs.mtx.RUnlock()
	if len(deletes) > 0 {
		subs.mtx.Lock()
		for _, id := range deletes {
			delete(subs.conns, id)
		}
		subs.mtx.Unlock()
	}
}

// applyOrderAcceptedEvent applies an accepted-order event to the local book
// projection and notifies local subscribers.
func (r *BookRouter) applyOrderAcceptedEvent(book *msgBook, note *msgjson.EpochOrderNote, epochIdx int64) {
	book.mtx.Lock()
	if epochIdx > book.epochIdx {
		book.epochIdx = epochIdx
	}
	book.mtx.Unlock()

	note.Seq = book.subs.nextSeq()
	r.sendNote(msgjson.EpochOrderRoute, book.subs, note)
}

func (r *BookRouter) applyMarketSuspendedEvent(book *msgBook, finalEpoch int64, persistBook bool, purged []order.OrderID) {
	note := &msgjson.TradeSuspension{
		MarketID:   book.name,
		FinalEpoch: uint64(finalEpoch),
		Persist:    persistBook,
	}
	if !persistBook {
		note.Seq = book.subs.nextSeq()
		book.mtx.Lock()
		book.orders = make(map[order.OrderID]*msgjson.BookOrderNote)
		book.mtx.Unlock()
	}
	r.sendNote(msgjson.SuspensionRoute, book.subs, note)
	log.Infof("Market %q suspended after epoch %d, persist book = %v, purged orders = %d.",
		book.name, finalEpoch, persistBook, len(purged))
}

func (r *BookRouter) applyMarketResumedEvent(book *msgBook, startEpoch int64, removed []*order.LimitOrder) {
	book.setEpoch(startEpoch)
	for _, lo := range removed {
		r.unbookOrder(book, lo)
	}
	r.sendNote(msgjson.ResumptionRoute, book.subs, &msgjson.TradeResumption{
		MarketID:   book.name,
		StartEpoch: uint64(startEpoch),
	})
	log.Infof("Market %q resumed at epoch %d", book.name, startEpoch)
}

// applyBookedOrder applies a newly booked limit order to the local book
// projection and notifies local subscribers.
func (r *BookRouter) applyBookedOrder(book *msgBook, lo *order.LimitOrder) {
	note := book.insert(lo)
	note.Seq = book.subs.nextSeq()
	r.sendNote(msgjson.BookOrderRoute, book.subs, note)
}

// applyEpochProcessedEvent applies the order book projection and notifications
// produced by an epoch_processed event.
func (r *BookRouter) applyEpochProcessedEvent(book *msgBook, event *meshevents.EpochProcessedEvent, result *epochProcessedResult) {
	r.sendMatchProof(book, event, result)

	for _, ord := range result.booked {
		lo, ok := ord.Order.(*order.LimitOrder)
		if !ok {
			log.Errorf("non-limit order %T received in booked orders", ord.Order)
			continue
		}
		r.applyBookedOrder(book, lo)
	}

	for _, lo := range result.updates.TradesPartial {
		r.updateRemaining(book, lo)
	}
	for _, lo := range result.unbooked {
		r.unbookOrder(book, lo)
	}
}

func (r *BookRouter) sendMatchProof(book *msgBook, event *meshevents.EpochProcessedEvent, result *epochProcessedResult) {
	misses := make([]msgjson.Bytes, 0, len(result.misses))
	for _, ord := range result.misses {
		oid := ord.ID()
		misses = append(misses, oid[:])
	}
	preimages := make([]msgjson.Bytes, 0, len(result.revealed))
	for _, revealed := range result.revealed {
		preimages = append(preimages, revealed.Preimage[:])
	}
	r.sendNote(msgjson.MatchProofRoute, book.subs, &msgjson.MatchProofNote{
		MarketID:  book.name,
		Epoch:     uint64(event.EpochIdx),
		Preimages: preimages,
		Misses:    misses,
		CSum:      event.CSum,
		Seed:      result.seed,
	})
}

func (r *BookRouter) updateRemaining(book *msgBook, lo *order.LimitOrder) {
	bookNote := book.update(lo)
	note := &msgjson.UpdateRemainingNote{
		OrderNote: bookNote.OrderNote,
		Remaining: lo.Remaining(),
	}
	note.Seq = book.subs.nextSeq()
	r.sendNote(msgjson.UpdateRemainingRoute, book.subs, note)
}

// unbookOrder removes an order from the book projection and
// notifies subscribers, doing nothing if the order was not in the projection.
func (r *BookRouter) unbookOrder(book *msgBook, lo *order.LimitOrder) {
	if !book.remove(lo) {
		return
	}
	oid := lo.ID()
	note := &msgjson.UnbookOrderNote{
		Seq:      book.subs.nextSeq(),
		MarketID: book.name,
		OrderID:  oid[:],
	}
	r.sendNote(msgjson.UnbookOrderRoute, book.subs, note)
}

// publishEpochReport updates recent matches and sends the epoch report to book
// subscribers and the latest price update to price subscribers.
func (r *BookRouter) publishEpochReport(book *msgBook, report *epochReport) {
	startStamp := report.epochIdx * report.epochDur
	endStamp := startStamp + report.epochDur
	stats := report.stats

	matchesWithTimestamp := make([][3]int64, 0, len(report.matches))
	for _, match := range report.matches {
		matchesWithTimestamp = append(matchesWithTimestamp, [3]int64{
			match[0],
			match[1],
			endStamp})
	}
	book.addRecentMatches(matchesWithTimestamp)

	r.sendNote(msgjson.EpochReportRoute, book.subs, &msgjson.EpochReportNote{
		MarketID:     book.name,
		Epoch:        uint64(report.epochIdx),
		BaseFeeRate:  report.baseFeeRate,
		QuoteFeeRate: report.quoteFeeRate,
		Candle: msgjson.Candle{
			StartStamp:  uint64(startStamp),
			EndStamp:    uint64(endStamp),
			MatchVolume: stats.MatchVolume,
			QuoteVolume: stats.QuoteVolume,
			HighRate:    stats.HighRate,
			LowRate:     stats.LowRate,
			StartRate:   stats.StartRate,
			EndRate:     stats.EndRate,
		},
		MatchSummary: report.matches,
	})
	if report.spot != nil {
		r.sendNote(msgjson.PriceUpdateRoute, r.priceFeeders, report.spot)
	}
}

// cancelOrderToMsgOrder converts an *order.CancelOrder to a
// *msgjson.BookOrderNote.
func cancelOrderToMsgOrder(o *order.CancelOrder, mkt string) *msgjson.BookOrderNote {
	oid := o.ID()
	return &msgjson.BookOrderNote{
		OrderNote: msgjson.OrderNote{
			// Seq is set by book router.
			MarketID: mkt,
			OrderID:  oid[:],
		},
		TradeNote: msgjson.TradeNote{
			// Side is 0 (neither buy or sell), so omitted.
			Time: uint64(o.ServerTime.UnixMilli()),
		},
	}
}

func epochOrderNote(ord order.Order, mkt string, epochIdx int64) *msgjson.EpochOrderNote {
	epochNote := new(msgjson.EpochOrderNote)
	switch o := ord.(type) {
	case *order.LimitOrder:
		epochNote.BookOrderNote = *limitOrderToMsgOrder(o, mkt)
		epochNote.OrderType = msgjson.LimitOrderNum
	case *order.MarketOrder:
		epochNote.BookOrderNote = *marketOrderToMsgOrder(o, mkt)
		epochNote.OrderType = msgjson.MarketOrderNum
	case *order.CancelOrder:
		epochNote.BookOrderNote = *cancelOrderToMsgOrder(o, mkt)
		epochNote.OrderType = msgjson.CancelOrderNum
		epochNote.TargetID = o.TargetOrderID[:]
	default:
		panic(fmt.Sprintf("unsupported epoch order type %T", ord))
	}
	epochNote.MarketID = mkt
	epochNote.Epoch = uint64(epochIdx)
	c := ord.Commitment()
	epochNote.Commit = c[:]
	return epochNote
}

// limitOrderToMsgOrder converts an *order.LimitOrder to a
// *msgjson.BookOrderNote.
func limitOrderToMsgOrder(o *order.LimitOrder, mkt string) *msgjson.BookOrderNote {
	oid := o.ID()
	oSide := uint8(msgjson.BuyOrderNum)
	if o.Sell {
		oSide = msgjson.SellOrderNum
	}
	tif := uint8(msgjson.StandingOrderNum)
	if o.Force == order.ImmediateTiF {
		tif = msgjson.ImmediateOrderNum
	}
	return &msgjson.BookOrderNote{
		OrderNote: msgjson.OrderNote{
			// Seq is set by book router.
			MarketID: mkt,
			OrderID:  oid[:],
		},
		TradeNote: msgjson.TradeNote{
			Side:     oSide,
			Quantity: o.Remaining(),
			Rate:     o.Rate,
			TiF:      tif,
			Time:     uint64(o.ServerTime.UnixMilli()),
		},
	}
}

// marketOrderToMsgOrder converts an *order.MarketOrder to a
// *msgjson.BookOrderNote.
func marketOrderToMsgOrder(o *order.MarketOrder, mkt string) *msgjson.BookOrderNote {
	oid := o.ID()
	oSide := uint8(msgjson.BuyOrderNum)
	if o.Sell {
		oSide = uint8(msgjson.SellOrderNum)
	}
	return &msgjson.BookOrderNote{
		OrderNote: msgjson.OrderNote{
			// Seq is set by book router.
			MarketID: mkt,
			OrderID:  oid[:],
		},
		TradeNote: msgjson.TradeNote{
			Side:     oSide,
			Quantity: o.Remaining(),
			Time:     uint64(o.ServerTime.UnixMilli()),
			// Rate and TiF not set for market orders.
		},
	}
}

// OrderToMsgOrder converts an order.Order into a *msgjson.BookOrderNote.
func OrderToMsgOrder(ord order.Order, mkt string) (*msgjson.BookOrderNote, error) {
	switch o := ord.(type) {
	case *order.LimitOrder:
		return limitOrderToMsgOrder(o, mkt), nil
	case *order.MarketOrder:
		return marketOrderToMsgOrder(o, mkt), nil
	case *order.CancelOrder:
		return cancelOrderToMsgOrder(o, mkt), nil
	}
	return nil, fmt.Errorf("unknown order type for %v: %T", ord.ID(), ord)
}
