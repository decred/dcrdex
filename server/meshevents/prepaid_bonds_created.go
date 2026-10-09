// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package meshevents

import (
	"encoding/json"
	"fmt"

	"decred.org/dcrdex/dex/encode"
)

// PrepaidBond is an issued prepaid bond token not yet redeemed.
type PrepaidBond struct {
	// CoinID is a random token generated at creation; the redeeming client
	// presents it as the bond coin. It is not an on-chain coin.
	CoinID   []byte
	Strength uint32
	LockTime int64 // Bond lock time in Unix seconds.
}

// PrepaidBondsCreatedEvent is the payload of a prepaid_bonds_created event,
// containing the issued prepaid bond tokens.
type PrepaidBondsCreatedEvent struct {
	Bonds []*PrepaidBond `json:"bonds"`
}

// Kind identifies the mesh event kind.
func (e *PrepaidBondsCreatedEvent) Kind() string { return EventKindPrepaidBondsCreated }

// Encode returns the event's JSON payload.
func (e *PrepaidBondsCreatedEvent) Encode() ([]byte, error) {
	return json.Marshal(e)
}

// DecodePrepaidBondsCreatedEvent decodes and validates a prepaid_bonds_created
// wire payload.
func DecodePrepaidBondsCreatedEvent(payload []byte) (*PrepaidBondsCreatedEvent, error) {
	return decodeEvent[PrepaidBondsCreatedEvent](payload)
}

// Validate checks the event is well-formed, independent of any node state.
func (e *PrepaidBondsCreatedEvent) Validate() error {
	if e == nil {
		return fmt.Errorf("nil prepaid bonds created event")
	}
	if e.Bonds == nil {
		return fmt.Errorf("nil prepaid bonds created list")
	}
	for _, bond := range e.Bonds {
		if bond == nil {
			return fmt.Errorf("nil prepaid bond")
		}
		if len(bond.CoinID) == 0 {
			return fmt.Errorf("empty prepaid bond id")
		}
	}
	return nil
}

func prepaidBondTxData(bond *PrepaidBond) []byte {
	return encode.BuildyBytes{0}.
		AddData(bond.CoinID).
		AddData(encode.Uint32Bytes(bond.Strength)).
		AddData(encode.Uint64Bytes(uint64(bond.LockTime)))
}

// EventTxData returns the versioned durable transaction data recorded in the
// event log and folded into the hash chain.
func (e *PrepaidBondsCreatedEvent) EventTxData() ([]byte, error) {
	if err := e.Validate(); err != nil {
		return nil, err
	}
	b := encode.BuildyBytes{0}
	for _, bond := range e.Bonds {
		b = b.AddData(prepaidBondTxData(bond))
	}
	return b, nil
}
