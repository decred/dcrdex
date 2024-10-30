// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package mm

import (
	"decred.org/dcrdex/client/db"
)

const (
	NoteTypeRunStats        = "runstats"
	NoteTypeRunEvent        = "runevent"
	NoteTypeCEXNotification = "cexnote"
	NoteTypeEpochReport     = "epochreport"
	NoteTypeCEXProblems     = "cexproblems"
)

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

const (
	TopicBalanceUpdate = "BalanceUpdate"
)

func newCexUpdateNote(cexName string, topic db.Topic, note interface{}) *cexNotification {
	return &cexNotification{
		Notification: db.NewNotification(NoteTypeCEXNotification, topic, "", "", db.Data),
		CEXName:      cexName,
		Note:         note,
	}
}

type botProblemsNotification struct {
	db.Notification
	Host    string       `json:"host"`
	BaseID  uint32       `json:"baseID"`
	QuoteID uint32       `json:"quoteID"`
	Report  *EpochReport `json:"report"`
}

func newEpochReportNote(host string, baseID, quoteID uint32, report *EpochReport) *botProblemsNotification {
	return &botProblemsNotification{
		Notification: db.NewNotification(NoteTypeEpochReport, "", "", "", db.Data),
		Host:         host,
		BaseID:       baseID,
		QuoteID:      quoteID,
		Report:       report,
	}
}

type cexProblemsNotification struct {
	db.Notification
	Host     string       `json:"host"`
	BaseID   uint32       `json:"baseID"`
	QuoteID  uint32       `json:"quoteID"`
	Problems *CEXProblems `json:"problems"`
}

func newCexProblemsNote(host string, baseID, quoteID uint32, problems *CEXProblems) *cexProblemsNotification {
	return &cexProblemsNotification{
		Notification: db.NewNotification(NoteTypeCEXProblems, "", "", "", db.Data),
		Host:         host,
		BaseID:       baseID,
		QuoteID:      quoteID,
		Problems:     problems,
	}
}
