// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package meshevents

import (
	"encoding/json"
	"fmt"
)

// MarketResumeScheduledEvent records when a suspended market will resume.
type MarketResumeScheduledEvent struct {
	Market        string `json:"market"`
	StartEpochIdx int64  `json:"startEpochIdx"`
	EpochDur      int64  `json:"epochDur"` // milliseconds
}

// Kind identifies the event kind.
func (e *MarketResumeScheduledEvent) Kind() string { return EventKindMarketResumeScheduled }

// Encode returns the event payload.
func (e *MarketResumeScheduledEvent) Encode() ([]byte, error) { return json.Marshal(e) }

// DecodeMarketResumeScheduledEvent decodes and validates the event payload.
func DecodeMarketResumeScheduledEvent(payload []byte) (*MarketResumeScheduledEvent, error) {
	return decodeEvent[MarketResumeScheduledEvent](payload)
}

// Validate checks the event fields independently of market state.
func (e *MarketResumeScheduledEvent) Validate() error {
	if e == nil {
		return fmt.Errorf("nil market_resume_scheduled event")
	}
	if err := validateMarketEpoch(e.Market, e.StartEpochIdx, e.EpochDur); err != nil {
		return err
	}
	return nil
}
