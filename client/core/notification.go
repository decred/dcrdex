// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package core

import (
	"bytes"
	"fmt"

	"decred.org/dcrdex/client/db"
	"decred.org/dcrdex/dex"
)

// Notifications should use the following note type strings.
const (
	NoteTypeFeePayment   = "feepayment"
	NoteTypeWithdraw     = "withdraw"
	NoteTypeOrder        = "order"
	NoteTypeMatch        = "match"
	NoteTypeEpoch        = "epoch"
	NoteTypeConnEvent    = "conn"
	NoteTypeBalance      = "balance"
	NoteTypeWalletConfig = "walletconfig"
	NoteTypeWalletState  = "walletstate"
	NoteTypeServerNotify = "notify"
)

// notify sends a notification to all subscribers. If the notification is of
// sufficient severity, it is stored in the database.
func (c *Core) notify(n Notification) {
	if n.Severity() >= db.Success {
		c.db.SaveNotification(n.DBNote())
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

	switch nt := n.(type) {
	case *MatchNote:
		c.updateCoreMatch(nt)
	}

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

// updateCoreMatch performs a targeted update of a match in the User struct.
func (c *Core) updateCoreMatch(n *MatchNote) {
	c.userMtx.Lock()
	defer c.userMtx.Unlock()
	xc := c.user.Exchanges[n.Host]
	mkt := xc.Markets[n.MarketID]
	for _, cord := range mkt.Orders {
		if !bytes.Equal(cord.ID, n.OrderID) {
			continue
		}
		for i, m := range cord.Matches {
			if bytes.Equal(m.MatchID, n.Match.MatchID) {
				cord.Matches[i] = n.Match
				return
			}
		}
		cord.Matches = append(cord.Matches, n.Match)
	}
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

// Notification is an interface for a user notification. Notification is
// satisfied by db.Notification, so concrete types can embed the db type.
type Notification interface {
	// Type is a string ID unique to the concrete type.
	Type() string
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

// FeePaymentNote is a notification regarding registration fee payment.
type FeePaymentNote struct {
	db.Notification
	Confirmations *uint32 `json:"confirmations,omitempty"`
	Dex           string  `json:"dex,omitempty"`
}

const (
	SubjectFeePaymentInProgress    = "Fee payment in progress"
	SubjectRegUpdate               = "regupdate"
	SubjectFeePaymentError         = "Fee payment error"
	SubjectAccountRegistered       = "Account registered"
	SubjectAccountUnlockError      = "Account unlock error"
	SubjectFeeCoinError            = "Fee coin error"
	SubjectWalletConnectionWarning = "Wallet connection warning"
	SubjectWalletUnlockError       = "Wallet unlock error"
)

func newFeePaymentNote(subject, details string, severity db.Severity, dexAddr string) *FeePaymentNote {
	host, _ := addrHost(dexAddr)
	return &FeePaymentNote{
		Notification: db.NewNotification(NoteTypeFeePayment, subject, details, severity),
		Dex:          host,
	}
}

func newFeePaymentNoteWithConfirmations(subject, details string, severity db.Severity, currConfs uint32, dexAddr string) *FeePaymentNote {
	feePmtNt := newFeePaymentNote(subject, details, severity, dexAddr)
	feePmtNt.Confirmations = &currConfs
	return feePmtNt
}

// WithdrawNote is a notification regarding a requested withdraw.
type WithdrawNote struct {
	db.Notification
}

const (
	WithdrawErrorSubject = "Withdraw error"
	WithdrawSendSubject  = "Withdraw sent"
)

func newWithdrawNote(subject, details string, severity db.Severity) *WithdrawNote {
	return &WithdrawNote{
		Notification: db.NewNotification(NoteTypeWithdraw, subject, details, severity),
	}
}

// OrderNote is a notification about an order or a match.
type OrderNote struct {
	db.Notification
	Order *Order `json:"order"`
}

const (
	SubjectOrderLoadFailure     = "Order load failure"
	SubjectOrderPlaced          = "Order placed"
	SubjectMissingMatches       = "Missing matches"
	SubjectWalletMissing        = "Wallet missing"
	SubjectMatchStatusError     = "Match status error"
	SubjectMatchRecoveryError   = "Match recovery error"
	SubjectNoFundingCoins       = "No funding coins"
	SubjectOrderCoinError       = "Order coin error"
	SubjectPreimageSent         = "preimage sent"
	SubjectCancelPreimageSent   = "cancel preimage sent"
	SubjectMissedCancel         = "Missed cancel"
	SubjectOrderBooked          = "Order booked"
	SubjectNoMatch              = "No match"
	SubjectOrderCanceled        = "Order canceled"
	SubjectCancel               = "cancel"
	SubjectMatchesMade          = "Matches made"
	SubjectSwapError            = "Swap error"
	SubjectSwapsInitiated       = "Swaps initiated"
	SubjectRedemptionError      = "Redemption error"
	SubjectMatchComplete        = "Match complete"
	SubjectRefundFailure        = "Refund Failure"
	SubjectMatchesRefunded      = "Matches Refunded"
	SubjectMatchRevoked         = "Match revoked"
	SubjectRevoke               = "revoke"
	SubjectMatchRecovered       = "Match recovered"
	SubjectCancellingOrder      = "Cancelling order"
	SubjectOrderStatusUpdate    = "Order status update"
	SubjectMatchResolutionError = "Match resolution error"
	SubjectFailedCancel         = "Failed cancel"
)

func newOrderNote(subject, details string, severity db.Severity, corder *Order) *OrderNote {
	return &OrderNote{
		Notification: db.NewNotification(NoteTypeOrder, subject, details, severity),
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
	SubjectAudit           = "audit"
	SubjectNewMatch        = "new_match"
	SubjectCounterConfirms = "counterconfirms"
	SubjectConfirms        = "confirms"
)

func newMatchNote(subject, details string, severity db.Severity, t *trackedTrade, match *matchTracker) *MatchNote {
	return &MatchNote{
		Notification: db.NewNotification(NoteTypeMatch, subject, details, severity),
		OrderID:      t.ID().Bytes(),
		Match: matchFromMetaMatchWithConfs(t.Order, &match.MetaMatch, match.swapConfirms,
			int64(t.wallets.fromAsset.SwapConf), match.counterConfirms, int64(t.wallets.toAsset.SwapConf)),
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

func newEpochNotification(host, mktID string, epochIdx uint64) *EpochNotification {
	return &EpochNotification{
		Host:         host,
		MarketID:     mktID,
		Notification: db.NewNotification(NoteTypeEpoch, "", "", db.Data),
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
	Host      string `json:"host"`
	Connected bool   `json:"connected"`
}

func newConnEventNote(subject, host string, connected bool, details string, severity db.Severity) *ConnEventNote {
	return &ConnEventNote{
		Notification: db.NewNotification(NoteTypeConnEvent, subject, details, severity),
		Host:         host,
		Connected:    connected,
	}
}

// BalanceNote is an update to a wallet's balance.
type BalanceNote struct {
	db.Notification
	AssetID uint32         `json:"assetID"`
	Balance *WalletBalance `json:"balance"`
}

func newBalanceNote(assetID uint32, bal *WalletBalance) *BalanceNote {
	return &BalanceNote{
		Notification: db.NewNotification(NoteTypeBalance, "balance updated", "", db.Data),
		AssetID:      assetID,
		Balance:      bal,
	}
}

// DEXAuthNote is a notification regarding individual DEX authentication status.
type DEXAuthNote struct {
	db.Notification
	Host          string `json:"host"`
	Authenticated bool   `json:"authenticated"`
}

const (
	SubjectDexAuthError     = "DEX auth error"
	SubjectUnknownOrders    = "DEX reported unknown orders"
	SubjectOrdersReconciled = "Orders reconciled with DEX"
)

func newDEXAuthNote(subject, host string, authenticated bool, details string, severity db.Severity) *DEXAuthNote {
	return &DEXAuthNote{
		Notification:  db.NewNotification("dex_auth", subject, details, severity),
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
	SubjectWalletConfigurationUpdated = "Wallet Configuration Updated"
	SubjectWalletPasswordUpdated      = "Wallet Password Updated"
)

func newWalletConfigNote(subject, details string, severity db.Severity, walletState *WalletState) *WalletConfigNote {
	return &WalletConfigNote{
		Notification: db.NewNotification(NoteTypeWalletConfig, subject, details, severity),
		Wallet:       walletState,
	}
}

// WalletStateNote is a notification regarding a change in wallet state,
// including: creation, locking, unlocking, and connect. This is intended to be
// a Data Severity notification.
type WalletStateNote WalletConfigNote

func newWalletStateNote(walletState *WalletState) *WalletStateNote {
	return &WalletStateNote{
		Notification: db.NewNotification(NoteTypeWalletState, "", "", db.Data),
		Wallet:       walletState,
	}
}

// ServerNotifyNote is a notification containing a server-originating message.
type ServerNotifyNote struct {
	db.Notification
}

const (
	SubjectMarketSuspendScheduled = "market suspend scheduled"
	SubjectMarketSuspended        = "market suspended"
	SubjectMarketResumeScheduled  = "market resume scheduled"
	SubjectMarketResumed          = "market resumed"
)

func newServerNotifyNote(subject, details string, severity db.Severity) *ServerNotifyNote {
	return &ServerNotifyNote{
		Notification: db.NewNotification(NoteTypeServerNotify, subject, details, severity),
	}
}
