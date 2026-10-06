// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package meshevents

import (
	"fmt"
	"reflect"
	"testing"
	"time"

	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/order"
	"decred.org/dcrdex/server/account"
)

func TestOrdersRevokedEventEncodeDecode(t *testing.T) {
	var oid order.OrderID
	oid[0] = 0xab
	var user account.AccountID
	user[0] = 0xcd
	revokeTime := time.UnixMilli(123456789000).UTC()

	tests := []struct {
		name     string
		event    *OrdersRevokedEvent
		wantJSON string
	}{
		{
			name: "orders",
			event: NewOrdersRevokedForOrdersEvent("dcr_btc", []order.OrderID{oid},
				OrderRevokeReasonFundingSpent, revokeTime),
			wantJSON: fmt.Sprintf(`{"market":"dcr_btc","orders":["%x"],"reason":1,"revokeTime":123456789000}`, oid[:]),
		},
		{
			name:     "user",
			event:    NewOrdersRevokedForUserEvent(user, OrderRevokeReasonDisconnected, revokeTime),
			wantJSON: fmt.Sprintf(`{"user":"%x","reason":2,"revokeTime":123456789000}`, user[:]),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			payload, err := tt.event.Encode()
			if err != nil {
				t.Fatalf("Encode: %v", err)
			}
			if string(payload) != tt.wantJSON {
				t.Fatalf("payload = %s, want %s", payload, tt.wantJSON)
			}
			got, err := DecodeOrdersRevokedEvent(payload)
			if err != nil {
				t.Fatalf("Decode: %v", err)
			}
			if !reflect.DeepEqual(got, tt.event) {
				t.Fatalf("round trip = %+v, want %+v", got, tt.event)
			}
			if got.Kind() != EventKindOrdersRevoked {
				t.Fatalf("kind = %q, want %q", got.Kind(), EventKindOrdersRevoked)
			}
		})
	}
}

func TestOrdersRevokedEventValidate(t *testing.T) {
	var oid order.OrderID
	var user account.AccountID
	revokeTime := time.UnixMilli(1000)
	orderForm := func() *OrdersRevokedEvent {
		return NewOrdersRevokedForOrdersEvent("dcr_btc", []order.OrderID{oid}, OrderRevokeReasonAdmin, revokeTime)
	}
	userForm := func() *OrdersRevokedEvent {
		return NewOrdersRevokedForUserEvent(user, OrderRevokeReasonPenalty, revokeTime)
	}
	if err := orderForm().Validate(); err != nil {
		t.Fatalf("order form Validate: %v", err)
	}
	if err := userForm().Validate(); err != nil {
		t.Fatalf("user form Validate: %v", err)
	}

	tests := []struct {
		name   string
		new    func() *OrdersRevokedEvent
		mutate func(*OrdersRevokedEvent)
	}{
		{"invalid reason", orderForm, func(e *OrdersRevokedEvent) { e.Reason = OrderRevokeReasonInvalid }},
		{"unknown reason", orderForm, func(e *OrdersRevokedEvent) { e.Reason = 200 }},
		{"missing revoke time", orderForm, func(e *OrdersRevokedEvent) { e.RevokeTime = 0 }},
		{"both forms set", orderForm, func(e *OrdersRevokedEvent) {
			e.User = user[:]
			e.Market = ""
		}},
		{"neither form set", orderForm, func(e *OrdersRevokedEvent) { e.OrderIDs = nil }},
		{"short user ID", userForm, func(e *OrdersRevokedEvent) { e.User = e.User[:8] }},
		{"user form with market", userForm, func(e *OrdersRevokedEvent) { e.Market = "dcr_btc" }},
		{"order form without market", orderForm, func(e *OrdersRevokedEvent) { e.Market = "" }},
		{"short order ID", orderForm, func(e *OrdersRevokedEvent) { e.OrderIDs = []dex.Bytes{oid[:8]} }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			event := tt.new()
			tt.mutate(event)
			if err := event.Validate(); err == nil {
				t.Fatal("expected validation error")
			}
			payload, err := event.Encode()
			if err != nil {
				t.Fatalf("Encode: %v", err)
			}
			if _, err := DecodeOrdersRevokedEvent(payload); err == nil {
				t.Fatal("expected decode validation error")
			}
		})
	}
}
