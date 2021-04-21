// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package admin

import (
	"time"

	"decred.org/dcrdex/dex"
)

// AssetPost is the expected structure of the asset POST data.
type AssetPost struct {
	FeeRateScale *float64 `json:"feeRateScale,omitempty"`
}

// AssetInfo is the result of the asset GET. Note that ScaledFeeRate is
// CurrentFeeRate multiplied by an operator-specified fee scale rate for this
// asset, and then limited by the dex.Asset.MaxFeeRate.
type AssetInfo struct {
	dex.Asset
	CurrentFeeRate uint64   `json:"currentFeeRate,omitempty"`
	ScaledFeeRate  uint64   `json:"scaledFeeRate,omitempty"`
	Synced         bool     `json:"synced"`
	Errors         []string `json:"errors,omitempty"`
}

// MarketStatus summarizes the operational status of a market.
type MarketStatus struct {
	Name          string `json:"market,omitempty"`
	Running       bool   `json:"running"`
	EpochDuration uint64 `json:"epochlen"`
	ActiveEpoch   int64  `json:"activeepoch"`
	StartEpoch    int64  `json:"startepoch"`
	SuspendEpoch  int64  `json:"finalepoch,omitempty"`
	PersistBook   *bool  `json:"persistbook,omitempty"`
}

// MatchData describes a match.
type MatchData struct {
	ID          string `json:"id"`
	TakerSell   bool   `json:"takerSell"`
	Maker       string `json:"makerOrder"`
	MakerAcct   string `json:"makerAcct"`
	MakerSwap   string `json:"makerSwap"`
	MakerRedeem string `json:"makerRedeem"`
	MakerAddr   string `json:"makerAddr"`
	Taker       string `json:"takerOrder"`
	TakerAcct   string `json:"takerAcct"`
	TakerSwap   string `json:"takerSwap"`
	TakerRedeem string `json:"takerRedeem"`
	TakerAddr   string `json:"takerAddr"`
	EpochIdx    uint64 `json:"epochIdx"`
	EpochDur    uint64 `json:"epochDur"`
	Quantity    uint64 `json:"quantity"`
	Rate        uint64 `json:"rate"`
	BaseRate    uint64 `json:"baseFeeRate"`
	QuoteRate   uint64 `json:"quoteFeeRate"`
	Active      bool   `json:"active"`
	Status      string `json:"status"`
}

// APITime marshals and unmarshals a time value in time.RFC3339Nano format.
type APITime struct {
	time.Time
}

// SuspendResult describes the result of a market suspend request. FinalEpoch is
// the last epoch before shutdown, and it the market will run for it's entire
// duration. As such, SuspendTime is the time at which the market is closed,
// immediately after close of FinalEpoch.
type SuspendResult struct {
	Market      string  `json:"market"`
	FinalEpoch  int64   `json:"finalepoch"`
	SuspendTime APITime `json:"supendtime"`
}

// ResumeResult is the result of a market resume request.
type ResumeResult struct {
	Market     string  `json:"market"`
	StartEpoch int64   `json:"startepoch"`
	StartTime  APITime `json:"starttime"`
}

// RFC3339Milli is the RFC3339 time formatting with millisecond precision.
const RFC3339Milli = "2006-01-02T15:04:05.999Z07:00"

// MarshalJSON marshals APITime to a JSON string in RFC3339 format except with
// millisecond precision.
func (at *APITime) MarshalJSON() ([]byte, error) {
	return []byte(`"` + at.Time.Format(RFC3339Milli) + `"`), nil
}

// UnmarshalJSON unmarshals JSON string containing a time in RFC3339 format with
// millisecond precision into an APITime.
func (at *APITime) UnmarshalJSON(b []byte) error {
	if len(b) < 2 {
		return nil
	}
	// Parenthesis are included in b and must be removed.
	t, err := time.Parse(RFC3339Milli, string(b[1:len(b)-1]))
	if err != nil {
		return nil
	}
	at.Time = t
	return nil
}

// BanResult holds the result of a ban.
type BanResult struct {
	AccountID  string  `json:"accountid"`
	BrokenRule byte    `json:"brokenrule"`
	BanTime    APITime `json:"bantime"`
}

// UnbanResult holds the result of an unban.
type UnbanResult struct {
	AccountID string  `json:"accountid"`
	UnbanTime APITime `json:"unbantime"`
}

// ForgiveResult holds the result of a forgive_match.
type ForgiveResult struct {
	AccountID   string  `json:"accountid"`
	Forgiven    bool    `json:"forgiven"`
	Unbanned    bool    `json:"unbanned"`
	ForgiveTime APITime `json:"forgivetime"`
}
