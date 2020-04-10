// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package core

import (
	"decred.org/dcrdex/client/db"
	"decred.org/dcrdex/dex"
)

// notify sends a notification to all subscribers. If the notification is of
// sufficient severity, it is stored in the database.
func (c *Core) notify(n Notification) {
	if n.Severity() >= db.Success {
		c.db.SaveNotification(n.DBNote())
	}
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
}

// FeePaymentNote is a notification regarding registration fee payment.
type FeePaymentNote struct {
	db.Notification
}

func newFeePaymentNote(subject, details string, severity db.Severity) *FeePaymentNote {
	return &FeePaymentNote{
		Notification: db.NewNotification("fee payment", subject, details, severity),
	}
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
