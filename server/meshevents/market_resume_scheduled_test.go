// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package meshevents

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestMarketResumeScheduledEvent(t *testing.T) {
	valid := MarketResumeScheduledEvent{Market: "dcr_btc", StartEpochIdx: 100, EpochDur: 6000}
	for _, tc := range []struct {
		name    string
		mutate  func(*MarketResumeScheduledEvent)
		wantErr bool
	}{
		{name: "valid"},
		{name: "missing market", mutate: func(e *MarketResumeScheduledEvent) { e.Market = "" }, wantErr: true},
		{name: "missing epoch", mutate: func(e *MarketResumeScheduledEvent) { e.StartEpochIdx = 0 }, wantErr: true},
		{name: "negative epoch", mutate: func(e *MarketResumeScheduledEvent) { e.StartEpochIdx = -1 }, wantErr: true},
		{name: "missing duration", mutate: func(e *MarketResumeScheduledEvent) { e.EpochDur = 0 }, wantErr: true},
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
			decoded, err := DecodeMarketResumeScheduledEvent(payload)
			if (err != nil) != tc.wantErr {
				t.Fatalf("decode error = %v, want error %t", err, tc.wantErr)
			}
			if tc.wantErr {
				return
			}
			if !reflect.DeepEqual(decoded, &event) {
				t.Fatalf("decoded = %+v, want %+v", decoded, event)
			}
			if decoded.Kind() != EventKindMarketResumeScheduled {
				t.Fatalf("kind = %q", decoded.Kind())
			}
		})
	}
	if err := (*MarketResumeScheduledEvent)(nil).Validate(); err == nil {
		t.Fatal("nil event accepted")
	}
	if _, err := DecodeMarketResumeScheduledEvent(json.RawMessage(`{`)); err == nil {
		t.Fatal("invalid JSON accepted")
	}
}
