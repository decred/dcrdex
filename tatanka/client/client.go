// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package client

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/fiatrates"
	"decred.org/dcrdex/dex/msgjson"
	"decred.org/dcrdex/tatanka/mj"
	"decred.org/dcrdex/tatanka/tanka"
	tcpclient "decred.org/dcrdex/tatanka/tcp/client"
	"github.com/decred/dcrd/crypto/blake256"
	"github.com/decred/dcrd/dcrec/secp256k1/v4"
)

const rsaPrivateKeyLength = 2048

// NetworkBackend represents a peer's communication protocol.
type NetworkBackend interface {
	Send(*msgjson.Message) error
	Request(msg *msgjson.Message, respHandler func(*msgjson.Message)) error
}

// tatanka is a Tatanka mesh server node.
type tatanka struct {
	NetworkBackend
	cm     *dex.ConnectionMaster
	peerID tanka.PeerID
	pub    *secp256k1.PublicKey
	config atomic.Value // *mj.TatankaConfig
}

type order struct {
	*tanka.Order
	oid      tanka.ID32
	proposed map[tanka.ID32]*tanka.Match
	accepted map[tanka.ID32]*tanka.Match
}

type market struct {
	log dex.Logger

	ordsMtx sync.RWMutex
	ords    map[tanka.ID32]*order
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

// peer is a network peer with which we have established encrypted
// communication.
type peer struct {
	id            tanka.PeerID
	pub           *secp256k1.PublicKey
	decryptionKey *rsa.PrivateKey // ours
	encryptionKey *rsa.PublicKey
}

// wireKey encrypts our RSA public key for transmission.
func (p *peer) wireKey() []byte {
	modulusB := p.decryptionKey.PublicKey.N.Bytes()
	encryptionKey := make([]byte, 8+len(modulusB))
	binary.BigEndian.PutUint64(encryptionKey[:8], uint64(p.decryptionKey.PublicKey.E))
	copy(encryptionKey[8:], modulusB)
	return encryptionKey
}

// https://stackoverflow.com/a/67035019
func (p *peer) decryptRSA(enc []byte) ([]byte, error) {
	msgLen := len(enc)
	hasher := blake256.New()
	step := p.decryptionKey.PublicKey.Size()
	var b []byte
	for start := 0; start < msgLen; start += step {
		finish := start + step
		if finish > msgLen {
			finish = msgLen
		}
		block, err := rsa.DecryptOAEP(hasher, rand.Reader, p.decryptionKey, enc[start:finish], []byte{})
		if err != nil {
			return nil, err
		}
		b = append(b, block...)
	}
	return b, nil
}

func (p *peer) encryptRSA(b []byte) ([]byte, error) {
	msgLen := len(b)
	hasher := blake256.New()
	step := p.encryptionKey.Size() - 2*hasher.Size() - 2
	var enc []byte
	for start := 0; start < msgLen; start += step {
		finish := start + step
		if finish > msgLen {
			finish = msgLen
		}
		block, err := rsa.EncryptOAEP(hasher, rand.Reader, p.encryptionKey, b[start:finish], []byte{})
		if err != nil {
			return nil, err
		}
		enc = append(enc, block...)
	}
	return enc, nil
}

// IncomingPeerConnect will be emitted when a peer requests a connection for
// the transmission of tankagrams.
type IncomingPeerConnect struct {
	PeerID tanka.PeerID
	Reject func()
}

// IncomingTankagram will be emitted when we receive a tankagram from a
// connected peer.
type IncomingTankagram struct {
	Msg     *msgjson.Message
	Respond func(thing interface{}) error
}

// NewMarketSubscriber will be emitted when a new client subscribes to a market.
type NewMarketSubscriber struct {
	MarketName string
	PeerID     tanka.PeerID
}

// Config is the configuration for the TankaClient.
type Config struct {
	Logger     dex.Logger
	PrivateKey *secp256k1.PrivateKey
}

type TankaClient struct {
	wg                   *sync.WaitGroup
	peerID               tanka.PeerID
	priv                 *secp256k1.PrivateKey
	log                  dex.Logger
	requestHandlers      map[string]func(*tatanka, *msgjson.Message) *msgjson.Error
	notificationHandlers map[string]func(*tatanka, *msgjson.Message)
	payloads             chan interface{}

	tankaMtx sync.RWMutex
	tankas   map[tanka.PeerID]*tatanka

	peersMtx sync.RWMutex
	peers    map[tanka.PeerID]*peer

	marketsMtx sync.RWMutex
	markets    map[string]*market

	fiatRatesMtx sync.RWMutex
	fiatRates    map[string]*fiatrates.FiatRateInfo
}

func New(cfg *Config) *TankaClient {
	var peerID tanka.PeerID
	copy(peerID[:], cfg.PrivateKey.PubKey().SerializeCompressed())

	c := &TankaClient{
		log:       cfg.Logger,
		peerID:    peerID,
		priv:      cfg.PrivateKey,
		tankas:    make(map[tanka.PeerID]*tatanka),
		peers:     make(map[tanka.PeerID]*peer),
		markets:   make(map[string]*market),
		payloads:  make(chan interface{}, 128),
		fiatRates: make(map[string]*fiatrates.FiatRateInfo),
	}
	c.prepareHandlers()
	return c
}

func (c *TankaClient) ID() tanka.PeerID {
	return c.peerID
}

func (c *TankaClient) prepareHandlers() {
	c.requestHandlers = map[string]func(*tatanka, *msgjson.Message) *msgjson.Error{
		mj.RouteTankagram: c.handleTankagram,
	}

	c.notificationHandlers = map[string]func(*tatanka, *msgjson.Message){
		mj.RouteBroadcast: c.handleBroadcast,
		mj.RouteRates:     c.handleRates,
	}
}

func (c *TankaClient) Next() <-chan interface{} {
	return c.payloads
}

func (c *TankaClient) handleTankagram(tt *tatanka, tankagram *msgjson.Message) *msgjson.Error {
	var gram mj.Tankagram
	if err := tankagram.Unmarshal(&gram); err != nil {
		return msgjson.NewError(mj.ErrBadRequest, "unmarshal error")
	}

	c.peersMtx.Lock()
	defer c.peersMtx.Unlock()
	p, peerExisted := c.peers[gram.From]
	if !peerExisted {
		// TODO: We should do a little message verification before accepting
		// new peers.
		if gram.Message == nil || gram.Message.Route != mj.RouteEncryptionKey {
			return msgjson.NewError(mj.ErrBadRequest, "where's your key?")
		}
		pub, err := secp256k1.ParsePubKey(gram.From[:])
		if err != nil {
			c.log.Errorf("could not parse pubkey for tankagram from %s: %w", gram.From, err)
			return msgjson.NewError(mj.ErrBadRequest, "bad pubkey")
		}
		priv, err := rsa.GenerateKey(rand.Reader, rsaPrivateKeyLength)
		if err != nil {
			return msgjson.NewError(mj.ErrInternal, "error generating rsa key: %v", err)
		}
		p = &peer{
			id:            gram.From,
			pub:           pub,
			decryptionKey: priv,
		}

		msg := gram.Message
		if err := mj.CheckSig(msg, p.pub); err != nil {
			c.log.Errorf("%s sent a unencrypted message with a bad signature: %w", p.id, err)
			return msgjson.NewError(mj.ErrBadRequest, "bad gram sig")
		}

		var b dex.Bytes
		if err := msg.Unmarshal(&b); err != nil {
			c.log.Errorf("%s tankagram unmarshal error: %w", err)
			return msgjson.NewError(mj.ErrBadRequest, "unmarshal key error")
		}

		p.encryptionKey, err = decodePubkeyRSA(b)
		if err != nil {
			c.log.Errorf("error decoding RSA pub key from %s: %v", p.id, err)
			return msgjson.NewError(mj.ErrBadRequest, "bad key encoding")
		}

		if err := c.sendResult(tt, tankagram.ID, dex.Bytes(p.wireKey())); err != nil {
			c.log.Errorf("error responding to encryption key message from peer %s", p.id)
		} else {
			c.peers[p.id] = p
			c.emit(&IncomingPeerConnect{
				PeerID: p.id,
				Reject: func() {
					c.peersMtx.Lock()
					delete(c.peers, p.id)
					c.peersMtx.Unlock()
				},
			})
		}
		return nil
	}

	// If this isn't the encryption key, this gram.Message is ignored and this
	// is assumed to be encrypted.
	if len(gram.EncryptedMsg) == 0 {
		c.log.Errorf("%s sent a tankagram with no message or data", p.id)
		return msgjson.NewError(mj.ErrBadRequest, "bad gram")
	}

	b, err := p.decryptRSA(gram.EncryptedMsg)
	if err != nil {
		c.log.Errorf("%s sent an enrypted message that didn't decrypt: %v", p.id, err)
		return msgjson.NewError(mj.ErrBadRequest, "bad encryption")
	}
	msg, err := msgjson.DecodeMessage(b)
	if err != nil {
		c.log.Errorf("%s sent a tankagram that didn't encode a message: %v", p.id, err)
		return msgjson.NewError(mj.ErrBadRequest, "where's the message?")
	}

	c.emit(&IncomingTankagram{
		Msg: msg,
		Respond: func(thing interface{}) error {
			b, err := json.Marshal(thing)
			if err != nil {
				return err
			}
			enc, err := p.encryptRSA(b)
			if err != nil {
				return err
			}
			return c.sendResult(tt, tankagram.ID, dex.Bytes(enc))
		},
	})

	return nil
}

func (c *TankaClient) handleBroadcast(tt *tatanka, msg *msgjson.Message) {
	var bcast mj.Broadcast
	if err := msg.Unmarshal(&bcast); err != nil {
		c.log.Errorf("%s broadcast unmarshal error: %w", err)
		return
	}
	switch bcast.Topic {
	case mj.TopicMarket:
		c.handleMarketBroadcast(tt, &bcast)
	}
	c.emit(bcast)
}

func (c *TankaClient) handleRates(tt *tatanka, msg *msgjson.Message) {
	var rm mj.RateMessage
	if err := msg.Unmarshal(&rm); err != nil {
		c.log.Errorf("%s rate message unmarshal error: %w", err)
		return
	}
	switch rm.Topic {
	case mj.TopicFiatRate:
		c.fiatRatesMtx.Lock()
		for ticker, rateInfo := range rm.Rates {
			c.fiatRates[strings.ToLower(ticker)] = &fiatrates.FiatRateInfo{
				Value:      rateInfo.Value,
				LastUpdate: time.Now(),
			}
		}
		c.fiatRatesMtx.Unlock()
	}
	c.emit(rm)
}

func (c *TankaClient) SubscribeToFiatRates() error {
	msg := mj.MustRequest(mj.RouteSubscribe, &mj.Subscription{
		Topic: mj.TopicFiatRate,
	})

	var nSuccessful int
	for _, tt := range c.tankaNodes() {
		var ok bool // true is only possible non-error payload.
		if err := c.request(tt, msg, &ok); err != nil {
			c.log.Errorf("Error subscribing to fiat rates with %s: %v", tt.peerID, err)
			continue
		}
		nSuccessful++
	}

	if nSuccessful == 0 {
		return errors.New("failed to subscribe to fiat rates on any servers")
	}

	return nil
}

func (c *TankaClient) FiatRate(assetID uint32) float64 {
	c.fiatRatesMtx.RLock()
	defer c.fiatRatesMtx.RUnlock()
	sym := dex.BipIDSymbol(assetID)
	rateInfo := c.fiatRates[sym]
	if rateInfo != nil && time.Since(rateInfo.LastUpdate) < fiatrates.FiatRateDataExpiry && rateInfo.Value > 0 {
		return rateInfo.Value
	}
	return 0
}

func (c *TankaClient) emit(thing interface{}) {
	select {
	case c.payloads <- thing:
	default:
		c.log.Errorf("payload channel is blocking")
	}
}

func (c *TankaClient) handleMarketBroadcast(_ *tatanka, bcast *mj.Broadcast) {
	mktName := string(bcast.Subject)
	c.marketsMtx.RLock()
	mkt, found := c.markets[mktName]
	c.marketsMtx.RUnlock()
	if !found {
		c.log.Debugf("received order notification for unknown market %q", mktName)
		return
	}
	switch bcast.MessageType {
	case mj.MessageTypeTrollBox:
		var troll mj.Troll
		if err := json.Unmarshal(bcast.Payload, &troll); err != nil {
			c.log.Errorf("error unmarshaling trollbox message: %v", err)
			return
		}
		fmt.Printf("trollbox message for market %s: %s\n", mktName, troll.Msg)
	case mj.MessageTypeNewOrder:
		var ord tanka.Order
		if err := json.Unmarshal(bcast.Payload, &ord); err != nil {
			c.log.Errorf("error unmarshaling new order: %v", err)
			return
		}
		mkt.addOrder(&ord)
	case mj.MessageTypeProposeMatch:
		var match tanka.Match
		if err := json.Unmarshal(bcast.Payload, &match); err != nil {
			c.log.Errorf("error unmarshaling match proposal: %v", err)
			return
		}
		mkt.addMatchProposal(&match)
	case mj.MessageTypeAcceptMatch:
		var match tanka.Match
		if err := json.Unmarshal(bcast.Payload, &match); err != nil {
			c.log.Errorf("error unmarshaling match proposal: %v", err)
			return
		}
		mkt.addMatchAcceptance(&match)
	case mj.MessageTypeNewSubscriber:
		var ns mj.NewSubscriber
		if err := json.Unmarshal(bcast.Payload, &ns); err != nil {
			c.log.Errorf("error decoding new_subscriber payload: %v", err)
		}
		// c.emit(&NewMarketSubscriber{
		// 	MarketName: mktName,
		// 	PeerID:     bcast.PeerID,
		// })
	default:
		c.log.Errorf("received broadcast on %s -> %s with unknown message type %s", bcast.Topic, bcast.Subject)
	}
}

func (c *TankaClient) Broadcast(topic tanka.Topic, subject tanka.Subject, msgType mj.BroadcastMessageType, thing interface{}) error {
	payload, err := json.Marshal(thing)
	if err != nil {
		return fmt.Errorf("error marshaling broadcast payload: %v", err)
	}
	note := mj.MustRequest(mj.RouteBroadcast, &mj.Broadcast{
		PeerID:      c.peerID,
		Topic:       topic,
		Subject:     subject,
		MessageType: msgType,
		Payload:     payload,
		Stamp:       time.Now(),
	})
	var oks int
	for _, tt := range c.tankas {
		var ok bool
		if err := c.request(tt, note, &ok); err != nil || !ok {
			c.log.Errorf("error sending to %s: ok = %t, err = %v", tt.peerID, ok, err)
		} else {
			oks++
		}
	}
	if oks == 0 {
		return errors.New("broadcast failed for all servers")
	}
	return nil
}

func (c *TankaClient) Connect(ctx context.Context) (*sync.WaitGroup, error) {
	var wg sync.WaitGroup
	c.wg = &wg

	c.log.Infof("Starting TankaClient with peer ID %s", c.peerID)

	wg.Add(1)
	go func() {
		defer wg.Done()
		<-ctx.Done()
		c.tankaMtx.Lock()
		for _, tt := range c.tankas {
			tt.cm.Disconnect()
		}
		c.tankas = make(map[tanka.PeerID]*tatanka)
		c.tankaMtx.Unlock()
	}()

	return c.wg, nil
}

func (c *TankaClient) AddTatankaNode(ctx context.Context, peerID tanka.PeerID, uri string, cert []byte) error {
	pub, err := secp256k1.ParsePubKey(peerID[:])
	if err != nil {
		return fmt.Errorf("error parsing pubkey from peer ID: %w", err)
	}

	log := c.log.SubLogger("TCP")
	cl, err := tcpclient.New(&tcpclient.Config{
		Logger: log,
		URL:    uri + "/ws",
		Cert:   cert,
		HandleMessage: func(msg *msgjson.Message) *msgjson.Error {
			return c.handleTatankaMessage(peerID, msg)
		},
	})

	if err != nil {
		return fmt.Errorf("error creating connection: %w", err)
	}

	cm := dex.NewConnectionMaster(cl)
	if err := cm.ConnectOnce(ctx); err != nil {
		return fmt.Errorf("error connecting: %w", err)
	}

	c.tankaMtx.Lock()
	defer c.tankaMtx.Unlock()

	if oldTanka, exists := c.tankas[peerID]; exists {
		oldTanka.cm.Disconnect()
		log.Infof("replacing existing connection")
	}

	c.tankas[peerID] = &tatanka{
		NetworkBackend: cl,
		cm:             cm,
		peerID:         peerID,
		pub:            pub,
	}

	return nil
}

func (c *TankaClient) handleTatankaMessage(peerID tanka.PeerID, msg *msgjson.Message) *msgjson.Error {
	if c.log.Level() == dex.LevelTrace {
		c.log.Tracef("Client handling message from tatanka node: route = %s, payload = %s", msg.Route, mj.Truncate(msg.Payload))
	}

	c.tankaMtx.RLock()
	tt, exists := c.tankas[peerID]
	c.tankaMtx.RUnlock()
	if !exists {
		c.log.Errorf("%q message received from unknown peer %s", msg.Route, peerID)
		return msgjson.NewError(mj.ErrAuth, "who the heck are you?")
	}

	if err := mj.CheckSig(msg, tt.pub); err != nil {
		// DRAFT TODO: Record for reputation somehow, no?
		c.log.Errorf("tatanka node %s sent a bad signature. disconnecting", tt.peerID)
		return msgjson.NewError(mj.ErrAuth, "bad sig")
	}

	switch msg.Type {
	case msgjson.Request:
		handle, found := c.requestHandlers[msg.Route]
		if !found {
			// DRAFT NOTE: We should pontentially be more permissive of unknown
			// routes in order to support minor network upgrades that add new
			// routes.
			c.log.Errorf("tatanka node %s sent a request to an unknown route %q", peerID, msg.Route)
			return msgjson.NewError(mj.ErrBadRequest, "what is route %q?", msg.Route)
		}
		c.handleRequest(tt, msg, handle)
	case msgjson.Notification:
		handle, found := c.notificationHandlers[msg.Route]
		if !found {
			// DRAFT NOTE: We should pontentially be more permissive of unknown
			// routes in order to support minor network upgrades that add new
			// routes.
			c.log.Errorf("tatanka node %s sent a notification to an unknown route %q", peerID, msg.Route)
			return msgjson.NewError(mj.ErrBadRequest, "what is route %q?", msg.Route)
		}
		handle(tt, msg)
	default:
		c.log.Errorf("tatanka node %s send a message with an unhandleable type %d", msg.Type)
		return msgjson.NewError(mj.ErrBadRequest, "message type %d doesn't work for me", msg.Type)
	}

	return nil
}

func (c *TankaClient) sendResult(tt *tatanka, msgID uint64, result interface{}) error {
	resp, err := msgjson.NewResponse(msgID, result, nil)
	if err != nil {
		return err
	}
	if err := c.send(tt, resp); err != nil {
		return err
	}
	return nil
}

func (c *TankaClient) handleRequest(tt *tatanka, msg *msgjson.Message, handle func(*tatanka, *msgjson.Message) *msgjson.Error) {
	if msgErr := handle(tt, msg); msgErr != nil {
		respMsg := mj.MustResponse(msg.ID, nil, msgErr)
		if err := c.send(tt, respMsg); err != nil {
			c.log.Errorf("Send error: %v", err)
		}
	}
}

func (c *TankaClient) Auth(peerID tanka.PeerID) error {
	c.tankaMtx.RLock()
	tt, found := c.tankas[peerID]
	c.tankaMtx.RUnlock()
	if !found {
		return fmt.Errorf("cannot auth with unknown server %s", peerID)
	}
	return c.auth(tt)
}

func (c *TankaClient) auth(tt *tatanka) error {
	connectMsg := mj.MustRequest(mj.RouteConnect, &mj.Connect{ID: c.peerID})
	var cfg *mj.TatankaConfig
	if err := c.request(tt, connectMsg, &cfg); err != nil {
		return err
	}
	tt.config.Store(cfg)
	return nil
}

func (c *TankaClient) tankaNodes() []*tatanka {
	c.tankaMtx.RLock()
	defer c.tankaMtx.RUnlock()
	tankas := make([]*tatanka, 0, len(c.tankas))
	for _, tanka := range c.tankas {
		tankas = append(tankas, tanka)
	}
	return tankas
}

func (c *TankaClient) PostBond(bond *tanka.Bond) error {
	msg := mj.MustRequest(mj.RoutePostBond, []*tanka.Bond{bond})
	var success bool
	for _, tt := range c.tankaNodes() {
		var res bool
		if err := c.request(tt, msg, &res); err != nil {
			c.log.Errorf("error sending bond to tatanka node %s", tt.peerID)
		} else {
			success = true
		}
	}
	if success {
		return nil
	}
	return errors.New("failed to report bond to any tatanka nodes")
}

func (c *TankaClient) SubscribeMarket(baseID, quoteID uint32) error {
	mktName, err := dex.MarketName(baseID, quoteID)
	if err != nil {
		return fmt.Errorf("error constructing market name: %w", err)
	}

	msg := mj.MustRequest(mj.RouteSubscribe, &mj.Subscription{
		Topic:   mj.TopicMarket,
		Subject: tanka.Subject(mktName),
	})

	c.marketsMtx.Lock()
	defer c.marketsMtx.Unlock()

	ttNodes := c.tankaNodes()
	subscribed := make([]*tatanka, 0, len(ttNodes))
	for _, tt := range ttNodes {
		var ok bool // true is only possible non-error payload.
		if err := c.request(tt, msg, &ok); err != nil {
			c.log.Errorf("Error subscribing to %s market with %s: %w", mktName, tt.peerID, err)
			continue
		}
		subscribed = append(subscribed, tt)
	}

	if len(subscribed) == 0 {
		return fmt.Errorf("failed to subscribe to market %s on any servers", mktName)
	} else {
		c.markets[mktName] = &market{
			log:  c.log.SubLogger(mktName),
			ords: make(map[tanka.ID32]*order),
		}
	}

	return nil
}

func (c *TankaClient) send(tt *tatanka, msg *msgjson.Message) error {
	mj.SignMessage(c.priv, msg)
	return tt.Send(msg)
}

func (c *TankaClient) request(tt *tatanka, msg *msgjson.Message, resp interface{}) error {
	mj.SignMessage(c.priv, msg)
	errChan := make(chan error)
	if err := tt.Request(msg, func(msg *msgjson.Message) {
		errChan <- msg.UnmarshalResult(&resp)
	}); err != nil {
		errChan <- fmt.Errorf("request error: %w", err)
	}

	select {
	case err := <-errChan:
		if err != nil {
			return fmt.Errorf("tankagram error: %w", err)
		}
	case <-time.After(time.Second * 30):
		return errors.New("timed out waiting for tankagram result")
	}
	return nil
}

func (c *TankaClient) ConnectPeer(peerID tanka.PeerID, hosts ...tanka.PeerID) (*mj.TankagramResult, error) {
	c.peersMtx.Lock()
	defer c.peersMtx.Unlock()
	p, exists := c.peers[peerID]
	if !exists {
		priv, err := rsa.GenerateKey(rand.Reader, rsaPrivateKeyLength)
		if err != nil {
			return nil, fmt.Errorf("error generating rsa key: %v", err)
		}
		remotePub, err := secp256k1.ParsePubKey(peerID[:])
		if err != nil {
			return nil, fmt.Errorf("error parsing remote pubkey: %v", err)
		}
		p = &peer{
			id:            peerID,
			pub:           remotePub,
			decryptionKey: priv,
		}
	}

	msg := mj.MustNotification(mj.RouteEncryptionKey, dex.Bytes(p.wireKey()))
	mj.SignMessage(c.priv, msg) // We sign the embedded message separately.

	req := mj.MustRequest(mj.RouteTankagram, &mj.Tankagram{
		To:      peerID,
		From:    c.peerID,
		Message: msg,
	})

	var tts []*tatanka
	if len(hosts) == 0 {
		tts = c.tankaNodes()
	} else {
		tts = make([]*tatanka, 0, len(hosts))
		c.tankaMtx.RLock()
		for _, host := range hosts {
			tt, exists := c.tankas[host]
			if !exists {
				c.log.Warnf("Requested host %q is not known", host)
				continue
			}
			tts = append(tts, tt)
		}
		c.tankaMtx.RUnlock()
	}
	if len(tts) == 0 {
		return nil, errors.New("no hosts")
	}

	for _, tt := range tts {
		var r mj.TankagramResult
		if err := c.request(tt, req, &r); err != nil {
			c.log.Errorf("error sending rsa key to %s: %v", peerID, err)
			continue
		}
		if r.Result == mj.TRTTransmitted {
			// We need to get this to the caller, as a Tankagram result can
			// be used as part of an audit request for reporting penalties.
			pub, err := decodePubkeyRSA(r.Response)
			if err != nil {
				return nil, fmt.Errorf("error decoding RSA pub key from %s: %v", p.id, err)
			}
			p.encryptionKey = pub
			c.peers[peerID] = p
			return &r, nil
		}
		return &r, nil
	}
	return nil, errors.New("no path")
}

func (c *TankaClient) SendTankagram(peerID tanka.PeerID, msg *msgjson.Message) (_ *mj.TankagramResult, _ json.RawMessage, err error) {
	c.peersMtx.RLock()
	p, known := c.peers[peerID]
	c.peersMtx.RUnlock()

	if !known {
		return nil, nil, fmt.Errorf("not connected to peer %s", peerID)
	}

	mj.SignMessage(c.priv, msg)
	tankaGram := &mj.Tankagram{
		From: c.peerID,
		To:   peerID,
	}
	tankaGram.EncryptedMsg, err = c.signAndEncryptTankagram(p, msg)
	if err != nil {
		return nil, nil, fmt.Errorf("error signing and encrypting tankagram for %s: %w", p.id, err)
	}
	wrappedMsg := mj.MustRequest(mj.RouteTankagram, tankaGram)
	for _, tt := range c.tankaNodes() {
		var r mj.TankagramResult
		if err := c.request(tt, wrappedMsg, &r); err != nil {
			c.log.Errorf("error sending tankagram to %s via %s: %v", p.id, tt.peerID, err)
			continue
		}
		switch r.Result {
		case mj.TRTTransmitted:
			resB, err := p.decryptRSA(r.Response)
			return &r, resB, err
		case mj.TRTErrFromPeer, mj.TRTErrBadClient:
			return &r, nil, nil
		case mj.TRTNoPath, mj.TRTErrFromTanka:
			continue
		default:
			return nil, nil, fmt.Errorf("unknown result %q", r.Result)
		}

	}
	return nil, nil, fmt.Errorf("no path")
}

func (c *TankaClient) signAndEncryptTankagram(p *peer, msg *msgjson.Message) ([]byte, error) {
	mj.SignMessage(c.priv, msg)
	b, err := json.Marshal(msg)
	if err != nil {
		return nil, fmt.Errorf("error marshaling tankagram: %w", err)
	}
	return p.encryptRSA(b)
}

func decodePubkeyRSA(b []byte) (*rsa.PublicKey, error) {
	if len(b) < 9 {
		return nil, fmt.Errorf("invalid payload length of %d", len(b))
	}
	exponentB, modulusB := b[:8], b[8:]
	exponent := int(binary.BigEndian.Uint64(exponentB))
	modulus := new(big.Int).SetBytes(modulusB)
	return &rsa.PublicKey{
		E: exponent,
		N: modulus,
	}, nil
}
