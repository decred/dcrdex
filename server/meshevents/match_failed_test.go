// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package meshevents

import (
	"bytes"
	"reflect"
	"slices"
	"testing"

	"decred.org/dcrdex/dex/encode"
	"decred.org/dcrdex/dex/order"
)

func TestMatchFailedEventRoundTrip(t *testing.T) {
	event := &MatchFailedEvent{
		MatchID: order.MatchID{1}, Base: 42, FailTime: 1670000000123,
		Status: order.NewlyMatched, Fault: MatchFailureMakerFault,
		TakerAddressKnown: true,
	}
	payload, err := event.Encode()
	if err != nil {
		t.Fatal(err)
	}
	got, err := DecodeMatchFailedEvent(payload)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, event) {
		t.Fatalf("round trip = %+v, want %+v", got, event)
	}

	// The captured state and fault are part of the event-log transaction data.
	txData, err := event.EventTxData()
	if err != nil {
		t.Fatal(err)
	}
	version, pushes, err := encode.DecodeBlob(txData)
	if err != nil {
		t.Fatal(err)
	}
	want := [][]byte{event.MatchID[:], encode.Uint32Bytes(event.Base), encode.Uint32Bytes(event.Quote),
		encode.Uint64Bytes(uint64(event.FailTime)), {byte(event.Status)}, {byte(event.Fault)}, {0}, {1}}
	if version != 0 || !reflect.DeepEqual(pushes, want) {
		t.Fatalf("txdata version/pushes = %d/%x, want 0/%x", version, pushes, want)
	}
	for _, change := range []func(*MatchFailedEvent){
		func(e *MatchFailedEvent) { e.Status = order.TakerSwapCast },
		func(e *MatchFailedEvent) { e.Fault = MatchFailureNoUserFault },
		func(e *MatchFailedEvent) { e.MakerAddressKnown = true },
		func(e *MatchFailedEvent) { e.TakerAddressKnown = false },
	} {
		changed := *event
		change(&changed)
		changedData, err := changed.EventTxData()
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Equal(txData, changedData) {
			t.Fatal("captured state/fault change did not change txdata")
		}
	}
}

func TestMatchFailedEventValidate(t *testing.T) {
	valid := func() *MatchFailedEvent {
		return &MatchFailedEvent{MatchID: order.MatchID{1}, Base: 42, FailTime: 1670000000123,
			Status: order.MakerSwapCast, Fault: MatchFailureTakerFault}
	}
	mutate := func(f func(*MatchFailedEvent)) *MatchFailedEvent { e := valid(); f(e); return e }
	for _, tt := range []struct {
		name    string
		event   *MatchFailedEvent
		wantErr bool
	}{
		{"ok", valid(), false},
		{"nil", nil, true},
		{"zero match ID", mutate(func(e *MatchFailedEvent) { e.MatchID = order.MatchID{} }), true},
		{"empty market", mutate(func(e *MatchFailedEvent) { e.Base = 0 }), true},
		{"empty fail time", mutate(func(e *MatchFailedEvent) { e.FailTime = 0 }), true},
		{"negative fail time", mutate(func(e *MatchFailedEvent) { e.FailTime = -1 }), true},
		{"missing fault", mutate(func(e *MatchFailedEvent) { e.Fault = 0 }), true},
		{"unknown fault", mutate(func(e *MatchFailedEvent) { e.Fault = 255 }), true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.event.Validate(); (err != nil) != tt.wantErr {
				t.Fatalf("Validate = %v, want error %v", err, tt.wantErr)
			}
		})
	}
	for _, tt := range []struct {
		status      order.MatchStatus
		validFaults []MatchFailureFault
	}{
		{order.NewlyMatched, []MatchFailureFault{MatchFailureNoUserFault, MatchFailureMakerFault, MatchFailureTakerFault}},
		{order.MakerSwapCast, []MatchFailureFault{MatchFailureNoUserFault, MatchFailureTakerFault}},
		{order.TakerSwapCast, []MatchFailureFault{MatchFailureNoUserFault, MatchFailureMakerFault}},
		{order.MakerRedeemed, []MatchFailureFault{MatchFailureNoUserFault, MatchFailureTakerFault}},
		{order.MatchComplete, nil}, {order.MatchStatus(255), nil},
	} {
		t.Run(tt.status.String(), func(t *testing.T) {
			for _, fault := range []MatchFailureFault{MatchFailureNoUserFault, MatchFailureMakerFault, MatchFailureTakerFault} {
				e := valid()
				e.Status, e.Fault = tt.status, fault
				wantValid := slices.Contains(tt.validFaults, fault)
				if err := e.Validate(); (err == nil) != wantValid {
					t.Fatalf("status %v fault %v: error %v, want valid %v", tt.status, fault, err, wantValid)
				}
			}
		})
	}
}
