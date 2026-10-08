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

// AuditAckRecordedEvent records a client's acknowledgement of its
// counterparty's swap contract.
type AuditAckRecordedEvent struct {
	MatchID order.MatchID `json:"matchID"`
	Base    uint32        `json:"base"`
	Quote   uint32        `json:"quote"`
	// Maker is true when the acknowledging client is the match's maker.
	Maker bool      `json:"maker"`
	Sig   dex.Bytes `json:"sig"`
}

// Kind identifies the mesh event kind.
func (e *AuditAckRecordedEvent) Kind() string { return EventKindAuditAckRecorded }

// Encode returns the canonical wire payload replicated across the mesh.
func (e *AuditAckRecordedEvent) Encode() ([]byte, error) {
	return json.Marshal(e)
}

// DecodeAuditAckRecordedEvent decodes and validates an audit_ack_recorded
// wire payload.
func DecodeAuditAckRecordedEvent(payload []byte) (*AuditAckRecordedEvent, error) {
	return decodeEvent[AuditAckRecordedEvent](payload)
}

// Validate checks the event is well-formed, independent of any node state.
func (e *AuditAckRecordedEvent) Validate() error {
	if e == nil {
		return fmt.Errorf("nil audit ack recorded event")
	}
	return validateAckRecorded("audit ack", e.MatchID, e.Base, e.Quote, e.Sig)
}

// validateAckRecorded checks that an acknowledgement identifies a match and
// market and includes a signature.
func validateAckRecorded(kind string, matchID order.MatchID, base, quote uint32, sig dex.Bytes) error {
	if matchID == (order.MatchID{}) {
		return fmt.Errorf("empty %s match id", kind)
	}
	if base == 0 && quote == 0 {
		return fmt.Errorf("empty %s market for match %v", kind, matchID)
	}
	if len(sig) == 0 {
		return fmt.Errorf("empty %s signature for match %v", kind, matchID)
	}
	return nil
}

// EventTxData returns the versioned transaction data recorded in the event log
// for an audit_ack_recorded event.
func (e *AuditAckRecordedEvent) EventTxData() ([]byte, error) {
	if e == nil {
		return nil, fmt.Errorf("nil audit ack")
	}
	return encode.BuildyBytes{0}.
		AddData(e.MatchID[:]).
		AddData(encode.Uint32Bytes(e.Base)).
		AddData(encode.Uint32Bytes(e.Quote)).
		AddData(boolBytes(e.Maker)).
		AddData(e.Sig), nil
}
