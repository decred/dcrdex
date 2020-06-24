// +build !harness

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
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/calc"
	"decred.org/dcrdex/dex/encode"
	"decred.org/dcrdex/dex/encrypt"
	"decred.org/dcrdex/dex/msgjson"
	dexbtc "decred.org/dcrdex/dex/networks/btc"
	dexdcr "decred.org/dcrdex/dex/networks/dcr"
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
	}

	tBTC = &dex.Asset{
		ID:       0,
		Symbol:   "btc",
		SwapSize: dexbtc.InitTxSize,
		FeeRate:  2,
		LotSize:  1e6,
		RateStep: 10,
		SwapConf: 1,
	}
	tDexPriv         *secp256k1.PrivateKey
	tDexKey          *secp256k1.PublicKey
	tPW                     = []byte("dexpw")
	wPW                     = "walletpw"
	tDexHost                = "somedex.tld"
	tDcrBtcMktName          = "dcr_btc"
	tErr                    = fmt.Errorf("test error")
	tFee             uint64 = 1e8
	tUnparseableHost        = string([]byte{0x7f})
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
	}
}

func makeAcker(serializer func(msg *msgjson.Message) msgjson.Signable) func(msg *msgjson.Message, f msgFunc) error {
	return func(msg *msgjson.Message, f msgFunc) error {
		signable := serializer(msg)
		sigMsg := signable.Serialize()
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
	invalidAcker = func(msg *msgjson.Message, f msgFunc) error {
		resp, _ := msgjson.NewResponse(msg.ID, msg, nil)
		f(resp)
		return nil
	}
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
		host:      tDexHost,
		encKey:    privKey.Serialize(),
		dexPubKey: tDexKey,
		privKey:   privKey,
		feeCoin:   []byte("somecoin"),
	}
}

func testDexConnection() (*dexConnection, *TWebsocket, *dexAccount) {
	conn := newTWebsocket()
	connMaster := dex.NewConnectionMaster(conn)
	connMaster.Connect(tCtx)
	acct := tNewAccount()
	mkt := &Market{
		Name:            tDcrBtcMktName,
		BaseID:          tDCR.ID,
		BaseSymbol:      tDCR.Symbol,
		QuoteID:         tBTC.ID,
		QuoteSymbol:     tBTC.Symbol,
		EpochLen:        60000,
		MarketBuyBuffer: 1.1,
		suspended:       false,
	}
	return &dexConnection{
		WsConn:     conn,
		connMaster: connMaster,
		acct:       acct,
		assets: map[uint32]*dex.Asset{
			tDCR.ID: tDCR,
			tBTC.ID: tBTC,
		},
		books: make(map[string]*bookie),
		cfg: &msgjson.ConfigResult{
			CancelMax:        0.8,
			BroadcastTimeout: 5 * 60 * 1000,
			Assets: []*msgjson.Asset{
				uncovertAssetInfo(tDCR),
				uncovertAssetInfo(tBTC),
			},
			Markets: []*msgjson.Market{
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
		notify:    func(Notification) {},
		marketMap: map[string]*Market{tDcrBtcMktName: mkt},
		trades:    make(map[order.OrderID]*trackedTrade),
		epoch:     map[string]uint64{tDcrBtcMktName: 0},
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
func (conn *TWebsocket) Connect(context.Context) (*sync.WaitGroup, error) {
	return &sync.WaitGroup{}, conn.connectErr
}

type TDB struct {
	updateWalletErr        error
	acct                   *db.AccountInfo
	acctErr                error
	getErr                 error
	storeErr               error
	encKeyErr              error
	accts                  []*db.AccountInfo
	updateOrderErr         error
	activeDEXOrders        []*db.MetaOrder
	matchesForOID          []*db.MetaMatch
	matchesForOIDErr       error
	activeMatchesForDEX    []*db.MetaMatch
	activeMatchesForDEXErr error
}

func (tdb *TDB) Run(context.Context) {}

func (tdb *TDB) ListAccounts() ([]string, error) {
	return nil, nil
}

func (tdb *TDB) Accounts() ([]*db.AccountInfo, error) {
	return tdb.accts, nil
}

func (tdb *TDB) Account(url string) (*db.AccountInfo, error) {
	return tdb.acct, tdb.acctErr
}

func (tdb *TDB) CreateAccount(ai *db.AccountInfo) error {
	return nil
}

func (tdb *TDB) UpdateOrder(m *db.MetaOrder) error {
	return tdb.updateOrderErr
}

func (tdb *TDB) ActiveDEXOrders(dex string) ([]*db.MetaOrder, error) {
	return tdb.activeDEXOrders, nil
}

func (tdb *TDB) ActiveOrders() ([]*db.MetaOrder, error) {
	return nil, nil
}

func (tdb *TDB) AccountOrders(dex string, n int, since uint64) ([]*db.MetaOrder, error) {
	return nil, nil
}

func (tdb *TDB) Order(order.OrderID) (*db.MetaOrder, error) {
	return nil, nil
}

func (tdb *TDB) MarketOrders(dex string, base, quote uint32, n int, since uint64) ([]*db.MetaOrder, error) {
	return nil, nil
}

func (tdb *TDB) SetChangeCoin(order.OrderID, order.CoinID) error {
	return nil
}

func (tdb *TDB) UpdateOrderStatus(oid order.OrderID, status order.OrderStatus) error {
	return nil
}

func (tdb *TDB) UpdateMatch(m *db.MetaMatch) error {
	return nil
}

func (tdb *TDB) ActiveMatches() ([]*db.MetaMatch, error) {
	return nil, nil
}

func (tdb *TDB) MatchesForOrder(oid order.OrderID) ([]*db.MetaMatch, error) {
	return tdb.matchesForOID, tdb.matchesForOIDErr
}

func (tdb *TDB) ActiveDEXMatches(dex string) ([]*db.MetaMatch, error) {
	return tdb.activeMatchesForDEX, tdb.activeMatchesForDEXErr
}

func (tdb *TDB) UpdateWallet(wallet *db.Wallet) error {
	return tdb.updateWalletErr
}

func (tdb *TDB) UpdateBalance(wid []byte, balance *db.Balance) error {
	return nil
}

func (tdb *TDB) Wallets() ([]*db.Wallet, error) {
	return nil, nil
}

func (tdb *TDB) AccountPaid(proof *msgjson.AccountProof) error {
	return nil
}

func (tdb *TDB) SaveNotification(*db.Notification) error        { return nil }
func (tdb *TDB) NotificationsN(int) ([]*db.Notification, error) { return nil, nil }

func (tdb *TDB) Store(k string, b []byte) error {
	return tdb.storeErr
}

func (tdb *TDB) ValueExists(k string) (bool, error) {
	return false, nil
}

func (tdb *TDB) Get(k string) ([]byte, error) {
	if k == keyParamsKey {
		return nil, tdb.encKeyErr
	}
	return nil, tdb.getErr
}

func (tdb *TDB) Backup() error {
	return nil
}

func (tdb *TDB) AckNotification(id []byte) error { return nil }

type tCoin struct {
	id, script []byte
	confs      uint32
	confsErr   error
	val        uint64
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
	return c.script
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
	mtx            sync.RWMutex
	payFeeCoin     *tCoin
	payFeeErr      error
	fundCoins      asset.Coins
	fundErr        error
	addrErr        error
	signCoinErr    error
	swapReceipts   []asset.Receipt
	auditInfo      asset.AuditInfo
	auditErr       error
	refundCoin     dex.Bytes
	refundErr      error
	redeemCoins    []dex.Bytes
	badSecret      bool
	fundedVal      uint64
	connectErr     error
	unlockErr      error
	balErr         error
	bal            *asset.Balance
	fundingCoins   asset.Coins
	fundingCoinErr error
	lockErr        error
}

func newTWallet(assetID uint32) (*xcWallet, *TXCWallet) {
	w := new(TXCWallet)
	return &xcWallet{
		Wallet:    w,
		connector: dex.NewConnectionMaster(w),
		AssetID:   assetID,
		lockTime:  time.Now().Add(time.Hour),
		hookedUp:  true,
		dbID:      encode.Uint32Bytes(assetID),
	}, w
}

func (w *TXCWallet) Info() *asset.WalletInfo {
	return &asset.WalletInfo{}
}

func (w *TXCWallet) Connect(ctx context.Context) (*sync.WaitGroup, error) {
	return &sync.WaitGroup{}, w.connectErr
}

func (w *TXCWallet) Run(ctx context.Context) { <-ctx.Done() }

func (w *TXCWallet) Balance() (*asset.Balance, error) {
	if w.balErr != nil {
		return nil, w.balErr
	}
	if w.bal == nil {
		w.bal = new(asset.Balance)
	}
	return w.bal, nil
}

func (w *TXCWallet) Fund(v uint64, _ *dex.Asset) (asset.Coins, error) {
	w.fundedVal = v
	return w.fundCoins, w.fundErr
}

func (w *TXCWallet) ReturnCoins(asset.Coins) error {
	return nil
}

func (w *TXCWallet) FundingCoins([]dex.Bytes) (asset.Coins, error) {
	return w.fundingCoins, w.fundingCoinErr
}

func (w *TXCWallet) Swap(swap *asset.Swaps, _ *dex.Asset) ([]asset.Receipt, asset.Coin, error) {
	return w.swapReceipts, &tCoin{id: []byte{0x0a, 0x0b}}, nil
}

func (w *TXCWallet) Redeem([]*asset.Redemption, *dex.Asset) ([]dex.Bytes, asset.Coin, error) {
	return w.redeemCoins, &tCoin{id: []byte{0x0c, 0x0d}}, nil
}

func (w *TXCWallet) SignMessage(asset.Coin, dex.Bytes) (pubkeys, sigs []dex.Bytes, err error) {
	return nil, nil, w.signCoinErr
}

func (w *TXCWallet) AuditContract(coinID, contract dex.Bytes) (asset.AuditInfo, error) {
	return w.auditInfo, w.auditErr
}

func (w *TXCWallet) LocktimeExpired(contract dex.Bytes) (bool, error) {
	return true, nil
}

func (w *TXCWallet) FindRedemption(ctx context.Context, coinID dex.Bytes) (dex.Bytes, error) {
	return nil, nil
}

func (w *TXCWallet) Refund(dex.Bytes, dex.Bytes, *dex.Asset) (dex.Bytes, error) {
	return w.refundCoin, w.refundErr
}

func (w *TXCWallet) Address() (string, error) {
	return "", w.addrErr
}

func (w *TXCWallet) Unlock(pw string, dur time.Duration) error {
	return w.unlockErr
}

func (w *TXCWallet) Lock() error {
	return w.lockErr
}

func (w *TXCWallet) Send(address string, fee uint64, _ *dex.Asset) (asset.Coin, error) {
	w.mtx.RLock()
	defer w.mtx.RUnlock()
	return w.payFeeCoin, w.payFeeErr
}

func (w *TXCWallet) Confirmations(id dex.Bytes) (uint32, error) {
	return 0, nil
}

func (w *TXCWallet) ConfirmTime(id dex.Bytes, nConfs uint32) (time.Time, error) {
	return time.Time{}, nil
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
				tDexHost: dc,
			},
			lockTimeTaker: dex.LockTimeTaker(dex.Testnet),
			lockTimeMaker: dex.LockTimeMaker(dex.Testnet),
			wallets:       make(map[uint32]*xcWallet),
			blockWaiters:  make(map[uint64]*blockWaiter),
			wsConstructor: func(*comms.WsCfg) (comms.WsConn, error) {
				return conn, nil
			},
			newCrypter: func([]byte) encrypt.Crypter { return crypter },
			reCrypter:  func([]byte, []byte) (encrypt.Crypter, error) { return crypter, crypter.recryptErr },
		},
		db:      db,
		queue:   queue,
		ws:      conn,
		dc:      dc,
		acct:    acct,
		crypter: crypter,
	}
}

func (rig *testRig) queueConfig() {
	rig.ws.queueResponse(msgjson.ConfigRoute, func(msg *msgjson.Message, f msgFunc) error {
		resp, _ := msgjson.NewResponse(msg.ID, rig.dc.cfg, nil)
		f(resp)
		return nil
	})
}

func (rig *testRig) queueRegister(regRes *msgjson.RegisterResult) {
	rig.ws.queueResponse(msgjson.RegisterRoute, func(msg *msgjson.Message, f msgFunc) error {
		resp, _ := msgjson.NewResponse(msg.ID, regRes, nil)
		f(resp)
		return nil
	})
}

func (rig *testRig) queueNotifyFee() {
	rig.ws.queueResponse(msgjson.NotifyFeeRoute, func(msg *msgjson.Message, f msgFunc) error {
		req := new(msgjson.NotifyFee)
		json.Unmarshal(msg.Payload, req)
		sigMsg := req.Serialize()
		sig, _ := tDexPriv.Sign(sigMsg)
		// Shouldn't Sig be dex.Bytes?
		result := &msgjson.Acknowledgement{Sig: sig.Serialize()}
		resp, _ := msgjson.NewResponse(msg.ID, result, nil)
		f(resp)
		return nil
	})
}

func (rig *testRig) queueConnect(rpcErr *msgjson.Error) {
	rig.ws.queueResponse(msgjson.ConnectRoute, func(msg *msgjson.Message, f msgFunc) error {
		connect := new(msgjson.Connect)
		msg.Unmarshal(connect)
		sign(tDexPriv, connect)
		result := &msgjson.ConnectResult{Sig: connect.Sig}
		resp, _ := msgjson.NewResponse(msg.ID, result, rpcErr)
		f(resp)
		return nil
	})
}

func tMarketID(base, quote uint32) string {
	return strconv.Itoa(int(base)) + "-" + strconv.Itoa(int(quote))
}

func TestMain(m *testing.M) {
	UseLoggerMaker(&dex.LoggerMaker{
		Backend:      slog.NewBackend(os.Stdout),
		DefaultLevel: slog.LevelTrace,
	})
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
		marketIDs[marketName(base.ID, quote.ID)] = struct{}{}
		cfg := rig.dc.cfg
		cfg.Markets = append(cfg.Markets, &msgjson.Market{
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
			mkt := marketName(market.BaseID, market.QuoteID)
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
	rig := newTestRig()
	tCore := rig.core
	dc := rig.dc

	// Ensure handleOrderBookMsg creates an order book as expected.
	oid1 := ordertest.RandomOrderID()
	bookMsg, err := msgjson.NewResponse(1, &msgjson.OrderBook{
		Seq:      1,
		MarketID: tDcrBtcMktName,
		Orders: []*msgjson.BookOrderNote{
			{
				TradeNote: msgjson.TradeNote{
					Side:     msgjson.BuyOrderNum,
					Quantity: 10,
					Rate:     2,
				},
				OrderNote: msgjson.OrderNote{
					Seq:      1,
					MarketID: tDcrBtcMktName,
					OrderID:  oid1[:],
				},
			},
		},
	}, nil)
	if err != nil {
		t.Fatalf("[NewResponse]: unexpected err: %v", err)
	}

	oid2 := ordertest.RandomOrderID()
	bookNote, _ := msgjson.NewNotification(msgjson.BookOrderRoute, &msgjson.BookOrderNote{
		TradeNote: msgjson.TradeNote{
			Side:     msgjson.BuyOrderNum,
			Quantity: 10,
			Rate:     2,
		},
		OrderNote: msgjson.OrderNote{
			Seq:      2,
			MarketID: tDcrBtcMktName,
			OrderID:  oid2[:],
		},
	})

	err = handleBookOrderMsg(tCore, dc, bookNote)
	if err == nil {
		t.Fatalf("no error for missing book")
	}

	// Sync to unknown dex
	_, _, err = tCore.Sync("unknown dex", tDCR.ID, tBTC.ID)
	if err == nil {
		t.Fatalf("no error for unknown dex")
	}
	_, _, err = tCore.Sync(tDexHost, tDCR.ID, 12345)
	if err == nil {
		t.Fatalf("no error for nonsense market")
	}

	// Success
	rig.ws.queueResponse(msgjson.OrderBookRoute, func(msg *msgjson.Message, f msgFunc) error {
		f(bookMsg)
		return nil
	})
	_, feed1, err := tCore.Sync(tDexHost, tDCR.ID, tBTC.ID)
	if err != nil {
		t.Fatalf("Sync 1 error: %v", err)
	}
	_, feed2, err := tCore.Sync(tDexHost, tDCR.ID, tBTC.ID)
	if err != nil {
		t.Fatalf("Sync 2 error: %v", err)
	}

	// Should be able to retrieve the book now.
	book, err := tCore.Book(tDexHost, tDCR.ID, tBTC.ID)
	if err != nil {
		t.Fatalf("Core.Book error: %v", err)
	}
	// Should have one buy order
	if len(book.Buys) != 1 {
		t.Fatalf("no buy orders found. expected 1")
	}

	err = handleBookOrderMsg(tCore, dc, bookNote)
	if err != nil {
		t.Fatalf("[handleBookOrderMsg]: unexpected err: %v", err)
	}

	// Both channels should have an update.
	select {
	case <-feed1.C:
	default:
		t.Fatalf("no update received on feed 1")
	}
	select {
	case <-feed2.C:
	default:
		t.Fatalf("no update received on feed 2")
	}

	// Close feed 1
	feed1.Close()

	oid3 := ordertest.RandomOrderID()
	bookNote, _ = msgjson.NewNotification(msgjson.BookOrderRoute, &msgjson.BookOrderNote{
		TradeNote: msgjson.TradeNote{
			Side:     msgjson.SellOrderNum,
			Quantity: 10,
			Rate:     3,
		},
		OrderNote: msgjson.OrderNote{
			Seq:      3,
			MarketID: tDcrBtcMktName,
			OrderID:  oid3[:],
		},
	})
	err = handleBookOrderMsg(tCore, dc, bookNote)
	if err != nil {
		t.Fatalf("[handleBookOrderMsg]: unexpected err: %v", err)
	}

	// feed1 should have no update
	select {
	case <-feed1.C:
		t.Fatalf("update for feed 1 after Close")
	default:
	}
	// feed2 should though
	select {
	case <-feed2.C:
	default:
		t.Fatalf("no update received on feed 2")
	}

	// Make sure the book has been updated.
	book, _ = tCore.Book(tDexHost, tDCR.ID, tBTC.ID)
	if len(book.Buys) != 2 {
		t.Fatalf("expected 2 buys, got %d", len(book.Buys))
	}
	if len(book.Sells) != 1 {
		t.Fatalf("expected 1 sell, got %d", len(book.Sells))
	}

	// Update the remaining quantity of the just booked order.
	bookNote, _ = msgjson.NewNotification(msgjson.BookOrderRoute, &msgjson.UpdateRemainingNote{
		OrderNote: msgjson.OrderNote{
			Seq:      4,
			MarketID: tDcrBtcMktName,
			OrderID:  oid3[:],
		},
		Remaining: 5 * 1e8,
	})
	err = handleUpdateRemainingMsg(tCore, dc, bookNote)
	if err != nil {
		t.Fatalf("[handleBookOrderMsg]: unexpected err: %v", err)
	}

	// feed2 should have an update
	select {
	case <-feed2.C:
	default:
		t.Fatalf("no update received on feed 2")
	}
	book, _ = tCore.Book(tDexHost, tDCR.ID, tBTC.ID)
	firstSellQty := book.Sells[0].Qty
	if firstSellQty != 5 {
		t.Fatalf("expected remaining quantity of 5.00000000 after update_remaining. got %.8f", firstSellQty)
	}

	// Ensure handleUnbookOrderMsg removes a book order from an associated
	// order book as expected.
	unbookNote, _ := msgjson.NewNotification(msgjson.UnbookOrderRoute, &msgjson.UnbookOrderNote{
		Seq:      5,
		MarketID: tDcrBtcMktName,
		OrderID:  oid1[:],
	})

	err = handleUnbookOrderMsg(tCore, dc, unbookNote)
	if err != nil {
		t.Fatalf("[handleUnbookOrderMsg]: unexpected err: %v", err)
	}
	// feed2 should have a notification.
	select {
	case <-feed2.C:
	default:
		t.Fatalf("no update received on feed 2")
	}
	book, _ = tCore.Book(tDexHost, tDCR.ID, tBTC.ID)
	if len(book.Buys) != 1 {
		t.Fatalf("expected 1 buy after unbook_order, got %d", len(book.Buys))
	}
}

type tDriver struct {
	f       func(*asset.WalletConfig, dex.Logger, dex.Network) (asset.Wallet, error)
	decoder func(coinID []byte) (string, error)
	winfo   *asset.WalletInfo
}

func (drv *tDriver) Setup(cfg *asset.WalletConfig, logger dex.Logger, net dex.Network) (asset.Wallet, error) {
	return drv.f(cfg, logger, net)
}

func (drv *tDriver) DecodeCoinID(coinID []byte) (string, error) {
	return drv.decoder(coinID)
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

	// ILT is based on DCR, so use it's coinID decoder. If this changes, update
	// the following decoder function:
	decoder := func(coinID []byte) (string, error) {
		return asset.DecodeCoinID(tDCR.ID, coinID) // using DCR decoder
	}

	// Create registration form.
	form := &WalletForm{
		AssetID:    tILT.ID,
		Account:    "default",
		ConfigText: "rpclisten=localhost",
	}

	ensureErr := func(tag string) {
		err := tCore.CreateWallet(tPW, []byte(wPW), form)
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
		decoder: decoder,
		winfo:   &asset.WalletInfo{},
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
	err := tCore.CreateWallet(tPW, []byte(wPW), form)
	if err != nil {
		t.Fatalf("error when should be no error: %v", err)
	}
}

func TestGetFee(t *testing.T) {
	rig := newTestRig()
	tCore := rig.core

	// DEX already registered
	_, err := tCore.GetFee(tDexHost, "")
	if !errorHasCode(err, dupeDEXErr) {
		t.Fatalf("wrong account exists error: %v", err)
	}

	// Lose the dexConnection
	tCore.connMtx.Lock()
	delete(tCore.conns, tDexHost)
	tCore.connMtx.Unlock()

	// connectDEX error
	_, err = tCore.GetFee(tUnparseableHost, "")
	if !errorHasCode(err, connectionErr) {
		t.Fatalf("wrong connectDEX error: %v", err)
	}

	// Queue a config response for success
	rig.queueConfig()

	// Success
	_, err = tCore.GetFee(tDexHost, "")
	if err != nil {
		t.Fatalf("GetFee error: %v", err)
	}
}

func TestRegister(t *testing.T) {
	// This test takes a little longer because the key is decrypted every time
	// Register is called.
	rig := newTestRig()
	tCore := rig.core
	dc := rig.dc
	delete(tCore.conns, tDexHost)

	wallet, tWallet := newTWallet(tDCR.ID)
	tCore.wallets[tDCR.ID] = wallet

	// When registering, successfully retrieving *db.AccountInfo from the DB is
	// an error (no dupes). Initial state is to return an error.
	rig.db.acctErr = tErr

	regRes := &msgjson.RegisterResult{
		DEXPubKey:    rig.acct.dexPubKey.Serialize(),
		ClientPubKey: dex.Bytes{0x1}, // part of the serialization, but not the response
		Address:      "someaddr",
		Fee:          tFee,
		Time:         encode.UnixMilliU(time.Now()),
	}
	sign(tDexPriv, regRes)

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
						tWallet.setConfs(0)
						tCore.tipChange(tDCR.ID, nil)
						return
					}
				case <-timeout.C:
					t.Fatalf("failed to find waiter before timeout")
				}
			}
		}()
	}

	queueResponses := func() {
		rig.queueConfig()
		rig.queueRegister(regRes)
		queueTipChange()
		rig.queueNotifyFee()
		rig.queueConnect(nil)
	}

	form := &RegisterForm{
		Addr:    tDexHost,
		AppPass: tPW,
		Fee:     tFee,
		Cert:    "required",
	}

	tWallet.payFeeCoin = &tCoin{id: []byte("abcdef")}

	ch := tCore.NotificationFeed()

	var err error
	run := func() {
		// Register method will error if url is already in conns map.
		tCore.connMtx.Lock()
		delete(tCore.conns, tDexHost)
		tCore.connMtx.Unlock()

		tWallet.setConfs(0)
		_, err = tCore.Register(form)
	}

	getNotification := func(tag string) interface{} {
		select {
		case n := <-ch:
			return n
			// When it works, it should be virtually instant, but I have seen it fail
			// at 1 millisecond.
		case <-time.NewTimer(time.Second).C:
			t.Fatalf("timed out waiting for %s notification", tag)
		}
		return nil
	}

	getBalanceNote := func() *BalanceNote {
		note, ok := getNotification("balance").(*BalanceNote)
		if !ok {
			t.Fatalf("wrong notification. Expected BalanceNote")
		}
		return note
	}

	getFeeNote := func() *FeePaymentNote {
		note, ok := getNotification("feepayment").(*FeePaymentNote)
		if !ok {
			t.Fatalf("wrong notification. Expected FeePaymentNote")
		}
		return note
	}

	queueResponses()
	run()
	if err != nil {
		t.Fatalf("registration error: %v", err)
	}
	// Should be two success notifications. One for fee paid on-chain, one for
	// fee notification sent.
	getBalanceNote()
	feeNote := getFeeNote()
	if feeNote.Severity() != db.Success {
		t.Fatalf("fee payment error notification: %s: %s", feeNote.Subject(), feeNote.Details())
	}
	feeNote = getFeeNote()
	if feeNote.Severity() != db.Success {
		t.Fatalf("fee payment error notification: %s: %s", feeNote.Subject(), feeNote.Details())
	}

	// password error
	rig.crypter.recryptErr = tErr
	_, err = tCore.Register(form)
	if !errorHasCode(err, passwordErr) {
		t.Fatalf("wrong password error: %v", err)
	}
	rig.crypter.recryptErr = nil

	// no host error
	form.Addr = ""
	_, err = tCore.Register(form)
	if !errorHasCode(err, emptyHostErr) {
		t.Fatalf("wrong empty host error: %v", err)
	}
	form.Addr = tDexHost

	// account already exists
	tCore.connMtx.Lock()
	tCore.conns[tDexHost] = dc
	tCore.connMtx.Unlock()
	_, err = tCore.Register(form)
	if !errorHasCode(err, dupeDEXErr) {
		t.Fatalf("wrong account exists error: %v", err)
	}

	// wallet not found
	delete(tCore.wallets, tDCR.ID)
	run()
	if !errorHasCode(err, walletErr) {
		t.Fatalf("wrong missing wallet error: %v", err)
	}
	tCore.wallets[tDCR.ID] = wallet

	// Unlock wallet error
	tWallet.unlockErr = tErr
	wallet.lockTime = time.Time{}
	_, err = tCore.Register(form)
	if !errorHasCode(err, walletAuthErr) {
		t.Fatalf("wrong wallet auth error: %v", err)
	}
	tWallet.unlockErr = nil

	// connectDEX error
	form.Addr = tUnparseableHost
	_, err = tCore.Register(form)
	if !errorHasCode(err, connectionErr) {
		t.Fatalf("wrong connectDEX error: %v", err)
	}
	form.Addr = tDexHost

	// asset not found
	cfgAssets := dc.cfg.Assets
	mkts := dc.cfg.Markets
	dc.cfg.Assets = dc.cfg.Assets[1:]
	dc.cfg.Markets = []*msgjson.Market{}
	rig.queueConfig()
	run()
	if !errorHasCode(err, assetSupportErr) {
		t.Fatalf("wrong error for missing asset: %v", err)
	}
	dc.cfg.Assets = cfgAssets
	dc.cfg.Markets = mkts

	// error creating signing key
	rig.crypter.encryptErr = tErr
	rig.queueConfig()
	run()
	if !errorHasCode(err, acctKeyErr) {
		t.Fatalf("wrong account key error: %v", err)
	}
	rig.crypter.encryptErr = nil

	// register request error
	rig.queueConfig()
	rig.ws.queueResponse(msgjson.RegisterRoute, func(msg *msgjson.Message, f msgFunc) error {
		return tErr
	})
	run()
	if !errorHasCode(err, registerErr) {
		t.Fatalf("wrong error for register request error: %v", err)
	}

	// signature error
	goodSig := regRes.Sig
	regRes.Sig = []byte("badsig")
	rig.queueConfig()
	rig.queueRegister(regRes)
	run()
	if !errorHasCode(err, signatureErr) {
		t.Fatalf("wrong error for bad signature on register response: %v", err)
	}
	regRes.Sig = goodSig

	// zero fee error
	goodFee := regRes.Fee
	regRes.Fee = 0
	rig.queueConfig()
	rig.queueRegister(regRes)
	run()
	if !errorHasCode(err, zeroFeeErr) {
		t.Fatalf("wrong error for zero fee: %v", err)
	}

	// wrong fee error
	regRes.Fee = tFee + 1
	rig.queueConfig()
	rig.queueRegister(regRes)
	run()
	if !errorHasCode(err, feeMismatchErr) {
		t.Fatalf("wrong error for wrong fee: %v", err)
	}
	regRes.Fee = goodFee

	// Form fee error
	form.Fee = tFee + 1
	rig.queueConfig()
	rig.queueRegister(regRes)
	run()
	if !errorHasCode(err, feeMismatchErr) {
		t.Fatalf("wrong error for wrong fee in form: %v", err)
	}
	form.Fee = tFee

	// PayFee error
	rig.queueConfig()
	rig.queueRegister(regRes)
	tWallet.payFeeErr = tErr
	run()
	if !errorHasCode(err, feeSendErr) {
		t.Fatalf("no error for PayFee error")
	}
	tWallet.payFeeErr = nil

	// notifyfee response error
	rig.queueConfig()
	rig.queueRegister(regRes)
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
	// 1st note is balance.
	getBalanceNote()
	// 2nd is feepayment.
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
	getBalanceNote()
	feeNote = getFeeNote()
	if feeNote.Severity() != db.Success {
		t.Fatalf("fee payment error notification: %s: %s", feeNote.Subject(), feeNote.Details())
	}
}

func TestReinstate(t *testing.T) {
	rig := newTestRig()
	tCore := rig.core
	dc := rig.dc
	acct := dc.acct

	dexStat := &DEXBrief{
		Host:         tDexHost,
		AcctNotFound: true,
	}

	wallet, _ := newTWallet(tDCR.ID)
	tCore.wallets[tDCR.ID] = wallet

	regRes := &msgjson.RegisterResult{
		DEXPubKey:    acct.dexPubKey.Serialize(),
		ClientPubKey: dex.Bytes{0x1}, // part of the serialization, but not the response
		Address:      "someaddr",
		Fee:          tFee,
		Time:         encode.UnixMilliU(time.Now()),
	}
	sign(tDexPriv, regRes)

	queueReinstate := func() {
		rig.ws.queueResponse(msgjson.ReinstateRoute, func(msg *msgjson.Message, f msgFunc) error {
			resp, _ := msgjson.NewResponse(msg.ID, regRes, nil)
			f(resp)
			return nil
		})
	}

	queueReinstate()
	rig.queueConnect(nil)
	err := tCore.reinstateDexAccount(dexStat, rig.crypter)
	if err != nil {
		t.Fatalf("reinstate error: %v", err)
	}
	if dexStat.AcctNotFound {
		t.Fatal("account not found still true after reinstate")
	}
}

func TestLogin(t *testing.T) {
	rig := newTestRig()
	tCore := rig.core
	rig.acct.markFeePaid()

	rig.queueConnect(nil)
	_, err := tCore.Login(tPW)
	if err != nil || !rig.acct.authed() {
		t.Fatalf("initial Login error: %v", err)
	}

	// No encryption key.
	rig.acct.unauth()
	rig.db.encKeyErr = tErr
	_, err = tCore.Login(tPW)
	if err == nil || rig.acct.authed() {
		t.Fatalf("no error for missing app key")
	}
	rig.db.encKeyErr = nil

	// Account not Paid. No error, and account should be unlocked.
	rig.acct.isPaid = false
	_, err = tCore.Login(tPW)
	if err != nil || rig.acct.authed() {
		t.Fatalf("error for unpaid account: %v", err)
	}
	if rig.acct.locked() {
		t.Fatalf("unpaid account is locked")
	}
	rig.acct.markFeePaid()

	// 'connect' route error.
	rig.acct.unauth()
	rig.ws.queueResponse(msgjson.ConnectRoute, func(msg *msgjson.Message, f msgFunc) error {
		resp, _ := msgjson.NewResponse(msg.ID, nil, msgjson.NewError(1, "test error"))
		f(resp)
		return nil
	})
	_, err = tCore.Login(tPW)
	// Should be no error, but also not authed. Error is sent and logged
	// as a notification.
	if err != nil || rig.acct.authed() {
		t.Fatalf("account authed after 'connect' error")
	}

	// Success again.
	rig.queueConnect(nil)
	_, err = tCore.Login(tPW)
	if err != nil || !rig.acct.authed() {
		t.Fatalf("final Login error: %v", err)
	}
}

func TestLoginAccountNotFoundError(t *testing.T) {
	rig := newTestRig()
	tCore := rig.core
	rig.acct.markFeePaid()
	dc := rig.dc
	acct := dc.acct

	accountNotFoundError := msgjson.NewError(msgjson.AccountNotFoundError, "test account not found error")
	rig.queueConnect(accountNotFoundError)

	regRes := &msgjson.RegisterResult{
		DEXPubKey:    acct.dexPubKey.Serialize(),
		ClientPubKey: dex.Bytes{0x1}, // part of the serialization, but not the response
		Address:      "someaddr",
		Fee:          tFee,
		Time:         encode.UnixMilliU(time.Now()),
	}
	queueReinstate := func() {
		rig.ws.queueResponse(msgjson.ReinstateRoute, func(msg *msgjson.Message, f msgFunc) error {
			resp, _ := msgjson.NewResponse(msg.ID, regRes, nil)
			f(resp)
			return nil
		})
	}
	queueReinstate()

	wallet, _ := newTWallet(tDCR.ID)
	tCore.wallets[tDCR.ID] = wallet
	rig.queueConnect(nil)

	result, err := tCore.Login(tPW)
	if err != nil {
		t.Fatalf("unexpected Login error: %v", err)
	}
	for _, dexStat := range result.DEXes {
		if dexStat.AcctNotFound {
			t.Fatalf("expected account not found error: %v", msgjson.AccountNotFoundError)
		}
	}
}

func TestInitializeDEXConnectionsSuccess(t *testing.T) {
	rig := newTestRig()
	tCore := rig.core
	rig.acct.markFeePaid()

	rig.queueConnect(nil)
	dexStats := tCore.initializeDEXConnections(rig.crypter)
	if dexStats == nil {
		t.Fatal("initializeDEXConnections failure")
	}
	for _, dexStat := range dexStats {
		if dexStat.AuthErr != "" {
			t.Fatalf("initializeDEXConnections authorization error %v", dexStat.AuthErr)
		}
	}
}

func TestInitializeDEXConnectionsAccountNotFoundError(t *testing.T) {
	rig := newTestRig()
	tCore := rig.core
	rig.acct.markFeePaid()

	rig.queueConnect(msgjson.NewError(msgjson.AccountNotFoundError, "test account not found error"))
	dexStats := tCore.initializeDEXConnections(rig.crypter)
	if dexStats == nil {
		t.Fatal("initializeDEXConnections failure")
	}
	for _, dexStat := range dexStats {
		if dexStat.AuthErr == "" {
			t.Fatalf("initializeDEXConnections authorization error %v", dexStat.AuthErr)
		}
		if !dexStat.AcctNotFound {
			t.Fatal("initializeDEXConnections expected account not found")
		}
	}
}

func TestConnectDEX(t *testing.T) {
	rig := newTestRig()
	tCore := rig.core

	ai := &db.AccountInfo{
		Host: "somedex.com",
	}

	rig.queueConfig()
	_, err := tCore.connectDEX(ai)
	if err != nil {
		t.Fatalf("initial connectDEX error: %v", err)
	}

	// Bad URL.
	ai.Host = tUnparseableHost // Illegal ASCIII control character
	_, err = tCore.connectDEX(ai)
	if err == nil {
		t.Fatalf("no error for bad URL")
	}
	ai.Host = "someotherdex.org"

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
	rig.queueConfig()
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
	emptyPass := []byte("")
	err = tCore.InitializeClient(emptyPass)
	if err == nil {
		t.Fatalf("no error for empty password")
	}

	// Store error. Use a non-empty password to pass empty password check.
	rig.db.storeErr = tErr
	err = tCore.InitializeClient(tPW)
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
	wallet, tWallet := newTWallet(tDCR.ID)
	tCore.wallets[tDCR.ID] = wallet
	address := "addr"

	// Successful
	_, err := tCore.Withdraw(tPW, tDCR.ID, 1e8, address)
	if err != nil {
		t.Fatalf("withdraw error: %v", err)
	}

	// 0 value
	_, err = tCore.Withdraw(tPW, tDCR.ID, 0, address)
	if err == nil {
		t.Fatalf("no error for zero value withdraw")
	}

	// no wallet
	_, err = tCore.Withdraw(tPW, 12345, 1e8, address)
	if err == nil {
		t.Fatalf("no error for unknown wallet")
	}

	// connect error
	wallet.hookedUp = false
	tWallet.connectErr = tErr
	_, err = tCore.Withdraw(tPW, tDCR.ID, 1e8, address)
	if err == nil {
		t.Fatalf("no error for wallet connect error")
	}
	tWallet.connectErr = nil

	// Send error
	tWallet.payFeeErr = tErr
	_, err = tCore.Withdraw(tPW, tDCR.ID, 1e8, address)
	if err == nil {
		t.Fatalf("no error for wallet Send error")
	}
	tWallet.payFeeErr = nil

	// Check the coin.
	tWallet.payFeeCoin = &tCoin{id: []byte{'a'}}
	coin, err := tCore.Withdraw(tPW, tDCR.ID, 1e8, address)
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
	dcrWallet.Unlock(wPW, time.Hour)

	btcWallet, tBtcWallet := newTWallet(tBTC.ID)
	tCore.wallets[tBTC.ID] = btcWallet
	btcWallet.address = "12DXGkvxFjuq5btXYkwWfBZaz1rVwFgini"
	btcWallet.Unlock(wPW, time.Hour)

	qty := tDCR.LotSize * 10
	rate := tBTC.RateStep * 1000

	form := &TradeForm{
		Host:    tDexHost,
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

	book := newBookie(func() {})
	rig.dc.books[tDcrBtcMktName] = book

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

	err := book.Sync(&msgjson.OrderBook{
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
	form.Host = "someotherdex.org"
	_, err = tCore.Trade(tPW, form)
	if err == nil {
		t.Fatalf("no error for unknown dex")
	}
	form.Host = tDexHost

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
	lo, dbOrder, preImg, _ := makeLimitOrder(dc, true, 0, 0)
	oid := lo.ID()
	mkt := dc.market(tDcrBtcMktName)
	tracker := newTrackedTrade(dbOrder, preImg, dc, mkt.EpochLen, rig.core.lockTimeTaker, rig.core.lockTimeMaker,
		rig.db, rig.queue, nil, nil, rig.core.notify)
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
		Order:    ord,
		preImg:   preImg,
		dc:       rig.dc,
		metaData: &db.OrderMetaData{},
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

func TestHandleRevokeMatchMsg(t *testing.T) {
	rig := newTestRig()
	ord := &order.LimitOrder{P: order.Prefix{ServerTime: time.Now()}}
	oid := ord.ID()
	mid := ordertest.RandomMatchID()
	preImg := newPreimage()
	payload := &msgjson.RevokeMatch{
		OrderID: oid[:],
		MatchID: mid[:],
	}
	req, _ := msgjson.NewRequest(rig.dc.NextID(), msgjson.RevokeMatchRoute, payload)

	// Ensure revoking a non-existent order generates an error.
	err := handleRevokeMatchMsg(rig.core, rig.dc, req)
	if err == nil {
		t.Fatal("[handleRevokeMatchMsg] expected a non-existent order")
	}

	match := &matchTracker{
		id: mid,
		MetaMatch: db.MetaMatch{
			MetaData: &db.MatchMetaData{},
			Match:    &order.UserMatch{},
		},
	}

	tracker := &trackedTrade{
		db: rig.db,
		metaData: &db.OrderMetaData{
			Status: order.OrderStatusBooked,
		},
		Order: ord,
		matches: map[order.MatchID]*matchTracker{
			mid: match,
		},
		preImg: preImg,
		dc:     rig.dc,
	}
	rig.dc.trades[oid] = tracker

	err = handleRevokeMatchMsg(rig.core, rig.dc, req)
	if err != nil {
		t.Fatalf("handleRevokeMatchMsg error: %v", err)
	}

	// Ensure the order status has been updated to revoked.
	tracker, _, _ = rig.dc.findOrder(oid)
	if tracker == nil {
		t.Fatalf("expected to find an order with id %s", oid.String())
	}

	if !match.MetaData.Proof.IsRevoked {
		t.Fatal("match not revoked")
	}
}

func TestTradeTracking(t *testing.T) {
	rig := newTestRig()
	dc := rig.dc
	tCore := rig.core
	dcrWallet, tDcrWallet := newTWallet(tDCR.ID)
	tCore.wallets[tDCR.ID] = dcrWallet
	dcrWallet.address = "DsVmA7aqqWeKWy461hXjytbZbgCqbB8g2dq"
	dcrWallet.Unlock(wPW, time.Hour)

	btcWallet, tBtcWallet := newTWallet(tBTC.ID)
	tCore.wallets[tBTC.ID] = btcWallet
	btcWallet.address = "12DXGkvxFjuq5btXYkwWfBZaz1rVwFgini"
	btcWallet.Unlock(wPW, time.Hour)

	matchSize := 4 * tDCR.LotSize
	cancelledQty := tDCR.LotSize
	qty := 2*matchSize + cancelledQty
	rate := tBTC.RateStep * 10
	lo, dbOrder, preImgL, addr := makeLimitOrder(dc, true, qty, tBTC.RateStep)
	loid := lo.ID()
	mid := ordertest.RandomMatchID()
	walletSet, err := tCore.walletSet(dc, tDCR.ID, tBTC.ID, true)
	if err != nil {
		t.Fatalf("walletSet error: %v", err)
	}
	mkt := dc.market(tDcrBtcMktName)
	tracker := newTrackedTrade(dbOrder, preImgL, dc, mkt.EpochLen, rig.core.lockTimeTaker, rig.core.lockTimeMaker,
		rig.db, rig.queue, walletSet, nil, rig.core.notify)
	rig.dc.trades[tracker.ID()] = tracker
	var match *matchTracker
	checkStatus := func(tag string, wantStatus order.MatchStatus) {
		if match.Match.Status != wantStatus {
			t.Fatalf("%s: wrong status wanted %d, got %d", tag, match.Match.Status, wantStatus)
		}
	}

	// MAKER MATCH
	//
	matchTime := time.Now()
	msgMatch := &msgjson.Match{
		OrderID:    loid[:],
		MatchID:    mid[:],
		Quantity:   matchSize,
		Rate:       rate,
		Address:    "counterparty-address",
		Side:       uint8(order.Maker),
		ServerTime: encode.UnixMilliU(matchTime),
	}
	counterSwapID := encode.RandomBytes(36)
	tDcrWallet.swapReceipts = []asset.Receipt{&tReceipt{coin: &tCoin{id: counterSwapID}}}
	sign(tDexPriv, msgMatch)
	msg, _ := msgjson.NewRequest(1, msgjson.MatchRoute, []*msgjson.Match{msgMatch})
	// queue an invalid DEX init ack
	rig.ws.queueResponse(msgjson.InitRoute, invalidAcker)
	err = handleMatchRoute(tCore, rig.dc, msg)
	if err == nil {
		t.Fatalf("no error for invalid server ack for init route")
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
	// auth.InitSig should be unset because our init request received
	// an invalid ack
	if len(auth.InitSig) != 0 {
		t.Fatalf("init sig recorded for invalid init ack")
	}

	// requeue an invalid DEX init ack and re-send pending init request
	rig.ws.queueResponse(msgjson.InitRoute, invalidAcker)
	err = tracker.resendPendingRequests()
	if err == nil {
		t.Fatalf("no error for invalid server ack for resent init request")
	}
	// auth.InitSig should remain unset because our resent init request
	// received an invalid ack still
	if len(auth.InitSig) != 0 {
		t.Fatalf("init sig recorded for second invalid init ack")
	}

	// queue a valid DEX init ack and re-send pending init request
	rig.ws.queueResponse(msgjson.InitRoute, initAcker)
	err = tracker.resendPendingRequests()
	if err != nil {
		t.Fatalf("unexpected error for resent pending init request: %v", err)
	}
	// auth.InitSig should now be set because our init request received
	// a valid ack
	if len(auth.InitSig) == 0 {
		t.Fatalf("init sig not recorded for valid init ack")
	}

	// Send the counter-party's init info.
	auditQty := calc.BaseToQuote(rate, matchSize)
	audit, auditInfo := tMsgAudit(loid, mid, addr, auditQty, proof.SecretHash)
	auditInfo.expiration = encode.DropMilliseconds(matchTime.Add(tracker.lockTimeTaker))
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

	auditInfo.expiration = matchTime.Add(time.Hour * 23)
	err = handleAuditRoute(tCore, rig.dc, msg)
	if err == nil {
		t.Fatalf("no maker error for early lock time")
	}
	auditInfo.expiration = matchTime.Add(time.Hour * 24)

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
		OrderID:    loid[:],
		MatchID:    mid[:],
		Quantity:   matchSize,
		Rate:       rate,
		Address:    "counterparty-address",
		Side:       uint8(order.Taker),
		ServerTime: encode.UnixMilliU(matchTime),
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
	auditInfo.expiration = encode.DropMilliseconds(matchTime.Add(tracker.lockTimeMaker))
	msg, _ = msgjson.NewRequest(1, msgjson.AuditRoute, audit)
	err = handleAuditRoute(tCore, rig.dc, msg)
	if err != nil {
		t.Fatalf("taker's match message error: %v", err)
	}

	auditInfo.expiration = matchTime.Add(time.Hour * 47)
	err = handleAuditRoute(tCore, rig.dc, msg)
	if err == nil {
		t.Fatalf("no taker error for early lock time")
	}
	auditInfo.expiration = matchTime.Add(time.Hour * 48)

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

func TestRefunds(t *testing.T) {
	checkStatus := func(tag string, match *matchTracker, wantStatus order.MatchStatus) {
		if match.Match.Status != wantStatus {
			t.Fatalf("%s: wrong status wanted %d, got %d", tag, match.Match.Status, wantStatus)
		}
	}
	checkRefund := func(tracker *trackedTrade, match *matchTracker, expectAmt uint64) {
		// Confirm that the status is SwapCast.
		if match.Match.Side == order.Maker {
			checkStatus("maker swapped", match, order.MakerSwapCast)
		} else {
			checkStatus("taker swapped", match, order.TakerSwapCast)
		}
		// Confirm isRefundable = true.
		if !tracker.isRefundable(match) {
			t.Fatalf("%s's swap not refundable", match.Match.Side)
		}
		// Check refund.
		amtRefunded, err := tracker.refundMatches([]*matchTracker{match})
		if err != nil {
			t.Fatalf("unexpected refund error %v", err)
		}
		// Check refunded amount.
		if amtRefunded != expectAmt {
			t.Fatalf("expected %d refund amount, got %d", expectAmt, amtRefunded)
		}
		// Confirm isRefundable = false.
		if tracker.isRefundable(match) {
			t.Fatalf("%s's swap refundable after being refunded", match.Match.Side)
		}
		// Expect refund re-attempt to not refund any coin.
		amtRefunded, err = tracker.refundMatches([]*matchTracker{match})
		if err != nil {
			t.Fatalf("unexpected refund error %v", err)
		}
		if amtRefunded != 0 {
			t.Fatalf("expected 0 refund amount, got %d", amtRefunded)
		}
		// Confirm that the status is unchanged.
		if match.Match.Side == order.Maker {
			checkStatus("maker swapped", match, order.MakerSwapCast)
		} else {
			checkStatus("taker swapped", match, order.TakerSwapCast)
		}
	}

	rig := newTestRig()
	dc := rig.dc
	tCore := rig.core
	dcrWallet, tDcrWallet := newTWallet(tDCR.ID)
	tCore.wallets[tDCR.ID] = dcrWallet
	dcrWallet.address = "DsVmA7aqqWeKWy461hXjytbZbgCqbB8g2dq"
	dcrWallet.Unlock(wPW, time.Hour)

	btcWallet, tBtcWallet := newTWallet(tBTC.ID)
	tCore.wallets[tBTC.ID] = btcWallet
	btcWallet.address = "12DXGkvxFjuq5btXYkwWfBZaz1rVwFgini"
	btcWallet.Unlock(wPW, time.Hour)

	matchSize := 4 * tDCR.LotSize
	qty := 3 * matchSize
	rate := tBTC.RateStep * 10
	lo, dbOrder, preImgL, addr := makeLimitOrder(dc, true, qty, tBTC.RateStep)
	loid := lo.ID()
	mid := ordertest.RandomMatchID()
	walletSet, err := tCore.walletSet(dc, tDCR.ID, tBTC.ID, true)
	if err != nil {
		t.Fatalf("walletSet error: %v", err)
	}
	mkt := dc.market(tDcrBtcMktName)
	tracker := newTrackedTrade(dbOrder, preImgL, dc, mkt.EpochLen, rig.core.lockTimeTaker, rig.core.lockTimeMaker,
		rig.db, rig.queue, walletSet, nil, rig.core.notify)
	rig.dc.trades[tracker.ID()] = tracker

	// MAKER REFUND, INVALID TAKER COUNTERSWAP
	//

	matchTime := time.Now()
	msgMatch := &msgjson.Match{
		OrderID:    loid[:],
		MatchID:    mid[:],
		Quantity:   matchSize,
		Rate:       rate,
		Address:    "counterparty-address",
		Side:       uint8(order.Maker),
		ServerTime: encode.UnixMilliU(matchTime),
	}
	swapID := encode.RandomBytes(36)
	contract := encode.RandomBytes(36)
	tDcrWallet.swapReceipts = []asset.Receipt{&tReceipt{coin: &tCoin{id: swapID, script: contract}}}
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
	checkStatus("maker swapped", match, order.MakerSwapCast)
	proof := &match.MetaData.Proof
	if !bytes.Equal(proof.Script, contract) {
		t.Fatalf("invalid contract recorded for Maker swap")
	}

	// Send the counter-party's init info.
	auditQty := calc.BaseToQuote(rate, matchSize)
	audit, auditInfo := tMsgAudit(loid, mid, addr, auditQty, proof.SecretHash)
	tBtcWallet.auditInfo = auditInfo
	auditInfo.expiration = encode.DropMilliseconds(matchTime.Add(tracker.lockTimeMaker))
	msg, _ = msgjson.NewRequest(1, msgjson.AuditRoute, audit)

	// Check audit errors.
	tBtcWallet.auditErr = tErr
	err = handleAuditRoute(tCore, rig.dc, msg)
	if err == nil {
		t.Fatalf("no maker error for AuditContract error")
	}

	// Attempt refund.
	tDcrWallet.refundCoin = encode.RandomBytes(36)
	tDcrWallet.refundErr = nil
	tBtcWallet.refundCoin = nil
	tBtcWallet.refundErr = fmt.Errorf("unexpected call to btcWallet.Refund")
	checkRefund(tracker, match, matchSize)

	// TAKER REFUND, NO MAKER REDEEM
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
	checkStatus("taker matched", match, order.NewlyMatched)
	// Send through the audit request for the maker's init.
	audit, auditInfo = tMsgAudit(loid, mid, addr, matchSize, nil)
	tBtcWallet.auditInfo = auditInfo
	auditInfo.expiration = encode.DropMilliseconds(matchTime.Add(tracker.lockTimeMaker))
	tBtcWallet.auditErr = nil
	msg, _ = msgjson.NewRequest(1, msgjson.AuditRoute, audit)
	err = handleAuditRoute(tCore, rig.dc, msg)
	if err != nil {
		t.Fatalf("taker's match message error: %v", err)
	}
	checkStatus("taker counter-party swapped", match, order.MakerSwapCast)
	auditInfo.coin.confs = tBTC.SwapConf
	counterSwapID := encode.RandomBytes(36)
	counterScript := encode.RandomBytes(36)
	tDcrWallet.swapReceipts = []asset.Receipt{&tReceipt{coin: &tCoin{id: counterSwapID, script: counterScript}}}
	rig.ws.queueResponse(msgjson.InitRoute, initAcker)
	dc.tickAsset(tBTC.ID)

	checkStatus("taker swapped", match, order.TakerSwapCast)
	if !bytes.Equal(match.MetaData.Proof.Script, counterScript) {
		t.Fatalf("invalid contract recorded for Taker swap")
	}

	// Attempt refund.
	checkRefund(tracker, match, matchSize)
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

func TestResolveActiveTrades(t *testing.T) {
	rig := newTestRig()
	tCore := rig.core

	btcWallet, tBtcWallet := newTWallet(tBTC.ID)
	tCore.wallets[tBTC.ID] = btcWallet

	dcrWallet, tDcrWallet := newTWallet(tDCR.ID)
	tCore.wallets[tDCR.ID] = dcrWallet

	rig.acct.auth() // Short path through initializeDEXConnections

	// Create an order
	qty := tDCR.LotSize * 5
	rate := tDCR.RateStep * 5
	lo := &order.LimitOrder{
		P: order.Prefix{
			OrderType:  order.LimitOrderType,
			BaseAsset:  tDCR.ID,
			QuoteAsset: tBTC.ID,
			ClientTime: time.Now(),
			ServerTime: time.Now(),
			Commit:     ordertest.RandomCommitment(),
		},
		T: order.Trade{
			Quantity: qty,
			Sell:     true,
		},
		Rate: rate,
	}

	oid := lo.ID()
	changeCoinID := encode.RandomBytes(32)
	changeCoin := &tCoin{id: changeCoinID}

	// Need to return an order from db.ActiveDEXOrders
	rig.db.activeDEXOrders = []*db.MetaOrder{
		{
			MetaData: &db.OrderMetaData{
				Status:     order.OrderStatusBooked,
				Host:       tDexHost,
				Proof:      db.OrderProof{},
				ChangeCoin: changeCoinID,
			},
			Order: lo,
		},
	}

	mid := ordertest.RandomMatchID()
	addr := ordertest.RandomAddress()
	match := &db.MetaMatch{
		MetaData: &db.MatchMetaData{
			Status: order.MakerSwapCast,
			Proof: db.MatchProof{
				CounterScript: encode.RandomBytes(50),
				SecretHash:    encode.RandomBytes(32),
				MakerSwap:     encode.RandomBytes(32),
				Auth: db.MatchAuth{
					MatchSig: encode.RandomBytes(32),
				},
			},
			DEX:   tDexHost,
			Base:  tDCR.ID,
			Quote: tBTC.ID,
		},
		Match: &order.UserMatch{
			OrderID:  oid,
			MatchID:  mid,
			Quantity: qty,
			Rate:     rate,
			Address:  addr,
			Status:   order.MakerSwapCast,
			Side:     order.Taker,
		},
	}
	rig.db.activeMatchesForDEX = []*db.MetaMatch{match}
	tDcrWallet.fundingCoins = asset.Coins{changeCoin}
	_, auditInfo := tMsgAudit(oid, mid, addr, qty, nil)
	tBtcWallet.auditInfo = auditInfo

	// reset
	reset := func() {
		rig.acct.lock()
		dcrWallet.Lock()
		btcWallet.Lock()
		rig.dc.trades = make(map[order.OrderID]*trackedTrade)
	}

	// Ensure the order is good, and reset the state.
	ensureGood := func(tag string) {
		_, err := tCore.Login(tPW)
		if err != nil {
			t.Fatalf("%s: login error: %v", tag, err)
		}

		if !btcWallet.unlocked() {
			t.Fatalf("%s: btc wallet not unlocked", tag)
		}

		if !dcrWallet.unlocked() {
			t.Fatalf("%s: dcr wallet not unlocked", tag)
		}

		trade, found := rig.dc.trades[oid]
		if !found {
			t.Fatalf("%s: trade with expected order id not found. len(trades) = %d", tag, len(rig.dc.trades))
		}

		_, found = trade.matches[mid]
		if !found {
			t.Fatalf("%s: trade with expected order id not found. len(matches) = %d", tag, len(trade.matches))
		}
		reset()
	}

	ensureGood("initial")

	// Ensure a failuare AND reset. err != nil just helps to make sure that we're
	// hitting errors in resolveActiveTrades, which sends errors as notifications,
	// vs somewhere else.
	ensureFail := func(tag string) {
		_, err := tCore.Login(tPW)
		if err != nil || len(rig.dc.trades) != 0 {
			t.Fatalf("%s: no error. err = %v, len(trades) = %d", tag, err, len(rig.dc.trades))
		}
		reset()
	}

	// No base wallet
	delete(tCore.wallets, tDCR.ID)
	ensureFail("missing base")
	tCore.wallets[tDCR.ID] = dcrWallet

	// Base wallet unlock errors
	tDcrWallet.unlockErr = tErr
	ensureFail("base unlock")
	tDcrWallet.unlockErr = nil

	// No quote wallet
	delete(tCore.wallets, tBTC.ID)
	ensureFail("missing quote")
	tCore.wallets[tBTC.ID] = btcWallet

	// Quote wallet unlock errors
	tBtcWallet.unlockErr = tErr
	ensureFail("quote unlock")
	tBtcWallet.unlockErr = nil

	// Funding coin error.
	tDcrWallet.fundingCoinErr = tErr
	ensureFail("funding coin")
	tDcrWallet.fundingCoinErr = nil

	// No matches
	rig.db.activeMatchesForDEXErr = tErr
	ensureFail("matches error")
	rig.db.activeMatchesForDEXErr = nil

	// Success again
	ensureGood("final")
}

func TestReadConnectMatches(t *testing.T) {
	rig := newTestRig()
	preImg := newPreimage()
	dc := rig.dc

	notes := make(map[string][]Notification)
	notify := func(note Notification) {
		notes[note.Type()] = append(notes[note.Type()], note)
	}

	lo := &order.LimitOrder{
		P: order.Prefix{
			// 	OrderType:  order.LimitOrderType,
			// 	BaseAsset:  tDCR.ID,
			// 	QuoteAsset: tBTC.ID,
			// 	ClientTime: time.Now(),
			ServerTime: time.Now(),
			// 	Commit:     preImg.Commit(),
		},
	}
	oid := lo.ID()
	dbOrder := &db.MetaOrder{
		MetaData: &db.OrderMetaData{},
		Order:    lo,
	}
	mkt := dc.market(tDcrBtcMktName)
	tracker := newTrackedTrade(dbOrder, preImg, dc, mkt.EpochLen, rig.core.lockTimeTaker, rig.core.lockTimeMaker,
		rig.db, rig.queue, nil, nil, notify)
	metaMatch := db.MetaMatch{
		MetaData: &db.MatchMetaData{},
		Match:    &order.UserMatch{},
	}

	// Store a match
	knownID := ordertest.RandomMatchID()
	knownMatch := &matchTracker{
		id:        knownID,
		MetaMatch: metaMatch,
	}
	tracker.matches[knownID] = knownMatch
	knownMsgMatch := &msgjson.Match{OrderID: oid[:], MatchID: knownID[:]}

	missingID := ordertest.RandomMatchID()
	missingMatch := &matchTracker{
		id:        missingID,
		MetaMatch: metaMatch,
	}
	tracker.matches[missingID] = missingMatch

	extraID := ordertest.RandomMatchID()
	extraMsgMatch := &msgjson.Match{OrderID: oid[:], MatchID: extraID[:]}

	matches := []*msgjson.Match{knownMsgMatch, extraMsgMatch}
	tracker.readConnectMatches(matches)

	if knownMatch.failErr != nil {
		t.Fatalf("error set for known and reported match")
	}

	if missingMatch.failErr == nil {
		t.Fatalf("error not set for missing match")
	}

	if len(notes["order"]) != 2 {
		t.Fatalf("expected 2 core 'order'-type notifications, got %d", len(notes["order"]))
	}

	if notes["order"][0].Subject() != "Missing matches" {
		t.Fatalf("no core notification sent for missing matches")
	}

	if notes["order"][1].Subject() != "Match resolution error" {
		t.Fatalf("no core notification sent for unknown matches")
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
	payload := &msgjson.EpochOrderNote{
		BookOrderNote: msgjson.BookOrderNote{
			OrderNote: msgjson.OrderNote{
				MarketID: tDcrBtcMktName,
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

	rig.dc.books[tDcrBtcMktName] = newBookie(func() {})

	err = handleEpochOrderMsg(rig.core, rig.dc, req)
	if err != nil {
		t.Fatalf("[handleEpochOrderMsg] unexpected error: %v", err)
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
	pimg := newPreimage()
	cmt := pimg.Commit()

	seed, csum, err := makeMatchProof([]order.Preimage{pimg}, []order.Commitment{cmt})
	if err != nil {
		t.Fatalf("[makeMatchProof] unexpected error: %v", err)
	}

	payload := &msgjson.MatchProofNote{
		MarketID:  tDcrBtcMktName,
		Epoch:     1,
		Preimages: []dex.Bytes{pimg[:]},
		CSum:      csum[:],
		Seed:      seed[:],
	}

	eo := &msgjson.EpochOrderNote{
		BookOrderNote: msgjson.BookOrderNote{
			OrderNote: msgjson.OrderNote{
				MarketID: tDcrBtcMktName,
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

	rig.dc.books[tDcrBtcMktName] = newBookie(func() {})

	err = rig.dc.books[tDcrBtcMktName].Enqueue(eo)
	if err != nil {
		t.Fatalf("[Enqueue] unexpected error: %v", err)
	}

	err = handleMatchProofMsg(rig.core, rig.dc, req)
	if err != nil {
		t.Fatalf("[handleMatchProofMsg] unexpected error: %v", err)
	}
}

func TestLogout(t *testing.T) {
	rig := newTestRig()
	tCore := rig.core

	dcrWallet, tDcrWallet := newTWallet(tDCR.ID)
	tCore.wallets[tDCR.ID] = dcrWallet

	btcWallet, _ := newTWallet(tBTC.ID)
	tCore.wallets[tBTC.ID] = btcWallet

	ord := &order.LimitOrder{P: order.Prefix{ServerTime: time.Now()}}
	tracker := &trackedTrade{
		Order:  ord,
		preImg: newPreimage(),
		dc:     rig.dc,
		metaData: &db.OrderMetaData{
			Status: order.OrderStatusBooked,
		},
		matches: make(map[order.MatchID]*matchTracker),
	}
	rig.dc.trades[ord.ID()] = tracker

	dc, _, _ := testDexConnection()
	initUserAssets := func() {
		tCore.user = new(User)
		tCore.user.Assets = make(map[uint32]*SupportedAsset, len(dc.assets))
		for assetsID := range dc.assets {
			tCore.user.Assets[assetsID] = &SupportedAsset{
				ID: assetsID,
			}
		}
	}

	ensureErr := func(tag string) {
		initUserAssets()
		err := tCore.Logout()
		if err == nil {
			t.Fatalf("%s: no error", tag)
		}
	}

	// Active orders error.
	ensureErr("active orders")

	tracker.metaData = &db.OrderMetaData{
		Status: order.OrderStatusExecuted,
	}
	mid := ordertest.RandomMatchID()
	tracker.matches[mid] = &matchTracker{
		MetaMatch: db.MetaMatch{
			MetaData: &db.MatchMetaData{
				Status: order.NewlyMatched,
			},
		},
	}
	// Active orders with matches error.
	ensureErr("active orders matches")
	rig.dc.trades = nil

	// Lock wallet error.
	tDcrWallet.lockErr = tErr
	ensureErr("lock wallet")
}

func TestSetEpoch(t *testing.T) {
	rig := newTestRig()
	dc := rig.dc
	rig.dc.books[tDcrBtcMktName] = newBookie(func() {})
	lo, dbOrder, preImg, _ := makeLimitOrder(dc, true, 0, 0)
	mkt := dc.market(tDcrBtcMktName)
	tracker := newTrackedTrade(dbOrder, preImg, dc, mkt.EpochLen, rig.core.lockTimeTaker, rig.core.lockTimeMaker,
		rig.db, rig.queue, nil, nil, rig.core.notify)
	dc.trades[lo.ID()] = tracker
	metaData := tracker.metaData

	payload := &msgjson.MatchProofNote{
		MarketID: tDcrBtcMktName,
		Epoch:    uint64(tracker.Time()) / tracker.epochLen,
	}
	nextReq := func() *msgjson.Message {
		payload.Epoch++
		req, _ := msgjson.NewRequest(rig.dc.NextID(), msgjson.MatchProofRoute, payload)
		return req
	}

	ensureStatus := func(tag string, status order.OrderStatus) {
		err := handleMatchProofMsg(rig.core, rig.dc, nextReq())
		if err != nil {
			t.Fatalf("error setting epoch for %s: %v", tag, err)
		}
		if metaData.Status != status {
			t.Fatalf("wrong status for %s. expected %s, got %s", tag, status, metaData.Status)
		}
		corder, _ := tracker.coreOrderInternal()
		if corder.Status != status {
			t.Fatalf("wrong core order status for %s. expected %s, got %s", tag, status, corder.Status)
		}
	}

	// Immediate limit order
	ensureStatus("immediate limit order", order.OrderStatusExecuted)

	// Standing limit order
	metaData.Status = order.OrderStatusEpoch
	lo.Force = order.StandingTiF
	ensureStatus("standing limit order", order.OrderStatusBooked)

	// Market order
	mo := &order.MarketOrder{
		P: lo.P,
		T: *lo.T.Copy(),
	}
	mo.P.OrderType = order.LimitOrderType
	dbOrder = &db.MetaOrder{
		MetaData: &db.OrderMetaData{
			Status: order.OrderStatusEpoch,
		},
		Order: mo,
	}
	tracker = newTrackedTrade(dbOrder, preImg, dc, mkt.EpochLen, rig.core.lockTimeTaker, rig.core.lockTimeMaker,
		rig.db, rig.queue, nil, nil, rig.core.notify)
	metaData = tracker.metaData
	dc.trades[mo.ID()] = tracker
	ensureStatus("market order", order.OrderStatusExecuted)

	// Same epoch shouldn't result in change.
	payload.Epoch--
	metaData.Status = order.OrderStatusEpoch
	ensureStatus("market order unchanged", order.OrderStatusEpoch)

}

func makeLimitOrder(dc *dexConnection, sell bool, qty, rate uint64) (*order.LimitOrder, *db.MetaOrder, order.Preimage, string) {
	preImg := newPreimage()
	addr := ordertest.RandomAddress()
	lo := &order.LimitOrder{
		P: order.Prefix{
			AccountID:  dc.acct.ID(),
			BaseAsset:  tDCR.ID,
			QuoteAsset: tBTC.ID,
			OrderType:  order.LimitOrderType,
			ClientTime: time.Now(),
			ServerTime: time.Now().Add(time.Millisecond),
			Commit:     preImg.Commit(),
		},
		T: order.Trade{
			Sell:     true,
			Quantity: qty,
			Address:  addr,
		},
		Rate:  tBTC.RateStep,
		Force: order.ImmediateTiF,
	}
	dbOrder := &db.MetaOrder{
		MetaData: &db.OrderMetaData{
			Status: order.OrderStatusEpoch,
			Host:   dc.acct.host,
			Proof: db.OrderProof{
				Preimage: preImg[:],
			},
		},
		Order: lo,
	}
	return lo, dbOrder, preImg, addr
}

func TestAddrHost(t *testing.T) {
	tests := []struct {
		name, addr, want string
	}{{
		name: "scheme, host, and port",
		addr: "https://localhost:5758",
		want: "localhost:5758",
	}, {
		name: "host and port",
		addr: "localhost:5758",
		want: "localhost:5758",
	}, {
		name: "just port",
		addr: ":5758",
		want: "localhost:5758",
	}, {
		name: "ip host and port",
		addr: "127.0.0.1:5758",
		want: "127.0.0.1:5758",
	}, {
		name: "just host",
		addr: "thatonedex.com",
		want: "thatonedex.com",
	}, {
		name: "shceme and host",
		addr: "https://thatonedex.com",
		want: "thatonedex.com",
	}, {
		name: "scheme, host, and path",
		addr: "https://thatonedex.com/any/path",
		want: "thatonedex.com",
	}, {
		name: "ipv6 host",
		addr: "[1:2::]",
		want: "[1:2::]",
	}, {
		name: "ipv6 host and port",
		addr: "[1:2::]:5758",
		want: "[1:2::]:5758",
	}, {
		name: "empty address",
		want: "localhost",
	}, {
		name: "invalid host",
		addr: "https://\n:1234",
		want: "https://\n:1234",
	}, {
		name: "invalid port",
		addr: ":asdf",
		want: ":asdf",
	}}
	for _, test := range tests {
		res := addrHost(test.addr)
		if res != test.want {
			t.Fatalf("wanted %s but got %s for test '%s'", test.want, res, test.name)
		}
		// Parsing results a second time should produce the same results.
		res = addrHost(res)
		if res != test.want {
			t.Fatalf("wanted %s but got %s for test '%s'", test.want, res, test.name)
		}
	}
}

func TestAssetBalance(t *testing.T) {
	rig := newTestRig()
	tCore := rig.core

	wallet, tWallet := newTWallet(tDCR.ID)
	tCore.wallets[tDCR.ID] = wallet
	bal := &asset.Balance{
		Available: 4e7,
		Immature:  6e7,
		Locked:    2e8,
	}
	tWallet.bal = bal
	balances, err := tCore.AssetBalance(tDCR.ID)
	if err != nil {
		t.Fatalf("error retreiving asset balance: %v", err)
	}
	dbtest.MustCompareAssetBalances(t, "zero-conf", bal, &balances.Balance)
}

func TestAssetCounter(t *testing.T) {
	counts := make(assetCounter)
	counts.add(1, 1)
	if len(counts) != 1 {
		t.Fatalf("count not added")
	}
	counts.add(1, 3)
	if counts[1] != 4 {
		t.Fatalf("counts not incremented properly")
	}
	newCounts := assetCounter{
		1: 100,
		2: 2,
	}
	counts.absorb(newCounts)
	if len(counts) != 2 {
		t.Fatalf("counts not absorbed properly")
	}
	if counts[2] != 2 {
		t.Fatalf("absorbed counts not set correctly")
	}
	if counts[1] != 104 {
		t.Fatalf("absorbed counts not combined correctly")
	}
}

func TestHandleTradeSuspensionMsg(t *testing.T) {
	rig := newTestRig()

	tCore := rig.core
	dcrWallet, _ := newTWallet(tDCR.ID)
	tCore.wallets[tDCR.ID] = dcrWallet
	dcrWallet.Unlock(wPW, time.Hour)

	btcWallet, _ := newTWallet(tBTC.ID)
	tCore.wallets[tBTC.ID] = btcWallet
	btcWallet.Unlock(wPW, time.Hour)

	// Ensure a non-existent market cannot be suspended.
	payload := &msgjson.TradeSuspension{
		MarketID: "dcr_dcr",
	}

	req, _ := msgjson.NewRequest(rig.dc.NextID(), msgjson.SuspensionRoute, payload)
	err := handleTradeSuspensionMsg(rig.core, rig.dc, req)
	if err == nil {
		t.Fatal("[handleTradeSuspensionMsg] expected a market " +
			"ID not found error: %v")
	}

	mkt := tDcrBtcMktName

	// Ensure a suspended market cannot be resuspended.
	err = rig.dc.suspend(mkt)
	if err != nil {
		t.Fatalf("[handleTradeSuspensionMsg] unexpected error: %v", err)
	}

	payload = &msgjson.TradeSuspension{
		MarketID:    mkt,
		FinalEpoch:  100,
		SuspendTime: encode.UnixMilliU(time.Now().Add(time.Millisecond * 20)),
		Persist:     true,
	}

	req, _ = msgjson.NewRequest(rig.dc.NextID(), msgjson.SuspensionRoute, payload)
	err = handleTradeSuspensionMsg(rig.core, rig.dc, req)
	if err == nil {
		t.Fatal("[handleTradeSuspensionMsg] expected a market " +
			"suspended error: %v")
	}

	// Suspend a market.
	err = rig.dc.resume(mkt)
	if err != nil {
		t.Fatalf("[handleTradeSuspensionMsg] unexpected error: %v", err)
	}

	req, _ = msgjson.NewRequest(rig.dc.NextID(), msgjson.SuspensionRoute, payload)
	err = handleTradeSuspensionMsg(rig.core, rig.dc, req)
	if err != nil {
		t.Fatalf("[handleTradeSuspensionMsg] unexpected error: %v", err)
	}

	// Wait for the suspend to execute.
	time.Sleep(time.Millisecond * 40)

	// Ensure trades for a suspended market generate an error.
	form := &TradeForm{
		Host:    tDexHost,
		IsLimit: true,
		Sell:    true,
		Base:    tDCR.ID,
		Quote:   tBTC.ID,
		Qty:     tDCR.LotSize * 10,
		Rate:    tBTC.RateStep * 1000,
		TifNow:  false,
	}

	_, err = rig.core.Trade(tPW, form)
	if err == nil {
		t.Fatalf("expected a suspension market error")
	}
}
