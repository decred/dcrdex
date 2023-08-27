// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package mm

import (
	"fmt"
	"sync/atomic"

	"decred.org/dcrdex/client/core"
	"decred.org/dcrdex/client/db"
	"decred.org/dcrdex/dex"
)

const (
	validationNote = "validation"
	botStartStop   = "botstartstop"
	mmStartStop    = "mmstartstop"
)

// NoteFeed contains a receiving channel for notifications.
type NoteFeed struct {
	C      <-chan core.Notification
	closer func()
}

// ReturnFeed should be called when the channel is no longer needed.
func (f *NoteFeed) ReturnFeed() {
	if f.closer != nil {
		f.closer()
	}
}

// NotificationFeed returns a new receiving channel for notifications. The
// channel has capacity 1024, and should be monitored for the lifetime of the
// Core. Blocking channels are silently ignored.
func (m *MarketMaker) NotificationFeed() *NoteFeed {
	id, ch := m.notificationFeed()
	return &NoteFeed{
		C:      ch,
		closer: func() { m.returnFeed(id) },
	}
}

func (m *MarketMaker) returnFeed(channelID uint64) {
	m.noteMtx.Lock()
	delete(m.noteChans, channelID)
	m.noteMtx.Unlock()
}

func (m *MarketMaker) logNote(n core.Notification) {
	if n.Subject() == "" && n.Details() == "" {
		return
	}

	logFun := m.log.Warnf // default in case the Severity level is unknown to notify
	switch n.Severity() {
	case db.Data:
		logFun = m.log.Tracef
	case db.Poke:
		logFun = m.log.Debugf
	case db.Success:
		logFun = m.log.Infof
	case db.WarningLevel:
		logFun = m.log.Warnf
	case db.ErrorLevel:
		logFun = m.log.Errorf
	}

	logFun("notify: %v", n)
}

// notify sends a notification to all subscribers. If the notification is of
// sufficient severity, it is stored in the database.
func (m *MarketMaker) notify(n core.Notification) {
	m.logNote(n)

	m.noteMtx.RLock()
	for _, ch := range m.noteChans {
		select {
		case ch <- n:
		default:
			m.log.Errorf("blocking notification channel")
		}
	}
	m.noteMtx.RUnlock()
}

var noteChanCounter uint64

func (m *MarketMaker) notificationFeed() (uint64, <-chan core.Notification) {
	ch := make(chan core.Notification, 1024)
	cid := atomic.AddUint64(&noteChanCounter, 1)
	m.noteMtx.Lock()
	m.noteChans[cid] = ch
	m.noteMtx.Unlock()
	return cid, ch
}

type botValidationErrorNote struct {
	db.Notification
}

func newValidationErrorNote(host string, baseID, quoteID uint32, errorMsg string) *botValidationErrorNote {
	baseSymbol := dex.BipIDSymbol(baseID)
	quoteSymbol := dex.BipIDSymbol(quoteID)
	msg := fmt.Sprintf("%s-%s @ %s: %s", host, baseSymbol, quoteSymbol, errorMsg)
	return &botValidationErrorNote{
		Notification: db.NewNotification(validationNote, "", "Bot Config Validation Error", msg, db.ErrorLevel),
	}
}

type botStartStopNote struct {
	db.Notification

	Host    string `json:"host"`
	Base    uint32 `json:"base"`
	Quote   uint32 `json:"quote"`
	Running bool   `json:"running"`
}

func newBotStartStopNote(host string, base, quote uint32, running bool) *botStartStopNote {
	return &botStartStopNote{
		Notification: db.NewNotification(botStartStop, "", "", "", db.Data),
		Host:         host,
		Base:         base,
		Quote:        quote,
		Running:      running,
	}
}

type mmStartStopNote struct {
	db.Notification

	Running bool `json:"running"`
}

func newMMStartStopNote(running bool) *mmStartStopNote {
	return &mmStartStopNote{
		Notification: db.NewNotification(mmStartStop, "", "", "", db.Data),
		Running:      running,
	}
}
