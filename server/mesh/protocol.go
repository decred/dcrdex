// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package mesh

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"

	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/msgjson"
	"decred.org/dcrdex/server/account"
)

// commandForward is a request from the slave to the master that wraps a
// client's state changing request. The master answers the forward when the
// command has run, with an error if the command was refused, or with an
// empty ack. The result arrives separately, in the CommandResult field of
// the eventEnvelope if an event was emitted, or in a commandResult or a
// commandFailure if not, and it can arrive before the ack.
type commandForward struct {
	CommandID string            `json:"commandID"`
	Kind      string            `json:"kind"`
	User      account.AccountID `json:"user"`
	Msg       *msgjson.Message  `json:"msg"`
}

func validateCommandForward(cmd *commandForward) error {
	if cmd == nil {
		return fmt.Errorf("nil command")
	}
	if cmd.CommandID == "" {
		return fmt.Errorf("command id not specified")
	}
	if cmd.Kind == "" {
		return fmt.Errorf("command kind not specified")
	}
	return validateClientRequestMsg(cmd.Msg)
}

// commandFailure is a message from the master to the slave that completes a
// forwarded command with an error, when no event was emitted.
type commandFailure struct {
	CommandID string         `json:"commandID"`
	Error     *msgjson.Error `json:"error"`
}

func validateCommandFailure(fail *commandFailure) error {
	if fail == nil {
		return fmt.Errorf("nil command failure")
	}
	if fail.CommandID == "" {
		return fmt.Errorf("command id not specified")
	}
	if fail.Error == nil {
		return fmt.Errorf("command error not specified")
	}
	return nil
}

// commandResult is a message from the master to the slave that completes a
// forwarded command with a result, when no event was emitted (for example a
// duplicate command that already has a result).
type commandResult struct {
	CommandID string          `json:"commandID"`
	Result    json.RawMessage `json:"result"`
}

func validateCommandResult(result *commandResult) error {
	if result == nil {
		return fmt.Errorf("nil command result")
	}
	if result.CommandID == "" {
		return fmt.Errorf("command id not specified")
	}
	if len(result.Result) == 0 {
		return fmt.Errorf("command result not specified")
	}
	if !json.Valid(result.Result) {
		return fmt.Errorf("invalid command result JSON")
	}
	return nil
}

// ClientProxyMessage is a request to deliver a message to a client
// connected to the peer.
type ClientProxyMessage struct {
	User account.AccountID `json:"user"`
	Msg  *msgjson.Message  `json:"msg"`
	// Broadcast delivers Msg to every client on the peer. User is ignored.
	Broadcast bool `json:"broadcast,omitempty"`
	// TimeoutMS is the timeout of a proxied request.
	TimeoutMS uint64 `json:"timeoutMS,omitempty"`
	// DeliverToClient marks a Response that the server sends to the client,
	// as opposed to a Response that the client sends to the peer.
	DeliverToClient bool `json:"deliverToClient,omitempty"`
}

func validateClientProxyMessage(msg *ClientProxyMessage) error {
	if msg == nil {
		return fmt.Errorf("nil client proxy message")
	}
	if msg.Msg == nil {
		return fmt.Errorf("nil client proxy message payload")
	}
	if msg.Broadcast && msg.Msg.Type != msgjson.Notification {
		return fmt.Errorf("broadcast requires a notification message, got type %d", msg.Msg.Type)
	}
	switch msg.Msg.Type {
	case msgjson.Request:
		if msg.Msg.ID == 0 {
			return fmt.Errorf("request id cannot be 0")
		}
	case msgjson.Response:
		if msg.Msg.ID == 0 {
			return fmt.Errorf("response id cannot be 0")
		}
	case msgjson.Notification:
	default:
		return fmt.Errorf("unsupported message type %d", msg.Msg.Type)
	}
	return nil
}

// maxClientConnectedUsers is the maximum number of users in one
// client_connected query. QueryClientConnected splits larger queries.
const maxClientConnectedUsers = 4096

// clientConnectedQuery asks the peer which of the listed client accounts are
// currently connected to it.
type clientConnectedQuery struct {
	Users []account.AccountID `json:"users"`
}

// clientConnectedResult lists the subset of the queried accounts that are
// connected to the answering node.
type clientConnectedResult struct {
	Connected []account.AccountID `json:"connected,omitempty"`
}

func validateClientConnectedQuery(query *clientConnectedQuery) error {
	if query == nil || len(query.Users) == 0 {
		return fmt.Errorf("empty client connected query")
	}
	if len(query.Users) > maxClientConnectedUsers {
		return fmt.Errorf("client connected query for %d users exceeds limit %d", len(query.Users), maxClientConnectedUsers)
	}
	return nil
}

func validateClientRequestMsg(msg *msgjson.Message) error {
	if msg == nil {
		return fmt.Errorf("nil request message")
	}
	if msg.Type != msgjson.Request {
		return fmt.Errorf("invalid message type %d", msg.Type)
	}
	if msg.ID == 0 {
		return fmt.Errorf("request id cannot be 0")
	}
	return nil
}

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
