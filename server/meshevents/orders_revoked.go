// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package meshevents

import (
	"encoding/json"
	"fmt"
	"time"

	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/order"
	"decred.org/dcrdex/server/account"
)

// OrderRevokeReason identifies why booked orders were revoked by an
// orders_revoked event.
type OrderRevokeReason uint8

const (
	OrderRevokeReasonInvalid OrderRevokeReason = 0
	// OrderRevokeReasonFundingSpent means one of the order's funding coins was
	// found spent while the order was still unfilled on the book.
	OrderRevokeReasonFundingSpent OrderRevokeReason = 1
	// OrderRevokeReasonDisconnected means the order's owner was disconnected
	// from the entire mesh for too long.
	OrderRevokeReasonDisconnected OrderRevokeReason = 2
	// OrderRevokeReasonPenalty means the order owner's tier dropped below the
	// trading threshold.
	OrderRevokeReasonPenalty OrderRevokeReason = 3
	// OrderRevokeReasonAdmin means an operator manually revoked the order.
	OrderRevokeReasonAdmin OrderRevokeReason = 4
)

// ValidOrderRevokeReason indicates whether reason is a known orders_revoked
// reason.
func ValidOrderRevokeReason(reason OrderRevokeReason) bool {
	switch reason {
	case OrderRevokeReasonFundingSpent, OrderRevokeReasonDisconnected,
		OrderRevokeReasonPenalty, OrderRevokeReasonAdmin:
		return true
	}
	return false
}

// OrdersRevokedEvent records a server-initiated revocation of booked orders.
// Exactly one of User or OrderIDs is set: User revokes that user's booked
// orders on every market; OrderIDs revokes the listed orders on Market.
type OrdersRevokedEvent struct {
	Market     string            `json:"market,omitempty"`
	User       dex.Bytes         `json:"user,omitempty"`
	OrderIDs   []dex.Bytes       `json:"orders,omitempty"`
	Reason     OrderRevokeReason `json:"reason"`
	RevokeTime int64             `json:"revokeTime"` // Revocation time in Unix milliseconds.
}

// NewOrdersRevokedForOrdersEvent creates an event to revoke the listed orders
// on the named market.
func NewOrdersRevokedForOrdersEvent(marketName string, oids []order.OrderID,
	reason OrderRevokeReason, revokeTime time.Time) *OrdersRevokedEvent {

	orderIDs := make([]dex.Bytes, 0, len(oids))
	for _, oid := range oids {
		orderIDs = append(orderIDs, oid.Bytes())
	}
	return &OrdersRevokedEvent{
		Market:     marketName,
		OrderIDs:   orderIDs,
		Reason:     reason,
		RevokeTime: revokeTime.UnixMilli(),
	}
}

// NewOrdersRevokedForUserEvent creates an event to revoke all of the user's
// booked orders across every market.
func NewOrdersRevokedForUserEvent(user account.AccountID,
	reason OrderRevokeReason, revokeTime time.Time) *OrdersRevokedEvent {

	return &OrdersRevokedEvent{
		User:       user[:],
		Reason:     reason,
		RevokeTime: revokeTime.UnixMilli(),
	}
}

// Kind identifies the mesh event kind.
func (e *OrdersRevokedEvent) Kind() string { return EventKindOrdersRevoked }

// Encode returns the canonical wire payload replicated across the mesh.
func (e *OrdersRevokedEvent) Encode() ([]byte, error) {
	return json.Marshal(e)
}

// DecodeOrdersRevokedEvent decodes and validates an orders_revoked wire
// payload.
func DecodeOrdersRevokedEvent(payload []byte) (*OrdersRevokedEvent, error) {
	return decodeEvent[OrdersRevokedEvent](payload)
}

// Validate checks the event is well-formed, independent of any node state.
func (e *OrdersRevokedEvent) Validate() error {
	if e == nil {
		return fmt.Errorf("nil orders revoked event")
	}
	if !ValidOrderRevokeReason(e.Reason) {
		return fmt.Errorf("orders_revoked event invalid reason %d", e.Reason)
	}
	if e.RevokeTime <= 0 {
		return fmt.Errorf("orders_revoked event missing revoke time")
	}
	hasUser := len(e.User) > 0
	hasOrders := len(e.OrderIDs) > 0
	if hasUser == hasOrders {
		return fmt.Errorf("orders_revoked event must set exactly one of user and orders")
	}
	if hasUser {
		if len(e.User) != account.HashSize {
			return fmt.Errorf("orders_revoked event invalid user ID length %d", len(e.User))
		}
		if e.Market != "" {
			return fmt.Errorf("orders_revoked user form must not set a market")
		}
		return nil
	}
	if e.Market == "" {
		return fmt.Errorf("orders_revoked event missing market name")
	}
	for _, oid := range e.OrderIDs {
		if len(oid) != order.OrderIDSize {
			return fmt.Errorf("orders_revoked event invalid order ID length %d", len(oid))
		}
	}
	return nil
}
