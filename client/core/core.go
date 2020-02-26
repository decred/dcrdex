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
	connMaster  *dex.ConnectionMaster
	assets      map[uint32]*dex.Asset
	cfg         *msgjson.ConfigResult
	acct        *dexAccount
	booksMtx    sync.RWMutex
	books       map[string]*book.OrderBook
	markets     []*Market
	matchMtx    sync.RWMutex
	negotiators map[order.MatchID]*matchNegotiator
}

// coinWaiter is a message waiting to be stamped, signed, and sent once a
// specified coin has the requisite confirmations. This type is similar to
// dcrdex/server/coinwaiter.Waiter, but is different enough to warrant a
// separate type.
type coinWaiter struct {
	assetID uint32
	trigger func() (bool, error)
	action  func(error)
}

func coinConfirmationTrigger(coin asset.Coin, reqConfs uint32) func() (bool, error) {
	return func() (bool, error) {
		confs, err := coin.Confirmations()
		if err != nil {
			return false, fmt.Errorf("Error getting confirmations for %x: %v", coin.ID(), err)
		}
		return confs >= reqConfs, nil
	}
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
	}
	log.Tracef("new client core created")
	return core, nil
}

// Run runs the core. Satisfies the runner.Runner interface.
func (c *Core) Run(ctx context.Context) {
	log.Infof("started DEX client core")
	// Store the context as a field, since we will need to spawn new DEX threads
	// when new accounts are registered.
	c.ctx = ctx
	c.initialize()
	<-ctx.Done()
	c.wg.Wait()
	log.Infof("DEX client core off")
}

// Markets returns a map of known markets.
func (c *Core) Markets() map[string][]*Market {
	c.connMtx.RLock()
	defer c.connMtx.RUnlock()
	infos := make(map[string][]*Market, len(c.conns))
	for uri, dc := range c.conns {
		infos[uri] = dc.markets
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
	crypter, err := encrypt.Deserialize(pw, keyParams)
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
		Markets:     c.Markets(),
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
	pwB, err := crypter.Decrypt(wallet.encPW)
	if err != nil {
		return fmt.Errorf("OpenWallet: decryption error: %v", err)
	}
	err = wallet.Unlock(string(pwB), aYear)
	if err != nil {
		return fmt.Errorf("wallet unlock error: %v", err)
	}
	dcrID, _ := dex.BipSymbolID("dcr")
	if assetID == dcrID {
		go c.checkUnpaidFees(wallet)
	}
	c.refreshUser()
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
		dc.matchMtx.RLock()
		defer dc.matchMtx.RUnlock()
		for _, neg := range dc.negotiators {
			prefix := neg.Order().Prefix()
			if prefix.BaseAsset == assetID || prefix.QuoteAsset == assetID {
				return fmt.Errorf("cannot lock %s wallet with active negotiations", unbip(assetID))
			}
		}
	}
	return wallet.lock()
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
	c.pendingReg = dc
	c.connMtx.Unlock()
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
	c.connMtx.Lock()
	dc, found := c.conns[ai.URL]
	// If it's not already in the map, see if there is a pre-registration pending.
	if !found && c.pendingReg != nil && c.pendingReg.acct.url == form.DEX {
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

	trigger := func() (bool, error) {
		confs, err := coin.Confirmations()
		if err != nil {
			return false, fmt.Errorf("Error getting confirmations for %x: %v", coin.ID(), err)
		}
		return confs >= uint32(dc.cfg.RegFeeConfirms), nil
	}
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
	crypter := encrypt.NewCrypter(pw)
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
			// Using the matchMtx here to synchronize access to the errs slice too.
			dc.matchMtx.Lock()
			defer dc.matchMtx.Unlock()
			if err != nil {
				errs = append(errs, fmt.Sprintf("%s: %v", dc.acct.url, err))
				return
			}
			negotiations = append(negotiations, n...)
		}(dc)
	}
	wg.Wait()
	if len(errs) > 0 {
		err = fmt.Errorf("authorization errors: %s", strings.Join(errs, ", "))
	}
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
		log.Warnf("fee paid with coin %x, but unable to serialize notifyfee request so server signature cannot be verified")
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
		sig := []byte{}
		if len(reqB) > 0 {
			// redefining err here, since these errors won't be sent over the
			// response channel.
			var err error
			sig, err = hex.DecodeString(ack.Sig)
			if err != nil {
				log.Warnf("account was registered, but server's signature could not be decoded: %v", err)
				return
			}
			_, err = checkSigS256(reqB, dc.acct.dexPubKey.Serialize(), sig)
			if err != nil {
				log.Warnf("account was registered, but DEX signature could not be verified: %v", err)
			}
		} else {
			log.Warnf("Marking account as paid, even though the server's signature could not be validated.")
		}
		errChan <- c.db.AccountPaid(&db.AccountProof{
			URL:   dc.acct.url,
			Stamp: req.Time,
			Sig:   sig,
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
	dc.matchMtx.Lock()
	defer dc.matchMtx.Unlock()
	for _, msgMatch := range result.Matches {
		// TODO: Re-create the order.
		negotiator, err := negotiate(c.ctx, msgMatch, &order.LimitOrder{})
		if err != nil {
			errs = append(errs, msgMatch.MatchID.String()+": "+err.Error())
			continue
		}
		negotiations = append(negotiations, negotiator)
		dc.negotiators[negotiator.matchID] = negotiator
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
			log.Errorf("reFee %s - notifyfee error: %v", err)
		}
		log.Infof("Fee paid at %s", dc.acct.url)
		return
	}

	trigger := func() (bool, error) {
		confs, err := dcrWallet.Confirmations(acctInfo.FeeCoin)
		if err != nil {
			return false, fmt.Errorf("Error getting confirmations for %x: %v", acctInfo.FeeCoin, err)
		}
		return confs >= uint32(dc.cfg.RegFeeConfirms), nil
	}
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
		log.Infof("Fee paid at %s", dc.acct.url)
		// New account won't have any active negotiations, so OK to discard first
		// first return value from authDEX.
		_, err = c.authDEX(dc)
		if err != nil {
			log.Errorf("fee paid, but failed to authenticate connection to %s", dc.acct.url)
		}
	})
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

		markets := make([]*Market, 0, len(dexCfg.Markets))
		for _, mkt := range dexCfg.Markets {
			base, quote := assets[mkt.Base], assets[mkt.Quote]
			markets = append(markets, &Market{
				BaseID:          base.ID,
				BaseSymbol:      base.Symbol,
				QuoteID:         quote.ID,
				QuoteSymbol:     quote.Symbol,
				EpochLen:        mkt.EpochLen,
				StartEpoch:      mkt.StartEpoch,
				MarketBuyBuffer: mkt.MarketBuyBuffer,
			})
		}

		// Create the dexConnection and add it to the map.
		dc := &dexConnection{
			WsConn:     conn,
			connMaster: connMaster,
			assets:     assets,
			cfg:        dexCfg,
			books:      make(map[string]*book.OrderBook),
			acct:       newDEXAccount(acctInfo),
			markets:    markets,
		}
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
func (c *Core) handleOrderBookMsg(dc *dexConnection, msg *msgjson.Message) error {
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
func (c *Core) handleBookOrderMsg(dc *dexConnection, msg *msgjson.Message) error {
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
func (c *Core) handleUnbookOrderMsg(dc *dexConnection, msg *msgjson.Message) error {
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
func (c *Core) handleEpochOrderMsg(dc *dexConnection, msg *msgjson.Message) error {
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
					err = c.handleEpochOrderMsg(dc, msg)
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
		log.Errorf("%s wallet is reporting a failed state: %v", nodeErr)
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
