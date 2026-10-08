// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package meshevents

import (
	"encoding/json"
	"fmt"
	"time"

	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/order"
	"decred.org/dcrdex/server/matcher"
)

// PreimageRevealedRecord pairs an encoded epoch order with the preimage its
// owner revealed for that order's commitment.
type PreimageRevealedRecord struct {
	EncodedOrder dex.Bytes `json:"order"`
	Preimage     dex.Bytes `json:"preimage"`
}

// EpochProcessedEvent is the payload of an epoch_processed event. It contains
// the preimage collection results and matching inputs for a closed epoch.
type EpochProcessedEvent struct {
	Market         string                   `json:"market"`
	EpochIdx       int64                    `json:"epochIdx"`
	EpochDur       int64                    `json:"epochDur"`  // milliseconds
	MatchTime      int64                    `json:"matchTime"` // Unix ms
	FeeRateBase    uint64                   `json:"feeRateBase"`
	FeeRateQuote   uint64                   `json:"feeRateQuote"`
	LastRate       uint64                   `json:"lastRate"` // last matched rate before this epoch, per the emitting master
	CSum           dex.Bytes                `json:"cSum"`
	MissRevokeTime int64                    `json:"missRevokeTime,omitempty"` // Unix ms; required if Missed is non-empty
	Revealed       []PreimageRevealedRecord `json:"revealed"`
	Missed         []dex.Bytes              `json:"missed"`
}

// NewEpochProcessedEvent builds an epoch_processed event from the epoch's
// preimage collection outcome and match inputs.
func NewEpochProcessedEvent(marketName string, epochIdx, epochDur int64, matchTime time.Time,
	feeRateBase, feeRateQuote, lastRate uint64, cSum []byte, ordersRevealed []*matcher.OrderRevealed,
	misses []order.Order, missRevokeTime time.Time) *EpochProcessedEvent {

	var missRevokeMs int64
	if !missRevokeTime.IsZero() {
		missRevokeMs = missRevokeTime.UnixMilli()
	}
	return &EpochProcessedEvent{
		Market:         marketName,
		EpochIdx:       epochIdx,
		EpochDur:       epochDur,
		MatchTime:      matchTime.UnixMilli(),
		FeeRateBase:    feeRateBase,
		FeeRateQuote:   feeRateQuote,
		LastRate:       lastRate,
		CSum:           append(dex.Bytes(nil), cSum...),
		MissRevokeTime: missRevokeMs,
		Revealed:       encodePreimageRevealedRecords(ordersRevealed),
		Missed:         encodeOrders(misses),
	}
}

func encodePreimageRevealedRecords(ordersRevealed []*matcher.OrderRevealed) []PreimageRevealedRecord {
	revealed := make([]PreimageRevealedRecord, 0, len(ordersRevealed))
	for _, ord := range ordersRevealed {
		revealed = append(revealed, PreimageRevealedRecord{
			EncodedOrder: dex.Bytes(order.EncodeOrder(ord.Order)),
			Preimage:     append(dex.Bytes(nil), ord.Preimage[:]...),
		})
	}
	return revealed
}

// decodePreimageRevealedRecords decodes revealed order/preimage pairs,
// checking each preimage against its order's commitment.
func decodePreimageRevealedRecords(revealed []PreimageRevealedRecord) ([]*matcher.OrderRevealed, error) {
	ordersRevealed := make([]*matcher.OrderRevealed, 0, len(revealed))
	for _, rec := range revealed {
		ord, err := order.DecodeOrder(rec.EncodedOrder)
		if err != nil {
			return nil, err
		}
		if len(rec.Preimage) != order.PreimageSize {
			return nil, fmt.Errorf("invalid preimage length %d for order %v", len(rec.Preimage), ord.ID())
		}
		var pi order.Preimage
		copy(pi[:], rec.Preimage)
		piCommit := pi.Commit()
		ordCommit := ord.Commitment()
		if piCommit != ordCommit {
			return nil, fmt.Errorf("preimage hash %x does not match order commitment %x for order %v",
				piCommit[:], ordCommit[:], ord.ID())
		}
		ordersRevealed = append(ordersRevealed, &matcher.OrderRevealed{
			Order:    ord,
			Preimage: pi,
		})
	}
	return ordersRevealed, nil
}

// encodeOrders encodes each order to its wire form.
func encodeOrders(orders []order.Order) []dex.Bytes {
	encoded := make([]dex.Bytes, 0, len(orders))
	for _, ord := range orders {
		encoded = append(encoded, dex.Bytes(order.EncodeOrder(ord)))
	}
	return encoded
}

// decodeOrders decodes a slice of wire-encoded orders.
func decodeOrders(encoded []dex.Bytes) ([]order.Order, error) {
	orders := make([]order.Order, 0, len(encoded))
	for _, enc := range encoded {
		ord, err := order.DecodeOrder(enc)
		if err != nil {
			return nil, err
		}
		orders = append(orders, ord)
	}
	return orders, nil
}

// Kind identifies the mesh event kind.
func (e *EpochProcessedEvent) Kind() string { return EventKindEpochProcessed }

// Encode returns the canonical wire payload replicated across the mesh.
func (e *EpochProcessedEvent) Encode() ([]byte, error) {
	return json.Marshal(e)
}

// DecodeEpochProcessedEvent decodes and validates an epoch_processed wire
// payload.
func DecodeEpochProcessedEvent(payload []byte) (*EpochProcessedEvent, error) {
	return decodeEvent[EpochProcessedEvent](payload)
}

// Validate checks the event is well-formed, independent of any node state.
func (e *EpochProcessedEvent) Validate() error {
	if e == nil {
		return fmt.Errorf("nil epoch processed event")
	}
	if e.Market == "" {
		return fmt.Errorf("epoch_processed event missing market name")
	}
	if e.EpochDur <= 0 {
		return fmt.Errorf("epoch_processed event invalid epoch duration %d", e.EpochDur)
	}
	if len(e.Missed) > 0 && e.MissRevokeTime == 0 {
		return fmt.Errorf("epoch_processed event missing miss revoke time")
	}
	return nil
}

// OrdersRevealed decodes the revealed orders and their preimages, verifying
// each preimage against its order's commitment.
func (e *EpochProcessedEvent) OrdersRevealed() ([]*matcher.OrderRevealed, error) {
	return decodePreimageRevealedRecords(e.Revealed)
}

// MissedOrders decodes the orders whose owners failed to reveal a preimage.
func (e *EpochProcessedEvent) MissedOrders() ([]order.Order, error) {
	return decodeOrders(e.Missed)
}
