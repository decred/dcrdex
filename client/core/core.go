// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package core

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"sync"
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
	srvacct "decred.org/dcrdex/server/account"
	"github.com/decred/dcrd/dcrec/secp256k1/v2"
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
	// LoggerMaker is a logger for the core to use. Having the logger as an
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
	waiters       map[string]coinWaiter
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
		cfg:           cfg,
		db:            db,
		certs:         cfg.Certs,
		conns:         make(map[string]*dexConnection),
		wallets:       make(map[uint32]*xcWallet),
		net:           cfg.Net,
		loggerMaker:   cfg.LoggerMaker,
		waiters:       make(map[string]coinWaiter),
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

func (c *Core) encryptionKey(pw string) (encrypt.Crypter, error) {
	keyParams, err := c.db.EncryptedKey()
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
		err := wallet.Connect()
		if err != nil {
			return nil, fmt.Errorf("Connect error: %v", err)
		}
	}
	return wallet, nil
}

// Wallets creates a slice of WalletStatus for all known wallets.
func (c *Core) Wallets() []*WalletStatus {
	c.walletMtx.RLock()
	defer c.walletMtx.RUnlock()
	stats := make([]*WalletStatus, 0, len(c.wallets))
	for assetID, wallet := range c.wallets {
		on, open := wallet.status()
		stats = append(stats, &WalletStatus{
			AssetID: assetID,
			Symbol:  unbip(assetID),
			Open:    open,
			Running: on,
		})
	}
	return stats
}

// user is a thread-safe getter for the User.
func (c *Core) User() *User {
	c.userMtx.RLock()
	defer c.userMtx.RUnlock()
	return c.user
}

// refreshUser is a thread-safe way to update the current User. This method
// should be called after adding wallets and DEXes.
func (c *Core) refreshUser() {
	k, _ := c.db.EncryptedKey()
	u := &User{
		Wallets:     c.Wallets(),
		Markets:     c.Markets(),
		Initialized: len(k) > 0,
	}
	c.userMtx.Lock()
	c.user = u
	c.userMtx.Unlock()
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
		return fmt.Errorf("%s wallet already exists", unbip(dbWallet.AssetID))
	}

	wallet, err := c.loadWallet(dbWallet)
	if err != nil {
		return fmt.Errorf("error loading wallet for %d -> %s: %v", dbWallet.AssetID, unbip(dbWallet.AssetID), err)
	}

	err = wallet.Connect()
	if err != nil {
		return fmt.Errorf("Error connecting wallet: %v", err)
	}

	// Store the wallet in the database.
	err = c.db.UpdateWallet(dbWallet)
	if err != nil {
		wallet.waiter.Stop()
		return fmt.Errorf("error storing wallet credentials: %v", err)
	}

	c.refreshUser()

	return nil
}

// loadWallet uses the data from the database to construct a new exchange
// wallet. The returned wallet is running but not connected.
func (c *Core) loadWallet(dbWallet *db.Wallet) (*xcWallet, error) {
	wallet := &xcWallet{AssetID: dbWallet.AssetID}
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
	wallet.waiter = dex.NewStartStopWaiter(w)
	wallet.waiter.Start(c.ctx)

	c.walletMtx.Lock()
	c.wallets[dbWallet.AssetID] = wallet
	c.walletMtx.Unlock()
	return wallet, nil
}

// WalletStatus returns 1) whether the wallet exists, 2) if it's currently
// running, and 3) whether it's currently open (unlocked).
func (c *Core) WalletStatus(assetID uint32) (has, running, open bool) {
	c.walletMtx.Lock()
	defer c.walletMtx.Unlock()
	wallet, has := c.wallets[assetID]
	if !has {
		log.Tracef("wallet status requested for unknown asset %d -> %s", assetID, unbip(assetID))
		return
	}
	running, open = wallet.status()
	return
}

// OpenWallet opens (unlocks) the wallet for use.
func (c *Core) OpenWallet(assetID uint32, pw string) error {
	wallet, err := c.connectedWallet(assetID)
	if err != nil {
		return fmt.Errorf("wallet error for %d -> %s: %v", assetID, unbip(assetID), err)
	}
	err = wallet.Unlock(pw, aYear)
	if err != nil {
		return fmt.Errorf("OpenWallet: %v", err)
	}
	return nil
}

// PreRegister creates a connection to the specified DEX and fetches the
// registration fee. The connection is left open and stored temporarily while
// registration is completed.
func (c *Core) PreRegister(dex string) (uint64, error) {
	c.connMtx.RLock()
	dc, found := c.conns[dex]
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
	if c.pendingTimer == nil {
		c.pendingTimer = time.AfterFunc(time.Minute*5, func() {
			c.connMtx.Lock()
			defer c.connMtx.Unlock()
			pendingDEX := c.pendingReg
			if pendingDEX != nil && pendingDEX.acct.url == dc.acct.url {
				pendingDEX.Close()
				c.pendingReg = nil
			}
		})
	} else {
		c.pendingTimer.Stop()
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
	ai, err := c.db.Account(form.DEX)
	if err == nil {
		return fmt.Errorf("account already exists for %s", form.DEX), nil
	}

	// Get a connection to the dex.
	ai = &db.AccountInfo{
		URL: form.DEX,
	}
	c.connMtx.Lock()
	dc, found := c.conns[form.DEX]
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
	acctID := srvacct.NewID(pubKey.SerializeCompressed())

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
		return fmt.Errorf("'register' requst error: %v", err), nil
	}
	err = extractError(errChan, requestTimeout)
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
	// Pay the registration fee.
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

	// Notify the server of the fee coin once there are enough confirmations.
	req := &msgjson.NotifyFee{
		AccountID: acctID[:],
		CoinID:    coin.ID(),
	}
	// We'll need this to validate the server's acknowledgement.
	reqB, err := req.Serialize()
	if err != nil {
		log.Warnf("fee paid with coin %x, but unable to serialize notifyfee request so server signature cannot be verified")
		// Dont quit. The fee is already paid, so follow through if possible.
	}

	// Set up the coin waiter.
	errChan = make(chan error, 1)
	c.waiterMtx.Lock()
	c.waiters[form.DEX] = coinWaiter{
		conn:  dc,
		asset: regAsset,
		coin:  coin,
		// DRAFT NOTE: Hard-coded to 1 for testing and until the 'config' response
		// structure includes the reg fee confirmation requirements.
		confs:   uint32(dc.cfg.RegFeeConfirms),
		route:   msgjson.NotifyFeeRoute,
		privKey: privKey,
		req:     req,
		f: func(msg *msgjson.Message, waiterErr error) {
			var err error
			if waiterErr != nil {
				errChan <- waiterErr
				return
			}
			ack := new(msgjson.Acknowledgement)
			err = msg.UnmarshalResult(ack)
			if err != nil {
				errChan <- fmt.Errorf("notify fee result json decode error: %v", err)
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
				_, err = checkSigS256(reqB, regRes.DEXPubKey, sig)
				if err != nil {
					log.Warnf("account was registered, but DEX signature could not be verified: %v", err)
				}
			} else {
				log.Warnf("Marking account as paid, even though the server's signature could not be validated.")
			}
			err = c.db.AccountPaid(&db.AccountProof{
				URL:   form.DEX,
				Stamp: req.Time,
				Sig:   sig,
			})
			if err != nil {
				errChan <- err
				return
			}
			// New account won't have any active negotiations, so OK to discard first
			// first return value.
			_, err = c.authDEX(crypter, dc)
			errChan <- err
		},
	}
	c.waiterMtx.Unlock()
	return nil, errChan
}

// InitializeClient sets the initial app-wide password for the client.
func (c *Core) InitializeClient(pw string) error {
	if pw == "" {
		return fmt.Errorf("empty password not allowed")
	}
	privKey, err := secp256k1.GeneratePrivateKey()
	if err != nil {
		return fmt.Errorf("error generating new private key: %v", err)
	}
	encKey, err := encrypt.NewCrypter(pw).Encrypt(privKey.Serialize())
	if err != nil {
		return fmt.Errorf("key encryption error: %v", err)
	}
	err = c.db.StoreEncryptedKey(encKey)
	if err != nil {
		return fmt.Errorf("error storing encrypted key: %v", err)
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
	for _, dexConn := range c.conns {
		dc := dexConn
		if dc.acct.authed() {
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			n, err := c.authDEX(crypter, dc)
			if err != nil {
				errs = append(errs, fmt.Sprintf("%s: %v", dc.acct.url, err))
				return
			}
			dc.matchMtx.Lock()
			negotiations = append(negotiations, n...)
			dc.matchMtx.Unlock()
		}()
	}
	wg.Wait()
	if errs != nil {
		err = fmt.Errorf("authorization errors: %s", strings.Join(errs, ", "))
	}
	return negotiations, err
}

// authDEX authenticates the connection for a DEX.
func (c *Core) authDEX(crypter encrypt.Crypter, dc *dexConnection) ([]Negotiation, error) {
	// Decrypt the account private key.
	err := dc.acct.unlock(crypter)
	if err != nil {
		return nil, fmt.Errorf("error unlocking account for %s: %v", dc.acct.url, err)
	}
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
	err = extractError(errChan, requestTimeout)
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
		negotiator, err := negotiate(c.ctx, msgMatch)
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

func (c *Core) Balance(uint32) (uint64, error) {
	return 0, nil
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
		a := acct
		wg.Add(1)
		go func() {
			defer wg.Done()
			dc, err := c.connectDEX(a)
			if err != nil {
				log.Errorf("error adding DEX %s: %v", a, err)
				return
			}
			c.connMtx.Lock()
			c.conns[a.URL] = dc
			c.connMtx.Unlock()
		}()
	}
	// If there were accounts, wait until they are loaded and log a messsage.
	if len(accts) > 0 {
		go func() {
			wg.Wait()
			c.connMtx.RLock()
			log.Infof("Successfully connected to %d out of %d "+
				"DEX servers", len(c.conns), len(accts))
			c.connMtx.RUnlock()
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
	err = conn.Connect(c.ctx)
	// If the initial connection returned an error, shut it down to kill the
	// auto-reconnect cycle.
	if err != nil {
		conn.Close()
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
			WsConn:  conn,
			assets:  assets,
			cfg:     dexCfg,
			books:   make(map[string]*book.OrderBook),
			acct:    newDEXAccount(acctInfo.URL, acctInfo.EncKey, acctInfo.DEXPubKey),
			markets: markets,
		}
		connChan <- dc
		c.wg.Add(1)
		// Listen for incoming messages.
		go c.listen(dc)
	})
	if err != nil {
		log.Errorf("error sending 'config' request: %v", err)
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

// removeWaiter removes a coinWaiter from the map.
func (c *Core) removeWaiter(dex string) {
	c.waiterMtx.Lock()
	delete(c.waiters, dex)
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
	for d, w := range c.waiters {
		waiter := w
		if waiter.asset.ID != assetID {
			continue
		}
		dex := d
		go func() {
			confs, err := waiter.coin.Confirmations()
			if err != nil {
				waiter.f(nil, fmt.Errorf("Error getting confirmations for %x: %v", waiter.coin.ID(), err))
				c.removeWaiter(dex)
				return
			}
			if confs >= waiter.confs {
				defer c.removeWaiter(dex)
				// Sign the request and send it.
				req := waiter.req
				err := stamp(waiter.privKey, req)
				if err != nil {
					waiter.f(nil, err)
					return
				}
				msg, err := msgjson.NewRequest(waiter.conn.NextID(), waiter.route, req)
				if err != nil {
					waiter.f(nil, fmt.Errorf("failed to create notifyfee request: %v", err))
					return
				}
				err = waiter.conn.Request(msg, func(msg *msgjson.Message) {
					waiter.f(msg, nil)
				})
				if err != nil {
					waiter.f(nil, fmt.Errorf("'notifyfee' request error: %v", err))
				}
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

// extractError extracts the error from the channel with a timeout.
func extractError(errChan <-chan error, delay time.Duration) error {
	select {
	case err := <-errChan:
		return err
	case <-time.NewTimer(delay).C:
		return fmt.Errorf("timed out waiting for 'connect' response.")
	}
}
