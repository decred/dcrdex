// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package meshevents

import (
	"testing"
	"time"

	"decred.org/dcrdex/dex/order"
)

func testCancelOrder(target order.OrderID) *order.CancelOrder {
	return &order.CancelOrder{
		P: order.Prefix{
			BaseAsset:  42,
			QuoteAsset: 0,
			OrderType:  order.CancelOrderType,
			ClientTime: time.UnixMilli(1000),
			ServerTime: time.UnixMilli(1001),
		},
		TargetOrderID: target,
	}
}

func TestSuspendedCancelEventEncodeDecode(t *testing.T) {
	target := testLimitOrder(5, order.Preimage{})
	cancel := testCancelOrder(target.ID())
	matchServerTime := time.UnixMilli(123456789000).UTC()

	event := NewSuspendedCancelEvent("dcr_btc", 42, 0, cancel, target, 9876, 6000, 11, 22, matchServerTime)
	payload, err := event.Encode()
	if err != nil {
		t.Fatalf("Encode error: %v", err)
	}
	got, err := DecodeSuspendedCancelEvent(payload)
	if err != nil {
		t.Fatalf("Decode error: %v", err)
	}
	// Wire round trip must be byte-identical.
	rePayload, err := got.Encode()
	if err != nil {
		t.Fatalf("re-Encode error: %v", err)
	}
	if string(payload) != string(rePayload) {
		t.Fatalf("round trip payload mismatch:\n%s\n%s", payload, rePayload)
	}

	gotCancel, err := got.CancelOrder()
	if err != nil {
		t.Fatalf("CancelOrder error: %v", err)
	}
	if gotCancel.ID() != cancel.ID() {
		t.Fatalf("cancel id = %v, want %v", gotCancel.ID(), cancel.ID())
	}
	gotTarget, err := got.TargetOrder()
	if err != nil {
		t.Fatalf("TargetOrder error: %v", err)
	}
	if gotTarget.ID() != target.ID() {
		t.Fatalf("target id = %v, want %v", gotTarget.ID(), target.ID())
	}
	if !got.MatchTime().Equal(matchServerTime) {
		t.Fatalf("match server time = %v, want %v", got.MatchTime(), matchServerTime)
	}
	if got.EpochIdx != 9876 || got.EpochDur != 6000 || got.FeeRateBase != 11 || got.FeeRateQuote != 22 {
		t.Fatalf("unexpected round trip: %+v", got)
	}
	if got.Kind() != EventKindSuspendedCancel {
		t.Fatalf("kind = %q, want %q", got.Kind(), EventKindSuspendedCancel)
	}
}

func TestSuspendedCancelEventValidate(t *testing.T) {
	target := testLimitOrder(5, order.Preimage{})
	cancel := testCancelOrder(target.ID())
	matchServerTime := time.UnixMilli(123456789000).UTC()
	newEvent := func() *SuspendedCancelEvent {
		return NewSuspendedCancelEvent("dcr_btc", 42, 0, cancel, target, 9876, 6000, 11, 22, matchServerTime)
	}
	if err := newEvent().Validate(); err != nil {
		t.Fatalf("Validate error: %v", err)
	}

	tests := []struct {
		name   string
		mutate func(*SuspendedCancelEvent)
	}{
		{name: "missing market", mutate: func(e *SuspendedCancelEvent) { e.Market = "" }},
		{name: "cancel has wrong order type", mutate: func(e *SuspendedCancelEvent) {
			e.Cancel = order.EncodeOrder(target)
		}},
		{name: "target has wrong order type", mutate: func(e *SuspendedCancelEvent) {
			e.Target = order.EncodeOrder(cancel)
		}},
		{name: "target is an immediate limit order", mutate: func(e *SuspendedCancelEvent) {
			nonStanding := testLimitOrder(6, order.Preimage{})
			nonStanding.Force = order.ImmediateTiF
			e.Target = order.EncodeOrder(nonStanding)
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			event := newEvent()
			tt.mutate(event)
			if err := event.Validate(); err == nil {
				t.Fatalf("expected validation error")
			}
			payload, err := event.Encode()
			if err != nil {
				t.Fatalf("Encode error: %v", err)
			}
			if _, err := DecodeSuspendedCancelEvent(payload); err == nil {
				t.Fatalf("expected decode validation error")
			}
		})
	}
}
