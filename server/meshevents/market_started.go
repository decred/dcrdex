// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package meshevents

import (
	"encoding/json"
	"fmt"
	"time"

	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/order"
)

// MarketRunParams contains the trading parameters for a market run.
// These parameters are recorded in the event log so all nodes use the same
// rules for order validation and matching when applying or replaying events.
type MarketRunParams struct {
	LotSize                uint64 `json:"lotSize"`
	RateStep               uint64 `json:"rateStep"`
	ParcelSize             uint32 `json:"parcelSize"`
	MaxUserCancelsPerEpoch uint32 `json:"maxUserCancels"`
	MinimumRate            uint64 `json:"minimumRate"`
}

// Validate checks that the run parameters are well-formed.
// MinimumRate and MaxUserCancelsPerEpoch may be zero.
func (p *MarketRunParams) Validate() error {
	if p == nil {
		return fmt.Errorf("missing market run parameters")
	}
	if p.LotSize == 0 {
		return fmt.Errorf("market run parameters missing lot size")
	}
	if p.RateStep == 0 {
		return fmt.Errorf("market run parameters missing rate step")
	}
	if p.ParcelSize == 0 {
		return fmt.Errorf("market run parameters missing parcel size")
	}
	return nil
}

// StartupOrderRevokeReason identifies why market startup revoked an order.
type StartupOrderRevokeReason uint8

const (
	StartupOrderRevokeReasonInvalid StartupOrderRevokeReason = 0

	StartupOrderRevokeReasonLotSizeIncompatible StartupOrderRevokeReason = 1
	StartupOrderRevokeReasonFundingCoinSpent    StartupOrderRevokeReason = 2
	StartupOrderRevokeReasonAccountLowBalance   StartupOrderRevokeReason = 3
	StartupOrderRevokeReasonEpochAbandoned      StartupOrderRevokeReason = 4
)

// ValidStartupOrderRevokeReason indicates whether reason is a known startup
// order revocation reason.
func ValidStartupOrderRevokeReason(reason StartupOrderRevokeReason) bool {
	switch reason {
	case StartupOrderRevokeReasonLotSizeIncompatible,
		StartupOrderRevokeReasonFundingCoinSpent,
		StartupOrderRevokeReasonAccountLowBalance,
		StartupOrderRevokeReasonEpochAbandoned:
		return true
	}
	return false
}

// StartupOrderRevokeRecord contains an encoded order and its startup
// revocation reason.
type StartupOrderRevokeRecord struct {
	EncodedOrder dex.Bytes                `json:"order"`
	Reason       StartupOrderRevokeReason `json:"reason"`
}

// NewStartupOrderRevokeRecord builds a revoke record for an order.
func NewStartupOrderRevokeRecord(ord order.Order, reason StartupOrderRevokeReason) StartupOrderRevokeRecord {
	return StartupOrderRevokeRecord{
		EncodedOrder: dex.Bytes(order.EncodeOrder(ord)),
		Reason:       reason,
	}
}

// MarketStartedEvent contains the epoch, trading parameters, and orders to
// revoke during startup recovery. Trading starts unless the market is draining
// or its scheduled final epoch has passed.
type MarketStartedEvent struct {
	Market          string                     `json:"market"`
	CurrentEpochIdx int64                      `json:"currentEpochIdx"` // current epoch at startup
	EpochDur        int64                      `json:"epochDur"`        // milliseconds
	RunParams       MarketRunParams            `json:"runParams"`
	RevocationTime  int64                      `json:"revocationTime"` // revocation timestamp in Unix milliseconds
	BookedRevokes   []StartupOrderRevokeRecord `json:"bookedRevokes"`
	// EpochRevokes lists the abandoned epoch orders. It must include every
	// active epoch order exactly once.
	EpochRevokes []StartupOrderRevokeRecord `json:"epochRevokes,omitempty"`
}

// NewMarketStartedEvent builds a market_started event.
func NewMarketStartedEvent(marketName string, currentEpochIdx, epochDur int64, runParams MarketRunParams,
	revocationTime time.Time, bookedRevokes, epochRevokes []StartupOrderRevokeRecord) *MarketStartedEvent {

	if bookedRevokes == nil {
		bookedRevokes = make([]StartupOrderRevokeRecord, 0)
	}
	return &MarketStartedEvent{
		Market:          marketName,
		CurrentEpochIdx: currentEpochIdx,
		EpochDur:        epochDur,
		RunParams:       runParams,
		RevocationTime:  revocationTime.UnixMilli(),
		BookedRevokes:   bookedRevokes,
		EpochRevokes:    epochRevokes,
	}
}

// Kind identifies the mesh event kind.
func (e *MarketStartedEvent) Kind() string { return EventKindMarketStarted }

// Encode returns the canonical wire payload replicated across the mesh.
func (e *MarketStartedEvent) Encode() ([]byte, error) {
	return json.Marshal(e)
}

// DecodeMarketStartedEvent decodes and validates a market_started wire
// payload.
func DecodeMarketStartedEvent(payload []byte) (*MarketStartedEvent, error) {
	return decodeEvent[MarketStartedEvent](payload)
}

// Validate checks the event is well-formed, independent of any node state.
func (e *MarketStartedEvent) Validate() error {
	if e == nil {
		return fmt.Errorf("nil market started event")
	}
	if e.Market == "" {
		return fmt.Errorf("market_started event missing market name")
	}
	if e.RevocationTime == 0 {
		return fmt.Errorf("market_started event missing revocation time")
	}
	if e.CurrentEpochIdx <= 0 {
		return fmt.Errorf("market_started event invalid current epoch %d", e.CurrentEpochIdx)
	}
	if e.EpochDur <= 0 {
		return fmt.Errorf("market_started event invalid epoch duration %d", e.EpochDur)
	}
	return e.RunParams.Validate()
}
