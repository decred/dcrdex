// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package mesh

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/msgjson"
	"decred.org/dcrdex/server/account"
)

const (
	// defaultPendingCommandTimeout is how long a slave waits for a forwarded
	// command before giving up and answering with an unknown outcome error.
	defaultPendingCommandTimeout = 2 * time.Minute

	// forwardCommandTimeout bounds the command_forward request itself. It must
	// stay below defaultPendingCommandTimeout so an entry held after a forward
	// timeout still has a window in which a late completion can land.
	forwardCommandTimeout = 90 * time.Second
)

// CommandRequest is a client state-changing request.
type CommandRequest struct {
	// Kind identifies the type of command.
	Kind string

	// User is the user that is requesting the command.
	User account.AccountID

	// Msg is the command payload.
	Msg *msgjson.Message

	// Mesh calls Respond to deliver the command result or failure.
	// Command executors complete requests through CommandCompletion.
	// If ExecuteCommand returns an error, the caller sends that error
	// to the client and Respond is not called.
	Respond func(*msgjson.Message) error
}

// pendingCommand tracks a forwarded command awaiting completion.
type pendingCommand struct {
	reqID   uint64
	respond func(*msgjson.Message) error
	expire  *time.Timer
	acked   bool
}

// resultUnavailableError answers a pending command whose outcome this node
// does not know. It must never be used for a refusal.
func resultUnavailableError(reason string) *msgjson.Error {
	return msgjson.NewError(msgjson.ResultUnavailableError,
		"mesh command outcome unknown (%s); resend the request", reason)
}

// commandCoordinator coordinates the execution of commands and the delivery of
// results and errors to the correct destination.
type commandCoordinator struct {
	log        dex.Logger
	transport  commandTransport
	nodeID     string
	executors  map[string]CommandExecutor
	applyEvent func(context.Context, *Event, eventOrigin) (any, error)

	pendingMtx sync.Mutex
	pending    map[string]*pendingCommand
}

func newCommandCoordinator(
	log dex.Logger,
	transport commandTransport,
	nodeID string,
	executors map[string]CommandExecutor,
	applyEvent func(context.Context, *Event, eventOrigin) (any, error),
) *commandCoordinator {
	return &commandCoordinator{
		log:        log,
		transport:  transport,
		nodeID:     nodeID,
		executors:  executors,
		applyEvent: applyEvent,
		pending:    make(map[string]*pendingCommand),
	}
}

// execute executes a command. If the node is currently master, the command
// will be executed locally, otherwise it is forwarded to the master. If the
// node is currently not in the state where it can execute locally or forward,
// a TryAgainLaterError is returned.
func (c *commandCoordinator) execute(ctx context.Context, req CommandRequest) *msgjson.Error {
	if req.Msg == nil {
		return msgjson.NewError(msgjson.RPCInternalError, "nil command message")
	}
	if req.Respond == nil {
		return msgjson.NewError(msgjson.RPCInternalError, "nil command responder")
	}

	if c.transport.canExecuteCommandLocally() {
		return c.executeLocal(ctx, "", req)
	}
	if c.transport.canForwardCommand() {
		return c.forward(ctx, req)
	}
	return msgjson.NewError(msgjson.TryAgainLaterError,
		"mesh command %q waiting for established master or slave state", req.Kind)
}

// executeLocal executes a command locally.
func (c *commandCoordinator) executeLocal(ctx context.Context, originCommandID string, req CommandRequest) *msgjson.Error {
	exec := c.executors[req.Kind]
	if exec == nil {
		return msgjson.NewError(msgjson.RPCInternalError, "mesh command executor not found for kind %q", req.Kind)
	}

	completion := newCommandCompletion(originCommandID, req, c)
	return exec(&CommandContext{
		Context:    ctx,
		Request:    req,
		Completion: completion,
	})
}

// executeForwarded executes a command forwarded from the slave to the master.
func (c *commandCoordinator) executeForwarded(ctx context.Context, commandID string, req CommandRequest) *msgjson.Error {
	return c.executeLocal(ctx, commandID, req)
}

// forward forwards a command to the master. It registers a pending command
// and then sends the request to the master.
func (c *commandCoordinator) forward(ctx context.Context, req CommandRequest) *msgjson.Error {
	commandID := c.newCommandID()
	cmd := &commandForward{
		CommandID: commandID,
		Kind:      req.Kind,
		User:      req.User,
		Msg:       req.Msg,
	}
	c.registerPending(commandID, req)

	rpcErr, outcomeUnknown := c.transport.forwardCommand(ctx, cmd)
	if outcomeUnknown {
		c.log.Debugf("Command %s forward outcome unknown; holding the pending entry.", commandID)
		return nil
	}

	if rpcErr != nil {
		// failAllPending may have already answered; do not reply twice.
		if c.takePending(commandID) == nil {
			return nil
		}
		return rpcErr
	}

	c.markPendingAcked(commandID)

	return nil
}

// markPendingAcked marks that the master has acknowledged they have received
// the command.
func (c *commandCoordinator) markPendingAcked(commandID string) {
	c.pendingMtx.Lock()
	if pending := c.pending[commandID]; pending != nil {
		pending.acked = true
	}
	c.pendingMtx.Unlock()
}

func (c *commandCoordinator) newCommandID() string {
	return fmt.Sprintf("%s-%d", c.nodeID, nextRequestID())
}

// receiveForwardedFailure completes a forwarded command with the failure that
// the master sent.
func (c *commandCoordinator) receiveForwardedFailure(commandID string, msgErr *msgjson.Error) {
	if err := c.deliverPendingError(commandID, msgErr); err != nil {
		c.log.Debugf("failed to deliver command %s failure: %v", commandID, err)
	}
}

// receiveForwardedResult completes a forwarded command with the result that
// the master sent.
func (c *commandCoordinator) receiveForwardedResult(commandID string, result json.RawMessage) {
	if err := c.deliverPending(commandID, result); err != nil {
		c.log.Debugf("failed to deliver command %s result: %v", commandID, err)
	}
}

// registerPending is used by the slave to start tracking a command they
// forwarded to the master.
func (c *commandCoordinator) registerPending(commandID string, req CommandRequest) {
	reqID := req.Msg.ID
	c.pendingMtx.Lock()
	defer c.pendingMtx.Unlock()
	c.pending[commandID] = &pendingCommand{
		reqID:   reqID,
		respond: req.Respond,
		expire: time.AfterFunc(defaultPendingCommandTimeout, func() {
			c.expirePending(commandID)
		}),
	}
}

// expirePending removes a command that has timed out and replies with
// ResultUnavailableError.
func (c *commandCoordinator) expirePending(commandID string) {
	pending := c.takePending(commandID)
	if pending == nil {
		return
	}
	reason := "forwarded command expired unacknowledged"
	if pending.acked {
		reason = "forwarded command expired without a completion"
	}
	c.log.Warnf("Forwarded command %s expired after %v (%s); "+
		"answering the client with an unknown-outcome error.", commandID, defaultPendingCommandTimeout, reason)
	msgErr := resultUnavailableError(reason)
	if err := deliverCommandError(pending.reqID, pending.respond, msgErr); err != nil {
		c.log.Debugf("failed to deliver expiry error for command %s: %v", commandID, err)
	}
}

// failAllPending answers every pending forward with an unknown-outcome error.
func (c *commandCoordinator) failAllPending(reason string) {
	c.pendingMtx.Lock()
	pending := c.pending
	c.pending = make(map[string]*pendingCommand)
	c.pendingMtx.Unlock()

	if len(pending) == 0 {
		return
	}
	c.log.Warnf("Failing %d pending forwarded command(s) with an unknown-outcome error: %s.",
		len(pending), reason)
	msgErr := resultUnavailableError(reason)
	for commandID, p := range pending {
		p.expire.Stop()
		if err := deliverCommandError(p.reqID, p.respond, msgErr); err != nil {
			c.log.Debugf("failed to deliver failure for command %s: %v", commandID, err)
		}
	}
}

// pendingCount returns the number of pending commands.
func (c *commandCoordinator) pendingCount() int {
	c.pendingMtx.Lock()
	defer c.pendingMtx.Unlock()
	return len(c.pending)
}

// removePending is used by the slave to remove a command from the pending list.
func (c *commandCoordinator) removePending(commandID string) {
	c.pendingMtx.Lock()
	if pending := c.pending[commandID]; pending != nil {
		pending.expire.Stop()
		delete(c.pending, commandID)
	}
	c.pendingMtx.Unlock()
}

// takePending is used by the slave to remove a command from the pending list
// and return it.
func (c *commandCoordinator) takePending(commandID string) *pendingCommand {
	c.pendingMtx.Lock()
	defer c.pendingMtx.Unlock()
	pending := c.pending[commandID]
	if pending == nil {
		return nil
	}
	pending.expire.Stop()
	delete(c.pending, commandID)
	return pending
}

// deliverPending is used by the slave to deliver a command result to the client.
func (c *commandCoordinator) deliverPending(commandID string, result any) error {
	pending := c.takePending(commandID)
	if pending == nil {
		return nil
	}
	return deliverCommandResult(pending.reqID, pending.respond, result)
}

// deliverPendingError is used by the slave to deliver a command error to the client.
func (c *commandCoordinator) deliverPendingError(commandID string, msgErr *msgjson.Error) error {
	pending := c.takePending(commandID)
	if pending == nil {
		return nil
	}
	return deliverCommandError(pending.reqID, pending.respond, msgErr)
}

// CommandCompletion completes a command. A command can be completed
// by emitting an event, successfully completing without an event,
// or failing with an error before an event is emitted.
type CommandCompletion struct {
	originCommandID string
	reqID           uint64
	respond         func(*msgjson.Message) error
	commands        *commandCoordinator
}

func newCommandCompletion(
	originCommandID string,
	req CommandRequest,
	commands *commandCoordinator,
) *CommandCompletion {
	return &CommandCompletion{
		originCommandID: originCommandID,
		reqID:           req.Msg.ID,
		respond:         req.Respond,
		commands:        commands,
	}
}

// Emit applies and publishes event, then attempts to send a non-nil local
// result. Forwarded results travel with the event for delivery by the slave.
// If resultFn is nil, it uses the result set by the event applier.
func (c *CommandCompletion) Emit(ctx context.Context, event *Event, resultFn func() any) error {
	_, err := c.commands.applyEvent(ctx, event, c.eventOrigin(resultFn))
	return err
}

// eventOrigin describes this completion's command as an event origin: a
// forwarded command when a peer command ID is attached, a local command
// otherwise.
func (c *CommandCompletion) eventOrigin(resultFn func() any) eventOrigin {
	if c.originCommandID != "" {
		return forwardedCommandEventOrigin(c.originCommandID, resultFn)
	}
	return localCommandEventOrigin(c, resultFn)
}

// Complete completes a command successfully without emitting an event. Local
// commands send the result directly; forwarded commands send the result to the
// slave.
func (c *CommandCompletion) Complete(ctx context.Context, result any) error {
	if c.originCommandID == "" {
		return c.deliverResult(result)
	}

	b, err := json.Marshal(result)
	if err != nil {
		return err
	}
	return c.commands.transport.sendCommandResult(ctx, &commandResult{
		CommandID: c.originCommandID,
		Result:    b,
	})
}

// Fail completes a command without an event. Local commands send the error
// response directly; forwarded commands send a failure response to the slave.
func (c *CommandCompletion) Fail(ctx context.Context, msgErr *msgjson.Error) error {
	if msgErr == nil {
		panic("nil command error")
	}
	if c.originCommandID != "" {
		return c.commands.transport.sendCommandFailure(ctx, &commandFailure{
			CommandID: c.originCommandID,
			Error:     msgErr,
		})
	}
	return c.deliverError(msgErr)
}

func (c *CommandCompletion) deliverResult(result any) error {
	return deliverCommandResult(c.reqID, c.respond, result)
}

func (c *CommandCompletion) deliverError(msgErr *msgjson.Error) error {
	return deliverCommandError(c.reqID, c.respond, msgErr)
}

// CommandContext contains a command request and its completion.
type CommandContext struct {
	context.Context
	Request    CommandRequest
	Completion *CommandCompletion
}

// CommandExecutor validates the command, and completes it using
// CommandContext.Completion. If the command is invalid, an error
// is returned, and the command is not completed.
type CommandExecutor func(*CommandContext) *msgjson.Error

// errEncodeCommandResult tags a failure to build the response message itself,
// distinguishing a server-side encoding bug from a routine transport failure.
var errEncodeCommandResult = errors.New("encode command result")

// deliverCommandResult sends a typed command result to the locally connected
// client.
func deliverCommandResult(reqID uint64, respond func(*msgjson.Message) error, result any) error {
	resp, err := msgjson.NewResponse(reqID, result, nil)
	if err != nil {
		return fmt.Errorf("%w: %w", errEncodeCommandResult, err)
	}
	return respond(resp)
}

// deliverCommandError sends a command error to the client.
func deliverCommandError(reqID uint64, respond func(*msgjson.Message) error, msgErr *msgjson.Error) error {
	resp, err := msgjson.NewResponse(reqID, nil, msgErr)
	if err != nil {
		return err
	}
	_ = respond(resp)
	return nil
}
