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

// MatchAckRecord is a single verified client match acknowledgement carried by
// a match_acks_recorded event.
type MatchAckRecord struct {
	MatchID order.MatchID `json:"matchID"`
	Base    uint32        `json:"base"`
	Quote   uint32        `json:"quote"`
	// Maker identifies an acknowledgement from the match's maker.
	Maker bool `json:"maker"`
	// Cancel marks a cancel match, which involves no swap.
	Cancel bool `json:"cancel,omitempty"`
	// Sig is the user's signature over the match notification.
	Sig dex.Bytes `json:"sig"`
	// Address is the user's swap address, unused for cancel matches.
	Address string `json:"address,omitempty"`
}

// MatchAcksRecordedEvent is a batch of verified client match acknowledgements.
type MatchAcksRecordedEvent struct {
	AckTime int64            `json:"ackTime"` // Acknowledgement time, in Unix milliseconds.
	Records []MatchAckRecord `json:"records"`
}

// Kind identifies the mesh event kind.
func (e *MatchAcksRecordedEvent) Kind() string { return EventKindMatchAcksRecorded }

// Encode returns the canonical wire payload replicated across the mesh.
func (e *MatchAcksRecordedEvent) Encode() ([]byte, error) {
	return json.Marshal(e)
}

// DecodeMatchAcksRecordedEvent decodes and validates a match_acks_recorded
// wire payload.
func DecodeMatchAcksRecordedEvent(payload []byte) (*MatchAcksRecordedEvent, error) {
	return decodeEvent[MatchAcksRecordedEvent](payload)
}

// Validate checks the event is well-formed, independent of any node state.
func (e *MatchAcksRecordedEvent) Validate() error {
	if e == nil {
		return fmt.Errorf("nil match acks recorded event")
	}
	if e.AckTime == 0 {
		return fmt.Errorf("empty match acks recorded time")
	}
	if len(e.Records) == 0 {
		return fmt.Errorf("empty match acks recorded event")
	}
	for i, record := range e.Records {
		if len(record.Sig) == 0 {
			return fmt.Errorf("empty match ack signature at index %d", i)
		}
		if !record.Cancel && record.Address == "" {
			return fmt.Errorf("empty match ack address for non-cancel match %v", record.MatchID)
		}
	}
	return nil
}

// EventTxData encodes the acknowledgement signatures and addresses recorded
// by a match_acks_recorded event.
func (e *MatchAcksRecordedEvent) EventTxData() ([]byte, error) {
	if e == nil {
		return nil, fmt.Errorf("nil match acks recorded event")
	}
	b := encode.BuildyBytes{0}
	for _, record := range e.Records {
		b = b.AddData(encode.BuildyBytes{0}.
			AddData(record.MatchID[:]).
			AddData(encode.Uint32Bytes(record.Base)).
			AddData(encode.Uint32Bytes(record.Quote)).
			AddData(boolBytes(record.Maker)).
			AddData(boolBytes(record.Cancel)).
			AddData(record.Sig).
			AddData([]byte(record.Address)))
	}
	return b, nil
}
