// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package meshevents

import (
	"encoding/json"
	"fmt"

	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/encode"
	"decred.org/dcrdex/dex/order"
)

// RedemptionAckRecordedEvent is a client ack of the counterparty's redeem.
type RedemptionAckRecordedEvent struct {
	MatchID order.MatchID `json:"matchID"`
	Base    uint32        `json:"base"`
	Quote   uint32        `json:"quote"`
	// Maker is true when the acking client is the match's maker (the acked
	// redemption is then the taker's).
	Maker bool      `json:"maker"`
	Sig   dex.Bytes `json:"sig"`
}

// Kind identifies the mesh event kind.
func (e *RedemptionAckRecordedEvent) Kind() string { return EventKindRedemptionAckRecorded }

// Encode returns the canonical wire payload replicated across the mesh.
func (e *RedemptionAckRecordedEvent) Encode() ([]byte, error) {
	return json.Marshal(e)
}

// DecodeRedemptionAckRecordedEvent decodes and validates a
// redemption_ack_recorded wire payload.
func DecodeRedemptionAckRecordedEvent(payload []byte) (*RedemptionAckRecordedEvent, error) {
	return decodeEvent[RedemptionAckRecordedEvent](payload)
}

// Validate checks the event is well-formed, independent of any node state.
func (e *RedemptionAckRecordedEvent) Validate() error {
	if e == nil {
		return fmt.Errorf("nil redemption ack recorded event")
	}
	return validateAckRecorded("redemption ack", e.MatchID, e.Base, e.Quote, e.Sig)
}

// EventTxData returns the versioned transaction data recorded in the event log
// for a redemption_ack_recorded event.
func (e *RedemptionAckRecordedEvent) EventTxData() ([]byte, error) {
	if e == nil {
		return nil, fmt.Errorf("nil redemption ack")
	}
	return encode.BuildyBytes{0}.
		AddData(e.MatchID[:]).
		AddData(encode.Uint32Bytes(e.Base)).
		AddData(encode.Uint32Bytes(e.Quote)).
		AddData(boolBytes(e.Maker)).
		AddData(e.Sig), nil
}
