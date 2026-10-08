// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package mesh

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"decred.org/dcrdex/dex/msgjson"
	"decred.org/dcrdex/server/account"
)

// singleServerTransport is the meshTransport when no peer is configured.
type singleServerTransport struct {
	// becameMaster starts the master workers.
	becameMaster func()
	// prep resolves with the master preparation outcome.
	prep *readiness
}

var _ meshTransport = (*singleServerTransport)(nil)

func newSingleServerTransport() *singleServerTransport {
	return &singleServerTransport{prep: newReadiness()}
}

// connect declares this node master immediately, as it can have no other
// master.
func (t *singleServerTransport) connect(ctx context.Context) (*sync.WaitGroup, error) {
	if t.becameMaster != nil {
		t.becameMaster()
	}
	if err := t.prep.wait(ctx); err != nil {
		return nil, err
	}
	return new(sync.WaitGroup), nil
}

func (t *singleServerTransport) notifyMasterReady() error {
	t.prep.resolve(nil)
	return nil
}

func (t *singleServerTransport) notifyMasterPreparationFailed(err error) error {
	if err == nil {
		err = fmt.Errorf("master preparation failed")
	}
	t.prep.resolve(err)
	return nil
}

func (t *singleServerTransport) ensureSeeded(context.Context) error {
	return nil
}

func (t *singleServerTransport) notifyReadyForEvents() {}

func (t *singleServerTransport) drainEventStream(context.Context) (bool, error) {
	return false, nil
}

func (t *singleServerTransport) requestMasterHandoff(context.Context) error {
	return nil
}

func (t *singleServerTransport) haltStatus() (bool, error) {
	return false, nil
}

func (t *singleServerTransport) checkEventPublishAvailable(forwardedCommand bool) error {
	if forwardedCommand {
		// Who forwarded?
		return fmt.Errorf("mesh event publisher unavailable for forwarded command")
	}
	return nil
}

func (t *singleServerTransport) notifyLocalEventCommitted(uint64, string, json.RawMessage) {}

func (t *singleServerTransport) postTerminalApplyFailureIfNeeded(error) {}

func (t *singleServerTransport) canExecuteCommandLocally() bool { return true }

func (t *singleServerTransport) canForwardCommand() bool { return false }

func (t *singleServerTransport) forwardCommand(context.Context, *commandForward) (*msgjson.Error, bool) {
	return msgjson.NewError(msgjson.RPCInternalError, "mesh transport unavailable for command forward"), false
}

func (t *singleServerTransport) sendCommandFailure(context.Context, *commandFailure) error {
	return fmt.Errorf("mesh transport unavailable for forwarded command failure")
}

func (t *singleServerTransport) sendCommandResult(context.Context, *commandResult) error {
	return fmt.Errorf("mesh transport unavailable for forwarded command result")
}

func (t *singleServerTransport) sendClientProxyMessage(_ context.Context, msg *ClientProxyMessage) error {
	if msg != nil && msg.Broadcast {
		return nil
	}
	return fmt.Errorf("%w in single-server mode", ErrClientProxyUnavailable)
}

func (t *singleServerTransport) queryClientConnected(context.Context, []account.AccountID) ([]account.AccountID, error) {
	return nil, nil
}
