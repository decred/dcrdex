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
	runStats       = "runstats"
	mmStartStop    = "mmstartstop"
	runEvent       = "runevent"
)

func newValidationErrorNote(host string, baseID, quoteID uint32, errorMsg string) *db.Notification {
	baseSymbol := dex.BipIDSymbol(baseID)
	quoteSymbol := dex.BipIDSymbol(quoteID)
	msg := fmt.Sprintf("%s-%s @ %s: %s", baseSymbol, quoteSymbol, host, errorMsg)
	note := db.NewNotification(validationNote, "", "Bot Config Validation Error", msg, db.ErrorLevel)
	return &note
}

type runStatsNote struct {
	db.Notification

	Host      string    `json:"host"`
	Base      uint32    `json:"base"`
	Quote     uint32    `json:"quote"`
	StartTime int64     `json:"startTime"`
	Stats     *RunStats `json:"stats"`
}

func newRunStatsNote(host string, base, quote uint32, stats *RunStats) *runStatsNote {
	return &runStatsNote{
		Notification: db.NewNotification(runStats, "", "", "", db.Data),
		Host:         host,
		Base:         base,
		Quote:        quote,
		Stats:        stats,
	}
}

type runEventNote struct {
	db.Notification

	Host      string             `json:"host"`
	Base      uint32             `json:"base"`
	Quote     uint32             `json:"quote"`
	StartTime int64              `json:"startTime"`
	Event     *MarketMakingEvent `json:"event"`
}

func newRunEventNote(host string, base, quote uint32, startTime int64, event *MarketMakingEvent) *runEventNote {
	return &runEventNote{
		Notification: db.NewNotification(runEvent, "", "", "", db.Data),
		Host:         host,
		Base:         base,
		Quote:        quote,
		StartTime:    startTime,
		Event:        event,
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
