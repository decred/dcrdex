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
)

// A updateAction classifies updates into how they affect the book or epoch
// queue.
type updateAction uint8

const (
	// invalidAction is the zero value action and should be considered programmer
	// error if received.
	invalidAction updateAction = iota
	// epochAction means an order is being added to the epoch queue and will
	// result in a msgjson.EpochOrderNote being sent to subscribers.
	epochAction
	// bookAction means an order is being added to the order book, and will result
	// in a msgjson.BookOrderNote being sent to subscribers.
	bookAction
	// unbookAction means an order is being removed from the order book and will
	// result in a msgjson.UnbookOrderNote being sent to subscribers.
	unbookAction
	// updateRemainingAction means a standing limit order has partially filled
	// and will result in a msgjson.UpdateRemainingNote being sent to
	// subscribers.
	updateRemainingAction
	// newEpochAction is an internal signal to the routers main loop that
	// indicates when a new epoch has opened.
	newEpochAction
	// epochReportAction is sent when all bookAction, unbookAction, and
	// updateRemainingAction signals are sent for a completed epoch.
	// This signal performs a couple of important roles. First, it informs the
	// client that the book updates are done, and the book will be static until
	// the end of the epoch. Second, it sends the candlestick data, so a
	// subscriber can maintain a up-to-date candles.Cache without repeatedly
	// querying the HTTP API for the data.
	epochReportAction
	// matchProofAction means the matching has been performed and will result in
	// a msgjson.MatchProofNote being sent to subscribers.
	matchProofAction
	// suspendAction means the market has suspended.
	suspendAction
	// resumeAction means the market has resumed.
	resumeAction
)

// String provides a string representation of a updateAction. This is primarily
// for logging and debugging purposes.
func (bua updateAction) String() string {
	switch bua {
	case invalidAction:
		return "invalid"
	case epochAction:
		return "epoch"
	case bookAction:
		return "book"
	case unbookAction:
		return "unbook"
	case updateRemainingAction:
		return "update_remaining"
	case newEpochAction:
		return "newEpoch"
	case matchProofAction:
		return "matchProof"
	case suspendAction:
		return "suspend"
	default:
		return ""
	}
}

// updateSignal combines an updateAction with data for which the action
// applies.
type updateSignal struct {
	action updateAction
	data   interface{} // sigData* type
}

func (us updateSignal) String() string {
	return us.action.String()
}

// nolint:structcheck,unused
type sigDataOrder struct {
	order    order.Order
	epochIdx int64
}

type sigDataBookedOrder sigDataOrder
type sigDataUnbookedOrder sigDataOrder
type sigDataEpochOrder sigDataOrder
type sigDataUpdateRemaining sigDataOrder

type sigDataEpochReport struct {
	epochIdx     int64
	epochDur     int64
	stats        *matcher.MatchCycleStats
	spot         *msgjson.Spot
	baseFeeRate  uint64
	quoteFeeRate uint64
	matches      [][2]int64
}

type sigDataNewEpoch struct {
	idx int64
}

type sigDataSuspend struct {
	finalEpoch  int64
	persistBook bool
}

type sigDataResume struct {
	epochIdx int64
	// TODO: indicate config change if applicable
}

type sigDataMatchProof struct {
	matchProof *order.MatchProof
}

// BookSource is a source of a market's order book and a feed of updates to the
// order book and epoch queue.
type BookSource interface {
	Book() (epoch int64, buys []*order.LimitOrder, sells []*order.LimitOrder)
	OrderFeed() <-chan *updateSignal
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
	// mtx guards orders and epochIdx
	mtx      sync.RWMutex
	running  bool
	orders   map[order.OrderID]*msgjson.BookOrderNote
	epochIdx int64
	subs     *subscribers
	source   BookSource
	baseID   uint32
	quoteID  uint32
}

func (book *msgBook) setEpoch(idx int64) {
	book.mtx.Lock()
	book.epochIdx = idx
	book.mtx.Unlock()
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

// Remove the order from the order book.
func (book *msgBook) remove(lo *order.LimitOrder) {
	book.mtx.Lock()
	defer book.mtx.Unlock()
	delete(book.orders, lo.ID())
}

// addBulkOrders adds the lists of orders to the order book, and records the
// currently active epoch. Use this for the initial sync of the orderbook.
func (book *msgBook) addBulkOrders(epoch int64, orderSets ...[]*order.LimitOrder) {
	book.mtx.Lock()
	defer book.mtx.Unlock()
	book.epochIdx = epoch
	for _, set := range orderSets {
		for _, lo := range set {
			book.orders[lo.ID()] = limitOrderToMsgOrder(lo, book.name)
		}
	}
}

// BookRouter handles order book subscriptions, syncing the market with a group
// of subscribers, and maintaining an intermediate copy of the orderbook in
// message payload format for quick, full-book syncing.
type BookRouter struct {
	books     map[string]*msgBook
	feeSource FeeSource

	priceFeeders *subscribers
	spotsMtx     sync.RWMutex
	spots        map[string]*msgjson.Spot
}

// NewBookRouter is a constructor for a BookRouter. Routes are registered with
// comms and a monitoring goroutine is started for each BookSource specified.
// The input sources is a mapping of market names to sources for order and epoch
// queue information.
func NewBookRouter(sources map[string]BookSource, feeSource FeeSource) *BookRouter {
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
	comms.Route(msgjson.OrderBookRoute, router.handleOrderBook)
	comms.Route(msgjson.UnsubOrderBookRoute, router.handleUnsubOrderBook)
	comms.Route(msgjson.FeeRateRoute, router.handleFeeRate)
	comms.Route(msgjson.PriceFeedRoute, router.handlePriceFeeder)

	return router
}

// Run implements dex.Runner, and is blocking.
func (r *BookRouter) Run(ctx context.Context) {
	var wg sync.WaitGroup
	for _, b := range r.books {
		wg.Add(1)
		go func(b *msgBook) {
			r.runBook(ctx, b)
			wg.Done()
		}(b)
	}
	wg.Wait()
}

// runBook is a monitoring loop for an order book.
func (r *BookRouter) runBook(ctx context.Context, book *msgBook) {
	// Get the initial book.
	feed := book.source.OrderFeed()
	book.addBulkOrders(book.source.Book())
	subs := book.subs

	defer func() {
		book.mtx.Lock()
		book.running = false
		book.orders = make(map[order.OrderID]*msgjson.BookOrderNote)
		book.mtx.Unlock()
		log.Infof("Book router terminating for market %q", book.name)
	}()

	book.mtx.Lock()
	book.running = true
	book.mtx.Unlock()

out:
	for {
		select {
		case u, ok := <-feed:
			if !ok {
				log.Errorf("Book order feed closed for market %q at epoch %d",
					book.name, book.epoch())
				break out
			}

			// Prepare the book/unbook/epoch note.
			var note interface{}
			var route string
			var spot *msgjson.Spot
			switch sigData := u.data.(type) {
			case sigDataNewEpoch:
				// New epoch index should be sent here by the market following
				// order matching and booking, but before new orders are added
				// to this new epoch. This is needed for msgjson.OrderBook in
				// sendBook, which must include the current epoch index.
				book.setEpoch(sigData.idx)
				continue // no notification to send

			case sigDataBookedOrder:
				route = msgjson.BookOrderRoute
				lo, ok := sigData.order.(*order.LimitOrder)
				if !ok {
					panic("non-limit order received with bookAction")
				}
				n := book.insert(lo)
				n.Seq = subs.nextSeq()
				note = n

			case sigDataUnbookedOrder:
				route = msgjson.UnbookOrderRoute
				lo, ok := sigData.order.(*order.LimitOrder)
				if !ok {
					panic("non-limit order received with unbookAction")
				}
				book.remove(lo)
				oid := sigData.order.ID()
				note = &msgjson.UnbookOrderNote{
					Seq:      subs.nextSeq(),
					MarketID: book.name,
					OrderID:  oid[:],
				}

			case sigDataUpdateRemaining:
				route = msgjson.UpdateRemainingRoute
				lo, ok := sigData.order.(*order.LimitOrder)
				if !ok {
					panic("non-limit order received with updateRemainingAction")
				}
				bookNote := book.update(lo)
				n := &msgjson.UpdateRemainingNote{
					OrderNote: bookNote.OrderNote,
					Remaining: lo.Remaining(),
				}
				n.Seq = subs.nextSeq()
				note = n

			case sigDataEpochReport:
				route = msgjson.EpochReportRoute
				startStamp := sigData.epochIdx * sigData.epochDur
				endStamp := startStamp + sigData.epochDur
				stats := sigData.stats
				spot = sigData.spot

				note = &msgjson.EpochReportNote{
					MarketID:     book.name,
					Epoch:        uint64(sigData.epochIdx),
					BaseFeeRate:  sigData.baseFeeRate,
					QuoteFeeRate: sigData.quoteFeeRate,
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
					MatchSummary: sigData.matches,
				}

			case sigDataEpochOrder:
				route = msgjson.EpochOrderRoute
				epochNote := new(msgjson.EpochOrderNote)
				switch o := sigData.order.(type) {
				case *order.LimitOrder:
					epochNote.BookOrderNote = *limitOrderToMsgOrder(o, book.name)
					epochNote.OrderType = msgjson.LimitOrderNum
				case *order.MarketOrder:
					epochNote.BookOrderNote = *marketOrderToMsgOrder(o, book.name)
					epochNote.OrderType = msgjson.MarketOrderNum
				case *order.CancelOrder:
					epochNote.BookOrderNote = *cancelOrderToMsgOrder(o, book.name)
					epochNote.OrderType = msgjson.CancelOrderNum
					epochNote.TargetID = o.TargetOrderID[:]
				}

				epochNote.Seq = subs.nextSeq()
				epochNote.MarketID = book.name
				epochNote.Epoch = uint64(sigData.epochIdx)
				c := sigData.order.Commitment()
				epochNote.Commit = c[:]

				note = epochNote

			case sigDataMatchProof:
				route = msgjson.MatchProofRoute
				mp := sigData.matchProof
				misses := make([]msgjson.Bytes, 0, len(mp.Misses))
				for _, o := range mp.Misses {
					oid := o.ID()
					misses = append(misses, oid[:])
				}
				preimages := make([]msgjson.Bytes, 0, len(mp.Preimages))
				for i := range mp.Preimages {
					preimages = append(preimages, mp.Preimages[i][:])
				}
				note = &msgjson.MatchProofNote{
					MarketID:  book.name,
					Epoch:     mp.Epoch.Idx, // not u.epochIdx
					Preimages: preimages,
					Misses:    misses,
					CSum:      mp.CSum,
					Seed:      mp.Seed,
				}

			case sigDataSuspend:
				// When sent with seq set, it indicates immediate stop, and may
				// also indicate to purge the book.
				route = msgjson.SuspensionRoute
				susp := &msgjson.TradeSuspension{
					MarketID: book.name,
					// SuspendTime of 0 means now.
					FinalEpoch: uint64(sigData.finalEpoch),
					Persist:    sigData.persistBook,
				}
				// Only set Seq if there is a book update.
				if !sigData.persistBook {
					susp.Seq = subs.nextSeq() // book purge
					book.mtx.Lock()
					book.orders = make(map[order.OrderID]*msgjson.BookOrderNote)
					book.mtx.Unlock()
					// The router is "running" although the market is suspended.
				}
				note = susp

				log.Infof("Market %q suspended after epoch %d, persist book = %v.",
					book.name, sigData.finalEpoch, sigData.persistBook)

			case sigDataResume:
				route = msgjson.ResumptionRoute
				note = &msgjson.TradeResumption{
					MarketID: book.name,
					// ResumeTime of 0 means now.
					StartEpoch: uint64(sigData.epochIdx),
				} // no Seq for the resume since it doesn't modify the book

				log.Infof("Market %q resumed at epoch %d", book.name, sigData.epochIdx)

			default:
				log.Errorf("Unknown orderbook update action %d", u.action)
				continue
			}

			r.sendNote(route, subs, note)

			if spot != nil {
				r.sendNote(msgjson.PriceUpdateRoute, r.priceFeeders, spot)
			}
		case <-ctx.Done():
			break out
		}
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
	book.mtx.RUnlock()

	return &msgjson.OrderBook{
		Seq:          book.subs.lastSeq(),
		MarketID:     book.name,
		Epoch:        uint64(epochIdx),
		Orders:       ords,
		BaseFeeRate:  r.feeSource.LastRate(book.baseID), // MaxFeeRate applied inside feeSource
		QuoteFeeRate: r.feeSource.LastRate(book.quoteID),
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
func (r *BookRouter) sendNote(route string, subs *subscribers, note interface{}) {
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
