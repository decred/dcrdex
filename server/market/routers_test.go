package market

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"testing"
	"time"

	"github.com/decred/dcrd/dcrec/secp256k1/v2"
	"github.com/decred/dcrdex/server/account"
	"github.com/decred/dcrdex/server/asset"
	"github.com/decred/dcrdex/server/comms/msgjson"
	// dex "github.com/decred/dcrdex/server/market/types"
	"github.com/decred/dcrdex/server/matcher"
	"github.com/decred/dcrdex/server/order"
	ordertest "github.com/decred/dcrdex/server/order/test"
)

const (
	dummySize   = 50
	btcLotSize  = 100_000
	btcRateStep = 1_000
	dcrLotSize  = 10_000_000
	dcrRateStep = 100_000
	btcID       = 0
	dcrID       = 42
	btcAddr     = "18Zpft83eov56iESWuPpV8XFLJ1b8gMZy7"
	dcrAddr     = "DsYXjAK3UiTVN9js8v9G21iRbr2wPty7f12"
)

var (
	oRig       *tOrderRig
	dummyError = fmt.Errorf("test error")
	clientTime = time.Now()
)

// The AuthManager handles client-related actions, including authorization and
// communications.
type TAuth struct {
	authErr error
	sends   []*msgjson.Message
}

func (a *TAuth) Route(route string, handler func(account.AccountID, *msgjson.Message) *msgjson.Error) {
}
func (a *TAuth) Auth(user account.AccountID, msg, sig []byte) error {
	return a.authErr
}
func (a *TAuth) Sign(...msgjson.Signable) {}
func (a *TAuth) Send(_ account.AccountID, msg *msgjson.Message) {
	a.sends = append(a.sends, msg)
}
func (a *TAuth) getSend() *msgjson.Message {
	if len(a.sends) == 0 {
		return nil
	}
	msg := a.sends[0]
	a.sends = a.sends[1:]
	return msg
}

type TMarketTunnel struct {
	addErr     error
	adds       []order.Order
	midGap     uint64
	locked     bool
	watched    bool
	cancelable bool
}

func (m *TMarketTunnel) AddEpoch(o order.Order) error {
	m.adds = append(m.adds, o)
	return m.addErr
}

func (m *TMarketTunnel) MidGap() uint64 {
	return m.midGap
}

func (m *TMarketTunnel) OutpointLocked(txid string, vout uint32) bool {
	return m.locked
}

func (m *TMarketTunnel) TxMonitored(user account.AccountID, txid string) bool {
	return m.watched
}

func (m *TMarketTunnel) pop() order.Order {
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

func (b *TBackend) UTXO(txid string, vout uint32, redeemScript []byte) (asset.UTXO, error) {
	op := fmt.Sprintf("%s:%d", txid, vout)
	v := b.utxos[op]
	if v == 0 {
		return nil, fmt.Errorf("no utxo")
	}
	return &tUTXO{val: v}, b.utxoErr
}
func (b *TBackend) BlockChannel(size int) chan uint32            { return nil }
func (b *TBackend) Transaction(txid string) (asset.DEXTx, error) { return nil, nil }
func (b *TBackend) InitTxSize() uint32                           { return dummySize }
func (b *TBackend) CheckAddress(string) bool                     { return b.addrChecks }
func (b *TBackend) addUTXO(utxo *msgjson.UTXO, val uint64) {
	op := fmt.Sprintf("%s:%d", utxo.TxID, utxo.Vout)
	b.utxos[op] = val
}

type tUTXO struct {
	val uint64
}

var utxoAuthErr error
var utxoConfsErr error
var utxoConfs int64 = 2

func (u *tUTXO) Confirmations() (int64, error) { return utxoConfs, utxoConfsErr }
func (u *tUTXO) Auth(pubkeys,
	sigs [][]byte, msg []byte) error {
	return utxoAuthErr
}
func (u *tUTXO) SpendSize() uint32 { return dummySize }
func (u *tUTXO) TxHash() []byte    { return nil }
func (u *tUTXO) TxID() string      { return "" }
func (u *tUTXO) Vout() uint32      { return 0 }
func (u *tUTXO) Value() uint64     { return u.val }

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

func (rig *tOrderRig) signedUTXO(id int, val uint64, numSigs int) *msgjson.UTXO {
	u := rig.user
	utxo := &msgjson.UTXO{
		TxID: randomBytes(32),
		Vout: rand.Uint32(),
	}
	pk := u.privKey.PubKey().SerializeCompressed()
	for i := 0; i < numSigs; i++ {
		sig, _ := u.privKey.Sign(utxo.Serialize())
		utxo.Sigs = append(utxo.Sigs, sig.Serialize())
		utxo.PubKeys = append(utxo.PubKeys, pk)
	}
	switch id {
	case btcID:
		rig.btc.addUTXO(utxo, val)
	case dcrID:
		rig.dcr.addUTXO(utxo, val)
	}
	return utxo
}

var assetBTC = &asset.Asset{
	ID:       0,
	Symbol:   "btc",
	LotSize:  btcLotSize,
	RateStep: btcRateStep,
	FeeRate:  4,
	SwapSize: dummySize,
	SwapConf: 2,
	FundConf: 2,
}

var assetDCR = &asset.Asset{
	ID:       42,
	Symbol:   "dcr",
	LotSize:  dcrLotSize,
	RateStep: dcrRateStep,
	FeeRate:  10,
	SwapSize: dummySize,
	SwapConf: 2,
	FundConf: 2,
}

var assetUnknown = &asset.Asset{
	ID:       54321,
	Symbol:   "buk",
	LotSize:  1000,
	RateStep: 100,
	FeeRate:  10,
	SwapSize: 1,
	SwapConf: 0,
	FundConf: 0,
}

func randomBytes(len int) []byte {
	bytes := make([]byte, len)
	rand.Read(bytes)
	return bytes
}

func makeEnsureErr(t *testing.T) func(tag string, rpcErr *msgjson.Error, code int) {
	return func(tag string, rpcErr *msgjson.Error, code int) {
		if rpcErr == nil {
			if code == -1 {
				return
			}
			t.Fatalf("%s: no rpc error", tag)
		}
		if rpcErr.Code != code {
			t.Fatalf("%s: wrong error code. expected %d, got %d", tag, code, rpcErr.Code)
		}
	}
}

func TestMain(m *testing.M) {
	privKey, _ := secp256k1.GeneratePrivateKey()
	oRig = &tOrderRig{
		btc: tNewBackend(),
		dcr: tNewBackend(),
		user: &tUser{
			acct:    ordertest.NextAccount(),
			privKey: privKey,
		},
		auth: &TAuth{sends: make([]*msgjson.Message, 0)},
		market: &TMarketTunnel{
			adds:       make([]order.Order, 0),
			midGap:     dcrRateStep * 1000,
			cancelable: true,
		},
	}
	assetDCR.Backend = oRig.dcr
	assetBTC.Backend = oRig.btc
	oRig.router = NewOrderRouter(&OrderRouterConfig{
		AuthManager: oRig.auth,
		Assets: map[uint32]*asset.Asset{
			0:  assetBTC,
			42: assetDCR,
		},
		Markets:         map[string]MarketTunnel{"dcr_btc": oRig.market},
		MarketBuyBuffer: 1.5,
	})
	os.Exit(m.Run())
}

func TestLimit(t *testing.T) {
	qty := uint64(dcrLotSize) * 10
	rate := uint64(1000) * dcrRateStep
	user := oRig.user
	limit := msgjson.Limit{
		Prefix: msgjson.Prefix{
			AccountID:  user.acct[:],
			Base:       dcrID,
			Quote:      btcID,
			OrderType:  msgjson.LimitOrderNum,
			ClientTime: uint64(clientTime.Unix()),
		},
		Trade: msgjson.Trade{
			Side:     msgjson.SellOrderNum,
			Quantity: qty,
			UTXOs: []*msgjson.UTXO{
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
	o := oRig.market.pop()
	if o == nil {
		t.Fatalf("no order submitted to epoch")
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
	limit.TiF = 0
	ensureErr("bad tif", sendLimit(), msgjson.OrderParameterError)
	limit.TiF = msgjson.StandingOrderNum

	// Now switch it to a buy order, and ensure it passes
	// Clear the sends cache first.
	oRig.auth.sends = nil
	limit.Side = msgjson.BuyOrderNum
	buyUTXO := oRig.signedUTXO(btcID, matcher.BaseToQuote(rate, qty*2), 1)
	limit.UTXOs = []*msgjson.UTXO{
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
		MarketOrder: order.MarketOrder{
			Prefix: order.Prefix{
				AccountID:  user.acct,
				BaseAsset:  limit.Base,
				QuoteAsset: limit.Quote,
				OrderType:  order.LimitOrderType,
				ClientTime: clientTime,
				// ServerTime: sTime,
			},
			// UTXOs:    []order.Outpoint{},
			Sell:     false,
			Quantity: qty,
			Address:  dcrAddr,
		},
		Rate:  rate,
		Force: order.StandingTiF,
	}
	// Get the last order submitted to the epoch
	o = oRig.market.pop()
	if o == nil {
		t.Fatalf("no buy order submitted to epoch")
	}

	// Check the utxo
	epochOrder := o.(*order.LimitOrder)
	if len(epochOrder.UTXOs) != 1 {
		t.Fatalf("expected 1 order UTXO, got %d", len(epochOrder.UTXOs))
	}
	epochUTXO := epochOrder.UTXOs[0]
	if !bytes.Equal(epochUTXO.TxHash(), buyUTXO.TxID) {
		t.Fatalf("utxo reporting wrong txid")
	}
	if epochUTXO.Vout() != buyUTXO.Vout {
		t.Fatalf("utxo reporting wrong vout")
	}

	// Now steal the UTXO
	lo.UTXOs = epochOrder.UTXOs

	// Get the server time from the response.
	respMsg := oRig.auth.getSend()
	if respMsg == nil {
		t.Fatalf("no response from limit order")
	}
	resp, _ := respMsg.Response()
	result := new(msgjson.OrderResult)
	json.Unmarshal(resp.Result, result)
	lo.ServerTime = time.Unix(int64(result.ServerTime), 0)

	// Check equivalence of IDs.
	if epochOrder.ID() != lo.ID() {
		t.Fatalf("failed to duplicate ID")
	}
}

func TestMarket(t *testing.T) {
	qty := uint64(dcrLotSize) * 10
	user := oRig.user
	mkt := msgjson.Market{
		Prefix: msgjson.Prefix{
			AccountID:  user.acct[:],
			Base:       dcrID,
			Quote:      btcID,
			OrderType:  msgjson.MarketOrderNum,
			ClientTime: uint64(clientTime.Unix()),
		},
		Trade: msgjson.Trade{
			Side:     msgjson.SellOrderNum,
			Quantity: qty,
			UTXOs: []*msgjson.UTXO{
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
	mkt.UTXOs = []*msgjson.UTXO{
		buyUTXO,
	}
	mkt.Address = dcrAddr
	mkt.Quantity = matcher.BaseToQuote(midGap, uint64(dcrLotSize*1.2))
	// First check an order that doesn't satisfy the market buy buffer. For
	// testing, the market buy buffer is set to 1.5.
	ensureErr("market buy buffer unsatisfied", sendMarket(), msgjson.FundingError)
	mkt.Quantity = qty
	rpcErr = sendMarket()
	if rpcErr != nil {
		t.Fatalf("error for buy order: %s", rpcErr.Message)
	}

	// Create the order manually, so that we can compare the IDs as another check
	// of equivalence.
	mo := &order.MarketOrder{
		Prefix: order.Prefix{
			AccountID:  user.acct,
			BaseAsset:  mkt.Base,
			QuoteAsset: mkt.Quote,
			OrderType:  order.MarketOrderType,
			ClientTime: clientTime,
			// ServerTime: sTime,
		},
		// UTXOs:    []order.Outpoint{},
		Sell:     false,
		Quantity: qty,
		Address:  dcrAddr,
	}
	// Get the last order submitted to the epoch
	o = oRig.market.pop()
	if o == nil {
		t.Fatalf("no buy order submitted to epoch")
	}

	// Check the utxo
	epochOrder := o.(*order.MarketOrder)
	if len(epochOrder.UTXOs) != 1 {
		t.Fatalf("expected 1 order UTXO, got %d", len(epochOrder.UTXOs))
	}
	epochUTXO := epochOrder.UTXOs[0]
	if !bytes.Equal(epochUTXO.TxHash(), buyUTXO.TxID) {
		t.Fatalf("utxo reporting wrong txid")
	}
	if epochUTXO.Vout() != buyUTXO.Vout {
		t.Fatalf("utxo reporting wrong vout")
	}

	// Now steal the UTXO
	mo.UTXOs = epochOrder.UTXOs

	// Get the server time from the response.
	respMsg := oRig.auth.getSend()
	if respMsg == nil {
		t.Fatalf("no response from market order")
	}
	resp, _ := respMsg.Response()
	result := new(msgjson.OrderResult)
	json.Unmarshal(resp.Result, result)
	mo.ServerTime = time.Unix(int64(result.ServerTime), 0)

	// Check equivalence of IDs.
	if epochOrder.ID() != mo.ID() {
		t.Fatalf("failed to duplicate ID")
	}
}

func TestCancel(t *testing.T) {
	user := oRig.user
	var targetID order.OrderID
	targetID[0] = 244
	cancel := msgjson.Cancel{
		Prefix: msgjson.Prefix{
			AccountID:  user.acct[:],
			Base:       dcrID,
			Quote:      btcID,
			OrderType:  msgjson.CancelOrderNum,
			ClientTime: uint64(clientTime.Unix()),
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
	o := oRig.market.pop()
	if o == nil {
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
		Prefix: order.Prefix{
			AccountID:  user.acct,
			BaseAsset:  cancel.Base,
			QuoteAsset: cancel.Quote,
			OrderType:  order.MarketOrderType,
			ClientTime: clientTime,
		},
		TargetOrderID: targetID,
	}
	// Send the order through again, so we can grab it from the epoch.
	rpcErr = sendCancel()
	if rpcErr != nil {
		t.Fatalf("error for valid order (after prefix testing): %s", rpcErr.Message)
	}
	o = oRig.market.pop()
	if o == nil {
		t.Fatalf("no cancel order submitted to epoch")
	}

	// Check the utxo
	epochOrder := o.(*order.CancelOrder)

	// Get the server time from the response.
	respMsg := oRig.auth.getSend()
	if respMsg == nil {
		t.Fatalf("no response from market order")
	}
	resp, _ := respMsg.Response()
	result := new(msgjson.OrderResult)
	json.Unmarshal(resp.Result, result)
	co.ServerTime = time.Unix(int64(result.ServerTime), 0)

	// Check equivalence of IDs.
	if epochOrder.ID() != co.ID() {
		t.Fatalf("failed to duplicate ID")
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
	prefix.ClientTime = uint64(clientTime.Add(-time.Second * maxClockOffset).Unix())
	checkCode("too old", msgjson.ClockRangeError)
	prefix.ClientTime = uint64(clientTime.Unix())

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
	ogUTXOs := trade.UTXOs
	trade.UTXOs = nil
	checkCode("no utxos", msgjson.FundingError)
	trade.UTXOs = ogUTXOs

	// No signatures
	utxo1 := trade.UTXOs[1]
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
	checkCode("utxo auth error", msgjson.UTXOAuthError)
	utxoAuthErr = nil

	// UTXO Confirmations error
	utxoConfsErr = dummyError
	checkCode("utxo confs error", msgjson.FundingError)
	utxoConfsErr = nil

	// Not enough confirmations
	utxoConfs = 0
	checkCode("utxo no confs", msgjson.FundingError)

	// Check 0 confs, but DEX-monitored transaction.
	oRig.market.watched = true
	checkCode("dex-monitored unconfirmed", -1)
	utxoConfs = 2
	oRig.market.watched = false
	// Clear the order from the epoch.
	oRig.market.pop()

	// Not enough funding
	trade.UTXOs = ogUTXOs[:1]
	checkCode("unfunded", msgjson.FundingError)
	trade.UTXOs = ogUTXOs

	// Invalid address
	receivingAsset.addrChecks = false
	checkCode("bad address", msgjson.OrderParameterError)
	receivingAsset.addrChecks = true
}
