package core

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"testing"
	"time"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/client/db"
	"decred.org/dcrdex/client/order"
	"decred.org/dcrdex/dex"
	dexbtc "decred.org/dcrdex/dex/btc"
	dexdcr "decred.org/dcrdex/dex/dcr"
	"decred.org/dcrdex/dex/encode"
	"decred.org/dcrdex/dex/encrypt"
	"decred.org/dcrdex/dex/msgjson"
	dexorder "decred.org/dcrdex/dex/order"
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
	tDexPriv *secp256k1.PrivateKey
	tDexKey  *secp256k1.PublicKey
	tPW      = []byte("dexpw")
	tDexUrl  = "somedex.tld"
	tErr     = fmt.Errorf("test error")
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

type TWebsocket struct {
	id       uint64
	sendErr  error
	reqErr   error
	msgs     <-chan *msgjson.Message
	handlers map[string][]func(*msgjson.Message, msgFunc) error
}

func newTWebsocket() *TWebsocket {
	return &TWebsocket{
		msgs:     make(<-chan *msgjson.Message),
		handlers: make(map[string][]func(*msgjson.Message, msgFunc) error),
	}
}

func tNewAccount() *dexAccount {
	privKey, _ := secp256k1.GeneratePrivateKey()
	secretKey, _ := encrypt.NewSecretKey(&tPW)
	encPW, _ := secretKey.Encrypt(privKey.Serialize())
	return &dexAccount{
		url:       tDexUrl,
		encKey:    encPW,
		privKey:   privKey,
		dexPubKey: tDexKey,
		feeCoin:   []byte("somecoin"),
	}
}

func testDexConnection() (*dexConnection, *TWebsocket, *dexAccount) {
	conn := newTWebsocket()
	acct := tNewAccount()
	return &dexConnection{
		websocket: conn,
		acct:      acct,
		assets: map[uint32]*dex.Asset{
			tDCR.ID: tDCR,
			tBTC.ID: tBTC,
		},
		cfg: &msgjson.ConfigResult{
			CancelMax:        0.8,
			BroadcastTimeout: 5 * 60000,
			Assets: []msgjson.Asset{
				*uncovertAssetInfo(tDCR),
				*uncovertAssetInfo(tBTC),
			},
			Markets: []msgjson.Market{
				{
					Name:            "dcr_btc",
					Base:            tDCR.ID,
					Quote:           tBTC.ID,
					EpochLen:        60000,
					MarketBuyBuffer: 1.1,
				},
			},
		},
	}, conn, acct
}

func (conn *TWebsocket) queueResponse(route string, handler func(*msgjson.Message, msgFunc) error) {
	handlers := conn.handlers[route]
	if handlers == nil {
		handlers = make([]func(*msgjson.Message, msgFunc) error, 0, 1)
	}
	conn.handlers[route] = append(handlers, handler)
}

func (conn *TWebsocket) NextID() uint64 {
	conn.id++
	return conn.id
}

func (conn *TWebsocket) WaitForShutdown()                {}
func (conn *TWebsocket) Send(msg *msgjson.Message) error { return conn.sendErr }
func (conn *TWebsocket) Request(msg *msgjson.Message, f msgFunc) error {
	handlers := conn.handlers[msg.Route]
	if len(handlers) > 0 {
		handler := handlers[0]
		conn.handlers[msg.Route] = handlers[1:]
		return handler(msg, f)
	}
	return conn.reqErr
}
func (conn *TWebsocket) MessageSource() <-chan *msgjson.Message { return conn.msgs }

type TDB struct {
	updateWalletErr error
	acct            *db.AccountInfo
	acctErr         error
}

func (db *TDB) Run(context.Context) {}

func (db *TDB) ListAccounts() ([]string, error) {
	return nil, nil
}

func (db *TDB) Accounts() ([]*db.AccountInfo, error) {
	return nil, nil
}

func (db *TDB) Account(url string) (*db.AccountInfo, error) {
	return db.acct, db.acctErr
}

func (db *TDB) CreateAccount(ai *db.AccountInfo) error {
	return nil
}

func (db *TDB) UpdateOrder(m *db.MetaOrder) error {
	return nil
}

func (db *TDB) ActiveOrders() ([]*db.MetaOrder, error) {
	return nil, nil
}

func (db *TDB) AccountOrders(dex string, n int, since uint64) ([]*db.MetaOrder, error) {
	return nil, nil
}

func (db *TDB) Order(dexorder.OrderID) (*db.MetaOrder, error) {
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

type tCoin struct {
	id       []byte
	confs    uint32
	confsErr error
}

func (c *tCoin) ID() dex.Bytes {
	return c.id
}

func (c *tCoin) Value() uint64 {
	return 0
}

func (c *tCoin) Confirmations() (uint32, error) {
	return c.confs, c.confsErr
}

func (c *tCoin) Redeem() dex.Bytes {
	return nil
}

type TXCWallet struct {
	payFeeCoin *tCoin
	payFeeErr  error
}

func newTWallet(assetID uint32) (*xcWallet, *TXCWallet) {
	w := new(TXCWallet)
	return &xcWallet{
		Wallet:  w,
		waiter:  dex.NewStartStopWaiter(w),
		AssetID: assetID,
	}, w
}

func (w *TXCWallet) Connect() error { return nil }

func (w *TXCWallet) Run(context.Context) {}

func (w *TXCWallet) Balance(*dex.Asset) (available, locked uint64, err error) {
	return 0, 0, nil
}

func (w *TXCWallet) Fund(uint64, *dex.Asset) (asset.Coins, error) {
	return nil, nil
}

func (w *TXCWallet) ReturnCoins(asset.Coins) error {
	return nil
}

func (w *TXCWallet) Swap([]*asset.Swap, *dex.Asset) ([]asset.Receipt, error) {
	return nil, nil
}

func (w *TXCWallet) Redeem([]*asset.Redemption, *dex.Asset) error {
	return nil
}

func (w *TXCWallet) SignMessage(asset.Coin, dex.Bytes) (pubkeys, sigs []dex.Bytes, err error) {
	return nil, nil, nil
}

func (w *TXCWallet) AuditContract(coinID, contract dex.Bytes) (asset.AuditInfo, error) {
	return nil, nil
}

func (w *TXCWallet) FindRedemption(ctx context.Context, coinID dex.Bytes) (dex.Bytes, error) {
	return nil, nil
}

func (w *TXCWallet) Refund(asset.Receipt, *dex.Asset) error {
	return nil
}

func (w *TXCWallet) Address() (string, error) {
	return "", nil
}

func (w *TXCWallet) Unlock(pw string, dur time.Duration) error {
	return nil
}

func (w *TXCWallet) Lock() error {
	return nil
}

func (w *TXCWallet) PayFee(fee uint64, address string, _ *dex.Asset) (asset.Coin, error) {
	return w.payFeeCoin, w.payFeeErr
}

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
	ws      *TWebsocket
	dexConn *dexConnection
	acct    *dexAccount
}

func newTestRig() *testRig {
	db := new(TDB)
	dc, conn, acct := testDexConnection()

	return &testRig{
		core: &Core{
			ctx: tCtx,
			db:  db,
			conns: map[string]*dexConnection{
				tDexUrl: dc,
			},
			loggerMaker: &dex.LoggerMaker{
				Backend:      slog.NewBackend(os.Stdout),
				DefaultLevel: slog.LevelTrace,
			},
			wallets: make(map[uint32]*xcWallet),
			waiters: make(map[string]coinWaiter),
		},
		db:      db,
		ws:      conn,
		dexConn: dc,
		acct:    acct,
	}
}

func tMarketID(bq [2]uint32) string {
	return strconv.Itoa(int(bq[0])) + "-" + strconv.Itoa(int(bq[1]))
}

func TestMain(m *testing.M) {
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

func TestListMarkets(t *testing.T) {
	rig := newTestRig()
	tCore := rig.core
	// Simulate 10 markets.
	marketIDs := make(map[string]struct{})
	rig.dexConn.cfg.Markets = nil
	for i := 0; i < 10; i++ {
		base, quote := randomMsgMarket()
		mkt := [2]uint32{base.ID, quote.ID}
		marketIDs[tMarketID(mkt)] = struct{}{}
		rig.dexConn.cfg.Markets = append(rig.dexConn.cfg.Markets, msgjson.Market{
			Name:            base.Symbol + "_" + quote.Symbol,
			Base:            base.ID,
			Quote:           quote.ID,
			EpochLen:        5000,
			StartEpoch:      1234,
			MarketBuyBuffer: 1.4,
		})
		rig.dexConn.assets[base.ID] = convertAssetInfo(base)
		rig.dexConn.assets[quote.ID] = convertAssetInfo(quote)
	}

	// Just check that the information is coming through correctly.
	dexes := tCore.ListMarkets()
	if len(dexes) != 1 {
		t.Fatalf("expected 1 MarketInfo, got %d", len(dexes))
	}
	mktCfg := dexes[0]
	if len(mktCfg.Markets) != 10 {
		t.Fatalf("expected 10 Markets, got %d", len(mktCfg.Markets))
	}
	assets := rig.dexConn.assets
	for _, market := range mktCfg.Markets {
		mkt := tMarketID([2]uint32{market.BaseID, market.QuoteID})
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

func TestDexConnectionOrderBook(t *testing.T) {
	tCore := newTestRig().core
	mid := "ob"
	dc := &dexConnection{
		books: make(map[string]*order.OrderBook),
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

	err = tCore.handleOrderBookMsg(dc, msg)
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

	err = tCore.handleBookOrderMsg(dc, msg)
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

	err = tCore.handleUnbookOrderMsg(dc, msg)
	if err != nil {
		t.Fatalf("[handleUnbookOrderMsg]: unexpected err: %v", err)
	}
	_, ok = dc.books[mid]
	if !ok {
		t.Fatalf("expected order book with market id %s", mid)
	}
}

type tDriver struct {
	f func(*asset.WalletConfig, dex.Logger, dex.Network) (asset.Wallet, error)
}

func (drv *tDriver) Setup(cfg *asset.WalletConfig, logger dex.Logger, net dex.Network) (asset.Wallet, error) {
	return drv.f(cfg, logger, net)
}

func TestCreateWallet(t *testing.T) {
	rig := newTestRig()
	tCore := rig.core

	// Create a new asset.
	a := *tDCR
	tILT := &a
	tILT.Symbol = "ilt"
	tILT.ID, _ = dex.BipSymbolID(tILT.Symbol)

	// // Get a wallet ID.
	// dbWallet := db.Wallet{
	// 	AssetID: tILT.ID,
	// 	Account: "default",
	// }
	// wid := dbWallet.SID()

	// Create registration form.
	form := &WalletForm{
		AssetID: tILT.ID,
		Account: "default",
	}

	// Try to add an existing wallet.
	wallet, _ := newTWallet(tILT.ID)
	tCore.wallets[tILT.ID] = wallet
	err := tCore.CreateWallet(form)
	if err == nil {
		t.Fatalf("no error for existing wallet")
	}
	delete(tCore.wallets, tILT.ID)

	// Try an unkown wallet (not yet asset.Register'ed).
	err = tCore.CreateWallet(form)
	if err == nil {
		t.Fatalf("no error for unknown asset")
	}

	// Register the asset.
	asset.Register(tILT.ID, &tDriver{f: func(wCfg *asset.WalletConfig, logger dex.Logger, net dex.Network) (asset.Wallet, error) {
		w, _ := newTWallet(tILT.ID)
		return w.Wallet, nil
	}})

	// Database error.
	rig.db.updateWalletErr = tErr
	err = tCore.CreateWallet(form)
	if err == nil {
		t.Fatalf("no error for database error")
	}
	rig.db.updateWalletErr = nil

	// Success
	err = tCore.CreateWallet(form)
	if err != nil {
		t.Fatalf("error when should be no error: %v", err)
	}
}

func TestRegister(t *testing.T) {
	rig := newTestRig()
	tCore := rig.core
	dc := rig.dexConn
	acct := dc.acct

	wallet, tWallet := newTWallet(tDCR.ID)
	tCore.wallets[tDCR.ID] = wallet

	// When registering, successfully retrieving *db.AccountInfo from the DB is
	// an error (no dupes). Initial state is to return an error.
	rig.db.acctErr = tErr

	regRes := &msgjson.RegisterResult{
		DEXPubKey: acct.dexPubKey.Serialize(),
		Address:   "someaddr",
		Fee:       1e8,
		Time:      encode.UnixMilliU(time.Now()),
	}
	sign(tDexPriv, regRes)

	var timer *time.Timer
	queueRegister := func() {
		rig.ws.queueResponse(msgjson.RegisterRoute, func(msg *msgjson.Message, f msgFunc) error {
			resp, _ := msgjson.NewResponse(msg.ID, regRes, nil)
			f(resp)
			// This is a good time to schedule a tip change, because the user's
			// password-based encryption key has been created, which is by far the
			// longest part.
			timer = time.AfterFunc(time.Millisecond*5, func() {
				tWallet.payFeeCoin.confs = tDCR.FundConf
				tCore.tipChange(tDCR.ID, nil)
			})
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
			result := &msgjson.Acknowledgement{Sig: hex.EncodeToString(sig.Serialize())}
			resp, _ := msgjson.NewResponse(msg.ID, result, nil)
			f(resp)
			return nil
		})
	}

	queueResponses := func() {
		queueRegister()
		queueNotifyFee()
	}

	form := &Registration{
		DEX:      tDexUrl,
		Password: string(tPW),
	}

	tWallet.payFeeCoin = &tCoin{id: []byte("abcdef")}

	var err error
	run := func() <-chan error {
		if timer != nil {
			timer.Stop()
		}
		tWallet.payFeeCoin.confs = tDCR.FundConf
		// No errors
		var errChan <-chan error
		err, errChan = tCore.Register(form)
		return errChan
	}

	runWithWait := func() {
		errChan := run()
		if err != nil {
			t.Fatalf("unexpected error before waiter: %v", err)
		}
		err = <-errChan
	}

	queueResponses()
	runWithWait()
	if err != nil {
		t.Fatalf("registration error: %v", err)
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
	rig.ws.queueResponse(msgjson.NotifyFeeRoute, func(msg *msgjson.Message, f msgFunc) error {
		m, _ := msgjson.NewResponse(msg.ID, nil, msgjson.NewError(1, "test error message"))
		f(m)
		return nil
	})
	runWithWait()
	if err == nil {
		t.Fatalf("no error for notifyfee response error")
	}

	// Make sure it's good again.
	queueResponses()
	runWithWait()
	if err != nil {
		t.Fatalf("error after regaining valid state: %v", err)
	}
}
