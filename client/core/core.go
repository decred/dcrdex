// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package core

import (
	"context"
	"encoding/json"
	"net/url"
	"sync"
	"time"

	"decred.org/dcrdex/client/comms"
	"decred.org/dcrdex/client/db"
	"decred.org/dcrdex/client/db/bolt"
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/msgjson"
)

var log dex.Logger

// websocket is satisfied by a comms.WsConn, or a stub for testing.
type websocket interface {
	NextID() uint64
	WaitForShutdown()
	Send(msg *msgjson.Message) error
	Request(msg *msgjson.Message, f func(*msgjson.Message)) error
	MessageSource() <-chan *msgjson.Message
}

// dexConnection is the websocket connection and the DEX configuration.
type dexConnection struct {
	conn   websocket
	assets map[uint32]*msgjson.Asset
	cfg    *msgjson.ConfigResult
}

// Config is the configuration for the Core.
type Config struct {
	// DBPath is a filepath to use for the client database. If the database does
	// not already exist, it will be created.
	DBPath string
	// Logger is a logger for the core to use. Having the logger as an argument
	// enables creating custom loggers for use in a GUI interface.
	Logger dex.Logger
	// Certs is a mapping of URL to filepaths of TLS Certificates for the server.
	// This is intended for accomodating self-signed certificates, mostly for
	// testing.
	Certs map[string]string
}

// Core is the core client application.
type Core struct {
	ctx     context.Context
	wg      sync.WaitGroup
	cfg     *Config
	connMtx sync.RWMutex
	conns   map[string]*dexConnection
	db      db.DB
	certs   map[string]string
}

// New is the constructor for a new Core.
func New(cfg *Config) *Core {
	log = cfg.Logger
	core := &Core{
		cfg:   cfg,
		conns: make(map[string]*dexConnection),
	}
	return core
}

// Run runs the core. Satisfies the runner.Runner interface.
func (c *Core) Run(ctx context.Context) {
	log.Infof("started DEX client core")
	db, err := bolt.NewDB(ctx, c.cfg.DBPath)
	if err != nil {
		log.Errorf("database initialization error: %v", err)
		return
	}
	c.ctx = ctx
	c.db = db
	go c.initialize()
	c.wg.Wait()
	log.Infof("DEX client core off")
}

// ListMarkets returns a list of known markets.
func (c *Core) ListMarkets() []*MarketInfo {
	c.connMtx.RLock()
	defer c.connMtx.RUnlock()
	var infos []*MarketInfo
	for uri, dc := range c.conns {
		mi := &MarketInfo{DEX: uri}
		for _, bq := range dc.cfg.Markets {
			base, quote := dc.assets[bq[0]], dc.assets[bq[1]]
			mi.Markets = append(mi.Markets, Market{
				BaseID:      base.ID,
				BaseSymbol:  base.Symbol,
				QuoteID:     quote.ID,
				QuoteSymbol: quote.Symbol,
			})
		}
		infos = append(infos, mi)
	}
	return infos
}

// initialize pulls the known DEX URLs from the database and attempts to
// connect and retreive the DEX configuration.
func (c *Core) initialize() {
	dexs, err := c.db.ListAccounts()
	if err != nil {
		log.Errorf("Error retreiving accounts from database: %v", err)
	}
	var wg sync.WaitGroup
	for _, uri := range dexs {
		wg.Add(1)
		u := uri
		go func() {
			c.addDex(u)
			wg.Done()
		}()
	}
	wg.Wait()
	if len(dexs) > 0 {
		c.connMtx.RLock()
		log.Infof("Successfully connected to %d out of %d DEX servers", len(c.conns), len(dexs))
		c.connMtx.RUnlock()
	}
}

// addDex adds a dexConnection to the conns map if a connection can be made
// and the DEX configuration is successfully retrieved. The connection is
// unauthenticated until the `connect` request is sent and accepted by the
// server.
func (c *Core) addDex(uri string) {
	// Get the host from the DEX URL.
	parsedURL, err := url.Parse(uri)
	if err != nil {
		log.Errorf("error parsing account URL %s: %v", uri, err)
		return
	}
	// Create a websocket connection to the server.
	conn, err := comms.NewWsConn(&comms.WsCfg{
		URL:      "wss://" + parsedURL.Host + "/ws",
		PingWait: 60 * time.Second,
		RpcCert:  c.certs[uri],
		ReconnectSync: func() {
			go c.handleReconnect(uri)
		},
		Ctx: c.ctx,
	})
	if err != nil {
		log.Errorf("Error creating websocket connection for %s: %v", uri, err)
		return
	}
	// Request the market configuration. The DEX is only added when the DEX
	// configuration is successfully retrieved.
	reqMsg, err := msgjson.NewRequest(conn.NextID(), msgjson.ConfigRoute, nil)
	if err != nil {
		log.Errorf("error creating 'config' request: %v", err)
	}
	conn.Request(reqMsg, func(msg *msgjson.Message) {
		resp, err := msg.Response()
		if err != nil {
			log.Errorf("failed to parse 'config' response message: %v", err)
			return
		}
		var dexCfg *msgjson.ConfigResult
		err = json.Unmarshal(resp.Result, dexCfg)
		if err != nil {
			log.Errorf("failed to parse config response")
			return
		}
		assets := make(map[uint32]*msgjson.Asset)
		for _, asset := range dexCfg.Assets {
			assets[asset.ID] = &asset
		}
		// Validate the markets so we don't have to check every time later.
		for _, bq := range dexCfg.Markets {
			_, ok := assets[bq[0]]
			if !ok {
				log.Errorf("%s reported a market with base asset %d, but did not provide the asset info.", uri, bq[0])
			}
			_, ok = assets[bq[1]]
			if !ok {
				log.Errorf("%s reported a market with quote asset %d, but did not provide the asset info.", uri, bq[1])
			}
		}
		// Create the dexConnection and add it to the map.
		dc := &dexConnection{
			conn:   conn,
			assets: assets,
			cfg:    dexCfg,
		}
		c.connMtx.Lock()
		c.conns[uri] = dc
		c.connMtx.Unlock()
		c.wg.Add(1)
		// Listen for incoming messages.
		go c.listen(dc)
	})
}

// handleReconnect is called when a WsConn indicates that a lost connection has
// been re-established.
func (c *Core) handleReconnect(uri string) {
	log.Infof("DEX at %s has reconnected", uri)
}

// listen monitors the DEX webscocket connection for server requests and
// notifications.
func (c *Core) listen(dc *dexConnection) {
	msgs := dc.conn.MessageSource()
	defer c.wg.Done()
out:
	for {
		select {
		case msg := <-msgs:
			switch msg.Route {
			// Requests
			case msgjson.MatchDataRoute:
				log.Info("match_data message received")
			case msgjson.MatchProofRoute:
				log.Info("match_proof message received")
			case msgjson.PreimageRoute:
				log.Info("preimage message received")
			case msgjson.MatchRoute:
				log.Info("match message received")
			case msgjson.AuditRoute:
				log.Info("audit message received")
			case msgjson.RedemptionRoute:
				log.Info("redemption message received")
			case msgjson.RevokeMatchRoute:
				log.Info("revoke_match message received")
			case msgjson.SuspensionRoute:
				log.Info("suspension message received")
			// Notifications
			case msgjson.BookOrderRoute:
				log.Info("book_order message received")
			case msgjson.EpochOrderRoute:
				log.Info("epoch_order message received")
			case msgjson.UnbookOrderRoute:
				log.Info("unbook_order message received")
			}
		case <-c.ctx.Done():
			break out
		}
	}
}
