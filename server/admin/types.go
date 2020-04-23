package admin

import (
	"time"
)

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
	t, err := time.Parse(RFC3339Milli, string(b))
	if err != nil {
		return nil
	}
	at.Time = t
	return nil
}
