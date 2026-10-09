// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package meshevents

import (
	"encoding/json"
	"fmt"

	"decred.org/dcrdex/dex/encode"
	"decred.org/dcrdex/dex/order"
)

// MatchFailureFault identifies the party responsible for a match failure.
type MatchFailureFault uint8

const (
	MatchFailureNoUserFault MatchFailureFault = iota + 1
	MatchFailureMakerFault
	MatchFailureTakerFault
)

// MatchFailedEvent records that a match failed, the status it reached,
// and which party, if any, was at fault.
type MatchFailedEvent struct {
	MatchID  order.MatchID `json:"matchID"`
	Base     uint32        `json:"base"`
	Quote    uint32        `json:"quote"`
	FailTime int64         `json:"failTime"` // Unix milliseconds
	// Status is the match status when the failure was detected.
	Status order.MatchStatus `json:"status"`
	Fault  MatchFailureFault `json:"fault"`
	// MakerAddressKnown and TakerAddressKnown record which swap addresses were
	// available when the failure was detected. They are checked for timeouts in
	// NewlyMatched to detect acknowledgements that invalidate the decision.
	MakerAddressKnown bool `json:"makerAddressKnown"`
	TakerAddressKnown bool `json:"takerAddressKnown"`
}

// Kind identifies the mesh event kind.
func (e *MatchFailedEvent) Kind() string { return EventKindMatchFailed }

// Encode returns the canonical wire payload replicated across the mesh.
func (e *MatchFailedEvent) Encode() ([]byte, error) {
	return json.Marshal(e)
}

// DecodeMatchFailedEvent decodes and validates a match_failed wire payload.
func DecodeMatchFailedEvent(payload []byte) (*MatchFailedEvent, error) {
	return decodeEvent[MatchFailedEvent](payload)
}

// Validate checks the event is well-formed, independent of any node state.
func (e *MatchFailedEvent) Validate() error {
	if e == nil {
		return fmt.Errorf("nil match failed event")
	}
	if e.MatchID == (order.MatchID{}) {
		return fmt.Errorf("empty match_failed match ID")
	}
	if e.Base == 0 && e.Quote == 0 {
		return fmt.Errorf("empty match_failed market for match %v", e.MatchID)
	}
	if e.FailTime <= 0 {
		return fmt.Errorf("empty match_failed time for match %v", e.MatchID)
	}
	switch e.Fault {
	case MatchFailureNoUserFault, MatchFailureMakerFault, MatchFailureTakerFault:
	default:
		return fmt.Errorf("invalid match failure fault %d", e.Fault)
	}
	switch e.Status {
	case order.NewlyMatched:
		// Either the maker failed to swap or the taker withheld its address.
	case order.MakerSwapCast, order.MakerRedeemed:
		if e.Fault == MatchFailureMakerFault {
			return fmt.Errorf("maker cannot be at fault at match status %v", e.Status)
		}
	case order.TakerSwapCast:
		if e.Fault == MatchFailureTakerFault {
			return fmt.Errorf("taker cannot be at fault at match status %v", e.Status)
		}
	default:
		return fmt.Errorf("invalid match failure status %v", e.Status)
	}
	return nil
}

// EventTxData returns the versioned transaction data recorded in the event log
// for a match_failed event.
func (e *MatchFailedEvent) EventTxData() ([]byte, error) {
	if e == nil {
		return nil, fmt.Errorf("nil match failed event")
	}
	return encode.BuildyBytes{0}.
		AddData(e.MatchID[:]).
		AddData(encode.Uint32Bytes(e.Base)).
		AddData(encode.Uint32Bytes(e.Quote)).
		AddData(encode.Uint64Bytes(uint64(e.FailTime))).
		AddData([]byte{byte(e.Status)}).
		AddData([]byte{byte(e.Fault)}).
		AddData(boolBytes(e.MakerAddressKnown)).
		AddData(boolBytes(e.TakerAddressKnown)), nil
}
