//go:build pgonline

package pg

import (
	"bytes"
	"testing"

	"decred.org/dcrdex/dex/order"
	"decred.org/dcrdex/server/db"
)

type testEventTxData interface {
	EventTxData() ([]byte, error)
}

func testEventApplyTip(t *testing.T, prevTip []byte, seq uint64, kind string, event []byte, txData testEventTxData) []byte {
	t.Helper()

	tx, err := txData.EventTxData()
	if err != nil {
		t.Fatalf("EventTxData error: %v", err)
	}
	return testEventApplyTipForTxData(t, prevTip, seq, kind, event, tx)
}

func testEventApplyTipForTxData(t *testing.T, prevTip []byte, seq uint64, kind string, event, tx []byte) []byte {
	t.Helper()

	req, err := newEventLogAppend(&db.EventLogMeta{Seq: seq, Event: event}, kind, tx)
	if err != nil {
		t.Fatalf("newEventLogAppend error: %v", err)
	}
	return eventLogHash(prevTip, seq, req.entry.Kind, req.entry.Event, req.entry.TxData)
}

func requireEventApplyLog(t *testing.T, log *db.EventLogEntry, seq uint64, kind string, event, tip []byte, txData testEventTxData) {
	t.Helper()

	wantTxData, err := txData.EventTxData()
	if err != nil {
		t.Fatalf("EventTxData error: %v", err)
	}
	requireEventApplyLogTxData(t, log, seq, kind, event, tip, wantTxData)
}

func requireEventApplyLogTxData(t *testing.T, log *db.EventLogEntry, seq uint64, kind string, event, tip, wantTxData []byte) {
	t.Helper()

	if log == nil {
		t.Fatalf("nil event log")
	}
	if log.Seq != seq || log.Kind != kind || !bytes.Equal(log.Event, event) {
		t.Fatalf("log = %+v, want seq %d kind %q event %q", log, seq, kind, event)
	}
	if !bytes.Equal(log.TipHash, tip) {
		t.Fatalf("log tip = %x, want %x", log.TipHash, tip)
	}
	if !bytes.Equal(log.TxData, wantTxData) {
		t.Fatalf("log tx data = %x, want %x", log.TxData, wantTxData)
	}
}

func wrongEventTip() []byte {
	return bytes.Repeat([]byte{0x01}, db.EventLogTipHashSize)
}

func testMarketMatchID(match *order.Match) db.MarketMatchID {
	return db.MarketMatchID{
		MatchID: match.ID(),
		Base:    match.Maker.Base(),
		Quote:   match.Maker.Quote(),
	}
}
