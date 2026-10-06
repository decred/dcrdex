// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package meshevents

import (
	"testing"
	"time"

	"decred.org/dcrdex/dex/order"
)

func TestOrderAcceptedEventEncodeDecode(t *testing.T) {
	lo := &order.LimitOrder{
		P: order.Prefix{
			BaseAsset:  42,
			QuoteAsset: 0,
			OrderType:  order.LimitOrderType,
			ClientTime: time.UnixMilli(1000),
			ServerTime: time.UnixMilli(1001),
		},
		T: order.Trade{
			Sell:     true,
			Quantity: 10,
		},
		Rate:  5,
		Force: order.StandingTiF,
	}
	event := NewOrderAcceptedEvent(lo)
	payload, err := event.Encode()
	if err != nil {
		t.Fatalf("Encode error: %v", err)
	}
	got, err := DecodeOrderAcceptedEvent(payload)
	if err != nil {
		t.Fatalf("Decode error: %v", err)
	}
	ord, err := got.Order()
	if err != nil {
		t.Fatalf("Order error: %v", err)
	}
	if ord.ID() != lo.ID() {
		t.Fatalf("round trip order id = %v, want %v", ord.ID(), lo.ID())
	}

	if _, err := DecodeOrderAcceptedEvent([]byte(`{"order":""}`)); err == nil {
		t.Fatalf("decoded event with empty order")
	}
}
