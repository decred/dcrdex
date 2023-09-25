// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package mm

import (
	"fmt"

	"decred.org/dcrdex/client/db"
	"decred.org/dcrdex/dex"
)

const (
	validationNote = "validation"
	botStartStop   = "botstartstop"
	mmStartStop    = "mmstartstop"
)

func newValidationErrorNote(host string, baseID, quoteID uint32, errorMsg string) *db.Notification {
	baseSymbol := dex.BipIDSymbol(baseID)
	quoteSymbol := dex.BipIDSymbol(quoteID)
	msg := fmt.Sprintf("%s-%s @ %s: %s", baseSymbol, quoteSymbol, host, errorMsg)
	note := db.NewNotification(validationNote, "", "Bot Config Validation Error", msg, db.ErrorLevel)
	return &note
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
