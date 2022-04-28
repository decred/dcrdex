// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package core

import (
	"fmt"

	"decred.org/dcrdex/client/comms"
	"decred.org/dcrdex/client/db"
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/msgjson"
)

// Notifications should use the following note type strings.
const (
	NoteTypeFeePayment   = "feepayment"
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
	NoteTypeDEXAuth      = "dex_auth"
	NoteTypeFiatRates    = "fiatrateupdate"
)

func (c *Core) logNote(n Notification) {
	// Do not log certain spammy note types that have no value in logs.
	switch n.Type() {
	case NoteTypeSpots: // expand this case as needed
		return
	default:
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
	ch := make(chan Notification, 1024)
	c.noteMtx.Lock()
	c.noteChans = append(c.noteChans, ch)
	c.noteMtx.Unlock()
	return ch
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
	trans, found := c.locale[topic]
	if !found {
		c.log.Errorf("no translation found for topic %q", topic)
		return string(topic), "translation error"
	}
	return trans.subject, c.localePrinter.Sprintf(string(topic), args...)
}

// Notification is an interface for a user notification. Notification is
// satisfied by db.Notification, so concrete types can embed the db type.
type Notification interface {
	// Type is a string ID unique to the concrete type.
	Type() string
	// Topic is a string ID unique to the message subject. Since subjects must
	// be translated, we cannot rely on the subject to programatically identify
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

const (
	TopicSeedNeedsSaving Topic = "SeedNeedsSaving"
	TopicUpgradedToSeed  Topic = "UpgradedToSeed"
)

func newSecurityNote(topic Topic, subject, details string, severity db.Severity) *SecurityNote {
	return &SecurityNote{
		Notification: db.NewNotification(NoteTypeSecurity, topic, subject, details, severity),
	}
}

// FeePaymentNote is a notification regarding registration fee payment.
type FeePaymentNote struct {
	db.Notification
	Asset         *uint32 `json:"asset,omitempty"`
	Confirmations *uint32 `json:"confirmations,omitempty"`
	Dex           string  `json:"dex,omitempty"`
}

const (
	TopicFeePaymentInProgress    Topic = "FeePaymentInProgress"
	TopicRegUpdate               Topic = "RegUpdate"
	TopicFeePaymentError         Topic = "FeePaymentError"
	TopicAccountRegistered       Topic = "AccountRegistered"
	TopicAccountUnlockError      Topic = "AccountUnlockError"
	TopicFeeCoinError            Topic = "FeeCoinError"
	TopicWalletConnectionWarning Topic = "WalletConnectionWarning"
	TopicWalletUnlockError       Topic = "WalletUnlockError"
	TopicWalletPeersWarning      Topic = "WalletPeersWarning"
	TopicWalletCommsWarning      Topic = "WalletCommsWarning"
	TopicWalletPeersRestored     Topic = "WalletPeersRestored"
)

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

// SendNote is a notification regarding a requested send or withdraw.
type SendNote struct {
	db.Notification
}

const (
	TopicSendError   Topic = "SendError"
	TopicSendSuccess Topic = "SendSuccess"
)

func newSendNote(topic Topic, subject, details string, severity db.Severity) *SendNote {
	return &SendNote{
		Notification: db.NewNotification(NoteTypeSend, topic, subject, details, severity),
	}
}

// OrderNote is a notification about an order or a match.
type OrderNote struct {
	db.Notification
	Order *Order `json:"order"`
}

const (
	TopicOrderLoadFailure     Topic = "OrderLoadFailure"
	TopicOrderPlaced          Topic = "OrderPlaced"
	TopicYoloPlaced           Topic = "YoloPlaced"
	TopicMissingMatches       Topic = "MissingMatches"
	TopicWalletMissing        Topic = "WalletMissing"
	TopicMatchErrorCoin       Topic = "MatchErrorCoin"
	TopicMatchErrorContract   Topic = "MatchErrorContract"
	TopicMatchRecoveryError   Topic = "MatchRecoveryError"
	TopicOrderCoinError       Topic = "OrderCoinError"
	TopicOrderCoinFetchError  Topic = "OrderCoinFetchError"
	TopicPreimageSent         Topic = "PreimageSent"
	TopicCancelPreimageSent   Topic = "CancelPreimageSent"
	TopicMissedCancel         Topic = "MissedCancel"
	TopicOrderBooked          Topic = "OrderBooked"
	TopicNoMatch              Topic = "NoMatch"
	TopicOrderCanceled        Topic = "OrderCanceled"
	TopicCancel               Topic = "Cancel"
	TopicMatchesMade          Topic = "MatchesMade"
	TopicSwapSendError        Topic = "SwapSendError"
	TopicInitError            Topic = "InitError"
	TopicReportRedeemError    Topic = "ReportRedeemError"
	TopicSwapsInitiated       Topic = "SwapsInitiated"
	TopicRedemptionError      Topic = "RedemptionError"
	TopicMatchComplete        Topic = "MatchComplete"
	TopicRefundFailure        Topic = "RefundFailure"
	TopicMatchesRefunded      Topic = "MatchesRefunded"
	TopicMatchRevoked         Topic = "MatchRevoked"
	TopicOrderRevoked         Topic = "OrderRevoked"
	TopicOrderAutoRevoked     Topic = "OrderAutoRevoked"
	TopicMatchRecovered       Topic = "MatchRecovered"
	TopicCancellingOrder      Topic = "CancellingOrder"
	TopicOrderStatusUpdate    Topic = "OrderStatusUpdate"
	TopicMatchResolutionError Topic = "MatchResolutionError"
	TopicFailedCancel         Topic = "FailedCancel"
	TopicOrderLoaded          Topic = "OrderLoaded"
	TopicOrderRetired         Topic = "OrderRetired"
)

func newOrderNote(topic Topic, subject, details string, severity db.Severity, corder *Order) *OrderNote {
	return &OrderNote{
		Notification: db.NewNotification(NoteTypeOrder, topic, subject, details, severity),
		Order:        corder,
	}
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
	TopicAudit           Topic = "Audit"
	TopicAuditTrouble    Topic = "AuditTrouble"
	TopicNewMatch        Topic = "NewMatch"
	TopicCounterConfirms Topic = "CounterConfirms"
	TopicConfirms        Topic = "Confirms"
)

func newMatchNote(topic Topic, subject, details string, severity db.Severity, t *trackedTrade, match *matchTracker) *MatchNote {
	var counterConfs int64
	if match.counterConfirms > 0 {
		// This can be -1 before it is actually checked, but for purposes of the
		// match note, it should be non-negative.
		counterConfs = match.counterConfirms
	}
	return &MatchNote{
		Notification: db.NewNotification(NoteTypeMatch, topic, subject, details, severity),
		OrderID:      t.ID().Bytes(),
		Match: matchFromMetaMatchWithConfs(t.Order, &match.MetaMatch, match.swapConfirms,
			int64(t.wallets.fromAsset.SwapConf), counterConfs, int64(t.wallets.toAsset.SwapConf)),
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
	TopicDEXConnected    Topic = "DEXConnected"
	TopicDEXDisconnected Topic = "DEXDisconnected"
)

func newConnEventNote(topic Topic, subject, host string, status comms.ConnectionStatus, details string, severity db.Severity) *ConnEventNote {
	return &ConnEventNote{
		Notification:     db.NewNotification(NoteTypeConnEvent, topic, subject, details, severity),
		Host:             host,
		ConnectionStatus: status,
	}
}

// fiatRatesNote is an update of fiat rate data for assets.
type fiatRatesNote struct {
	db.Notification
	FiatRates map[uint32]float64 `json:"fiatRates"`
}

const TopicFiatRatesUpdate Topic = "fiatrateupdate"

func newFiatRatesUpdate(rates map[uint32]float64) *fiatRatesNote {
	return &fiatRatesNote{
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
		Notification: db.NewNotification(NoteTypeBalance, TopicBalanceUpdated, "balance updated", "", db.Data),
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
	TopicDexAuthError     Topic = "DexAuthError"
	TopicUnknownOrders    Topic = "UnknownOrders"
	TopicOrdersReconciled Topic = "OrdersReconciled"
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
	TopicWalletConfigurationUpdated Topic = "WalletConfigurationUpdated"
	TopicWalletPasswordUpdated      Topic = "WalletPasswordUpdated"
)

func newWalletConfigNote(topic Topic, subject, details string, severity db.Severity, walletState *WalletState) *WalletConfigNote {
	return &WalletConfigNote{
		Notification: db.NewNotification(NoteTypeWalletConfig, topic, subject, details, severity),
		Wallet:       walletState,
	}
}

// WalletStateNote is a notification regarding a change in wallet state,
// including: creation, locking, unlocking, and connect. This is intended to be
// a Data Severity notification.
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
	TopicMarketSuspendScheduled   Topic = "MarketSuspendScheduled"
	TopicMarketSuspended          Topic = "MarketSuspended"
	TopicMarketSuspendedWithPurge Topic = "MarketSuspendedWithPurge"
	TopicMarketResumeScheduled    Topic = "MarketResumeScheduled"
	TopicMarketResumed            Topic = "MarketResumed"
	TopicPenalized                Topic = "Penalized"
	TopicDEXNotification          Topic = "DEXNotification"
)

func newServerNotifyNote(topic Topic, subject, details string, severity db.Severity) *ServerNotifyNote {
	return &ServerNotifyNote{
		Notification: db.NewNotification(NoteTypeServerNotify, topic, subject, details, severity),
	}
}

// UpgradeNote is a notification containing a .
type UpgradeNote struct {
	db.Notification
}

const (
	TopicUpgradeNeeded Topic = "UpgradeNeeded"
)

func newUpgradeNote(topic Topic, subject, details string, severity db.Severity) *UpgradeNote {
	return &UpgradeNote{
		Notification: db.NewNotification(NoteTypeUpgrade, topic, subject, details, severity),
	}
}
