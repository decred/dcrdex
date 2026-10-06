// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

// Package meshevents defines payloads for replicated DEX events.
package meshevents

import "encoding/json"

// EventSchemaVersion identifies the event schema shared by mesh peers.
const EventSchemaVersion uint32 = 0

// decodeEvent decodes a JSON event payload and validates it.
func decodeEvent[T any, PT interface {
	*T
	Validate() error
}](payload []byte) (PT, error) {
	e := PT(new(T))
	if err := json.Unmarshal(payload, e); err != nil {
		return nil, err
	}
	if err := e.Validate(); err != nil {
		return nil, err
	}
	return e, nil
}

const (
	EventKindBondPosted          = "bond_posted"
	EventKindPrepaidBondsCreated = "prepaid_bonds_created"
	EventKindOrderAccepted       = "order_accepted"
	EventKindMarketStarted       = "market_started"

	EventKindMarketSuspendScheduled = "market_suspend_scheduled"
	EventKindMarketSuspended        = "market_suspended"
	EventKindMarketResumeScheduled  = "market_resume_scheduled"
	EventKindMarketResumed          = "market_resumed"
	EventKindAdvanceEpoch           = "advance_epoch"
	EventKindEpochProcessed         = "epoch_processed"
	EventKindSuspendedCancel        = "suspended_cancel"
	EventKindOrdersRevoked          = "orders_revoked"
	EventKindReputationForgiven     = "reputation_forgiven"
)
