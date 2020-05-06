// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package core

import (
	"bytes"
	"context"
	"encoding/hex"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/client/comms"
	"decred.org/dcrdex/client/db"
	"decred.org/dcrdex/client/db/bolt"
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/calc"
	"decred.org/dcrdex/dex/config"
	"decred.org/dcrdex/dex/encode"
	"decred.org/dcrdex/dex/encrypt"
	"decred.org/dcrdex/dex/msgjson"
	"decred.org/dcrdex/dex/order"
	"decred.org/dcrdex/dex/wait"
	"github.com/decred/dcrd/dcrec/secp256k1/v2"
)

const (
	keyParamsKey     = "keyParams"
	conversionFactor = 1e8
)

var (
	// log is a logger generated with the LogMaker provided with Config.
	unbip          = dex.BipIDSymbol
	aYear          = time.Hour * 24 * 365
	requestTimeout = 10 * time.Second
	// The coin waiters will query for transaction data every recheckInterval.
	recheckInterval = time.Second * 5
)

// dexConnection is the websocket connection and the DEX configuration.
type dexConnection struct {
	comms.WsConn
	connMaster *dex.ConnectionMaster
	assets     map[uint32]*dex.Asset
	cfg        *msgjson.ConfigResult
	acct       *dexAccount
	notify     func(Notification)

	booksMtx sync.RWMutex
	books    map[string]*bookie

	marketMtx sync.RWMutex
	marketMap map[string]*Market

	tradeMtx sync.RWMutex
	trades   map[order.OrderID]*trackedTrade
}

// refreshMarkets rebuilds, saves, and returns the market map. The map itself
// should be treated as read only. A new map is constructed and is assigned to
// dc.marketMap under lock, and can be safely accessed with
// dexConnection.markets. refreshMarkets is used when a change to the status of
// a market or the user's orders on a market has changed.
func (dc *dexConnection) refreshMarkets() map[string]*Market {
	marketMap := make(map[string]*Market, len(dc.cfg.Markets))
	for _, mkt := range dc.cfg.Markets {
		// The presence of the asset for every market was already verified when the
		// dexConnection was created in connectDEX.
		base, quote := dc.assets[mkt.Base], dc.assets[mkt.Quote]
		market := &Market{
			Name:            mkt.Name,
			BaseID:          base.ID,
			BaseSymbol:      base.Symbol,
			QuoteID:         quote.ID,
			QuoteSymbol:     quote.Symbol,
			EpochLen:        mkt.EpochLen,
			StartEpoch:      mkt.StartEpoch,
			MarketBuyBuffer: mkt.MarketBuyBuffer,
		}
		mid := market.marketName()
		dc.tradeMtx.RLock()
		for _, trade := range dc.trades {
			if trade.mktID == mid {
				corder, _ := trade.coreOrder()
				market.Orders = append(market.Orders, corder)
			}
		}
		dc.tradeMtx.RUnlock()
		marketMap[market.marketName()] = market
	}
	dc.marketMtx.Lock()
	dc.marketMap = marketMap
	dc.marketMtx.Unlock()
	return marketMap
}

// markets returns the current market map.
func (dc *dexConnection) markets() map[string]*Market {
	dc.marketMtx.RLock()
	defer dc.marketMtx.RUnlock()
	return dc.marketMap
}

// hasOrders checks whether there are any open orders or negotiating matches for
// the specified asset.
func (dc *dexConnection) hasOrders(assetID uint32) bool {
	dc.tradeMtx.RLock()
	defer dc.tradeMtx.RUnlock()
	for _, trade := range dc.trades {
		if trade.Base() == assetID || trade.Quote() == assetID {
			return true
		}
	}
	return false
}

// findOrder returns the tracker and preimage for an order ID, and a boolean
// indicating whether this is a cancel order.
func (dc *dexConnection) findOrder(oid order.OrderID) (tracker *trackedTrade, preImg order.Preimage, isCancel bool) {
	dc.tradeMtx.RLock()
	defer dc.tradeMtx.RUnlock()
	for _, tracker := range dc.trades {
		if tracker.ID() == oid {
			return tracker, tracker.preImg, false
		} else if tracker.cancel != nil && tracker.cancel.ID() == oid {
			return tracker, tracker.cancel.preImg, true
		}
	}
	return
}

// signAndRequest signs and sends the request, unmarshaling the response into
// the provided interface.
func (dc *dexConnection) signAndRequest(signable msgjson.Signable, route string, result interface{}) error {
	err := sign(dc.acct.privKey, signable)
	if err != nil {
		return fmt.Errorf("error signing %s message: %v", route, err)
	}
	req, err := msgjson.NewRequest(dc.NextID(), route, signable)
	if err != nil {
		return fmt.Errorf("error encoding %s request: %v", route, err)
	}
	errChan := make(chan error, 1)
	err = dc.Request(req, func(msg *msgjson.Message) {
		errChan <- msg.UnmarshalResult(result)
	})
	if err != nil {
		return err
	}
	err = extractError(errChan, requestTimeout, route)
	if err != nil {
		return err
	}
	return nil
}

// ack sends an Acknowledgement for a match-related request.
func (dc *dexConnection) ack(msgID uint64, matchID order.MatchID, signable msgjson.Signable) error {
	ack := &msgjson.Acknowledgement{
		MatchID: matchID[:],
	}
	sigMsg, err := signable.Serialize()
	if err != nil {
		return fmt.Errorf("Serialize error - %v", err)
	}
	ack.Sig, err = dc.acct.sign(sigMsg)
	if err != nil {
		return fmt.Errorf("sign error - %v", err)
	}
	msg, err := msgjson.NewResponse(msgID, ack, nil)
	if err != nil {
		return fmt.Errorf("NewResponse error - %v", err)
	}
	err = dc.Send(msg)
	if err != nil {
		return fmt.Errorf("Send error - %v", err)
	}
	return nil
}

// serverMatches are an intermediate structure used by the dexConnection to
// sort incoming match notifications.
type serverMatches struct {
	tracker    *trackedTrade
	msgMatches []*msgjson.Match
	cancel     *msgjson.Match
}

// parseMatches sorts the list of matches and associates them with a trade.
func (dc *dexConnection) parseMatches(msgMatches []*msgjson.Match, checkSigs bool) (map[order.OrderID]*serverMatches, []msgjson.Acknowledgement, error) {
	var acks []msgjson.Acknowledgement
	matches := make(map[order.OrderID]*serverMatches)
	var errs []string
	for _, msgMatch := range msgMatches {
		var oid order.OrderID
		copy(oid[:], msgMatch.OrderID)
		tracker, _, isCancel := dc.findOrder(oid)
		if tracker == nil {
			errs = append(errs, "order "+oid.String()+" not found")
			continue
		}
		sigMsg, err := msgMatch.Serialize()
		if err != nil {
			errs = append(errs, err.Error())
			continue
		}
		if checkSigs {
			err = dc.acct.checkSig(sigMsg, msgMatch.Sig)
			if err != nil {
				return nil, nil, fmt.Errorf("parseMatches: %v", err)
			}
		}
		sig, err := dc.acct.sign(sigMsg)
		if err != nil {
			errs = append(errs, err.Error())
			continue
		}
		acks = append(acks, msgjson.Acknowledgement{
			MatchID: msgMatch.MatchID,
			Sig:     sig,
		})

		trackerID := tracker.ID()
		match := matches[trackerID]
		if match == nil {
			match = &serverMatches{
				tracker: tracker,
			}
			matches[trackerID] = match
		}
		if isCancel {
			match.cancel = msgMatch
		} else {
			match.msgMatches = append(match.msgMatches, msgMatch)
		}
	}
	var err error
	if len(errs) > 0 {
		err = fmt.Errorf("match errors: %s", strings.Join(errs, ", "))
	}
	return matches, acks, err
}

// runMatches runs the sorted matches returned from parseMatches.
func (dc *dexConnection) runMatches(matches map[order.OrderID]*serverMatches) error {
	for _, match := range matches {
		tracker := match.tracker
		if len(match.msgMatches) > 0 {
			err := tracker.negotiate(match.msgMatches)
			if err != nil {
				return err
			}
		}
		if match.cancel != nil {
			err := tracker.processCancelMatch(match.cancel)
			if err != nil {
				return err
			}
		}
	}
	dc.refreshMarkets()
	return nil
}

// compareServerMatches resolves the matches reported by the server in the
// 'connect' response against those marked incomplete in the matchTracker map
// for each serverMatch.
// Reported matches with missing trackers are already checked by parseMatches,
// but we also must check for incomplete matches that the server is not
// reporting.
//
// DRAFT NOTE: Right now, the matches are just checked and notifications sent,
// but it may be a good  place to trigger a FindRedemption if the conditions
// warrant.
func (dc *dexConnection) compareServerMatches(matches map[order.OrderID]*serverMatches) {
	for _, match := range matches {
		// readConnectMatches sends notifications for any problems encountered.
		match.tracker.readConnectMatches(match.msgMatches)
	}
}

// tickAsset checks open matches related to a specific asset for needed action.
func (dc *dexConnection) tickAsset(assetID uint32) {
	dc.tradeMtx.RLock()
	defer dc.tradeMtx.RUnlock()
	for _, trade := range dc.trades {
		if trade.Base() == assetID || trade.Quote() == assetID {
			err := trade.tick()
			if err != nil {
				log.Errorf("%s tick error: %v", dc.acct.url, err)
			}
		}
	}
}

// blockWaiter is a message waiting to be stamped, signed, and sent once a
// specified coin has the requisite confirmations. The blockWaiter is similar to
// dcrdex/server/blockWaiter.Waiter, but is different enough to warrant a
// separate type.
type blockWaiter struct {
	assetID uint32
	trigger func() (bool, error)
	action  func(error)
}

// Config is the configuration for the Core.
type Config struct {
	// DBPath is a filepath to use for the client database. If the database does
	// not already exist, it will be created.
	DBPath string
	// Net is the current network.
	Net dex.Network
}

// Core is the core client application. Core manages DEX connections, wallets,
// database access, match negotiation and more.
type Core struct {
	ctx           context.Context
	wg            sync.WaitGroup
	cfg           *Config
	pendingTimer  *time.Timer
	pendingReg    *dexConnection
	db            db.DB
	net           dex.Network
	wsConstructor func(*comms.WsCfg) (comms.WsConn, error)
	newCrypter    func([]byte) encrypt.Crypter
	reCrypter     func([]byte, []byte) (encrypt.Crypter, error)
	latencyQ      *wait.TickerQueue

	connMtx sync.RWMutex
	conns   map[string]*dexConnection

	walletMtx sync.RWMutex
	wallets   map[uint32]*xcWallet

	waiterMtx    sync.Mutex
	blockWaiters map[uint64]*blockWaiter

	userMtx sync.RWMutex
	user    *User

	noteMtx   sync.RWMutex
	noteChans []chan Notification

	epochMtx sync.RWMutex
	epoch    uint64
}

// New is the constructor for a new Core.
func New(cfg *Config) (*Core, error) {
	db, err := bolt.NewDB(cfg.DBPath)
	if err != nil {
		return nil, fmt.Errorf("database initialization error: %v", err)
	}
	core := &Core{
		cfg:          cfg,
		db:           db,
		conns:        make(map[string]*dexConnection),
		wallets:      make(map[uint32]*xcWallet),
		net:          cfg.Net,
		blockWaiters: make(map[uint64]*blockWaiter),
		// Allowing to change the constructor makes testing a lot easier.
		wsConstructor: comms.NewWsConn,
		newCrypter:    encrypt.NewCrypter,
		reCrypter:     encrypt.Deserialize,
		latencyQ:      wait.NewTickerQueue(recheckInterval),
	}

	// Populate the initial user data. User won't include any DEX info yet, as
	// those are retrieved when Run is called and the core connects to the DEXes.
	core.refreshUser()
	log.Debugf("new client core created")
	return core, nil
}

// Run runs the core. Satisfies the runner.Runner interface.
func (c *Core) Run(ctx context.Context) {
	log.Infof("started DEX client core")
	// Store the context as a field, since we will need to spawn new DEX threads
	// when new accounts are registered.
	c.ctx = ctx
	c.initialize()
	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		c.db.Run(ctx)
	}()
	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		c.latencyQ.Run(ctx)
	}()
	c.wg.Wait()
	log.Infof("DEX client core off")
}

// Exchanges returns a map of Exchange keyed by URI, including a list of markets
// and their orders.
func (c *Core) Exchanges() map[string]*Exchange {
	c.connMtx.RLock()
	defer c.connMtx.RUnlock()
	infos := make(map[string]*Exchange, len(c.conns))
	for uri, dc := range c.conns {
		infos[uri] = &Exchange{
			URL:        uri,
			Markets:    dc.markets(),
			Assets:     dc.assets,
			FeePending: dc.acct.feePending(),
		}
	}
	return infos
}

// wallet gets the wallet for the specified asset ID in a thread-safe way.
func (c *Core) wallet(assetID uint32) (*xcWallet, bool) {
	c.walletMtx.RLock()
	defer c.walletMtx.RUnlock()
	w, found := c.wallets[assetID]
	return w, found
}

// encryptionKey retrieves the application encryption key. The key itself is
// encrypted using an encryption key derived from the user's password.
func (c *Core) encryptionKey(pw []byte) (encrypt.Crypter, error) {
	keyParams, err := c.db.Get(keyParamsKey)
	if err != nil {
		return nil, fmt.Errorf("key retrieval error: %v", err)
	}
	crypter, err := c.reCrypter(pw, keyParams)
	if err != nil {
		return nil, fmt.Errorf("encryption key deserialization error: %v", err)
	}
	return crypter, nil
}

// connectedWallet fetches a wallet and will connect the wallet if it is not
// already connected.
func (c *Core) connectedWallet(assetID uint32) (*xcWallet, error) {
	wallet, exists := c.wallet(assetID)
	if !exists {
		return nil, fmt.Errorf("no wallet found for %d -> %s", assetID, unbip(assetID))
	}
	if !wallet.connected() {
		log.Infof("connecting wallet for %s", unbip(assetID))
		err := wallet.Connect(c.ctx)
		if err != nil {
			return nil, fmt.Errorf("Connect error: %v", err)
		}
		// If first connecting the wallet, try to get the balance. Ignore errors
		// here with the assumption that some wallets may not reveal balance until
		// unlocked.
		balance, _, err := wallet.Balance(0)
		if err == nil {
			wallet.setBalance(balance)
		}
	}
	return wallet, nil
}

// Wallets creates a slice of WalletState for all known wallets.
func (c *Core) Wallets() []*WalletState {
	c.walletMtx.RLock()
	defer c.walletMtx.RUnlock()
	state := make([]*WalletState, 0, len(c.wallets))
	for _, wallet := range c.wallets {
		state = append(state, wallet.state())
	}
	return state
}

// SupportedAssets returns a list of asset information for supported assets that
// may or may not have a wallet yet.
func (c *Core) SupportedAssets() map[uint32]*SupportedAsset {
	supported := asset.Assets()
	assets := make(map[uint32]*SupportedAsset, len(supported))
	c.walletMtx.RLock()
	defer c.walletMtx.RUnlock()
	for assetID, asset := range supported {
		var wallet *WalletState
		w, found := c.wallets[assetID]
		if found {
			wallet = w.state()
		}
		assets[assetID] = &SupportedAsset{
			ID:     assetID,
			Symbol: asset.Symbol,
			Wallet: wallet,
			Info:   asset.Info,
		}
	}
	return assets
}

// User is a thread-safe getter for the User.
func (c *Core) User() *User {
	c.userMtx.RLock()
	defer c.userMtx.RUnlock()
	return c.user
}

// refreshUser is a thread-safe way to update the current User. This method
// should be called after adding wallets and DEXes.
func (c *Core) refreshUser() {
	initialized, err := c.IsInitialized()
	if err != nil {
		log.Errorf("refreshUser: error checking if app is initialized: %v", err)
	}
	u := &User{
		Assets:      c.SupportedAssets(),
		Exchanges:   c.Exchanges(),
		Initialized: initialized,
	}
	c.userMtx.Lock()
	c.user = u
	c.userMtx.Unlock()
}

// CreateWallet creates a new exchange wallet.
func (c *Core) CreateWallet(appPW, walletPW []byte, form *WalletForm) error {
	assetID := form.AssetID
	symbol := unbip(assetID)
	_, exists := c.wallet(assetID)
	if exists {
		return fmt.Errorf("%s wallet already exists", symbol)
	}

	crypter, err := c.encryptionKey(appPW)
	if err != nil {
		return err
	}
	encPW, err := crypter.Encrypt(walletPW)
	if err != nil {
		return fmt.Errorf("wallet password encryption error: %v", err)
	}

	if form.INIPath == "" {
		form.INIPath, err = asset.DefaultConfigPath(assetID)
		if err != nil {
			return fmt.Errorf("cannot use default wallet config path: %v", err)
		}
	}

	settings, err := config.Parse(form.INIPath)
	if err != nil {
		return fmt.Errorf("error parsing config file: %v", err)
	}

	dbWallet := &db.Wallet{
		AssetID:     assetID,
		Account:     form.Account,
		Settings:    settings,
		EncryptedPW: encPW,
	}

	wallet, err := c.loadWallet(dbWallet)
	if err != nil {
		return fmt.Errorf("error loading wallet for %d -> %s: %v", assetID, symbol, err)
	}

	err = wallet.Connect(c.ctx)
	if err != nil {
		return fmt.Errorf("Error connecting wallet: %v", err)
	}

	initErr := func(s string, a ...interface{}) error {
		wallet.Disconnect()
		return fmt.Errorf(s, a...)
	}

	err = wallet.Unlock(string(walletPW), aYear)
	if err != nil {
		return initErr("%s wallet authentication error: %v", symbol, err)
	}

	var lockedAmt uint64
	dbWallet.Balance, lockedAmt, err = wallet.Balance(0)
	if err != nil {
		return initErr("error getting balance for %s: %v", symbol, err)
	}
	wallet.setBalance(dbWallet.Balance)

	dbWallet.Address, err = wallet.Address()
	if err != nil {
		return initErr("error getting deposit address for %s: %v", symbol, err)
	}
	wallet.setAddress(dbWallet.Address)

	// Store the wallet in the database.
	err = c.db.UpdateWallet(dbWallet)
	if err != nil {
		return initErr("error storing wallet credentials: %v", err)
	}

	log.Infof("Created %s wallet. Account %q balance available = %d / "+
		"locked = %d, Deposit address = %s",
		symbol, form.Account, dbWallet.Balance, lockedAmt, dbWallet.Address)

	// The wallet has been successfully created. Store it.
	c.walletMtx.Lock()
	c.wallets[assetID] = wallet
	c.walletMtx.Unlock()

	c.refreshUser()
	return nil
}

// loadWallet uses the data from the database to construct a new exchange
// wallet. The returned wallet is running but not connected.
func (c *Core) loadWallet(dbWallet *db.Wallet) (*xcWallet, error) {
	wallet := &xcWallet{
		Account:   dbWallet.Account,
		AssetID:   dbWallet.AssetID,
		balance:   dbWallet.Balance,
		balUpdate: dbWallet.BalUpdate,
		encPW:     dbWallet.EncryptedPW,
		address:   dbWallet.Address,
	}
	walletCfg := &asset.WalletConfig{
		Account:  dbWallet.Account,
		Settings: dbWallet.Settings,
		TipChange: func(err error) {
			c.tipChange(dbWallet.AssetID, err)
		},
	}
	logger := loggerMaker.SubLogger("CORE", unbip(dbWallet.AssetID))
	w, err := asset.Setup(dbWallet.AssetID, walletCfg, logger, c.net)
	if err != nil {
		return nil, fmt.Errorf("error creating wallet: %v", err)
	}
	wallet.Wallet = w
	wallet.connector = dex.NewConnectionMaster(w)
	return wallet, nil
}

// WalletState returns the *WalletState for the asset ID.
func (c *Core) WalletState(assetID uint32) *WalletState {
	c.walletMtx.Lock()
	defer c.walletMtx.Unlock()
	wallet, has := c.wallets[assetID]
	if !has {
		log.Tracef("wallet status requested for unknown asset %d -> %s", assetID, unbip(assetID))
		return nil
	}
	return wallet.state()
}

// OpenWallet opens (unlocks) the wallet for use.
func (c *Core) OpenWallet(assetID uint32, appPW []byte) error {
	crypter, err := c.encryptionKey(appPW)
	if err != nil {
		return err
	}
	wallet, err := c.connectedWallet(assetID)
	if err != nil {
		return fmt.Errorf("OpenWallet: wallet not found for %d -> %s: %v", assetID, unbip(assetID), err)
	}
	err = unlockWallet(wallet, crypter)
	if err != nil {
		return err
	}

	state := wallet.state()
	avail, locked, _ := wallet.Balance(0) // the wallet just unlocked successfully
	log.Infof("Connected to and unlocked %s wallet. Account %q balance available "+
		"= %d / locked = %d, Deposit address = %s",
		state.Symbol, wallet.Account, avail, locked, state.Address)

	if dcrID, _ := dex.BipSymbolID("dcr"); assetID == dcrID {
		go c.checkUnpaidFees(wallet)
	}
	c.refreshUser()
	return nil
}

// unlockWallet unlocks the wallet with the crypter.
func unlockWallet(wallet *xcWallet, crypter encrypt.Crypter) error {
	pwB, err := crypter.Decrypt(wallet.encPW)
	if err != nil {
		return fmt.Errorf("unlockWallet decryption error: %v", err)
	}
	err = wallet.Unlock(string(pwB), aYear)
	if err != nil {
		return fmt.Errorf("unlockWallet unlock error: %v", err)
	}
	return nil
}

// CloseWallet closes the wallet for the specified asset. The wallet cannot be
// closed if there are active negotiations for the asset.
func (c *Core) CloseWallet(assetID uint32) error {
	wallet, err := c.connectedWallet(assetID)
	if err != nil {
		return fmt.Errorf("CloseWallet wallet not found for %d -> %s: %v", assetID, unbip(assetID), err)
	}
	c.connMtx.RLock()
	defer c.connMtx.RUnlock()
	for _, dc := range c.conns {
		if dc.hasOrders(assetID) {
			return fmt.Errorf("cannot lock %s wallet with active orders or negotiations", unbip(assetID))
		}
	}
	err = wallet.Lock()
	if err != nil {
		return err
	}
	c.refreshUser()
	return nil
}

// ConnectWallet connects to the wallet without unlocking.
func (c *Core) ConnectWallet(assetID uint32) error {
	_, err := c.connectedWallet(assetID)
	return err
}

// PreRegister creates a connection to the specified DEX and fetches the
// registration fee. The connection is left open and stored temporarily while
// registration is completed.
func (c *Core) PreRegister(form *PreRegisterForm) (uint64, error) {
	c.connMtx.RLock()
	_, found := c.conns[form.URL]
	c.connMtx.RUnlock()
	if found {
		return 0, fmt.Errorf("already registered at %s", form.URL)
	}

	dc, err := c.connectDEX(&db.AccountInfo{
		URL:  form.URL,
		Cert: []byte(form.Cert),
	})
	if err != nil {
		return 0, err
	}

	c.connMtx.Lock()
	defer c.connMtx.Unlock() // remain locked for pendingReg and pendingTimer
	c.pendingReg = dc

	// After a while, if the registration hasn't been completed, disconnect from
	// the DEX. The Register loop will form a new connection if pendingReg is nil.
	if c.pendingTimer == nil {
		c.pendingTimer = time.AfterFunc(time.Minute*5, func() {
			c.connMtx.Lock()
			defer c.connMtx.Unlock()
			pendingDEX := c.pendingReg
			if pendingDEX != nil && pendingDEX.acct.url == dc.acct.url {
				pendingDEX.connMaster.Disconnect()
				c.pendingReg = nil
			}
		})
	} else {
		if !c.pendingTimer.Stop() {
			<-c.pendingTimer.C
		}
		c.pendingTimer.Reset(time.Minute * 5)
	}
	return dc.cfg.Fee, nil
}

// Register registers an account with a new DEX. If an error occurs while
// fetching the DEX configuration or creating the fee transaction, it will be
// returned immediately as the first argument. A thread will be started to wait
// for the requisite confirmations and send the fee notification to the server.
// Any error returned from that thread will be sent over the returned channel.
func (c *Core) Register(form *RegisterForm) error {
	// Check the app password.
	crypter, err := c.encryptionKey(form.AppPass)
	if err != nil {
		return err
	}
	// For now, asset ID is hard-coded to Decred for registration fees.
	assetID, _ := dex.BipSymbolID("dcr")
	if form.URL == "" {
		return fmt.Errorf("no dex url specified")
	}
	wallet, err := c.connectedWallet(assetID)
	if err != nil {
		return fmt.Errorf("Register: wallet error for %d -> %s: %v", assetID, unbip(assetID), err)
	}

	// Make sure the account doesn't already exist.
	_, err = c.db.Account(form.URL)
	if err == nil {
		return fmt.Errorf("account already exists for %s", form.URL)
	}

	// Get a connection to the dex.
	ai := &db.AccountInfo{
		URL:  form.URL,
		Cert: []byte(form.Cert),
	}

	// Lock conns map, pendingReg, and pendingTimer.
	c.connMtx.Lock()
	dc, found := c.conns[ai.URL]
	// If it's not already in the map, see if there is a pre-registration pending.
	if !found && c.pendingReg != nil {
		if c.pendingReg.acct.url != ai.URL {
			return fmt.Errorf("pending registration exists for dex %s", c.pendingReg.acct.url)
		}
		if c.pendingTimer != nil { // it should be set
			if !c.pendingTimer.Stop() {
				return fmt.Errorf("pre-registration timer already fired and shutdown the connection")
			}
			c.pendingTimer = nil
		}
		dc = c.pendingReg
		c.conns[ai.URL] = dc
		ai.Cert = dc.acct.cert
		found = true
	}
	// If it was neither in the map or pre-registered, get a new connection.
	if !found {
		dc, err = c.connectDEX(ai)
		if err != nil {
			c.connMtx.Unlock()
			return err
		}
		c.conns[ai.URL] = dc
	}
	c.connMtx.Unlock()

	// Update user in case conns or wallets were updated.
	c.refreshUser()

	regAsset, found := dc.assets[assetID]
	if !found {
		return fmt.Errorf("asset information not found: %v", err)
	}

	// Create a new private key for the account.
	privKey, err := secp256k1.GeneratePrivateKey()
	if err != nil {
		return fmt.Errorf("error creating wallet key: %v", err)
	}

	// Encrypt the private key.
	encKey, err := crypter.Encrypt(privKey.Serialize())
	if err != nil {
		return fmt.Errorf("error encrypting private key: %v", err)
	}

	// The account ID is generated from the public key.
	pubKey := privKey.PubKey()

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
	errChan := make(chan error, 1)
	err = dc.Request(regMsg, func(msg *msgjson.Message) {
		errChan <- msg.UnmarshalResult(regRes)
	})
	if err != nil {
		return fmt.Errorf("'register' request error: %v", err)
	}
	err = extractError(errChan, requestTimeout, "register")
	if err != nil {
		return fmt.Errorf("'register' result decode error: %v", err)
	}

	// Check the server's signature.
	msg, err := regRes.Serialize()
	if err != nil {
		return fmt.Errorf("error serializing RegisterResult for signature check: %v", err)
	}

	// Create a public key for this account.
	dexPubKey, err := checkSigS256(msg, regRes.DEXPubKey, regRes.Sig)
	if err != nil {
		return fmt.Errorf("DEX signature validation error: %v", err)
	}

	// Check that the fee is non-zero.
	if regRes.Fee == 0 {
		return fmt.Errorf("zero registration fees not supported")
	}
	if regRes.Fee != dc.cfg.Fee {
		return fmt.Errorf("DEX 'register' result fee doesn't match the 'config' value. %d != %d", regRes.Fee, dc.cfg.Fee)
	}
	if regRes.Fee != form.Fee {
		return fmt.Errorf("registration fee provided to Register does not match the DEX registration fee. %d != %d", form.Fee, regRes.Fee)
	}

	// Pay the registration fee.
	log.Infof("Attempting registration fee payment for %s of %d units of %s", regRes.Address,
		regRes.Fee, regAsset.Symbol)
	coin, err := wallet.PayFee(regRes.Address, regRes.Fee, regAsset)
	if err != nil {
		return fmt.Errorf("error paying registration fee: %v", err)
	}
	// Set the dexConnection account fields.
	dc.acct.dexPubKey = dexPubKey
	dc.acct.encKey = encKey
	dc.acct.feeCoin = coin.ID()
	dc.acct.unlock(crypter)

	// Set the db.AccountInfo fields and save the account info.
	ai.EncKey = encKey
	ai.DEXPubKey = dexPubKey
	ai.FeeCoin = coin.ID()
	err = c.db.CreateAccount(ai)
	if err != nil {
		log.Errorf("error saving account: %v", err)
		// Don't abandon registration. The fee is already paid.
	}
	details := fmt.Sprintf("Waiting for %d confirmations before trading at %s", dc.cfg.RegFeeConfirms, dc.acct.url)
	c.notify(newFeePaymentNote("Fee paid", details, db.Success))

	// Set up the coin waiter.
	c.verifyRegistrationFee(wallet, dc, coin.ID(), assetID)
	c.refreshUser()
	return nil
}

// verifyRegistrationFee waits the required amount of confirmations for the
// registration fee payment. Once the requirment is met the server is notified.
// If the server acknowedlment is successfull, the account is set as 'paid' in
// the database. Notifications about confirmations increase, errors and success
// events are broadcasted to all subscribers.
func (c *Core) verifyRegistrationFee(wallet *xcWallet, dc *dexConnection, coinID []byte, assetID uint32) {
	reqConfs := dc.cfg.RegFeeConfirms

	trigger := func() (bool, error) {
		confs, err := wallet.Confirmations(coinID)
		if err != nil {
			return false, fmt.Errorf("Error getting confirmations for %s: %v", hex.EncodeToString(coinID), err)
		}
		details := fmt.Sprintf("Fee payment confirmations %v/%v", confs, uint32(reqConfs))

		if confs < uint32(reqConfs) {
			c.notify(newFeePaymentNoteWithConfirmations("Waiting for confirmations", details, db.Data, uint32(reqConfs), confs))
		}

		return confs >= uint32(reqConfs), nil
	}

	c.wait(assetID, trigger, func(err error) {
		log.Debugf("Registration fee txn %s now has %d confirmations.", coinIDString(assetID, coinID), reqConfs)
		defer func() {
			if err != nil {
				details := fmt.Sprintf("Error encountered while paying fees to %s: %v", dc.acct.url, err)
				c.notify(newFeePaymentNote("Fee payment error", details, db.ErrorLevel))
			} else {
				details := fmt.Sprintf("You may now trade at %s", dc.acct.url)
				c.notify(newFeePaymentNote("Account registered", details, db.Success))
			}
		}()
		if err != nil {
			return
		}
		log.Infof("Notifying dex %s of fee payment.", dc.acct.url)
		err = c.notifyFee(dc, coinID)
		if err != nil {
			return
		}
		dc.acct.pay()
		err = c.authDEX(dc)
		if err != nil {
			log.Errorf("fee paid, but failed to authenticate connection to %s: %v", dc.acct.url, err)
		}
	})

}

// IsInitialized checks if the app is already initialized.
func (c *Core) IsInitialized() (bool, error) {
	return c.db.ValueExists(keyParamsKey)
}

// InitializeClient sets the initial app-wide password for the client.
func (c *Core) InitializeClient(pw []byte) error {
	if initialized, err := c.IsInitialized(); err != nil {
		return fmt.Errorf("error checking if app is already initialized: %v", err)
	} else if initialized {
		return fmt.Errorf("already initialized, login instead")
	}

	if len(pw) == 0 {
		return fmt.Errorf("empty password not allowed")
	}

	crypter := c.newCrypter(pw)
	err := c.db.Store(keyParamsKey, crypter.Serialize())
	if err != nil {
		return fmt.Errorf("error storing key parameters: %v", err)
	}
	c.refreshUser()
	return nil
}

// Login logs the user in, decrypting the account keys for all known DEXes.
func (c *Core) Login(pw []byte) ([]*db.Notification, error) {
	crypter, err := c.encryptionKey(pw)
	if err != nil {
		return nil, err
	}

	loaded := c.resolveActiveTrades(crypter)
	if loaded > 0 {
		log.Infof("loaded %d incomplete orders", loaded)
	}

	c.initializeDEXConnections(crypter)
	notes, err := c.db.NotificationsN(10)
	if err != nil {
		log.Errorf("Login -> NotificationsN error: %v", err)
	}
	c.refreshUser()
	return notes, nil
}

// initializeDEXConnections connects to the DEX servers in the conns map and
// authenticates the connection. If registration is incomplete, reFee is run and
// the connection will be authenticated once the `notifyfee` request is sent.
func (c *Core) initializeDEXConnections(crypter encrypt.Crypter) {
	// Connections will be attempted in parallel, so we'll need to protect the
	// errorSet.
	var wg sync.WaitGroup
	c.connMtx.RLock()
	defer c.connMtx.RUnlock()
	for _, dc := range c.conns {
		// Copy the iterator for use in the authDEX goroutine.
		if dc.acct.authed() {
			continue
		}
		err := dc.acct.unlock(crypter)
		if err != nil {
			details := fmt.Sprintf("error unlocking account for %s: %v", dc.acct.url, err)
			c.notify(newFeePaymentNote("Account unlock error", details, db.ErrorLevel))
			continue
		}
		dcrID, _ := dex.BipSymbolID("dcr")
		if !dc.acct.paid() {
			if len(dc.acct.feeCoin) == 0 {
				details := fmt.Sprintf("Empty fee coin for %s.", dc.acct.url)
				c.notify(newFeePaymentNote("Fee coin error", details, db.ErrorLevel))
				continue
			}
			// Try to unlock the Decred wallet, which should run the reFee cycle, and
			// in turn will run authDEX.
			dcrWallet, err := c.connectedWallet(dcrID)
			if err != nil {
				log.Debugf("Failed to connect for reFee at %s with error: %v", dc.acct.url, err)
				details := fmt.Sprintf("Incomplete registration detected for %s, but failed to connect to the Decred wallet", dc.acct.url)
				c.notify(newFeePaymentNote("Wallet connection warning", details, db.WarningLevel))
				continue
			}
			if !dcrWallet.unlocked() {
				err = unlockWallet(dcrWallet, crypter)
				if err != nil {
					details := fmt.Sprintf("Connected to Decred wallet to complete registration at %s, but failed to unlock: %v", dc.acct.url, err)
					c.notify(newFeePaymentNote("Wallet unlock error", details, db.ErrorLevel))
					continue
				}
			}
			c.reFee(dcrWallet, dc)
			continue
		}

		wg.Add(1)
		go func(dc *dexConnection) {
			defer wg.Done()
			err := c.authDEX(dc)
			if err != nil {
				details := fmt.Sprintf("%s: %v", dc.acct.url, err)
				c.notify(newFeePaymentNote("DEX auth error", details, db.ErrorLevel))
				return
			}
		}(dc)
	}
	wg.Wait()
}

// resolveActiveTrades loads order and match data from the database. Only active
// orders and orders with active matches are loaded. Also, only active matches
// are loaded, even if there are inactive matches for the same order, but it may
// be desirable to load all matches, so this behavior may change.
func (c *Core) resolveActiveTrades(crypter encrypt.Crypter) int {
	failed := make(map[uint32]struct{})
	c.connMtx.RLock()
	defer c.connMtx.RUnlock()
	var loaded int
	for _, dc := range c.conns {
		// loadDBTrades can add to the failed map.
		ready, err := c.loadDBTrades(dc, crypter, failed)
		if err != nil {
			details := fmt.Sprintf("Some orders failed to load from the database: %v", err)
			c.notify(newOrderNote("Order load failure", details, db.ErrorLevel, nil))
		}
		if len(ready) > 0 {
			err = c.resumeTrades(dc, ready)
			if err != nil {
				details := fmt.Sprintf("Some active orders failed to resume: %v", err)
				c.notify(newOrderNote("Order resumption error", details, db.ErrorLevel, nil))
			}
		}
		loaded += len(ready)
		dc.refreshMarkets()
	}
	return loaded
}

var waiterID uint64

func (c *Core) wait(assetID uint32, trigger func() (bool, error), action func(error)) {
	c.waiterMtx.Lock()
	defer c.waiterMtx.Unlock()
	c.blockWaiters[atomic.AddUint64(&waiterID, 1)] = &blockWaiter{
		assetID: assetID,
		trigger: trigger,
		action:  action,
	}
}

func (c *Core) notifyFee(dc *dexConnection, coinID []byte) error {
	if dc.acct.locked() {
		return fmt.Errorf("%s account locked. cannot notify fee.", dc.acct.url)
	}
	// Notify the server of the fee coin once there are enough confirmations.
	req := &msgjson.NotifyFee{
		AccountID: dc.acct.id[:],
		CoinID:    coinID,
	}
	// We'll need this to validate the server's acknowledgement.
	reqB, err := req.Serialize()
	if err != nil {
		log.Warnf("fee paid with coin %x, but unable to serialize notifyfee request so server signature cannot be verified", coinID)
		// Don't quit. The fee is already paid, so follow through if possible.
	}
	// Sign the request and send it.
	err = stamp(dc.acct.privKey, req)
	if err != nil {
		return err
	}
	msg, err := msgjson.NewRequest(dc.NextID(), msgjson.NotifyFeeRoute, req)
	if err != nil {
		return fmt.Errorf("failed to create notifyfee request: %v", err)
	}

	errChan := make(chan error, 1)
	err = dc.Request(msg, func(resp *msgjson.Message) {
		ack := new(msgjson.Acknowledgement)
		err = resp.UnmarshalResult(ack)
		if err != nil {
			errChan <- fmt.Errorf("notify fee result error: %v", err)
			return
		}
		// If there was a serialization error, validation is skipped. A warning
		// message was already logged.
		if len(reqB) > 0 {
			// redefining err here, since these errors won't be sent over the
			// response channel.
			err := dc.acct.checkSig(reqB, ack.Sig)
			if err != nil {
				log.Warnf("account was registered, but DEX signature could not be verified: %v", err)
			}
		} else {
			log.Warnf("Marking account as paid, even though the server's signature could not be validated.")
		}
		errChan <- c.db.AccountPaid(&db.AccountProof{
			URL:   dc.acct.url,
			Stamp: req.Time,
			Sig:   ack.Sig,
		})
	})
	if err != nil {
		return fmt.Errorf("'notifyfee' request error: %v", err)
	}
	return extractError(errChan, requestTimeout, "notifyfee")
}

// Withdraw initiates a withdraw from an exchange wallet. The client password
// must be provided as an additional verification.
func (c *Core) Withdraw(pw []byte, assetID uint32, value uint64) (asset.Coin, error) {
	_, err := c.encryptionKey(pw)
	if err != nil {
		return nil, fmt.Errorf("Withdraw password error: %v", err)
	}
	if value == 0 {
		return nil, fmt.Errorf("%s zero withdraw", unbip(assetID))
	}
	wallet, found := c.wallet(assetID)
	if !found {
		return nil, fmt.Errorf("%s wallet not found", unbip(assetID))
	}
	coin, err := wallet.Withdraw(wallet.address, value, wallet.Info().DefaultFeeRate)
	if err != nil {
		details := fmt.Sprintf("Error encountered during %s withdraw: %v", unbip(assetID), err)
		c.notify(newWithdrawNote("Withdraw error", details, db.ErrorLevel))
	} else {
		details := fmt.Sprintf("Withdraw of %s has completed successfully. Coin ID = %s", unbip(assetID), coin)
		c.notify(newWithdrawNote("Withdraw sent", details, db.Success))
	}
	return coin, err
}

// Trade is used to place a market or limit order.
func (c *Core) Trade(pw []byte, form *TradeForm) (*Order, error) {
	// Check the user password.
	crypter, err := c.encryptionKey(pw)
	if err != nil {
		return nil, fmt.Errorf("Trade password error: %v", err)
	}

	// Get the dexConnection and the dex.Asset for each asset.
	c.connMtx.RLock()
	dc, found := c.conns[form.DEX]
	c.connMtx.RUnlock()
	if !found {
		return nil, fmt.Errorf("unknown DEX %s", form.DEX)
	}

	rate, qty := form.Rate, form.Qty
	if form.IsLimit && rate == 0 {
		return nil, fmt.Errorf("zero-rate order not allowed")
	}

	wallets, err := c.walletSet(dc, form.Base, form.Quote, form.Sell)
	if err != nil {
		return nil, err
	}

	fromWallet, toWallet := wallets.fromWallet, wallets.toWallet
	fromID, toID := fromWallet.AssetID, toWallet.AssetID

	if !fromWallet.connected() {
		err = fromWallet.Connect(c.ctx)
		if err != nil {
			return nil, fmt.Errorf("Error connecting wallet: %v", err)
		}
	}
	if !fromWallet.unlocked() {
		err = unlockWallet(fromWallet, crypter)
		if err != nil {
			return nil, fmt.Errorf("failed to unlock %s wallet: %v", unbip(fromID), err)
		}
	}
	if !toWallet.connected() {
		err = toWallet.Connect(c.ctx)
		if err != nil {
			return nil, fmt.Errorf("Error connecting wallet: %v", err)
		}
	}
	if !toWallet.unlocked() {
		err = unlockWallet(toWallet, crypter)
		if err != nil {
			return nil, fmt.Errorf("failed to unlock %s wallet: %v", unbip(toID), err)
		}
	}

	// Get an address for the swap contract.
	addr, err := toWallet.Address()
	if err != nil {
		return nil, err
	}

	// Fund the order and prepare the coins.
	fundQty := qty
	if form.IsLimit && !form.Sell {
		fundQty = calc.BaseToQuote(rate, fundQty)
	}
	coins, err := fromWallet.Fund(fundQty, wallets.fromAsset)
	if err != nil {
		return nil, err
	}
	coinIDs := make([]order.CoinID, 0, len(coins))
	for i := range coins {
		coinIDs = append(coinIDs, []byte(coins[i].ID()))
	}
	msgCoins, err := messageCoins(fromWallet, coins)
	if err != nil {
		return nil, err
	}

	// Construct the order.
	preImg := newPreimage()
	prefix := &order.Prefix{
		AccountID:  dc.acct.ID(),
		BaseAsset:  form.Base,
		QuoteAsset: form.Quote,
		OrderType:  order.MarketOrderType,
		ClientTime: time.Now(),
		Commit:     preImg.Commit(),
	}
	var ord order.Order
	if form.IsLimit {
		prefix.OrderType = order.LimitOrderType
		tif := order.StandingTiF
		if form.TifNow {
			tif = order.ImmediateTiF
		}
		ord = &order.LimitOrder{
			P: *prefix,
			T: order.Trade{
				Coins:    coinIDs,
				Sell:     form.Sell,
				Quantity: form.Qty,
				Address:  addr,
			},
			Rate:  form.Rate,
			Force: tif,
		}
	} else {
		ord = &order.MarketOrder{
			P: *prefix,
			T: order.Trade{
				Coins:    coinIDs,
				Sell:     form.Sell,
				Quantity: form.Qty,
				Address:  addr,
			},
		}
	}
	err = order.ValidateOrder(ord, order.OrderStatusEpoch, wallets.baseAsset.LotSize)
	if err != nil {
		return nil, err
	}

	// Everything is ready. Send the order.
	route, msgOrder := messageOrder(ord, msgCoins)

	// Send and get the result.
	var result = new(msgjson.OrderResult)
	err = dc.signAndRequest(msgOrder, route, result)
	if err != nil {
		return nil, err
	}

	// If we encounter an error, perform some basic logging.
	//
	// TODO: Notify the client somehow.
	logAbandon := func(err interface{}) {
		log.Errorf("Abandoning order. "+
			"preimage: %x, server time: %d: %v",
			preImg[:], result.ServerTime, err)
	}

	err = validateOrderResponse(dc, result, ord, msgOrder)
	if err != nil {
		log.Errorf("Abandoning order. preimage: %x, server time: %d: %v",
			preImg[:], result.ServerTime, err)
		return nil, err
	}

	// Store the order.
	dbOrder := &db.MetaOrder{
		MetaData: &db.OrderMetaData{
			Status: order.OrderStatusEpoch,
			DEX:    dc.acct.url,
			Proof: db.OrderProof{
				DEXSig:   result.Sig,
				Preimage: preImg[:],
			},
		},
		Order: ord,
	}
	err = c.db.UpdateOrder(dbOrder)
	if err != nil {
		logAbandon(fmt.Sprintf("failed to store order in database: %v", err))
		return nil, fmt.Errorf("Database error. order abandoned")
	}

	// Prepare and store the tracker and get the core.Order to return.
	tracker := newTrackedTrade(dbOrder, preImg, dc, c.db, c.latencyQ, wallets, coins, c.notify)
	corder, _ := tracker.coreOrder()
	dc.tradeMtx.Lock()
	dc.trades[tracker.ID()] = tracker
	dc.tradeMtx.Unlock()

	// Send a low-priority notification.
	details := fmt.Sprintf("%sing %.8f %s (%s)",
		sellString(corder.Sell), float64(corder.Qty)/conversionFactor, unbip(form.Base), tracker.token())
	if !form.IsLimit && !form.Sell {
		details = fmt.Sprintf("selling %.8f %s (%s)",
			float64(corder.Qty)/conversionFactor, unbip(form.Quote), tracker.token())
	}
	c.notify(newOrderNote("Order placed", details, db.Poke, corder))

	// Refresh the markets and user.
	dc.refreshMarkets()
	c.refreshUser()

	return corder, nil
}

// walletSet is a pair of wallets with asset configurations identified in useful
// ways.
type walletSet struct {
	baseAsset  *dex.Asset
	quoteAsset *dex.Asset
	fromWallet *xcWallet
	fromAsset  *dex.Asset
	toWallet   *xcWallet
	toAsset    *dex.Asset
}

// walletSet constructs a walletSet.
func (c *Core) walletSet(dc *dexConnection, baseID, quoteID uint32, sell bool) (*walletSet, error) {
	baseAsset, found := dc.assets[baseID]
	if !found {
		return nil, fmt.Errorf("unknown base asset %d -> %s for %s", baseID, unbip(baseID), dc.acct.url)
	}
	quoteAsset, found := dc.assets[quoteID]
	if !found {
		return nil, fmt.Errorf("unknown quote asset %d -> %s for %s", quoteID, unbip(quoteID), dc.acct.url)
	}

	// Connect and open the wallets if needed.
	baseWallet, found := c.wallet(baseID)
	if !found {
		return nil, fmt.Errorf("%s wallet not found", unbip(baseID))
	}
	quoteWallet, found := c.wallet(quoteID)
	if !found {
		return nil, fmt.Errorf("%s wallet not found", unbip(quoteID))
	}

	// We actually care less about base/quote, and more about from/to, which
	// depends on whether this is a buy or sell order.
	fromAsset, toAsset := baseAsset, quoteAsset
	fromWallet, toWallet := baseWallet, quoteWallet
	if !sell {
		fromAsset, toAsset = quoteAsset, baseAsset
		fromWallet, toWallet = quoteWallet, baseWallet
	}

	return &walletSet{
		baseAsset:  baseAsset,
		quoteAsset: quoteAsset,
		fromWallet: fromWallet,
		fromAsset:  fromAsset,
		toWallet:   toWallet,
		toAsset:    toAsset,
	}, nil
}

// Cancel is used to send a cancel order which cancels a limit order.
func (c *Core) Cancel(pw []byte, tradeID string) error {
	// Check the user password.
	_, err := c.encryptionKey(pw)
	if err != nil {
		return fmt.Errorf("Trade password error: %v", err)
	}

	// Find the order. Make sure it's a limit order.
	oid, err := order.IDFromHex(tradeID)
	if err != nil {
		return err
	}
	dc, tracker, _ := c.findDEXOrder(oid)
	if tracker == nil {
		return fmt.Errorf("active order %s not found. cannot cancel", oid)
	}
	if tracker.Type() != order.LimitOrderType {
		return fmt.Errorf("cannot cancel non-limit order %s of type %s", oid, tracker.Type())
	}

	// Construct the order.
	prefix := tracker.Prefix()
	preImg := newPreimage()
	co := &order.CancelOrder{
		P: order.Prefix{
			AccountID:  prefix.AccountID,
			BaseAsset:  prefix.BaseAsset,
			QuoteAsset: prefix.QuoteAsset,
			OrderType:  order.CancelOrderType,
			ClientTime: time.Now(),
			Commit:     preImg.Commit(),
		},
		TargetOrderID: oid,
	}
	err = order.ValidateOrder(co, order.OrderStatusEpoch, 0)
	if err != nil {
		return err
	}

	// Create and send the order message. Check the response before using it.
	route, msgOrder := messageOrder(co, nil)
	var result = new(msgjson.OrderResult)
	err = tracker.dc.signAndRequest(msgOrder, route, result)
	if err != nil {
		return err
	}
	err = validateOrderResponse(dc, result, co, msgOrder)
	if err != nil {
		log.Errorf("Abandoning order. preimage: %x, server time: %d: %v",
			preImg[:], result.ServerTime, err)
		return err
	}

	// Store the order.
	err = c.db.UpdateOrder(&db.MetaOrder{
		MetaData: &db.OrderMetaData{
			Status: order.OrderStatusEpoch,
			DEX:    dc.acct.url,
			Proof: db.OrderProof{
				DEXSig:   result.Sig,
				Preimage: preImg[:],
			},
		},
		Order: co,
	})
	if err != nil {
		log.Errorf("failed to store order in database: %v", err)
		return fmt.Errorf("Database error. order abandoned")
	}

	// Store the cancel order with the tracker.
	tracker.cancelTrade(co, preImg)

	return nil
}

// findDEXOrder finds the dexConnection and order for the order ID. A boolean is
// returned indicating whether this is the cancel order for the trade.
func (c *Core) findDEXOrder(oid order.OrderID) (*dexConnection, *trackedTrade, bool) {
	c.connMtx.RLock()
	defer c.connMtx.RUnlock()
	for _, dc := range c.conns {
		tracker, _, isCancel := dc.findOrder(oid)
		if tracker != nil {
			return dc, tracker, isCancel
		}
	}
	return nil, nil, false
}

// authDEX authenticates the connection for a DEX.
func (c *Core) authDEX(dc *dexConnection) error {
	// Prepare and sign the message for the 'connect' route.
	acctID := dc.acct.ID()
	payload := &msgjson.Connect{
		AccountID:  acctID[:],
		APIVersion: 0,
		Time:       encode.UnixMilliU(time.Now()),
	}
	b, err := payload.Serialize()
	if err != nil {
		return fmt.Errorf("error serializing 'connect' message: %v", err)
	}
	sig, err := dc.acct.sign(b)
	if err != nil {
		return fmt.Errorf("signing error: %v", err)
	}
	payload.SetSig(sig)
	// Send the 'connect' request.
	req, err := msgjson.NewRequest(dc.NextID(), msgjson.ConnectRoute, payload)
	if err != nil {
		return fmt.Errorf("error encoding 'connect' request: %v", err)
	}
	errChan := make(chan error, 1)
	var result = new(msgjson.ConnectResult)
	err = dc.Request(req, func(msg *msgjson.Message) {
		errChan <- msg.UnmarshalResult(result)
	})
	// Check the request error.
	if err != nil {
		return err
	}
	// Check the response error.
	err = extractError(errChan, requestTimeout, "connect")
	if err != nil {
		return fmt.Errorf("'connect' error: %v", err)
	}
	log.Debugf("authenticated connection to %s", dc.acct.url)
	// Set the account as authenticated.
	dc.acct.auth()

	matches, _, err := dc.parseMatches(result.Matches, false)
	if err != nil {
		log.Error(err)
	}

	dc.compareServerMatches(matches)

	return nil
}

// Balance retrieves the current wallet balance.
func (c *Core) Balance(assetID uint32) (uint64, error) {
	wallet, err := c.connectedWallet(assetID)
	if err != nil {
		return 0, fmt.Errorf("%d -> %s wallet error: %v", assetID, unbip(assetID), err)
	}
	return wallet.balance, nil
}

// initialize pulls the known DEX URLs from the database and attempts to
// connect and retrieve the DEX configuration.
func (c *Core) initialize() {
	accts, err := c.db.Accounts()
	if err != nil {
		log.Errorf("Error retrieve accounts from database: %v", err)
	}
	var wg sync.WaitGroup
	for _, acct := range accts {
		wg.Add(1)
		go func(acct *db.AccountInfo) {
			defer wg.Done()
			dc, err := c.connectDEX(acct)
			if err != nil {
				log.Errorf("error connecting to DEX %s: %v", acct.URL, err)
				return
			}
			log.Debugf("connectDEX for %s completed, checking account...", acct.URL)
			if !acct.Paid {
				if len(acct.FeeCoin) == 0 {
					// Register should have set this when creating the account
					// that was obtained via db.Accounts.
					log.Warnf("Incomplete registration without fee payment detected for DEX %s. "+
						"Discarding account.", acct.URL)
					return
				}
				log.Infof("Incomplete registration detected for DEX %s. "+
					"Registration will be completed when the Decred wallet is unlocked.",
					acct.URL)
				details := fmt.Sprintf("Unlock your Decred wallet to complete registration for %s", acct.URL)
				c.notify(newFeePaymentNote("Incomplete registration", details, db.WarningLevel))
				// checkUnpaidFees will pay the fees if the wallet is unlocked
			}
			c.connMtx.Lock()
			c.conns[acct.URL] = dc
			c.connMtx.Unlock()
			log.Debugf("dex connection to %s ready", acct.URL)
		}(acct)
	}
	// If there were accounts, wait until they are loaded and log a messsage.
	if len(accts) > 0 {
		go func() {
			wg.Wait()
			c.connMtx.RLock()
			log.Infof("Successfully connected to %d out of %d "+
				"DEX servers", len(c.conns), len(accts))
			c.connMtx.RUnlock()
			c.refreshUser()
		}()
	}
	dbWallets, err := c.db.Wallets()
	if err != nil {
		log.Errorf("error loading wallets from database: %v", err)
	}
	c.walletMtx.Lock()
	for _, dbWallet := range dbWallets {
		wallet, err := c.loadWallet(dbWallet)
		aid := dbWallet.AssetID
		if err != nil {
			log.Errorf("error loading %d -> %s wallet: %v", aid, unbip(aid), err)
			continue
		}
		// Wallet is loaded from the DB, but not yet connected.
		log.Infof("Loaded %s wallet configuration. Account %q, Deposit address = %s",
			unbip(aid), dbWallet.Account, dbWallet.Address)
		c.wallets[dbWallet.AssetID] = wallet
	}
	numWallets := len(c.wallets)
	c.walletMtx.Unlock()
	if len(dbWallets) > 0 {
		log.Infof("successfully loaded %d of %d wallets", numWallets, len(dbWallets))
	}
	c.refreshUser()
}

// feeLock is used to ensure that no more than one reFee check is running at a
// time.
var feeLock uint32

// checkUnpaidFees checks whether the registration fee info has an acceptable
// state, and tries to rectify any inconsistencies.
func (c *Core) checkUnpaidFees(dcrWallet *xcWallet) {
	c.connMtx.RLock()
	defer c.connMtx.RUnlock()
	if !atomic.CompareAndSwapUint32(&feeLock, 0, 1) {
		return
	}
	var wg sync.WaitGroup
	for _, dc := range c.conns {
		if dc.acct.paid() {
			continue
		}
		if len(dc.acct.feeCoin) == 0 {
			log.Errorf("empty fee coin found for unpaid account")
			continue
		}
		wg.Add(1)
		go func(dc *dexConnection) {
			c.reFee(dcrWallet, dc)
			wg.Done()
		}(dc)
	}
	wg.Wait()
	atomic.StoreUint32(&feeLock, 0)
}

// reFee attempts to finish the fee payment process for a DEX. reFee might be
// called if the client was shutdown after a fee was paid, but before it had the
// requisite confirmations for the 'notifyfee' message to be sent to the server.
func (c *Core) reFee(dcrWallet *xcWallet, dc *dexConnection) {
	// Get the database account info.
	acctInfo, err := c.db.Account(dc.acct.url)
	if err != nil {
		log.Errorf("reFee %s - error retrieving account info: %v", dc.acct.url, err)
		return
	}
	// A couple sanity checks.
	if !bytes.Equal(acctInfo.FeeCoin, dc.acct.feeCoin) {
		log.Errorf("reFee %s - fee coin mismatch. %x != %x", dc.acct.url, acctInfo.FeeCoin, dc.acct.feeCoin)
		return
	}
	if acctInfo.Paid {
		log.Errorf("reFee %s - account for %x already marked paid", dc.acct.url, dc.acct.feeCoin)
		return
	}
	// Get the coin for the fee.
	confs, err := dcrWallet.Confirmations(acctInfo.FeeCoin)
	if err != nil {
		log.Errorf("reFee %s - error getting coin confirmations: %v", dc.acct.url, err)
		return
	}
	if confs >= uint32(dc.cfg.RegFeeConfirms) {
		err := c.notifyFee(dc, acctInfo.FeeCoin)
		if err != nil {
			log.Errorf("reFee %s - notifyfee error: %v", dc.acct.url, err)
			details := fmt.Sprintf("Error encountered while paying fees to %s: %v", dc.acct.url, err)
			c.notify(newFeePaymentNote("Fee payment error", details, db.ErrorLevel))
		} else {
			log.Infof("Fee paid at %s", dc.acct.url)
			details := fmt.Sprintf("You may now trade at %s.", dc.acct.url)
			c.notify(newFeePaymentNote("Registration complete", details, db.Success))
			// dc.acct.pay() and c.authDEX????
			dc.acct.pay()
			err = c.authDEX(dc)
			if err != nil {
				log.Errorf("fee paid, but failed to authenticate connection to %s: %v", dc.acct.url, err)
			}
		}
		return
	}
	dcrID, _ := dex.BipSymbolID("dcr")
	c.verifyRegistrationFee(dcrWallet, dc, acctInfo.FeeCoin, dcrID)
}

// dbTrackers prepares trackedTrades based on active orders and matches in the
// database. Since dbTrackers runs before sign in when wallets are not connected
// or unlocked, wallets and coins are not added to the returned trackers. Use
// resumeTrades with the app Crypter to prepare wallets and coins.
func (c *Core) dbTrackers(dc *dexConnection) (map[order.OrderID]*trackedTrade, error) {
	// Prepare active orders, according to the DB.
	dbOrders, err := c.db.ActiveDEXOrders(dc.acct.url)
	if err != nil {
		return nil, fmt.Errorf("database error when fetching orders for %s: %x", dc.acct.url, err)
	}
	// It's possible for an order to not be active, but still have active matches.
	// Grab the orders for those too.
	haveOrder := func(oid order.OrderID) bool {
		for _, dbo := range dbOrders {
			if dbo.Order.ID() == oid {
				return true
			}
		}
		return false
	}

	activeDBMatches, err := c.db.ActiveDEXMatches(dc.acct.url)
	if err != nil {
		return nil, fmt.Errorf("database error fetching active matches for %s: %v", dc.acct.url, err)
	}
	for _, dbMatch := range activeDBMatches {
		oid := dbMatch.Match.OrderID
		if !haveOrder(oid) {
			dbOrder, err := c.db.Order(oid)
			if err != nil {
				return nil, fmt.Errorf("database error fetching order %s for active match %s at %s: %v", oid, dbMatch.Match.MatchID, dc.acct.url, err)
			}
			dbOrders = append(dbOrders, dbOrder)
		}
	}

	cancels := make(map[order.OrderID]*order.CancelOrder)
	cancelPreimages := make(map[order.OrderID]order.Preimage)
	trackers := make(map[order.OrderID]*trackedTrade, len(dbOrders))
	for _, dbOrder := range dbOrders {
		ord, md := dbOrder.Order, dbOrder.MetaData
		var preImg order.Preimage
		copy(preImg[:], md.Proof.Preimage)
		if co, ok := ord.(*order.CancelOrder); ok {
			cancels[co.TargetOrderID] = co
			cancelPreimages[co.TargetOrderID] = preImg
		} else {
			var preImg order.Preimage
			copy(preImg[:], dbOrder.MetaData.Proof.Preimage)
			trackers[dbOrder.Order.ID()] = newTrackedTrade(dbOrder, preImg, dc, c.db, c.latencyQ, nil, nil, c.notify)
		}
	}
	for oid := range cancels {
		tracker, found := trackers[oid]
		if !found {
			log.Errorf("unmatched active cancel order: %s", oid)
			continue
		}
		tracker.cancelTrade(cancels[oid], cancelPreimages[oid])
	}

	for _, dbMatch := range activeDBMatches {
		// Impossible for the tracker to not be here, since it would have errored
		// in the previous activeDBMatches loop.
		tracker := trackers[dbMatch.Match.OrderID]
		tracker.matches[dbMatch.Match.MatchID] = &matchTracker{
			id:        dbMatch.Match.MatchID,
			prefix:    tracker.Prefix(),
			trade:     tracker.Trade(),
			MetaMatch: *dbMatch,
		}
	}

	return trackers, nil
}

// loadDBTrades loads orders and matches from the database for the specified
// dexConnection. If there are active trades, the necessary wallets will be
// unlocked. To prevent spamming wallet connections, the 'failed' map will be
// populated with asset IDs for which the attempt to connect or unlock has
// failed. The failed map should be passed on subsequent calls for other dexes.
func (c *Core) loadDBTrades(dc *dexConnection, crypter encrypt.Crypter, failed map[uint32]struct{}) ([]*trackedTrade, error) {
	// Parse the active trades and see if any wallets need unlocking.
	trades, err := c.dbTrackers(dc)
	if err != nil {
		return nil, fmt.Errorf("error retreiving active matches: %v", err)
	}

	errs := newErrorSet(dc.acct.url + ": ")
	ready := make([]*trackedTrade, 0, len(dc.trades))
	for _, trade := range trades {
		base, quote := trade.Base(), trade.Quote()
		_, baseFailed := failed[base]
		_, quoteFailed := failed[quote]
		if !baseFailed {
			baseWallet, err := c.connectedWallet(base)
			if err != nil {
				baseFailed = true
				failed[base] = struct{}{}
			} else if !baseWallet.unlocked() {
				err = unlockWallet(baseWallet, crypter)
				if err != nil {
					baseFailed = true
					failed[base] = struct{}{}
				}
			}
		}
		if !baseFailed && !quoteFailed {
			quoteWallet, err := c.connectedWallet(quote)
			if err != nil {
				quoteFailed = true
				failed[quote] = struct{}{}
			} else if !quoteWallet.unlocked() {
				err = unlockWallet(quoteWallet, crypter)
				if err != nil {
					quoteFailed = true
					failed[quote] = struct{}{}
				}
			}
		}
		if baseFailed {
			errs.add("could not complete order %s because the wallet for %s cannot be used", trade.token(), unbip(base))
			continue
		}
		if quoteFailed {
			errs.add("could not complete order %s because the wallet for %s cannot be used", trade.token(), unbip(quote))
			continue
		}
		ready = append(ready, trade)
	}
	return ready, errs.ifany()
}

// resumeTrades recovers the states of active trades and matches, including
// loading audit info needed to finish swaps and funding coins needed to create
// new matches on an order.
func (c *Core) resumeTrades(dc *dexConnection, trackers []*trackedTrade) error {
	var tracker *trackedTrade
	notifyErr := func(subject, s string, a ...interface{}) {
		detail := fmt.Sprintf(s, a...)
		corder, _ := tracker.coreOrder()
		c.notify(newOrderNote(subject, detail, db.ErrorLevel, corder))
	}
	for _, tracker = range trackers {
		// See if the order is 100% filled.
		trade := tracker.Trade()
		// Make sure we have the necessary wallets.
		wallets, err := c.walletSet(dc, tracker.Base(), tracker.Quote(), trade.Sell)
		if err != nil {
			notifyErr("Wallet missing", "Wallet retrieval error for active order %s: %v", tracker.token(), err)
			continue
		}
		tracker.wallets = wallets
		// If matches haven't redeemed, but the counter-swap has been received,
		// reload the audit info.
		isActive := tracker.metaData.Status == order.OrderStatusBooked || tracker.metaData.Status == order.OrderStatusEpoch
		var stillNeedsCoins bool
		for _, match := range tracker.matches {
			dbMatch, metaData := match.Match, match.MetaData
			var counterSwap []byte
			takerNeedsSwap := dbMatch.Side == order.Taker && dbMatch.Status >= order.MakerSwapCast && dbMatch.Status < order.MatchComplete
			if takerNeedsSwap {
				stillNeedsCoins = true
				counterSwap = metaData.Proof.MakerSwap
			}
			makerNeedsSwap := dbMatch.Side == order.Maker && dbMatch.Status >= order.TakerSwapCast && dbMatch.Status < order.MakerRedeemed
			if makerNeedsSwap {
				counterSwap = metaData.Proof.TakerSwap
			}
			if takerNeedsSwap || makerNeedsSwap {
				if len(counterSwap) == 0 {
					match.failErr = fmt.Errorf("missing counter-swap, order %s, match %s", tracker.ID(), match.id)
					notifyErr("Match status error", "Match %s for order %s is in state %s, but has no maker swap coin.", dbMatch.Side, tracker.token(), dbMatch.Status)
					continue
				}
				counterContract := metaData.Proof.CounterScript
				if len(counterContract) == 0 {
					match.failErr = fmt.Errorf("missing counter-contract, order %s, match %s", tracker.ID(), match.id)
					notifyErr("Match status error", "Match %s for order %s is in state %s, but has no maker swap contract.", dbMatch.Side, tracker.token(), dbMatch.Status)
					continue
				}
				auditInfo, err := wallets.toWallet.AuditContract(counterSwap, counterContract)
				if err != nil {
					match.failErr = fmt.Errorf("audit error, order %s, match %s: %v", tracker.ID(), match.id, err)
					notifyErr("Match recovery error", "Error auditing counter-parties swap contract during swap recovery on order %s: %v", tracker.token(), err)
					continue
				}
				match.counterSwap = auditInfo
				continue
			}
		}

		// Active orders and orders with matches with unsent swaps need the funding
		// coin(s).
		if isActive || stillNeedsCoins {
			coinIDs := trade.Coins
			if len(tracker.metaData.ChangeCoin) != 0 {
				coinIDs = []order.CoinID{tracker.metaData.ChangeCoin}
			}
			if len(coinIDs) == 0 {
				notifyErr("No funding coins", "Order %s has no %s funding coins", tracker.token(), unbip(wallets.fromAsset.ID))
				continue
			}
			byteIDs := make([]dex.Bytes, 0, len(coinIDs))
			for _, cid := range coinIDs {
				byteIDs = append(byteIDs, []byte(cid))
			}
			if len(byteIDs) == 0 {
				notifyErr("Order coin error", "No coins for loaded order %s %s: %v", unbip(wallets.fromAsset.ID), tracker.token(), err)
				continue
			}
			coins, err := wallets.fromWallet.FundingCoins(byteIDs)
			if err != nil {
				notifyErr("Order coin error", "Source coins retrieval error for %s %s: %v", unbip(wallets.fromAsset.ID), tracker.token(), err)
				continue
			}
			tracker.coins = mapifyCoins(coins)
		}

		dc.tradeMtx.Lock()
		dc.trades[tracker.ID()] = tracker
		dc.tradeMtx.Unlock()
	}
	return nil
}

// connectDEX creates and connects a dexConnection, but does not authenticate
// the connection through the 'connect' route.
func (c *Core) connectDEX(acctInfo *db.AccountInfo) (*dexConnection, error) {
	// Get the host from the DEX URL.
	uri := acctInfo.URL
	parsedURL, err := url.Parse(uri)
	if err != nil {
		return nil, fmt.Errorf("error parsing account URL %s: %v", uri, err)
	}

	// Create a websocket connection to the server.
	conn, err := c.wsConstructor(&comms.WsCfg{
		URL:      "wss://" + parsedURL.Host + "/ws",
		PingWait: 60 * time.Second,
		Cert:     acctInfo.Cert,
		ReconnectSync: func() {
			go c.handleReconnect(uri)
		},
	})
	if err != nil {
		return nil, fmt.Errorf("Error creating websocket connection for %s: %v", uri, err)
	}
	connMaster := dex.NewConnectionMaster(conn)
	err = connMaster.Connect(c.ctx)
	// If the initial connection returned an error, shut it down to kill the
	// auto-reconnect cycle.
	if err != nil {
		connMaster.Disconnect()
		return nil, fmt.Errorf("Error initalizing websocket connection: %v", err)
	}

	// Request the market configuration. The DEX is only added when the DEX
	// configuration is successfully retrieved.
	reqMsg, err := msgjson.NewRequest(conn.NextID(), msgjson.ConfigRoute, nil)
	if err != nil {
		log.Errorf("error creating 'config' request: %v", err)
	}
	connChan := make(chan *dexConnection, 1)
	var reqErr error
	err = conn.Request(reqMsg, func(msg *msgjson.Message) {
		dexCfg := new(msgjson.ConfigResult)
		err = msg.UnmarshalResult(dexCfg)
		if err != nil {
			reqErr = fmt.Errorf("'config' result decode error: %v", err)
			connChan <- nil
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

		marketMap := make(map[string]*Market)
		for _, mkt := range dexCfg.Markets {
			base, quote := assets[mkt.Base], assets[mkt.Quote]
			market := &Market{
				Name:            mkt.Name,
				BaseID:          base.ID,
				BaseSymbol:      base.Symbol,
				QuoteID:         quote.ID,
				QuoteSymbol:     quote.Symbol,
				EpochLen:        mkt.EpochLen,
				StartEpoch:      mkt.StartEpoch,
				MarketBuyBuffer: mkt.MarketBuyBuffer,
			}
			marketMap[mkt.Name] = market
		}

		// Create the dexConnection and add it to the map.
		dc := &dexConnection{
			WsConn:     conn,
			connMaster: connMaster,
			assets:     assets,
			cfg:        dexCfg,
			books:      make(map[string]*bookie),
			acct:       newDEXAccount(acctInfo),
			marketMap:  marketMap,
			trades:     make(map[order.OrderID]*trackedTrade),
			notify:     c.notify,
		}

		dc.refreshMarkets()
		connChan <- dc
		c.wg.Add(1)
		// Listen for incoming messages.
		log.Infof("Connected to DEX %s and listening for messages.", uri)
		go c.listen(dc)
	}) // End 'config' request

	if err != nil {
		log.Errorf("error sending 'config' request: %v", err)
		reqErr = err
		connChan <- nil
	}
	return <-connChan, reqErr
}

// If the passed epoch is greater than the highest previously passed epoch,
// send an epoch notification to all subscribers.
func (c *Core) setEpoch(epochIdx uint64) {
	c.epochMtx.Lock()
	defer c.epochMtx.Unlock()
	if epochIdx > c.epoch {
		c.epoch = epochIdx
		c.notify(newEpochNotification(epochIdx))
	}
}

// handleReconnect is called when a WsConn indicates that a lost connection has
// been re-established.
func (c *Core) handleReconnect(uri string) {
	log.Infof("DEX at %s has reconnected", uri)
}

// handleMatchProofMsg is called when a match_proof notification is received.
func handleMatchProofMsg(c *Core, dc *dexConnection, msg *msgjson.Message) error {
	var note msgjson.MatchProofNote
	err := msg.Unmarshal(&note)
	if err != nil {
		return fmt.Errorf("match proof note unmarshal error: %v", err)
	}

	// Expire the epoch
	c.setEpoch(note.Epoch + 1)

	dc.booksMtx.RLock()
	defer dc.booksMtx.RUnlock()

	book, ok := dc.books[note.MarketID]
	if !ok {
		return fmt.Errorf("no order book found with market id %q",
			note.MarketID)
	}

	// Reset the epoch queue after processing the match proof message.
	defer book.ResetEpoch()

	return book.ValidateMatchProof(note)
}

// handleRevokeMatchMsg is called when a revoke_match message is received.
func handleRevokeMatchMsg(_ *Core, dc *dexConnection, msg *msgjson.Message) error {
	var revocation msgjson.RevokeMatch
	err := msg.Unmarshal(&revocation)
	if err != nil {
		return fmt.Errorf("revoke match unmarshal error: %v", err)
	}

	var oid order.OrderID
	copy(oid[:], revocation.OrderID)

	tracker, _, _ := dc.findOrder(oid)
	if tracker == nil {
		return fmt.Errorf("no order found with id %s", oid.String())
	}

	md := tracker.metaData
	md.Status = order.OrderStatusRevoked
	metaOrder := &db.MetaOrder{
		MetaData: md,
		Order:    tracker.Order,
	}

	return tracker.db.UpdateOrder(metaOrder)
}

// routeHandler is a handler for a message from the DEX.
type routeHandler func(*Core, *dexConnection, *msgjson.Message) error

var reqHandlers = map[string]routeHandler{
	msgjson.PreimageRoute:    handlePreimageRequest,
	msgjson.MatchRoute:       handleMatchRoute,
	msgjson.AuditRoute:       handleAuditRoute,
	msgjson.RedemptionRoute:  handleRedemptionRoute,
	msgjson.RevokeMatchRoute: handleRevokeMatchMsg,
	msgjson.SuspensionRoute:  nil,
}

var noteHandlers = map[string]routeHandler{
	msgjson.MatchProofRoute:  handleMatchProofMsg,
	msgjson.BookOrderRoute:   handleBookOrderMsg,
	msgjson.EpochOrderRoute:  handleEpochOrderMsg,
	msgjson.UnbookOrderRoute: handleUnbookOrderMsg,
}

// listen monitors the DEX websocket connection for server requests and
// notifications.
func (c *Core) listen(dc *dexConnection) {
	defer c.wg.Done()
	msgs := dc.MessageSource()
	// Lets run a match check every 1/3 broadcast timeout.
	//
	// DRAFT NOTE: This is set as seconds in server/dex/dex.go, but I think it
	// should be milliseconds, no?
	bTimeout := time.Millisecond * time.Duration(dc.cfg.BroadcastTimeout)
	ticker := time.NewTicker(bTimeout / 3)
out:
	for {
		select {
		case msg, ok := <-msgs:
			if !ok {
				log.Debugf("Connection closed for %s.", dc.acct.url)
				// TODO: This just means that wsConn, which created the
				// MessageSource channel, was shut down before this loop
				// returned via ctx.Done. It may be necessary to investigate the
				// most appropriate normal shutdown sequence (i.e. close all
				// connections before stopping Core).
				return
			}

			var handler routeHandler
			var found bool
			switch msg.Type {
			case msgjson.Request:
				handler, found = reqHandlers[msg.Route]
			case msgjson.Notification:
				handler, found = noteHandlers[msg.Route]
			case msgjson.Response:
				log.Errorf("A response was received in the message queue: %s", msg)
			default:
				log.Errorf("invalid message type %d from MessageSource", msg.Type)
				continue
			}
			// Until all the routes have handlers, check for nil too.
			if !found || handler == nil {
				log.Errorf("no handler found for route %s", msg.Route)
				continue
			}
			err := handler(c, dc, msg)
			if err != nil {
				log.Error(err)
			}
		case <-ticker.C:
			dc.tradeMtx.Lock()
			for _, trade := range dc.trades {
				err := trade.tick()
				if err != nil {
					log.Error(err)
				}
			}
			dc.tradeMtx.Unlock()
		case <-c.ctx.Done():
			break out
		}
	}
}

// handlePreimageRequest handles a DEX-originating request for an order
// preimage.
func handlePreimageRequest(c *Core, dc *dexConnection, msg *msgjson.Message) error {
	req := new(msgjson.PreimageRequest)
	err := msg.Unmarshal(req)
	if err != nil {
		return fmt.Errorf("preimage request parsing error: %v", err)
	}

	var oid order.OrderID
	copy(oid[:], req.OrderID)
	tracker, preImg, isCancel := dc.findOrder(oid)
	if tracker == nil {
		return fmt.Errorf("no active order found for preimage request for %s", oid)
	}
	resp, err := msgjson.NewResponse(msg.ID, &msgjson.PreimageResponse{
		Preimage: preImg[:],
	}, nil)
	if err != nil {
		return fmt.Errorf("preimage response encoding error: %v", err)
	}
	err = dc.Send(resp)
	if err != nil {
		return fmt.Errorf("preimage send error: %v", err)
	}
	corder, cancelOrder := tracker.coreOrder()
	var details string
	if isCancel {
		corder = cancelOrder
		details = fmt.Sprintf("match cycle has begun for cancellation order for trade %s", tracker.token())
	} else {
		details = fmt.Sprintf("match cycle has begun for order %s", tracker.token())
	}
	c.notify(newOrderNote("Preimage sent", details, db.Poke, corder))
	return nil
}

// handleMatchRoute processes the DEX-originating match route request,
// indicating that a match has been made and needs to be negotiated.
func handleMatchRoute(c *Core, dc *dexConnection, msg *msgjson.Message) error {
	msgMatches := make([]*msgjson.Match, 0)
	err := msg.Unmarshal(&msgMatches)
	if err != nil {
		return fmt.Errorf("match request parsing error: %v", err)
	}
	matches, acks, err := dc.parseMatches(msgMatches, true)
	if err != nil {
		return err
	}

	resp, err := msgjson.NewResponse(msg.ID, acks, nil)
	if err != nil {
		return err
	}
	err = dc.Send(resp)
	if err != nil {
		return err
	}

	defer c.refreshUser()

	return dc.runMatches(matches)
}

// handleAuditRoute handles the DEX-originating audit request, which is sent
// when a match counter-party reports their initiation transaction.
func handleAuditRoute(c *Core, dc *dexConnection, msg *msgjson.Message) error {
	audit := new(msgjson.Audit)
	err := msg.Unmarshal(audit)
	if err != nil {
		return fmt.Errorf("audit request parsing error: %v", err)
	}
	var oid order.OrderID
	copy(oid[:], audit.OrderID)
	tracker, _, _ := dc.findOrder(oid)
	if tracker == nil {
		return fmt.Errorf("audit request received for unknown order: %s", string(msg.Payload))
	}
	err = tracker.processAudit(msg.ID, audit)
	if err != nil {
		return err
	}
	return tracker.tick()
}

// handleRedemptionRoute handles the DEX-originating redemption request, which
// is sent when a match counter-party reports their redemption transaction.
func handleRedemptionRoute(c *Core, dc *dexConnection, msg *msgjson.Message) error {
	redemption := new(msgjson.Redemption)
	err := msg.Unmarshal(redemption)
	if err != nil {
		return fmt.Errorf("redemption request parsing error: %v", err)
	}
	var oid order.OrderID
	copy(oid[:], redemption.OrderID)
	tracker, _, _ := dc.findOrder(oid)
	if tracker == nil {
		return fmt.Errorf("redemption request received for unknown order: %s", string(msg.Payload))
	}
	err = tracker.processRedemption(msg.ID, redemption)
	if err != nil {
		return err
	}
	return tracker.tick()
}

// removeWaiter removes a blockWaiter from the map.
func (c *Core) removeWaiter(id uint64) {
	c.waiterMtx.Lock()
	delete(c.blockWaiters, id)
	c.waiterMtx.Unlock()
}

// tipChange is called by a wallet backend when the tip block changes, or when
// a connection error is encountered such that tip change reporting may be
// adversely affected.
func (c *Core) tipChange(assetID uint32, nodeErr error) {
	if nodeErr != nil {
		log.Errorf("%s wallet is reporting a failed state: %v", unbip(assetID), nodeErr)
		return
	}
	log.Tracef("processing tip change for %s", unbip(assetID))
	c.waiterMtx.Lock()
	for id, waiter := range c.blockWaiters {
		if waiter.assetID != assetID {
			continue
		}
		go func(id uint64, waiter *blockWaiter) {
			ok, err := waiter.trigger()
			if err != nil {
				waiter.action(err)
				c.removeWaiter(id)
			}
			if ok {
				waiter.action(nil)
				c.removeWaiter(id)
			}
		}(id, waiter)
	}
	c.waiterMtx.Unlock()
	c.connMtx.RLock()
	for _, dc := range c.conns {
		dc.tickAsset(assetID)
	}
	c.connMtx.RUnlock()
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
	payload.Stamp(encode.UnixMilliU(time.Now()))
	return sign(privKey, payload)
}

// extractError extracts the error from the channel with a timeout.
func extractError(errChan <-chan error, delay time.Duration, route string) error {
	select {
	case err := <-errChan:
		return err
	case <-time.NewTimer(delay).C:
		return fmt.Errorf("timed out waiting for '%s' response.", route)
	}
}

// newPreimage creates a random order commitment. If you require a matching
// commitment, generate a Preimage, then Preimage.Commit().
func newPreimage() (p order.Preimage) {
	copy(p[:], encode.RandomBytes(order.PreimageSize))
	return
}

// messagePrefix converts the order.Prefix to a msgjson.Prefix.
func messagePrefix(prefix *order.Prefix) *msgjson.Prefix {
	oType := uint8(msgjson.LimitOrderNum)
	switch prefix.OrderType {
	case order.MarketOrderType:
		oType = msgjson.MarketOrderNum
	case order.CancelOrderType:
		oType = msgjson.CancelOrderNum
	}
	return &msgjson.Prefix{
		AccountID:  prefix.AccountID[:],
		Base:       prefix.BaseAsset,
		Quote:      prefix.QuoteAsset,
		OrderType:  oType,
		ClientTime: encode.UnixMilliU(prefix.ClientTime),
		Commit:     prefix.Commit[:],
	}
}

// messageTrade converts the order.Trade to a msgjson.Trade, adding the coins.
func messageTrade(trade *order.Trade, coins []*msgjson.Coin) *msgjson.Trade {
	side := uint8(msgjson.BuyOrderNum)
	if trade.Sell {
		side = msgjson.SellOrderNum
	}
	return &msgjson.Trade{
		Side:     side,
		Quantity: trade.Quantity,
		Coins:    coins,
		Address:  trade.Address,
	}
}

// messageCoin converts the []asset.Coin to a []*msgjson.Coin, signing the coin
// IDs and retrieving the pubkeys too.
func messageCoins(wallet *xcWallet, coins asset.Coins) ([]*msgjson.Coin, error) {
	msgCoins := make([]*msgjson.Coin, 0, len(coins))
	for _, coin := range coins {
		coinID := coin.ID()
		pubKeys, sigs, err := wallet.SignMessage(coin, coinID)
		if err != nil {
			return nil, err
		}
		msgCoins = append(msgCoins, &msgjson.Coin{
			ID:      coinID,
			PubKeys: pubKeys,
			Sigs:    sigs,
			Redeem:  coin.Redeem(),
		})
	}
	return msgCoins, nil
}

// messageOrder converts an order.Order of any underlying type to an appropriate
// msgjson type used for submitting the order.
func messageOrder(ord order.Order, coins []*msgjson.Coin) (string, msgjson.Stampable) {
	prefix, trade := ord.Prefix(), ord.Trade()
	switch o := ord.(type) {
	case *order.LimitOrder:
		tifFlag := uint8(msgjson.StandingOrderNum)
		if o.Force == order.ImmediateTiF {
			tifFlag = msgjson.ImmediateOrderNum
		}
		return msgjson.LimitRoute, &msgjson.LimitOrder{
			Prefix: *messagePrefix(prefix),
			Trade:  *messageTrade(trade, coins),
			Rate:   o.Rate,
			TiF:    tifFlag,
		}
	case *order.MarketOrder:
		return msgjson.MarketRoute, &msgjson.MarketOrder{
			Prefix: *messagePrefix(prefix),
			Trade:  *messageTrade(trade, coins),
		}
	case *order.CancelOrder:
		return msgjson.CancelRoute, &msgjson.CancelOrder{
			Prefix:   *messagePrefix(prefix),
			TargetID: o.TargetOrderID[:],
		}
	default:
		panic("unknown order type")
	}
}

// validateOrderResponse validates the response against the order and the order
// message.
func validateOrderResponse(dc *dexConnection, result *msgjson.OrderResult, ord order.Order, msgOrder msgjson.Stampable) error {
	if result.ServerTime == 0 {
		return fmt.Errorf("OrderResult cannot have servertime = 0")
	}
	msgOrder.Stamp(result.ServerTime)
	msg, err := msgOrder.Serialize()
	if err != nil {
		return fmt.Errorf("serialization error. order abandoned")
	}
	err = dc.acct.checkSig(msg, result.Sig)
	if err != nil {
		return fmt.Errorf("signature error. order abandoned")
	}
	ord.SetTime(encode.UnixTimeMilli(int64(result.ServerTime)))
	// Check the order ID
	if len(result.OrderID) != order.OrderIDSize {
		return fmt.Errorf("failed ID length check. order abandoned")
	}
	var checkID order.OrderID
	copy(checkID[:], result.OrderID)
	oid := ord.ID()
	if oid != checkID {
		return fmt.Errorf("failed ID match. order abandoned")
	}
	return nil
}
