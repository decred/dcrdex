// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package meshevents

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"decred.org/dcrdex/dex/order"
	"decred.org/dcrdex/server/matcher"
)

func TestEpochProcessedEventEncodeDecode(t *testing.T) {
	var pi order.Preimage
	pi[0] = 0xab
	revealedOrd := testLimitOrder(5, pi)
	missedOrd := testLimitOrder(6, order.Preimage{})
	matchTime := time.UnixMilli(123456789000).UTC()
	missRevokeTime := time.UnixMilli(123456789999).UTC()
	cSum := []byte{0x0c, 0x0d}

	event := NewEpochProcessedEvent("dcr_btc", 9876, 6000, matchTime, 11, 22, 33, cSum,
		[]*matcher.OrderRevealed{{Order: revealedOrd, Preimage: pi}}, []order.Order{missedOrd}, missRevokeTime)
	payload, err := event.Encode()
	if err != nil {
		t.Fatalf("Encode error: %v", err)
	}
	// The wire encoding is consensus-critical and must not change.
	wantJSON := fmt.Sprintf(`{"market":"dcr_btc","epochIdx":9876,"epochDur":6000,"matchTime":123456789000,`+
		`"feeRateBase":11,"feeRateQuote":22,"lastRate":33,"cSum":"0c0d","missRevokeTime":123456789999,`+
		`"revealed":[{"order":"%x","preimage":"%s"}],"missed":["%x"]}`,
		order.EncodeOrder(revealedOrd), hex.EncodeToString(pi[:]), order.EncodeOrder(missedOrd))
	if string(payload) != wantJSON {
		t.Fatalf("payload = %s, want %s", payload, wantJSON)
	}

	got, err := DecodeEpochProcessedEvent(payload)
	if err != nil {
		t.Fatalf("Decode error: %v", err)
	}
	if got.MatchTime != matchTime.UnixMilli() {
		t.Fatalf("match time = %v, want %v", got.MatchTime, matchTime.UnixMilli())
	}
	if got.MissRevokeTime != missRevokeTime.UnixMilli() {
		t.Fatalf("miss revoke time = %v, want %v", got.MissRevokeTime, missRevokeTime.UnixMilli())
	}
	if got.FeeRateBase != 11 || got.FeeRateQuote != 22 || got.LastRate != 33 || !bytes.Equal(got.CSum, cSum) {
		t.Fatalf("unexpected round trip: %+v", got)
	}
	revealed, err := got.OrdersRevealed()
	if err != nil {
		t.Fatalf("OrdersRevealed error: %v", err)
	}
	if len(revealed) != 1 || revealed[0].Order.ID() != revealedOrd.ID() || revealed[0].Preimage != pi {
		t.Fatalf("unexpected revealed round trip")
	}
	missed, err := got.MissedOrders()
	if err != nil {
		t.Fatalf("MissedOrders error: %v", err)
	}
	if len(missed) != 1 || missed[0].ID() != missedOrd.ID() {
		t.Fatalf("unexpected missed round trip")
	}
	if got.Kind() != EventKindEpochProcessed {
		t.Fatalf("kind = %q, want %q", got.Kind(), EventKindEpochProcessed)
	}

	// Commitment mismatch is caught by OrdersRevealed: this is the only event
	// carrying preimage reveals, so it is where verification lives.
	var mutated EpochProcessedEvent
	if err := json.Unmarshal(payload, &mutated); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	mutated.Revealed[0].Preimage[0] ^= 0xff
	if _, err := mutated.OrdersRevealed(); err == nil {
		t.Fatalf("OrdersRevealed accepted a preimage that does not match the commitment")
	}
}

func TestEpochProcessedEventValidate(t *testing.T) {
	base := func() *EpochProcessedEvent {
		return NewEpochProcessedEvent("dcr_btc", 9876, 6000, time.UnixMilli(1000), 1, 1, 1, []byte{0x01},
			nil, []order.Order{testLimitOrder(5, order.Preimage{})}, time.UnixMilli(2000))
	}
	if err := base().Validate(); err != nil {
		t.Fatalf("Validate error: %v", err)
	}
	tests := []struct {
		name   string
		mutate func(*EpochProcessedEvent)
	}{
		{"missing market", func(e *EpochProcessedEvent) { e.Market = "" }},
		{"zero duration", func(e *EpochProcessedEvent) { e.EpochDur = 0 }},
		{"negative duration", func(e *EpochProcessedEvent) { e.EpochDur = -1 }},
		{"misses without revoke time", func(e *EpochProcessedEvent) { e.MissRevokeTime = 0 }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			event := base()
			tt.mutate(event)
			if err := event.Validate(); err == nil {
				t.Fatalf("expected validation error")
			}
			payload, err := event.Encode()
			if err != nil {
				t.Fatalf("Encode error: %v", err)
			}
			if _, err := DecodeEpochProcessedEvent(payload); err == nil {
				t.Fatalf("expected decode validation error")
			}
		})
	}
}
