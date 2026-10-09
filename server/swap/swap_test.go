// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package swap

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/calc"
	"decred.org/dcrdex/dex/encode"
	"decred.org/dcrdex/dex/msgjson"
	"decred.org/dcrdex/dex/order"
	"decred.org/dcrdex/server/account"
	"decred.org/dcrdex/server/asset"
	"decred.org/dcrdex/server/coinlock"
	"decred.org/dcrdex/server/comms"
	"decred.org/dcrdex/server/db"
	"decred.org/dcrdex/server/matcher"
	"decred.org/dcrdex/server/mesh"
	"decred.org/dcrdex/server/meshevents"
	"github.com/decred/dcrd/dcrec/secp256k1/v4"
	"github.com/decred/dcrd/dcrec/secp256k1/v4/ecdsa"
)

const (
	ABCID  = 123
	XYZID  = 789
	ACCTID = 456
)

var (
	testCtx      context.Context
	acctTemplate = account.AccountID{
		0x22, 0x4c, 0xba, 0xaa, 0xfa, 0x80, 0xbf, 0x3b, 0xd1, 0xff, 0x73, 0x15,
		0x90, 0xbc, 0xbd, 0xda, 0x5a, 0x76, 0xf9, 0x1e, 0x60, 0xa1, 0x56, 0x99,
		0x46, 0x34, 0xe9, 0x1c, 0xaa, 0xaa, 0xaa, 0xaa,
	}
	acctCounter      uint32
	dexPrivKey       *secp256k1.PrivateKey
	tBcastTimeout    time.Duration
	txWaitExpiration time.Duration
)

type tUser struct {
	sig      []byte
	sigHex   string
	acct     account.AccountID
	addr     string
	lbl      string
	matchIDs []order.MatchID
}

func tickMempool() {
	time.Sleep(fastRecheckInterval * 3 / 2)
}

func timeOutMempool() {
	time.Sleep(txWaitExpiration * 3 / 2)
}

func dirtyEncode(s string) []byte {
	b, err := hex.DecodeString(s)
	if err != nil {
		fmt.Printf("dirtyEncode error for input '%s': %v", s, err)
	}
	return b
}

// A new tUser with a unique account ID, signature, and address.
func tNewUser(lbl string) *tUser {
	intBytes := make([]byte, 4)
	binary.BigEndian.PutUint32(intBytes, acctCounter)
	acctID := account.AccountID{}
	copy(acctID[:], acctTemplate[:])
	copy(acctID[account.HashSize-4:], intBytes)
	addr := strconv.Itoa(int(acctCounter))
	sig := []byte{0xab} // Just to differentiate from the addr.
	sig = append(sig, intBytes...)
	sigHex := hex.EncodeToString(sig)
	acctCounter++
	return &tUser{
		sig:    sig,
		sigHex: sigHex,
		acct:   acctID,
		addr:   addr,
		lbl:    lbl,
	}
}

type TRequest struct {
	req      *msgjson.Message
	respFunc func(comms.Link, *msgjson.Message)
}

// This stub satisfies AuthManager.
type TAuthManager struct {
	mtx         sync.Mutex
	verifyErr   error
	privkey     *secp256k1.PrivateKey
	reqs        map[account.AccountID][]*TRequest
	resps       map[account.AccountID][]*msgjson.Message
	ntfns       map[account.AccountID][]*msgjson.Message
	newNtfn     chan struct{}
	suspensions map[account.AccountID]account.Rule
	newSuspend  chan struct{}
	swapID      uint64
	// Use swapReceived if you need to synchronize error responses to init
	// requests.
	swapReceived chan struct{}
	auditReq     chan struct{}
	redeemID     uint64
	// Use redeemReceived if you need to synchronize error responses to redeem
	// requests.
	redeemReceived chan struct{}
	redemptionReq  chan struct{}
	// ntfnLocal records whether the last notification used SendIfLocal.
	ntfnLocal map[account.AccountID]map[string]bool
}

func newTAuthManager() *TAuthManager {
	// Reuse any previously generated dex server private key.
	if dexPrivKey == nil {
		dexPrivKey, _ = secp256k1.GeneratePrivateKey()
	}
	return &TAuthManager{
		privkey:     dexPrivKey,
		reqs:        make(map[account.AccountID][]*TRequest),
		ntfns:       make(map[account.AccountID][]*msgjson.Message),
		resps:       make(map[account.AccountID][]*msgjson.Message),
		suspensions: make(map[account.AccountID]account.Rule),
		ntfnLocal:   make(map[account.AccountID]map[string]bool),
	}
}

func (m *TAuthManager) Send(user account.AccountID, msg *msgjson.Message) error {
	return m.recordSend(user, msg, false)
}

func (m *TAuthManager) SendIfLocal(user account.AccountID, msg *msgjson.Message) error {
	return m.recordSend(user, msg, true)
}

func (m *TAuthManager) recordSend(user account.AccountID, msg *msgjson.Message, local bool) error {
	m.mtx.Lock()
	defer m.mtx.Unlock()
	if msg.Route == "" {
		m.resps[user] = append(m.resps[user], msg)
		if m.redeemReceived != nil && msg.ID == m.redeemID {
			m.redeemReceived <- struct{}{}
		}
		if m.swapReceived != nil && msg.ID == m.swapID {
			m.swapReceived <- struct{}{}
		}
		return nil
	}
	if m.ntfnLocal[user] == nil {
		m.ntfnLocal[user] = make(map[string]bool)
	}
	m.ntfnLocal[user][msg.Route] = local
	m.ntfns[user] = append(m.ntfns[user], msg)
	if m.newNtfn != nil {
		select {
		case m.newNtfn <- struct{}{}:
		default:
		}
	}
	return nil
}

func (m *TAuthManager) ntfnWasLocal(user account.AccountID, route string) (local, ok bool) {
	m.mtx.Lock()
	defer m.mtx.Unlock()
	local, ok = m.ntfnLocal[user][route]
	return
}

func (m *TAuthManager) Request(user account.AccountID, msg *msgjson.Message,
	f func(comms.Link, *msgjson.Message)) error {
	return m.RequestWithTimeout(user, msg, f, time.Hour, func() {})
}

func (m *TAuthManager) RequestWithTimeout(user account.AccountID, msg *msgjson.Message,
	f func(comms.Link, *msgjson.Message), _ time.Duration, _ func()) error {
	m.mtx.Lock()
	defer m.mtx.Unlock()
	tReq := &TRequest{
		req:      msg,
		respFunc: f,
	}
	l := m.reqs[user]
	if l == nil {
		l = make([]*TRequest, 0, 1)
	}
	m.reqs[user] = append(l, tReq)
	switch {
	case m.auditReq != nil && msg.Route == msgjson.AuditRoute:
		m.auditReq <- struct{}{}
	case m.redemptionReq != nil && msg.Route == msgjson.RedemptionRoute:
		m.redemptionReq <- struct{}{}
	}
	return nil
}
func (m *TAuthManager) Sign(signables ...msgjson.Signable) {
	for _, signable := range signables {
		hash := sha256.Sum256(signable.Serialize())
		sig := ecdsa.Sign(m.privkey, hash[:])
		signable.SetSig(sig.Serialize())
	}
}
func (m *TAuthManager) Suspended(user account.AccountID) (found, suspended bool) {
	var rule account.Rule
	rule, found = m.suspensions[user]
	suspended = rule != account.NoRule
	return // TODO: test suspended account handling (no trades, just cancels)
}

func (m *TAuthManager) VerifyUserSig(user account.AccountID, msg, sig []byte) error {
	return m.verifyErr
}

func (m *TAuthManager) Route(string,
	func(account.AccountID, *msgjson.Message) *msgjson.Error) {
}

func (m *TAuthManager) ReputationOutcomePolicy() *db.ReputationOutcomePolicy {
	return &db.ReputationOutcomePolicy{PreimageLimit: 40, MatchLimit: 60, OrderLimit: 100, FreeCancelThreshold: 2}
}

func (m *TAuthManager) penalize(id account.AccountID, rule account.Rule) {
	m.mtx.Lock()
	defer m.mtx.Unlock()
	m.suspensions[id] = rule
	if m.newSuspend != nil {
		m.newSuspend <- struct{}{}
	}
}

func (m *TAuthManager) flushPenalty(user account.AccountID) (found bool, rule account.Rule) {
	m.mtx.Lock()
	defer m.mtx.Unlock()
	rule, found = m.suspensions[user]
	if found {
		delete(m.suspensions, user)
	}
	return
}

// pop front
func (m *TAuthManager) popReq(id account.AccountID) *TRequest {
	m.mtx.Lock()
	defer m.mtx.Unlock()
	reqs := m.reqs[id]
	if len(reqs) == 0 {
		return nil
	}
	req := reqs[0]
	m.reqs[id] = reqs[1:]
	return req
}

// push front
func (m *TAuthManager) pushReq(id account.AccountID, req *TRequest) {
	m.mtx.Lock()
	defer m.mtx.Unlock()
	m.reqs[id] = append([]*TRequest{req}, m.reqs[id]...)
}

func (m *TAuthManager) getNtfn(id account.AccountID, route string, payload any) error {
	m.mtx.Lock()
	defer m.mtx.Unlock()
	msgs := m.ntfns[id]
	for i, msg := range msgs {
		if msg.Route == route {
			m.ntfns[id] = append(msgs[:i], msgs[i+1:]...)
			return msg.Unmarshal(payload)
		}
	}
	return fmt.Errorf("no %s notification", route)
}

func (m *TAuthManager) hasNtfn(id account.AccountID, route string) bool {
	m.mtx.Lock()
	defer m.mtx.Unlock()
	for _, msg := range m.ntfns[id] {
		if msg.Route == route {
			return true
		}
	}
	return false
}

// push front
func (m *TAuthManager) pushResp(id account.AccountID, msg *msgjson.Message) {
	m.mtx.Lock()
	defer m.mtx.Unlock()
	m.resps[id] = append([]*msgjson.Message{msg}, m.resps[id]...)
}

// pop front
func (m *TAuthManager) popResp(id account.AccountID) (msg *msgjson.Message, resp *msgjson.ResponsePayload) {
	m.mtx.Lock()
	defer m.mtx.Unlock()
	msgs := m.resps[id]
	if len(msgs) == 0 {
		return
	}
	msg = msgs[0]
	m.resps[id] = msgs[1:]
	resp, _ = msg.Response()
	return
}

type TStorage struct {
	mtx sync.Mutex

	activeSwaps []*db.SwapDataFull
	orders      map[order.OrderID]order.Order

	swapDataByID map[order.MatchID]*db.SwapDataFull

	matchAckEvents            []*meshevents.MatchAcksRecordedEvent
	applyMatchAcksRecordedErr error
	swapContracts             []*meshevents.SwapContractRecordedEvent
	auditAcks                 []*meshevents.AuditAckRecordedEvent
	redemptionAcks            []*meshevents.RedemptionAckRecordedEvent
	saveContractErr           error
	applyAuditAckRecordedErr  error
	redemptions               []*meshevents.SwapRedemptionRecordedEvent
	applyRedemptionAckErr     error
	applyRedemptionErr        error
	matchFailures             []*meshevents.MatchFailedEvent
	applyMatchFailedErr       error

	fatalMtx sync.RWMutex
	fatal    chan struct{}
	fatalErr error
}

func (ts *TStorage) LastErr() error {
	ts.fatalMtx.RLock()
	defer ts.fatalMtx.RUnlock()
	return ts.fatalErr
}

func (ts *TStorage) Fatal() <-chan struct{} {
	ts.fatalMtx.RLock()
	defer ts.fatalMtx.RUnlock()
	return ts.fatal
}

func (ts *TStorage) fatalBackendErr(err error) {
	ts.fatalMtx.Lock()
	if ts.fatal == nil {
		ts.fatal = make(chan struct{})
		close(ts.fatal)
	}
	ts.fatalErr = err // consider slice and append
	ts.fatalMtx.Unlock()
}

func (ts *TStorage) Order(oid order.OrderID, base, quote uint32) (order.Order, order.OrderStatus, error) {
	ts.mtx.Lock()
	defer ts.mtx.Unlock()
	if ts.orders != nil {
		ord, found := ts.orders[oid]
		if !found {
			return nil, order.OrderStatusUnknown, db.ArchiveError{Code: db.ErrUnknownOrder}
		}
		return ord, order.OrderStatusExecuted, nil
	}
	return nil, order.OrderStatusUnknown, nil // not loading swaps
}

func (ts *TStorage) ActiveSwaps() ([]*db.SwapDataFull, error) {
	ts.mtx.Lock()
	defer ts.mtx.Unlock()
	return ts.activeSwaps, nil
}

func (ts *TStorage) SwapDataFullByID(mid order.MatchID) (*db.SwapDataFull, error) {
	ts.mtx.Lock()
	defer ts.mtx.Unlock()
	if data := ts.swapDataByID[mid]; data != nil {
		return data, nil
	}
	return nil, db.ArchiveError{Code: db.ErrUnknownMatch}
}

func (ts *TStorage) EventLogFrontier(context.Context) (*db.EventLogPosition, error) {
	return &db.EventLogPosition{}, nil
}

func (ts *TStorage) EventLogEntriesAfter(context.Context, uint64, int) ([]*db.EventLogEntry, error) {
	return nil, nil
}

func (ts *TStorage) ApplyMatchAcksRecordedEvent(_ context.Context, _ *db.EventLogMeta, update *meshevents.MatchAcksRecordedEvent) (*db.EventLogEntry, error) {
	ts.mtx.Lock()
	defer ts.mtx.Unlock()

	ts.matchAckEvents = append(ts.matchAckEvents, update)
	if ts.applyMatchAcksRecordedErr != nil {
		return nil, ts.applyMatchAcksRecordedErr
	}
	return new(db.EventLogEntry), nil
}

func (ts *TStorage) ApplySwapContractRecordedEvent(_ context.Context, _ *db.EventLogMeta, contract *meshevents.SwapContractRecordedEvent) (*db.EventLogEntry, error) {
	ts.mtx.Lock()
	defer ts.mtx.Unlock()
	if ts.saveContractErr != nil {
		return nil, ts.saveContractErr
	}
	ts.swapContracts = append(ts.swapContracts, contract)
	return new(db.EventLogEntry), nil
}

func (ts *TStorage) ApplyAuditAckRecordedEvent(_ context.Context, _ *db.EventLogMeta, ack *meshevents.AuditAckRecordedEvent) (*db.EventLogEntry, error) {
	ts.mtx.Lock()
	defer ts.mtx.Unlock()
	if ts.applyAuditAckRecordedErr != nil {
		return nil, ts.applyAuditAckRecordedErr
	}
	ts.auditAcks = append(ts.auditAcks, ack)
	return new(db.EventLogEntry), nil
}

func (ts *TStorage) ApplySwapRedemptionRecordedEvent(_ context.Context, _ *db.EventLogMeta, _ *db.ReputationOutcomePolicy, redemption *meshevents.SwapRedemptionRecordedEvent) (*db.EventLogEntry, error) {
	ts.mtx.Lock()
	defer ts.mtx.Unlock()
	if ts.applyRedemptionErr != nil {
		return nil, ts.applyRedemptionErr
	}
	ts.redemptions = append(ts.redemptions, redemption)
	return new(db.EventLogEntry), nil
}

func (ts *TStorage) ApplyRedemptionAckRecordedEvent(_ context.Context, _ *db.EventLogMeta, ack *meshevents.RedemptionAckRecordedEvent) (*db.EventLogEntry, error) {
	ts.mtx.Lock()
	defer ts.mtx.Unlock()
	if ts.applyRedemptionAckErr != nil {
		return nil, ts.applyRedemptionAckErr
	}
	ts.redemptionAcks = append(ts.redemptionAcks, ack)
	return new(db.EventLogEntry), nil
}

func (ts *TStorage) ApplyMatchFailedEvent(_ context.Context, _ *db.EventLogMeta, _ *db.ReputationOutcomePolicy, event *meshevents.MatchFailedEvent) (*db.EventLogEntry, error) {
	ts.mtx.Lock()
	defer ts.mtx.Unlock()
	ts.matchFailures = append(ts.matchFailures, event)
	if ts.applyMatchFailedErr != nil {
		return nil, ts.applyMatchFailedErr
	}
	return new(db.EventLogEntry), nil
}

type redeemKey struct {
	redemptionCoin       string
	counterpartySwapCoin string
}

// This stub satisfies asset.Backend.
type TBackend struct {
	mtx             sync.RWMutex
	contracts       map[string]*asset.Contract
	contractErr     error
	fundsErr        error
	redemptions     map[redeemKey]asset.Coin
	redemptionErr   error
	bChan           chan *asset.BlockUpdate // to trigger processBlock and eventually (after up to BroadcastTimeout) checkInaction depending on block time
	lbl             string
	invalidFeeRate  bool
	rejectSwapAddrs bool

	wantRedeemContract []byte // optional expected contract data for Redemption
}

func newTBackend(lbl string) TBackend {
	return TBackend{
		bChan:       make(chan *asset.BlockUpdate, 5),
		lbl:         lbl,
		contracts:   make(map[string]*asset.Contract),
		redemptions: make(map[redeemKey]asset.Coin),
		fundsErr:    asset.CoinNotFoundError,
	}
}

func newUTXOBackend(lbl string) *TUTXOBackend {
	return &TUTXOBackend{TBackend: newTBackend(lbl)}
}

func newAccountBackend(lbl string) *TAccountBackend {
	return &TAccountBackend{newTBackend(lbl)}
}

func (a *TBackend) Contract(coinID, redeemScript []byte) (*asset.Contract, error) {
	a.mtx.RLock()
	defer a.mtx.RUnlock()
	if a.contractErr != nil {
		return nil, a.contractErr
	}
	contract, found := a.contracts[string(coinID)]
	if !found || contract == nil {
		return nil, asset.CoinNotFoundError
	}

	return contract, nil
}
func (a *TBackend) Redemption(redemptionID, cpSwapCoinID, contractData []byte) (asset.Coin, error) {
	a.mtx.RLock()
	defer a.mtx.RUnlock()
	if a.redemptionErr != nil {
		return nil, a.redemptionErr
	}
	redeem, found := a.redemptions[redeemKey{string(redemptionID), string(cpSwapCoinID)}]
	if !found || redeem == nil {
		return nil, asset.CoinNotFoundError
	}
	if a.wantRedeemContract != nil && !bytes.Equal(contractData, a.wantRedeemContract) {
		return nil, fmt.Errorf("redemption contract = %x, want %x", contractData, a.wantRedeemContract)
	}
	return redeem, nil
}
func (a *TBackend) ValidateCoinID(coinID []byte) (string, error) {
	return "", nil
}
func (a *TBackend) ValidateContract(contract []byte) error {
	return nil
}
func (a *TBackend) BlockChannel(size int) <-chan *asset.BlockUpdate  { return a.bChan }
func (a *TBackend) FeeRate(context.Context) (uint64, error)          { return 10, nil }
func (a *TBackend) CheckSwapAddress(string) bool                     { return !a.rejectSwapAddrs }
func (a *TBackend) Connect(context.Context) (*sync.WaitGroup, error) { return nil, nil }
func (a *TBackend) ValidateSecret(secret, contract []byte) bool      { return true }
func (a *TBackend) Synced() (bool, error)                            { return true, nil }
func (a *TBackend) TxData([]byte) ([]byte, error) {
	return nil, nil
}

func (a *TBackend) setContractErr(err error) {
	a.mtx.Lock()
	defer a.mtx.Unlock()
	a.contractErr = err
}
func (a *TBackend) setContract(contract *asset.Contract, resetErr bool) {
	a.mtx.Lock()
	a.contracts[string(contract.ID())] = contract
	if resetErr {
		a.contractErr = nil
	}
	a.mtx.Unlock()
}

func (a *TBackend) setRedemptionErr(err error) {
	a.mtx.Lock()
	defer a.mtx.Unlock()
	a.redemptionErr = err
}
func (a *TBackend) setRedemption(redeem asset.Coin, cpSwap asset.Coin, resetErr bool) {
	a.mtx.Lock()
	a.redemptions[redeemKey{string(redeem.ID()), string(cpSwap.ID())}] = redeem
	if resetErr {
		a.redemptionErr = nil
	}
	a.mtx.Unlock()
}
func (*TBackend) Info() *asset.BackendInfo {
	return &asset.BackendInfo{}
}
func (a *TBackend) ValidateFeeRate(asset.Coin, uint64) bool {
	return !a.invalidFeeRate
}

type TUTXOBackend struct {
	TBackend
	funds asset.FundingCoin
}

func (a *TUTXOBackend) FundingCoin(_ context.Context, coinID, redeemScript []byte) (asset.FundingCoin, error) {
	a.mtx.RLock()
	defer a.mtx.RUnlock()
	return a.funds, a.fundsErr
}

func (a *TUTXOBackend) VerifyUnspentCoin(_ context.Context, coinID []byte) error { return nil }

type TAccountBackend struct {
	TBackend
}

var _ asset.AccountBalancer = (*TAccountBackend)(nil)

func (b *TAccountBackend) AccountBalance(addr string) (uint64, error) {
	return 0, nil
}

func (b *TAccountBackend) ValidateSignature(addr string, pubkey, msg, sig []byte) error {
	return nil
}

func (a *TAccountBackend) InitTxSize() uint64 { return 100 }

func (b *TAccountBackend) RedeemSize() uint64 {
	return 21_000
}

// This stub satisfies asset.Transaction, used by asset.Backend.
type TCoin struct {
	mtx       sync.RWMutex
	id        []byte
	confs     int64
	confsErr  error
	auditAddr string
	auditVal  uint64
	feeRate   uint64
}

func (coin *TCoin) Confirmations(context.Context) (int64, error) {
	coin.mtx.RLock()
	defer coin.mtx.RUnlock()
	return coin.confs, coin.confsErr
}

func (coin *TCoin) Addresses() []string {
	return []string{coin.auditAddr}
}

func (coin *TCoin) setConfs(confs int64) {
	coin.mtx.Lock()
	defer coin.mtx.Unlock()
	coin.confs = confs
}

func (coin *TCoin) Auth(pubkeys, sigs [][]byte, msg []byte) error { return nil }
func (coin *TCoin) ID() []byte                                    { return coin.id }
func (coin *TCoin) TxID() string                                  { return hex.EncodeToString(coin.id) }
func (coin *TCoin) Value() uint64                                 { return coin.auditVal }
func (coin *TCoin) SpendSize() uint32                             { return 0 }
func (coin *TCoin) String() string                                { return hex.EncodeToString(coin.id) /* not txid:vout */ }

func (coin *TCoin) FeeRate() uint64 {
	return coin.feeRate
}

func TNewAsset(backend asset.Backend, assetID uint32) *asset.BackedAsset {
	return &asset.BackedAsset{
		Backend: backend,
		Asset: dex.Asset{
			ID:         assetID,
			Symbol:     "qwe",
			MaxFeeRate: 120, // not used by Swapper other than prohibiting zero
			SwapConf:   2,
		},
	}
}

var testMsgID uint64

func nextID() uint64 {
	return atomic.AddUint64(&testMsgID, 1)
}

func tNewResponse(id uint64, resp []byte) *msgjson.Message {
	msg, _ := msgjson.NewResponse(id, json.RawMessage(resp), nil)
	return msg
}

// testRig is the primary test data structure.
type testRig struct {
	abc           *asset.BackedAsset
	abcNode       *TUTXOBackend
	xyz           *asset.BackedAsset
	xyzNode       *TUTXOBackend
	acctAsset     *asset.BackedAsset
	acctNode      *TAccountBackend
	auth          *TAuthManager
	swapper       *Swapper
	swapperWaiter *dex.StartStopWaiter
	swapperDone   chan struct{}
	storage       *TStorage
	matches       *tMatchSet
	matchInfo     *tMatch
}

type tSwapMesh struct {
	commandErr *msgjson.Error
	reqs       []mesh.CommandRequest
	err        error
	events     []*mesh.Event
	applier    map[string]mesh.EventApplier
}

func (m *tSwapMesh) ExecuteCommand(_ context.Context, req mesh.CommandRequest) *msgjson.Error {
	m.reqs = append(m.reqs, req)
	return m.commandErr
}

func (m *tSwapMesh) ApplyEvent(ctx context.Context, event *mesh.Event) (any, error) {
	m.events = append(m.events, event)
	if m.err != nil {
		return nil, m.err
	}
	if m.applier == nil {
		return nil, nil
	}
	applier := m.applier[event.Kind]
	if applier == nil {
		return nil, fmt.Errorf("unsupported test swap event %q", event.Kind)
	}
	applyCtx := &mesh.EventApplyContext{Context: ctx}
	_, err := applier(applyCtx, event)
	return applyCtx.Result(), err
}

func matchAckEvents(storage *TStorage) []*meshevents.MatchAcksRecordedEvent {
	storage.mtx.Lock()
	defer storage.mtx.Unlock()
	return append([]*meshevents.MatchAcksRecordedEvent(nil), storage.matchAckEvents...)
}

func notificationCount(auth *TAuthManager, route string) int {
	auth.mtx.Lock()
	defer auth.mtx.Unlock()
	var n int
	for _, msgs := range auth.ntfns {
		for _, msg := range msgs {
			if msg.Route == route {
				n++
			}
		}
	}
	return n
}

func tNewUnstartedRig(matchInfo *tMatch) *testRig {
	storage := &TStorage{}
	authMgr := newTAuthManager()

	abcBackend := newUTXOBackend("abc")
	xyzBackend := newUTXOBackend("xyz")
	acctBackend := newAccountBackend("acct")

	abcAsset := TNewAsset(abcBackend, ABCID)
	abcCoinLocker := coinlock.NewAssetCoinLocker()

	xyzAsset := TNewAsset(xyzBackend, XYZID)
	xyzCoinLocker := coinlock.NewAssetCoinLocker()

	acctAsset := TNewAsset(acctBackend, ACCTID)

	swapper, err := NewSwapper(&Config{
		Assets: map[uint32]*SwapperAsset{
			ABCID:  {abcAsset, abcCoinLocker},
			XYZID:  {xyzAsset, xyzCoinLocker},
			ACCTID: {BackedAsset: acctAsset}, // no coin locker for account based asset.
		},
		Storage:          storage,
		AuthManager:      authMgr,
		BroadcastTimeout: tBcastTimeout,
		TxWaitExpiration: txWaitExpiration,
		LockTimeTaker:    dex.LockTimeTaker(dex.Testnet),
		LockTimeMaker:    dex.LockTimeMaker(dex.Testnet),
		SwapDone:         func(order.Order, *order.Match, bool) {},
	})
	if err != nil {
		panic(err.Error())
	}

	return &testRig{
		abc:       abcAsset,
		abcNode:   abcBackend,
		xyz:       xyzAsset,
		xyzNode:   xyzBackend,
		acctAsset: acctAsset,
		acctNode:  acctBackend,
		auth:      authMgr,
		swapper:   swapper,
		storage:   storage,
		matchInfo: matchInfo,
	}
}

func tNewTestRig(matchInfo *tMatch) (*testRig, func()) {
	rig := tNewUnstartedRig(matchInfo)
	return rig, rig.start()
}

func (rig *testRig) start() func() {
	swapper := rig.swapper
	storage := rig.storage

	swapperDone := make(chan struct{})
	meshSvc, err := mesh.NewService(&mesh.ServiceConfig{
		Commands:       swapper.Commands(),
		Events:         swapper.Events(),
		EventLogReader: storage,
		OnHalt:         func(error) {},
		MasterWorkers: []mesh.MasterWorker{{
			Name: "Swapper",
			Run: func(ctx context.Context, reportReady func(error)) {
				defer close(swapperDone)
				swapper.Run(ctx, reportReady)
			},
		}, {
			// Mirrors the production sentinel that enables the inaction
			// checks once the last market has reported ready.
			Name: "Swap inactivity checks",
			Run: func(ctx context.Context, reportReady func(error)) {
				swapper.EnableInactionChecks()
				reportReady(nil)
				<-ctx.Done()
			},
		}},
		Logger: dex.Disabled,
	})
	if err != nil {
		panic(err.Error())
	}
	swapper.SetMeshService(meshSvc)
	ssw := dex.NewStartStopWaiter(meshSvc)
	ssw.Start(testCtx)
	if err := meshSvc.WaitUntilReadyForComms(testCtx); err != nil {
		panic(err.Error())
	}
	cleanup := func() {
		ssw.Stop()
		ssw.WaitForShutdown()
	}

	rig.swapperWaiter = ssw
	rig.swapperDone = swapperDone
	return cleanup
}

func TestNewSwapper(t *testing.T) {
	cfg := &Config{AuthManager: newTAuthManager()}
	if _, err := NewSwapper(cfg); err == nil || !strings.Contains(err.Error(), "swap-done applier is not configured") {
		t.Fatalf("expected missing SwapDone error, got %v", err)
	}
	cfg.SwapDone = func(order.Order, *order.Match, bool) {}
	if _, err := NewSwapper(cfg); err != nil {
		t.Fatalf("NewSwapper: %v", err)
	}
}

func (rig *testRig) applyMatchesAndRequestAcks(t *testing.T, matchSets ...*order.MatchSet) {
	t.Helper()
	rig.swapper.TrackMatches(matchSets)
	rig.swapper.RequestMatchAcks(matchSets)
}

func (rig *testRig) getTracker() *matchTracker {
	rig.swapper.matchMtx.Lock()
	defer rig.swapper.matchMtx.Unlock()
	return rig.swapper.matches[rig.matchInfo.matchID]
}

// waitChans waits on the specified channels sequentially in the order given.
// nil channels are ignored.
func (rig *testRig) waitChans(tag string, chans ...chan struct{}) error {
	for _, c := range chans {
		if c == nil {
			continue
		}
		select {
		case <-c:
		case <-time.After(time.Second):
			return fmt.Errorf("waiting on %q timed out", tag)
		}
	}
	return nil
}

// perMatchAddrs builds a map from match ID to per-match address for the given
// user by scanning all match infos, or just rig.matchInfo when rig.matches is
// not set.
func (rig *testRig) perMatchAddrs(user *tUser, isMaker bool) map[order.MatchID]string {
	addrs := make(map[order.MatchID]string)
	matchInfos := []*tMatch{rig.matchInfo}
	if rig.matches != nil {
		matchInfos = rig.matches.matchInfos
	}
	for _, mi := range matchInfos {
		if isMaker && mi.maker == user {
			addrs[mi.matchID] = mi.makerPerMatchAddr
		} else if !isMaker && mi.taker == user {
			addrs[mi.matchID] = mi.takerPerMatchAddr
		}
	}
	return addrs
}

// Maker: Acknowledge the servers match notification.
func (rig *testRig) ackMatch_maker(checkSig bool) (err error) {
	matchInfo := rig.matchInfo
	err = rig.ackMatch(matchInfo.maker, matchInfo.makerOID, matchInfo.taker.addr,
		rig.perMatchAddrs(matchInfo.maker, true))
	if err != nil {
		return err
	}
	if checkSig {
		tracker := rig.getTracker()
		if !bytes.Equal(tracker.Sigs.MakerMatch, matchInfo.maker.sig) {
			return fmt.Errorf("expected maker audit signature '%x', got '%x'", matchInfo.maker.sig, tracker.Sigs.MakerMatch)
		}
	}
	return nil
}

// Taker: Acknowledge the servers match notification.
func (rig *testRig) ackMatch_taker(checkSig bool) error {
	matchInfo := rig.matchInfo
	err := rig.ackMatch(matchInfo.taker, matchInfo.takerOID, matchInfo.maker.addr,
		rig.perMatchAddrs(matchInfo.taker, false))
	if err != nil {
		return err
	}
	if checkSig {
		tracker := rig.getTracker()
		if !bytes.Equal(tracker.Sigs.TakerMatch, matchInfo.taker.sig) {
			return fmt.Errorf("expected taker audit signature '%x', got '%x'", matchInfo.taker.sig, tracker.Sigs.TakerMatch)
		}
	}
	return nil
}

func (rig *testRig) ackMatch(user *tUser, oid order.OrderID, counterAddr string, matchAddrs map[order.MatchID]string) error {
	req := rig.auth.popReq(user.acct)
	if req == nil {
		return fmt.Errorf("failed to find match notification for %s", user.lbl)
	}
	if req.req.Route != msgjson.MatchRoute {
		return fmt.Errorf("expected method '%s', got '%s'", msgjson.MatchRoute, req.req.Route)
	}
	err := rig.checkMatchNotification(req.req, oid, counterAddr)
	if err != nil {
		return err
	}
	// The maker and taker would sign the notifications and return a list of
	// authorizations, including per-match swap addresses.
	resp := tNewResponse(req.req.ID, tAckArrWithAddrs(user, user.matchIDs, matchAddrs))
	req.respFunc(nil, resp) // e.g. processMatchAcks, may Send resp on error
	return nil
}

// Helper to check the match notifications.
func (rig *testRig) checkMatchNotification(msg *msgjson.Message, oid order.OrderID, counterAddr string) error {
	matchInfo := rig.matchInfo
	var notes []*msgjson.Match
	err := json.Unmarshal(msg.Payload, &notes)
	if err != nil {
		fmt.Printf("checkMatchNotification unmarshal error: %v\n", err)
	}
	var notification *msgjson.Match
	for _, n := range notes {
		if bytes.Equal(n.MatchID, matchInfo.matchID[:]) {
			notification = n
			break
		}
		if err = checkSigS256(n, rig.auth.privkey.PubKey()); err != nil {
			return fmt.Errorf("incorrect server signature: %w", err)
		}
	}
	if notification == nil {
		return fmt.Errorf("did not find match ID %s in match notifications", matchInfo.matchID)
	}
	if notification.OrderID.String() != oid.String() {
		return fmt.Errorf("expected order ID %s, got %s", oid, notification.OrderID)
	}
	if notification.Quantity != matchInfo.qty {
		return fmt.Errorf("expected order quantity %d, got %d", matchInfo.qty, notification.Quantity)
	}
	if notification.Rate != matchInfo.rate {
		return fmt.Errorf("expected match rate %d, got %d", matchInfo.rate, notification.Rate)
	}
	if notification.Address != counterAddr {
		return fmt.Errorf("expected match address %s, got %s", counterAddr, notification.Address)
	}
	return nil
}

// Helper to check the swap status for specified user.
// Swap counterparty usually gets a request from server notifying that it's time
// for his turn. Synchronize with that before checking status change.
func (rig *testRig) ensureSwapStatus(tag string, wantStatus order.MatchStatus, waitOn ...chan struct{}) error {
	if err := rig.waitChans(tag, waitOn...); err != nil {
		return err
	}
	tracker := rig.getTracker()
	tracker.mtx.RLock()
	status := tracker.Status
	tracker.mtx.RUnlock()
	if status != wantStatus {
		return fmt.Errorf("unexpected swap status %d after maker swap notification, wanted: %s", status, wantStatus)
	}
	return nil
}

// Can be used to ensure that a non-error response is returned from the swapper.
func (rig *testRig) checkServerResponseSuccess(user *tUser) error {
	msg, resp := rig.auth.popResp(user.acct)
	if msg == nil {
		return fmt.Errorf("unexpected nil response to %s's request", user.lbl)
	}
	if resp.Error != nil {
		return fmt.Errorf("%s swap rpc error. code: %d, msg: %s", user.lbl, resp.Error.Code, resp.Error.Message)
	}
	return nil
}

// Can be used to ensure that an error response is returned from the swapper.
func (rig *testRig) checkServerResponseFail(user *tUser, code int, grep ...string) error {
	msg, resp := rig.auth.popResp(user.acct)
	if msg == nil {
		return fmt.Errorf("no response for %s", user.lbl)
	}
	if resp.Error == nil {
		return fmt.Errorf("no error for %s", user.lbl)
	}
	if resp.Error.Code != code {
		return fmt.Errorf("wrong error code for %s. expected %d, got %d", user.lbl, code, resp.Error.Code)
	}
	if len(grep) > 0 && !strings.Contains(resp.Error.Message, grep[0]) {
		return fmt.Errorf("error missing the message %q", grep[0])
	}
	return nil
}

// swapRecipient returns the per-match swap address for the given side if one
// has been stored on the match tracker (via the ack flow), otherwise falls back
// to the order-level address.
func (rig *testRig) swapRecipient(isMaker bool) string {
	matchInfo := rig.matchInfo
	tracker := rig.getTracker()
	tracker.mtx.RLock()
	defer tracker.mtx.RUnlock()
	if isMaker {
		// Maker's swap pays to the taker's address.
		if tracker.takerSwapAddr != "" {
			return tracker.takerSwapAddr
		}
		return matchInfo.taker.addr
	}
	// Taker's swap pays to the maker's address.
	if tracker.makerSwapAddr != "" {
		return tracker.makerSwapAddr
	}
	return matchInfo.maker.addr
}

// Maker: Send swap transaction (swap init request).
func (rig *testRig) sendSwap_maker(expectSuccess bool) (err error) {
	matchInfo := rig.matchInfo
	swap, err := rig.sendSwap(matchInfo.maker, matchInfo.makerOID, rig.swapRecipient(true))
	if err != nil {
		return fmt.Errorf("error sending maker swap request: %w", err)
	}
	matchInfo.db.makerSwap = swap
	if expectSuccess {
		err = rig.ensureSwapStatus("server received our swap -> counterparty got audit request",
			order.MakerSwapCast, rig.auth.swapReceived, rig.auth.auditReq)
		if err != nil {
			return fmt.Errorf("ensure swap status: %w", err)
		}
		err = rig.checkServerResponseSuccess(matchInfo.maker)
		if err != nil {
			return fmt.Errorf("check server response success: %w", err)
		}
	}
	return nil
}

// Taker: Send swap transaction (swap init request).
func (rig *testRig) sendSwap_taker(expectSuccess bool) (err error) {
	matchInfo := rig.matchInfo
	swap, err := rig.sendSwap(matchInfo.taker, matchInfo.takerOID, rig.swapRecipient(false))
	if err != nil {
		return fmt.Errorf("error sending taker swap request: %w", err)
	}

	matchInfo.db.takerSwap = swap
	if expectSuccess {
		err = rig.ensureSwapStatus("server received our swap -> counterparty got audit request",
			order.TakerSwapCast, rig.auth.swapReceived, rig.auth.auditReq)
		if err != nil {
			return fmt.Errorf("ensure swap status: %w", err)
		}
		err = rig.checkServerResponseSuccess(matchInfo.taker)
		if err != nil {
			return fmt.Errorf("check server response success: %w", err)
		}
	}
	return nil
}

func (rig *testRig) sendSwap(user *tUser, oid order.OrderID, recipient string) (*tSwap, error) {
	matchInfo := rig.matchInfo
	swap := tNewSwap(matchInfo, oid, recipient, user)
	if isQuoteSwap(user, matchInfo.match) {
		rig.xyzNode.setContract(swap.coin, false)
	} else {
		rig.abcNode.setContract(swap.coin, false)
	}
	rig.auth.swapID = swap.req.ID
	rpcErr := rig.swapper.handleInit(user.acct, swap.req)
	if rpcErr != nil {
		resp, _ := msgjson.NewResponse(swap.req.ID, nil, rpcErr)
		_ = rig.auth.Send(user.acct, resp)
		return nil, fmt.Errorf("%s swap rpc error. code: %d, msg: %s", user.lbl, rpcErr.Code, rpcErr.Message)
	}
	return swap, nil
}

// Taker: Process the 'audit' request from the swapper. The request should be
// acknowledged separately with ackAudit_taker.
func (rig *testRig) auditSwap_taker() error {
	matchInfo := rig.matchInfo
	req := rig.auth.popReq(matchInfo.taker.acct)
	matchInfo.db.takerAudit = req
	if req == nil {
		return fmt.Errorf("failed to find audit request for taker after maker's init")
	}
	return rig.auditSwap(req.req, matchInfo.takerOID, matchInfo.db.makerSwap, "taker", matchInfo.taker)
}

// Maker: Process the 'audit' request from the swapper. The request should be
// acknowledged separately with ackAudit_maker.
func (rig *testRig) auditSwap_maker() error {
	matchInfo := rig.matchInfo
	req := rig.auth.popReq(matchInfo.maker.acct)
	matchInfo.db.makerAudit = req
	if req == nil {
		return fmt.Errorf("failed to find audit request for maker after taker's init")
	}
	return rig.auditSwap(req.req, matchInfo.makerOID, matchInfo.db.takerSwap, "maker", matchInfo.maker)
}

// checkSigS256 checks that the message's signature was created with the
// private key for the provided secp256k1 public key.
func checkSigS256(msg msgjson.Signable, pubKey *secp256k1.PublicKey) error {
	signature, err := ecdsa.ParseDERSignature(msg.SigBytes())
	if err != nil {
		return fmt.Errorf("error decoding secp256k1 Signature from bytes: %w", err)
	}
	msgB := msg.Serialize()
	hash := sha256.Sum256(msgB)
	if !signature.Verify(hash[:], pubKey) {
		return fmt.Errorf("secp256k1 signature verification failed")
	}
	return nil
}

func (rig *testRig) auditSwap(msg *msgjson.Message, oid order.OrderID, swap *tSwap, tag string, user *tUser) error {
	if msg == nil {
		return fmt.Errorf("no %s 'audit' request from DEX", user.lbl)
	}

	if msg.Route != msgjson.AuditRoute {
		return fmt.Errorf("expected method '%s', got '%s'", msgjson.AuditRoute, msg.Route)
	}
	var params *msgjson.Audit
	err := json.Unmarshal(msg.Payload, &params)
	if err != nil {
		return fmt.Errorf("error unmarshaling audit params: %w", err)
	}
	if err = checkSigS256(params, rig.auth.privkey.PubKey()); err != nil {
		return fmt.Errorf("incorrect server signature: %w", err)
	}
	if params.OrderID.String() != oid.String() {
		return fmt.Errorf("%s : incorrect order ID in auditSwap, expected '%s', got '%s'", tag, oid, params.OrderID)
	}
	matchID := rig.matchInfo.matchID
	if params.MatchID.String() != matchID.String() {
		return fmt.Errorf("%s : incorrect match ID in auditSwap, expected '%s', got '%s'", tag, matchID, params.MatchID)
	}
	if params.Contract.String() != swap.contract {
		return fmt.Errorf("%s : incorrect contract. expected '%s', got '%s'", tag, swap.contract, params.Contract)
	}
	if !bytes.Equal(params.TxData, swap.coin.TxData) {
		return fmt.Errorf("%s : incorrect tx data. expected '%s', got '%s'", tag, swap.coin.TxData, params.TxData)
	}
	return nil
}

// Maker: Acknowledge the DEX 'audit' request.
func (rig *testRig) ackAudit_maker(checkSig bool) error {
	maker := rig.matchInfo.maker
	err := rig.ackAudit(maker, rig.matchInfo.db.makerAudit)
	if err != nil {
		return err
	}
	if checkSig {
		tracker := rig.getTracker()
		if !bytes.Equal(tracker.Sigs.MakerAudit, maker.sig) {
			return fmt.Errorf("expected taker audit signature '%x', got '%x'", maker.sig, tracker.Sigs.MakerAudit)
		}
	}
	return nil
}

// Taker: Acknowledge the DEX 'audit' request.
func (rig *testRig) ackAudit_taker(checkSig bool) error {
	taker := rig.matchInfo.taker
	err := rig.ackAudit(taker, rig.matchInfo.db.takerAudit)
	if err != nil {
		return err
	}
	if checkSig {
		tracker := rig.getTracker()
		if !bytes.Equal(tracker.Sigs.TakerAudit, taker.sig) {
			return fmt.Errorf("expected taker audit signature '%x', got '%x'", taker.sig, tracker.Sigs.TakerAudit)
		}
	}
	return nil
}

func (rig *testRig) ackAudit(user *tUser, req *TRequest) error {
	if req == nil {
		return fmt.Errorf("no %s 'audit' request from DEX", user.lbl)
	}
	req.respFunc(nil, tNewResponse(req.req.ID, tAck(user, rig.matchInfo.matchID)))
	return nil
}

// Maker: Redeem taker's swap transaction.
func (rig *testRig) redeem_maker(expectSuccess bool) error {
	matchInfo := rig.matchInfo
	redeem, err := rig.redeem(matchInfo.maker, matchInfo.makerOID)
	if err != nil {
		return fmt.Errorf("error sending maker redeem request: %w", err)
	}
	matchInfo.db.makerRedeem = redeem
	if expectSuccess {
		err = rig.ensureSwapStatus("server received our redeem -> counterparty got redemption request",
			order.MakerRedeemed, rig.auth.redeemReceived, rig.auth.redemptionReq)
		if err != nil {
			return fmt.Errorf("ensure swap status: %w", err)
		}
		err = rig.checkServerResponseSuccess(matchInfo.maker)
		if err != nil {
			return fmt.Errorf("check server response success: %w", err)
		}
	}
	return nil
}

// Taker: Redeem maker's swap transaction.
func (rig *testRig) redeem_taker(expectSuccess bool) error {
	matchInfo := rig.matchInfo
	redeem, err := rig.redeem(matchInfo.taker, matchInfo.takerOID)
	if err != nil {
		return fmt.Errorf("error sending taker redeem request: %w", err)
	}
	matchInfo.db.takerRedeem = redeem
	if expectSuccess {
		if err := rig.waitChans("server received our redeem and we got a redemption note", rig.auth.redeemReceived, rig.auth.redemptionReq); err != nil {
			return err
		}
		if tracker := rig.getTracker(); tracker != nil {
			return fmt.Errorf("expected completed match to be removed, found it in status %v", tracker.Status)
		}
		err = rig.checkServerResponseSuccess(matchInfo.taker)
		if err != nil {
			return fmt.Errorf("check server response success: %w", err)
		}
	}
	return nil
}

func (rig *testRig) redeem(user *tUser, oid order.OrderID) (*tRedeem, error) {
	matchInfo := rig.matchInfo
	redeem := tNewRedeem(matchInfo, oid, user)
	if isQuoteSwap(user, matchInfo.match) {
		// do not clear redemptionErr yet
		rig.abcNode.setRedemption(redeem.coin, redeem.cpSwapCoin, false)
	} else {
		rig.xyzNode.setRedemption(redeem.coin, redeem.cpSwapCoin, false)
	}
	rig.auth.redeemID = redeem.req.ID
	rpcErr := rig.swapper.handleRedeem(user.acct, redeem.req)
	if rpcErr != nil {
		resp, _ := msgjson.NewResponse(redeem.req.ID, nil, rpcErr)
		_ = rig.auth.Send(user.acct, resp)
		return nil, fmt.Errorf("%s swap rpc error. code: %d, msg: %s", user.lbl, rpcErr.Code, rpcErr.Message)
	}
	return redeem, nil
}

// Taker: Acknowledge the DEX 'redemption' request.
func (rig *testRig) ackRedemption_taker(checkSig bool) error {
	matchInfo := rig.matchInfo
	err := rig.ackRedemption(matchInfo.taker, matchInfo.takerOID, matchInfo.db.makerRedeem)
	if err != nil {
		return err
	}
	if checkSig {
		tracker := rig.getTracker()
		if !bytes.Equal(tracker.Sigs.TakerRedeem, matchInfo.taker.sig) {
			return fmt.Errorf("expected taker redemption signature '%x', got '%x'", matchInfo.taker.sig, tracker.Sigs.TakerRedeem)
		}
	}
	return nil
}

// Maker: Acknowledge the DEX 'redemption' request.
func (rig *testRig) ackRedemption_maker(checkSig bool) error {
	matchInfo := rig.matchInfo
	err := rig.ackRedemption(matchInfo.maker, matchInfo.makerOID, matchInfo.db.takerRedeem)
	if err != nil {
		return err
	}
	if checkSig {
		tracker := rig.getTracker()
		if tracker != nil {
			return fmt.Errorf("expected match to be removed, found it, in status %v", tracker.Status)
		}
	}
	return nil
}

func (rig *testRig) ackRedemption(user *tUser, oid order.OrderID, redeem *tRedeem) error {
	if redeem == nil {
		return fmt.Errorf("nil redeem info")
	}
	req := rig.auth.popReq(user.acct)
	if req == nil {
		return fmt.Errorf("failed to find redemption request for %s", user.lbl)
	}
	err := rig.checkRedeem(req.req, oid, redeem.coin.ID(), user.lbl)
	if err != nil {
		return err
	}
	req.respFunc(nil, tNewResponse(req.req.ID, tAck(user, rig.matchInfo.matchID)))
	return nil
}

func (rig *testRig) checkRedeem(msg *msgjson.Message, oid order.OrderID, coinID []byte, tag string) error {
	var params *msgjson.Redemption
	err := json.Unmarshal(msg.Payload, &params)
	if err != nil {
		return fmt.Errorf("error unmarshaling redeem params: %w", err)
	}
	if err = checkSigS256(params, rig.auth.privkey.PubKey()); err != nil {
		return fmt.Errorf("incorrect server signature: %w", err)
	}
	if params.OrderID.String() != oid.String() {
		return fmt.Errorf("%s : incorrect order ID in checkRedeem, expected '%s', got '%s'", tag, oid, params.OrderID)
	}
	matchID := rig.matchInfo.matchID
	if params.MatchID.String() != matchID.String() {
		return fmt.Errorf("%s : incorrect match ID in checkRedeem, expected '%s', got '%s'", tag, matchID, params.MatchID)
	}
	if !bytes.Equal(params.CoinID, coinID) {
		return fmt.Errorf("%s : incorrect coinID in checkRedeem. expected '%x', got '%x'", tag, coinID, params.CoinID)
	}
	return nil
}

func makeCancelOrder(limitOrder *order.LimitOrder, user *tUser) *order.CancelOrder {
	return &order.CancelOrder{
		P: order.Prefix{
			AccountID:  user.acct,
			BaseAsset:  limitOrder.BaseAsset,
			QuoteAsset: limitOrder.QuoteAsset,
			OrderType:  order.CancelOrderType,
			ClientTime: unixMsNow(),
			ServerTime: unixMsNow(),
		},
		TargetOrderID: limitOrder.ID(),
	}
}

func makeLimitOrder(qty, rate uint64, user *tUser, makerSell bool) *order.LimitOrder {
	return &order.LimitOrder{
		P: order.Prefix{
			AccountID:  user.acct,
			BaseAsset:  ABCID,
			QuoteAsset: XYZID,
			OrderType:  order.LimitOrderType,
			ClientTime: time.UnixMilli(1566497654000),
			ServerTime: time.UnixMilli(1566497655000),
		},
		T: order.Trade{
			Sell:     makerSell,
			Quantity: qty,
			Address:  user.addr,
		},
		Rate: rate,
	}
}

func makeMarketOrder(qty uint64, user *tUser, makerSell bool) *order.MarketOrder {
	return &order.MarketOrder{
		P: order.Prefix{
			AccountID:  user.acct,
			BaseAsset:  ABCID,
			QuoteAsset: XYZID,
			OrderType:  order.LimitOrderType,
			ClientTime: time.UnixMilli(1566497654000),
			ServerTime: time.UnixMilli(1566497655000),
		},
		T: order.Trade{
			Sell:     makerSell,
			Quantity: qty,
			Address:  user.addr,
		},
	}
}

func limitLimitPair(makerQty, takerQty, makerRate, takerRate uint64, maker, taker *tUser, makerSell bool) (*order.LimitOrder, *order.LimitOrder) {
	return makeLimitOrder(makerQty, makerRate, maker, makerSell), makeLimitOrder(takerQty, takerRate, taker, !makerSell)
}

func marketLimitPair(makerQty, takerQty, rate uint64, maker, taker *tUser, makerSell bool) (*order.LimitOrder, *order.MarketOrder) {
	return makeLimitOrder(makerQty, rate, maker, makerSell), makeMarketOrder(takerQty, taker, !makerSell)
}

func tLimitPair(makerQty, takerQty, matchQty, makerRate, takerRate uint64, makerSell bool) *tMatchSet {
	set := new(tMatchSet)
	maker, taker := tNewUser("maker"), tNewUser("taker")
	makerOrder, takerOrder := limitLimitPair(makerQty, takerQty, makerRate, takerRate, maker, taker, makerSell)
	return set.add(tMatchInfo(maker, taker, matchQty, makerRate, makerOrder, takerOrder))
}

func tPerfectLimitLimit(qty, rate uint64, makerSell bool) *tMatchSet {
	return tLimitPair(qty, qty, qty, rate, rate, makerSell)
}

func tMarketPair(makerQty, takerQty, rate uint64, makerSell bool) *tMatchSet {
	set := new(tMatchSet)
	maker, taker := tNewUser("maker"), tNewUser("taker")
	makerOrder, takerOrder := marketLimitPair(makerQty, takerQty, rate, maker, taker, makerSell)
	return set.add(tMatchInfo(maker, taker, makerQty, rate, makerOrder, takerOrder))
}

func tPerfectLimitMarket(qty, rate uint64, makerSell bool) *tMatchSet {
	return tMarketPair(qty, qty, rate, makerSell)
}

func tCancelPair() *tMatchSet {
	set := new(tMatchSet)
	user := tNewUser("user")
	qty := uint64(1e8)
	rate := uint64(1e8)
	makerOrder := makeLimitOrder(qty, rate, user, true)
	cancelOrder := makeCancelOrder(makerOrder, user)
	return set.add(tMatchInfo(user, user, qty, rate, makerOrder, cancelOrder))
}

// tMatch is the match info for a single match. A tMatch is typically created
// with tMatchInfo.
type tMatch struct {
	match    *order.Match
	matchID  order.MatchID
	makerOID order.OrderID
	takerOID order.OrderID
	qty      uint64
	rate     uint64
	maker    *tUser
	taker    *tUser
	// Per-match swap addresses provided in match acks.
	makerPerMatchAddr string
	takerPerMatchAddr string
	// secretHash is the secret hash for this match, used in swap contracts.
	secretHash []byte
	db         struct {
		makerRedeem *tRedeem
		takerRedeem *tRedeem
		makerSwap   *tSwap
		takerSwap   *tSwap
		makerAudit  *TRequest
		takerAudit  *TRequest
	}
}

func makeAck(mid order.MatchID, sig []byte, addr string) msgjson.Acknowledgement {
	return msgjson.Acknowledgement{
		MatchID: mid[:],
		Sig:     sig,
		Address: addr,
	}
}

func tAck(user *tUser, matchID order.MatchID) []byte {
	b, _ := json.Marshal(makeAck(matchID, user.sig, ""))
	return b
}

// tAckArrWithAddrs builds acknowledgements for a user, using matchAddrs to
// supply per-match addresses for each match ID.
func tAckArrWithAddrs(user *tUser, matchIDs []order.MatchID, matchAddrs map[order.MatchID]string) []byte {
	ackArr := make([]msgjson.Acknowledgement, 0, len(matchIDs))
	for _, matchID := range matchIDs {
		ackArr = append(ackArr, makeAck(matchID, user.sig, matchAddrs[matchID]))
	}
	b, _ := json.Marshal(ackArr)
	return b
}

func tMatchInfo(maker, taker *tUser, matchQty, matchRate uint64, makerOrder *order.LimitOrder, takerOrder order.Order) *tMatch {
	match := &order.Match{
		Taker:        takerOrder,
		Maker:        makerOrder,
		Quantity:     matchQty,
		Rate:         matchRate,
		FeeRateBase:  42,
		FeeRateQuote: 62,
		Epoch: order.EpochID{ // make Epoch.End() be now, Idx and Dur not important per se
			Idx: uint64(time.Now().UnixMilli()),
			Dur: 1,
		}, // Need Epoch set for lock time
	}
	mid := match.ID()
	maker.matchIDs = append(maker.matchIDs, mid)
	taker.matchIDs = append(taker.matchIDs, mid)
	return &tMatch{
		match:             match,
		qty:               matchQty,
		rate:              matchRate,
		matchID:           mid,
		makerOID:          makerOrder.ID(),
		takerOID:          takerOrder.ID(),
		maker:             maker,
		taker:             taker,
		makerPerMatchAddr: fmt.Sprintf("maker-pmaddr-%s", mid),
		takerPerMatchAddr: fmt.Sprintf("taker-pmaddr-%s", mid),
		secretHash:        encode.RandomBytes(32),
	}
}

// Matches are submitted to the swapper in small batches, one for each taker.
type tMatchSet struct {
	matchSet   *order.MatchSet
	matchInfos []*tMatch
}

// Add a new match to the tMatchSet.
func (set *tMatchSet) add(matchInfo *tMatch) *tMatchSet {
	match := matchInfo.match
	if set.matchSet == nil {
		set.matchSet = &order.MatchSet{Taker: match.Taker}
	}
	if set.matchSet.Taker.User() != match.Taker.User() {
		fmt.Println("!!!tMatchSet taker mismatch!!!")
	}
	ms := set.matchSet
	ms.Epoch = matchInfo.match.Epoch
	ms.Makers = append(ms.Makers, match.Maker)
	ms.Amounts = append(ms.Amounts, matchInfo.qty)
	ms.Rates = append(ms.Rates, matchInfo.rate)
	ms.Total += matchInfo.qty
	// MatchSet.Matches assigns these fee rates to each match.
	ms.FeeRateBase = matchInfo.match.FeeRateBase
	ms.FeeRateQuote = matchInfo.match.FeeRateQuote
	set.matchInfos = append(set.matchInfos, matchInfo)
	return set
}

// Either a market or limit order taker, and any number of makers.
func tMultiMatchSet(matchQtys, rates []uint64, makerSell bool, isMarket bool) *tMatchSet {
	var sum uint64
	for _, v := range matchQtys {
		sum += v
	}
	// Taker order can be > sum of match amounts
	taker := tNewUser("taker")
	var takerOrder order.Order
	if isMarket {
		takerOrder = makeMarketOrder(sum*5/4, taker, !makerSell)
	} else {
		takerOrder = makeLimitOrder(sum*5/4, rates[0], taker, !makerSell)
	}
	set := new(tMatchSet)
	now := uint64(time.Now().UnixMilli())
	for i, v := range matchQtys {
		maker := tNewUser("maker" + strconv.Itoa(i))
		// Alternate market and limit orders
		makerOrder := makeLimitOrder(v, rates[i], maker, makerSell)
		matchInfo := tMatchInfo(maker, taker, v, rates[i], makerOrder, takerOrder)
		matchInfo.match.Epoch.Idx = now
		matchInfo.match.Epoch.Dur = 1
		set.add(matchInfo)
	}
	return set
}

// tSwap is the information needed for spoofing a swap transaction.
type tSwap struct {
	coin     *asset.Contract
	req      *msgjson.Message
	contract string
}

var (
	tConfsSpoofer     int64
	tValSpoofer       uint64 = 1
	tRecipientSpoofer        = ""
	tLockTimeSpoofer  time.Time
)

func tNewSwap(matchInfo *tMatch, oid order.OrderID, recipient string, user *tUser) *tSwap {
	auditVal := matchInfo.qty
	if isQuoteSwap(user, matchInfo.match) {
		auditVal = matcher.BaseToQuote(matchInfo.rate, matchInfo.qty)
	}
	coinID := randBytes(36)
	coin := &TCoin{
		feeRate:   1,
		confs:     tConfsSpoofer,
		auditAddr: recipient + tRecipientSpoofer,
		auditVal:  auditVal * tValSpoofer,
		id:        coinID,
	}

	contract := &asset.Contract{
		Coin:        coin,
		SwapAddress: recipient + tRecipientSpoofer,
		TxData:      encode.RandomBytes(100),
		SecretHash:  matchInfo.secretHash,
	}

	contract.LockTime = encode.DropMilliseconds(matchInfo.match.Epoch.End().Add(dex.LockTimeTaker(dex.Testnet)))
	if user == matchInfo.maker {
		contract.LockTime = encode.DropMilliseconds(matchInfo.match.Epoch.End().Add(dex.LockTimeMaker(dex.Testnet)))
	}

	if !tLockTimeSpoofer.IsZero() {
		contract.LockTime = tLockTimeSpoofer
	}

	script := "01234567" + user.sigHex
	req, _ := msgjson.NewRequest(nextID(), msgjson.InitRoute, &msgjson.Init{
		OrderID: oid[:],
		MatchID: matchInfo.matchID[:],
		// We control what the backend returns, so the txid doesn't matter right now.
		CoinID: coinID,
		//Time:     uint64(time.Now().UnixMilli()),
		Contract: dirtyEncode(script),
	})

	return &tSwap{
		coin:     contract,
		req:      req,
		contract: script,
	}
}

func isQuoteSwap(user *tUser, match *order.Match) bool {
	makerSell := match.Maker.Sell
	isMaker := user.acct == match.Maker.User()
	return (isMaker && !makerSell) || (!isMaker && makerSell)
}

func randBytes(len int) []byte {
	bytes := make([]byte, len)
	rand.Read(bytes)
	return bytes
}

// tRedeem is the information needed to spoof a redemption transaction.
type tRedeem struct {
	req        *msgjson.Message
	coin       *TCoin
	cpSwapCoin *asset.Contract
}

func tNewRedeem(matchInfo *tMatch, oid order.OrderID, user *tUser) *tRedeem {
	coinID := randBytes(36)
	req, _ := msgjson.NewRequest(nextID(), msgjson.RedeemRoute, &msgjson.Redeem{
		OrderID: oid[:],
		MatchID: matchInfo.matchID[:],
		CoinID:  coinID,
		//Time:    uint64(time.Now().UnixMilli()),
	})
	var cpSwapCoin *asset.Contract
	switch user.acct {
	case matchInfo.maker.acct:
		cpSwapCoin = matchInfo.db.takerSwap.coin
	case matchInfo.taker.acct:
		cpSwapCoin = matchInfo.db.makerSwap.coin
	default:
		panic("wrong user")
	}
	return &tRedeem{
		req:        req,
		coin:       &TCoin{id: coinID},
		cpSwapCoin: cpSwapCoin,
	}
}

// Create a closure that will call t.Fatal if an error is non-nil.
func makeEnsureNilErr(t *testing.T) func(error) {
	return func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestMain(m *testing.M) {
	fastRecheckInterval = time.Millisecond * 40
	taperedRecheckInterval = time.Millisecond * 40
	txWaitExpiration = time.Millisecond * 200
	tBcastTimeout = txWaitExpiration * 5
	minBlockPeriod = tBcastTimeout / 3
	logger := dex.StdOutLogger("TEST", dex.LevelTrace)
	UseLogger(logger)
	db.UseLogger(logger)
	matcher.UseLogger(logger)
	comms.UseLogger(logger)
	var shutdown func()
	testCtx, shutdown = context.WithCancel(context.Background())
	code := m.Run()
	shutdown()
	os.Exit(code)
}

func TestFatalStorageErr(t *testing.T) {
	rig, cleanup := tNewTestRig(nil)
	defer cleanup()

	// Test a fatal storage backend error that causes the main loop to return
	// first, the anomalous shutdown route.
	rig.storage.fatalBackendErr(errors.New("backend error"))

	select {
	case <-rig.swapperDone:
	case <-time.After(time.Second):
		t.Fatalf("swapper worker did not stop after fatal storage error")
	}
}

func TestTrackMatchesAfterStop(t *testing.T) {
	rig, cleanup := tNewTestRig(nil)
	cleanup() // Stop the worker before registering committed matches.

	set := tPerfectLimitLimit(uint64(1e8), uint64(1e8), true)
	match := set.matchInfos[0].match
	rig.swapper.TrackMatches([]*order.MatchSet{set.matchSet})
	rig.swapper.matchMtx.RLock()
	tracked := rig.swapper.matches[match.ID()]
	makerTracked := rig.swapper.userMatches[match.Maker.User()][match.ID()]
	takerTracked := rig.swapper.userMatches[match.Taker.User()][match.ID()]
	rig.swapper.matchMtx.RUnlock()
	if tracked == nil || makerTracked != tracked || takerTracked != tracked {
		t.Fatal("committed match was not tracked after worker stopped")
	}
	for _, ord := range []order.Order{match.Maker, match.Taker} {
		assetID := ord.Quote()
		if ord.Trade().Sell {
			assetID = ord.Base()
		}
		for _, coin := range ord.Trade().Coins {
			if !rig.swapper.coins[assetID].Locker.CoinLocked(coin) {
				t.Fatalf("funding coin %x was not locked after worker stopped", coin)
			}
		}
	}
}

func testSwap(t *testing.T, rig *testRig) {
	t.Helper()
	ensureNilErr := makeEnsureNilErr(t)

	sendBlock := func(node *TBackend) {
		node.bChan <- &asset.BlockUpdate{Err: nil}
	}

	// Step through the negotiation process. No errors should be generated.
	var takerAcked bool
	for _, matchInfo := range rig.matches.matchInfos {
		rig.matchInfo = matchInfo
		ensureNilErr(rig.ackMatch_maker(true))
		if !takerAcked {
			ensureNilErr(rig.ackMatch_taker(true))
			takerAcked = true
		}

		// Assuming market's base asset is abc, quote is xyz
		makerSwapAsset, takerSwapAsset := rig.xyz, rig.abc
		if matchInfo.match.Maker.Sell {
			makerSwapAsset, takerSwapAsset = rig.abc, rig.xyz
		}

		ensureNilErr(rig.sendSwap_maker(true))
		ensureNilErr(rig.auditSwap_taker())
		ensureNilErr(rig.ackAudit_taker(true))
		matchInfo.db.makerSwap.coin.Coin.(*TCoin).setConfs(int64(makerSwapAsset.SwapConf))
		sendBlock(&makerSwapAsset.Backend.(*TUTXOBackend).TBackend)
		ensureNilErr(rig.sendSwap_taker(true))
		ensureNilErr(rig.auditSwap_maker())
		ensureNilErr(rig.ackAudit_maker(true))
		matchInfo.db.takerSwap.coin.Coin.(*TCoin).setConfs(int64(takerSwapAsset.SwapConf))
		sendBlock(&takerSwapAsset.Backend.(*TUTXOBackend).TBackend)
		ensureNilErr(rig.redeem_maker(true))
		ensureNilErr(rig.ackRedemption_taker(true))
		ensureNilErr(rig.redeem_taker(true))
		ensureNilErr(rig.ackRedemption_maker(true))
	}
}

func TestSwaps(t *testing.T) {
	newRig := func(t *testing.T) *testRig {
		t.Helper()
		rig := tNewUnstartedRig(nil)
		// Keep retries and inactivity penalties out of these successful swaps.
		rig.swapper.bTimeout = time.Hour
		rig.auth.auditReq = make(chan struct{}, 1)
		rig.auth.redeemReceived = make(chan struct{}, 2)
		rig.auth.redemptionReq = make(chan struct{}, 2)
		t.Cleanup(rig.start())
		return rig
	}

	for _, makerSell := range []bool{true, false} {
		sellStr := " buy"
		if makerSell {
			sellStr = " sell"
		}
		t.Run("perfect limit-limit match"+sellStr, func(t *testing.T) {
			rig := newRig(t)
			rig.matches = tPerfectLimitLimit(uint64(1e8), uint64(1e8), makerSell)
			rig.applyMatchesAndRequestAcks(t, rig.matches.matchSet)
			testSwap(t, rig)
		})
		t.Run("perfect limit-market match"+sellStr, func(t *testing.T) {
			rig := newRig(t)
			rig.matches = tPerfectLimitMarket(uint64(1e8), uint64(1e8), makerSell)
			rig.applyMatchesAndRequestAcks(t, rig.matches.matchSet)
			testSwap(t, rig)
		})
		t.Run("imperfect limit-market match"+sellStr, func(t *testing.T) {
			rig := newRig(t)
			// only requirement is that maker val > taker val.
			rig.matches = tMarketPair(uint64(10e8), uint64(2e8), uint64(5e8), makerSell)
			rig.applyMatchesAndRequestAcks(t, rig.matches.matchSet)
			testSwap(t, rig)
		})
		t.Run("imperfect limit-limit match"+sellStr, func(t *testing.T) {
			rig := newRig(t)
			rig.matches = tLimitPair(uint64(10e8), uint64(2e8), uint64(2e8), uint64(5e8), uint64(5e8), makerSell)
			rig.applyMatchesAndRequestAcks(t, rig.matches.matchSet)
			testSwap(t, rig)
		})
		for _, isMarket := range []bool{true, false} {
			marketStr := " limit"
			if isMarket {
				marketStr = " market"
			}
			t.Run("three match set"+sellStr+marketStr, func(t *testing.T) {
				rig := newRig(t)
				matchQtys := []uint64{uint64(1e8), uint64(9e8), uint64(3e8)}
				rates := []uint64{uint64(10e8), uint64(11e8), uint64(12e8)}
				// one taker, 3 makers => 4 'match' requests
				rig.matches = tMultiMatchSet(matchQtys, rates, makerSell, isMarket)

				rig.applyMatchesAndRequestAcks(t, rig.matches.matchSet)
				testSwap(t, rig)
			})
		}
	}
}

func TestInvalidFeeRate(t *testing.T) {
	set := tPerfectLimitLimit(uint64(1e8), uint64(1e8), true)
	matchInfo := set.matchInfos[0]
	rig, cleanup := tNewTestRig(matchInfo)
	defer cleanup()

	rig.auth.swapReceived = make(chan struct{}, 1)

	rig.applyMatchesAndRequestAcks(t, set.matchSet)

	rig.abcNode.invalidFeeRate = true

	// The error will be generated by the chainWaiter thread, so will need to
	// check the response.
	if err := rig.sendSwap_maker(false); err != nil {
		t.Fatal(err)
	}
	timeOutMempool()
	if err := rig.waitChans("invalid fee rate", rig.auth.swapReceived); err != nil {
		t.Fatalf("error waiting for response: %v", err)
	}
	// Should have an rpc error.
	msg, resp := rig.auth.popResp(matchInfo.maker.acct)
	if msg == nil {
		t.Fatalf("no response for missing tx after timeout")
	}
	if resp.Error == nil {
		t.Fatalf("no rpc error for erroneous maker swap %v", resp.Error)
	}
}

func TestTxWaiters(t *testing.T) {
	set := tPerfectLimitLimit(uint64(1e8), uint64(1e8), true)
	matchInfo := set.matchInfos[0]
	rig, cleanup := tNewTestRig(matchInfo)
	defer cleanup()

	rig.auth.auditReq = make(chan struct{}, 1)
	rig.auth.redeemReceived = make(chan struct{}, 1)
	rig.auth.redemptionReq = make(chan struct{}, 1)
	rig.auth.swapReceived = make(chan struct{}, 1)

	ensureNilErr := makeEnsureNilErr(t)
	dummyError := fmt.Errorf("test error")

	sendBlock := func(node *TBackend) {
		node.bChan <- &asset.BlockUpdate{Err: nil}
	}

	rig.applyMatchesAndRequestAcks(t, set.matchSet)

	// Get the MatchNotifications that the swapper sent to the clients and check
	// the match notification length, content, IDs, etc.
	if err := rig.ackMatch_maker(true); err != nil {
		t.Fatal(err)
	}
	if err := rig.ackMatch_taker(true); err != nil {
		t.Fatal(err)
	}

	// Set a non-latency error.
	rig.abcNode.setContractErr(dummyError)
	rig.sendSwap_maker(false)
	if err := rig.waitChans("maker contract error", rig.auth.swapReceived); err != nil {
		t.Fatalf("error waiting for maker swap error response: %v", err)
	}
	msg, _ := rig.auth.popResp(matchInfo.maker.acct)
	if msg == nil {
		t.Fatalf("no response for erroneous maker swap")
	}

	// Set an error for the maker's swap asset
	rig.abcNode.setContractErr(asset.CoinNotFoundError)
	// The error will be generated by the chainWaiter thread, so will need to
	// check the response.
	if err := rig.sendSwap_maker(false); err != nil {
		t.Fatal(err)
	}

	// Until recorded, an overlapping request remains in progress.
	dupSwapReq := *matchInfo.db.makerSwap.req
	dupSwapReq.ID = nextID()
	if rpcErr := rig.swapper.handleInit(matchInfo.maker.acct, &dupSwapReq); rpcErr == nil || rpcErr.Code != msgjson.DuplicateRequestError {
		t.Fatalf("pending init retry error = %v, want DuplicateRequestError", rpcErr)
	}

	// Now timeout the initial search.
	timeOutMempool()
	if err := rig.waitChans("maker mempool timeout error", rig.auth.swapReceived); err != nil {
		t.Fatalf("error waiting for maker swap error response: %v", err)
	}
	// Should have an rpc error.
	msg, resp := rig.auth.popResp(matchInfo.maker.acct)
	if msg == nil {
		t.Fatalf("no response for missing tx after timeout")
	}
	if resp.Error == nil {
		t.Fatalf("no rpc error for erroneous maker swap")
	}

	rig.abcNode.setContractErr(nil)
	// Everything should work now.
	if err := rig.sendSwap_maker(true); err != nil {
		t.Fatal(err)
	}
	matchInfo.db.makerSwap.coin.Coin.(*TCoin).setConfs(int64(rig.abc.SwapConf))
	sendBlock(&rig.abc.Backend.(*TUTXOBackend).TBackend)
	if err := rig.auditSwap_taker(); err != nil {
		t.Fatal(err)
	}
	if err := rig.ackAudit_taker(true); err != nil {
		t.Fatal(err)
	}
	// Non-latency error.
	rig.xyzNode.setContractErr(dummyError)
	rig.sendSwap_taker(false)
	if err := rig.waitChans("taker contract error", rig.auth.swapReceived); err != nil {
		t.Fatalf("error waiting for taker swap error response: %v", err)
	}
	msg, _ = rig.auth.popResp(matchInfo.taker.acct)
	if msg == nil {
		t.Fatalf("no response for erroneous taker swap")
	}
	// For the taker swap, simulate latency.
	rig.xyzNode.setContractErr(asset.CoinNotFoundError)
	ensureNilErr(rig.sendSwap_taker(false))
	// Wait a tick
	tickMempool()
	// There should not be a response yet.
	msg, _ = rig.auth.popResp(matchInfo.taker.acct)
	if msg != nil {
		t.Fatalf("unexpected response for latent taker swap")
	}
	// Clear the error.
	rig.xyzNode.setContractErr(nil)
	tickMempool()
	if err := rig.waitChans("taker mempool timeout error", rig.auth.swapReceived); err != nil {
		t.Fatalf("error waiting for taker timeout error response: %v", err)
	}

	msg, resp = rig.auth.popResp(matchInfo.taker.acct)
	if msg == nil {
		t.Fatalf("no response for ok taker swap")
	}
	if resp.Error != nil {
		t.Fatalf("unexpected rpc error for ok taker swap. code: %d, msg: %s",
			resp.Error.Code, resp.Error.Message)
	}
	matchInfo.db.takerSwap.coin.Coin.(*TCoin).setConfs(int64(rig.xyz.SwapConf))
	sendBlock(&rig.xyz.Backend.(*TUTXOBackend).TBackend)

	ensureNilErr(rig.auditSwap_maker())
	ensureNilErr(rig.ackAudit_maker(true))

	// Set a transaction error for the maker's redemption.
	rig.xyzNode.setRedemptionErr(asset.CoinNotFoundError)

	ensureNilErr(rig.redeem_maker(false))
	dupRedeemReq := *matchInfo.db.makerRedeem.req
	dupRedeemReq.ID = nextID()
	if rpcErr := rig.swapper.handleRedeem(matchInfo.maker.acct, &dupRedeemReq); rpcErr == nil || rpcErr.Code != msgjson.DuplicateRequestError {
		t.Fatalf("pending redeem retry error = %v, want DuplicateRequestError", rpcErr)
	}
	tickMempool()
	tickMempool()
	msg, _ = rig.auth.popResp(matchInfo.maker.acct)
	if msg != nil {
		t.Fatalf("unexpected response for latent maker redeem")
	}
	// Clear the error.
	rig.xyzNode.setRedemptionErr(nil)
	tickMempool()
	if err := rig.waitChans("maker redemption error", rig.auth.redeemReceived); err != nil {
		t.Fatalf("error waiting for taker timeout error response: %v", err)
	}
	msg, resp = rig.auth.popResp(matchInfo.maker.acct)
	if msg == nil {
		t.Fatalf("no response for erroneous maker redeem")
	}
	if resp.Error != nil {
		t.Fatalf("unexpected rpc error for erroneous maker redeem. code: %d, msg: %s",
			resp.Error.Code, resp.Error.Message)
	}
	// Back to the taker, but let it timeout first, and then rebroadcast.
	// Get the tracker now, since it will be removed from the match dict if
	// everything goes right
	tracker := rig.getTracker()
	ensureNilErr(rig.ackRedemption_taker(true))
	rig.abcNode.setRedemptionErr(asset.CoinNotFoundError)
	ensureNilErr(rig.redeem_taker(false))
	timeOutMempool()
	if err := rig.waitChans("taker redemption timeout", rig.auth.redeemReceived); err != nil {
		t.Fatalf("error waiting for taker timeout error response: %v", err)
	}
	msg, _ = rig.auth.popResp(matchInfo.taker.acct)
	if msg == nil {
		t.Fatalf("no response for erroneous taker redeem")
	}
	rig.abcNode.setRedemptionErr(nil)
	ensureNilErr(rig.redeem_taker(true))
	tickMempool()
	tickMempool()
	ensureNilErr(rig.ackRedemption_maker(true))
	// Set the number of confirmations on the redemptions.
	matchInfo.db.makerRedeem.coin.setConfs(int64(rig.xyz.SwapConf))
	matchInfo.db.takerRedeem.coin.setConfs(int64(rig.abc.SwapConf))
	// send a block through for either chain to trigger a completion check.
	rig.xyzNode.bChan <- &asset.BlockUpdate{Err: nil}
	tickMempool()
	if tracker.Status != order.MatchComplete {
		t.Fatalf("match not marked as complete: %d", tracker.Status)
	}
	// Make sure that the tracker is removed from swappers match map.
	if rig.getTracker() != nil {
		t.Fatalf("matchTracker not removed from swapper's match map")
	}
}

// TestProcessBlockScansAllMatches verifies that a block notification updates
// every match on the block asset even when the scan also holds matches on
// unrelated assets. Map iteration is unordered, so an aborted scan can escape
// detection if all confirmable matches happen to be visited first.
func TestProcessBlockScansAllMatches(t *testing.T) {
	set := tMultiMatchSet([]uint64{1e8, 2e8, 3e8}, []uint64{5e7, 5e7, 5e7}, true, false)
	rig := tNewUnstartedRig(set.matchInfos[0])
	swapper := rig.swapper
	now := time.Now().UTC()

	// Confirmable maker swaps on the block's asset.
	swapper.TrackMatches([]*order.MatchSet{set.matchSet})
	trackers := swapper.matchSlice()
	for _, mt := range trackers {
		mt.Status = order.MakerSwapCast
		mt.makerStatus.swap = &asset.Contract{Coin: &TCoin{id: randBytes(36), confs: int64(rig.abc.SwapConf)}}
		mt.makerStatus.swapTime = now
	}

	// Decoy matches touching neither side of the block asset.
	base := set.matchInfos[0].match
	for i := 0; i < 12; i++ {
		decoy := &order.Match{
			Maker:    base.Maker,
			Taker:    base.Taker,
			Quantity: base.Quantity + uint64(i+1),
			Rate:     base.Rate,
			Epoch:    base.Epoch,
			Status:   order.NewlyMatched,
		}
		swapper.addMatch(&matchTracker{
			Match:       decoy,
			time:        now,
			matchTime:   now,
			makerStatus: &swapStatus{swapAsset: XYZID, redeemAsset: ACCTID},
			takerStatus: &swapStatus{swapAsset: ACCTID, redeemAsset: XYZID},
		})
	}

	swapper.processBlock(context.Background(), &blockNotification{time: now, assetID: ABCID})

	for i, mt := range trackers {
		if mt.makerStatus.swapConfTime().IsZero() {
			t.Fatalf("match %d not confirmed by the block scan", i)
		}
	}
}

func TestSwapConfirmationRetainsFundingLocks(t *testing.T) {
	for _, tt := range []struct {
		name    string
		status  order.MatchStatus
		assetID uint32
	}{
		{name: "maker", status: order.MakerSwapCast, assetID: ABCID},
		{name: "taker", status: order.TakerSwapCast, assetID: XYZID},
	} {
		t.Run(tt.name, func(t *testing.T) {
			set := tPerfectLimitLimit(uint64(1e8), uint64(1e8), true)
			mi := set.matchInfos[0]
			rig := tNewUnstartedRig(mi)
			swapper := rig.swapper
			now := time.Now().UTC()

			var ord order.Order = mi.match.Maker
			if tt.status == order.TakerSwapCast {
				ord = mi.match.Taker
			}
			coin := order.CoinID(randBytes(36))
			ord.Trade().Coins = []order.CoinID{coin}
			swapperAsset := swapper.coins[tt.assetID]
			locker := swapperAsset.Locker
			swapper.TrackMatches([]*order.MatchSet{set.matchSet})
			if !locker.CoinLocked(coin) {
				t.Fatal("funding coin not locked after tracking match")
			}

			mt := rig.getTracker()
			mt.Status = tt.status
			status := mt.makerStatus
			if tt.status == order.TakerSwapCast {
				status = mt.takerStatus
			}
			status.swap = &asset.Contract{Coin: &TCoin{id: randBytes(36), confs: int64(swapperAsset.SwapConf)}}
			status.swapTime = now

			swapper.processBlock(context.Background(), &blockNotification{time: now, assetID: tt.assetID})
			if got := status.swapConfTime(); !got.Equal(now) {
				t.Fatalf("confirmation time = %v, want %v", got, now)
			}
			if !locker.CoinLocked(coin) {
				t.Fatal("swap confirmation unlocked funding coin")
			}

			swapper.matchMtx.Lock()
			swapper.deleteMatch(mt)
			swapper.matchMtx.Unlock()
			if locker.CoinLocked(coin) {
				t.Fatal("match deletion did not unlock funding coin")
			}
		})
	}
}

func TestBroadcastTimeouts(t *testing.T) {
	rig, cleanup := tNewTestRig(nil)
	defer cleanup()

	rig.auth.newSuspend = make(chan struct{}, 1)
	rig.auth.auditReq = make(chan struct{}, 1)
	rig.auth.redemptionReq = make(chan struct{}, 1)
	// Buffered; Send is non-blocking if this fills.
	rig.auth.newNtfn = make(chan struct{}, 20)

	ensureNilErr := makeEnsureNilErr(t)
	sendBlock := func(node *TBackend) {
		node.bChan <- &asset.BlockUpdate{Err: nil}
	}

	ntfnWait := func(timeout time.Duration) {
		t.Helper()
		select {
		case <-rig.auth.newNtfn:
		case <-time.After(timeout):
			t.Fatalf("no notification received")
		}
	}

	checkRevokeMatch := func(user *tUser, i int) {
		t.Helper()
		rev := new(msgjson.RevokeMatch)
		err := rig.auth.getNtfn(user.acct, msgjson.RevokeMatchRoute, rev)
		if err != nil {
			t.Fatalf("failed to get revoke_match ntfn: %v", err)
		}
		if err = checkSigS256(rev, rig.auth.privkey.PubKey()); err != nil {
			t.Fatalf("incorrect server signature: %v", err)
		}
		if !bytes.Equal(rev.MatchID, rig.matchInfo.matchID[:]) {
			t.Fatalf("unexpected revocation match ID for %s at step %d. expected %s, got %s",
				user.lbl, i, rig.matchInfo.matchID, rev.MatchID)
		}

		// TODO: expect revoke order for at-fault user
	}

	// tryExpire waits for a BroadcastTimeout failure and then checks that a
	// revoke_match message is sent to both users.
	tryExpire := func(i, j int, jerk, victim *tUser, node *TBackend) bool {
		t.Helper()
		if i != j {
			return false
		}
		// Sending a block through should schedule an inaction check after duration
		// BroadcastTimeout.
		sendBlock(node)
		deadline := time.After(rig.swapper.bTimeout * 3)
		for !rig.auth.hasNtfn(jerk.acct, msgjson.RevokeMatchRoute) ||
			!rig.auth.hasNtfn(victim.acct, msgjson.RevokeMatchRoute) {
			select {
			case <-rig.auth.newNtfn:
			case <-deadline:
				t.Fatalf("no revoke_match notification")
			}
		}
		checkRevokeMatch(jerk, i)
		checkRevokeMatch(victim, i)
		rig.storage.mtx.Lock()
		failures := append([]*meshevents.MatchFailedEvent(nil), rig.storage.matchFailures...)
		rig.storage.mtx.Unlock()
		if len(failures) == 0 {
			t.Fatal("timeout did not record a failure")
		}
		failed := failures[len(failures)-1]
		wantStatus := []order.MatchStatus{order.NewlyMatched, order.MakerSwapCast, order.TakerSwapCast, order.MakerRedeemed}[i]
		wantFault := []meshevents.MatchFailureFault{meshevents.MatchFailureMakerFault, meshevents.MatchFailureTakerFault,
			meshevents.MatchFailureMakerFault, meshevents.MatchFailureTakerFault}[i]
		if failed.MatchID != rig.matchInfo.matchID || failed.Status != wantStatus || failed.Fault != wantFault {
			t.Fatalf("timeout recorded wrong failure: %+v", failed)
		}
		return true
	}
	// Run a timeout test after every important step.
	for i := 0; i <= 3; i++ {
		set := tPerfectLimitLimit(uint64(1e8), uint64(1e8), true) // same orders, different users
		matchInfo := set.matchInfos[0]
		rig.matchInfo = matchInfo
		rig.applyMatchesAndRequestAcks(t, set.matchSet)
		// Step through the negotiation process. No errors should be generated.
		// TODO: timeout each match ack, not block based inaction.

		ensureNilErr(rig.ackMatch_maker(true))
		ensureNilErr(rig.ackMatch_taker(true))

		// Drain counterparty_address notifications generated by the ack flow.
		ntfnWait(time.Second)
		ntfnWait(time.Second)

		// Timeout waiting for maker swap.
		if tryExpire(i, 0, matchInfo.maker, matchInfo.taker, &rig.abcNode.TBackend) {
			continue
		}

		ensureNilErr(rig.sendSwap_maker(true))

		// Pull the server's 'audit' request from the comms queue.
		ensureNilErr(rig.auditSwap_taker())

		// NOTE: timeout on the taker's audit ack response itself does not cause
		// a revocation. The taker not broadcasting their swap when maker's swap
		// reaches swapconf plus bTimeout is the trigger.
		ensureNilErr(rig.ackAudit_taker(true))

		// Maker's swap reaches swapConf.
		matchInfo.db.makerSwap.coin.Coin.(*TCoin).setConfs(int64(rig.abc.SwapConf))
		sendBlock(&rig.abcNode.TBackend) // tryConfirmSwap
		// With maker swap confirmed, inaction happens bTimeout after
		// swapConfirmed time.
		if tryExpire(i, 1, matchInfo.taker, matchInfo.maker, &rig.xyzNode.TBackend) {
			continue
		}

		ensureNilErr(rig.sendSwap_taker(true))

		// Pull the server's 'audit' request from the comms queue
		ensureNilErr(rig.auditSwap_maker())

		// NOTE: timeout on the maker's audit ack response itself does not cause
		// a revocation. The maker not broadcasting their redeem when taker's
		// swap reaches swapconf plus bTimeout is the trigger.
		ensureNilErr(rig.ackAudit_maker(true))

		// Taker's swap reaches swapConf.
		matchInfo.db.takerSwap.coin.Coin.(*TCoin).setConfs(int64(rig.xyz.SwapConf))
		sendBlock(&rig.xyzNode.TBackend)
		// With taker swap confirmed, inaction happens bTimeout after
		// swapConfirmed time.
		if tryExpire(i, 2, matchInfo.maker, matchInfo.taker, &rig.xyzNode.TBackend) {
			continue
		}

		ensureNilErr(rig.redeem_maker(true))

		// Pull the server's 'redemption' request from the comms queue
		ensureNilErr(rig.ackRedemption_taker(true))

		// Maker's redeem reaches swapConf. Not necessary for taker redeem.
		// matchInfo.db.makerRedeem.coin.setConfs(int64(rig.xyz.SwapConf))
		// sendBlock(rig.xyzNode)
		if tryExpire(i, 3, matchInfo.taker, matchInfo.maker, &rig.abcNode.TBackend) {
			continue
		}

		// Next is redeem_taker... not a block-based inaction.

		return
	}
}

func TestSigErrors(t *testing.T) {
	dummyError := fmt.Errorf("test error")
	set := tPerfectLimitLimit(uint64(1e8), uint64(1e8), true)
	matchInfo := set.matchInfos[0]
	rig, cleanup := tNewTestRig(matchInfo)
	defer cleanup()

	rig.auth.auditReq = make(chan struct{}, 1)
	rig.auth.redemptionReq = make(chan struct{}, 1)
	rig.auth.redeemReceived = make(chan struct{}, 1)
	rig.auth.swapReceived = make(chan struct{}, 1)

	rig.applyMatchesAndRequestAcks(t, set.matchSet) // pushes a new req to m.reqs
	ensureNilErr := makeEnsureNilErr(t)

	// We need a way to restore the state of the queue after testing an auth error.
	var tReq *TRequest
	var msg *msgjson.Message
	apply := func(user *tUser) {
		if msg != nil {
			rig.auth.pushResp(user.acct, msg)
		}
		if tReq != nil {
			rig.auth.pushReq(user.acct, tReq)
		}
	}
	stash := func(user *tUser) {
		msg, _ = rig.auth.popResp(user.acct)
		tReq = rig.auth.popReq(user.acct)
		apply(user) // put them back now that we have a copy
	}
	testAction := func(stepFunc func(bool) error, user *tUser, failChans ...chan struct{}) {
		t.Helper()
		// First do it with an auth error.
		rig.auth.verifyErr = dummyError
		stash(user) // make a copy of this user's next req/resp pair
		// The error will be pulled from the auth manager.
		_ = stepFunc(false) // popReq => req.respFunc(..., tNewResponse()) => send error to client (m.resps)
		if err := rig.waitChans("testAction", failChans...); err != nil {
			t.Fatalf("testAction failChan wait error: %v", err)
		}
		// ensureSigErr makes sure that the specified user has a signature error
		// response from the swapper.
		ensureNilErr(rig.checkServerResponseFail(user, msgjson.SignatureError))
		// Again with no auth error to go to the next step.
		rig.auth.verifyErr = nil
		apply(user) // restore the initial live request
		ensureNilErr(stepFunc(true))
	}
	maker, taker := matchInfo.maker, matchInfo.taker
	// 1 live match, 2 pending client acks
	testAction(rig.ackMatch_maker, maker)
	testAction(rig.ackMatch_taker, taker)
	testAction(rig.sendSwap_maker, maker, rig.auth.swapReceived)
	ensureNilErr(rig.auditSwap_taker())
	testAction(rig.ackAudit_taker, taker)
	testAction(rig.sendSwap_taker, taker, rig.auth.swapReceived)
	ensureNilErr(rig.auditSwap_maker())
	testAction(rig.ackAudit_maker, maker)
	testAction(rig.redeem_maker, maker, rig.auth.redeemReceived)
	testAction(rig.ackRedemption_taker, taker)
	testAction(rig.redeem_taker, taker, rig.auth.redeemReceived)
	testAction(rig.ackRedemption_maker, maker)
}

func TestMalformedSwap(t *testing.T) {
	set := tPerfectLimitLimit(uint64(1e8), uint64(1e8), true)
	matchInfo := set.matchInfos[0]
	rig, cleanup := tNewTestRig(matchInfo)
	defer cleanup()

	rig.auth.swapReceived = make(chan struct{}, 1)

	rig.applyMatchesAndRequestAcks(t, set.matchSet)
	ensureNilErr := makeEnsureNilErr(t)

	ensureNilErr(rig.ackMatch_maker(true))
	ensureNilErr(rig.ackMatch_taker(true))

	ensureErr := func(tag string) {
		t.Helper()
		ensureNilErr(rig.sendSwap_maker(false))
		ensureNilErr(rig.waitChans(tag, rig.auth.swapReceived))
	}

	// Bad contract value
	tValSpoofer = 2
	ensureErr("bad maker contract")
	ensureNilErr(rig.checkServerResponseFail(matchInfo.maker, msgjson.ContractError))
	tValSpoofer = 1
	// Bad contract recipient
	tRecipientSpoofer = "2"
	ensureErr("bad recipient")
	ensureNilErr(rig.checkServerResponseFail(matchInfo.maker, msgjson.ContractError))
	tRecipientSpoofer = ""
	// Bad locktime
	tLockTimeSpoofer = time.Unix(1, 0)
	ensureErr("bad locktime")
	ensureNilErr(rig.checkServerResponseFail(matchInfo.maker, msgjson.ContractError))
	tLockTimeSpoofer = time.Time{}

	// Low fee, unconfirmed
	rig.abcNode.invalidFeeRate = true
	ensureErr("low fee")
	ensureNilErr(rig.checkServerResponseFail(matchInfo.maker, msgjson.ContractError))

	// Works with low fee, but now a confirmation.
	tConfsSpoofer = 1
	defer func() { tConfsSpoofer = 0 }()
	ensureNilErr(rig.sendSwap_maker(true))
}

func TestRetriesDuringSwap(t *testing.T) {
	rig, cleanup := tNewTestRig(nil)
	defer cleanup()

	ensureNilErr := makeEnsureNilErr(t)
	// retryUntilSuccess will retry action until success or timeout.
	retryUntilSuccess := func(action func() error, onSuccess, onFail func()) {
		startWaitingTime := time.Now()
		for {
			err := action()
			if err == nil {
				// We are done, finally got a retry attempt that worked.
				onSuccess()
				break
			}
			onFail()

			if time.Since(startWaitingTime) > 10*time.Second {
				t.Fatalf("timed out retrying, err: %v", err)
			}
			time.Sleep(100 * time.Millisecond)
		}
	}

	match := tPerfectLimitLimit(uint64(1e8), uint64(1e8), false)
	rig.matches = match
	rig.matchInfo = match.matchInfos[0]
	rig.auth.swapReceived = make(chan struct{}, 1)
	rig.auth.auditReq = make(chan struct{}, 1)
	rig.auth.redeemReceived = make(chan struct{}, 1)
	rig.auth.redemptionReq = make(chan struct{}, 1)
	rig.applyMatchesAndRequestAcks(t, rig.matches.matchSet)

	ensureNilErr(rig.ackMatch_maker(true))
	ensureNilErr(rig.ackMatch_taker(true))

	ensureNilErr(rig.sendSwap_maker(true))
	ensureNilErr(rig.auditSwap_taker())
	tracker := rig.getTracker()
	// We're "rewinding time" back NewlyMatched status to be able to retry sending
	// the same init request again.
	tracker.mtx.Lock()
	tracker.Status = order.NewlyMatched
	tracker.mtx.Unlock()
	retryUntilSuccess(func() error {
		// We might get a couple of duplicate init request errors here, in that case
		// we simply retry request later.
		// We need to wait for swapSearching semaphore (it coordinates init request
		// handling) to clear, so our retry request should eventually work (it has
		// adequate fee and 0 confs).
		return rig.sendSwap_maker(false)
	}, func() {
		// On success, executes once.
		ensureNilErr(rig.ensureSwapStatus("server received our swap -> counterparty got audit request",
			order.MakerSwapCast, rig.auth.swapReceived, rig.auth.auditReq))
		ensureNilErr(rig.checkServerResponseSuccess(rig.matchInfo.maker))
		ensureNilErr(rig.auditSwap_taker())
	}, func() {
		// On every failure.
		ensureNilErr(rig.ensureSwapStatus("server received our swap",
			order.NewlyMatched, rig.auth.swapReceived))
		ensureNilErr(rig.checkServerResponseFail(rig.matchInfo.maker, msgjson.DuplicateRequestError))
	})

	ensureNilErr(rig.ackAudit_taker(true))
	ensureNilErr(rig.sendSwap_taker(true))
	ensureNilErr(rig.auditSwap_maker())
	tracker = rig.getTracker()
	// We're "rewinding time" back MakerSwapCast status to be able to retry sending
	// the same init request again.
	tracker.mtx.Lock()
	tracker.Status = order.MakerSwapCast
	tracker.mtx.Unlock()
	retryUntilSuccess(func() error {
		// We might get a couple of duplicate init request errors here, in that case
		// we simply retry request later.
		// We need to wait for swapSearching semaphore (it coordinates init request
		// handling) to clear, so our retry request should eventually work (it has
		// adequate fee and 0 confs).
		return rig.sendSwap_taker(false)
	}, func() {
		// On success, executes once.
		ensureNilErr(rig.ensureSwapStatus("server received our swap -> counterparty got audit request",
			order.TakerSwapCast, rig.auth.swapReceived, rig.auth.auditReq))
		ensureNilErr(rig.checkServerResponseSuccess(rig.matchInfo.taker))
		ensureNilErr(rig.auditSwap_maker())
	}, func() {
		// On every failure.
		ensureNilErr(rig.ensureSwapStatus("server received our swap",
			order.MakerSwapCast, rig.auth.swapReceived))
		ensureNilErr(rig.checkServerResponseFail(rig.matchInfo.taker, msgjson.DuplicateRequestError))
	})

	ensureNilErr(rig.ackAudit_maker(true))

	ensureNilErr(rig.redeem_maker(true))
	ensureNilErr(rig.ackRedemption_taker(true))
	tracker = rig.getTracker()
	// We're "rewinding time" back TakerSwapCast status to be able to retry sending
	// the same redeem request again.
	tracker.mtx.Lock()
	tracker.Status = order.TakerSwapCast
	tracker.mtx.Unlock()
	retryUntilSuccess(func() error {
		// We might get a couple of duplicate redeem request errors here, in that case
		// we simply retry request later.
		// We need to wait for redeemSearching semaphore (it coordinates redeem request
		// handling) to clear, so our retry request should eventually work.
		return rig.redeem_maker(false)
	}, func() {
		// On success, executes once.
		ensureNilErr(rig.ensureSwapStatus("server received our redeem -> counterparty got redemption request",
			order.MakerRedeemed, rig.auth.redeemReceived, rig.auth.redemptionReq))
		ensureNilErr(rig.checkServerResponseSuccess(rig.matchInfo.maker))
		ensureNilErr(rig.ackRedemption_taker(true))
	}, func() {
		// On every failure.
		ensureNilErr(rig.ensureSwapStatus("server received our redeem",
			order.TakerSwapCast, rig.auth.redeemReceived))
		ensureNilErr(rig.checkServerResponseFail(rig.matchInfo.maker, msgjson.DuplicateRequestError))
	})

	ensureNilErr(rig.redeem_taker(true))
	// Retry the original requests after the match has left memory.
	info := rig.matchInfo
	init := new(msgjson.Init)
	ensureNilErr(info.db.makerSwap.req.Unmarshal(init))
	redeem := new(msgjson.Redeem)
	ensureNilErr(info.db.takerRedeem.req.Unmarshal(redeem))
	rig.storage.mtx.Lock()
	contracts, redemptions := len(rig.storage.swapContracts), len(rig.storage.redemptions)
	rig.storage.swapDataByID = map[order.MatchID]*db.SwapDataFull{
		info.matchID: {
			MatchData: &db.MatchData{
				ID: info.matchID, Maker: info.makerOID, MakerAcct: info.maker.acct,
				Taker: info.takerOID, TakerAcct: info.taker.acct,
			},
			SwapData: &db.SwapData{
				ContractACoinID: init.CoinID, ContractA: init.Contract,
				RedeemBCoinID: redeem.CoinID, RedeemASecret: redeem.Secret,
			},
		},
	}
	rig.storage.mtx.Unlock()

	redeemRetry := *info.db.takerRedeem.req
	redeemRetry.ID = nextID()
	if rpcErr := rig.swapper.handleRedeem(info.taker.acct, &redeemRetry); rpcErr != nil {
		t.Fatalf("completed redeem retry failed: %v", rpcErr)
	}
	ensureNilErr(rig.checkServerResponseSuccess(info.taker))

	tickMempool()
	tickMempool()
	ensureNilErr(rig.ackRedemption_maker(true)) // no-op; match already removed

	redeemRetry.ID = nextID()
	if rpcErr := rig.swapper.handleRedeem(info.taker.acct, &redeemRetry); rpcErr != nil {
		t.Fatalf("completed redeem retry failed: %v", rpcErr)
	}
	ensureNilErr(rig.checkServerResponseSuccess(info.taker))

	initRetry := *info.db.makerSwap.req
	initRetry.ID = nextID()
	if rpcErr := rig.swapper.handleInit(info.maker.acct, &initRetry); rpcErr != nil {
		t.Fatalf("completed init retry failed: %v", rpcErr)
	}
	ensureNilErr(rig.checkServerResponseSuccess(info.maker))

	rig.storage.mtx.Lock()
	defer rig.storage.mtx.Unlock()
	if len(rig.storage.swapContracts) != contracts || len(rig.storage.redemptions) != redemptions {
		t.Fatal("retry emitted another settlement event")
	}
}

func TestBadParams(t *testing.T) {
	set := tPerfectLimitLimit(uint64(1e8), uint64(1e8), true)
	matchInfo := set.matchInfos[0]
	rig, cleanup := tNewTestRig(matchInfo)
	defer cleanup()
	rig.applyMatchesAndRequestAcks(t, set.matchSet)
	swapper := rig.swapper
	match := rig.getTracker()
	user := matchInfo.maker
	acker := &messageAcker{
		user:    user.acct,
		match:   match,
		isMaker: true,
		isAudit: true,
	}
	ensureNilErr := makeEnsureNilErr(t)

	ackArr := make([]*msgjson.Acknowledgement, 0)
	matches := []*messageAcker{
		{match: rig.getTracker()},
	}

	encodedAckArray := func() json.RawMessage {
		b, _ := json.Marshal(ackArr)
		return json.RawMessage(b)
	}

	// Invalid result.
	msg, _ := msgjson.NewResponse(1, nil, nil)
	msg.Payload = json.RawMessage(`{"result":?}`)
	swapper.processAck(msg, acker)
	ensureNilErr(rig.checkServerResponseFail(user, msgjson.RPCParseError))
	swapper.processMatchAcks(user.acct, msg, []*messageAcker{})
	ensureNilErr(rig.checkServerResponseFail(user, msgjson.RPCParseError))

	msg, _ = msgjson.NewResponse(1, encodedAckArray(), nil)
	swapper.processMatchAcks(user.acct, msg, matches)
	ensureNilErr(rig.checkServerResponseFail(user, msgjson.AckCountError))
}

// TestAckMeshUnavailable checks that mesh unavailability while recording an
// acknowledgement returns a retryable error to the client.
func TestAckMeshUnavailable(t *testing.T) {
	for _, tt := range []struct {
		name    string
		params  msgjson.Signable
		isAudit bool
	}{
		{name: "audit", params: &msgjson.Audit{}, isAudit: true},
		{name: "redemption", params: &msgjson.Redemption{}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			set := tPerfectLimitLimit(uint64(1e8), uint64(1e8), true)
			info := set.matchInfos[0]
			rig := tNewUnstartedRig(info)
			rig.swapper.TrackMatches([]*order.MatchSet{set.matchSet})
			rig.swapper.SetMeshService(&tSwapMesh{err: fmt.Errorf("drain in progress: %w", mesh.ErrUnavailable)})
			user := info.maker
			acker := &messageAcker{
				user:    user.acct,
				match:   rig.getTracker(),
				params:  tt.params,
				isMaker: true,
				isAudit: tt.isAudit,
			}
			ack := &msgjson.Acknowledgement{MatchID: info.matchID[:], Sig: user.sig}
			msg, err := msgjson.NewResponse(1, ack, nil)
			if err != nil {
				t.Fatal(err)
			}
			rig.swapper.processAck(msg, acker)
			if err := rig.checkServerResponseFail(user, msgjson.TryAgainLaterError); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func tMakerMatchAckRecord(matchInfo *tMatch) meshevents.MatchAckRecord {
	return meshevents.MatchAckRecord{
		MatchID: matchInfo.matchID,
		Base:    matchInfo.match.Maker.BaseAsset,
		Quote:   matchInfo.match.Maker.QuoteAsset,
		Maker:   true,
		Sig:     matchInfo.maker.sig,
		Address: matchInfo.makerPerMatchAddr,
	}
}

func tTakerMatchAckRecord(matchInfo *tMatch) meshevents.MatchAckRecord {
	return meshevents.MatchAckRecord{
		MatchID: matchInfo.matchID,
		Base:    matchInfo.match.Maker.BaseAsset,
		Quote:   matchInfo.match.Maker.QuoteAsset,
		Sig:     matchInfo.taker.sig,
		Address: matchInfo.takerPerMatchAddr,
	}
}

func TestProcessMatchAcksEmitsEvent(t *testing.T) {
	set := tPerfectLimitLimit(uint64(1e8), uint64(1e8), true)
	matchInfo := set.matchInfos[0]
	rig := tNewUnstartedRig(matchInfo)
	tMesh := new(tSwapMesh)
	rig.swapper.SetMeshService(tMesh)
	rig.applyMatchesAndRequestAcks(t, set.matchSet)
	tracker := rig.getTracker()
	initialTime := tracker.time

	req := rig.auth.popReq(matchInfo.maker.acct)
	if req == nil {
		t.Fatalf("no match request sent")
	}
	resp := tNewResponse(req.req.ID, tAckArrWithAddrs(matchInfo.maker, matchInfo.maker.matchIDs,
		map[order.MatchID]string{matchInfo.matchID: matchInfo.makerPerMatchAddr}))
	req.respFunc(nil, resp)

	if len(tMesh.events) != 1 {
		t.Fatalf("expected 1 emitted event, got %d", len(tMesh.events))
	}
	if tMesh.events[0].Kind != meshevents.EventKindMatchAcksRecorded {
		t.Fatalf("event kind = %q, want %q", tMesh.events[0].Kind, meshevents.EventKindMatchAcksRecorded)
	}
	event, err := meshevents.DecodeMatchAcksRecordedEvent(tMesh.events[0].Payload)
	if err != nil {
		t.Fatalf("DecodeMatchAcksRecordedEvent error: %v", err)
	}
	if event.AckTime == 0 {
		t.Fatalf("empty event ack time")
	}
	wantRecords := []meshevents.MatchAckRecord{tMakerMatchAckRecord(matchInfo)}
	if !reflect.DeepEqual(event.Records, wantRecords) {
		t.Fatalf("wrong match ack event records.\nwant: %#v\n got: %#v", wantRecords, event.Records)
	}
	if msg, _ := rig.auth.popResp(matchInfo.maker.acct); msg != nil {
		t.Fatalf("unexpected error response: %v", msg)
	}
	if got := len(matchAckEvents(rig.storage)); got != 0 {
		t.Fatalf("storage writes before event application = %d, want 0", got)
	}
	if len(tracker.Sigs.MakerMatch) != 0 || len(tracker.Sigs.TakerMatch) != 0 ||
		tracker.makerSwapAddr != "" || tracker.takerSwapAddr != "" ||
		!tracker.time.Equal(initialTime) || tracker.counterPartyAddrsSent {
		t.Fatal("match state changed before event application")
	}
	if got := notificationCount(rig.auth, msgjson.CounterPartyAddressRoute); got != 0 {
		t.Fatalf("address notifications before event application = %d, want 0", got)
	}
}

// TestReackKeepsRecordedAddress checks that a re-ack with any other
// address — divergent, empty, or backend-rejected — keeps the recorded
// address in memory and in the stored event, without validating the
// submitted one.
func TestReackKeepsRecordedAddress(t *testing.T) {
	for _, tt := range []struct {
		name   string
		reack  string
		reject bool // backend rejects all addresses; coercion must skip validation
	}{
		{name: "divergent", reack: "divergent-taker-addr"},
		{name: "empty", reack: ""},
		{name: "invalid", reack: "backend-rejected-addr", reject: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			set := tPerfectLimitLimit(uint64(1e8), uint64(1e8), true)
			matchInfo := set.matchInfos[0]
			rig := tNewUnstartedRig(matchInfo)
			rig.swapper.TrackMatches([]*order.MatchSet{set.matchSet})
			tMesh := new(tSwapMesh)
			rig.swapper.SetMeshService(tMesh)
			tracker := rig.getTracker()
			taker := matchInfo.taker

			tracker.mtx.Lock()
			tracker.makerSwapAddr = matchInfo.makerPerMatchAddr
			tracker.takerSwapAddr = matchInfo.takerPerMatchAddr
			tracker.Sigs.MakerMatch = matchInfo.maker.sig
			tracker.Sigs.TakerMatch = taker.sig
			tracker.counterPartyAddrsSent = true
			tracker.mtx.Unlock()

			if tt.reject {
				rig.abcNode.rejectSwapAddrs = true
				rig.xyzNode.rejectSwapAddrs = true
			}

			rig.swapper.RequestMatchAcks([]*order.MatchSet{set.matchSet})
			req := rig.auth.popReq(taker.acct)
			if req == nil || req.req.Route != msgjson.MatchRoute {
				t.Fatal("no match re-request")
			}
			req.respFunc(nil, tNewResponse(req.req.ID, tAckArrWithAddrs(taker,
				[]order.MatchID{matchInfo.matchID},
				map[order.MatchID]string{matchInfo.matchID: tt.reack})))
			if msg, resp := rig.auth.popResp(taker.acct); msg != nil {
				t.Fatalf("unexpected error response: %+v", resp)
			}

			if len(tMesh.events) != 1 {
				t.Fatalf("match ack events = %d, want 1", len(tMesh.events))
			}
			apply := rig.swapper.Events()[meshevents.EventKindMatchAcksRecorded]
			if _, err := apply(&mesh.EventApplyContext{Context: context.Background()}, tMesh.events[0]); err != nil {
				t.Fatal(err)
			}

			tracker.mtx.RLock()
			got := tracker.takerSwapAddr
			tracker.mtx.RUnlock()
			if got != matchInfo.takerPerMatchAddr {
				t.Fatalf("taker addr = %q, want %q", got, matchInfo.takerPerMatchAddr)
			}
			updates := matchAckEvents(rig.storage)
			if len(updates) != 1 || len(updates[0].Records) != 1 ||
				updates[0].Records[0].Address != matchInfo.takerPerMatchAddr {
				t.Fatalf("stored re-ack = %+v, want %q", updates, matchInfo.takerPerMatchAddr)
			}
		})
	}
}

func TestApplySwapContractRecordedEvent(t *testing.T) {
	swapTime := time.UnixMilli(1670000000123).UTC()
	storageErr := errors.New("storage error")
	tests := []struct {
		name       string
		maker      bool
		storageErr error
	}{
		{name: "maker", maker: true},
		{name: "taker"},
		{name: "storage error", maker: true, storageErr: storageErr},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			set := tPerfectLimitLimit(uint64(1e8), uint64(1e8), true)
			info := set.matchInfos[0]
			rig := tNewUnstartedRig(info)
			rig.swapper.TrackMatches([]*order.MatchSet{set.matchSet})
			tracker := rig.getTracker()
			status, otherStatus := tracker.makerStatus, tracker.takerStatus
			before, after := order.NewlyMatched, order.MakerSwapCast
			if !tt.maker {
				status, otherStatus = tracker.takerStatus, tracker.makerStatus
				before, after = order.MakerSwapCast, order.TakerSwapCast
			}
			tracker.Status = before
			rig.storage.saveContractErr = tt.storageErr

			recorded := &meshevents.SwapContractRecordedEvent{
				MatchID:     info.matchID,
				Base:        info.match.Maker.BaseAsset,
				Quote:       info.match.Maker.QuoteAsset,
				Maker:       tt.maker,
				Status:      after,
				CoinID:      []byte("swap-coin"),
				CoinTxID:    "swap-tx",
				CoinString:  "swap-tx:0",
				Value:       1e8,
				FeeRate:     10,
				Contract:    []byte("contract"),
				SwapAddress: "recipient",
				SecretHash:  []byte("secret-hash"),
				LockTime:    swapTime.Add(time.Hour).UnixMilli(),
				TxData:      []byte("transaction"),
				SwapTime:    swapTime.UnixMilli(),
			}
			event, err := mesh.NewEvent(recorded)
			if err != nil {
				t.Fatal(err)
			}
			apply := rig.swapper.Events()[meshevents.EventKindSwapContractRecorded]
			_, err = apply(&mesh.EventApplyContext{Context: context.Background()}, event)
			if !errors.Is(err, tt.storageErr) {
				t.Fatalf("apply error = %v, want %v", err, tt.storageErr)
			}
			if otherStatus.swap != nil || !otherStatus.swapTime.IsZero() {
				t.Fatal("application changed the other party's swap")
			}
			contracts := storedSwapContracts(rig.storage)
			if tt.storageErr != nil {
				if len(contracts) != 0 || tracker.Status != before || status.swap != nil || !status.swapTime.IsZero() {
					t.Fatal("failed storage write changed the recorded swap or match status")
				}
				if len(rig.swapper.activeCoinIDs) != 0 || len(rig.swapper.activeSecretHashes) != 0 ||
					len(rig.swapper.matchCoinIDs) != 0 || len(rig.swapper.matchSecretHashes) != 0 {
					t.Fatal("failed storage write registered contract reuse entries")
				}
				return
			}

			if !reflect.DeepEqual(contracts, []*meshevents.SwapContractRecordedEvent{recorded}) {
				t.Fatalf("stored contracts = %+v, want %+v", contracts, recorded)
			}
			if tracker.Status != after || !status.swapTime.Equal(swapTime) {
				t.Fatalf("status/time = %v/%v, want %v/%v", tracker.Status, status.swapTime, after, swapTime)
			}
			contract := status.swap
			if contract == nil {
				t.Fatal("swap contract was not recorded in memory")
			}
			if !bytes.Equal(contract.ID(), recorded.CoinID) || contract.TxID() != recorded.CoinTxID ||
				contract.String() != recorded.CoinString || contract.Value() != recorded.Value || contract.FeeRate() != recorded.FeeRate {
				t.Fatalf("wrong recorded coin: %+v", contract.Coin)
			}
			if !bytes.Equal(contract.ContractData, recorded.Contract) || !bytes.Equal(contract.SecretHash, recorded.SecretHash) ||
				!bytes.Equal(contract.TxData, recorded.TxData) || contract.SwapAddress != recorded.SwapAddress ||
				contract.LockTime.UnixMilli() != recorded.LockTime {
				t.Fatalf("wrong recorded contract: %+v", contract)
			}

			otherMatch := info.matchID
			otherMatch[0] ^= 1
			if err := rig.swapper.checkSwapContractDedup(otherMatch, recorded.CoinID, recorded.Contract, nil, false); !errors.Is(err, errSwapContractInUse) {
				t.Fatalf("contract reuse error = %v, want %v", err, errSwapContractInUse)
			}
			var wantSecretErr error
			if tt.maker {
				wantSecretErr = errSecretHashInUse
			}
			if err := rig.swapper.checkSwapContractDedup(otherMatch, []byte("other-coin"), recorded.Contract, recorded.SecretHash, true); !errors.Is(err, wantSecretErr) {
				t.Fatalf("secret hash reuse error = %v, want %v", err, wantSecretErr)
			}
		})
	}
}

func TestApplyAuditAckRecordedEvent(t *testing.T) {
	storageErr := errors.New("storage error")
	tests := []struct {
		name       string
		maker      bool
		storageErr error
	}{
		{name: "maker", maker: true},
		{name: "taker"},
		{name: "storage error", maker: true, storageErr: storageErr},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			set := tPerfectLimitLimit(uint64(1e8), uint64(1e8), true)
			info := set.matchInfos[0]
			rig := tNewUnstartedRig(info)
			rig.swapper.TrackMatches([]*order.MatchSet{set.matchSet})
			tracker := rig.getTracker()
			makerSig, takerSig := []byte("previous maker ack"), []byte("previous taker ack")
			tracker.Sigs.MakerAudit, tracker.Sigs.TakerAudit = makerSig, takerSig
			rig.storage.applyAuditAckRecordedErr = tt.storageErr
			recorded := &meshevents.AuditAckRecordedEvent{
				MatchID: info.matchID,
				Base:    info.match.Maker.BaseAsset,
				Quote:   info.match.Maker.QuoteAsset,
				Maker:   tt.maker,
				Sig:     []byte("audit ack"),
			}
			event, err := mesh.NewEvent(recorded)
			if err != nil {
				t.Fatal(err)
			}
			apply := rig.swapper.Events()[meshevents.EventKindAuditAckRecorded]
			_, err = apply(&mesh.EventApplyContext{Context: context.Background()}, event)
			if !errors.Is(err, tt.storageErr) {
				t.Fatalf("apply error = %v, want %v", err, tt.storageErr)
			}
			var wantAcks []*meshevents.AuditAckRecordedEvent
			if tt.storageErr == nil {
				wantAcks = []*meshevents.AuditAckRecordedEvent{recorded}
				if tt.maker {
					makerSig = recorded.Sig
				} else {
					takerSig = recorded.Sig
				}
			}
			if !reflect.DeepEqual(rig.storage.auditAcks, wantAcks) {
				t.Fatalf("stored audit acks = %+v, want %+v", rig.storage.auditAcks, wantAcks)
			}
			if !bytes.Equal(tracker.Sigs.MakerAudit, makerSig) || !bytes.Equal(tracker.Sigs.TakerAudit, takerSig) {
				t.Fatalf("maker/taker audit signatures = %x/%x, want %x/%x",
					tracker.Sigs.MakerAudit, tracker.Sigs.TakerAudit, makerSig, takerSig)
			}
		})
	}
}

func storedSwapContracts(storage *TStorage) []*meshevents.SwapContractRecordedEvent {
	storage.mtx.Lock()
	defer storage.mtx.Unlock()
	return append([]*meshevents.SwapContractRecordedEvent(nil), storage.swapContracts...)
}

func TestApplySwapRedemptionRecordedEvent(t *testing.T) {
	storageErr := errors.New("storage error")
	redeemTime := time.UnixMilli(1670000000123).UTC()
	tests := []struct {
		name          string
		maker         bool
		initialStatus order.MatchStatus
		storageErr    error
		unknownMatch  bool
		wantErr       string
	}{
		{name: "maker", maker: true, initialStatus: order.TakerSwapCast},
		{name: "taker", initialStatus: order.MakerRedeemed},
		{name: "maker storage error", maker: true, initialStatus: order.TakerSwapCast, storageErr: storageErr, wantErr: "storage error"},
		{name: "taker storage error", initialStatus: order.MakerRedeemed, storageErr: storageErr, wantErr: "storage error"},
		{name: "maker wrong status", maker: true, initialStatus: order.MakerRedeemed, wantErr: "requires status"},
		{name: "taker wrong status", initialStatus: order.TakerSwapCast, wantErr: "requires status"},
		{name: "unknown match", maker: true, initialStatus: order.TakerSwapCast, unknownMatch: true, wantErr: "unknown match"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			set := tPerfectLimitLimit(uint64(1e8), uint64(1e8), true)
			info := set.matchInfos[0]
			rig := tNewUnstartedRig(info)
			swapper := rig.swapper
			swapper.TrackMatches([]*order.MatchSet{set.matchSet})
			tracker := rig.getTracker()
			tracker.Status = tt.initialStatus
			rig.storage.applyRedemptionErr = tt.storageErr

			actor, counterparty := tracker.takerStatus, tracker.makerStatus
			actorOrder, nextStatus := tracker.Taker, order.MatchComplete
			if tt.maker {
				actor, counterparty = tracker.makerStatus, tracker.takerStatus
				actorOrder, nextStatus = tracker.Maker, order.MakerRedeemed
			}
			counterparty.swap = &asset.Contract{ContractData: randBytes(50)}
			// A taker redeems after the maker has already revealed the secret.
			if !tt.maker && tt.initialStatus == order.MakerRedeemed {
				counterparty.redemption = &TCoin{id: randBytes(36)}
				counterparty.redeemTime = redeemTime.Add(-time.Second)
				counterparty.secret = randBytes(32)
			}
			previousCoin, previousTime := counterparty.redemption, counterparty.redeemTime
			previousSecret := append([]byte(nil), counterparty.secret...)

			recorded := &meshevents.SwapRedemptionRecordedEvent{
				MatchID:    info.matchID,
				Base:       tracker.Maker.BaseAsset,
				Quote:      tracker.Maker.QuoteAsset,
				Maker:      tt.maker,
				Status:     nextStatus,
				CoinID:     randBytes(36),
				CoinTxID:   "redemption transaction",
				CoinString: "redemption coin",
				Value:      1e8,
				FeeRate:    12,
				RedeemTime: redeemTime.UnixMilli(),
			}
			if tt.maker {
				recorded.Secret = randBytes(32)
			}
			if tt.unknownMatch {
				recorded.MatchID[0] ^= 1
			}
			event, err := mesh.NewEvent(recorded)
			if err != nil {
				t.Fatal(err)
			}

			var completed []swapDoneCall
			swapper.swapDone = func(ord order.Order, _ *order.Match, faulted bool) {
				completed = append(completed, swapDoneCall{ord.ID(), faulted})
			}
			coinID, contract, secretHash := randBytes(36), randBytes(50), randBytes(32)
			swapper.registerSwapContractDedup(info.matchID, coinID, contract, secretHash, true)
			otherMatch := info.matchID
			otherMatch[0] ^= 1

			apply := swapper.Events()[meshevents.EventKindSwapRedemptionRecorded]
			_, err = apply(&mesh.EventApplyContext{Context: context.Background()}, event)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("apply error = %v, want %q", err, tt.wantErr)
			}
			if tt.storageErr != nil && !errors.Is(err, tt.storageErr) {
				t.Fatalf("apply error = %v, want wrapped storage error", err)
			}

			wantDeleted := tt.wantErr == "" && !tt.maker
			if gone := rig.getTracker() == nil; gone != wantDeleted {
				t.Fatalf("match deleted = %v, want %v", gone, wantDeleted)
			}
			dedupErr := swapper.checkSwapContractDedup(otherMatch, coinID, contract, secretHash, true)
			if freed := dedupErr == nil; freed != wantDeleted {
				t.Fatalf("contract reuse entries freed = %v, want %v", freed, wantDeleted)
			}
			if counterparty.redemption != previousCoin || !counterparty.redeemTime.Equal(previousTime) || !bytes.Equal(counterparty.secret, previousSecret) {
				t.Fatal("application changed the counterparty's redemption")
			}
			if len(rig.storage.redemptionAcks) != 0 {
				t.Fatalf("unexpected redeem ack writes: %#v", rig.storage.redemptionAcks)
			}
			if tt.wantErr != "" {
				if len(rig.storage.redemptions) != 0 || tracker.Status != tt.initialStatus || actor.redemption != nil || !actor.redeemTime.IsZero() || len(actor.secret) != 0 || len(completed) != 0 {
					t.Fatal("failed application changed storage, redemption state, or order completion")
				}
				return
			}

			if !reflect.DeepEqual(rig.storage.redemptions, []*meshevents.SwapRedemptionRecordedEvent{recorded}) {
				t.Fatalf("stored redemptions = %+v, want %+v", rig.storage.redemptions, recorded)
			}
			if tracker.Status != nextStatus || !actor.redeemTime.Equal(redeemTime) {
				t.Fatalf("status/time = %v/%v, want %v/%v", tracker.Status, actor.redeemTime, nextStatus, redeemTime)
			}
			coin := actor.redemption
			if coin == nil || !bytes.Equal(coin.ID(), recorded.CoinID) || coin.TxID() != recorded.CoinTxID || coin.String() != recorded.CoinString || coin.Value() != recorded.Value || coin.FeeRate() != recorded.FeeRate {
				t.Fatalf("wrong reconstructed redemption: %+v", coin)
			}
			if !bytes.Equal(actor.secret, recorded.Secret) {
				t.Fatalf("secret = %x, want %x", actor.secret, recorded.Secret)
			}
			wantCompleted := []swapDoneCall{{actorOrder.ID(), false}}
			if !reflect.DeepEqual(completed, wantCompleted) {
				t.Fatalf("swapDone calls = %v, want %v", completed, wantCompleted)
			}
		})
	}
}

func TestApplyRedemptionAckRecordedEvent(t *testing.T) {
	storageErr := errors.New("storage error")
	tests := []struct {
		name         string
		maker        bool
		missingMatch bool
		storageErr   error
	}{
		{name: "maker after match removal", maker: true, missingMatch: true},
		{name: "maker with live match", maker: true},
		{name: "taker"},
		{name: "taker after match removal", missingMatch: true},
		{name: "taker storage error", storageErr: storageErr},
		{name: "maker storage error", maker: true, storageErr: storageErr},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			set := tPerfectLimitLimit(uint64(1e8), uint64(1e8), true)
			info := set.matchInfos[0]
			rig := tNewUnstartedRig(info)
			rig.swapper.TrackMatches([]*order.MatchSet{set.matchSet})
			tracker := rig.getTracker()
			makerSig, takerSig := []byte("previous maker ack"), []byte("previous taker ack")
			tracker.Sigs.MakerRedeem, tracker.Sigs.TakerRedeem = makerSig, takerSig
			rig.storage.applyRedemptionAckErr = tt.storageErr
			if tt.missingMatch {
				rig.swapper.matchMtx.Lock()
				rig.swapper.deleteMatch(tracker)
				rig.swapper.matchMtx.Unlock()
			}

			recorded := &meshevents.RedemptionAckRecordedEvent{
				MatchID: info.matchID,
				Base:    info.match.Maker.BaseAsset,
				Quote:   info.match.Maker.QuoteAsset,
				Maker:   tt.maker,
				Sig:     []byte("redemption ack"),
			}
			event, err := newRedemptionAckRecordedEvent(tracker, tt.maker, recorded.Sig)
			if err != nil {
				t.Fatal(err)
			}
			apply := rig.swapper.Events()[meshevents.EventKindRedemptionAckRecorded]
			_, err = apply(&mesh.EventApplyContext{Context: context.Background()}, event)
			if !errors.Is(err, tt.storageErr) {
				t.Fatalf("apply error = %v, want %v", err, tt.storageErr)
			}

			wantDeleted := tt.missingMatch || (tt.maker && tt.storageErr == nil)
			if deleted := rig.getTracker() == nil; deleted != wantDeleted {
				t.Fatalf("match deleted = %v, want %v", deleted, wantDeleted)
			}
			var wantAcks []*meshevents.RedemptionAckRecordedEvent
			if tt.storageErr == nil {
				wantAcks = []*meshevents.RedemptionAckRecordedEvent{recorded}
				if !tt.missingMatch {
					if tt.maker {
						makerSig = recorded.Sig
					} else {
						takerSig = recorded.Sig
					}
				}
			}
			if !reflect.DeepEqual(rig.storage.redemptionAcks, wantAcks) {
				t.Fatalf("stored acknowledgements = %+v, want %+v", rig.storage.redemptionAcks, wantAcks)
			}
			if !bytes.Equal(tracker.Sigs.MakerRedeem, makerSig) || !bytes.Equal(tracker.Sigs.TakerRedeem, takerSig) {
				t.Fatalf("maker/taker signatures = %x/%x, want %x/%x", tracker.Sigs.MakerRedeem, tracker.Sigs.TakerRedeem, makerSig, takerSig)
			}
		})
	}
}

func TestDeleteMatchRetainsSharedOrderLocks(t *testing.T) {
	set := tMultiMatchSet([]uint64{1e8, 2e8}, []uint64{5e7, 5e7}, true, false)
	rig := tNewUnstartedRig(set.matchInfos[0])
	swapper := rig.swapper
	now := time.Now().UTC()

	takerOrd := set.matchInfos[0].match.Taker
	takerOrd.Trade().Coins = []order.CoinID{randBytes(36)}

	trackers := make([]*matchTracker, 0, 2)
	for _, mi := range set.matchInfos {
		mt := &matchTracker{
			Match:       mi.match,
			time:        now,
			matchTime:   now,
			makerStatus: &swapStatus{swapAsset: ABCID, redeemAsset: XYZID},
			takerStatus: &swapStatus{swapAsset: XYZID, redeemAsset: ABCID},
		}
		swapper.addMatch(mt)
		trackers = append(trackers, mt)
	}
	swapper.LockOrdersCoins([]order.Order{takerOrd})

	locker := swapper.coins[XYZID].Locker // buy-side taker funds with the quote asset
	deleteTracker := func(mt *matchTracker) {
		swapper.matchMtx.Lock()
		swapper.deleteMatch(mt)
		swapper.matchMtx.Unlock()
	}

	deleteTracker(trackers[0])
	for _, coin := range takerOrd.Trade().Coins {
		if !locker.CoinLocked(coin) {
			t.Fatalf("shared taker order unlocked while another active match references it")
		}
	}

	deleteTracker(trackers[1])
	for _, coin := range takerOrd.Trade().Coins {
		if locker.CoinLocked(coin) {
			t.Fatalf("taker order still locked after its last active match was deleted")
		}
	}
}

func TestApplyMatchAcksRecordedEvent(t *testing.T) {
	ackTime := time.UnixMilli(1670000000123).UTC()
	storageErr := errors.New("storage error")

	applyEvent := func(t *testing.T, rig *testRig, at time.Time, records ...meshevents.MatchAckRecord) error {
		t.Helper()
		event, err := newMatchAcksRecordedEvent(at, records)
		if err != nil {
			t.Fatal(err)
		}
		applier := rig.swapper.Events()[meshevents.EventKindMatchAcksRecorded]
		before := len(matchAckEvents(rig.storage))
		_, err = applier(&mesh.EventApplyContext{Context: context.Background()}, event)
		events := matchAckEvents(rig.storage)
		want := &meshevents.MatchAcksRecordedEvent{AckTime: at.UnixMilli(), Records: records}
		if len(events) != before+1 || !reflect.DeepEqual(events[len(events)-1], want) {
			t.Fatalf("storage events = %+v, want last event %+v", events, want)
		}
		return err
	}

	requireAckState := func(t *testing.T, tracker *matchTracker, makerSig []byte, makerAddr string, takerSig []byte, takerAddr string) {
		t.Helper()
		if tracker == nil {
			t.Fatal("missing match tracker")
		}
		tracker.mtx.RLock()
		defer tracker.mtx.RUnlock()
		if !bytes.Equal(tracker.Sigs.MakerMatch, makerSig) || tracker.makerSwapAddr != makerAddr {
			t.Fatalf("maker ack = %x / %q, want %x / %q", tracker.Sigs.MakerMatch, tracker.makerSwapAddr, makerSig, makerAddr)
		}
		if !bytes.Equal(tracker.Sigs.TakerMatch, takerSig) || tracker.takerSwapAddr != takerAddr {
			t.Fatalf("taker ack = %x / %q, want %x / %q", tracker.Sigs.TakerMatch, tracker.takerSwapAddr, takerSig, takerAddr)
		}
	}

	for _, tt := range []struct {
		name  string
		maker bool
	}{
		{name: "maker", maker: true},
		{name: "taker"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			set := tPerfectLimitLimit(uint64(1e8), uint64(1e8), true)
			info := set.matchInfos[0]
			rig := tNewUnstartedRig(info)
			rig.swapper.TrackMatches([]*order.MatchSet{set.matchSet})
			record := tTakerMatchAckRecord(info)
			var makerSig, takerSig []byte
			var makerAddr, takerAddr string
			if tt.maker {
				record = tMakerMatchAckRecord(info)
				makerSig, makerAddr = record.Sig, record.Address
			} else {
				takerSig, takerAddr = record.Sig, record.Address
			}
			tracker := rig.getTracker()
			initialTime := tracker.time
			if err := applyEvent(t, rig, ackTime, record); err != nil {
				t.Fatal(err)
			}
			requireAckState(t, tracker, makerSig, makerAddr, takerSig, takerAddr)
			if !tracker.time.Equal(initialTime) || tracker.counterPartyAddrsSent {
				t.Fatal("started maker deadline before both addresses were available")
			}
			if got := notificationCount(rig.auth, msgjson.CounterPartyAddressRoute); got != 0 {
				t.Fatalf("address notifications = %d, want 0", got)
			}
		})
	}

	t.Run("storage error", func(t *testing.T) {
		set := tPerfectLimitLimit(uint64(1e8), uint64(1e8), true)
		info := set.matchInfos[0]
		rig := tNewUnstartedRig(info)
		rig.swapper.TrackMatches([]*order.MatchSet{set.matchSet})
		rig.storage.applyMatchAcksRecordedErr = storageErr
		tracker := rig.getTracker()
		initialTime := tracker.time
		if err := applyEvent(t, rig, ackTime, tMakerMatchAckRecord(info), tTakerMatchAckRecord(info)); !errors.Is(err, storageErr) {
			t.Fatalf("apply error = %v, want %v", err, storageErr)
		}
		requireAckState(t, tracker, nil, "", nil, "")
		if !tracker.time.Equal(initialTime) || tracker.counterPartyAddrsSent {
			t.Fatal("started maker deadline after storage failure")
		}
		if got := notificationCount(rig.auth, msgjson.CounterPartyAddressRoute); got != 0 {
			t.Fatalf("address notifications = %d, want 0", got)
		}
	})

	t.Run("both sides and repeated acknowledgement", func(t *testing.T) {
		set := tPerfectLimitLimit(uint64(1e8), uint64(1e8), true)
		info := set.matchInfos[0]
		rig := tNewUnstartedRig(info)
		rig.swapper.TrackMatches([]*order.MatchSet{set.matchSet})
		maker, taker := tMakerMatchAckRecord(info), tTakerMatchAckRecord(info)
		if err := applyEvent(t, rig, ackTime, maker); err != nil {
			t.Fatal(err)
		}
		secondAckTime := ackTime.Add(time.Second)
		if err := applyEvent(t, rig, secondAckTime, taker); err != nil {
			t.Fatal(err)
		}
		tracker := rig.getTracker()
		requireAckState(t, tracker, maker.Sig, maker.Address, taker.Sig, taker.Address)
		if !tracker.time.Equal(secondAckTime) || !tracker.counterPartyAddrsSent {
			t.Fatal("second acknowledgement did not start the maker deadline")
		}
		if got := notificationCount(rig.auth, msgjson.CounterPartyAddressRoute); got != 2 {
			t.Fatalf("address notifications = %d, want 2", got)
		}

		// Refresh the signature, but keep the address already sent to the counterparty.
		reack := taker
		reack.Sig = []byte("new-signature")
		reack.Address = "different-address"
		if err := applyEvent(t, rig, secondAckTime.Add(time.Second), reack); err != nil {
			t.Fatal(err)
		}
		requireAckState(t, tracker, maker.Sig, maker.Address, reack.Sig, taker.Address)
		if !tracker.time.Equal(secondAckTime) {
			t.Fatal("repeated acknowledgement reset maker deadline")
		}
		if got := notificationCount(rig.auth, msgjson.CounterPartyAddressRoute); got != 2 {
			t.Fatalf("address notifications after repeated acknowledgement = %d, want 2", got)
		}
	})

	t.Run("trade and cancel matches", func(t *testing.T) {
		makerSet := tPerfectLimitLimit(uint64(1e8), uint64(1e8), true)
		takerSet := tPerfectLimitLimit(uint64(2e8), uint64(1e8), true)
		cancelSet := tCancelPair()
		cancelInfo := cancelSet.matchInfos[0]
		makerInfo, takerInfo := makerSet.matchInfos[0], takerSet.matchInfos[0]
		rig := tNewUnstartedRig(makerInfo)
		rig.swapper.TrackMatches([]*order.MatchSet{makerSet.matchSet, takerSet.matchSet, cancelSet.matchSet})
		maker, taker := tMakerMatchAckRecord(makerInfo), tTakerMatchAckRecord(takerInfo)
		cancelMaker, cancelTaker := tMakerMatchAckRecord(cancelInfo), tTakerMatchAckRecord(cancelInfo)
		cancelMaker.Cancel, cancelMaker.Address = true, ""
		cancelTaker.Cancel, cancelTaker.Address = true, ""
		if err := applyEvent(t, rig, ackTime, cancelMaker, maker, cancelTaker, taker); err != nil {
			t.Fatal(err)
		}
		requireAckState(t, rig.swapper.matches[maker.MatchID], maker.Sig, maker.Address, nil, "")
		requireAckState(t, rig.swapper.matches[taker.MatchID], nil, "", taker.Sig, taker.Address)
		if rig.swapper.matches[cancelInfo.matchID] != nil {
			t.Fatal("cancel match gained a tracker")
		}
		if got := notificationCount(rig.auth, msgjson.CounterPartyAddressRoute); got != 0 {
			t.Fatalf("address notifications = %d, want 0", got)
		}
	})
}

func TestCancel(t *testing.T) {
	set := tCancelPair()
	matchInfo := set.matchInfos[0]
	rig, cleanup := tNewTestRig(matchInfo)
	defer cleanup()
	rig.applyMatchesAndRequestAcks(t, set.matchSet)
	// There should be no matchTracker
	if rig.getTracker() != nil {
		t.Fatalf("found matchTracker for a cancellation")
	}
	// The user should have two match requests.
	user := matchInfo.maker
	req := rig.auth.popReq(user.acct)
	if req == nil {
		t.Fatalf("no match request sent")
	}
	matchNotes := make([]*msgjson.Match, 0)
	err := json.Unmarshal(req.req.Payload, &matchNotes)
	if err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if len(matchNotes) != 2 {
		t.Fatalf("expected 2 match notification, got %d", len(matchNotes))
	}
	for _, match := range matchNotes {
		if err = checkSigS256(match, rig.auth.privkey.PubKey()); err != nil {
			t.Fatalf("incorrect server signature: %v", err)
		}
	}
	makerNote, takerNote := matchNotes[0], matchNotes[1]
	if makerNote.OrderID.String() != matchInfo.makerOID.String() {
		t.Fatalf("expected maker ID %s, got %s", matchInfo.makerOID, makerNote.OrderID)
	}
	if takerNote.OrderID.String() != matchInfo.takerOID.String() {
		t.Fatalf("expected taker ID %s, got %s", matchInfo.takerOID, takerNote.OrderID)
	}
	if makerNote.MatchID.String() != takerNote.MatchID.String() {
		t.Fatalf("match ID mismatch. %s != %s", makerNote.MatchID, takerNote.MatchID)
	}

	// Ack both notifications. Cancel matches are never swap-tracked, but the
	// ack sigs must still be recorded.
	resp := tNewResponse(req.req.ID,
		tAckArrWithAddrs(user, []order.MatchID{matchInfo.matchID, matchInfo.matchID}, nil))
	req.respFunc(nil, resp)
	if msg, r := rig.auth.popResp(user.acct); msg != nil {
		t.Fatalf("unexpected error response to cancel acks: %+v", r.Error)
	}
	updates := matchAckEvents(rig.storage)
	if len(updates) != 1 {
		t.Fatalf("expected 1 match acks storage update, got %d", len(updates))
	}
	acks := updates[0].Records
	if len(acks) != 2 {
		t.Fatalf("expected 2 recorded acks, got %d", len(acks))
	}
	for i, wantMaker := range []bool{true, false} {
		ack := acks[i]
		if !ack.Cancel || ack.Maker != wantMaker {
			t.Fatalf("ack %d: Cancel = %v, Maker = %v, want true, %v", i, ack.Cancel, ack.Maker, wantMaker)
		}
		if !bytes.Equal(ack.Sig, user.sig) {
			t.Fatalf("ack %d: wrong sig", i)
		}
		if ack.Address != "" {
			t.Fatalf("ack %d: unexpected address %q", i, ack.Address)
		}
	}
	if rig.getTracker() != nil {
		t.Fatalf("cancel match gained a tracker after acks")
	}
}

func TestAccountTracking(t *testing.T) {
	const lotSize uint64 = 1e8
	const rate = 2e10
	const lots = 5
	const qty = lotSize * lots
	makerAddr := "maker"
	takerAddr := "taker"

	trackedSet := tPerfectLimitLimit(qty, rate, true)
	matchInfo := trackedSet.matchInfos[0]

	rig, cleanup := tNewTestRig(matchInfo)
	defer cleanup()

	var baseAsset, quoteAsset uint32 = ACCTID, XYZID

	addOrder := func(matchSet *order.MatchSet, makerSell bool) {
		maker, taker := matchSet.Makers[0], matchSet.Taker
		taker.Prefix().BaseAsset = baseAsset
		maker.BaseAsset = baseAsset
		taker.Prefix().QuoteAsset = quoteAsset
		maker.QuoteAsset = quoteAsset
		maker.Coins = []order.CoinID{[]byte(makerAddr)}
		taker.Trade().Coins = []order.CoinID{[]byte(takerAddr)}
		taker.Trade().Address = takerAddr
		maker.Address = makerAddr
		if makerSell {
			taker.Trade().Sell = false
			maker.Sell = true
		} else {
			taker.Trade().Sell = true
			maker.Sell = false
		}
		rig.applyMatchesAndRequestAcks(t, matchSet)
	}

	checkStats := func(addr string, expQty, expSwaps uint64, expRedeems int) {
		t.Helper()
		q, s, r := rig.swapper.AccountStats(addr, ACCTID)
		if q != expQty {
			t.Fatalf("wrong quantity. wanted %d, got %d", expQty, q)
		}
		if s != expSwaps {
			t.Fatalf("wrong swaps. wanted %d, got %d", expSwaps, s)
		}
		if r != expRedeems {
			t.Fatalf("wrong redeems. wanted %d, got %d", expRedeems, r)
		}
	}

	addOrder(trackedSet.matchSet, true)
	checkStats(makerAddr, qty, 1, 0)
	checkStats(takerAddr, 0, 0, 1)

	tracker := rig.getTracker()
	tracker.Status = order.MakerSwapCast
	checkStats(makerAddr, 0, 0, 0)
	checkStats(takerAddr, 0, 0, 1)

	tracker.Status = order.MatchComplete
	checkStats(takerAddr, 0, 0, 0)

	rig.swapper.matchMtx.Lock()
	rig.swapper.deleteMatch(tracker)
	rig.swapper.matchMtx.Unlock()
	if len(rig.swapper.acctMatches[ACCTID]) > 0 {
		t.Fatalf("account tracking not deleted for removed match")
	}

	// Do 3 maker buys, 2 maker sells, and check the numbers.
	for i := 0; i < 5; i++ {
		set := tPerfectLimitLimit(qty, rate, i > 2)
		addOrder(set.matchSet, i > 2)
	}

	checkStats(makerAddr, qty*2, 2, 3)
	checkStats(takerAddr, qty*3, 3, 2)

	quoteAsset, baseAsset = baseAsset, quoteAsset
	set := tPerfectLimitLimit(qty, rate, false)

	addOrder(set.matchSet, false)
	checkStats(makerAddr, qty*2+calc.BaseToQuote(rate, qty), 3, 3)
	checkStats(takerAddr, qty*3, 3, 3)
}

func TestPerMatchSwapAddresses(t *testing.T) {
	set := tPerfectLimitLimit(uint64(1e8), uint64(1e8), true)
	matchInfo := set.matchInfos[0]
	rig, cleanup := tNewTestRig(matchInfo)
	defer cleanup()

	rig.auth.newNtfn = make(chan struct{}, 4)

	rig.applyMatchesAndRequestAcks(t, set.matchSet)

	tracker := rig.getTracker()

	// Maker acks with per-match address (included automatically via ack flow).
	if err := rig.ackMatch_maker(true); err != nil {
		t.Fatalf("maker ack: %v", err)
	}

	tracker.mtx.RLock()
	if tracker.makerSwapAddr != matchInfo.makerPerMatchAddr {
		t.Fatalf("expected maker swap addr %q, got %q", matchInfo.makerPerMatchAddr, tracker.makerSwapAddr)
	}
	// Taker hasn't acked yet, so no counterparty addrs should be sent.
	if tracker.counterPartyAddrsSent {
		t.Fatalf("counterPartyAddrsSent set before taker ack")
	}
	tracker.mtx.RUnlock()

	// Taker acks with per-match address.
	if err := rig.ackMatch_taker(true); err != nil {
		t.Fatalf("taker ack: %v", err)
	}

	tracker.mtx.RLock()
	if tracker.takerSwapAddr != matchInfo.takerPerMatchAddr {
		t.Fatalf("expected taker swap addr %q, got %q", matchInfo.takerPerMatchAddr, tracker.takerSwapAddr)
	}
	if !tracker.counterPartyAddrsSent {
		t.Fatalf("counterPartyAddrsSent not set after both acks")
	}
	tracker.mtx.RUnlock()

	// Both acked. Check that counterparty_address notifications were sent.
	// Two notifications: one to maker (with taker's addr) and one to taker
	// (with maker's addr).
	select {
	case <-rig.auth.newNtfn:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for first counterparty_address notification")
	}
	select {
	case <-rig.auth.newNtfn:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for second counterparty_address notification")
	}

	// Check the notification contents.
	var makerCPA msgjson.CounterPartyAddress
	err := rig.auth.getNtfn(matchInfo.maker.acct, msgjson.CounterPartyAddressRoute, &makerCPA)
	if err != nil {
		t.Fatalf("maker counterparty_address notification error: %v", err)
	}
	if makerCPA.Address != matchInfo.takerPerMatchAddr {
		t.Fatalf("maker received counterparty addr %q, expected %q", makerCPA.Address, matchInfo.takerPerMatchAddr)
	}

	var takerCPA msgjson.CounterPartyAddress
	err = rig.auth.getNtfn(matchInfo.taker.acct, msgjson.CounterPartyAddressRoute, &takerCPA)
	if err != nil {
		t.Fatalf("taker counterparty_address notification error: %v", err)
	}
	if takerCPA.Address != matchInfo.makerPerMatchAddr {
		t.Fatalf("taker received counterparty addr %q, expected %q", takerCPA.Address, matchInfo.makerPerMatchAddr)
	}
}

func TestPerMatchAddressInProcessInit(t *testing.T) {
	set := tPerfectLimitLimit(uint64(1e8), uint64(1e8), true)
	matchInfo := set.matchInfos[0]
	rig, cleanup := tNewTestRig(matchInfo)
	defer cleanup()

	rig.auth.swapReceived = make(chan struct{}, 1)
	rig.auth.auditReq = make(chan struct{}, 1)

	rig.applyMatchesAndRequestAcks(t, set.matchSet)

	// Ack both sides. Per-match addresses are included automatically via the
	// ack flow (tMatchInfo populates makerPerMatchAddr/takerPerMatchAddr).
	if err := rig.ackMatch_maker(true); err != nil {
		t.Fatalf("maker ack: %v", err)
	}
	if err := rig.ackMatch_taker(true); err != nil {
		t.Fatalf("taker ack: %v", err)
	}

	// Verify the per-match addresses were stored on the tracker.
	tracker := rig.getTracker()
	tracker.mtx.RLock()
	if tracker.takerSwapAddr != matchInfo.takerPerMatchAddr {
		t.Fatalf("expected taker per-match addr %q on tracker, got %q",
			matchInfo.takerPerMatchAddr, tracker.takerSwapAddr)
	}
	tracker.mtx.RUnlock()

	// Maker sends swap using sendSwap_maker, which picks up the taker's
	// per-match address from the tracker via swapRecipient.
	if err := rig.sendSwap_maker(true); err != nil {
		t.Fatalf("sendSwap_maker with per-match addr failed: %v", err)
	}
}

func TestPerMatchAddressWrongAddr(t *testing.T) {
	set := tPerfectLimitLimit(uint64(1e8), uint64(1e8), true)
	matchInfo := set.matchInfos[0]
	rig, cleanup := tNewTestRig(matchInfo)
	defer cleanup()

	rig.auth.swapReceived = make(chan struct{}, 1)

	rig.applyMatchesAndRequestAcks(t, set.matchSet)

	// Ack both sides with per-match addresses via the ack flow.
	if err := rig.ackMatch_maker(true); err != nil {
		t.Fatalf("maker ack: %v", err)
	}
	if err := rig.ackMatch_taker(true); err != nil {
		t.Fatalf("taker ack: %v", err)
	}

	// Maker sends swap with wrong address (order-level addr instead of
	// per-match addr).
	swap := tNewSwap(matchInfo, matchInfo.makerOID, matchInfo.taker.addr, matchInfo.maker)
	rig.abcNode.setContract(swap.coin, false)
	rig.auth.swapID = swap.req.ID
	rpcErr := rig.swapper.handleInit(matchInfo.maker.acct, swap.req)
	if rpcErr != nil {
		// Immediate error means synchronous rejection.
		return
	}
	// May also be an async error via the coin waiter.
	timeOutMempool()
	select {
	case <-rig.auth.swapReceived:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for swap response")
	}
	if err := rig.checkServerResponseFail(matchInfo.maker, msgjson.ContractError, "incorrect recipient"); err != nil {
		t.Fatalf("expected contract error for wrong address: %v", err)
	}
}

func TestCoinIDDedup(t *testing.T) {
	// Create two independent matches with different makers and takers.
	qty := uint64(1e8)
	rate := uint64(1e8)

	maker1, taker1 := tNewUser("maker1"), tNewUser("taker1")
	makerOrder1 := makeLimitOrder(qty, rate, maker1, true)
	takerOrder1 := makeLimitOrder(qty, rate, taker1, false)
	matchInfo1 := tMatchInfo(maker1, taker1, qty, rate, makerOrder1, takerOrder1)
	set1 := new(tMatchSet).add(matchInfo1)

	maker2, taker2 := tNewUser("maker2"), tNewUser("taker2")
	makerOrder2 := makeLimitOrder(qty, rate, maker2, true)
	takerOrder2 := makeLimitOrder(qty, rate, taker2, false)
	matchInfo2 := tMatchInfo(maker2, taker2, qty, rate, makerOrder2, takerOrder2)
	set2 := new(tMatchSet).add(matchInfo2)

	rig, cleanup := tNewTestRig(matchInfo1)
	defer cleanup()

	rig.auth.swapReceived = make(chan struct{}, 2)
	rig.auth.auditReq = make(chan struct{}, 2)

	rig.applyMatchesAndRequestAcks(t, set1.matchSet)
	rig.applyMatchesAndRequestAcks(t, set2.matchSet)

	// Ack both matches.
	rig.matchInfo = matchInfo1
	if err := rig.ackMatch_maker(true); err != nil {
		t.Fatalf("match1 maker ack: %v", err)
	}
	if err := rig.ackMatch_taker(true); err != nil {
		t.Fatalf("match1 taker ack: %v", err)
	}
	rig.matchInfo = matchInfo2
	if err := rig.ackMatch_maker(true); err != nil {
		t.Fatalf("match2 maker ack: %v", err)
	}
	if err := rig.ackMatch_taker(true); err != nil {
		t.Fatalf("match2 taker ack: %v", err)
	}

	// Send swap for match 1 using taker's per-match address.
	rig.matchInfo = matchInfo1
	swap1 := tNewSwap(matchInfo1, matchInfo1.makerOID, matchInfo1.takerPerMatchAddr, matchInfo1.maker)
	rig.abcNode.setContract(swap1.coin, false)
	rig.auth.swapID = swap1.req.ID
	rpcErr := rig.swapper.handleInit(matchInfo1.maker.acct, swap1.req)
	if rpcErr != nil {
		t.Fatalf("match1 swap init failed: %v", rpcErr.Message)
	}
	select {
	case <-rig.auth.swapReceived:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for match1 swap response")
	}
	if err := rig.checkServerResponseSuccess(matchInfo1.maker); err != nil {
		t.Fatalf("match1 swap should succeed: %v", err)
	}

	// Try to use the same CoinID and contract for match 2 - this should fail.
	rig.matchInfo = matchInfo2
	reusedCoinID := swap1.coin.ID() // same coin
	swap2 := tNewSwap(matchInfo2, matchInfo2.makerOID, matchInfo2.takerPerMatchAddr, matchInfo2.maker)
	// Override the CoinID and contract to reuse match1's.
	var init1 msgjson.Init
	swap1.req.Unmarshal(&init1)
	var init2 msgjson.Init
	swap2.req.Unmarshal(&init2)
	init2.CoinID = reusedCoinID
	init2.Contract = init1.Contract // same contract data
	swap2.req, _ = msgjson.NewRequest(swap2.req.ID, msgjson.InitRoute, &init2)
	// Also set the contract on the backend for the reused coin.
	swap2.coin.Coin.(*TCoin).id = reusedCoinID
	rig.abcNode.setContract(swap2.coin, false)
	rig.auth.swapID = swap2.req.ID
	rpcErr = rig.swapper.handleInit(matchInfo2.maker.acct, swap2.req)
	if rpcErr != nil {
		// Synchronous rejection.
		if !strings.Contains(rpcErr.Message, "already in use") {
			t.Fatalf("expected 'already in use' error, got: %s", rpcErr.Message)
		}
		return
	}

	timeOutMempool()
	select {
	case <-rig.auth.swapReceived:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for match2 swap response")
	}
	if err := rig.checkServerResponseFail(matchInfo2.maker, msgjson.ContractError, "already in use"); err != nil {
		t.Fatalf("expected CoinID reuse error: %v", err)
	}
}

func TestCoinIDDedupSameMatch(t *testing.T) {
	// Verify that the same CoinID can be retried for the same match.
	set := tPerfectLimitLimit(uint64(1e8), uint64(1e8), true)
	matchInfo := set.matchInfos[0]
	rig, cleanup := tNewTestRig(matchInfo)
	defer cleanup()

	rig.auth.swapReceived = make(chan struct{}, 2)
	rig.auth.auditReq = make(chan struct{}, 2)

	rig.applyMatchesAndRequestAcks(t, set.matchSet)

	if err := rig.ackMatch_maker(true); err != nil {
		t.Fatalf("maker ack: %v", err)
	}
	if err := rig.ackMatch_taker(true); err != nil {
		t.Fatalf("taker ack: %v", err)
	}

	// First init should succeed using taker's per-match address.
	swap := tNewSwap(matchInfo, matchInfo.makerOID, matchInfo.takerPerMatchAddr, matchInfo.maker)
	rig.abcNode.setContract(swap.coin, false)
	rig.auth.swapID = swap.req.ID
	rpcErr := rig.swapper.handleInit(matchInfo.maker.acct, swap.req)
	if rpcErr != nil {
		t.Fatalf("first init failed: %v", rpcErr.Message)
	}
	select {
	case <-rig.auth.swapReceived:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for first swap response")
	}
	if err := rig.checkServerResponseSuccess(matchInfo.maker); err != nil {
		t.Fatalf("first init should succeed: %v", err)
	}

	// Retry same CoinID for the same match (e.g. client retries) - the
	// status check in step() should catch that we're past the init step.
	// The dedup check itself should pass since existingMatch == match.MatchID.
	swap2 := tNewSwap(matchInfo, matchInfo.makerOID, matchInfo.takerPerMatchAddr, matchInfo.maker)
	// Copy the same CoinID.
	var init2 msgjson.Init
	swap2.req.Unmarshal(&init2)
	init2.CoinID = swap.coin.ID()
	swap2.req, _ = msgjson.NewRequest(swap2.req.ID, msgjson.InitRoute, &init2)
	rpcErr = rig.swapper.handleInit(matchInfo.maker.acct, swap2.req)
	// This should fail because the match status has already advanced, but the
	// CoinID dedup itself should not be the reason - it should be a step error.
	if rpcErr == nil {
		t.Fatal("expected error for duplicate init on already-swapped match")
	}
	if strings.Contains(rpcErr.Message, "already in use") {
		t.Fatal("CoinID dedup should not reject same-match retries")
	}
}

type swapDoneCall struct {
	oid  order.OrderID
	fail bool
}

func TestApplyMatchFailedEvent(t *testing.T) {
	storageErr := errors.New("storage error")
	type doneCall struct{ maker, faulted bool }
	type addressState struct{ maker, taker bool }
	tests := []struct {
		name                              string
		status                            order.MatchStatus
		fault                             meshevents.MatchFailureFault
		captured, current                 addressState
		missing, wrongMarket, wrongStatus bool
		storageErr                        error
		wantErr                           string
		wantDone                          []doneCall
	}{
		{name: "maker did not swap", fault: meshevents.MatchFailureMakerFault,
			captured: addressState{maker: true, taker: true}, current: addressState{maker: true, taker: true},
			wantDone: []doneCall{{maker: true, faulted: true}, {maker: false, faulted: false}}},
		{name: "taker did not provide address", fault: meshevents.MatchFailureTakerFault,
			captured: addressState{maker: true}, current: addressState{maker: true},
			wantDone: []doneCall{{maker: true, faulted: false}, {maker: false, faulted: true}}},
		{name: "taker did not swap", status: order.MakerSwapCast, fault: meshevents.MatchFailureTakerFault,
			current:  addressState{maker: true, taker: true},
			wantDone: []doneCall{{maker: true, faulted: false}, {maker: false, faulted: true}}},
		{name: "maker did not redeem", status: order.TakerSwapCast, fault: meshevents.MatchFailureMakerFault, wantDone: []doneCall{{maker: true, faulted: true}, {maker: false, faulted: false}}},
		{name: "taker did not redeem", status: order.MakerRedeemed, fault: meshevents.MatchFailureTakerFault, wantDone: []doneCall{{maker: false, faulted: true}}},
		{name: "no fault", status: order.MakerSwapCast, fault: meshevents.MatchFailureNoUserFault, wantDone: []doneCall{{maker: true, faulted: false}, {maker: false, faulted: false}}},
		{name: "storage error", fault: meshevents.MatchFailureMakerFault, storageErr: storageErr, wantErr: "storage error"},
		{name: "missing match", fault: meshevents.MatchFailureMakerFault, missing: true, wantErr: "unknown match"},
		{name: "wrong market", fault: meshevents.MatchFailureMakerFault, wrongMarket: true, wantErr: "market mismatch"},
		{name: "stale status", fault: meshevents.MatchFailureMakerFault, wrongStatus: true, wantErr: "requires status"},
		{name: "taker address arrived", fault: meshevents.MatchFailureTakerFault,
			captured: addressState{maker: true}, current: addressState{maker: true, taker: true}, wantErr: "swap addresses changed"},
		{name: "maker address arrived", fault: meshevents.MatchFailureMakerFault,
			captured: addressState{taker: true}, current: addressState{maker: true, taker: true}, wantErr: "swap addresses changed"},
		{name: "taker address arrived first", fault: meshevents.MatchFailureTakerFault,
			current: addressState{taker: true}, wantErr: "swap addresses changed"},
		{name: "maker address arrived first", fault: meshevents.MatchFailureTakerFault,
			current: addressState{maker: true}, wantErr: "swap addresses changed"},
		{name: "no fault ignores address changes", fault: meshevents.MatchFailureNoUserFault,
			captured: addressState{taker: true}, current: addressState{maker: true, taker: true},
			wantDone: []doneCall{{maker: true, faulted: false}, {maker: false, faulted: false}}},
	}
	for _, mode := range []struct {
		name     string
		position *db.EventLogPosition
	}{
		{name: "original"},
		{name: "replicated", position: &db.EventLogPosition{Seq: 2}},
	} {
		for _, tt := range tests {
			t.Run(mode.name+"/"+tt.name, func(t *testing.T) {
				set := tPerfectLimitLimit(uint64(1e8), uint64(1e8), true)
				info := set.matchInfos[0]
				rig := tNewUnstartedRig(info)
				failTime := time.UnixMilli(1670000000123).UTC()
				if !tt.missing {
					rig.swapper.TrackMatches([]*order.MatchSet{set.matchSet})
					tracker := rig.getTracker()
					tracker.Status = tt.status
					// Restored timeout estimates must not affect event application.
					tracker.time = failTime.Add(time.Hour)
					if tt.current.maker {
						tracker.makerSwapAddr = "maker address"
					}
					if tt.current.taker {
						tracker.takerSwapAddr = "taker address"
					}
					if tt.wrongStatus {
						tracker.Status = order.MakerSwapCast
					}
				}
				rig.storage.applyMatchFailedErr = tt.storageErr
				var done []swapDoneCall
				rig.swapper.swapDone = func(ord order.Order, _ *order.Match, faulted bool) {
					done = append(done, swapDoneCall{ord.ID(), faulted})
				}
				failed := &meshevents.MatchFailedEvent{
					MatchID: info.matchID, Base: info.match.Maker.Base(), Quote: info.match.Maker.Quote(),
					FailTime: failTime.UnixMilli(), Status: tt.status, Fault: tt.fault,
					MakerAddressKnown: tt.captured.maker, TakerAddressKnown: tt.captured.taker,
				}
				if tt.wrongMarket {
					failed.Base++
				}
				event, err := mesh.NewEvent(failed)
				if err != nil {
					t.Fatal(err)
				}
				applyCtx := &mesh.EventApplyContext{Context: context.Background(), Position: mode.position}
				_, err = rig.swapper.Events()[event.Kind](applyCtx, event)
				if tt.wantErr == "" {
					if err != nil {
						t.Fatal(err)
					}
				} else if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error = %v, want %q", err, tt.wantErr)
				}
				if tt.storageErr != nil && !errors.Is(err, tt.storageErr) {
					t.Fatal("storage error not preserved")
				}
				if (tt.missing || tt.wrongStatus || tt.wantErr == "swap addresses changed") && !errors.Is(err, errMatchFailureSuperseded) {
					t.Fatal("stale decision not classified as superseded")
				}
				var wantEvents []*meshevents.MatchFailedEvent
				if tt.wantErr == "" || tt.storageErr != nil {
					wantEvents = []*meshevents.MatchFailedEvent{failed}
				}
				if !reflect.DeepEqual(rig.storage.matchFailures, wantEvents) {
					t.Fatalf("stored events = %+v, want %+v", rig.storage.matchFailures, wantEvents)
				}
				wantTracked := !tt.missing && tt.wantErr != ""
				if tracked := rig.getTracker() != nil; tracked != wantTracked {
					t.Fatalf("match tracked = %v, want %v", tracked, wantTracked)
				}
				var wantDone []swapDoneCall
				for _, call := range tt.wantDone {
					oid := info.takerOID
					if call.maker {
						oid = info.makerOID
					}
					wantDone = append(wantDone, swapDoneCall{oid, call.faulted})
				}
				if !reflect.DeepEqual(done, wantDone) {
					t.Fatalf("swapDone calls = %v, want %v", done, wantDone)
				}
				wantNotes := 0
				if tt.wantErr == "" {
					wantNotes = 2
				}
				if got := notificationCount(rig.auth, msgjson.RevokeMatchRoute); got != wantNotes {
					t.Fatalf("revocations = %d, want %d", got, wantNotes)
				}
				if tt.wantErr == "" {
					for _, user := range []account.AccountID{info.maker.acct, info.taker.acct} {
						if !rig.auth.ntfnLocal[user][msgjson.RevokeMatchRoute] {
							t.Fatal("revocation notification was not local")
						}
					}
				}
				if len(rig.auth.suspensions) != 0 {
					t.Fatal("applier called legacy Penalize")
				}
			})
		}
	}
}

func TestMatchFailureDetection(t *testing.T) {
	for _, tt := range []struct {
		name                           string
		status                         order.MatchStatus
		block, missingAddress, expired bool
		wantFault                      meshevents.MatchFailureFault
	}{
		{name: "missing taker address", status: order.NewlyMatched, missingAddress: true, wantFault: meshevents.MatchFailureTakerFault},
		{name: "maker swap timeout", status: order.NewlyMatched, wantFault: meshevents.MatchFailureMakerFault},
		{name: "taker redemption timeout", status: order.MakerRedeemed, wantFault: meshevents.MatchFailureTakerFault},
		{name: "expired maker contract", status: order.MakerSwapCast, expired: true, wantFault: meshevents.MatchFailureNoUserFault},
		{name: "expired contract after taker swap", status: order.TakerSwapCast, expired: true, wantFault: meshevents.MatchFailureNoUserFault},
		{name: "taker swap timeout", status: order.MakerSwapCast, block: true, wantFault: meshevents.MatchFailureTakerFault},
		{name: "maker redemption timeout", status: order.TakerSwapCast, block: true, wantFault: meshevents.MatchFailureMakerFault},
		{name: "expired contract at taker timeout", status: order.MakerSwapCast, block: true, expired: true, wantFault: meshevents.MatchFailureNoUserFault},
		{name: "expired contract at maker timeout", status: order.TakerSwapCast, block: true, expired: true, wantFault: meshevents.MatchFailureNoUserFault},
	} {
		t.Run(tt.name, func(t *testing.T) {
			set := tPerfectLimitLimit(uint64(1e8), uint64(1e8), true)
			rig := tNewUnstartedRig(set.matchInfos[0])
			s := rig.swapper
			s.SetMeshService(&tSwapMesh{applier: s.Events()})
			s.TrackMatches([]*order.MatchSet{set.matchSet})
			s.EnableInactionChecks()
			tracker := rig.getTracker()
			now := time.Now()
			overdue := now.Add(-2 * s.bTimeout)
			tracker.Status = tt.status
			tracker.time = overdue
			tracker.matchTime = now
			tracker.makerSwapAddr = "maker address"
			tracker.makerStatus.swapConfirmed = overdue
			tracker.takerStatus.swapConfirmed = overdue
			tracker.makerStatus.redeemTime = overdue
			if !tt.missingAddress {
				tracker.takerSwapAddr = "taker address"
			}
			if tt.expired {
				tracker.makerStatus.swap = &asset.Contract{LockTime: now.Add(-time.Hour)}
			}
			if tt.block {
				s.checkInactionBlockBased(ABCID)
			} else {
				s.checkInactionEventBased()
			}
			if len(rig.storage.matchFailures) != 1 {
				t.Fatalf("stored failures = %d, want 1", len(rig.storage.matchFailures))
			}
			event := rig.storage.matchFailures[0]
			if !tt.block && (!event.MakerAddressKnown || event.TakerAddressKnown == tt.missingAddress) {
				t.Fatalf("captured addresses = %v/%v, want true/%v", event.MakerAddressKnown, event.TakerAddressKnown, !tt.missingAddress)
			}
			if event.Status != tt.status || event.Fault != tt.wantFault {
				t.Fatalf("failure = %v/%v, want %v/%v", event.Status, event.Fault, tt.status, tt.wantFault)
			}
		})
	}
}

func TestResumePendingRequests(t *testing.T) {
	for _, tt := range []struct {
		name                             string
		status                           order.MatchStatus
		makerAuditAcked, takerAuditAcked bool
		makerAddr, takerAddr             string
		setup                            func(*matchTracker)
		wantMaker, wantTaker             []string
	}{

		{
			name:      "both match acknowledgements missing",
			wantMaker: []string{msgjson.MatchRoute},
			wantTaker: []string{msgjson.MatchRoute},
		},
		{
			name:      "taker match acknowledgement missing",
			makerAddr: "maker-addr",
			wantTaker: []string{msgjson.MatchRoute},
		},
		{
			name:      "maker match acknowledgement missing",
			takerAddr: "taker-addr",
			wantMaker: []string{msgjson.MatchRoute},
		},
		{
			name:      "both match acknowledgements recorded",
			makerAddr: "maker-addr",
			takerAddr: "taker-addr",
		},
		{
			name:      "taker audit missing",
			status:    order.MakerSwapCast,
			makerAddr: "maker-addr",
			takerAddr: "taker-addr",
			wantTaker: []string{msgjson.AuditRoute},
		},
		{
			name:      "both audits missing",
			status:    order.TakerSwapCast,
			makerAddr: "maker-addr",
			takerAddr: "taker-addr",
			wantMaker: []string{msgjson.AuditRoute},
			wantTaker: []string{msgjson.AuditRoute},
		},
		{
			name:            "maker audit missing",
			status:          order.TakerSwapCast,
			makerAddr:       "maker-addr",
			takerAddr:       "taker-addr",
			takerAuditAcked: true,
			wantMaker:       []string{msgjson.AuditRoute},
		},
		{
			name:            "both audits recorded",
			makerAuditAcked: true,
			takerAuditAcked: true,
			status:          order.TakerSwapCast,
			makerAddr:       "maker-addr",
			takerAddr:       "taker-addr",
		},
		{
			name:            "redemption acknowledgement missing",
			makerAuditAcked: true,
			takerAuditAcked: true,
			status:          order.MakerRedeemed,
			makerAddr:       "maker-addr",
			takerAddr:       "taker-addr",
			wantTaker:       []string{msgjson.RedemptionRoute},
		},
		{
			name:            "redemption acknowledgement recorded",
			makerAuditAcked: true,
			takerAuditAcked: true,
			status:          order.MakerRedeemed,
			makerAddr:       "maker-addr",
			takerAddr:       "taker-addr",
			setup:           func(match *matchTracker) { match.Sigs.TakerRedeem = []byte("ack") },
		},
		{
			name:      "complete",
			status:    order.MatchComplete,
			makerAddr: "maker-addr",
			takerAddr: "taker-addr",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			rig, tracker, set := newStaleRig(tt.status, tt.makerAddr, tt.takerAddr)
			info := set.matchInfos[0]
			if tt.makerAuditAcked {
				tracker.Sigs.MakerAudit = info.maker.sig
			}
			if tt.takerAuditAcked {
				tracker.Sigs.TakerAudit = info.taker.sig
			}
			if tt.setup != nil {
				tt.setup(tracker)
			}
			tMesh := &tSwapMesh{applier: rig.swapper.Events()}
			rig.swapper.SetMeshService(tMesh)
			redeemTime := tracker.makerStatus.redeemTime
			started := time.Now()
			if err := rig.swapper.resumePendingRequests(context.Background()); err != nil {
				t.Fatal(err)
			}
			if len(tMesh.events) != 0 || rig.getTracker() == nil {
				t.Fatal("resuming requests emitted an event or removed the match")
			}
			if tracker.time.Before(started) {
				t.Fatal("match timeout was not extended")
			}
			if !tracker.makerStatus.redeemTime.Equal(redeemTime) {
				t.Fatal("recorded redemption time changed")
			}
			if !redeemTime.IsZero() && tracker.makerStatus.redeemInactionStart().Before(started) {
				t.Fatal("redemption timeout was not extended")
			}

			for _, side := range []struct {
				maker  bool
				routes []string
			}{{true, tt.wantMaker}, {false, tt.wantTaker}} {
				user, oid, counterparty := info.taker.acct, info.takerOID, tracker.makerStatus
				counterAddr := info.maker.addr
				if side.maker {
					user, oid, counterparty, counterAddr = info.maker.acct, info.makerOID, tracker.takerStatus, info.taker.addr
				}
				for _, route := range side.routes {
					req := rig.auth.popReq(user)
					if req == nil || req.req.Route != route {
						t.Fatalf("user %v: expected %s request, got %v", user, route, req)
					}
					switch route {
					case msgjson.MatchRoute:
						if err := rig.checkMatchNotification(req.req, oid, counterAddr); err != nil {
							t.Fatal(err)
						}
					case msgjson.AuditRoute:
						var got msgjson.Audit
						if err := req.req.Unmarshal(&got); err != nil {
							t.Fatal(err)
						}
						want := msgjson.Audit{
							OrderID: oid[:], MatchID: info.matchID[:], Time: uint64(counterparty.swapTime.UnixMilli()),
							CoinID: counterparty.swap.ID(), Contract: counterparty.swap.ContractData, TxData: counterparty.swap.TxData,
						}
						want.Signature = got.Signature
						if !reflect.DeepEqual(got, want) {
							t.Fatalf("audit = %+v, want %+v", got, want)
						}
					case msgjson.RedemptionRoute:
						var got msgjson.Redemption
						if err := req.req.Unmarshal(&got); err != nil {
							t.Fatal(err)
						}
						want := msgjson.Redemption{
							Redeem: msgjson.Redeem{OrderID: oid[:], MatchID: info.matchID[:], CoinID: counterparty.redemption.ID(), Secret: counterparty.secret},
							Time:   uint64(redeemTime.UnixMilli()),
						}
						want.Signature = got.Signature
						if !reflect.DeepEqual(got, want) {
							t.Fatalf("redemption = %+v, want %+v", got, want)
						}
					}
				}
				if routes := popRoutes(rig.auth, user); len(routes) != 0 {
					t.Fatalf("user %v: unexpected requests %v", user, routes)
				}
			}
		})
	}
}

// TestEnableInactionChecks checks that startup delays cannot cause inactivity
// penalties and that each settlement step gets a fresh response window afterward.
func TestEnableInactionChecks(t *testing.T) {
	for _, tt := range []struct {
		name      string
		status    order.MatchStatus
		wantFault meshevents.MatchFailureFault
	}{
		{"maker swap", order.NewlyMatched, meshevents.MatchFailureMakerFault},
		{"taker swap", order.MakerSwapCast, meshevents.MatchFailureTakerFault},
		{"maker redemption", order.TakerSwapCast, meshevents.MatchFailureMakerFault},
		{"taker redemption", order.MakerRedeemed, meshevents.MatchFailureTakerFault},
	} {
		t.Run(tt.name, func(t *testing.T) {
			rig, tracker, _ := newStaleRig(tt.status, "maker-addr", "taker-addr")
			s := rig.swapper
			s.bTimeout = time.Hour
			tMesh := &tSwapMesh{applier: s.Events()}
			s.SetMeshService(tMesh)

			// Keep contract expiry separate from the inactivity timeout.
			for _, status := range []*swapStatus{tracker.makerStatus, tracker.takerStatus} {
				if status.swap != nil {
					status.swap.LockTime = time.Now().Add(24 * time.Hour)
				}
			}
			check := s.checkInactionEventBased
			var responseStart *time.Time
			switch tt.status {
			case order.NewlyMatched:
				responseStart = &tracker.time
			case order.MakerRedeemed:
				responseStart = &tracker.makerStatus.redeemGraceStart
				tracker.makerStatus.redeemTime = time.Now().Add(-2 * s.bTimeout)
			case order.MakerSwapCast, order.TakerSwapCast:
				status := tracker.makerStatus
				if tt.status == order.TakerSwapCast {
					status = tracker.takerStatus
				}
				responseStart = &status.swapConfirmed
				check = func() { s.checkInactionBlockBased(status.swapAsset) }
			}
			*responseStart = time.Now().Add(-2 * s.bTimeout)
			recordedRedemption := tracker.makerStatus.redeemTime

			check()
			if len(tMesh.events) != 0 {
				t.Fatal("match failed before markets were ready")
			}

			s.EnableInactionChecks()
			check()
			if len(tMesh.events) != 0 {
				t.Fatal("match failed during the startup response window")
			}

			*responseStart = time.Now().Add(-2 * s.bTimeout)
			check()
			if len(tMesh.events) != 1 || len(rig.storage.matchFailures) != 1 {
				t.Fatalf("after timeout: emitted %d events, stored %d failures, want one of each",
					len(tMesh.events), len(rig.storage.matchFailures))
			}
			failed := rig.storage.matchFailures[0]
			if failed.MatchID != tracker.ID() || failed.Status != tt.status || failed.Fault != tt.wantFault {
				t.Fatalf("failure = %+v, want match %v status %v fault %v", failed, tracker.ID(), tt.status, tt.wantFault)
			}
			if !tracker.makerStatus.redeemTime.Equal(recordedRedemption) {
				t.Fatal("recorded redemption time changed")
			}
		})
	}

	t.Run("worker checks confirmed swaps without new blocks", func(t *testing.T) {
		rig, tracker, _ := newStaleRig(order.MakerSwapCast, "maker-addr", "taker-addr")
		s := rig.swapper
		s.bTimeout = 100 * time.Millisecond
		tracker.makerStatus.swapConfirmed = time.Now().Add(-time.Hour)
		tracker.makerStatus.swap.LockTime = time.Now().Add(time.Hour)
		s.SetMeshService(&tSwapMesh{applier: s.Events()})
		failed := make(chan struct{}, 2) // one callback for each side of the match
		s.swapDone = func(order.Order, *order.Match, bool) { failed <- struct{}{} }

		ctx, cancel := context.WithCancel(testCtx)
		ready := make(chan error, 1)
		done := make(chan struct{})
		go func() {
			defer close(done)
			s.Run(ctx, func(err error) { ready <- err })
		}()
		defer func() {
			cancel()
			<-done
		}()
		select {
		case err := <-ready:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(time.Second):
			t.Fatal("worker did not report ready")
		}

		// Keep checks disabled past the old startup timer, then start a fresh
		// response window. No block notification will trigger another check.
		select {
		case <-failed:
			t.Fatal("match failed before inactivity checks were enabled")
		case <-time.After(2 * s.bTimeout):
		}
		enabledAt := time.Now()
		s.EnableInactionChecks()
		select {
		case <-failed:
			if time.Since(enabledAt) < s.bTimeout {
				t.Fatal("match failed before its response window expired")
			}
		case <-time.After(2 * time.Second):
			t.Fatal("confirmed swap was not checked after startup")
		}
		rig.storage.mtx.Lock()
		defer rig.storage.mtx.Unlock()
		if len(rig.storage.matchFailures) != 1 || rig.storage.matchFailures[0].Fault != meshevents.MatchFailureTakerFault {
			t.Fatalf("expected one taker inactivity failure, got %+v", rig.storage.matchFailures)
		}
	})
}

func TestFailMatch(t *testing.T) {
	tests := []struct {
		name           string
		applyErr       error
		callTwice      bool
		advanceTo      order.MatchStatus // status move between decision and propose
		addressArrived bool
		wantEvents     int
	}{
		{
			name:       "emits and applies",
			wantEvents: 1,
		},
		{
			name:       "apply error leaves match tracked",
			applyErr:   errors.New("apply failed"),
			callTwice:  true,
			wantEvents: 2,
		},
		{
			name:       "duplicate after apply does not emit",
			callTwice:  true,
			wantEvents: 1,
		},
		{
			name:       "stale decision status is rejected, not repurposed",
			advanceTo:  order.MakerRedeemed,
			wantEvents: 1,
		},
		{
			name:           "submission preserves captured address state",
			addressArrived: true,
			wantEvents:     1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			set := tPerfectLimitLimit(uint64(1e8), uint64(1e8), true)
			matchInfo := set.matchInfos[0]
			rig := tNewUnstartedRig(matchInfo)
			rig.swapper.TrackMatches([]*order.MatchSet{set.matchSet})
			tracker := rig.getTracker()
			tracker.mtx.Lock()
			tracker.Status = order.TakerSwapCast
			if tt.addressArrived {
				tracker.Status = order.NewlyMatched
			}
			tracker.mtx.Unlock()

			failure := matchFailure{match: tracker, status: tracker.Status, fault: meshevents.MatchFailureMakerFault}
			if tt.addressArrived {
				failure.fault = meshevents.MatchFailureTakerFault
				tracker.takerSwapAddr = "taker address"
			}
			tMesh := &tSwapMesh{err: tt.applyErr, applier: rig.swapper.Events()}
			rig.swapper.SetMeshService(tMesh)
			if tt.advanceTo != 0 {
				tracker.mtx.Lock()
				tracker.Status = tt.advanceTo
				tracker.mtx.Unlock()
			}
			rig.swapper.failMatch(failure)
			if tt.callTwice {
				rig.swapper.failMatch(failure)
			}

			if len(tMesh.events) != tt.wantEvents {
				t.Fatalf("match_failed events = %d, want %d", len(tMesh.events), tt.wantEvents)
			}
			event, err := meshevents.DecodeMatchFailedEvent(tMesh.events[0].Payload)
			if err != nil {
				t.Fatalf("DecodeMatchFailedEvent error: %v", err)
			}
			if event.MatchID != matchInfo.matchID || event.Status != failure.status || event.Fault != failure.fault || event.MakerAddressKnown || event.TakerAddressKnown {
				t.Fatalf("match_failed event = %#v, want captured decision %+v", event, failure)
			}
			applied := tt.applyErr == nil && tt.advanceTo == 0 && !tt.addressArrived
			tracked := rig.swapper.matches[matchInfo.matchID] != nil
			if applied && tracked {
				t.Fatalf("failed match still tracked")
			}
			if !applied && !tracked {
				t.Fatalf("live match was untracked")
			}
		})
	}
}

func TestExtendInactionDeadlines(t *testing.T) {
	now := time.Now()
	past, future := now.Add(-time.Hour), now.Add(time.Hour)
	for _, tt := range []struct {
		name                                     string
		recorded, grace                          time.Time
		wantMatch, wantConfirmed, wantRedemption time.Time
	}{
		{name: "unset timestamps", wantMatch: now},
		{name: "past timestamps", recorded: past, wantMatch: now, wantConfirmed: now, wantRedemption: now},
		{name: "future timestamps", recorded: future, wantMatch: future, wantConfirmed: future, wantRedemption: future},
		{name: "existing later grace", recorded: past, grace: future, wantMatch: now, wantConfirmed: now, wantRedemption: future},
	} {
		t.Run(tt.name, func(t *testing.T) {
			tracker := &matchTracker{
				time: tt.recorded,
				makerStatus: &swapStatus{
					redeemTime:       tt.recorded,
					redeemGraceStart: tt.grace,
					swapConfirmed:    tt.recorded,
				},
				takerStatus: &swapStatus{swapConfirmed: tt.recorded},
			}
			s := &Swapper{matches: map[order.MatchID]*matchTracker{{}: tracker}}
			s.extendInactionDeadlines(now)

			if !tracker.time.Equal(tt.wantMatch) {
				t.Fatalf("match response start = %v, want %v", tracker.time, tt.wantMatch)
			}
			if !tracker.makerStatus.swapConfirmed.Equal(tt.wantConfirmed) || !tracker.takerStatus.swapConfirmed.Equal(tt.wantConfirmed) {
				t.Fatalf("swap confirmation times = %v/%v, want %v", tracker.makerStatus.swapConfirmed, tracker.takerStatus.swapConfirmed, tt.wantConfirmed)
			}
			if got := tracker.makerStatus.redeemInactionStart(); !got.Equal(tt.wantRedemption) {
				t.Fatalf("redemption response start = %v, want %v", got, tt.wantRedemption)
			}
			if !tracker.makerStatus.redeemTime.Equal(tt.recorded) {
				t.Fatal("recorded redemption time changed")
			}
		})
	}
}

func TestUserConnectedResend(t *testing.T) {
	for _, tt := range []struct {
		name                   string
		master                 bool
		makerReconnects        bool
		bothAcked              bool
		setup                  func(*matchTracker)
		wantRoute, wantAddress string
	}{
		{
			name:            "master sends maker the counterparty address",
			master:          true,
			makerReconnects: true,
			bothAcked:       true,
			wantAddress:     "taker-addr",
		},
		{
			name:        "master sends taker the counterparty address",
			master:      true,
			bothAcked:   true,
			wantAddress: "maker-addr",
		},
		{
			name:            "maker reconnect does not resend taker's pending request",
			master:          true,
			makerReconnects: true,
		},
		{
			name:      "master resends taker's pending match request",
			master:    true,
			wantRoute: msgjson.MatchRoute,
		},
		{
			name:      "master resends taker's pending audit request",
			master:    true,
			bothAcked: true,
			setup: func(match *matchTracker) {
				match.Status = order.MakerSwapCast
				match.makerStatus.swap = &asset.Contract{
					Coin:         &TCoin{id: randBytes(36)},
					ContractData: randBytes(32),
					TxData:       randBytes(50),
				}
				match.makerStatus.swapTime = time.Now()
			},
			wantRoute:   msgjson.AuditRoute,
			wantAddress: "maker-addr",
		},
		{
			name:      "master resends taker's pending redemption request",
			master:    true,
			bothAcked: true,
			setup: func(match *matchTracker) {
				match.Status = order.MakerRedeemed
				match.makerStatus.redemption = &TCoin{id: randBytes(36)}
				match.makerStatus.redeemTime = time.Now()
				match.makerStatus.secret = randBytes(32)
			},
			wantRoute:   msgjson.RedemptionRoute,
			wantAddress: "maker-addr",
		},
		{
			name:            "non-master sends maker the local counterparty address",
			makerReconnects: true,
			bothAcked:       true,
			wantAddress:     "taker-addr",
		},
		{
			name:        "non-master sends taker the local counterparty address",
			bothAcked:   true,
			wantAddress: "maker-addr",
		},
		{
			name:            "non-master maker reconnect sends nothing with taker's ack missing",
			makerReconnects: true,
		},
		{name: "non-master does not resend taker's pending request"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			set := tPerfectLimitLimit(uint64(1e8), uint64(1e8), true)
			info := set.matchInfos[0]
			rig := tNewUnstartedRig(info)
			rig.swapper.TrackMatches([]*order.MatchSet{set.matchSet})
			rig.swapper.master.Store(tt.master)
			tracker := rig.getTracker()
			tracker.makerSwapAddr = "maker-addr"
			if tt.bothAcked {
				tracker.takerSwapAddr = "taker-addr"
				tracker.counterPartyAddrsSent = true
			}

			if tt.setup != nil {
				tt.setup(tracker)
			}

			user, otherUser := info.taker.acct, info.maker.acct
			if tt.makerReconnects {
				user, otherUser = otherUser, user
			}
			rig.swapper.UserConnected(user)

			if tt.wantRoute != "" {
				req := rig.auth.popReq(user)
				if req == nil || req.req.Route != tt.wantRoute {
					t.Fatalf("request = %v, want %s", req, tt.wantRoute)
				}
			}
			if tt.wantAddress != "" {
				var note msgjson.CounterPartyAddress
				if err := rig.auth.getNtfn(user, msgjson.CounterPartyAddressRoute, &note); err != nil {
					t.Fatal(err)
				}
				if note.Address != tt.wantAddress {
					t.Fatalf("address = %q, want %q", note.Address, tt.wantAddress)
				}
				if local, ok := rig.auth.ntfnWasLocal(user, msgjson.CounterPartyAddressRoute); !ok || !local {
					t.Fatal("counterparty address notification was not local")
				}
			}
			rig.auth.mtx.Lock()
			remaining := len(rig.auth.reqs[user]) + len(rig.auth.ntfns[user])
			otherMessages := len(rig.auth.reqs[otherUser]) + len(rig.auth.ntfns[otherUser])
			rig.auth.mtx.Unlock()
			if remaining != 0 {
				t.Fatalf("reconnecting user received %d unexpected messages", remaining)
			}
			if otherMessages != 0 {
				t.Fatalf("other user received %d messages", otherMessages)
			}
		})
	}
}

// newStaleRig makes a rig with one tracked match at status and the given
// per-match addresses. Event times are aged a full bTimeout so only last-send
// stamps can hold a re-send back. Contracts and the maker redemption follow
// from status; audit acknowledgements must be set by the caller.
func newStaleRig(status order.MatchStatus, makerAddr, takerAddr string) (*testRig, *matchTracker, *tMatchSet) {
	set := tPerfectLimitLimit(uint64(1e8), uint64(1e8), true)
	matchInfo := set.matchInfos[0]
	rig := tNewUnstartedRig(matchInfo)
	rig.swapper.TrackMatches([]*order.MatchSet{set.matchSet})
	tracker := rig.getTracker()
	aged := time.Now().Add(-rig.swapper.bTimeout)
	tracker.Status = status
	tracker.makerSwapAddr = makerAddr
	tracker.takerSwapAddr = takerAddr
	tracker.time = aged
	tContract := func() *asset.Contract {
		return &asset.Contract{
			Coin:         &TCoin{id: randBytes(36)},
			ContractData: encode.RandomBytes(32),
			TxData:       encode.RandomBytes(50),
		}
	}
	switch status {
	case order.MakerSwapCast, order.TakerSwapCast, order.MakerRedeemed:
		tracker.makerStatus.swap = tContract()
		tracker.makerStatus.swapTime = aged
		if status == order.MakerRedeemed {
			tracker.makerStatus.redemption = &TCoin{id: randBytes(36)}
			tracker.makerStatus.redeemTime = aged
			tracker.makerStatus.secret = encode.RandomBytes(32)
		}
	}
	if status == order.TakerSwapCast || status == order.MakerRedeemed {
		tracker.takerStatus.swap = tContract()
		tracker.takerStatus.swapTime = aged
	}
	return rig, tracker, set
}

func popRoutes(auth *TAuthManager, user account.AccountID) (routes []string) {
	for {
		req := auth.popReq(user)
		if req == nil {
			return
		}
		routes = append(routes, req.req.Route)
	}
}

func drainStaleComms(rig *testRig, set *tMatchSet) {
	maker, taker := set.matchInfos[0].maker.acct, set.matchInfos[0].taker.acct
	popRoutes(rig.auth, maker)
	popRoutes(rig.auth, taker)
	_ = rig.auth.getNtfn(maker, msgjson.CounterPartyAddressRoute, new(msgjson.CounterPartyAddress))
	_ = rig.auth.getNtfn(taker, msgjson.CounterPartyAddressRoute, new(msgjson.CounterPartyAddress))
}

func requireQuiet(t *testing.T, rig *testRig, set *tMatchSet) {
	t.Helper()
	maker, taker := set.matchInfos[0].maker.acct, set.matchInfos[0].taker.acct
	if got := popRoutes(rig.auth, maker); len(got) != 0 {
		t.Fatalf("maker routes = %v, want none", got)
	}
	if got := popRoutes(rig.auth, taker); len(got) != 0 {
		t.Fatalf("taker routes = %v, want none", got)
	}
	if err := rig.auth.getNtfn(maker, msgjson.CounterPartyAddressRoute, new(msgjson.CounterPartyAddress)); err == nil {
		t.Fatalf("unexpected maker counterparty address")
	}
	if err := rig.auth.getNtfn(taker, msgjson.CounterPartyAddressRoute, new(msgjson.CounterPartyAddress)); err == nil {
		t.Fatalf("unexpected taker counterparty address")
	}
}

// requireCounterpartyAddress checks the address sent through Send rather than
// SendIfLocal. An empty want requires no notification.
func requireCounterpartyAddress(t *testing.T, auth *TAuthManager, side string, user account.AccountID, want string) {
	t.Helper()
	var cpa msgjson.CounterPartyAddress
	err := auth.getNtfn(user, msgjson.CounterPartyAddressRoute, &cpa)
	if want == "" {
		if err == nil {
			t.Fatalf("unexpected %s counterparty address %q", side, cpa.Address)
		}
		return
	}
	if err != nil {
		t.Fatalf("%s counterparty address: %v", side, err)
	}
	if cpa.Address != want {
		t.Fatalf("%s counterparty address addr = %q, want %q", side, cpa.Address, want)
	}
	if local, ok := auth.ntfnWasLocal(user, msgjson.CounterPartyAddressRoute); !ok || local {
		t.Fatalf("%s counterparty address used SendIfLocal, want Send", side)
	}
}

func TestResendStaleRequests(t *testing.T) {
	tests := []struct {
		name                               string
		status                             order.MatchStatus
		recentAction                       bool
		bothAuditsAcked                    bool
		makerAddr, takerAddr               string
		wantMaker, wantTaker               []string
		wantMakerAddress, wantTakerAddress string
	}{
		{name: "recent match", recentAction: true},
		{name: "recent match with both addresses", recentAction: true, makerAddr: "maker-addr", takerAddr: "taker-addr"},
		{
			name:      "newly matched, taker address missing",
			status:    order.NewlyMatched,
			makerAddr: "maker-addr",
			wantTaker: []string{msgjson.MatchRoute},
		},
		{
			name:             "both addresses, newly matched, address to maker only",
			status:           order.NewlyMatched,
			makerAddr:        "maker-addr",
			takerAddr:        "taker-addr",
			wantMakerAddress: "taker-addr",
		},
		{
			name:             "maker swap cast, taker audit missing",
			status:           order.MakerSwapCast,
			makerAddr:        "maker-addr",
			takerAddr:        "taker-addr",
			wantTaker:        []string{msgjson.AuditRoute},
			wantTakerAddress: "maker-addr",
		},
		{
			name:            "maker redeemed, taker redeem ack missing",
			bothAuditsAcked: true,
			status:          order.MakerRedeemed,
			makerAddr:       "maker-addr",
			takerAddr:       "taker-addr",
			wantTaker:       []string{msgjson.RedemptionRoute},
		},
		{
			name:            "taker swap cast, nothing pending",
			bothAuditsAcked: true,
			status:          order.TakerSwapCast,
			makerAddr:       "maker-addr",
			takerAddr:       "taker-addr",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rig, tracker, set := newStaleRig(tt.status, tt.makerAddr, tt.takerAddr)
			maker, taker := set.matchInfos[0].maker.acct, set.matchInfos[0].taker.acct
			if tt.bothAuditsAcked {
				tracker.Sigs.MakerAudit = set.matchInfos[0].maker.sig
				tracker.Sigs.TakerAudit = set.matchInfos[0].taker.sig
			}
			if tt.recentAction {
				tracker.time = time.Now()
			}
			rig.swapper.resendStaleRequests()
			if got := popRoutes(rig.auth, maker); !slices.Equal(got, tt.wantMaker) {
				t.Fatalf("maker routes = %v, want %v", got, tt.wantMaker)
			}
			if got := popRoutes(rig.auth, taker); !slices.Equal(got, tt.wantTaker) {
				t.Fatalf("taker routes = %v, want %v", got, tt.wantTaker)
			}
			requireCounterpartyAddress(t, rig.auth, "maker", maker, tt.wantMakerAddress)
			requireCounterpartyAddress(t, rig.auth, "taker", taker, tt.wantTakerAddress)
		})
	}
}

// TestRecentRequestsAreNotResent checks that sends postpone periodic retries.
func TestRecentRequestsAreNotResent(t *testing.T) {
	for _, tc := range []struct {
		name                 string
		status               order.MatchStatus
		makerAddr, takerAddr string
		seed                 func(*testRig, *matchTracker, *tMatchSet)
	}{
		{
			name:      "audit and counterparty address",
			status:    order.MakerSwapCast,
			makerAddr: "maker-addr",
			takerAddr: "taker-addr",
			seed: func(rig *testRig, tr *matchTracker, _ *tMatchSet) {
				rig.swapper.requestAudit(tr, false)
				rig.swapper.sendCounterPartyAddresses(tr)
			},
		},
		{
			name:      "redemption",
			status:    order.MakerRedeemed,
			makerAddr: "maker-addr",
			takerAddr: "taker-addr",
			seed: func(rig *testRig, tr *matchTracker, set *tMatchSet) {
				tr.Sigs.MakerAudit = set.matchInfos[0].maker.sig
				tr.Sigs.TakerAudit = set.matchInfos[0].taker.sig
				rig.swapper.resendRedemptionRequest(tr)
			},
		},
		{
			name: "match", // NewlyMatched, no addresses
			seed: func(rig *testRig, _ *matchTracker, set *tMatchSet) {
				rig.swapper.RequestMatchAcks([]*order.MatchSet{set.matchSet})
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rig, tr, set := newStaleRig(tc.status, tc.makerAddr, tc.takerAddr)
			tc.seed(rig, tr, set)
			drainStaleComms(rig, set)
			rig.swapper.resendStaleRequests()
			requireQuiet(t, rig, set)
		})
	}
}

func TestCounterpartyAddressResendPerUser(t *testing.T) {
	tests := []struct {
		name                               string
		status                             order.MatchStatus
		takerReconnects                    bool // which side reconnects before the tick
		wantMakerAddress, wantTakerAddress string
		wantTaker                          []string // routes the tick re-issues to the taker
	}{
		{
			name:             "newly matched, taker reconnect, address to maker",
			status:           order.NewlyMatched,
			takerReconnects:  true,
			wantMakerAddress: "taker-addr",
		},
		{
			name:             "maker swap cast, maker reconnect, address to taker",
			status:           order.MakerSwapCast,
			wantTakerAddress: "maker-addr",
			wantTaker:        []string{msgjson.AuditRoute},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rig, _, set := newStaleRig(tt.status, "maker-addr", "taker-addr")
			maker, taker := set.matchInfos[0].maker.acct, set.matchInfos[0].taker.acct

			// Reconnecting one user must not postpone a retry for the other.
			reconnectingUser := maker
			if tt.takerReconnects {
				reconnectingUser = taker
			}
			rig.swapper.UserConnected(reconnectingUser)
			drainStaleComms(rig, set)

			// The user who needs to broadcast a swap must still receive a retry.
			rig.swapper.resendStaleRequests()
			if got := popRoutes(rig.auth, maker); len(got) != 0 {
				t.Fatalf("maker routes = %v, want none", got)
			}
			if got := popRoutes(rig.auth, taker); !slices.Equal(got, tt.wantTaker) {
				t.Fatalf("taker routes = %v, want %v", got, tt.wantTaker)
			}
			requireCounterpartyAddress(t, rig.auth, "maker", maker, tt.wantMakerAddress)
			requireCounterpartyAddress(t, rig.auth, "taker", taker, tt.wantTakerAddress)

			// The tick's send stamped its recipient's side.
			rig.swapper.resendStaleRequests()
			requireQuiet(t, rig, set)
		})
	}
}

func TestDeleteMatchCleansUpCoinIDs(t *testing.T) {
	set := tPerfectLimitLimit(uint64(1e8), uint64(1e8), true)
	matchInfo := set.matchInfos[0]
	rig, cleanup := tNewTestRig(matchInfo)
	defer cleanup()

	rig.applyMatchesAndRequestAcks(t, set.matchSet)

	tracker := rig.getTracker()

	// Directly register CoinIDs in the dedup maps to simulate what
	// processInit does, avoiding the async coin waiter complexity.
	fakeCoinID1 := "aabbccdd"
	fakeCoinID2 := "eeff0011"
	mid := matchInfo.matchID

	rig.swapper.activeCoinsMtx.Lock()
	rig.swapper.activeCoinIDs[fakeCoinID1] = mid
	rig.swapper.activeCoinIDs[fakeCoinID2] = mid
	rig.swapper.matchCoinIDs[mid] = []string{fakeCoinID1, fakeCoinID2}
	rig.swapper.activeCoinsMtx.Unlock()

	// Verify they're registered.
	rig.swapper.activeCoinsMtx.Lock()
	if _, exists := rig.swapper.activeCoinIDs[fakeCoinID1]; !exists {
		rig.swapper.activeCoinsMtx.Unlock()
		t.Fatal("CoinID1 not registered")
	}
	rig.swapper.activeCoinsMtx.Unlock()

	// Delete the match and verify cleanup. deleteMatch requires matchMtx.
	rig.swapper.matchMtx.Lock()
	rig.swapper.deleteMatch(tracker)
	rig.swapper.matchMtx.Unlock()

	rig.swapper.activeCoinsMtx.Lock()
	if _, exists := rig.swapper.activeCoinIDs[fakeCoinID1]; exists {
		rig.swapper.activeCoinsMtx.Unlock()
		t.Fatal("CoinID1 not cleaned up after deleteMatch")
	}
	if _, exists := rig.swapper.activeCoinIDs[fakeCoinID2]; exists {
		rig.swapper.activeCoinsMtx.Unlock()
		t.Fatal("CoinID2 not cleaned up after deleteMatch")
	}
	if _, exists := rig.swapper.matchCoinIDs[mid]; exists {
		rig.swapper.activeCoinsMtx.Unlock()
		t.Fatal("matchCoinIDs entry not cleaned up after deleteMatch")
	}
	rig.swapper.activeCoinsMtx.Unlock()
}

func TestSecretHashDedup(t *testing.T) {
	// A malicious maker reusing the same secret hash across two matches
	// should be rejected by the server.
	qty := uint64(1e8)
	rate := uint64(1e8)

	maker1, taker1 := tNewUser("maker1"), tNewUser("taker1")
	makerOrder1 := makeLimitOrder(qty, rate, maker1, true)
	takerOrder1 := makeLimitOrder(qty, rate, taker1, false)
	matchInfo1 := tMatchInfo(maker1, taker1, qty, rate, makerOrder1, takerOrder1)
	set1 := new(tMatchSet).add(matchInfo1)

	maker2, taker2 := tNewUser("maker2"), tNewUser("taker2")
	makerOrder2 := makeLimitOrder(qty, rate, maker2, true)
	takerOrder2 := makeLimitOrder(qty, rate, taker2, false)
	matchInfo2 := tMatchInfo(maker2, taker2, qty, rate, makerOrder2, takerOrder2)
	set2 := new(tMatchSet).add(matchInfo2)

	rig, cleanup := tNewTestRig(matchInfo1)
	defer cleanup()

	rig.auth.swapReceived = make(chan struct{}, 2)
	rig.auth.auditReq = make(chan struct{}, 2)

	rig.applyMatchesAndRequestAcks(t, set1.matchSet)
	rig.applyMatchesAndRequestAcks(t, set2.matchSet)

	// Ack both matches.
	rig.matchInfo = matchInfo1
	if err := rig.ackMatch_maker(true); err != nil {
		t.Fatalf("match1 maker ack: %v", err)
	}
	if err := rig.ackMatch_taker(true); err != nil {
		t.Fatalf("match1 taker ack: %v", err)
	}
	rig.matchInfo = matchInfo2
	if err := rig.ackMatch_maker(true); err != nil {
		t.Fatalf("match2 maker ack: %v", err)
	}
	if err := rig.ackMatch_taker(true); err != nil {
		t.Fatalf("match2 taker ack: %v", err)
	}

	// Send swap for match 1.
	rig.matchInfo = matchInfo1
	swap1 := tNewSwap(matchInfo1, matchInfo1.makerOID, matchInfo1.takerPerMatchAddr, matchInfo1.maker)
	rig.abcNode.setContract(swap1.coin, false)
	rig.auth.swapID = swap1.req.ID
	rpcErr := rig.swapper.handleInit(matchInfo1.maker.acct, swap1.req)
	if rpcErr != nil {
		t.Fatalf("match1 swap init failed: %v", rpcErr.Message)
	}
	select {
	case <-rig.auth.swapReceived:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for match1 swap response")
	}
	if err := rig.checkServerResponseSuccess(matchInfo1.maker); err != nil {
		t.Fatalf("match1 swap should succeed: %v", err)
	}

	// Send swap for match 2 with the same secret hash as match 1.
	rig.matchInfo = matchInfo2
	swap2 := tNewSwap(matchInfo2, matchInfo2.makerOID, matchInfo2.takerPerMatchAddr, matchInfo2.maker)
	// Override the secret hash on the contract stored in the backend.
	swap2.coin.SecretHash = swap1.coin.SecretHash
	rig.abcNode.setContract(swap2.coin, false)
	rig.auth.swapID = swap2.req.ID
	rpcErr = rig.swapper.handleInit(matchInfo2.maker.acct, swap2.req)
	if rpcErr != nil {
		// Synchronous rejection.
		if !strings.Contains(rpcErr.Message, "already in use") {
			t.Fatalf("expected 'already in use' error, got: %s", rpcErr.Message)
		}
		return
	}

	timeOutMempool()
	select {
	case <-rig.auth.swapReceived:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for match2 swap response")
	}
	if err := rig.checkServerResponseFail(matchInfo2.maker, msgjson.ContractError, "already in use"); err != nil {
		t.Fatalf("expected secret hash reuse error: %v", err)
	}
}

func TestSecretHashDedupSameMatch(t *testing.T) {
	// Same secret hash retried for the same match should not be rejected
	// by the dedup check.
	set := tPerfectLimitLimit(uint64(1e8), uint64(1e8), true)
	matchInfo := set.matchInfos[0]
	rig, cleanup := tNewTestRig(matchInfo)
	defer cleanup()

	rig.auth.swapReceived = make(chan struct{}, 2)
	rig.auth.auditReq = make(chan struct{}, 2)

	rig.applyMatchesAndRequestAcks(t, set.matchSet)

	if err := rig.ackMatch_maker(true); err != nil {
		t.Fatalf("maker ack: %v", err)
	}
	if err := rig.ackMatch_taker(true); err != nil {
		t.Fatalf("taker ack: %v", err)
	}

	// First init should succeed.
	swap := tNewSwap(matchInfo, matchInfo.makerOID, matchInfo.takerPerMatchAddr, matchInfo.maker)
	rig.abcNode.setContract(swap.coin, false)
	rig.auth.swapID = swap.req.ID
	rpcErr := rig.swapper.handleInit(matchInfo.maker.acct, swap.req)
	if rpcErr != nil {
		t.Fatalf("first init failed: %v", rpcErr.Message)
	}
	select {
	case <-rig.auth.swapReceived:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for first swap response")
	}
	if err := rig.checkServerResponseSuccess(matchInfo.maker); err != nil {
		t.Fatalf("first init should succeed: %v", err)
	}

	// Retry same match with same secret hash - dedup should not reject.
	swap2 := tNewSwap(matchInfo, matchInfo.makerOID, matchInfo.takerPerMatchAddr, matchInfo.maker)
	swap2.coin.SecretHash = swap.coin.SecretHash
	var init2 msgjson.Init
	swap2.req.Unmarshal(&init2)
	init2.CoinID = swap.coin.ID()
	swap2.req, _ = msgjson.NewRequest(swap2.req.ID, msgjson.InitRoute, &init2)
	rpcErr = rig.swapper.handleInit(matchInfo.maker.acct, swap2.req)
	if rpcErr == nil {
		t.Fatal("expected error for duplicate init on already-swapped match")
	}
	if strings.Contains(rpcErr.Message, "already in use") {
		t.Fatal("secret hash dedup should not reject same-match retries")
	}
}

func TestDeleteMatchCleansUpSecretHashes(t *testing.T) {
	set := tPerfectLimitLimit(uint64(1e8), uint64(1e8), true)
	matchInfo := set.matchInfos[0]
	rig, cleanup := tNewTestRig(matchInfo)
	defer cleanup()

	rig.applyMatchesAndRequestAcks(t, set.matchSet)

	tracker := rig.getTracker()
	mid := matchInfo.matchID

	// Register secret hashes in the dedup maps.
	fakeHash := "abcdef0123456789"
	rig.swapper.activeCoinsMtx.Lock()
	rig.swapper.activeSecretHashes[fakeHash] = mid
	rig.swapper.matchSecretHashes[mid] = []string{fakeHash}
	rig.swapper.activeCoinsMtx.Unlock()

	// Delete the match.
	rig.swapper.matchMtx.Lock()
	rig.swapper.deleteMatch(tracker)
	rig.swapper.matchMtx.Unlock()

	// Verify cleanup.
	rig.swapper.activeCoinsMtx.Lock()
	if _, exists := rig.swapper.activeSecretHashes[fakeHash]; exists {
		rig.swapper.activeCoinsMtx.Unlock()
		t.Fatal("secret hash not cleaned up after deleteMatch")
	}
	if _, exists := rig.swapper.matchSecretHashes[mid]; exists {
		rig.swapper.activeCoinsMtx.Unlock()
		t.Fatal("matchSecretHashes entry not cleaned up after deleteMatch")
	}
	rig.swapper.activeCoinsMtx.Unlock()
}

// seedActiveSwap adds an active match and its orders to the storage stub.
func seedActiveSwap(ts *TStorage, info *tMatch, status order.MatchStatus, makerAddr, takerAddr string) *db.SwapData {
	match := info.match
	if ts.orders == nil {
		ts.orders = make(map[order.OrderID]order.Order)
	}
	ts.orders[info.makerOID] = match.Maker
	ts.orders[info.takerOID] = match.Taker
	sd := &db.SwapData{MakerSwapAddr: makerAddr, TakerSwapAddr: takerAddr}
	ts.activeSwaps = append(ts.activeSwaps, &db.SwapDataFull{
		Base:  match.Maker.Base(),
		Quote: match.Maker.Quote(),
		MatchData: &db.MatchData{
			ID:            info.matchID,
			Taker:         info.takerOID,
			TakerAcct:     info.taker.acct,
			TakerAddr:     info.taker.addr,
			TakerSell:     !match.Maker.T.Sell,
			Maker:         info.makerOID,
			MakerAcct:     info.maker.acct,
			MakerAddr:     info.maker.addr,
			Epoch:         match.Epoch,
			Quantity:      match.Quantity,
			Rate:          match.Rate,
			BaseRate:      match.FeeRateBase,
			QuoteRate:     match.FeeRateQuote,
			Active:        true,
			Status:        status,
			MakerSwapAddr: makerAddr,
			TakerSwapAddr: takerAddr,
		},
		SwapData: sd,
	})
	return sd
}

func TestRestoreActiveSwaps(t *testing.T) {
	for _, tc := range []struct {
		name      string
		status    order.MatchStatus
		makerAddr string
		takerAddr string
	}{
		{name: "newly matched", status: order.NewlyMatched},
		{name: "maker acknowledged", status: order.NewlyMatched, makerAddr: "maker-addr"},
		{name: "maker redeemed", status: order.MakerRedeemed, makerAddr: "maker-addr", takerAddr: "taker-addr"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			maker, taker := tNewUser("maker"), tNewUser("taker")
			makerOrder, takerOrder := limitLimitPair(1e8, 1e8, 1e8, 1e8, maker, taker, true)
			makerOrder.T.Coins = []order.CoinID{randBytes(36), randBytes(36)}
			takerOrder.T.Coins = []order.CoinID{randBytes(36)}
			info := tMatchInfo(maker, taker, 1e8, 1e8, makerOrder, takerOrder)
			rig := tNewUnstartedRig(info)
			sd := seedActiveSwap(rig.storage, info, tc.status, tc.makerAddr, tc.takerAddr)
			var contractA, contractB *asset.Contract
			var redemption *TCoin
			if tc.makerAddr != "" {
				sd.SigMatchAckMaker = randBytes(70)
			}
			if tc.takerAddr != "" {
				sd.SigMatchAckTaker = randBytes(70)
			}
			if tc.status == order.MakerRedeemed {
				sd.RedeemASecret = randBytes(32)
				secretHash := sha256.Sum256(sd.RedeemASecret)
				info.secretHash = secretHash[:]
				contractA = &asset.Contract{Coin: &TCoin{id: randBytes(36)}, SecretHash: info.secretHash}
				contractB = &asset.Contract{Coin: &TCoin{id: randBytes(36)}, SecretHash: info.secretHash}
				redemption = &TCoin{id: randBytes(36)}
				sd.ContractACoinID, sd.ContractA = contractA.ID(), randBytes(50)
				sd.ContractBCoinID, sd.ContractB = contractB.ID(), randBytes(50)
				sd.ContractAAckSig, sd.ContractBAckSig = randBytes(70), randBytes(70)
				sd.RedeemACoinID = redemption.ID()
				sd.RedeemAAckSig = randBytes(70)
				rig.abcNode.setContract(contractA, false)
				rig.xyzNode.setContract(contractB, false)
				rig.xyzNode.setRedemption(redemption, contractB, false)
				rig.xyzNode.wantRedeemContract = sd.ContractB
			}

			if err := rig.swapper.RestoreActiveSwaps(false); err != nil {
				t.Fatal(err)
			}
			for _, coin := range makerOrder.T.Coins {
				if !rig.swapper.coins[ABCID].Locker.CoinLocked(coin) {
					t.Fatal("maker funding coin not locked after restore")
				}
			}
			for _, coin := range takerOrder.T.Coins {
				if !rig.swapper.coins[XYZID].Locker.CoinLocked(coin) {
					t.Fatal("taker funding coin not locked after restore")
				}
			}

			tracker := rig.getTracker()
			if tracker == nil {
				t.Fatal("no tracker restored for the active match")
			}
			if tracker.Status != tc.status || tracker.makerSwapAddr != tc.makerAddr || tracker.takerSwapAddr != tc.takerAddr {
				t.Fatalf("restored tracker = %v %q/%q, want %v %q/%q",
					tracker.Status, tracker.makerSwapAddr, tracker.takerSwapAddr, tc.status, tc.makerAddr, tc.takerAddr)
			}
			wantSigs := order.Signatures{
				MakerMatch: sd.SigMatchAckMaker, TakerMatch: sd.SigMatchAckTaker,
				MakerAudit: sd.ContractBAckSig, TakerAudit: sd.ContractAAckSig,
				TakerRedeem: sd.RedeemAAckSig,
			}
			if !reflect.DeepEqual(tracker.Sigs, wantSigs) {
				t.Fatalf("restored signatures = %x, want %x", tracker.Sigs, wantSigs)
			}
			if tracker.makerStatus.swap != contractA || tracker.takerStatus.swap != contractB {
				t.Fatal("restored contracts do not match the backend contracts")
			}
			if tc.status == order.MakerRedeemed {
				if tracker.makerStatus.redemption != redemption || !bytes.Equal(tracker.makerStatus.secret, sd.RedeemASecret) {
					t.Fatal("maker redemption or secret was not restored")
				}
				for _, key := range []string{swapContractKey(sd.ContractACoinID, sd.ContractA), swapContractKey(sd.ContractBCoinID, sd.ContractB)} {
					if rig.swapper.activeCoinIDs[key] != info.matchID {
						t.Fatal("restored contract was not registered for duplicate detection")
					}
				}
				if rig.swapper.activeSecretHashes[secretHashKey(info.secretHash)] != info.matchID {
					t.Fatal("restored secret hash was not registered for duplicate detection")
				}
			}
		})
	}
}

func TestRestoreActiveSwapsFailures(t *testing.T) {
	maker, taker := tNewUser("maker"), tNewUser("taker")
	makerOrder, takerOrder := limitLimitPair(1e8, 1e8, 1e8, 1e8, maker, taker, true)
	makerOrder.T.Coins = []order.CoinID{randBytes(36), randBytes(36)}
	takerOrder.T.Coins = []order.CoinID{randBytes(36)}
	info := tMatchInfo(maker, taker, 1e8, 1e8, makerOrder, takerOrder)
	for _, tc := range []struct {
		name    string
		corrupt func(*TStorage)
		wantErr string
	}{
		{"missing taker order", func(ts *TStorage) {
			delete(ts.orders, info.takerOID)
		}, "failed to load taker order"},
		{"missing maker order", func(ts *TStorage) {
			delete(ts.orders, info.makerOID)
		}, "failed to load maker order"},
		{"taker order ID mismatch", func(ts *TStorage) {
			ts.orders[info.takerOID] = info.match.Maker
		}, fmt.Sprintf("loaded taker order %v for active match %v, but computed ID", info.takerOID, info.matchID)},
		{"maker order ID mismatch", func(ts *TStorage) {
			ts.orders[info.makerOID] = info.match.Taker
		}, fmt.Sprintf("loaded maker order %v for active match %v, but computed ID", info.makerOID, info.matchID)},
		{"corrupt match row", func(ts *TStorage) {
			ts.activeSwaps[0].MatchData.Quantity++
		}, fmt.Sprintf("loaded match %v, but computed ID", info.matchID)},
		{"missing swap contract", func(ts *TStorage) {
			ts.activeSwaps[0].Status = order.MakerSwapCast
			ts.activeSwaps[0].SwapData.ContractACoinID = randBytes(36)
		}, "unable to find swap out coin"},
		{"conflicting funding coin", func(ts *TStorage) {
			otherMaker, otherTaker := tNewUser("other maker"), tNewUser("other taker")
			otherMakerOrder, otherTakerOrder := limitLimitPair(1e8, 1e8, 1e8, 1e8, otherMaker, otherTaker, true)
			otherMakerOrder.T.Coins = []order.CoinID{makerOrder.T.Coins[0]}
			otherTakerOrder.T.Coins = []order.CoinID{randBytes(36)}
			other := tMatchInfo(otherMaker, otherTaker, 1e8, 1e8, otherMakerOrder, otherTakerOrder)
			seedActiveSwap(ts, other, order.NewlyMatched, "", "")
		}, "failed to seed swap coin locks"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rig := tNewUnstartedRig(info)
			seedActiveSwap(rig.storage, info, order.NewlyMatched, "maker-addr", "taker-addr")
			tc.corrupt(rig.storage)
			if err := rig.swapper.RestoreActiveSwaps(false); err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("restore error = %v, want %q", err, tc.wantErr)
			}
		})
	}
}
