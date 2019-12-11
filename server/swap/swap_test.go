// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package swap

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"strconv"
	"sync"
	"testing"
	"time"

	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/msgjson"
	"decred.org/dcrdex/dex/order"
	"decred.org/dcrdex/server/account"
	"decred.org/dcrdex/server/asset"
	"decred.org/dcrdex/server/coinlock"
	"decred.org/dcrdex/server/comms"
	"decred.org/dcrdex/server/matcher"
)

const (
	ABCID = 123
	XYZID = 789
)

var (
	testCtx      context.Context
	acctTemplate = account.AccountID{
		0x22, 0x4c, 0xba, 0xaa, 0xfa, 0x80, 0xbf, 0x3b, 0xd1, 0xff, 0x73, 0x15,
		0x90, 0xbc, 0xbd, 0xda, 0x5a, 0x76, 0xf9, 0x1e, 0x60, 0xa1, 0x56, 0x99,
		0x46, 0x34, 0xe9, 0x1c, 0xaa, 0xaa, 0xaa, 0xaa,
	}
	acctCounter uint32 = 0
)

type tUser struct {
	sig      []byte
	sigHex   string
	acct     account.AccountID
	addr     string
	lbl      string
	matchIDs []string
}

func tickMempool() {
	time.Sleep(recheckInterval * 3 / 2)
}

func timeOutMempool() {
	time.Sleep(txWaitExpiration * 3 / 2)
}

func timeoutBroadcast() {
	// swapper.bTimeout is 5*txWaitExpiration for testing
	time.Sleep(txWaitExpiration * 6)
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
	mtx     sync.Mutex
	authErr error
	reqs    map[account.AccountID][]*TRequest
	resps   map[account.AccountID][]*msgjson.Message
	penalty struct {
		violator account.AccountID
		match    *order.Match
		step     order.MatchStatus
	}
}

func newTAuthManager() *TAuthManager {
	return &TAuthManager{
		reqs:  make(map[account.AccountID][]*TRequest),
		resps: make(map[account.AccountID][]*msgjson.Message),
	}
}

func (m *TAuthManager) Send(user account.AccountID, msg *msgjson.Message) {
	m.mtx.Lock()
	defer m.mtx.Unlock()
	l := m.resps[user]
	if l == nil {
		l = make([]*msgjson.Message, 0, 1)
	}
	m.resps[user] = append(l, msg)
}

func (m *TAuthManager) Request(user account.AccountID, msg *msgjson.Message, f func(comms.Link, *msgjson.Message)) {
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
}
func (m *TAuthManager) Sign(...msgjson.Signable) {}
func (m *TAuthManager) Auth(user account.AccountID, msg, sig []byte) error {
	return m.authErr
}
func (m *TAuthManager) Route(string,
	func(account.AccountID, *msgjson.Message) *msgjson.Error) {
}

func (m *TAuthManager) Penalize(id account.AccountID, match *order.Match, step order.MatchStatus) {
	m.mtx.Lock()
	defer m.mtx.Unlock()
	m.penalty.violator = id
	m.penalty.match = match
	m.penalty.step = step
}

func (m *TAuthManager) flushPenalty() (account.AccountID, *order.Match, order.MatchStatus) {
	m.mtx.Lock()
	defer m.mtx.Unlock()
	user := m.penalty.violator
	match := m.penalty.match
	step := m.penalty.step
	m.penalty.violator = account.AccountID{}
	m.penalty.match = nil
	m.penalty.step = 0
	return user, match, step
}

func (m *TAuthManager) getReq(id account.AccountID) *TRequest {
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

func (m *TAuthManager) pushReq(id account.AccountID, req *TRequest) {
	m.mtx.Lock()
	defer m.mtx.Unlock()
	m.reqs[id] = append([]*TRequest{req}, m.reqs[id]...)
}

func (m *TAuthManager) pushResp(id account.AccountID, msg *msgjson.Message) {
	m.mtx.Lock()
	defer m.mtx.Unlock()
	m.resps[id] = append([]*msgjson.Message{msg}, m.resps[id]...)
}

func (m *TAuthManager) getResp(id account.AccountID) (*msgjson.Message, *msgjson.ResponsePayload) {
	m.mtx.Lock()
	defer m.mtx.Unlock()
	msgs := m.resps[id]
	if len(msgs) == 0 {
		return nil, nil
	}
	msg := msgs[0]
	m.resps[id] = msgs[1:]
	resp, _ := msg.Response()
	return msg, resp
}

type TStorage struct{}

func (s *TStorage) UpdateMatch(match *order.Match) error { return nil }
func (s *TStorage) CancelOrder(*order.LimitOrder) error  { return nil }

// This stub satisfies asset.DEXAsset.
type TAsset struct {
	mtx     sync.RWMutex
	coin    asset.Coin
	coinErr error
	bChan   chan uint32
	lbl     string
}

func newTAsset(lbl string) *TAsset {
	return &TAsset{
		bChan: make(chan uint32, 5),
		lbl:   lbl,
	}
}

func (a *TAsset) Coin(coinID, redeemScript []byte) (asset.Coin, error) {
	a.mtx.RLock()
	defer a.mtx.RUnlock()
	return a.coin, a.coinErr
}
func (a *TAsset) BlockChannel(size int) chan uint32 { return a.bChan }
func (a *TAsset) InitTxSize() uint32                { return 100 }
func (a *TAsset) CheckAddress(string) bool          { return true }

func (a *TAsset) setCoinErr(err error) {
	a.mtx.Lock()
	defer a.mtx.Unlock()
	a.coinErr = err
}

// This stub satisfies asset.Transaction, used by asset.DEXAsset.
type TCoin struct {
	mtx       sync.RWMutex
	id        []byte
	confs     int64
	confsErr  error
	auditAddr string
	auditVal  uint64
}

func (coin *TCoin) Confirmations() (int64, error) {
	coin.mtx.RLock()
	defer coin.mtx.RUnlock()
	return coin.confs, coin.confsErr
}

func (coin *TCoin) setConfs(confs int64) {
	coin.mtx.Lock()
	defer coin.mtx.Unlock()
	coin.confs = confs
}

const (
	tNoSpoofSpendsUTXO = iota
	tSpoofSpendsUTXOErr
	tSpoofSpendsUTXONotFound
)

var tSpoofSpendsUTXO = tNoSpoofSpendsUTXO

func (coin *TCoin) SpendsCoin(coinID []byte) (bool, error) {
	if tSpoofSpendsUTXO == tSpoofSpendsUTXOErr {
		return false, fmt.Errorf("test error")
	} else if tSpoofSpendsUTXO == tSpoofSpendsUTXONotFound {
		return false, nil
	}
	return true, nil
}

var tAuditContractErr error = nil

func (coin *TCoin) AuditContract() (string, uint64, error) {
	if tAuditContractErr != nil {
		return "", 0, tAuditContractErr
	}
	return coin.auditAddr, coin.auditVal, nil
}

func (coin *TCoin) Auth(pubkeys, sigs [][]byte, msg []byte) error { return nil }
func (coin *TCoin) ID() []byte                                    { return coin.id }
func (coin *TCoin) TxID() string                                  { return hex.EncodeToString(coin.id) }
func (coin *TCoin) Value() uint64                                 { return 0 }
func (coin *TCoin) SpendSize() uint32                             { return 0 }

func (coin *TCoin) FeeRate() uint64 {
	return 1
}

func TNewAsset(backend asset.DEXAsset) *asset.BackedAsset {
	return &asset.BackedAsset{
		Backend: backend,
		Asset:   dex.Asset{SwapConf: 2},
	}
}

func tNewResponse(id uint64, resp []byte) *msgjson.Message {
	msg, _ := msgjson.NewResponse(id, json.RawMessage(resp), nil)
	return msg
}

// testRig is the primary test data structure.
type testRig struct {
	abc       *asset.BackedAsset
	abcNode   *TAsset
	xyz       *asset.BackedAsset
	xyzNode   *TAsset
	auth      *TAuthManager
	swapper   *Swapper
	matches   *tMatchSet
	matchInfo *tMatch
}

func tNewTestRig(matchInfo *tMatch) *testRig {
	abcBackend := newTAsset("abc")
	abcAsset := TNewAsset(abcBackend)
	abcCoinLocker := coinlock.NewAssetCoinLocker()

	xyzBackend := newTAsset("xyz")
	xyzAsset := TNewAsset(xyzBackend)
	xyzCoinLocker := coinlock.NewAssetCoinLocker()

	authMgr := newTAuthManager()
	storage := &TStorage{}

	swapper := NewSwapper(&Config{
		Ctx: testCtx,
		Assets: map[uint32]*LockableAsset{
			ABCID: {abcAsset, abcCoinLocker},
			XYZID: {xyzAsset, xyzCoinLocker},
		},
		Storage:          storage,
		AuthManager:      authMgr,
		BroadcastTimeout: txWaitExpiration * 5,
	})

	return &testRig{
		abc:       abcAsset,
		abcNode:   abcBackend,
		xyz:       xyzAsset,
		xyzNode:   xyzBackend,
		auth:      authMgr,
		swapper:   swapper,
		matchInfo: matchInfo,
	}
}

func (rig *testRig) getTracker() *matchTracker {
	rig.swapper.matchMtx.Lock()
	defer rig.swapper.matchMtx.Unlock()
	return rig.swapper.matches[rig.matchInfo.matchID]
}

// Taker: Acknowledge the servers match notification.
func (rig *testRig) ackMatch_maker(checkSig bool) (err error) {
	matchInfo := rig.matchInfo
	err = rig.ackMatch(matchInfo.maker, matchInfo.makerOID, matchInfo.taker.addr)
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

// Maker: Acknowledge the servers match notification.
func (rig *testRig) ackMatch_taker(checkSig bool) error {
	matchInfo := rig.matchInfo
	err := rig.ackMatch(matchInfo.taker, matchInfo.takerOID, matchInfo.maker.addr)
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

func (rig *testRig) ackMatch(user *tUser, oid, counterAddr string) error {
	// If the match is already acked, which might be the case for the taker when
	// an order.MatchSet hash multiple makers, skip the this step without error.
	req := rig.auth.getReq(user.acct)
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
	// authorizations.
	resp := tNewResponse(req.req.ID, tAckArr(user, user.matchIDs))
	req.respFunc(nil, resp)
	return nil
}

// helper to check the match notifications.
func (rig *testRig) checkMatchNotification(msg *msgjson.Message, oid, counterAddr string) error {
	matchInfo := rig.matchInfo
	var notes []*msgjson.Match
	err := json.Unmarshal(msg.Payload, &notes)
	if err != nil {
		fmt.Printf("checkMatchNotification unmarshal error: %v\n", err)
	}
	var notification *msgjson.Match
	for _, n := range notes {
		if n.MatchID.String() == matchInfo.matchID {
			notification = n
			break
		}
	}
	if notification == nil {
		return fmt.Errorf("did not find match ID %s in match notifications", matchInfo.matchID)
	}
	if notification.OrderID.String() != oid {
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

// Can be used to ensure that a non-error response is returned from the swapper.
func (rig *testRig) checkResponse(user *tUser, txType string) error {
	msg, resp := rig.auth.getResp(user.acct)
	if msg == nil {
		return fmt.Errorf("unexpected nil response to %s's '%s'", user.lbl, txType)
	}
	if resp.Error != nil {
		return fmt.Errorf("%s swap rpc error. code: %d, msg: %s", user.lbl, resp.Error.Code, resp.Error.Message)
	}
	return nil
}

// Maker: Send swap transaction.
func (rig *testRig) sendSwap_maker(checkStatus bool) (err error) {
	matchInfo := rig.matchInfo
	swap, err := rig.sendSwap(matchInfo.maker, matchInfo.makerOID, matchInfo.taker.addr)
	if err != nil {
		return fmt.Errorf("error sending maker swap transaction: %v", err)
	}
	matchInfo.db.makerSwap = swap
	tracker := rig.getTracker()
	// Check the match status.
	if checkStatus {
		if tracker.Status != order.MakerSwapCast {
			return fmt.Errorf("unexpected swap status %d after maker swap notification", tracker.Status)
		}
		err := rig.checkResponse(matchInfo.maker, "init")
		if err != nil {
			return err
		}
	}
	return nil
}

// Taker: Send swap transaction.
func (rig *testRig) sendSwap_taker(checkStatus bool) (err error) {
	matchInfo := rig.matchInfo
	taker := matchInfo.taker
	swap, err := rig.sendSwap(taker, matchInfo.takerOID, matchInfo.maker.addr)
	if err != nil {
		return fmt.Errorf("error sending taker swap transaction: %v", err)
	}
	matchInfo.db.takerSwap = swap
	if err != nil {
		return err
	}
	tracker := rig.getTracker()
	// Check the match status.
	if checkStatus {
		if tracker.Status != order.TakerSwapCast {
			return fmt.Errorf("unexpected swap status %d after taker swap notification", tracker.Status)
		}
		err := rig.checkResponse(matchInfo.taker, "init")
		if err != nil {
			return err
		}
	}
	return nil
}

func (rig *testRig) sendSwap(user *tUser, oid, recipient string) (*tSwap, error) {
	matchInfo := rig.matchInfo
	swap := tNewSwap(matchInfo, oid, recipient, user)
	if isQuoteSwap(user, matchInfo.match) {
		rig.xyzNode.coin = swap.coin
	} else {
		rig.abcNode.coin = swap.coin
	}
	rpcErr := rig.swapper.handleInit(user.acct, swap.req)
	if rpcErr != nil {
		resp, _ := msgjson.NewResponse(swap.req.ID, nil, rpcErr)
		rig.auth.Send(user.acct, resp)
		return nil, fmt.Errorf("%s swap rpc error. code: %d, msg: %s", user.lbl, rpcErr.Code, rpcErr.Message)
	}
	return swap, nil
}

// Taker: Process the 'audit' request from the swapper. The request should be
// acknowledged separately with ackAudit_taker.
func (rig *testRig) auditSwap_taker() error {
	matchInfo := rig.matchInfo
	req := rig.auth.getReq(matchInfo.taker.acct)
	matchInfo.db.takerAudit = req
	if req == nil {
		return fmt.Errorf("failed to find audit request for taker after maker's init")
	}
	return rig.auditSwap(req.req, matchInfo.takerOID, matchInfo.db.makerSwap.contract, "taker", matchInfo.taker)
}

// Maker: Process the 'audit' request from the swapper. The request should be
// acknowledged separately with ackAudit_maker.
func (rig *testRig) auditSwap_maker() error {
	matchInfo := rig.matchInfo
	req := rig.auth.getReq(matchInfo.maker.acct)
	matchInfo.db.makerAudit = req
	if req == nil {
		return fmt.Errorf("failed to find audit request for maker after taker's init")
	}
	return rig.auditSwap(req.req, matchInfo.makerOID, matchInfo.db.takerSwap.contract, "maker", matchInfo.maker)
}

func (rig *testRig) auditSwap(msg *msgjson.Message, oid, contract, tag string, user *tUser) error {
	if msg == nil {
		return fmt.Errorf("no %s 'audit' request from DEX", user.lbl)
	}

	if msg.Route != msgjson.AuditRoute {
		return fmt.Errorf("expected method '%s', got '%s'", msgjson.AuditRoute, msg.Route)
	}
	var params *msgjson.Audit
	err := json.Unmarshal(msg.Payload, &params)
	if err != nil {
		return fmt.Errorf("error unmarshaling audit params: %v", err)
	}
	if params.OrderID.String() != oid {
		return fmt.Errorf("%s : incorrect order ID in auditSwap, expected '%s', got '%s'", tag, oid, params.OrderID)
	}
	matchID := rig.matchInfo.matchID
	if params.MatchID.String() != matchID {
		return fmt.Errorf("%s : incorrect match ID in auditSwap, expected '%s', got '%s'", tag, matchID, params.MatchID)
	}
	if params.Contract.String() != contract {
		return fmt.Errorf("%s : incorrect contract. expected '%s', got '%s'", tag, contract, params.Contract)
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
func (rig *testRig) redeem_maker(checkStatus bool) error {
	matchInfo := rig.matchInfo
	matchInfo.db.makerRedeem = rig.redeem(matchInfo.maker, matchInfo.makerOID)
	tracker := rig.getTracker()
	// Check the match status
	if checkStatus {
		if tracker.Status != order.MakerRedeemed {
			return fmt.Errorf("unexpected swap status %d after maker redeem notification", tracker.Status)
		}
		err := rig.checkResponse(matchInfo.maker, "redeem")
		if err != nil {
			return err
		}
	}
	return nil
}

// Taker: Redeem maker's swap transaction.
func (rig *testRig) redeem_taker(checkStatus bool) error {
	matchInfo := rig.matchInfo
	matchInfo.db.takerRedeem = rig.redeem(matchInfo.taker, matchInfo.takerOID)
	tracker := rig.getTracker()
	// Check the match status
	if checkStatus {
		if tracker.Status != order.MatchComplete {
			return fmt.Errorf("unexpected swap status %d after taker redeem notification", tracker.Status)
		}
		err := rig.checkResponse(matchInfo.taker, "redeem")
		if err != nil {
			return err
		}
	}
	return nil
}

func (rig *testRig) redeem(user *tUser, oid string) *tRedeem {
	matchInfo := rig.matchInfo
	redeem := tNewRedeem(matchInfo, oid, user)
	if isQuoteSwap(user, matchInfo.match) {
		rig.abcNode.coin = redeem.coin
	} else {
		rig.xyzNode.coin = redeem.coin
	}
	rpcErr := rig.swapper.handleRedeem(user.acct, redeem.req)
	if rpcErr != nil {
		msg, _ := msgjson.NewResponse(redeem.req.ID, nil, rpcErr)
		rig.auth.Send(user.acct, msg)
	}
	return redeem
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
		if !bytes.Equal(tracker.Sigs.MakerRedeem, matchInfo.maker.sig) {
			return fmt.Errorf("expected maker redemption signature '%x', got '%x'", matchInfo.maker.sig, tracker.Sigs.MakerRedeem)
		}
	}
	return nil
}

func (rig *testRig) ackRedemption(user *tUser, oid string, redeem *tRedeem) error {
	if redeem == nil {
		return fmt.Errorf("nil redeem info")
	}
	req := rig.auth.getReq(user.acct)
	if req == nil {
		return fmt.Errorf("failed to find audit request for %s after counterparty's init", user.lbl)
	}
	err := rig.checkRedeem(req.req, oid, redeem.coin.ID(), user.lbl)
	if err != nil {
		return err
	}
	req.respFunc(nil, tNewResponse(req.req.ID, tAck(user, rig.matchInfo.matchID)))
	return nil
}

func (rig *testRig) checkRedeem(msg *msgjson.Message, oid string, coinID []byte, tag string) error {
	var params *msgjson.Redemption
	err := json.Unmarshal(msg.Payload, &params)
	if err != nil {
		return fmt.Errorf("error unmarshaling redeem params: %v", err)
	}
	if params.OrderID.String() != oid {
		return fmt.Errorf("%s : incorrect order ID in checkRedeem, expected '%s', got '%s'", tag, oid, params.OrderID)
	}
	matchID := rig.matchInfo.matchID
	if params.MatchID.String() != matchID {
		return fmt.Errorf("%s : incorrect match ID in checkRedeem, expected '%s', got '%s'", tag, matchID, params.MatchID)
	}
	if !bytes.Equal(params.CoinID, coinID) {
		return fmt.Errorf("%s : incorrect coinID in checkRedeem. expected '%x', got '%x'", tag, coinID, params.CoinID)
	}
	return nil
}

func makeCancelOrder(limitOrder *order.LimitOrder, user *tUser) *order.CancelOrder {
	return &order.CancelOrder{
		Prefix: order.Prefix{
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
		MarketOrder: order.MarketOrder{
			Prefix: order.Prefix{
				AccountID:  user.acct,
				BaseAsset:  ABCID,
				QuoteAsset: XYZID,
				OrderType:  order.LimitOrderType,
				ClientTime: order.UnixTimeMilli(1566497654000),
				ServerTime: order.UnixTimeMilli(1566497655000),
			},
			Sell:     makerSell,
			Quantity: qty,
			Address:  user.addr,
		},
		Rate: rate,
	}
}

func makeMarketOrder(qty uint64, user *tUser, makerSell bool) *order.MarketOrder {
	return &order.MarketOrder{
		Prefix: order.Prefix{
			AccountID:  user.acct,
			BaseAsset:  ABCID,
			QuoteAsset: XYZID,
			OrderType:  order.LimitOrderType,
			ClientTime: order.UnixTimeMilli(1566497654000),
			ServerTime: order.UnixTimeMilli(1566497655000),
		},
		Sell:     makerSell,
		Quantity: qty,
		Address:  user.addr,
	}
}

func limitLimitPair(makerQty, takerQty, makerRate, takerRate uint64, maker, taker *tUser, makerSell bool) (*order.LimitOrder, *order.LimitOrder) {
	return makeLimitOrder(makerQty, makerRate, maker, makerSell), makeLimitOrder(takerQty, takerRate, taker, !makerSell)
}

func marketLimitPair(makerQty, takerQty, rate uint64, maker, taker *tUser, makerSell bool) (*order.LimitOrder, *order.MarketOrder) {
	return makeLimitOrder(makerQty, rate, maker, makerSell), makeMarketOrder(takerQty, taker, !makerSell)
}

func tLimitPair(makerQty, takerQty, matchQty, makerRate, takerRate uint64, sell bool) *tMatchSet {
	set := new(tMatchSet)
	maker, taker := tNewUser("maker"), tNewUser("taker")
	makerOrder, takerOrder := limitLimitPair(makerQty, takerQty, makerRate, takerRate, maker, taker, sell)
	return set.add(tMatchInfo(maker, taker, matchQty, makerRate, makerOrder, takerOrder))
}

func tPerfectLimitLimit(qty, rate uint64, sell bool) *tMatchSet {
	return tLimitPair(qty, qty, qty, rate, rate, sell)
}

func tMarketPair(makerQty, takerQty, rate uint64, makerSell bool) *tMatchSet {
	set := new(tMatchSet)
	maker, taker := tNewUser("maker"), tNewUser("taker")
	makerOrder, takerOrder := marketLimitPair(makerQty, takerQty, rate, maker, taker, makerSell)
	return set.add(tMatchInfo(maker, taker, makerQty, rate, makerOrder, takerOrder))
}

func tPerfectLimitMarket(qty, rate uint64, sell bool) *tMatchSet {
	return tMarketPair(qty, qty, rate, sell)
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
	matchID  string
	makerOID string
	takerOID string
	qty      uint64
	rate     uint64
	maker    *tUser
	taker    *tUser
	db       struct {
		makerRedeem *tRedeem
		takerRedeem *tRedeem
		makerSwap   *tSwap
		takerSwap   *tSwap
		makerAudit  *TRequest
		takerAudit  *TRequest
	}
}

func makeAck(id string, sig string) msgjson.Acknowledgement {
	return msgjson.Acknowledgement{
		MatchID: id,
		Sig:     sig,
	}
}

func tAck(user *tUser, matchID string) []byte {
	b, _ := json.Marshal(makeAck(matchID, user.sigHex))
	return b
}

func tAckArr(user *tUser, matchIDs []string) []byte {
	ackArr := make([]msgjson.Acknowledgement, 0, len(matchIDs))
	for _, matchID := range matchIDs {
		ackArr = append(ackArr, makeAck(matchID, user.sigHex))
	}
	b, _ := json.Marshal(ackArr)
	return b
}

func tMatchInfo(maker, taker *tUser, matchQty, matchRate uint64, makerOrder *order.LimitOrder, takerOrder order.Order) *tMatch {
	match := &order.Match{
		Taker:    takerOrder,
		Maker:    makerOrder,
		Quantity: matchQty,
		Rate:     matchRate,
	}
	matchID := match.ID().String()
	maker.matchIDs = append(maker.matchIDs, matchID)
	taker.matchIDs = append(taker.matchIDs, matchID)
	return &tMatch{
		match:    match,
		qty:      matchQty,
		rate:     matchRate,
		matchID:  matchID,
		makerOID: makerOrder.ID().String(),
		takerOID: takerOrder.ID().String(),
		maker:    maker,
		taker:    taker,
	}
}

type tMatches []*tMatch

// Matches are submitted to the swapper in small batches, one for each taker.
type tMatchSet struct {
	matchSet   *order.MatchSet
	matchInfos tMatches
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
	ms.Makers = append(ms.Makers, match.Maker)
	ms.Amounts = append(ms.Amounts, matchInfo.qty)
	ms.Rates = append(ms.Rates, matchInfo.rate)
	ms.Total += matchInfo.qty
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
	for i, v := range matchQtys {
		maker := tNewUser("maker" + strconv.Itoa(i))
		// Alternate market and limit orders
		makerOrder := makeLimitOrder(v, rates[i], maker, makerSell)
		matchInfo := tMatchInfo(maker, taker, v, rates[i], makerOrder, takerOrder)
		set.add(matchInfo)
	}
	return set
}

// tSwap is the information needed for spoofing a swap transaction.
type tSwap struct {
	coin     *TCoin
	req      *msgjson.Message
	contract string
}

var tValSpoofer uint64 = 1
var tRecipientSpoofer = ""

func tNewSwap(matchInfo *tMatch, oid, recipient string, user *tUser) *tSwap {
	auditVal := matchInfo.qty
	if isQuoteSwap(user, matchInfo.match) {
		auditVal = matcher.BaseToQuote(matchInfo.rate, matchInfo.qty)
	}
	coinID := randBytes(36)
	makerSwap := &TCoin{
		confs:     0,
		auditAddr: recipient + tRecipientSpoofer,
		auditVal:  auditVal * tValSpoofer,
		id:        coinID,
	}
	contract := "01234567" + user.sigHex
	req, _ := msgjson.NewRequest(1, msgjson.InitRoute, &msgjson.Init{
		OrderID: dirtyEncode(oid),
		MatchID: dirtyEncode(matchInfo.matchID),
		// We control what the backend returns, so the txid doesn't matter right now.
		CoinID:   coinID,
		Time:     uint64(order.UnixMilli(unixMsNow())),
		Contract: dirtyEncode(contract),
	})

	return &tSwap{
		coin:     makerSwap,
		req:      req,
		contract: contract,
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
	req  *msgjson.Message
	coin *TCoin
}

func tNewRedeem(matchInfo *tMatch, oid string, user *tUser) *tRedeem {
	coinID := randBytes(36)
	req, _ := msgjson.NewRequest(1, msgjson.InitRoute, &msgjson.Redeem{
		OrderID: dirtyEncode(oid),
		MatchID: dirtyEncode(matchInfo.matchID),
		CoinID:  coinID,
		Time:    uint64(order.UnixMilli(unixMsNow())),
	})
	return &tRedeem{
		req:  req,
		coin: &TCoin{id: coinID},
	}
}

// Create a closure that will call t.Fatal if an error is non-nil.
func makeEnsureNilErr(t *testing.T) func(error) {
	return func(err error) {
		if err != nil {
			t.Fatalf(err.Error())
		}
	}
}

func makeMustBeError(t *testing.T) func(error, string) {
	return func(err error, tag string) {
		if err == nil {
			t.Fatalf("no error for %s", tag)
		}
	}
}

// Create a closure that will call t.Fatal if a user doesn't have an
// msgjson.RPCError of the correct type.
func rpcErrorChecker(t *testing.T, rig *testRig, code int) func(*tUser) {
	return func(user *tUser) {
		msg, resp := rig.auth.getResp(user.acct)
		if msg == nil {
			t.Fatalf("no response for %s", user.lbl)
		}
		if resp.Error == nil {
			t.Fatalf("no error for %s", user.lbl)
		}
		if resp.Error.Code != code {
			t.Fatalf("wrong error code for %s. expected %d, got %d", user.lbl, msgjson.SignatureError, resp.Error.Code)
		}
	}
}

func TestMain(m *testing.M) {
	recheckInterval = time.Millisecond * 5
	txWaitExpiration = time.Millisecond * 50
	var shutdown func()
	testCtx, shutdown = context.WithCancel(context.Background())
	defer shutdown()
	// logger := slog.NewBackend(os.Stdout).Logger("COMMSTEST")
	// logger.SetLevel(slog.LevelTrace)
	// UseLogger(logger)
	os.Exit(m.Run())
}

func testSwap(t *testing.T, rig *testRig) {
	ensureNilErr := makeEnsureNilErr(t)

	// Step through the negotiation process. No errors should be generated.
	var takerAcked bool
	for _, matchInfo := range rig.matches.matchInfos {
		rig.matchInfo = matchInfo
		ensureNilErr(rig.ackMatch_maker(true))
		if !takerAcked {
			ensureNilErr(rig.ackMatch_taker(true))
			takerAcked = true
		}
		ensureNilErr(rig.sendSwap_maker(true))
		ensureNilErr(rig.auditSwap_taker())
		ensureNilErr(rig.ackAudit_taker(true))
		ensureNilErr(rig.sendSwap_taker(true))
		ensureNilErr(rig.auditSwap_maker())
		ensureNilErr(rig.ackAudit_maker(true))
		ensureNilErr(rig.redeem_maker(true))
		ensureNilErr(rig.ackRedemption_taker(true))
		ensureNilErr(rig.redeem_taker(true))
		ensureNilErr(rig.ackRedemption_maker(true))
	}
}

func TestSwaps(t *testing.T) {
	rig := tNewTestRig(nil)
	for _, makerSell := range []bool{true, false} {
		sellStr := " buy"
		if makerSell {
			sellStr = " sell"
		}
		t.Run("perfect limit-limit match"+sellStr, func(t *testing.T) {
			rig.matches = tPerfectLimitLimit(uint64(1e8), uint64(1e8), makerSell)
			rig.swapper.Negotiate([]*order.MatchSet{rig.matches.matchSet})
			testSwap(t, rig)
		})
		t.Run("perfect limit-market match"+sellStr, func(t *testing.T) {
			rig.matches = tPerfectLimitMarket(uint64(1e8), uint64(1e8), makerSell)
			rig.swapper.Negotiate([]*order.MatchSet{rig.matches.matchSet})
			testSwap(t, rig)
		})
		t.Run("imperfect limit-market match"+sellStr, func(t *testing.T) {
			// only requirement is that maker val > taker val.
			rig.matches = tMarketPair(uint64(10e8), uint64(2e8), uint64(5e8), makerSell)
			rig.swapper.Negotiate([]*order.MatchSet{rig.matches.matchSet})
			testSwap(t, rig)
		})
		t.Run("imperfect limit-limit match"+sellStr, func(t *testing.T) {
			rig.matches = tLimitPair(uint64(10e8), uint64(2e8), uint64(2e8), uint64(5e8), uint64(5e8), makerSell)
			rig.swapper.Negotiate([]*order.MatchSet{rig.matches.matchSet})
			testSwap(t, rig)
		})
		for _, isMarket := range []bool{true, false} {
			marketStr := " limit"
			if isMarket {
				marketStr = " market"
			}
			t.Run("three match set"+sellStr+marketStr, func(t *testing.T) {
				matchQtys := []uint64{uint64(1e8), uint64(9e8), uint64(3e8)}
				rates := []uint64{uint64(10e8), uint64(11e8), uint64(12e8)}
				rig.matches = tMultiMatchSet(matchQtys, rates, makerSell, isMarket)
				rig.swapper.Negotiate([]*order.MatchSet{rig.matches.matchSet})
				testSwap(t, rig)
			})
		}
	}
}

func TestNoAck(t *testing.T) {
	set := tPerfectLimitLimit(uint64(1e8), uint64(1e8), true)
	matchInfo := set.matchInfos[0]
	rig := tNewTestRig(matchInfo)
	rig.swapper.Negotiate([]*order.MatchSet{set.matchSet})
	ensureNilErr := makeEnsureNilErr(t)
	mustBeError := makeMustBeError(t)
	maker, taker := matchInfo.maker, matchInfo.taker

	// Check that the response from the Swapper is an
	// msgjson.SettlementSequenceError.
	checkSeqError := func(user *tUser) {
		msg, resp := rig.auth.getResp(user.acct)
		if msg == nil {
			t.Fatalf("checkSeqError: no message")
		}
		if resp.Error == nil {
			t.Fatalf("no error for %s", user.lbl)
		}
		if resp.Error.Code != msgjson.SettlementSequenceError {
			t.Fatalf("wrong rpc error for %s. expected %d, got %d", user.lbl, msgjson.SettlementSequenceError, resp.Error.Code)
		}
	}

	// Don't acknowledge from either side yet. Have the maker broadcast their swap
	// transaction
	mustBeError(rig.sendSwap_maker(true), "maker swap send")
	checkSeqError(maker)
	ensureNilErr(rig.ackMatch_maker(true))
	// Should be good to send the swap now.
	ensureNilErr(rig.sendSwap_maker(true))
	// For the taker, there must be two acknowledgements before broadcasting the
	// swap transaction, the match ack and the audit ack.
	mustBeError(rig.sendSwap_taker(true), "no match-ack taker swap send")
	checkSeqError(taker)
	ensureNilErr(rig.ackMatch_taker(true))
	// Try to send the swap without acknowledging the 'audit'.
	mustBeError(rig.sendSwap_taker(true), "no audit-ack taker swap send")
	checkSeqError(taker)
	ensureNilErr(rig.auditSwap_taker())
	ensureNilErr(rig.ackAudit_taker(true))
	ensureNilErr(rig.sendSwap_taker(true))
	// The maker should have received an 'audit' request. Don't acknowledge yet.
	mustBeError(rig.redeem_maker(true), "maker redeem")
	checkSeqError(maker)
	ensureNilErr(rig.auditSwap_maker())
	ensureNilErr(rig.ackAudit_maker(true))
	ensureNilErr(rig.redeem_maker(true))
	// The taker should have received a 'redemption' request. Don't acknowledge
	// yet.
	mustBeError(rig.redeem_taker(true), "taker redeem")
	checkSeqError(taker)
	ensureNilErr(rig.ackRedemption_taker(true))
	ensureNilErr(rig.redeem_taker(true))
	ensureNilErr(rig.ackRedemption_maker(true))
}

func TestTxWaiters(t *testing.T) {
	set := tPerfectLimitLimit(uint64(1e8), uint64(1e8), true)
	matchInfo := set.matchInfos[0]
	rig := tNewTestRig(matchInfo)
	rig.swapper.Negotiate([]*order.MatchSet{set.matchSet})
	ensureNilErr := makeEnsureNilErr(t)
	dummyError := fmt.Errorf("test error")

	// Get the MatchNotifications that the swapper sent to the clients and check
	// the match notification length, content, IDs, etc.
	ensureNilErr(rig.ackMatch_maker(true))
	ensureNilErr(rig.ackMatch_taker(true))
	// Set an error for the maker's swap asset
	rig.abcNode.setCoinErr(dummyError)
	// The error will be generated by the chainWaiter thread, so will need to
	// check the response.
	ensureNilErr(rig.sendSwap_maker(false))
	timeOutMempool()
	// Should have an rpc error.
	msg, resp := rig.auth.getResp(matchInfo.maker.acct)
	if msg == nil {
		t.Fatalf("no response for erroneous maker swap")
	}
	if resp.Error == nil {
		t.Fatalf("no rpc error for erroneous maker swap")
	}

	rig.abcNode.setCoinErr(nil)
	// Everything should work now.
	ensureNilErr(rig.sendSwap_maker(true))
	ensureNilErr(rig.auditSwap_taker())
	ensureNilErr(rig.ackAudit_taker(true))
	// For the taker swap, simulate latency.
	rig.xyzNode.setCoinErr(dummyError)
	ensureNilErr(rig.sendSwap_taker(false))
	// Wait a tick
	tickMempool()
	// There should not be a response yet.
	msg, _ = rig.auth.getResp(matchInfo.taker.acct)
	if msg != nil {
		t.Fatalf("unexpected response for latent taker swap")
	}
	// Clear the error.
	rig.xyzNode.setCoinErr(nil)
	tickMempool()
	msg, resp = rig.auth.getResp(matchInfo.taker.acct)
	if msg == nil {
		t.Fatalf("no response for erroneous taker swap")
	}
	if resp.Error != nil {
		t.Fatalf("unexpected rpc error for erroneous taker swap. code: %d, msg: %s",
			resp.Error.Code, resp.Error.Message)
	}
	ensureNilErr(rig.auditSwap_maker())
	ensureNilErr(rig.ackAudit_maker(true))

	// Set a transaction error for the maker's redemption.
	rig.xyzNode.setCoinErr(dummyError)
	ensureNilErr(rig.redeem_maker(false))
	tickMempool()
	tickMempool()
	msg, _ = rig.auth.getResp(matchInfo.maker.acct)
	if msg != nil {
		t.Fatalf("unexpected response for latent maker redeem")
	}
	// Clear the error.
	rig.xyzNode.setCoinErr(nil)
	tickMempool()
	msg, resp = rig.auth.getResp(matchInfo.maker.acct)
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
	rig.abcNode.setCoinErr(dummyError)
	ensureNilErr(rig.redeem_taker(false))
	timeOutMempool()
	msg, _ = rig.auth.getResp(matchInfo.taker.acct)
	if msg == nil {
		t.Fatalf("no response for erroneous taker redeem")
	}
	rig.abcNode.setCoinErr(nil)
	ensureNilErr(rig.redeem_taker(true))
	// Set the number of confirmations on the redemptions.
	matchInfo.db.makerRedeem.coin.setConfs(int64(rig.xyz.SwapConf))
	matchInfo.db.takerRedeem.coin.setConfs(int64(rig.abc.SwapConf))
	// send a block through for either chain to trigger a completion check.
	rig.xyzNode.bChan <- 1
	tickMempool()
	if tracker.Status != order.MatchComplete {
		t.Fatalf("match not marked as complete: %d", tracker.Status)
	}
	// Make sure that the tracker is removed from swappers match map.
	if rig.getTracker() != nil {
		t.Fatalf("matchTracker not removed from swapper's match map")
	}
}

func TestBroadcastTimeouts(t *testing.T) {
	rig := tNewTestRig(nil)
	ensureNilErr := makeEnsureNilErr(t)
	sendBlock := func(node *TAsset) {
		node.bChan <- 1
		tickMempool()
	}
	checkRevokeMatch := func(user *tUser, i int) {
		req := rig.auth.getReq(user.acct)
		if req == nil {
			t.Fatalf("no match_cancellation")
		}
		params := new(msgjson.RevokeMatch)
		err := json.Unmarshal(req.req.Payload, &params)
		if err != nil {
			t.Fatalf("unmarshal error for %s at step %d: %s", user.lbl, i, string(req.req.Payload))
		}
		if params.MatchID.String() != rig.matchInfo.matchID {
			t.Fatalf("unexpected revocation match ID for %s at step %d. expected %s, got %s",
				user.lbl, i, rig.matchInfo.matchID, params.MatchID)
		}
		if req.req.Route != msgjson.RevokeMatchRoute {
			t.Fatalf("wrong request method for %s at step %d: expected '%s', got '%s'",
				user.lbl, i, msgjson.RevokeMatchRoute, req.req.Route)
		}
	}
	// tryExpire will sleep for the duration of a BroadcastTimeout, and then
	// check that a penalty was assigned to the appropriate user, and that a
	// revoke_match message is sent to both users.
	tryExpire := func(i, j int, step order.MatchStatus, jerk, victim *tUser, node *TAsset) bool {
		if i != j {
			return false
		}
		// Sending a block through should schedule an inaction check after duration
		// BroadcastTimeout.
		sendBlock(node)
		timeoutBroadcast()
		user, match, lastStep := rig.auth.flushPenalty()
		if match == nil {
			t.Fatalf("no penalty at step %d", i)
		}
		if user != jerk.acct {
			t.Fatalf("user mismatch at step %d", i)
		}
		if lastStep != step {
			t.Fatalf("wrong match status at step %d. expected %d, got %d", i, step, lastStep)
		}
		// Make sure the specified user has a cancellation for this order
		checkRevokeMatch(jerk, i)
		checkRevokeMatch(victim, i)
		return true
	}
	// Run a timeout test after every important step.
	for i := 0; i <= 7; i++ {
		set := tPerfectLimitLimit(uint64(1e8), uint64(1e8), true)
		matchInfo := set.matchInfos[0]
		rig.matchInfo = matchInfo
		rig.swapper.Negotiate([]*order.MatchSet{set.matchSet})
		// Step through the negotiation process. No errors should be generated.
		ensureNilErr(rig.ackMatch_maker(true))
		ensureNilErr(rig.ackMatch_taker(true))
		if tryExpire(i, 0, order.NewlyMatched, matchInfo.maker, matchInfo.taker, rig.abcNode) {
			continue
		}
		if tryExpire(i, 1, order.NewlyMatched, matchInfo.maker, matchInfo.taker, rig.abcNode) {
			continue
		}
		ensureNilErr(rig.sendSwap_maker(true))
		matchInfo.db.makerSwap.coin.setConfs(int64(rig.abc.SwapConf))
		// Do the audit here to clear the 'audit' request from the comms queue.
		ensureNilErr(rig.auditSwap_taker())
		sendBlock(rig.abcNode)
		if tryExpire(i, 2, order.MakerSwapCast, matchInfo.taker, matchInfo.maker, rig.xyzNode) {
			continue
		}
		ensureNilErr(rig.ackAudit_taker(true))
		if tryExpire(i, 3, order.MakerSwapCast, matchInfo.taker, matchInfo.maker, rig.xyzNode) {
			continue
		}
		ensureNilErr(rig.sendSwap_taker(true))
		matchInfo.db.takerSwap.coin.setConfs(int64(rig.xyz.SwapConf))
		// Do the audit here to clear the 'audit' request from the comms queue.
		ensureNilErr(rig.auditSwap_maker())
		sendBlock(rig.xyzNode)
		if tryExpire(i, 4, order.TakerSwapCast, matchInfo.maker, matchInfo.taker, rig.xyzNode) {
			continue
		}
		ensureNilErr(rig.ackAudit_maker(true))
		if tryExpire(i, 5, order.TakerSwapCast, matchInfo.maker, matchInfo.taker, rig.xyzNode) {
			continue
		}
		ensureNilErr(rig.redeem_maker(true))
		matchInfo.db.makerRedeem.coin.setConfs(int64(rig.xyz.SwapConf))
		// Ack the redemption here to clear the 'audit' request from the comms queue.
		ensureNilErr(rig.ackRedemption_taker(true))
		sendBlock(rig.xyzNode)
		if tryExpire(i, 6, order.MakerRedeemed, matchInfo.taker, matchInfo.maker, rig.abcNode) {
			continue
		}
		if tryExpire(i, 7, order.MakerRedeemed, matchInfo.taker, matchInfo.maker, rig.abcNode) {
			continue
		}
		return
	}
}

func TestSigErrors(t *testing.T) {
	dummyError := fmt.Errorf("test error")
	set := tPerfectLimitLimit(uint64(1e8), uint64(1e8), true)
	matchInfo := set.matchInfos[0]
	rig := tNewTestRig(matchInfo)
	rig.swapper.Negotiate([]*order.MatchSet{set.matchSet})
	ensureNilErr := makeEnsureNilErr(t)
	// checkResp makes sure that the specified user has a signature error response
	// from the swapper.
	checkResp := rpcErrorChecker(t, rig, msgjson.SignatureError)
	// We need a way to restore the state of the queue.
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
		msg, _ = rig.auth.getResp(user.acct)
		tReq = rig.auth.getReq(user.acct)
		apply(user)
	}
	testAction := func(stepFunc func(bool) error, user *tUser) {
		rig.auth.authErr = dummyError
		stash(user)
		// I really don't care if this is an error. The error will be pulled from
		// the auth manager. But golangci-lint really wants me to check the error.
		err := stepFunc(false)
		if err == nil && err != nil {
			fmt.Printf("impossible")
		}
		checkResp(user)
		rig.auth.authErr = nil
		apply(user)
		ensureNilErr(stepFunc(true))
	}
	maker, taker := matchInfo.maker, matchInfo.taker
	testAction(rig.ackMatch_maker, maker)
	testAction(rig.ackMatch_taker, taker)
	testAction(rig.sendSwap_maker, maker)
	ensureNilErr(rig.auditSwap_taker())
	testAction(rig.ackAudit_taker, taker)
	testAction(rig.sendSwap_taker, taker)
	ensureNilErr(rig.auditSwap_maker())
	testAction(rig.ackAudit_maker, maker)
	testAction(rig.redeem_maker, maker)
	testAction(rig.ackRedemption_taker, taker)
	testAction(rig.redeem_taker, taker)
	testAction(rig.ackRedemption_maker, maker)
}

func TestMalformedSwap(t *testing.T) {
	set := tPerfectLimitLimit(uint64(1e8), uint64(1e8), true)
	matchInfo := set.matchInfos[0]
	rig := tNewTestRig(matchInfo)
	rig.swapper.Negotiate([]*order.MatchSet{set.matchSet})
	ensureNilErr := makeEnsureNilErr(t)
	checkContractErr := rpcErrorChecker(t, rig, msgjson.ContractError)

	ensureNilErr(rig.ackMatch_maker(true))
	ensureNilErr(rig.ackMatch_taker(true))
	// Bad contract value
	tValSpoofer = 2
	ensureNilErr(rig.sendSwap_maker(false))
	checkContractErr(matchInfo.maker)
	tValSpoofer = 1
	// Bad contract recipient
	tRecipientSpoofer = "2"
	ensureNilErr(rig.sendSwap_maker(false))
	checkContractErr(matchInfo.maker)
	tRecipientSpoofer = ""
	// Unspecified AuditContract error
	tAuditContractErr = fmt.Errorf("test error")
	ensureNilErr(rig.sendSwap_maker(false))
	checkContractErr(matchInfo.maker)
	tAuditContractErr = nil
	// Now make sure it works.
	ensureNilErr(rig.sendSwap_maker(true))
}

func TestBadRedeems(t *testing.T) {
	set := tPerfectLimitLimit(uint64(1e8), uint64(1e8), true)
	matchInfo := set.matchInfos[0]
	rig := tNewTestRig(matchInfo)
	rig.swapper.Negotiate([]*order.MatchSet{set.matchSet})
	ensureNilErr := makeEnsureNilErr(t)
	checkResp := rpcErrorChecker(t, rig, msgjson.RedemptionError)

	// Negotiate up to the redeem transactions.
	ensureNilErr(rig.ackMatch_maker(true))
	ensureNilErr(rig.ackMatch_taker(true))
	ensureNilErr(rig.sendSwap_maker(true))
	ensureNilErr(rig.auditSwap_taker())
	ensureNilErr(rig.ackAudit_taker(true))
	ensureNilErr(rig.sendSwap_taker(true))
	ensureNilErr(rig.auditSwap_maker())
	ensureNilErr(rig.ackAudit_maker(true))
	// SpendsUTXO -> false, nil
	tSpoofSpendsUTXO = tSpoofSpendsUTXONotFound
	ensureNilErr(rig.redeem_maker(false))
	checkResp(matchInfo.maker)
	// Unspoof
	tSpoofSpendsUTXO = tNoSpoofSpendsUTXO
	ensureNilErr(rig.redeem_maker(false))
	ensureNilErr(rig.ackRedemption_taker(true))
	// SpendsUTXO -> false, non-nil
	tSpoofSpendsUTXO = tSpoofSpendsUTXOErr
	ensureNilErr(rig.redeem_taker(false))
	checkResp(matchInfo.taker)
	// Unspoof
	tSpoofSpendsUTXO = tNoSpoofSpendsUTXO
	ensureNilErr(rig.redeem_taker(true))
	ensureNilErr(rig.ackRedemption_maker(true))
}

func TestBadParams(t *testing.T) {
	set := tPerfectLimitLimit(uint64(1e8), uint64(1e8), true)
	matchInfo := set.matchInfos[0]
	rig := tNewTestRig(matchInfo)
	rig.swapper.Negotiate([]*order.MatchSet{set.matchSet})
	swapper := rig.swapper
	match := rig.getTracker()
	user := matchInfo.maker
	acker := &messageAcker{
		user:    user.acct,
		match:   match,
		isMaker: true,
		isAudit: true,
	}
	checkParseErr := rpcErrorChecker(t, rig, msgjson.RPCParseError)
	checkSigErr := rpcErrorChecker(t, rig, msgjson.SignatureError)
	checkAckCountErr := rpcErrorChecker(t, rig, msgjson.AckCountError)
	checkMismatchErr := rpcErrorChecker(t, rig, msgjson.IDMismatchError)

	ack := &msgjson.Acknowledgement{
		MatchID: matchInfo.matchID,
		// non-hex characters
		Sig: "zyxw",
	}
	ackArr := make([]*msgjson.Acknowledgement, 0)
	matches := []*messageAcker{
		{match: rig.getTracker()},
	}

	encodedAck := func() json.RawMessage {
		b, _ := json.Marshal(ack)
		return json.RawMessage(b)
	}
	encodedAckArray := func() json.RawMessage {
		b, _ := json.Marshal(ackArr)
		return json.RawMessage(b)
	}

	// Invalid result.
	msg, _ := msgjson.NewResponse(1, nil, nil)
	msg.Payload = json.RawMessage(`{"result":?}`)
	swapper.processAck(msg, acker)
	checkParseErr(user)
	swapper.processMatchAcks(user.acct, msg, []*messageAcker{})
	checkParseErr(user)

	msg, _ = msgjson.NewResponse(1, encodedAck(), nil)
	swapper.processAck(msg, acker)
	checkSigErr(user)

	msg, _ = msgjson.NewResponse(1, encodedAckArray(), nil)
	swapper.processMatchAcks(user.acct, msg, matches)
	checkAckCountErr(user)

	// check bad ack ID
	ack.MatchID = "bogusid"
	ackArr = append(ackArr, ack)
	msg, _ = msgjson.NewResponse(1, encodedAckArray(), nil)
	swapper.processMatchAcks(user.acct, msg, matches)
	checkMismatchErr(user)

	// Fix the ID, but check for a sig error
	ack.MatchID = matchInfo.matchID
	msg, _ = msgjson.NewResponse(1, encodedAckArray(), nil)
	swapper.processMatchAcks(user.acct, msg, matches)
	checkSigErr(user)
}

func TestCancel(t *testing.T) {
	set := tCancelPair()
	matchInfo := set.matchInfos[0]
	rig := tNewTestRig(matchInfo)
	rig.swapper.Negotiate([]*order.MatchSet{set.matchSet})
	// There should be no matchTracker
	if rig.getTracker() != nil {
		t.Fatalf("found matchTracker for a cancellation")
	}
	// The user should have two match requests.
	user := matchInfo.maker
	req := rig.auth.getReq(user.acct)
	matchNotes := make([]*msgjson.Match, 0)
	err := json.Unmarshal(req.req.Payload, &matchNotes)
	if err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if len(matchNotes) != 2 {
		t.Fatalf("expected 2 match notification, got %d", len(matchNotes))
	}
	makerNote, takerNote := matchNotes[0], matchNotes[1]
	if makerNote.OrderID.String() != matchInfo.makerOID {
		t.Fatalf("expected maker ID %s, got %s", matchInfo.makerOID, makerNote.OrderID)
	}
	if takerNote.OrderID.String() != matchInfo.takerOID {
		t.Fatalf("expected taker ID %s, got %s", matchInfo.takerOID, takerNote.OrderID)
	}
	if makerNote.MatchID.String() != takerNote.MatchID.String() {
		t.Fatalf("match ID mismatch. %s != %s", makerNote.MatchID, takerNote.MatchID)
	}
}

func TestTxMonitored(t *testing.T) {
	sendBlock := func(node *TAsset) {
		node.bChan <- 1
		tickMempool()
	}

	makerSell := true
	set := tPerfectLimitLimit(uint64(1e8), uint64(1e8), makerSell)
	matchInfo := set.matchInfos[0]
	rig := tNewTestRig(matchInfo)
	rig.swapper.Negotiate([]*order.MatchSet{set.matchSet})
	ensureNilErr := makeEnsureNilErr(t)
	//mustBeError := makeMustBeError(t)
	maker, taker := matchInfo.maker, matchInfo.taker

	var makerLockedAsset, takerLockedAsset uint32
	if makerSell {
		makerLockedAsset = matchInfo.match.Maker.Base()  // maker sell locks base asset
		takerLockedAsset = matchInfo.match.Taker.Quote() // taker buy locks quote asset
	} else {
		makerLockedAsset = matchInfo.match.Maker.Quote() // maker sell locks base asset
		takerLockedAsset = matchInfo.match.Taker.Base()  // taker buy locks quote asset
	}

	tracker := rig.getTracker()
	if tracker.Status != order.NewlyMatched {
		t.Fatalf("match not marked as NewlyMatched: %d", tracker.Status)
	}

	// Maker acks match and sends swap tx.
	ensureNilErr(rig.ackMatch_maker(true))
	ensureNilErr(rig.sendSwap_maker(true))
	makerContractTx := rig.matchInfo.db.makerSwap.coin.TxID()
	if !rig.swapper.TxMonitored(maker.acct, makerLockedAsset, makerContractTx) {
		t.Errorf("maker contract %s (asset %d) was not monitored",
			makerContractTx, makerLockedAsset)
	}

	if tracker.Status != order.MakerSwapCast {
		t.Fatalf("match not marked as MakerSwapCast: %d", tracker.Status)
	}
	sendBlock(rig.abcNode)
	sendBlock(rig.xyzNode)

	// For the taker, there must be two acknowledgements before broadcasting the
	// swap transaction, the match ack and the audit ack.
	ensureNilErr(rig.ackMatch_taker(true))
	ensureNilErr(rig.auditSwap_taker())
	ensureNilErr(rig.ackAudit_taker(true))
	ensureNilErr(rig.sendSwap_taker(true))

	takerContractTx := rig.matchInfo.db.takerSwap.coin.TxID()
	if !rig.swapper.TxMonitored(taker.acct, takerLockedAsset, takerContractTx) {
		t.Errorf("taker contract %s (asset %d) was not monitored",
			takerContractTx, takerLockedAsset)
	}

	if tracker.Status != order.TakerSwapCast {
		t.Fatalf("match not marked as TakerSwapCast: %d", tracker.Status)
	}
	sendBlock(rig.abcNode)
	sendBlock(rig.xyzNode)

	ensureNilErr(rig.auditSwap_maker())
	ensureNilErr(rig.ackAudit_maker(true))

	// Set the number of confirmations on the contracts.
	//tracker.makerStatus.swapTime
	// tracker.makerStatus.swapConfirmed = time.Now()
	// tracker.takerStatus.swapConfirmed = time.Now()
	matchInfo.db.makerSwap.coin.setConfs(int64(rig.abc.SwapConf))
	matchInfo.db.takerSwap.coin.setConfs(int64(rig.xyz.SwapConf))

	// send a block through for either chain to trigger a swap check.
	sendBlock(rig.abcNode)
	sendBlock(rig.xyzNode)

	if rig.swapper.TxMonitored(maker.acct, makerLockedAsset, makerContractTx) {
		t.Errorf("maker contract %s (asset %d) was still monitored",
			makerContractTx, makerLockedAsset)
	}

	sendBlock(rig.abcNode)
	sendBlock(rig.xyzNode)

	if rig.swapper.TxMonitored(taker.acct, takerLockedAsset, takerContractTx) {
		t.Errorf("taker contract %s (asset %d) was still monitored",
			takerContractTx, takerLockedAsset)
	}

	// Now redeem

	ensureNilErr(rig.redeem_maker(true))

	makerRedeemTx := rig.matchInfo.db.makerRedeem.coin.TxID()
	if !rig.swapper.TxMonitored(maker.acct, takerLockedAsset, makerRedeemTx) {
		t.Errorf("maker redeem %s (asset %d) was not monitored",
			makerRedeemTx, takerLockedAsset)
	}

	if tracker.Status != order.MakerRedeemed {
		t.Fatalf("match not marked as MakerRedeemed: %d", tracker.Status)
	}

	ensureNilErr(rig.ackRedemption_taker(true))
	ensureNilErr(rig.redeem_taker(true))

	takerRedeemTx := rig.matchInfo.db.takerRedeem.coin.TxID()
	if !rig.swapper.TxMonitored(taker.acct, makerLockedAsset, takerRedeemTx) {
		t.Errorf("taker redeem %s (asset %d) was not monitored",
			takerRedeemTx, makerLockedAsset)
	}

	if tracker.Status != order.MatchComplete {
		t.Fatalf("match not marked as MatchComplete: %d", tracker.Status)
	}

	ensureNilErr(rig.ackRedemption_maker(true))
}
