// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package main

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	dexeth "decred.org/dcrdex/dex/networks/eth"
	swapv1 "decred.org/dcrdex/dex/networks/eth/contracts/v1"
	"decred.org/dcrdex/evmrelay"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/rpc"
)

// --- test helpers ---

// tCalldataWithDeadline generates valid redeemWithSignature calldata with a
// single redemption and a specified deadline.
func tCalldataWithDeadline(t *testing.T, participant common.Address, value, relayerFee *big.Int, nonce uint64, deadline time.Time) []byte {
	t.Helper()
	return tCalldataForRecipient(t, participant, common.HexToAddress("0x2222222222222222222222222222222222222222"), value, relayerFee, nonce, deadline)
}

func tCalldataForRecipient(t *testing.T, participant, feeRecipient common.Address, value, relayerFee *big.Int, nonce uint64, deadline time.Time) []byte {
	t.Helper()
	a := dexeth.ABIs[1]
	redemptions := []swapv1.ETHSwapRedemption{{
		V: swapv1.ETHSwapVector{
			SecretHash:      [32]byte{1},
			Value:           value,
			Initiator:       common.HexToAddress("0x1111111111111111111111111111111111111111"),
			RefundTimestamp: uint64(time.Now().Add(time.Hour).Unix()),
			Participant:     participant,
		},
		Secret: [32]byte{2},
	}}
	n := new(big.Int).SetUint64(nonce)
	dl := new(big.Int).SetInt64(deadline.Unix())
	sig := make([]byte, 65)

	calldata, err := a.Pack("redeemWithSignature", redemptions, feeRecipient, relayerFee, n, dl, sig)
	if err != nil {
		t.Fatalf("tCalldataWithDeadline: %v", err)
	}
	return calldata
}

// tCalldata generates valid redeemWithSignature calldata with a single
// redemption and a future deadline.
func tCalldata(t *testing.T, participant common.Address, value, relayerFee *big.Int, nonce uint64) []byte {
	t.Helper()
	return tCalldataWithDeadline(t, participant, value, relayerFee, nonce, time.Now().Add(time.Hour))
}

func tRelayCalldata(t *testing.T, relayAddr, participant common.Address, value, relayerFee *big.Int, nonce uint64) []byte {
	t.Helper()
	return tCalldataForRecipient(t, participant, relayAddr, value, relayerFee, nonce, time.Now().Add(time.Hour))
}

// tServer creates a minimal relayServer for testing with empty maps and a
// temp directory for persistence.
func tServer(t *testing.T) *relayServer {
	t.Helper()
	return &relayServer{
		chains:    make(map[int64]*chainClient),
		relayAddr: common.HexToAddress("0x3333333333333333333333333333333333333333"),
		limiter:   newIPLimiter(),
		store:     newTaskStore(t.TempDir()),
	}
}

func testPutTask(t *testing.T, store *taskStore, entry *taskEntry) {
	t.Helper()
	store.mtx.Lock()
	defer store.mtx.Unlock()

	if existing := store.tasks[entry.TaskID]; existing != nil {
		delete(store.pending, existing.pendingKey())
	}

	store.tasks[entry.TaskID] = entry
	if entry.State == evmrelay.RelayStatePending {
		store.pending[entry.pendingKey()] = pendingReservationForTask(entry.TaskID)
	}
	if entry.QueueSeq > store.nextQueueSeq {
		store.nextQueueSeq = entry.QueueSeq
	}
	store.queuedByChain = make(map[int64][]string)
	store.activeByChain = make(map[int64]string)
	store.rebuildIndexesLocked()
}

func testPutPendingReservation(t *testing.T, store *taskStore, pk pendingKey, taskID string) {
	t.Helper()
	store.mtx.Lock()
	defer store.mtx.Unlock()
	store.pending[pk] = pendingReservationForTask(taskID)
}

func testHasPendingReservation(t *testing.T, store *taskStore, pk pendingKey) bool {
	t.Helper()
	store.mtx.Lock()
	defer store.mtx.Unlock()
	_, ok := store.pending[pk]
	return ok
}

func testPendingReservation(t *testing.T, store *taskStore, pk pendingKey) (pendingReservation, bool) {
	t.Helper()
	store.mtx.Lock()
	defer store.mtx.Unlock()
	reservation, ok := store.pending[pk]
	return reservation, ok
}

// tPost sends a POST request to the handler and returns the response recorder.
func tPost(handler http.HandlerFunc, path, body string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", path, strings.NewReader(body))
	handler(w, r)
	return w
}

// tGet sends a GET request to the handler and returns the response recorder.
func tGet(handler http.HandlerFunc, path string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", path, nil)
	handler(w, r)
	return w
}

// respJSON decodes a JSON response body into a map.
func respJSON(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &m); err != nil {
		t.Fatalf("error decoding response JSON: %v\nbody: %s", err, w.Body.String())
	}
	return m
}

// tFakeRPC creates an ethclient connected to a fake server that returns errors
// for all JSON-RPC calls.
func tFakeRPC(t *testing.T) *ethclient.Client {
	t.Helper()
	rpcSrv := rpc.NewServer()
	rpcClient := rpc.DialInProc(rpcSrv)
	ec := ethclient.NewClient(rpcClient)
	t.Cleanup(ec.Close)
	return ec
}

type tEthRPC struct {
	header         *types.Header
	headerErr      error
	blockHeader    bool
	tipCap         *big.Int
	tipCapErr      error
	blockTipCap    bool
	estimateGas    uint64
	estimateGasErr error
	blockEstimate  bool
	receipts       map[common.Hash]*types.Receipt
	receiptErrs    map[common.Hash]error
	blockReceipt   bool
	sendErr        error
	sentTxs        []*types.Transaction
	pendingNonce   hexutil.Uint64
}

func (e *tEthRPC) GetBlockByNumber(ctx context.Context, _ string, _ bool) (*types.Header, error) {
	if e.blockHeader {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	return e.header, e.headerErr
}

func (e *tEthRPC) MaxPriorityFeePerGas(ctx context.Context) (*hexutil.Big, error) {
	if e.blockTipCap {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	if e.tipCapErr != nil {
		return nil, e.tipCapErr
	}
	if e.tipCap == nil {
		tip := hexutil.Big(*big.NewInt(0))
		return &tip, nil
	}
	tip := hexutil.Big(*e.tipCap)
	return &tip, nil
}

func (e *tEthRPC) EstimateGas(ctx context.Context, _ map[string]any) (hexutil.Uint64, error) {
	if e.blockEstimate {
		<-ctx.Done()
		return 0, ctx.Err()
	}
	return hexutil.Uint64(e.estimateGas), e.estimateGasErr
}

func (e *tEthRPC) GetTransactionReceipt(ctx context.Context, txHash common.Hash) (*types.Receipt, error) {
	if e.blockReceipt {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	if e.receiptErrs != nil {
		if err := e.receiptErrs[txHash]; err != nil {
			return nil, err
		}
	}
	if e.receipts == nil {
		return nil, nil
	}
	return e.receipts[txHash], nil
}

func (e *tEthRPC) GetTransactionCount(_ context.Context, _ common.Address, _ string) (hexutil.Uint64, error) {
	return e.pendingNonce, nil
}

func (e *tEthRPC) SendRawTransaction(_ context.Context, raw string) (common.Hash, error) {
	if e.sendErr != nil {
		return common.Hash{}, e.sendErr
	}
	rawTx, err := hexutil.Decode(raw)
	if err != nil {
		return common.Hash{}, err
	}
	tx := new(types.Transaction)
	if err := tx.UnmarshalBinary(rawTx); err != nil {
		return common.Hash{}, err
	}
	e.sentTxs = append(e.sentTxs, tx)
	return tx.Hash(), nil
}

func tRPCClient(t *testing.T, api *tEthRPC) *ethclient.Client {
	t.Helper()
	rpcSrv := rpc.NewServer()
	if err := rpcSrv.RegisterName("eth", api); err != nil {
		t.Fatalf("register rpc service: %v", err)
	}
	rpcClient := rpc.DialInProc(rpcSrv)
	ec := ethclient.NewClient(rpcClient)
	t.Cleanup(ec.Close)
	return ec
}

func tChainClientForTests(t *testing.T, api *tEthRPC, chainID int64, target common.Address) *chainClient {
	t.Helper()
	privKey, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	return &chainClient{
		chainID:        big.NewInt(chainID),
		ec:             tRPCClient(t, api),
		privKey:        privKey,
		relayAddr:      crypto.PubkeyToAddress(privKey.PublicKey),
		signer:         types.NewLondonSigner(big.NewInt(chainID)),
		gases:          dexeth.VersionedGases[1],
		profitPerGas:   big.NewInt(0),
		allowedTargets: map[common.Address]bool{target: true},
	}
}

func tSignedDynamicTx(t *testing.T, cc *chainClient, to common.Address, data []byte, nonce uint64, gas uint64, tipCap, feeCap *big.Int) (*types.Transaction, []byte) {
	t.Helper()
	tx := types.NewTx(&types.DynamicFeeTx{
		ChainID:   cc.chainID,
		Nonce:     nonce,
		GasTipCap: new(big.Int).Set(tipCap),
		GasFeeCap: new(big.Int).Set(feeCap),
		Gas:       gas,
		To:        &to,
		Data:      append([]byte(nil), data...),
	})
	signedTx, err := types.SignTx(tx, cc.signer, cc.privKey)
	if err != nil {
		t.Fatalf("SignTx: %v", err)
	}
	rawTx, err := signedTx.MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary: %v", err)
	}
	return signedTx, rawTx
}

func tTestHeader(baseFee *big.Int) *types.Header {
	return &types.Header{
		ParentHash:  common.Hash{},
		UncleHash:   types.EmptyUncleHash,
		Root:        common.Hash{},
		TxHash:      types.EmptyTxsHash,
		ReceiptHash: types.EmptyReceiptsHash,
		Bloom:       types.Bloom{},
		Difficulty:  big.NewInt(0),
		Number:      big.NewInt(1),
		GasLimit:    30_000_000,
		GasUsed:     0,
		Time:        uint64(time.Now().Unix()),
		Extra:       []byte{},
		BaseFee:     new(big.Int).Set(baseFee),
	}
}

func tTestReceipt(txHash common.Hash, status uint64) *types.Receipt {
	return &types.Receipt{
		Type:              types.DynamicFeeTxType,
		Status:            status,
		CumulativeGasUsed: 21_000,
		Bloom:             types.Bloom{},
		Logs:              []*types.Log{},
		TxHash:            txHash,
		GasUsed:           21_000,
	}
}

// --- tests ---

func TestParseSignedRedeemDataV1(t *testing.T) {
	participant := common.HexToAddress("0xabcdef0123456789abcdef0123456789abcdef01")

	tests := []struct {
		name           string
		calldata       func() []byte
		wantErr        bool
		participant    common.Address
		feeRecipient   common.Address
		nonce          uint64
		relayerFee     *big.Int
		totalRedeemed  *big.Int
		numRedemptions int
	}{
		{
			name: "single redemption",
			calldata: func() []byte {
				return tCalldata(t, participant, big.NewInt(1e18), big.NewInt(1e15), 42)
			},
			participant:    participant,
			feeRecipient:   common.HexToAddress("0x2222222222222222222222222222222222222222"),
			nonce:          42,
			relayerFee:     big.NewInt(1e15),
			totalRedeemed:  big.NewInt(1e18),
			numRedemptions: 1,
		},
		{
			name: "multiple redemptions",
			calldata: func() []byte {
				a := dexeth.ABIs[1]
				redemptions := []swapv1.ETHSwapRedemption{
					{
						V: swapv1.ETHSwapVector{
							SecretHash:      [32]byte{1},
							Value:           big.NewInt(1e18),
							Initiator:       common.HexToAddress("0x1111111111111111111111111111111111111111"),
							RefundTimestamp: 9999,
							Participant:     participant,
						},
						Secret: [32]byte{0xaa},
					},
					{
						V: swapv1.ETHSwapVector{
							SecretHash:      [32]byte{2},
							Value:           big.NewInt(2e18),
							Initiator:       common.HexToAddress("0x1111111111111111111111111111111111111111"),
							RefundTimestamp: 9999,
							Participant:     participant,
						},
						Secret: [32]byte{0xbb},
					},
				}
				feeRecipient := common.HexToAddress("0x2222222222222222222222222222222222222222")
				relayerFee := big.NewInt(5e15)
				nonce := big.NewInt(7)
				deadline := big.NewInt(time.Now().Add(time.Hour).Unix())
				sig := make([]byte, 65)
				calldata, err := a.Pack("redeemWithSignature", redemptions, feeRecipient, relayerFee, nonce, deadline, sig)
				if err != nil {
					t.Fatal(err)
				}
				return calldata
			},
			participant:    participant,
			feeRecipient:   common.HexToAddress("0x2222222222222222222222222222222222222222"),
			nonce:          7,
			relayerFee:     big.NewInt(5e15),
			totalRedeemed:  new(big.Int).Add(big.NewInt(1e18), big.NewInt(2e18)),
			numRedemptions: 2,
		},
		{
			name: "calldata too short",
			calldata: func() []byte {
				return []byte{0x01, 0x02}
			},
			wantErr: true,
		},
		{
			name: "unknown selector",
			calldata: func() []byte {
				cd := tCalldata(t, participant, big.NewInt(1e18), big.NewInt(1e15), 1)
				cd[0] ^= 0xff // corrupt first byte of selector
				return cd
			},
			wantErr: true,
		},
		{
			name: "wrong method - redeem instead of redeemWithSignature",
			calldata: func() []byte {
				a := dexeth.ABIs[1]
				redemptions := []swapv1.ETHSwapRedemption{{
					V: swapv1.ETHSwapVector{
						SecretHash:      [32]byte{1},
						Value:           big.NewInt(1e18),
						Initiator:       common.HexToAddress("0x1111111111111111111111111111111111111111"),
						RefundTimestamp: 9999,
						Participant:     participant,
					},
					Secret: [32]byte{2},
				}}
				cd, err := a.Pack("redeem", common.Address{}, redemptions)
				if err != nil {
					t.Fatal(err)
				}
				return cd
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parsed, err := dexeth.ParseSignedRedeemDataV1(tt.calldata())
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if parsed.Participant != tt.participant {
				t.Errorf("participant: got %s, want %s", parsed.Participant.Hex(), tt.participant.Hex())
			}
			if parsed.FeeRecipient != tt.feeRecipient {
				t.Errorf("feeRecipient: got %s, want %s", parsed.FeeRecipient.Hex(), tt.feeRecipient.Hex())
			}
			if parsed.Nonce.Uint64() != tt.nonce {
				t.Errorf("nonce: got %d, want %d", parsed.Nonce.Uint64(), tt.nonce)
			}
			if parsed.RelayerFee.Cmp(tt.relayerFee) != 0 {
				t.Errorf("relayerFee: got %s, want %s", parsed.RelayerFee, tt.relayerFee)
			}
			if parsed.TotalRedeemed.Cmp(tt.totalRedeemed) != 0 {
				t.Errorf("totalRedeemed: got %s, want %s", parsed.TotalRedeemed, tt.totalRedeemed)
			}
			if parsed.NumRedemptions != tt.numRedemptions {
				t.Errorf("numRedemptions: got %d, want %d", parsed.NumRedemptions, tt.numRedemptions)
			}
		})
	}
}

func TestRelayStateJSON(t *testing.T) {
	tests := []struct {
		state evmrelay.RelayState
		str   string
	}{
		{evmrelay.RelayStatePending, string(evmrelay.RelayStatePending)},
		{evmrelay.RelayStateSuccess, string(evmrelay.RelayStateSuccess)},
		{evmrelay.RelayStateFailed, string(evmrelay.RelayStateFailed)},
	}

	for _, tt := range tests {
		t.Run(tt.str, func(t *testing.T) {
			// Marshal
			data, err := json.Marshal(tt.state)
			if err != nil {
				t.Fatalf("Marshal: %v", err)
			}
			want := `"` + tt.str + `"`
			if string(data) != want {
				t.Errorf("Marshal: got %s, want %s", data, want)
			}

			// Unmarshal round-trip
			var got evmrelay.RelayState
			if err := json.Unmarshal(data, &got); err != nil {
				t.Fatalf("Unmarshal: %v", err)
			}
			if got != tt.state {
				t.Errorf("Unmarshal round-trip: got %q, want %q", got, tt.state)
			}
		})
	}

	// Unknown string should error.
	t.Run("unknown string", func(t *testing.T) {
		var s evmrelay.RelayState
		if err := json.Unmarshal([]byte(`"bogus"`), &s); err == nil {
			t.Fatal("expected error for unknown task state")
		}
	})
}

func TestHexDecode(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string // hex-encoded expected output
		wantErr bool
	}{
		{"with 0x prefix", "0xabcdef", "abcdef", false},
		{"without prefix", "abcdef", "abcdef", false},
		{"empty string", "", "", false},
		{"invalid hex", "zzzz", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := hexDecode(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if hex.EncodeToString(got) != tt.want {
				t.Errorf("got %x, want %s", got, tt.want)
			}
		})
	}
}

func TestHandleHealth(t *testing.T) {
	s := tServer(t)

	tests := []struct {
		name     string
		query    string
		wantCode int
	}{
		{"missing chain param", "/api/health", http.StatusBadRequest},
		{"invalid chain ID", "/api/health?chain=abc", http.StatusBadRequest},
		{"unknown chain ID", "/api/health?chain=999", http.StatusServiceUnavailable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := tGet(s.handleHealth, tt.query)
			if w.Code != tt.wantCode {
				t.Errorf("status: got %d, want %d; body: %s", w.Code, tt.wantCode, w.Body.String())
			}
		})
	}
}

func TestHandleRelayDuplicates(t *testing.T) {
	s := tServer(t)

	// Set up a chain entry with a fake RPC so the handler can get past
	// getChain(). The fake RPC returns errors, which is fine for the
	// duplicate-check paths (they return before any RPC call).
	chainID := int64(42)
	gases := dexeth.VersionedGases[1]
	target := common.HexToAddress("0x0000000000000000000000000000000000000000")
	s.chains[chainID] = &chainClient{
		ec: tFakeRPC(t),

		chainID:        big.NewInt(chainID),
		gases:          gases,
		allowedTargets: map[common.Address]bool{target: true},
	}

	participant := common.HexToAddress("0xabcdef0123456789abcdef0123456789abcdef01")
	value := big.NewInt(1e18)
	relayerFee := big.NewInt(1e15)
	nonce := uint64(1)

	calldata := tRelayCalldata(t, s.relayAddr, participant, value, relayerFee, nonce)
	calldataHex := hex.EncodeToString(calldata)

	// Pre-populate a pending task.
	taskID := "existing-task-id"
	testPutTask(t, s.store, &taskEntry{
		TaskID:      taskID,
		ChainID:     chainID,
		State:       evmrelay.RelayStatePending,
		Participant: participant,
		Nonce:       nonce,
		Calldata:    calldata,
	})
	testPutPendingReservation(t, s.store, pendingKey{participant: participant, nonce: nonce}, taskID)

	// Same calldata → 200, returns existing taskID (retry).
	t.Run("retry returns existing task", func(t *testing.T) {
		body := `{"chainID":42,"target":"0x0000000000000000000000000000000000000000","calldata":"0x` + calldataHex + `"}`
		w := tPost(s.handleRelay, "/api/relay", body)
		if w.Code != http.StatusOK {
			t.Fatalf("status: got %d, want %d; body: %s", w.Code, http.StatusOK, w.Body.String())
		}
		resp := respJSON(t, w)
		if resp["taskID"] != taskID {
			t.Errorf("taskID: got %v, want %s", resp["taskID"], taskID)
		}
	})

	// Different calldata, same participant+nonce → 409 conflict.
	t.Run("conflict on different calldata same nonce", func(t *testing.T) {
		calldata2 := tRelayCalldata(t, s.relayAddr, participant, big.NewInt(2e18), relayerFee, nonce)
		calldataHex2 := hex.EncodeToString(calldata2)
		body := `{"chainID":42,"target":"0x0000000000000000000000000000000000000000","calldata":"0x` + calldataHex2 + `"}`
		w := tPost(s.handleRelay, "/api/relay", body)
		if w.Code != http.StatusConflict {
			t.Fatalf("status: got %d, want %d; body: %s", w.Code, http.StatusConflict, w.Body.String())
		}
	})

	// Different nonce → passes duplicate check (fails later at fee validation
	// since the fake RPC returns errors, but the important thing is it's not
	// 200 or 409).
	t.Run("different nonce passes duplicate check", func(t *testing.T) {
		calldata3 := tRelayCalldata(t, s.relayAddr, participant, value, relayerFee, nonce+1)
		calldataHex3 := hex.EncodeToString(calldata3)
		body := `{"chainID":42,"target":"0x0000000000000000000000000000000000000000","calldata":"0x` + calldataHex3 + `"}`
		w := tPost(s.handleRelay, "/api/relay", body)
		if w.Code == http.StatusOK || w.Code == http.StatusConflict {
			t.Fatalf("expected neither 200 nor 409, got %d; body: %s", w.Code, w.Body.String())
		}
	})
}

func TestHandleRelayStatus(t *testing.T) {
	s := tServer(t)
	participant := common.HexToAddress("0xabcdef0123456789abcdef0123456789abcdef01")
	now := time.Now()

	// Pre-populate tasks.
	testPutTask(t, s.store, &taskEntry{
		TaskID:  "pending-task",
		ChainID: 1,
		State:   evmrelay.RelayStatePending,
		Phase:   taskPhaseActive,
		RelayTxHashes: []common.Hash{
			common.HexToHash("0xaaaa"),
		},
		Participant: participant,
		Nonce:       1,
	})

	minedAt := now.Add(-time.Minute)
	testPutTask(t, s.store, &taskEntry{
		TaskID:        "success-task",
		ChainID:       1,
		ResolvedAt:    &minedAt,
		State:         evmrelay.RelayStateSuccess,
		SuccessTxHash: common.HexToHash("0xbbbb"),
		RelayTxHashes: []common.Hash{
			common.HexToHash("0xbbbb"),
			common.HexToHash("0xdddd"),
		},
		Participant: participant,
		Nonce:       2,
	})
	testPutTask(t, s.store, &taskEntry{
		TaskID:        "failed-task",
		ChainID:       1,
		ResolvedAt:    &minedAt,
		State:         evmrelay.RelayStateFailed,
		FailureReason: failureReasonReverted,
		RelayTxHashes: []common.Hash{
			common.HexToHash("0xcccc"),
		},
		Participant: participant,
		Nonce:       3,
	})

	tests := []struct {
		name       string
		taskID     string
		wantCode   int
		wantState  string
		wantTxHash string
	}{
		{"unknown task", "no-such-task", http.StatusNotFound, "", ""},
		// Pending task with no chain configured — handler skips receipt
		// check and returns current state.
		{"pending task", "pending-task", http.StatusOK, "pending", ""},
		{"success task", "success-task", http.StatusOK, "success", common.HexToHash("0xbbbb").Hex()},
		{"failed task", "failed-task", http.StatusOK, "failed", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			r := httptest.NewRequest("GET", "/api/relay/"+tt.taskID, nil)
			r.SetPathValue("taskID", tt.taskID)
			s.handleRelayStatus(w, r)

			if w.Code != tt.wantCode {
				t.Fatalf("status: got %d, want %d; body: %s", w.Code, tt.wantCode, w.Body.String())
			}
			if tt.wantState != "" {
				resp := respJSON(t, w)
				if resp["state"] != tt.wantState {
					t.Errorf("state: got %v, want %s", resp["state"], tt.wantState)
				}
				if got, _ := resp["txHash"].(string); got != tt.wantTxHash {
					t.Errorf("txHash: got %q, want %q", got, tt.wantTxHash)
				}
			}
		})
	}
}

func TestHandleRelayTargetValidation(t *testing.T) {
	s := tServer(t)

	chainID := int64(42)
	gases := dexeth.VersionedGases[1]
	allowedTarget := common.HexToAddress("0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	s.chains[chainID] = &chainClient{
		ec: tFakeRPC(t),

		chainID:        big.NewInt(chainID),
		gases:          gases,
		allowedTargets: map[common.Address]bool{allowedTarget: true},
	}

	participant := common.HexToAddress("0xabcdef0123456789abcdef0123456789abcdef01")
	calldata := tRelayCalldata(t, s.relayAddr, participant, big.NewInt(1e18), big.NewInt(1e15), 1)
	calldataHex := hex.EncodeToString(calldata)

	// Request with disallowed target → 400.
	t.Run("disallowed target", func(t *testing.T) {
		body := `{"chainID":42,"target":"0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","calldata":"0x` + calldataHex + `"}`
		w := tPost(s.handleRelay, "/api/relay", body)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("status: got %d, want %d; body: %s", w.Code, http.StatusBadRequest, w.Body.String())
		}
		resp := respJSON(t, w)
		if !strings.Contains(resp["error"].(string), "target address not allowed") {
			t.Errorf("unexpected error: %v", resp["error"])
		}
	})

	// Request with allowed target should queue successfully.
	t.Run("allowed target passes check", func(t *testing.T) {
		body := `{"chainID":42,"target":"0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","calldata":"0x` + calldataHex + `"}`
		w := tPost(s.handleRelay, "/api/relay", body)
		if w.Code != http.StatusCreated {
			t.Fatalf("status: got %d, want %d; body: %s", w.Code, http.StatusCreated, w.Body.String())
		}
	})
}

func TestHandleRelayDeadlineValidation(t *testing.T) {
	s := tServer(t)

	chainID := int64(42)
	gases := dexeth.VersionedGases[1]
	target := common.HexToAddress("0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	s.chains[chainID] = &chainClient{
		ec: tFakeRPC(t),

		chainID:        big.NewInt(chainID),
		gases:          gases,
		allowedTargets: map[common.Address]bool{target: true},
	}

	participant := common.HexToAddress("0xabcdef0123456789abcdef0123456789abcdef01")

	// Expired deadline → 400.
	t.Run("expired deadline", func(t *testing.T) {
		calldata := tCalldataForRecipient(t, participant, s.relayAddr, big.NewInt(1e18), big.NewInt(1e15), 1, time.Now().Add(-time.Hour))
		calldataHex := hex.EncodeToString(calldata)
		body := `{"chainID":42,"target":"` + target.Hex() + `","calldata":"0x` + calldataHex + `"}`
		w := tPost(s.handleRelay, "/api/relay", body)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("status: got %d, want %d; body: %s", w.Code, http.StatusBadRequest, w.Body.String())
		}
		resp := respJSON(t, w)
		if !strings.Contains(resp["error"].(string), "deadline expired") {
			t.Errorf("unexpected error: %v", resp["error"])
		}
	})

	t.Run("deadline too soon for relay", func(t *testing.T) {
		calldata := tCalldataForRecipient(t, participant, s.relayAddr, big.NewInt(1e18), big.NewInt(1e15), 2, time.Now().Add(taskDeadlineMargin/2))
		calldataHex := hex.EncodeToString(calldata)
		body := `{"chainID":42,"target":"` + target.Hex() + `","calldata":"0x` + calldataHex + `"}`
		w := tPost(s.handleRelay, "/api/relay", body)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("status: got %d, want %d; body: %s", w.Code, http.StatusBadRequest, w.Body.String())
		}
		resp := respJSON(t, w)
		if !strings.Contains(resp["error"].(string), "deadline too soon for relay") {
			t.Errorf("unexpected error: %v", resp["error"])
		}
	})

	// Future deadline passes and should queue successfully.
	t.Run("future deadline passes check", func(t *testing.T) {
		calldata := tRelayCalldata(t, s.relayAddr, participant, big.NewInt(1e18), big.NewInt(1e15), 2)
		calldataHex := hex.EncodeToString(calldata)
		body := `{"chainID":42,"target":"` + target.Hex() + `","calldata":"0x` + calldataHex + `"}`
		w := tPost(s.handleRelay, "/api/relay", body)
		if w.Code != http.StatusCreated {
			t.Fatalf("status: got %d, want %d; body: %s", w.Code, http.StatusCreated, w.Body.String())
		}
	})
}

func TestHandleRelayReservationCleanup(t *testing.T) {
	s := tServer(t)

	chainID := int64(42)
	gases := dexeth.VersionedGases[1]
	target := common.HexToAddress("0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	s.chains[chainID] = &chainClient{
		ec: tFakeRPC(t),

		chainID:        big.NewInt(chainID),
		gases:          gases,
		allowedTargets: map[common.Address]bool{target: true},
	}

	participant := common.HexToAddress("0xabcdef0123456789abcdef0123456789abcdef01")
	calldata := tRelayCalldata(t, s.relayAddr, participant, big.NewInt(1e18), big.NewInt(1e15), 1)
	calldataHex := hex.EncodeToString(calldata)
	pk := pendingKey{participant: participant, nonce: 1}

	// The request should queue successfully, leaving the reservation held.
	body := `{"chainID":42,"target":"` + target.Hex() + `","calldata":"0x` + calldataHex + `"}`
	w := tPost(s.handleRelay, "/api/relay", body)

	if w.Code != http.StatusCreated {
		t.Fatalf("status: got %d, want %d; body: %s", w.Code, http.StatusCreated, w.Body.String())
	}

	// The pending slot should still be held while the task is queued.
	reserved := testHasPendingReservation(t, s.store, pk)
	if !reserved {
		t.Error("pending slot should remain reserved after enqueue")
	}
}

func TestHandleRelayFeeRecipientValidation(t *testing.T) {
	s := tServer(t)

	chainID := int64(42)
	gases := dexeth.VersionedGases[1]
	target := common.HexToAddress("0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	s.chains[chainID] = &chainClient{
		ec:             tFakeRPC(t),
		chainID:        big.NewInt(chainID),
		gases:          gases,
		allowedTargets: map[common.Address]bool{target: true},
	}

	participant := common.HexToAddress("0xabcdef0123456789abcdef0123456789abcdef01")

	tests := []struct {
		name         string
		feeRecipient common.Address
		wantErr      string
	}{
		{name: "zero fee recipient", feeRecipient: common.Address{}, wantErr: "feeRecipient must match relay address"},
		{name: "wrong fee recipient", feeRecipient: common.HexToAddress("0x2222222222222222222222222222222222222222"), wantErr: "does not match relay address"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			calldata := tCalldataForRecipient(t, participant, tt.feeRecipient, big.NewInt(1e18), big.NewInt(1e15), 1, time.Now().Add(time.Hour))
			body := `{"chainID":42,"target":"` + target.Hex() + `","calldata":"0x` + hex.EncodeToString(calldata) + `"}`
			w := tPost(s.handleRelay, "/api/relay", body)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("status: got %d, want %d; body: %s", w.Code, http.StatusBadRequest, w.Body.String())
			}
			resp := respJSON(t, w)
			if !strings.Contains(resp["error"].(string), tt.wantErr) {
				t.Fatalf("error: got %q, want substring %q", resp["error"], tt.wantErr)
			}
		})
	}
}

func TestHandleEstimateFee(t *testing.T) {
	s := tServer(t)

	chainID := int64(42)
	gases := dexeth.VersionedGases[1]
	allowedTarget := common.HexToAddress("0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	s.chains[chainID] = &chainClient{
		ec:             tFakeRPC(t),
		chainID:        big.NewInt(chainID),
		gases:          gases,
		allowedTargets: map[common.Address]bool{allowedTarget: true},
	}

	tests := []struct {
		name     string
		body     string
		wantCode int
		wantErr  string
	}{
		{
			name:     "invalid JSON",
			body:     `not json`,
			wantCode: http.StatusBadRequest,
			wantErr:  "invalid request body",
		},
		{
			name:     "zero numRedemptions",
			body:     `{"chainID":42,"target":"0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","numRedemptions":0}`,
			wantCode: http.StatusBadRequest,
			wantErr:  "numRedemptions must be > 0",
		},
		{
			name:     "too many redemptions",
			body:     `{"chainID":42,"target":"0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","numRedemptions":21}`,
			wantCode: http.StatusBadRequest,
			wantErr:  "numRedemptions must be <=",
		},
		{
			name:     "unknown chain",
			body:     `{"chainID":999,"target":"0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","numRedemptions":1}`,
			wantCode: http.StatusBadRequest,
			wantErr:  "chain 999 not configured",
		},
		{
			name:     "disallowed target",
			body:     `{"chainID":42,"target":"0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","numRedemptions":1}`,
			wantCode: http.StatusBadRequest,
			wantErr:  "target address not allowed",
		},
		{
			name:     "valid request but fake RPC fails",
			body:     `{"chainID":42,"target":"0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","numRedemptions":1}`,
			wantCode: http.StatusInternalServerError,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := tPost(s.handleEstimateFee, "/api/estimatefee", tt.body)
			if w.Code != tt.wantCode {
				t.Fatalf("status: got %d, want %d; body: %s", w.Code, tt.wantCode, w.Body.String())
			}
			if tt.wantErr != "" {
				resp := respJSON(t, w)
				errMsg, _ := resp["error"].(string)
				if !strings.Contains(errMsg, tt.wantErr) {
					t.Errorf("error: got %q, want substring %q", errMsg, tt.wantErr)
				}
			}
		})
	}
}

func TestProcessChainTasksLeavesQueuedTaskPendingOnRPCError(t *testing.T) {
	s := tServer(t)

	chainID := int64(42)
	s.chains[chainID] = &chainClient{
		ec:             tFakeRPC(t),
		chainID:        big.NewInt(chainID),
		gases:          dexeth.VersionedGases[1],
		allowedTargets: map[common.Address]bool{},
	}

	participant := common.HexToAddress("0xabcdef0123456789abcdef0123456789abcdef01")
	taskID := "pending-rpc-error"
	testPutTask(t, s.store, &taskEntry{
		TaskID:      taskID,
		ChainID:     chainID,
		ValidUntil:  time.Now().Add(time.Hour),
		State:       evmrelay.RelayStatePending,
		Phase:       taskPhaseQueued,
		Target:      common.HexToAddress("0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"),
		Calldata:    tRelayCalldata(t, s.relayAddr, participant, big.NewInt(1e18), big.NewInt(1e15), 1),
		Participant: participant,
		Nonce:       1,
	})
	testPutPendingReservation(t, s.store, pendingKey{participant: participant, nonce: 1}, taskID)

	err := s.processChainTasks(t.Context(), chainID)
	if err == nil {
		t.Fatal("expected RPC error")
	}
	snapshot, ok := s.store.snapshot(taskID)
	if !ok {
		t.Fatal("task missing after processing")
	}
	if got := snapshot.State; got != evmrelay.RelayStatePending {
		t.Fatalf("state: got %v, want pending", got)
	}
	if snapshot.Phase != taskPhaseQueued {
		t.Fatalf("phase: got %v, want queued", snapshot.Phase)
	}
	if !testHasPendingReservation(t, s.store, pendingKey{participant: participant, nonce: 1}) {
		t.Fatal("pending lock should remain held on RPC error")
	}
}

func TestProcessChainTasksUsesTimeout(t *testing.T) {
	s := tServer(t)

	const chainID = 42
	target := common.HexToAddress("0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	rpcAPI := &tEthRPC{blockHeader: true}
	s.chains[chainID] = tChainClientForTests(t, rpcAPI, chainID, target)

	participant := common.HexToAddress("0xabcdef0123456789abcdef0123456789abcdef01")
	taskID := "timed-out-queued-task"
	testPutTask(t, s.store, &taskEntry{
		TaskID:      taskID,
		ChainID:     chainID,
		ValidUntil:  time.Now().Add(time.Hour),
		State:       evmrelay.RelayStatePending,
		Phase:       taskPhaseQueued,
		Target:      target,
		Calldata:    tRelayCalldata(t, s.relayAddr, participant, big.NewInt(1e18), big.NewInt(1e15), 1),
		Participant: participant,
		Nonce:       1,
	})
	testPutPendingReservation(t, s.store, pendingKey{participant: participant, nonce: 1}, taskID)

	start := time.Now()
	err := s.processChainTasksWithTimeout(context.Background(), chainID, 20*time.Millisecond)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected context deadline exceeded, got %v", err)
	}
	if elapsed := time.Since(start); elapsed > 250*time.Millisecond {
		t.Fatalf("processChainTasksWithTimeout took too long: %v", elapsed)
	}

	snapshot, ok := s.store.snapshot(taskID)
	if !ok {
		t.Fatal("task missing after timed-out processing")
	}
	if snapshot.State != evmrelay.RelayStatePending {
		t.Fatalf("state: got %s, want %s", snapshot.State, evmrelay.RelayStatePending)
	}
	if snapshot.Phase != taskPhaseQueued {
		t.Fatalf("phase: got %s, want %s", snapshot.Phase, taskPhaseQueued)
	}
}

func TestRateLimiting(t *testing.T) {
	s := tServer(t)
	relayHandler := s.rateLimitHandler(s.handleRelay)
	estimateHandler := s.rateLimitHandler(s.handleEstimateFee)

	// First 20 requests (burst) should not get 429.
	for i := 0; i < 20; i++ {
		body := `{"chainID":999}`
		w := tPost(relayHandler, "/api/relay", body)
		if w.Code == http.StatusTooManyRequests {
			t.Fatalf("request %d got 429, expected other error", i)
		}
	}

	// 21st request should get 429.
	body := `{"chainID":999}`
	w := tPost(relayHandler, "/api/relay", body)
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d; body: %s", w.Code, w.Body.String())
	}

	// handleEstimateFee should also be rate limited.
	w = tPost(estimateHandler, "/api/estimatefee", body)
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("estimatefee: expected 429, got %d; body: %s", w.Code, w.Body.String())
	}
}

func TestShouldAttemptRedeemReplacement(t *testing.T) {
	now := time.Now()
	nonce := uint64(7)
	tests := []struct {
		name  string
		entry taskEntry
		want  bool
	}{
		{
			name: "eligible active task",
			entry: taskEntry{
				State:         evmrelay.RelayStatePending,
				Phase:         taskPhaseActive,
				ValidUntil:    now.Add(time.Hour),
				RelayerNonce:  &nonce,
				RelayTxHashes: []common.Hash{common.HexToHash("0xaaaa")},
				RelayTxData:   []byte{1, 2, 3},
				FirstSentAt:   ptrTime(now.Add(-3 * time.Minute)),
			},
			want: true,
		},
		{
			name: "too soon after first send",
			entry: taskEntry{
				State:         evmrelay.RelayStatePending,
				Phase:         taskPhaseActive,
				ValidUntil:    now.Add(time.Hour),
				RelayerNonce:  &nonce,
				RelayTxHashes: []common.Hash{common.HexToHash("0xaaaa")},
				RelayTxData:   []byte{1},
				FirstSentAt:   ptrTime(now.Add(-time.Minute)),
			},
		},
		{
			name: "replacement cap reached",
			entry: taskEntry{
				State:        evmrelay.RelayStatePending,
				Phase:        taskPhaseActive,
				ValidUntil:   now.Add(time.Hour),
				RelayerNonce: &nonce,
				RelayTxHashes: []common.Hash{
					common.HexToHash("0xaaaa"),
					common.HexToHash("0xbbbb"),
					common.HexToHash("0xcccc"),
					common.HexToHash("0xdddd"),
				},
				RelayTxData: []byte{1},
				FirstSentAt: ptrTime(now.Add(-3 * time.Minute)),
			},
		},
		{
			name: "already canceling",
			entry: taskEntry{
				State:         evmrelay.RelayStatePending,
				Phase:         taskPhaseCanceling,
				ValidUntil:    now.Add(time.Hour),
				RelayerNonce:  &nonce,
				RelayTxHashes: []common.Hash{common.HexToHash("0xaaaa")},
				RelayTxData:   []byte{1},
				FirstSentAt:   ptrTime(now.Add(-3 * time.Minute)),
			},
		},
		{
			name: "expired task should cancel instead",
			entry: taskEntry{
				State:         evmrelay.RelayStatePending,
				Phase:         taskPhaseActive,
				ValidUntil:    now.Add(-time.Minute),
				RelayerNonce:  &nonce,
				RelayTxHashes: []common.Hash{common.HexToHash("0xaaaa")},
				RelayTxData:   []byte{1},
				FirstSentAt:   ptrTime(now.Add(-3 * time.Minute)),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldAttemptRedeemReplacement(&tt.entry, now); got != tt.want {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestShouldAttemptCancelReplacement(t *testing.T) {
	now := time.Now()
	nonce := uint64(7)
	tests := []struct {
		name  string
		entry taskEntry
		want  bool
	}{
		{
			name: "eligible canceling task",
			entry: taskEntry{
				State:          evmrelay.RelayStatePending,
				Phase:          taskPhaseCanceling,
				RelayerNonce:   &nonce,
				CancelTxHashes: []common.Hash{common.HexToHash("0xbbbb")},
				CancelTxData:   []byte{1, 2, 3},
				LastReplacedAt: ptrTime(now.Add(-taskReplacePeriod - time.Minute)),
			},
			want: true,
		},
		{
			name: "too soon after last cancel replacement",
			entry: taskEntry{
				State:          evmrelay.RelayStatePending,
				Phase:          taskPhaseCanceling,
				RelayerNonce:   &nonce,
				CancelTxHashes: []common.Hash{common.HexToHash("0xbbbb")},
				CancelTxData:   []byte{1},
				LastReplacedAt: ptrTime(now.Add(-time.Minute)),
			},
		},
		{
			name: "replacement cap reached",
			entry: taskEntry{
				State:        evmrelay.RelayStatePending,
				Phase:        taskPhaseCanceling,
				RelayerNonce: &nonce,
				CancelTxHashes: []common.Hash{
					common.HexToHash("0xbbbb"),
					common.HexToHash("0xcccc"),
					common.HexToHash("0xdddd"),
					common.HexToHash("0xeeee"),
				},
				CancelTxData:   []byte{1},
				LastReplacedAt: ptrTime(now.Add(-taskReplacePeriod - time.Minute)),
			},
		},
		{
			name: "not canceling",
			entry: taskEntry{
				State:          evmrelay.RelayStatePending,
				Phase:          taskPhaseActive,
				RelayerNonce:   &nonce,
				CancelTxHashes: []common.Hash{common.HexToHash("0xbbbb")},
				CancelTxData:   []byte{1},
				LastReplacedAt: ptrTime(now.Add(-taskReplacePeriod - time.Minute)),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldAttemptCancelReplacement(&tt.entry, now); got != tt.want {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestReconcileActiveTaskRedeemReceiptWinsOverCancel(t *testing.T) {
	const chainID = 42
	target := common.HexToAddress("0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	rpcAPI := &tEthRPC{
		receipts: make(map[common.Hash]*types.Receipt),
	}
	cc := tChainClientForTests(t, rpcAPI, chainID, target)
	s := tServer(t)
	s.relayAddr = cc.relayAddr
	s.chains[chainID] = cc

	participant := common.HexToAddress("0xabcdef0123456789abcdef0123456789abcdef01")
	redeemHash := common.HexToHash("0xaaaa")
	cancelHash := common.HexToHash("0xbbbb")
	now := time.Now()
	rpcAPI.receipts[redeemHash] = tTestReceipt(redeemHash, 1)
	rpcAPI.receipts[cancelHash] = tTestReceipt(cancelHash, 1)

	testPutTask(t, s.store, &taskEntry{
		TaskID:        "task-success",
		ChainID:       chainID,
		ValidUntil:    now.Add(time.Hour),
		State:         evmrelay.RelayStatePending,
		Phase:         taskPhaseCanceling,
		Participant:   participant,
		Nonce:         1,
		Target:        target,
		RelayTxHashes: []common.Hash{redeemHash},
		CancelTxHashes: []common.Hash{
			cancelHash,
		},
	})

	snapshot, ok := s.store.snapshot("task-success")
	if !ok {
		t.Fatal("task missing")
	}
	changed, err := s.reconcileActiveTask(context.Background(), &snapshot)
	if err != nil {
		t.Fatalf("reconcileActiveTask error: %v", err)
	}
	if !changed {
		t.Fatal("expected task to change")
	}

	updated, ok := s.store.snapshot("task-success")
	if !ok {
		t.Fatal("updated task missing")
	}
	if updated.State != evmrelay.RelayStateSuccess {
		t.Fatalf("state: got %s, want %s", updated.State, evmrelay.RelayStateSuccess)
	}
	if updated.SuccessTxHash != redeemHash {
		t.Fatalf("success tx hash: got %s, want %s", updated.SuccessTxHash.Hex(), redeemHash.Hex())
	}
}

func TestReconcileActiveTaskCancelReceiptMarksCanceled(t *testing.T) {
	const chainID = 42
	target := common.HexToAddress("0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	cancelHash := common.HexToHash("0xbbbb")
	rpcAPI := &tEthRPC{
		receipts: map[common.Hash]*types.Receipt{
			cancelHash: tTestReceipt(cancelHash, 1),
		},
	}
	cc := tChainClientForTests(t, rpcAPI, chainID, target)
	s := tServer(t)
	s.relayAddr = cc.relayAddr
	s.chains[chainID] = cc

	testPutTask(t, s.store, &taskEntry{
		TaskID:        "task-canceled",
		ChainID:       chainID,
		ValidUntil:    time.Now().Add(-time.Minute),
		State:         evmrelay.RelayStatePending,
		Phase:         taskPhaseCanceling,
		Participant:   common.HexToAddress("0xabcdef0123456789abcdef0123456789abcdef01"),
		Nonce:         1,
		Target:        target,
		RelayTxHashes: []common.Hash{common.HexToHash("0xaaaa")},
		CancelTxHashes: []common.Hash{
			cancelHash,
		},
	})

	snapshot, _ := s.store.snapshot("task-canceled")
	changed, err := s.reconcileActiveTask(context.Background(), &snapshot)
	if err != nil {
		t.Fatalf("reconcileActiveTask error: %v", err)
	}
	if !changed {
		t.Fatal("expected task to change")
	}

	updated, _ := s.store.snapshot("task-canceled")
	if updated.State != evmrelay.RelayStateFailed {
		t.Fatalf("state: got %s, want %s", updated.State, evmrelay.RelayStateFailed)
	}
	if updated.FailureReason != failureReasonCanceled {
		t.Fatalf("failure reason: got %q, want %q", updated.FailureReason, failureReasonCanceled)
	}
}

func TestReconcileActiveTaskReplacesRedeem(t *testing.T) {
	const chainID = 42
	target := common.HexToAddress("0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	rpcAPI := &tEthRPC{
		header:      tTestHeader(big.NewInt(1_000_000_000)),
		tipCap:      big.NewInt(2_000_000_000),
		estimateGas: dexeth.VersionedGases[1].SignedRedeemN(1),
	}
	cc := tChainClientForTests(t, rpcAPI, chainID, target)
	s := tServer(t)
	s.relayAddr = cc.relayAddr
	s.chains[chainID] = cc

	participant := common.HexToAddress("0xabcdef0123456789abcdef0123456789abcdef01")
	calldata := tRelayCalldata(t, s.relayAddr, participant, big.NewInt(1e18), big.NewInt(1e17), 1)
	prevTx, prevRaw := tSignedDynamicTx(t, cc, target, calldata, 7, cc.gases.SignedRedeemN(1), big.NewInt(2_000_000_000), big.NewInt(10_000_000_000))
	relayerNonce := uint64(7)

	testPutTask(t, s.store, &taskEntry{
		TaskID:        "task-replace",
		ChainID:       chainID,
		ValidUntil:    time.Now().Add(time.Hour),
		State:         evmrelay.RelayStatePending,
		Phase:         taskPhaseActive,
		Participant:   participant,
		Nonce:         1,
		Target:        target,
		Calldata:      calldata,
		RelayerNonce:  &relayerNonce,
		RelayTxHashes: []common.Hash{prevTx.Hash()},
		RelayTxData:   prevRaw,
		FirstSentAt:   ptrTime(time.Now().Add(-taskReplacePeriod - time.Minute)),
	})

	snapshot, _ := s.store.snapshot("task-replace")
	changed, err := s.reconcileActiveTask(context.Background(), &snapshot)
	if err != nil {
		t.Fatalf("reconcileActiveTask error: %v", err)
	}
	if !changed {
		t.Fatal("expected task to change")
	}
	if len(rpcAPI.sentTxs) != 1 {
		t.Fatalf("sent txs: got %d, want 1", len(rpcAPI.sentTxs))
	}

	updated, _ := s.store.snapshot("task-replace")
	if len(updated.RelayTxHashes) != 2 {
		t.Fatalf("relay tx hashes len: got %d, want 2", len(updated.RelayTxHashes))
	}

	replaced, err := preparedTxFromRaw(updated.RelayTxData)
	if err != nil {
		t.Fatalf("preparedTxFromRaw: %v", err)
	}
	if replaced.tx.GasTipCap().Cmp(prevTx.GasTipCap()) <= 0 {
		t.Fatalf("replacement tip cap: got %s, want > %s", replaced.tx.GasTipCap(), prevTx.GasTipCap())
	}
	if replaced.tx.GasFeeCap().Cmp(prevTx.GasFeeCap()) <= 0 {
		t.Fatalf("replacement fee cap: got %s, want > %s", replaced.tx.GasFeeCap(), prevTx.GasFeeCap())
	}
}

func TestReconcileActiveTaskBroadcastsBumpedCancellation(t *testing.T) {
	const chainID = 42
	target := common.HexToAddress("0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	rpcAPI := &tEthRPC{
		header:      tTestHeader(big.NewInt(1_000_000_000)),
		tipCap:      big.NewInt(2_000_000_000),
		estimateGas: dexeth.VersionedGases[1].SignedRedeemN(1),
	}
	cc := tChainClientForTests(t, rpcAPI, chainID, target)
	s := tServer(t)
	s.relayAddr = cc.relayAddr
	s.chains[chainID] = cc

	participant := common.HexToAddress("0xabcdef0123456789abcdef0123456789abcdef01")
	calldata := tRelayCalldata(t, s.relayAddr, participant, big.NewInt(1e18), big.NewInt(1e17), 1)
	prevTx, prevRaw := tSignedDynamicTx(t, cc, target, calldata, 11, cc.gases.SignedRedeemN(1), big.NewInt(5_000_000_000), big.NewInt(20_000_000_000))
	relayerNonce := uint64(11)

	testPutTask(t, s.store, &taskEntry{
		TaskID:        "task-cancel",
		ChainID:       chainID,
		ValidUntil:    time.Now().Add(-time.Minute),
		State:         evmrelay.RelayStatePending,
		Phase:         taskPhaseActive,
		Participant:   participant,
		Nonce:         1,
		Target:        target,
		Calldata:      calldata,
		RelayerNonce:  &relayerNonce,
		RelayTxHashes: []common.Hash{prevTx.Hash()},
		RelayTxData:   prevRaw,
	})

	snapshot, _ := s.store.snapshot("task-cancel")
	changed, err := s.reconcileActiveTask(context.Background(), &snapshot)
	if err != nil {
		t.Fatalf("reconcileActiveTask error: %v", err)
	}
	if !changed {
		t.Fatal("expected task to change")
	}
	if len(rpcAPI.sentTxs) != 1 {
		t.Fatalf("sent txs: got %d, want 1", len(rpcAPI.sentTxs))
	}

	updated, _ := s.store.snapshot("task-cancel")
	if updated.Phase != taskPhaseCanceling {
		t.Fatalf("phase: got %s, want %s", updated.Phase, taskPhaseCanceling)
	}
	cancelTx, err := preparedTxFromRaw(updated.CancelTxData)
	if err != nil {
		t.Fatalf("preparedTxFromRaw: %v", err)
	}
	if cancelTx.tx.Nonce() != prevTx.Nonce() {
		t.Fatalf("cancel nonce: got %d, want %d", cancelTx.tx.Nonce(), prevTx.Nonce())
	}
	if cancelTx.tx.GasTipCap().Cmp(prevTx.GasTipCap()) <= 0 {
		t.Fatalf("cancel tip cap: got %s, want > %s", cancelTx.tx.GasTipCap(), prevTx.GasTipCap())
	}
	if cancelTx.tx.GasFeeCap().Cmp(prevTx.GasFeeCap()) <= 0 {
		t.Fatalf("cancel fee cap: got %s, want > %s", cancelTx.tx.GasFeeCap(), prevTx.GasFeeCap())
	}
}

func TestReconcileActiveTaskReplacesCancellation(t *testing.T) {
	const chainID = 42
	target := common.HexToAddress("0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	rpcAPI := &tEthRPC{
		header: tTestHeader(big.NewInt(1_000_000_000)),
		tipCap: big.NewInt(2_000_000_000),
	}
	cc := tChainClientForTests(t, rpcAPI, chainID, target)
	s := tServer(t)
	s.relayAddr = cc.relayAddr
	s.chains[chainID] = cc

	participant := common.HexToAddress("0xabcdef0123456789abcdef0123456789abcdef01")
	redeemHash := common.HexToHash("0xaaaa")
	prevCancelTx, prevCancelRaw := tSignedDynamicTx(t, cc, cc.relayAddr, nil, 11, 21_000, big.NewInt(5_000_000_000), big.NewInt(20_000_000_000))
	relayerNonce := uint64(11)

	testPutTask(t, s.store, &taskEntry{
		TaskID:        "task-replace-cancel",
		ChainID:       chainID,
		ValidUntil:    time.Now().Add(-time.Minute),
		State:         evmrelay.RelayStatePending,
		Phase:         taskPhaseCanceling,
		Participant:   participant,
		Nonce:         1,
		Target:        target,
		RelayerNonce:  &relayerNonce,
		RelayTxHashes: []common.Hash{redeemHash},
		CancelTxHashes: []common.Hash{
			prevCancelTx.Hash(),
		},
		CancelTxData:   prevCancelRaw,
		LastReplacedAt: ptrTime(time.Now().Add(-taskReplacePeriod - time.Minute)),
	})

	snapshot, _ := s.store.snapshot("task-replace-cancel")
	changed, err := s.reconcileActiveTask(context.Background(), &snapshot)
	if err != nil {
		t.Fatalf("reconcileActiveTask error: %v", err)
	}
	if !changed {
		t.Fatal("expected task to change")
	}
	if len(rpcAPI.sentTxs) != 1 {
		t.Fatalf("sent txs: got %d, want 1", len(rpcAPI.sentTxs))
	}

	updated, _ := s.store.snapshot("task-replace-cancel")
	if len(updated.CancelTxHashes) != 2 {
		t.Fatalf("cancel tx hashes len: got %d, want 2", len(updated.CancelTxHashes))
	}

	replaced, err := preparedTxFromRaw(updated.CancelTxData)
	if err != nil {
		t.Fatalf("preparedTxFromRaw: %v", err)
	}
	if replaced.tx.GasTipCap().Cmp(prevCancelTx.GasTipCap()) <= 0 {
		t.Fatalf("replacement cancel tip cap: got %s, want > %s", replaced.tx.GasTipCap(), prevCancelTx.GasTipCap())
	}
	if replaced.tx.GasFeeCap().Cmp(prevCancelTx.GasFeeCap()) <= 0 {
		t.Fatalf("replacement cancel fee cap: got %s, want > %s", replaced.tx.GasFeeCap(), prevCancelTx.GasFeeCap())
	}
}

func TestLogHandlerBodyReadError(t *testing.T) {
	s := tServer(t)

	var handlerBodyLen int
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		handlerBodyLen = len(b)
		w.WriteHeader(http.StatusOK)
	})

	handler := s.logHandler(inner)

	// Simulate a request with a body that returns an error by using
	// a reader that fails immediately.
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/test", &errReader{})
	handler(w, r)

	// The downstream handler should receive an empty body (not a nil or
	// consumed body that could cause a panic).
	if handlerBodyLen != 0 {
		t.Fatalf("expected empty body after read error, got %d bytes", handlerBodyLen)
	}
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

// errReader is an io.ReadCloser that always returns an error on Read.
type errReader struct{}

func (e *errReader) Read([]byte) (int, error) { return 0, errors.New("read error") }
func (e *errReader) Close() error             { return nil }

func TestReconcileActiveTaskStillPendingNoReceipt(t *testing.T) {
	const chainID = 42
	target := common.HexToAddress("0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	rpcAPI := &tEthRPC{
		header: tTestHeader(big.NewInt(1_000_000_000)),
		tipCap: big.NewInt(2_000_000_000),
	}
	cc := tChainClientForTests(t, rpcAPI, chainID, target)
	s := tServer(t)
	s.relayAddr = cc.relayAddr
	s.chains[chainID] = cc

	participant := common.HexToAddress("0xabcdef0123456789abcdef0123456789abcdef01")
	relayerNonce := uint64(5)

	calldata := tRelayCalldata(t, cc.relayAddr, participant, big.NewInt(1e18), big.NewInt(1e15), 1)

	_, rawTx := tSignedDynamicTx(t, cc, target, calldata, relayerNonce, 100_000, big.NewInt(1e9), big.NewInt(5e9))
	relayTxHash := common.HexToHash("0xaaaa")

	now := time.Now()
	testPutTask(t, s.store, &taskEntry{
		TaskID:        "task-pending",
		ChainID:       chainID,
		ValidUntil:    now.Add(time.Hour),
		State:         evmrelay.RelayStatePending,
		Phase:         taskPhaseActive,
		Participant:   participant,
		Nonce:         1,
		Target:        target,
		Calldata:      calldata,
		RelayerNonce:  &relayerNonce,
		RelayTxHashes: []common.Hash{relayTxHash},
		RelayTxData:   rawTx,
		FirstSentAt:   ptrTime(now.Add(-10 * time.Second)),
		LastSentAt:    ptrTime(now.Add(-1 * time.Second)),
	})

	snapshot, _ := s.store.snapshot("task-pending")
	changed, err := s.reconcileActiveTask(context.Background(), &snapshot)
	if err != nil {
		t.Fatalf("reconcileActiveTask error: %v", err)
	}
	// No receipt found, not yet time for replacement or rebroadcast,
	// so nothing should change.
	if changed {
		t.Fatal("expected no change when receipt is not found and no replacement/rebroadcast is due")
	}

	updated, ok := s.store.snapshot("task-pending")
	if !ok {
		t.Fatal("task not found after reconcile")
	}
	if updated.State != evmrelay.RelayStatePending {
		t.Fatalf("state: got %s, want %s", updated.State, evmrelay.RelayStatePending)
	}
	if updated.Phase != taskPhaseActive {
		t.Fatalf("phase: got %s, want %s", updated.Phase, taskPhaseActive)
	}
}

func TestReconcileActiveTaskRevertedReceipt(t *testing.T) {
	const chainID = 42
	target := common.HexToAddress("0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")

	relayTxHash := common.HexToHash("0xaaaa")
	rpcAPI := &tEthRPC{
		header:   tTestHeader(big.NewInt(1_000_000_000)),
		tipCap:   big.NewInt(2_000_000_000),
		receipts: map[common.Hash]*types.Receipt{relayTxHash: tTestReceipt(relayTxHash, 0)},
	}
	cc := tChainClientForTests(t, rpcAPI, chainID, target)
	s := tServer(t)
	s.relayAddr = cc.relayAddr
	s.chains[chainID] = cc

	participant := common.HexToAddress("0xabcdef0123456789abcdef0123456789abcdef01")
	relayerNonce := uint64(5)
	calldata := tRelayCalldata(t, cc.relayAddr, participant, big.NewInt(1e18), big.NewInt(1e15), 1)
	_, rawTx := tSignedDynamicTx(t, cc, target, calldata, relayerNonce, 100_000, big.NewInt(1e9), big.NewInt(5e9))

	testPutTask(t, s.store, &taskEntry{
		TaskID:        "task-reverted",
		ChainID:       chainID,
		ValidUntil:    time.Now().Add(time.Hour),
		State:         evmrelay.RelayStatePending,
		Phase:         taskPhaseActive,
		Participant:   participant,
		Nonce:         1,
		Target:        target,
		Calldata:      calldata,
		RelayerNonce:  &relayerNonce,
		RelayTxHashes: []common.Hash{relayTxHash},
		RelayTxData:   rawTx,
		FirstSentAt:   ptrTime(time.Now().Add(-time.Minute)),
		LastSentAt:    ptrTime(time.Now().Add(-10 * time.Second)),
	})

	snapshot, _ := s.store.snapshot("task-reverted")
	changed, err := s.reconcileActiveTask(context.Background(), &snapshot)
	if err != nil {
		t.Fatalf("reconcileActiveTask error: %v", err)
	}
	if !changed {
		t.Fatal("expected change for reverted receipt")
	}

	updated, ok := s.store.snapshot("task-reverted")
	if !ok {
		t.Fatal("task not found")
	}
	if updated.State != evmrelay.RelayStateFailed {
		t.Fatalf("state: got %s, want %s", updated.State, evmrelay.RelayStateFailed)
	}
	if updated.FailureReason != failureReasonReverted {
		t.Fatalf("reason: got %s, want %s", updated.FailureReason, failureReasonReverted)
	}
}

func TestPromoteQueuedTaskHappyPath(t *testing.T) {
	const chainID = 42
	target := common.HexToAddress("0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	rpcAPI := &tEthRPC{
		header:      tTestHeader(big.NewInt(1_000_000_000)),
		tipCap:      big.NewInt(2_000_000_000),
		estimateGas: 80_000,
	}
	cc := tChainClientForTests(t, rpcAPI, chainID, target)
	s := tServer(t)
	s.relayAddr = cc.relayAddr
	s.chains[chainID] = cc

	participant := common.HexToAddress("0xabcdef0123456789abcdef0123456789abcdef01")
	calldata := tRelayCalldata(t, cc.relayAddr, participant, big.NewInt(1e18), big.NewInt(1e16), 1)

	entry := &taskEntry{
		TaskID:      "task-promote",
		ChainID:     chainID,
		QueueSeq:    1,
		ValidUntil:  time.Now().Add(10 * time.Minute),
		State:       evmrelay.RelayStatePending,
		Phase:       taskPhaseQueued,
		Participant: participant,
		Nonce:       1,
		Target:      target,
		Calldata:    calldata,
	}
	testPutTask(t, s.store, entry)

	snapshot, ok := s.store.nextQueuedTask(chainID)
	if !ok {
		t.Fatal("no queued task found")
	}

	changed, err := s.promoteQueuedTask(context.Background(), &snapshot)
	if err != nil {
		t.Fatalf("promoteQueuedTask error: %v", err)
	}
	if !changed {
		t.Fatal("expected task to change")
	}
	if len(rpcAPI.sentTxs) != 1 {
		t.Fatalf("sent txs: got %d, want 1", len(rpcAPI.sentTxs))
	}

	updated, ok := s.store.snapshot("task-promote")
	if !ok {
		t.Fatal("task not found")
	}
	if updated.Phase != taskPhaseActive {
		t.Fatalf("phase: got %s, want %s", updated.Phase, taskPhaseActive)
	}
	if updated.State != evmrelay.RelayStatePending {
		t.Fatalf("state: got %s, want %s", updated.State, evmrelay.RelayStatePending)
	}
	if updated.RelayerNonce == nil {
		t.Fatal("relayer nonce should be set")
	}
	if len(updated.RelayTxHashes) != 1 {
		t.Fatalf("relay tx hashes: got %d, want 1", len(updated.RelayTxHashes))
	}
}

func TestPromoteQueuedTaskExpired(t *testing.T) {
	const chainID = 42
	target := common.HexToAddress("0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	rpcAPI := &tEthRPC{
		header: tTestHeader(big.NewInt(1_000_000_000)),
		tipCap: big.NewInt(2_000_000_000),
	}
	cc := tChainClientForTests(t, rpcAPI, chainID, target)
	s := tServer(t)
	s.relayAddr = cc.relayAddr
	s.chains[chainID] = cc

	participant := common.HexToAddress("0xabcdef0123456789abcdef0123456789abcdef01")
	calldata := tRelayCalldata(t, cc.relayAddr, participant, big.NewInt(1e18), big.NewInt(1e16), 1)

	entry := &taskEntry{
		TaskID:      "task-expired",
		ChainID:     chainID,
		QueueSeq:    1,
		ValidUntil:  time.Now().Add(-time.Minute),
		State:       evmrelay.RelayStatePending,
		Phase:       taskPhaseQueued,
		Participant: participant,
		Nonce:       1,
		Target:      target,
		Calldata:    calldata,
	}
	testPutTask(t, s.store, entry)

	snapshot := *entry
	changed, err := s.promoteQueuedTask(context.Background(), &snapshot)
	if err != nil {
		t.Fatalf("promoteQueuedTask error: %v", err)
	}
	if !changed {
		t.Fatal("expected task to change (marked failed)")
	}

	updated, ok := s.store.snapshot("task-expired")
	if !ok {
		t.Fatal("task not found")
	}
	if updated.State != evmrelay.RelayStateFailed {
		t.Fatalf("state: got %s, want %s", updated.State, evmrelay.RelayStateFailed)
	}
	if updated.FailureReason != failureReasonExpired {
		t.Fatalf("reason: got %s, want %s", updated.FailureReason, failureReasonExpired)
	}
}

func TestClientIP(t *testing.T) {
	trusted := map[string]struct{}{"10.0.0.1": {}}

	tests := []struct {
		name           string
		remoteAddr     string
		realIP         string
		forwarded      string
		trustedProxies map[string]struct{}
		want           string
	}{
		{
			name:       "no proxies uses RemoteAddr",
			remoteAddr: "192.168.1.1:12345",
			want:       "192.168.1.1",
		},
		{
			name:           "untrusted proxy ignores headers",
			remoteAddr:     "192.168.1.1:12345",
			realIP:         "1.2.3.4",
			trustedProxies: trusted,
			want:           "192.168.1.1",
		},
		{
			name:           "trusted proxy uses X-Real-IP",
			remoteAddr:     "10.0.0.1:12345",
			realIP:         "1.2.3.4",
			trustedProxies: trusted,
			want:           "1.2.3.4",
		},
		{
			name:           "trusted proxy uses X-Forwarded-For first entry",
			remoteAddr:     "10.0.0.1:12345",
			forwarded:      "5.6.7.8, 10.0.0.1",
			trustedProxies: trusted,
			want:           "5.6.7.8",
		},
		{
			name:           "trusted proxy X-Real-IP takes precedence over X-Forwarded-For",
			remoteAddr:     "10.0.0.1:12345",
			realIP:         "1.2.3.4",
			forwarded:      "5.6.7.8",
			trustedProxies: trusted,
			want:           "1.2.3.4",
		},
		{
			name:           "invalid X-Real-IP falls through to X-Forwarded-For",
			remoteAddr:     "10.0.0.1:12345",
			realIP:         "not-an-ip",
			forwarded:      "5.6.7.8",
			trustedProxies: trusted,
			want:           "5.6.7.8",
		},
		{
			name:           "invalid X-Real-IP and X-Forwarded-For falls back to RemoteAddr",
			remoteAddr:     "10.0.0.1:12345",
			realIP:         "garbage",
			forwarded:      "also-garbage",
			trustedProxies: trusted,
			want:           "10.0.0.1",
		},
		{
			name:           "empty proxies config always uses RemoteAddr",
			remoteAddr:     "10.0.0.1:12345",
			realIP:         "1.2.3.4",
			trustedProxies: nil,
			want:           "10.0.0.1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest("GET", "/", nil)
			r.RemoteAddr = tt.remoteAddr
			if tt.realIP != "" {
				r.Header.Set("X-Real-IP", tt.realIP)
			}
			if tt.forwarded != "" {
				r.Header.Set("X-Forwarded-For", tt.forwarded)
			}
			got := clientIP(r, tt.trustedProxies)
			if got != tt.want {
				t.Errorf("clientIP = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestHandleRelayQueueFull(t *testing.T) {
	s := tServer(t)

	chainID := int64(42)
	target := common.HexToAddress("0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	s.chains[chainID] = &chainClient{
		ec:             tFakeRPC(t),
		chainID:        big.NewInt(chainID),
		gases:          dexeth.VersionedGases[1],
		allowedTargets: map[common.Address]bool{target: true},
	}

	participant := common.HexToAddress("0xabcdef0123456789abcdef0123456789abcdef01")

	// Fill the queue to the limit.
	for i := 0; i < maxQueuedTasksPerChain; i++ {
		testPutTask(t, s.store, &taskEntry{
			TaskID:      fmt.Sprintf("fill-%d", i),
			ChainID:     chainID,
			QueueSeq:    uint64(i + 1),
			ValidUntil:  time.Now().Add(time.Hour),
			State:       evmrelay.RelayStatePending,
			Phase:       taskPhaseQueued,
			Participant: common.HexToAddress(fmt.Sprintf("0x%040x", i+0x1000)),
			Nonce:       uint64(i),
			Target:      target,
		})
	}

	// Next submission should get 503.
	calldata := tRelayCalldata(t, s.relayAddr, participant, big.NewInt(1e18), big.NewInt(1e15), 9999)
	calldataHex := hex.EncodeToString(calldata)
	body := fmt.Sprintf(`{"chainID":%d,"target":"%s","calldata":"0x%s"}`, chainID, target.Hex(), calldataHex)
	w := tPost(s.handleRelay, "/api/relay", body)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status: got %d, want %d; body: %s", w.Code, http.StatusServiceUnavailable, w.Body.String())
	}
	resp := respJSON(t, w)
	errMsg, _ := resp["error"].(string)
	if !strings.Contains(errMsg, "queue is full") {
		t.Errorf("error: got %q, want substring 'queue is full'", errMsg)
	}
}

func ptrTime(t time.Time) *time.Time {
	return &t
}
