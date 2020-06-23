// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package core

import (
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"time"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/client/db"
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/encode"
	"decred.org/dcrdex/dex/encrypt"
	"decred.org/dcrdex/dex/order"
	"decred.org/dcrdex/server/account"
	"github.com/decred/dcrd/dcrec/secp256k1/v2"
)

// errorSet is a slice of orders with a prefix prepended to the Error output.
type errorSet struct {
	prefix string
	errs   []error
}

// newErrorSet constructs an error set with a prefix.
func newErrorSet(s string, a ...interface{}) *errorSet {
	return &errorSet{prefix: fmt.Sprintf(s, a...)}
}

// add adds the message to the slice as an error and returns the errorSet.
func (set *errorSet) add(s string, a ...interface{}) *errorSet {
	set.errs = append(set.errs, fmt.Errorf(s, a...))
	return set
}

// addErr adds the error to the set.
func (set *errorSet) addErr(err error) *errorSet {
	set.errs = append(set.errs, err)
	return set
}

// If any returns the error set if there are any errors, else nil.
func (set *errorSet) ifany() error {
	if len(set.errs) > 0 {
		return set
	}
	return nil
}

// Error satisfies the error interface. Error strings are concatenated using a
// ", " and prepended with the prefix.
func (set *errorSet) Error() string {
	errStrings := make([]string, 0, len(set.errs))
	for i := range set.errs {
		errStrings = append(errStrings, set.errs[i].Error())
	}
	return set.prefix + strings.Join(errStrings, ", ")
}

// WalletForm is information necessary to create a new exchange wallet.
// The ConfigText, if provided, will be parsed for wallet connection settings.
// If ConfigText is not provided, and a file exists at the `asset.DefaultConfigPath`,
// that file will be parsed for wallet connection settings.
type WalletForm struct {
	AssetID    uint32
	Account    string
	ConfigText string
}

// WalletState is the current status of an exchange wallet.
type WalletState struct {
	Symbol  string      `json:"symbol"`
	AssetID uint32      `json:"assetID"`
	Open    bool        `json:"open"`
	Running bool        `json:"running"`
	Balance *db.Balance `json:"balance"`
	Address string      `json:"address"`
	FeeRate uint64      `json:"feerate"`
	Units   string      `json:"units"`
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

// RegisterForm is information necessary to register an account on a DEX.
type RegisterForm struct {
	Addr    string           `json:"url"`
	AppPass encode.PassBytes `json:"appPass"`
	Fee     uint64           `json:"fee"`
	Cert    string           `json:"cert"`
}

// Match represents a match on an order. An order may have many matches.
type Match struct {
	MatchID string            `json:"matchID"`
	Status  order.MatchStatus `json:"status"`
	Rate    uint64            `json:"rate"`
	Qty     uint64            `json:"qty"`
	Side    order.MatchSide   `json:"side"`
}

// Order is core's general type for an order. An order may be a market, limit,
// or cancel order. Some fields are only relevant to particular order types.
type Order struct {
	Host        string            `json:"host"`
	MarketID    string            `json:"market"`
	Type        order.OrderType   `json:"type"`
	ID          string            `json:"id"`
	Stamp       uint64            `json:"stamp"`
	Sig         dex.Bytes         `json:"sig"`
	Status      order.OrderStatus `json:"status"`
	Epoch       uint64            `json:"epoch"`
	Qty         uint64            `json:"qty"`
	Sell        bool              `json:"sell"`
	Filled      uint64            `json:"filled"`
	Matches     []*Match          `json:"matches"`
	Cancelling  bool              `json:"cancelling"`
	Canceled    bool              `json:"canceled"`
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
	pendingSuspend  *time.Timer
	suspended       bool
	mtx             sync.Mutex
}

// Display returns an ID string suitable for displaying in a UI.
func (m *Market) Display() string {
	return newDisplayIDFromSymbols(m.BaseSymbol, m.QuoteSymbol)
}

// mktID is a string ID constructed from the asset IDs.
func (m *Market) marketName() string {
	return marketName(m.BaseID, m.QuoteID)
}

// suspended returns the market's suspended state.
func (m *Market) Suspended() bool {
	m.mtx.Lock()
	defer m.mtx.Unlock()
	return m.suspended
}

// setSuspended sets the market's suspended state.
func (m *Market) setSuspended(state bool) {
	m.mtx.Lock()
	defer m.mtx.Unlock()
	m.suspended = state
}

// Exchange represents a single DEX with any number of markets.
type Exchange struct {
	Host          string                `json:"host"`
	Markets       map[string]*Market    `json:"markets"`
	Assets        map[uint32]*dex.Asset `json:"assets"`
	FeePending    bool                  `json:"feePending"`
	Connected     bool                  `json:"connected"`
	ConfsRequired uint32                `json:"confsrequired"`
	RegConfirms   *uint32               `json:"confs,omitempty"`
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
	Qty      float64 `json:"qty"`
	Rate     float64 `json:"rate"`
	Epoch    uint64  `json:"epoch"`
	Sell     bool    `json:"sell"`
	Token    string  `json:"token"`
	MarketID string  `json:"marketID"`
}

// RemainingUpdate is an update to the quantity for an order on the order book.
type RemainingUpdate struct {
	Token string  `json:"token"`
	Qty   float64 `json:"qty"`
}

// OrderBook represents an order book, which are sorted buys and sells, and
// unsorted epoch orders.
type OrderBook struct {
	Sells []*MiniOrder `json:"sells"`
	Buys  []*MiniOrder `json:"buys"`
	Epoch []*MiniOrder `json:"epoch"`
}

const (
	BookOrderAction   = "book_order"
	EpochOrderAction  = "epoch_order"
	UnbookOrderAction = "unbook_order"
)

// BookUpdate is an order book update.
type BookUpdate struct {
	Action   string      `json:"action"`
	Host     string      `json:"host"`
	MarketID string      `json:"marketID"`
	Payload  interface{} `json:"payload"`
}

// dexAccount is the core type to represent the client's account information for
// a DEX.
type dexAccount struct {
	host         string
	encKey       []byte
	keyMtx       sync.RWMutex
	privKey      *secp256k1.PrivateKey
	id           account.AccountID
	dexPubKey    *secp256k1.PublicKey
	feeCoin      []byte
	cert         []byte
	isPaid       bool
	authMtx      sync.RWMutex
	isAuthed     bool
	accountProof []byte
}

// newDEXAccount is a constructor for a new *dexAccount.
func newDEXAccount(acctInfo *db.AccountInfo) *dexAccount {
	return &dexAccount{
		host:         acctInfo.Host,
		encKey:       acctInfo.EncKey,
		dexPubKey:    acctInfo.DEXPubKey,
		isPaid:       acctInfo.Paid,
		feeCoin:      acctInfo.FeeCoin,
		cert:         acctInfo.Cert,
		accountProof: acctInfo.AccountProof,
	}
}

func (a *dexAccount) dbInfo() *db.AccountInfo {
	return &db.AccountInfo{
		Host:      a.host,
		Cert:      a.cert,
		EncKey:    a.encKey,
		DEXPubKey: a.dexPubKey,
		FeeCoin:   a.feeCoin,
	}
}

// ID returns the account ID.
func (a *dexAccount) ID() account.AccountID {
	a.keyMtx.RLock()
	defer a.keyMtx.RUnlock()
	return a.id
}

// setupEncryption creates and returns a new privkey for the account.
// Returns existing privkey if previously set up.
func (a *dexAccount) setupEncryption(crypter encrypt.Crypter) (*secp256k1.PrivateKey, error) {
	// Check if privKey exists. Check a.encKey instead of a.privKey
	// as a.privKey will be nil if the account is locked.
	a.keyMtx.RLock()
	if a.encKey != nil {
		defer a.keyMtx.RUnlock()
		// no need to generate a new privKey, return existing privKey AFTER unlocking.
		err := a.unlock(crypter)
		return a.privKey, err
	}
	a.keyMtx.RUnlock()

	// Create a new private key for the account.
	privKey, err := secp256k1.GeneratePrivateKey()
	if err != nil {
		return nil, fmt.Errorf("error creating acct private key: %v", err)
	}
	// Encrypt the private key.
	encKey, err := crypter.Encrypt(privKey.Serialize())
	if err != nil {
		return nil, fmt.Errorf("error encrypting acct private key: %v", err)
	}

	a.keyMtx.Lock()
	a.encKey = encKey
	a.privKey = privKey
	a.id = account.NewID(privKey.PubKey().SerializeCompressed())
	a.keyMtx.Unlock()

	return privKey, nil
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

// feePending checks whether the fee transaction has been broadcast, but the
// notifyfee request has not been sent/accepted yet.
func (a *dexAccount) feePending() bool {
	a.authMtx.RLock()
	defer a.authMtx.RUnlock()
	return !a.isPaid && len(a.feeCoin) > 0
}

// feePaid returns true if the account regisration fee has been accepted by the
// DEX.
func (a *dexAccount) feePaid() bool {
	a.authMtx.RLock()
	defer a.authMtx.RUnlock()
	return a.isPaid
}

// markFeePaid sets the account paid flag.
func (a *dexAccount) markFeePaid() {
	a.authMtx.Lock()
	a.isPaid = true
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

// checkSig checks the signature against the message and the DEX pubkey.
func (a *dexAccount) checkSig(msg []byte, sig []byte) error {
	_, err := checkSigS256(msg, a.dexPubKey.Serialize(), sig)
	return err
}

// TradeForm is used to place a market or limit order
type TradeForm struct {
	Host    string `json:"host"`
	IsLimit bool   `json:"isLimit"`
	Sell    bool   `json:"sell"`
	Base    uint32 `json:"base"`
	Quote   uint32 `json:"quote"`
	Qty     uint64 `json:"qty"`
	Rate    uint64 `json:"rate"`
	TifNow  bool   `json:"tifnow"`
}

// marketName is a string ID constructed from the asset IDs.
func marketName(b, q uint32) string {
	mkt, _ := dex.MarketName(b, q)
	return mkt
}

// token is a short representation of a byte-slice-like ID, such as a match ID
// or an order ID. The token is meant for display where the 64-character
// hexadecimal IDs are untenable.
func token(id []byte) string {
	if len(id) < 4 {
		return ""
	}
	return hex.EncodeToString(id[:4])
}

// coinIDString converts a coin ID to a human-readable string. If an error is
// encountered, the error is logged at Warning level, and the value
// "<invalid coin>" is returned.
func coinIDString(assetID uint32, coinID []byte) string {
	coinStr, err := asset.DecodeCoinID(assetID, coinID)
	if err != nil {
		log.Warnf("invalid coin ID %x for asset %d -> %s: %v", coinID, assetID, unbip(assetID), err)
		return "<invalid coin>"
	}
	return coinStr
}

// DEXBrief holds data returned from initializeDEXConnections.
type DEXBrief struct {
	Host      string `json:"host"`
	AcctID    string `json:"acctID"`
	AcctFound bool
	Authed    bool     `json:"authed"`
	AuthErr   string   `json:"autherr,omitempty"`
	TradeIDs  []string `json:"tradeIDs"`
}

// LoginResult holds data returned from Login.
type LoginResult struct {
	Notifications []*db.Notification `json:"notifications"`
	DEXes         []*DEXBrief        `json:"dexes"`
}

// RegisterResult holds data returned from Register.
type RegisterResult struct {
	FeeID       string `json:"feeID"`
	ReqConfirms uint16 `json:"reqConfirms"`
}

// assetCounter tracks a count for a series of assets and provides methods for
// adding to the count and combining assetCounters. Methods return the receiver
// for convenience.
type assetCounter map[uint32]int

// add increments the count for a specific asset.
func (c assetCounter) add(assetID uint32, increment int) assetCounter {
	c[assetID] = c[assetID] + increment
	return c
}

// absorb adds the counts from another assetCounter to the current counts.
func (c assetCounter) absorb(otherCounter assetCounter) assetCounter {
	for assetID, count := range otherCounter {
		c.add(assetID, count)
	}
	return c
}
