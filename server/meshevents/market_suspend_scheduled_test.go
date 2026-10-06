// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package meshevents

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestMarketSuspendScheduledEvent(t *testing.T) {
	valid := MarketSuspendScheduledEvent{Market: "dcr_btc", FinalEpochIdx: 100, EpochDur: 6000, PersistBook: true}
	for _, tc := range []struct {
		name    string
		mutate  func(*MarketSuspendScheduledEvent)
		wantErr bool
	}{
		{name: "valid"},
		{name: "purge book", mutate: func(e *MarketSuspendScheduledEvent) { e.PersistBook = false }},
		{name: "missing market", mutate: func(e *MarketSuspendScheduledEvent) { e.Market = "" }, wantErr: true},
		{name: "missing epoch", mutate: func(e *MarketSuspendScheduledEvent) { e.FinalEpochIdx = 0 }, wantErr: true},
		{name: "negative epoch", mutate: func(e *MarketSuspendScheduledEvent) { e.FinalEpochIdx = -1 }, wantErr: true},
		{name: "missing duration", mutate: func(e *MarketSuspendScheduledEvent) { e.EpochDur = 0 }, wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			event := valid
			if tc.mutate != nil {
				tc.mutate(&event)
			}
			payload, err := event.Encode()
			if err != nil {
				t.Fatal(err)
			}
			decoded, err := DecodeMarketSuspendScheduledEvent(payload)
			if (err != nil) != tc.wantErr {
				t.Fatalf("decode error = %v, want error %t", err, tc.wantErr)
			}
			if tc.wantErr {
				return
			}
			if !reflect.DeepEqual(decoded, &event) {
				t.Fatalf("decoded = %+v, want %+v", decoded, event)
			}
			if decoded.Kind() != EventKindMarketSuspendScheduled {
				t.Fatalf("kind = %q", decoded.Kind())
			}
		})
	}
	if err := (*MarketSuspendScheduledEvent)(nil).Validate(); err == nil {
		t.Fatal("nil event accepted")
	}
	if _, err := DecodeMarketSuspendScheduledEvent(json.RawMessage(`{`)); err == nil {
		t.Fatal("invalid JSON accepted")
	}
}
