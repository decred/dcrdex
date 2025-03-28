package mesh

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/fiatrates"
	"decred.org/dcrdex/tatanka/mj"
	"decred.org/dcrdex/tatanka/tanka"
)

type order struct {
	*tanka.Order
	oid      tanka.ID40
	proposed map[tanka.ID32]*tanka.Match
	accepted map[tanka.ID32]*tanka.Match
}

type market struct {
	log dex.Logger

	ordsMtx sync.RWMutex
	ords    map[tanka.ID40]*order
}

func (m *market) addOrder(ord *tanka.Order) {
	m.ordsMtx.Lock()
	defer m.ordsMtx.Unlock()
	oid := ord.ID()
	if _, exists := m.ords[oid]; exists {
		// ignore it then
		return
	}
	m.ords[oid] = &order{
		Order:    ord,
		oid:      oid,
		proposed: make(map[tanka.ID32]*tanka.Match),
		accepted: make(map[tanka.ID32]*tanka.Match),
	}
}

func (m *market) addMatchProposal(match *tanka.Match) {
	m.ordsMtx.Lock()
	defer m.ordsMtx.Unlock()
	ord, found := m.ords[match.OrderID]
	if !found {
		m.log.Debugf("ignoring match proposal for unknown order %s", match.OrderID)
	}
	// Make sure it's not already known or accepted
	mid := match.ID()
	if ord.proposed[mid] != nil {
		// Already known
		return
	}
	if ord.accepted[mid] != nil {
		// Already accepted
		return
	}
	ord.proposed[mid] = match
}

func (m *market) addMatchAcceptance(match *tanka.Match) {
	m.ordsMtx.Lock()
	defer m.ordsMtx.Unlock()
	ord, found := m.ords[match.OrderID]
	if !found {
		m.log.Debugf("ignoring match proposal for unknown order %s", match.OrderID)
	}
	// Make sure it's not already known or accepted
	mid := match.ID()
	if ord.proposed[mid] != nil {
		delete(ord.proposed, mid)
	}
	if ord.accepted[mid] != nil {
		// Already accepted
		return
	}
	ord.accepted[mid] = match
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
	case mj.MessageTypeProposeMatch:
		var match tanka.Match
		if err := json.Unmarshal(bcast.Payload, &match); err != nil {
			m.log.Errorf("error unmarshaling match proposal: %v", err)
			return
		}
		mkt.addMatchProposal(&match)
	case mj.MessageTypeAcceptMatch:
		var match tanka.Match
		if err := json.Unmarshal(bcast.Payload, &match); err != nil {
			m.log.Errorf("error unmarshaling match proposal: %v", err)
			return
		}
		mkt.addMatchAcceptance(&match)
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
