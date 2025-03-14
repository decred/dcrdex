// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package mesh

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/fiatrates"
	"decred.org/dcrdex/dex/lexi"
	"decred.org/dcrdex/dex/msgjson"
	"decred.org/dcrdex/tatanka/client/conn"
	"decred.org/dcrdex/tatanka/mj"
	"decred.org/dcrdex/tatanka/tanka"
	"github.com/decred/dcrd/dcrec/secp256k1/v4"
)

// Config is the configuration settings for Mesh.
type Config struct {
	DataDir    string
	PrivateKey *secp256k1.PrivateKey
	Logger     dex.Logger
	EntryNode  *TatankaCredentials
}

// Mesh is a manager for operations on the Tatanka Mesh Network.
type Mesh struct {
	priv   *secp256k1.PrivateKey
	peerID tanka.PeerID
	// cfg      *Config
	log       dex.Logger
	entryNode *TatankaCredentials
	conn      *meshConn
	payloads  chan interface{}

	dataDir   string
	db        *lexi.DB
	dbCM      *dex.ConnectionMaster
	bondTable *lexi.Table

	marketsMtx sync.RWMutex
	markets    map[string]*market

	fiatRatesMtx sync.RWMutex
	fiatRates    map[string]*fiatrates.FiatRateInfo
}

// New is the constructor for a new Mesh.
func New(cfg *Config) (*Mesh, error) {
	var peerID tanka.PeerID
	copy(peerID[:], cfg.PrivateKey.PubKey().SerializeCompressed())

	if cfg.DataDir == "" {
		return nil, errors.New("no data directory provided")
	}

	mesh := &Mesh{
		priv:      cfg.PrivateKey,
		peerID:    peerID,
		log:       cfg.Logger,
		dataDir:   cfg.DataDir,
		entryNode: cfg.EntryNode,
		payloads:  make(chan interface{}, 128),
		markets:   make(map[string]*market),
		fiatRates: make(map[string]*fiatrates.FiatRateInfo),
	}

	if err := mesh.initializeDB(); err != nil {
		return nil, fmt.Errorf("failed to initialize database: %w", err)
	}

	return mesh, nil
}

// Connect initializes the Mesh.
func (m *Mesh) Connect(ctx context.Context) (*sync.WaitGroup, error) {
	var wg sync.WaitGroup

	dbCM := dex.NewConnectionMaster(m.db)
	if err := dbCM.ConnectOnce(ctx); err != nil {
		return nil, fmt.Errorf("couldn't start database: %w", err)
	}
	m.dbCM = dbCM

	mesh := conn.New(&conn.Config{
		EntryNode: m.entryNode,
		Logger:    m.log.SubLogger("tTC"),
		Handlers: &conn.MessageHandlers{
			HandleTatankaRequest: func(tatankaID tanka.PeerID, msg *msgjson.Message) *msgjson.Error {
				return m.handleTatankaRequest(tatankaID, msg)
			},
			HandleTatankaNotification: func(tatankaID tanka.PeerID, msg *msgjson.Message) {
				m.handleTatankaNotification(tatankaID, msg)
			},
			HandlePeerMessage: func(peerID tanka.PeerID, msg *conn.IncomingTankagram) *msgjson.Error {
				return m.handlePeerRequest(peerID, msg)
			},
		},
		PrivateKey: m.priv,
	})

	meshCM := dex.NewConnectionMaster(mesh)
	if err := meshCM.ConnectOnce(ctx); err != nil {
		return nil, fmt.Errorf("ConnectOnce error: %w", err)
	}

	m.conn = &meshConn{mesh, meshCM}

	wg.Add(1)
	go func() {
		<-dbCM.Done()
		<-meshCM.Done()
		wg.Done()
	}()

	return &wg, nil
}

// ID returns our peer ID on Mesh.
func (m *Mesh) ID() tanka.PeerID {
	return m.peerID
}

func (m *Mesh) initializeDB() error {
	db, err := lexi.New(&lexi.Config{
		Path: m.dataDir,
		Log:  m.log.SubLogger("DB"),
	})
	if err != nil {
		return err
	}
	m.db = db

	m.bondTable, err = db.Table("bond")
	return err
}

// Next emits certain types of messages.
func (m *Mesh) Next() <-chan any {
	return m.payloads
}

func (m *Mesh) emit(thing any) {
	select {
	case m.payloads <- thing:
	default:
		m.log.Errorf("payload channel is blocking")
	}
}

func (m *Mesh) handleTatankaRequest(tatankaID tanka.PeerID, msg *msgjson.Message) *msgjson.Error {
	switch msg.Route {
	default:
		m.log.Debugf("Received a request from tatanka node %s for unknown route %q", tatankaID, msg.Route)
	}
	return nil
}

func (m *Mesh) handleTatankaNotification(peerID tanka.PeerID, msg *msgjson.Message) {
	switch msg.Route {
	case mj.RouteBroadcast:
		m.handleBroadcast(msg)
	case mj.RouteRates:
		m.handleRates(msg)
	default:
		m.emit(msg)
	}
}

func (m *Mesh) handlePeerRequest(peerID tanka.PeerID, msgI any) *msgjson.Error {
	switch msgI.(type) {
	case *conn.IncomingTankagram:
	}
	m.emit(msgI)
	return nil
}

func (m *Mesh) Broadcast(topic tanka.Topic, subject tanka.Subject, msgType mj.BroadcastMessageType, thing interface{}) error {
	payload, err := json.Marshal(thing)
	if err != nil {
		return fmt.Errorf("error marshaling broadcast payload: %v", err)
	}
	req := mj.MustRequest(mj.RouteBroadcast, &mj.Broadcast{
		PeerID:      m.peerID,
		Topic:       topic,
		Subject:     subject,
		MessageType: msgType,
		Payload:     payload,
		Stamp:       time.Now(),
	})
	// Only possible non-error response is `true`.
	var ok bool
	return m.conn.RequestMesh(req, &ok)
}

func (m *Mesh) SubscribeToFiatRates() error {
	req := mj.MustRequest(mj.RouteSubscribe, &mj.Subscription{
		Topic: mj.TopicFiatRate,
	})

	// Only possible non-error response is `true`.
	var ok bool
	return m.conn.RequestMesh(req, &ok)
}

// PostBond stores the bond in the database and sends it to the mesh.
func (m *Mesh) PostBond(bond *tanka.Bond) error {
	k := bond.ID()
	if err := m.bondTable.Set(k[:], lexi.JSON(bond)); err != nil {
		return fmt.Errorf("error storing bond in DB: %w", err)
	}
	req := mj.MustRequest(mj.RoutePostBond, []*tanka.Bond{bond})
	var res bool
	return m.conn.RequestMesh(req, &res)
}

// ActiveBonds retrieves the active bonds from the database.
func (m *Mesh) ActiveBonds() ([]*tanka.Bond, error) {
	bonds := make([]*tanka.Bond, 0, 1)
	return bonds, m.bondTable.Iterate(nil, func(it *lexi.Iter) error {
		var bond tanka.Bond
		if err := it.V(func(vB []byte) error {
			return json.Unmarshal(vB, &bond)
		}); err != nil {
			return err
		}
		bonds = append(bonds, &bond)
		return nil
	})
}

func (m *Mesh) SubscribeMarket(baseID, quoteID uint32) error {
	mktName, err := dex.MarketName(baseID, quoteID)
	if err != nil {
		return fmt.Errorf("error constructing market name: %w", err)
	}

	req := mj.MustRequest(mj.RouteSubscribe, &mj.Subscription{
		Topic:   mj.TopicMarket,
		Subject: tanka.Subject(mktName),
	})
	mj.SignMessage(m.priv, req)

	m.marketsMtx.Lock()
	defer m.marketsMtx.Unlock()

	// Only possible non-error response is `true`.
	var ok bool
	if err := m.conn.RequestMesh(req, &ok); err != nil {
		return err
	}

	m.markets[mktName] = &market{
		log:  m.log.SubLogger(mktName),
		ords: make(map[tanka.ID32]*order),
	}
	return nil
}

func (m *Mesh) handleBroadcast(msg *msgjson.Message) {
	var bcast mj.Broadcast
	if err := msg.Unmarshal(&bcast); err != nil {
		m.log.Errorf("%s broadcast unmarshal error: %w", err)
		return
	}
	switch bcast.Topic {
	case mj.TopicMarket:
		m.handleMarketBroadcast(&bcast)
	}
	m.emit(&bcast)
}

func (m *Mesh) handleRates(msg *msgjson.Message) {
	var rm mj.RateMessage
	if err := msg.Unmarshal(&rm); err != nil {
		m.log.Errorf("%s rate message unmarshal error: %w", err)
		return
	}
	switch rm.Topic {
	case mj.TopicFiatRate:
		m.fiatRatesMtx.Lock()
		for ticker, rateInfo := range rm.Rates {
			m.fiatRates[strings.ToLower(ticker)] = &fiatrates.FiatRateInfo{
				Value:      rateInfo.Value,
				LastUpdate: time.Now(),
			}
		}
		m.fiatRatesMtx.Unlock()
	}
	m.emit(&rm)
}

func (m *Mesh) Auth(tatankaID tanka.PeerID) error {
	return m.conn.Auth(tatankaID)
}

func (m *Mesh) ConnectPeer(peerID tanka.PeerID) error {
	return m.conn.ConnectPeer(peerID)
}

func (m *Mesh) RequestPeer(peerID tanka.PeerID, msg *msgjson.Message, thing interface{}) error {
	return m.conn.RequestPeer(peerID, msg, thing)
}

type TatankaCredentials = conn.TatankaCredentials

// meshConn is our representation of the connection to the mesh network.
type meshConn struct {
	*conn.MeshConn
	cm *dex.ConnectionMaster
}
