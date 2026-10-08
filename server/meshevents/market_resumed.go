// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package meshevents

import (
	"encoding/json"
	"fmt"
)

// MarketResumedEvent resumes trading with the supplied parameters and order revocations.
type MarketResumedEvent struct {
	Market        string `json:"market"`
	StartEpochIdx int64  `json:"startEpochIdx"`
	EpochDur      int64  `json:"epochDur"` // milliseconds
	// Timestamp is the order revocation time in Unix milliseconds.
	Timestamp int64 `json:"timestamp"`
	// ResumeRevokes lists booked orders that failed the checks for resumption.
	ResumeRevokes []StartupOrderRevokeRecord `json:"resumeRevokes,omitempty"`
	// RunParams contains the trading parameters used when the market resumes.
	RunParams MarketRunParams `json:"runParams"`
}

// Kind identifies the event kind.
func (e *MarketResumedEvent) Kind() string { return EventKindMarketResumed }

// Encode returns the event payload.
func (e *MarketResumedEvent) Encode() ([]byte, error) { return json.Marshal(e) }

// DecodeMarketResumedEvent decodes and validates the event payload.
func DecodeMarketResumedEvent(payload []byte) (*MarketResumedEvent, error) {
	return decodeEvent[MarketResumedEvent](payload)
}

// Validate checks the event fields independently of market state.
func (e *MarketResumedEvent) Validate() error {
	if e == nil {
		return fmt.Errorf("nil market_resumed event")
	}
	if err := validateMarketEpoch(e.Market, e.StartEpochIdx, e.EpochDur); err != nil {
		return err
	}
	if e.Timestamp <= 0 {
		return fmt.Errorf("market_resumed event missing timestamp")
	}
	if err := e.RunParams.Validate(); err != nil {
		return err
	}
	return nil
}
