// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package core

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/client/db"
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/calc"
	"decred.org/dcrdex/dex/encode"
	"decred.org/dcrdex/dex/encrypt"
	"decred.org/dcrdex/dex/msgjson"
	"decred.org/dcrdex/dex/order"
	"decred.org/dcrdex/server/account"
	"github.com/decred/dcrd/dcrec/secp256k1/v2"
)

// WalletForm is information necessary to create a new exchange wallet.
type WalletForm struct {
	AssetID uint32
	Account string
	INIPath string
}

// WalletState is the current status of an exchange wallet.
type WalletState struct {
	Symbol  string `json:"symbol"`
	AssetID uint32 `json:"assetID"`
	Open    bool   `json:"open"`
	Running bool   `json:"running"`
	Updated uint64 `json:"updated"`
	Balance uint64 `json:"balance"`
	Address string `json:"address"`
	FeeRate uint64 `json:"feerate"`
	Units   string `json:"units"`
}

// User is information about the user's wallets and DEX accounts.
type User struct {
	Exchanges   map[string]*Exchange       `json:"exchanges"`
	Initialized bool                       `json:"inited"`
	Assets      map[uint32]*SupportedAsset `json:"assets"`
}

// SupportedAsset is data about an asset and possibly the wallet associated
// with it.
type SupportedAsset struct {
	ID     uint32            `json:"id"`
	Symbol string            `json:"symbol"`
	Wallet *WalletState      `json:"wallet"`
	Info   *asset.WalletInfo `json:"info"`
}

// xcWallet is a wallet.
type xcWallet struct {
	asset.Wallet
	connector *dex.ConnectionMaster
	AssetID   uint32
	mtx       sync.RWMutex
	lockTime  time.Time
	hookedUp  bool
	balance   uint64
	balUpdate time.Time
	encPW     []byte
	address   string
}

// Unlock unlocks the wallet.
func (w *xcWallet) Unlock(pw string, dur time.Duration) error {
	err := w.Wallet.Unlock(pw, dur)
	if err != nil {
		return err
	}
	w.mtx.Lock()
	w.lockTime = time.Now().Add(dur)
	w.mtx.Unlock()
	return nil
}

// lock the wallet, setting the lockTime to the time when the wallet will be
// locked.
func (w *xcWallet) lock() error {
	w.mtx.Lock()
	w.lockTime = time.Time{}
	w.mtx.Unlock()
	return w.Lock()
}

// unlocked will return true if the lockTime has not passed.
func (w *xcWallet) unlocked() bool {
	w.mtx.RLock()
	defer w.mtx.RUnlock()
	return w.lockTime.After(time.Now())
}

// state returns the current WalletState.
func (w *xcWallet) state() *WalletState {
	w.mtx.RLock()
	defer w.mtx.RUnlock()
	winfo := w.Info()
	return &WalletState{
		Symbol:  unbip(w.AssetID),
		AssetID: w.AssetID,
		Open:    w.lockTime.After(time.Now()),
		Running: w.connector.On(),
		Balance: w.balance,
		Address: w.address,
		FeeRate: winfo.FeeRate, // Withdraw fee, not swap.
		Units:   winfo.Units,
	}
}

// setBalance sets the wallet balance.
func (w *xcWallet) setBalance(bal uint64) {
	w.mtx.Lock()
	w.balance = bal
	w.balUpdate = time.Now()
	w.mtx.Unlock()
}

// setAddress sets the wallet's deposit address.
func (w *xcWallet) setAddress(addr string) {
	w.mtx.Lock()
	w.address = addr
	w.mtx.Unlock()
}

// connected is true if the wallet has already been connected.
func (w *xcWallet) connected() bool {
	w.mtx.RLock()
	defer w.mtx.RUnlock()
	return w.hookedUp
}

// Connect calls the dex.Connector's Connect method and sets the
// xcWallet.hookedUp flag.
func (w *xcWallet) Connect(ctx context.Context) error {
	err := w.connector.Connect(ctx)
	if err != nil {
		return err
	}
	w.mtx.Lock()
	w.hookedUp = true
	w.mtx.Unlock()
	return nil
}

// Registration is information necessary to register an account on a DEX.
type Registration struct {
	DEX      string
	Password string
	Fee      uint64
}

// Match represents a match on an order. An order may have many matches.
type Match struct {
	MatchID string            `json:"matchID"`
	Step    order.MatchStatus `json:"step"`
	Rate    uint64            `json:"rate"`
	Qty     uint64            `json:"qty"`
}

// Order is core's general type for an order. An order may be a market, limit,
// or cancel order. Some fields are only relevant to particular order types.
type Order struct {
	Type        order.OrderType   `json:"type"`
	ID          string            `json:"id"`
	Stamp       uint64            `json:"stamp"`
	Qty         uint64            `json:"qty"`
	Sell        bool              `json:"sell"`
	Filled      uint64            `json:"filled"`
	Matches     []*Match          `json:"matches"`
	Cancelling  bool              `json:"cancelling"`
	Rate        uint64            `json:"rate"`               // limit only
	TimeInForce order.TimeInForce `json:"tif"`                // limit only
	TargetID    string            `json:"targetID,omitempty"` // cancel only
}

// Market is market info.
type Market struct {
	Name            string   `json:"name"`
	BaseID          uint32   `json:"baseid"`
	BaseSymbol      string   `json:"basesymbol"`
	QuoteID         uint32   `json:"quoteid"`
	QuoteSymbol     string   `json:"quotesymbol"`
	EpochLen        uint64   `json:"epochlen"`
	StartEpoch      uint64   `json:"startepoch"`
	MarketBuyBuffer float64  `json:"buybuffer"`
	Orders          []*Order `json:"orders"`
}

// Display returns an ID string suitable for displaying in a UI.
func (m *Market) Display() string {
	return newDisplayIDFromSymbols(m.BaseSymbol, m.QuoteSymbol)
}

// sid is a simpler string ID constructed from the asset IDs.
func (m *Market) sid() string {
	return sid(m.BaseID, m.QuoteID)
}

// Exchange represents a single DEX with any number of markets.
type Exchange struct {
	URL        string                `json:"url"`
	Markets    map[string]*Market    `json:"markets"`
	Assets     map[uint32]*dex.Asset `json:"assets"`
	FeePending bool                  `json:"feePending"`
}

// Return the markets as a slice sorted by Display ID, ascending.
func (xc *Exchange) SortedMarkets() []*Market {
	markets := make([]*Market, 0, len(xc.Markets))
	for _, market := range xc.Markets {
		markets = append(markets, market)
	}
	sort.Slice(markets, func(i, j int) bool {
		m1, m2 := markets[i], markets[j]
		switch true {
		case m1.BaseSymbol < m2.BaseSymbol:
			return true
		case m1.BaseSymbol > m2.BaseSymbol:
			return false
		default: // Same base symbol.
			return m2.QuoteSymbol < m2.QuoteSymbol
		}
	})
	return markets
}

// newDisplayID creates a display-friendly market ID for a base/quote ID pair.
func newDisplayID(base, quote uint32) string {
	return newDisplayIDFromSymbols(unbip(base), unbip(quote))
}

// newDisplayIDFromSymbols creates a display-friendly market ID for a base/quote
// symbol pair.
func newDisplayIDFromSymbols(base, quote string) string {
	return strings.ToUpper(base) + "-" + strings.ToUpper(quote)
}

// MiniOrder is minimal information about an order in a market's order book.
type MiniOrder struct {
	Qty   float64 `json:"qty"`
	Rate  float64 `json:"rate"`
	Epoch bool    `json:"epoch"`
}

// OrderBook represents an order book, which is just two sorted lists of orders.
type OrderBook struct {
	Sells []*MiniOrder `json:"sells"`
	Buys  []*MiniOrder `json:"buys"`
}

// BookUpdate is an order book update.
type BookUpdate struct {
	Market string
}

// dexAccount is the core type to represent the client's account information for
// a DEX.
type dexAccount struct {
	url       string
	encKey    []byte
	keyMtx    sync.RWMutex
	privKey   *secp256k1.PrivateKey
	id        account.AccountID
	dexPubKey *secp256k1.PublicKey
	feeCoin   []byte
	isPaid    bool
	authMtx   sync.RWMutex
	isAuthed  bool
}

// newDEXAccount is a constructor for a new *dexAccount.
func newDEXAccount(acctInfo *db.AccountInfo) *dexAccount {
	return &dexAccount{
		url:       acctInfo.URL,
		encKey:    acctInfo.EncKey,
		dexPubKey: acctInfo.DEXPubKey,
		isPaid:    acctInfo.Paid,
		feeCoin:   acctInfo.FeeCoin,
	}
}

// ID returns the account ID.
func (a *dexAccount) ID() account.AccountID {
	a.keyMtx.RLock()
	defer a.keyMtx.RUnlock()
	return a.id
}

// unlock decrypts the account private key.
func (a *dexAccount) unlock(crypter encrypt.Crypter) error {
	keyB, err := crypter.Decrypt(a.encKey)
	if err != nil {
		return err
	}
	privKey, pubKey := secp256k1.PrivKeyFromBytes(keyB)
	a.keyMtx.Lock()
	a.privKey = privKey
	a.id = account.NewID(pubKey.SerializeCompressed())
	a.keyMtx.Unlock()
	return nil
}

// lock clears the account private key.
func (a *dexAccount) lock() {
	a.keyMtx.Lock()
	a.privKey = nil
	a.keyMtx.Unlock()
}

// locked will be true if the account private key is currently decrypted.
func (a *dexAccount) locked() bool {
	a.keyMtx.RLock()
	defer a.keyMtx.RUnlock()
	return a.privKey == nil
}

// authed will be true if the account has been authenticated i.e. the 'connect'
// request has been succesfully sent.
func (a *dexAccount) authed() bool {
	a.authMtx.RLock()
	defer a.authMtx.RUnlock()
	return a.isAuthed
}

// auth sets the account as authenticated.
func (a *dexAccount) auth() {
	a.authMtx.Lock()
	a.isAuthed = true
	a.authMtx.Unlock()
}

// unauth sets the account as un-authenticated.
func (a *dexAccount) unauth() {
	a.authMtx.Lock()
	a.isAuthed = false
	a.authMtx.Unlock()
}

// paid will be true if the account regisration fee has been accepted by the
// DEX.
func (a *dexAccount) paid() bool {
	a.authMtx.RLock()
	defer a.authMtx.RUnlock()
	return a.isPaid
}

// pay sets the account paid flag.
func (a *dexAccount) pay() {
	a.authMtx.Lock()
	a.isPaid = true
	a.authMtx.Unlock()
}

// feePending checks whether the fee transaction has been broadcast, but the
// notifyfee request has not been sent/accepted yet.
func (a *dexAccount) feePending() bool {
	a.authMtx.RLock()
	defer a.authMtx.RUnlock()
	return !a.isPaid && len(a.feeCoin) > 0
}

// sign uses the account private key to sign the message. If the account is
// locked, an error will be returned.
func (a *dexAccount) sign(msg []byte) ([]byte, error) {
	a.keyMtx.RLock()
	defer a.keyMtx.RUnlock()
	if a.privKey == nil {
		return nil, fmt.Errorf("account locked")
	}
	sig, err := a.privKey.Sign(msg)
	if err != nil {
		return nil, err
	}
	return sig.Serialize(), nil
}

// checkSig checks the signature against the message and the DEX pubkey.
func (a *dexAccount) checkSig(msg []byte, sig []byte) error {
	_, err := checkSigS256(msg, a.dexPubKey.Serialize(), sig)
	return err
}

// MatchUpdate will be delivered from the negotiation feed.
type MatchUpdate struct{}

// A Negotiation represents an active match negotiation.
type Negotiation interface {
	// Feed is a MatchUpdate channel. Feed always returns the same channel and
	// does not support multiple clients. The channel will be closed when the
	// Negotiation process has completed or encountered an unrecoverable error.
	Feed() <-chan *MatchUpdate
	Order() order.Order
}

// A matchNegotiator negotiates a match. matchNegotiator satisfies the
// Negotiation interface.
type matchNegotiator struct {
	order.UserMatch
	trade  order.Order
	update chan *MatchUpdate
}

// Feed returns the MatchUpdate channel. Part of the Negotiator interface.
func (m *matchNegotiator) Feed() <-chan *MatchUpdate {
	return m.update
}

// Order returns the order associated with the match.
func (m *matchNegotiator) Order() order.Order {
	return m.trade
}

// runMatch is the match negotiation thread.
func (m *matchNegotiator) runMatch(ctx context.Context) {
	// do match stuff
	<-ctx.Done()
}

// TradeForm is used to place a market or limit order
type TradeForm struct {
	DEX     string `json:"dex"`
	IsLimit bool   `json:"isLimit"`
	Sell    bool   `json:"sell"`
	Base    uint32 `json:"base"`
	Quote   uint32 `json:"quote"`
	Qty     uint64 `json:"qty"`
	Rate    uint64 `json:"rate"`
	TifNow  bool   `json:"tifnow"`
}

// trackedTrade is an order placed by the core.
type trackedTrade struct {
	// mtx   sync.RWMutex
	order.Order
	dc            *dexConnection
	preImg        order.Preimage
	sid           string
	cancelOrder   *order.CancelOrder
	cancelPreImg  order.Preimage
	cancelMatches struct {
		maker *msgjson.Match
		taker *msgjson.Match
	}
	negotiators map[order.MatchID]*matchNegotiator
}

// newTrackedTrade is a constructor for a trackedTrade.
func newTrackedTrade(trade order.Order, dc *dexConnection, preImg order.Preimage) *trackedTrade {
	return &trackedTrade{
		Order:       trade,
		dc:          dc,
		preImg:      preImg,
		sid:         sid(trade.Base(), trade.Quote()),
		negotiators: make(map[order.MatchID]*matchNegotiator),
	}
}

// rate returns the order's rate, or zero if a market or cancel order.
func (t *trackedTrade) rate() uint64 {
	if ord, ok := t.Order.(*order.LimitOrder); ok {
		return ord.Rate
	}
	return 0
}

// coreOrder constructs a *core.Order for the tracked order.Order.
func (t *trackedTrade) coreOrder() (*Order, *Order) {
	prefix, trade := t.Prefix(), t.Trade()
	var tif order.TimeInForce
	if lo, ok := t.Order.(*order.LimitOrder); ok {
		tif = lo.Force
	}
	coreOrder := &Order{
		Type:        prefix.OrderType,
		ID:          t.ID().String(),
		Stamp:       encode.UnixMilliU(prefix.ServerTime),
		Rate:        t.rate(),
		Qty:         trade.Quantity,
		Sell:        trade.Sell,
		Filled:      trade.Filled,
		Cancelling:  t.cancelOrder != nil,
		TimeInForce: tif,
	}
	for _, n := range t.negotiators {
		coreOrder.Matches = append(coreOrder.Matches, &Match{
			MatchID: n.MatchID.String(),
			Step:    n.Status,
			Rate:    n.Rate,
			Qty:     n.Quantity,
		})
	}
	var cancelOrder *Order
	if t.cancelOrder != nil {
		cancelOrder = &Order{
			Type:     order.CancelOrderType,
			Stamp:    encode.UnixMilliU(t.cancelOrder.ServerTime),
			TargetID: t.cancelOrder.TargetOrderID.String(),
		}
	}
	return coreOrder, cancelOrder
}

// negotiate creates a matchNegotiator and starts the negotiation thread.
func (t *trackedTrade) negotiate(ctx context.Context, msgMatch *msgjson.Match) error {
	if len(msgMatch.MatchID) != order.MatchIDSize {
		return fmt.Errorf("match id of incorrect length. expected %d, got %d",
			order.MatchIDSize, len(msgMatch.MatchID))
	}
	var mid order.MatchID
	copy(mid[:], msgMatch.MatchID)
	var oid order.OrderID
	copy(oid[:], msgMatch.OrderID)
	if oid != t.ID() {
		return fmt.Errorf("negotiate called for wrong order. %s != %s", oid, t.ID())
	}
	n := &matchNegotiator{
		UserMatch: order.UserMatch{
			OrderID:  oid,
			MatchID:  mid,
			Quantity: msgMatch.Quantity,
			Rate:     msgMatch.Rate,
			Address:  msgMatch.Address,
			Status:   order.MatchStatus(msgMatch.Status),
			Side:     order.MatchSide(msgMatch.Side),
		},
		update: make(chan *MatchUpdate, 1),
		trade:  t.Order,
	}
	// First check that this isn't a match on a cancel order. I'm not crazy about
	// this, but I am detecting this case right now based on the Address field
	// being an empty string.
	isCancel := t.cancelOrder != nil && msgMatch.Address == ""
	if isCancel {
		t.cancelMatches.maker = msgMatch
	} else {
		go n.runMatch(ctx)
	}
	t.negotiators[n.MatchID] = n
	isMarketBuy := t.Type() == order.MarketOrderType && !t.Trade().Sell
	var filled uint64
	for _, n := range t.negotiators {
		if isMarketBuy {
			filled += calc.BaseToQuote(n.Rate, n.Quantity)
		} else {
			filled += n.Quantity
		}
	}
	t.Trade().Filled = filled
	return nil
}

// cancel should be called with the message for the match on a cancel order.
func (t *trackedTrade) cancel(msgMatch *msgjson.Match) error {
	var oid order.OrderID
	copy(oid[:], msgMatch.OrderID)
	if oid != t.cancelOrder.ID() {
		return fmt.Errorf("negotiate called for wrong order. %s != %s", oid, t.cancelOrder.ID())
	}
	log.Infof("maker notification for cancel order detected for order %s. match id = %s", oid, msgMatch.MatchID)
	t.cancelMatches.taker = msgMatch
	return nil
}

// sid is a string ID constructed from the asset IDs.
func sid(b, q uint32) string {
	return strconv.Itoa(int(b)) + "-" + strconv.Itoa(int(q))
}
