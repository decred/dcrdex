// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package core

import (
	"fmt"
	"sync/atomic"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/client/comms"
	"decred.org/dcrdex/client/db"
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/msgjson"
	"decred.org/dcrdex/dex/order"
	"decred.org/dcrdex/server/account"
)

// Notifications should use the following note type strings.
const (
	NoteTypeFeePayment     = "feepayment"
	NoteTypeBondPost       = "bondpost"
	NoteTypeBondRefund     = "bondrefund"
	NoteTypeUnknownBond    = "unknownbond"
	NoteTypeSend           = "send"
	NoteTypeOrder          = "order"
	NoteTypeMatch          = "match"
	NoteTypeEpoch          = "epoch"
	NoteTypeConnEvent      = "conn"
	NoteTypeBalance        = "balance"
	NoteTypeSpots          = "spots"
	NoteTypeWalletConfig   = "walletconfig"
	NoteTypeWalletState    = "walletstate"
	NoteTypeWalletSync     = "walletsync"
	NoteTypeServerNotify   = "notify"
	NoteTypeSecurity       = "security"
	NoteTypeUpgrade        = "upgrade"
	NoteTypeBot            = "bot"
	NoteTypeDEXAuth        = "dex_auth"
	NoteTypeFiatRates      = "fiatrateupdate"
	NoteTypeCreateWallet   = "createwallet"
	NoteTypeLogin          = "login"
	NoteTypeWalletNote     = "walletnote"
	NoteTypeReputation     = "reputation"
	NoteTypeActionRequired = "actionrequired"
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

func (c *Core) Broadcast(n Notification) {
	c.notify(n)
}

// notify sends a notification to all subscribers. If the notification is of
// sufficient severity, it is stored in the database.
func (c *Core) notify(n Notification) {
	if n.Severity() >= db.Success {
		c.db.SaveNotification(n.DBNote())
	} else if n.Severity() == db.Poke {
		c.pokesCache.add(n.DBNote())
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

// NoteFeed contains a receiving channel for notifications.
type NoteFeed struct {
	C      <-chan Notification
	closer func()
}

// ReturnFeed should be called when the channel is no longer needed.
func (c *NoteFeed) ReturnFeed() {
	if c.closer != nil {
		c.closer()
	}
}

// NotificationFeed returns a new receiving channel for notifications. The
// channel has capacity 1024, and should be monitored for the lifetime of the
// Core. Blocking channels are silently ignored.
func (c *Core) NotificationFeed() *NoteFeed {
	id, ch := c.notificationFeed()
	return &NoteFeed{
		C:      ch,
		closer: func() { c.returnFeed(id) },
	}
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

func (c *Core) formatDetails(topic Topic, args ...any) (translatedSubject, details string) {
	locale := c.locale()
	trans, found := locale.m[topic]
	if !found {
		c.log.Errorf("No translation found for topic %q", topic)
		originTrans, found := originLocale[topic]
		if !found {
			return string(topic), "translation error"
		}
		return originTrans.subject.T, fmt.Sprintf(originTrans.template.T, args...)
	}
	return trans.subject.T, locale.printer.Sprintf(string(topic), args...)
}

func makeCoinIDToken(txHash string, assetID uint32) string {
	return fmt.Sprintf("{{{%d|%s}}}", assetID, txHash)
}

func makeOrderToken(orderToken string) string {
	return fmt.Sprintf("{{{order|%s}}}", orderToken)
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

const (
	TopicSeedNeedsSaving Topic = "SeedNeedsSaving"
	TopicUpgradedToSeed  Topic = "UpgradedToSeed"
)

func newSecurityNote(topic Topic, subject, details string, severity db.Severity) *SecurityNote {
	return &SecurityNote{
		Notification: db.NewNotification(NoteTypeSecurity, topic, subject, details, severity),
	}
}

const (
	TopicFeePaymentInProgress    Topic = "FeePaymentInProgress"
	TopicFeePaymentError         Topic = "FeePaymentError"
	TopicFeeCoinError            Topic = "FeeCoinError"
	TopicRegUpdate               Topic = "RegUpdate"
	TopicBondConfirming          Topic = "BondConfirming"
	TopicBondRefunded            Topic = "BondRefunded"
	TopicBondPostError           Topic = "BondPostError"
	TopicBondPostErrorConfirm    Topic = "BondPostErrorConfirm"
	TopicBondCoinError           Topic = "BondCoinError"
	TopicAccountRegistered       Topic = "AccountRegistered"
	TopicAccountUnlockError      Topic = "AccountUnlockError"
	TopicWalletConnectionWarning Topic = "WalletConnectionWarning"
	TopicWalletUnlockError       Topic = "WalletUnlockError"
	TopicWalletCommsWarning      Topic = "WalletCommsWarning"
	TopicWalletPeersRestored     Topic = "WalletPeersRestored"
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

const (
	TopicBondAuthUpdate Topic = "BondAuthUpdate"
)

// BondPostNote is a notification regarding bond posting.
type BondPostNote struct {
	db.Notification
	Asset         *uint32       `json:"asset,omitempty"`
	Confirmations *int32        `json:"confirmations,omitempty"`
	BondedTier    *int64        `json:"bondedTier,omitempty"`
	CoinID        *string       `json:"coinID,omitempty"`
	Dex           string        `json:"dex,omitempty"`
	Auth          *ExchangeAuth `json:"auth,omitempty"`
}

func newBondPostNote(topic Topic, subject, details string, severity db.Severity, dexAddr string) *BondPostNote {
	host, _ := addrHost(dexAddr)
	return &BondPostNote{
		Notification: db.NewNotification(NoteTypeBondPost, topic, subject, details, severity),
		Dex:          host,
	}
}

func newBondPostNoteWithConfirmations(
	topic Topic,
	subject string,
	details string,
	severity db.Severity,
	asset uint32,
	coinID string,
	currConfs int32,
	host string,
	auth *ExchangeAuth,
) *BondPostNote {

	bondPmtNt := newBondPostNote(topic, subject, details, severity, host)
	bondPmtNt.Asset = &asset
	bondPmtNt.CoinID = &coinID
	bondPmtNt.Confirmations = &currConfs
	bondPmtNt.Auth = auth
	return bondPmtNt
}

func newBondPostNoteWithTier(topic Topic, subject, details string, severity db.Severity, dexAddr string, bondedTier int64, auth *ExchangeAuth) *BondPostNote {
	bondPmtNt := newBondPostNote(topic, subject, details, severity, dexAddr)
	bondPmtNt.BondedTier = &bondedTier
	bondPmtNt.Auth = auth
	return bondPmtNt
}

func newBondAuthUpdate(host string, auth *ExchangeAuth) *BondPostNote {
	n := newBondPostNote(TopicBondAuthUpdate, "", "", db.Data, host)
	n.Auth = auth
	return n
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
	Order       *Order `json:"order"`
	TemporaryID uint64 `json:"tempID,omitempty"`
}

const (
	TopicOrderLoadFailure     Topic = "OrderLoadFailure"
	TopicOrderResumeFailure   Topic = "OrderResumeFailure"
	TopicBuyOrderPlaced       Topic = "BuyOrderPlaced"
	TopicSellOrderPlaced      Topic = "SellOrderPlaced"
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
	TopicBuyOrderCanceled     Topic = "BuyOrderCanceled"
	TopicSellOrderCanceled    Topic = "SellOrderCanceled"
	TopicCancel               Topic = "Cancel"
	TopicBuyMatchesMade       Topic = "BuyMatchesMade"
	TopicSellMatchesMade      Topic = "SellMatchesMade"
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
	TopicAsyncOrderFailure    Topic = "AsyncOrderFailure"
	TopicAsyncOrderSubmitted  Topic = "AsyncOrderSubmitted"
	TopicOrderQuantityTooHigh Topic = "OrderQuantityTooHigh"
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
	TopicAudit                 Topic = "Audit"
	TopicAuditTrouble          Topic = "AuditTrouble"
	TopicNewMatch              Topic = "NewMatch"
	TopicCounterConfirms       Topic = "CounterConfirms"
	TopicConfirms              Topic = "Confirms"
	TopicRedemptionResubmitted Topic = "RedemptionResubmitted"
	TopicRefundResubmitted     Topic = "RefundResubmitted"
	TopicSwapRefunded          Topic = "SwapRefunded"
	TopicSwapRedeemed          Topic = "SwapRedeemed"
	TopicRedemptionConfirmed   Topic = "RedemptionConfirmed"
	TopicRefundConfirmed       Topic = "RefundConfirmed"
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
			int64(match.redemptionConfs), int64(match.redemptionConfsReq),
			int64(match.refundConfs), int64(match.refundConfsReq)),
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
	TopicDexConnectivity Topic = "DEXConnectivity"
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
	TopicDexAuthError     Topic = "DexAuthError"
	TopicDexAuthErrorBond Topic = "DexAuthErrorBond"
	TopicUnknownOrders    Topic = "UnknownOrders"
	TopicOrdersReconciled Topic = "OrdersReconciled"
	TopicBondConfirmed    Topic = "BondConfirmed"
	TopicBondExpired      Topic = "BondExpired"
	TopicAccountRegTier   Topic = "AccountRegTier"
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
	TopicWalletPeersWarning         Topic = "WalletPeersWarning"
	TopicWalletTypeDeprecated       Topic = "WalletTypeDeprecated"
	TopicWalletPeersUpdate          Topic = "WalletPeersUpdate"
	TopicBondWalletNotConnected     Topic = "BondWalletNotConnected"
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
const TopicTokenApproval Topic = "TokenApproval"

func newTokenApprovalNote(walletState *WalletState) *WalletStateNote {
	return &WalletStateNote{
		Notification: db.NewNotification(NoteTypeWalletState, TopicTokenApproval, "", "", db.Data),
		Wallet:       walletState,
	}
}

func newWalletStateNote(walletState *WalletState) *WalletStateNote {
	return &WalletStateNote{
		Notification: db.NewNotification(NoteTypeWalletState, TopicWalletState, "", "", db.Data),
		Wallet:       walletState,
	}
}

// WalletSyncNote is a notification of the wallet sync status.
type WalletSyncNote struct {
	db.Notification
	AssetID      uint32            `json:"assetID"`
	SyncStatus   *asset.SyncStatus `json:"syncStatus"`
	SyncProgress float32           `json:"syncProgress"`
}

const TopicWalletSync = "WalletSync"

func newWalletSyncNote(assetID uint32, ss *asset.SyncStatus) *WalletSyncNote {
	return &WalletSyncNote{
		Notification: db.NewNotification(NoteTypeWalletSync, TopicWalletState, "", "", db.Data),
		AssetID:      assetID,
		SyncStatus:   ss,
		SyncProgress: ss.BlockProgress(),
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

// UpgradeNote is a notification regarding an outdated client.
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

// ServerConfigUpdateNote is sent when a server's configuration is updated.
type ServerConfigUpdateNote struct {
	db.Notification
	Host string `json:"host"`
}

const TopicServerConfigUpdate Topic = "ServerConfigUpdate"

func newServerConfigUpdateNote(host string) *ServerConfigUpdateNote {
	return &ServerConfigUpdateNote{
		Notification: db.NewNotification(NoteTypeServerNotify, TopicServerConfigUpdate, "", "", db.Data),
		Host:         host,
	}
}

// WalletCreationNote is a notification regarding asynchronous wallet creation.
type WalletCreationNote struct {
	db.Notification
	AssetID uint32 `json:"assetID"`
}

const (
	TopicQueuedCreationFailed  Topic = "QueuedCreationFailed"
	TopicQueuedCreationSuccess Topic = "QueuedCreationSuccess"
	TopicCreationQueued        Topic = "CreationQueued"
)

func newWalletCreationNote(topic Topic, subject, details string, severity db.Severity, assetID uint32) *WalletCreationNote {
	return &WalletCreationNote{
		Notification: db.NewNotification(NoteTypeCreateWallet, topic, subject, details, severity),
		AssetID:      assetID,
	}
}

// LoginNote is a notification with the recent login status.
type LoginNote struct {
	db.Notification
}

const TopicLoginStatus Topic = "LoginStatus"

func newLoginNote(message string) *LoginNote {
	return &LoginNote{
		Notification: db.NewNotification(NoteTypeLogin, TopicLoginStatus, "", message, db.Data),
	}
}

// WalletNote is a notification originating from a wallet.
type WalletNote struct {
	db.Notification
	Payload asset.WalletNotification `json:"payload"`
}

const TopicWalletNotification Topic = "WalletNotification"

func newWalletNote(n asset.WalletNotification) *WalletNote {
	return &WalletNote{
		Notification: db.NewNotification(NoteTypeWalletNote, TopicWalletNotification, "", "", db.Data),
		Payload:      n,
	}
}

type ReputationNote struct {
	db.Notification
	Host       string             `json:"host"`
	Reputation account.Reputation `json:"rep"`
}

const TopicReputationUpdate = "ReputationUpdate"

func newReputationNote(host string, rep account.Reputation) *ReputationNote {
	return &ReputationNote{
		Notification: db.NewNotification(NoteTypeReputation, TopicReputationUpdate, "", "", db.Data),
		Host:         host,
		Reputation:   rep,
	}
}

const TopicUnknownBondTierZero = "UnknownBondTierZero"

// newUnknownBondTierZeroNote is used when unknown bonds are reported by the
// server while at target tier zero.
func newUnknownBondTierZeroNote(subject, details string) *db.Notification {
	note := db.NewNotification(NoteTypeUnknownBond, TopicUnknownBondTierZero, subject, details, db.WarningLevel)
	return &note
}

const (
	ActionIDRedeemRejected = "redeemRejected"
	TopicRedeemRejected    = "RedeemRejected"
	ActionIDRefundRejected = "refundRejected"
	TopicRefundRejected    = "RefundRejected"
)

func newActionRequiredNote(actionID, uniqueID string, payload any) *asset.ActionRequiredNote {
	n := &asset.ActionRequiredNote{
		UniqueID: uniqueID,
		ActionID: actionID,
		Payload:  payload,
	}
	const routeNotNeededCuzCoreHasNoteType = ""
	n.Route = routeNotNeededCuzCoreHasNoteType
	return n
}

type RejectedTxData struct {
	OrderID dex.Bytes `json:"orderID"`
	CoinID  dex.Bytes `json:"coinID"`
	AssetID uint32    `json:"assetID"`
	CoinFmt string    `json:"coinFmt"`
	TxType  string    `json:"txType"`
}

// ActionRequiredNote is structured like a WalletNote. The payload will be
// an *asset.ActionRequiredNote. This is done for compatibility reasons.
type ActionRequiredNote WalletNote

func newRejectedTxNote(assetID uint32, oid order.OrderID, coinID []byte, txType asset.ConfirmTxType) (*asset.ActionRequiredNote, *ActionRequiredNote) {
	data := &RejectedTxData{
		AssetID: assetID,
		OrderID: oid[:],
		CoinID:  coinID,
		CoinFmt: coinIDString(assetID, coinID),
		TxType:  txType.String(),
	}
	uniqueID := dex.Bytes(coinID).String()
	actionID := ActionIDRedeemRejected
	topic := db.Topic(TopicRedeemRejected)
	if txType == asset.CTRefund {
		actionID = ActionIDRefundRejected
		topic = TopicRefundRejected
	}
	actionNote := newActionRequiredNote(actionID, uniqueID, data)
	coreNote := &ActionRequiredNote{
		Notification: db.NewNotification(NoteTypeActionRequired, topic, "", "", db.Data),
		Payload:      actionNote,
	}
	return actionNote, coreNote
}
