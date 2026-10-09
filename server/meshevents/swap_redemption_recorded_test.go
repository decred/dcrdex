// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package meshevents

import (
	"reflect"
	"testing"

	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/order"
)

func tSwapRedemptionRecordedEvent() *SwapRedemptionRecordedEvent {
	var mid order.MatchID
	mid[0] = 0x01
	return &SwapRedemptionRecordedEvent{
		MatchID:    mid,
		Base:       42,
		Quote:      0,
		Maker:      true,
		Status:     order.MakerRedeemed,
		CoinID:     dex.Bytes{0x0a, 0x0b},
		CoinTxID:   "txid",
		CoinString: "txid:0",
		Value:      1e8,
		FeeRate:    10,
		Secret:     dex.Bytes{0x0c},
		RedeemTime: 1670000000123,
	}
}

func TestSwapRedemptionRecordedEventRoundTrip(t *testing.T) {
	event := tSwapRedemptionRecordedEvent()

	payload, err := event.Encode()
	if err != nil {
		t.Fatalf("Encode error: %v", err)
	}

	got, err := DecodeSwapRedemptionRecordedEvent(payload)
	if err != nil {
		t.Fatalf("Decode error: %v", err)
	}
	if !reflect.DeepEqual(got, event) {
		t.Fatalf("round trip mismatch.\nwant: %#v\n got: %#v", event, got)
	}
}

func TestSwapRedemptionRecordedEventValidate(t *testing.T) {
	mutate := func(f func(e *SwapRedemptionRecordedEvent)) *SwapRedemptionRecordedEvent {
		e := tSwapRedemptionRecordedEvent()
		f(e)
		return e
	}
	tests := []struct {
		name    string
		event   *SwapRedemptionRecordedEvent
		wantErr bool
	}{
		{"nil", nil, true},
		{"maker ok", tSwapRedemptionRecordedEvent(), false},
		{"taker ok", mutate(func(e *SwapRedemptionRecordedEvent) {
			e.Maker = false
			e.Status = order.MatchComplete
			e.Secret = nil
		}), false},
		{"zero match id", mutate(func(e *SwapRedemptionRecordedEvent) { e.MatchID = order.MatchID{} }), true},
		{"empty market", mutate(func(e *SwapRedemptionRecordedEvent) { e.Base, e.Quote = 0, 0 }), true},
		{"empty coin id", mutate(func(e *SwapRedemptionRecordedEvent) { e.CoinID = nil }), true},
		{"empty redeem time", mutate(func(e *SwapRedemptionRecordedEvent) { e.RedeemTime = 0 }), true},
		{"negative redeem time", mutate(func(e *SwapRedemptionRecordedEvent) { e.RedeemTime = -1 }), true},
		{"maker wrong status", mutate(func(e *SwapRedemptionRecordedEvent) { e.Status = order.MatchComplete }), true},
		{"taker wrong status", mutate(func(e *SwapRedemptionRecordedEvent) {
			e.Maker = false
			e.Status = order.MakerRedeemed
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
