// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package mesh

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"

	"decred.org/dcrdex/dex"
)

// Event is one replicated state change.
type Event struct {
	Kind    string          `json:"kind"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

// EventEncoder is a typed event payload that can be turned into an Event.
// Kind names the event and Encode produces its payload.
type EventEncoder interface {
	Kind() string
	Encode() ([]byte, error)
}

// NewEvent wraps e as an *Event ready to apply or publish.
func NewEvent(e EventEncoder) (*Event, error) {
	payload, err := e.Encode()
	if err != nil {
		return nil, err
	}
	return &Event{Kind: e.Kind(), Payload: payload}, nil
}

func validateEvent(event *Event) error {
	if event == nil {
		return fmt.Errorf("nil event")
	}
	if event.Kind == "" {
		return fmt.Errorf("event kind not specified")
	}
	return nil
}

const eventLogTipHashSize = sha256.Size

// eventEnvelope is an event and its metadata, sent by the master to the slave
// over the event stream.
type eventEnvelope struct {
	Seq     uint64    `json:"seq"`
	TipHash dex.Bytes `json:"tipHash"`
	// MasterTip is the master's tip when the event was sent. If
	// Seq == MasterTip, the slave will know that it is caught up.
	MasterTip       uint64          `json:"masterTip"`
	Kind            string          `json:"kind"`
	OriginCommandID string          `json:"originCommandID,omitempty"`
	CommandResult   json.RawMessage `json:"commandResult,omitempty"`
	Payload         json.RawMessage `json:"payload,omitempty"`
}

func validateEventEnvelope(envelope *eventEnvelope) error {
	if envelope == nil {
		return fmt.Errorf("nil event envelope")
	}
	if envelope.Seq == 0 {
		return fmt.Errorf("event seq cannot be 0")
	}
	if envelope.MasterTip < envelope.Seq {
		return fmt.Errorf("event master tip %d before seq %d", envelope.MasterTip, envelope.Seq)
	}
	if len(envelope.TipHash) != eventLogTipHashSize {
		return fmt.Errorf("event tip hash length %d, want %d", len(envelope.TipHash), eventLogTipHashSize)
	}
	return validateEvent(&Event{
		Kind:    envelope.Kind,
		Payload: envelope.Payload,
	})
}

// eventBatch is an ordered run of event envelopes sent as one request over
// the event stream.
type eventBatch struct {
	Entries []*eventEnvelope `json:"entries"`
}

func validateEventBatch(batch *eventBatch) error {
	if batch == nil || len(batch.Entries) == 0 {
		return fmt.Errorf("empty event batch")
	}
	if len(batch.Entries) > eventStreamBatchLimit {
		return fmt.Errorf("event batch of %d entries exceeds limit %d",
			len(batch.Entries), eventStreamBatchLimit)
	}
	for i, envelope := range batch.Entries {
		if err := validateEventEnvelope(envelope); err != nil {
			return fmt.Errorf("entry %d: %w", i, err)
		}
		if i > 0 && envelope.Seq != batch.Entries[i-1].Seq+1 {
			return fmt.Errorf("entry %d: seq %d does not follow %d", i, envelope.Seq, batch.Entries[i-1].Seq)
		}
	}
	return nil
}

// eventAck is the slave's response to an eventBatch, sent after every
// entry in the batch was applied.
type eventAck struct{}

// streamSubscribe is the slave's subscription to the master's event stream. A
// slave with no replayable history must first seed from a snapshot by sending
// a snapshotRequest.
type streamSubscribe struct {
	Frontier frontierMessage `json:"frontier"`
}

// streamSubscribeResult is the master's response to a slave's streamSubscribe.
type streamSubscribeResult struct {
	MasterTip uint64 `json:"masterTip"`
}

func validateStreamSubscribe(sub *streamSubscribe) error {
	if sub == nil {
		return fmt.Errorf("nil stream subscribe")
	}
	if err := validateFrontierMessage(sub.Frontier); err != nil {
		return fmt.Errorf("invalid subscribe frontier: %w", err)
	}
	return nil
}

// snapshotRequest is the slave's request for a snapshot of the master's
// state. A slave with an empty event log sends it before it subscribes to the
// event stream.
type snapshotRequest struct{}
