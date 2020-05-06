// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package core

import (
	"fmt"

	"decred.org/dcrdex/client/db"
	"decred.org/dcrdex/dex"
)

// notify sends a notification to all subscribers. If the notification is of
// sufficient severity, it is stored in the database.
func (c *Core) notify(n Notification) {
	if n.Severity() >= db.Success {
		c.db.SaveNotification(n.DBNote())
	}

	logFun := log.Warnf // default in case the Severity level is unknown to notify
	switch n.Severity() {
	case db.Data:
		logFun = log.Tracef
	case db.Poke:
		logFun = log.Debugf
	case db.Success:
		logFun = log.Infof
	case db.WarningLevel:
		logFun = log.Warnf
	case db.ErrorLevel:
		logFun = log.Errorf
	}
	logFun("notify: %v", n)

	c.noteMtx.RLock()
	for _, ch := range c.noteChans {
		select {
		case ch <- n:
		default:
			log.Errorf("blocking notification channel")
		}
	}
	c.noteMtx.RUnlock()
}

// NotificationFeed returns a new receiving channel for notifications. The
// channel has capacity 16, and should be monitored for the lifetime of the
// Core. Blocking channels are silently ignored.
func (c *Core) NotificationFeed() <-chan Notification {
	ch := make(chan Notification, 16)
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
			log.Errorf("error saving notification acknowledgement for %s: %v", id, err)
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
	ConfirmationsRequired uint32 `json:"confirmationsrequired,omitempty"`
	ConfirmationsReceived uint32 `json:"confirmationsreceived,omitempty"`
}

func newFeePaymentNote(subject, details string, severity db.Severity) *FeePaymentNote {
	return &FeePaymentNote{
		Notification: db.NewNotification("fee payment", subject, details, severity),
	}
}

func newFeePaymentNoteWithConfirmations(subject, details string, severity db.Severity, reqConfs uint32, currConfs uint32) *FeePaymentNote {
	feePmtNt := newFeePaymentNote(subject, details, severity)
	feePmtNt.ConfirmationsRequired = reqConfs
	feePmtNt.ConfirmationsReceived = currConfs
	return feePmtNt
}

// WithdrawNote is a notification regarding a requested withdraw.
type WithdrawNote struct {
	db.Notification
}

func newWithdrawNote(subject, details string, severity db.Severity) *WithdrawNote {
	return &WithdrawNote{
		Notification: db.NewNotification("withdraw", subject, details, severity),
	}
}

// OrderNote is a notification about an order or a match.
type OrderNote struct {
	db.Notification
	Order *Order `json:"order"`
}

func newOrderNote(subject, details string, severity db.Severity, corder *Order) *OrderNote {
	return &OrderNote{
		Notification: db.NewNotification("order", subject, details, severity),
		Order:        corder,
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
	Epoch uint64 `json:"epoch"`
}

func newEpochNotification(epochIdx uint64) *EpochNotification {
	return &EpochNotification{
		Notification: db.NewNotification("epoch", "", "", db.Data),
		Epoch:        epochIdx,
	}
}

// String supplements db.Notification's Stringer with the Epoch index.
func (on *EpochNotification) String() string {
	return fmt.Sprintf("%s - Index: %d", on.Notification.String(), on.Epoch)
}
