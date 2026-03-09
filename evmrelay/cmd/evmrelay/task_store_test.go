// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package main

import (
	"testing"
	"time"

	"decred.org/dcrdex/evmrelay"
	"github.com/ethereum/go-ethereum/common"
)

func tQueuedTask(taskID string, chainID int64, participant common.Address, nonce uint64) *taskEntry {
	return &taskEntry{
		TaskID:      taskID,
		ChainID:     chainID,
		ValidUntil:  time.Now().Add(time.Hour),
		State:       evmrelay.RelayStatePending,
		Phase:       taskPhaseQueued,
		Participant: participant,
		Nonce:       nonce,
		Calldata:    []byte(taskID),
	}
}

func TestTaskStoreNextQueuedTaskFIFO(t *testing.T) {
	store := newTaskStore(t.TempDir())
	participant := common.HexToAddress("0xabcdef0123456789abcdef0123456789abcdef01")

	first := tQueuedTask("task-1", 1, participant, 1)
	second := tQueuedTask("task-2", 1, participant, 2)
	third := tQueuedTask("task-3", 1, participant, 3)
	store.createQueuedTask(first)
	store.createQueuedTask(second)
	store.createQueuedTask(third)

	snapshot, ok := store.nextQueuedTask(1)
	if !ok {
		t.Fatal("missing first queued task")
	}
	if snapshot.TaskID != "task-1" {
		t.Fatalf("first queued task: got %q, want %q", snapshot.TaskID, "task-1")
	}
	if snapshot.QueueSeq >= second.QueueSeq || second.QueueSeq >= third.QueueSeq {
		t.Fatalf("queue seq should increase strictly: got %d, %d, %d", snapshot.QueueSeq, second.QueueSeq, third.QueueSeq)
	}

	if !store.activateTask("task-1", 7, common.HexToHash("0x1"), []byte{1}) {
		t.Fatal("activateTask returned false")
	}

	snapshot, ok = store.nextQueuedTask(1)
	if !ok {
		t.Fatal("missing second queued task after first activated")
	}
	if snapshot.TaskID != "task-2" {
		t.Fatalf("next queued task after activation: got %q, want %q", snapshot.TaskID, "task-2")
	}
}

func TestTaskStoreQueueViewsByChainAndPhase(t *testing.T) {
	store := newTaskStore(t.TempDir())
	p1 := common.HexToAddress("0xabcdef0123456789abcdef0123456789abcdef01")
	p2 := common.HexToAddress("0xabcdef0123456789abcdef0123456789abcdef02")
	p3 := common.HexToAddress("0xabcdef0123456789abcdef0123456789abcdef03")
	p4 := common.HexToAddress("0xabcdef0123456789abcdef0123456789abcdef04")

	queuedChain1 := tQueuedTask("queued-chain-1", 1, p1, 1)
	queuedChain2 := tQueuedTask("queued-chain-2", 2, p2, 1)
	activeChain1 := &taskEntry{
		TaskID:       "active-chain-1",
		ChainID:      1,
		ValidUntil:   time.Now().Add(time.Hour),
		State:        evmrelay.RelayStatePending,
		Phase:        taskPhaseActive,
		Participant:  p3,
		Nonce:        1,
		RelayerNonce: ptrUint64(9),
		RelayTxHashes: []common.Hash{
			common.HexToHash("0xaaaa"),
		},
		RelayTxData: []byte{1},
	}
	cancelingChain2 := &taskEntry{
		TaskID:       "canceling-chain-2",
		ChainID:      2,
		ValidUntil:   time.Now().Add(time.Hour),
		State:        evmrelay.RelayStatePending,
		Phase:        taskPhaseCanceling,
		Participant:  p4,
		Nonce:        1,
		RelayerNonce: ptrUint64(10),
		CancelTxHashes: []common.Hash{
			common.HexToHash("0xbbbb"),
		},
		CancelTxData: []byte{2},
	}

	store.createQueuedTask(queuedChain1)
	store.createQueuedTask(queuedChain2)
	testPutTask(t, store, activeChain1)
	testPutTask(t, store, cancelingChain2)

	if snapshot, ok := store.nextQueuedTask(1); !ok || snapshot.TaskID != queuedChain1.TaskID {
		t.Fatalf("chain 1 next queued task = %q, want %q", snapshot.TaskID, queuedChain1.TaskID)
	}
	if snapshot, ok := store.nextQueuedTask(2); !ok || snapshot.TaskID != queuedChain2.TaskID {
		t.Fatalf("chain 2 next queued task = %q, want %q", snapshot.TaskID, queuedChain2.TaskID)
	}
	if snapshot, ok := store.activeTask(1); !ok || snapshot.TaskID != activeChain1.TaskID {
		t.Fatalf("chain 1 active task = %q, want %q", snapshot.TaskID, activeChain1.TaskID)
	}
	if snapshot, ok := store.activeTask(2); !ok || snapshot.TaskID != cancelingChain2.TaskID {
		t.Fatalf("chain 2 active task = %q, want %q", snapshot.TaskID, cancelingChain2.TaskID)
	}
}

func TestTaskStorePendingReservations(t *testing.T) {
	store := newTaskStore(t.TempDir())
	participant := common.HexToAddress("0xabcdef0123456789abcdef0123456789abcdef01")
	pk := pendingKey{participant: participant, nonce: 1}

	if got := store.reservePendingSlot(pk, []byte("a")); got.existingTaskID != "" || got.conflict {
		t.Fatalf("unexpected reservation result for new slot: %+v", got)
	}
	if !testHasPendingReservation(t, store, pk) {
		t.Fatal("expected reserved slot to exist")
	}

	store.releasePendingReservation(pk)
	if testHasPendingReservation(t, store, pk) {
		t.Fatal("expected released reservation to be removed")
	}

	entry := tQueuedTask("task-1", 1, participant, 1)
	store.createQueuedTask(entry)

	if got := store.reservePendingSlot(pk, []byte("task-1")); got.existingTaskID != "task-1" || got.conflict {
		t.Fatalf("unexpected reservation result for identical calldata: %+v", got)
	}
	if got := store.reservePendingSlot(pk, []byte("different")); !got.conflict {
		t.Fatalf("expected conflict for different calldata, got %+v", got)
	}

	store.deleteTask("task-1")
	if testHasPendingReservation(t, store, pk) {
		t.Fatal("expected task deletion to clear reservation")
	}
}

func TestTaskStoreTerminalTransitionsReleaseReservations(t *testing.T) {
	t.Run("markTaskFailed", func(t *testing.T) {
		store := newTaskStore(t.TempDir())
		participant := common.HexToAddress("0xabcdef0123456789abcdef0123456789abcdef01")
		entry := tQueuedTask("task-failed", 1, participant, 1)
		store.createQueuedTask(entry)

		if !store.markTaskFailed("task-failed", failureReasonUnprofitable, nil) {
			t.Fatal("markTaskFailed returned false")
		}

		snapshot, ok := store.snapshot("task-failed")
		if !ok {
			t.Fatal("missing failed task")
		}
		if snapshot.State != evmrelay.RelayStateFailed {
			t.Fatalf("state: got %s, want %s", snapshot.State, evmrelay.RelayStateFailed)
		}
		if snapshot.ResolvedAt == nil {
			t.Fatal("expected ResolvedAt to be set")
		}
		if testHasPendingReservation(t, store, entry.pendingKey()) {
			t.Fatal("expected failed task reservation to be cleared")
		}
	})

	t.Run("markTaskSucceeded", func(t *testing.T) {
		store := newTaskStore(t.TempDir())
		participant := common.HexToAddress("0xabcdef0123456789abcdef0123456789abcdef02")
		entry := tQueuedTask("task-succeeded", 1, participant, 2)
		store.createQueuedTask(entry)

		txHash := common.HexToHash("0x1234")
		resolvedAt := time.Now()
		if !store.markTaskSucceeded("task-succeeded", txHash, resolvedAt) {
			t.Fatal("markTaskSucceeded returned false")
		}

		snapshot, ok := store.snapshot("task-succeeded")
		if !ok {
			t.Fatal("missing succeeded task")
		}
		if snapshot.State != evmrelay.RelayStateSuccess {
			t.Fatalf("state: got %s, want %s", snapshot.State, evmrelay.RelayStateSuccess)
		}
		if snapshot.SuccessTxHash != txHash {
			t.Fatalf("success tx hash: got %s, want %s", snapshot.SuccessTxHash.Hex(), txHash.Hex())
		}
		if snapshot.ResolvedAt == nil || !snapshot.ResolvedAt.Equal(resolvedAt) {
			t.Fatalf("resolvedAt: got %v, want %v", snapshot.ResolvedAt, resolvedAt)
		}
		if testHasPendingReservation(t, store, entry.pendingKey()) {
			t.Fatal("expected succeeded task reservation to be cleared")
		}
	})
}

func TestTaskStorePrune(t *testing.T) {
	store := newTaskStore(t.TempDir())
	participant := common.HexToAddress("0xabcdef0123456789abcdef0123456789abcdef01")
	now := time.Now()

	testPutTask(t, store, &taskEntry{
		TaskID:      "fresh-pending",
		ChainID:     1,
		State:       evmrelay.RelayStatePending,
		Participant: participant,
		Nonce:       1,
	})
	testPutPendingReservation(t, store, pendingKey{participant: participant, nonce: 1}, "fresh-pending")

	testPutTask(t, store, &taskEntry{
		TaskID:      "stale-pending",
		ChainID:     1,
		State:       evmrelay.RelayStatePending,
		Participant: participant,
		Nonce:       2,
	})
	testPutPendingReservation(t, store, pendingKey{participant: participant, nonce: 2}, "stale-pending")

	staleTime := now.Add(-72 * time.Hour)
	testPutTask(t, store, &taskEntry{
		TaskID:      "stale-success",
		ChainID:     1,
		ResolvedAt:  &staleTime,
		State:       evmrelay.RelayStateSuccess,
		Participant: participant,
		Nonce:       3,
	})
	testPutTask(t, store, &taskEntry{
		TaskID:        "stale-failed",
		ChainID:       1,
		ResolvedAt:    &staleTime,
		State:         evmrelay.RelayStateFailed,
		FailureReason: failureReasonUnprofitable,
		Participant:   participant,
		Nonce:         4,
	})

	pruned := store.prune(now)
	if pruned != 2 {
		t.Fatalf("pruned: got %d, want 2", pruned)
	}
	if _, ok := store.snapshot("fresh-pending"); !ok {
		t.Fatal("fresh pending task should remain after prune")
	}
	if _, ok := store.snapshot("stale-pending"); !ok {
		t.Fatal("stale pending task should remain after prune")
	}
	if _, ok := store.snapshot("stale-success"); ok {
		t.Fatal("stale success task should be pruned")
	}
	if _, ok := store.snapshot("stale-failed"); ok {
		t.Fatal("stale failed task should be pruned")
	}
	if !testHasPendingReservation(t, store, pendingKey{participant: participant, nonce: 1}) {
		t.Fatal("fresh pending reservation should remain after prune")
	}
	if !testHasPendingReservation(t, store, pendingKey{participant: participant, nonce: 2}) {
		t.Fatal("stale pending reservation should remain after prune")
	}
}

func TestTaskStoreReplaceActiveTxTracksHashes(t *testing.T) {
	store := newTaskStore(t.TempDir())
	nonce := uint64(9)
	entry := &taskEntry{
		TaskID:       "task-1",
		State:        evmrelay.RelayStatePending,
		Phase:        taskPhaseActive,
		Participant:  common.HexToAddress("0xabcdef0123456789abcdef0123456789abcdef01"),
		Nonce:        1,
		RelayerNonce: &nonce,
		RelayTxHashes: []common.Hash{
			common.HexToHash("0xaaaa"),
		},
		RelayTxData: []byte{1, 2, 3},
	}
	testPutTask(t, store, entry)

	newHash := common.HexToHash("0xbbbb")
	if !store.replaceActiveTx("task-1", newHash, []byte{4, 5, 6}) {
		t.Fatal("replaceActiveTx returned false")
	}

	snapshot, ok := store.snapshot("task-1")
	if !ok {
		t.Fatal("task missing")
	}
	if got := snapshot.latestRelayTxHash(); got != newHash {
		t.Fatalf("relay tx hash: got %s, want %s", got.Hex(), newHash.Hex())
	}
	if len(snapshot.RelayTxHashes) != 2 {
		t.Fatalf("relay tx hashes len: got %d, want 2", len(snapshot.RelayTxHashes))
	}
	if snapshot.LastReplacedAt == nil {
		t.Fatal("expected LastReplacedAt to be set")
	}
}

func TestTaskStoreLoadRebuildsQueueOrderAndReservations(t *testing.T) {
	dataDir := t.TempDir()
	now := time.Now()
	p1 := common.HexToAddress("0xabcdef0123456789abcdef0123456789abcdef01")
	p2 := common.HexToAddress("0xabcdef0123456789abcdef0123456789abcdef02")
	p3 := common.HexToAddress("0xabcdef0123456789abcdef0123456789abcdef03")

	s1 := newTaskStore(dataDir)

	active := tQueuedTask("active", 1, p1, 1)
	queued1 := tQueuedTask("queued-1", 1, p2, 1)
	queued2 := tQueuedTask("queued-2", 2, p3, 1)
	storeOld := &taskEntry{
		TaskID:      "old-success",
		ChainID:     1,
		QueueSeq:    99,
		ResolvedAt:  ptrTime(now.Add(-72 * time.Hour)),
		State:       evmrelay.RelayStateSuccess,
		Participant: common.HexToAddress("0xabcdef0123456789abcdef0123456789abcdef04"),
		Nonce:       1,
	}

	s1.createQueuedTask(active)
	s1.createQueuedTask(queued1)
	s1.createQueuedTask(queued2)
	if !s1.activateTask("active", 11, common.HexToHash("0xaaaa"), []byte{1}) {
		t.Fatal("activateTask returned false")
	}
	testPutTask(t, s1, storeOld)

	if err := s1.save(); err != nil {
		t.Fatalf("save error: %v", err)
	}

	s2 := newTaskStore(dataDir)
	if err := s2.load(); err != nil {
		t.Fatalf("load error: %v", err)
	}

	if snapshot, ok := s2.activeTask(1); !ok || snapshot.TaskID != "active" {
		t.Fatalf("active task after load = %q, want %q", snapshot.TaskID, "active")
	}
	if snapshot, ok := s2.nextQueuedTask(1); !ok || snapshot.TaskID != "queued-1" {
		t.Fatalf("chain 1 next queued task after load = %q, want %q", snapshot.TaskID, "queued-1")
	}
	if snapshot, ok := s2.nextQueuedTask(2); !ok || snapshot.TaskID != "queued-2" {
		t.Fatalf("chain 2 next queued task after load = %q, want %q", snapshot.TaskID, "queued-2")
	}
	if _, ok := s2.snapshot("old-success"); ok {
		t.Fatal("expected old resolved task to be pruned on load")
	}

	if !testHasPendingReservation(t, s2, active.pendingKey()) {
		t.Fatal("expected active task reservation to be rebuilt")
	}
	if !testHasPendingReservation(t, s2, queued1.pendingKey()) {
		t.Fatal("expected queued chain 1 reservation to be rebuilt")
	}
	if !testHasPendingReservation(t, s2, queued2.pendingKey()) {
		t.Fatal("expected queued chain 2 reservation to be rebuilt")
	}
}

func ptrUint64(v uint64) *uint64 {
	return &v
}
