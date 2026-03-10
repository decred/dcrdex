// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"decred.org/dcrdex/evmrelay"
	"github.com/ethereum/go-ethereum/common"
)

type taskPhase string

const (
	taskPhaseQueued    taskPhase = "queued"
	taskPhaseActive    taskPhase = "active"
	taskPhaseCanceling taskPhase = "canceling"

	failureReasonReverted     = "reverted"
	failureReasonDropped      = "dropped"
	failureReasonExpired      = "expired"
	failureReasonCanceled     = "canceled"
	failureReasonUnprofitable = "unprofitable"
)

// taskEntry is the durable record for one relay submission.
//
// Field groups:
//   - identity and queue ordering: TaskID, ChainID, QueueSeq
//   - public task result: State, FailureReason, ResolvedAt
//   - relay execution state: Phase, ValidUntil, Participant, Nonce, Target,
//     Calldata, RelayerNonce
//   - tx tracking: SuccessTxHash, RelayTxHashes, RelayTxData,
//     CancelTxHashes, CancelTxData, FirstSentAt, LastSentAt, LastReplacedAt
//
// A task starts queued, may later become active or canceling, and finally ends
// as success or failed.
type taskEntry struct {
	// Identity and queue ordering.
	TaskID   string `json:"taskID"`
	ChainID  int64  `json:"chainID"`
	QueueSeq uint64 `json:"queueSeq"`

	// Public task result.
	ValidUntil    time.Time           `json:"validUntil"`
	ResolvedAt    *time.Time          `json:"resolvedAt,omitempty"`
	State         evmrelay.RelayState `json:"state"`
	FailureReason string              `json:"failureReason,omitempty"`

	// Relay execution state.
	Phase        taskPhase      `json:"phase"`
	Participant  common.Address `json:"participant"`
	Nonce        uint64         `json:"nonce"`
	Target       common.Address `json:"target"`
	Calldata     []byte         `json:"calldata"`
	RelayerNonce *uint64        `json:"relayerNonce,omitempty"`

	// Transaction tracking.
	SuccessTxHash  common.Hash   `json:"successTxHash"`
	RelayTxHashes  []common.Hash `json:"relayTxHashes,omitempty"`
	RelayTxData    []byte        `json:"relayTxData,omitempty"`
	CancelTxHashes []common.Hash `json:"cancelTxHashes,omitempty"`
	CancelTxData   []byte        `json:"cancelTxData,omitempty"`
	FirstSentAt    *time.Time    `json:"firstSentAt,omitempty"`
	LastSentAt     *time.Time    `json:"lastSentAt,omitempty"`
	LastReplacedAt *time.Time    `json:"lastReplacedAt,omitempty"`
}

func (e *taskEntry) pendingKey() pendingKey {
	return pendingKey{participant: e.Participant, nonce: e.Nonce}
}

func (e *taskEntry) latestRelayTxHash() common.Hash {
	if e == nil || len(e.RelayTxHashes) == 0 {
		return common.Hash{}
	}
	return e.RelayTxHashes[len(e.RelayTxHashes)-1]
}

func (e *taskEntry) latestCancelTxHash() common.Hash {
	if e == nil || len(e.CancelTxHashes) == 0 {
		return common.Hash{}
	}
	return e.CancelTxHashes[len(e.CancelTxHashes)-1]
}

func (e *taskEntry) statusTxHash() common.Hash {
	if e == nil || e.State != evmrelay.RelayStateSuccess {
		return common.Hash{}
	}
	return e.SuccessTxHash
}

// taskStore owns durable task state and the in-memory participant+nonce index.
//
// Invariants:
//   - nextQueueSeq only increases
//   - every pending task has a reservation in pending keyed by participant+nonce
//   - queuedByChain is the in-memory FIFO index for queued tasks only
//   - activeByChain tracks the current active or canceling task for each chain
//   - terminal tasks have no reservation in pending
//   - queued tasks may have no relayer nonce yet; active/canceling tasks must
//     already own one
type taskStore struct {
	mtx sync.RWMutex
	// saveMtx serializes writes to tasks.json after a snapshot is taken under mtx.
	saveMtx sync.Mutex
	// tasks is the durable source of truth keyed by task ID.
	tasks map[string]*taskEntry
	// pending indexes in-flight tasks by participant and contract nonce.
	pending map[pendingKey]pendingReservation
	// queuedByChain is the in-memory FIFO queue of queued task IDs for each chain.
	queuedByChain map[int64][]string
	// activeByChain points to the one nonce-owning task per chain, if any.
	activeByChain map[int64]string
	// dataDir is where tasks.json is stored.
	dataDir string
	// nextQueueSeq assigns durable FIFO order to newly queued tasks.
	nextQueueSeq uint64
}

func newTaskStore(dataDir string) *taskStore {
	return &taskStore{
		tasks:         make(map[string]*taskEntry),
		pending:       make(map[pendingKey]pendingReservation),
		queuedByChain: make(map[int64][]string),
		activeByChain: make(map[int64]string),
		dataDir:       dataDir,
	}
}

func (ts *taskStore) tasksFilePath() string {
	return filepath.Join(ts.dataDir, "tasks.json")
}

// persistence

type persistedTasks struct {
	NextQueueSeq uint64       `json:"nextQueueSeq"`
	Tasks        []*taskEntry `json:"tasks"`
}

// load replaces the in-memory task set with the saved snapshot on disk.
func (ts *taskStore) load() error {
	data, err := os.ReadFile(ts.tasksFilePath())
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("error reading tasks file: %w", err)
	}

	var persisted persistedTasks
	if err := json.Unmarshal(data, &persisted); err != nil {
		return fmt.Errorf("error parsing tasks file: %w", err)
	}

	now := time.Now()
	loaded := 0

	ts.mtx.Lock()
	defer ts.mtx.Unlock()

	ts.tasks = make(map[string]*taskEntry)
	ts.pending = make(map[pendingKey]pendingReservation)
	ts.queuedByChain = make(map[int64][]string)
	ts.activeByChain = make(map[int64]string)
	ts.nextQueueSeq = persisted.NextQueueSeq

	for _, entry := range persisted.Tasks {
		if shouldPruneTask(now, entry) {
			continue
		}
		if entry.TaskID == "" || entry.ChainID == 0 || !entry.State.Valid() {
			log.Warnf("Skipping invalid task entry: taskID=%q chainID=%d state=%q", entry.TaskID, entry.ChainID, entry.State)
			continue
		}
		ts.tasks[entry.TaskID] = entry
		if entry.State == evmrelay.RelayStatePending {
			ts.pending[entry.pendingKey()] = pendingReservationForTask(entry.TaskID)
		}
		loaded++
	}
	ts.rebuildIndexesLocked()

	log.Infof("Loaded %d tasks from disk (%d pruned)", loaded, len(persisted.Tasks)-loaded)
	return nil
}

// save writes a cloned snapshot of the current task state to disk. Task data is
// cloned under ts.mtx, then marshaled and written after unlocking. saveMtx
// serializes writers so concurrent saves cannot race on the temp file.
func (ts *taskStore) save() error {
	ts.saveMtx.Lock()
	defer ts.saveMtx.Unlock()

	ts.mtx.Lock()
	entries := make([]*taskEntry, 0, len(ts.tasks))
	for _, entry := range ts.tasks {
		cloned := cloneTaskEntry(entry)
		entries = append(entries, &cloned)
	}
	persisted := persistedTasks{
		NextQueueSeq: ts.nextQueueSeq,
		Tasks:        entries,
	}
	ts.mtx.Unlock()

	data, err := json.MarshalIndent(&persisted, "", "  ")
	if err != nil {
		return fmt.Errorf("error marshaling tasks: %w", err)
	}

	tmpPath := ts.tasksFilePath() + ".tmp"
	f, err := os.OpenFile(tmpPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return fmt.Errorf("error creating tasks temp file: %w", err)
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("error writing tasks file: %w", err)
	}
	if err := f.Sync(); err != nil {
		f.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("error syncing tasks file: %w", err)
	}
	if err := f.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("error closing tasks file: %w", err)
	}
	if err := os.Rename(tmpPath, ts.tasksFilePath()); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("error replacing tasks file: %w", err)
	}
	return nil
}

func shouldPruneTask(now time.Time, entry *taskEntry) bool {
	if entry.State == evmrelay.RelayStatePending || entry.ResolvedAt == nil {
		return false
	}
	return now.Sub(*entry.ResolvedAt) > taskRetentionPeriod
}

func cloneTaskEntry(entry *taskEntry) taskEntry {
	clone := *entry
	clone.Calldata = append([]byte(nil), entry.Calldata...)
	clone.RelayTxData = append([]byte(nil), entry.RelayTxData...)
	clone.CancelTxData = append([]byte(nil), entry.CancelTxData...)
	clone.RelayTxHashes = append([]common.Hash(nil), entry.RelayTxHashes...)
	clone.CancelTxHashes = append([]common.Hash(nil), entry.CancelTxHashes...)
	clone.RelayerNonce = cloneUint64Ptr(entry.RelayerNonce)
	clone.ResolvedAt = cloneTimePtr(entry.ResolvedAt)
	clone.FirstSentAt = cloneTimePtr(entry.FirstSentAt)
	clone.LastSentAt = cloneTimePtr(entry.LastSentAt)
	clone.LastReplacedAt = cloneTimePtr(entry.LastReplacedAt)
	return clone
}

// queries

func (ts *taskStore) prune(now time.Time) int {
	ts.mtx.Lock()
	defer ts.mtx.Unlock()

	pruned := 0
	for taskID, entry := range ts.tasks {
		if !shouldPruneTask(now, entry) {
			continue
		}
		delete(ts.tasks, taskID)
		delete(ts.pending, entry.pendingKey())
		pruned++
	}

	return pruned
}

func (ts *taskStore) snapshot(taskID string) (taskEntry, bool) {
	ts.mtx.Lock()
	defer ts.mtx.Unlock()

	entry, ok := ts.tasks[taskID]
	if !ok || entry == nil {
		return taskEntry{}, false
	}
	return cloneTaskEntry(entry), true
}

func (ts *taskStore) activeTask(chainID int64) (taskEntry, bool) {
	ts.mtx.Lock()
	defer ts.mtx.Unlock()

	taskID := ts.activeByChain[chainID]
	if taskID == "" {
		return taskEntry{}, false
	}

	entry := ts.tasks[taskID]
	if entry == nil || entry.ChainID != chainID || entry.State != evmrelay.RelayStatePending ||
		(entry.Phase != taskPhaseActive && entry.Phase != taskPhaseCanceling) {
		delete(ts.activeByChain, chainID)
		return taskEntry{}, false
	}
	return cloneTaskEntry(entry), true
}

func (ts *taskStore) nextQueuedTask(chainID int64) (taskEntry, bool) {
	ts.mtx.Lock()
	defer ts.mtx.Unlock()

	queue := ts.queuedByChain[chainID]
	for len(queue) > 0 {
		taskID := queue[0]
		entry := ts.tasks[taskID]
		if entry != nil && entry.ChainID == chainID && entry.State == evmrelay.RelayStatePending && entry.Phase == taskPhaseQueued {
			ts.queuedByChain[chainID] = queue
			return cloneTaskEntry(entry), true
		}
		queue = queue[1:]
	}
	delete(ts.queuedByChain, chainID)
	return taskEntry{}, false
}

func (ts *taskStore) queueDepth(chainID int64) int {
	ts.mtx.RLock()
	defer ts.mtx.RUnlock()
	count := 0
	for _, taskID := range ts.queuedByChain[chainID] {
		entry := ts.tasks[taskID]
		if entry != nil && entry.ChainID == chainID && entry.State == evmrelay.RelayStatePending && entry.Phase == taskPhaseQueued {
			count++
		}
	}
	return count
}

// reservations

type reservationResult struct {
	existingTaskID string
	conflict       bool
}

func (ts *taskStore) reservePendingSlot(pk pendingKey, calldata []byte) reservationResult {
	ts.mtx.Lock()
	defer ts.mtx.Unlock()

	if reservation, ok := ts.pending[pk]; ok {
		if reservation.hasTask() {
			if existing := ts.tasks[reservation.taskID]; existing != nil && bytes.Equal(existing.Calldata, calldata) {
				return reservationResult{existingTaskID: reservation.taskID}
			}
		}
		return reservationResult{conflict: true}
	}

	ts.pending[pk] = reservePendingTask()
	return reservationResult{}
}

func (ts *taskStore) releasePendingReservation(pk pendingKey) {
	ts.mtx.Lock()
	defer ts.mtx.Unlock()

	reservation, ok := ts.pending[pk]
	if ok && reservation.reserved && !reservation.hasTask() {
		delete(ts.pending, pk)
	}
}

// task transitions
//
// Methods ending in Locked require ts.mtx to already be held.

func (ts *taskStore) createQueuedTask(entry *taskEntry) {
	ts.mtx.Lock()
	defer ts.mtx.Unlock()

	ts.nextQueueSeq++
	entry.QueueSeq = ts.nextQueueSeq
	ts.tasks[entry.TaskID] = entry
	ts.pending[entry.pendingKey()] = pendingReservationForTask(entry.TaskID)
	ts.enqueueQueuedTaskLocked(entry)
}

func (ts *taskStore) deleteTask(taskID string) {
	ts.mtx.Lock()
	defer ts.mtx.Unlock()

	if entry := ts.tasks[taskID]; entry != nil {
		ts.removeQueuedTaskLocked(entry)
		ts.clearActiveTaskLocked(entry)
		delete(ts.pending, entry.pendingKey())
	}
	delete(ts.tasks, taskID)
}

// activateTaskLocked transitions a task from queued to active.
func (ts *taskStore) activateTaskLocked(taskID string, relayerNonce uint64, txHash common.Hash, rawTx []byte) bool {
	entry := ts.tasks[taskID]
	if entry == nil || entry.State != evmrelay.RelayStatePending || entry.Phase != taskPhaseQueued {
		return false
	}
	ts.removeQueuedTaskLocked(entry)
	entry.Phase = taskPhaseActive
	ts.activeByChain[entry.ChainID] = entry.TaskID
	entry.RelayerNonce = &relayerNonce
	entry.RelayTxHashes = []common.Hash{txHash}
	entry.RelayTxData = append([]byte(nil), rawTx...)
	entry.CancelTxHashes = nil
	entry.CancelTxData = nil
	entry.LastReplacedAt = nil
	return true
}

// recordBroadcastAttemptLocked updates the timestamps for a pending task after a
// broadcast or rebroadcast attempt.
func (ts *taskStore) recordBroadcastAttemptLocked(taskID string) bool {
	entry := ts.tasks[taskID]
	if entry == nil || entry.State != evmrelay.RelayStatePending {
		return false
	}
	now := time.Now()
	if entry.FirstSentAt == nil {
		entry.FirstSentAt = &now
	}
	entry.LastSentAt = &now
	return true
}

// startCancellationLocked transitions an active task into canceling.
func (ts *taskStore) startCancellationLocked(taskID string, txHash common.Hash, rawTx []byte) bool {
	entry := ts.tasks[taskID]
	if entry == nil || entry.State != evmrelay.RelayStatePending || entry.RelayerNonce == nil {
		return false
	}
	entry.Phase = taskPhaseCanceling
	entry.CancelTxHashes = []common.Hash{txHash}
	entry.CancelTxData = append([]byte(nil), rawTx...)
	now := time.Now()
	entry.LastReplacedAt = &now
	return true
}

// replaceActiveTxLocked records a same-nonce redeem replacement while the task
// remains active.
func (ts *taskStore) replaceActiveTxLocked(taskID string, txHash common.Hash, rawTx []byte) bool {
	entry := ts.tasks[taskID]
	if entry == nil || entry.State != evmrelay.RelayStatePending || entry.Phase != taskPhaseActive || entry.RelayerNonce == nil {
		return false
	}
	entry.RelayTxData = append([]byte(nil), rawTx...)
	entry.RelayTxHashes = appendUniqueHash(entry.RelayTxHashes, txHash)
	now := time.Now()
	entry.LastReplacedAt = &now
	return true
}

// replaceCancelTxLocked records a same-nonce cancel replacement while the task
// remains in the canceling phase.
func (ts *taskStore) replaceCancelTxLocked(taskID string, txHash common.Hash, rawTx []byte) bool {
	entry := ts.tasks[taskID]
	if entry == nil || entry.State != evmrelay.RelayStatePending || entry.Phase != taskPhaseCanceling || entry.RelayerNonce == nil {
		return false
	}
	entry.CancelTxData = append([]byte(nil), rawTx...)
	entry.CancelTxHashes = appendUniqueHash(entry.CancelTxHashes, txHash)
	now := time.Now()
	entry.LastReplacedAt = &now
	return true
}

// markTaskFailedLocked transitions any task to the failed terminal state.
func (ts *taskStore) markTaskFailedLocked(taskID string, reason string, resolvedAt *time.Time) bool {
	entry := ts.tasks[taskID]
	if entry == nil {
		return false
	}
	ts.removeQueuedTaskLocked(entry)
	ts.clearActiveTaskLocked(entry)
	entry.State = evmrelay.RelayStateFailed
	entry.FailureReason = reason
	if resolvedAt == nil {
		now := time.Now()
		resolvedAt = &now
	}
	entry.ResolvedAt = resolvedAt
	delete(ts.pending, entry.pendingKey())
	return true
}

// markTaskSucceededLocked transitions any task to the success terminal state.
func (ts *taskStore) markTaskSucceededLocked(taskID string, txHash common.Hash, resolvedAt time.Time) bool {
	entry := ts.tasks[taskID]
	if entry == nil {
		return false
	}
	ts.removeQueuedTaskLocked(entry)
	ts.clearActiveTaskLocked(entry)
	entry.State = evmrelay.RelayStateSuccess
	entry.SuccessTxHash = txHash
	entry.ResolvedAt = &resolvedAt
	delete(ts.pending, entry.pendingKey())
	return true
}

func (ts *taskStore) activateTask(taskID string, relayerNonce uint64, txHash common.Hash, rawTx []byte) bool {
	ts.mtx.Lock()
	defer ts.mtx.Unlock()
	return ts.activateTaskLocked(taskID, relayerNonce, txHash, rawTx)
}

func (ts *taskStore) replaceActiveTx(taskID string, txHash common.Hash, rawTx []byte) bool {
	ts.mtx.Lock()
	defer ts.mtx.Unlock()
	if !ts.replaceActiveTxLocked(taskID, txHash, rawTx) {
		return false
	}
	return ts.recordBroadcastAttemptLocked(taskID)
}

func (ts *taskStore) recordBroadcastAttempt(taskID string) bool {
	ts.mtx.Lock()
	defer ts.mtx.Unlock()
	return ts.recordBroadcastAttemptLocked(taskID)
}

func (ts *taskStore) startCancellation(taskID string, txHash common.Hash, rawTx []byte) bool {
	ts.mtx.Lock()
	defer ts.mtx.Unlock()
	if !ts.startCancellationLocked(taskID, txHash, rawTx) {
		return false
	}
	return ts.recordBroadcastAttemptLocked(taskID)
}

func (ts *taskStore) replaceCancelTx(taskID string, txHash common.Hash, rawTx []byte) bool {
	ts.mtx.Lock()
	defer ts.mtx.Unlock()
	if !ts.replaceCancelTxLocked(taskID, txHash, rawTx) {
		return false
	}
	return ts.recordBroadcastAttemptLocked(taskID)
}

func (ts *taskStore) markTaskSucceeded(taskID string, txHash common.Hash, resolvedAt time.Time) bool {
	ts.mtx.Lock()
	defer ts.mtx.Unlock()
	return ts.markTaskSucceededLocked(taskID, txHash, resolvedAt)
}

func (ts *taskStore) markTaskFailed(taskID string, reason string, resolvedAt *time.Time) bool {
	ts.mtx.Lock()
	defer ts.mtx.Unlock()
	return ts.markTaskFailedLocked(taskID, reason, resolvedAt)
}

// helpers

func appendUniqueHash(hashes []common.Hash, hash common.Hash) []common.Hash {
	for _, existing := range hashes {
		if existing == hash {
			return hashes
		}
	}
	return append(hashes, hash)
}

func (ts *taskStore) rebuildIndexesLocked() {
	for _, entry := range ts.tasks {
		if entry.State != evmrelay.RelayStatePending {
			continue
		}
		if entry.Phase == taskPhaseQueued {
			ts.queuedByChain[entry.ChainID] = append(ts.queuedByChain[entry.ChainID], entry.TaskID)
			continue
		}
		if entry.Phase == taskPhaseActive || entry.Phase == taskPhaseCanceling {
			ts.activeByChain[entry.ChainID] = entry.TaskID
		}
	}
	for chainID, taskIDs := range ts.queuedByChain {
		sort.Slice(taskIDs, func(i, j int) bool {
			return ts.tasks[taskIDs[i]].QueueSeq < ts.tasks[taskIDs[j]].QueueSeq
		})
		ts.queuedByChain[chainID] = taskIDs
	}
}

func (ts *taskStore) enqueueQueuedTaskLocked(entry *taskEntry) {
	if entry == nil {
		return
	}
	ts.queuedByChain[entry.ChainID] = append(ts.queuedByChain[entry.ChainID], entry.TaskID)
}

func (ts *taskStore) removeQueuedTaskLocked(entry *taskEntry) {
	if entry == nil {
		return
	}
	taskIDs := ts.queuedByChain[entry.ChainID]
	for i, taskID := range taskIDs {
		if taskID != entry.TaskID {
			continue
		}
		taskIDs = append(taskIDs[:i], taskIDs[i+1:]...)
		if len(taskIDs) == 0 {
			delete(ts.queuedByChain, entry.ChainID)
		} else {
			ts.queuedByChain[entry.ChainID] = taskIDs
		}
		return
	}
}

func (ts *taskStore) clearActiveTaskLocked(entry *taskEntry) {
	if entry == nil {
		return
	}
	if ts.activeByChain[entry.ChainID] == entry.TaskID {
		delete(ts.activeByChain, entry.ChainID)
	}
}

func cloneUint64Ptr(v *uint64) *uint64 {
	if v == nil {
		return nil
	}
	cloned := *v
	return &cloned
}

func cloneTimePtr(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	cloned := *t
	return &cloned
}
