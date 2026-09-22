// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package meshevents

import (
	"encoding/json"
	"fmt"

	"decred.org/dcrdex/dex/encode"
	"decred.org/dcrdex/dex/order"
	"decred.org/dcrdex/server/account"
)

// ReputationForgivenessScope identifies what a reputation_forgiven event
// forgives: a whole account, or a single match.
type ReputationForgivenessScope uint8

const (
	ReputationForgivenessScopeInvalid ReputationForgivenessScope = iota
	ReputationForgivenessScopeUser
	ReputationForgivenessScopeMatch
)

// ReputationForgivenEvent is the payload of a reputation_forgiven event,
// identifying the account or match whose penalties are forgiven.
type ReputationForgivenEvent struct {
	AccountID account.AccountID          `json:"accountID"`
	Scope     ReputationForgivenessScope `json:"scope"`
	MatchID   *order.MatchID             `json:"matchID,omitempty"`
}

// Kind identifies the mesh event kind.
func (e *ReputationForgivenEvent) Kind() string { return EventKindReputationForgiven }

// Encode returns the event's JSON payload.
func (e *ReputationForgivenEvent) Encode() ([]byte, error) {
	return json.Marshal(e)
}

// DecodeReputationForgivenEvent decodes and validates a reputation_forgiven
// wire payload.
func DecodeReputationForgivenEvent(payload []byte) (*ReputationForgivenEvent, error) {
	return decodeEvent[ReputationForgivenEvent](payload)
}

// Validate checks the event is well-formed, independent of any node state.
func (e *ReputationForgivenEvent) Validate() error {
	if e == nil {
		return fmt.Errorf("nil reputation forgiveness event")
	}
	var zeroAccount account.AccountID
	if e.AccountID == zeroAccount {
		return fmt.Errorf("zero account id in reputation forgiveness event")
	}
	var zeroMID order.MatchID
	switch e.Scope {
	case ReputationForgivenessScopeUser:
		if e.MatchID != nil {
			return fmt.Errorf("user-scope reputation forgiveness event specifies match")
		}
	case ReputationForgivenessScopeMatch:
		if e.MatchID == nil || *e.MatchID == zeroMID {
			return fmt.Errorf("match-scope reputation forgiveness event missing match id")
		}
	default:
		return fmt.Errorf("invalid reputation forgiveness scope %d", e.Scope)
	}
	return nil
}

// Match returns the target match id for a match-scope event, or the zero id.
func (e *ReputationForgivenEvent) Match() order.MatchID {
	if e.MatchID == nil {
		return order.MatchID{}
	}
	return *e.MatchID
}

// EventTxData returns the versioned durable transaction data recorded in the
// event log and folded into the hash chain.
func (e *ReputationForgivenEvent) EventTxData() ([]byte, error) {
	mid := e.Match()
	return encode.BuildyBytes{0}.
		AddData(e.AccountID[:]).
		AddData([]byte{byte(e.Scope)}).
		AddData(mid[:]), nil
}
