// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package market

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/encode"
	"decred.org/dcrdex/dex/msgjson"
	"decred.org/dcrdex/dex/order"
	"decred.org/dcrdex/server/comms"
)

// A bookUpdateAction classifies updates into how they affect the book or epoch
// queue.
type bookUpdateAction uint8

const (
	// invalidAction is the zero value action and should be considered programmer
	// error if received.
	invalidAction bookUpdateAction = iota
	// epochAction means an order is being added to the epoch queue and will
	// result in a msgjson.EpochOrderNote being sent to subscribers.
	epochAction
	// bookAction means an order is being added to the order book, and will result
	// in a msgjson.BookOrderNote being sent to subscribers.
	bookAction
	// unbookAction means an order is being removed from the order book and will
	// result in a msgjson.UnbookOrderNote being sent to subscribers.
	unbookAction
	// newEpochAction is an internal signal to the routers main loop that
	// indicates when a new epoch has opened.
	newEpochAction
	// matchProofAction means the matching has been performed and will result in
	// a msgjson.MatchProofNote being sent to subscribers.
	matchProofAction
)

// String provides a string representation of a bookUpdateAction. This is
// primarily for logging and debugging purposes.
func (bua bookUpdateAction) String() string {
	switch bua {
	case invalidAction:
		return "invalid"
	case epochAction:
		return "epoch"
	case bookAction:
		return "book"
	case unbookAction:
		return "unbook"
	case newEpochAction:
		return "newEpoch"
	case matchProofAction:
		return "matchProof"
	default:
		return ""
	}
}

// bookUpdateSignal combines a bookUpdateAction with data for which the action
// applies.
type bookUpdateSignal struct {
	action     bookUpdateAction
	order      order.Order
	matchProof *order.MatchProof
	epochIdx   int64
}

// BookSource is a source of a market's order book and a feed of updates to the
// order book and epoch queue.
type BookSource interface {
	Book() (epoch int64, buys []*order.LimitOrder, sells []*order.LimitOrder)
	OrderFeed() <-chan *bookUpdateSignal
}

// subscribers is a manager for a map of subscribers and a sequence counter.
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

// nextSeq gets the next sequence number by incrementing the counter.
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
	orders   map[order.OrderID]*msgjson.BookOrderNote
	epochIdx int64
	subs     *subscribers
	source   BookSource
}

func (book *msgBook) setEpoch(idx int64) {
	book.mtx.Lock()
	book.epochIdx = idx
	book.mtx.Unlock()
}

// Update updates the order book with the new order information. If an order
// with the same ID already exists in the book, it is overwritten without
// warning.  Such a case would be typical when an order's filled amount changes.
func (book *msgBook) update(lo *order.LimitOrder) *msgjson.BookOrderNote {
	book.mtx.Lock()
	defer book.mtx.Unlock()
	msgOrder := limitOrderToMsgOrder(lo, book.name)
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
	books map[string]*msgBook
}

// NewBookRouter is a constructor for a BookRouter. Routes are registered with
// comms and a monitoring goroutine is started for each BookSource specified.
// The input sources is a mapping of market names to sources for order and epoch
// queue information.
func NewBookRouter(sources map[string]BookSource) *BookRouter {
	router := &BookRouter{
		books: make(map[string]*msgBook),
	}
	for mkt, src := range sources {
		subs := &subscribers{
			conns: make(map[uint64]comms.Link),
		}
		book := &msgBook{
			name:   mkt,
			orders: make(map[order.OrderID]*msgjson.BookOrderNote),
			subs:   subs,
			source: src,
		}
		router.books[mkt] = book
	}
	comms.Route(msgjson.OrderBookRoute, router.handleOrderBook)
	comms.Route(msgjson.UnsubOrderBookRoute, router.handleUnsubOrderBook)
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

out:
	for {
		select {
		case u, ok := <-feed:
			if !ok {
				log.Errorf("order feed closed before shutting down BookRouter")
				break out
			}

			// Prepare the book/unbook/epoch note.
			var note interface{}
			var route string
			switch u.action {
			case newEpochAction:
				// New epoch index should be sent here by the market following
				// order matching and booking, but before new orders are added
				// to this new epoch. This is needed for msgjson.OrderBook in
				// sendBook, which must include the current epoch index.
				book.setEpoch(u.epochIdx)
				continue // no notification to send

			case bookAction:
				route = msgjson.BookOrderRoute
				lo, ok := u.order.(*order.LimitOrder)
				if !ok {
					panic("non-limit order received with bookAction")
				}
				n := book.update(lo)
				n.Seq = subs.nextSeq()
				note = n

			case unbookAction:
				route = msgjson.UnbookOrderRoute
				lo, ok := u.order.(*order.LimitOrder)
				if !ok {
					panic("non-limit order received with unbookAction")
				}
				book.remove(lo)
				oid := u.order.ID()
				note = &msgjson.UnbookOrderNote{
					Seq:      subs.nextSeq(),
					MarketID: book.name,
					OrderID:  oid[:],
				}

			case epochAction:
				route = msgjson.EpochOrderRoute
				epochNote := new(msgjson.EpochOrderNote)
				switch o := u.order.(type) {
				case *order.LimitOrder:
					epochNote.BookOrderNote = *limitOrderToMsgOrder(o, book.name)
					epochNote.OrderType = msgjson.LimitOrderNum
				case *order.MarketOrder:
					epochNote.BookOrderNote = *marketOrderToMsgOrder(o, book.name)
					epochNote.OrderType = msgjson.MarketOrderNum
				case *order.CancelOrder:
					epochNote.OrderType = msgjson.CancelOrderNum
					epochNote.TargetID = o.TargetOrderID[:]
				}

				epochNote.Seq = subs.nextSeq()
				epochNote.MarketID = book.name
				epochNote.Epoch = uint64(u.epochIdx)
				c := u.order.Commitment()
				epochNote.Commit = c[:]

				note = epochNote

			case matchProofAction:
				route = msgjson.MatchProofRoute
				misses := make([]msgjson.Bytes, 0, len(u.matchProof.Misses))
				for _, o := range u.matchProof.Misses {
					oid := o.ID()
					misses = append(misses, oid[:])
				}
				preimages := make([]msgjson.Bytes, 0, len(u.matchProof.Preimages))
				for i := range u.matchProof.Preimages {
					preimages = append(preimages, u.matchProof.Preimages[i][:])
				}
				note = &msgjson.MatchProofNote{
					MarketID:  book.name,
					Epoch:     u.matchProof.Epoch.Idx, // not u.epochIdx
					Preimages: preimages,
					Misses:    misses,
					CSum:      u.matchProof.CSum,
					Seed:      u.matchProof.Seed,
				}

			default:
				panic(fmt.Sprintf("unknown orderbook update action %d", u.action))
			}

			r.sendNote(route, subs, note)
		case <-ctx.Done():
			break out
		}
	}
}

// sendBook encodes and sends the the entire order book to the specified client.
func (r *BookRouter) sendBook(conn comms.Link, book *msgBook, msgID uint64) {
	seq := book.subs.lastSeq()
	book.mtx.RLock()
	msgBook := make([]*msgjson.BookOrderNote, 0, len(book.orders))
	for _, o := range book.orders {
		msgBook = append(msgBook, o)
	}
	epoch := book.epochIdx
	book.mtx.RUnlock()

	msg, err := msgjson.NewResponse(msgID, &msgjson.OrderBook{
		Seq:      seq,
		MarketID: book.name,
		Epoch:    uint64(epoch),
		Orders:   msgBook,
	}, nil)
	if err != nil {
		log.Errorf("error encoding 'orderbook' response: %v", err)
		return
	}

	err = conn.Send(msg)
	if err != nil {
		log.Debugf("error sending 'orderbook' response: %v", err)
	}
}

// handleOrderBook is the handler for the non-authenticated 'orderbook' route.
// A client sends a request to this route to start an order book subscription,
// downloading the existing order book and receiving updates as a feed of
// notifications.
func (r *BookRouter) handleOrderBook(conn comms.Link, msg *msgjson.Message) *msgjson.Error {
	sub := new(msgjson.OrderBookSubscription)
	err := json.Unmarshal(msg.Payload, sub)
	if err != nil {
		return &msgjson.Error{
			Code:    msgjson.RPCParseError,
			Message: "parse error: " + err.Error(),
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
	go r.sendBook(conn, book, msg.ID)
	return nil
}

// handleUnsubOrderBook is the handler for the non-authenticated
// 'unsub_orderbook' route. Clients use this route to unsubscribe from an
// order book.
func (r *BookRouter) handleUnsubOrderBook(conn comms.Link, msg *msgjson.Message) *msgjson.Error {
	unsub := new(msgjson.UnsubOrderBook)
	err := json.Unmarshal(msg.Payload, unsub)
	if err != nil {
		return &msgjson.Error{
			Code:    msgjson.RPCParseError,
			Message: "parse error: " + err.Error(),
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

// sendNote sends a notification to the specified subscribers.
func (r *BookRouter) sendNote(route string, subs *subscribers, note interface{}) {
	msg, err := msgjson.NewNotification(route, note)
	if err != nil {
		log.Errorf("error creating notification-type Message: %v", err)
		// Do I need to do some kind of resync here?
		return
	}

	deletes := make([]uint64, 0)
	subs.mtx.RLock()
	for _, conn := range subs.conns {
		err := conn.Send(msg)
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
			Time:     encode.UnixMilliU(o.ServerTime),
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
			Time:     encode.UnixMilliU(o.ServerTime),
			// Rate and TiF not set for market orders.
		},
	}
}
