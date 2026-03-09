// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package main

import (
	"context"
	"errors"
	"fmt"
	"time"

	dexeth "decred.org/dcrdex/dex/networks/eth"
	"decred.org/dcrdex/evmrelay"
	"github.com/ethereum/go-ethereum/common"
)

func (s *relayServer) pruneTasks() {
	pruned := s.store.prune(time.Now())
	if pruned > 0 {
		log.Infof("Pruned %d old tasks", pruned)
		if err := s.store.save(); err != nil {
			log.Errorf("Error saving pruned tasks: %v", err)
		}
	}

	s.limiter.prune(rateLimitPruneAge)
}

func (s *relayServer) processChainTasks(ctx context.Context, chainID int64) error {
	return s.processChainTasksWithTimeout(ctx, chainID, taskProcessTimeout)
}

func (s *relayServer) processChainTasksWithTimeout(ctx context.Context, chainID int64, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	if snapshot, ok := s.store.activeTask(chainID); ok {
		changed, err := s.reconcileActiveTask(ctx, &snapshot)
		if changed {
			return s.store.save()
		}
		return err
	}

	snapshot, ok := s.store.nextQueuedTask(chainID)
	if !ok {
		return nil
	}

	changed, err := s.promoteQueuedTask(ctx, &snapshot)
	if changed {
		return s.store.save()
	}
	return err
}

func (s *relayServer) promoteQueuedTask(ctx context.Context, snapshot *taskEntry) (bool, error) {
	cc, err := s.getChain(snapshot.ChainID)
	if err != nil {
		return false, err
	}

	now := time.Now()
	if !snapshot.ValidUntil.After(now) {
		return s.store.markTaskFailed(snapshot.TaskID, failureReasonExpired, nil), nil
	}

	calldata, parsed, err := validateRelayCalldata(common.Bytes2Hex(snapshot.Calldata), s.relayAddr, now)
	if err != nil {
		reason := failureReasonDropped
		if errors.Is(err, errDeadlineExpired) {
			reason = failureReasonExpired
		}
		return s.store.markTaskFailed(snapshot.TaskID, reason, nil), nil
	}

	baseFee, tipCap, err := cc.validateFee(ctx, calldata, parsed)
	if err != nil {
		if isPermanentPreflightError(err) {
			return s.store.markTaskFailed(snapshot.TaskID, failureReasonUnprofitable, nil), nil
		}
		return false, err
	}

	prepared, err := cc.prepareRedeemTx(ctx, snapshot.Target, calldata, parsed, baseFee, tipCap)
	if err != nil {
		if isPermanentPreflightError(err) {
			return s.store.markTaskFailed(snapshot.TaskID, failureReasonUnprofitable, nil), nil
		}
		return false, err
	}

	if !s.store.activateTask(snapshot.TaskID, prepared.nonce, prepared.hash, prepared.rawTx) {
		return false, nil
	}

	s.store.recordBroadcastAttempt(snapshot.TaskID)
	if err := cc.sendPreparedTx(ctx, prepared); err != nil {
		log.Warnf("Initial broadcast failed for task %s on chain %d: %v", snapshot.TaskID, snapshot.ChainID, err)
	} else {
		log.Infof("Chain %d: broadcast queued task %s as tx %s", snapshot.ChainID, snapshot.TaskID, prepared.hash.Hex())
	}

	return true, nil
}

func (s *relayServer) reconcileActiveTask(ctx context.Context, snapshot *taskEntry) (bool, error) {
	cc, err := s.getChain(snapshot.ChainID)
	if err != nil {
		return false, err
	}

	now := time.Now()

	// First check every redeem candidate hash. Multiple same-nonce redeem
	// transactions may exist after replacements, and any earlier candidate can
	// still win the race and become the canonical on-chain result.
	for _, txHash := range relayReceiptHashes(snapshot) {
		if receipt, err := cc.ec.TransactionReceipt(ctx, txHash); err == nil && receipt != nil {
			if receipt.Status == 1 {
				return s.store.markTaskSucceeded(snapshot.TaskID, txHash, now), nil
			}
			return s.store.markTaskFailed(snapshot.TaskID, failureReasonReverted, &now), nil
		} else if err != nil && !isTxNotFound(err) {
			return false, err
		}
	}

	// While the task is still active and within its validity window, try to
	// replace a stuck redeem with a higher-fee same-nonce redeem before moving
	// on to cancellation.
	if shouldAttemptRedeemReplacement(snapshot, now) {
		calldata, parsed, err := validateRelayCalldata(common.Bytes2Hex(snapshot.Calldata), s.relayAddr, now)
		if err != nil {
			return false, err
		}
		baseFee, tipCap, err := cc.validateFee(ctx, calldata, parsed)
		if err == nil {
			currentTx, decodeErr := preparedTxFromRaw(snapshot.RelayTxData)
			if decodeErr != nil {
				return false, decodeErr
			}
			prepared, prepErr := cc.prepareRedeemReplacementTx(ctx, snapshot.Target, calldata, parsed, baseFee, tipCap, *snapshot.RelayerNonce, currentTx.tx)
			if prepErr == nil {
				if !s.store.replaceActiveTx(snapshot.TaskID, prepared.hash, prepared.rawTx) {
					return false, nil
				}
				if err := cc.sendPreparedTx(ctx, prepared); err != nil {
					log.Warnf("Replacement broadcast failed for task %s on chain %d: %v", snapshot.TaskID, snapshot.ChainID, err)
				} else {
					log.Infof("Chain %d: replaced task %s with tx %s", snapshot.ChainID, snapshot.TaskID, prepared.hash.Hex())
				}
				return true, nil
			}
			if !isPermanentPreflightError(prepErr) {
				return false, prepErr
			}
		} else if !isPermanentPreflightError(err) {
			return false, err
		}
	}

	// Once cancellation has started, the cancel tx is just another same-nonce
	// candidate. Keep checking its receipts because either the cancel or an
	// earlier redeem can still be mined first.
	for _, txHash := range cancelReceiptHashes(snapshot) {
		if receipt, err := cc.ec.TransactionReceipt(ctx, txHash); err == nil && receipt != nil {
			return s.store.markTaskFailed(snapshot.TaskID, failureReasonCanceled, &now), nil
		} else if err != nil && !isTxNotFound(err) {
			return false, err
		}
	}

	// After the task expires, submit the first same-nonce cancel tx. The task
	// remains pending until either this cancel or one of the redeem candidates
	// is actually mined.
	if now.After(snapshot.ValidUntil) && snapshot.RelayerNonce != nil && snapshot.latestCancelTxHash() == (common.Hash{}) {
		currentTx, err := preparedTxFromRaw(snapshot.RelayTxData)
		if err != nil {
			return false, err
		}
		prepared, err := cc.prepareCancellationTx(ctx, *snapshot.RelayerNonce, currentTx.tx)
		if err != nil {
			return false, err
		}
		if !s.store.startCancellation(snapshot.TaskID, prepared.hash, prepared.rawTx) {
			return false, nil
		}
		if err := cc.sendPreparedTx(ctx, prepared); err != nil {
			log.Warnf("Cancel broadcast failed for task %s on chain %d: %v", snapshot.TaskID, snapshot.ChainID, err)
		}
		return true, nil
	}

	// If the cancel itself gets stuck, keep replacing it with a higher-fee
	// same-nonce cancel so the queue does not remain blocked behind an
	// underpriced cancellation.
	if shouldAttemptCancelReplacement(snapshot, now) {
		currentCancelTx, err := preparedTxFromRaw(snapshot.CancelTxData)
		if err != nil {
			return false, err
		}
		prepared, err := cc.prepareCancellationTx(ctx, *snapshot.RelayerNonce, currentCancelTx.tx)
		if err != nil {
			return false, err
		}
		if !s.store.replaceCancelTx(snapshot.TaskID, prepared.hash, prepared.rawTx) {
			return false, nil
		}
		if err := cc.sendPreparedTx(ctx, prepared); err != nil {
			log.Warnf("Cancellation replacement broadcast failed for task %s on chain %d: %v", snapshot.TaskID, snapshot.ChainID, err)
		} else {
			log.Infof("Chain %d: replaced cancellation for task %s with tx %s", snapshot.ChainID, snapshot.TaskID, prepared.hash.Hex())
		}
		return true, nil
	}

	if snapshot.LastSentAt != nil && time.Since(*snapshot.LastSentAt) < taskRebroadcastPeriod {
		return false, nil
	}

	// Nothing has resolved and no replacement/cancel transition is due yet, so
	// rebroadcast the current in-flight raw tx to keep it visible to the node.
	rawTx := snapshot.RelayTxData
	if snapshot.Phase == taskPhaseCanceling {
		rawTx = snapshot.CancelTxData
	}
	if len(rawTx) == 0 {
		return false, fmt.Errorf("task %s has no raw tx data for rebroadcast", snapshot.TaskID)
	}

	if !s.store.recordBroadcastAttempt(snapshot.TaskID) {
		return false, nil
	}

	prepared, err := preparedTxFromRaw(rawTx)
	if err != nil {
		return false, err
	}
	if err := cc.sendPreparedTx(ctx, prepared); err != nil {
		log.Debugf("Rebroadcast failed for task %s on chain %d: %v", snapshot.TaskID, snapshot.ChainID, err)
	}

	return true, nil
}

func shouldAttemptRedeemReplacement(snapshot *taskEntry, now time.Time) bool {
	if snapshot == nil || snapshot.State != evmrelay.RelayStatePending || snapshot.Phase != taskPhaseActive {
		return false
	}
	if snapshot.RelayerNonce == nil || snapshot.latestRelayTxHash() == (common.Hash{}) || len(snapshot.RelayTxData) == 0 {
		return false
	}
	if snapshot.latestCancelTxHash() != (common.Hash{}) || now.After(snapshot.ValidUntil) {
		return false
	}
	if relayReplacementAttempts(snapshot) >= taskMaxReplaceAttempts {
		return false
	}
	if snapshot.FirstSentAt == nil {
		return false
	}

	anchor := snapshot.FirstSentAt
	if snapshot.LastReplacedAt != nil {
		anchor = snapshot.LastReplacedAt
	}
	return now.Sub(*anchor) >= taskReplacePeriod
}

func shouldAttemptCancelReplacement(snapshot *taskEntry, now time.Time) bool {
	if snapshot == nil || snapshot.State != evmrelay.RelayStatePending || snapshot.Phase != taskPhaseCanceling {
		return false
	}
	if snapshot.RelayerNonce == nil || snapshot.latestCancelTxHash() == (common.Hash{}) || len(snapshot.CancelTxData) == 0 {
		return false
	}
	if cancelReplacementAttempts(snapshot) >= taskMaxReplaceAttempts || snapshot.LastReplacedAt == nil {
		return false
	}
	return now.Sub(*snapshot.LastReplacedAt) >= taskReplacePeriod
}

func relayReplacementAttempts(snapshot *taskEntry) int {
	if snapshot == nil || len(snapshot.RelayTxHashes) == 0 {
		return 0
	}
	return len(snapshot.RelayTxHashes) - 1
}

func cancelReplacementAttempts(snapshot *taskEntry) int {
	if snapshot == nil || len(snapshot.CancelTxHashes) == 0 {
		return 0
	}
	return len(snapshot.CancelTxHashes) - 1
}

func relayReceiptHashes(snapshot *taskEntry) []common.Hash {
	if snapshot == nil {
		return nil
	}
	return snapshot.RelayTxHashes
}

func cancelReceiptHashes(snapshot *taskEntry) []common.Hash {
	if snapshot == nil {
		return nil
	}
	return snapshot.CancelTxHashes
}

func isPermanentPreflightError(err error) bool {
	return errors.Is(err, errFeeTooLow) ||
		errors.Is(err, errFeeExceedsRedeemedValue) ||
		errors.Is(err, errGasEstimateTooHigh) ||
		errors.Is(err, errDeadlineExpired)
}

func newPendingTaskEntry(taskID string, chainID int64, target common.Address, parsed *dexeth.SignedRedemptionV1, calldata []byte) (*taskEntry, error) {
	now := time.Now()
	validUntil, err := taskValidUntil(parsed, now)
	if err != nil {
		return nil, err
	}

	return &taskEntry{
		TaskID:      taskID,
		ChainID:     chainID,
		ValidUntil:  validUntil,
		State:       evmrelay.RelayStatePending,
		Phase:       taskPhaseQueued,
		Participant: parsed.Participant,
		Nonce:       parsed.Nonce.Uint64(),
		Target:      target,
		Calldata:    append([]byte(nil), calldata...),
	}, nil
}

func taskValidUntil(parsed *dexeth.SignedRedemptionV1, now time.Time) (time.Time, error) {
	validUntil := now.Add(taskMaxPendingLifetime)
	if deadline := time.Unix(parsed.Deadline.Int64(), 0).Add(-taskDeadlineMargin); deadline.Before(validUntil) {
		validUntil = deadline
	}
	if !validUntil.After(now) {
		return time.Time{}, &deadlineTooSoonError{
			Deadline:   time.Unix(parsed.Deadline.Int64(), 0),
			ValidUntil: validUntil,
			Now:        now,
		}
	}
	return validUntil, nil
}
