// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package market

import (
	"context"
	"encoding/json"
	"sync"

	"github.com/decred/dcrdex/server/comms"
	"github.com/decred/dcrdex/server/comms/msgjson"
	"github.com/decred/dcrdex/server/market/types"
	"github.com/decred/dcrdex/server/order"
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
)

// bookUpdateSignal combines a bookUpdateAction with the order on which the
// action applies.
type bookUpdateSignal struct {
	action bookUpdateAction
	order  order.Order
}

// BookSource is a source of a market's order book and a feed of updates to the
// order book and epoch queue.
type BookSource interface {
	Book() (buys []*order.LimitOrder, sells []*order.LimitOrder)
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

// lastSeq gets the last retreived sequence number.
func (s *subscribers) lastSeq() uint64 {
	s.mtx.RLock()
	defer s.mtx.RUnlock()
	return s.seq
}

// msgBook is a local copy of the order book information. The orders are saved
// as msgjson.BookOrderNote structures.
type msgBook struct {
	mtx    sync.RWMutex
	name   string
	orders map[order.OrderID]*msgjson.BookOrderNote
	subs   *subscribers
	source BookSource
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

// addBulkOrders adds the lists of orders to the order book. Use this for the
// initial sync of the orderbook.
func (book *msgBook) addBulkOrders(orderSets ...[]*order.LimitOrder) {
	book.mtx.Lock()
	defer book.mtx.Unlock()
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
	ctx      context.Context
	books    map[string]*msgBook
	epochLen uint64
}

// BookRouterConfig is the configuration settings for a BookRouter and the only
// argument to its constructor.
type BookRouterConfig struct {
	// Ctx is the application context. The monitoring goroutines will be shut down
	// on context cancellation.
	Ctx context.Context
	// Sources is a mapping of market names to sources for order and epoch queue
	// information.
	Sources map[string]BookSource
	// EpochDuration is the DEX's configured epoch duration.
	EpochDuration uint64
}

// NewBookRouter is a constructor for a BookRouter. Routes are registered with
// comms and a monitoring goroutine is started for each BookSource speicified.
func NewBookRouter(cfg *BookRouterConfig) *BookRouter {
	router := &BookRouter{
		ctx:      cfg.Ctx,
		books:    make(map[string]*msgBook),
		epochLen: cfg.EpochDuration,
	}
	for mkt, src := range cfg.Sources {
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
		go router.run(book)
	}
	comms.Route(msgjson.OrderBookRoute, router.handleOrderBook)
	comms.Route(msgjson.UnsubOrderBookRoute, router.handleUnsubOrderBook)
	return router
}

// run is a montoring loop for an order book.
func (r *BookRouter) run(book *msgBook) {
	// Get the initial book.
	feed := book.source.OrderFeed()
	book.addBulkOrders(book.source.Book())
	subs := book.subs

out:
	for {
		select {
		case u := <-feed:
			seq := subs.nextSeq()
			var note interface{}
			route := msgjson.BookOrderRoute
			switch u.action {
			case bookAction:
				lo, ok := u.order.(*order.LimitOrder)
				if !ok {
					panic("non-limit order received with bookAction")
				}
				n := book.update(lo)
				n.Seq = seq
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
					OrderID:  oid[:],
					MarketID: book.name,
					Seq:      seq,
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
				epochNote.Seq = seq
				epochNote.MarketID = book.name
				t := uint64(u.order.Time())
				epochNote.Epoch = t - t%r.epochLen
				note = epochNote
			default:
				panic("unknown orderbook update action")
			}
			r.sendNote(route, subs, note)
		case <-r.ctx.Done():
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
	book.mtx.RUnlock()
	msg, err := msgjson.NewResponse(msgID, &msgjson.OrderBook{
		Seq:      seq,
		MarketID: book.name,
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
	mkt, err := types.MarketName(sub.Base, sub.Quote)
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
		TradeNote: msgjson.TradeNote{
			Side:     oSide,
			Quantity: o.Remaining(),
			Rate:     o.Rate,
			TiF:      tif,
			Time:     uint64(o.ServerTime.Unix()),
		},
		OrderNote: msgjson.OrderNote{
			MarketID: mkt,
			OrderID:  oid[:],
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
			MarketID: mkt,
			OrderID:  oid[:],
		},
		TradeNote: msgjson.TradeNote{
			Side:     oSide,
			Quantity: o.Remaining(),
			Time:     uint64(o.ServerTime.Unix()),
		},
	}
}
