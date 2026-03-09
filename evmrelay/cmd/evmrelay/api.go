// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package main

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	dexeth "decred.org/dcrdex/dex/networks/eth"
	"decred.org/dcrdex/evmrelay"
	"github.com/ethereum/go-ethereum/common"
	"github.com/google/uuid"
)

func (s *relayServer) handleHealth(w http.ResponseWriter, r *http.Request) {
	chainIDStr := r.URL.Query().Get("chain")
	if chainIDStr == "" {
		writeJSON(w, http.StatusBadRequest, &evmrelay.HealthResponse{OK: false, Error: "missing chain parameter"})
		return
	}

	var chainID int64
	if _, err := fmt.Sscanf(chainIDStr, "%d", &chainID); err != nil {
		writeJSON(w, http.StatusBadRequest, &evmrelay.HealthResponse{OK: false, Error: "invalid chain ID"})
		return
	}

	cc, err := s.getChain(chainID)
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, &evmrelay.HealthResponse{OK: false, Error: err.Error()})
		return
	}

	if err := cc.checkHealth(r.Context()); err != nil {
		log.Errorf("Health check failed for chain %d: %v", chainID, err)
		writeJSON(w, http.StatusServiceUnavailable, &evmrelay.HealthResponse{OK: false, Error: "RPC unreachable"})
		return
	}

	writeJSON(w, http.StatusOK, &evmrelay.HealthResponse{OK: true})
}

func (s *relayServer) handleEstimateFee(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)

	var req evmrelay.EstimateFeeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, &evmrelay.ErrorResponse{Error: "invalid request body"})
		return
	}

	cc, err := s.getChain(req.ChainID)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, &evmrelay.ErrorResponse{Error: err.Error()})
		return
	}

	if _, err := validateEstimateFeeRequest(&req, cc.allowedTargets); err != nil {
		writeJSON(w, http.StatusBadRequest, &evmrelay.ErrorResponse{Error: err.Error()})
		return
	}

	fee, err := cc.estimateFee(r.Context(), req.NumRedemptions)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, &evmrelay.ErrorResponse{Error: err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, &evmrelay.EstimateFeeResponse{
		RelayAddr: s.relayAddr.Hex(),
		Fee:       fee.String(),
	})
}

func (s *relayServer) handleRelay(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)

	var req evmrelay.RelayRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, &evmrelay.SubmitResponse{Error: "invalid request body"})
		return
	}

	cc, err := s.getChain(req.ChainID)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, &evmrelay.SubmitResponse{Error: err.Error()})
		return
	}

	target, err := validateAllowedTarget(req.Target, cc.allowedTargets)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, &evmrelay.SubmitResponse{Error: err.Error()})
		return
	}

	calldata, parsed, err := validateRelayCalldata(req.Calldata, s.relayAddr, time.Now())
	if err != nil {
		writeJSON(w, http.StatusBadRequest, &evmrelay.SubmitResponse{Error: err.Error()})
		return
	}

	if depth := s.store.queueDepth(req.ChainID); depth >= maxQueuedTasksPerChain {
		writeJSON(w, http.StatusServiceUnavailable, &evmrelay.SubmitResponse{
			Error: fmt.Sprintf("chain %d queue is full (%d pending tasks)", req.ChainID, depth),
		})
		return
	}

	pk := pendingKey{participant: parsed.Participant, nonce: parsed.Nonce.Uint64()}

	reservation := s.store.reservePendingSlot(pk, calldata)
	if reservation.existingTaskID != "" {
		writeJSON(w, http.StatusOK, &evmrelay.SubmitResponse{TaskID: reservation.existingTaskID})
		return
	}
	if reservation.conflict {
		writeJSON(w, http.StatusConflict, &evmrelay.SubmitResponse{
			Error: "a different redemption is already in-flight for this nonce",
		})
		return
	}

	cleanupReservation := func() {
		s.store.releasePendingReservation(pk)
	}

	taskID := uuid.New().String()
	entry, err := newPendingTaskEntry(taskID, req.ChainID, target, parsed, calldata)
	if err != nil {
		cleanupReservation()
		writeJSON(w, http.StatusBadRequest, &evmrelay.SubmitResponse{Error: err.Error()})
		return
	}
	s.store.createQueuedTask(entry)
	if err := s.store.save(); err != nil {
		cleanupReservation()
		s.store.deleteTask(taskID)
		writeJSON(w, http.StatusInternalServerError, &evmrelay.SubmitResponse{Error: fmt.Sprintf("failed to persist relay task: %v", err)})
		return
	}

	log.Infof("Chain %d: queued task %s (req=%x)", req.ChainID, taskID, requestFingerprint(req))
	writeJSON(w, http.StatusCreated, &evmrelay.SubmitResponse{TaskID: taskID})
}

func (s *relayServer) handleRelayStatus(w http.ResponseWriter, r *http.Request) {
	taskID := r.PathValue("taskID")
	if taskID == "" {
		writeJSON(w, http.StatusBadRequest, &evmrelay.StatusResponse{Error: "missing task ID"})
		return
	}

	snapshot, ok := s.store.snapshot(taskID)
	if !ok {
		writeJSON(w, http.StatusNotFound, &evmrelay.StatusResponse{Error: "task not found"})
		return
	}

	writeJSON(w, http.StatusOK, &evmrelay.StatusResponse{
		State:         snapshot.State,
		TxHash:        hashString(snapshot.statusTxHash()),
		FailureReason: snapshot.FailureReason,
	})
}

func validateEstimateFeeRequest(req *evmrelay.EstimateFeeRequest, allowedTargets map[common.Address]bool) (common.Address, error) {
	if err := validateNumRedemptions(req.NumRedemptions); err != nil {
		return common.Address{}, err
	}
	return validateAllowedTarget(req.Target, allowedTargets)
}

func validateAllowedTarget(target string, allowedTargets map[common.Address]bool) (common.Address, error) {
	if !common.IsHexAddress(target) {
		return common.Address{}, fmt.Errorf("invalid target address")
	}

	addr := common.HexToAddress(target)
	if !allowedTargets[addr] {
		return common.Address{}, fmt.Errorf("target address not allowed")
	}

	return addr, nil
}

func validateNumRedemptions(numRedemptions int) error {
	if numRedemptions <= 0 {
		return fmt.Errorf("numRedemptions must be > 0")
	}
	if numRedemptions > evmrelay.MaxSignedRedeemBatch {
		return fmt.Errorf("numRedemptions must be <= %d", evmrelay.MaxSignedRedeemBatch)
	}
	return nil
}

func validateRelayCalldata(calldataHex string, relayAddr common.Address, now time.Time) ([]byte, *dexeth.SignedRedemptionV1, error) {
	calldata, err := hexDecode(calldataHex)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid calldata hex")
	}

	parsed, err := validateRelayCalldataBytes(calldata, relayAddr, now)
	if err != nil {
		return nil, nil, err
	}

	return calldata, parsed, nil
}

func validateRelayCalldataBytes(calldata []byte, relayAddr common.Address, now time.Time) (*dexeth.SignedRedemptionV1, error) {
	parsed, err := dexeth.ParseSignedRedeemDataV1(calldata)
	if err != nil {
		return nil, fmt.Errorf("invalid calldata: %v", err)
	}
	if err := validateSignedRedeemRequest(parsed, relayAddr, now); err != nil {
		return nil, err
	}

	return parsed, nil
}

func validateSignedRedeemRequest(parsed *dexeth.SignedRedemptionV1, relayAddr common.Address, now time.Time) error {
	if parsed.NumRedemptions <= 0 {
		return fmt.Errorf("numRedemptions must be > 0")
	}
	if parsed.NumRedemptions > evmrelay.MaxSignedRedeemBatch {
		return fmt.Errorf("too many redemptions: %d > %d", parsed.NumRedemptions, evmrelay.MaxSignedRedeemBatch)
	}
	if parsed.Deadline.Int64() <= now.Unix() {
		return &deadlineExpiredError{
			Deadline: time.Unix(parsed.Deadline.Int64(), 0),
			Now:      now,
		}
	}
	if parsed.FeeRecipient == (common.Address{}) {
		return fmt.Errorf("feeRecipient must match relay address")
	}
	if parsed.FeeRecipient != relayAddr {
		return fmt.Errorf("feeRecipient %s does not match relay address %s", parsed.FeeRecipient.Hex(), relayAddr.Hex())
	}
	return nil
}

func requestFingerprint(req evmrelay.RelayRequest) [32]byte {
	return sha256.Sum256([]byte(fmt.Sprintf("%d|%s|%s", req.ChainID, req.Target, req.Calldata)))
}

func hashString(hash common.Hash) string {
	if hash == (common.Hash{}) {
		return ""
	}
	return hash.Hex()
}
