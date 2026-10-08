// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package meshevents

import (
	"math"
	"testing"
)

func TestAdvanceEpochEventEncodeDecode(t *testing.T) {
	event := NewAdvanceEpochEvent("dcr_btc", 41, 42, 6000)
	payload, err := event.Encode()
	if err != nil {
		t.Fatalf("Encode error: %v", err)
	}
	// The wire encoding is consensus-critical and must not change.
	const wantJSON = `{"market":"dcr_btc","closedEpochIdx":41,"openedEpochIdx":42,"epochDur":6000}`
	if string(payload) != wantJSON {
		t.Fatalf("payload = %s, want %s", payload, wantJSON)
	}
	got, err := DecodeAdvanceEpochEvent(payload)
	if err != nil {
		t.Fatalf("Decode error: %v", err)
	}
	if *got != *event {
		t.Fatalf("round trip = %+v, want %+v", got, event)
	}
	if got.Kind() != EventKindAdvanceEpoch {
		t.Fatalf("kind = %q, want %q", got.Kind(), EventKindAdvanceEpoch)
	}

	// Final-epoch form leaves OpenedEpochIdx zero.
	finalEvent := NewAdvanceEpochEvent("dcr_btc", 41, 0, 6000)
	if err := finalEvent.Validate(); err != nil {
		t.Fatalf("final epoch Validate error: %v", err)
	}
}

func TestAdvanceEpochEventValidate(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*AdvanceEpochEvent)
	}{
		{"missing market", func(e *AdvanceEpochEvent) { e.Market = "" }},
		{"zero duration", func(e *AdvanceEpochEvent) { e.EpochDur = 0 }},
		{"negative duration", func(e *AdvanceEpochEvent) { e.EpochDur = -1 }},
		{"negative closed epoch", func(e *AdvanceEpochEvent) { e.ClosedEpochIdx = -1 }},
		{"opened epoch overflow", func(e *AdvanceEpochEvent) {
			e.ClosedEpochIdx = math.MaxInt64
			e.OpenedEpochIdx = math.MinInt64
		}},
		{"opened epoch skips closed epoch", func(e *AdvanceEpochEvent) { e.OpenedEpochIdx = e.ClosedEpochIdx + 2 }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			event := NewAdvanceEpochEvent("dcr_btc", 41, 42, 6000)
			tt.mutate(event)
			if err := event.Validate(); err == nil {
				t.Fatalf("expected validation error")
			}
			payload, err := event.Encode()
			if err != nil {
				t.Fatalf("Encode error: %v", err)
			}
			if _, err := DecodeAdvanceEpochEvent(payload); err == nil {
				t.Fatalf("expected decode validation error")
			}
		})
	}
}
