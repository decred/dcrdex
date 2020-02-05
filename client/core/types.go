// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package core

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/dex"
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

// WalletStatus is the current status of an exchange wallet.
type WalletStatus struct {
	Symbol  string `json:"symbol"`
	AssetID uint32 `json:"assetID"`
	Open    bool   `json:"open"`
	Running bool   `json:"running"`
}

// User is information about the user's wallets and DEX accounts.
type User struct {
	Markets     map[string][]*Market `json:"markets"`
	Wallets     []*WalletStatus      `json:"wallets"`
	Initialized bool                 `json:"inited"`
}

// xcWallet is a wallet.
type xcWallet struct {
	asset.Wallet
	waiter   *dex.StartStopWaiter
	AssetID  uint32
	mtx      sync.RWMutex
	lockTime time.Time
	hookedUp bool
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

// status returns whether the wallet is running as well as whether it is
// unlocked.
func (w *xcWallet) status() (on, open bool) {
	return w.waiter.On(), w.unlocked()
}

// unlocked returns true if the wallet is unlocked
func (w *xcWallet) unlocked() bool {
	w.mtx.RLock()
	defer w.mtx.RUnlock()
	return w.lockTime.After(time.Now())
}

// connected is true if the wallet has already been connected.
func (w *xcWallet) connected() bool {
	w.mtx.RLock()
	defer w.mtx.RUnlock()
	return w.hookedUp
}

// Connect wraps the asset.Wallet method and sets the xcWallet.hookedUp flag.
func (w *xcWallet) Connect() error {
	err := w.Wallet.Connect()
	if err != nil {
		return err
	}
	w.mtx.Lock()
	defer w.mtx.Unlock()
	w.hookedUp = true
	return nil
}

// Registration is information necessary to register an account on a DEX.
type Registration struct {
	DEX      string
	Password string
	Fee      uint64
}

// Market is market info.
type Market struct {
	BaseID          uint32  `json:"baseid"`
	BaseSymbol      string  `json:"basesymbol"`
	QuoteID         uint32  `json:"quoteid"`
	QuoteSymbol     string  `json:"quotesymbol"`
	EpochLen        uint64  `json:"epochlen"`
	StartEpoch      uint64  `json:"startepoch"`
	MarketBuyBuffer float64 `json:"buybuffer"`
}

// Display returns an ID string suitable for displaying in a UI.
func (m *Market) Display() string {
	return newDisplayIDFromSymbols(m.BaseSymbol, m.QuoteSymbol)
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
	authMtx   sync.RWMutex
	isAuthed  bool
}

// newDEXAccount is a constructor for a new *dexAccount.
func newDEXAccount(url string, encKey []byte, dexPubKey *secp256k1.PublicKey) *dexAccount {
	return &dexAccount{
		url:       url,
		encKey:    encKey,
		dexPubKey: dexPubKey,
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

// MatchUpdate will be delivered from the negotiation feed.
type MatchUpdate struct{}

// A Negotiation represents an active match negotiation.
type Negotiation interface {
	// Feed is a MatchUpdate channel. Feed always returns the same channel and
	// does not support multiple clients. The channel will be closed when the
	// Negotiation process has completed or encountered an unrecoverable error.
	Feed() <-chan *MatchUpdate
}

// A matchNegotiator negotiates a match. matchNegotiator satisfies the
// Negotiation interface.
type matchNegotiator struct {
	orderID  order.OrderID
	matchID  order.MatchID
	quantity uint64
	rate     uint64
	address  string
	status   order.MatchStatus
	side     order.MatchSide
	update   chan *MatchUpdate
}

// negotiate creates a matchNegotiator and starts the negotiation thread.
func negotiate(ctx context.Context, msgMatch *msgjson.Match) (*matchNegotiator, error) {
	if len(msgMatch.OrderID) != order.OrderIDSize {
		return nil, fmt.Errorf("order id of incorrect length. expected %d, got %d",
			order.OrderIDSize, len(msgMatch.OrderID))
	}
	if len(msgMatch.MatchID) != order.MatchIDSize {
		return nil, fmt.Errorf("match id of incorrect length. expected %d, got %d",
			order.MatchIDSize, len(msgMatch.MatchID))
	}
	var oid order.OrderID
	copy(oid[:], msgMatch.OrderID)
	var mid order.MatchID
	copy(mid[:], msgMatch.MatchID)
	n := &matchNegotiator{
		orderID:  oid,
		matchID:  mid,
		quantity: msgMatch.Quantity,
		rate:     msgMatch.Rate,
		address:  msgMatch.Address,
		status:   order.MatchStatus(msgMatch.Status),
		side:     order.MatchSide(msgMatch.Side),
		update:   make(chan *MatchUpdate, 1),
	}
	go n.runMatch(ctx)
	return n, nil
}

// Feed returns the MatchUpdate channel. Part of the Negotiator interface.
func (m *matchNegotiator) Feed() <-chan *MatchUpdate {
	return m.update
}

// runMatch is the match negotiation thread.
func (m *matchNegotiator) runMatch(ctx context.Context) {
	// do match stuff
	<-ctx.Done()
}
