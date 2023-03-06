// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package core

import (
	"fmt"
	"sync/atomic"

	"decred.org/dcrdex/client/comms"
	"decred.org/dcrdex/client/db"
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/msgjson"
)

// Notifications should use the following note type strings.
const (
	NoteTypeFeePayment   = "feepayment"
	NoteTypeBondPost     = "bondpost"
	NoteTypeBondRefund   = "bondrefund"
	NoteTypeSend         = "send"
	NoteTypeOrder        = "order"
	NoteTypeMatch        = "match"
	NoteTypeEpoch        = "epoch"
	NoteTypeConnEvent    = "conn"
	NoteTypeBalance      = "balance"
	NoteTypeSpots        = "spots"
	NoteTypeWalletConfig = "walletconfig"
	NoteTypeWalletState  = "walletstate"
	NoteTypeServerNotify = "notify"
	NoteTypeSecurity     = "security"
	NoteTypeUpgrade      = "upgrade"
	NoteTypeBot          = "bot"
	NoteTypeDEXAuth      = "dex_auth"
	NoteTypeFiatRates    = "fiatrateupdate"
	NoteTypeCreateWallet = "createwallet"
	NoteTypeLogin        = "login"
)

var noteChanCounter uint64

func (c *Core) logNote(n Notification) {
	// Do not log certain spammy note types that have no value in logs.
	switch n.Type() {
	case NoteTypeSpots: // expand this case as needed
		return
	default:
	}
	if n.Subject() == "" && n.Details() == "" {
		return
	}

	logFun := c.log.Warnf // default in case the Severity level is unknown to notify
	switch n.Severity() {
	case db.Data:
		logFun = c.log.Tracef
	case db.Poke:
		logFun = c.log.Debugf
	case db.Success:
		logFun = c.log.Infof
	case db.WarningLevel:
		logFun = c.log.Warnf
	case db.ErrorLevel:
		logFun = c.log.Errorf
	}

	logFun("notify: %v", n)
}

// notify sends a notification to all subscribers. If the notification is of
// sufficient severity, it is stored in the database.
func (c *Core) notify(n Notification) {
	if n.Severity() >= db.Success {
		c.db.SaveNotification(n.DBNote())
	}

	c.logNote(n)

	c.noteMtx.RLock()
	for _, ch := range c.noteChans {
		select {
		case ch <- n:
		default:
			c.log.Errorf("blocking notification channel")
		}
	}
	c.noteMtx.RUnlock()
}

// NotificationFeed returns a new receiving channel for notifications. The
// channel has capacity 1024, and should be monitored for the lifetime of the
// Core. Blocking channels are silently ignored.
func (c *Core) NotificationFeed() <-chan Notification {
	_, ch := c.notificationFeed()
	return ch
}

func (c *Core) notificationFeed() (uint64, <-chan Notification) {
	ch := make(chan Notification, 1024)
	cid := atomic.AddUint64(&noteChanCounter, 1)
	c.noteMtx.Lock()
	c.noteChans[cid] = ch
	c.noteMtx.Unlock()
	return cid, ch
}

func (c *Core) returnFeed(channelID uint64) {
	c.noteMtx.Lock()
	delete(c.noteChans, channelID)
	c.noteMtx.Unlock()
}

// AckNotes sets the acknowledgement field for the notifications.
func (c *Core) AckNotes(ids []dex.Bytes) {
	for _, id := range ids {
		err := c.db.AckNotification(id)
		if err != nil {
			c.log.Errorf("error saving notification acknowledgement for %s: %v", id, err)
		}
	}
}

func (c *Core) formatDetails(topic Topic, args ...interface{}) (translatedSubject, details string) {
	return translator.Format(string(topic), args...)
}

// Notification is an interface for a user notification. Notification is
// satisfied by db.Notification, so concrete types can embed the db type.
type Notification interface {
	// Type is a string ID unique to the concrete type.
	Type() string
	// Topic is a string ID unique to the message subject. Since subjects must
	// be translated, we cannot rely on the subject to programmatically identify
	// the message.
	Topic() Topic
	// Subject is a short description of the notification contents. When displayed
	// to the user, the Subject will typically be given visual prominence. For
	// notifications with Severity < Poke (not meant for display), the Subject
	// field may be repurposed as a second-level category ID.
	Subject() string
	// Details should contain more detailed information.
	Details() string
	// Severity is the notification severity.
	Severity() db.Severity
	// Time is the notification timestamp. The timestamp is set in
	// db.NewNotification. Time is a UNIX timestamp, in milliseconds.
	Time() uint64
	// Acked is true if the user has seen the notification. Acknowledgement is
	// recorded with (*Core).AckNotes.
	Acked() bool
	// ID should be unique, except in the case of identical copies of
	// db.Notification where the IDs should be the same.
	ID() dex.Bytes
	// Stamp sets the notification timestamp. If db.NewNotification is used to
	// construct the db.Notification, the timestamp will already be set.
	Stamp()
	// DBNote returns the underlying *db.Notification.
	DBNote() *db.Notification
	// String generates a compact human-readable representation of the
	// Notification that is suitable for logging.
	String() string
}

// Topic is a language-independent unique ID for a Notification.
type Topic = db.Topic

// SecurityNote is a note regarding application security, credentials, or
// authentication.
type SecurityNote struct {
	db.Notification
}

// const name needs to be the same as value in order to translation tool to
// work properly.
const (
	TopicSeedNeedsSaving Topic = "TopicSeedNeedsSaving"
	TopicUpgradedToSeed  Topic = "TopicUpgradedToSeed"
)

func newSecurityNote(topic Topic, subject, details string, severity db.Severity) *SecurityNote {
	return &SecurityNote{
		Notification: db.NewNotification(NoteTypeSecurity, topic, subject, details, severity),
	}
}

// const name needs to be the same as value in order to translation tool to
// work properly.
const (
	TopicFeePaymentInProgress    Topic = "TopicFeePaymentInProgress"
	TopicFeePaymentError         Topic = "TopicFeePaymentError"
	TopicFeeCoinError            Topic = "TopicFeeCoinError"
	TopicRegUpdate               Topic = "TopicRegUpdate"
	TopicBondConfirming          Topic = "TopicBondConfirming"
	TopicBondPostError           Topic = "TopicBondPostError"
	TopicBondCoinError           Topic = "TopicBondCoinError"
	TopicAccountRegistered       Topic = "TopicAccountRegistered"
	TopicAccountUnlockError      Topic = "TopicAccountUnlockError"
	TopicWalletConnectionWarning Topic = "TopicWalletConnectionWarning"
	TopicWalletUnlockError       Topic = "TopicWalletUnlockError"
	TopicWalletCommsWarning      Topic = "TopicWalletCommsWarning"
	TopicWalletPeersRestored     Topic = "TopicWalletPeersRestored"
	TopicBondRefunded            Topic = "TopicBondRefunded"
)

// FeePaymentNote is a notification regarding registration fee payment.
type FeePaymentNote struct {
	db.Notification
	Asset         *uint32 `json:"asset,omitempty"`
	Confirmations *uint32 `json:"confirmations,omitempty"`
	Dex           string  `json:"dex,omitempty"`
}

func newFeePaymentNote(topic Topic, subject, details string, severity db.Severity, dexAddr string) *FeePaymentNote {
	host, _ := addrHost(dexAddr)
	return &FeePaymentNote{
		Notification: db.NewNotification(NoteTypeFeePayment, topic, subject, details, severity),
		Dex:          host,
	}
}

func newFeePaymentNoteWithConfirmations(topic Topic, subject, details string, severity db.Severity, asset, currConfs uint32, dexAddr string) *FeePaymentNote {
	feePmtNt := newFeePaymentNote(topic, subject, details, severity, dexAddr)
	feePmtNt.Asset = &asset
	feePmtNt.Confirmations = &currConfs
	return feePmtNt
}

// BondRefundNote is a notification regarding bond refunds.
type BondRefundNote struct {
	db.Notification
}

func newBondRefundNote(topic Topic, subject, details string, severity db.Severity) *BondRefundNote {
	return &BondRefundNote{
		Notification: db.NewNotification(NoteTypeBondRefund, topic, subject, details, severity),
	}
}

// BondPostNote is a notification regarding bond posting.
type BondPostNote struct {
	db.Notification
	Asset         *uint32 `json:"asset,omitempty"`
	Confirmations *int32  `json:"confirmations,omitempty"`
	Tier          *int64  `json:"tier,omitempty"`
	CoinID        *string `json:"coinID,omitempty"`
	Dex           string  `json:"dex,omitempty"`
}

func newBondPostNote(topic Topic, subject, details string, severity db.Severity, dexAddr string) *BondPostNote {
	host, _ := addrHost(dexAddr)
	return &BondPostNote{
		Notification: db.NewNotification(NoteTypeBondPost, topic, subject, details, severity),
		Dex:          host,
	}
}

func newBondPostNoteWithConfirmations(topic Topic, subject, details string, severity db.Severity, asset uint32, coinID string, currConfs int32, dexAddr string) *BondPostNote {
	bondPmtNt := newBondPostNote(topic, subject, details, severity, dexAddr)
	bondPmtNt.Asset = &asset
	bondPmtNt.CoinID = &coinID
	bondPmtNt.Confirmations = &currConfs
	return bondPmtNt
}

func newBondPostNoteWithTier(topic Topic, subject, details string, severity db.Severity, dexAddr string, tier int64) *BondPostNote {
	bondPmtNt := newBondPostNote(topic, subject, details, severity, dexAddr)
	bondPmtNt.Tier = &tier
	return bondPmtNt
}

// SendNote is a notification regarding a requested send or withdraw.
type SendNote struct {
	db.Notification
}

const (
	TopicSendError   Topic = "TopicSendError"
	TopicSendSuccess Topic = "TopicSendSuccess"
)

func newSendNote(topic Topic, subject, details string, severity db.Severity) *SendNote {
	return &SendNote{
		Notification: db.NewNotification(NoteTypeSend, topic, subject, details, severity),
	}
}

// OrderNote is a notification about an order or a match.
type OrderNote struct {
	db.Notification
	Order       *Order `json:"order"`
	TemporaryID uint64 `json:"tempID,omitempty"`
}

// const name needs to be the same as value in order to translation tool to
// work properly.
const (
	TopicOrderLoadFailure     Topic = "TopicOrderLoadFailure"
	TopicOrderResumeFailure   Topic = "TopicOrderResumeFailure"
	TopicBuyOrderPlaced       Topic = "TopicBuyOrderPlaced"
	TopicSellOrderPlaced      Topic = "TopicSellOrderPlaced"
	TopicYoloPlaced           Topic = "TopicYoloPlaced"
	TopicMissingMatches       Topic = "TopicMissingMatches"
	TopicWalletMissing        Topic = "TopicWalletMissing"
	TopicMatchErrorCoin       Topic = "TopicMatchErrorCoin"
	TopicMatchErrorContract   Topic = "TopicMatchErrorContract"
	TopicMatchRecoveryError   Topic = "TopicMatchRecoveryError"
	TopicOrderCoinError       Topic = "TopicOrderCoinError"
	TopicOrderCoinFetchError  Topic = "TopicOrderCoinFetchError"
	TopicPreimageSent         Topic = "TopicPreimageSent"
	TopicCancelPreimageSent   Topic = "TopicCancelPreimageSent"
	TopicMissedCancel         Topic = "TopicMissedCancel"
	TopicOrderBooked          Topic = "TopicOrderBooked"
	TopicNoMatch              Topic = "TopicNoMatch"
	TopicBuyOrderCanceled     Topic = "TopicBuyOrderCanceled"
	TopicSellOrderCanceled    Topic = "TopicSellOrderCanceled"
	TopicCancel               Topic = "TopicCancel"
	TopicBuyMatchesMade       Topic = "TopicBuyMatchesMade"
	TopicSellMatchesMade      Topic = "TopicSellMatchesMade"
	TopicSwapSendError        Topic = "TopicSwapSendError"
	TopicInitError            Topic = "TopicInitError"
	TopicReportRedeemError    Topic = "TopicReportRedeemError"
	TopicSwapsInitiated       Topic = "TopicSwapsInitiated"
	TopicRedemptionError      Topic = "TopicRedemptionError"
	TopicMatchComplete        Topic = "TopicMatchComplete"
	TopicRefundFailure        Topic = "TopicRefundFailure"
	TopicMatchesRefunded      Topic = "TopicMatchesRefunded"
	TopicMatchRevoked         Topic = "TopicMatchRevoked"
	TopicOrderRevoked         Topic = "TopicOrderRevoked"
	TopicOrderAutoRevoked     Topic = "TopicOrderAutoRevoked"
	TopicMatchRecovered       Topic = "TopicMatchRecovered"
	TopicCancellingOrder      Topic = "TopicCancellingOrder"
	TopicOrderStatusUpdate    Topic = "TopicOrderStatusUpdate"
	TopicMatchResolutionError Topic = "TopicMatchResolutionError"
	TopicFailedCancel         Topic = "TopicFailedCancel"
	TopicOrderLoaded          Topic = "TopicOrderLoaded"
	TopicOrderRetired         Topic = "TopicOrderRetired"
	TopicAsyncOrderFailure    Topic = "TopicAsyncOrderFailure"
	TopicAsyncOrderSubmitted  Topic = "TopicAsyncOrderSubmitted"
)

func newOrderNote(topic Topic, subject, details string, severity db.Severity, corder *Order) *OrderNote {
	return &OrderNote{
		Notification: db.NewNotification(NoteTypeOrder, topic, subject, details, severity),
		Order:        corder,
	}
}

func newOrderNoteWithTempID(topic Topic, subject, details string, severity db.Severity, corder *Order, tempID uint64) *OrderNote {
	note := newOrderNote(topic, subject, details, severity, corder)
	note.TemporaryID = tempID
	return note
}

// MatchNote is a notification about a match.
type MatchNote struct {
	db.Notification
	OrderID  dex.Bytes `json:"orderID"`
	Match    *Match    `json:"match"`
	Host     string    `json:"host"`
	MarketID string    `json:"marketID"`
}

const (
	TopicAudit                 Topic = "TopicAudit"
	TopicAuditTrouble          Topic = "TopicAuditTrouble"
	TopicNewMatch              Topic = "TopicNewMatch"
	TopicCounterConfirms       Topic = "TopicCounterConfirms"
	TopicConfirms              Topic = "TopicConfirms"
	TopicRedemptionResubmitted Topic = "TopicRedemptionResubmitted"
	TopicSwapRefunded          Topic = "TopicSwapRefunded"
	TopicRedemptionConfirmed   Topic = "TopicRedemptionConfirmed"
)

func newMatchNote(topic Topic, subject, details string, severity db.Severity, t *trackedTrade, match *matchTracker) *MatchNote {
	swapConfs, counterConfs := match.confirms()
	if counterConfs < 0 {
		// This can be -1 before it is actually checked, but for purposes of the
		// match note, it should be non-negative.
		counterConfs = 0
	}
	return &MatchNote{
		Notification: db.NewNotification(NoteTypeMatch, topic, subject, details, severity),
		OrderID:      t.ID().Bytes(),
		Match: matchFromMetaMatchWithConfs(t.Order, &match.MetaMatch, swapConfs,
			int64(t.metaData.FromSwapConf), counterConfs, int64(t.metaData.ToSwapConf),
			int64(match.redemptionConfs), int64(match.redemptionConfsReq)),
		Host:     t.dc.acct.host,
		MarketID: marketName(t.Base(), t.Quote()),
	}
}

// String supplements db.Notification's Stringer with the Order's ID, if the
// Order is not nil.
func (on *OrderNote) String() string {
	base := on.Notification.String()
	if on.Order == nil {
		return base
	}
	return fmt.Sprintf("%s - Order: %s", base, on.Order.ID)
}

// EpochNotification is a data notification that a new epoch has begun.
type EpochNotification struct {
	db.Notification
	Host     string `json:"host"`
	MarketID string `json:"marketID"`
	Epoch    uint64 `json:"epoch"`
}

const TopicEpoch Topic = "Epoch"

func newEpochNotification(host, mktID string, epochIdx uint64) *EpochNotification {
	return &EpochNotification{
		Host:         host,
		MarketID:     mktID,
		Notification: db.NewNotification(NoteTypeEpoch, TopicEpoch, "", "", db.Data),
		Epoch:        epochIdx,
	}
}

// String supplements db.Notification's Stringer with the Epoch index.
func (on *EpochNotification) String() string {
	return fmt.Sprintf("%s - Index: %d", on.Notification.String(), on.Epoch)
}

// ConnEventNote is a notification regarding individual DEX connection status.
type ConnEventNote struct {
	db.Notification
	Host             string                 `json:"host"`
	ConnectionStatus comms.ConnectionStatus `json:"connectionStatus"`
}

const (
	TopicDEXConnected    Topic = "TopicDEXConnected"
	TopicDEXDisconnected Topic = "TopicDEXDisconnected"
)

func newConnEventNote(topic Topic, subject, host string, status comms.ConnectionStatus, details string, severity db.Severity) *ConnEventNote {
	return &ConnEventNote{
		Notification:     db.NewNotification(NoteTypeConnEvent, topic, subject, details, severity),
		Host:             host,
		ConnectionStatus: status,
	}
}

// FiatRatesNote is an update of fiat rate data for assets.
type FiatRatesNote struct {
	db.Notification
	FiatRates map[uint32]float64 `json:"fiatRates"`
}

const TopicFiatRatesUpdate Topic = "fiatrateupdate"

func newFiatRatesUpdate(rates map[uint32]float64) *FiatRatesNote {
	return &FiatRatesNote{
		Notification: db.NewNotification(NoteTypeFiatRates, TopicFiatRatesUpdate, "", "", db.Data),
		FiatRates:    rates,
	}
}

// BalanceNote is an update to a wallet's balance.
type BalanceNote struct {
	db.Notification
	AssetID uint32         `json:"assetID"`
	Balance *WalletBalance `json:"balance"`
}

const TopicBalanceUpdated Topic = "BalanceUpdated"

func newBalanceNote(assetID uint32, bal *WalletBalance) *BalanceNote {
	return &BalanceNote{
		Notification: db.NewNotification(NoteTypeBalance, TopicBalanceUpdated, "", "", db.Data),
		AssetID:      assetID,
		Balance:      bal, // Once created, balance is never modified by Core.
	}
}

// SpotPriceNote is a notification of an update to the market's spot price.
type SpotPriceNote struct {
	db.Notification
	Host  string                   `json:"host"`
	Spots map[string]*msgjson.Spot `json:"spots"`
}

const TopicSpotsUpdate Topic = "SpotsUpdate"

func newSpotPriceNote(host string, spots map[string]*msgjson.Spot) *SpotPriceNote {
	return &SpotPriceNote{
		Notification: db.NewNotification(NoteTypeSpots, TopicSpotsUpdate, "", "", db.Data),
		Host:         host,
		Spots:        spots,
	}
}

// DEXAuthNote is a notification regarding individual DEX authentication status.
type DEXAuthNote struct {
	db.Notification
	Host          string `json:"host"`
	Authenticated bool   `json:"authenticated"`
}

const (
	TopicDexAuthError     Topic = "TopicDexAuthError"
	TopicUnknownOrders    Topic = "TopicUnknownOrders"
	TopicOrdersReconciled Topic = "TopicOrdersReconciled"
	TopicBondConfirmed    Topic = "TopicBondConfirmed"
	TopicBondExpired      Topic = "TopicBondExpired"
)

func newDEXAuthNote(topic Topic, subject, host string, authenticated bool, details string, severity db.Severity) *DEXAuthNote {
	return &DEXAuthNote{
		Notification:  db.NewNotification(NoteTypeDEXAuth, topic, subject, details, severity),
		Host:          host,
		Authenticated: authenticated,
	}
}

// WalletConfigNote is a notification regarding a change in wallet
// configuration.
type WalletConfigNote struct {
	db.Notification
	Wallet *WalletState `json:"wallet"`
}

const (
	TopicWalletConfigurationUpdated Topic = "TopicWalletConfigurationUpdated"
	TopicWalletPasswordUpdated      Topic = "TopicWalletPasswordUpdated"
	TopicWalletPeersWarning         Topic = "TopicWalletPeersWarning"
	TopicWalletTypeDeprecated       Topic = "TopicWalletTypeDeprecated"
	TopicWalletPeersUpdate          Topic = "TopicWalletPeersUpdate"
)

func newWalletConfigNote(topic Topic, subject, details string, severity db.Severity, walletState *WalletState) *WalletConfigNote {
	return &WalletConfigNote{
		Notification: db.NewNotification(NoteTypeWalletConfig, topic, subject, details, severity),
		Wallet:       walletState,
	}
}

// WalletStateNote is a notification regarding a change in wallet state,
// including: creation, locking, unlocking, connect, disabling and enabling. This
// is intended to be a Data Severity notification.
type WalletStateNote WalletConfigNote

const TopicWalletState Topic = "WalletState"

func newWalletStateNote(walletState *WalletState) *WalletStateNote {
	return &WalletStateNote{
		Notification: db.NewNotification(NoteTypeWalletState, TopicWalletState, "", "", db.Data),
		Wallet:       walletState,
	}
}

// ServerNotifyNote is a notification containing a server-originating message.
type ServerNotifyNote struct {
	db.Notification
}

const (
	TopicMarketSuspendScheduled   Topic = "TopicMarketSuspendScheduled"
	TopicMarketSuspended          Topic = "TopicMarketSuspended"
	TopicMarketSuspendedWithPurge Topic = "TopicMarketSuspendedWithPurge"
	TopicMarketResumeScheduled    Topic = "TopicMarketResumeScheduled"
	TopicMarketResumed            Topic = "TopicMarketResumed"
	TopicPenalized                Topic = "TopicPenalized"
	TopicDEXNotification          Topic = "TopicDEXNotification"
	TopicUpgradeNeeded            Topic = "TopicUpgradeNeeded"
)

func newServerNotifyNote(topic Topic, subject, details string, severity db.Severity) *ServerNotifyNote {
	return &ServerNotifyNote{
		Notification: db.NewNotification(NoteTypeServerNotify, topic, subject, details, severity),
	}
}

// UpgradeNote is a notification regarding an outdated client.
type UpgradeNote struct {
	db.Notification
}

func newUpgradeNote(topic Topic, subject, details string, severity db.Severity) *UpgradeNote {
	return &UpgradeNote{
		Notification: db.NewNotification(NoteTypeUpgrade, topic, subject, details, severity),
	}
}

// WalletCreationNote is a notification regarding asynchronous wallet creation.
type WalletCreationNote struct {
	db.Notification
	AssetID uint32 `json:"assetID"`
}

const (
	TopicQueuedCreationFailed  Topic = "TopicQueuedCreationFailed"
	TopicQueuedCreationSuccess Topic = "TopicQueuedCreationSuccess"
	TopicCreationQueued        Topic = "TopicCreationQueued"
)

func newWalletCreationNote(topic Topic, subject, details string, severity db.Severity, assetID uint32) *WalletCreationNote {
	return &WalletCreationNote{
		Notification: db.NewNotification(NoteTypeCreateWallet, topic, subject, details, severity),
		AssetID:      assetID,
	}
}

// BotNote is a note that describes the operation of a automated trading bot.
type BotNote struct {
	db.Notification
	Report *BotReport `json:"report"`
}

const (
	TopicBotCreated Topic = "TopicBotCreated"
	TopicBotStarted Topic = "TopicBotStarted"
	TopicBotStopped Topic = "TopicBotStopped"
	TopicBotUpdated Topic = "TopicBotUpdated"
	TopicBotRetired Topic = "TopicBotRetired"
)

func newBotNote(topic Topic, subject, details string, severity db.Severity, report *BotReport) *BotNote {
	return &BotNote{
		Notification: db.NewNotification(NoteTypeBot, topic, subject, details, severity),
		Report:       report,
	}
}

// LoginNote is a notification with the recent login status.
type LoginNote struct {
	db.Notification
}

const TopicLoginStatus Topic = "TopicLoginStatus"

func newLoginNote(message string) *LoginNote {
	return &LoginNote{
		Notification: db.NewNotification(NoteTypeLogin, TopicLoginStatus, "", message, db.Data),
	}
}
