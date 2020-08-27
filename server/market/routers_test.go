package market

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"sync"
	"testing"
	"time"

	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/encode"
	"decred.org/dcrdex/dex/msgjson"
	"decred.org/dcrdex/dex/order"
	ordertest "decred.org/dcrdex/dex/order/test"
	"decred.org/dcrdex/server/account"
	"decred.org/dcrdex/server/asset"
	"decred.org/dcrdex/server/book"
	"decred.org/dcrdex/server/comms"
	"decred.org/dcrdex/server/matcher"
	"decred.org/dcrdex/server/swap"
	"github.com/decred/dcrd/dcrec/secp256k1/v2"
	"github.com/decred/slog"
)

const (
	dummySize    = 50
	btcLotSize   = 100_000
	btcRateStep  = 1_000
	dcrLotSize   = 10_000_000
	dcrRateStep  = 100_000
	btcID        = 0
	dcrID        = 42
	btcAddr      = "18Zpft83eov56iESWuPpV8XFLJ1b8gMZy7"
	dcrAddr      = "DsYXjAK3UiTVN9js8v9G21iRbr2wPty7f12"
	mktName1     = "btc_ltc"
	mkt1BaseRate = 5e7
	mktName2     = "dcr_doge"
	mkt2BaseRate = 8e9
	mktName3     = "dcr_btc"
	mkt3BaseRate = 3e9

	clientPreimageDelay = 75 * time.Millisecond
)

var (
	oRig       *tOrderRig
	dummyError = fmt.Errorf("expected test error")
	testCtx    context.Context
	rig        *testRig
	mkt1       = &ordertest.Market{
		Base:    0, // BTC
		Quote:   2, // LTC
		LotSize: btcLotSize,
	}
	mkt2 = &ordertest.Market{
		Base:    42, // DCR
		Quote:   3,  // DOGE
		LotSize: dcrLotSize,
	}
	mkt3 = &ordertest.Market{
		Base:    42, // DCR
		Quote:   0,  // BTC
		LotSize: dcrLotSize,
	}
	buyer1 = &ordertest.Writer{
		Addr:   "LSdTvMHRm8sScqwCi6x9wzYQae8JeZhx6y", // LTC receiving address
		Acct:   ordertest.NextAccount(),
		Sell:   false,
		Market: mkt1,
	}
	seller1 = &ordertest.Writer{
		Addr:   "1A1zP1eP5QGefi2DMPTfTL5SLmv7DivfNa", // BTC
		Acct:   ordertest.NextAccount(),
		Sell:   true,
		Market: mkt1,
	}
	buyer2 = &ordertest.Writer{
		Addr:   "DsaAKsMvZ6HrqhmbhLjV9qVbPkkzF5daowT", // DCR
		Acct:   ordertest.NextAccount(),
		Sell:   false,
		Market: mkt2,
	}
	seller2 = &ordertest.Writer{
		Addr:   "DE53BHmWWEi4G5a3REEJsjMpgNTnzKT98a", // DOGE
		Acct:   ordertest.NextAccount(),
		Sell:   true,
		Market: mkt2,
	}
	buyer3 = &ordertest.Writer{
		Addr:   "1A1zP1eP5QGefi2DMPTfTL5SLmv7DivfNa", // BTC
		Acct:   ordertest.NextAccount(),
		Sell:   false,
		Market: mkt3,
	}
	seller3 = &ordertest.Writer{
		Addr:   "DsaAKsMvZ6HrqhmbhLjV9qVbPkkzF5daowT", // DCR
		Acct:   ordertest.NextAccount(),
		Sell:   true,
		Market: mkt3,
	}
)

func nowMs() time.Time {
	return time.Now().Truncate(time.Millisecond).UTC()
}

// The AuthManager handles client-related actions, including authorization and
// communications.
type TAuth struct {
	authErr            error
	sendsMtx           sync.Mutex
	sends              []*msgjson.Message
	piMtx              sync.Mutex
	preimagesByMsgID   map[uint64]order.Preimage
	preimagesByOrdID   map[string]order.Preimage
	handlePreimageDone chan struct{}
	suspensions        map[account.AccountID]bool
	canceledOrder      order.OrderID
	cancelOrder        order.OrderID
}

func (a *TAuth) Route(route string, handler func(account.AccountID, *msgjson.Message) *msgjson.Error) {
	log.Infof("Route for %s", route)
}
func (a *TAuth) Suspended(user account.AccountID) (found, suspended bool) {
	suspended, found = a.suspensions[user]
	return // TODO: test suspended account handling (no trades, just cancels)
}
func (a *TAuth) Auth(user account.AccountID, msg, sig []byte) error {
	//log.Infof("Auth for user %v", user)
	return a.authErr
}
func (a *TAuth) Sign(...msgjson.Signable) error { log.Info("Sign"); return nil }
func (a *TAuth) SendWhenConnected(user account.AccountID, msg *msgjson.Message, _ time.Duration, _ func()) {
	if err := a.Send(user, msg); err != nil {
		log.Debug(err)
	}
}
func (a *TAuth) Send(user account.AccountID, msg *msgjson.Message) error {
	msgTxt, _ := json.Marshal(msg)
	log.Infof("Send for user %v. Message: %v", user, string(msgTxt))
	a.sendsMtx.Lock()
	a.sends = append(a.sends, msg)
	a.sendsMtx.Unlock()

	a.piMtx.Lock()
	defer a.piMtx.Unlock()
	preimage, ok := a.preimagesByMsgID[msg.ID]
	if ok && msg.Type == msgjson.Response {
		log.Infof("preimage found for msg id %v: %x", msg.ID, preimage)
		payload, err := msg.Response()
		if err != nil {
			return fmt.Errorf("Failed to unmarshal message ResponsePayload: %v", err)
		}
		if payload.Error != nil {
			return fmt.Errorf("invalid response: %v", payload.Error.Message)
		}
		ordRes := new(msgjson.OrderResult)
		err = json.Unmarshal(payload.Result, ordRes)
		if err != nil {
			return fmt.Errorf("Failed to unmarshal message Payload into OrderResult: %v", err)
		}
		log.Debugf("setting preimage for order %v", ordRes.OrderID)
		a.preimagesByOrdID[ordRes.OrderID.String()] = preimage
	}
	return nil
}
func (a *TAuth) getSend() *msgjson.Message {
	a.sendsMtx.Lock()
	defer a.sendsMtx.Unlock()
	if len(a.sends) == 0 {
		return nil
	}
	msg := a.sends[0]
	a.sends = a.sends[1:]
	return msg
}
func (a *TAuth) Request(user account.AccountID, msg *msgjson.Message, f func(comms.Link, *msgjson.Message)) error {
	return a.RequestWithTimeout(user, msg, f, time.Hour, func() {})
}
func (a *TAuth) RequestWhenConnected(user account.AccountID, req *msgjson.Message, handlerFunc func(comms.Link, *msgjson.Message),
	expireTimeout, connectTimeout time.Duration, expireFunc func()) {
	// TODO
	a.RequestWithTimeout(user, req, handlerFunc, expireTimeout, expireFunc)
}
func (a *TAuth) RequestWithTimeout(user account.AccountID, msg *msgjson.Message, f func(comms.Link, *msgjson.Message), expDur time.Duration, exp func()) error {
	log.Infof("Request for user %v", user)
	// Emulate the client.
	if msg.Route == msgjson.PreimageRoute {
		// Respond with the preimage for the referenced order id in the
		// PreimageRequest.
		var piReq msgjson.PreimageRequest
		json.Unmarshal(msg.Payload, &piReq)
		log.Info("order id:", piReq.OrderID.String())
		a.piMtx.Lock()
		pi, found := a.preimagesByOrdID[piReq.OrderID.String()]
		a.piMtx.Unlock()
		if !found {
			// If we have no preimage for this order, then we've decided to
			// expire the response after the expire duration.
			time.AfterFunc(expDur, exp)
		}
		log.Infof("found preimage: %x", pi)
		piMsg := &msgjson.PreimageResponse{
			Preimage: pi[:],
		}
		resp, _ := msgjson.NewResponse(5, piMsg, nil)
		go func() {
			// Simulate network latency before handling the response.
			time.Sleep(clientPreimageDelay)
			f(nil, resp)
			if a.handlePreimageDone != nil { // this tests wants to know when this is done
				a.handlePreimageDone <- struct{}{}
			}
		}()
	}
	return nil
}
func (a *TAuth) Penalize(user account.AccountID, rule account.Rule) error {
	log.Infof("Penalize for user %v", user)
	return nil
}

func (a *TAuth) RecordCompletedOrder(account.AccountID, order.OrderID, time.Time) {}
func (a *TAuth) RecordCancel(aid account.AccountID, coid, oid order.OrderID, t time.Time) {
	a.cancelOrder = coid
	a.canceledOrder = oid
}
func (a *TAuth) Unban(account.AccountID) error { return nil }

type TMarketTunnel struct {
	adds       []*orderRecord
	auth       *TAuth
	midGap     uint64
	mbBuffer   float64
	epochIdx   uint64
	epochDur   uint64
	locked     bool
	cancelable bool
}

func (m *TMarketTunnel) SubmitOrder(o *orderRecord) error {
	// set the server time
	now := nowMs()
	o.order.SetTime(now)

	m.adds = append(m.adds, o)

	// Send the order, but skip the signature
	oid := o.order.ID()
	resp, _ := msgjson.NewResponse(1, &msgjson.OrderResult{
		Sig:        msgjson.Bytes{},
		OrderID:    oid[:],
		ServerTime: encode.UnixMilliU(now),
	}, nil)
	err := m.auth.Send(account.AccountID{}, resp)
	if err != nil {
		log.Debug("Send:", err)
	}

	return nil
}

func (m *TMarketTunnel) MidGap() uint64 {
	return m.midGap
}

func (m *TMarketTunnel) CoinLocked(assetID uint32, coinid order.CoinID) bool {
	return m.locked
}

func (m *TMarketTunnel) MarketBuyBuffer() float64 {
	return m.mbBuffer
}

func (m *TMarketTunnel) pop() *orderRecord {
	if len(m.adds) == 0 {
		return nil
	}
	o := m.adds[0]
	m.adds = m.adds[1:]
	return o
}

func (m *TMarketTunnel) Cancelable(order.OrderID) bool {
	return m.cancelable
}

func (m *TMarketTunnel) Suspend(asSoonAs time.Time, persistBook bool) (finalEpochIdx int64, finalEpochEnd time.Time) {
	// no suspension
	return -1, time.Time{}
}

func (m *TMarketTunnel) Running() bool {
	return true
}

type TBackend struct {
	utxoErr    error
	utxos      map[string]uint64
	addrChecks bool
}

func tNewBackend() *TBackend {
	return &TBackend{
		utxos:      make(map[string]uint64),
		addrChecks: true,
	}
}

func (b *TBackend) utxo(coinID []byte) (*tUTXO, error) {
	str := hex.EncodeToString(coinID)
	v := b.utxos[str]
	if v == 0 {
		return nil, fmt.Errorf("no utxo")
	}
	return &tUTXO{val: v, decoded: str}, b.utxoErr
}
func (b *TBackend) Contract(coinID, redeemScript []byte) (asset.Contract, error) {
	return b.utxo(coinID)
}
func (b *TBackend) FundingCoin(coinID, redeemScript []byte) (asset.FundingCoin, error) {
	return b.utxo(coinID)
}
func (b *TBackend) Redemption(redemptionID, contractID []byte) (asset.Coin, error) {
	return b.utxo(redemptionID)
}
func (b *TBackend) BlockChannel(size int) <-chan *asset.BlockUpdate { return nil }
func (b *TBackend) InitTxSize() uint32                              { return dummySize }
func (b *TBackend) InitTxSizeBase() uint32                          { return dummySize / 2 }
func (b *TBackend) CheckAddress(string) bool                        { return b.addrChecks }
func (b *TBackend) addUTXO(coin *msgjson.Coin, val uint64) {
	b.utxos[hex.EncodeToString(coin.ID)] = val
}
func (b *TBackend) Run(context.Context) {}
func (b *TBackend) ValidateCoinID(coinID []byte) (string, error) {
	return "", nil
}
func (b *TBackend) ValidateContract(contract []byte) error {
	return nil
}

func (b *TBackend) ValidateSecret(secret, contract []byte) bool { return true }
func (b *TBackend) VerifyUnspentCoin(coinID []byte) error {
	_, err := b.utxo(coinID)
	return err
}
func (b *TBackend) FeeRate() (uint64, error) {
	return 9, nil
}

type tUTXO struct {
	val     uint64
	decoded string
}

var utxoAuthErr error
var utxoConfsErr error
var utxoConfs int64 = 2

func (u *tUTXO) Confirmations() (int64, error) { return utxoConfs, utxoConfsErr }
func (u *tUTXO) Auth(pubkeys, sigs [][]byte, msg []byte) error {
	return utxoAuthErr
}
func (u *tUTXO) SwapAddress() string             { return "" }
func (u *tUTXO) SpendSize() uint32               { return dummySize }
func (u *tUTXO) ID() []byte                      { return nil }
func (u *tUTXO) TxID() string                    { return "" }
func (u *tUTXO) String() string                  { return u.decoded }
func (u *tUTXO) SpendsCoin([]byte) (bool, error) { return true, nil }
func (u *tUTXO) Value() uint64                   { return u.val }
func (u *tUTXO) FeeRate() uint64                 { return 0 }
func (u *tUTXO) RedeemScript() []byte            { return nil }
func (u *tUTXO) LockTime() time.Time             { return time.Time{} }

type tUser struct {
	acct    account.AccountID
	privKey *secp256k1.PrivateKey
}

type tOrderRig struct {
	btc    *TBackend
	dcr    *TBackend
	user   *tUser
	auth   *TAuth
	market *TMarketTunnel
	router *OrderRouter
}

func (rig *tOrderRig) signedUTXO(id int, val uint64, numSigs int) *msgjson.Coin {
	u := rig.user
	coin := &msgjson.Coin{
		ID: randomBytes(36),
	}
	pk := u.privKey.PubKey().SerializeCompressed()
	for i := 0; i < numSigs; i++ {
		sig, _ := u.privKey.Sign(coin.ID)
		coin.Sigs = append(coin.Sigs, sig.Serialize())
		coin.PubKeys = append(coin.PubKeys, pk)
	}
	switch id {
	case btcID:
		rig.btc.addUTXO(coin, val)
	case dcrID:
		rig.dcr.addUTXO(coin, val)
	}
	return coin
}

var assetBTC = &asset.BackedAsset{
	Asset: dex.Asset{
		ID:           0,
		Symbol:       "btc",
		LotSize:      btcLotSize,
		RateStep:     btcRateStep,
		MaxFeeRate:   14,
		SwapSize:     dummySize,
		SwapSizeBase: dummySize / 2,
		SwapConf:     2,
	},
}

var assetDCR = &asset.BackedAsset{
	Asset: dex.Asset{
		ID:           42,
		Symbol:       "dcr",
		LotSize:      dcrLotSize,
		RateStep:     dcrRateStep,
		MaxFeeRate:   10,
		SwapSize:     dummySize,
		SwapSizeBase: dummySize / 2,
		SwapConf:     2,
	},
}

var assetUnknown = &asset.BackedAsset{
	Asset: dex.Asset{
		ID:           54321,
		Symbol:       "buk",
		LotSize:      1000,
		RateStep:     100,
		MaxFeeRate:   10,
		SwapSize:     2,
		SwapSizeBase: 1,
		SwapConf:     0,
	},
}

func randomBytes(len int) []byte {
	bytes := make([]byte, len)
	rand.Read(bytes)
	return bytes
}

func makeEnsureErr(t *testing.T) func(tag string, rpcErr *msgjson.Error, code int) {
	return func(tag string, rpcErr *msgjson.Error, code int) {
		t.Helper()
		if rpcErr == nil {
			if code == -1 {
				return
			}
			t.Fatalf("%s: no rpc error for code %d", tag, code)
		}
		if rpcErr.Code != code {
			t.Fatalf("%s: wrong error code. expected %d, got %d: %s", tag, code, rpcErr.Code, rpcErr.Message)
		}
	}
}

func TestMain(m *testing.M) {
	logger := slog.NewBackend(os.Stdout).Logger("MARKETTEST")
	logger.SetLevel(slog.LevelDebug)
	UseLogger(logger)
	book.UseLogger(logger)
	matcher.UseLogger(logger)
	swap.UseLogger(logger)

	privKey, _ := secp256k1.GeneratePrivateKey()
	auth := &TAuth{
		sends:            make([]*msgjson.Message, 0),
		preimagesByMsgID: make(map[uint64]order.Preimage),
		preimagesByOrdID: make(map[string]order.Preimage),
	}
	oRig = &tOrderRig{
		btc: tNewBackend(),
		dcr: tNewBackend(),
		user: &tUser{
			acct:    ordertest.NextAccount(),
			privKey: privKey,
		},
		auth: auth,
		market: &TMarketTunnel{
			adds:       make([]*orderRecord, 0),
			auth:       auth,
			midGap:     dcrRateStep * 1000,
			mbBuffer:   1.5, // 150% of lot size
			cancelable: true,
			epochIdx:   1573773894,
			epochDur:   60_000,
		},
	}
	assetDCR.Backend = oRig.dcr
	assetBTC.Backend = oRig.btc
	oRig.router = NewOrderRouter(&OrderRouterConfig{
		AuthManager: oRig.auth,
		Assets: map[uint32]*asset.BackedAsset{
			0:  assetBTC,
			42: assetDCR,
		},
		Markets: map[string]MarketTunnel{"dcr_btc": oRig.market},
	})
	rig = newTestRig()
	src1 := rig.source1
	src2 := rig.source2
	src3 := rig.source3
	// Load up the order books up with 16 orders each.
	for i := 0; i < 8; i++ {
		src1.sells = append(src1.sells,
			makeLO(seller1, mkRate1(1.0, 1.2), randLots(10), order.StandingTiF))
		src1.buys = append(src1.buys,
			makeLO(buyer1, mkRate1(0.8, 1.0), randLots(10), order.StandingTiF))
		src2.sells = append(src2.sells,
			makeLO(seller2, mkRate2(1.0, 1.2), randLots(10), order.StandingTiF))
		src2.buys = append(src2.buys,
			makeLO(buyer2, mkRate2(0.8, 1.0), randLots(10), order.StandingTiF))
		src3.sells = append(src3.sells,
			makeLO(seller3, mkRate3(1.0, 1.2), randLots(10), order.StandingTiF))
		src3.buys = append(src3.buys,
			makeLO(buyer3, mkRate3(0.8, 1.0), randLots(10), order.StandingTiF))
	}
	tick(100)
	doIt := func() int {
		// Not counted as coverage, must test Archiver constructor explicitly.
		var shutdown context.CancelFunc
		testCtx, shutdown = context.WithCancel(context.Background())
		rig.router = NewBookRouter(rig.sources())
		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			rig.router.Run(testCtx)
			wg.Done()
		}()
		time.Sleep(100 * time.Millisecond) // let the router actually start in runBook
		defer func() {
			shutdown()
			wg.Wait()
		}()
		return m.Run()
	}
	os.Exit(doIt())
}

// Order router tests

func TestLimit(t *testing.T) {
	qty := uint64(dcrLotSize) * 10
	rate := uint64(1000) * dcrRateStep
	user := oRig.user
	clientTime := nowMs()
	pi := ordertest.RandomPreimage()
	commit := pi.Commit()
	limit := msgjson.LimitOrder{
		Prefix: msgjson.Prefix{
			AccountID:  user.acct[:],
			Base:       dcrID,
			Quote:      btcID,
			OrderType:  msgjson.LimitOrderNum,
			ClientTime: encode.UnixMilliU(clientTime),
			Commit:     commit[:],
		},
		Trade: msgjson.Trade{
			Side:     msgjson.SellOrderNum,
			Quantity: qty,
			Coins: []*msgjson.Coin{
				oRig.signedUTXO(dcrID, qty-dcrLotSize, 1),
				oRig.signedUTXO(dcrID, 2*dcrLotSize, 2),
			},
			Address: btcAddr,
		},
		Rate: rate,
		TiF:  msgjson.StandingOrderNum,
	}
	reqID := uint64(5)

	ensureErr := makeEnsureErr(t)

	sendLimit := func() *msgjson.Error {
		msg, _ := msgjson.NewRequest(reqID, msgjson.LimitRoute, limit)
		return oRig.router.handleLimit(user.acct, msg)
	}

	// First just send it through and ensure there are no errors.
	ensureErr("valid order", sendLimit(), -1)
	// Make sure the order was submitted to the market
	oRecord := oRig.market.pop()
	if oRecord == nil {
		t.Fatalf("no order submitted to epoch")
	}

	// Check TiF
	epochOrder := oRecord.order.(*order.LimitOrder)
	if epochOrder.Force != order.StandingTiF {
		t.Errorf("Got force %v, expected %v (standing)", epochOrder.Force, order.StandingTiF)
	}

	// Now check with immediate TiF.
	limit.TiF = msgjson.ImmediateOrderNum
	ensureErr("valid order", sendLimit(), -1)
	oRecord = oRig.market.pop()
	if oRecord == nil {
		t.Fatalf("no order submitted to epoch")
	}
	epochOrder = oRecord.order.(*order.LimitOrder)
	if epochOrder.Force != order.ImmediateTiF {
		t.Errorf("Got force %v, expected %v (immediate)", epochOrder.Force, order.ImmediateTiF)
	}

	// Test an invalid payload.
	msg := new(msgjson.Message)
	msg.Payload = []byte(`?`)
	rpcErr := oRig.router.handleLimit(user.acct, msg)
	ensureErr("bad payload", rpcErr, msgjson.RPCParseError)

	// Wrong order type marked for limit order
	limit.OrderType = msgjson.MarketOrderNum
	ensureErr("wrong order type", sendLimit(), msgjson.OrderParameterError)
	limit.OrderType = msgjson.LimitOrderNum

	testPrefixTrade(&limit.Prefix, &limit.Trade, oRig.dcr, oRig.btc,
		func(tag string, code int) { ensureErr(tag, sendLimit(), code) },
	)

	// Rate = 0
	limit.Rate = 0
	ensureErr("zero rate", sendLimit(), msgjson.OrderParameterError)
	limit.Rate = rate

	// non-step-multiple rate
	limit.Rate = rate + (btcRateStep / 2)
	ensureErr("non-step-multiple", sendLimit(), msgjson.OrderParameterError)
	limit.Rate = rate

	// Time-in-force incorrectly marked
	limit.TiF = 0 // not msgjson.StandingOrderNum (1) or msgjson.ImmediateOrderNum (2)
	ensureErr("bad tif", sendLimit(), msgjson.OrderParameterError)
	limit.TiF = msgjson.StandingOrderNum

	// Now switch it to a buy order, and ensure it passes
	// Clear the sends cache first.
	oRig.auth.sends = nil
	limit.Side = msgjson.BuyOrderNum
	buyUTXO := oRig.signedUTXO(btcID, matcher.BaseToQuote(rate, qty*2), 1)
	limit.Coins = []*msgjson.Coin{
		buyUTXO,
	}
	limit.Address = dcrAddr
	rpcErr = sendLimit()
	if rpcErr != nil {
		t.Fatalf("error for buy order: %s", rpcErr.Message)
	}

	// Create the order manually, so that we can compare the IDs as another check
	// of equivalence.
	lo := &order.LimitOrder{
		P: order.Prefix{
			AccountID:  user.acct,
			BaseAsset:  limit.Base,
			QuoteAsset: limit.Quote,
			OrderType:  order.LimitOrderType,
			ClientTime: clientTime,
			Commit:     commit,
		},
		T: order.Trade{
			Sell:     false,
			Quantity: qty,
			Address:  dcrAddr,
		},
		Rate:  rate,
		Force: order.StandingTiF,
	}
	// Get the last order submitted to the epoch
	oRecord = oRig.market.pop()
	if oRecord == nil {
		t.Fatalf("no buy order submitted to epoch")
	}

	// Check the utxo
	epochOrder = oRecord.order.(*order.LimitOrder)
	if len(epochOrder.Coins) != 1 {
		t.Fatalf("expected 1 order UTXO, got %d", len(epochOrder.Coins))
	}
	epochUTXO := epochOrder.Coins[0]
	if !bytes.Equal(epochUTXO, buyUTXO.ID) {
		t.Fatalf("utxo reporting wrong txid")
	}

	// Now steal the Coins
	lo.Coins = epochOrder.Coins

	// Get the server time from the response.
	respMsg := oRig.auth.getSend()
	if respMsg == nil {
		t.Fatalf("no response from limit order")
	}
	resp, _ := respMsg.Response()
	result := new(msgjson.OrderResult)
	err := json.Unmarshal(resp.Result, result)
	if err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	lo.ServerTime = encode.UnixTimeMilli(int64(result.ServerTime))

	// Check equivalence of IDs.
	if epochOrder.ID() != lo.ID() {
		t.Fatalf("failed to duplicate ID, got %v, wanted %v", lo.UID(), epochOrder.UID())
	}
}

func TestMarketStartProcessStop(t *testing.T) {
	qty := uint64(dcrLotSize) * 10
	user := oRig.user
	clientTime := nowMs()
	pi := ordertest.RandomPreimage()
	commit := pi.Commit()
	mkt := msgjson.MarketOrder{
		Prefix: msgjson.Prefix{
			AccountID:  user.acct[:],
			Base:       dcrID,
			Quote:      btcID,
			OrderType:  msgjson.MarketOrderNum,
			ClientTime: encode.UnixMilliU(clientTime),
			Commit:     commit[:],
		},
		Trade: msgjson.Trade{
			Side:     msgjson.SellOrderNum,
			Quantity: qty,
			Coins: []*msgjson.Coin{
				oRig.signedUTXO(dcrID, qty-dcrLotSize, 1),
				oRig.signedUTXO(dcrID, 2*dcrLotSize, 2),
			},
			Address: btcAddr,
		},
	}
	reqID := uint64(5)

	ensureErr := makeEnsureErr(t)

	sendMarket := func() *msgjson.Error {
		msg, _ := msgjson.NewRequest(reqID, msgjson.MarketRoute, mkt)
		return oRig.router.handleMarket(user.acct, msg)
	}

	// First just send it through and ensure there are no errors.
	ensureErr("valid order", sendMarket(), -1)

	// Make sure the order was submitted to the market
	o := oRig.market.pop()
	if o == nil {
		t.Fatalf("no order submitted to epoch")
	}

	// Test an invalid payload.
	msg := new(msgjson.Message)
	msg.Payload = []byte(`?`)
	rpcErr := oRig.router.handleMarket(user.acct, msg)
	ensureErr("bad payload", rpcErr, msgjson.RPCParseError)

	// Wrong order type marked for market order
	mkt.OrderType = msgjson.LimitOrderNum
	ensureErr("wrong order type", sendMarket(), msgjson.OrderParameterError)
	mkt.OrderType = msgjson.MarketOrderNum

	testPrefixTrade(&mkt.Prefix, &mkt.Trade, oRig.dcr, oRig.btc,
		func(tag string, code int) { ensureErr(tag, sendMarket(), code) },
	)

	// Now switch it to a buy order, and ensure it passes
	// Clear the sends cache first.
	oRig.auth.sends = nil
	mkt.Side = msgjson.BuyOrderNum

	midGap := oRig.market.MidGap()
	buyUTXO := oRig.signedUTXO(btcID, matcher.BaseToQuote(midGap, qty), 1)
	mkt.Coins = []*msgjson.Coin{
		buyUTXO,
	}
	mkt.Address = dcrAddr
	mkt.Quantity = matcher.BaseToQuote(midGap, uint64(dcrLotSize*1.2))
	// First check an order that doesn't satisfy the market buy buffer. For
	// testing, the market buy buffer is set to 1.5.
	ensureErr("market buy buffer unsatisfied", sendMarket(), msgjson.FundingError)
	mktBuyQty := matcher.BaseToQuote(midGap, uint64(dcrLotSize*1.6))
	mkt.Quantity = mktBuyQty
	rpcErr = sendMarket()
	if rpcErr != nil {
		t.Fatalf("error for buy order: %s", rpcErr.Message)
	}

	// Create the order manually, so that we can compare the IDs as another check
	// of equivalence.
	mo := &order.MarketOrder{
		P: order.Prefix{
			AccountID:  user.acct,
			BaseAsset:  mkt.Base,
			QuoteAsset: mkt.Quote,
			OrderType:  order.MarketOrderType,
			ClientTime: clientTime,
			Commit:     commit,
		},
		T: order.Trade{
			Sell:     false,
			Quantity: mktBuyQty,
			Address:  dcrAddr,
		},
	}

	// Get the last order submitted to the epoch
	oRecord := oRig.market.pop()
	if oRecord == nil {
		t.Fatalf("no buy order submitted to epoch")
	}

	// Check the utxo
	epochOrder := oRecord.order.(*order.MarketOrder)
	if len(epochOrder.Coins) != 1 {
		t.Fatalf("expected 1 order UTXO, got %d", len(epochOrder.Coins))
	}
	epochUTXO := epochOrder.Coins[0]
	if !bytes.Equal(epochUTXO, buyUTXO.ID) {
		t.Fatalf("utxo reporting wrong txid")
	}

	// Now steal the Coins
	mo.Coins = epochOrder.Coins

	// Get the server time from the response.
	respMsg := oRig.auth.getSend()
	if respMsg == nil {
		t.Fatalf("no response from market order")
	}
	resp, _ := respMsg.Response()
	result := new(msgjson.OrderResult)
	err := json.Unmarshal(resp.Result, result)
	if err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	mo.ServerTime = encode.UnixTimeMilli(int64(result.ServerTime))

	// Check equivalence of IDs.
	if epochOrder.ID() != mo.ID() {
		t.Fatalf("failed to duplicate ID")
	}
}

func TestCancel(t *testing.T) {
	user := oRig.user
	targetID := order.OrderID{244}
	clientTime := nowMs()
	pi := ordertest.RandomPreimage()
	commit := pi.Commit()
	cancel := msgjson.CancelOrder{
		Prefix: msgjson.Prefix{
			AccountID:  user.acct[:],
			Base:       dcrID,
			Quote:      btcID,
			OrderType:  msgjson.CancelOrderNum,
			ClientTime: encode.UnixMilliU(clientTime),
			Commit:     commit[:],
		},
		TargetID: targetID[:],
	}
	reqID := uint64(5)

	ensureErr := makeEnsureErr(t)

	sendCancel := func() *msgjson.Error {
		msg, _ := msgjson.NewRequest(reqID, msgjson.CancelRoute, cancel)
		return oRig.router.handleCancel(user.acct, msg)
	}

	// First just send it through and ensure there are no errors.
	ensureErr("valid order", sendCancel(), -1)
	// Make sure the order was submitted to the market
	oRecord := oRig.market.pop()
	if oRecord == nil {
		t.Fatalf("no order submitted to epoch")
	}

	// Test an invalid payload.
	msg := new(msgjson.Message)
	msg.Payload = []byte(`?`)
	rpcErr := oRig.router.handleCancel(user.acct, msg)
	ensureErr("bad payload", rpcErr, msgjson.RPCParseError)

	// Unknown order.
	oRig.market.cancelable = false
	ensureErr("non cancelable", sendCancel(), msgjson.OrderParameterError)
	oRig.market.cancelable = true

	// Wrong order type marked for cancel order
	cancel.OrderType = msgjson.LimitOrderNum
	ensureErr("wrong order type", sendCancel(), msgjson.OrderParameterError)
	cancel.OrderType = msgjson.CancelOrderNum

	testPrefix(&cancel.Prefix, func(tag string, code int) {
		ensureErr(tag, sendCancel(), code)
	})

	// Test a short order ID.
	badID := []byte{0x01, 0x02}
	cancel.TargetID = badID
	ensureErr("bad target ID", sendCancel(), msgjson.OrderParameterError)
	cancel.TargetID = targetID[:]

	// Clear the sends cache.
	oRig.auth.sends = nil

	// Create the order manually, so that we can compare the IDs as another check
	// of equivalence.
	co := &order.CancelOrder{
		P: order.Prefix{
			AccountID:  user.acct,
			BaseAsset:  cancel.Base,
			QuoteAsset: cancel.Quote,
			OrderType:  order.CancelOrderType,
			ClientTime: clientTime,
			Commit:     commit,
		},
		TargetOrderID: targetID,
	}
	// Send the order through again, so we can grab it from the epoch.
	rpcErr = sendCancel()
	if rpcErr != nil {
		t.Fatalf("error for valid order (after prefix testing): %s", rpcErr.Message)
	}
	oRecord = oRig.market.pop()
	if oRecord == nil {
		t.Fatalf("no cancel order submitted to epoch")
	}

	// Check the utxo
	epochOrder := oRecord.order.(*order.CancelOrder)

	// Get the server time from the response.
	respMsg := oRig.auth.getSend()
	if respMsg == nil {
		t.Fatalf("no response from market order")
	}
	resp, _ := respMsg.Response()
	result := new(msgjson.OrderResult)
	err := json.Unmarshal(resp.Result, result)
	if err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	co.ServerTime = encode.UnixTimeMilli(int64(result.ServerTime))

	// Check equivalence of IDs.
	if epochOrder.ID() != co.ID() {
		t.Fatalf("failed to duplicate ID: %v != %v", epochOrder.ID(), co.ID())
	}
}

func testPrefix(prefix *msgjson.Prefix, checkCode func(string, int)) {
	ogAcct := prefix.AccountID
	oid := ordertest.NextAccount()
	prefix.AccountID = oid[:]
	checkCode("bad account", msgjson.OrderParameterError)
	prefix.AccountID = ogAcct

	// Signature error
	oRig.auth.authErr = dummyError
	checkCode("bad order sig", msgjson.SignatureError)
	oRig.auth.authErr = nil

	// Unknown asset
	prefix.Base = assetUnknown.ID
	checkCode("unknown asset", msgjson.UnknownMarketError)

	// Unknown market. 1 is BIP0044 testnet designator, which a "known" asset,
	// but with no markets.
	prefix.Base = 1
	checkCode("unknown market", msgjson.UnknownMarketError)
	prefix.Base = assetDCR.ID

	// Too old
	ct := prefix.ClientTime
	prefix.ClientTime = ct - maxClockOffset - 1 // offset >= maxClockOffset
	checkCode("too old", msgjson.ClockRangeError)
	prefix.ClientTime = ct

	// Set server time = bad
	prefix.ServerTime = 1
	checkCode("server time set", msgjson.OrderParameterError)
	prefix.ServerTime = 0
}

func testPrefixTrade(prefix *msgjson.Prefix, trade *msgjson.Trade, fundingAsset, receivingAsset *TBackend, checkCode func(string, int)) {
	// Wrong account ID
	testPrefix(prefix, checkCode)

	// Invalid side number
	trade.Side = 100
	checkCode("bad side num", msgjson.OrderParameterError)
	trade.Side = msgjson.SellOrderNum

	// Zero quantity
	qty := trade.Quantity
	trade.Quantity = 0
	checkCode("zero quantity", msgjson.OrderParameterError)

	// non-lot-multiple
	trade.Quantity = qty + (dcrLotSize / 2)
	checkCode("non-lot-multiple", msgjson.OrderParameterError)
	trade.Quantity = qty

	// No utxos
	ogUTXOs := trade.Coins
	trade.Coins = nil
	checkCode("no utxos", msgjson.FundingError)
	trade.Coins = ogUTXOs

	// No signatures
	utxo1 := trade.Coins[1]
	ogSigs := utxo1.Sigs
	utxo1.Sigs = nil
	checkCode("no utxo sigs", msgjson.SignatureError)

	// Different number of signatures than pubkeys
	utxo1.Sigs = ogSigs[:1]
	checkCode("not enough sigs", msgjson.OrderParameterError)
	utxo1.Sigs = ogSigs

	// output is locked
	oRig.market.locked = true
	checkCode("output locked", msgjson.FundingError)
	oRig.market.locked = false

	// utxo err
	fundingAsset.utxoErr = dummyError
	checkCode("utxo err", msgjson.FundingError)
	fundingAsset.utxoErr = nil

	// UTXO Auth error
	utxoAuthErr = dummyError
	checkCode("utxo auth error", msgjson.CoinAuthError)
	utxoAuthErr = nil

	// Clear the order from the epoch.
	oRig.market.pop()

	// Not enough funding
	trade.Coins = ogUTXOs[:1]
	checkCode("unfunded", msgjson.FundingError)
	trade.Coins = ogUTXOs

	// Invalid address
	receivingAsset.addrChecks = false
	checkCode("bad address", msgjson.OrderParameterError)
	receivingAsset.addrChecks = true
}

// Book Router Tests

// nolint:unparm
func randLots(max int) uint64 {
	return uint64(rand.Intn(max) + 1)
}

func randRate(baseRate, lotSize uint64, min, max float64) uint64 {
	multiplier := rand.Float64()*(max-min) + min
	rate := uint64(multiplier * float64(baseRate))
	return rate - rate%lotSize
}

func makeLO(writer *ordertest.Writer, rate, lots uint64, force order.TimeInForce) *order.LimitOrder {
	lo, _ := ordertest.WriteLimitOrder(writer, rate, lots, force, 0)
	return lo
}

func makeLORevealed(writer *ordertest.Writer, rate, lots uint64, force order.TimeInForce) (*order.LimitOrder, order.Preimage) {
	return ordertest.WriteLimitOrder(writer, rate, lots, force, 0)
}

func makeMO(writer *ordertest.Writer, lots uint64) *order.MarketOrder {
	mo, _ := ordertest.WriteMarketOrder(writer, lots, 0)
	return mo
}

func makeMORevealed(writer *ordertest.Writer, lots uint64) (*order.MarketOrder, order.Preimage) {
	return ordertest.WriteMarketOrder(writer, lots, 0)
}

func makeCO(writer *ordertest.Writer, targetID order.OrderID) *order.CancelOrder {
	co, _ := ordertest.WriteCancelOrder(writer, targetID, 0)
	return co
}

func makeCORevealed(writer *ordertest.Writer, targetID order.OrderID) (*order.CancelOrder, order.Preimage) {
	return ordertest.WriteCancelOrder(writer, targetID, 0)
}

type TBookSource struct {
	buys  []*order.LimitOrder
	sells []*order.LimitOrder
	feed  chan *updateSignal
}

func tNewBookSource() *TBookSource {
	return &TBookSource{
		feed: make(chan *updateSignal, 16),
	}
}

func (s *TBookSource) Book() (eidx int64, buys []*order.LimitOrder, sells []*order.LimitOrder) {
	return 13241324, s.buys, s.sells
}
func (s *TBookSource) OrderFeed() <-chan *updateSignal {
	return s.feed
}

type TLink struct {
	mtx         sync.Mutex
	id          uint64
	ip          string
	sends       []*msgjson.Message
	sendErr     error
	sendTrigger chan struct{}
	banished    bool
}

var linkCounter uint64

func tNewLink() *TLink {
	linkCounter++
	return &TLink{
		id:          linkCounter,
		ip:          "[1:800:dex:rules::]",
		sends:       make([]*msgjson.Message, 0),
		sendTrigger: make(chan struct{}, 1),
	}
}

func (conn *TLink) ID() uint64 { return conn.id }
func (conn *TLink) IP() string { return conn.ip }
func (conn *TLink) Send(msg *msgjson.Message) error {
	conn.mtx.Lock()
	defer conn.mtx.Unlock()
	if conn.sendErr != nil {
		return conn.sendErr
	}
	conn.sends = append(conn.sends, msg)
	conn.sendTrigger <- struct{}{}
	return nil
}
func (conn *TLink) SendError(id uint64, msgErr *msgjson.Error) {
	msg, err := msgjson.NewResponse(id, nil, msgErr)
	if err != nil {
		log.Errorf("SendError: failed to create message: %v", err)
	}
	conn.mtx.Lock()
	defer conn.mtx.Unlock()
	conn.sends = append(conn.sends, msg)
	conn.sendTrigger <- struct{}{}
}

func (conn *TLink) getSend() *msgjson.Message {
	select {
	case <-conn.sendTrigger:
	case <-time.NewTimer(2 * time.Second).C:
		panic("no send")
	}

	conn.mtx.Lock()
	defer conn.mtx.Unlock()
	if len(conn.sends) == 0 {
		return nil
	}
	s := conn.sends[0]
	conn.sends = conn.sends[1:]
	return s
}

// There are no requests in the routers.
func (conn *TLink) Request(msg *msgjson.Message, f func(comms.Link, *msgjson.Message), expDur time.Duration, exp func()) error {
	return nil
}
func (conn *TLink) Banish() {
	conn.banished = true
}
func (conn *TLink) Disconnect() {}

type testRig struct {
	router  *BookRouter
	source1 *TBookSource // btc_ltc
	source2 *TBookSource // dcr_doge
	source3 *TBookSource // dcr_btc
}

func newTestRig() *testRig {
	src1 := tNewBookSource()
	src2 := tNewBookSource()
	src3 := tNewBookSource()
	return &testRig{
		source1: src1,
		source2: src2,
		source3: src3,
	}
}

func (rig *testRig) sources() map[string]BookSource {
	return map[string]BookSource{
		mktName1: rig.source1,
		mktName2: rig.source2,
		mktName3: rig.source3,
	}
}

func tick(d int) { time.Sleep(time.Duration(d) * time.Millisecond) }

func newSubscription(mkt *ordertest.Market) *msgjson.Message {
	msg, _ := msgjson.NewRequest(1, msgjson.OrderBookRoute, &msgjson.OrderBookSubscription{
		Base:  mkt.Base,
		Quote: mkt.Quote,
	})
	return msg
}

func newSubscriber(mkt *ordertest.Market) (*TLink, *msgjson.Message) {
	return tNewLink(), newSubscription(mkt)
}

func findOrder(id msgjson.Bytes, books ...[]*order.LimitOrder) *order.LimitOrder {
	for _, book := range books {
		for _, o := range book {
			if o.ID().String() == id.String() {
				return o
			}
		}
	}
	return nil
}

func getEpochNoteFromLink(t *testing.T, link *TLink) *msgjson.EpochOrderNote {
	t.Helper()
	noteMsg := link.getSend()
	if noteMsg == nil {
		t.Fatalf("no epoch notification sent")
	}
	epochNote := new(msgjson.EpochOrderNote)
	err := json.Unmarshal(noteMsg.Payload, epochNote)
	if err != nil {
		t.Fatalf("error unmarshaling epoch notification: %v", err)
	}
	return epochNote
}

func getBookNoteFromLink(t *testing.T, link *TLink) *msgjson.BookOrderNote {
	t.Helper()
	noteMsg := link.getSend()
	if noteMsg == nil {
		t.Fatalf("no epoch notification sent")
	}
	bookNote := new(msgjson.BookOrderNote)
	err := json.Unmarshal(noteMsg.Payload, bookNote)
	if err != nil {
		t.Fatalf("error unmarshaling epoch notification: %v", err)
	}
	return bookNote
}

func getUpdateRemainingNoteFromLink(t *testing.T, link *TLink) *msgjson.UpdateRemainingNote {
	t.Helper()
	noteMsg := link.getSend()
	if noteMsg == nil {
		t.Fatalf("no epoch notification sent")
	}
	urNote := new(msgjson.UpdateRemainingNote)
	err := json.Unmarshal(noteMsg.Payload, urNote)
	if err != nil {
		t.Fatalf("error unmarshaling epoch notification: %v", err)
	}
	return urNote
}

func getUnbookNoteFromLink(t *testing.T, link *TLink) *msgjson.UnbookOrderNote {
	t.Helper()
	noteMsg := link.getSend()
	if noteMsg == nil {
		t.Fatalf("no epoch notification sent")
	}
	unbookNote := new(msgjson.UnbookOrderNote)
	err := json.Unmarshal(noteMsg.Payload, unbookNote)
	if err != nil {
		t.Fatalf("error unmarshaling epoch notification: %v", err)
	}
	return unbookNote
}

func mkRate1(min, max float64) uint64 {
	return randRate(mkt1BaseRate, mkt1.LotSize, min, max)
}

func mkRate2(min, max float64) uint64 {
	return randRate(mkt2BaseRate, mkt2.LotSize, min, max)
}

func mkRate3(min, max float64) uint64 {
	return randRate(mkt3BaseRate, mkt3.LotSize, min, max)
}

func TestRouter(t *testing.T) {
	src1 := rig.source1
	src2 := rig.source2
	router := rig.router

	checkResponse := func(tag, mktName string, msgID uint64, conn *TLink) []*msgjson.BookOrderNote {
		t.Helper()
		respMsg := conn.getSend()
		if respMsg == nil {
			t.Fatalf("(%s): no response sent for subscription", tag)
		}
		if respMsg.ID != msgID {
			t.Fatalf("(%s): wrong ID for response. wanted %d, got %d", tag, msgID, respMsg.ID)
		}
		resp, err := respMsg.Response()
		if err != nil {
			t.Fatalf("(%s): error parsing response: %v", tag, err)
		}
		if resp.Error != nil {
			t.Fatalf("response is error: %v", resp.Error)
		}
		book := new(msgjson.OrderBook)
		err = json.Unmarshal(resp.Result, book)
		if err != nil {
			t.Fatalf("(%s): unmarshal error: %v", tag, err)
		}
		if len(book.Orders) != 16 {
			t.Fatalf("(%s): expected 16 orders, received %d", tag, len(book.Orders))
		}
		if book.MarketID != mktName {
			t.Fatalf("(%s): wrong market ID. expected %s, got %s", tag, mktName1, book.MarketID)
		}
		return book.Orders
	}

	findBookOrder := func(id msgjson.Bytes, src *TBookSource) *order.LimitOrder {
		t.Helper()
		return findOrder(id, src.buys, src.sells)
	}

	compareTrade := func(msgOrder *msgjson.BookOrderNote, ord order.Order, tag string) {
		t.Helper()
		prefix, trade := ord.Prefix(), ord.Trade()
		if trade.Sell != (msgOrder.Side == msgjson.SellOrderNum) {
			t.Fatalf("%s: message order has wrong side marked. sell = %t, side = '%d'", tag, trade.Sell, msgOrder.Side)
		}
		if msgOrder.Quantity != trade.Remaining() {
			t.Fatalf("%s: message order quantity incorrect. expected %d, got %d", tag, trade.Quantity, msgOrder.Quantity)
		}
		if msgOrder.Time != uint64(prefix.Time()) {
			t.Fatalf("%s: wrong time. expected %d, got %d", tag, prefix.Time(), msgOrder.Time)
		}
	}

	compareLO := func(msgOrder *msgjson.BookOrderNote, lo *order.LimitOrder, tifFlag uint8, tag string) {
		t.Helper()
		if msgOrder.Rate != lo.Rate {
			t.Fatalf("%s: message order rate incorrect. expected %d, got %d", tag, lo.Rate, msgOrder.Rate)
		}
		if msgOrder.TiF != tifFlag {
			t.Fatalf("%s: message order has wrong time-in-force flag. wanted %d, got %d", tag, tifFlag, msgOrder.TiF)
		}
		compareTrade(msgOrder, lo, tag)
	}

	// A helper function to scan through the received msgjson.OrderBook.Orders and
	// compare the orders to the order book.
	checkBook := func(source *TBookSource, tifFlag uint8, tag string, msgOrders ...*msgjson.BookOrderNote) {
		t.Helper()
		for i, msgOrder := range msgOrders {
			lo := findBookOrder(msgOrder.OrderID, source)
			if lo == nil {
				t.Fatalf("%s(%d): order not found", tag, i)
			}
			compareLO(msgOrder, lo, tifFlag, tag)
		}
	}

	// Have a subscriber connect and pull the orders from market 1.
	// The format used here is link[market]_[count]
	link1, sub := newSubscriber(mkt1)
	if err := router.handleOrderBook(link1, sub); err != nil {
		t.Fatalf("handleOrderBook: %v", err)
	}
	orders := checkResponse("first link, market 1", mktName1, sub.ID, link1)
	checkBook(src1, msgjson.StandingOrderNum, "first link, market 1", orders...)

	// Another subscriber to the same market should behave identically.
	link2, sub := newSubscriber(mkt1)
	if err := router.handleOrderBook(link2, sub); err != nil {
		t.Fatalf("handleOrderBook: %v", err)
	}
	orders = checkResponse("second link, market 1", mktName1, sub.ID, link2)
	checkBook(src1, msgjson.StandingOrderNum, "second link, market 1", orders...)

	// An epoch notification sent on market 1's channel should arrive at both
	// clients.
	lo := makeLO(buyer1, mkRate1(0.8, 1.0), randLots(10), order.ImmediateTiF)
	sig := &updateSignal{
		action: epochAction,
		data: sigDataEpochOrder{
			order:    lo,
			epochIdx: 12345678,
		},
	}
	src1.feed <- sig

	epochNote := getEpochNoteFromLink(t, link1)
	compareLO(&epochNote.BookOrderNote, lo, msgjson.ImmediateOrderNum, "epoch notification, link1")
	if epochNote.MarketID != mktName1 {
		t.Fatalf("wrong market id. got %s, wanted %s", mktName1, epochNote.MarketID)
	}

	epochNote = getEpochNoteFromLink(t, link2)
	compareLO(&epochNote.BookOrderNote, lo, msgjson.ImmediateOrderNum, "epoch notification, link2")

	// just for kicks, checks the epoch is as expected.
	wantIdx := sig.data.(sigDataEpochOrder).epochIdx
	if epochNote.Epoch != uint64(wantIdx) {
		t.Fatalf("wrong epoch. wanted %d, got %d", wantIdx, epochNote.Epoch)
	}

	// Have both subscribers subscribe to market 2.
	sub = newSubscription(mkt2)
	if err := router.handleOrderBook(link1, sub); err != nil {
		t.Fatalf("handleOrderBook: %v", err)
	}
	if err := router.handleOrderBook(link2, sub); err != nil {
		t.Fatalf("handleOrderBook: %v", err)
	}
	orders = checkResponse("first link, market 2", mktName2, sub.ID, link1)
	checkBook(src2, msgjson.StandingOrderNum, "first link, market 2", orders...)
	orders = checkResponse("second link, market 2", mktName2, sub.ID, link2)
	checkBook(src2, msgjson.StandingOrderNum, "second link, market 2", orders...)

	// Send an epoch update for a market order.
	mo := makeMO(buyer2, randLots(10))
	sig = &updateSignal{
		action: epochAction,
		data: sigDataEpochOrder{
			order:    mo,
			epochIdx: 12345678,
		},
	}
	src2.feed <- sig

	epochNote = getEpochNoteFromLink(t, link1)
	compareTrade(&epochNote.BookOrderNote, mo, "link 1 market 2 epoch update (market order)")

	epochNote = getEpochNoteFromLink(t, link2)
	compareTrade(&epochNote.BookOrderNote, mo, "link 2 market 2 epoch update (market order)")

	// Make a new standing limit order with a quantity of at least 3 lots for
	// the market 2 sell book. Book it with a bookAction, fill 1 lot, and send
	// an updateRemainingAction update.
	lo = makeLO(seller2, mkRate2(1.0, 1.2), randLots(10)+1, order.StandingTiF)
	lo.FillAmt = mkt2.LotSize

	sig = &updateSignal{
		action: bookAction,
		data: sigDataBookedOrder{
			order:    lo,
			epochIdx: 12344365,
		},
	}
	src2.feed <- sig

	bookNote := getBookNoteFromLink(t, link1)
	compareLO(bookNote, lo, msgjson.StandingOrderNum, "book notification, link1, market 2")
	if bookNote.MarketID != mktName2 {
		t.Fatalf("wrong market id. wanted %s, got %s", mktName2, bookNote.MarketID)
	}

	bookNote = getBookNoteFromLink(t, link2)
	compareLO(bookNote, lo, msgjson.StandingOrderNum, "book notification, link2, market 2")

	if bookNote.Quantity != lo.Remaining() {
		t.Fatalf("wrong quantity in book update. expected %d, got %d", lo.Remaining(), bookNote.Quantity)
	}

	// Update the order's remaining quantity. Leave one lot remaining.
	lo.FillAmt = lo.Quantity - mkt2.LotSize

	sig = &updateSignal{
		action: updateRemainingAction,
		data: sigDataUpdateRemaining{
			order:    lo,
			epochIdx: 12344365,
		},
	}

	src2.feed <- sig

	urNote := getUpdateRemainingNoteFromLink(t, link2)
	if urNote.Remaining != lo.Remaining() {
		t.Fatalf("wrong remaining quantity for link2. expected %d, got %d", lo.Remaining(), urNote.Remaining)
	}
	// clear the send from client 1
	link1.getSend()

	// Now unbook the order.
	sig = &updateSignal{
		action: unbookAction,
		data: sigDataUnbookedOrder{
			order:    lo,
			epochIdx: 12345678,
		},
	}
	src2.feed <- sig

	unbookNote := getUnbookNoteFromLink(t, link1)
	if lo.ID().String() != unbookNote.OrderID.String() {
		t.Fatalf("wrong cancel ID. expected %s, got %s", lo.ID(), unbookNote.OrderID)
	}
	if unbookNote.MarketID != mktName2 {
		t.Fatalf("wrong market id. wanted %s, got %s", mktName2, unbookNote.MarketID)
	}
	// clear the send from client 2
	link2.getSend()

	// Make sure the order is no longer stored in the router's books
	if router.books[mktName2].orders[lo.ID()] != nil {
		t.Fatalf("order still in book after unbookAction")
	}

	// Now unsubscribe link 1 from market 1.
	unsub, _ := msgjson.NewRequest(10, msgjson.UnsubOrderBookRoute, &msgjson.UnsubOrderBook{
		MarketID: mktName1,
	})
	if err := router.handleUnsubOrderBook(link1, unsub); err != nil {
		t.Fatalf("handleUnsubOrderBook: %v", err)
	}

	// Client 1 should have an unsub response from the server.
	respMsg := link1.getSend()
	if respMsg == nil {
		t.Fatalf("no response for unsub")
	}
	resp, _ := respMsg.Response()
	var success bool
	err := json.Unmarshal(resp.Result, &success)
	if err != nil {
		t.Fatalf("err unmarshaling unsub response")
	}
	if !success {
		t.Fatalf("expected true for unsub result, got false")
	}

	mo = makeMO(seller1, randLots(10))
	sig = &updateSignal{
		action: epochAction,
		data: sigDataEpochOrder{
			order:    mo,
			epochIdx: 12345678,
		},
	}
	src1.feed <- sig

	if link2.getSend() == nil {
		t.Fatalf("client 2 didn't receive an update after client 1 unsubbed")
	}
	select {
	case <-link1.sendTrigger:
		t.Fatalf("client 1 should not have received an update after unsubscribing")
	case <-time.NewTimer(20 * time.Millisecond).C:
		// Client 2 already got a send, so we should not need to wait much if at
		// all since BookRouter.sendNote is already running or done.
	}

	// Now epoch a cancel order to client 2.
	targetID := src1.buys[0].ID()
	co := makeCO(buyer1, targetID)
	sig = &updateSignal{
		action: epochAction,
		data: sigDataEpochOrder{
			order:    co,
			epochIdx: 12345678,
		},
	}
	src1.feed <- sig

	epochNote = getEpochNoteFromLink(t, link2)
	if epochNote.OrderType != msgjson.CancelOrderNum {
		t.Fatalf("epoch cancel notification not of cancel type. expected %d, got %d",
			msgjson.CancelOrderNum, epochNote.OrderType)
	}

	if epochNote.TargetID.String() != targetID.String() {
		t.Fatalf("epoch cancel notification has wrong order ID. expected %s, got %s",
			targetID, epochNote.TargetID)
	}

	// Send another, but err on the send. Check for unsubscribed
	link2.sendErr = dummyError
	src1.feed <- sig

	// Wait for (*BookRouter).sendNote to remove the erroring link from the
	// subscription conns map.
	time.Sleep(50 * time.Millisecond)

	subs := router.books[mktName1].subs
	subs.mtx.RLock()
	l := subs.conns[link2.ID()]
	subs.mtx.RUnlock()
	if l != nil {
		t.Fatalf("client not removed from subscription list")
	}
}

func TestBadMessages(t *testing.T) {
	router := rig.router
	link, sub := newSubscriber(mkt1)

	checkErr := func(tag string, rpcErr *msgjson.Error, code int) {
		t.Helper()
		if rpcErr == nil {
			t.Fatalf("%s: no error", tag)
		}
		if rpcErr.Code != code {
			t.Fatalf("%s: wrong code. wanted %d, got %d", tag, code, rpcErr.Code)
		}
	}

	// Bad encoding
	ogPayload := sub.Payload
	sub.Payload = []byte(`?`)
	rpcErr := router.handleOrderBook(link, sub)
	checkErr("bad payload", rpcErr, msgjson.RPCParseError)
	sub.Payload = ogPayload

	// Use an unknown market
	badMkt := &ordertest.Market{
		Base:  400000,
		Quote: 400001,
	}
	sub = newSubscription(badMkt)
	rpcErr = router.handleOrderBook(link, sub)
	checkErr("bad payload", rpcErr, msgjson.UnknownMarket)

	// Valid asset IDs, but not an actual market on the DEX.
	badMkt = &ordertest.Market{
		Base:  15845,   // SDGO
		Quote: 5264462, // PTN
	}
	sub = newSubscription(badMkt)
	rpcErr = router.handleOrderBook(link, sub)
	checkErr("bad payload", rpcErr, msgjson.UnknownMarket)

	// Unsub with invalid payload
	unsub, _ := msgjson.NewRequest(10, msgjson.UnsubOrderBookRoute, &msgjson.UnsubOrderBook{
		MarketID: mktName1,
	})
	ogPayload = unsub.Payload
	unsub.Payload = []byte(`?`)
	rpcErr = router.handleUnsubOrderBook(link, unsub)
	checkErr("bad payload", rpcErr, msgjson.RPCParseError)
	unsub.Payload = ogPayload

	// Try unsubscribing from an unknown market
	unsub, _ = msgjson.NewRequest(10, msgjson.UnsubOrderBookRoute, &msgjson.UnsubOrderBook{
		MarketID: "sdgo_ptn",
	})
	rpcErr = router.handleUnsubOrderBook(link, unsub)
	checkErr("bad payload", rpcErr, msgjson.UnknownMarket)

	// Unsub a user that's not subscribed.
	unsub, _ = msgjson.NewRequest(10, msgjson.UnsubOrderBookRoute, &msgjson.UnsubOrderBook{
		MarketID: mktName1,
	})
	rpcErr = router.handleUnsubOrderBook(link, unsub)
	checkErr("bad payload", rpcErr, msgjson.NotSubscribedError)
}
