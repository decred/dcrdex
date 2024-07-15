// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package mm

import (
	"fmt"

	"decred.org/dcrdex/client/db"
	"decred.org/dcrdex/dex"
)

const (
	NoteTypeValidation       = "validation"
	NoteTypeRunStats         = "runstats"
	NoteTypeRunEvent         = "runevent"
	NoteTypeCEXBalanceUpdate = "cexbal"
)

func newValidationErrorNote(host string, baseID, quoteID uint32, errorMsg string) *db.Notification {
	baseSymbol := dex.BipIDSymbol(baseID)
	quoteSymbol := dex.BipIDSymbol(quoteID)
	msg := fmt.Sprintf("%s-%s @ %s: %s", baseSymbol, quoteSymbol, host, errorMsg)
	note := db.NewNotification(NoteTypeValidation, "", "Bot Config Validation Error", msg, db.ErrorLevel)
	return &note
}

type runStatsNote struct {
	db.Notification

	Host      string    `json:"host"`
	BaseID    uint32    `json:"baseID"`
	QuoteID   uint32    `json:"quoteID"`
	StartTime int64     `json:"startTime"`
	Stats     *RunStats `json:"stats"`
}

func newRunStatsNote(host string, baseID, quoteID uint32, stats *RunStats) *runStatsNote {
	return &runStatsNote{
		Notification: db.NewNotification(NoteTypeRunStats, "", "", "", db.Data),
		Host:         host,
		BaseID:       baseID,
		QuoteID:      quoteID,
		Stats:        stats,
	}
}

type runEventNote struct {
	db.Notification

	Host      string             `json:"host"`
	BaseID    uint32             `json:"baseID"`
	QuoteID   uint32             `json:"quoteID"`
	StartTime int64              `json:"startTime"`
	Event     *MarketMakingEvent `json:"event"`
}

func newRunEventNote(host string, baseID, quoteID uint32, startTime int64, event *MarketMakingEvent) *runEventNote {
	return &runEventNote{
		Notification: db.NewNotification(NoteTypeRunEvent, "", "", "", db.Data),
		Host:         host,
		BaseID:       baseID,
		QuoteID:      quoteID,
		StartTime:    startTime,
		Event:        event,
	}
}

type cexNotification struct {
	db.Notification
	CEXName string      `json:"cexName"`
	Note    interface{} `json:"note"`
}

func newCexUpdateNote(cexName string, noteType string, note interface{}) *cexNotification {
	return &cexNotification{
		Notification: db.NewNotification(noteType, "", "", "", db.Data),
		CEXName:      cexName,
		Note:         note,
	}
}
