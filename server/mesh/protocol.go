// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package mesh

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
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
