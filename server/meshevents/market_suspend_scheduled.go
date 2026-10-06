// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package meshevents

import (
	"encoding/json"
	"fmt"
)

// MarketSuspendScheduledEvent records when a market will suspend and whether to retain its book.
type MarketSuspendScheduledEvent struct {
	Market        string `json:"market"`
	FinalEpochIdx int64  `json:"finalEpochIdx"`
	EpochDur      int64  `json:"epochDur"` // milliseconds
	PersistBook   bool   `json:"persistBook"`
}

// Kind identifies the event kind.
func (e *MarketSuspendScheduledEvent) Kind() string { return EventKindMarketSuspendScheduled }

// Encode returns the event payload.
func (e *MarketSuspendScheduledEvent) Encode() ([]byte, error) { return json.Marshal(e) }

// DecodeMarketSuspendScheduledEvent decodes and validates the event payload.
func DecodeMarketSuspendScheduledEvent(payload []byte) (*MarketSuspendScheduledEvent, error) {
	return decodeEvent[MarketSuspendScheduledEvent](payload)
}

// Validate checks the event fields independently of market state.
func (e *MarketSuspendScheduledEvent) Validate() error {
	if e == nil {
		return fmt.Errorf("nil market_suspend_scheduled event")
	}
	if err := validateMarketEpoch(e.Market, e.FinalEpochIdx, e.EpochDur); err != nil {
		return err
	}
	return nil
}

// validateMarketEpoch checks that a market name and positive epoch coordinates are supplied.
func validateMarketEpoch(market string, epochIdx, epochDur int64) error {
	if market == "" {
		return fmt.Errorf("missing market name")
	}
	if epochIdx <= 0 {
		return fmt.Errorf("invalid epoch %d", epochIdx)
	}
	if epochDur <= 0 {
		return fmt.Errorf("invalid epoch duration %d", epochDur)
	}
	return nil
}
