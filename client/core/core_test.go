package core

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"sync"
	"testing"
	"time"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/client/comms"
	"decred.org/dcrdex/client/db"
	dbtest "decred.org/dcrdex/client/db/test"
	book "decred.org/dcrdex/client/order"
	"decred.org/dcrdex/dex"
	dexbtc "decred.org/dcrdex/dex/btc"
	"decred.org/dcrdex/dex/calc"
	dexdcr "decred.org/dcrdex/dex/dcr"
	"decred.org/dcrdex/dex/encode"
	"decred.org/dcrdex/dex/encrypt"
	"decred.org/dcrdex/dex/msgjson"
	"decred.org/dcrdex/dex/order"
	ordertest "decred.org/dcrdex/dex/order/test"
	"decred.org/dcrdex/dex/wait"
	"decred.org/dcrdex/server/account"
	"github.com/decred/dcrd/crypto/blake256"
	"github.com/decred/dcrd/dcrec/secp256k1/v2"
	"github.com/decred/slog"
)

var (
	tCtx context.Context
	tDCR = &dex.Asset{
		ID:       42,
		Symbol:   "dcr",
		SwapSize: dexdcr.InitTxSize,
		FeeRate:  10,
		LotSize:  1e7,
		RateStep: 100,
		SwapConf: 1,
		FundConf: 1,
	}

	tBTC = &dex.Asset{
		ID:       0,
		Symbol:   "btc",
		SwapSize: dexbtc.InitTxSize,
		FeeRate:  2,
		LotSize:  1e6,
		RateStep: 10,
		SwapConf: 1,
		FundConf: 1,
	}
	tDexPriv       *secp256k1.PrivateKey
	tDexKey        *secp256k1.PublicKey
	tPW                   = "dexpw"
	wPW                   = "walletpw"
	tDexUrl               = "somedex.tld"
	tDcrBtcMktName        = "dcr_btc"
	tErr                  = fmt.Errorf("test error")
	tFee           uint64 = 1e8
)

type tMsg = *msgjson.Message
type msgFunc = func(*msgjson.Message)

func uncovertAssetInfo(ai *dex.Asset) *msgjson.Asset {
	return &msgjson.Asset{
		Symbol:   ai.Symbol,
		ID:       ai.ID,
		LotSize:  ai.LotSize,
		RateStep: ai.RateStep,
		FeeRate:  ai.FeeRate,
		SwapSize: ai.SwapSize,
		SwapConf: uint16(ai.SwapConf),
		FundConf: uint16(ai.FundConf),
	}
}

func makeAcker(serializer func(msg *msgjson.Message) msgjson.Signable) func(msg *msgjson.Message, f msgFunc) error {
	return func(msg *msgjson.Message, f msgFunc) error {
		signable := serializer(msg)
		sigMsg, _ := signable.Serialize()
		sig, _ := tDexPriv.Sign(sigMsg)
		ack := &msgjson.Acknowledgement{
			Sig: sig.Serialize(),
		}
		resp, _ := msgjson.NewResponse(msg.ID, ack, nil)
		f(resp)
		return nil
	}
}

var (
	initAcker = makeAcker(func(msg *msgjson.Message) msgjson.Signable {
		init := new(msgjson.Init)
		msg.Unmarshal(init)
		return init
	})
	redeemAcker = makeAcker(func(msg *msgjson.Message) msgjson.Signable {
		redeem := new(msgjson.Redeem)
		msg.Unmarshal(redeem)
		return redeem
	})
)

type TWebsocket struct {
	mtx        sync.RWMutex
	id         uint64
	sendErr    error
	reqErr     error
	connectErr error
	msgs       <-chan *msgjson.Message
	handlers   map[string][]func(*msgjson.Message, msgFunc) error
}

func newTWebsocket() *TWebsocket {
	return &TWebsocket{
		msgs:     make(<-chan *msgjson.Message),
		handlers: make(map[string][]func(*msgjson.Message, msgFunc) error),
	}
}

func tNewAccount() *dexAccount {
	privKey, _ := secp256k1.GeneratePrivateKey()
	return &dexAccount{
		url:       tDexUrl,
		encKey:    privKey.Serialize(),
		dexPubKey: tDexKey,
		privKey:   privKey,
		feeCoin:   []byte("somecoin"),
	}
}

func testDexConnection() (*dexConnection, *TWebsocket, *dexAccount) {
	conn := newTWebsocket()
	acct := tNewAccount()
	mkt := &Market{
		Name:            tDcrBtcMktName,
		BaseID:          tDCR.ID,
		BaseSymbol:      tDCR.Symbol,
		QuoteID:         tBTC.ID,
		QuoteSymbol:     tBTC.Symbol,
		EpochLen:        60000,
		MarketBuyBuffer: 1.1,
	}
	return &dexConnection{
		WsConn: conn,
		acct:   acct,
		assets: map[uint32]*dex.Asset{
			tDCR.ID: tDCR,
			tBTC.ID: tBTC,
		},
		books: make(map[string]*book.OrderBook),
		cfg: &msgjson.ConfigResult{
			CancelMax:        0.8,
			BroadcastTimeout: 5 * 60 * 1000,
			Assets: []msgjson.Asset{
				*uncovertAssetInfo(tDCR),
				*uncovertAssetInfo(tBTC),
			},
			Markets: []msgjson.Market{
				{
					Name:            tDcrBtcMktName,
					Base:            tDCR.ID,
					Quote:           tBTC.ID,
					EpochLen:        60000,
					MarketBuyBuffer: 1.1,
				},
			},
			Fee: tFee,
		},
		marketMap: map[string]*Market{tDcrBtcMktName: mkt},
		trades:    make(map[order.OrderID]*trackedTrade),
	}, conn, acct
}

func (conn *TWebsocket) getHandlers(route string) []func(*msgjson.Message, msgFunc) error {
	conn.mtx.RLock()
	defer conn.mtx.RUnlock()
	return conn.handlers[route]
}

func (conn *TWebsocket) queueResponse(route string, handler func(*msgjson.Message, msgFunc) error) {
	handlers := conn.getHandlers(route)
	if handlers == nil {
		handlers = make([]func(*msgjson.Message, msgFunc) error, 0, 1)
	}
	conn.mtx.Lock()
	conn.handlers[route] = append(handlers, handler)
	conn.mtx.Unlock()
}

func (conn *TWebsocket) NextID() uint64 {
	conn.mtx.Lock()
	defer conn.mtx.Unlock()
	conn.id++
	return conn.id
}
func (conn *TWebsocket) Send(msg *msgjson.Message) error { return conn.sendErr }
func (conn *TWebsocket) Request(msg *msgjson.Message, f msgFunc) error {
	handlers := conn.getHandlers(msg.Route)
	if len(handlers) > 0 {
		handler := handlers[0]
		conn.handlers[msg.Route] = handlers[1:]
		return handler(msg, f)
	}
	return conn.reqErr
}
func (conn *TWebsocket) MessageSource() <-chan *msgjson.Message { return conn.msgs }
func (conn *TWebsocket) Connect(context.Context) (error, *sync.WaitGroup) {
	return conn.connectErr, &sync.WaitGroup{}
}

type TDB struct {
	updateWalletErr error
	acct            *db.AccountInfo
	acctErr         error
	getErr          error
	storeErr        error
	encKeyErr       error
	accts           []*db.AccountInfo
	updateOrderErr  error
}

func (db *TDB) Run(context.Context) {}

func (db *TDB) ListAccounts() ([]string, error) {
	return nil, nil
}

func (db *TDB) Accounts() ([]*db.AccountInfo, error) {
	return db.accts, nil
}

func (db *TDB) Account(url string) (*db.AccountInfo, error) {
	return db.acct, db.acctErr
}

func (db *TDB) CreateAccount(ai *db.AccountInfo) error {
	return nil
}

func (db *TDB) UpdateOrder(m *db.MetaOrder) error {
	return db.updateOrderErr
}

func (db *TDB) ActiveDEXOrders(dex string) ([]*db.MetaOrder, error) {
	return nil, nil
}

func (db *TDB) ActiveOrders() ([]*db.MetaOrder, error) {
	return nil, nil
}

func (db *TDB) AccountOrders(dex string, n int, since uint64) ([]*db.MetaOrder, error) {
	return nil, nil
}

func (db *TDB) Order(order.OrderID) (*db.MetaOrder, error) {
	return nil, nil
}

func (db *TDB) MarketOrders(dex string, base, quote uint32, n int, since uint64) ([]*db.MetaOrder, error) {
	return nil, nil
}

func (db *TDB) UpdateMatch(m *db.MetaMatch) error {
	return nil
}

func (db *TDB) ActiveMatches() ([]*db.MetaMatch, error) {
	return nil, nil
}

func (db *TDB) UpdateWallet(wallet *db.Wallet) error {
	return db.updateWalletErr
}

func (db *TDB) Wallets() ([]*db.Wallet, error) {
	return nil, nil
}

func (db *TDB) AccountPaid(proof *db.AccountProof) error {
	return nil
}

func (db *TDB) SaveNotification(*db.Notification) error        { return nil }
func (db *TDB) NotificationsN(int) ([]*db.Notification, error) { return nil, nil }

func (db *TDB) Store(k string, b []byte) error {
	return db.storeErr
}

func (db *TDB) Get(k string) ([]byte, error) {
	if k == keyParamsKey {
		return nil, db.encKeyErr
	}
	return nil, db.getErr
}

func (db *TDB) Backup() error {
	return nil
}

func (db *TDB) AckNotification(id []byte) error { return nil }

type tCoin struct {
	id       []byte
	confs    uint32
	confsErr error
	val      uint64
}

func (c *tCoin) ID() dex.Bytes {
	return c.id
}

func (c *tCoin) String() string {
	return hex.EncodeToString(c.id)
}

func (c *tCoin) Value() uint64 {
	return c.val
}

func (c *tCoin) Confirmations() (uint32, error) {
	return c.confs, c.confsErr
}

func (c *tCoin) Redeem() dex.Bytes {
	return nil
}

type tReceipt struct {
	coin       *tCoin
	expiration time.Time
}

func (r *tReceipt) Coin() asset.Coin {
	return r.coin
}

func (r *tReceipt) Expiration() time.Time {
	return r.expiration
}

type tAuditInfo struct {
	recipient  string
	expiration time.Time
	coin       *tCoin
	secretHash []byte
}

func (ai *tAuditInfo) Recipient() string {
	return ai.recipient
}

func (ai *tAuditInfo) Expiration() time.Time {
	return ai.expiration
}

func (ai *tAuditInfo) Coin() asset.Coin {
	return ai.coin
}

func (ai *tAuditInfo) SecretHash() dex.Bytes {
	return ai.secretHash
}

type TXCWallet struct {
	mtx          sync.RWMutex
	payFeeCoin   *tCoin
	payFeeErr    error
	fundCoins    asset.Coins
	fundErr      error
	addrErr      error
	signCoinErr  error
	swapReceipts []asset.Receipt
	auditInfo    asset.AuditInfo
	auditErr     error
	redeemCoins  []dex.Bytes
	badSecret    bool
	fundedVal    uint64
	connectErr   error
	unlockErr    error
	balErr       error
}

func newTWallet(assetID uint32) (*xcWallet, *TXCWallet) {
	w := new(TXCWallet)
	return &xcWallet{
		Wallet:    w,
		connector: dex.NewConnectionMaster(w),
		AssetID:   assetID,
		lockTime:  time.Now().Add(time.Hour),
		hookedUp:  true,
	}, w
}

func (w *TXCWallet) Info() *asset.WalletInfo {
	return &asset.WalletInfo{}
}

func (w *TXCWallet) Connect(ctx context.Context) (error, *sync.WaitGroup) {
	return w.connectErr, &sync.WaitGroup{}
}

func (w *TXCWallet) Run(ctx context.Context) { <-ctx.Done() }

func (w *TXCWallet) Balance(confs uint32) (available, locked uint64, err error) {
	return 0, 0, w.balErr
}

func (w *TXCWallet) Fund(v uint64, _ *dex.Asset) (asset.Coins, error) {
	w.fundedVal = v
	return w.fundCoins, w.fundErr
}

func (w *TXCWallet) ReturnCoins(asset.Coins) error {
	return nil
}

func (w *TXCWallet) FundingCoins([]dex.Bytes) (asset.Coins, error) {
	return nil, nil
}

func (w *TXCWallet) Swap(swap *asset.Swaps, _ *dex.Asset) ([]asset.Receipt, asset.Coin, error) {
	return w.swapReceipts, &tCoin{id: []byte{0x0a, 0x0b}}, nil
}

func (w *TXCWallet) Redeem([]*asset.Redemption, *dex.Asset) ([]dex.Bytes, error) {
	return w.redeemCoins, nil
}

func (w *TXCWallet) SignMessage(asset.Coin, dex.Bytes) (pubkeys, sigs []dex.Bytes, err error) {
	return nil, nil, w.signCoinErr
}

func (w *TXCWallet) AuditContract(coinID, contract dex.Bytes) (asset.AuditInfo, error) {
	return w.auditInfo, w.auditErr
}

func (w *TXCWallet) FindRedemption(ctx context.Context, coinID dex.Bytes) (dex.Bytes, error) {
	return nil, nil
}

func (w *TXCWallet) Refund(asset.Receipt, *dex.Asset) error {
	return nil
}

func (w *TXCWallet) Address() (string, error) {
	return "", w.addrErr
}

func (w *TXCWallet) Unlock(pw string, dur time.Duration) error {
	return w.unlockErr
}

func (w *TXCWallet) Lock() error {
	return nil
}

func (w *TXCWallet) Send(address string, fee uint64, _ *dex.Asset) (asset.Coin, error) {
	w.mtx.RLock()
	defer w.mtx.RUnlock()
	return w.payFeeCoin, w.payFeeErr
}

func (w *TXCWallet) Confirmations(id dex.Bytes) (uint32, error) {
	return 0, nil
}

func (w *TXCWallet) PayFee(address string, fee uint64, nfo *dex.Asset) (asset.Coin, error) {
	return w.payFeeCoin, w.payFeeErr
}

func (w *TXCWallet) Withdraw(address string, value, feeRate uint64) (asset.Coin, error) {
	return w.payFeeCoin, w.payFeeErr
}

func (w *TXCWallet) ValidateSecret(secret, secretHash []byte) bool {
	return !w.badSecret
}

func (w *TXCWallet) setConfs(confs uint32) {
	w.mtx.Lock()
	w.payFeeCoin.confs = confs
	w.mtx.Unlock()
}

type tCrypter struct {
	encryptErr error
	decryptErr error
	recryptErr error
}

func (c *tCrypter) Encrypt(b []byte) ([]byte, error) { return b, c.encryptErr }

func (c *tCrypter) Decrypt(b []byte) ([]byte, error) { return b, c.decryptErr }

func (c *tCrypter) Serialize() []byte { return nil }

func (c *tCrypter) Close() {}

var tAssetID uint32

func randomAsset() *msgjson.Asset {
	tAssetID++
	return &msgjson.Asset{
		Symbol: "BT" + strconv.Itoa(int(tAssetID)),
		ID:     tAssetID,
	}
}

func randomMsgMarket() (baseAsset, quoteAsset *msgjson.Asset) {
	return randomAsset(), randomAsset()
}

type testRig struct {
	core    *Core
	db      *TDB
	queue   *wait.TickerQueue
	ws      *TWebsocket
	dc      *dexConnection
	acct    *dexAccount
	crypter *tCrypter
}

func newTestRig() *testRig {
	db := new(TDB)

	// Set the global waiter expiration, and start the waiter.
	txWaitExpiration = time.Millisecond * 10
	queue := wait.NewTickerQueue(time.Millisecond * 5)
	go queue.Run(tCtx)

	dc, conn, acct := testDexConnection()

	crypter := &tCrypter{}

	return &testRig{
		core: &Core{
			ctx:      tCtx,
			db:       db,
			latencyQ: queue,
			conns: map[string]*dexConnection{
				tDexUrl: dc,
			},
			loggerMaker: &dex.LoggerMaker{
				Backend:      slog.NewBackend(os.Stdout),
				DefaultLevel: slog.LevelTrace,
			},
			wallets:      make(map[uint32]*xcWallet),
			blockWaiters: make(map[uint64]*blockWaiter),
			wsConstructor: func(*comms.WsCfg) (comms.WsConn, error) {
				return conn, nil
			},
			newCrypter: func(string) encrypt.Crypter { return crypter },
			reCrypter:  func(string, []byte) (encrypt.Crypter, error) { return crypter, crypter.recryptErr },
		},
		db:      db,
		queue:   queue,
		ws:      conn,
		dc:      dc,
		acct:    acct,
		crypter: crypter,
	}
}

func tMarketID(base, quote uint32) string {
	return strconv.Itoa(int(base)) + "-" + strconv.Itoa(int(quote))
}

func TestMain(m *testing.M) {
	log = slog.NewBackend(os.Stdout).Logger("TEST")
	var shutdown context.CancelFunc
	tCtx, shutdown = context.WithCancel(context.Background())
	tDexPriv, _ = secp256k1.GeneratePrivateKey()
	tDexKey = tDexPriv.PubKey()

	doIt := func() int {
		// Not counted as coverage, must test Archiver constructor explicitly.
		defer shutdown()
		return m.Run()
	}
	os.Exit(doIt())
}

func TestMarkets(t *testing.T) {
	rig := newTestRig()
	// The test rig's dexConnection comes with a market. Clear that for this test.
	rig.dc.cfg.Markets = nil
	tCore := rig.core
	// Simulate 10 markets.
	marketIDs := make(map[string]struct{})
	for i := 0; i < 10; i++ {
		base, quote := randomMsgMarket()
		marketIDs[mktID(base.ID, quote.ID)] = struct{}{}
		cfg := rig.dc.cfg
		cfg.Markets = append(cfg.Markets, msgjson.Market{
			Name:            base.Symbol + quote.Symbol,
			Base:            base.ID,
			Quote:           quote.ID,
			EpochLen:        5000,
			MarketBuyBuffer: 1.4,
		})
		rig.dc.assets[base.ID] = convertAssetInfo(base)
		rig.dc.assets[quote.ID] = convertAssetInfo(quote)
	}
	rig.dc.refreshMarkets()

	// Just check that the information is coming through correctly.
	xcs := tCore.Exchanges()
	if len(xcs) != 1 {
		t.Fatalf("expected 1 MarketInfo, got %d", len(xcs))
	}
	assets := rig.dc.assets
	for _, xc := range xcs {
		for _, market := range xc.Markets {
			mkt := mktID(market.BaseID, market.QuoteID)
			_, found := marketIDs[mkt]
			if !found {
				t.Fatalf("market %s not found", mkt)
			}
			if assets[market.BaseID].Symbol != market.BaseSymbol {
				t.Fatalf("base symbol mismatch. %s != %s", assets[market.BaseID].Symbol, market.BaseSymbol)
			}
			if assets[market.QuoteID].Symbol != market.QuoteSymbol {
				t.Fatalf("quote symbol mismatch. %s != %s", assets[market.QuoteID].Symbol, market.QuoteSymbol)
			}
		}
	}
}

func TestDexConnectionOrderBook(t *testing.T) {
	tCore := newTestRig().core
	mid := "ob"
	dc := &dexConnection{
		books: make(map[string]*book.OrderBook),
	}

	// Ensure handleOrderBookMsg creates an order book as expected.
	oid, err := hex.DecodeString("1d6c8998e93898f872fa43f35ede17c3196c6a1a2054cb8d91f2e184e8ca0316")
	if err != nil {
		t.Fatalf("[DecodeString]: unexpected err: %v", err)
	}
	msg, err := msgjson.NewResponse(1, &msgjson.OrderBook{
		Seq:      1,
		MarketID: "ob",
		Orders: []*msgjson.BookOrderNote{
			{
				TradeNote: msgjson.TradeNote{
					Side:     msgjson.BuyOrderNum,
					Quantity: 10,
					Rate:     2,
				},
				OrderNote: msgjson.OrderNote{
					Seq:      1,
					MarketID: mid,
					OrderID:  oid,
				},
			},
		},
	}, nil)
	if err != nil {
		t.Fatalf("[NewResponse]: unexpected err: %v", err)
	}

	err = handleOrderBookMsg(tCore, dc, msg)
	if err != nil {
		t.Fatalf("[handleOrderBookMsg]: unexpected err: %v", err)
	}
	if len(dc.books) != 1 {
		t.Fatalf("expected %v order book created, got %v", 1, len(dc.books))
	}
	_, ok := dc.books[mid]
	if !ok {
		t.Fatalf("expected order book with market id %s", mid)
	}

	// Ensure handleBookOrderMsg creates a book order for the associated
	// order book as expected.
	oid, err = hex.DecodeString("d445ab685f5cf54dfebdaa05232892b4cfa453a566b5e85f62627dd6834c5c02")
	if err != nil {
		t.Fatalf("[DecodeString]: unexpected err: %v", err)
	}
	bookSeq := uint64(2)
	msg, err = msgjson.NewNotification(msgjson.BookOrderRoute, &msgjson.BookOrderNote{
		TradeNote: msgjson.TradeNote{
			Side:     msgjson.BuyOrderNum,
			Quantity: 10,
			Rate:     2,
		},
		OrderNote: msgjson.OrderNote{
			Seq:      bookSeq,
			MarketID: mid,
			OrderID:  oid,
		},
	})
	if err != nil {
		t.Fatalf("[NewNotification]: unexpected err: %v", err)
	}

	err = handleBookOrderMsg(tCore, dc, msg)
	if err != nil {
		t.Fatalf("[handleBookOrderMsg]: unexpected err: %v", err)
	}
	_, ok = dc.books[mid]
	if !ok {
		t.Fatalf("expected order book with market id %s", mid)
	}

	// Ensure handleUnbookOrderMsg removes a book order from an associated
	// order book as expected.
	oid, err = hex.DecodeString("d445ab685f5cf54dfebdaa05232892b4cfa453a566b5e85f62627dd6834c5c02")
	if err != nil {
		t.Fatalf("[DecodeString]: unexpected err: %v", err)
	}
	unbookSeq := uint64(3)
	msg, err = msgjson.NewNotification(msgjson.UnbookOrderRoute, &msgjson.UnbookOrderNote{
		Seq:      unbookSeq,
		MarketID: mid,
		OrderID:  oid,
	})
	if err != nil {
		t.Fatalf("[NewNotification]: unexpected err: %v", err)
	}

	err = handleUnbookOrderMsg(tCore, dc, msg)
	if err != nil {
		t.Fatalf("[handleUnbookOrderMsg]: unexpected err: %v", err)
	}
	_, ok = dc.books[mid]
	if !ok {
		t.Fatalf("expected order book with market id %s", mid)
	}
}

type tDriver struct {
	f     func(*asset.WalletConfig, dex.Logger, dex.Network) (asset.Wallet, error)
	winfo *asset.WalletInfo
}

func (drv *tDriver) Setup(cfg *asset.WalletConfig, logger dex.Logger, net dex.Network) (asset.Wallet, error) {
	return drv.f(cfg, logger, net)
}

func (drv *tDriver) Info() *asset.WalletInfo {
	return drv.winfo
}

func TestCreateWallet(t *testing.T) {
	rig := newTestRig()
	tCore := rig.core

	// Create a new asset.
	a := *tDCR
	tILT := &a
	tILT.Symbol = "ilt"
	tILT.ID, _ = dex.BipSymbolID(tILT.Symbol)

	// Create registration form.
	form := &WalletForm{
		AssetID: tILT.ID,
		Account: "default",
	}

	ensureErr := func(tag string) {
		err := tCore.CreateWallet(tPW, wPW, form)
		if err == nil {
			t.Fatalf("no %s error", tag)
		}
	}

	// Try to add an existing wallet.
	wallet, tWallet := newTWallet(tILT.ID)
	tCore.wallets[tILT.ID] = wallet
	ensureErr("existing wallet")
	delete(tCore.wallets, tILT.ID)

	// Failure to retrieve encryption key params.
	rig.db.encKeyErr = tErr
	ensureErr("db.Get")
	rig.db.encKeyErr = nil

	// Crypter error.
	rig.crypter.encryptErr = tErr
	ensureErr("Encrypt")
	rig.crypter.encryptErr = nil

	// Try an unknown wallet (not yet asset.Register'ed).
	ensureErr("unregistered asset")

	// Register the asset.
	asset.Register(tILT.ID, &tDriver{
		f: func(wCfg *asset.WalletConfig, logger dex.Logger, net dex.Network) (asset.Wallet, error) {
			return wallet.Wallet, nil
		},
		winfo: &asset.WalletInfo{},
	})

	// Connection error.
	tWallet.connectErr = tErr
	ensureErr("Connect")
	tWallet.connectErr = nil

	// Unlock error.
	tWallet.unlockErr = tErr
	ensureErr("Unlock")
	tWallet.unlockErr = nil

	// Address error.
	tWallet.addrErr = tErr
	ensureErr("Address")
	tWallet.addrErr = nil

	// Balance error.
	tWallet.balErr = tErr
	ensureErr("Balance")
	tWallet.balErr = nil

	// Database error.
	rig.db.updateWalletErr = tErr
	ensureErr("db.UpdateWallet")
	rig.db.updateWalletErr = nil

	// Success
	delete(tCore.wallets, tILT.ID)
	err := tCore.CreateWallet(tPW, wPW, form)
	if err != nil {
		t.Fatalf("error when should be no error: %v", err)
	}
}

func TestRegister(t *testing.T) {
	// This test takes a little longer because the key is decrypted every time
	// Register is called.
	rig := newTestRig()
	tCore := rig.core
	dc := rig.dc
	acct := dc.acct

	wallet, tWallet := newTWallet(tDCR.ID)
	tCore.wallets[tDCR.ID] = wallet

	// When registering, successfully retrieving *db.AccountInfo from the DB is
	// an error (no dupes). Initial state is to return an error.
	rig.db.acctErr = tErr

	regRes := &msgjson.RegisterResult{
		DEXPubKey:    acct.dexPubKey.Serialize(),
		ClientPubKey: dex.Bytes{0x1}, // part of the serialization, but not the response
		Address:      "someaddr",
		Fee:          tFee,
		Time:         encode.UnixMilliU(time.Now()),
	}
	sign(tDexPriv, regRes)

	queueRegister := func() {
		rig.ws.queueResponse(msgjson.RegisterRoute, func(msg *msgjson.Message, f msgFunc) error {
			resp, _ := msgjson.NewResponse(msg.ID, regRes, nil)
			f(resp)
			return nil
		})
	}

	queueNotifyFee := func() {
		rig.ws.queueResponse(msgjson.NotifyFeeRoute, func(msg *msgjson.Message, f msgFunc) error {
			req := new(msgjson.NotifyFee)
			json.Unmarshal(msg.Payload, req)
			sigMsg, _ := req.Serialize()
			sig, _ := tDexPriv.Sign(sigMsg)
			// Shouldn't Sig be dex.Bytes?
			result := &msgjson.Acknowledgement{Sig: sig.Serialize()}
			resp, _ := msgjson.NewResponse(msg.ID, result, nil)
			f(resp)
			return nil
		})
	}

	queueTipChange := func() {
		go func() {
			timeout := time.NewTimer(time.Second * 2)
			for {
				select {
				case <-time.NewTimer(time.Millisecond).C:
					tCore.waiterMtx.Lock()
					waiterCount := len(tCore.blockWaiters)
					tCore.waiterMtx.Unlock()
					if waiterCount > 0 {
						tWallet.setConfs(tDCR.FundConf)
						tCore.tipChange(tDCR.ID, nil)
						return
					}
				case <-timeout.C:
					t.Fatalf("failed to find waiter before timeout")
				}
			}
		}()
	}

	queueConnect := func() {
		rig.ws.queueResponse(msgjson.ConnectRoute, func(msg *msgjson.Message, f msgFunc) error {
			result := &msgjson.ConnectResult{}
			resp, _ := msgjson.NewResponse(msg.ID, result, nil)
			f(resp)
			return nil
		})
	}

	queueResponses := func() {
		queueRegister()
		queueTipChange()
		queueNotifyFee()
		queueConnect()
	}

	form := &Registration{
		DEX:      tDexUrl,
		Password: tPW,
		Fee:      tFee,
	}

	tWallet.payFeeCoin = &tCoin{id: []byte("abcdef")}

	ch := tCore.NotificationFeed()

	var err error
	run := func() {
		tWallet.setConfs(tDCR.FundConf)
		err = tCore.Register(form)
	}

	getFeeNote := func() *FeePaymentNote {
		select {
		case n := <-ch:
			switch note := n.(type) {
			case *FeePaymentNote:
				return note
			default:
				t.Fatalf("wrong notification type: %T", note)
			}
			// When it works, it should be virtually instant, but I have seen it fail
			// at 1 millisecond.
		case <-time.NewTimer(time.Second).C:
			t.Fatalf("timed out waiting for fee payment notification")
		}
		return nil
	}

	queueResponses()
	run()
	if err != nil {
		t.Fatalf("registration error: %v", err)
	}
	// Should be two success notifications. One for fee paid on-chain, one for
	// fee notification sent.
	feeNote := getFeeNote()
	if feeNote.Severity() != db.Success {
		t.Fatalf("fee payment error notification: %s: %s", feeNote.Subject(), feeNote.Details())
	}
	feeNote = getFeeNote()
	if feeNote.Severity() != db.Success {
		t.Fatalf("fee payment error notification: %s: %s", feeNote.Subject(), feeNote.Details())
	}

	// wallet not found
	delete(tCore.wallets, tDCR.ID)
	run()
	if err == nil {
		t.Fatalf("no error for missing wallet")
	}
	tCore.wallets[tDCR.ID] = wallet

	// account already exists
	rig.db.acct = &db.AccountInfo{
		URL:       tDexUrl,
		EncKey:    acct.encKey,
		DEXPubKey: acct.dexPubKey,
		FeeCoin:   acct.feeCoin,
	}
	rig.db.acctErr = nil
	run()
	if err == nil {
		t.Fatalf("no error for account already exists")
	}
	rig.db.acct = nil
	rig.db.acctErr = tErr

	// asset not found
	dcrAsset := dc.assets[tDCR.ID]
	delete(dc.assets, tDCR.ID)
	run()
	if err == nil {
		t.Fatalf("no error for missing asset")
	}
	dc.assets[tDCR.ID] = dcrAsset

	// register request error
	rig.ws.queueResponse(msgjson.RegisterRoute, func(msg *msgjson.Message, f msgFunc) error {
		return tErr
	})
	run()
	if err == nil {
		t.Fatalf("no error for register request error")
	}

	// signature error
	goodSig := regRes.Sig
	regRes.Sig = []byte("badsig")
	queueRegister()
	run()
	if err == nil {
		t.Fatalf("no error for bad signature on register response")
	}
	regRes.Sig = goodSig

	// zero fee error
	goodFee := regRes.Fee
	regRes.Fee = 0
	queueRegister()
	run()
	if err == nil {
		t.Fatalf("no error for zero fee")
	}
	regRes.Fee = goodFee

	// PayFee error
	queueRegister()
	tWallet.payFeeErr = tErr
	run()
	if err == nil {
		t.Fatalf("no error for PayFee error")
	}
	tWallet.payFeeErr = nil

	// May want to smarten up error handling in the coin waiter loop. If so
	// this check can be re-implemented.
	// // coin confirmation error
	// queueRegister()
	// tWallet.payFeeCoin.confsErr = tErr
	// run()
	// if err == nil {
	// 	t.Fatalf("no error for coin confirmation error")
	// }
	// tWallet.payFeeCoin.confsErr = nil

	// notifyfee response error
	queueRegister()
	queueTipChange()
	rig.ws.queueResponse(msgjson.NotifyFeeRoute, func(msg *msgjson.Message, f msgFunc) error {
		m, _ := msgjson.NewResponse(msg.ID, nil, msgjson.NewError(1, "test error message"))
		f(m)
		return nil
	})
	run()
	// This should not return a registration error, but the 2nd FeePaymentNote
	// should indicate an error.
	if err != nil {
		t.Fatalf("error for notifyfee response error: %v", err)
	}
	// 1st note is fee sent.
	feeNote = getFeeNote()
	if feeNote.Severity() != db.Success {
		t.Fatalf("fee payment error notification: %s: %s", feeNote.Subject(), feeNote.Details())
	}
	// 2nd note is fee error
	feeNote = getFeeNote()
	if feeNote.Severity() != db.ErrorLevel {
		t.Fatalf("non-error fee payment notification for notifyfee response error: %s: %s", feeNote.Subject(), feeNote.Details())
	}

	// Make sure it's good again.
	queueResponses()
	run()
	if err != nil {
		t.Fatalf("error after regaining valid state: %v", err)
	}
	feeNote = getFeeNote()
	if feeNote.Severity() != db.Success {
		t.Fatalf("fee payment error notification: %s: %s", feeNote.Subject(), feeNote.Details())
	}
}

func TestLogin(t *testing.T) {

	rig := newTestRig()
	tCore := rig.core
	rig.acct.pay()

	queueSuccess := func() {
		rig.ws.queueResponse(msgjson.ConnectRoute, func(msg *msgjson.Message, f msgFunc) error {
			result := &msgjson.ConnectResult{}
			resp, _ := msgjson.NewResponse(msg.ID, result, nil)
			f(resp)
			return nil
		})
	}

	queueSuccess()
	_, err := tCore.Login(tPW)
	if err != nil {
		t.Fatalf("initial Login error: %v", err)
	}

	// No encryption key.
	rig.acct.unauth()
	rig.db.encKeyErr = tErr
	_, err = tCore.Login(tPW)
	if err == nil {
		t.Fatalf("no error for missing app key")
	}
	rig.db.encKeyErr = nil

	// Account not Paid. No error, and account should be unlocked.
	rig.acct.isPaid = false
	_, err = tCore.Login(tPW)
	if err != nil {
		t.Fatalf("error for unpaid account: %v", err)
	}
	if rig.acct.locked() {
		t.Fatalf("unpaid account is locked")
	}
	rig.acct.pay()

	// 'connect' route error.
	rig.acct.unauth()
	rig.ws.queueResponse(msgjson.ConnectRoute, func(msg *msgjson.Message, f msgFunc) error {
		resp, _ := msgjson.NewResponse(msg.ID, nil, msgjson.NewError(1, "test error"))
		f(resp)
		return nil
	})
	_, err = tCore.Login(tPW)
	if err == nil {
		t.Fatalf("no error for 'connect' route error")
	}

	// Success again.
	rig.acct.unauth()
	queueSuccess()
	_, err = tCore.Login(tPW)
	if err != nil {
		t.Fatalf("final Login error: %v", err)
	}
}

func TestConnectDEX(t *testing.T) {
	rig := newTestRig()
	tCore := rig.core

	ai := &db.AccountInfo{
		URL: "https://somedex.com",
	}

	queueConfig := func() {
		rig.ws.queueResponse(msgjson.ConfigRoute, func(msg *msgjson.Message, f msgFunc) error {
			result := &msgjson.ConfigResult{
				BroadcastTimeout: 5 * 60 * 1000,
			}
			resp, _ := msgjson.NewResponse(msg.ID, result, nil)
			f(resp)
			return nil
		})
	}

	queueConfig()
	_, err := tCore.connectDEX(ai)
	if err != nil {
		t.Fatalf("initial connectDEX error: %v", err)
	}

	// Bad URL.
	ai.URL = ":::"
	_, err = tCore.connectDEX(ai)
	if err == nil {
		t.Fatalf("no error for bad URL")
	}
	ai.URL = "https://someotherdex.org"

	// Constructor error.
	ogConstructor := tCore.wsConstructor
	tCore.wsConstructor = func(*comms.WsCfg) (comms.WsConn, error) {
		return nil, tErr
	}
	_, err = tCore.connectDEX(ai)
	if err == nil {
		t.Fatalf("no error for WsConn constructor error")
	}
	tCore.wsConstructor = ogConstructor

	// WsConn.Connect error.
	rig.ws.connectErr = tErr
	_, err = tCore.connectDEX(ai)
	if err == nil {
		t.Fatalf("no error for WsConn.Connect error")
	}
	rig.ws.connectErr = nil

	// 'config' route error.
	rig.ws.queueResponse(msgjson.ConfigRoute, func(msg *msgjson.Message, f msgFunc) error {
		resp, _ := msgjson.NewResponse(msg.ID, nil, msgjson.NewError(1, "test error"))
		f(resp)
		return nil
	})
	_, err = tCore.connectDEX(ai)
	if err == nil {
		t.Fatalf("no error for 'config' route error")
	}

	// Success again.
	queueConfig()
	_, err = tCore.connectDEX(ai)
	if err != nil {
		t.Fatalf("final connectDEX error: %v", err)
	}
}

func TestInitializeClient(t *testing.T) {
	rig := newTestRig()
	tCore := rig.core

	err := tCore.InitializeClient(tPW)
	if err != nil {
		t.Fatalf("InitializeClient error: %v", err)
	}

	// Empty password.
	err = tCore.InitializeClient("")
	if err == nil {
		t.Fatalf("no error for empty password")
	}

	// Store error
	rig.db.storeErr = tErr
	err = tCore.InitializeClient("")
	if err == nil {
		t.Fatalf("no error for StoreEncryptedKey error")
	}
	rig.db.storeErr = nil

	// Success again
	err = tCore.InitializeClient(tPW)
	if err != nil {
		t.Fatalf("final InitializeClient error: %v", err)
	}
}

func TestWithdraw(t *testing.T) {
	rig := newTestRig()
	tCore := rig.core
	wallet, xcWallet := newTWallet(tDCR.ID)
	tCore.wallets[tDCR.ID] = wallet
	wallet.address = "addr"

	// Successful
	_, err := tCore.Withdraw(tPW, tDCR.ID, 1e8)
	if err != nil {
		t.Fatalf("withdraw error: %v", err)
	}

	// 0 value
	_, err = tCore.Withdraw(tPW, tDCR.ID, 0)
	if err == nil {
		t.Fatalf("no error for zero value withdraw")
	}

	// no wallet
	_, err = tCore.Withdraw(tPW, 12345, 1e8)
	if err == nil {
		t.Fatalf("no error for unknown wallet")
	}

	// Send error
	xcWallet.payFeeErr = tErr
	_, err = tCore.Withdraw(tPW, tDCR.ID, 1e8)
	if err == nil {
		t.Fatalf("no error for wallet Send error")
	}
	xcWallet.payFeeErr = nil

	// Check the coin.
	xcWallet.payFeeCoin = &tCoin{id: []byte{'a'}}
	coin, err := tCore.Withdraw(tPW, tDCR.ID, 1e8)
	if err != nil {
		t.Fatalf("coin check error: %v", err)
	}
	coinID := coin.ID()
	if len(coinID) != 1 || coinID[0] != 'a' {
		t.Fatalf("coin ID not propagated")
	}
}

func TestTrade(t *testing.T) {
	rig := newTestRig()
	tCore := rig.core
	dcrWallet, tDcrWallet := newTWallet(tDCR.ID)
	tCore.wallets[tDCR.ID] = dcrWallet
	dcrWallet.address = "DsVmA7aqqWeKWy461hXjytbZbgCqbB8g2dq"
	dcrWallet.Unlock(tPW, time.Hour)

	btcWallet, tBtcWallet := newTWallet(tBTC.ID)
	tCore.wallets[tBTC.ID] = btcWallet
	btcWallet.address = "12DXGkvxFjuq5btXYkwWfBZaz1rVwFgini"
	btcWallet.Unlock(tPW, time.Hour)

	qty := tDCR.LotSize * 10
	rate := tBTC.RateStep * 1000

	form := &TradeForm{
		DEX:     tDexUrl,
		IsLimit: true,
		Sell:    true,
		Base:    tDCR.ID,
		Quote:   tBTC.ID,
		Qty:     qty,
		Rate:    rate,
		TifNow:  false,
	}

	dcrCoin := &tCoin{
		id:  encode.RandomBytes(36),
		val: qty * 2,
	}
	tDcrWallet.fundCoins = asset.Coins{dcrCoin}

	btcVal := calc.BaseToQuote(rate, qty*2)
	btcCoin := &tCoin{
		id:  encode.RandomBytes(36),
		val: btcVal,
	}
	tBtcWallet.fundCoins = asset.Coins{btcCoin}

	orderBook := book.NewOrderBook()
	rig.dc.books[tDcrBtcMktName] = orderBook

	msgOrderNote := &msgjson.BookOrderNote{
		OrderNote: msgjson.OrderNote{
			OrderID: encode.RandomBytes(32),
		},
		TradeNote: msgjson.TradeNote{
			Side:     msgjson.SellOrderNum,
			Quantity: tDCR.LotSize,
			Time:     uint64(time.Now().Unix()),
			Rate:     rate,
		},
	}

	err := orderBook.Sync(&msgjson.OrderBook{
		MarketID: tDcrBtcMktName,
		Seq:      1,
		Epoch:    1,
		Orders:   []*msgjson.BookOrderNote{msgOrderNote},
	})
	if err != nil {
		t.Fatalf("order book sync error: %v", err)
	}

	badSig := false
	noID := false
	badID := false
	handleLimit := func(msg *msgjson.Message, f msgFunc) error {
		// Need to stamp and sign the message with the server's key.
		msgOrder := new(msgjson.LimitOrder)
		err := msg.Unmarshal(msgOrder)
		if err != nil {
			t.Fatalf("unmarshal error: %v", err)
		}
		lo := convertMsgLimitOrder(msgOrder)
		f(orderResponse(msg.ID, msgOrder, lo, badSig, noID, badID))
		return nil
	}

	handleMarket := func(msg *msgjson.Message, f msgFunc) error {
		// Need to stamp and sign the message with the server's key.
		msgOrder := new(msgjson.MarketOrder)
		err := msg.Unmarshal(msgOrder)
		if err != nil {
			t.Fatalf("unmarshal error: %v", err)
		}
		mo := convertMsgMarketOrder(msgOrder)
		f(orderResponse(msg.ID, msgOrder, mo, badSig, noID, badID))
		return nil
	}

	ensureErr := func(tag string) {
		_, err = tCore.Trade(tPW, form)
		if err == nil {
			t.Fatalf("%s: no error", tag)
		}
	}

	// Initial success
	rig.ws.queueResponse(msgjson.LimitRoute, handleLimit)
	_, err = tCore.Trade(tPW, form)
	if err != nil {
		t.Fatalf("limit order error: %v", err)
	}

	// Check that the Fund request for a limit sell came through and that
	// value was not adjusted internally with BaseToQuote.
	if tDcrWallet.fundedVal != qty {
		t.Fatalf("limit sell expected funded value %d, got %d", qty, tDcrWallet.fundedVal)
	}
	tDcrWallet.fundedVal = 0

	// Should not be able to close wallet now, since there are orders.
	if tCore.CloseWallet(tDCR.ID) == nil {
		t.Fatalf("no error for closing DCR wallet with active orders")
	}
	if tCore.CloseWallet(tBTC.ID) == nil {
		t.Fatalf("no error for closing BTC wallet with active orders")
	}

	// Dex not found
	form.DEX = "https://someotherdex.org"
	_, err = tCore.Trade(tPW, form)
	if err == nil {
		t.Fatalf("no error for unknown dex")
	}
	form.DEX = tDexUrl

	// No base asset
	form.Base = 12345
	ensureErr("bad base asset")
	form.Base = tDCR.ID

	// No quote asset
	form.Quote = 12345
	ensureErr("bad quote asset")
	form.Quote = tBTC.ID

	// Limit order zero rate
	form.Rate = 0
	ensureErr("zero rate limit")
	form.Rate = rate

	// No from wallet
	delete(tCore.wallets, tDCR.ID)
	ensureErr("no dcr wallet")
	tCore.wallets[tDCR.ID] = dcrWallet

	// No to wallet
	delete(tCore.wallets, tBTC.ID)
	ensureErr("no btc wallet")
	tCore.wallets[tBTC.ID] = btcWallet

	// Address error
	tBtcWallet.addrErr = tErr
	ensureErr("address error")
	tBtcWallet.addrErr = nil

	// Not enough funds
	tDcrWallet.fundErr = tErr
	ensureErr("funds error")
	tDcrWallet.fundErr = nil

	// Lot size violation
	ogQty := form.Qty
	form.Qty += tDCR.LotSize / 2
	ensureErr("bad size")
	form.Qty = ogQty

	// Coin signature error
	tDcrWallet.signCoinErr = tErr
	ensureErr("signature error")
	tDcrWallet.signCoinErr = nil

	// LimitRoute error
	rig.ws.reqErr = tErr
	ensureErr("Request error")
	rig.ws.reqErr = nil

	// The rest need a queued handler

	// Bad signature
	rig.ws.queueResponse(msgjson.LimitRoute, handleLimit)
	badSig = true
	ensureErr("bad server sig")
	badSig = false

	// No order ID in response
	rig.ws.queueResponse(msgjson.LimitRoute, handleLimit)
	noID = true
	ensureErr("no ID")
	noID = false

	// Wrong order ID in response
	rig.ws.queueResponse(msgjson.LimitRoute, handleLimit)
	badID = true
	ensureErr("no ID")
	badID = false

	// Storage failure
	rig.ws.queueResponse(msgjson.LimitRoute, handleLimit)
	rig.db.updateOrderErr = tErr
	ensureErr("db failure")
	rig.db.updateOrderErr = nil

	// Success when buying.
	form.Sell = false
	rig.ws.queueResponse(msgjson.LimitRoute, handleLimit)
	_, err = tCore.Trade(tPW, form)
	if err != nil {
		t.Fatalf("limit order error: %v", err)
	}

	// Check that the Fund request for a limit buy came through to the BTC wallet
	// and that the value was adjusted internally with BaseToQuote.
	expQty := calc.BaseToQuote(rate, qty)
	if tBtcWallet.fundedVal != expQty {
		t.Fatalf("limit buy expected funded value %d, got %d", expQty, tBtcWallet.fundedVal)
	}
	tBtcWallet.fundedVal = 0

	// Successful market buy order
	form.IsLimit = false
	rig.ws.queueResponse(msgjson.MarketRoute, handleMarket)
	_, err = tCore.Trade(tPW, form)
	if err != nil {
		t.Fatalf("market order error: %v", err)
	}

	// The funded qty for a market buy should not be adjusted.
	if tBtcWallet.fundedVal != qty {
		t.Fatalf("market buy expected funded value %d, got %d", qty, tBtcWallet.fundedVal)
	}
	tBtcWallet.fundedVal = 0

	// Successful market sell order.
	form.Sell = true
	rig.ws.queueResponse(msgjson.MarketRoute, handleMarket)
	_, err = tCore.Trade(tPW, form)
	if err != nil {
		t.Fatalf("market order error: %v", err)
	}

	// The funded qty for a market sell order should not be adjusted.
	if tDcrWallet.fundedVal != qty {
		t.Fatalf("market sell expected funded value %d, got %d", qty, tDcrWallet.fundedVal)
	}
	tDcrWallet.fundedVal = 0
}

func TestCancel(t *testing.T) {
	rig := newTestRig()
	dc := rig.dc
	preImg := newPreimage()
	lo := &order.LimitOrder{
		P: order.Prefix{
			OrderType:  order.LimitOrderType,
			BaseAsset:  tDCR.ID,
			QuoteAsset: tBTC.ID,
			ClientTime: time.Now(),
			ServerTime: time.Now(),
			Commit:     preImg.Commit(),
		},
	}
	dbOrder := &db.MetaOrder{
		MetaData: &db.OrderMetaData{
			Status: order.OrderStatusEpoch,
			DEX:    dc.acct.url,
			Proof: db.OrderProof{
				Preimage: preImg[:],
			},
		},
		Order: lo,
	}
	oid := lo.ID()
	tracker := newTrackedTrade(dbOrder, preImg, dc, rig.db, rig.queue, nil, nil, rig.core.notify)
	dc.trades[oid] = tracker

	handleCancel := func(msg *msgjson.Message, f msgFunc) error {
		// Need to stamp and sign the message with the server's key.
		msgOrder := new(msgjson.CancelOrder)
		err := msg.Unmarshal(msgOrder)
		if err != nil {
			t.Fatalf("unmarshal error: %v", err)
		}
		co := convertMsgCancelOrder(msgOrder)
		f(orderResponse(msg.ID, msgOrder, co, false, false, false))
		return nil
	}

	sid := oid.String()
	rig.ws.queueResponse(msgjson.CancelRoute, handleCancel)
	err := rig.core.Cancel(tPW, sid)
	if err != nil {
		t.Fatalf("cancel error: %v", err)
	}
	if tracker.cancel == nil {
		t.Fatalf("cancel order not found")
	}
	// remove the cancel order so we can check its nilness on error.
	tracker.cancel = nil

	ensureErr := func(tag string) {
		err := rig.core.Cancel(tPW, sid)
		if err == nil {
			t.Fatalf("%s: no error", tag)
		}
		if tracker.cancel != nil {
			t.Fatalf("%s: cancel order found", tag)
		}
	}

	// Bad order ID
	ogID := sid
	sid = "badid"
	ensureErr("bad id")
	sid = ogID

	// Order not found
	delete(dc.trades, oid)
	ensureErr("no order")
	dc.trades[oid] = tracker

	// Send error
	rig.ws.reqErr = tErr
	ensureErr("Request error")
	rig.ws.reqErr = nil

}

func TestHandlePreimageRequest(t *testing.T) {
	rig := newTestRig()
	ord := &order.LimitOrder{P: order.Prefix{ServerTime: time.Now()}}
	oid := ord.ID()
	preImg := newPreimage()
	payload := &msgjson.PreimageRequest{
		OrderID: oid[:],
	}
	req, _ := msgjson.NewRequest(rig.dc.NextID(), msgjson.PreimageRoute, payload)

	tracker := &trackedTrade{
		Order:  ord,
		preImg: preImg,
		dc:     rig.dc,
	}
	rig.dc.trades[oid] = tracker
	err := handlePreimageRequest(rig.core, rig.dc, req)
	if err != nil {
		t.Fatalf("handlePreimageRequest error: %v", err)
	}

	ensureErr := func(tag string) {
		err := handlePreimageRequest(rig.core, rig.dc, req)
		if err == nil {
			t.Fatalf("%s: no error", tag)
		}
	}

	delete(rig.dc.trades, oid)
	ensureErr("no tracker")
	rig.dc.trades[oid] = tracker

	rig.ws.sendErr = tErr
	ensureErr("send error")
	rig.ws.sendErr = nil
}

func TestTradeTracking(t *testing.T) {
	rig := newTestRig()
	dc := rig.dc
	tCore := rig.core
	dcrWallet, tDcrWallet := newTWallet(tDCR.ID)
	tCore.wallets[tDCR.ID] = dcrWallet
	dcrWallet.address = "DsVmA7aqqWeKWy461hXjytbZbgCqbB8g2dq"
	dcrWallet.Unlock(tPW, time.Hour)

	btcWallet, tBtcWallet := newTWallet(tBTC.ID)
	tCore.wallets[tBTC.ID] = btcWallet
	btcWallet.address = "12DXGkvxFjuq5btXYkwWfBZaz1rVwFgini"
	btcWallet.Unlock(tPW, time.Hour)

	matchSize := 4 * tDCR.LotSize
	cancelledQty := tDCR.LotSize
	qty := 2*matchSize + cancelledQty
	rate := tBTC.RateStep * 10
	preImgL := newPreimage()
	addr := ordertest.RandomAddress()
	lo := &order.LimitOrder{
		P: order.Prefix{
			AccountID:  dc.acct.ID(),
			BaseAsset:  tDCR.ID,
			QuoteAsset: tBTC.ID,
			OrderType:  order.MarketOrderType,
			ClientTime: time.Now(),
			ServerTime: time.Now().Add(time.Millisecond),
			Commit:     preImgL.Commit(),
		},
		T: order.Trade{
			Sell:     true,
			Quantity: qty,
			Address:  addr,
		},
		Rate: tBTC.RateStep,
	}
	dbOrder := &db.MetaOrder{
		MetaData: &db.OrderMetaData{
			Status: order.OrderStatusEpoch,
			DEX:    dc.acct.url,
			Proof: db.OrderProof{
				Preimage: preImgL[:],
			},
		},
		Order: lo,
	}
	loid := lo.ID()
	mid := ordertest.RandomMatchID()
	walletSet, err := tCore.walletSet(dc, tDCR.ID, tBTC.ID, true)
	if err != nil {
		t.Fatalf("walletSet error: %v", err)
	}
	tracker := newTrackedTrade(dbOrder, preImgL, dc, rig.db, rig.queue, walletSet, nil, rig.core.notify)
	rig.dc.trades[tracker.ID()] = tracker
	var match *matchTracker
	checkStatus := func(tag string, wantStatus order.MatchStatus) {
		if match.Match.Status != wantStatus {
			t.Fatalf("%s: wrong status wanted %d, got %d", tag, match.Match.Status, wantStatus)
		}
	}

	// MAKER MATCH
	//
	msgMatch := &msgjson.Match{
		OrderID:  loid[:],
		MatchID:  mid[:],
		Quantity: matchSize,
		Rate:     rate,
		Address:  "counterparty-address",
		Side:     uint8(order.Maker),
	}
	counterSwapID := encode.RandomBytes(36)
	tDcrWallet.swapReceipts = []asset.Receipt{&tReceipt{coin: &tCoin{id: counterSwapID}}}
	sign(tDexPriv, msgMatch)
	msg, _ := msgjson.NewRequest(1, msgjson.MatchRoute, []*msgjson.Match{msgMatch})
	rig.ws.queueResponse(msgjson.InitRoute, initAcker)
	err = handleMatchRoute(tCore, rig.dc, msg)
	if err != nil {
		t.Fatalf("match messages error: %v", err)
	}
	match, found := tracker.matches[mid]
	if !found {
		t.Fatalf("match not found")
	}

	// We're the maker, so the init transaction should be broadcast.
	checkStatus("maker swapped", order.MakerSwapCast)
	_, metaData := match.Match, match.MetaData
	proof, auth := &metaData.Proof, &metaData.Proof.Auth
	if len(auth.MatchSig) == 0 {
		t.Fatalf("no match sig recorded")
	}
	if !bytes.Equal(proof.MakerSwap, counterSwapID) {
		t.Fatalf("receipt ID not recorded")
	}
	if len(proof.Secret) == 0 {
		t.Fatalf("secret not set")
	}
	if len(proof.SecretHash) == 0 {
		t.Fatalf("secret hash not set")
	}

	// Send the counter-party's init info.
	auditQty := calc.BaseToQuote(rate, matchSize)
	audit, auditInfo := tMsgAudit(loid, mid, addr, auditQty, proof.SecretHash)
	tBtcWallet.auditInfo = auditInfo
	msg, _ = msgjson.NewRequest(1, msgjson.AuditRoute, audit)

	// Check audit errors.
	tBtcWallet.auditErr = tErr
	err = handleAuditRoute(tCore, rig.dc, msg)
	if err == nil {
		t.Fatalf("no maker error for AuditContract error")
	}

	// Check expiration error.
	tBtcWallet.auditErr = asset.CoinNotFoundError
	err = handleAuditRoute(tCore, rig.dc, msg)
	if err == nil {
		t.Fatalf("no maker error for AuditContract expiration")
	}
	var errSet *errorSet
	if !errors.As(err, &errSet) {
		t.Fatalf("unexpected error type")
	}
	var expErr ExpirationErr
	if !errors.As(errSet.errs[0], &expErr) {
		t.Fatalf("wrong error type. expecting ExpirationTimeout, got %T: %v", errSet.errs[0], errSet.errs[0])
	}
	tBtcWallet.auditErr = nil

	auditInfo.coin.val = auditQty - 1
	err = handleAuditRoute(tCore, rig.dc, msg)
	if err == nil {
		t.Fatalf("no maker error for low value")
	}
	auditInfo.coin.val = auditQty

	auditInfo.secretHash = []byte{0x01}
	err = handleAuditRoute(tCore, rig.dc, msg)
	if err == nil {
		t.Fatalf("no maker error for wrong secret hash")
	}
	auditInfo.secretHash = proof.SecretHash

	auditInfo.recipient = "wrong address"
	err = handleAuditRoute(tCore, rig.dc, msg)
	if err == nil {
		t.Fatalf("no maker error for wrong address")
	}
	auditInfo.recipient = addr

	err = handleAuditRoute(tCore, rig.dc, msg)
	if err != nil {
		t.Fatalf("match message error: %v", err)
	}
	checkStatus("maker counter-party swapped", order.TakerSwapCast)
	if match.counterSwap == nil {
		t.Fatalf("counter-swap not set")
	}
	if !bytes.Equal(proof.CounterScript, audit.Contract) {
		t.Fatalf("counter-script not recorded")
	}
	if !bytes.Equal(proof.TakerSwap, audit.CoinID) {
		t.Fatalf("taker contract ID not set")
	}
	if !bytes.Equal(auth.AuditSig, audit.Sig) {
		t.Fatalf("audit sig not set")
	}
	if auth.AuditStamp != audit.Time {
		t.Fatalf("audit time not set")
	}
	// Confirming the counter-swap triggers a redemption.
	auditInfo.coin.confs = tBTC.SwapConf
	redeemCoin := encode.RandomBytes(36)
	tBtcWallet.redeemCoins = []dex.Bytes{redeemCoin}
	rig.ws.queueResponse(msgjson.RedeemRoute, redeemAcker)
	dc.tickAsset(tBTC.ID)
	checkStatus("maker redeemed", order.MakerRedeemed)
	if !bytes.Equal(proof.MakerRedeem, redeemCoin) {
		t.Fatalf("redeem coin ID not logged")
	}
	redemptionCoin := encode.RandomBytes(36)
	// The taker's redemption is simply logged.
	redemption := &msgjson.Redemption{
		Redeem: msgjson.Redeem{
			OrderID: loid[:],
			MatchID: mid[:],
			CoinID:  redemptionCoin,
		},
	}
	msg, _ = msgjson.NewRequest(1, msgjson.RedemptionRoute, redemption)
	err = handleRedemptionRoute(tCore, rig.dc, msg)
	if err != nil {
		t.Fatalf("redemption message error: %v", err)
	}
	checkStatus("maker match complete", order.MatchComplete)
	if !bytes.Equal(redemptionCoin, proof.TakerRedeem) {
		t.Fatalf("taker redemption coin not recorded")
	}

	// TAKER MATCH
	//
	mid = ordertest.RandomMatchID()
	msgMatch = &msgjson.Match{
		OrderID:  loid[:],
		MatchID:  mid[:],
		Quantity: matchSize,
		Rate:     rate,
		Address:  "counterparty-address",
		Side:     uint8(order.Taker),
	}
	sign(tDexPriv, msgMatch)
	msg, _ = msgjson.NewRequest(1, msgjson.MatchRoute, []*msgjson.Match{msgMatch})
	err = handleMatchRoute(tCore, rig.dc, msg)
	if err != nil {
		t.Fatalf("match messages error: %v", err)
	}
	match, found = tracker.matches[mid]
	if !found {
		t.Fatalf("match not found")
	}
	checkStatus("taker matched", order.NewlyMatched)
	_, metaData = match.Match, match.MetaData
	proof, auth = &metaData.Proof, &metaData.Proof.Auth
	if len(auth.MatchSig) == 0 {
		t.Fatalf("no match sig recorded")
	}
	// Secret should not be set yet.
	if len(proof.Secret) != 0 {
		t.Fatalf("secret set for taker")
	}
	if len(proof.SecretHash) != 0 {
		t.Fatalf("secret hash set for taker")
	}
	// Now send through the audit request for the maker's init.
	audit, auditInfo = tMsgAudit(loid, mid, addr, matchSize, nil)
	tBtcWallet.auditInfo = auditInfo
	msg, _ = msgjson.NewRequest(1, msgjson.AuditRoute, audit)
	err = handleAuditRoute(tCore, rig.dc, msg)
	if err != nil {
		t.Fatalf("taker's match message error: %v", err)
	}
	checkStatus("taker counter-party swapped", order.MakerSwapCast)
	if len(proof.SecretHash) == 0 {
		t.Fatalf("secret hash not set for taker")
	}
	if !bytes.Equal(proof.MakerSwap, audit.CoinID) {
		t.Fatalf("maker redeem coin not set")
	}
	if !bytes.Equal(auth.AuditSig, audit.Sig) {
		t.Fatalf("audit sig not set for taker")
	}
	if auth.AuditStamp != audit.Time {
		t.Fatalf("audit time not set for taker")
	}
	// The swap should not be sent, since the auditInfo coin doesn't have the
	// requisite confirmations.
	if len(proof.TakerSwap) != 0 {
		t.Fatalf("swap broadcast before confirmations")
	}
	// Now with the confirmations.
	auditInfo.coin.confs = tBTC.SwapConf
	swapID := encode.RandomBytes(36)
	tDcrWallet.swapReceipts = []asset.Receipt{&tReceipt{coin: &tCoin{id: swapID}}}
	rig.ws.queueResponse(msgjson.InitRoute, initAcker)
	dc.tickAsset(tBTC.ID)
	checkStatus("taker swapped", order.TakerSwapCast)
	if len(proof.TakerSwap) == 0 {
		t.Fatalf("swap not broadcast with confirmations")
	}
	// Receive the maker's redemption.
	redemptionCoin = encode.RandomBytes(36)
	redemption = &msgjson.Redemption{
		Redeem: msgjson.Redeem{
			OrderID: loid[:],
			MatchID: mid[:],
			CoinID:  redemptionCoin,
		},
	}
	redeemCoin = encode.RandomBytes(36)
	tBtcWallet.redeemCoins = []dex.Bytes{redeemCoin}
	msg, _ = msgjson.NewRequest(1, msgjson.RedemptionRoute, redemption)

	tBtcWallet.badSecret = true
	err = handleRedemptionRoute(tCore, rig.dc, msg)
	if err == nil {
		t.Fatalf("no error for wrong secret")
	}
	tBtcWallet.badSecret = false

	rig.ws.queueResponse(msgjson.RedeemRoute, redeemAcker)
	err = handleRedemptionRoute(tCore, rig.dc, msg)
	if err != nil {
		t.Fatalf("redemption message error: %v", err)
	}
	checkStatus("taker complete", order.MatchComplete)
	if !bytes.Equal(proof.MakerRedeem, redemptionCoin) {
		t.Fatalf("redemption coin ID not logged")
	}
	if len(proof.TakerRedeem) == 0 {
		t.Fatalf("taker redemption not sent")
	}

	// CANCEL ORDER MATCH
	//
	copy(mid[:], encode.RandomBytes(32))
	preImgC := newPreimage()
	co := &order.CancelOrder{
		P: order.Prefix{
			AccountID:  dc.acct.ID(),
			BaseAsset:  tDCR.ID,
			QuoteAsset: tBTC.ID,
			OrderType:  order.MarketOrderType,
			ClientTime: time.Now(),
			ServerTime: time.Now().Add(time.Millisecond),
			Commit:     preImgC.Commit(),
		},
	}
	tracker.cancel = &trackedCancel{CancelOrder: *co}
	coid := co.ID()
	m1 := &msgjson.Match{
		OrderID:  loid[:],
		MatchID:  mid[:],
		Quantity: cancelledQty,
		Rate:     rate,
		Address:  "",
	}
	m2 := &msgjson.Match{
		OrderID:  coid[:],
		MatchID:  mid[:],
		Quantity: cancelledQty,
		Rate:     rate,
		Address:  "testaddr",
	}
	sign(tDexPriv, m1)
	sign(tDexPriv, m2)
	msg, _ = msgjson.NewRequest(1, msgjson.MatchRoute, []*msgjson.Match{m1, m2})
	err = handleMatchRoute(tCore, rig.dc, msg)
	if err != nil {
		t.Fatalf("match messages error: %v", err)
	}
	if tracker.cancel.matches.maker == nil {
		t.Fatalf("cancelMatches.maker not set")
	}
	if tracker.Trade().Filled() != qty {
		t.Fatalf("fill not set")
	}
	if tracker.cancel.matches.taker == nil {
		t.Fatalf("cancelMatches.taker not set")
	}
}

func TestNotifications(t *testing.T) {
	tCore := newTestRig().core

	// Insert a notification into the database.
	typedNote := newOrderNote("abc", "def", 100, nil)

	ch := tCore.NotificationFeed()
	tCore.notify(typedNote)
	select {
	case n := <-ch:
		dbtest.MustCompareNotifications(t, n.DBNote(), &typedNote.Notification)
	default:
		t.Fatalf("no notification received over the notification channel")
	}
}

func convertMsgLimitOrder(msgOrder *msgjson.LimitOrder) *order.LimitOrder {
	tif := order.ImmediateTiF
	if msgOrder.TiF == msgjson.StandingOrderNum {
		tif = order.StandingTiF
	}
	return &order.LimitOrder{
		P:     convertMsgPrefix(&msgOrder.Prefix, order.LimitOrderType),
		T:     convertMsgTrade(&msgOrder.Trade),
		Rate:  msgOrder.Rate,
		Force: tif,
	}
}

func convertMsgMarketOrder(msgOrder *msgjson.MarketOrder) *order.MarketOrder {
	return &order.MarketOrder{
		P: convertMsgPrefix(&msgOrder.Prefix, order.MarketOrderType),
		T: convertMsgTrade(&msgOrder.Trade),
	}
}

func convertMsgCancelOrder(msgOrder *msgjson.CancelOrder) *order.CancelOrder {
	var oid order.OrderID
	copy(oid[:], msgOrder.TargetID)
	return &order.CancelOrder{
		P:             convertMsgPrefix(&msgOrder.Prefix, order.CancelOrderType),
		TargetOrderID: oid,
	}
}

func convertMsgPrefix(msgPrefix *msgjson.Prefix, oType order.OrderType) order.Prefix {
	var commit order.Commitment
	copy(commit[:], msgPrefix.Commit)
	var acctID account.AccountID
	copy(acctID[:], msgPrefix.AccountID)
	return order.Prefix{
		AccountID:  acctID,
		BaseAsset:  msgPrefix.Base,
		QuoteAsset: msgPrefix.Quote,
		OrderType:  oType,
		ClientTime: encode.UnixTimeMilli(int64(msgPrefix.ClientTime)),
		//ServerTime set in epoch queue processing pipeline.
		Commit: commit,
	}
}

func convertMsgTrade(msgTrade *msgjson.Trade) order.Trade {
	coins := make([]order.CoinID, 0, len(msgTrade.Coins))
	for _, coin := range msgTrade.Coins {
		var b []byte = coin.ID
		coins = append(coins, b)
	}
	sell := true
	if msgTrade.Side == msgjson.BuyOrderNum {
		sell = false
	}
	return order.Trade{
		Coins:    coins,
		Sell:     sell,
		Quantity: msgTrade.Quantity,
		Address:  msgTrade.Address,
	}
}

func orderResponse(msgID uint64, msgPrefix msgjson.Stampable, ord order.Order, badSig, noID, badID bool) *msgjson.Message {
	orderTime := time.Now()
	timeStamp := encode.UnixMilliU(orderTime)
	msgPrefix.Stamp(timeStamp)
	sign(tDexPriv, msgPrefix)
	if badSig {
		msgPrefix.SetSig(encode.RandomBytes(5))
	}
	ord.SetTime(orderTime)
	oid := ord.ID()
	oidB := oid[:]
	if noID {
		oidB = nil
	} else if badID {
		oidB = encode.RandomBytes(32)
	}
	resp, _ := msgjson.NewResponse(msgID, &msgjson.OrderResult{
		Sig:        msgPrefix.SigBytes(),
		OrderID:    oidB,
		ServerTime: timeStamp,
	}, nil)
	return resp
}

func tMsgAudit(oid order.OrderID, mid order.MatchID, recipient string, val uint64, secretHash []byte) (*msgjson.Audit, *tAuditInfo) {
	auditID := encode.RandomBytes(36)
	auditContract := encode.RandomBytes(75)
	if secretHash == nil {
		secretHash = encode.RandomBytes(32)
	}
	auditStamp := encode.UnixMilliU(time.Now())
	audit := &msgjson.Audit{
		OrderID:  oid[:],
		MatchID:  mid[:],
		Time:     auditStamp,
		CoinID:   auditID,
		Contract: auditContract,
	}
	sign(tDexPriv, audit)
	auditCoin := &tCoin{id: auditID, val: val}
	auditInfo := &tAuditInfo{
		recipient:  recipient,
		coin:       auditCoin,
		secretHash: secretHash,
	}
	return audit, auditInfo
}

func TestHandleEpochOrderMsg(t *testing.T) {
	rig := newTestRig()
	ord := &order.LimitOrder{P: order.Prefix{ServerTime: time.Now()}}
	oid := ord.ID()
	mid := hex.EncodeToString(encode.RandomBytes(order.OrderIDSize))
	payload := &msgjson.EpochOrderNote{
		BookOrderNote: msgjson.BookOrderNote{
			OrderNote: msgjson.OrderNote{
				MarketID: mid,
				OrderID:  oid.Bytes(),
			},
			TradeNote: msgjson.TradeNote{
				Side:     msgjson.BuyOrderNum,
				Rate:     4,
				Quantity: 10,
			},
		},
		Epoch: 1,
	}

	req, _ := msgjson.NewRequest(rig.dc.NextID(), msgjson.EpochOrderRoute, payload)

	// Ensure handling an epoch order associated with a non-existent orderbook
	// generates an error.
	err := handleEpochOrderMsg(rig.core, rig.dc, req)
	if err == nil {
		t.Fatal("[handleEpochOrderMsg] expected a non-existent orderbook error")
	}

	rig.dc.books[mid] = book.NewOrderBook()

	err = handleEpochOrderMsg(rig.core, rig.dc, req)
	if err != nil {
		t.Fatalf("[handleEpochOrderMsg] unexpected error: %v", err)
	}

	payload.Epoch = 2
	req, _ = msgjson.NewRequest(rig.dc.NextID(), msgjson.EpochOrderRoute, payload)

	// Ensure receiving an epoch order with a different epoch resets the queue.
	err = handleEpochOrderMsg(rig.core, rig.dc, req)
	if err != nil {
		t.Fatalf("[handleEpochOrderMsg] unexpected error: %v", err)
	}

	epochSize := rig.dc.books[mid].EpochSize()
	if epochSize != 1 {
		t.Fatalf("[handleEpochOrderMsg] expected an epoch size of 1, got %d",
			epochSize)
	}
}

func makeMatchProof(preimages []order.Preimage, commitments []order.Commitment) (msgjson.Bytes, msgjson.Bytes, error) {
	if len(preimages) != len(commitments) {
		return nil, nil, fmt.Errorf("expected equal number of preimages and commitments")
	}

	sbuff := make([]byte, 0, len(preimages)*order.PreimageSize)
	cbuff := make([]byte, 0, len(commitments)*order.CommitmentSize)
	for i := 0; i < len(preimages); i++ {
		sbuff = append(sbuff, preimages[i][:]...)
		cbuff = append(cbuff, commitments[i][:]...)
	}
	seed := blake256.Sum256(sbuff)
	csum := blake256.Sum256(cbuff)
	return seed[:], csum[:], nil
}

func TestHandleMatchProofMsg(t *testing.T) {
	rig := newTestRig()
	mid := hex.EncodeToString(encode.RandomBytes(order.OrderIDSize))
	pimg := newPreimage()
	cmt := pimg.Commit()

	seed, csum, err := makeMatchProof([]order.Preimage{pimg}, []order.Commitment{cmt})
	if err != nil {
		t.Fatalf("[makeMatchProof] unexpected error: %v", err)
	}

	payload := &msgjson.MatchProofNote{
		MarketID:  mid,
		Epoch:     1,
		Preimages: []dex.Bytes{pimg[:]},
		CSum:      csum[:],
		Seed:      seed[:],
	}

	eo := &msgjson.EpochOrderNote{
		BookOrderNote: msgjson.BookOrderNote{
			OrderNote: msgjson.OrderNote{
				MarketID: mid,
				OrderID:  encode.RandomBytes(order.OrderIDSize),
			},
		},
		Epoch:  1,
		Commit: cmt[:],
	}

	req, _ := msgjson.NewRequest(rig.dc.NextID(), msgjson.MatchProofRoute, payload)

	// Ensure match proof validation generates an error for a non-existent
	// orderbook generates an error.
	err = handleMatchProofMsg(rig.core, rig.dc, req)
	if err == nil {
		t.Fatal("[handleMatchProofMsg] expected a non-existent orderbook error")
	}

	rig.dc.books[mid] = book.NewOrderBook()

	err = rig.dc.books[mid].Enqueue(eo)
	if err != nil {
		t.Fatalf("[Enqueue] unexpected error: %v", err)
	}

	err = handleMatchProofMsg(rig.core, rig.dc, req)
	if err != nil {
		t.Fatalf("[handleMatchProofMsg] unexpected error: %v", err)
	}
}
