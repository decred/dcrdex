// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package core

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/client/db"
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/calc"
	"decred.org/dcrdex/dex/candles"
	"decred.org/dcrdex/dex/encode"
	"decred.org/dcrdex/dex/encrypt"
	"decred.org/dcrdex/dex/keygen"
	"decred.org/dcrdex/dex/msgjson"
	"decred.org/dcrdex/dex/order"
	"decred.org/dcrdex/server/account"
	"github.com/decred/dcrd/dcrec/secp256k1/v4"
	"github.com/decred/dcrd/hdkeychain/v3"
)

// HDKeyPurpose is here because we're high-jacking the BIP-32 + BIP-43 HD key
// system to create child keys, so we must set a the purpose field to create an
// hdkeychain.ExtendedKey.
var HDKeyPurpose uint32 = hdkeychain.HardenedKeyStart + 0x646578 // ASCII "dex"

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

// ifAny returns the error set if there are any errors, else nil.
func (set *errorSet) ifAny() error {
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
	return set.prefix + "{" + strings.Join(errStrings, ", ") + "}"
}

// WalletForm is information necessary to create a new exchange wallet.
// The ConfigText, if provided, will be parsed for wallet connection settings.
type WalletForm struct {
	AssetID uint32
	Config  map[string]string
	Type    string
}

// WalletBalance is an exchange wallet's balance which includes contractlocked
// amounts in addition to other balance details stored in db.
type WalletBalance struct {
	*db.Balance
	// OrderLocked is the total amount of funds that is currently locked
	// for swap, but not actually swapped yet. This amount is also included
	// in the `Locked` balance value.
	OrderLocked uint64 `json:"orderlocked"`
	// ContractLocked is the total amount of funds locked in unspent
	// (i.e. unredeemed / unrefunded) swap contracts.
	ContractLocked uint64 `json:"contractlocked"`
}

// WalletState is the current status of an exchange wallet.
type WalletState struct {
	Symbol       string            `json:"symbol"`
	AssetID      uint32            `json:"assetID"`
	Version      uint32            `json:"version"`
	WalletType   string            `json:"type"`
	Traits       asset.WalletTrait `json:"traits"`
	Open         bool              `json:"open"`
	Running      bool              `json:"running"`
	Balance      *WalletBalance    `json:"balance"`
	Address      string            `json:"address"`
	Units        string            `json:"units"`
	Encrypted    bool              `json:"encrypted"`
	PeerCount    uint32            `json:"peerCount"`
	Synced       bool              `json:"synced"`
	SyncProgress float32           `json:"syncProgress"`
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
	Asset   *uint32          `json:"assetID,omitempty"` // do not default to 0
	// Cert can be a string, which is interpreted as a filepath, or a []byte,
	// which is interpreted as the file contents of the certificate.
	Cert interface{} `json:"cert"`
}

// Match represents a match on an order. An order may have many matches.
type Match struct {
	MatchID       dex.Bytes         `json:"matchID"`
	Status        order.MatchStatus `json:"status"`
	Active        bool              `json:"active"`
	Revoked       bool              `json:"revoked"`
	Rate          uint64            `json:"rate"`
	Qty           uint64            `json:"qty"`
	Side          order.MatchSide   `json:"side"`
	FeeRate       uint64            `json:"feeRate"`
	Swap          *Coin             `json:"swap,omitempty"`
	CounterSwap   *Coin             `json:"counterSwap,omitempty"`
	Redeem        *Coin             `json:"redeem,omitempty"`
	CounterRedeem *Coin             `json:"counterRedeem,omitempty"`
	Refund        *Coin             `json:"refund,omitempty"`
	Stamp         uint64            `json:"stamp"`
	IsCancel      bool              `json:"isCancel"`
}

// Coin encodes both the coin ID and the asset-dependent string representation
// of the coin ID.
type Coin struct {
	ID       dex.Bytes `json:"id"`
	StringID string    `json:"stringID"`
	AssetID  uint32    `json:"assetID"`
	Symbol   string    `json:"symbol"`
	// Confs is populated only if this is a swap coin and we are waiting for
	// confirmations e.g. when this is the maker's swap and we're in
	// MakerSwapCast, or this is the takers swap, and we're in TakerSwapCast.
	Confs *Confirmations `json:"confs,omitempty"`
}

// NewCoin constructs a new Coin.
func NewCoin(assetID uint32, coinID []byte) *Coin {
	return &Coin{
		ID:       coinID,
		StringID: coinIDString(assetID, coinID),
		AssetID:  assetID,
		Symbol:   unbip(assetID),
	}
}

// Confirmations is the confirmation count and confirmation requirement of a
// Coin.
type Confirmations struct {
	Count    int64 `json:"count"`
	Required int64 `json:"required"`
}

// SetConfirmations sets the Confs field of the Coin.
func (c *Coin) SetConfirmations(confs, confReq int64) {
	c.Confs = &Confirmations{
		Count:    confs,
		Required: confReq,
	}
}

// matchFromMetaMatch constructs a *Match from an Order and a *MetaMatch.
// This function is intended for use with inactive matches. For active matches,
// use matchFromMetaMatchWithConfs.
func matchFromMetaMatch(ord order.Order, metaMatch *db.MetaMatch) *Match {
	return matchFromMetaMatchWithConfs(ord, metaMatch, 0, 0, 0, 0)
}

// matchFromMetaMatchWithConfs constructs a *Match from a *MetaMatch,
// and sets the confirmations for swaps-in-waiting.
func matchFromMetaMatchWithConfs(ord order.Order, metaMatch *db.MetaMatch, swapConfs, swapReq, counterSwapConfs, counterReq int64) *Match {
	if _, isCancel := ord.(*order.CancelOrder); isCancel {
		fmt.Println("matchFromMetaMatchWithConfs got a cancel order for match", metaMatch)
		return &Match{}
	}
	side := metaMatch.Side
	sell := ord.Trade().Sell
	status := metaMatch.Status

	fromID, toID := ord.Quote(), ord.Base()
	if sell {
		fromID, toID = ord.Base(), ord.Quote()
	}

	proof := &metaMatch.MetaData.Proof

	swapCoin, counterSwapCoin := proof.MakerSwap, proof.TakerSwap
	redeemCoin, counterRedeemCoin := proof.MakerRedeem, proof.TakerRedeem
	if side == order.Taker {
		swapCoin, counterSwapCoin = proof.TakerSwap, proof.MakerSwap
		redeemCoin, counterRedeemCoin = proof.TakerRedeem, proof.MakerRedeem
	}

	var refund, redeem, counterRedeem, swap, counterSwap *Coin
	if len(proof.RefundCoin) > 0 {
		refund = NewCoin(fromID, proof.RefundCoin)
	}
	if len(swapCoin) > 0 {
		swap = NewCoin(fromID, swapCoin)
		if (side == order.Taker && status == order.TakerSwapCast) ||
			(side == order.Maker && status == order.MakerSwapCast) &&
				!proof.IsRevoked() && swapReq > 0 {

			swap.SetConfirmations(swapConfs, swapReq)
		}
	}
	if len(counterSwapCoin) > 0 {
		counterSwap = NewCoin(toID, counterSwapCoin)
		if (side == order.Maker && status == order.TakerSwapCast) ||
			(side == order.Taker && status == order.MakerSwapCast) &&
				!proof.IsRevoked() && counterReq > 0 {

			counterSwap.SetConfirmations(counterSwapConfs, counterReq)
		}
	}
	if len(redeemCoin) > 0 {
		redeem = NewCoin(toID, redeemCoin)
	}
	if len(counterRedeemCoin) > 0 {
		counterRedeem = NewCoin(fromID, counterRedeemCoin)
	}

	userMatch, proof := metaMatch.UserMatch, &metaMatch.MetaData.Proof
	match := &Match{
		MatchID:       userMatch.MatchID[:],
		Status:        userMatch.Status,
		Active:        db.MatchIsActive(userMatch, proof),
		Revoked:       proof.IsRevoked(),
		Rate:          userMatch.Rate,
		Qty:           userMatch.Quantity,
		Side:          userMatch.Side,
		FeeRate:       userMatch.FeeRateSwap,
		Stamp:         metaMatch.MetaData.Stamp,
		IsCancel:      userMatch.Address == "",
		Swap:          swap,
		CounterSwap:   counterSwap,
		Redeem:        redeem,
		CounterRedeem: counterRedeem,
		Refund:        refund,
	}

	return match
}

// Order is core's general type for an order. An order may be a market, limit,
// or cancel order. Some fields are only relevant to particular order types.
type Order struct {
	Host         string            `json:"host"`
	BaseID       uint32            `json:"baseID"`
	BaseSymbol   string            `json:"baseSymbol"`
	QuoteID      uint32            `json:"quoteID"`
	QuoteSymbol  string            `json:"quoteSymbol"`
	MarketID     string            `json:"market"`
	Type         order.OrderType   `json:"type"`
	ID           dex.Bytes         `json:"id"`
	Stamp        uint64            `json:"stamp"`
	Sig          dex.Bytes         `json:"sig"`
	Status       order.OrderStatus `json:"status"`
	Epoch        uint64            `json:"epoch"`
	Qty          uint64            `json:"qty"`
	Sell         bool              `json:"sell"`
	Filled       uint64            `json:"filled"`
	Matches      []*Match          `json:"matches"`
	Cancelling   bool              `json:"cancelling"`
	Canceled     bool              `json:"canceled"`
	FeesPaid     *FeeBreakdown     `json:"feesPaid"`
	FundingCoins []*Coin           `json:"fundingCoins"`
	LockedAmt    uint64            `json:"lockedamt"`
	Rate         uint64            `json:"rate"` // limit only
	TimeInForce  order.TimeInForce `json:"tif"`  // limit only
}

// FeeBreakdown is categorized fee information.
type FeeBreakdown struct {
	Swap       uint64 `json:"swap"`
	Redemption uint64 `json:"redemption"`
}

// coreOrderFromTrade constructs an *Order from the supplied limit or market
// order and associated metadata.
func coreOrderFromTrade(ord order.Order, metaData *db.OrderMetaData) *Order {
	prefix, trade := ord.Prefix(), ord.Trade()

	var rate uint64
	var tif order.TimeInForce
	switch ot := ord.(type) {
	case *order.LimitOrder:
		rate = ot.Rate
		tif = ot.Force
	case *order.CancelOrder:
		fmt.Println("coreOrderFromTrade got a cancel order", ord.ID())
		return &Order{}
	}

	var cancelling, canceled bool
	if !metaData.LinkedOrder.IsZero() {
		if metaData.Status == order.OrderStatusCanceled {
			canceled = true
		} else {
			cancelling = true
		}
	}

	baseID, quoteID := ord.Base(), ord.Quote()
	fromID := quoteID
	if trade.Sell {
		fromID = baseID
	}

	fundingCoins := make([]*Coin, 0, len(trade.Coins))
	for i := range trade.Coins {
		fundingCoins = append(fundingCoins, NewCoin(fromID, trade.Coins[i]))
	}

	corder := &Order{
		Host:        metaData.Host,
		BaseID:      baseID,
		BaseSymbol:  unbip(baseID),
		QuoteID:     quoteID,
		QuoteSymbol: unbip(quoteID),
		MarketID:    marketName(baseID, quoteID),
		Type:        prefix.OrderType,
		ID:          ord.ID().Bytes(),
		Stamp:       encode.UnixMilliU(prefix.ServerTime),
		Sig:         metaData.Proof.DEXSig,
		Status:      metaData.Status,
		Rate:        rate,
		Qty:         trade.Quantity,
		Sell:        trade.Sell,
		Filled:      trade.Filled(),
		TimeInForce: tif,
		Canceled:    canceled,
		Cancelling:  cancelling,
		FeesPaid: &FeeBreakdown{
			Swap:       metaData.SwapFeesPaid,
			Redemption: metaData.RedemptionFeesPaid,
		},
		FundingCoins: fundingCoins,
	}

	return corder
}

// Market is market info.
type Market struct {
	Name            string        `json:"name"`
	BaseID          uint32        `json:"baseid"`
	BaseSymbol      string        `json:"basesymbol"`
	QuoteID         uint32        `json:"quoteid"`
	QuoteSymbol     string        `json:"quotesymbol"`
	LotSize         uint64        `json:"lotsize"`
	RateStep        uint64        `json:"ratestep"`
	EpochLen        uint64        `json:"epochlen"`
	StartEpoch      uint64        `json:"startepoch"`
	MarketBuyBuffer float64       `json:"buybuffer"`
	Orders          []*Order      `json:"orders"`
	SpotPrice       *msgjson.Spot `json:"spot"`
}

// BaseContractLocked is the amount of base asset locked in un-redeemed
// contracts.
func (m *Market) BaseContractLocked() uint64 {
	return m.sideContractLocked(true)
}

// QuoteContractLocked is the amount of quote asset locked in un-redeemed
// contracts.
func (m *Market) QuoteContractLocked() uint64 {
	return m.sideContractLocked(false)
}

func (m *Market) sideContractLocked(sell bool) (amt uint64) {
	for _, ord := range m.Orders {
		if ord.Sell != sell {
			continue
		}
		for _, match := range ord.Matches {
			if match.Status >= order.MakerRedeemed || match.Refund != nil {
				continue
			}
			if (match.Side == order.Maker && match.Status >= order.MakerSwapCast) ||
				(match.Side == order.Taker && match.Status == order.TakerSwapCast) {

				swapAmount := match.Qty
				if !ord.Sell {
					swapAmount = calc.BaseToQuote(match.Rate, match.Qty)
				}
				amt += swapAmount
			}
		}
	}
	return
}

// BaseOrderLocked is the amount of base asset locked in epoch or booked orders.
func (m *Market) BaseOrderLocked() uint64 {
	return m.sideOrderLocked(true)
}

// QuoteOrderLocked is the amount of quote asset locked in epoch or booked
// orders.
func (m *Market) QuoteOrderLocked() uint64 {
	return m.sideOrderLocked(false)
}

func (m *Market) sideOrderLocked(sell bool) (amt uint64) {
	for _, ord := range m.Orders {
		if ord.Sell != sell {
			continue
		}
		amt += ord.LockedAmt
	}
	return
}

// Display returns an ID string suitable for displaying in a UI.
func (m *Market) Display() string {
	return newDisplayIDFromSymbols(m.BaseSymbol, m.QuoteSymbol)
}

// mktID is a string ID constructed from the asset IDs.
func (m *Market) marketName() string {
	return marketName(m.BaseID, m.QuoteID)
}

// FeeAsset has an analogous msgjson type for server providing supported
// registration fee assets.
type FeeAsset struct {
	ID    uint32 `json:"id"`
	Confs uint32 `json:"confs"`
	Amt   uint64 `json:"amount"`
}

// PendingFeeState conveys a pending registration fee's asset and current
// confirmation count.
type PendingFeeState struct {
	Symbol  string `json:"symbol"`
	AssetID uint32 `json:"assetID"`
	Confs   uint32 `json:"confs"`
}

// Exchange represents a single DEX with any number of markets.
type Exchange struct {
	Host       string                `json:"host"`
	AcctID     string                `json:"acctID"`
	Markets    map[string]*Market    `json:"markets"`
	Assets     map[uint32]*dex.Asset `json:"assets"`
	Connected  bool                  `json:"connected"`
	Fee        *FeeAsset             `json:"feeAsset"` // DEPRECATED. DCR.
	RegFees    map[string]*FeeAsset  `json:"regFees"`
	PendingFee *PendingFeeState      `json:"pendingFee,omitempty"`
	CandleDurs []string              `json:"candleDurs"`
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
// Replaced MiniOrder, which had a float Qty in conventional units.
type MiniOrder struct {
	Qty       float64 `json:"qty"`
	QtyAtomic uint64  `json:"qtyAtomic"`
	Rate      float64 `json:"rate"`
	MsgRate   uint64  `json:"msgRate"`
	Epoch     uint64  `json:"epoch,omitempty"`
	Sell      bool    `json:"sell"`
	Token     string  `json:"token"`
}

// RemainderUpdate is an update to the quantity for an order on the order book.
// Replaced RemainingUpdate, which had a float Qty in conventional units.
type RemainderUpdate struct {
	Token     string  `json:"token"`
	Qty       float64 `json:"qty"`
	QtyAtomic uint64  `json:"qtyAtomic"`
}

// OrderBook represents an order book, which are sorted buys and sells, and
// unsorted epoch orders.
type OrderBook struct {
	Sells []*MiniOrder `json:"sells"`
	Buys  []*MiniOrder `json:"buys"`
	Epoch []*MiniOrder `json:"epoch"`
}

// MarketOrderBook is used as the BookUpdate's Payload with the FreshBookAction.
// The subscriber will likely need to translate into a JSON tagged type.
type MarketOrderBook struct {
	Base  uint32     `json:"base"`
	Quote uint32     `json:"quote"`
	Book  *OrderBook `json:"book"`
}

type CandleUpdate struct {
	Dur          string          `json:"dur"`
	DurMilliSecs uint64          `json:"ms"`
	Candle       *candles.Candle `json:"candle"`
}

const (
	FreshBookAction       = "book"
	FreshCandlesAction    = "candles"
	BookOrderAction       = "book_order"
	EpochOrderAction      = "epoch_order"
	UnbookOrderAction     = "unbook_order"
	UpdateRemainingAction = "update_remaining"
	CandleUpdateAction    = "candle_update"
)

// BookUpdate is an order book update.
type BookUpdate struct {
	Action   string      `json:"action"`
	Host     string      `json:"host"`
	MarketID string      `json:"marketID"`
	Payload  interface{} `json:"payload"`
}

type CandlesPayload struct {
	Dur          string           `json:"dur"`
	DurMilliSecs uint64           `json:"ms"`
	Candles      []msgjson.Candle `json:"candles"`
}

// dexAccount is the core type to represent the client's account information for
// a DEX.
type dexAccount struct {
	host      string
	cert      []byte
	dexPubKey *secp256k1.PublicKey

	keyMtx  sync.RWMutex
	encKey  []byte
	privKey *secp256k1.PrivateKey
	id      account.AccountID

	feeAssetID uint32
	feeCoin    []byte

	authMtx     sync.RWMutex
	isPaid      bool // feeCoin fully confirmed, ready to trade
	isAuthed    bool
	isSuspended bool
}

// newDEXAccount is a constructor for a new *dexAccount.
func newDEXAccount(acctInfo *db.AccountInfo) *dexAccount {
	return &dexAccount{
		host:       acctInfo.Host,
		encKey:     acctInfo.EncKey(),
		dexPubKey:  acctInfo.DEXPubKey,
		isPaid:     acctInfo.Paid,
		feeAssetID: acctInfo.FeeAssetID,
		feeCoin:    acctInfo.FeeCoin,
		cert:       acctInfo.Cert,
		// isSuspended is determined on connect, not stored
	}
}

// ID returns the account ID.
func (a *dexAccount) ID() account.AccountID {
	a.keyMtx.RLock()
	defer a.keyMtx.RUnlock()
	return a.id
}

// setupCryptoV2 generates a hierarchical deterministic key for the account.
// setupCryptoV2 should be called before adding the account's *dexConnection to
// the Core.conns map. This sets the dexAccount's encKey, privKey, and id.
func (a *dexAccount) setupCryptoV2(creds *db.PrimaryCredentials, crypter encrypt.Crypter, keyIndex uint32) error {
	if keyIndex >= hdkeychain.HardenedKeyStart {
		return fmt.Errorf("maximum key generation reached, cannot generate %dth key", keyIndex)
	}

	seed, err := crypter.Decrypt(creds.EncSeed)
	if err != nil {
		return fmt.Errorf("seed decryption error: %w", err)
	}

	dexPkB := a.dexPubKey.SerializeCompressed()
	// And because I'm neurotic.
	if len(dexPkB) != 33 {
		return fmt.Errorf("invalid dex pubkey length %d", len(dexPkB))
	}

	// Deterministically generate the DEX private key using a chain of extended
	// keys. We could surmise a hundred different algorithms to derive the DEX
	// key, and there's nothing particularly special about doing it this way,
	// but it works.

	// Prepare the chain of child indices.
	kids := make([]uint32, 0, 11) // 1 x purpose, 1 x version (incl. oddness), 8 x 4-byte uint32s, 1 x acct key index.
	// Hardened "purpose" key.
	kids = append(kids, HDKeyPurpose)
	// Second child is the the format/oddness byte.
	kids = append(kids, uint32(dexPkB[0]))
	byteSeq := dexPkB[1:]
	// Generate uint32's from the 4-byte chunks of the pubkey.
	for i := 0; i < 8; i++ {
		kids = append(kids, binary.LittleEndian.Uint32(byteSeq[i*4:i*4+4]))
	}
	// Last child is the account key index.
	kids = append(kids, keyIndex)

	// Harden children by modding i first, technically doubling the
	// collision rate, but OK.
	for i := 0; i < len(kids); i++ {
		kids[i] = kids[i]%hdkeychain.HardenedKeyStart + hdkeychain.HardenedKeyStart
	}

	extKey, err := keygen.GenDeepChild(seed, kids)
	if err != nil {
		return fmt.Errorf("GenDeepChild error: %w", err)
	}

	privB, err := extKey.SerializedPrivKey()
	if err != nil {
		return fmt.Errorf("SerializedPrivKey error: %w", err)
	}

	encKey, err := crypter.Encrypt(privB)
	if err != nil {
		return fmt.Errorf("key Encrypt error: %w", err)
	}

	pkBytes := extKey.SerializedPubKey()
	priv := secp256k1.PrivKeyFromBytes(privB)

	a.keyMtx.Lock()
	a.encKey = encKey
	a.privKey = priv
	a.id = account.NewID(pkBytes)
	a.keyMtx.Unlock()

	return nil
}

// unlock decrypts the account private key.
func (a *dexAccount) unlock(crypter encrypt.Crypter) error {
	keyB, err := crypter.Decrypt(a.encKey)
	if err != nil {
		return err
	}
	privKey := secp256k1.PrivKeyFromBytes(keyB)
	pubKey := privKey.PubKey()
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
// request has been successfully sent.
func (a *dexAccount) authed() bool {
	a.authMtx.RLock()
	defer a.authMtx.RUnlock()
	return a.isAuthed
}

// auth sets the account as authenticated, but possibly suspended (may not place
// new orders, but may still be negotiating swaps).
func (a *dexAccount) auth(suspended bool) {
	a.authMtx.Lock()
	a.isAuthed = true
	a.isSuspended = suspended
	a.authMtx.Unlock()
}

// unauth sets the account as un-authenticated.
func (a *dexAccount) unauth() {
	a.authMtx.Lock()
	a.isAuthed = false
	a.authMtx.Unlock()
}

// suspended will be true if the account was suspended as of the latest authDEX.
func (a *dexAccount) suspended() bool {
	a.authMtx.RLock()
	defer a.authMtx.RUnlock()
	return a.isSuspended
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
	return signMsg(a.privKey, msg), nil
}

// checkSig checks the signature against the message and the DEX pubkey.
func (a *dexAccount) checkSig(msg []byte, sig []byte) error {
	return checkSigS256(msg, a.dexPubKey.SerializeCompressed(), sig)
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
		return "<invalid coin>:" + hex.EncodeToString(coinID)
	}
	return coinStr
}

// DEXBrief holds data returned from initializeDEXConnections.
type DEXBrief struct {
	Host     string   `json:"host"`
	AcctID   string   `json:"acctID"`
	Authed   bool     `json:"authed"`
	AuthErr  string   `json:"autherr,omitempty"`
	TradeIDs []string `json:"tradeIDs"`
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

// OrderFilter is almost the same as db.OrderFilter, except the Offset order ID
// is a dex.Bytes instead of a order.OrderID.
type OrderFilter struct {
	N        int                 `json:"n"`
	Offset   dex.Bytes           `json:"offset"`
	Hosts    []string            `json:"hosts"`
	Assets   []uint32            `json:"assets"`
	Statuses []order.OrderStatus `json:"statuses"`
}

// Account holds data returned from AccountExport.
type Account struct {
	Host          string `json:"host"`
	AccountID     string `json:"accountID"`
	PrivKey       string `json:"privKey"`
	DEXPubKey     string `json:"DEXPubKey"`
	Cert          string `json:"cert"`
	FeeCoin       string `json:"feeCoin"`
	FeeProofSig   string `json:"feeProofSig"`
	FeeProofStamp uint64 `json:"FeeProofStamp"`
}

// assetMap tracks a series of assets and provides methods for registering an
// asset and merging with another assetMap.
type assetMap map[uint32]struct{}

// count registers a new asset.
func (c assetMap) count(assetID uint32) {
	c[assetID] = struct{}{}
}

// merge merges the entries of another assetMap.
func (c assetMap) merge(other assetMap) {
	for assetID := range other {
		c[assetID] = struct{}{}
	}
}

// MaxOrderEstimate is an estimate of the fees and locked amounts associated
// with an order.
type MaxOrderEstimate struct {
	Swap   *asset.SwapEstimate   `json:"swap"`
	Redeem *asset.RedeemEstimate `json:"redeem"`
}

// OrderEstimate is a Core.PreOrder estimate.
type OrderEstimate struct {
	Swap   *asset.PreSwap   `json:"swap"`
	Redeem *asset.PreRedeem `json:"redeem"`
}
