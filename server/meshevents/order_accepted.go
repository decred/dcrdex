// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package meshevents

import (
	"encoding/json"
	"fmt"

	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/order"
)

// OrderAcceptedEvent is the payload of an order_accepted event.
type OrderAcceptedEvent struct {
	EncodedOrder dex.Bytes `json:"order"`
}

// NewOrderAcceptedEvent builds an order_accepted event for an order.
func NewOrderAcceptedEvent(ord order.Order) *OrderAcceptedEvent {
	return &OrderAcceptedEvent{EncodedOrder: order.EncodeOrder(ord)}
}

// Kind identifies the mesh event kind.
func (e *OrderAcceptedEvent) Kind() string { return EventKindOrderAccepted }

// Encode returns the canonical wire payload replicated across the mesh.
func (e *OrderAcceptedEvent) Encode() ([]byte, error) {
	return json.Marshal(e)
}

// DecodeOrderAcceptedEvent decodes and validates an order_accepted wire payload.
func DecodeOrderAcceptedEvent(payload []byte) (*OrderAcceptedEvent, error) {
	return decodeEvent[OrderAcceptedEvent](payload)
}

// Validate checks the event is well-formed, independent of any node state.
func (e *OrderAcceptedEvent) Validate() error {
	if e == nil {
		return fmt.Errorf("nil order accepted event")
	}
	_, err := e.Order()
	return err
}

// Order decodes the accepted order carried by the event.
func (e *OrderAcceptedEvent) Order() (order.Order, error) {
	if len(e.EncodedOrder) == 0 {
		return nil, fmt.Errorf("empty accepted order")
	}
	return order.DecodeOrder(e.EncodedOrder)
}
