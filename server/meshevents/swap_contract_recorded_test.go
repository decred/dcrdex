// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package meshevents

import (
	"reflect"
	"testing"

	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/order"
)

func tSwapContractRecordedEvent() *SwapContractRecordedEvent {
	var mid order.MatchID
	mid[0] = 0x01
	return &SwapContractRecordedEvent{
		MatchID:     mid,
		Base:        42,
		Quote:       0,
		Maker:       true,
		Status:      order.MakerSwapCast,
		CoinID:      dex.Bytes{0x0a, 0x0b},
		CoinTxID:    "txid",
		CoinString:  "txid:0",
		Value:       1e8,
		FeeRate:     10,
		Contract:    dex.Bytes{0x0c},
		SwapAddress: "swap-addr",
		SecretHash:  dex.Bytes{0x0d},
		LockTime:    1670000000123,
		TxData:      dex.Bytes{0x0e},
		SwapTime:    1670000000456,
	}
}

func TestSwapContractRecordedEventRoundTrip(t *testing.T) {
	event := tSwapContractRecordedEvent()

	payload, err := event.Encode()
	if err != nil {
		t.Fatalf("Encode error: %v", err)
	}

	got, err := DecodeSwapContractRecordedEvent(payload)
	if err != nil {
		t.Fatalf("Decode error: %v", err)
	}
	if !reflect.DeepEqual(got, event) {
		t.Fatalf("round trip mismatch.\nwant: %#v\n got: %#v", event, got)
	}
}

func TestSwapContractRecordedEventValidate(t *testing.T) {
	mutate := func(f func(e *SwapContractRecordedEvent)) *SwapContractRecordedEvent {
		e := tSwapContractRecordedEvent()
		f(e)
		return e
	}
	tests := []struct {
		name    string
		event   *SwapContractRecordedEvent
		wantErr bool
	}{
		{"maker ok", tSwapContractRecordedEvent(), false},
		{"taker ok", mutate(func(e *SwapContractRecordedEvent) {
			e.Maker = false
			e.Status = order.TakerSwapCast
		}), false},
		{"zero match id", mutate(func(e *SwapContractRecordedEvent) { e.MatchID = order.MatchID{} }), true},
		{"empty market", mutate(func(e *SwapContractRecordedEvent) { e.Base, e.Quote = 0, 0 }), true},
		{"empty coin id", mutate(func(e *SwapContractRecordedEvent) { e.CoinID = nil }), true},
		{"empty contract", mutate(func(e *SwapContractRecordedEvent) { e.Contract = nil }), true},
		{"empty swap address", mutate(func(e *SwapContractRecordedEvent) { e.SwapAddress = "" }), true},
		{"empty secret hash", mutate(func(e *SwapContractRecordedEvent) { e.SecretHash = nil }), true},
		{"empty lock time", mutate(func(e *SwapContractRecordedEvent) { e.LockTime = 0 }), true},
		{"empty swap time", mutate(func(e *SwapContractRecordedEvent) { e.SwapTime = 0 }), true},
		{"maker wrong status", mutate(func(e *SwapContractRecordedEvent) { e.Status = order.TakerSwapCast }), true},
		{"taker wrong status", mutate(func(e *SwapContractRecordedEvent) {
			e.Maker = false
			e.Status = order.MakerSwapCast
		}), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.event.Validate(); (err != nil) != tt.wantErr {
				t.Fatalf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
