// +build !harness

package core

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"decred.org/dcrdex/client/asset"
	_ "decred.org/dcrdex/client/asset/btc" // register btc asset driver for coinIDString
	_ "decred.org/dcrdex/client/asset/dcr" // ditto dcr
	_ "decred.org/dcrdex/client/asset/ltc" // ditto ltc
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
	"decred.org/dcrdex/dex/order/test"
	ordertest "decred.org/dcrdex/dex/order/test"
	"decred.org/dcrdex/dex/wait"
	"decred.org/dcrdex/server/account"
	"github.com/decred/dcrd/crypto/blake256"
	"github.com/decred/dcrd/dcrec/secp256k1/v3"
	"github.com/decred/dcrd/dcrec/secp256k1/v3/ecdsa"
)

var (
	tCtx context.Context
	tDCR = &dex.Asset{
		ID:           42,
		Symbol:       "dcr",
		SwapSize:     dexdcr.InitTxSize,
		SwapSizeBase: dexdcr.InitTxSizeBase,
		MaxFeeRate:   10,
		LotSize:      1e7,
		RateStep:     100,
		SwapConf:     1,
	}

	tBTC = &dex.Asset{
		ID:           0,
		Symbol:       "btc",
		SwapSize:     dexbtc.InitTxSize,
		SwapSizeBase: dexbtc.InitTxSizeBase,
		MaxFeeRate:   2,
		LotSize:      1e6,
		RateStep:     10,
		SwapConf:     1,
	}
	tDexPriv            *secp256k1.PrivateKey
	tDexKey             *secp256k1.PublicKey
	tDexAccountID       account.AccountID
	tPW                        = []byte("dexpw")
	wPW                        = "walletpw"
	tDexHost                   = "somedex.tld:7232"
	tDcrBtcMktName             = "dcr_btc"
	tErr                       = fmt.Errorf("test error")
	tFee                uint64 = 1e8
	tUnparseableHost           = string([]byte{0x7f})
	tSwapFeesPaid       uint64 = 500
	tRedemptionFeesPaid uint64 = 350
	tLogger                    = dex.StdOutLogger("TCORE", dex.LevelTrace)
	tMaxFeeRate         uint64 = 10
)

type tMsg = *msgjson.Message
type msgFunc = func(*msgjson.Message)

func uncovertAssetInfo(ai *dex.Asset) *msgjson.Asset {
	return &msgjson.Asset{
		Symbol:       ai.Symbol,
		ID:           ai.ID,
		LotSize:      ai.LotSize,
		RateStep:     ai.RateStep,
		MaxFeeRate:   ai.MaxFeeRate,
		SwapSize:     ai.SwapSize,
		SwapSizeBase: ai.SwapSizeBase,
		SwapConf:     uint16(ai.SwapConf),
	}
}

func makeAcker(serializer func(msg *msgjson.Message) msgjson.Signable) func(msg *msgjson.Message, f msgFunc) error {
	return func(msg *msgjson.Message, f msgFunc) error {
		signable := serializer(msg)
		sigMsg := signable.Serialize()
		sig := ecdsa.Sign(tDexPriv, sigMsg)
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
	// handlers simulates a peer (server) response for request, and handles the
	// response with the msgFunc.
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
	return &dexAccount{
		host:      tDexHost,
		encKey:    privKey.Serialize(),
		dexPubKey: tDexKey,
		privKey:   privKey,
		id:        tDexAccountID,
		feeCoin:   []byte("somecoin"),
	}
}

func testDexConnection(ctx context.Context) (*dexConnection, *TWebsocket, *dexAccount) {
	conn := newTWebsocket()
	connMaster := dex.NewConnectionMaster(conn)
	connMaster.Connect(ctx)
	acct := tNewAccount()
	return &dexConnection{
		WsConn:     conn,
		log:        tLogger,
		connMaster: connMaster,
		acct:       acct,
		assets: map[uint32]*dex.Asset{
			tDCR.ID: tDCR,
			tBTC.ID: tBTC,
		},
		books: make(map[string]*bookie),
		cfg: &msgjson.ConfigResult{
			CancelMax:        0.8,
			BroadcastTimeout: 1000, // 1000 ms for faster expiration, but ticker fires fast
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
					MarketStatus: msgjson.MarketStatus{
						StartEpoch: 12, // since the stone age
						FinalEpoch: 0,  // no scheduled suspend
						// Persist:   nil,
					},
				},
			},
			Fee: tFee,
		},
		tickInterval: time.Millisecond * 1000 / 3,
		notify:       func(Notification) {},
		trades:       make(map[order.OrderID]*trackedTrade),
		epoch:        map[string]uint64{tDcrBtcMktName: 0},
		connected:    1,
	}, conn, acct
}

func (conn *TWebsocket) queueResponse(route string, handler func(*msgjson.Message, msgFunc) error) {
	conn.mtx.Lock()
	defer conn.mtx.Unlock()
	handlers := conn.handlers[route]
	if handlers == nil {
		handlers = make([]func(*msgjson.Message, msgFunc) error, 0, 1)
	}
	conn.handlers[route] = append(handlers, handler)
}

func (conn *TWebsocket) NextID() uint64 {
	conn.mtx.Lock()
	defer conn.mtx.Unlock()
	conn.id++
	return conn.id
}
func (conn *TWebsocket) Send(msg *msgjson.Message) error { return conn.sendErr }
func (conn *TWebsocket) Request(msg *msgjson.Message, f msgFunc) error {
	return conn.RequestWithTimeout(msg, f, 0, func() {})
}
func (conn *TWebsocket) RequestWithTimeout(msg *msgjson.Message, f func(*msgjson.Message), _ time.Duration, _ func()) error {
	if conn.reqErr != nil {
		return conn.reqErr
	}
	conn.mtx.Lock()
	defer conn.mtx.Unlock()
	handlers := conn.handlers[msg.Route]
	if len(handlers) > 0 {
		handler := handlers[0]
		conn.handlers[msg.Route] = handlers[1:]
		return handler(msg, f)
	}
	return fmt.Errorf("no handler for route %q", msg.Route)
}
func (conn *TWebsocket) MessageSource() <-chan *msgjson.Message { return conn.msgs }
func (conn *TWebsocket) IsDown() bool {
	return false
}
func (conn *TWebsocket) Connect(context.Context) (*sync.WaitGroup, error) {
	return &sync.WaitGroup{}, conn.connectErr
}

type TDB struct {
	updateWalletErr       error
	acct                  *db.AccountInfo
	acctErr               error
	getErr                error
	storeErr              error
	encKeyErr             error
	createAccountErr      error
	accountPaidErr        error
	accts                 []*db.AccountInfo
	updateOrderErr        error
	activeDEXOrders       []*db.MetaOrder
	matchesForOID         []*db.MetaMatch
	matchesForOIDErr      error
	updateMatchMtx        sync.RWMutex
	updateMatchChans      map[order.MatchID]chan order.MatchStatus
	activeMatchOIDs       []order.OrderID
	activeMatchOIDSErr    error
	lastStatusID          order.OrderID
	lastStatus            order.OrderStatus
	wallet                *db.Wallet
	walletErr             error
	setWalletPwErr        error
	orderOrders           map[order.OrderID]*db.MetaOrder
	orderErr              error
	linkedFromID          order.OrderID
	linkedToID            order.OrderID
	existValues           map[string]bool
	accountProof          *db.AccountProof
	accountProofErr       error
	verifyAccountPaid     bool
	verifyCreateAccount   bool
	accountInfoPersisted  *db.AccountInfo
	accountProofPersisted *db.AccountProof
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
	tdb.verifyCreateAccount = true
	tdb.accountInfoPersisted = ai
	return tdb.createAccountErr
}

func (tdb *TDB) DisableAccount(ai *db.AccountInfo) error {
	tdb.accts = nil
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

func (tdb *TDB) Order(oid order.OrderID) (*db.MetaOrder, error) {
	if tdb.orderErr != nil {
		return nil, tdb.orderErr
	}
	return tdb.orderOrders[oid], nil
}

func (tdb *TDB) Orders(*db.OrderFilter) ([]*db.MetaOrder, error) {
	return nil, nil
}

func (tdb *TDB) MarketOrders(dex string, base, quote uint32, n int, since uint64) ([]*db.MetaOrder, error) {
	return nil, nil
}

func (tdb *TDB) UpdateOrderMetaData(order.OrderID, *db.OrderMetaData) error {
	return nil
}

func (tdb *TDB) UpdateOrderStatus(oid order.OrderID, status order.OrderStatus) error {
	tdb.lastStatusID = oid
	tdb.lastStatus = status
	return nil
}

func (tdb *TDB) LinkOrder(oid, linkedID order.OrderID) error {
	tdb.linkedFromID = oid
	tdb.linkedToID = linkedID
	return nil
}

func (tdb *TDB) setUpdateMatchHook(mid order.MatchID, ch chan order.MatchStatus) {
	tdb.updateMatchMtx.Lock()
	defer tdb.updateMatchMtx.Unlock()
	tdb.updateMatchChans[mid] = ch
}

func (tdb *TDB) waitForMatchUpdateOrFail(mid order.MatchID, t *testing.T) order.MatchStatus {
	t.Helper()
	tdb.updateMatchMtx.RLock()
	updateCh := tdb.updateMatchChans[mid]
	tdb.updateMatchMtx.RUnlock()

	var latestStatus order.MatchStatus
	if updateCh == nil {
		t.Fatalf("tdb is not configured to track db updates for match %v", mid)
	}
	select {
	case latestStatus = <-updateCh:
	case <-time.After(1 * time.Second):
		t.Fatalf("no db update for match %v in 1 second", mid)
	}
	return latestStatus
}

func (tdb *TDB) UpdateMatch(m *db.MetaMatch) error {
	// Use RLock as another process might be waiting for this update in
	// tdb.waitForMatchUpdate, to prevent a deadlock.
	tdb.updateMatchMtx.RLock()
	updateCh := tdb.updateMatchChans[m.MatchID]
	tdb.updateMatchMtx.RUnlock()
	if updateCh != nil {
		updateCh <- m.Status
	}
	return nil
}

func (tdb *TDB) ActiveMatches() ([]*db.MetaMatch, error) {
	return nil, nil
}

func (tdb *TDB) MatchesForOrder(oid order.OrderID) ([]*db.MetaMatch, error) {
	return tdb.matchesForOID, tdb.matchesForOIDErr
}

func (tdb *TDB) DEXOrdersWithActiveMatches(dex string) ([]order.OrderID, error) {
	return tdb.activeMatchOIDs, tdb.activeMatchOIDSErr
}

func (tdb *TDB) UpdateWallet(wallet *db.Wallet) error {
	tdb.wallet = wallet
	return tdb.updateWalletErr
}

func (tdb *TDB) SetWalletPassword(wid []byte, newPW []byte) error {
	return tdb.setWalletPwErr
}

func (tdb *TDB) UpdateBalance(wid []byte, balance *db.Balance) error {
	return nil
}

func (tdb *TDB) Wallets() ([]*db.Wallet, error) {
	return nil, nil
}

func (tdb *TDB) Wallet([]byte) (*db.Wallet, error) {
	return tdb.wallet, tdb.walletErr
}

func (tdb *TDB) AccountProof(url string) (*db.AccountProof, error) {
	return tdb.accountProof, tdb.accountProofErr
}

func (tdb *TDB) AccountPaid(proof *db.AccountProof) error {
	tdb.verifyAccountPaid = true
	tdb.accountProofPersisted = proof
	return tdb.accountPaidErr
}

func (tdb *TDB) SaveNotification(*db.Notification) error        { return nil }
func (tdb *TDB) NotificationsN(int) ([]*db.Notification, error) { return nil, nil }

func (tdb *TDB) Store(k string, b []byte) error {
	return tdb.storeErr
}

func (tdb *TDB) ValueExists(k string) (bool, error) {
	return tdb.existValues[k], nil
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
	id []byte

	val uint64
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

type tReceipt struct {
	coin       *tCoin
	contract   []byte
	expiration time.Time
}

func (r *tReceipt) Coin() asset.Coin {
	return r.coin
}

func (r *tReceipt) Contract() dex.Bytes {
	return r.contract
}

func (r *tReceipt) Expiration() time.Time {
	return r.expiration
}

func (r *tReceipt) String() string {
	return r.coin.String()
}

type TXCWallet struct {
	mtx               sync.RWMutex
	payFeeCoin        *tCoin
	payFeeErr         error
	addrErr           error
	signCoinErr       error
	lastSwaps         *asset.Swaps
	swapReceipts      []asset.Receipt
	swapCounter       int
	swapErr           error
	auditInfo         *asset.AuditInfo
	auditErr          error
	auditChan         chan struct{}
	refundCoin        dex.Bytes
	refundErr         error
	redeemCoins       []dex.Bytes
	redeemCounter     int
	redeemErr         error
	redeemErrChan     chan error
	badSecret         bool
	fundedVal         uint64
	fundedSwaps       uint64
	connectErr        error
	unlockErr         error
	balErr            error
	bal               *asset.Balance
	fundingCoins      asset.Coins
	fundRedeemScripts []dex.Bytes
	returnedCoins     asset.Coins
	fundingCoinErr    error
	lockErr           error
	locked            bool
	changeCoin        *tCoin
	syncStatus        func() (bool, float32, error)
	confsMtx          sync.RWMutex
	confs             map[string]uint32
	confsErr          map[string]error
	preSwap           *asset.PreSwap
	preRedeem         *asset.PreRedeem
}

func newTWallet(assetID uint32) (*xcWallet, *TXCWallet) {
	w := &TXCWallet{
		changeCoin: &tCoin{id: encode.RandomBytes(36)},
		syncStatus: func() (synced bool, progress float32, err error) { return true, 1, nil },
		confs:      make(map[string]uint32),
		confsErr:   make(map[string]error),
	}
	return &xcWallet{
		Wallet:       w,
		connector:    dex.NewConnectionMaster(w),
		AssetID:      assetID,
		hookedUp:     true,
		dbID:         encode.Uint32Bytes(assetID),
		encPW:        []byte{0x01},
		synced:       true,
		syncProgress: 1,
		pw:           string(tPW),
	}, w
}

func (w *TXCWallet) Info() *asset.WalletInfo {
	return &asset.WalletInfo{}
}

func (w *TXCWallet) OwnsAddress(address string) (bool, error) {
	return true, nil
}

func (w *TXCWallet) Connect(ctx context.Context) (*sync.WaitGroup, error) {
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		<-ctx.Done()
		wg.Done()
	}()
	return &wg, w.connectErr
}

func (w *TXCWallet) Balance() (*asset.Balance, error) {
	if w.balErr != nil {
		return nil, w.balErr
	}
	if w.bal == nil {
		w.bal = new(asset.Balance)
	}
	return w.bal, nil
}

func (w *TXCWallet) FeeRate() (uint64, error) {
	return 24, nil
}

func (w *TXCWallet) FundOrder(ord *asset.Order) (asset.Coins, []dex.Bytes, error) {
	w.fundedVal = ord.Value
	w.fundedSwaps = ord.MaxSwapCount
	return w.fundingCoins, w.fundRedeemScripts, w.fundingCoinErr
}

func (w *TXCWallet) MaxOrder(lotSize uint64, nfo *dex.Asset) (*asset.SwapEstimate, error) {
	return nil, nil
}

func (w *TXCWallet) PreSwap(*asset.PreSwapForm) (*asset.PreSwap, error) { return w.preSwap, nil }
func (w *TXCWallet) PreRedeem(*asset.PreRedeemForm) (*asset.PreRedeem, error) {
	return w.preRedeem, nil
}
func (w *TXCWallet) RedemptionFees() (uint64, error) { return 0, nil }

func (w *TXCWallet) ReturnCoins(coins asset.Coins) error {
	w.returnedCoins = coins
	coinInSlice := func(coin asset.Coin) bool {
		for _, c := range coins {
			if bytes.Equal(c.ID(), coin.ID()) {
				return true
			}
		}
		return false
	}

	for _, c := range w.fundingCoins {
		if coinInSlice(c) {
			continue
		}
		return errors.New("not found")
	}
	return nil
}

func (w *TXCWallet) FundingCoins([]dex.Bytes) (asset.Coins, error) {
	return w.fundingCoins, w.fundingCoinErr
}

func (w *TXCWallet) Swap(swaps *asset.Swaps) ([]asset.Receipt, asset.Coin, uint64, error) {
	w.swapCounter++
	w.lastSwaps = swaps
	if w.swapErr != nil {
		return nil, nil, 0, w.swapErr
	}
	return w.swapReceipts, w.changeCoin, tSwapFeesPaid, nil
}

func (w *TXCWallet) Redeem(*asset.RedeemForm) ([]dex.Bytes, asset.Coin, uint64, error) {
	defer func() {
		if w.redeemErrChan != nil {
			w.redeemErrChan <- w.redeemErr
		}
	}()
	w.redeemCounter++
	if w.redeemErr != nil {
		return nil, nil, 0, w.redeemErr
	}
	return w.redeemCoins, &tCoin{id: []byte{0x0c, 0x0d}}, tRedemptionFeesPaid, nil
}

func (w *TXCWallet) SignMessage(asset.Coin, dex.Bytes) (pubkeys, sigs []dex.Bytes, err error) {
	return nil, nil, w.signCoinErr
}

func (w *TXCWallet) AuditContract(coinID, contract, txData dex.Bytes) (*asset.AuditInfo, error) {
	defer func() {
		if w.auditChan != nil {
			w.auditChan <- struct{}{}
		}
	}()
	return w.auditInfo, w.auditErr
}

func (w *TXCWallet) LocktimeExpired(contract dex.Bytes) (bool, time.Time, error) {
	return true, time.Now().Add(-time.Minute), nil
}

func (w *TXCWallet) FindRedemption(ctx context.Context, coinID dex.Bytes) (redemptionCoin, secret dex.Bytes, err error) {
	return nil, nil, fmt.Errorf("not mocked")
}

func (w *TXCWallet) Refund(dex.Bytes, dex.Bytes) (dex.Bytes, error) {
	return w.refundCoin, w.refundErr
}

func (w *TXCWallet) Address() (string, error) {
	return "", w.addrErr
}

func (w *TXCWallet) Unlock(pw string) error {
	return w.unlockErr
}

func (w *TXCWallet) Lock() error {
	return w.lockErr
}

func (w *TXCWallet) Locked() bool {
	return w.locked
}

func (w *TXCWallet) Send(address string, fee uint64, _ *dex.Asset) (asset.Coin, error) {
	w.mtx.RLock()
	defer w.mtx.RUnlock()
	return w.payFeeCoin, w.payFeeErr
}

func (w *TXCWallet) ConfirmTime(id dex.Bytes, nConfs uint32) (time.Time, error) {
	return time.Time{}, nil
}

func (w *TXCWallet) PayFee(address string, fee uint64) (asset.Coin, error) {
	return w.payFeeCoin, w.payFeeErr
}

func (w *TXCWallet) Withdraw(address string, value uint64) (asset.Coin, error) {
	return w.payFeeCoin, w.payFeeErr
}

func (w *TXCWallet) ValidateSecret(secret, secretHash []byte) bool {
	return !w.badSecret
}

func (w *TXCWallet) SyncStatus() (synced bool, progress float32, err error) {
	return w.syncStatus()
}

func (w *TXCWallet) setConfs(coinID dex.Bytes, confs uint32, err error) {
	id := coinID.String()
	w.confsMtx.Lock()
	w.confs[id] = confs
	w.confsErr[id] = err
	w.confsMtx.Unlock()
}

func (w *TXCWallet) Confirmations(ctx context.Context, coinID dex.Bytes) (uint32, bool, error) {
	id := coinID.String()
	w.confsMtx.RLock()
	defer w.confsMtx.RUnlock()
	return w.confs[id], false, w.confsErr[id]
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
	shutdown func()
	core     *Core
	db       *TDB
	queue    *wait.TickerQueue
	ws       *TWebsocket
	dc       *dexConnection
	acct     *dexAccount
	crypter  *tCrypter
}

func newTestRig() *testRig {
	tdb := &TDB{
		orderOrders: make(map[order.OrderID]*db.MetaOrder),
		wallet:      &db.Wallet{},
		existValues: map[string]bool{
			keyParamsKey: true,
		},
		updateMatchChans: map[order.MatchID]chan order.MatchStatus{},
	}

	ai := &db.AccountInfo{
		Host: "somedex.com",
	}
	tdb.acct = ai
	tdb.accts = append(tdb.accts, ai)

	// Set the global waiter expiration, and start the waiter.
	queue := wait.NewTickerQueue(time.Millisecond * 5)
	ctx, cancel := context.WithCancel(tCtx)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		queue.Run(ctx)
	}()

	dc, conn, acct := testDexConnection(ctx)

	shutdown := func() {
		cancel()
		wg.Wait()
		dc.connMaster.Wait()
	}

	crypter := &tCrypter{}

	rig := &testRig{
		shutdown: shutdown,
		core: &Core{
			ctx:      ctx,
			cfg:      &Config{},
			db:       tdb,
			log:      tLogger,
			latencyQ: queue,
			conns: map[string]*dexConnection{
				tDexHost: dc,
			},
			lockTimeTaker: dex.LockTimeTaker(dex.Testnet),
			lockTimeMaker: dex.LockTimeMaker(dex.Testnet),
			wallets:       make(map[uint32]*xcWallet),
			blockWaiters:  make(map[string]*blockWaiter),
			piSyncers:     make(map[order.OrderID]chan struct{}),
			tickSched:     make(map[order.OrderID]*time.Timer),
			wsConstructor: func(*comms.WsCfg) (comms.WsConn, error) {
				return conn, nil
			},
			newCrypter: func([]byte) encrypt.Crypter { return crypter },
			reCrypter:  func([]byte, []byte) (encrypt.Crypter, error) { return crypter, crypter.recryptErr },
		},
		db:      tdb,
		queue:   queue,
		ws:      conn,
		dc:      dc,
		acct:    acct,
		crypter: crypter,
	}

	return rig
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
		sig := ecdsa.Sign(tDexPriv, sigMsg)
		// Shouldn't Sig be dex.Bytes?
		result := &msgjson.Acknowledgement{Sig: sig.Serialize()}
		resp, _ := msgjson.NewResponse(msg.ID, result, nil)
		f(resp)
		return nil
	})
}

func (rig *testRig) queueConnect(rpcErr *msgjson.Error, matches []*msgjson.Match, orders []*msgjson.OrderStatus) {
	rig.ws.queueResponse(msgjson.ConnectRoute, func(msg *msgjson.Message, f msgFunc) error {
		connect := new(msgjson.Connect)
		msg.Unmarshal(connect)
		sign(tDexPriv, connect)
		result := &msgjson.ConnectResult{Sig: connect.Sig, ActiveMatches: matches, ActiveOrderStatuses: orders}
		var resp *msgjson.Message
		if rpcErr != nil {
			resp, _ = msgjson.NewResponse(msg.ID, nil, rpcErr)
		} else {
			resp, _ = msgjson.NewResponse(msg.ID, result, nil)
		}
		f(resp)
		return nil
	})
}

func (rig *testRig) queueCancel(rpcErr *msgjson.Error) {
	rig.ws.queueResponse(msgjson.CancelRoute, func(msg *msgjson.Message, f msgFunc) error {
		var resp *msgjson.Message
		if rpcErr == nil {
			// Need to stamp and sign the message with the server's key.
			msgOrder := new(msgjson.CancelOrder)
			err := msg.Unmarshal(msgOrder)
			if err != nil {
				rpcErr = msgjson.NewError(msgjson.RPCParseError, "unable to unmarshal request")
			} else {
				co := convertMsgCancelOrder(msgOrder)
				resp = orderResponse(msg.ID, msgOrder, co, false, false, false)
			}
		}
		if rpcErr != nil {
			resp, _ = msgjson.NewResponse(msg.ID, nil, rpcErr)
		}
		f(resp)
		return nil
	})
}

func TestMain(m *testing.M) {
	var shutdown context.CancelFunc
	tCtx, shutdown = context.WithCancel(context.Background())
	tDexPriv, _ = secp256k1.GeneratePrivateKey()
	tDexKey = tDexPriv.PubKey()
	tDexAccountID = account.NewID(tDexPriv.PubKey().SerializeCompressed())

	doIt := func() int {
		// Not counted as coverage, must test Archiver constructor explicitly.
		defer shutdown()
		return m.Run()
	}
	os.Exit(doIt())
}

func TestMarkets(t *testing.T) {
	rig := newTestRig()
	defer rig.shutdown()
	// The test rig's dexConnection comes with a market. Clear that for this test.
	rig.dc.cfgMtx.Lock()
	rig.dc.cfg.Markets = nil
	rig.dc.cfgMtx.Unlock()
	numMarkets := 10

	tCore := rig.core
	// Simulate 10 markets.
	marketIDs := make(map[string]struct{})
	for i := 0; i < numMarkets; i++ {
		base, quote := randomMsgMarket()
		marketIDs[marketName(base.ID, quote.ID)] = struct{}{}
		rig.dc.cfgMtx.RLock()
		cfg := rig.dc.cfg
		rig.dc.cfgMtx.RUnlock()
		cfg.Markets = append(cfg.Markets, &msgjson.Market{
			Name:            base.Symbol + quote.Symbol,
			Base:            base.ID,
			Quote:           quote.ID,
			EpochLen:        5000,
			MarketBuyBuffer: 1.4,
		})
		rig.dc.assetsMtx.Lock()
		rig.dc.assets[base.ID] = convertAssetInfo(base)
		rig.dc.assets[quote.ID] = convertAssetInfo(quote)
		rig.dc.assetsMtx.Unlock()
	}

	// Just check that the information is coming through correctly.
	xcs := tCore.Exchanges()
	if len(xcs) != 1 {
		t.Fatalf("expected 1 MarketInfo, got %d", len(xcs))
	}

	rig.dc.assetsMtx.RLock()
	defer rig.dc.assetsMtx.RUnlock()

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
	defer rig.shutdown()
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
	_, err = tCore.SyncBook("unknown dex", tDCR.ID, tBTC.ID)
	if err == nil {
		t.Fatalf("no error for unknown dex")
	}
	_, err = tCore.SyncBook(tDexHost, tDCR.ID, 12345)
	if err == nil {
		t.Fatalf("no error for nonsense market")
	}

	// Success
	rig.ws.queueResponse(msgjson.OrderBookRoute, func(msg *msgjson.Message, f msgFunc) error {
		f(bookMsg)
		return nil
	})
	feed1, err := tCore.SyncBook(tDexHost, tDCR.ID, tBTC.ID)
	if err != nil {
		t.Fatalf("SyncBook 1 error: %v", err)
	}
	feed2, err := tCore.SyncBook(tDexHost, tDCR.ID, tBTC.ID)
	if err != nil {
		t.Fatalf("SyncBook 2 error: %v", err)
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

	// Both channels should have a full orderbook.
	select {
	case <-feed1.C:
	default:
		t.Fatalf("no book on feed 1")
	}
	select {
	case <-feed2.C:
	default:
		t.Fatalf("no book on feed 2")
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
	defer rig.shutdown()
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
		AssetID: tILT.ID,
		Config: map[string]string{
			"rpclisten": "localhost",
		},
	}

	ensureErr := func(tag string) {
		t.Helper()
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
	defer rig.shutdown()
	tCore := rig.core
	cert := []byte{}

	// DEX already registered
	_, err := tCore.GetFee(tDexHost, cert)
	if !errorHasCode(err, dupeDEXErr) {
		t.Fatalf("wrong account exists error: %v", err)
	}

	// Lose the dexConnection
	tCore.connMtx.Lock()
	delete(tCore.conns, tDexHost)
	tCore.connMtx.Unlock()

	// connectDEX error
	_, err = tCore.GetFee(tUnparseableHost, cert)
	if !errorHasCode(err, connectionErr) {
		t.Fatalf("wrong connectDEX error: %v", err)
	}

	// Queue a config response for success
	rig.queueConfig()

	// Success
	_, err = tCore.GetFee(tDexHost, cert)
	if err != nil {
		t.Fatalf("GetFee error: %v", err)
	}
}

func TestRegister(t *testing.T) {
	// This test takes a little longer because the key is decrypted every time
	// Register is called.
	rig := newTestRig()
	defer rig.shutdown()
	tCore := rig.core
	dc := rig.dc
	delete(tCore.conns, tDexHost)

	wallet, tWallet := newTWallet(tDCR.ID)
	tCore.wallets[tDCR.ID] = wallet

	// When registering, successfully retrieving *db.AccountInfo from the DB is
	// an error (no dupes). Initial state is to return an error.
	rig.db.acctErr = tErr

	regRes := &msgjson.RegisterResult{
		DEXPubKey:    rig.acct.dexPubKey.SerializeCompressed(),
		ClientPubKey: dex.Bytes{0x1}, // part of the serialization, but not the response
		Address:      "someaddr",
		Fee:          tFee,
		Time:         encode.UnixMilliU(time.Now()),
	}
	sign(tDexPriv, regRes)

	queueTipChange := func() {
		go func() {
			t.Helper()
			timeout := time.NewTimer(time.Second * 2)
			for {
				select {
				case <-time.NewTimer(time.Millisecond).C:
					tCore.waiterMtx.Lock()
					waiterCount := len(tCore.blockWaiters)
					tCore.waiterMtx.Unlock()
					if waiterCount > 0 { // when verifyRegistrationFee adds a waiter, then we can trigger tip change
						tWallet.setConfs(tWallet.payFeeCoin.id, 0, nil)
						tCore.tipChange(tDCR.ID, nil)
						return
					}
				case <-timeout.C:
					t.Errorf("failed to find waiter before timeout")
				}
			}
		}()
	}

	queueResponses := func() {
		rig.queueConfig()
		rig.queueRegister(regRes)
		queueTipChange()
		rig.queueNotifyFee()
		rig.queueConnect(nil, nil, nil)
	}

	form := &RegisterForm{
		Addr:    tDexHost,
		AppPass: tPW,
		Fee:     tFee,
		Cert:    []byte{},
	}

	tWallet.payFeeCoin = &tCoin{id: encode.RandomBytes(36)}

	ch := tCore.NotificationFeed()

	var err error
	run := func() {
		// Register method will error if url is already in conns map.
		tCore.connMtx.Lock()
		delete(tCore.conns, tDexHost)
		tCore.connMtx.Unlock()

		tWallet.setConfs(tWallet.payFeeCoin.id, 0, nil)
		_, err = tCore.Register(form)
	}

	getNotification := func(tag string) interface{} {
		t.Helper()
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

	// The feepayment note for mined fee payment txn notification to server, and
	// the balance note from tip change are concurrent and thus come in no
	// guaranteed order.
	getFeeAndBalanceNote := func() *FeePaymentNote {
		t.Helper()
		var feeNote *FeePaymentNote
		var balanceNote *BalanceNote
		for feeNote == nil || balanceNote == nil {
			ntfn := getNotification("feepayment or balance")
			switch note := ntfn.(type) {
			case *FeePaymentNote:
				feeNote = note
			case *BalanceNote:
				balanceNote = note
			default:
				t.Fatalf("wrong notification (%T). Expected FeePaymentNote or BalanceNote", ntfn)
			}
		}
		return feeNote
	}

	queueResponses()
	run()
	if err != nil {
		t.Fatalf("registration error: %v", err)
	}
	// Should be two success notifications. One for fee paid on-chain, one for
	// fee notification sent, each along with a balance note.
	feeNote := getFeeAndBalanceNote() // payment in progress
	if feeNote.Severity() != db.Success {
		t.Fatalf("fee payment error notification: %s: %s", feeNote.Subject(), feeNote.Details())
	}
	feeNote = getFeeAndBalanceNote() // balance note concurrent with fee note (Account registered)
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
	if !errorHasCode(err, missingWalletErr) {
		t.Fatalf("wrong missing wallet error: %v", err)
	}
	tCore.wallets[tDCR.ID] = wallet

	// Unlock wallet error
	tWallet.unlockErr = tErr
	tWallet.locked = true
	_, err = tCore.Register(form)
	if !errorHasCode(err, walletAuthErr) {
		t.Fatalf("wrong wallet auth error: %v", err)
	}
	tWallet.unlockErr = nil
	tWallet.locked = false

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
	// register: balance note followed by fee payment note
	feeNote = getFeeAndBalanceNote() // Fee payment in progress
	if feeNote.Severity() != db.Success {
		t.Fatalf("fee payment error notification: %s: %s", feeNote.Subject(), feeNote.Details())
	}
	// fee error. fee and balance note from tip change in no guaranteed order.
	feeNote = getFeeAndBalanceNote() // Fee payment error "test error message"
	if feeNote.Severity() != db.ErrorLevel {
		t.Fatalf("non-error fee payment notification for notifyfee response error: %s: %s", feeNote.Subject(), feeNote.Details())
	}

	// Make sure it's good again.
	queueResponses()
	run()
	if err != nil {
		t.Fatalf("error after regaining valid state: %v", err)
	}
	feeNote = getFeeAndBalanceNote() // Fee payment in progress
	if feeNote.Severity() != db.Success {
		t.Fatalf("fee payment error notification: %s: %s", feeNote.Subject(), feeNote.Details())
	}
}

func TestLogin(t *testing.T) {
	rig := newTestRig()
	defer rig.shutdown()
	tCore := rig.core
	rig.acct.markFeePaid()

	rig.queueConnect(nil, nil, nil)
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
	rig.queueConnect(nil, nil, nil)
	_, err = tCore.Login(tPW)
	if err != nil || rig.acct.authed() {
		t.Fatalf("error for unpaid account: %v", err)
	}
	if rig.acct.locked() {
		t.Fatalf("unpaid account is locked")
	}
	rig.acct.markFeePaid()

	// 'connect' route error.
	rig = newTestRig()
	defer rig.shutdown()
	tCore = rig.core
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

	// Success with some matches in the response.
	rig = newTestRig()
	defer rig.shutdown()
	dc := rig.dc
	qty := 3 * tDCR.LotSize
	lo, dbOrder, preImg, addr := makeLimitOrder(dc, true, qty, tBTC.RateStep*10)
	lo.Force = order.StandingTiF
	dbOrder.MetaData.Status = order.OrderStatusBooked // leave unfunded to have it canceled on auth/'connect'
	oid := lo.ID()
	mkt := dc.marketConfig(tDcrBtcMktName)
	dcrWallet, _ := newTWallet(tDCR.ID)
	tCore.wallets[tDCR.ID] = dcrWallet
	btcWallet, tBtcWallet := newTWallet(tBTC.ID)
	tCore.wallets[tBTC.ID] = btcWallet
	walletSet, _ := tCore.walletSet(dc, tDCR.ID, tBTC.ID, true)
	tracker := newTrackedTrade(dbOrder, preImg, dc, mkt.EpochLen, rig.core.lockTimeTaker, rig.core.lockTimeMaker,
		rig.db, rig.queue, walletSet, nil, rig.core.notify) // nil means no funding coins
	matchID := ordertest.RandomMatchID()
	match := &matchTracker{
		MetaMatch: db.MetaMatch{
			UserMatch: &order.UserMatch{MatchID: matchID},
			MetaData:  &db.MatchMetaData{},
		},
	}
	tracker.matches[matchID] = match
	knownMsgMatch := &msgjson.Match{OrderID: oid[:], MatchID: matchID[:]}

	// Known trade, but missing match
	missingID := ordertest.RandomMatchID()
	missingMatch := &matchTracker{
		MetaMatch: db.MetaMatch{
			UserMatch: &order.UserMatch{MatchID: missingID},
			MetaData:  &db.MatchMetaData{},
		},
	}
	tracker.matches[missingID] = missingMatch

	// extra match
	extraID := ordertest.RandomMatchID()
	matchTime := time.Now()
	extraMsgMatch := &msgjson.Match{
		OrderID:    oid[:],
		MatchID:    extraID[:],
		Side:       uint8(order.Taker),
		Status:     uint8(order.MakerSwapCast),
		ServerTime: encode.UnixMilliU(matchTime),
	}

	// The extra match is already at MakerSwapCast, and we're the taker, which
	// will invoke match status conflict resolution and a contract audit.
	_, auditInfo := tMsgAudit(oid, extraID, addr, qty, encode.RandomBytes(32))
	auditInfo.Expiration = encode.DropMilliseconds(matchTime.Add(tracker.lockTimeMaker))
	tBtcWallet.auditInfo = auditInfo
	missedContract := encode.RandomBytes(50)
	rig.ws.queueResponse(msgjson.MatchStatusRoute, func(msg *msgjson.Message, f msgFunc) error {
		resp, _ := msgjson.NewResponse(msg.ID, []*msgjson.MatchStatusResult{{
			MatchID:       extraID[:],
			Status:        uint8(order.MakerSwapCast),
			MakerContract: missedContract,
			MakerSwap:     encode.RandomBytes(36),
			Active:        true,
			MakerTxData:   []byte{0x01},
		}}, nil)
		f(resp)
		return nil
	})

	dc.trades = map[order.OrderID]*trackedTrade{
		oid: tracker,
	}

	tCore = rig.core
	rig.acct.markFeePaid()
	rig.queueConnect(nil, []*msgjson.Match{knownMsgMatch /* missing missingMatch! */, extraMsgMatch}, nil)
	rig.queueCancel(nil) // for the unfunded order that gets canceled in authDEX
	// Login>authDEX will do 4 match DB updates for these two matches:
	// missing -> revoke -> update match
	// extra -> negotiate -> newTrackers -> update match
	// matchConflicts (from extras) -> resolveMatchConflicts -> resolveConflictWithServerData
	// 	-> update match after spawning auditContract
	// 	-> update match in auditContract (second because of lock) ** the ASYNC one we have to wait for **
	rig.db.setUpdateMatchHook(missingID, make(chan order.MatchStatus, 1))
	rig.db.setUpdateMatchHook(extraID, make(chan order.MatchStatus, 3))
	_, err = tCore.Login(tPW) // authDEX -> async contract audit for the extra match
	if err != nil || !rig.acct.authed() {
		t.Fatalf("final Login error: %v", err)
	}
	// Wait for expected db updates.
	rig.db.waitForMatchUpdateOrFail(missingID, t)
	for i := 0; i < 3; i++ {
		rig.db.waitForMatchUpdateOrFail(extraID, t)
	}

	// check t.metaData.LinkedOrder or for db.LinkOrder call, then db.UpdateOrder call
	if tracker.metaData.LinkedOrder.IsZero() {
		t.Errorf("cancel order not set")
	}
	if rig.db.linkedFromID != oid || rig.db.linkedToID.IsZero() {
		t.Errorf("automatic cancel order not linked")
	}

	if !tracker.matches[missingID].MetaData.Proof.SelfRevoked {
		t.Errorf("SelfRevoked not true for missing match tracker")
	}
	if tracker.matches[matchID].swapErr != nil {
		t.Errorf("swapErr set for non-missing match tracker")
	}
	if tracker.matches[matchID].MetaData.Proof.IsRevoked() {
		t.Errorf("IsRevoked true for non-missing match tracker")
	}
	// Conflict resolution will have run negotiate on the extra match from the
	// connect response, bringing our match count up to 3.
	if len(tracker.matches) != 3 {
		t.Errorf("Extra trade not accepted into matches")
	}
	tracker.mtx.Lock()
	defer tracker.mtx.Unlock()
	match = tracker.matches[extraID]
	if !bytes.Equal(match.MetaData.Proof.CounterContract, missedContract) {
		t.Errorf("Missed maker contract not retrieved, %s, %s", match.MatchID, hex.EncodeToString(match.MetaData.Proof.CounterContract))
	}
}

func TestLoginAccountNotFoundError(t *testing.T) {
	rig := newTestRig()
	defer rig.shutdown()
	tCore := rig.core
	rig.acct.markFeePaid()

	expectedErrorMessage := "test account not found error"
	accountNotFoundError := msgjson.NewError(msgjson.AccountNotFoundError, expectedErrorMessage)
	rig.queueConnect(accountNotFoundError, nil, nil)

	wallet, _ := newTWallet(tDCR.ID)
	tCore.wallets[tDCR.ID] = wallet
	rig.queueConnect(nil, nil, nil)

	result, err := tCore.Login(tPW)
	if err != nil {
		t.Fatalf("unexpected Login error: %v", err)
	}
	for _, dexStat := range result.DEXes {
		if dexStat.Authed || dexStat.AuthErr == "" || !strings.Contains(dexStat.AuthErr, expectedErrorMessage) {
			t.Fatalf("expected account not found error")
		}
	}
}

func TestInitializeDEXConnectionsSuccess(t *testing.T) {
	rig := newTestRig()
	defer rig.shutdown()
	tCore := rig.core
	rig.acct.markFeePaid()
	rig.queueConnect(nil, nil, nil)

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
	defer rig.shutdown()
	tCore := rig.core
	rig.acct.markFeePaid()
	expectedErrorMessage := "test account not found error"
	rig.queueConnect(msgjson.NewError(msgjson.AccountNotFoundError, expectedErrorMessage), nil, nil)

	dexStats := tCore.initializeDEXConnections(rig.crypter)

	if dexStats == nil {
		t.Fatal("initializeDEXConnections failure")
	}
	for _, dexStat := range dexStats {
		if dexStat.AuthErr == "" {
			t.Fatalf("initializeDEXConnections authorization error %v", dexStat.AuthErr)
		}
		if dexStat.Authed || dexStat.AuthErr == "" || !strings.Contains(dexStat.AuthErr, expectedErrorMessage) {
			t.Fatalf("expected account not found error")
		}
	}
	if len(rig.core.conns) > 0 {
		t.Fatalf("expected rig.core.conns to be empty due to disabling the account")
	}
}

func TestConnectDEX(t *testing.T) {
	rig := newTestRig()
	defer rig.shutdown()
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
	defer rig.shutdown()
	tCore := rig.core
	rig.db.existValues[keyParamsKey] = false

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
	defer rig.shutdown()
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
	defer rig.shutdown()
	tCore := rig.core
	dcrWallet, tDcrWallet := newTWallet(tDCR.ID)
	tCore.wallets[tDCR.ID] = dcrWallet
	dcrWallet.address = "DsVmA7aqqWeKWy461hXjytbZbgCqbB8g2dq"
	dcrWallet.Unlock(rig.crypter)

	btcWallet, tBtcWallet := newTWallet(tBTC.ID)
	tCore.wallets[tBTC.ID] = btcWallet
	btcWallet.address = "12DXGkvxFjuq5btXYkwWfBZaz1rVwFgini"
	btcWallet.Unlock(rig.crypter)

	var lots uint64 = 10
	qty := tDCR.LotSize * lots
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
	tDcrWallet.fundingCoins = asset.Coins{dcrCoin}
	tDcrWallet.fundRedeemScripts = []dex.Bytes{nil}

	btcVal := calc.BaseToQuote(rate, qty*2)
	btcCoin := &tCoin{
		id:  encode.RandomBytes(36),
		val: btcVal,
	}
	tBtcWallet.fundingCoins = asset.Coins{btcCoin}
	tBtcWallet.fundRedeemScripts = []dex.Bytes{nil}

	book := newBookie(tLogger, func() {})
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
		t.Helper()
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
		t.Helper()
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
		t.Helper()
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
	if tDcrWallet.fundedSwaps != lots {
		t.Fatalf("limit sell expected %d max swaps, got %d", lots, tDcrWallet.fundedSwaps)
	}
	tDcrWallet.fundedSwaps = 0

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

	// Account locked = probably not logged in
	rig.dc.acct.lock()
	_, err = tCore.Trade(tPW, form)
	if err == nil {
		t.Fatalf("no error for disconnected dex")
	}
	rig.dc.acct.unlock(rig.crypter)

	// DEX not connected
	atomic.StoreUint32(&rig.dc.connected, 0)
	_, err = tCore.Trade(tPW, form)
	if err == nil {
		t.Fatalf("no error for disconnected dex")
	}
	atomic.StoreUint32(&rig.dc.connected, 1)

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
	tDcrWallet.fundingCoinErr = tErr
	ensureErr("funds error")
	tDcrWallet.fundingCoinErr = nil

	// Lot size violation
	ogQty := form.Qty
	form.Qty += tDCR.LotSize / 2
	ensureErr("bad size")
	form.Qty = ogQty

	// Coin signature error
	tDcrWallet.signCoinErr = tErr
	ensureErr("signature error")
	tDcrWallet.signCoinErr = nil

	// Sync-in-progress error
	dcrWallet.synced = false
	ensureErr("base not synced")
	dcrWallet.synced = true

	btcWallet.synced = false
	ensureErr("quote not synced")
	btcWallet.synced = true

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
	// The number of lots should still be the same as for a sell order.
	if tBtcWallet.fundedSwaps != lots {
		t.Fatalf("limit buy expected %d max swaps, got %d", lots, tBtcWallet.fundedSwaps)
	}
	tBtcWallet.fundedSwaps = 0

	// Successful market buy order
	form.IsLimit = false
	form.Qty = calc.BaseToQuote(rate, qty)
	rig.ws.queueResponse(msgjson.MarketRoute, handleMarket)
	_, err = tCore.Trade(tPW, form)
	if err != nil {
		t.Fatalf("market order error: %v", err)
	}

	// The funded qty for a market buy should not be adjusted.
	if tBtcWallet.fundedVal != form.Qty {
		t.Fatalf("market buy expected funded value %d, got %d", qty, tBtcWallet.fundedVal)
	}
	tBtcWallet.fundedVal = 0
	if tBtcWallet.fundedSwaps != lots {
		t.Fatalf("market buy expected %d max swaps, got %d", lots, tBtcWallet.fundedSwaps)
	}
	tBtcWallet.fundedSwaps = 0

	// Successful market sell order.
	form.Sell = true
	form.Qty = qty
	rig.ws.queueResponse(msgjson.MarketRoute, handleMarket)
	_, err = tCore.Trade(tPW, form)
	if err != nil {
		t.Fatalf("market order error: %v", err)
	}

	// The funded qty for a market sell order should not be adjusted.
	if tDcrWallet.fundedVal != qty {
		t.Fatalf("market sell expected funded value %d, got %d", qty, tDcrWallet.fundedVal)
	}
	if tDcrWallet.fundedSwaps != lots {
		t.Fatalf("market sell expected %d max swaps, got %d", lots, tDcrWallet.fundedSwaps)
	}
}

func TestCancel(t *testing.T) {
	rig := newTestRig()
	defer rig.shutdown()
	dc := rig.dc
	lo, dbOrder, preImg, _ := makeLimitOrder(dc, true, 0, 0)
	lo.Force = order.StandingTiF
	oid := lo.ID()
	mkt := dc.marketConfig(tDcrBtcMktName)
	tracker := newTrackedTrade(dbOrder, preImg, dc, mkt.EpochLen, rig.core.lockTimeTaker, rig.core.lockTimeMaker,
		rig.db, rig.queue, nil, nil, rig.core.notify)
	dc.trades[oid] = tracker

	rig.queueCancel(nil)
	err := rig.core.Cancel(tPW, oid[:])
	if err != nil {
		t.Fatalf("cancel error: %v", err)
	}
	if tracker.cancel == nil {
		t.Fatalf("cancel order not found")
	}

	ensureErr := func(tag string) {
		t.Helper()
		err := rig.core.Cancel(tPW, oid[:])
		if err == nil {
			t.Fatalf("%s: no error", tag)
		}
	}

	// Should get an error for existing cancel order.
	ensureErr("second cancel")

	// remove the cancel order so we can check its nilness on error.
	tracker.cancel = nil

	ensureNilCancel := func(tag string) {
		if tracker.cancel != nil {
			t.Fatalf("%s: cancel order found", tag)
		}
	}

	// Bad order ID
	ogID := oid
	oid = order.OrderID{0x01, 0x02}
	ensureErr("bad id")
	ensureNilCancel("bad id")
	oid = ogID

	// Order not found
	delete(dc.trades, oid)
	ensureErr("no order")
	ensureNilCancel("no order")
	dc.trades[oid] = tracker

	// Send error
	rig.ws.reqErr = tErr
	ensureErr("Request error")
	ensureNilCancel("Request error")
	rig.ws.reqErr = nil

}

func TestHandlePreimageRequest(t *testing.T) {
	rig := newTestRig()
	defer rig.shutdown()
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
		db:       rig.db,
		dc:       rig.dc,
		metaData: &db.OrderMetaData{},
	}

	loadSyncer := func() {
		rig.core.piSyncMtx.Lock()
		rig.core.piSyncers[oid] = make(chan struct{})
		rig.core.piSyncMtx.Unlock()
	}

	rig.dc.trades[oid] = tracker
	loadSyncer()
	err := handlePreimageRequest(rig.core, rig.dc, req)
	if err != nil {
		t.Fatalf("handlePreimageRequest error: %v", err)
	}

	ensureErr := func(tag string) {
		t.Helper()
		loadSyncer()
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

func TestHandleRevokeOrderMsg(t *testing.T) {
	rig := newTestRig()
	defer rig.shutdown()
	dc := rig.dc
	tCore := rig.core
	dcrWallet, tDcrWallet := newTWallet(tDCR.ID)
	tCore.wallets[tDCR.ID] = dcrWallet
	dcrWallet.address = "DsVmA7aqqWeKWy461hXjytbZbgCqbB8g2dq"
	dcrWallet.Unlock(rig.crypter)

	fundCoinDcrID := encode.RandomBytes(36)
	fundCoinDcr := &tCoin{id: fundCoinDcrID}

	btcWallet, _ := newTWallet(tBTC.ID)
	tCore.wallets[tBTC.ID] = btcWallet
	btcWallet.address = "12DXGkvxFjuq5btXYkwWfBZaz1rVwFgini"
	btcWallet.Unlock(rig.crypter)

	// fundCoinBID := encode.RandomBytes(36)
	// fundCoinB := &tCoin{id: fundCoinBID}

	qty := 2 * tDCR.LotSize
	rate := tBTC.RateStep * 10
	lo, dbOrder, preImg, _ := makeLimitOrder(dc, true, qty, rate) // sell DCR
	lo.Coins = []order.CoinID{fundCoinDcrID}
	dbOrder.MetaData.Status = order.OrderStatusBooked
	oid := lo.ID()

	tDcrWallet.fundingCoins = asset.Coins{fundCoinDcr}

	walletSet, err := tCore.walletSet(dc, tDCR.ID, tBTC.ID, true)
	if err != nil {
		t.Fatalf("walletSet error: %v", err)
	}

	// Not in dc.trades yet.

	// Send a request for the unknown order.
	payload := &msgjson.RevokeOrder{
		OrderID: oid[:],
	}
	req, _ := msgjson.NewRequest(rig.dc.NextID(), msgjson.RevokeOrderRoute, payload)

	// Ensure revoking a non-existent order generates an error.
	err = handleRevokeOrderMsg(rig.core, rig.dc, req)
	if err == nil {
		t.Fatal("[handleRevokeOrderMsg] expected a non-existent order")
	}

	// Now store the order in dc.trades.
	mkt := dc.marketConfig(tDcrBtcMktName)
	tracker := newTrackedTrade(dbOrder, preImg, dc, mkt.EpochLen,
		rig.core.lockTimeTaker, rig.core.lockTimeMaker,
		rig.db, rig.queue, walletSet, tDcrWallet.fundingCoins, rig.core.notify)
	rig.dc.trades[oid] = tracker

	orderNotes, feedDone := orderNoteFeed(tCore)
	defer feedDone()

	// Success
	err = handleRevokeOrderMsg(rig.core, rig.dc, req)
	if err != nil {
		t.Fatalf("handleRevokeOrderMsg error: %v", err)
	}

	verifyRevokeNotification(orderNotes, SubjectOrderRevoked, t)

	if tracker.metaData.Status != order.OrderStatusRevoked {
		t.Errorf("expected order status %v, got %v", order.OrderStatusRevoked, tracker.metaData.Status)
	}
}

func TestHandleRevokeMatchMsg(t *testing.T) {
	rig := newTestRig()
	defer rig.shutdown()
	dc := rig.dc
	tCore := rig.core
	dcrWallet, tDcrWallet := newTWallet(tDCR.ID)
	tCore.wallets[tDCR.ID] = dcrWallet
	dcrWallet.address = "DsVmA7aqqWeKWy461hXjytbZbgCqbB8g2dq"
	dcrWallet.Unlock(rig.crypter)

	fundCoinDcrID := encode.RandomBytes(36)
	fundCoinDcr := &tCoin{id: fundCoinDcrID}

	btcWallet, _ := newTWallet(tBTC.ID)
	tCore.wallets[tBTC.ID] = btcWallet
	btcWallet.address = "12DXGkvxFjuq5btXYkwWfBZaz1rVwFgini"
	btcWallet.Unlock(rig.crypter)

	// fundCoinBID := encode.RandomBytes(36)
	// fundCoinB := &tCoin{id: fundCoinBID}

	matchSize := 4 * tDCR.LotSize
	cancelledQty := tDCR.LotSize
	qty := 2*matchSize + cancelledQty
	//rate := tBTC.RateStep * 10
	lo, dbOrder, preImg, _ := makeLimitOrder(dc, true, qty, tBTC.RateStep)
	lo.Coins = []order.CoinID{fundCoinDcrID}
	dbOrder.MetaData.Status = order.OrderStatusBooked
	oid := lo.ID()

	tDcrWallet.fundingCoins = asset.Coins{fundCoinDcr}

	mid := ordertest.RandomMatchID()
	walletSet, err := tCore.walletSet(dc, tDCR.ID, tBTC.ID, true)
	if err != nil {
		t.Fatalf("walletSet error: %v", err)
	}
	mkt := dc.marketConfig(tDcrBtcMktName)

	tracker := newTrackedTrade(dbOrder, preImg, dc, mkt.EpochLen,
		rig.core.lockTimeTaker, rig.core.lockTimeMaker,
		rig.db, rig.queue, walletSet, tDcrWallet.fundingCoins, rig.core.notify)

	match := &matchTracker{
		MetaMatch: db.MetaMatch{
			UserMatch: &order.UserMatch{MatchID: mid},
			MetaData:  &db.MatchMetaData{},
		},
	}
	tracker.matches[mid] = match

	payload := &msgjson.RevokeMatch{
		OrderID: oid[:],
		MatchID: mid[:],
	}
	req, _ := msgjson.NewRequest(rig.dc.NextID(), msgjson.RevokeMatchRoute, payload)

	// Ensure revoking a non-existent order generates an error.
	err = handleRevokeMatchMsg(rig.core, rig.dc, req)
	if err == nil {
		t.Fatal("[handleRevokeMatchMsg] expected a non-existent order")
	}

	rig.dc.trades[oid] = tracker

	// Success
	err = handleRevokeMatchMsg(rig.core, rig.dc, req)
	if err != nil {
		t.Fatalf("handleRevokeMatchMsg error: %v", err)
	}
}

func TestTradeTracking(t *testing.T) {
	rig := newTestRig()
	defer rig.shutdown()
	dc := rig.dc
	tCore := rig.core
	dcrWallet, tDcrWallet := newTWallet(tDCR.ID)
	tCore.wallets[tDCR.ID] = dcrWallet
	dcrWallet.address = "DsVmA7aqqWeKWy461hXjytbZbgCqbB8g2dq"
	dcrWallet.Unlock(rig.crypter)

	btcWallet, tBtcWallet := newTWallet(tBTC.ID)
	tCore.wallets[tBTC.ID] = btcWallet
	btcWallet.address = "12DXGkvxFjuq5btXYkwWfBZaz1rVwFgini"
	btcWallet.Unlock(rig.crypter)

	matchSize := 4 * tDCR.LotSize
	cancelledQty := tDCR.LotSize
	qty := 2*matchSize + cancelledQty
	rate := tBTC.RateStep * 10
	lo, dbOrder, preImgL, addr := makeLimitOrder(dc, true, qty, tBTC.RateStep)
	lo.Force = order.StandingTiF
	// fundCoinDcrID := encode.RandomBytes(36)
	// lo.Coins = []order.CoinID{fundCoinDcrID}
	loid := lo.ID()

	//fundCoinDcr := &tCoin{id: fundCoinDcrID}
	//tDcrWallet.fundingCoins = asset.Coins{fundCoinDcr}

	mid := ordertest.RandomMatchID()
	walletSet, err := tCore.walletSet(dc, tDCR.ID, tBTC.ID, true)
	if err != nil {
		t.Fatalf("walletSet error: %v", err)
	}
	mkt := dc.marketConfig(tDcrBtcMktName)
	fundCoinDcrID := encode.RandomBytes(36)
	fundingCoins := asset.Coins{&tCoin{id: fundCoinDcrID}}
	tracker := newTrackedTrade(dbOrder, preImgL, dc, mkt.EpochLen, rig.core.lockTimeTaker, rig.core.lockTimeMaker,
		rig.db, rig.queue, walletSet, fundingCoins, rig.core.notify)
	rig.dc.trades[tracker.ID()] = tracker
	var match *matchTracker
	checkStatus := func(tag string, wantStatus order.MatchStatus) {
		t.Helper()
		if match.Status != wantStatus {
			t.Fatalf("%s: wrong status wanted %v, got %v", tag,
				wantStatus, match.Status)
		}
	}

	// create new notification feed to catch swap-related errors from goroutines
	notes := tCore.NotificationFeed()
	drainNotes := func() {
		for {
			select {
			case <-notes:
			default:
				return
			}
		}
	}

	lastSwapErrorNote := func() Notification {
		for {
			select {
			case note := <-notes:
				if note.Severity() == db.ErrorLevel && note.Subject() == SubjectSwapError {
					return note
				}
			default:
				return nil
			}
		}
	}

	type swapRelatedAction struct {
		name                 string
		fn                   func() error
		expectError          bool
		expectMatchDBUpdates map[order.MatchID]int
		expectSwapErrorNote  bool
	}
	testSwapRelatedAction := func(action swapRelatedAction) {
		t.Helper()
		drainNotes() // clear previous (swap error) notes before exec'ing swap-related action
		for mid, nMatchDBUpdate := range action.expectMatchDBUpdates {
			if nMatchDBUpdate > 0 {
				rig.db.setUpdateMatchHook(mid, make(chan order.MatchStatus, nMatchDBUpdate))
			}
		}
		// Try the action and confirm the behaviour is as expected.
		err := action.fn()
		if action.expectError && err == nil {
			t.Fatalf("%s: expected error but got nil", action.name)
		} else if !action.expectError && err != nil {
			t.Fatalf("%s: unexpected error: %v", action.name, err)
		}
		// Check that we received the expected number of match db updates.
		for mid, nMatchDBUpdate := range action.expectMatchDBUpdates {
			for i := 0; i < nMatchDBUpdate; i++ {
				rig.db.waitForMatchUpdateOrFail(mid, t)
			}
			rig.db.setUpdateMatchHook(mid, nil)
		}
		// Check that we received a swap error note (if expected), and that
		// no error note was received, if not expected.
		time.Sleep(100 * time.Millisecond) // wait briefly as swap error notes may be sent from a goroutine
		swapErrNote := lastSwapErrorNote()
		if action.expectSwapErrorNote && swapErrNote == nil {
			t.Fatalf("%s: expected swap error note but got nil", action.name)
		} else if !action.expectSwapErrorNote && swapErrNote != nil {
			t.Fatalf("%s: unexpected swap error note: %s", action.name, swapErrNote.Details())
		}
	}

	// MAKER MATCH
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

	// Make sure that a fee rate higher than our recorded MaxFeeRate results in
	// an error.
	msgMatch.FeeRateBase = tMaxFeeRate + 1
	msg, _ := msgjson.NewRequest(1, msgjson.MatchRoute, []*msgjson.Match{msgMatch})
	err = handleMatchRoute(tCore, rig.dc, msg)
	if err == nil || !strings.Contains(err.Error(), "is > MaxFeeRate") {
		t.Fatalf("no error for fee rate > MaxFeeRate %t", lo.Trade().Sell)
	}

	// Restore fee rate.
	msgMatch.FeeRateBase = tMaxFeeRate
	msg, _ = msgjson.NewRequest(1, msgjson.MatchRoute, []*msgjson.Match{msgMatch})

	// Handle new match as maker with a queued invalid DEX init ack.
	// handleMatchRoute should have no errors but trigger a match db update (status = NewlyMatched).
	// Maker's swap should be bcasted, triggering another match db update (NewlyMatched->MakerSwapCast).
	// sendInitAsync should fail because of invalid ack and produce a swap error note.
	testSwapRelatedAction(swapRelatedAction{
		name: "handleMatchRoute",
		fn: func() error {
			// queue an invalid DEX init ack
			rig.ws.queueResponse(msgjson.InitRoute, invalidAcker)
			return handleMatchRoute(tCore, rig.dc, msg)
		},
		expectError:          false,
		expectMatchDBUpdates: map[order.MatchID]int{mid: 2},
		expectSwapErrorNote:  true,
	})

	var found bool
	match, found = tracker.matches[mid]
	if !found {
		t.Fatalf("match not found")
	}

	// We're the maker, so the init transaction should be broadcast.
	checkStatus("maker swapped", order.MakerSwapCast)
	proof, auth := &match.MetaData.Proof, &match.MetaData.Proof.Auth
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

	// requeue an invalid DEX init ack and resend pending init request
	testSwapRelatedAction(swapRelatedAction{
		name: "resend pending init (invalid ack)",
		fn: func() error {
			rig.ws.queueResponse(msgjson.InitRoute, invalidAcker)
			tCore.resendPendingRequests(tracker)
			return nil
		},
		expectError:          false,
		expectMatchDBUpdates: nil,  // no db update for invalid init ack
		expectSwapErrorNote:  true, // expect swap error note for invalid init ack
	})
	// auth.InitSig should remain unset because our resent init request
	// received an invalid ack still
	if len(auth.InitSig) != 0 {
		t.Fatalf("init sig recorded for second invalid init ack")
	}

	// queue a valid DEX init ack and re-send pending init request
	// a valid ack should produce a db update otherwise it's an error
	testSwapRelatedAction(swapRelatedAction{
		name: "resend pending init (valid ack)",
		fn: func() error {
			rig.ws.queueResponse(msgjson.InitRoute, initAcker)
			tCore.resendPendingRequests(tracker)
			return nil
		},
		expectError:          false,
		expectMatchDBUpdates: map[order.MatchID]int{mid: 1}, // expect db update for valid init ack
		expectSwapErrorNote:  false,                         // no swap error note for valid init ack
	})
	// auth.InitSig should now be set because our init request received
	// a valid ack
	if len(auth.InitSig) == 0 {
		t.Fatalf("init sig not recorded for valid init ack")
	}

	// Send the counter-party's init info.
	auditQty := calc.BaseToQuote(rate, matchSize)
	audit, auditInfo := tMsgAudit(loid, mid, addr, auditQty, proof.SecretHash)
	auditInfo.Expiration = encode.DropMilliseconds(matchTime.Add(tracker.lockTimeTaker))
	tBtcWallet.auditInfo = auditInfo
	msg, _ = msgjson.NewRequest(1, msgjson.AuditRoute, audit)

	// Check audit errors.
	tBtcWallet.auditErr = tErr
	err = tracker.auditContract(match, audit.CoinID, audit.Contract, nil)
	if err == nil {
		t.Fatalf("no maker error for AuditContract error")
	}

	// Check expiration error.
	match.MetaData.Proof.SelfRevoked = true // keeps trying unless revoked
	tBtcWallet.auditErr = asset.CoinNotFoundError
	err = tracker.auditContract(match, audit.CoinID, audit.Contract, nil)
	if err == nil {
		t.Fatalf("no maker error for AuditContract expiration")
	}
	var expErr ExpirationErr
	if !errors.As(err, &expErr) {
		t.Fatalf("wrong error type. expecting ExpirationTimeout, got %T: %v", err, err)
	}
	tBtcWallet.auditErr = nil
	match.MetaData.Proof.SelfRevoked = false

	auditInfo.Coin.(*tCoin).val = auditQty - 1
	err = tracker.auditContract(match, audit.CoinID, audit.Contract, nil)
	if err == nil {
		t.Fatalf("no maker error for low value")
	}
	auditInfo.Coin.(*tCoin).val = auditQty

	auditInfo.SecretHash = []byte{0x01}
	err = tracker.auditContract(match, audit.CoinID, audit.Contract, nil)
	if err == nil {
		t.Fatalf("no maker error for wrong secret hash")
	}
	auditInfo.SecretHash = proof.SecretHash

	auditInfo.Recipient = "wrong address"
	err = tracker.auditContract(match, audit.CoinID, audit.Contract, nil)
	if err == nil {
		t.Fatalf("no maker error for wrong address")
	}
	auditInfo.Recipient = addr

	auditInfo.Expiration = matchTime.Add(tracker.lockTimeTaker - time.Hour)
	err = tracker.auditContract(match, audit.CoinID, audit.Contract, nil)
	if err == nil {
		t.Fatalf("no maker error for early lock time")
	}
	auditInfo.Expiration = matchTime.Add(tracker.lockTimeTaker)

	// success, full handleAuditRoute>processAuditMsg>auditContract
	rig.db.setUpdateMatchHook(mid, make(chan order.MatchStatus, 1))
	err = handleAuditRoute(tCore, rig.dc, msg)
	if err != nil {
		t.Fatalf("audit error: %v", err)
	}
	// let the async auditContract run
	newMatchStatus := rig.db.waitForMatchUpdateOrFail(mid, t)
	if newMatchStatus != order.TakerSwapCast {
		t.Fatalf("wrong match status. wanted %v, got %v", order.TakerSwapCast, newMatchStatus)
	}
	if match.counterSwap == nil {
		t.Fatalf("counter-swap not set")
	}
	if !bytes.Equal(proof.CounterContract, audit.Contract) {
		t.Fatalf("counter-script not recorded")
	}
	if !bytes.Equal(proof.TakerSwap, audit.CoinID) {
		t.Fatalf("taker contract ID not set")
	}
	rig.db.waitForMatchUpdateOrFail(mid, t) // AuditSig is set in a second match data update
	if !bytes.Equal(auth.AuditSig, audit.Sig) {
		t.Fatalf("audit sig not set")
	}
	if auth.AuditStamp != audit.Time {
		t.Fatalf("audit time not set")
	}

	// Confirming the counter-swap triggers a redemption.
	tBtcWallet.setConfs(auditInfo.Coin.ID(), tBTC.SwapConf, nil)
	redeemCoin := encode.RandomBytes(36)
	//<-tBtcWallet.redeemErrChan
	tBtcWallet.redeemCoins = []dex.Bytes{redeemCoin}
	rig.ws.queueResponse(msgjson.RedeemRoute, redeemAcker)
	tCore.tickAsset(dc, tBTC.ID)
	// TakerSwapCast -> MakerRedeem update (after bcasting redeem)
	newMatchStatus = rig.db.waitForMatchUpdateOrFail(mid, t)
	if newMatchStatus != order.MakerRedeemed {
		t.Fatalf("wrong match status. wanted %v, got %v", order.MakerRedeemed, newMatchStatus)
	}
	// MakerRedeem -> MatchComplete update (after maker's redeem ack is received with valid sig)
	newMatchStatus = rig.db.waitForMatchUpdateOrFail(mid, t)
	if newMatchStatus != order.MatchComplete {
		t.Fatalf("wrong match status. wanted %v, got %v", order.MatchComplete, newMatchStatus)
	}
	if !bytes.Equal(proof.MakerRedeem, redeemCoin) {
		t.Fatalf("redeem coin ID not logged")
	}
	// No redemption request received as maker. Only taker gets a redemption
	// request following maker's redeem.

	// Check that fees were incremented appropriately.
	if tracker.metaData.SwapFeesPaid != tSwapFeesPaid {
		t.Fatalf("wrong fees recorded for swap. expected %d, got %d", tSwapFeesPaid, tracker.metaData.SwapFeesPaid)
	}
	// Check that fees were incremented appropriately.
	if tracker.metaData.RedemptionFeesPaid != tRedemptionFeesPaid {
		t.Fatalf("wrong fees recorded for redemption. expected %d, got %d", tRedemptionFeesPaid, tracker.metaData.SwapFeesPaid)
	}
	rig.db.setUpdateMatchHook(mid, nil)

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
	rig.db.setUpdateMatchHook(mid, make(chan order.MatchStatus, 1))
	err = handleMatchRoute(tCore, rig.dc, msg)
	if err != nil {
		t.Fatalf("match messages error: %v", err)
	}
	match, found = tracker.matches[mid]
	if !found {
		t.Fatalf("match not found")
	}
	newMatchStatus = rig.db.waitForMatchUpdateOrFail(mid, t)
	if newMatchStatus != order.NewlyMatched {
		t.Fatalf("wrong match status. wanted %v, got %v", order.NewlyMatched, newMatchStatus)
	}
	proof, auth = &match.MetaData.Proof, &match.MetaData.Proof.Auth
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
	// early lock time
	auditInfo.Expiration = matchTime.Add(tracker.lockTimeMaker - time.Hour)
	err = tracker.auditContract(match, audit.CoinID, audit.Contract, nil)
	if err == nil {
		t.Fatalf("no taker error for early lock time")
	}

	// success, full handleAuditRoute>processAuditMsg>auditContract
	auditInfo.Expiration = encode.DropMilliseconds(matchTime.Add(tracker.lockTimeMaker))
	msg, _ = msgjson.NewRequest(1, msgjson.AuditRoute, audit)
	err = handleAuditRoute(tCore, rig.dc, msg)
	if err != nil {
		t.Fatalf("taker's match message error: %v", err)
	}
	// let the async auditContract run, updating match status
	newMatchStatus = rig.db.waitForMatchUpdateOrFail(mid, t)
	if newMatchStatus != order.MakerSwapCast {
		t.Fatalf("wrong match status. wanted %v, got %v", order.MakerSwapCast, newMatchStatus)
	}
	if len(proof.SecretHash) == 0 {
		t.Fatalf("secret hash not set for taker")
	}
	if !bytes.Equal(proof.MakerSwap, audit.CoinID) {
		t.Fatalf("maker redeem coin not set")
	}
	rig.db.waitForMatchUpdateOrFail(mid, t) // AuditSig is set in a second match data update
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
	// confirming maker's swap should trigger taker's swap bcast
	tBtcWallet.setConfs(auditInfo.Coin.ID(), tBTC.SwapConf, nil)
	swapID := encode.RandomBytes(36)
	tDcrWallet.swapReceipts = []asset.Receipt{&tReceipt{coin: &tCoin{id: swapID}}}
	rig.ws.queueResponse(msgjson.InitRoute, initAcker)
	tCore.tickAsset(dc, tBTC.ID)
	newMatchStatus = rig.db.waitForMatchUpdateOrFail(mid, t) // MakerSwapCast->TakerSwapCast (after taker's swap bcast)
	if newMatchStatus != order.TakerSwapCast {
		t.Fatalf("wrong match status. wanted %v, got %v", order.TakerSwapCast, newMatchStatus)
	}
	if len(proof.TakerSwap) == 0 {
		t.Fatalf("swap not broadcast with confirmations")
	}
	rig.db.waitForMatchUpdateOrFail(mid, t) // init ack sig is set in a second match data update
	if len(auth.InitSig) == 0 {
		t.Fatalf("init ack sig not set for taker")
	}

	// Receive the maker's redemption.
	redemptionCoin := encode.RandomBytes(36)
	redemption := &msgjson.Redemption{
		Redeem: msgjson.Redeem{
			OrderID: loid[:],
			MatchID: mid[:],
			CoinID:  redemptionCoin,
		},
	}
	sign(tDexPriv, redemption)
	redeemCoin = encode.RandomBytes(36)
	tBtcWallet.redeemCoins = []dex.Bytes{redeemCoin}
	msg, _ = msgjson.NewRequest(1, msgjson.RedemptionRoute, redemption)

	tBtcWallet.badSecret = true
	err = handleRedemptionRoute(tCore, rig.dc, msg)
	if err == nil {
		t.Fatalf("no error for wrong secret")
	}
	newMatchStatus = rig.db.waitForMatchUpdateOrFail(mid, t) // wrong secret still updates match
	if newMatchStatus != order.TakerSwapCast {               // but status is same
		t.Fatalf("wrong match status. wanted %v, got %v", order.TakerSwapCast, newMatchStatus)
	}
	tBtcWallet.badSecret = false

	tBtcWallet.redeemErrChan = make(chan error, 1)
	rig.ws.queueResponse(msgjson.RedeemRoute, redeemAcker)
	err = handleRedemptionRoute(tCore, rig.dc, msg)
	if err != nil {
		t.Fatalf("redemption message error: %v", err)
	}
	err = <-tBtcWallet.redeemErrChan
	if err != nil {
		t.Fatalf("should have worked, got: %v", err)
	}
	// For taker, there's one status update to MakerRedeemed on processRedemption
	newMatchStatus = rig.db.waitForMatchUpdateOrFail(mid, t)
	if newMatchStatus != order.MakerRedeemed {
		t.Fatalf("wrong match status. wanted %v, got %v", order.MakerRedeemed, newMatchStatus)
	}
	// and another status update to MatchComplete when taker's redemption is bcast
	newMatchStatus = rig.db.waitForMatchUpdateOrFail(mid, t)
	if newMatchStatus != order.MatchComplete {
		t.Fatalf("wrong match status. wanted %v, got %v", order.MatchComplete, newMatchStatus)
	}
	if !bytes.Equal(proof.MakerRedeem, redemptionCoin) {
		t.Fatalf("redemption coin ID not logged")
	}
	if len(proof.TakerRedeem) == 0 {
		t.Fatalf("taker redemption not sent")
	}
	// Then a match update to set the redeem ack sig when the 'redeem' request back
	// to the server succeeds.
	rig.db.waitForMatchUpdateOrFail(mid, t)
	if len(auth.RedeemSig) == 0 {
		t.Fatalf("redeem ack sig not set for taker")
	}
	rig.db.setUpdateMatchHook(mid, nil)
	tBtcWallet.redeemErrChan = nil

	// CANCEL ORDER MATCH
	//
	tDcrWallet.returnedCoins = nil
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
		Address:  "testaddr",
	}
	sign(tDexPriv, m1)
	sign(tDexPriv, m2)
	msg, _ = msgjson.NewRequest(1, msgjson.MatchRoute, []*msgjson.Match{m1, m2})
	err = handleMatchRoute(tCore, rig.dc, msg)
	if err != nil {
		t.Fatalf("handleMatchRoute error (cancel with swaps): %v", err)
	}
	if tracker.cancel.matches.maker == nil {
		t.Fatalf("cancelMatches.maker not set")
	}
	if tracker.Trade().Filled() != qty {
		t.Fatalf("fill not set. %d != %d", tracker.Trade().Filled(), qty)
	}
	if tracker.cancel.matches.taker == nil {
		t.Fatalf("cancelMatches.taker not set")
	}
	// Since there are no unswapped orders, the change coin should be returned.
	if len(tDcrWallet.returnedCoins) != 1 || !bytes.Equal(tDcrWallet.returnedCoins[0].ID(), tDcrWallet.changeCoin.id) {
		t.Fatalf("change coin not returned")
	}

	resetMatches := func() {
		tracker.matches = make(map[order.MatchID]*matchTracker)
		tracker.change = nil
		tracker.metaData.ChangeCoin = nil
		tracker.coinsLocked = true
	}

	// If there is no change coin and no matches, the funding coin should be
	// returned instead.
	resetMatches()
	// The change coins would also have been added to the coins map, so delete
	// that too.
	delete(tracker.coins, tDcrWallet.changeCoin.String())
	err = handleMatchRoute(tCore, rig.dc, msg)
	if err != nil {
		t.Fatalf("handleMatchRoute error (cancel without swaps): %v", err)
	}
	if len(tDcrWallet.returnedCoins) != 1 || !bytes.Equal(tDcrWallet.returnedCoins[0].ID(), fundCoinDcrID) {
		t.Fatalf("change coin not returned (cancel without swaps)")
	}

	// If the order is an immediate order, the asset.Swaps.LockChange should be
	// false regardless of whether the order is filled.
	resetMatches()
	tracker.cancel = nil
	rig.ws.queueResponse(msgjson.InitRoute, initAcker)
	msgMatch.Side = uint8(order.Maker)
	sign(tDexPriv, msgMatch)
	msg, _ = msgjson.NewRequest(1, msgjson.MatchRoute, []*msgjson.Match{msgMatch})

	tracker.metaData.Status = order.OrderStatusEpoch
	lo.Force = order.ImmediateTiF
	err = handleMatchRoute(tCore, rig.dc, msg)
	if err != nil {
		t.Fatalf("handleMatchRoute error (immediate partial fill): %v", err)
	}
	if tDcrWallet.lastSwaps.LockChange != false {
		t.Fatalf("change locked for executed non-standing order (immediate partial fill)")
	}
}

func TestReconcileTrades(t *testing.T) {
	rig := newTestRig()
	defer rig.shutdown()
	dc := rig.dc

	mkt := dc.marketConfig(tDcrBtcMktName)
	rig.core.wallets[mkt.Base], _ = newTWallet(mkt.Base)
	rig.core.wallets[mkt.Quote], _ = newTWallet(mkt.Quote)
	walletSet, err := rig.core.walletSet(dc, mkt.Base, mkt.Quote, true)
	if err != nil {
		t.Fatalf("walletSet error: %v", err)
	}

	type orderSet struct {
		epoch               *trackedTrade
		booked              *trackedTrade // standing limit orders only
		bookedPendingCancel *trackedTrade // standing limit orders only
		executed            *trackedTrade
	}
	makeOrderSet := func(force order.TimeInForce) *orderSet {
		orders := &orderSet{
			epoch:    makeTradeTracker(rig, mkt, walletSet, force, order.OrderStatusEpoch),
			executed: makeTradeTracker(rig, mkt, walletSet, force, order.OrderStatusExecuted),
		}
		if force == order.StandingTiF {
			orders.booked = makeTradeTracker(rig, mkt, walletSet, force, order.OrderStatusBooked)
			orders.bookedPendingCancel = makeTradeTracker(rig, mkt, walletSet, force, order.OrderStatusBooked)
			orders.bookedPendingCancel.cancel = &trackedCancel{
				CancelOrder: order.CancelOrder{
					P: order.Prefix{
						ServerTime: time.Now().UTC().Add(-16 * time.Minute),
					},
				},
			}
		}
		return orders
	}

	standingOrders := makeOrderSet(order.StandingTiF)
	immediateOrders := makeOrderSet(order.ImmediateTiF)

	tests := []struct {
		name                string
		clientOrders        []*trackedTrade        // orders known to the client
		serverOrders        []*msgjson.OrderStatus // orders considered active by the server
		orderStatusRes      []*msgjson.OrderStatus // server's response to order_status requests
		expectOrderStatuses map[order.OrderID]order.OrderStatus
	}{
		{
			name:         "server orders unknown to client",
			clientOrders: []*trackedTrade{},
			serverOrders: []*msgjson.OrderStatus{
				{
					ID:     test.RandomOrderID().Bytes(),
					Status: uint16(order.OrderStatusBooked),
				},
			},
			expectOrderStatuses: map[order.OrderID]order.OrderStatus{},
		},
		{
			name: "different server-client statuses",
			clientOrders: []*trackedTrade{
				standingOrders.epoch,
				standingOrders.booked,
				standingOrders.bookedPendingCancel,
				immediateOrders.epoch,
				immediateOrders.executed,
			},
			serverOrders: []*msgjson.OrderStatus{
				{
					ID:     standingOrders.epoch.ID().Bytes(),
					Status: uint16(order.OrderStatusBooked), // now booked
				},
				{
					ID:     standingOrders.booked.ID().Bytes(),
					Status: uint16(order.OrderStatusEpoch), // invald! booked orders cannot return to epoch!
				},
				{
					ID:     standingOrders.bookedPendingCancel.ID().Bytes(),
					Status: uint16(order.OrderStatusBooked), // still booked, cancel order should be deleted
				},
				{
					ID:     immediateOrders.epoch.ID().Bytes(),
					Status: uint16(order.OrderStatusBooked), // invalid, immediate orders cannot be booked!
				},
				{
					ID:     immediateOrders.executed.ID().Bytes(),
					Status: uint16(order.OrderStatusBooked), // invalid, inactive orders should not be returned by DEX!
				},
			},
			expectOrderStatuses: map[order.OrderID]order.OrderStatus{
				standingOrders.epoch.ID():               order.OrderStatusBooked,   // epoch => booked
				standingOrders.booked.ID():              order.OrderStatusBooked,   // should not change, cannot return to epoch
				standingOrders.bookedPendingCancel.ID(): order.OrderStatusBooked,   // no status change
				immediateOrders.epoch.ID():              order.OrderStatusEpoch,    // should not change, cannot be booked
				immediateOrders.executed.ID():           order.OrderStatusExecuted, // should not change, inactive cannot become active
			},
		},
		{
			name: "active becomes inactive",
			clientOrders: []*trackedTrade{
				standingOrders.epoch,
				standingOrders.booked,
				standingOrders.bookedPendingCancel,
				standingOrders.executed,
				immediateOrders.epoch,
				immediateOrders.executed,
			},
			serverOrders: []*msgjson.OrderStatus{}, // no active order reported by server
			orderStatusRes: []*msgjson.OrderStatus{
				{
					ID:     standingOrders.epoch.ID().Bytes(),
					Status: uint16(order.OrderStatusRevoked),
				},
				{
					ID:     standingOrders.booked.ID().Bytes(),
					Status: uint16(order.OrderStatusRevoked),
				},
				{
					ID:     standingOrders.bookedPendingCancel.ID().Bytes(),
					Status: uint16(order.OrderStatusCanceled),
				},
				{
					ID:     immediateOrders.epoch.ID().Bytes(),
					Status: uint16(order.OrderStatusExecuted),
				},
			},
			expectOrderStatuses: map[order.OrderID]order.OrderStatus{
				standingOrders.epoch.ID():               order.OrderStatusRevoked,  // preimage missed = revoked
				standingOrders.booked.ID():              order.OrderStatusRevoked,  // booked, not canceled = assume revoked (may actually be executed)
				standingOrders.bookedPendingCancel.ID(): order.OrderStatusCanceled, // booked pending canceled = assume canceled (may actually be revoked or executed)
				standingOrders.executed.ID():            order.OrderStatusExecuted, // should not change
				immediateOrders.epoch.ID():              order.OrderStatusExecuted, // preimage sent, not canceled = executed
				immediateOrders.executed.ID():           order.OrderStatusExecuted, // should not change
			},
		},
	}

	for _, tt := range tests {
		// Track client orders in dc.trades.
		dc.tradeMtx.Lock()
		var pendingCancel *trackedTrade
		dc.trades = make(map[order.OrderID]*trackedTrade)
		for _, tracker := range tt.clientOrders {
			dc.trades[tracker.ID()] = tracker
			if tracker.cancel != nil {
				pendingCancel = tracker
			}
		}
		dc.tradeMtx.Unlock()

		// Queue order_status response if required for reconciliation.
		if len(tt.orderStatusRes) > 0 {
			rig.ws.queueResponse(msgjson.OrderStatusRoute, func(msg *msgjson.Message, f msgFunc) error {
				resp, _ := msgjson.NewResponse(msg.ID, tt.orderStatusRes, nil)
				f(resp)
				return nil
			})
		}

		// Reconcile tracked orders with server orders.
		dc.reconcileTrades(tt.serverOrders)

		dc.tradeMtx.RLock()
		if len(dc.trades) != len(tt.expectOrderStatuses) {
			t.Fatalf("%s: post-reconcileTrades order count mismatch. expected %d, got %d",
				tt.name, len(tt.expectOrderStatuses), len(dc.trades))
		}
		for oid, tracker := range dc.trades {
			expectedStatus, expected := tt.expectOrderStatuses[oid]
			if !expected {
				t.Fatalf("%s: unexpected order %v tracked by client", tt.name, oid)
			}
			tracker.mtx.RLock()
			if tracker.metaData.Status != expectedStatus {
				t.Fatalf("%s: client reported wrong order status %v, expected %v",
					tt.name, tracker.metaData.Status, expectedStatus)
			}
			tracker.mtx.RUnlock()
		}
		dc.tradeMtx.RUnlock()

		// Check if a previously canceled order existed; if the order is still
		// active (Epoch/Booked status) and the cancel order is deleted, having
		// been there for over 15 minutes since the cancel order's epoch ended.
		if pendingCancel != nil {
			pendingCancel.mtx.RLock()
			status, stillHasCancelOrder := pendingCancel.metaData.Status, pendingCancel.cancel != nil
			pendingCancel.mtx.RUnlock()
			if status == order.OrderStatusBooked {
				if stillHasCancelOrder {
					t.Fatalf("%s: expected stale cancel order to be deleted for now-booked order", tt.name)
				}
				// Cancel order deleted. Canceling the order again should succeed.
				rig.queueCancel(nil)
				err = rig.core.Cancel(tPW, pendingCancel.ID().Bytes())
				if err != nil {
					t.Fatalf("cancel order error after deleting previous stale cancel: %v", err)
				}
			}
		}
	}
}

func makeTradeTracker(rig *testRig, mkt *msgjson.Market, walletSet *walletSet, force order.TimeInForce, status order.OrderStatus) *trackedTrade {
	qty := 4 * tDCR.LotSize
	lo, dbOrder, preImg, _ := makeLimitOrder(rig.dc, true, qty, tBTC.RateStep)
	lo.Force = force
	dbOrder.MetaData.Status = status

	tracker := newTrackedTrade(dbOrder, preImg, rig.dc, mkt.EpochLen,
		rig.core.lockTimeTaker, rig.core.lockTimeMaker,
		rig.db, rig.queue, walletSet, nil, rig.core.notify)

	return tracker
}

func TestRefunds(t *testing.T) {
	rig := newTestRig()
	defer rig.shutdown()
	checkStatus := func(tag string, match *matchTracker, wantStatus order.MatchStatus) {
		t.Helper()
		if match.Status != wantStatus {
			t.Fatalf("%s: wrong status wanted %v, got %v", tag, wantStatus, match.Status)
		}
	}
	checkRefund := func(tracker *trackedTrade, match *matchTracker, expectAmt uint64) {
		t.Helper()
		// Confirm that the status is SwapCast.
		if match.Side == order.Maker {
			checkStatus("maker swapped", match, order.MakerSwapCast)
		} else {
			checkStatus("taker swapped", match, order.TakerSwapCast)
		}
		// Confirm isRefundable = true.
		if !tracker.isRefundable(match) {
			t.Fatalf("%s's swap not refundable", match.Side)
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
			t.Fatalf("%s's swap refundable after being refunded", match.Side)
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
		if match.Side == order.Maker {
			checkStatus("maker swapped", match, order.MakerSwapCast)
		} else {
			checkStatus("taker swapped", match, order.TakerSwapCast)
		}
	}

	dc := rig.dc
	tCore := rig.core
	dcrWallet, tDcrWallet := newTWallet(tDCR.ID)
	tCore.wallets[tDCR.ID] = dcrWallet
	dcrWallet.address = "DsVmA7aqqWeKWy461hXjytbZbgCqbB8g2dq"
	dcrWallet.Unlock(rig.crypter)

	btcWallet, tBtcWallet := newTWallet(tBTC.ID)
	tCore.wallets[tBTC.ID] = btcWallet
	btcWallet.address = "12DXGkvxFjuq5btXYkwWfBZaz1rVwFgini"
	btcWallet.Unlock(rig.crypter)

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
	mkt := dc.marketConfig(tDcrBtcMktName)
	fundCoinsDCR := asset.Coins{&tCoin{id: encode.RandomBytes(36)}}
	tDcrWallet.fundingCoins = fundCoinsDCR
	tDcrWallet.fundRedeemScripts = []dex.Bytes{nil}
	tracker := newTrackedTrade(dbOrder, preImgL, dc, mkt.EpochLen, rig.core.lockTimeTaker, rig.core.lockTimeMaker,
		rig.db, rig.queue, walletSet, fundCoinsDCR, rig.core.notify)
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
	tDcrWallet.swapReceipts = []asset.Receipt{&tReceipt{coin: &tCoin{id: swapID}, contract: contract}}
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
	auditInfo.Expiration = encode.DropMilliseconds(matchTime.Add(tracker.lockTimeMaker))

	// Check audit errors.
	tBtcWallet.auditErr = tErr
	err = tracker.auditContract(match, audit.CoinID, audit.Contract, nil)
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
	// Reset funding coins in the trackedTrade, wipe change coin.
	tracker.mtx.Lock()
	tracker.coins = mapifyCoins(fundCoinsDCR)
	tracker.coinsLocked = true
	tracker.changeLocked = false
	tracker.change = nil
	tracker.metaData.ChangeCoin = nil
	tracker.mtx.Unlock()
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
	rig.db.setUpdateMatchHook(mid, make(chan order.MatchStatus, 1))
	audit, auditInfo = tMsgAudit(loid, mid, addr, matchSize, nil)
	tBtcWallet.auditInfo = auditInfo
	auditInfo.Expiration = encode.DropMilliseconds(matchTime.Add(tracker.lockTimeMaker))
	tBtcWallet.auditErr = nil
	msg, _ = msgjson.NewRequest(1, msgjson.AuditRoute, audit)
	err = handleAuditRoute(tCore, rig.dc, msg)
	if err != nil {
		t.Fatalf("taker's match message error: %v", err)
	}
	// let the async auditContract run, updating match status
	newMatchStatus := rig.db.waitForMatchUpdateOrFail(mid, t)
	if newMatchStatus != order.MakerSwapCast {
		t.Fatalf("wrong match status. wanted %v, got %v", order.MakerSwapCast, newMatchStatus)
	}
	rig.db.waitForMatchUpdateOrFail(mid, t) // AuditSig set in second update to match data
	tracker.mtx.RLock()
	if !bytes.Equal(match.MetaData.Proof.Auth.AuditSig, audit.Sig) {
		t.Fatalf("audit sig not set for taker")
	}
	tracker.mtx.RUnlock()
	// maker's swap confirmation should trigger taker's swap bcast
	tBtcWallet.setConfs(auditInfo.Coin.ID(), tBTC.SwapConf, nil)
	counterSwapID := encode.RandomBytes(36)
	counterScript := encode.RandomBytes(36)
	tDcrWallet.swapReceipts = []asset.Receipt{&tReceipt{coin: &tCoin{id: counterSwapID}, contract: counterScript}}
	rig.ws.queueResponse(msgjson.InitRoute, initAcker)
	tCore.tickAsset(dc, tBTC.ID)
	newMatchStatus = rig.db.waitForMatchUpdateOrFail(mid, t) // MakerSwapCast->TakerSwapCast (after taker's swap bcast)
	if newMatchStatus != order.TakerSwapCast {
		t.Fatalf("wrong match status. wanted %v, got %v", order.TakerSwapCast, newMatchStatus)
	}
	tracker.mtx.RLock()
	if !bytes.Equal(match.MetaData.Proof.Script, counterScript) {
		t.Fatalf("invalid contract recorded for Taker swap")
	}
	tracker.mtx.RUnlock()

	// Attempt refund.
	rig.db.setUpdateMatchHook(mid, nil)
	checkRefund(tracker, match, matchSize)
}

func TestNotifications(t *testing.T) {
	rig := newTestRig()
	defer rig.shutdown()

	// Insert a notification into the database.
	typedNote := newOrderNote("abc", "def", 100, nil)

	tCore := rig.core
	ch := tCore.NotificationFeed()
	tCore.notify(typedNote)
	select {
	case n := <-ch:
		dbtest.MustCompareNotifications(t, n.DBNote(), &typedNote.Notification)
	case <-time.After(time.Second):
		t.Fatalf("no notification received over the notification channel")
	}
}

func TestResolveActiveTrades(t *testing.T) {
	rig := newTestRig()
	defer rig.shutdown()
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
		Rate:  rate,
		Force: order.StandingTiF, // we're calling it booked in OrderMetaData
	}

	oid := lo.ID()
	changeCoinID := encode.RandomBytes(36)
	changeCoin := &tCoin{id: changeCoinID}

	dbOrder := &db.MetaOrder{
		MetaData: &db.OrderMetaData{
			Status:     order.OrderStatusBooked,
			Host:       tDexHost,
			Proof:      db.OrderProof{},
			ChangeCoin: changeCoinID,
		},
		Order: lo,
	}

	// Need to return an order from db.ActiveDEXOrders
	rig.db.activeDEXOrders = []*db.MetaOrder{dbOrder}
	rig.db.orderOrders[oid] = dbOrder

	mid := ordertest.RandomMatchID()
	addr := ordertest.RandomAddress()
	match := &db.MetaMatch{
		MetaData: &db.MatchMetaData{
			Proof: db.MatchProof{
				CounterContract: encode.RandomBytes(50),
				SecretHash:      encode.RandomBytes(32),
				MakerSwap:       encode.RandomBytes(32),
				Auth: db.MatchAuth{
					MatchSig: encode.RandomBytes(32),
				},
			},
			DEX:   tDexHost,
			Base:  tDCR.ID,
			Quote: tBTC.ID,
		},
		UserMatch: &order.UserMatch{
			OrderID:  oid,
			MatchID:  mid,
			Quantity: qty - tDCR.LotSize,
			Rate:     rate,
			Address:  addr,
			Status:   order.MakerSwapCast,
			Side:     order.Taker,
		},
	}
	rig.db.activeMatchOIDs = []order.OrderID{oid}
	rig.db.matchesForOID = []*db.MetaMatch{match}
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
	ensureGood := func(tag string, expCoinsLoaded int) {
		t.Helper()
		description := fmt.Sprintf("%s: side = %s, order status = %s, match status = %s",
			tag, match.Side, dbOrder.MetaData.Status, match.Status)
		_, err := tCore.Login(tPW)
		if err != nil {
			t.Fatalf("%s: login error: %v", description, err)
		}

		if !btcWallet.unlocked() {
			t.Fatalf("%s: btc wallet not unlocked", description)
		}

		if !dcrWallet.unlocked() {
			t.Fatalf("%s: dcr wallet not unlocked", description)
		}

		trade, found := rig.dc.trades[oid]
		if !found {
			t.Fatalf("%s: trade with expected order id not found. len(trades) = %d", description, len(rig.dc.trades))
		}

		_, found = trade.matches[mid]
		if !found {
			t.Fatalf("%s: trade with expected order id not found. len(matches) = %d", description, len(trade.matches))
		}

		if len(trade.coins) != expCoinsLoaded {
			t.Fatalf("%s: expected %d coin loaded, got %d", description, expCoinsLoaded, len(trade.coins))
		}

		reset()
	}

	ensureGood("initial", 1)

	// Ensure a failure AND reset. err != nil just helps to make sure that we're
	// hitting errors in resolveActiveTrades, which sends errors as
	// notifications, vs somewhere else.
	ensureFail := func(tag string) {
		t.Helper()
		_, err := tCore.Login(tPW)
		if err != nil || len(rig.dc.trades) != 0 {
			t.Fatalf("%s: no error. err = %v, len(trades) = %d", tag, err, len(rig.dc.trades))
		}
		reset()
	}

	// NEGATIVE PATHS

	// No base wallet
	delete(tCore.wallets, tDCR.ID)
	ensureFail("missing base")
	tCore.wallets[tDCR.ID] = dcrWallet

	// Base wallet unlock errors
	tDcrWallet.unlockErr = tErr
	tDcrWallet.locked = true
	ensureFail("base unlock")
	tDcrWallet.unlockErr = nil
	tDcrWallet.locked = false

	// No quote wallet
	delete(tCore.wallets, tBTC.ID)
	ensureFail("missing quote")
	tCore.wallets[tBTC.ID] = btcWallet

	// Quote wallet unlock errors
	tBtcWallet.unlockErr = tErr
	tBtcWallet.locked = true
	ensureFail("quote unlock")
	tBtcWallet.unlockErr = nil
	tBtcWallet.locked = false

	// Funding coin error still puts it in the trades map, just with no coins
	// locked.
	tDcrWallet.fundingCoinErr = tErr
	ensureGood("funding coin", 0)
	tDcrWallet.fundingCoinErr = nil

	// No matches
	rig.db.activeMatchOIDSErr = tErr
	ensureFail("matches error")
	rig.db.activeMatchOIDSErr = nil

	// POSITIVE PATHS

	activeStatuses := []order.OrderStatus{order.OrderStatusEpoch, order.OrderStatusBooked}
	inactiveStatuses := []order.OrderStatus{order.OrderStatusExecuted, order.OrderStatusCanceled, order.OrderStatusRevoked}

	tests := []struct {
		name          string
		side          []order.MatchSide
		orderStatuses []order.OrderStatus
		matchStatuses []order.MatchStatus
		expectedCoins int
	}{
		// With an active order, the change coin should always be loaded.
		{
			name:          "active-order",
			side:          []order.MatchSide{order.Taker, order.Maker},
			orderStatuses: activeStatuses,
			matchStatuses: []order.MatchStatus{order.NewlyMatched, order.MakerSwapCast,
				order.TakerSwapCast, order.MakerRedeemed, order.MatchComplete},
			expectedCoins: 1,
		},
		// With an inactive order, as taker, if match is >= TakerSwapCast, there
		// will be no funding coin fetched.
		{
			name:          "inactive taker > MakerSwapCast",
			side:          []order.MatchSide{order.Taker},
			orderStatuses: inactiveStatuses,
			matchStatuses: []order.MatchStatus{order.TakerSwapCast, order.MakerRedeemed,
				order.MatchComplete},
			expectedCoins: 0,
		},
		// But there will be for NewlyMatched && MakerSwapCast
		{
			name:          "inactive taker < TakerSwapCast",
			side:          []order.MatchSide{order.Taker},
			orderStatuses: inactiveStatuses,
			matchStatuses: []order.MatchStatus{order.NewlyMatched, order.MakerSwapCast},
			expectedCoins: 1,
		},
		// For a maker with an inactive order, only NewlyMatched would
		// necessitate fetching of coins.
		{
			name:          "inactive maker NewlyMatched",
			side:          []order.MatchSide{order.Maker},
			orderStatuses: inactiveStatuses,
			matchStatuses: []order.MatchStatus{order.NewlyMatched},
			expectedCoins: 1,
		},
		{
			name:          "inactive maker > NewlyMatched",
			side:          []order.MatchSide{order.Maker},
			orderStatuses: inactiveStatuses,
			matchStatuses: []order.MatchStatus{order.MakerSwapCast, order.TakerSwapCast,
				order.MakerRedeemed, order.MatchComplete},
			expectedCoins: 0,
		},
	}

	for _, tt := range tests {
		for _, side := range tt.side {
			match.Side = side
			for _, orderStatus := range tt.orderStatuses {
				dbOrder.MetaData.Status = orderStatus
				for _, matchStatus := range tt.matchStatuses {
					match.Status = matchStatus
					ensureGood(tt.name, tt.expectedCoins)
				}
			}
		}
	}
}

func TestCompareServerMatches(t *testing.T) {
	rig := newTestRig()
	defer rig.shutdown()
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
	mkt := dc.marketConfig(tDcrBtcMktName)
	tracker := newTrackedTrade(dbOrder, preImg, dc, mkt.EpochLen, rig.core.lockTimeTaker, rig.core.lockTimeMaker,
		rig.db, rig.queue, nil, nil, nil)

	// Known trade, and known match
	knownID := ordertest.RandomMatchID()
	knownMatch := &matchTracker{
		MetaMatch: db.MetaMatch{
			UserMatch: &order.UserMatch{MatchID: knownID},
			MetaData:  &db.MatchMetaData{},
		},
		counterConfirms: -1,
	}
	tracker.matches[knownID] = knownMatch
	knownMsgMatch := &msgjson.Match{OrderID: oid[:], MatchID: knownID[:]}

	// Known trade, but missing match
	missingID := ordertest.RandomMatchID()
	missingMatch := &matchTracker{
		MetaMatch: db.MetaMatch{
			UserMatch: &order.UserMatch{MatchID: missingID},
			MetaData:  &db.MatchMetaData{},
		},
	}
	tracker.matches[missingID] = missingMatch

	// extra match
	extraID := ordertest.RandomMatchID()
	extraMsgMatch := &msgjson.Match{OrderID: oid[:], MatchID: extraID[:]}

	// Entirely missing order
	loMissing, dbOrderMissing, preImgMissing, _ := makeLimitOrder(dc, true, 3*tDCR.LotSize, tBTC.RateStep*10)
	trackerMissing := newTrackedTrade(dbOrderMissing, preImgMissing, dc, mkt.EpochLen, rig.core.lockTimeTaker, rig.core.lockTimeMaker,
		rig.db, rig.queue, nil, nil, notify)
	oidMissing := loMissing.ID()
	// an active match for the missing trade
	matchIDMissing := ordertest.RandomMatchID()
	missingTradeMatch := &matchTracker{
		MetaMatch: db.MetaMatch{
			UserMatch: &order.UserMatch{MatchID: matchIDMissing},
			MetaData:  &db.MatchMetaData{},
		},
		counterConfirms: -1,
	}
	trackerMissing.matches[knownID] = missingTradeMatch
	// an inactive match for the missing trade
	matchIDMissingInactive := ordertest.RandomMatchID()
	missingTradeMatchInactive := &matchTracker{
		MetaMatch: db.MetaMatch{
			UserMatch: &order.UserMatch{
				MatchID: matchIDMissingInactive,
				Status:  order.MatchComplete,
			},
			MetaData: &db.MatchMetaData{
				Proof: db.MatchProof{
					Auth: db.MatchAuth{
						RedeemSig: []byte{1, 2, 3}, // won't be considered complete with out it
					},
				},
			},
		},
		counterConfirms: 1,
	}
	trackerMissing.matches[matchIDMissingInactive] = missingTradeMatchInactive

	srvMatches := map[order.OrderID]*serverMatches{
		oid: {
			tracker:    tracker,
			msgMatches: []*msgjson.Match{knownMsgMatch, extraMsgMatch},
		},
		// oidMissing not included (missing!)
	}

	dc.trades = map[order.OrderID]*trackedTrade{
		oid:        tracker,
		oidMissing: trackerMissing,
	}

	exceptions, _ := dc.compareServerMatches(srvMatches)
	if len(exceptions) != 2 {
		t.Fatalf("exceptions did not include both trades, just %d", len(exceptions))
	}

	exc, ok := exceptions[oid]
	if !ok {
		t.Fatalf("exceptions did not include trade %v", oid)
	}
	if exc.trade.ID() != oid {
		t.Fatalf("wrong trade ID, got %v, want %v", exc.trade.ID(), oid)
	}
	if len(exc.missing) != 1 {
		t.Fatalf("found %d missing matches for trade %v, expected 1", len(exc.missing), oid)
	}
	if exc.missing[0].MatchID != missingID {
		t.Fatalf("wrong missing match, got %v, expected %v", exc.missing[0].MatchID, missingID)
	}
	if len(exc.extra) != 1 {
		t.Fatalf("found %d extra matches for trade %v, expected 1", len(exc.extra), oid)
	}
	if !bytes.Equal(exc.extra[0].MatchID, extraID[:]) {
		t.Fatalf("wrong extra match, got %v, expected %v", exc.extra[0].MatchID, extraID)
	}

	exc, ok = exceptions[oidMissing]
	if !ok {
		t.Fatalf("exceptions did not include trade %v", oidMissing)
	}
	if exc.trade.ID() != oidMissing {
		t.Fatalf("wrong trade ID, got %v, want %v", exc.trade.ID(), oidMissing)
	}
	if len(exc.missing) != 1 { // no matchIDMissingInactive
		t.Fatalf("found %d missing matches for trade %v, expected 1", len(exc.missing), oid)
	}
	if exc.missing[0].MatchID != matchIDMissing {
		t.Fatalf("wrong missing match, got %v, expected %v", exc.missing[0].MatchID, matchIDMissing)
	}
	if len(exc.extra) != 0 {
		t.Fatalf("found %d extra matches for trade %v, expected 0", len(exc.extra), oid)
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

func tMsgAudit(oid order.OrderID, mid order.MatchID, recipient string, val uint64, secretHash []byte) (*msgjson.Audit, *asset.AuditInfo) {
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
	auditInfo := &asset.AuditInfo{
		Recipient:  recipient,
		Coin:       auditCoin,
		Contract:   auditContract,
		SecretHash: secretHash,
	}
	return audit, auditInfo
}

func TestHandleEpochOrderMsg(t *testing.T) {
	rig := newTestRig()
	defer rig.shutdown()
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

	rig.dc.books[tDcrBtcMktName] = newBookie(tLogger, func() {})

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
	defer rig.shutdown()
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

	rig.dc.books[tDcrBtcMktName] = newBookie(tLogger, func() {})

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
	defer rig.shutdown()
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

	ensureErr := func(tag string) {
		t.Helper()
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
			MetaData: &db.MatchMetaData{},
			UserMatch: &order.UserMatch{
				OrderID: ord.ID(),
				MatchID: mid,
				Status:  order.NewlyMatched,
				Side:    order.Maker,
			},
		},
		prefix:          ord.Prefix(),
		trade:           ord.Trade(),
		counterConfirms: -1,
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
	defer rig.shutdown()
	dc := rig.dc
	dc.books[tDcrBtcMktName] = newBookie(tLogger, func() {})

	mktEpoch := func() uint64 {
		dc.epochMtx.RLock()
		defer dc.epochMtx.RUnlock()
		return dc.epoch[tDcrBtcMktName]
	}

	payload := &msgjson.MatchProofNote{
		MarketID: tDcrBtcMktName,
		Epoch:    1,
	}
	req, _ := msgjson.NewRequest(rig.dc.NextID(), msgjson.MatchProofRoute, payload)
	err := handleMatchProofMsg(rig.core, rig.dc, req)
	if err != nil {
		t.Fatalf("error advancing epoch: %v", err)
	}
	if mktEpoch() != 2 {
		t.Fatalf("expected epoch 1, got %d", mktEpoch())
	}

	payload.Epoch = 0
	req, _ = msgjson.NewRequest(rig.dc.NextID(), msgjson.MatchProofRoute, payload)
	err = handleMatchProofMsg(rig.core, rig.dc, req)
	if err != nil {
		t.Fatalf("error handling match proof: %v", err)
	}
	if mktEpoch() != 2 {
		t.Fatalf("epoch decremented")
	}
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
			// Coins needed?
			Sell:     sell,
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
			MaxFeeRate: tMaxFeeRate,
		},
		Order: lo,
	}
	return lo, dbOrder, preImg, addr
}

func TestAddrHost(t *testing.T) {
	tests := []struct {
		name, addr, want string
		wantErr          bool
	}{{
		name: "scheme, host, and port",
		addr: "https://localhost:5758",
		want: "localhost:5758",
	}, {
		name: "scheme, ipv6 host, and port",
		addr: "https://[::1]:5758",
		want: "[::1]:5758",
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
		want: "thatonedex.com:7232",
	}, {
		name: "scheme and host",
		addr: "https://thatonedex.com",
		want: "thatonedex.com:7232",
	}, {
		name: "scheme, host, and path",
		addr: "https://thatonedex.com/any/path",
		want: "thatonedex.com:7232",
	}, {
		name: "ipv6 host",
		addr: "[1:2::]",
		want: "[1:2::]:7232",
	}, {
		name: "ipv6 host and port",
		addr: "[1:2::]:5758",
		want: "[1:2::]:5758",
	}, {
		name: "empty address",
		want: "localhost:7232",
	}, {
		name:    "invalid host",
		addr:    "https://\n:1234",
		wantErr: true,
	}, {
		name:    "invalid port",
		addr:    ":asdf",
		wantErr: true,
	}}
	for _, test := range tests {
		res, err := addrHost(test.addr)
		if res != test.want {
			t.Fatalf("wanted %s but got %s for test '%s'", test.want, res, test.name)
		}
		if test.wantErr {
			if err == nil {
				t.Fatalf("wanted error for test %s, but got none", test.name)
			}
			continue
		} else if err != nil {
			t.Fatalf("addrHost error for test %s: %v", test.name, err)
		}
		// Parsing results a second time should produce the same results.
		res, _ = addrHost(res)
		if res != test.want {
			t.Fatalf("wanted %s but got %s for test '%s'", test.want, res, test.name)
		}
	}
}

func TestAssetBalance(t *testing.T) {
	rig := newTestRig()
	defer rig.shutdown()
	tCore := rig.core

	wallet, tWallet := newTWallet(tDCR.ID)
	tCore.wallets[tDCR.ID] = wallet
	bal := &asset.Balance{
		Available: 4e7,
		Immature:  6e7,
		Locked:    2e8,
	}
	tWallet.bal = bal
	walletBal, err := tCore.AssetBalance(tDCR.ID)
	if err != nil {
		t.Fatalf("error retreiving asset balance: %v", err)
	}
	dbtest.MustCompareAssetBalances(t, "zero-conf", bal, &walletBal.Balance.Balance)
	if walletBal.ContractLocked != 0 {
		t.Fatalf("contractlocked balance %d > expected value 0", walletBal.ContractLocked)
	}
}

func TestAssetCounter(t *testing.T) {
	assets := make(assetMap)
	assets.count(1)
	if len(assets) != 1 {
		t.Fatalf("count not added")
	}

	newCounts := assetMap{
		1: struct{}{},
		2: struct{}{},
	}
	assets.merge(newCounts)
	if len(assets) != 2 {
		t.Fatalf("counts not absorbed properly")
	}
}

func TestHandleTradeSuspensionMsg(t *testing.T) {
	rig := newTestRig()
	defer rig.shutdown()

	tCore := rig.core
	dc := rig.dc
	dcrWallet, tDcrWallet := newTWallet(tDCR.ID)
	tCore.wallets[tDCR.ID] = dcrWallet
	dcrWallet.Unlock(rig.crypter)

	btcWallet, _ := newTWallet(tBTC.ID)
	tCore.wallets[tBTC.ID] = btcWallet
	btcWallet.Unlock(rig.crypter)

	mkt := dc.marketConfig(tDcrBtcMktName)
	walletSet, _ := tCore.walletSet(dc, tDCR.ID, tBTC.ID, true)

	rig.dc.books[tDcrBtcMktName] = newBookie(tLogger, func() {})

	addTracker := func(coins asset.Coins) *trackedTrade {
		lo, dbOrder, preImg, _ := makeLimitOrder(dc, true, 0, 0)
		oid := lo.ID()
		tracker := newTrackedTrade(dbOrder, preImg, dc, mkt.EpochLen, rig.core.lockTimeTaker, rig.core.lockTimeMaker,
			rig.db, rig.queue, walletSet, coins, rig.core.notify)
		dc.trades[oid] = tracker
		return tracker
	}

	// Make a trade that has a single funding coin, no change coin, and no
	// active matches.
	fundCoinDcrID := encode.RandomBytes(36)
	freshTracker := addTracker(asset.Coins{&tCoin{id: fundCoinDcrID}})
	freshTracker.metaData.Status = order.OrderStatusBooked // suspend with purge only purges book orders since epoch orders are always processed first

	// Ensure a non-existent market cannot be suspended.
	payload := &msgjson.TradeSuspension{
		MarketID: "dcr_dcr",
	}

	req, _ := msgjson.NewRequest(rig.dc.NextID(), msgjson.SuspensionRoute, payload)
	err := handleTradeSuspensionMsg(rig.core, rig.dc, req)
	if err == nil {
		t.Fatal("[handleTradeSuspensionMsg] expected a market ID not found error")
	}

	newPayload := func() *msgjson.TradeSuspension {
		return &msgjson.TradeSuspension{
			MarketID:    tDcrBtcMktName,
			FinalEpoch:  100,
			SuspendTime: encode.UnixMilliU(time.Now().Add(time.Millisecond * 20)),
			Persist:     false, // Make sure the coins are returned.
		}
	}

	// Suspend a running market.
	rig.dc.cfgMtx.Lock()
	mktConf := rig.dc.findMarketConfig(tDcrBtcMktName)
	mktConf.StartEpoch = 12
	rig.dc.cfgMtx.Unlock()

	payload = newPayload()
	payload.SuspendTime = 0 // now

	orderNotes, feedDone := orderNoteFeed(tCore)
	defer feedDone()

	req, _ = msgjson.NewRequest(rig.dc.NextID(), msgjson.SuspensionRoute, payload)
	err = handleTradeSuspensionMsg(rig.core, rig.dc, req)
	if err != nil {
		t.Fatalf("[handleTradeSuspensionMsg] unexpected error: %v", err)
	}

	verifyRevokeNotification(orderNotes, SubjectOrderAutoRevoked, t)

	// Check that the funding coin was returned. Use the tradeMtx for
	// synchronization.
	dc.tradeMtx.Lock()
	if len(tDcrWallet.returnedCoins) != 1 || !bytes.Equal(tDcrWallet.returnedCoins[0].ID(), fundCoinDcrID) {
		t.Fatalf("funding coin not returned")
	}
	dc.tradeMtx.Unlock()

	// Make sure the change coin is returned for a trade with a change coin.
	delete(dc.trades, freshTracker.ID())
	swappedTracker := addTracker(nil)
	changeCoinID := encode.RandomBytes(36)
	swappedTracker.change = &tCoin{id: changeCoinID}
	swappedTracker.changeLocked = true
	swappedTracker.metaData.Status = order.OrderStatusBooked
	rig.dc.cfgMtx.Lock()
	mktConf.StartEpoch = 12 // make it appear running again first
	mktConf.FinalEpoch = 0
	mktConf.Persist = nil
	rig.dc.cfgMtx.Unlock()
	req, _ = msgjson.NewRequest(rig.dc.NextID(), msgjson.SuspensionRoute, payload)
	err = handleTradeSuspensionMsg(rig.core, rig.dc, req)
	if err != nil {
		t.Fatalf("[handleTradeSuspensionMsg] unexpected error: %v", err)
	}
	// Check that the funding coin was returned.
	dc.tradeMtx.Lock()
	if len(tDcrWallet.returnedCoins) != 1 || !bytes.Equal(tDcrWallet.returnedCoins[0].ID(), changeCoinID) {
		t.Fatalf("change coin not returned")
	}
	tDcrWallet.returnedCoins = nil
	dc.tradeMtx.Unlock()

	// Make sure the coin isn't returned if there are unswapped matches.
	mid := ordertest.RandomMatchID()
	match := &matchTracker{
		MetaMatch: db.MetaMatch{
			UserMatch: &order.UserMatch{MatchID: mid}, // Default status = NewlyMatched
			MetaData:  &db.MatchMetaData{},
		},
		counterConfirms: -1,
	}
	swappedTracker.matches[mid] = match
	rig.dc.cfgMtx.Lock()
	mktConf.StartEpoch = 12 // make it appear running again first
	rig.dc.cfgMtx.Unlock()
	req, _ = msgjson.NewRequest(rig.dc.NextID(), msgjson.SuspensionRoute, payload)
	err = handleTradeSuspensionMsg(rig.core, rig.dc, req)
	if err != nil {
		t.Fatalf("[handleTradeSuspensionMsg] unexpected error: %v", err)
	}
	dc.tradeMtx.Lock()
	if tDcrWallet.returnedCoins != nil {
		t.Fatalf("change coin returned with active matches")
	}
	dc.tradeMtx.Unlock()

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

func orderNoteFeed(tCore *Core) (orderNotes chan *OrderNote, done func()) {
	orderNotes = make(chan *OrderNote, 1)

	ntfnFeed := tCore.NotificationFeed()
	feedDone := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case n := <-ntfnFeed:
				ordNote, ok := n.(*OrderNote)
				if ok {
					orderNotes <- ordNote
					return // just one OrderNote and done
				}
			case <-tCtx.Done():
				return
			case <-feedDone:
				return
			}
		}
	}()

	done = func() {
		close(feedDone) // close first on return
		wg.Wait()
	}
	return orderNotes, done
}

func verifyRevokeNotification(ch chan *OrderNote, expectedSubject string, t *testing.T) {
	select {
	case actualOrderNote := <-ch:
		if expectedSubject != actualOrderNote.SubjectText {
			t.Fatalf("SubjectText mismatch. %s != %s", actualOrderNote.SubjectText,
				expectedSubject)
		}
		return
	case <-tCtx.Done():
		return
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for OrderNote notification")
		return
	}
}

func TestHandleTradeResumptionMsg(t *testing.T) {
	rig := newTestRig()
	defer rig.shutdown()

	tCore := rig.core
	dcrWallet, _ := newTWallet(tDCR.ID)
	tCore.wallets[tDCR.ID] = dcrWallet
	dcrWallet.Unlock(rig.crypter)

	btcWallet, _ := newTWallet(tBTC.ID)
	tCore.wallets[tBTC.ID] = btcWallet
	btcWallet.Unlock(rig.crypter)

	epochLen := rig.dc.marketConfig(tDcrBtcMktName).EpochLen

	handleLimit := func(msg *msgjson.Message, f msgFunc) error {
		// Need to stamp and sign the message with the server's key.
		msgOrder := new(msgjson.LimitOrder)
		err := msg.Unmarshal(msgOrder)
		if err != nil {
			t.Fatalf("unmarshal error: %v", err)
		}
		lo := convertMsgLimitOrder(msgOrder)
		f(orderResponse(msg.ID, msgOrder, lo, false, false, false))
		return nil
	}

	tradeForm := &TradeForm{
		Host:    tDexHost,
		IsLimit: true,
		Sell:    true,
		Base:    tDCR.ID,
		Quote:   tBTC.ID,
		Qty:     tDCR.LotSize * 10,
		Rate:    tBTC.RateStep * 1000,
		TifNow:  false,
	}

	rig.ws.queueResponse(msgjson.LimitRoute, handleLimit)

	// Ensure a non-existent market cannot be suspended.
	payload := &msgjson.TradeResumption{
		MarketID: "dcr_dcr",
	}

	req, _ := msgjson.NewRequest(rig.dc.NextID(), msgjson.ResumptionRoute, payload)
	err := handleTradeResumptionMsg(rig.core, rig.dc, req)
	if err == nil {
		t.Fatal("[handleTradeResumptionMsg] expected a market ID not found error")
	}

	var resumeTime uint64
	newPayload := func() *msgjson.TradeResumption {
		return &msgjson.TradeResumption{
			MarketID:   tDcrBtcMktName,
			ResumeTime: resumeTime, // set the time to test the scheduling notification case, zero it for immediate resume
			StartEpoch: resumeTime / epochLen,
		}
	}

	// Notify of scheduled resume.
	rig.dc.cfgMtx.Lock()
	mktConf := rig.dc.findMarketConfig(tDcrBtcMktName)
	mktConf.StartEpoch = 12
	mktConf.FinalEpoch = mktConf.StartEpoch + 1 // long since closed
	rig.dc.cfgMtx.Unlock()

	resumeTime = encode.UnixMilliU(time.Now().Add(time.Hour))
	payload = newPayload()
	req, _ = msgjson.NewRequest(rig.dc.NextID(), msgjson.ResumptionRoute, payload)
	err = handleTradeResumptionMsg(rig.core, rig.dc, req)
	if err != nil {
		t.Fatalf("[handleTradeResumptionMsg] unexpected error: %v", err)
	}

	// Should be suspended still, no trades
	_, err = rig.core.Trade(tPW, tradeForm)
	if err == nil {
		t.Fatal("trade was accepted for suspended market")
	}

	// Resume the market immediately.
	resumeTime = encode.UnixMilliU(time.Now())
	payload = newPayload()
	payload.ResumeTime = 0 // resume now, not scheduled
	req, _ = msgjson.NewRequest(rig.dc.NextID(), msgjson.ResumptionRoute, payload)
	err = handleTradeResumptionMsg(rig.core, rig.dc, req)
	if err != nil {
		t.Fatalf("[handleTradeResumptionMsg] unexpected error: %v", err)
	}

	// Ensure trades for a resumed market are processed without error.
	_, err = rig.core.Trade(tPW, tradeForm)
	if err != nil {
		t.Fatalf("unexpected trade error %v", err)
	}
}

func TestHandleNomatch(t *testing.T) {
	rig := newTestRig()
	defer rig.shutdown()
	tCore := rig.core
	dc := rig.dc
	mkt := dc.marketConfig(tDcrBtcMktName)

	dcrWallet, _ := newTWallet(tDCR.ID)
	tCore.wallets[tDCR.ID] = dcrWallet
	btcWallet, _ := newTWallet(tBTC.ID)
	tCore.wallets[tBTC.ID] = btcWallet

	walletSet, err := tCore.walletSet(dc, tDCR.ID, tBTC.ID, true)
	if err != nil {
		t.Fatalf("walletSet error: %v", err)
	}

	// Four types of order to check

	// 1. Immediate limit order
	loImmediate, dbOrder, preImgL, _ := makeLimitOrder(dc, true, tDCR.LotSize*100, tBTC.RateStep)
	loImmediate.Force = order.ImmediateTiF
	immediateOID := loImmediate.ID()
	immediateTracker := newTrackedTrade(dbOrder, preImgL, dc, mkt.EpochLen, rig.core.lockTimeTaker, rig.core.lockTimeMaker,
		rig.db, rig.queue, walletSet, nil, rig.core.notify)
	dc.trades[immediateOID] = immediateTracker

	// 2. Standing limit order
	loStanding, dbOrder, preImgL, _ := makeLimitOrder(dc, true, tDCR.LotSize*100, tBTC.RateStep)
	loStanding.Force = order.StandingTiF
	standingOID := loStanding.ID()
	standingTracker := newTrackedTrade(dbOrder, preImgL, dc, mkt.EpochLen, rig.core.lockTimeTaker, rig.core.lockTimeMaker,
		rig.db, rig.queue, walletSet, nil, rig.core.notify)
	dc.trades[standingOID] = standingTracker

	// 3. Cancel order.
	cancelOrder := &order.CancelOrder{
		P: order.Prefix{
			ServerTime: time.Now(),
		},
	}
	cancelOID := cancelOrder.ID()
	standingTracker.cancel = &trackedCancel{
		CancelOrder: *cancelOrder,
	}

	// 4. Market order.
	loWillBeMarket, dbOrder, preImgL, _ := makeLimitOrder(dc, true, tDCR.LotSize*100, tBTC.RateStep)
	mktOrder := &order.MarketOrder{
		P: loWillBeMarket.P,
		T: *loWillBeMarket.Trade().Copy(),
	}
	dbOrder.Order = mktOrder
	marketOID := mktOrder.ID()
	marketTracker := newTrackedTrade(dbOrder, preImgL, dc, mkt.EpochLen, rig.core.lockTimeTaker, rig.core.lockTimeMaker,
		rig.db, rig.queue, walletSet, nil, rig.core.notify)
	dc.trades[marketOID] = marketTracker

	runNomatch := func(tag string, oid order.OrderID) {
		tracker, _, _ := dc.findOrder(oid)
		if tracker == nil {
			t.Fatalf("%s: order ID not found", tag)
		}
		payload := &msgjson.NoMatch{OrderID: oid[:]}
		req, _ := msgjson.NewRequest(dc.NextID(), msgjson.NoMatchRoute, payload)
		err := handleNoMatchRoute(tCore, dc, req)
		if err != nil {
			t.Fatalf("handleNoMatchRoute error: %v", err)
		}
	}

	checkTradeStatus := func(tag string, oid order.OrderID, expStatus order.OrderStatus) {
		tracker, _, _ := dc.findOrder(oid)
		if tracker.metaData.Status != expStatus {
			t.Fatalf("%s: wrong status. expected %s, got %s", tag, expStatus, tracker.metaData.Status)
		}
		if rig.db.lastStatusID != oid {
			t.Fatalf("%s: order status not stored", tag)
		}
		if rig.db.lastStatus != expStatus {
			t.Fatalf("%s: wrong order status stored. expected %s, got %s", tag, expStatus, rig.db.lastStatus)
		}
	}

	runNomatch("cancel", cancelOID)
	if rig.db.lastStatusID != cancelOID || rig.db.lastStatus != order.OrderStatusExecuted {
		t.Fatalf("cancel status not updated")
	}
	if rig.db.linkedFromID != standingOID || !rig.db.linkedToID.IsZero() {
		t.Fatalf("missed cancel not unlinked. wanted trade ID %s, got %s. wanted zeroed linked ID, got %s",
			standingOID, rig.db.linkedFromID, rig.db.linkedToID)
	}

	runNomatch("standing limit", standingOID)
	checkTradeStatus("standing limit", standingOID, order.OrderStatusBooked)

	runNomatch("immediate", immediateOID)
	checkTradeStatus("immediate", immediateOID, order.OrderStatusExecuted)

	runNomatch("market", marketOID)
	checkTradeStatus("market", marketOID, order.OrderStatusExecuted)

	// Unknown order should error.
	oid := ordertest.RandomOrderID()
	payload := &msgjson.NoMatch{OrderID: oid[:]}
	req, _ := msgjson.NewRequest(dc.NextID(), msgjson.NoMatchRoute, payload)
	err = handleNoMatchRoute(tCore, dc, req)
	if !errorHasCode(err, unknownOrderErr) {
		t.Fatalf("wrong error for unknown order ID: %v", err)
	}
}

func TestWalletSettings(t *testing.T) {
	rig := newTestRig()
	defer rig.shutdown()
	tCore := rig.core
	rig.db.wallet = &db.Wallet{
		Settings: map[string]string{
			"abc": "123",
		},
	}
	var assetID uint32 = 54321

	// wallet not found
	_, err := tCore.WalletSettings(assetID)
	if !errorHasCode(err, missingWalletErr) {
		t.Fatalf("wrong error for missing wallet: %v", err)
	}

	tCore.wallets[assetID] = &xcWallet{}

	// db error
	rig.db.walletErr = tErr
	_, err = tCore.WalletSettings(assetID)
	if !errorHasCode(err, dbErr) {
		t.Fatalf("wrong error when expected db error: %v", err)
	}
	rig.db.walletErr = nil

	// success
	returnedSettings, err := tCore.WalletSettings(assetID)
	if err != nil {
		t.Fatalf("WalletSettings error: %v", err)
	}

	if len(returnedSettings) != 1 || returnedSettings["abc"] != "123" {
		t.Fatalf("returned wallet settings are not correct: %v", returnedSettings)
	}
}

func TestReconfigureWallet(t *testing.T) {
	rig := newTestRig()
	defer rig.shutdown()
	tCore := rig.core
	rig.db.wallet = &db.Wallet{
		Settings: map[string]string{
			"abc": "123",
		},
	}
	newSettings := map[string]string{
		"def": "456",
	}
	var assetID uint32 = 54321

	// App Password error
	rig.crypter.recryptErr = tErr
	err := tCore.ReconfigureWallet(tPW, nil, assetID, newSettings)
	if !errorHasCode(err, authErr) {
		t.Fatalf("wrong error for password error: %v", err)
	}
	rig.crypter.recryptErr = nil

	// Missing wallet error
	err = tCore.ReconfigureWallet(tPW, nil, assetID, newSettings)
	if !errorHasCode(err, missingWalletErr) {
		t.Fatalf("wrong error for missing wallet: %v", err)
	}

	xyzWallet, tXyzWallet := newTWallet(assetID)
	tCore.wallets[assetID] = xyzWallet
	asset.Register(assetID, &tDriver{
		f: func(wCfg *asset.WalletConfig, logger dex.Logger, net dex.Network) (asset.Wallet, error) {
			return xyzWallet.Wallet, nil
		},
	})
	if err = xyzWallet.Connect(); err != nil {
		t.Fatal(err)
	}
	defer xyzWallet.Disconnect()

	// Connect error
	tXyzWallet.connectErr = tErr
	err = tCore.ReconfigureWallet(tPW, nil, assetID, newSettings)
	if !errorHasCode(err, connectWalletErr) {
		t.Fatalf("wrong error when expecting connection error: %v", err)
	}
	tXyzWallet.connectErr = nil

	// Unlock error
	tXyzWallet.Unlock(wPW)
	tXyzWallet.unlockErr = tErr
	err = tCore.ReconfigureWallet(tPW, nil, assetID, newSettings)
	if !errorHasCode(err, walletAuthErr) {
		t.Fatalf("wrong error when expecting auth error: %v", err)
	}
	tXyzWallet.unlockErr = nil

	// For the last success, make sure that we also clear any related
	// tickGovernors.
	match := &matchTracker{
		suspectSwap:  true,
		tickGovernor: time.NewTimer(time.Hour),
	}
	tCore.conns[tDexHost].trades[order.OrderID{}] = &trackedTrade{
		Order: &order.LimitOrder{
			P: order.Prefix{
				BaseAsset: assetID,
			},
		},
		wallets: &walletSet{
			fromAsset:  &dex.Asset{ID: assetID},
			fromWallet: &xcWallet{AssetID: assetID},
			toAsset:    &dex.Asset{},
			toWallet:   &xcWallet{},
		},
		matches: map[order.MatchID]*matchTracker{
			{}: match,
		},
	}

	// Success updating settings.
	err = tCore.ReconfigureWallet(tPW, nil, assetID, newSettings)
	if err != nil {
		t.Fatalf("ReconfigureWallet error: %v", err)
	}

	settings := rig.db.wallet.Settings
	if len(settings) != 1 || settings["def"] != "456" {
		t.Fatalf("settings not stored")
	}

	if match.tickGovernor != nil {
		t.Fatalf("tickGovernor not removed")
	}

	// Success updating wallet PW.
	newWalletPW := []byte("password")
	err = tCore.ReconfigureWallet(tPW, newWalletPW, assetID, newSettings)
	if err != nil {
		t.Fatalf("ReconfigureWallet error: %v", err)
	}

	// Check that the xcWallet was updated.
	xyzWallet = tCore.wallets[assetID]
	if !bytes.Equal(xyzWallet.encPW, newWalletPW) {
		t.Fatalf("xcWallet encPW field not updated want: %x got: %x", newWalletPW, xyzWallet.encPW)
	}
}

func TestSetWalletPassword(t *testing.T) {
	rig := newTestRig()
	defer rig.shutdown()
	tCore := rig.core
	rig.db.wallet = &db.Wallet{
		EncryptedPW: []byte("abc"),
	}
	newPW := []byte("def")
	var assetID uint32 = 54321

	// Nil password error
	err := tCore.SetWalletPassword(tPW, assetID, nil)
	if !errorHasCode(err, passwordErr) {
		t.Fatalf("wrong error for nil password error: %v", err)
	}

	// Auth error
	rig.crypter.recryptErr = tErr
	err = tCore.SetWalletPassword(tPW, assetID, newPW)
	if !errorHasCode(err, authErr) {
		t.Fatalf("wrong error for auth error: %v", err)
	}
	rig.crypter.recryptErr = nil

	// Missing wallet error
	err = tCore.SetWalletPassword(tPW, assetID, newPW)
	if !errorHasCode(err, missingWalletErr) {
		t.Fatalf("wrong error for missing wallet: %v", err)
	}

	xyzWallet, tXyzWallet := newTWallet(assetID)
	tCore.wallets[assetID] = xyzWallet

	// Connection error
	xyzWallet.hookedUp = false
	tXyzWallet.connectErr = tErr
	err = tCore.SetWalletPassword(tPW, assetID, newPW)
	if !errorHasCode(err, connectionErr) {
		t.Fatalf("wrong error for connection error: %v", err)
	}
	xyzWallet.hookedUp = true
	tXyzWallet.connectErr = nil

	// Unlock error
	tXyzWallet.unlockErr = tErr
	err = tCore.SetWalletPassword(tPW, assetID, newPW)
	if !errorHasCode(err, authErr) {
		t.Fatalf("wrong error for auth error: %v", err)
	}
	tXyzWallet.unlockErr = nil

	// SetWalletPassword db error
	rig.db.setWalletPwErr = tErr
	err = tCore.SetWalletPassword(tPW, assetID, newPW)
	if !errorHasCode(err, dbErr) {
		t.Fatalf("wrong error for missing wallet: %v", err)
	}
	rig.db.setWalletPwErr = nil

	// Success
	err = tCore.SetWalletPassword(tPW, assetID, newPW)
	if err != nil {
		t.Fatalf("SetWalletPassword error: %v", err)
	}

	// Check that the xcWallet was updated.
	if !bytes.Equal(xyzWallet.encPW, newPW) {
		t.Fatalf("xcWallet encPW field not updated")
	}
}

func TestHandlePenaltyMsg(t *testing.T) {
	rig := newTestRig()
	defer rig.shutdown()
	tCore := rig.core
	dc := rig.dc
	penalty := &msgjson.Penalty{
		Rule:     account.Rule(1),
		Time:     uint64(1598929305),
		Duration: uint64(3153600000000000000),
		Details:  "You may no longer trade. Leave your client running to finish pending trades.",
	}
	diffKey, _ := secp256k1.GeneratePrivateKey()
	noMatch, err := msgjson.NewNotification(msgjson.NoMatchRoute, "fake")
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name    string
		key     *secp256k1.PrivateKey
		payload interface{}
		wantErr bool
	}{{
		name:    "ok",
		key:     tDexPriv,
		payload: penalty,
	}, {
		name:    "bad note",
		key:     tDexPriv,
		payload: noMatch,
		wantErr: true,
	}, {
		name:    "wrong sig",
		key:     diffKey,
		payload: penalty,
		wantErr: true,
	}}
	for _, test := range tests {
		var err error
		var note *msgjson.Message
		switch v := test.payload.(type) {
		case *msgjson.Penalty:
			penaltyNote := &msgjson.PenaltyNote{
				Penalty: v,
			}
			sign(test.key, penaltyNote)
			note, err = msgjson.NewNotification(msgjson.PenaltyRoute, penaltyNote)
			if err != nil {
				t.Fatalf("error creating penalty notification: %v", err)
			}
		case *msgjson.Message:
			note = v
		default:
			t.Fatalf("unknown payload type: %T", v)
		}

		err = handlePenaltyMsg(tCore, dc, note)
		if test.wantErr {
			if err == nil {
				t.Fatalf("expected error for test %s", test.name)
			}
			continue
		}
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", test.name, err)
		}
	}
}

func TestPreimageSync(t *testing.T) {
	rig := newTestRig()
	defer rig.shutdown()
	tCore := rig.core
	dcrWallet, tDcrWallet := newTWallet(tDCR.ID)
	tCore.wallets[tDCR.ID] = dcrWallet
	dcrWallet.Unlock(rig.crypter)

	btcWallet, tBtcWallet := newTWallet(tBTC.ID)
	tCore.wallets[tBTC.ID] = btcWallet
	btcWallet.Unlock(rig.crypter)

	var lots uint64 = 10
	qty := tDCR.LotSize * lots
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
	tDcrWallet.fundingCoins = asset.Coins{dcrCoin}
	tDcrWallet.fundRedeemScripts = []dex.Bytes{nil}

	btcVal := calc.BaseToQuote(rate, qty*2)
	btcCoin := &tCoin{
		id:  encode.RandomBytes(36),
		val: btcVal,
	}
	tBtcWallet.fundingCoins = asset.Coins{btcCoin}
	tBtcWallet.fundRedeemScripts = []dex.Bytes{nil}

	limitRouteProcessing := make(chan order.OrderID)

	rig.ws.queueResponse(msgjson.LimitRoute, func(msg *msgjson.Message, f msgFunc) error {
		t.Helper()
		// Need to stamp and sign the message with the server's key.
		msgOrder := new(msgjson.LimitOrder)
		err := msg.Unmarshal(msgOrder)
		if err != nil {
			return fmt.Errorf("unmarshal error: %w", err)
		}
		lo := convertMsgLimitOrder(msgOrder)
		resp := orderResponse(msg.ID, msgOrder, lo, false, false, false)
		limitRouteProcessing <- lo.ID()
		<-time.NewTimer(time.Millisecond * 100).C
		f(resp)
		return nil
	})

	errChan := make(chan error, 1)
	// Run the trade in a goroutine.
	go func() {
		_, err := tCore.Trade(tPW, form)
		errChan <- err
	}()

	// Wait for the limit route to start processing. Then we have 100 ms to call
	// handlePreimageRequest to catch the early-preimage case.
	var oid order.OrderID
	select {
	case oid = <-limitRouteProcessing:
	case <-time.NewTimer(time.Second).C:
		t.Fatalf("limit route never hit")
	}

	// So ideally, we're calling handlePreimageRequest about 100 ms before we
	// even have an order id back from the server. This shouldn't result in an
	// error.
	payload := &msgjson.PreimageRequest{
		OrderID: oid[:],
	}
	req, _ := msgjson.NewRequest(rig.dc.NextID(), msgjson.PreimageRoute, payload)
	err := handlePreimageRequest(rig.core, rig.dc, req)
	if err != nil {
		t.Fatalf("early preimage request error: %v", err)
	}
	err = <-errChan
	if err != nil {
		t.Fatalf("trade error: %v", err)
	}
}

func TestMatchStatusResolution(t *testing.T) {
	rig := newTestRig()
	defer rig.shutdown()
	tCore := rig.core
	dc := rig.dc
	mkt := dc.marketConfig(tDcrBtcMktName)

	dcrWallet, _ := newTWallet(tDCR.ID)
	tCore.wallets[tDCR.ID] = dcrWallet
	btcWallet, tBtcWallet := newTWallet(tBTC.ID)
	tCore.wallets[tBTC.ID] = btcWallet
	walletSet, _ := tCore.walletSet(dc, tDCR.ID, tBTC.ID, true)

	qty := 3 * tDCR.LotSize
	secret := encode.RandomBytes(32)
	secretHash := sha256.Sum256(secret)

	lo, dbOrder, preImg, addr := makeLimitOrder(dc, true, qty, tBTC.RateStep*10)
	dbOrder.MetaData.Status = order.OrderStatusExecuted // so there is no order_status request for this
	oid := lo.ID()
	trade := newTrackedTrade(dbOrder, preImg, dc, mkt.EpochLen, rig.core.lockTimeTaker, rig.core.lockTimeMaker,
		rig.db, rig.queue, walletSet, nil, rig.core.notify)

	dc.trades[trade.ID()] = trade
	matchID := ordertest.RandomMatchID()
	matchTime := time.Now()
	match := &matchTracker{
		MetaMatch: db.MetaMatch{
			MetaData: &db.MatchMetaData{},
			UserMatch: &order.UserMatch{
				MatchID: matchID,
				Address: addr,
			},
		},
	}
	trade.matches[matchID] = match

	// oid order.OrderID, mid order.MatchID, recipient string, val uint64, secretHash []byte
	_, auditInfo := tMsgAudit(oid, matchID, addr, qty, secretHash[:])
	tBtcWallet.auditInfo = auditInfo

	connectMatches := func(status order.MatchStatus) []*msgjson.Match {
		return []*msgjson.Match{
			{
				OrderID: oid[:],
				MatchID: matchID[:],
				Status:  uint8(status),
				Side:    uint8(match.Side),
			},
		}

	}

	tBytes := encode.RandomBytes(2)
	tCoinID := encode.RandomBytes(36)
	tTxData := encode.RandomBytes(1)

	setAuthSigs := func(status order.MatchStatus) {
		isMaker := match.Side == order.Maker
		match.MetaData.Proof.Auth = db.MatchAuth{}
		auth := &match.MetaData.Proof.Auth
		auth.MatchStamp = encode.UnixMilliU(matchTime)
		if status >= order.MakerSwapCast {
			if isMaker {
				auth.InitSig = tBytes
			} else {
				auth.AuditSig = tBytes
			}
		}
		if status >= order.TakerSwapCast {
			if isMaker {
				auth.AuditSig = tBytes
			} else {
				auth.InitSig = tBytes
			}
		}
		if status >= order.MakerRedeemed {
			if isMaker {
				auth.RedeemSig = tBytes
			} else {
				auth.RedemptionSig = tBytes
			}
		}
		if status >= order.MatchComplete {
			if isMaker {
				auth.RedemptionSig = tBytes
			} else {
				auth.RedeemSig = tBytes
			}
		}
	}

	// Call setProof before setAuthSigs
	setProof := func(status order.MatchStatus) {
		isMaker := match.Side == order.Maker
		match.Status = status
		match.MetaData.Proof = db.MatchProof{}
		proof := &match.MetaData.Proof

		if isMaker {
			auditInfo.Expiration = matchTime.Add(trade.lockTimeTaker)
		} else {
			auditInfo.Expiration = matchTime.Add(trade.lockTimeMaker)
		}

		if status >= order.MakerSwapCast {
			proof.MakerSwap = tCoinID
			proof.SecretHash = secretHash[:]
			if isMaker {
				proof.Script = tBytes
				proof.Secret = secret
			} else {
				proof.CounterContract = tBytes
			}
		}
		if status >= order.TakerSwapCast {
			proof.TakerSwap = tCoinID
			if isMaker {
				proof.CounterContract = tBytes
			} else {
				proof.Script = tBytes
			}
		}
		if status >= order.MakerRedeemed {
			proof.MakerRedeem = tCoinID
			if !isMaker {
				proof.Secret = secret
			}
		}
		if status >= order.MatchComplete {
			proof.TakerRedeem = tCoinID
		}
	}

	setLocalMatchStatus := func(proofStatus, authStatus order.MatchStatus) {
		setProof(proofStatus)
		setAuthSigs(authStatus)
	}

	var tMatchResults *msgjson.MatchStatusResult
	setMatchResults := func(status order.MatchStatus) *msgjson.MatchStatusResult {
		tMatchResults = &msgjson.MatchStatusResult{
			MatchID: matchID[:],
			Status:  uint8(status),
			Active:  status != order.MatchComplete,
		}
		if status >= order.MakerSwapCast {
			tMatchResults.MakerContract = tBytes
			tMatchResults.MakerSwap = tCoinID
		}
		if status == order.MakerSwapCast || status == order.TakerSwapCast {
			tMatchResults.TakerTxData = tTxData
		}
		if status >= order.TakerSwapCast {
			tMatchResults.TakerContract = tBytes
			tMatchResults.TakerSwap = tCoinID
		}
		if status >= order.MakerRedeemed {
			tMatchResults.MakerRedeem = tCoinID
			tMatchResults.Secret = secret
		}
		if status >= order.MatchComplete {
			tMatchResults.TakerRedeem = tCoinID
		}
		return tMatchResults
	}

	type test struct {
		ours, servers      order.MatchStatus
		side               order.MatchSide
		tweaker            func()
		countStatusUpdates int
	}

	testName := func(tt *test) string {
		return fmt.Sprintf("%s / %s (%s)", tt.ours, tt.servers, tt.side)
	}

	runTest := func(tt *test) order.MatchStatus {
		match.Side = tt.side
		setLocalMatchStatus(tt.ours, tt.servers)
		setMatchResults(tt.servers)
		if tt.tweaker != nil {
			tt.tweaker()
		}
		rig.queueConnect(nil, connectMatches(tt.servers), nil)
		rig.ws.queueResponse(msgjson.MatchStatusRoute, func(msg *msgjson.Message, f msgFunc) error {
			resp, _ := msgjson.NewResponse(msg.ID, []*msgjson.MatchStatusResult{tMatchResults}, nil)
			f(resp)
			return nil
		})
		if tt.countStatusUpdates > 0 {
			rig.db.setUpdateMatchHook(match.MatchID, make(chan order.MatchStatus, tt.countStatusUpdates))
		}
		if err := tCore.authDEX(dc); err != nil {
			t.Fatalf("unexpected authDEX error: %v", err)
		}
		for i := 0; i < tt.countStatusUpdates; i++ {
			rig.db.waitForMatchUpdateOrFail(match.MatchID, t)
		}
		rig.db.setUpdateMatchHook(match.MatchID, nil)
		trade.mtx.Lock()
		newStatus := match.Status
		trade.mtx.Unlock()
		return newStatus
	}

	// forwardResolvers are recoverable status combos where the server is ahead
	// of us.
	forwardResolvers := []*test{
		{
			ours:               order.NewlyMatched,
			servers:            order.MakerSwapCast,
			side:               order.Taker,
			countStatusUpdates: 2,
		},
		{
			ours:               order.MakerSwapCast,
			servers:            order.TakerSwapCast,
			side:               order.Maker,
			countStatusUpdates: 2,
		},
		{
			ours:    order.TakerSwapCast,
			servers: order.MakerRedeemed,
			side:    order.Taker,
		},
		{
			ours:    order.MakerRedeemed,
			servers: order.MatchComplete,
			side:    order.Maker,
		},
	}

	// Check that all of the forwardResolvers update the match status.
	for _, tt := range forwardResolvers {
		newStatus := runTest(tt)
		if newStatus == tt.ours {
			t.Fatalf("(%s) status not updated for forward resolution path", testName(tt))
		}
		if match.MetaData.Proof.SelfRevoked {
			t.Fatalf("(%s) match self-revoked during forward resolution", testName(tt))
		}
	}

	// backwardsResolvers are recoverable status mismatches where we are ahead
	// of the server but can be resolved by deferring to resendPendingRequests.
	backWardsResolvers := []*test{
		{
			ours:    order.MakerSwapCast,
			servers: order.NewlyMatched,
			side:    order.Maker,
		},
		{
			ours:    order.TakerSwapCast,
			servers: order.MakerSwapCast,
			side:    order.Taker,
		},
		{
			ours:    order.MakerRedeemed,
			servers: order.TakerSwapCast,
			side:    order.Maker,
		},
		{
			ours:    order.MatchComplete,
			servers: order.MakerRedeemed,
			side:    order.Taker,
		},
	}

	// Backwards resolvers won't update the match status, but also won't revoke
	// the match.
	for _, tt := range backWardsResolvers {
		newStatus := runTest(tt)
		if newStatus != tt.ours {
			t.Fatalf("(%s) status changed for backwards resolution path", testName(tt))
		}
		if match.MetaData.Proof.SelfRevoked {
			t.Fatalf("(%s) match self-revoked during backwards resolution", testName(tt))
		}
	}

	// nonsense are status combos that make no sense, so should always result
	// in a self-revocation.
	nonsense := []*test{
		{ // Server has our info before us
			ours:    order.NewlyMatched,
			servers: order.MakerSwapCast,
			side:    order.Maker,
		},
		{ // Two steps apart
			ours:    order.NewlyMatched,
			servers: order.TakerSwapCast,
			side:    order.Maker,
		},
		{ // Server didn't send contract
			ours:    order.NewlyMatched,
			servers: order.MakerSwapCast,
			side:    order.Taker,
			tweaker: func() {
				tMatchResults.MakerContract = nil
			},
		},
		{ // Server didn't send coin ID.
			ours:    order.NewlyMatched,
			servers: order.MakerSwapCast,
			side:    order.Taker,
			tweaker: func() {
				tMatchResults.MakerSwap = nil
			},
		},
		{ // Audit failed.
			ours:    order.NewlyMatched,
			servers: order.MakerSwapCast,
			side:    order.Taker,
			tweaker: func() {
				auditInfo.Expiration = matchTime
			},
			countStatusUpdates: 2, // async auditContract -> revoke and db update
		},
		{ // Server has our info before us
			ours:    order.MakerSwapCast,
			servers: order.TakerSwapCast,
			side:    order.Taker,
		},
		{ // Server has our info before us
			ours:    order.MakerSwapCast,
			servers: order.TakerSwapCast,
			side:    order.Taker,
		},
		{ // Server didn't send contract
			ours:    order.MakerSwapCast,
			servers: order.TakerSwapCast,
			side:    order.Maker,
			tweaker: func() {
				tMatchResults.TakerContract = nil
			},
		},
		{ // Server didn't send coin ID.
			ours:    order.MakerSwapCast,
			servers: order.TakerSwapCast,
			side:    order.Maker,
			tweaker: func() {
				tMatchResults.TakerSwap = nil
			},
		},
		{ // Audit failed.
			ours:    order.MakerSwapCast,
			servers: order.TakerSwapCast,
			side:    order.Maker,
			tweaker: func() {
				auditInfo.Expiration = matchTime
			},
			countStatusUpdates: 2, // async auditContract -> revoke and db update
		},
		{ // Taker has counter-party info the server doesn't.
			ours:    order.MakerSwapCast,
			servers: order.NewlyMatched,
			side:    order.Taker,
		},
		{ // Maker has a server ack, but they say they don't have the data.
			ours:    order.MakerSwapCast,
			servers: order.NewlyMatched,
			side:    order.Maker,
			tweaker: func() {
				match.MetaData.Proof.Auth.InitSig = tBytes
			},
		},
		{ // Maker has counter-party info the server doesn't.
			ours:    order.TakerSwapCast,
			servers: order.MakerSwapCast,
			side:    order.Maker,
		},
		{ // Taker has a server ack, but they say they don't have the data.
			ours:    order.TakerSwapCast,
			servers: order.MakerSwapCast,
			side:    order.Taker,
			tweaker: func() {
				match.MetaData.Proof.Auth.InitSig = tBytes
			},
		},
		{ // Server has redeem info before us.
			ours:    order.TakerSwapCast,
			servers: order.MakerRedeemed,
			side:    order.Maker,
		},
		{ // Server didn't provide redemption coin ID.
			ours:    order.TakerSwapCast,
			servers: order.MakerRedeemed,
			side:    order.Taker,
			tweaker: func() {
				tMatchResults.MakerRedeem = nil
			},
		},
		{ // Server didn't provide secret.
			ours:    order.TakerSwapCast,
			servers: order.MakerRedeemed,
			side:    order.Taker,
			tweaker: func() {
				tMatchResults.Secret = nil
			},
		},
		{ // Server has our redemption data before us.
			ours:    order.MakerRedeemed,
			servers: order.MatchComplete,
			side:    order.Taker,
		},
		{ // We have data before the server.
			ours:    order.MakerRedeemed,
			servers: order.TakerSwapCast,
			side:    order.Taker,
		},
		{ // We have a server ack, but they say they don't have the data.
			ours:    order.MakerRedeemed,
			servers: order.TakerSwapCast,
			side:    order.Maker,
			tweaker: func() {
				match.MetaData.Proof.Auth.RedeemSig = tBytes
			},
		},
		{ // We have data before the server.
			ours:    order.MatchComplete,
			servers: order.MakerSwapCast,
			side:    order.Maker,
		},
		{ // We have a server ack, but they say they don't have the data.
			ours:    order.MatchComplete,
			servers: order.MakerSwapCast,
			side:    order.Taker,
			tweaker: func() {
				match.MetaData.Proof.Auth.RedeemSig = tBytes
			},
		},
	}

	for _, tt := range nonsense {
		runTest(tt)
		if !match.MetaData.Proof.SelfRevoked {
			t.Fatalf("(%s) match not self-revoked during nonsense resolution", testName(tt))
		}
	}

	// Run two matches for the same order.
	match2ID := ordertest.RandomMatchID()
	match2 := &matchTracker{
		MetaMatch: db.MetaMatch{
			MetaData: &db.MatchMetaData{},
			UserMatch: &order.UserMatch{
				MatchID: match2ID,
				Address: addr,
			},
		},
	}
	trade.matches[match2ID] = match2
	setAuthSigs(order.NewlyMatched)
	setProof(order.NewlyMatched)
	match2.Side = order.Taker
	match2.MetaData.Proof = match.MetaData.Proof

	srvMatches := connectMatches(order.MakerSwapCast)
	srvMatches = append(srvMatches, &msgjson.Match{OrderID: oid[:],
		MatchID: match2ID[:],
		Status:  uint8(order.MakerSwapCast),
		Side:    uint8(order.Taker),
	})

	res1 := setMatchResults(order.MakerSwapCast)
	res2 := setMatchResults(order.MakerSwapCast)
	res2.MatchID = match2ID[:]

	rig.queueConnect(nil, srvMatches, nil)
	rig.ws.queueResponse(msgjson.MatchStatusRoute, func(msg *msgjson.Message, f msgFunc) error {
		resp, _ := msgjson.NewResponse(msg.ID, []*msgjson.MatchStatusResult{res1, res2}, nil)
		f(resp)
		return nil
	})
	// 2 matches resolved via contract audit: 2 synchronous updates, 2 async
	rig.db.setUpdateMatchHook(match.MatchID, make(chan order.MatchStatus, 2))
	rig.db.setUpdateMatchHook(match2.MatchID, make(chan order.MatchStatus, 2))
	tCore.authDEX(dc)
	for i := 0; i < 2; i++ {
		rig.db.waitForMatchUpdateOrFail(match.MatchID, t)
		rig.db.waitForMatchUpdateOrFail(match2.MatchID, t)
	}
	trade.mtx.Lock()
	newStatus1 := match.Status
	newStatus2 := match2.Status
	trade.mtx.Unlock()
	if newStatus1 != order.MakerSwapCast {
		t.Fatalf("wrong status for match 1: %s", newStatus1)
	}
	if newStatus2 != order.MakerSwapCast {
		t.Fatalf("wrong status for match 2: %s", newStatus2)
	}
}

func TestSuspectTrades(t *testing.T) {
	rig := newTestRig()
	defer rig.shutdown()
	dc := rig.dc
	tCore := rig.core

	dcrWallet, tDcrWallet := newTWallet(tDCR.ID)
	tCore.wallets[tDCR.ID] = dcrWallet
	btcWallet, tBtcWallet := newTWallet(tBTC.ID)
	tCore.wallets[tBTC.ID] = btcWallet
	walletSet, _ := tCore.walletSet(dc, tDCR.ID, tBTC.ID, true)

	lo, dbOrder, preImg, _ := makeLimitOrder(dc, true, 0, 0)
	oid := lo.ID()
	mkt := dc.marketConfig(tDcrBtcMktName)
	tracker := newTrackedTrade(dbOrder, preImg, dc, mkt.EpochLen, rig.core.lockTimeTaker, rig.core.lockTimeMaker,
		rig.db, rig.queue, walletSet, nil, rig.core.notify)
	dc.trades[oid] = tracker

	newMatch := func(side order.MatchSide, status order.MatchStatus) *matchTracker {
		return &matchTracker{
			prefix: lo.Prefix(),
			trade:  lo.Trade(),
			MetaMatch: db.MetaMatch{
				MetaData: &db.MatchMetaData{
					Proof: db.MatchProof{
						Auth: db.MatchAuth{
							MatchStamp: encode.UnixMilliU(time.Now()),
							AuditStamp: encode.UnixMilliU(time.Now()),
						},
					},
				},
				UserMatch: &order.UserMatch{
					MatchID: ordertest.RandomMatchID(),
					Side:    side,
					Address: ordertest.RandomAddress(),
					Status:  status,
				},
			},
		}
	}

	var swappableMatch1, swappableMatch2 *matchTracker
	setSwaps := func() {
		swappableMatch1 = newMatch(order.Maker, order.NewlyMatched)
		swappableMatch2 = newMatch(order.Taker, order.MakerSwapCast)

		// Set counterswaps for both swaps.
		_, auditInfo := tMsgAudit(oid, swappableMatch2.MatchID, ordertest.RandomAddress(), 1, encode.RandomBytes(32))
		tBtcWallet.setConfs(auditInfo.Coin.ID(), tDCR.SwapConf, nil)
		swappableMatch2.counterSwap = auditInfo
		_, auditInfo = tMsgAudit(oid, swappableMatch1.MatchID, ordertest.RandomAddress(), 1, encode.RandomBytes(32))
		tBtcWallet.setConfs(auditInfo.Coin.ID(), tDCR.SwapConf, nil)
		swappableMatch1.counterSwap = auditInfo

		tDcrWallet.swapCounter = 0
		tracker.matches = map[order.MatchID]*matchTracker{
			swappableMatch1.MatchID: swappableMatch1,
			swappableMatch2.MatchID: swappableMatch2,
		}
	}
	setSwaps()

	// Initial success
	_, err := tCore.tick(tracker)
	if err != nil {
		t.Fatalf("swap tick error: %v", err)
	}

	setSwaps()
	tDcrWallet.swapErr = tErr
	_, err = tCore.tick(tracker)
	if err == nil || !strings.Contains(err.Error(), "error sending swap transaction") {
		t.Fatalf("swap error not propagated, err = %v", err)
	}
	if tDcrWallet.swapCounter != 1 {
		t.Fatalf("never swapped")
	}

	// Both matches should be marked as suspect and have tickGovernors in place.
	tracker.mtx.Lock()
	for i, m := range []*matchTracker{swappableMatch1, swappableMatch2} {
		if !m.suspectSwap {
			t.Fatalf("swappable match %d not suspect after failed swap", i+1)
		}
		if m.tickGovernor == nil {
			t.Fatalf("swappable match %d has no tick meterer set", i+1)
		}
	}
	tracker.mtx.Unlock()

	// Ticking right away again should do nothing.
	tDcrWallet.swapErr = nil
	_, err = tCore.tick(tracker)
	if err != nil {
		t.Fatalf("tick error during metered swap tick: %v", err)
	}
	if tDcrWallet.swapCounter != 1 {
		t.Fatalf("swapped during metered tick")
	}

	// But once the tickGovernors expire, we should succeed with two separate
	// requests.
	tracker.mtx.Lock()
	swappableMatch1.tickGovernor = nil
	swappableMatch2.tickGovernor = nil
	tracker.mtx.Unlock()
	_, err = tCore.tick(tracker)
	if err != nil {
		t.Fatalf("tick error while swapping suspect matches: %v", err)
	}
	if tDcrWallet.swapCounter != 3 {
		t.Fatalf("suspect swap matches not run or not run separately. expected 2 new calls to Swap, got %d", tDcrWallet.swapCounter-1)
	}

	var redeemableMatch1, redeemableMatch2 *matchTracker
	setRedeems := func() {
		redeemableMatch1 = newMatch(order.Maker, order.TakerSwapCast)
		redeemableMatch2 = newMatch(order.Taker, order.MakerRedeemed)
		_, auditInfo := tMsgAudit(oid, redeemableMatch1.MatchID, ordertest.RandomAddress(), 1, encode.RandomBytes(32))
		tBtcWallet.setConfs(auditInfo.Coin.ID(), tBTC.SwapConf, nil)
		redeemableMatch1.counterSwap = auditInfo
		tBtcWallet.redeemCounter = 0
		tracker.matches = map[order.MatchID]*matchTracker{
			redeemableMatch1.MatchID: redeemableMatch1,
			redeemableMatch2.MatchID: redeemableMatch2,
		}
		rig.ws.queueResponse(msgjson.RedeemRoute, redeemAcker)
		rig.ws.queueResponse(msgjson.RedeemRoute, redeemAcker)
	}
	setRedeems()

	// Initial success
	tBtcWallet.redeemCoins = []dex.Bytes{encode.RandomBytes(36), encode.RandomBytes(36)}
	_, err = tCore.tick(tracker)
	if err != nil {
		t.Fatalf("redeem tick error: %v", err)
	}
	if tBtcWallet.redeemCounter != 1 {
		t.Fatalf("never redeemed")
	}

	setRedeems()
	tBtcWallet.redeemErr = tErr
	_, err = tCore.tick(tracker)
	if err == nil || !strings.Contains(err.Error(), "error sending redeem transaction") {
		t.Fatalf("redeem error not propagated. err = %v", err)
	}
	if tBtcWallet.redeemCounter != 1 {
		t.Fatalf("never redeemed")
	}

	// Both matches should be marked as suspect and have tickGovernors in place.
	tracker.mtx.Lock()
	for i, m := range []*matchTracker{redeemableMatch1, redeemableMatch2} {
		if !m.suspectRedeem {
			t.Fatalf("redeemable match %d not suspect after failed swap", i+1)
		}
		if m.tickGovernor == nil {
			t.Fatalf("redeemable match %d has no tick meterer set", i+1)
		}
	}
	tracker.mtx.Unlock()

	// Ticking right away again should do nothing.
	tBtcWallet.redeemErr = nil
	_, err = tCore.tick(tracker)
	if err != nil {
		t.Fatalf("tick error during metered redeem tick: %v", err)
	}
	if tBtcWallet.redeemCounter != 1 {
		t.Fatalf("redeemed during metered tick %d", tBtcWallet.redeemCounter)
	}

	// But once the tickGovernors expire, we should succeed with two separate
	// requests.
	tracker.mtx.Lock()
	redeemableMatch1.tickGovernor = nil
	redeemableMatch2.tickGovernor = nil
	tracker.mtx.Unlock()
	_, err = tCore.tick(tracker)
	if err != nil {
		t.Fatalf("tick error while redeeming suspect matches: %v", err)
	}
	if tBtcWallet.redeemCounter != 3 {
		t.Fatalf("suspect redeem matches not run or not run separately. expected 2 new calls to Redeem, got %d", tBtcWallet.redeemCounter-1)
	}
}

func TestWalletSyncing(t *testing.T) {
	rig := newTestRig()
	defer rig.shutdown()
	tCore := rig.core

	noteFeed := tCore.NotificationFeed()
	dcrWallet, tDcrWallet := newTWallet(tDCR.ID)
	dcrWallet.synced = false
	dcrWallet.syncProgress = 0
	_ = dcrWallet.Connect()
	defer dcrWallet.Disconnect()

	tStart := time.Now()
	testDuration := 100 * time.Millisecond
	syncTickerPeriod = 10 * time.Millisecond

	tDcrWallet.syncStatus = func() (bool, float32, error) {
		progress := float32(float64(time.Since(tStart)) / float64(testDuration))
		if progress >= 1 {
			return true, 1, nil
		}
		return false, progress, nil
	}

	_, err := tCore.connectWallet(dcrWallet)
	if err != nil {
		t.Fatalf("connectWallet error: %v", err)
	}

	timeout := time.NewTimer(time.Second)
	defer timeout.Stop()
	var progressNotes int
out:
	for {
		select {
		case note := <-noteFeed:
			walletNote, ok := note.(*WalletStateNote)
			if !ok {
				continue
			}
			if walletNote.Wallet.Synced {
				break out
			}
			progressNotes++
		case <-timeout.C:
			t.Fatalf("timed out waiting for synced wallet note. Received %d progress notes", progressNotes)
		}
	}
	// Should get 9 notes, but just make sure we get at least half of them to
	// avoid github vm false positives.
	if progressNotes < 5 /* 9? */ {
		t.Fatalf("expected 9 progress notes, got %d", progressNotes)
	}
	// t.Logf("got %d progress notes", progressNotes)
}

func TestParseCert(t *testing.T) {
	byteCert := []byte{0x0a, 0x0b}
	cert, err := parseCert("anyhost", []byte{0x0a, 0x0b})
	if err != nil {
		t.Fatalf("byte cert error: %v", err)
	}
	if !bytes.Equal(cert, byteCert) {
		t.Fatalf("byte cert note returned unmodified. expected %x, got %x", byteCert, cert)
	}
	byteCert = []byte{0x05, 0x06}
	certFile, _ := ioutil.TempFile("", "dumbcert")
	defer os.Remove(certFile.Name())
	certFile.Write(byteCert)
	certFile.Close()
	cert, err = parseCert("anyhost", certFile.Name())
	if err != nil {
		t.Fatalf("file cert error: %v", err)
	}
	if !bytes.Equal(cert, byteCert) {
		t.Fatalf("byte cert note returned unmodified. expected %x, got %x", byteCert, cert)
	}
	cert, err = parseCert("dex-test.ssgen.io:7232", []byte(nil))
	if err != nil {
		t.Fatalf("CertStore cert error: %v", err)
	}
	if len(cert) == 0 {
		t.Fatalf("no cert returned from cert store")
	}
}

func TestPreOrder(t *testing.T) {
	rig := newTestRig()
	defer rig.shutdown()
	tCore := rig.core

	btcWallet, tBtcWallet := newTWallet(tBTC.ID)
	tCore.wallets[tBTC.ID] = btcWallet
	dcrWallet, tDcrWallet := newTWallet(tDCR.ID)
	tCore.wallets[tDCR.ID] = dcrWallet

	var rate uint64 = 1e8
	quoteConvertedLotSize := calc.BaseToQuote(rate, tDCR.LotSize)

	book := newBookie(tLogger, func() {})
	rig.dc.books[tDcrBtcMktName] = book

	sellNote := &msgjson.BookOrderNote{
		OrderNote: msgjson.OrderNote{
			OrderID: encode.RandomBytes(32),
		},
		TradeNote: msgjson.TradeNote{
			Side:     msgjson.SellOrderNum,
			Quantity: quoteConvertedLotSize * 10,
			Time:     uint64(time.Now().Unix()),
			Rate:     rate,
		},
	}

	buyNote := *sellNote
	buyNote.TradeNote.Quantity = tDCR.LotSize * 10
	buyNote.TradeNote.Side = msgjson.BuyOrderNum

	err := book.Sync(&msgjson.OrderBook{
		MarketID: tDcrBtcMktName,
		Seq:      1,
		Epoch:    1,
		Orders:   []*msgjson.BookOrderNote{sellNote, &buyNote},
	})
	if err != nil {
		t.Fatalf("Sync error: %v", err)
	}

	preSwap := &asset.PreSwap{
		Estimate: &asset.SwapEstimate{
			MaxFees:            1001,
			Lots:               5,
			RealisticBestCase:  15,
			RealisticWorstCase: 20,
			Locked:             25,
		},
	}

	tBtcWallet.preSwap = preSwap

	preRedeem := &asset.PreRedeem{
		Estimate: &asset.RedeemEstimate{
			RealisticBestCase:  15,
			RealisticWorstCase: 20,
		},
	}

	tDcrWallet.preRedeem = preRedeem

	form := &TradeForm{
		Host: tDexHost,
		Sell: false,
		// IsLimit: true,
		Base:  tDCR.ID,
		Quote: tBTC.ID,
		Qty:   quoteConvertedLotSize * 5,
		Rate:  rate,
	}
	preOrder, err := tCore.PreOrder(form)
	if err != nil {
		t.Fatalf("PreOrder market buy error: %v", err)
	}

	compUint64 := func(tag string, a, b uint64) {
		t.Helper()
		if a != b {
			t.Fatalf("%s: %d != %d", tag, a, b)
		}
	}

	est1, est2 := preSwap.Estimate, preOrder.Swap.Estimate
	compUint64("MaxFees", est1.MaxFees, est2.MaxFees)
	compUint64("RealisticWorstCase", est1.RealisticWorstCase, est2.RealisticWorstCase)
	compUint64("RealisticBestCase", est1.RealisticBestCase, est2.RealisticBestCase)
	compUint64("Locked", est1.Locked, est2.Locked)

	// Missing book is an error
	delete(rig.dc.books, tDcrBtcMktName)
	_, err = tCore.PreOrder(form)
	if err == nil {
		t.Fatalf("no error for market order with missing book")
	}
	rig.dc.books[tDcrBtcMktName] = book

	// Exercise the market sell path too.
	form.Sell = true
	_, err = tCore.PreOrder(form)
	if err != nil {
		t.Fatalf("PreOrder market sell error: %v", err)
	}

	// Market orders have to have a market to make estimates.
	book.Unbook(&msgjson.UnbookOrderNote{
		MarketID: tDcrBtcMktName,
		OrderID:  sellNote.OrderID,
	})
	book.Unbook(&msgjson.UnbookOrderNote{
		MarketID: tDcrBtcMktName,
		OrderID:  buyNote.OrderID,
	})
	_, err = tCore.PreOrder(form)
	if err == nil {
		t.Fatalf("no error for market order with empty market")
	}

	// Limit orders have no such restriction.
	form.IsLimit = true
	_, err = tCore.PreOrder(form)
	if err != nil {
		t.Fatalf("PreOrder limit sell error: %v", err)
	}

	// no DEX
	delete(tCore.conns, rig.dc.acct.host)
	_, err = tCore.PreOrder(form)
	if err == nil {
		t.Fatalf("no error for unknown DEX")
	}
	tCore.conns[rig.dc.acct.host] = rig.dc

	// no wallet
	delete(tCore.wallets, tDCR.ID)
	_, err = tCore.PreOrder(form)
	if err == nil {
		t.Fatalf("no error for missing wallet")
	}
	tCore.wallets[tDCR.ID] = dcrWallet

	// base wallet not connected
	dcrWallet.hookedUp = false
	tDcrWallet.connectErr = tErr
	_, err = tCore.PreOrder(form)
	if err == nil {
		t.Fatalf("no error for unconnected base wallet")
	}
	dcrWallet.hookedUp = true
	tDcrWallet.connectErr = nil

	// quote wallet not connected
	btcWallet.hookedUp = false
	tBtcWallet.connectErr = tErr
	_, err = tCore.PreOrder(form)
	if err == nil {
		t.Fatalf("no error for unconnected quote wallet")
	}
	btcWallet.hookedUp = true
	tBtcWallet.connectErr = nil

	// success again
	_, err = tCore.PreOrder(form)
	if err != nil {
		t.Fatalf("PreOrder error after fixing everything: %v", err)
	}

}
