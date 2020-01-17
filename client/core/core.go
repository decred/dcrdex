// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package core

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"sync"
	"time"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/client/comms"
	"decred.org/dcrdex/client/db"
	"decred.org/dcrdex/client/db/bolt"
	"decred.org/dcrdex/client/order"
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/encode"
	"decred.org/dcrdex/dex/encrypt"
	"decred.org/dcrdex/dex/msgjson"
	srvacct "decred.org/dcrdex/server/account"
	"github.com/decred/dcrd/dcrec/secp256k1/v2"
)

var (
	// log is a logger generated with the LogMaker provided with Config.
	log   dex.Logger
	unbip = dex.BipIDSymbol
)

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
	websocket
	assets   map[uint32]*dex.Asset
	cfg      *msgjson.ConfigResult
	acct     *account
	booksMtx sync.RWMutex
	books    map[string]*order.OrderBook
}

// coinWaiter is a message waiting to be stamped, signed, and sent once a
// specified coin has the requisite confirmations.
type coinWaiter struct {
	conn    *dexConnection
	coin    asset.Coin
	confs   uint32
	asset   *dex.Asset
	route   string
	privKey *secp256k1.PrivateKey
	req     msgjson.Stampable
	f       func(*msgjson.Message, error)
}

// Config is the configuration for the Core.
type Config struct {
	// DBPath is a filepath to use for the client database. If the database does
	// not already exist, it will be created.
	DBPath string
	// LogMaker is a logger for the core to use. Having the logger as an argument
	// enables creating custom loggers for use in a GUI interface.
	LoggerMaker *dex.LoggerMaker
	// Certs is a mapping of URL to filepaths of TLS Certificates for the server.
	// This is intended for accommodating self-signed certificates.
	Certs map[string]string
	// Net is the current network.
	Net dex.Network
}

// Core is the core client application.
type Core struct {
	ctx         context.Context
	wg          sync.WaitGroup
	cfg         *Config
	connMtx     sync.RWMutex
	conns       map[string]*dexConnection
	db          db.DB
	certs       map[string]string
	wallets     map[uint32]asset.Wallet
	walletMtx   sync.Mutex
	loggerMaker *dex.LoggerMaker
	net         dex.Network
	waiterMtx   sync.Mutex
	waiters     map[string]coinWaiter
}

// New is the constructor for a new Core.
func New(cfg *Config) (*Core, error) {
	log = cfg.LoggerMaker.NewLogger("CORE")
	db, err := bolt.NewDB(cfg.DBPath)
	if err != nil {
		return nil, fmt.Errorf("database initialization error: %v", err)
	}
	core := &Core{
		cfg:         cfg,
		db:          db,
		conns:       make(map[string]*dexConnection),
		wallets:     make(map[uint32]asset.Wallet),
		net:         cfg.Net,
		loggerMaker: cfg.LoggerMaker,
		waiters:     make(map[string]coinWaiter),
	}
	return core, nil
}

// Run runs the core. Satisfies the runner.Runner interface.
func (c *Core) Run(ctx context.Context) {
	log.Infof("started DEX client core")
	// Store the context as a field for now, since we will need to spawn new
	// DEX threads when new accounts are registered.
	c.ctx = ctx
	// Have one thread just wait on context cancellation, since if there are no
	// DEX accounts yet, there would be nothing else on the WaitGroup.
	c.initialize()
	<-ctx.Done()
	c.wg.Wait()
	log.Infof("DEX client core off")
}

// ListMarkets returns a list of known markets.
func (c *Core) ListMarkets() []*MarketInfo {
	c.connMtx.RLock()
	defer c.connMtx.RUnlock()
	infos := make([]*MarketInfo, 0, len(c.conns))
	for uri, dc := range c.conns {
		mi := &MarketInfo{DEX: uri}
		for _, mkt := range dc.cfg.Markets {
			base, quote := dc.assets[mkt.Base], dc.assets[mkt.Quote]
			mi.Markets = append(mi.Markets, Market{
				BaseID:          base.ID,
				BaseSymbol:      base.Symbol,
				QuoteID:         quote.ID,
				QuoteSymbol:     quote.Symbol,
				EpochLen:        mkt.EpochLen,
				StartEpoch:      mkt.StartEpoch,
				MarketBuyBuffer: mkt.MarketBuyBuffer,
			})
		}
		infos = append(infos, mi)
	}
	return infos
}

// wallet gets the wallet for the specified asset ID in a thread-safe way.
func (c *Core) wallet(assetID uint32) (asset.Wallet, bool) {
	c.walletMtx.Lock()
	defer c.walletMtx.Unlock()
	w, found := c.wallets[assetID]
	return w, found
}

// CreateWallet creates a new exchange wallet.
func (c *Core) CreateWallet(form *WalletForm) error {
	dbWallet := &db.Wallet{
		AssetID: form.AssetID,
		Account: form.Account,
		INIPath: form.INIPath,
	}
	_, exists := c.wallet(form.AssetID)
	if exists {
		return fmt.Errorf("%s wallet", unbip(dbWallet.AssetID))
	}
	wallet := &Wallet{AssetID: form.AssetID}

	walletCfg := &asset.WalletConfig{
		Account: dbWallet.Account,
		INIPath: dbWallet.INIPath,
		TipChange: func(err error) {
			c.tipChange(form.AssetID, err)
		},
	}

	logger := c.loggerMaker.SubLogger("CORE", unbip(dbWallet.AssetID))
	w, err := asset.Setup(dbWallet.AssetID, walletCfg, logger, c.net)
	if err != nil {
		return fmt.Errorf("error creating wallet: %v", err)
	}
	wallet.Wallet = w
	wallet.waiter = dex.NewStartStopWaiter(w)
	wallet.waiter.Start(c.ctx)

	// Store the wallet in the database.
	err = c.db.UpdateWallet(dbWallet)
	if err != nil {
		wallet.waiter.Stop()
		return fmt.Errorf("error storing wallet credentials: %v", err)
	}

	c.walletMtx.Lock()
	c.wallets[form.AssetID] = wallet
	c.walletMtx.Unlock()

	return nil
}

// Register registers an account with a new DEX. Register will block for the
// entire process, including waiting for confirmations.
func (c *Core) Register(form *Registration) error {
	// For now, asset ID is hard-coded to Decred for registration fees.
	assetID, _ := dex.BipSymbolID("dcr")
	if form.DEX == "" {
		return fmt.Errorf("no dex url specified")
	}
	wallet, found := c.wallet(assetID)
	if !found {
		return fmt.Errorf("no wallet found for %s", unbip(assetID))
	}

	// Make sure the account doesn't already exist.
	ai, err := c.db.Account(form.DEX)
	if err == nil {
		return fmt.Errorf("account already exists for %s", form.DEX)
	}

	// Get a connection to the dex.
	ai = &db.AccountInfo{
		URL: form.DEX,
	}
	c.connMtx.RLock()
	dc := c.conns[form.DEX]
	c.connMtx.RUnlock()
	if dc == nil {
		c := c.addDex(ai)
		dc = <-c
		if dc == nil {
			return fmt.Errorf("failed to connect to DEX at %s", form.DEX)
		}
	}

	regAsset, found := dc.assets[assetID]
	if !found {
		return fmt.Errorf("asset information not found: %v", err)
	}

	// Create a new private key for the account.
	privKey, err := secp256k1.GeneratePrivateKey()
	if err != nil {
		return fmt.Errorf("error creating wallet key: %v", err)
	}

	// Create an encryption key.
	pw := []byte(form.Password)
	secretKey, err := encrypt.NewSecretKey(&pw)
	if err != nil {
		return fmt.Errorf("error creating encryption key: %v", err)
	}

	// Encrypt the private key.
	encPW, err := secretKey.Encrypt(privKey.Serialize())
	if err != nil {
		return fmt.Errorf("error encrypting private key: %v", err)
	}

	// The account ID is generated from the public key.
	pubKey := privKey.PubKey()
	acctID := srvacct.NewID(pubKey.SerializeCompressed())

	// Prepare and sign the registration payload.
	dexReg := &msgjson.Register{
		PubKey: pubKey.Serialize(),
		Time:   encode.UnixMilliU(time.Now()),
	}
	err = sign(privKey, dexReg)
	if err != nil {
		return err
	}

	// Create and send the the request, grabbing the result in the callback.
	regMsg, err := msgjson.NewRequest(dc.NextID(), msgjson.RegisterRoute, dexReg)
	if err != nil {
		return fmt.Errorf("error encoding message: %v", err)
	}
	regRes := new(msgjson.RegisterResult)
	var wg sync.WaitGroup
	wg.Add(1)
	err = dc.Request(regMsg, func(msg *msgjson.Message) {
		defer wg.Done()
		var resp *msgjson.ResponsePayload
		resp, err = msg.Response()
		if err != nil {
			return
		}
		if resp.Error != nil {
			err = fmt.Errorf("'register' request error: %d: %s", resp.Error.Code, resp.Error.Message)
			return
		}
		err = json.Unmarshal(resp.Result, regRes)
		if err != nil {
			err = fmt.Errorf("Error unmarshaling 'register' response: %v", err)
			return
		}
	})
	if err != nil {
		return fmt.Errorf("'register' requst error: %v", err)
	}
	wg.Wait()
	if err != nil {
		return err
	}

	// Check the server's signature.
	msg, err := regRes.Serialize()
	if err != nil {
		return fmt.Errorf("error serializing RegisterResult for signature check: %v", err)
	}

	dexPubKey, err := checkSigS256(msg, regRes.DEXPubKey, regRes.Sig)
	if err != nil {
		return fmt.Errorf("DEX signature validation error: %v", err)
	}

	// Check that the fee is non-zero.
	if regRes.Fee == 0 {
		return fmt.Errorf("zero registration fees not supported")
	}
	// Pay the registration fee.
	coin, err := wallet.PayFee(regRes.Fee, regRes.Address)
	if err != nil {
		return fmt.Errorf("error paying registration fee: %v", err)
	}

	// Set the dexConnection account fields.
	dc.acct.dexPubKey = dexPubKey
	dc.acct.encKey = encPW
	dc.acct.feeCoin = coin.ID()
	dc.acct.privKey = privKey

	// Set the db.AccountInfo fields and save the account info.
	ai.EncKey = encPW
	ai.DEXPubKey = dexPubKey
	ai.FeeCoin = coin.ID()
	err = c.db.CreateAccount(ai)
	if err != nil {
		log.Errorf("error saving account: %v", err)
		// Don't abandon registration. The fee is already paid.
	}

	// Notify the server of the fee coin once there are enough confirmations.
	res := make(chan error, 1)
	req := &msgjson.NotifyFee{
		AccountID: acctID[:],
		CoinID:    coin.ID(),
	}
	// We'll need this to validate the server's acknowledgement.
	reqB, err := req.Serialize()
	if err != nil {
		log.Warnf("fee paid with coin %x, but unable to serialize notifyfee request so server signature cannot be verified")
	}
	c.waiterMtx.Lock()
	c.waiters[form.DEX] = coinWaiter{
		conn:    dc,
		asset:   regAsset,
		coin:    coin,
		confs:   regAsset.FundConf,
		route:   msgjson.NotifyFeeRoute,
		privKey: privKey,
		req:     req,
		f: func(msg *msgjson.Message, waiterErr error) {
			var err error
			defer func() { res <- err }()
			if waiterErr != nil {
				err = waiterErr
				return
			}
			resp, err := msg.Response()
			if err != nil {
				err = fmt.Errorf("error decoding response: %v", err)
				return
			}
			if resp.Error != nil {
				err = fmt.Errorf("notifyfee error: %d:%s", resp.Error.Code, resp.Error.Message)
				return
			}
			ack := new(msgjson.Acknowledgement)
			err = json.Unmarshal(resp.Result, ack)
			if err != nil {
				err = fmt.Errorf("notify fee result json decode error: %v", err)
				return
			}
			// If there was a serialization error, validation is skipped. A warning
			// message was already logged.
			if len(reqB) > 0 {
				// redefining err here, since these errors won't be sent over the
				// response channel.
				sig, err := hex.DecodeString(ack.Sig)
				if err != nil {
					log.Warnf("account was registered, but server's signature could not be decoded: %v", err)
					return
				}
				_, err = checkSigS256(reqB, regRes.DEXPubKey, sig)
				if err != nil {
					log.Warnf("account was registered, but DEX signature could not be verified: %v", err)
				}
			}
		},
	}
	c.waiterMtx.Unlock()
	return <-res
}

func (c *Core) Login(dex, pw string) error {
	return nil
}

func (c *Core) Sync(dex string, base, quote uint32) (chan *BookUpdate, error) {
	return make(chan *BookUpdate), nil
}

func (c *Core) Book(dex string, base, quote uint32) *OrderBook {
	return nil
}

func (c *Core) Unsync(dex string, base, quote uint32) {}

func (c *Core) Balance(uint32) (uint64, error) {
	return 0, nil
}

// initialize pulls the known DEX URLs from the database and attempts to
// connect and retreive the DEX configuration.
func (c *Core) initialize() {
	accts, err := c.db.Accounts()
	if err != nil {
		log.Errorf("Error retreiving accounts from database: %v", err)
	}
	for _, acct := range accts {
		a := acct
		go func() {
			c.addDex(a)
		}()
	}
	if len(accts) > 0 {
		c.connMtx.RLock()
		log.Infof("Successfully connected to %d out of %d "+
			"DEX servers", len(c.conns), len(accts))
		c.connMtx.RUnlock()
	}
}

// addDex adds a dexConnection to the conns map if a connection can be made
// and the DEX configuration is successfully retrieved. The connection is
// unauthenticated until the `connect` request is sent and accepted by the
// server.
func (c *Core) addDex(acct *db.AccountInfo) <-chan *dexConnection {
	// Get the host from the DEX URL.
	connChan := make(chan *dexConnection, 1)
	makeErr := func() <-chan *dexConnection {
		connChan <- nil
		return connChan
	}
	uri := acct.URL
	parsedURL, err := url.Parse(uri)
	if err != nil {
		log.Errorf("error parsing account URL %s: %v", uri, err)
		return makeErr()
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
		return makeErr()
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
			makeErr()
			return
		}
		var dexCfg *msgjson.ConfigResult
		err = json.Unmarshal(resp.Result, dexCfg)
		if err != nil {
			log.Errorf("failed to parse config response")
			makeErr()
			return
		}
		assets := make(map[uint32]*dex.Asset)
		for i := range dexCfg.Assets {
			asset := &dexCfg.Assets[i]
			assets[asset.ID] = convertAssetInfo(asset)
		}
		// Validate the markets so we don't have to check every time later.
		for _, mkt := range dexCfg.Markets {
			_, ok := assets[mkt.Base]
			if !ok {
				log.Errorf("%s reported a market with base asset %d, "+
					"but did not provide the asset info.", uri, mkt.Base)
			}
			_, ok = assets[mkt.Quote]
			if !ok {
				log.Errorf("%s reported a market with quote asset %d, "+
					"but did not provide the asset info.", uri, mkt.Quote)
			}
		}
		// Create the dexConnection and add it to the map.
		dc := &dexConnection{
			websocket: conn,
			assets:    assets,
			cfg:       dexCfg,
			books:     make(map[string]*order.OrderBook),
			acct: &account{
				url:       acct.URL,
				encKey:    acct.EncKey,
				dexPubKey: acct.DEXPubKey,
				feeCoin:   acct.FeeCoin,
			},
		}
		c.connMtx.Lock()
		c.conns[uri] = dc
		c.connMtx.Unlock()
		connChan <- dc
		c.wg.Add(1)
		// Listen for incoming messages.
		go c.listen(dc)
	})
	return connChan
}

// handleReconnect is called when a WsConn indicates that a lost connection has
// been re-established.
func (c *Core) handleReconnect(uri string) {
	log.Infof("DEX at %s has reconnected", uri)
}

// handleOrderBookMsg is called when an orderbook response is received.
func (c *Core) handleOrderBookMsg(dc *dexConnection, msg *msgjson.Message) error {
	resp, err := msg.Response()
	if err != nil {
		return err
	}

	var snapshot msgjson.OrderBook
	err = json.Unmarshal(resp.Result, &snapshot)
	if err != nil {
		return fmt.Errorf("order book unmarshal error: %v", err)
	}

	if snapshot.MarketID == "" {
		return fmt.Errorf("snapshot market id cannot be an empty string")
	}

	ob := order.NewOrderBook()
	err = ob.Sync(&snapshot)
	if err != nil {
		return err
	}

	dc.booksMtx.Lock()
	dc.books[snapshot.MarketID] = ob
	dc.booksMtx.Unlock()

	return nil
}

// handleBookOrderMsg is called when a book_order notification is received.
func (c *Core) handleBookOrderMsg(dc *dexConnection, msg *msgjson.Message) error {
	var note msgjson.BookOrderNote
	err := json.Unmarshal(msg.Payload, &note)
	if err != nil {
		return fmt.Errorf("book order note unmarshal error: %v", err)
	}

	dc.booksMtx.Lock()
	ob, ok := dc.books[note.MarketID]
	dc.booksMtx.Unlock()
	if !ok {
		return fmt.Errorf("no order book found with market id '%v'",
			note.MarketID)
	}

	return ob.Book(&note)
}

// handleUnbookOrderMsg is called when an unbook_order notification is
// received.
func (c *Core) handleUnbookOrderMsg(dc *dexConnection, msg *msgjson.Message) error {
	var note msgjson.UnbookOrderNote
	err := json.Unmarshal(msg.Payload, &note)
	if err != nil {
		return fmt.Errorf("unbook order note unmarshal error: %v", err)
	}

	dc.booksMtx.Lock()
	defer dc.booksMtx.Unlock()

	ob, ok := dc.books[note.MarketID]
	if !ok {
		return fmt.Errorf("no order book found with market id %q",
			note.MarketID)
	}

	return ob.Unbook(&note)
}

// listen monitors the DEX websocket connection for server requests and
// notifications.
func (c *Core) listen(dc *dexConnection) {
	msgs := dc.MessageSource()
	defer c.wg.Done()
out:
	for {
		select {
		case msg := <-msgs:
			switch msg.Type {
			case msgjson.Request:
				switch msg.Route {
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
				default:
					log.Errorf("request with unknown route (%v) received",
						msg.Route)
				}

			case msgjson.Notification:
				var err error
				switch msg.Route {
				case msgjson.BookOrderRoute:
					err = c.handleBookOrderMsg(dc, msg)
				case msgjson.EpochOrderRoute:
					log.Info("epoch_order message received")
				case msgjson.UnbookOrderRoute:
					err = c.handleUnbookOrderMsg(dc, msg)
				default:
					err = fmt.Errorf("notification with unknown route "+
						"(%v) received", msg.Route)
				}
				if err != nil {
					log.Error(err)
				}

			case msgjson.Response:
				var err error
				switch msg.Route {
				case msgjson.OrderBookRoute:
					err = c.handleOrderBookMsg(dc, msg)
				default:
					err = fmt.Errorf("response mesage with unknown route "+
						"(%v) received", msg.Route)
				}
				if err != nil {
					log.Error(err)
				}

			default:
				log.Errorf("invalid message type %d from MessageSource", msg.Type)
			}
		case <-c.ctx.Done():
			break out
		}
	}
}

// tipChange is called by a wallet backend when the tip block changes.
func (c *Core) tipChange(assetID uint32, nodeErr error) {
	if nodeErr != nil {
		log.Errorf("%s wallet is reporting a failed state: %v", nodeErr)
		return
	}
	c.waiterMtx.Lock()
	defer c.waiterMtx.Unlock()
	for _, w := range c.waiters {
		waiter := w
		if waiter.asset.ID != assetID {
			continue
		}
		go func() {
			confs, err := waiter.coin.Confirmations()
			if err != nil {
				waiter.f(nil, fmt.Errorf("Error getting confirmations for %x: %v", waiter.coin.ID(), err))
				return
			}
			if confs >= waiter.confs {
				// Sign the request and send it.
				req := waiter.req
				err := stamp(waiter.privKey, req)
				if err != nil {
					waiter.f(nil, err)
					return
				}
				msg, err := msgjson.NewRequest(waiter.conn.NextID(), waiter.route, req)
				if err != nil {
					waiter.f(nil, fmt.Errorf("failed to crate notifyfee request: %v", err))
					return
				}
				waiter.conn.Request(msg, func(msg *msgjson.Message) {
					waiter.f(msg, nil)
				})

			}
		}()
	}
}

// convertAssetInfo converts from a *msgjson.Asset to the nearly identical
// *dex.Asset.
func convertAssetInfo(asset *msgjson.Asset) *dex.Asset {
	return &dex.Asset{
		ID:       asset.ID,
		Symbol:   asset.Symbol,
		LotSize:  asset.LotSize,
		RateStep: asset.RateStep,
		FeeRate:  asset.FeeRate,
		SwapSize: asset.SwapSize,
		SwapConf: uint32(asset.SwapConf),
		FundConf: uint32(asset.FundConf),
	}
}

// checkSigS256 checks that the message's signature was created with the
// private key for the provided secp256k1 public key.
func checkSigS256(msg, pkBytes, sigBytes []byte) (*secp256k1.PublicKey, error) {
	pubKey, err := secp256k1.ParsePubKey(pkBytes)
	if err != nil {
		return nil, fmt.Errorf("error decoding secp256k1 PublicKey from bytes: %v", err)
	}
	signature, err := secp256k1.ParseDERSignature(sigBytes)
	if err != nil {
		return nil, fmt.Errorf("error decoding secp256k1 Signature from bytes: %v", err)
	}
	if !signature.Verify(msg, pubKey) {
		return nil, fmt.Errorf("secp256k1 signature verification failed")
	}
	return pubKey, nil
}

// sign signs the msgjson.Signable with the provided private key.
func sign(privKey *secp256k1.PrivateKey, payload msgjson.Signable) error {
	sigMsg, err := payload.Serialize()
	if err != nil {
		return fmt.Errorf("Error serializing request: %v", err)
	}
	sig, err := privKey.Sign(sigMsg)
	if err != nil {
		return fmt.Errorf("message signing error: %v", err)
	}
	payload.SetSig(sig.Serialize())
	return nil
}

// stamp adds a timestamp and signature to the msgjson.Stampable.
func stamp(privKey *secp256k1.PrivateKey, payload msgjson.Stampable) error {
	payload.Stamp(encode.UnixMilliU(time.Now()), 0, 0)
	return sign(privKey, payload)
}
