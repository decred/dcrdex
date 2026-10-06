// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package meshevents

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestMarketResumedEvent(t *testing.T) {
	valid := MarketResumedEvent{Market: "dcr_btc", StartEpochIdx: 100, EpochDur: 6000, Timestamp: 600000, RunParams: MarketRunParams{LotSize: 100000, RateStep: 1000, ParcelSize: 10}}
	for _, tc := range []struct {
		name    string
		mutate  func(*MarketResumedEvent)
		wantErr bool
	}{
		{name: "valid"},
		{name: "missing market", mutate: func(e *MarketResumedEvent) { e.Market = "" }, wantErr: true},
		{name: "missing epoch", mutate: func(e *MarketResumedEvent) { e.StartEpochIdx = 0 }, wantErr: true},
		{name: "negative epoch", mutate: func(e *MarketResumedEvent) { e.StartEpochIdx = -1 }, wantErr: true},
		{name: "missing duration", mutate: func(e *MarketResumedEvent) { e.EpochDur = 0 }, wantErr: true},
		{name: "missing timestamp", mutate: func(e *MarketResumedEvent) { e.Timestamp = 0 }, wantErr: true},
		{name: "missing run parameters", mutate: func(e *MarketResumedEvent) { e.RunParams = MarketRunParams{} }, wantErr: true},
		{name: "invalid run parameters", mutate: func(e *MarketResumedEvent) { e.RunParams.LotSize = 0 }, wantErr: true},
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
			decoded, err := DecodeMarketResumedEvent(payload)
			if (err != nil) != tc.wantErr {
				t.Fatalf("decode error = %v, want error %t", err, tc.wantErr)
			}
			if tc.wantErr {
				return
			}
			if !reflect.DeepEqual(decoded, &event) {
				t.Fatalf("decoded = %+v, want %+v", decoded, event)
			}
			if decoded.Kind() != EventKindMarketResumed {
				t.Fatalf("kind = %q", decoded.Kind())
			}
		})
	}
	if err := (*MarketResumedEvent)(nil).Validate(); err == nil {
		t.Fatal("nil event accepted")
	}
	if _, err := DecodeMarketResumedEvent(json.RawMessage(`{`)); err == nil {
		t.Fatal("invalid JSON accepted")
	}
}
