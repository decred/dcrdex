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

// SwapContractRecordedEvent is the payload for recording a validated swap contract.
type SwapContractRecordedEvent struct {
	MatchID order.MatchID `json:"matchID"`
	Base    uint32        `json:"base"`
	Quote   uint32        `json:"quote"`
	// Maker identifies the maker's contract.
	Maker bool `json:"maker"`
	// Status is the match status after this contract is recorded.
	Status      order.MatchStatus `json:"status"`
	CoinID      dex.Bytes         `json:"coinID"`
	CoinTxID    string            `json:"coinTxID,omitempty"`
	CoinString  string            `json:"coinString,omitempty"`
	Value       uint64            `json:"value"`
	FeeRate     uint64            `json:"feeRate"`
	Contract    dex.Bytes         `json:"contract"`
	SwapAddress string            `json:"swapAddress"`
	SecretHash  dex.Bytes         `json:"secretHash"`
	LockTime    int64             `json:"lockTime"` // Unix milliseconds
	TxData      dex.Bytes         `json:"txData,omitempty"`
	// SwapTime is when the server accepted the contract, in Unix milliseconds.
	SwapTime int64 `json:"swapTime"`
}

// Kind identifies the mesh event kind.
func (e *SwapContractRecordedEvent) Kind() string { return EventKindSwapContractRecorded }

// Encode returns the canonical wire payload replicated across the mesh.
func (e *SwapContractRecordedEvent) Encode() ([]byte, error) {
	return json.Marshal(e)
}

// DecodeSwapContractRecordedEvent decodes and validates a
// swap_contract_recorded wire payload.
func DecodeSwapContractRecordedEvent(payload []byte) (*SwapContractRecordedEvent, error) {
	return decodeEvent[SwapContractRecordedEvent](payload)
}

// Validate checks the event is well-formed, independent of any node state.
func (e *SwapContractRecordedEvent) Validate() error {
	if e == nil {
		return fmt.Errorf("nil swap contract recorded event")
	}
	if e.MatchID == (order.MatchID{}) {
		return fmt.Errorf("empty swap contract recorded match id")
	}
	if e.Base == 0 && e.Quote == 0 {
		return fmt.Errorf("empty swap contract recorded market for match %v", e.MatchID)
	}
	if len(e.CoinID) == 0 {
		return fmt.Errorf("empty swap contract coin id for match %v", e.MatchID)
	}
	if len(e.Contract) == 0 {
		return fmt.Errorf("empty swap contract for match %v", e.MatchID)
	}
	if e.SwapAddress == "" {
		return fmt.Errorf("empty swap address for match %v", e.MatchID)
	}
	if len(e.SecretHash) == 0 {
		return fmt.Errorf("empty swap secret hash for match %v", e.MatchID)
	}
	if e.LockTime == 0 {
		return fmt.Errorf("empty swap lock time for match %v", e.MatchID)
	}
	if e.SwapTime == 0 {
		return fmt.Errorf("empty swap contract time for match %v", e.MatchID)
	}
	if e.Maker {
		if e.Status != order.MakerSwapCast {
			return fmt.Errorf("maker swap contract event has status %v", e.Status)
		}
	} else if e.Status != order.TakerSwapCast {
		return fmt.Errorf("taker swap contract event has status %v", e.Status)
	}
	return nil
}

// EventTxData returns the versioned transaction data recorded in the event log
// for a swap_contract_recorded event.
func (e *SwapContractRecordedEvent) EventTxData() ([]byte, error) {
	if e == nil {
		return nil, fmt.Errorf("nil swap contract")
	}
	return encode.BuildyBytes{0}.
		AddData(e.MatchID[:]).
		AddData(encode.Uint32Bytes(e.Base)).
		AddData(encode.Uint32Bytes(e.Quote)).
		AddData(boolBytes(e.Maker)).
		AddData(e.Contract).
		AddData(e.CoinID).
		AddData(encode.Uint64Bytes(uint64(e.SwapTime))), nil
}
