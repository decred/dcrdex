// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package mesh

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/msgjson"
	"decred.org/dcrdex/server/db"
)

const (
	// subscribeTimeout is the slave's idle timeout. If the slave does not have
	// an active stream or snapshot for this timeout, it is disconnected.
	subscribeTimeout = 15 * time.Minute

	// seedTimeout limits the wait for seeding a new node.
	seedTimeout = 10 * time.Minute

	// seedChunkStallTimeout is the maximum time the slave waits between
	// snapshot chunks before seeding is considered stalled.
	seedChunkStallTimeout = 90 * time.Second
)

// errSeedConnectionClosed identifies a seed wait stopped by connection loss.
var errSeedConnectionClosed = errors.New("connection closed")

// runSubscriber starts the subscriber loop on the slave. It waits
// for the node to be able to receive events, then requests the
// master to start streaming events.
func (n *node) runSubscriber(conn *nodeConn) {
	select {
	case <-n.eventsGate.resolved():
	case <-conn.link.Done():
		return
	}

	for {
		state := n.control.currentState()
		if !sameConn(state.activeConn, conn) || !state.mode.canReceiveEventStream() {
			return
		}
		if n.trySubscribe(conn) {
			return
		}
		if sleepSubscribeRetry(n.runContext, conn) != nil {
			return
		}
	}
}

// trySubscribe requests the master to start streaming events.
// It returns true if the request was accepted.
func (n *node) trySubscribe(conn *nodeConn) (done bool) {
	frontier, err := n.eventLogReader.EventLogFrontier(n.runContext)
	if err != nil {
		if n.runContext.Err() == nil {
			n.log.Errorf("trySubscribe: EventLogFrontier: %v", err)
		}
		return false
	}

	var result streamSubscribeResult
	err = conn.link.Request(n.runContext, streamSubscribeRoute, &streamSubscribe{
		Frontier: toFrontierMessage(frontier),
	}, &result)
	if err == nil {
		n.log.Infof("Mesh event subscribe accepted by peer %s from frontier %s (peer tip %d).",
			meshPeerLogID(conn), frontier, result.MasterTip)

		// Check if we are already caught up.
		if result.MasterTip == frontier.Seq {
			if postErr := n.control.post(streamCaughtUpSignal{conn: conn, target: frontier}); postErr != nil {
				n.log.Debugf("failed to post subscribe caught-up: %v", postErr)
			}
		}
		return true
	}

	if subscribeRejectionPermanent(err) {
		n.log.Warnf("Mesh event subscribe permanently rejected by peer %s: %v", meshPeerLogID(conn), err)
		n.postSubscribeRejected(conn, fmt.Errorf("stream subscribe rejected: %w", err))
		return true
	}

	// Retryable rejection or transport failure.
	return false
}

// subscribeRejectionPermanent reports whether err is a permanent stream
// refusal.
func subscribeRejectionPermanent(err error) bool {
	var peerErr *peerRPCError
	return errors.As(err, &peerErr) && peerErr.Code == msgjson.SubscribeRejectedError
}

// postSubscribeRejected reports a permanent stream refusal to the state
// machine.
func (n *node) postSubscribeRejected(conn *nodeConn, err error) {
	_ = n.control.post(subscribeRejectedSignal{conn: conn, err: err, at: time.Now()})
}

// sleepSubscribeRetry waits for the retry delay.
// It returns an error if ctx ends or the connection closes first.
func sleepSubscribeRetry(ctx context.Context, conn *nodeConn) error {
	const subscribeRetryDelay = time.Second
	timer := time.NewTimer(subscribeRetryDelay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-conn.link.Done():
		return errSeedConnectionClosed
	case <-ctx.Done():
		return ctx.Err()
	}
}

// seedInProgress reports whether initial database seeding is pending or
// running.
func (n *node) seedInProgress() bool {
	return n.seeding.Load()
}

// ensureSeeded loads a snapshot if required for startup.
// It returns immediately when no seeding is pending.
func (n *node) ensureSeeded(ctx context.Context) error {
	if !n.seeding.Load() {
		return nil
	}
	defer n.seeding.Store(false)

	ctx, cancel := context.WithTimeout(ctx, seedTimeout)
	defer cancel()

	const seedResolutionPollInterval = 250 * time.Millisecond
	poll := time.NewTicker(seedResolutionPollInterval)
	defer poll.Stop()

	for {
		// Check if we are already seeded.
		if frontier, err := n.eventLogReader.EventLogFrontier(ctx); err == nil && frontier.Seq > 0 {
			n.log.Infof("Mesh join: database already seeded at frontier %s.", frontier)
			return nil
		}

		state := n.control.currentState()
		switch {
		case state.mode.isAuthoritativeMaster():
			n.log.Infof("Mesh join: resolved as master; nothing to seed.")
			return nil
		case state.mode == modeEstablishedSlave:
			// Equal progress, and our log is empty, so the peer's is too.
			n.log.Infof("Mesh join: peer has no history; nothing to seed (mesh genesis).")
			return nil
		case state.mode == modeEstablishedSlaveSyncing && state.activeConn != nil:
			if err := n.seedFromConn(ctx, state.activeConn); err != nil {
				if ctx.Err() != nil {
					return seedDeadlineError(ctx)
				}
				if errors.Is(err, errSeedConnectionClosed) {
					n.log.Debugf("Mesh seed attempt ended after connection loss: %v", err)
				} else {
					n.log.Warnf("Mesh seed attempt failed: %v", err)
				}
				break
			}
			return nil
		}

		select {
		case <-poll.C:
		case <-ctx.Done():
			return seedDeadlineError(ctx)
		}
	}
}

// seedFromConn requests a snapshot and waits for it to be received and
// loaded into the database.
func (n *node) seedFromConn(ctx context.Context, conn *nodeConn) error {
	seed := newSeedAttempt(n.runContext, n.snapshotStore, n.log)
	conn.seed.Store(seed)
	defer conn.seed.Store(nil)

	if err := n.requestSnapshot(ctx, conn); err != nil {
		return err
	}
	n.log.Infof("Mesh snapshot request accepted; awaiting snapshot from peer %s.", meshPeerLogID(conn))

	return awaitSeedOutcome(ctx, conn, seed)
}

// requestSnapshot sends a snapshot request to the master.
// It keeps retrying until the request is accepted, the context is cancelled,
// or the connection is lost.
func (n *node) requestSnapshot(ctx context.Context, conn *nodeConn) error {
	for {
		err := conn.link.Request(ctx, snapshotRequestRoute, &snapshotRequest{}, nil)
		if err == nil {
			return nil
		}
		if err := sleepSubscribeRetry(ctx, conn); err != nil {
			return fmt.Errorf("snapshot request not accepted: %w", err)
		}
	}
}

// awaitSeedOutcome waits for the snapshot transfer and database load
// to complete. It ends early if the transfer has had no updates for
// seedChunkStallTimeout.
func awaitSeedOutcome(ctx context.Context, conn *nodeConn, seed *seedAttempt) error {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	// Wait for the transfer to complete.
	for !seed.rx.transferComplete() {
		select {
		case <-seed.done: // a receive error can fail the seed early
			return seed.outcome()
		case <-ctx.Done():
			return ctx.Err()
		case <-conn.link.Done():
			if !seed.rx.transferComplete() {
				return fmt.Errorf("%w during snapshot transfer", errSeedConnectionClosed)
			}
			// The final chunk landed as the conn died.
		case <-ticker.C:
			if seed.rx.transferComplete() {
				continue
			}
			if last := seed.rx.lastProgress(); !last.IsZero() && time.Since(last) > seedChunkStallTimeout {
				conn.link.Disconnect()
				return fmt.Errorf("snapshot transfer stalled: no chunk for %v", seedChunkStallTimeout)
			}
		}
	}

	// Wait for the database load to complete.
	select {
	case <-seed.done:
		return seed.outcome()
	case <-ctx.Done():
		return ctx.Err()
	}
}

// seedDeadlineError describes an expired seed deadline or interrupted seed
// wait.
func seedDeadlineError(ctx context.Context) error {
	err := context.Cause(ctx)
	if errors.Is(err, context.DeadlineExceeded) {
		return errors.New("snapshot seed from mesh peer did not complete within the deadline")
	}
	return fmt.Errorf("snapshot seed interrupted: %w", err)
}

// seedAttempt holds one attempt to seed the database from a peer's snapshot.
type seedAttempt struct {
	rx    *snapshotReceiver
	ctx   context.Context
	store db.SnapshotStore
	log   dex.Logger

	mtx      sync.Mutex
	finished bool
	err      error
	done     chan struct{}
}

func newSeedAttempt(ctx context.Context, store db.SnapshotStore, log dex.Logger) *seedAttempt {
	return &seedAttempt{
		rx:    &snapshotReceiver{},
		ctx:   ctx,
		store: store,
		log:   log,
		done:  make(chan struct{}),
	}
}

// runLoad loads the received snapshot into the database and records the
// outcome.
func (a *seedAttempt) runLoad() {
	frontier, err := a.rx.load(a.ctx, a.store)
	if err != nil {
		a.fail(err)
		return
	}
	a.log.Infof("Loaded snapshot from mesh peer; database seeded at frontier %s.", frontier)
	a.succeed()
}

// fail records the first failure unless the attempt has already finished.
func (a *seedAttempt) fail(err error) {
	a.mtx.Lock()
	defer a.mtx.Unlock()
	if a.finished {
		return
	}
	a.finished = true
	a.err = err
	close(a.done)
}

// succeed records success unless the attempt has already finished.
func (a *seedAttempt) succeed() {
	a.mtx.Lock()
	defer a.mtx.Unlock()
	if a.finished {
		return
	}
	a.finished = true
	close(a.done)
}

// outcome returns the recorded error. Wait for done before reading the final
// outcome.
func (a *seedAttempt) outcome() error {
	a.mtx.Lock()
	defer a.mtx.Unlock()
	return a.err
}

// watchSubscribeTimeout periodically checks that the slave has either
// an active stream or snapshot. If the slave is idle for too long,
// it is disconnected.
func (n *node) watchSubscribeTimeout(peerConn *nodeConn) {
	ticker := time.NewTicker(subscribeTimeout / 10)
	defer ticker.Stop()

	var silentSince time.Time
	for {
		select {
		case <-peerConn.link.Done():
			return
		case <-ticker.C:
		}

		state := n.control.currentState()
		if !sameConn(state.activeConn, peerConn) {
			return
		}

		silent := state.mode.isAuthoritativeMaster() &&
			!n.stream.isStreamingTo(peerConn.ID()) &&
			!n.snapshots.sendingTo(peerConn.ID())
		if !silent {
			silentSince = time.Time{}
			continue
		}
		if silentSince.IsZero() {
			silentSince = time.Now()
		}
		if time.Since(silentSince) < subscribeTimeout {
			continue
		}
		n.log.Debugf("Mesh peer %s adopted but not streaming for %v. Disconnecting so it can connect and handshake again.",
			meshPeerLogID(peerConn), subscribeTimeout)
		peerConn.link.Disconnect()
		return
	}
}
