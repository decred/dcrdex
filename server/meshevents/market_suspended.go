// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package meshevents

import (
	"encoding/json"
	"fmt"
)

// MarketSuspendedEvent completes a scheduled suspension after its final epoch is processed.
type MarketSuspendedEvent struct {
	Market        string `json:"market"`
	FinalEpochIdx int64  `json:"finalEpochIdx"`
	EpochDur      int64  `json:"epochDur"` // milliseconds
	// Timestamp is the order revocation time in Unix milliseconds.
	Timestamp int64 `json:"timestamp"`
}

// Kind identifies the event kind.
func (e *MarketSuspendedEvent) Kind() string { return EventKindMarketSuspended }

// Encode returns the event payload.
func (e *MarketSuspendedEvent) Encode() ([]byte, error) { return json.Marshal(e) }

// DecodeMarketSuspendedEvent decodes and validates the event payload.
func DecodeMarketSuspendedEvent(payload []byte) (*MarketSuspendedEvent, error) {
	return decodeEvent[MarketSuspendedEvent](payload)
}

// Validate checks the event fields independently of market state.
func (e *MarketSuspendedEvent) Validate() error {
	if e == nil {
		return fmt.Errorf("nil market_suspended event")
	}
	if err := validateMarketEpoch(e.Market, e.FinalEpochIdx, e.EpochDur); err != nil {
		return err
	}
	if e.Timestamp <= 0 {
		return fmt.Errorf("market_suspended event missing timestamp")
	}
	return nil
}
