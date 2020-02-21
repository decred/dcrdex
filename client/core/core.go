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
	book "decred.org/dcrdex/client/order"
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/encode"
	"decred.org/dcrdex/dex/encrypt"
	"decred.org/dcrdex/dex/msgjson"
	"decred.org/dcrdex/dex/order"
	"decred.org/dcrdex/server/account"
	"github.com/decred/dcrd/dcrec/secp256k1/v2"
)

const (
	keyParamsKey = "keyParams"
)

var (
	// log is a logger generated with the LogMaker provided with Config.
	log            dex.Logger
	unbip          = dex.BipIDSymbol
	aYear          = time.Hour * 24 * 365
	requestTimeout = 10 * time.Second
)

// dexConnection is the websocket connection and the DEX configuration.
type dexConnection struct {
	comms.WsConn
	connMaster *dex.ConnectionMaster
	assets     map[uint32]*dex.Asset
	cfg        *msgjson.ConfigResult
	acct       *dexAccount

	booksMtx sync.RWMutex
	books    map[string]*book.OrderBook

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
		mid := market.sid()
		dc.tradeMtx.RLock()
		for _, trade := range dc.trades {
			if trade.sid == mid {
				coreOrder, _ := trade.coreOrder()
				market.Orders = append(market.Orders, coreOrder)
			}
		}
		dc.tradeMtx.RUnlock()
		marketMap[market.sid()] = market
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

// coinWaiter is a message waiting to be stamped, signed, and sent once a
// specified coin has the requisite confirmations. The coinWaiter is similar to
// dcrdex/server/coinwaiter.Waiter, but is different enough to warrant a
// separate type.
type coinWaiter struct {
	assetID uint32
	trigger func() (bool, error)
	action  func(error)
}

// Config is the configuration for the Core.
type Config struct {
	// DBPath is a filepath to use for the client database. If the database does
	// not already exist, it will be created.
	DBPath string
	// LoggerMaker is a LoggerMaker for the core to use. Having the logger as an
	// argument enables creating custom loggers for use in a GUI interface.
	LoggerMaker *dex.LoggerMaker
	// Certs is a mapping of URL to filepaths of TLS Certificates for the server.
	// This is intended for accommodating self-signed certificates.
	Certs map[string]string
	// Net is the current network.
	Net dex.Network
}

// Core is the core client application. Core manages DEX connections, wallets,
// database access, match negotiation and more.
type Core struct {
	ctx           context.Context
	wg            sync.WaitGroup
	cfg           *Config
	connMtx       sync.RWMutex
	conns         map[string]*dexConnection
	pendingTimer  *time.Timer
	pendingReg    *dexConnection
	db            db.DB
	certs         map[string]string
	wallets       map[uint32]*xcWallet
	walletMtx     sync.RWMutex
	loggerMaker   *dex.LoggerMaker
	net           dex.Network
	waiterMtx     sync.Mutex
	waiters       map[uint64]*coinWaiter
	wsConstructor func(*comms.WsCfg) (comms.WsConn, error)
	userMtx       sync.RWMutex
	user          *User
	newCrypter    func(string) encrypt.Crypter
	reCrypter     func(string, []byte) (encrypt.Crypter, error)
}

// New is the constructor for a new Core.
func New(cfg *Config) (*Core, error) {
	log = cfg.LoggerMaker.Logger("CORE")
	db, err := bolt.NewDB(cfg.DBPath)
	if err != nil {
		return nil, fmt.Errorf("database initialization error: %v", err)
	}
	core := &Core{
		cfg:         cfg,
		db:          db,
		certs:       cfg.Certs,
		conns:       make(map[string]*dexConnection),
		wallets:     make(map[uint32]*xcWallet),
		net:         cfg.Net,
		loggerMaker: cfg.LoggerMaker,
		waiters:     make(map[uint64]*coinWaiter),
		// Allowing to change the constructor makes testing a lot easier.
		wsConstructor: comms.NewWsConn,
		newCrypter:    encrypt.NewCrypter,
		reCrypter:     encrypt.Deserialize,
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
	c.db.Run(ctx)
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
func (c *Core) encryptionKey(pw string) (encrypt.Crypter, error) {
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
	// An unititialized user would not have this key/value stored yet, so would
	// be an error. This is likely the only error possible here.
	k, _ := c.db.Get(keyParamsKey)
	u := &User{
		Assets:      c.SupportedAssets(),
		Exchanges:   c.Exchanges(),
		Initialized: len(k) > 0,
	}
	c.userMtx.Lock()
	c.user = u
	c.userMtx.Unlock()
}

// CreateWallet creates a new exchange wallet.
func (c *Core) CreateWallet(appPW, walletPW string, form *WalletForm) error {
	crypter, err := c.encryptionKey(appPW)
	if err != nil {
		return err
	}
	encPW, err := crypter.Encrypt([]byte(walletPW))
	if err != nil {
		return fmt.Errorf("wallet password encryption error: %v", err)
	}

	dbWallet := &db.Wallet{
		AssetID:     form.AssetID,
		Account:     form.Account,
		INIPath:     form.INIPath,
		EncryptedPW: encPW,
	}
	_, exists := c.wallet(form.AssetID)
	if exists {
		return fmt.Errorf("%s wallet already exists", unbip(dbWallet.AssetID))
	}

	wallet, err := c.loadWallet(dbWallet)
	if err != nil {
		return fmt.Errorf("error loading wallet for %d -> %s: %v", dbWallet.AssetID, unbip(dbWallet.AssetID), err)
	}

	err = wallet.Connect(c.ctx)
	if err != nil {
		// Assume that the wallet form values are invalid, and drop the wallet from
		// the wallets map.
		c.walletMtx.Lock()
		delete(c.wallets, dbWallet.AssetID)
		c.walletMtx.Unlock()
		return fmt.Errorf("Error connecting wallet: %v", err)
	}

	initErr := func(s string, a ...interface{}) error {
		wallet.connector.Disconnect()
		return fmt.Errorf(s, a...)
	}

	dbWallet.Balance, _, err = wallet.Balance(0)
	if err != nil {
		return initErr("error getting balance for %s: %v", unbip(form.AssetID), err)
	}
	wallet.setBalance(dbWallet.Balance)

	dbWallet.Address, err = wallet.Address()
	if err != nil {
		return initErr("error getting deposit address for %s: %v", unbip(form.AssetID), err)
	}
	wallet.setAddress(dbWallet.Address)

	// Store the wallet in the database.
	err = c.db.UpdateWallet(dbWallet)
	if err != nil {
		return initErr("error storing wallet credentials: %v", err)
	}

	c.refreshUser()
	return nil
}

func (c *Core) DEXConn(dex string) (ws comms.WsConn, aid account.AccountID, dexPk *secp256k1.PublicKey, signer func(msg []byte) ([]byte, error)) {
	c.connMtx.RLock()
	defer c.connMtx.RUnlock()
	dc, ok := c.conns[dex]
	if !ok {
		return
	}
	return dc.WsConn, dc.acct.ID(), dc.acct.dexPubKey, dc.acct.sign
}

// loadWallet uses the data from the database to construct a new exchange
// wallet. The returned wallet is running but not connected.
func (c *Core) loadWallet(dbWallet *db.Wallet) (*xcWallet, error) {
	wallet := &xcWallet{
		AssetID:   dbWallet.AssetID,
		balance:   dbWallet.Balance,
		balUpdate: dbWallet.BalUpdate,
		encPW:     dbWallet.EncryptedPW,
		address:   dbWallet.Address,
	}
	walletCfg := &asset.WalletConfig{
		Account: dbWallet.Account,
		INIPath: dbWallet.INIPath,
		TipChange: func(err error) {
			c.tipChange(dbWallet.AssetID, err)
		},
	}
	logger := c.loggerMaker.SubLogger("CORE", unbip(dbWallet.AssetID))
	w, err := asset.Setup(dbWallet.AssetID, walletCfg, logger, c.net)
	if err != nil {
		return nil, fmt.Errorf("error creating wallet: %v", err)
	}
	wallet.Wallet = w
	wallet.connector = dex.NewConnectionMaster(w)

	c.walletMtx.Lock()
	c.wallets[dbWallet.AssetID] = wallet
	c.walletMtx.Unlock()
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
func (c *Core) OpenWallet(assetID uint32, appPW string) error {
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
	dcrID, _ := dex.BipSymbolID("dcr")
	if assetID == dcrID {
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
	err = wallet.lock()
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
func (c *Core) PreRegister(dex string) (uint64, error) {
	c.connMtx.RLock()
	_, found := c.conns[dex]
	c.connMtx.RUnlock()
	if found {
		return 0, fmt.Errorf("already registered at %s", dex)
	}

	dc, err := c.connectDEX(&db.AccountInfo{URL: dex})
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
func (c *Core) Register(form *Registration) (error, <-chan error) {
	// Check the app password.
	crypter, err := c.encryptionKey(form.Password)
	if err != nil {
		return err, nil
	}
	// For now, asset ID is hard-coded to Decred for registration fees.
	assetID, _ := dex.BipSymbolID("dcr")
	if form.DEX == "" {
		return fmt.Errorf("no dex url specified"), nil
	}
	wallet, err := c.connectedWallet(assetID)
	if err != nil {
		return fmt.Errorf("Register: wallet error for %d -> %s: %v", assetID, unbip(assetID), err), nil
	}

	// Make sure the account doesn't already exist.
	_, err = c.db.Account(form.DEX)
	if err == nil {
		return fmt.Errorf("account already exists for %s", form.DEX), nil
	}

	// Get a connection to the dex.
	ai := &db.AccountInfo{
		URL: form.DEX,
	}

	// Lock conns map, pendingReg, and pendingTimer.
	c.connMtx.Lock()
	dc, found := c.conns[ai.URL]
	// If it's not already in the map, see if there is a pre-registration pending.
	if !found && c.pendingReg != nil {
		if c.pendingReg.acct.url != ai.URL {
			return fmt.Errorf("pending registration exists for dex %s", c.pendingReg.acct.url), nil
		}
		if c.pendingTimer != nil { // it should be set
			if !c.pendingTimer.Stop() {
				return fmt.Errorf("pre-registration timer already fired and shutdown the connection"), nil
			}
			c.pendingTimer = nil
		}
		dc = c.pendingReg
		c.conns[ai.URL] = dc
		found = true
	}
	// If it was neither in the map or pre-registered, get a new connection.
	if !found {
		dc, err = c.connectDEX(ai)
		if err != nil {
			c.connMtx.Unlock()
			return err, nil
		}
		c.conns[ai.URL] = dc
	}
	c.connMtx.Unlock()

	// Update user in case conns or wallets were updated.
	c.refreshUser()

	regAsset, found := dc.assets[assetID]
	if !found {
		return fmt.Errorf("asset information not found: %v", err), nil
	}

	// Create a new private key for the account.
	privKey, err := secp256k1.GeneratePrivateKey()
	if err != nil {
		return fmt.Errorf("error creating wallet key: %v", err), nil
	}

	// Encrypt the private key.
	encKey, err := crypter.Encrypt(privKey.Serialize())
	if err != nil {
		return fmt.Errorf("error encrypting private key: %v", err), nil
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
		return err, nil
	}

	// Create and send the the request, grabbing the result in the callback.
	regMsg, err := msgjson.NewRequest(dc.NextID(), msgjson.RegisterRoute, dexReg)
	if err != nil {
		return fmt.Errorf("error encoding message: %v", err), nil
	}
	regRes := new(msgjson.RegisterResult)
	errChan := make(chan error, 1)
	err = dc.Request(regMsg, func(msg *msgjson.Message) {
		errChan <- msg.UnmarshalResult(regRes)
	})
	if err != nil {
		return fmt.Errorf("'register' request error: %v", err), nil
	}
	err = extractError(errChan, requestTimeout, "register")
	if err != nil {
		return fmt.Errorf("'register' result decode error: %v", err), nil
	}

	// Check the server's signature.
	msg, err := regRes.Serialize()
	if err != nil {
		return fmt.Errorf("error serializing RegisterResult for signature check: %v", err), nil
	}

	// Create a public key for this account.
	dexPubKey, err := checkSigS256(msg, regRes.DEXPubKey, regRes.Sig)
	if err != nil {
		return fmt.Errorf("DEX signature validation error: %v", err), nil
	}

	// Check that the fee is non-zero.
	if regRes.Fee == 0 {
		return fmt.Errorf("zero registration fees not supported"), nil
	}
	if regRes.Fee != dc.cfg.Fee {
		return fmt.Errorf("DEX 'register' result fee doesn't match the 'config' value. %d != %d", regRes.Fee, dc.cfg.Fee), nil
	}
	if regRes.Fee != form.Fee {
		return fmt.Errorf("registration fee provided to Register does not match the DEX registration fee. %d != %d", form.Fee, regRes.Fee), nil
	}

	// Pay the registration fee.
	log.Infof("Attempting registration fee payment for %s of %d units of %s", regRes.Address,
		regRes.Fee, regAsset.Symbol)
	coin, err := wallet.PayFee(regRes.Address, regRes.Fee, regAsset)
	if err != nil {
		return fmt.Errorf("error paying registration fee: %v", err), nil
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

	trigger := confTrigger(wallet, coin.ID(), dc.cfg.RegFeeConfirms)
	// Set up the coin waiter.
	errChan = make(chan error, 1)
	c.wait(assetID, trigger, func(err error) {
		log.Debugf("Registration fee txn %x now has %d confirmations.", coin.ID(), dc.cfg.RegFeeConfirms)
		if err != nil {
			errChan <- err
		}
		log.Infof("Notifying dex %s of fee payment.", dc.acct.url)
		err = c.notifyFee(dc, coin.ID())
		if err != nil {
			errChan <- err
			return
		}
		dc.acct.pay()
		// New account won't have any active negotiations, so OK to discard first
		// first return value from authDEX.
		_, err = c.authDEX(dc)
		errChan <- err
	})
	c.refreshUser()
	return nil, errChan
}

// InitializeClient sets the initial app-wide password for the client.
func (c *Core) InitializeClient(pw string) error {
	if pw == "" {
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
func (c *Core) Login(pw string) (negotiations []Negotiation, err error) {
	crypter, err := c.encryptionKey(pw)
	if err != nil {
		return nil, err
	}
	var wg sync.WaitGroup
	var mtx sync.Mutex
	var errs []string
	c.connMtx.RLock()
	defer c.connMtx.RUnlock()
	for _, dc := range c.conns {
		// Copy the iterator for use in the authDEX goroutine.
		if dc.acct.authed() {
			continue
		}
		err := dc.acct.unlock(crypter)
		if err != nil {
			log.Errorf("error unlocking account for %s: %v", dc.acct.url, err)
			continue
		}
		if !dc.acct.paid() {
			// Unlock the account, but don't authenticate. Registration will be
			// completed when the user unlocks the Decred password.
			log.Infof("skipping authorization for unpaid account %s", dc.acct.url)
			continue
		}
		wg.Add(1)
		go func(dc *dexConnection) {
			defer wg.Done()
			n, err := c.authDEX(dc)
			if err != nil {
				mtx.Lock()
				errs = append(errs, fmt.Sprintf("%s: %v", dc.acct.url, err))
				mtx.Unlock()
				return
			}
			negotiations = append(negotiations, n...)
		}(dc)
	}
	wg.Wait()
	if len(errs) > 0 {
		err = fmt.Errorf("authorization errors: %s", strings.Join(errs, ", "))
	}
	c.refreshUser()
	return negotiations, err
}

var waiterID uint64

func (c *Core) wait(assetID uint32, trigger func() (bool, error), action func(error)) {
	c.waiterMtx.Lock()
	defer c.waiterMtx.Unlock()
	c.waiters[atomic.AddUint64(&waiterID, 1)] = &coinWaiter{
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
func (c *Core) Withdraw(pw string, assetID uint32, value uint64) (asset.Coin, error) {
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
	return wallet.Withdraw(wallet.address, value, wallet.Info().FeeRate)
}

// Trade is used to place a market or limit order.
func (c *Core) Trade(pw string, form *TradeForm) (*Order, error) {
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
	baseAsset, found := dc.assets[form.Base]
	if !found {
		return nil, fmt.Errorf("unknown base asset %d -> %s for %s", form.Base, unbip(form.Base), form.DEX)
	}
	quoteAsset, found := dc.assets[form.Quote]
	if !found {
		return nil, fmt.Errorf("unknown quote asset %d -> %s for %s", form.Quote, unbip(form.Quote), form.DEX)
	}

	// We actually care less about base/quote, and more about from/to, which
	// depends on whether this is a buy or sell order.
	fromID, toID, fromAsset := form.Base, form.Quote, baseAsset
	rate, qty := form.Rate, form.Qty
	if !form.Sell {
		fromID, toID, fromAsset = toID, fromID, quoteAsset
	}
	if form.IsLimit && rate == 0 {
		return nil, fmt.Errorf("zero-rate order not allowed")
	}

	// Connect and open the wallets if needed.
	fromWallet, err := c.connectedWallet(fromID)
	if err != nil {
		return nil, err
	}
	if !fromWallet.unlocked() {
		err = unlockWallet(fromWallet, crypter)
		if err != nil {
			return nil, fmt.Errorf("failed to unlock %s wallet", unbip(fromID))
		}
	}
	toWallet, err := c.connectedWallet(toID)
	if err != nil {
		return nil, err
	}
	if !toWallet.unlocked() {
		err = unlockWallet(toWallet, crypter)
		if err != nil {
			return nil, fmt.Errorf("failed to unlock %s wallet", unbip(toID))
		}
	}

	// Get an address for the swap contract.
	addr, err := toWallet.Address()
	if err != nil {
		return nil, err
	}

	// Fund the order and prepare the coins.
	coins, err := fromWallet.Fund(qty, fromAsset)
	if err != nil {
		return nil, err
	}
	coinIDs := make([]order.CoinID, 0, len(coins))
	for i := range coins {
		var b []byte = coins[i].ID()
		coinIDs = append(coinIDs, b)
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
	trade := &order.Trade{
		Coins:    coinIDs,
		Sell:     form.Sell,
		Quantity: form.Qty,
		Address:  addr,
	}
	var ord order.Order
	if form.IsLimit {
		prefix.OrderType = order.LimitOrderType
		tif := order.StandingTiF
		if form.TifNow {
			tif = order.ImmediateTiF
		}
		ord = &order.LimitOrder{
			P:     *prefix,
			T:     *trade,
			Rate:  form.Rate,
			Force: tif,
		}
	} else {
		ord = &order.MarketOrder{
			P: *prefix,
			T: *trade,
		}
	}
	err = order.ValidateOrder(ord, order.OrderStatusEpoch, baseAsset.LotSize)
	if err != nil {
		return nil, err
	}

	// Everything is ready. Send the order.
	route, msgOrder := messageOrder(ord, msgCoins)

	// Send and get the result.
	var result = new(msgjson.OrderResult)
	err = c.signAndRequest(msgOrder, dc, route, result)
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
	err = c.db.UpdateOrder(&db.MetaOrder{
		MetaData: &db.OrderMetaData{
			Status: order.OrderStatusEpoch,
			DEX:    dc.acct.url,
			Proof: db.OrderProof{
				DEXSig:   result.Sig,
				Preimage: preImg[:],
			},
		},
		Order: ord,
	})
	if err != nil {
		logAbandon(fmt.Sprintf("failed to store order in database: %v", err))
		return nil, fmt.Errorf("Database error. order abandoned")
	}

	// Prepare and store the tracker and get the core.Order to return.
	tracker := newTrackedTrade(ord, dc, preImg)
	coreOrder, _ := tracker.coreOrder()
	dc.tradeMtx.Lock()
	dc.trades[tracker.ID()] = tracker
	dc.tradeMtx.Unlock()

	// Refresh the markets and user.
	dc.refreshMarkets()
	c.refreshUser()

	return coreOrder, nil
}

// Cancel is used to send a cancel order which cancels a limit order.
func (c *Core) Cancel(pw string, tradeID string) error {
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
	err = c.signAndRequest(msgOrder, tracker.dc, route, result)
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

// signAndRequest signs and sends the request, unmarshaling the response into
// the provided interface.
func (c *Core) signAndRequest(signable msgjson.Signable, dc *dexConnection, route string, result interface{}) error {
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

// authDEX authenticates the connection for a DEX.
func (c *Core) authDEX(dc *dexConnection) ([]Negotiation, error) {
	// Prepare and sign the message for the 'connect' route.
	acctID := dc.acct.ID()
	payload := &msgjson.Connect{
		AccountID:  acctID[:],
		APIVersion: 0,
		Time:       encode.UnixMilliU(time.Now()),
	}
	b, err := payload.Serialize()
	if err != nil {
		return nil, fmt.Errorf("error serializing 'connect' message: %v", err)
	}
	sig, err := dc.acct.sign(b)
	if err != nil {
		return nil, fmt.Errorf("signing error: %v", err)
	}
	payload.SetSig(sig)
	// Send the 'connect' request.
	req, err := msgjson.NewRequest(dc.NextID(), msgjson.ConnectRoute, payload)
	if err != nil {
		return nil, fmt.Errorf("error encoding 'connect' request: %v", err)
	}
	errChan := make(chan error, 1)
	var result = new(msgjson.ConnectResult)
	err = dc.Request(req, func(msg *msgjson.Message) {
		errChan <- msg.UnmarshalResult(result)
	})
	// Check the request error.
	if err != nil {
		return nil, err
	}
	// Check the response error.
	err = extractError(errChan, requestTimeout, "connect")
	if err != nil {
		return nil, fmt.Errorf("'connect' error: %v", err)
	}
	log.Debugf("authenticated connection to %s", dc.acct.url)
	// Set the account as authenticated.
	dc.acct.auth()
	// Prepare the trade Negotiations.
	negotiations := make([]Negotiation, 0, len(result.Matches))
	var errs []string
	dc.tradeMtx.Lock()
	defer dc.tradeMtx.Unlock()
	for _, msgMatch := range result.Matches {
		var oid order.OrderID
		copy(oid[:], msgMatch.OrderID)
		tracker, found := dc.trades[oid]
		if !found {
			errs = append(errs, "order "+msgMatch.OrderID.String()+" does not have a matching active order")
			continue
		}
		err := tracker.negotiate(c.ctx, msgMatch)
		if err != nil {
			errs = append(errs, err.Error())
			continue
		}
	}
	if len(errs) > 0 {
		err = fmt.Errorf("errors beginning match negotiations: %s", strings.Join(errs, ", "))
	}
	return negotiations, err
}

func (c *Core) Sync(dex string, base, quote uint32) (chan *BookUpdate, error) {
	return make(chan *BookUpdate), nil
}

func (c *Core) Book(dex string, base, quote uint32) *OrderBook {
	return nil
}

func (c *Core) Unsync(dex string, base, quote uint32) {}

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
	for _, dbWallet := range dbWallets {
		_, err := c.loadWallet(dbWallet)
		if err != nil {
			aid := dbWallet.AssetID
			log.Errorf("error loading %d -> %s wallet: %v", aid, unbip(aid), err)
		}
	}
	if len(dbWallets) > 0 {
		c.walletMtx.RLock()
		numWallets := len(c.wallets)
		c.walletMtx.RUnlock()
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
		} else {
			log.Infof("Fee paid at %s", dc.acct.url)
			// dc.acct.pay() and c.authDEX????
			dc.acct.pay()
			_, err = c.authDEX(dc)
			if err != nil {
				log.Errorf("fee paid, but failed to authenticate connection to %s", dc.acct.url)
			}
		}
		return
	}

	trigger := confTrigger(dcrWallet, acctInfo.FeeCoin, dc.cfg.RegFeeConfirms)
	// Set up the coin waiter.
	dcrID, _ := dex.BipSymbolID("dcr")
	c.wait(dcrID, trigger, func(err error) {
		if err != nil {
			log.Errorf("reFee %s - waiter error: %v", dc.acct.url, err)
			return
		}
		err = c.notifyFee(dc, acctInfo.FeeCoin)
		if err != nil {
			log.Errorf("reFee %s - notifyfee error: %v", dc.acct.url, err)
			return
		}
		dc.acct.pay()
		log.Infof("waiter: Fee paid at %s", dc.acct.url)
		// New account won't have any active negotiations, so OK to discard first
		// first return value from authDEX.
		_, err = c.authDEX(dc)
		if err != nil {
			log.Errorf("fee paid, but failed to authenticate connection to %s", dc.acct.url)
		}
	})
}

// Prepare trackers based on orders stored as active in the database.
func (c *Core) dbTrackers(dc *dexConnection) (map[order.OrderID]*trackedTrade, error) {
	// Prepare active orders, according to the DB.
	dbOrders, err := c.db.ActiveDEXOrders(dc.acct.url)
	if err != nil {
		return nil, fmt.Errorf("database error when fetching orders for %s: %x", dc.acct.url, err)
	}
	cancels := make(map[order.OrderID]*order.CancelOrder)
	cancelPreimages := make(map[order.OrderID]order.Preimage)
	trackers := make(map[order.OrderID]*trackedTrade, len(dbOrders))
	for _, dbOrder := range dbOrders {
		oid := dbOrder.Order.ID()
		var preImg order.Preimage
		copy(preImg[:], dbOrder.MetaData.Proof.Preimage)
		if co, ok := dbOrder.Order.(*order.CancelOrder); ok {
			cancels[co.TargetOrderID] = co
			cancelPreimages[co.TargetOrderID] = preImg
		} else {
			trackers[oid] = newTrackedTrade(dbOrder.Order, dc, preImg)
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
	return trackers, nil
}

// connectDEX creates and connects a *dexConnection, but does not authenticate the
// connection through the 'connect' route.
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
		RpcCert:  c.certs[uri],
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
			books:      make(map[string]*book.OrderBook),
			acct:       newDEXAccount(acctInfo),
			marketMap:  marketMap,
		}
		dc.trades, reqErr = c.dbTrackers(dc)
		if reqErr != nil {
			connChan <- nil
			return
		}
		dc.refreshMarkets()
		connChan <- dc
		c.wg.Add(1)
		// Listen for incoming messages.
		log.Infof("Connected to DEX %s and listening for messages.", uri)
		go c.listen(dc)

	})
	if err != nil {
		log.Errorf("error sending 'config' request: %v", err)
		reqErr = err
		connChan <- nil
	}
	return <-connChan, reqErr
}

// handleReconnect is called when a WsConn indicates that a lost connection has
// been re-established.
func (c *Core) handleReconnect(uri string) {
	log.Infof("DEX at %s has reconnected", uri)
}

// handleOrderBookMsg is called when an orderbook response is received.
func handleOrderBookMsg(_ *Core, dc *dexConnection, msg *msgjson.Message) error {
	snapshot := new(msgjson.OrderBook)
	err := msg.UnmarshalResult(snapshot)
	if err != nil {
		return err
	}

	if snapshot.MarketID == "" {
		return fmt.Errorf("snapshot market id cannot be an empty string")
	}

	ob := book.NewOrderBook()
	err = ob.Sync(snapshot)
	if err != nil {
		return err
	}

	dc.booksMtx.Lock()
	dc.books[snapshot.MarketID] = ob
	dc.booksMtx.Unlock()

	return nil
}

// handleBookOrderMsg is called when a book_order notification is received.
func handleBookOrderMsg(_ *Core, dc *dexConnection, msg *msgjson.Message) error {
	var note msgjson.BookOrderNote
	err := msg.Unmarshal(&note)
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
func handleUnbookOrderMsg(_ *Core, dc *dexConnection, msg *msgjson.Message) error {
	var note msgjson.UnbookOrderNote
	err := msg.Unmarshal(&note)
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

// handleEpochOrderMsg is called when an epoch_order notification is
// received.
func handleEpochOrderMsg(_ *Core, dc *dexConnection, msg *msgjson.Message) error {
	var note msgjson.EpochOrderNote
	err := msg.Unmarshal(&note)
	if err != nil {
		return fmt.Errorf("epoch order note unmarshal error: %v", err)
	}

	dc.booksMtx.Lock()
	defer dc.booksMtx.Unlock()

	ob, ok := dc.books[note.MarketID]
	if !ok {
		return fmt.Errorf("no order book found with market id %q",
			note.MarketID)
	}

	return ob.Enqueue(&note)
}

// routeHandler is a handler for a message from the DEX.
type routeHandler func(*Core, *dexConnection, *msgjson.Message) error

var reqHandlers = map[string]routeHandler{
	msgjson.MatchProofRoute:  nil,
	msgjson.PreimageRoute:    handlePreimageRequest,
	msgjson.MatchRoute:       handleMatchRoute,
	msgjson.AuditRoute:       nil,
	msgjson.RedemptionRoute:  nil,
	msgjson.RevokeMatchRoute: nil,
	msgjson.SuspensionRoute:  nil,
}

var noteHandlers = map[string]routeHandler{
	msgjson.BookOrderRoute:   handleBookOrderMsg,
	msgjson.EpochOrderRoute:  handleEpochOrderMsg,
	msgjson.UnbookOrderRoute: handleUnbookOrderMsg,
}

var respHandlers = map[string]routeHandler{
	msgjson.OrderBookRoute: handleOrderBookMsg,
}

// listen monitors the DEX websocket connection for server requests and
// notifications.
func (c *Core) listen(dc *dexConnection) {
	defer c.wg.Done()
	msgs := dc.MessageSource()
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
				handler, found = respHandlers[msg.Route]
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
		case <-c.ctx.Done():
			break out
		}
	}
}

// handlePreimageRequest handles a DEX-originating request for an order
// preimage.
func handlePreimageRequest(_ *Core, dc *dexConnection, msg *msgjson.Message) error {
	req := new(msgjson.PreimageRequest)
	err := msg.Unmarshal(req)
	if err != nil {
		return fmt.Errorf("preimage request parsing error: %v", err)
	}

	var oid order.OrderID
	copy(oid[:], req.OrderID)
	tracker, preImg, _ := dc.findOrder(oid)
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
	return nil
}

// handleMatchRoute processes the DEX-originating match route request,
// indicating that a match has been made and needs to be negotiated.
func handleMatchRoute(c *Core, dc *dexConnection, msg *msgjson.Message) error {
	dumpErr := func(s string, a ...interface{}) error {
		customPart := fmt.Sprintf(s, a...)
		return fmt.Errorf("match route error for notification from %s for match data %s: %s",
			dc.acct.url, string(msg.Payload), customPart)
	}
	msgMatches := make([]*msgjson.Match, 0)
	err := msg.Unmarshal(&msgMatches)
	if err != nil {
		return dumpErr("match request parsing error: %v", err)
	}
	var acks []msgjson.Acknowledgement
	// The server will only accept the match acknowledgments
	// ([]msgjson.Acknowledgement) when all are present, so if an error occurs on
	// processing even one match, we might as well abandon everything.
	for _, msgMatch := range msgMatches {
		var oid order.OrderID
		copy(oid[:], msgMatch.OrderID)

		// Run the appropriate function under lock to prevent concurrent access to
		// trades field and underlying trackedTrades.
		tracker, _, isCancel := dc.findOrder(oid)
		if tracker == nil {
			return dumpErr("order %s not found for match route", oid)
		}
		var err error
		if isCancel {
			err = tracker.processCancelMatch(msgMatch)
		} else {
			err = tracker.negotiate(c.ctx, msgMatch)
		}
		if err != nil {
			return dumpErr(err.Error())
		}
		if err != nil {
			return dumpErr(err.Error())
		}
		sigMsg, err := msgMatch.Serialize()
		if err != nil {
			return dumpErr(err.Error())
		}
		sig, err := dc.acct.sign(sigMsg)
		if err != nil {
			return dumpErr(err.Error())
		}
		acks = append(acks, msgjson.Acknowledgement{
			MatchID: msgMatch.MatchID,
			Sig:     sig,
		})
	}
	resp, err := msgjson.NewResponse(msg.ID, acks, nil)
	if err != nil {
		return dumpErr(err.Error())
	}
	err = dc.Send(resp)
	if err != nil {
		dumpErr(err.Error())
	}
	return nil
}

// removeWaiter removes a coinWaiter from the map.
func (c *Core) removeWaiter(id uint64) {
	c.waiterMtx.Lock()
	delete(c.waiters, id)
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
	defer c.waiterMtx.Unlock()
	for id, waiter := range c.waiters {
		if waiter.assetID != assetID {
			continue
		}
		go func(id uint64, waiter *coinWaiter) {
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

// confTrigger is a Core.wait trigger that checks a coin ID for a minimum number
// of confirmations.
func confTrigger(wallet *xcWallet, coinID []byte, reqConfs uint16) func() (bool, error) {
	return func() (bool, error) {
		confs, err := wallet.Confirmations(coinID)
		if err != nil {
			return false, fmt.Errorf("Error getting confirmations for %s: %v", hex.EncodeToString(coinID), err)
		}
		return confs >= uint32(reqConfs), nil
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
