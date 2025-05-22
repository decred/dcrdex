package mesh

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/fiatrates"
	"decred.org/dcrdex/dex/msgjson"
	"decred.org/dcrdex/tatanka/client/orderbook"
	"decred.org/dcrdex/tatanka/client/trade"
	"decred.org/dcrdex/tatanka/mj"
	"decred.org/dcrdex/tatanka/tanka"
)

type order struct {
	*tanka.Order
	oid tanka.ID40

	// matches are accepted matches where the counterparty has responded to
	// our bid positively.
	matchesMtx sync.RWMutex
	matches    map[tanka.ID32]*tanka.Match
	remain     uint64
}

type market struct {
	log     dex.Logger
	peerID  tanka.PeerID
	conn    *meshConn
	baseID  uint32
	quoteID uint32

	// ords are our orders and matches.
	ordsMtx sync.RWMutex
	ords    map[tanka.ID40]*order

	// book is known market orders minus ours.
	book *orderbook.Book
}

func (m *market) addOwnOrder(ord *tanka.Order) {
	oid := ord.ID()
	m.ordsMtx.RLock()
	if _, exists := m.ords[oid]; exists {
		// ignore it then
		return
	}
	m.ordsMtx.RUnlock()
	o := &order{
		Order:   ord,
		oid:     oid,
		matches: make(map[tanka.ID32]*tanka.Match),
	}
	desire := &trade.DesiredTrade{
		Qty:  ord.Qty,
		Rate: ord.Rate,
		Sell: ord.Sell,
	}
	matches, _ := trade.MatchBook(desire, m.feeParams(), m.book.Find)
	// TODO: Do asyncronously.
	o.remain = ord.Qty
	for _, match := range matches {
		now := time.Now()
		matchThem := &tanka.Match{
			From:    m.peerID,
			OrderID: ord.ID(),
			Qty:     match.Qty,
			BaseID:  m.baseID,
			QuoteID: m.quoteID,
			Stamp:   now,
		}
		ok, err := m.negotiate(ord.From, matchThem)
		if err != nil {
			m.log.Errorf("unable to negotiate match with peer %s: %v", ord.From, err)
			continue
		}
		if !ok {
			continue
		}
		// They agree.
		matchUs := &tanka.Match{
			From:    match.Order.From,
			OrderID: match.Order.ID(),
			Qty:     match.Qty,
			BaseID:  m.baseID,
			QuoteID: m.quoteID,
			Stamp:   now,
		}
		o.matches[matchUs.ID()] = matchUs
		o.remain -= match.Qty
	}
	// TODO: Retry with orders from the book if some were not accepted but
	// more compatible orders exist. We need a way to mark tried orders
	// first.
	m.ordsMtx.Lock()
	m.ords[oid] = o
	m.ordsMtx.Unlock()
}

// TODO: Correct these.
func (m *market) feeParams() *trade.FeeParameters {
	return &trade.FeeParameters{
		MaxFeeExposure:    1,
		BaseFeesPerMatch:  1,
		QuoteFeesPerMatch: 1,
	}
}

func (m *market) addOrder(ord *tanka.Order) {
	// TODO: Validate order.
	m.book.Add(ord)
	check := func(o *order) {
		o.matchesMtx.Lock()
		defer o.matchesMtx.Unlock()
		if o.remain == 0 {
			return
		}
		if ord.Sell == o.Order.Sell {
			return
		}
		if o.Sell {
			if ord.Rate < o.Order.Rate {
				return
			}
		} else if ord.Rate > o.Order.Rate {
			return
		}
		if compat, _ := trade.OrderIsMatchable(o.remain, ord, m.feeParams()); !compat {
			return
		}
		maxQty := ord.Qty
		if ord.Qty > o.remain {
			maxQty = o.remain
		}
		lots := maxQty / ord.LotSize
		qty := lots * ord.LotSize
		o.remain -= qty
		now := time.Now()
		matchThem := &tanka.Match{
			From:    m.peerID,
			OrderID: o.Order.ID(),
			Qty:     qty,
			BaseID:  m.baseID,
			QuoteID: m.quoteID,
			Stamp:   now,
		}
		ok, err := m.negotiate(ord.From, matchThem)
		if err != nil {
			m.log.Errorf("unable to negotiate match with peer %s: %v", ord.From, err)
			return
		}
		if !ok {
			return
		}
		// They agree.
		matchUs := &tanka.Match{
			From:    ord.From,
			OrderID: ord.ID(),
			Qty:     qty,
			BaseID:  m.baseID,
			QuoteID: m.quoteID,
			Stamp:   now,
		}
		o.matches[matchUs.ID()] = matchUs
	}
	m.ordsMtx.RLock()
	defer m.ordsMtx.RUnlock()
	for _, ord := range m.ords {
		check(ord)
	}
}

func (m *market) negotiate(to tanka.PeerID, match *tanka.Match) (bool, error) {
	b, err := json.Marshal(*match)
	if err != nil {
		return false, err
	}
	msg := &msgjson.Message{
		Route:   mj.RouteNegotiate,
		Payload: b,
	}
	// Is this necessary or will request try the connection first?
	if err := m.conn.ConnectPeer(to); err != nil {
		return false, err
	}
	var ok bool
	return ok, m.conn.RequestPeer(to, msg, &ok)
}

func (m *market) handleNegotiate(match *tanka.Match) bool {
	mid := match.ID()
	m.ordsMtx.RLock()
	ord, found := m.ords[match.OrderID]
	m.ordsMtx.RUnlock()
	// Order is finished or never existed.
	if !found {
		m.log.Debugf("ignoring match proposal for unknown order %s", match.OrderID)
		return false
	}
	ord.matchesMtx.Lock()
	defer ord.matchesMtx.Unlock()
	// We already confirmed this match.
	if ord.matches[mid] != nil {
		return true
	}
	// We no longer have the quantity necessary.
	// TODO: negotiate a lower quantity.
	if ord.remain < match.Qty {
		return false
	}
	ord.matches[mid] = match
	ord.remain -= match.Qty
	return true
}

func (m *Mesh) handleMarketBroadcast(bcast *mj.Broadcast) {
	mktName := string(bcast.Subject)
	m.marketsMtx.RLock()
	mkt, found := m.markets[mktName]
	m.marketsMtx.RUnlock()
	if !found {
		m.log.Debugf("received order notification for unknown market %q", mktName)
		return
	}
	switch bcast.MessageType {
	case mj.MessageTypeTrollBox:
		var troll mj.Troll
		if err := json.Unmarshal(bcast.Payload, &troll); err != nil {
			m.log.Errorf("error unmarshaling trollbox message: %v", err)
			return
		}
		fmt.Printf("trollbox message for market %s: %s\n", mktName, troll.Msg)
	case mj.MessageTypeNewOrder:
		var ord tanka.Order
		if err := json.Unmarshal(bcast.Payload, &ord); err != nil {
			m.log.Errorf("error unmarshaling new order: %v", err)
			return
		}
		mkt.addOrder(&ord)
	case mj.MessageTypeNewSubscriber:
		var ns mj.NewSubscriber
		if err := json.Unmarshal(bcast.Payload, &ns); err != nil {
			m.log.Errorf("error decoding new_subscriber payload: %v", err)
		}
		// c.emit(&NewMarketSubscriber{
		// 	MarketName: mktName,
		// 	PeerID:     bcast.PeerID,
		// })
	default:
		m.log.Errorf("received broadcast on %s -> %s with unknown message type %s", bcast.Topic, bcast.Subject)
	}
}

func (m *Mesh) FiatRate(assetID uint32) float64 {
	m.fiatRatesMtx.RLock()
	defer m.fiatRatesMtx.RUnlock()
	sym := dex.BipIDSymbol(assetID)
	rateInfo := m.fiatRates[sym]
	if rateInfo != nil && time.Since(rateInfo.LastUpdate) < fiatrates.FiatRateDataExpiry && rateInfo.Value > 0 {
		return rateInfo.Value
	}
	return 0
}
