// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package mesh

import (
	"context"
	"fmt"

	"decred.org/dcrdex/server/db"
)

// CommittedEventApplyError reports a failure after the event transaction
// committed.
type CommittedEventApplyError struct {
	Applied *db.EventLogEntry
	Err     error
}

func (err *CommittedEventApplyError) Error() string {
	return fmt.Sprintf("committed event apply side effect failed: %v", err.Err)
}

func (err *CommittedEventApplyError) Unwrap() error {
	return err.Err
}

// eventOriginKind states who initiated an event and therefore who is owed a
// client response after applying the event.
type eventOriginKind uint8

const (
	// originPlain is an authoritative event with no client command attached.
	// This is used for events which are emitted by master workers.
	originPlain eventOriginKind = iota
	// originLocalCommand is a command from a client connected to this node.
	originLocalCommand
	// originForwardedCommand is a command forwarded by the peer slave and
	// is currently being applied on the master.
	originForwardedCommand
	// originReceivedCommand is a command that was forwarded by the slave to
	// the master, and is currently being applied on the slave after successfully
	// being applied on the master.
	originReceivedCommand
)

// eventOrigin records how to deliver the command result for an event.
// Construct it with the origin helpers.
//
// Fields by origin kind:
//   - originLocalCommand: completion, resultFn
//   - originForwardedCommand: commandID, resultFn
//   - originReceivedCommand: commandID
type eventOrigin struct {
	kind       eventOriginKind
	commandID  string
	completion *CommandCompletion
	resultFn   func() any
}

func plainEventOrigin() eventOrigin { return eventOrigin{kind: originPlain} }

func localCommandEventOrigin(completion *CommandCompletion, resultFn func() any) eventOrigin {
	return eventOrigin{
		kind:       originLocalCommand,
		completion: completion,
		resultFn:   resultFn,
	}
}

func forwardedCommandEventOrigin(commandID string, resultFn func() any) eventOrigin {
	return eventOrigin{
		kind:      originForwardedCommand,
		commandID: commandID,
		resultFn:  resultFn,
	}
}

// collectsAfterCommandResult reports whether this node owns the local client
// response for the event and should therefore gather AfterCommandResult
// callbacks during apply.
func (o eventOrigin) collectsAfterCommandResult() bool {
	return o.kind == originLocalCommand || o.kind == originReceivedCommand
}

// deliverLocalResult delivers a local-command result when this origin owns
// that response. Other origins are a no-op.
func (o eventOrigin) deliverLocalResult(result any) error {
	if o.kind != originLocalCommand || result == nil || o.completion == nil {
		return nil
	}
	return o.completion.deliverResult(result)
}

// EventApplyContext is passed to an EventApplier. It contains metadata
// related to the event, and callbacks to run after the event is applied.
type EventApplyContext struct {
	context.Context
	// Position is the master's assigned seq and tip hash for a received event.
	// It is nil when this node is the master and is applying the event for
	// the first time.
	Position *db.EventLogPosition

	collectAfterCommandResult bool
	afterCommandResult        []func(context.Context)
	result                    any
}

// SetResult sets the apply's result: ApplyEvent returns it, and for a command
// with no result function it is also the client's command response.
func (c *EventApplyContext) SetResult(result any) {
	c.result = result
}

// Result returns the value recorded by SetResult, or nil.
func (c *EventApplyContext) Result() any {
	return c.result
}

// AfterCommandResult registers f to run after successful application,
// following any attempt by this node to send the command result.
// It returns false if this apply does not handle that response,
// or if c or f is nil.
func (c *EventApplyContext) AfterCommandResult(f func(context.Context)) bool {
	if c == nil || !c.collectAfterCommandResult || f == nil {
		return false
	}
	c.afterCommandResult = append(c.afterCommandResult, f)
	return true
}

// EventApplier applies one event and must persist and return the log row
// (at Position when set). Mesh delivers the client response; the applier
// does not.
type EventApplier func(*EventApplyContext, *Event) (*db.EventLogEntry, error)
