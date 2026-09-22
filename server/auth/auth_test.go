// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package auth

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/encode"
	"decred.org/dcrdex/dex/msgjson"
	"decred.org/dcrdex/dex/order"
	ordertest "decred.org/dcrdex/dex/order/test"
	"decred.org/dcrdex/dex/wait"
	"decred.org/dcrdex/server/account"
	"decred.org/dcrdex/server/comms"
	"decred.org/dcrdex/server/db"
	"decred.org/dcrdex/server/mesh"
	"decred.org/dcrdex/server/meshevents"
	"github.com/decred/dcrd/dcrec/secp256k1/v4"
	"github.com/decred/dcrd/dcrec/secp256k1/v4/ecdsa"
)

func noop() {}

type emptyEventLogReader struct{}

func (emptyEventLogReader) EventLogFrontier(context.Context) (*db.EventLogPosition, error) {
	return &db.EventLogPosition{}, nil
}

func (emptyEventLogReader) EventLogEntriesAfter(context.Context, uint64, int) ([]*db.EventLogEntry, error) {
	return nil, nil
}

func randBytes(l int) []byte {
	b := make([]byte, l)
	rand.Read(b)
	return b
}

var randomMatchID = ordertest.RandomMatchID

type ratioData struct {
	oidsCompleted  []order.OrderID
	timesCompleted []int64
	oidsCancels    []order.OrderID
	oidsCanceled   []order.OrderID
	timesCanceled  []int64
	epochGaps      []int32
}

// TStorage satisfies the Storage interface
type TStorage struct {
	accountReadErr            error
	acctInfo                  *db.Account
	acctInfoErr               error
	acct                      *account.Account
	matches                   []*db.MatchData
	matchStatuses             []*db.MatchStatus
	userMatchOutcomes         []*db.MatchOutcome
	reputationPreimages       []*db.PreimageOutcome
	reputationMatches         []*db.MatchResult
	reputationOrders          []*db.OrderOutcome
	reputationErr             error
	getUserReputationData     func(context.Context, account.AccountID, int, int, int) ([]*db.PreimageOutcome, []*db.MatchResult, []*db.OrderOutcome, error)
	orderStatuses             []*db.OrderStatus
	acctErr                   error
	regAddr                   string
	regAsset                  uint32
	bonds                     []*db.Bond
	bondPostedResult          *db.BondPostedResult
	bondPostedErr             error
	bondPostedLimits          [3]int
	bondPostedMeta            *db.EventLogMeta
	bondPostedEvent           *meshevents.BondPostedEvent
	prepaidBonds              map[string]*meshevents.PrepaidBond
	prepaidBondsCreatedLog    *db.EventLogEntry
	prepaidBondsCreatedErr    error
	prepaidBondsCreatedMeta   *db.EventLogMeta
	prepaidBondsCreatedUpdate *meshevents.PrepaidBondsCreatedEvent
	reputationForgivenResult  *db.ReputationForgivenResult
	reputationForgivenErr     error
	reputationForgivenMeta    *db.EventLogMeta
	reputationForgivenUpdate  *meshevents.ReputationForgivenEvent
	repInputsListener         func(users ...account.AccountID)
	ratio                     ratioData
}

func (s *TStorage) SetReputationInputsListener(listener func(users ...account.AccountID)) {
	s.repInputsListener = listener
}

func (s *TStorage) notifyRepInputs(users ...account.AccountID) {
	if s.repInputsListener != nil {
		s.repInputsListener(users...)
	}
}

func (s *TStorage) AccountInfo(account.AccountID) (*db.Account, error) {
	return s.acctInfo, s.acctInfoErr
}
func (s *TStorage) Account(ctx context.Context, acct account.AccountID, lockTimeThresh time.Time) (*account.Account, []*db.Bond, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	if s.accountReadErr != nil {
		return nil, nil, s.accountReadErr
	}
	// Mirror the DB query: only bonds at or past the lock-time threshold.
	bonds := make([]*db.Bond, 0, len(s.bonds))
	for _, bond := range s.bonds {
		if bond.LockTime >= lockTimeThresh.Unix() {
			bonds = append(bonds, bond)
		}
	}
	return s.acct, bonds, nil
}
func (s *TStorage) setBondTier(tier uint32) {
	s.bonds = []*db.Bond{{Strength: tier, LockTime: time.Now().Unix() * 2}}
}
func (s *TStorage) CreateAccountWithBond(acct *account.Account, bond *db.Bond) error {
	s.acct = acct
	if acct != nil && acct.PubKey != nil {
		s.acctInfo = &db.Account{
			AccountID: acct.ID,
			Pubkey:    acct.PubKey.SerializeCompressed(),
		}
	}
	if bond != nil {
		s.AddBond(acct.ID, bond)
	}
	return nil
}
func (s *TStorage) AddBond(acct account.AccountID, bond *db.Bond) error {
	if bond == nil {
		return nil
	}
	for _, existing := range s.bonds {
		if existing.AssetID == bond.AssetID && bytes.Equal(existing.CoinID, bond.CoinID) {
			return nil
		}
	}
	s.bonds = append(s.bonds, bond)
	return nil
}
func (s *TStorage) ApplyBondPostedEvent(_ context.Context, meta *db.EventLogMeta, event *meshevents.BondPostedEvent, pimgSz, matchSz, orderSz int) (*db.BondPostedResult, error) {
	s.bondPostedMeta = meta
	s.bondPostedEvent = event
	s.bondPostedLimits = [3]int{pimgSz, matchSz, orderSz}
	return s.bondPostedResult, s.bondPostedErr
}

func (s *TStorage) ApplyReputationForgivenEvent(_ context.Context, meta *db.EventLogMeta, event *meshevents.ReputationForgivenEvent) (*db.ReputationForgivenResult, error) {
	s.reputationForgivenMeta = meta
	s.reputationForgivenUpdate = event
	return s.reputationForgivenResult, s.reputationForgivenErr
}

func (s *TStorage) FetchPrepaidBond(coinID []byte) (uint32, int64, error) {
	if s.prepaidBonds != nil {
		bond := s.prepaidBonds[string(coinID)]
		if bond == nil {
			return 0, 0, fmt.Errorf("pre-paid bond not found")
		}
		return bond.Strength, bond.LockTime, nil
	}
	return 1, time.Now().Add(time.Hour * 48).Unix(), nil
}
func (s *TStorage) ApplyPrepaidBondsCreatedEvent(_ context.Context, meta *db.EventLogMeta, event *meshevents.PrepaidBondsCreatedEvent) (*db.EventLogEntry, error) {
	s.prepaidBondsCreatedMeta = meta
	s.prepaidBondsCreatedUpdate = event
	return s.prepaidBondsCreatedLog, s.prepaidBondsCreatedErr
}

func (s *TStorage) CompletedAndAtFaultMatchStats(aid account.AccountID, lastN int) ([]*db.MatchOutcome, error) {
	return s.userMatchOutcomes, nil
}
func (s *TStorage) UserMatchFails(aid account.AccountID, lastN int) ([]*db.MatchFail, error) {
	return nil, nil
}
func (s *TStorage) UserOrderStatuses(aid account.AccountID, base, quote uint32, oids []order.OrderID) ([]*db.OrderStatus, error) {
	return s.orderStatuses, nil
}
func (s *TStorage) ActiveUserOrderStatuses(aid account.AccountID) ([]*db.OrderStatus, error) {
	var activeOrderStatuses []*db.OrderStatus
	for _, orderStatus := range s.orderStatuses {
		if orderStatus.Status == order.OrderStatusEpoch || orderStatus.Status == order.OrderStatusBooked {
			activeOrderStatuses = append(activeOrderStatuses, orderStatus)
		}
	}
	return activeOrderStatuses, nil
}
func (s *TStorage) AllActiveUserMatches(account.AccountID) ([]*db.MatchData, error) {
	return s.matches, nil
}
func (s *TStorage) MatchStatuses(aid account.AccountID, base, quote uint32, matchIDs []order.MatchID) ([]*db.MatchStatus, error) {
	return s.matchStatuses, nil
}
func (s *TStorage) CreateAccount(acct *account.Account, assetID uint32, addr string) error {
	s.regAddr = addr
	s.regAsset = assetID
	return s.acctErr
}
func (s *TStorage) setRatioData(dat *ratioData) {
	s.ratio = *dat
}

func (s *TStorage) GetUserReputationData(ctx context.Context, user account.AccountID, pimgSz, matchSz, orderSz int) ([]*db.PreimageOutcome, []*db.MatchResult, []*db.OrderOutcome, error) {
	if s.getUserReputationData != nil {
		return s.getUserReputationData(ctx, user, pimgSz, matchSz, orderSz)
	}
	if ctx != nil {
		if err := ctx.Err(); err != nil {
			return nil, nil, nil, err
		}
	}
	if s.reputationErr != nil {
		return nil, nil, nil, s.reputationErr
	}
	pimgs := append([]*db.PreimageOutcome(nil), s.reputationPreimages...)
	matches := append([]*db.MatchResult(nil), s.reputationMatches...)
	ords := append([]*db.OrderOutcome(nil), s.reputationOrders...)
	if len(pimgs) > pimgSz {
		pimgs = pimgs[len(pimgs)-pimgSz:]
	}
	if len(matches) > matchSz {
		matches = matches[len(matches)-matchSz:]
	}
	if len(ords) > orderSz {
		ords = ords[len(ords)-orderSz:]
	}
	return pimgs, matches, ords, nil
}

// TSigner satisfies the Signer interface
type TSigner struct {
	mtx sync.Mutex
	sig *ecdsa.Signature
	//privKey *secp256k1.PrivateKey
	pubkey *secp256k1.PublicKey
}

// tDefaultSig is returned when a test has not supplied a signature.
var tDefaultSig = func() *ecdsa.Signature {
	priv, err := secp256k1.GeneratePrivateKey()
	if err != nil {
		panic(err)
	}
	return ecdsa.Sign(priv, randBytes(32))
}()

// Maybe actually change this to an ecdsa.Sign with a private key instead?
func (s *TSigner) Sign(hash []byte) *ecdsa.Signature {
	s.mtx.Lock()
	defer s.mtx.Unlock()
	if s.sig == nil {
		return tDefaultSig
	}
	return s.sig
}

func (s *TSigner) setSig(sig *ecdsa.Signature) {
	s.mtx.Lock()
	s.sig = sig
	s.mtx.Unlock()
}

func (s *TSigner) PubKey() *secp256k1.PublicKey { return s.pubkey }

type tReq struct {
	timeout  time.Duration
	msg      *msgjson.Message
	respFunc func(comms.Link, *msgjson.Message)
}

// tRPCClient satisfies the comms.Link interface.
type TRPCClient struct {
	id         uint64
	ip         dex.IPKey
	addr       string
	sendErr    error
	sendRawErr error
	requestErr error
	banished   bool
	// mtx protects sends and reqs from concurrent reputation notifications.
	mtx    sync.Mutex
	sends  []*msgjson.Message
	reqs   []*tReq
	on     uint32
	closed chan struct{}
}

func (c *TRPCClient) ID() uint64    { return c.id }
func (c *TRPCClient) IP() dex.IPKey { return c.ip }
func (c *TRPCClient) Addr() string  { return c.addr }
func (c *TRPCClient) Authorized()   {}
func (c *TRPCClient) Send(msg *msgjson.Message) error {
	c.mtx.Lock()
	c.sends = append(c.sends, msg)
	c.mtx.Unlock()
	return c.sendErr
}
func (c *TRPCClient) SendRaw(b []byte) error {
	if c.sendRawErr != nil {
		return c.sendRawErr
	}
	msg, err := msgjson.DecodeMessage(b)
	if err != nil {
		return err
	}
	c.mtx.Lock()
	c.sends = append(c.sends, msg)
	c.mtx.Unlock()
	return nil
}
func (c *TRPCClient) SendError(id uint64, msg *msgjson.Error) {
}
func (c *TRPCClient) Request(msg *msgjson.Message, f func(comms.Link, *msgjson.Message), timeout time.Duration, _ func()) error {
	c.mtx.Lock()
	c.reqs = append(c.reqs, &tReq{
		msg:      msg,
		timeout:  timeout,
		respFunc: f,
	})
	c.mtx.Unlock()
	return c.requestErr
}
func (c *TRPCClient) RequestRaw(msgID uint64, rawMsg []byte, f func(comms.Link, *msgjson.Message), expireTime time.Duration, expire func()) error {
	return nil
}

func (c *TRPCClient) Done() <-chan struct{} {
	return c.closed
}
func (c *TRPCClient) Disconnect() {
	if atomic.CompareAndSwapUint32(&c.on, 0, 1) {
		close(c.closed)
	}
}
func (c *TRPCClient) Banish() { c.banished = true }
func (c *TRPCClient) getReq() *tReq {
	c.mtx.Lock()
	defer c.mtx.Unlock()
	if len(c.reqs) == 0 {
		return nil
	}
	req := c.reqs[0]
	c.reqs = c.reqs[1:]
	return req
}
func (c *TRPCClient) getSend() *msgjson.Message {
	c.mtx.Lock()
	defer c.mtx.Unlock()
	if len(c.sends) == 0 {
		return nil
	}
	msg := c.sends[0]
	c.sends = c.sends[1:]
	return msg
}
func (c *TRPCClient) sendCount() int {
	c.mtx.Lock()
	defer c.mtx.Unlock()
	return len(c.sends)
}

func (c *TRPCClient) CustomID() string {
	return ""
}

func (c *TRPCClient) SetCustomID(string) {}

var tClientID uint64

func tNewRPCClient() *TRPCClient {
	tClientID++
	return &TRPCClient{
		id:     tClientID,
		ip:     dex.NewIPKey("123.123.123.123"),
		addr:   "addr",
		closed: make(chan struct{}),
	}
}

var tAcctID uint64

func newAccountID() account.AccountID {
	tAcctID++
	ib := make([]byte, 8)
	binary.BigEndian.PutUint64(ib, tAcctID)
	var acctID account.AccountID
	copy(acctID[len(acctID)-8:], ib)
	return acctID
}

type tUser struct {
	conn    *TRPCClient
	acctID  account.AccountID
	privKey *secp256k1.PrivateKey
}

// makes a new user with its own account ID, tRPCClient
func tNewUser(t *testing.T) *tUser {
	t.Helper()
	conn := tNewRPCClient()
	privKey, err := secp256k1.GeneratePrivateKey()
	if err != nil {
		t.Fatalf("error generating private key: %v", err)
	}
	acctID := account.NewID(privKey.PubKey().SerializeCompressed())
	return &tUser{
		conn:    conn,
		acctID:  acctID,
		privKey: privKey,
	}
}

func (u *tUser) randomSignature() *ecdsa.Signature {
	return ecdsa.Sign(u.privKey, randBytes(32))
}

type testRig struct {
	mgr     *AuthManager
	storage *TStorage
	signer  *TSigner
}

type tMesh struct {
	executeErr     *msgjson.Error
	executeHook    func(context.Context, mesh.CommandRequest) *msgjson.Error
	executedReq    mesh.CommandRequest
	executeCount   int
	proxiedErr     error
	proxiedUser    account.AccountID
	proxiedMsg     *msgjson.Message
	proxiedTimeout time.Duration
	proxiedDeliver bool
	proxyCount     int
	proxyWait      <-chan struct{}
	proxyReady     chan struct{}
}

func (m *tMesh) ExecuteCommand(ctx context.Context, req mesh.CommandRequest) *msgjson.Error {
	m.executeCount++
	m.executedReq = req
	if m.executeHook != nil {
		return m.executeHook(ctx, req)
	}
	return m.executeErr
}

func (m *tMesh) ProxyClientMessage(_ context.Context, req *mesh.ClientProxyMessage) error {
	if m.proxyWait != nil {
		<-m.proxyWait
	}
	m.proxyCount++
	m.proxiedUser = req.User
	m.proxiedMsg = req.Msg
	m.proxiedTimeout = time.Duration(req.TimeoutMS) * time.Millisecond
	m.proxiedDeliver = req.DeliverToClient
	if m.proxyReady != nil {
		select {
		case m.proxyReady <- struct{}{}:
		default:
		}
	}
	return m.proxiedErr
}

func setTestMeshService(t *testing.T, svc MeshService) {
	t.Helper()
	prev := rig.mgr.mesh
	rig.mgr.SetMeshService(svc)
	t.Cleanup(func() {
		rig.mgr.SetMeshService(prev)
	})
}

var rig *testRig

type tSignable struct {
	b   []byte
	sig []byte
}

func (s *tSignable) SetSig(b []byte)  { s.sig = b }
func (s *tSignable) SigBytes() []byte { return s.sig }
func (s *tSignable) Serialize() []byte {
	return s.b
}

func signMsg(priv *secp256k1.PrivateKey, msg []byte) []byte {
	hash := sha256.Sum256(msg)
	sig := ecdsa.Sign(priv, hash[:])
	return sig.Serialize()
}

func tNewConnect(user *tUser) *msgjson.Connect {
	return &msgjson.Connect{
		AccountID:  user.acctID[:],
		APIVersion: 0,
		Time:       uint64(time.Now().UnixMilli()),
	}
}

func tNewPostBondRequest(t *testing.T, user *tUser, assetID uint32, coinID []byte) (*msgjson.Message, *msgjson.PostBond) {
	t.Helper()
	postBond := &msgjson.PostBond{
		AcctPubKey: user.privKey.PubKey().SerializeCompressed(),
		AssetID:    assetID,
		Version:    0,
		CoinID:     coinID,
	}
	postBond.SetSig(signMsg(user.privKey, postBond.Serialize()))
	msg, err := msgjson.NewRequest(comms.NextID(), msgjson.PostBondRoute, postBond)
	if err != nil {
		t.Fatalf("NewRequest error: %v", err)
	}
	return msg, postBond
}

func newTestAuthManager(t *testing.T) (*AuthManager, *TStorage) {
	t.Helper()
	storage := &TStorage{}
	dexKey, err := secp256k1.ParsePubKey(tDexPubKeyBytes)
	if err != nil {
		t.Fatalf("ParsePubKey error: %v", err)
	}
	authMgr := NewAuthManager(&Config{
		Storage:    storage,
		Signer:     &TSigner{pubkey: dexKey},
		BondExpiry: 86400,
		BondAssets: map[string]*msgjson.BondAsset{
			"dcr": {
				Version: 0,
				ID:      42,
				Confs:   uint32(tBondConfs),
				Amt:     tRegFee * 10,
			},
		},
		BondTxParser:    tParseBondTx,
		CancelThreshold: 0.9,
		TxDataSources:   make(map[uint32]TxDataSource),
		Route:           func(string, comms.MsgHandler) {},
	})
	authMgr.ctx = t.Context()
	return authMgr, storage
}

type blockingAccountStorage struct {
	*TStorage
	started chan struct{}
}

func (s *blockingAccountStorage) Account(ctx context.Context, _ account.AccountID, _ time.Time) (*account.Account, []*db.Bond, error) {
	close(s.started)
	<-ctx.Done()
	return nil, nil, ctx.Err()
}

func eventsWithCapture(authMgr *AuthManager, captured *[]*mesh.Event) map[string]mesh.EventApplier {
	events := authMgr.Events()
	for kind, apply := range events {
		kind, apply := kind, apply
		events[kind] = func(applyCtx *mesh.EventApplyContext, event *mesh.Event) (*db.EventLogEntry, error) {
			*captured = append(*captured, &mesh.Event{
				Kind:    event.Kind,
				Payload: append([]byte(nil), event.Payload...),
			})
			return apply(applyCtx, event)
		}
	}
	return events
}

func extractConnectResult(t *testing.T, msg *msgjson.Message) *msgjson.ConnectResult {
	t.Helper()
	if msg == nil {
		t.Fatalf("no response from 'connect' request")
	}
	resp, _ := msg.Response()
	result := new(msgjson.ConnectResult)
	err := json.Unmarshal(resp.Result, result)
	if err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	return result
}

func queueUser(t *testing.T, user *tUser) *msgjson.Message {
	t.Helper()
	rig.storage.acct = &account.Account{ID: user.acctID, PubKey: user.privKey.PubKey()}
	connect := tNewConnect(user)
	sigMsg := connect.Serialize()
	sig := signMsg(user.privKey, sigMsg)
	connect.SetSig(sig)
	msg, _ := msgjson.NewRequest(comms.NextID(), msgjson.ConnectRoute, connect)
	return msg
}

func connectUser(t *testing.T, user *tUser) *msgjson.Message {
	t.Helper()
	return tryConnectUser(t, user, false)
}

func tryConnectUser(t *testing.T, user *tUser, wantErr bool) *msgjson.Message {
	t.Helper()
	connect := queueUser(t, user)
	err := rig.mgr.handleConnect(user.conn, connect)
	if (err != nil) != wantErr {
		t.Fatalf("handleConnect: wantErr=%v, got err=%v", wantErr, err)
	}

	// Check the response.
	respMsg := user.conn.getSend()
	if respMsg == nil {
		t.Fatalf("no response from 'connect' request")
	}
	if respMsg.ID != connect.ID {
		t.Fatalf("'connect' response has wrong ID. expected %d, got %d", connect.ID, respMsg.ID)
	}
	return respMsg
}

func makeEnsureErr(t *testing.T) func(rpcErr *msgjson.Error, tag string, code int) {
	return func(rpcErr *msgjson.Error, tag string, code int) {
		t.Helper()
		if rpcErr == nil {
			t.Fatalf("no error for %s ID", tag)
		}
		if rpcErr.Code != code {
			t.Fatalf("wrong error code for %s. expected %d, got %d: %s",
				tag, code, rpcErr.Code, rpcErr.Message)
		}
	}
}

func waitFor(pred func() bool, timeout time.Duration) (fail bool) {
	tStart := time.Now()
	for {
		if pred() {
			return false
		}
		if time.Since(tStart) > timeout {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
}

var (
	tBondConfs       int64 = 5
	tParseBondTxAcct account.AccountID
	tParseBondTxErr  error
)

func tParseBondTx(assetID uint32, ver uint16, rawTx []byte) (bondCoinID []byte, amt int64,
	lockTime int64, acct account.AccountID, err error) {
	return nil, 0, time.Now().Add(time.Minute).Unix(), tParseBondTxAcct, tParseBondTxErr
}

const (
	tRegFee       uint64 = 500_000_000
	tDexPubKeyHex string = "032e3678f9889206dcea4fc281556c9e543c5d5ffa7efe8d11118b52e29c773f27"
)

var tDexPubKeyBytes = []byte{
	0x03, 0x2e, 0x36, 0x78, 0xf9, 0x88, 0x92, 0x06, 0xdc, 0xea, 0x4f, 0xc2,
	0x81, 0x55, 0x6c, 0x9e, 0x54, 0x3c, 0x5d, 0x5f, 0xfa, 0x7e, 0xfe, 0x8d,
	0x11, 0x11, 0x8b, 0x52, 0xe2, 0x9c, 0x77, 0x3f, 0x27,
}

var tRoutes = make(map[string]comms.MsgHandler)

func TestMain(m *testing.M) {
	doIt := func() int {
		UseLogger(dex.StdOutLogger("AUTH_TEST", dex.LevelTrace))
		ctx, shutdown := context.WithCancel(context.Background())
		defer shutdown()
		storage := &TStorage{}
		// secp256k1.PrivKeyFromBytes
		dexKey, _ := secp256k1.ParsePubKey(tDexPubKeyBytes)
		signer := &TSigner{pubkey: dexKey}
		authMgr := NewAuthManager(&Config{
			Storage:    storage,
			Signer:     signer,
			BondExpiry: 86400,
			BondAssets: map[string]*msgjson.BondAsset{
				"dcr": {
					Version: 0,
					ID:      42,
					Confs:   uint32(tBondConfs),
					Amt:     tRegFee * 10,
				},
			},
			BondTxParser:    tParseBondTx,
			CancelThreshold: 0.9,
			TxDataSources:   make(map[uint32]TxDataSource),
			Route: func(route string, handler comms.MsgHandler) {
				tRoutes[route] = handler
			},
		})
		meshSvc, err := mesh.NewService(&mesh.ServiceConfig{
			EventLogReader: emptyEventLogReader{},
			OnHalt:         func(error) {},
			Logger:         dex.Disabled,
		})
		if err != nil {
			fmt.Printf("NewService error: %v\n", err)
			return 1
		}
		authMgr.SetMeshService(meshSvc)
		cm := dex.NewConnectionMaster(authMgr)
		cm.Connect(ctx)
		defer cm.Disconnect()
		rig = &testRig{
			storage: storage,
			signer:  signer,
			mgr:     authMgr,
		}
		return m.Run()
	}

	os.Exit(doIt())
}

func userMatchData(takerUser account.AccountID) (*db.MatchData, *order.UserMatch) {
	var baseRate, quoteRate uint64 = 123, 73
	side := order.Taker
	takerSell := true
	feeRateSwap := baseRate // user is selling

	anyID := newAccountID()
	var mid order.MatchID
	copy(mid[:], anyID[:])
	anyID = newAccountID()
	var oid order.OrderID
	copy(oid[:], anyID[:])
	takerUserMatch := &order.UserMatch{
		OrderID:     oid,
		MatchID:     mid,
		Quantity:    1,
		Rate:        2,
		Address:     "makerSwapAddress", // counterparty
		Status:      order.MakerRedeemed,
		Side:        side,
		FeeRateSwap: feeRateSwap,
	}

	var oid2 order.OrderID
	anyID = newAccountID()
	copy(oid2[:], anyID[:])
	matchData := &db.MatchData{
		ID:            mid,
		Taker:         oid,
		TakerAcct:     takerUser,
		TakerAddr:     "takerSwapAddress",
		TakerSell:     takerSell,
		Maker:         oid2,
		MakerAcct:     newAccountID(),
		MakerAddr:     takerUserMatch.Address,
		MakerSwapAddr: takerUserMatch.Address, // per-match address
		TakerSwapAddr: "takerSwapAddress",     // per-match address
		Epoch: order.EpochID{
			Dur: 10000,
			Idx: 132412342,
		},
		Quantity:  takerUserMatch.Quantity,
		Rate:      takerUserMatch.Rate,
		BaseRate:  baseRate,
		QuoteRate: quoteRate,
		Active:    true,
		Status:    takerUserMatch.Status,
	}

	//matchTime := matchData.Epoch.End()
	return matchData, takerUserMatch
}

func TestGraceLimit(t *testing.T) {
	tests := []struct {
		name      string
		thresh    float64
		wantLimit int
	}{
		{"0.99 => 99", 0.99, 99}, // 98.99999999999991
		{"0.98 => 49", 0.98, 49}, // 48.99999999999996
		{"0.96 => 24", 0.96, 24}, // 23.99999999999998
		{"0.95 => 19", 0.95, 19}, // 18.999999999999982
		{"0.9 => 9", 0.9, 9},     // 9.000000000000002
		{"0.875 => 7", 0.875, 7}, // exact
		{"0.8 => 4", 0.8, 4},     // 4.000000000000001
		{"0.75 => 3", 0.75, 3},   // exact
		{"0.5 => 1", 0.5, 1},     // exact
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			auth := &AuthManager{
				cancelThresh: tt.thresh,
			}
			got := auth.GraceLimit()
			if got != tt.wantLimit {
				t.Errorf("incorrect grace limit. got %d, want %d", got, tt.wantLimit)
			}
		})
	}
}

func randomOrderID() (oid order.OrderID) {
	copy(oid[:], encode.RandomBytes(32))
	return
}

var reputationDBID int64

func nextReputationDBID() int64 {
	reputationDBID++
	return reputationDBID
}

func newMatchResult(status order.MatchStatus, fail bool) *db.MatchResult {
	outcome := db.OutcomeSwapSuccess
	if fail {
		outcome = matchStatusToOutcome(status)
	}
	return &db.MatchResult{
		DBID:         nextReputationDBID(),
		MatchID:      randomMatchID(),
		MatchOutcome: outcome,
	}
}

func newPreimageOutcome(miss bool) *db.PreimageOutcome {
	return &db.PreimageOutcome{
		DBID:    nextReputationDBID(),
		OrderID: randomOrderID(),
		Miss:    miss,
	}
}

func setViolations() (wantScore int32) {
	rig.storage.reputationMatches = []*db.MatchResult{
		newMatchResult(order.NewlyMatched, true),
		newMatchResult(order.MatchComplete, false), // success
		newMatchResult(order.NewlyMatched, true),
		newMatchResult(order.MakerSwapCast, true), // noSwapAsTaker at index 3
		newMatchResult(order.TakerSwapCast, true),
		newMatchResult(order.MakerRedeemed, false), // success (for maker)
		newMatchResult(order.MakerRedeemed, true),
		newMatchResult(order.MatchComplete, false), // success
		newMatchResult(order.MatchComplete, false), // success
	}
	rig.storage.reputationPreimages = []*db.PreimageOutcome{newPreimageOutcome(true)}
	for range rig.storage.reputationMatches {
		rig.storage.reputationPreimages = append(rig.storage.reputationPreimages, newPreimageOutcome(false))
	}
	return 4*matchCompletedScore + 1*preimageMissScore +
		2*noSwapAsMakerScore + noSwapAsTakerScore + noRedeemAsMakerScore + 1*noRedeemAsTakerScore
}

func clearViolations() {
	rig.storage.reputationPreimages = nil
	rig.storage.reputationMatches = nil
	rig.storage.reputationOrders = nil
}

func TestAuthManager_loadUserScore(t *testing.T) {
	// Spot test with all violations set
	wantScore := setViolations()
	defer clearViolations()
	user := tNewUser(t)
	score, err := rig.mgr.loadUserScoreContext(context.Background(), user.acctID)
	if err != nil {
		t.Fatal(err)
	}
	if score != wantScore {
		t.Errorf("wrong score. got %d, want %d", score, wantScore)
	}

	// add one NoSwapAsTaker (match inactive at MakerSwapCast)
	rig.storage.reputationMatches = append(rig.storage.reputationMatches,
		newMatchResult(order.MakerSwapCast, true))
	wantScore += noSwapAsTakerScore

	score, err = rig.mgr.loadUserScoreContext(context.Background(), user.acctID)
	if err != nil {
		t.Fatal(err)
	}
	if score != wantScore {
		t.Errorf("wrong score. got %d, want %d", score, wantScore)
	}

	tests := []struct {
		name           string
		user           account.AccountID
		matchOutcomes  []*db.MatchResult
		preimageMisses []*db.PreimageOutcome
		wantScore      int32
	}{
		{
			name: "negative",
			user: user.acctID,
			matchOutcomes: []*db.MatchResult{
				newMatchResult(order.MatchComplete, false),
				newMatchResult(order.MatchComplete, false),
				newMatchResult(order.MatchComplete, false),
				newMatchResult(order.MatchComplete, false),
			},
			wantScore: 4,
		},
		{
			name:          "nuthin",
			user:          user.acctID,
			matchOutcomes: []*db.MatchResult{},
			wantScore:     0,
		},
		{
			name: "balance",
			user: user.acctID,
			matchOutcomes: []*db.MatchResult{
				newMatchResult(order.MatchComplete, false),
				newMatchResult(order.MatchComplete, false),
				newMatchResult(order.MatchComplete, false),
				newMatchResult(order.MatchComplete, false),
			},
			preimageMisses: []*db.PreimageOutcome{
				newPreimageOutcome(true),
				newPreimageOutcome(true),
			},
			wantScore: 0,
		},
		{
			name: "tipping red",
			user: user.acctID,
			matchOutcomes: []*db.MatchResult{
				newMatchResult(order.NewlyMatched, true),
				newMatchResult(order.MakerSwapCast, true),
				newMatchResult(order.MatchComplete, false),
				newMatchResult(order.MatchComplete, false),
				newMatchResult(order.MatchComplete, false),
				newMatchResult(order.NewlyMatched, true),
				newMatchResult(order.MakerRedeemed, true),
				newMatchResult(order.MatchComplete, false),
				newMatchResult(order.MatchComplete, false),
			},
			preimageMisses: []*db.PreimageOutcome{
				newPreimageOutcome(true),
				newPreimageOutcome(false),
			},
			wantScore: 2*noSwapAsMakerScore + 1*noSwapAsTakerScore + 0*noRedeemAsMakerScore +
				1*noRedeemAsTakerScore + 1*preimageMissScore + 5*matchCompletedScore,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rig.storage.reputationMatches = tt.matchOutcomes
			rig.storage.reputationPreimages = tt.preimageMisses
			score, err := rig.mgr.loadUserScoreContext(context.Background(), tt.user)
			if err != nil {
				t.Fatalf("got err: %v", err)
			}
			if score != tt.wantScore {
				t.Errorf("incorrect user score. got %d, want %d", score, tt.wantScore)
			}
		})
	}
}

func TestRepCacheStorageListener(t *testing.T) {
	authMgr, storage := newTestAuthManager(t)
	if storage.repInputsListener == nil {
		t.Fatal("NewAuthManager did not register a reputation-inputs listener with storage")
	}

	user := testAcctID(0xbb)
	ctx := context.Background()
	var calls int32
	for i := 0; i < 2; i++ {
		if _, err := authMgr.rep.get(ctx, user, fixedRepFetcher(5, &calls)); err != nil {
			t.Fatalf("rep.get error: %v", err)
		}
	}
	if calls != 1 {
		t.Fatalf("fetches before notification = %d, want 1 (second get cached)", calls)
	}

	storage.notifyRepInputs(user)
	if _, err := authMgr.rep.get(ctx, user, fixedRepFetcher(5, &calls)); err != nil {
		t.Fatalf("rep.get after notification error: %v", err)
	}
	if calls != 2 {
		t.Fatalf("fetches after notification = %d, want 2 (entry invalidated)", calls)
	}
}

func TestLoadUserReputationAccountError(t *testing.T) {
	authMgr, storage := newTestAuthManager(t)
	user := testAcctID(5)

	storage.accountReadErr = errors.New("db down")
	if _, err := authMgr.loadUserReputation(context.Background(), user); err == nil {
		t.Fatal("expected an error from a failed account read")
	}

	// Failure must not have been cached as a nonexistent account.
	storage.accountReadErr = nil
	storage.acct = &account.Account{ID: user}
	storage.bonds = []*db.Bond{{Strength: 2, LockTime: time.Now().Add(48 * time.Hour).Unix()}}
	before := time.Now().Add(authMgr.bondExpiry).Unix()
	rep, err := authMgr.loadUserReputation(context.Background(), user)
	after := time.Now().Add(authMgr.bondExpiry).Unix()
	if err != nil {
		t.Fatalf("loadUserReputation error after recovery: %v", err)
	}
	if rep == nil {
		t.Fatal("user read as unknown after a transient account read error")
	}
	if rep.BondedTier != 2 {
		t.Fatalf("bonded tier = %d, want 2", rep.BondedTier)
	}
	if rep.BondExpiryThreshold < before || rep.BondExpiryThreshold > after {
		t.Fatalf("bond expiry threshold = %d, want between %d and %d", rep.BondExpiryThreshold, before, after)
	}
}

func TestLoadUserReputationCancellation(t *testing.T) {
	auth, storage := newTestAuthManager(t)
	blocking := &blockingAccountStorage{TStorage: storage, started: make(chan struct{})}
	auth.storage = blocking
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := auth.loadUserReputationWithTimeout(ctx, testAcctID(1))
		done <- err
	}()
	<-blocking.started
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("reputation error = %v, want cancellation", err)
		}
	case <-time.After(time.Second):
		t.Fatal("account read did not stop after cancellation")
	}
}

func TestUserReputationAt(t *testing.T) {
	authMgr, storage := newTestAuthManager(t)
	user := testAcctID(0xac)
	storage.acct = &account.Account{ID: user}
	// The bond is expired now but was active at the earlier time.
	lockTime := time.Now().Add(-time.Hour)
	storage.bonds = []*db.Bond{{Strength: 2, LockTime: lockTime.Unix()}}

	tier, _, _, err := authMgr.UserReputationAt(user, lockTime.Add(-authMgr.bondExpiry).Add(-time.Minute))
	if err != nil {
		t.Fatalf("UserReputationAt error: %v", err)
	}
	if tier != 2 {
		t.Fatalf("tier at pre-expiry as-of = %d, want 2", tier)
	}

	tier, _, _, err = authMgr.UserReputationAt(user, time.Now())
	if err != nil {
		t.Fatalf("UserReputationAt error: %v", err)
	}
	if tier != 0 {
		t.Fatalf("wall-clock tier = %d, want 0 for expired bond", tier)
	}
}

func TestAcctRepStatus(t *testing.T) {
	authMgr, storage := newTestAuthManager(t)
	user := testAcctID(0xaa)
	storage.acct = &account.Account{ID: user}
	storage.setBondTier(1)

	connected, rep, err := authMgr.AcctRepStatus(user)
	if err != nil {
		t.Fatalf("AcctRepStatus error: %v", err)
	}
	if connected {
		t.Fatal("reported connected with no client connection")
	}
	if rep == nil || rep.EffectiveTier() != 1 {
		t.Fatalf("rep = %+v, want effective tier 1", rep)
	}

	// A failed lookup must not be reported as tier 0.
	storage.reputationErr = errors.New("db down")
	authMgr.rep.invalidate(user)
	if _, rep, err = authMgr.AcctRepStatus(user); err == nil {
		t.Fatal("no error from failed score load")
	} else if rep != nil {
		t.Fatal("non-nil rep with load error")
	}

	// Connectivity is independent of the lookup result.
	authMgr.connMtx.Lock()
	authMgr.users[user] = &clientInfo{}
	authMgr.connMtx.Unlock()
	if connected, _, err = authMgr.AcctRepStatus(user); !connected || err == nil {
		t.Fatalf("AcctRepStatus on load error = (connected %v, err %v), want connected with error", connected, err)
	}
	authMgr.connMtx.Lock()
	delete(authMgr.users, user)
	authMgr.connMtx.Unlock()

	// Account-read failure is also an error.
	storage.reputationErr = nil
	storage.accountReadErr = errors.New("db down")
	if _, _, err = authMgr.AcctRepStatus(user); err == nil {
		t.Fatal("no error from failed account read")
	}

	// Unknown account: nil rep, nil error.
	storage.accountReadErr = nil
	storage.acct = nil
	authMgr.rep.invalidate(user)
	if _, rep, err = authMgr.AcctRepStatus(user); err != nil || rep != nil {
		t.Fatalf("unknown account: rep %+v, err %v", rep, err)
	}
}

func TestReputationInputsNotify(t *testing.T) {
	authMgr, storage := newTestAuthManager(t)
	user := tNewUser(t)
	storage.acct = &account.Account{ID: user.acctID, PubKey: user.privKey.PubKey()}
	storage.setBondTier(1)
	storage.reputationMatches = []*db.MatchResult{{
		DBID: 1, MatchID: randomMatchID(), MatchOutcome: db.OutcomeSwapSuccess,
	}}
	authMgr.signer.(*TSigner).setSig(user.randomSignature())
	authMgr.connMtx.Lock()
	authMgr.users[user.acctID] = &clientInfo{
		acct: &account.Account{ID: user.acctID},
		conn: user.conn,
	}
	authMgr.connMtx.Unlock()

	storage.notifyRepInputs(user.acctID)
	var msg *msgjson.Message
	deadline := time.Now().Add(time.Second)
	for msg == nil && time.Now().Before(deadline) {
		msg = user.conn.getSend()
		if msg == nil {
			time.Sleep(time.Millisecond)
		}
	}
	if msg == nil {
		t.Fatal("no score_changed notification")
	}
	if msg.Route != msgjson.ScoreChangeRoute {
		t.Fatalf("route = %s, want %s", msg.Route, msgjson.ScoreChangeRoute)
	}
	note := new(msgjson.ScoreChangedNotification)
	if err := msg.Unmarshal(note); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if note.Reputation.Score != 1 {
		t.Fatalf("score = %d, want 1", note.Reputation.Score)
	}
	if note.Reputation.BondExpiryThreshold == 0 {
		t.Fatal("missing bond expiry threshold")
	}
}

func TestConnect(t *testing.T) {
	user := tNewUser(t)
	rig.signer.setSig(user.randomSignature())

	// Before connecting, put an activeOrder and activeMatch in storage.
	matchData, userMatch := userMatchData(user.acctID)
	matchTime := matchData.Epoch.End()

	rig.storage.orderStatuses = []*db.OrderStatus{
		{
			ID:     userMatch.OrderID,
			Status: order.OrderStatusBooked,
		},
	}
	defer func() { rig.storage.orderStatuses = nil }()

	rig.storage.matches = []*db.MatchData{matchData}
	defer func() { rig.storage.matches = nil }()

	epochGaps := []int32{1} // penalized

	rig.storage.setRatioData(&ratioData{
		oidsCompleted:  []order.OrderID{{0x1}},
		timesCompleted: []int64{1234},
		oidsCancels:    []order.OrderID{{0x2}},
		oidsCanceled:   []order.OrderID{{0x1}},
		timesCanceled:  []int64{1235},
		epochGaps:      epochGaps,
	}) // 1:1 = 50%
	defer rig.storage.setRatioData(&ratioData{}) // clean slate

	// TODO: update tests now that there is are no close/ban and unban
	// operations, instead an integral tier.

	// TODO: update tests now that cancel ratio is part of the score equation
	// rather than a hard close operation.

	/* cancel ratio stuff

	// Close account on connect with failing cancel ratio, and no grace period.
	rig.mgr.cancelThresh = 0.2 // thresh below actual ratio, and no grace period with total/(1+total) = 2/3 = 0.66... > 0.2
	tryConnectUser(t, user, false)
	if rig.storage.closedID != user.acctID {
		t.Fatalf("Expected account %v to be closed on connect, got %v", user.acctID, rig.storage.closedID)
	}

	// Make it a free cancel.
	rig.storage.closedID = account.AccountID{} // unclose the account in db
	epochGaps[0] = 2
	connectUser(t, user)
	if rig.storage.closedID == user.acctID {
		t.Fatalf("Expected account %v to NOT be closed with free cancels, but it was.", user)
	}
	epochGaps[0] = 1

	// Try again just meeting cancel ratio.
	rig.storage.closedID = account.AccountID{} // unclose the account in db
	rig.mgr.cancelThresh = 0.6                 // passable threshold for 1 cancel : 1 completion (0.5)

	connectUser(t, user)
	if rig.storage.closedID == user.acctID {
		t.Fatalf("Expected account %v to NOT be closed on connect, but it was.", user)
	}

	// Add another cancel, bringing cancels to 2, completions 1 for a ratio of
	// 2:1 (2/3 = 0.666...), and total/(1+total) = 3/4 = 0.75 > thresh (0.6), so
	// no grace period.
	rig.storage.ratio.oidsCanceled = append(rig.storage.ratio.oidsCanceled, order.OrderID{0x3})
	rig.storage.ratio.oidsCancels = append(rig.storage.ratio.oidsCancels, order.OrderID{0x4})
	rig.storage.ratio.timesCanceled = append(rig.storage.ratio.timesCanceled, 12341234)
	rig.storage.ratio.epochGaps = append(rig.storage.ratio.epochGaps, 1)

	tryConnectUser(t, user, false)
	if rig.storage.closedID != user.acctID {
		t.Fatalf("Expected account %v to be closed on connect, got %v", user.acctID, rig.storage.closedID)
	}

	// Make one a free cancel.
	rig.storage.closedID = account.AccountID{} // unclose the account in db
	rig.storage.ratio.epochGaps[1] = 2
	connectUser(t, user)
	if rig.storage.closedID == user.acctID {
		t.Fatalf("Expected account %v to NOT be closed with free cancels, but it was.", user)
	}
	rig.storage.ratio.epochGaps[1] = 0

	// Try again just meeting cancel ratio.
	rig.storage.closedID = account.AccountID{} // unclose the account in db
	rig.mgr.cancelThresh = 0.7                 // passable threshold for 2 cancel : 1 completion (0.6666..)

	tryConnectUser(t, user, false)
	if rig.storage.closedID == user.acctID {
		t.Fatalf("Expected account %v to NOT be closed on connect, but it was.", user)
	}

	// Test the grace period (threshold <= total/(1+total) and no completions)
	// 2 cancels, 0 success, 2 total
	rig.mgr.cancelThresh = 0.7             // 2/(1+2) = 0.66.. < threshold
	rig.storage.ratio.timesCompleted = nil // no completions
	rig.storage.ratio.oidsCompleted = nil
	tryConnectUser(t, user, false)
	if rig.storage.closedID == user.acctID {
		t.Fatalf("Expected account %v to NOT be closed on connect, but it was.", user)
	}

	// 3 cancels, 0 success, 3 total => rate = 1.0, exceeds threshold
	rig.mgr.cancelThresh = 0.75 // 3/(1+3) == threshold, still in grace period
	rig.storage.ratio.oidsCanceled = append(rig.storage.ratio.oidsCanceled, order.OrderID{0x4})
	rig.storage.ratio.oidsCancels = append(rig.storage.ratio.oidsCancels, order.OrderID{0x5})
	rig.storage.ratio.timesCanceled = append(rig.storage.ratio.timesCanceled, 12341239)
	rig.storage.ratio.epochGaps = append(rig.storage.ratio.epochGaps, 1)

	tryConnectUser(t, user, false)
	if rig.storage.closedID == user.acctID {
		t.Fatalf("Expected account %v to NOT be closed on connect, but it was.", user)
	}

	*/

	// Connect with a violation score above revocation threshold.
	wantScore := setViolations()
	defer clearViolations()

	if wantScore > rig.mgr.penaltyThreshold {
		t.Fatalf("test score of %v is not at least the revocation threshold of %v, revise the test", wantScore, rig.mgr.penaltyThreshold)
	}

	// Test loadUserScore while here.
	_, err := rig.mgr.loadUserScoreContext(context.Background(), user.acctID)
	if err != nil {
		t.Fatal(err)
	}

	// if score != wantScore {
	// 	t.Errorf("wrong score. got %d, want %d", score, wantScore)
	// }

	// No error, but Penalize account that was not previously closed.
	tryConnectUser(t, user, false)

	makerSwapCastIdx := 3
	rig.storage.reputationMatches = append(rig.storage.reputationMatches[:makerSwapCastIdx], rig.storage.reputationMatches[makerSwapCastIdx+1:]...)
	wantScore -= noSwapAsTakerScore
	if wantScore <= rig.mgr.penaltyThreshold {
		t.Fatalf("test score of %v is not more than the penalty threshold of %v, revise the test", wantScore, rig.mgr.penaltyThreshold)
	}
	_, err = rig.mgr.loadUserScoreContext(context.Background(), user.acctID)
	if err != nil {
		t.Fatal(err)
	}
	// if score != wantScore {
	// 	t.Errorf("wrong score. got %d, want %d", score, wantScore)
	// }

	// Connect the user.
	before := time.Now().Add(rig.mgr.bondExpiry).Unix()
	respMsg := connectUser(t, user)
	after := time.Now().Add(rig.mgr.bondExpiry).Unix()
	cResp := extractConnectResult(t, respMsg)
	if cResp.Reputation.BondExpiryThreshold < before || cResp.Reputation.BondExpiryThreshold > after {
		t.Fatalf("bond expiry threshold = %d, want between %d and %d", cResp.Reputation.BondExpiryThreshold, before, after)
	}
	if len(cResp.ActiveOrderStatuses) != 1 {
		t.Fatalf("no active orders")
	}
	msgOrder := cResp.ActiveOrderStatuses[0]
	if msgOrder.ID.String() != userMatch.OrderID.String() {
		t.Fatal("active order ID mismatch: ", msgOrder.ID.String(), " != ", userMatch.OrderID.String())
	}
	if msgOrder.Status != uint16(order.OrderStatusBooked) {
		t.Fatal("active order Status mismatch: ", msgOrder.Status, " != ", order.OrderStatusBooked)
	}
	if len(cResp.ActiveMatches) != 1 {
		t.Fatalf("no active matches")
	}
	msgMatch := cResp.ActiveMatches[0]
	if msgMatch.OrderID.String() != userMatch.OrderID.String() {
		t.Fatal("active match OrderID mismatch: ", msgMatch.OrderID.String(), " != ", userMatch.OrderID.String())
	}
	if msgMatch.MatchID.String() != userMatch.MatchID.String() {
		t.Fatal("active match MatchID mismatch: ", msgMatch.MatchID.String(), " != ", userMatch.MatchID.String())
	}
	if msgMatch.Quantity != userMatch.Quantity {
		t.Fatal("active match Quantity mismatch: ", msgMatch.Quantity, " != ", userMatch.Quantity)
	}
	if msgMatch.Rate != userMatch.Rate {
		t.Fatal("active match Rate mismatch: ", msgMatch.Rate, " != ", userMatch.Rate)
	}
	if msgMatch.Address != userMatch.Address {
		t.Fatal("active match Address mismatch: ", msgMatch.Address, " != ", userMatch.Address)
	}
	if msgMatch.Status != uint8(userMatch.Status) {
		t.Fatal("active match Status mismatch: ", msgMatch.Status, " != ", userMatch.Status)
	}
	if msgMatch.Side != uint8(userMatch.Side) {
		t.Fatal("active match Side mismatch: ", msgMatch.Side, " != ", userMatch.Side)
	}
	if msgMatch.FeeRateQuote != matchData.QuoteRate {
		t.Fatal("active match quote fee rate mismatch: ", msgMatch.FeeRateQuote, " != ", matchData.QuoteRate)
	}
	if msgMatch.FeeRateBase != matchData.BaseRate {
		t.Fatal("active match base fee rate mismatch: ", msgMatch.FeeRateBase, " != ", matchData.BaseRate)
	}
	if msgMatch.ServerTime != uint64(matchTime.UnixMilli()) {
		t.Fatal("active match time mismatch: ", msgMatch.ServerTime, " != ", uint64(matchTime.UnixMilli()))
	}

	// Send a request to the client.
	type tPayload struct {
		A int
	}
	a5 := &tPayload{A: 5}
	reqID := comms.NextID()
	msg, err := msgjson.NewRequest(reqID, "request", a5)
	if err != nil {
		t.Fatalf("NewRequest error: %v", err)
	}
	var responded bool
	rig.mgr.Request(user.acctID, msg, func(comms.Link, *msgjson.Message) {
		responded = true
	})
	req := user.conn.getReq()
	if req == nil {
		t.Fatalf("no request")
	}
	var a tPayload
	err = json.Unmarshal(req.msg.Payload, &a)
	if err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if a.A != 5 {
		t.Fatalf("wrong value for A. expected 5, got %d", a.A)
	}
	// Respond to the DEX's request.
	msg = &msgjson.Message{ID: reqID}
	req.respFunc(user.conn, msg)
	if !responded {
		t.Fatalf("responded flag not set")
	}

	reuser := &tUser{
		acctID:  user.acctID,
		privKey: user.privKey,
		conn:    tNewRPCClient(),
	}
	connectUser(t, reuser)
	a10 := &tPayload{A: 10}
	msg, _ = msgjson.NewRequest(comms.NextID(), "request", a10)
	err = rig.mgr.RequestWithTimeout(reuser.acctID, msg, func(comms.Link, *msgjson.Message) {}, time.Minute, func() {})
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	// The a10 message should be in the new connection
	if user.conn.getReq() != nil {
		t.Fatalf("old connection received a request after reconnection")
	}
	if reuser.conn.getReq() == nil {
		t.Fatalf("new connection did not receive the request")
	}
}

func TestAccountErrors(t *testing.T) {
	user := tNewUser(t)
	rig.signer.setSig(user.randomSignature())
	connect := queueUser(t, user)

	// Put a match in storage
	matchData, userMatch := userMatchData(user.acctID)
	matchTime := matchData.Epoch.End()
	rig.storage.matches = []*db.MatchData{matchData}

	rig.mgr.handleConnect(user.conn, connect)
	rig.storage.matches = nil

	// Check the response.
	respMsg := user.conn.getSend()
	result := extractConnectResult(t, respMsg)
	if len(result.ActiveMatches) != 1 {
		t.Fatalf("expected 1 match, received %d", len(result.ActiveMatches))
	}
	match := result.ActiveMatches[0]
	if match.OrderID.String() != userMatch.OrderID.String() {
		t.Fatal("wrong OrderID: ", match.OrderID, " != ", userMatch.OrderID)
	}
	if match.MatchID.String() != userMatch.MatchID.String() {
		t.Fatal("wrong MatchID: ", match.MatchID, " != ", userMatch.OrderID)
	}
	if match.Quantity != userMatch.Quantity {
		t.Fatal("wrong Quantity: ", match.Quantity, " != ", userMatch.OrderID)
	}
	if match.Rate != userMatch.Rate {
		t.Fatal("wrong Rate: ", match.Rate, " != ", userMatch.OrderID)
	}
	if match.Address != userMatch.Address {
		t.Fatal("wrong Address: ", match.Address, " != ", userMatch.OrderID)
	}
	if match.Status != uint8(userMatch.Status) {
		t.Fatal("wrong Status: ", match.Status, " != ", userMatch.OrderID)
	}
	if match.Side != uint8(userMatch.Side) {
		t.Fatal("wrong Side: ", match.Side, " != ", userMatch.OrderID)
	}
	if match.FeeRateQuote != matchData.QuoteRate {
		t.Fatal("wrong quote fee rate: ", match.FeeRateQuote, " != ", matchData.QuoteRate)
	}
	if match.FeeRateBase != matchData.BaseRate {
		t.Fatal("wrong base fee rate: ", match.FeeRateBase, " != ", matchData.BaseRate)
	}
	if match.ServerTime != uint64(matchTime.UnixMilli()) {
		t.Fatal("wrong match time: ", match.ServerTime, " != ", uint64(matchTime.UnixMilli()))
	}
	// Make a violation score above penalty threshold reflected by the DB.
	score := setViolations()
	defer clearViolations()

	rig.mgr.removeClient(rig.mgr.user(user.acctID)) // disconnect first, NOTE that link.Disconnect is async
	user.conn = tNewRPCClient()                     // disconnect necessitates new conn ID
	rpcErr := rig.mgr.handleConnect(user.conn, connect)
	if rpcErr != nil {
		t.Fatalf("should be no error for closed account")
	}
	client := rig.mgr.user(user.acctID)
	rig.storage.setBondTier(1)
	if client == nil {
		t.Fatalf("client not found")
	}
	initPenaltyThresh := rig.mgr.penaltyThreshold
	defer func() { rig.mgr.penaltyThreshold = initPenaltyThresh }()
	rig.mgr.penaltyThreshold = score
	if _, tier := rig.mgr.AcctStatus(user.acctID); tier > 0 {
		t.Errorf("client should have been tier 0")
	}

	// Raise the penalty threshold to ensure automatic reinstatement.
	rig.mgr.penaltyThreshold = score - 1

	rig.mgr.removeClient(rig.mgr.user(user.acctID)) // disconnect first, NOTE that link.Disconnect is async
	user.conn = tNewRPCClient()                     // disconnect necessitates new conn ID
	rpcErr = rig.mgr.handleConnect(user.conn, connect)
	if rpcErr != nil {
		t.Fatalf("should be no error for closed account")
	}
	client = rig.mgr.user(user.acctID)
	if client == nil {
		t.Fatalf("client not found")
	}
	if _, tier := rig.mgr.AcctStatus(user.acctID); tier < 1 {
		t.Errorf("client should have unbanned automatically")
	}

}

func TestRoute(t *testing.T) {
	user := tNewUser(t)
	rig.signer.setSig(user.randomSignature())
	connectUser(t, user)

	var translated account.AccountID
	rig.mgr.Route("testroute", func(id account.AccountID, msg *msgjson.Message) *msgjson.Error {
		translated = id
		return nil
	})
	f := tRoutes["testroute"]
	if f == nil {
		t.Fatalf("'testroute' not registered")
	}
	rpcErr := f(user.conn, nil)
	if rpcErr != nil {
		t.Fatalf("rpc error: %s", rpcErr.Message)
	}
	if translated != user.acctID {
		t.Fatalf("account ID not set")
	}

	// Run the route with an unknown client. Should be an UnauthorizedConnection
	// error.
	foreigner := tNewUser(t)
	rpcErr = f(foreigner.conn, nil)
	if rpcErr == nil {
		t.Fatalf("no error for unauthed user")
	}
	if rpcErr.Code != msgjson.UnauthorizedConnection {
		t.Fatalf("wrong error for unauthed user. expected %d, got %d",
			msgjson.UnauthorizedConnection, rpcErr.Code)
	}
}

func TestRequestWithTimeoutProxiesThroughMesh(t *testing.T) {
	auth, _ := newTestAuthManager(t)
	user := tNewUser(t)
	req, err := msgjson.NewRequest(comms.NextID(), msgjson.PreimageRoute, map[string]string{"market": "dcr_btc"})
	if err != nil {
		t.Fatal(err)
	}
	req.Sig = randBytes(16)
	id, wantRequest := req.ID, req.String()
	resp, err := msgjson.NewResponse(id, map[string]string{"status": "ok"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp.Sig = randBytes(16)
	wantResponse := resp.String()
	proxyReady := make(chan struct{}, 1)
	peer := &tMesh{proxyReady: proxyReady}
	auth.SetMeshService(peer)
	t.Cleanup(func() { auth.takeProxyRespHandler(user.acctID, id) })

	got := make(chan *msgjson.Message, 1)
	expired := make(chan struct{}, 1)
	err = auth.RequestWithTimeout(user.acctID, req, func(conn comms.Link, msg *msgjson.Message) {
		if conn != nil {
			t.Errorf("expected nil proxied conn, got %T", conn)
		}
		got <- msg
	}, time.Minute, func() { expired <- struct{}{} })
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-proxyReady:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for proxied request")
	}

	if peer.proxiedMsg.String() != wantRequest {
		t.Fatalf("proxied request = %s, want %s", peer.proxiedMsg, wantRequest)
	}
	if peer.proxyCount != 1 || peer.proxiedUser != user.acctID {
		t.Fatalf("proxy count/user = %d/%v", peer.proxyCount, peer.proxiedUser)
	}
	if peer.proxiedTimeout != time.Minute {
		t.Fatalf("proxied timeout = %v, want %v", peer.proxiedTimeout, time.Minute)
	}

	if err := auth.HandleProxiedClientMessage(context.Background(), &mesh.ClientProxyMessage{User: user.acctID, Msg: resp}); err != nil {
		t.Fatal(err)
	}
	select {
	case msg := <-got:
		if msg.String() != wantResponse {
			t.Fatalf("callback response = %s, want %s", msg, wantResponse)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for proxied callback")
	}
	select {
	case <-expired:
		t.Fatal("unexpected expire callback")
	default:
	}
}

func TestRequestPeerFailureAndTimeout(t *testing.T) {
	for _, tt := range []struct {
		name    string
		err     error
		timeout time.Duration
	}{
		{"relay failure", errors.New("proxy failed"), time.Minute},
		{"response timeout", nil, time.Millisecond},
	} {
		t.Run(tt.name, func(t *testing.T) {
			auth, _ := newTestAuthManager(t)
			user := tNewUser(t)
			auth.SetMeshService(&tMesh{proxiedErr: tt.err})
			req, err := msgjson.NewRequest(comms.NextID(), msgjson.PreimageRoute, nil)
			if err != nil {
				t.Fatal(err)
			}
			responded := make(chan struct{}, 1)
			expired := make(chan struct{}, 2)
			err = auth.RequestWithTimeout(user.acctID, req, func(comms.Link, *msgjson.Message) { responded <- struct{}{} }, tt.timeout, func() { expired <- struct{}{} })
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { auth.takeProxyRespHandler(user.acctID, req.ID) })
			select {
			case <-expired:
			case <-responded:
				t.Fatal("unexpected response callback")
			case <-time.After(time.Second):
				t.Fatal("request did not expire")
			}
			if tt.err == nil {
				// A late response must not reach a client that has since connected here.
				auth.users[user.acctID] = &clientInfo{acct: &account.Account{ID: user.acctID}, conn: user.conn}
				late, err := msgjson.NewResponse(req.ID, nil, nil)
				if err != nil {
					t.Fatal(err)
				}
				if err = auth.HandleProxiedClientMessage(context.Background(), &mesh.ClientProxyMessage{User: user.acctID, Msg: late}); err != nil {
					t.Fatal(err)
				}
				if user.conn.getSend() != nil {
					t.Fatal("late response delivered to client")
				}
			}
			if auth.takeProxyRespHandler(user.acctID, req.ID) != nil {
				t.Fatal("expired handler remains registered")
			}
			select {
			case <-responded:
				t.Fatal("unexpected response callback")
			case <-expired:
				t.Fatal("request expired twice")
			default:
			}
		})
	}
}

func TestProxyResponseHandlerReplacement(t *testing.T) {
	auth, _ := newTestAuthManager(t)
	user := testAcctID(1)
	id := comms.NextID()
	expired := make(chan string, 3)
	old := auth.registerProxyRespHandler(user, id, func(comms.Link, *msgjson.Message) {}, time.Hour, func() { expired <- "old" })
	auth.registerProxyRespHandler(user, id, func(comms.Link, *msgjson.Message) {}, 100*time.Millisecond, func() { expired <- "replacement" })
	t.Cleanup(func() { old.expire.Stop(); auth.takeProxyRespHandler(user, id) })
	if old.expire.Stop() {
		t.Fatal("replacement did not stop the old timer")
	}
	// Force the old timer's real callback to run, as if it had already been
	// queued when the replacement stopped it.
	old.expire.Reset(0)
	select {
	case name := <-expired:
		if name != "replacement" {
			t.Fatalf("%s request expired, want replacement", name)
		}
	case <-time.After(time.Second):
		t.Fatal("replacement request did not expire")
	}
	if auth.takeProxyRespHandler(user, id) != nil {
		t.Fatal("expired handler remains registered")
	}
	select {
	case name := <-expired:
		t.Fatalf("unexpected additional expiry: %s", name)
	default:
	}
}

func TestRequestPeerLateFailure(t *testing.T) {
	auth, _ := newTestAuthManager(t)
	user := testAcctID(1)
	req, err := msgjson.NewRequest(comms.NextID(), msgjson.PreimageRoute, nil)
	if err != nil {
		t.Fatal(err)
	}
	release := make(chan struct{})
	var releaseOnce sync.Once
	releaseRelay := func() { releaseOnce.Do(func() { close(release) }) }
	t.Cleanup(releaseRelay)
	relayed := make(chan struct{}, 1)
	auth.SetMeshService(&tMesh{proxyWait: release, proxyReady: relayed, proxiedErr: errors.New("late relay failure")})
	completed := make(chan string, 4)
	if err = auth.RequestWithTimeout(user, req, func(comms.Link, *msgjson.Message) { completed <- "response" }, time.Minute, func() { completed <- "timeout" }); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { auth.takeProxyRespHandler(user, req.ID) })
	resp, err := msgjson.NewResponse(req.ID, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = auth.HandleProxiedClientMessage(context.Background(), &mesh.ClientProxyMessage{User: user, Msg: resp}); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-completed:
		if got != "response" {
			t.Fatalf("completion = %s, want response", got)
		}
	case <-time.After(time.Second):
		t.Fatal("original request did not complete")
	}
	// Reuse the entry only after the original request has completed. Its
	// blocked relay must not expire this new registration when it fails.
	auth.SetMeshService(&tMesh{})
	if err = auth.RequestWithTimeout(user, req, func(comms.Link, *msgjson.Message) { completed <- "replacement response" }, 100*time.Millisecond, func() { completed <- "replacement timeout" }); err != nil {
		t.Fatal(err)
	}
	releaseRelay()
	select {
	case <-relayed:
	case <-time.After(time.Second):
		t.Fatal("blocked relay did not return")
	}
	select {
	case got := <-completed:
		if got != "replacement timeout" {
			t.Fatalf("late failure caused %s", got)
		}
	case <-time.After(time.Second):
		t.Fatal("replacement request did not expire")
	}
	select {
	case got := <-completed:
		t.Fatalf("unexpected extra callback: %s", got)
	default:
	}
}

func TestProxyClientMessageResponseDelivery(t *testing.T) {
	for _, tt := range []struct {
		name           string
		pendingRequest bool
		sendErr        error
	}{
		{name: "matching server request ID", pendingRequest: true},
		{name: "send failure", sendErr: errors.New("send failed")},
	} {
		t.Run(tt.name, func(t *testing.T) {
			auth, _ := newTestAuthManager(t)
			user := tNewUser(t)
			client := &clientInfo{acct: &account.Account{ID: user.acctID}, conn: user.conn}
			auth.users[user.acctID] = client
			auth.conns[user.conn.ID()] = client
			resp, err := msgjson.NewResponse(comms.NextID(), map[string]string{"status": "ok"}, nil)
			if err != nil {
				t.Fatal(err)
			}
			var handler *respHandler
			if tt.pendingRequest {
				handler = auth.registerProxyRespHandler(user.acctID, resp.ID, func(comms.Link, *msgjson.Message) {}, time.Hour, func() {})
				t.Cleanup(func() { auth.takeProxyRespHandler(user.acctID, resp.ID) })
			}
			user.conn.sendErr = tt.sendErr
			err = auth.HandleProxiedClientMessage(context.Background(), &mesh.ClientProxyMessage{User: user.acctID, Msg: resp, DeliverToClient: true})
			if !errors.Is(err, tt.sendErr) {
				t.Fatalf("delivery error = %v, want %v", err, tt.sendErr)
			}
			if tt.sendErr != nil {
				if auth.user(user.acctID) != nil {
					t.Fatal("failed connection remains registered")
				}
				return
			}
			sent := user.conn.getSend()
			if sent == nil || sent.ID != resp.ID {
				t.Fatalf("delivered message = %v, want response %d", sent, resp.ID)
			}
			if handler != nil && auth.takeProxyRespHandler(user.acctID, resp.ID) != handler {
				t.Fatal("client response consumed server-request handler")
			}
		})
	}
}

func TestProxyClientMessageRequest(t *testing.T) {
	auth, _ := newTestAuthManager(t)
	user := tNewUser(t)
	client := &clientInfo{acct: &account.Account{ID: user.acctID}, conn: user.conn, respHandlers: make(map[uint64]*respHandler)}
	auth.users[user.acctID] = client
	auth.conns[user.conn.ID()] = client

	req, err := msgjson.NewRequest(comms.NextID(), msgjson.PreimageRoute, map[string]string{"market": "dcr_btc"})
	if err != nil {
		t.Fatalf("NewRequest error: %v", err)
	}

	proxyReady := make(chan struct{}, 1)
	peer := &tMesh{proxyReady: proxyReady}
	auth.SetMeshService(peer)

	err = auth.HandleProxiedClientMessage(context.Background(), &mesh.ClientProxyMessage{
		User:      user.acctID,
		Msg:       req,
		TimeoutMS: uint64(time.Minute / time.Millisecond),
	})
	if err != nil {
		t.Fatalf("ProxyClientMessage request error: %v", err)
	}

	localReq := user.conn.getReq()
	if localReq == nil {
		t.Fatal("no local proxied request sent")
	}
	t.Cleanup(func() { client.respHandler(localReq.msg.ID) })
	if localReq.msg.ID == req.ID {
		t.Fatalf("proxied local request id was not rewritten")
	}
	wantRequest := *req
	wantRequest.ID = localReq.msg.ID
	if localReq.msg.String() != wantRequest.String() {
		t.Fatalf("local request = %s, want %s", localReq.msg, &wantRequest)
	}

	localResp, err := msgjson.NewResponse(localReq.msg.ID, map[string]string{"status": "ok"}, nil)
	if err != nil {
		t.Fatalf("NewResponse error: %v", err)
	}
	localResp.Sig = randBytes(16)
	wantResponse := *localResp
	wantResponse.ID = req.ID
	wantResponseJSON := wantResponse.String()
	localReq.respFunc(user.conn, localResp)
	if localResp.ID != localReq.msg.ID {
		t.Fatal("forwarding the response changed its original ID")
	}

	select {
	case <-proxyReady:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for proxied client response")
	}

	if peer.proxiedMsg.String() != wantResponseJSON {
		t.Fatalf("relayed response = %s, want %s", peer.proxiedMsg, wantResponseJSON)
	}
	if peer.proxiedDeliver {
		t.Fatal("client response marked for client delivery")
	}
	if peer.proxyCount != 1 {
		t.Fatalf("proxy count = %d, want 1", peer.proxyCount)
	}
	if peer.proxiedUser != user.acctID {
		t.Fatalf("proxied response user = %v, want %v", peer.proxiedUser, user.acctID)
	}
}

func TestSendPeer(t *testing.T) {
	relayErr := errors.New("relay failed")
	for _, tt := range []struct {
		name    string
		err     error
		wantErr error
	}{
		{"delivery", nil, nil},
		{"no mesh peer", mesh.ErrClientProxyUnavailable, ErrUserNotConnected},
		{"peer disconnected", fmt.Errorf("%w to peer", mesh.ErrClientNotConnected), ErrUserNotConnected},
		{"relay error", relayErr, relayErr},
	} {
		t.Run(tt.name, func(t *testing.T) {
			auth, _ := newTestAuthManager(t)
			user := tNewUser(t)
			resp, err := msgjson.NewResponse(comms.NextID(), map[string]string{"status": "ok"}, nil)
			if err != nil {
				t.Fatal(err)
			}
			peer := &tMesh{proxiedErr: tt.err}
			auth.SetMeshService(peer)
			if err = auth.Send(user.acctID, resp); !errors.Is(err, tt.wantErr) {
				t.Fatalf("Send error = %v, want %v", err, tt.wantErr)
			}
			if tt.wantErr != nil {
				return
			}
			if peer.proxyCount != 1 || peer.proxiedUser != user.acctID {
				t.Fatalf("proxy count/user = %d/%v", peer.proxyCount, peer.proxiedUser)
			}
			if peer.proxiedMsg.String() != resp.String() {
				t.Fatalf("proxied response = %s, want %s", peer.proxiedMsg, resp)
			}
			if !peer.proxiedDeliver {
				t.Fatal("response not marked for client delivery")
			}
		})
	}
}

func TestLocalClientMessages(t *testing.T) {
	for _, connected := range []bool{false, true} {
		t.Run(fmt.Sprintf("connected=%v", connected), func(t *testing.T) {
			auth, _ := newTestAuthManager(t)
			user := tNewUser(t)
			peer := &tMesh{}
			auth.SetMeshService(peer)
			if connected {
				client := &clientInfo{acct: &account.Account{ID: user.acctID}, conn: user.conn, respHandlers: make(map[uint64]*respHandler)}
				auth.users[user.acctID] = client
				auth.conns[user.conn.ID()] = client
			}
			resp, err := msgjson.NewResponse(comms.NextID(), nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			if err = auth.SendIfLocal(user.acctID, resp); err != nil {
				t.Fatal(err)
			}
			if (user.conn.getSend() != nil) != connected {
				t.Fatal("incorrect local send result")
			}
			req, err := msgjson.NewRequest(comms.NextID(), msgjson.PreimageRoute, nil)
			if err != nil {
				t.Fatal(err)
			}
			if err = auth.RequestIfLocal(user.acctID, req, func(comms.Link, *msgjson.Message) {}); err != nil {
				t.Fatal(err)
			}
			if (user.conn.getReq() != nil) != connected {
				t.Fatal("incorrect local request result")
			}
			if connected {
				client := auth.user(user.acctID)
				client.respHandler(req.ID)
				for _, timeout := range []time.Duration{0, -time.Second} {
					if err = auth.RequestWithTimeout(user.acctID, req, func(comms.Link, *msgjson.Message) {}, timeout, func() {}); err != nil {
						t.Fatal(err)
					}
					client.respHandler(req.ID)
					got := user.conn.getReq()
					if got == nil || got.timeout != DefaultRequestTimeout {
						t.Fatalf("local request with timeout %v = %+v, want default timeout", timeout, got)
					}
				}
			}
			if peer.proxyCount != 0 {
				t.Fatal("local-only delivery used mesh")
			}
		})
	}
}

func TestHandleProxiedClientMessageDisconnectedUser(t *testing.T) {
	auth, _ := newTestAuthManager(t)
	user := tNewUser(t)
	for _, kind := range []msgjson.MessageType{msgjson.Request, msgjson.Response, msgjson.Notification} {
		t.Run(fmt.Sprintf("type=%d", kind), func(t *testing.T) {
			msg := &msgjson.Message{Type: kind, ID: comms.NextID(), Route: msgjson.PreimageRoute, Payload: json.RawMessage(`{}`)}
			err := auth.HandleProxiedClientMessage(context.Background(), &mesh.ClientProxyMessage{User: user.acctID, Msg: msg, DeliverToClient: true})
			if !errors.Is(err, mesh.ErrClientNotConnected) {
				t.Fatalf("delivery error = %v, want ErrClientNotConnected", err)
			}
		})
	}
}

func TestHandlePostBondSubmitsCommand(t *testing.T) {
	const assetID = 42
	coinID := []byte{0x01, 0x02, 0x03}
	user := tNewUser(t)
	meshReq := &tMesh{}
	setTestMeshService(t, meshReq)

	msg, _ := tNewPostBondRequest(t, user, assetID, coinID)

	rpcErr := rig.mgr.handlePostBond(user.conn, msg)
	if rpcErr != nil {
		t.Fatalf("handlePostBond error: %v", rpcErr)
	}
	if meshReq.executeCount != 1 {
		t.Fatalf("wrong execute count. got %d, want 1", meshReq.executeCount)
	}
	if meshReq.executedReq.User != user.acctID {
		t.Fatalf("wrong command user. got %v, want %v", meshReq.executedReq.User, user.acctID)
	}
	if meshReq.executedReq.Msg != msg {
		t.Fatalf("executed wrong message")
	}
	if meshReq.executedReq.Kind != commandKindPostBond {
		t.Fatalf("wrong command kind. got %q, want %q", meshReq.executedReq.Kind, commandKindPostBond)
	}
	if user.conn.sendCount() != 0 {
		t.Fatalf("unexpected immediate postbond response")
	}

	resp, err := msgjson.NewResponse(msg.ID, &msgjson.PostBondResult{
		AccountID: user.acctID[:],
		AssetID:   assetID,
		BondID:    coinID,
		Strength:  2,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := meshReq.executedReq.Respond(resp); err != nil {
		t.Fatal(err)
	}
	if sent := user.conn.getSend(); sent != resp {
		t.Fatalf("delivered response = %v, want %v", sent, resp)
	}
}

func decodePostBondResult(t *testing.T, msg *msgjson.Message) *msgjson.PostBondResult {
	t.Helper()
	resp, err := msg.Response()
	if err != nil {
		t.Fatalf("postbond response decode: %v", err)
	}
	if resp.Error != nil {
		t.Fatalf("postbond response error: %v", resp.Error)
	}
	var result msgjson.PostBondResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatalf("postbond result decode: %v", err)
	}
	return &result
}

func TestCreatePrepaidBonds(t *testing.T) {
	for _, tt := range []struct {
		name       string
		storageErr error
	}{
		{name: "emits event and returns token IDs"},
		{name: "storage error", storageErr: errors.New("store prepaid bonds failed")},
	} {
		t.Run(tt.name, func(t *testing.T) {
			authMgr, storage := newTestAuthManager(t)
			storage.prepaidBondsCreatedErr = tt.storageErr
			if tt.storageErr == nil {
				storage.prepaidBondsCreatedLog = &db.EventLogEntry{
					Seq:  1,
					Kind: meshevents.EventKindPrepaidBondsCreated,
				}
			}
			meshSvc, err := mesh.NewService(&mesh.ServiceConfig{
				EventLogReader: emptyEventLogReader{},
				OnHalt:         func(error) {},
				Commands:       authMgr.Commands(),
				Events:         authMgr.Events(),
				Logger:         dex.Disabled,
			})
			if err != nil {
				t.Fatalf("NewService error: %v", err)
			}
			authMgr.SetMeshService(meshSvc)

			const duration = 48 * time.Hour
			minLockTime := time.Now().Add(authMgr.bondExpiry + duration).Unix()
			coinIDs, err := authMgr.CreatePrepaidBonds(2, 3, int64(duration.Seconds()))
			maxLockTime := time.Now().Add(authMgr.bondExpiry + duration).Unix()
			event := storage.prepaidBondsCreatedUpdate
			if event == nil || len(event.Bonds) != 2 {
				t.Fatalf("storage event = %+v, want two prepaid bonds", event)
			}
			if tt.storageErr != nil {
				var rpcErr *msgjson.Error
				if !errors.As(err, &rpcErr) || rpcErr.Code != msgjson.RPCInternalError || coinIDs != nil {
					t.Fatalf("coin IDs = %x, error = %v, want no IDs and an internal error", coinIDs, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("CreatePrepaidBonds error: %v", err)
			}
			if len(coinIDs) != 2 {
				t.Fatalf("created %d IDs, want 2", len(coinIDs))
			}
			for i, bond := range event.Bonds {
				if len(coinIDs[i]) != prepaidBondIDLength {
					t.Fatalf("pre-paid bond ID length = %d, want %d", len(coinIDs[i]), prepaidBondIDLength)
				}
				if !bytes.Equal(bond.CoinID, coinIDs[i]) {
					t.Fatalf("event coin ID = %x, returned %x", bond.CoinID, coinIDs[i])
				}
				if bond.Strength != 3 {
					t.Fatalf("event strength = %d, want 3", bond.Strength)
				}
				if bond.LockTime < minLockTime || bond.LockTime > maxLockTime {
					t.Fatalf("event lock time = %d, want between %d and %d", bond.LockTime, minLockTime, maxLockTime)
				}
			}
		})
	}
}

func TestExecutePostBond(t *testing.T) {
	const (
		assetID  = 42
		strength = 1
	)
	coinID := []byte{0x01, 0x02, 0x03}

	tests := []struct {
		name           string
		existingBond   bool
		delayedConfirm bool
		wantEvent      bool
		accountReadErr error
	}{
		{
			name:           "account lookup fails",
			accountReadErr: errors.New("account lookup failed"),
		},
		{
			name:      "confirmed new bond emits event",
			wantEvent: true,
		},
		{
			name:         "existing bond completes without event",
			existingBond: true,
		},
		{
			name:           "delayed confirmation responds after event",
			delayedConfirm: true,
			wantEvent:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			user := tNewUser(t)
			authMgr, storage := newTestAuthManager(t)
			storage.accountReadErr = tt.accountReadErr
			if tt.wantEvent {
				// New bonds must use the transaction result without a second reputation read.
				storage.reputationErr = errors.New("unexpected reputation lookup after apply")
			}
			authMgr.signer.(*TSigner).setSig(user.randomSignature())

			// Build the bond that checkBond will report for this command.
			lockTime := time.Now().Add(48 * time.Hour).Unix()
			amount := int64(tRegFee * 10)
			bond := &db.Bond{
				Version:  0,
				AssetID:  assetID,
				CoinID:   coinID,
				Amount:   amount,
				Strength: strength,
				LockTime: lockTime,
			}
			storage.bondPostedResult = &db.BondPostedResult{
				BondAdded: true,
				Log:       &db.EventLogEntry{Seq: 1, Kind: meshevents.EventKindBondPosted},
				Bonds:     []*db.Bond{bond},
			}

			if tt.existingBond {
				// Existing bonds are acknowledged without publishing another event.
				storage.acct = &account.Account{ID: user.acctID, PubKey: user.privKey.PubKey()}
				storage.bonds = []*db.Bond{bond}
			}

			// Capture published events while still routing them through the real applier.
			var capturedEvents []*mesh.Event
			meshSvc, err := mesh.NewService(&mesh.ServiceConfig{
				EventLogReader: emptyEventLogReader{},
				OnHalt:         func(error) {},
				Commands:       authMgr.Commands(),
				Events:         eventsWithCapture(authMgr, &capturedEvents),
				Logger:         dex.Disabled,
			})
			if err != nil {
				t.Fatalf("NewService error: %v", err)
			}

			var confirmed chan struct{}
			if tt.delayedConfirm {
				// Start under-confirmed, then close confirmed so the bond waiter can finish.
				confirmed = make(chan struct{})
				queueCtx, shutdownQueue := context.WithCancel(context.Background())
				queueDone := make(chan struct{})
				authMgr.latencyQ = wait.NewTickerQueue(time.Millisecond)
				go func() {
					defer close(queueDone)
					authMgr.latencyQ.Run(queueCtx)
				}()
				t.Cleanup(func() {
					shutdownQueue()
					<-queueDone
				})
			}

			// Mock the asset backend's bond lookup.
			authMgr.checkBond = func(_ context.Context, gotAssetID uint32, ver uint16, gotCoinID []byte) (amt, gotLockTime, confs int64, acct account.AccountID, err error) {
				if gotAssetID != assetID || ver != 0 || !bytes.Equal(gotCoinID, coinID) {
					err = fmt.Errorf("bond lookup = %d/%d/%x, want %d/0/%x", gotAssetID, ver, gotCoinID, assetID, coinID)
					t.Error(err)
					return
				}
				if !tt.delayedConfirm {
					confs = tBondConfs
				} else {
					select {
					case <-confirmed:
						confs = tBondConfs
					default:
					}
				}
				return amount, lockTime, confs, user.acctID, nil
			}

			// Execute the postbond command and collect the client response.
			responses := make(chan *msgjson.Message, 1)
			msg, _ := tNewPostBondRequest(t, user, assetID, coinID)
			rpcErr := meshSvc.ExecuteCommand(context.Background(), mesh.CommandRequest{
				Kind: commandKindPostBond,
				User: user.acctID,
				Msg:  msg,
				Respond: func(resp *msgjson.Message) error {
					responses <- resp
					return nil
				},
			})
			if tt.accountReadErr != nil {
				if rpcErr == nil || rpcErr.Code != msgjson.RPCInternalError {
					t.Fatalf("account lookup error = %v, want RPCInternalError", rpcErr)
				}
				if len(capturedEvents) != 0 || storage.bondPostedEvent != nil {
					t.Fatal("bond applied after failed account lookup")
				}
				return
			}
			if rpcErr != nil {
				t.Fatalf("Execute postbond command error: %v", rpcErr)
			}

			var sent *msgjson.Message
			// Confirmed and existing bonds respond now; delayed bonds wait for the queue.
			select {
			case sent = <-responses:
				if tt.delayedConfirm {
					t.Fatalf("unexpected response before delayed confirmation: %v", sent)
				}
			default:
				if !tt.delayedConfirm {
					t.Fatal("no immediate postbond response")
				}
			}

			if tt.delayedConfirm {
				// The waiter emits the event and responds after the bond reaches confirmations.
				close(confirmed)
				select {
				case sent = <-responses:
				case <-time.After(time.Second):
					t.Fatal("timed out waiting for delayed postbond response")
				}
			}

			if sent == nil {
				t.Fatal("no postbond result delivered")
			}
			if sent.ID != msg.ID {
				t.Fatalf("wrong response id. got %d, want %d", sent.ID, msg.ID)
			}
			// All successful postbond commands return a signed result.
			result := decodePostBondResult(t, sent)
			if len(result.SigBytes()) == 0 {
				t.Fatalf("postbond result was not signed")
			}
			if result.Reputation == nil {
				t.Fatalf("postbond result did not include reputation")
			}
			if result.Reputation.BondedTier != int64(strength) {
				t.Fatalf("postbond reputation = %+v, want bonded tier %d", result.Reputation, strength)
			}

			// New bonds publish exactly one event; existing bonds only return the result.
			wantEvents := 0
			if tt.wantEvent {
				wantEvents = 1
			}
			if len(capturedEvents) != wantEvents {
				t.Fatalf("captured %d bond posted events, want %d", len(capturedEvents), wantEvents)
			}
			if !tt.wantEvent {
				return
			}

			// The event payload should describe the accepted bond.
			if capturedEvents[0].Kind != meshevents.EventKindBondPosted {
				t.Fatalf("wrong event kind %q for bond posted", capturedEvents[0].Kind)
			}
			posted, err := meshevents.DecodeBondPostedEvent(capturedEvents[0].Payload)
			if err != nil {
				t.Fatalf("DecodeBondPostedEvent error: %v", err)
			}
			if posted.Account == nil || posted.Account.AccountID != user.acctID {
				t.Fatalf("wrong event account in payload")
			}
			if posted.Bond == nil || posted.Bond.AssetID != assetID || !bytes.Equal(posted.Bond.CoinID, coinID) {
				t.Fatalf("wrong event bond in payload")
			}
		})
	}
}

func hasBondWaiter(authMgr *AuthManager, key string) bool {
	authMgr.bondWaiterMtx.Lock()
	defer authMgr.bondWaiterMtx.Unlock()
	_, found := authMgr.bondWaiterIdx[key]
	return found
}

func executePrepaidPostBondForTest(t *testing.T, authMgr *AuthManager, user *tUser, coinID []byte, capturedEvents *[]*mesh.Event) (*msgjson.Message, *msgjson.Error) {
	t.Helper()
	meshSvc, err := mesh.NewService(&mesh.ServiceConfig{
		EventLogReader: emptyEventLogReader{},
		OnHalt:         func(error) {},
		Commands:       authMgr.Commands(),
		Events:         eventsWithCapture(authMgr, capturedEvents),
		Logger:         dex.Disabled,
	})
	if err != nil {
		t.Fatalf("NewService error: %v", err)
	}

	responses := make(chan *msgjson.Message, 1)
	msg, _ := tNewPostBondRequest(t, user, account.PrepaidBondID, coinID)
	rpcErr := meshSvc.ExecuteCommand(context.Background(), mesh.CommandRequest{
		Kind: commandKindPostBond,
		User: user.acctID,
		Msg:  msg,
		Respond: func(resp *msgjson.Message) error {
			responses <- resp
			return nil
		},
	})
	select {
	case resp := <-responses:
		return resp, rpcErr
	default:
		return nil, rpcErr
	}
}

func TestExecutePrepaidPostBond(t *testing.T) {
	user := tNewUser(t)
	validLockTime := time.Now().Add(72 * time.Hour).Unix()
	validCoinID := bytes.Repeat([]byte{0x05}, 16)

	t.Run("redeem pre-paid bond and retry", func(t *testing.T) {
		authMgr, storage := newTestAuthManager(t)
		authMgr.signer.(*TSigner).setSig(user.randomSignature())
		storage.prepaidBonds = map[string]*meshevents.PrepaidBond{
			string(validCoinID): {
				CoinID:   validCoinID,
				Strength: 4,
				LockTime: validLockTime,
			},
		}
		bond := &db.Bond{
			AssetID:  account.PrepaidBondID,
			CoinID:   validCoinID,
			Strength: 4,
			LockTime: validLockTime,
		}
		storage.bondPostedResult = &db.BondPostedResult{
			BondAdded: true,
			Log:       &db.EventLogEntry{Seq: 1, Kind: meshevents.EventKindBondPosted},
			Bonds:     []*db.Bond{bond},
		}

		authMgr.checkBond = func(context.Context, uint32, uint16, []byte) (int64, int64, int64, account.AccountID, error) {
			t.Fatal("checkBond called for pre-paid bond")
			return 0, 0, 0, account.AccountID{}, nil
		}

		var capturedEvents []*mesh.Event
		resp, rpcErr := executePrepaidPostBondForTest(t, authMgr, user, validCoinID, &capturedEvents)
		if rpcErr != nil {
			t.Fatalf("Execute postbond command error: %v", rpcErr)
		}
		if resp == nil {
			t.Fatal("no pre-paid postbond response")
		}
		result := decodePostBondResult(t, resp)
		if result.AssetID != account.PrepaidBondID || result.Amount != 0 || result.Strength != 4 || !bytes.Equal(result.BondID, validCoinID) {
			t.Fatalf("wrong pre-paid postbond result: %+v", result)
		}
		if len(result.SigBytes()) == 0 {
			t.Fatal("pre-paid postbond result was not signed")
		}
		if result.Reputation == nil || result.Reputation.BondedTier != 4 {
			t.Fatalf("pre-paid postbond reputation = %+v, want bonded tier 4", result.Reputation)
		}
		if len(capturedEvents) != 1 || capturedEvents[0].Kind != meshevents.EventKindBondPosted {
			t.Fatalf("captured events = %+v, want one bond_posted event", capturedEvents)
		}
		if storage.bondPostedEvent == nil || storage.bondPostedEvent.Bond == nil ||
			storage.bondPostedEvent.Bond.AssetID != account.PrepaidBondID ||
			!bytes.Equal(storage.bondPostedEvent.Bond.CoinID, validCoinID) {
			t.Fatalf("wrong bond posted storage update: %+v", storage.bondPostedEvent)
		}
		if hasBondWaiter(authMgr, bondKey(account.PrepaidBondID, validCoinID)) {
			t.Fatalf("pre-paid bond waiter was not removed after success")
		}

		// On retry, storage has the account and bond but no unredeemed token.
		storage.acct = &account.Account{ID: user.acctID, PubKey: user.privKey.PubKey()}
		storage.bonds = []*db.Bond{bond}
		storage.prepaidBonds = map[string]*meshevents.PrepaidBond{}

		// A retry must not return success if reputation cannot be loaded.
		storage.reputationErr = errors.New("reputation lookup failed")
		authMgr.rep.invalidate(user.acctID)
		failed, rpcErr := executePrepaidPostBondForTest(t, authMgr, user, validCoinID, &capturedEvents)
		if failed != nil || rpcErr == nil || rpcErr.Code != msgjson.RPCInternalError {
			t.Fatalf("retry during reputation failure: response %v, error %v", failed, rpcErr)
		}
		storage.reputationErr = nil

		// Retry after redemption without replenishing the consumed token.
		resp, rpcErr = executePrepaidPostBondForTest(t, authMgr, user, validCoinID, &capturedEvents)
		if rpcErr != nil {
			t.Fatalf("Execute postbond command error: %v", rpcErr)
		}
		if resp == nil {
			t.Fatal("no retry postbond response")
		}
		result = decodePostBondResult(t, resp)
		if result.Reputation == nil || result.Reputation.BondedTier != 4 {
			t.Fatalf("retry reputation = %+v, want bonded tier 4", result.Reputation)
		}
		if len(capturedEvents) != 1 {
			t.Fatalf("total events after same-account retry = %d, want 1", len(capturedEvents))
		}
	})

	t.Run("unknown token releases waiter", func(t *testing.T) {
		authMgr, storage := newTestAuthManager(t)
		authMgr.signer.(*TSigner).setSig(user.randomSignature())
		storage.prepaidBonds = map[string]*meshevents.PrepaidBond{}

		var capturedEvents []*mesh.Event
		resp, rpcErr := executePrepaidPostBondForTest(t, authMgr, user, validCoinID, &capturedEvents)
		if resp != nil {
			t.Fatalf("unexpected pre-paid postbond response: %v", resp)
		}
		if rpcErr == nil || rpcErr.Code != msgjson.BondError {
			t.Fatalf("unknown token error = %v, want BondError", rpcErr)
		}
		if hasBondWaiter(authMgr, bondKey(account.PrepaidBondID, validCoinID)) {
			t.Fatalf("pre-paid bond waiter was not removed after unknown token")
		}
	})

	t.Run("account lookup fails", func(t *testing.T) {
		authMgr, storage := newTestAuthManager(t)
		storage.accountReadErr = errors.New("account lookup failed")

		var capturedEvents []*mesh.Event
		_, rpcErr := executePrepaidPostBondForTest(t, authMgr, user, validCoinID, &capturedEvents)
		if rpcErr == nil || rpcErr.Code != msgjson.RPCInternalError {
			t.Fatalf("account lookup error = %v, want RPCInternalError", rpcErr)
		}
		if len(capturedEvents) != 0 || storage.bondPostedEvent != nil {
			t.Fatal("bond applied after failed account lookup")
		}
	})
}

func TestExecuteForgiveReputation(t *testing.T) {
	for _, tt := range []struct {
		name       string
		scope      meshevents.ReputationForgivenessScope
		forgiven   bool
		storageErr error
	}{
		{"user", meshevents.ReputationForgivenessScopeUser, true, nil},
		{"match", meshevents.ReputationForgivenessScopeMatch, true, nil},
		{"match not forgiven", meshevents.ReputationForgivenessScopeMatch, false, nil},
		{"match rejected by storage", meshevents.ReputationForgivenessScopeMatch, false, errors.New("match is not eligible")},
	} {
		t.Run(tt.name, func(t *testing.T) {
			user := tNewUser(t)
			authMgr, storage := newTestAuthManager(t)
			authCtx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			authMgr.ctx = authCtx
			storage.acct = &account.Account{ID: user.acctID, PubKey: user.privKey.PubKey()}
			storage.bonds = []*db.Bond{{Strength: 1, LockTime: time.Now().Add(48 * time.Hour).Unix()}}
			storage.reputationForgivenErr = tt.storageErr
			if tt.storageErr == nil {
				storage.reputationForgivenResult = &db.ReputationForgivenResult{
					Forgiven: tt.forgiven,
					Log:      &db.EventLogEntry{Seq: 1, Kind: meshevents.EventKindReputationForgiven},
				}
			}
			svc, err := mesh.NewService(&mesh.ServiceConfig{
				EventLogReader: emptyEventLogReader{},
				OnHalt:         func(error) {},
				Commands:       authMgr.Commands(),
				Events:         authMgr.Events(),
				Logger:         dex.Disabled,
			})
			if err != nil {
				t.Fatal(err)
			}
			authMgr.SetMeshService(svc)

			var matchID order.MatchID
			if tt.scope == meshevents.ReputationForgivenessScopeUser {
				err = authMgr.ForgiveUser(user.acctID)
			} else {
				matchID = randomMatchID()
				var forgiven, unbanned bool
				forgiven, unbanned, err = authMgr.ForgiveMatchFail(user.acctID, matchID)
				if err == nil && (forgiven != tt.forgiven || !unbanned) {
					t.Fatalf("forgiven/unbanned = %v/%v, want %v/true", forgiven, unbanned, tt.forgiven)
				}
			}
			if tt.storageErr != nil {
				var rpcErr *msgjson.Error
				if !errors.As(err, &rpcErr) || rpcErr.Code != msgjson.RPCInternalError ||
					rpcErr.Message != "failed to apply reputation forgiveness: "+tt.storageErr.Error() {
					t.Fatalf("forgiveness error = %v, want storage rejection", err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			update := storage.reputationForgivenUpdate
			if update == nil || update.AccountID != user.acctID || update.Scope != tt.scope || update.Match() != matchID {
				t.Fatalf("incorrect storage update: %+v", update)
			}
		})
	}

	t.Run("reject invalid commands before storage", func(t *testing.T) {
		authMgr, storage := newTestAuthManager(t)
		user, other := tNewUser(t), tNewUser(t)
		svc, err := mesh.NewService(&mesh.ServiceConfig{
			EventLogReader: emptyEventLogReader{},
			OnHalt:         func(error) {},
			Commands:       authMgr.Commands(),
			Events:         authMgr.Events(),
			Logger:         dex.Disabled,
		})
		if err != nil {
			t.Fatal(err)
		}
		for _, tt := range []struct {
			name string
			user account.AccountID
			req  meshevents.ReputationForgivenEvent
			code int
		}{
			{"account mismatch", other.acctID, meshevents.ReputationForgivenEvent{AccountID: user.acctID, Scope: meshevents.ReputationForgivenessScopeUser}, msgjson.RPCInternalError},
			{"match scope without match", user.acctID, meshevents.ReputationForgivenEvent{AccountID: user.acctID, Scope: meshevents.ReputationForgivenessScopeMatch}, msgjson.RPCParseError},
		} {
			t.Run(tt.name, func(t *testing.T) {
				msg, err := msgjson.NewRequest(comms.NextID(), commandKindForgiveReputation, &tt.req)
				if err != nil {
					t.Fatal(err)
				}
				rpcErr := svc.ExecuteCommand(t.Context(), mesh.CommandRequest{
					Kind: commandKindForgiveReputation, User: tt.user, Msg: msg,
					Respond: func(*msgjson.Message) error {
						t.Error("invalid command delivered a result")
						return nil
					},
				})
				if rpcErr == nil || rpcErr.Code != tt.code {
					t.Fatalf("command error = %v, want code %d", rpcErr, tt.code)
				}
				if storage.reputationForgivenUpdate != nil {
					t.Fatal("invalid command reached storage")
				}
			})
		}
	})
}

func TestReputationForgivenessCommandWrapper(t *testing.T) {
	req := &meshevents.ReputationForgivenEvent{
		AccountID: tNewUser(t).acctID,
		Scope:     meshevents.ReputationForgivenessScopeUser,
	}

	t.Run("execute error", func(t *testing.T) {
		authMgr, _ := newTestAuthManager(t)
		wantErr := msgjson.NewError(msgjson.RPCInternalError, "mesh failed")
		authMgr.SetMeshService(&tMesh{executeErr: wantErr})
		_, err := authMgr.executeReputationForgivenessCommand(t.Context(), req)
		if err != wantErr {
			t.Fatalf("error = %v, want %v", err, wantErr)
		}
	})

	for _, name := range []string{"caller cancellation", "auth shutdown"} {
		t.Run(name, func(t *testing.T) {
			authMgr, _ := newTestAuthManager(t)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			callCtx := ctx
			if name == "auth shutdown" {
				authMgr.ctx = ctx
				callCtx = t.Context()
			}
			finished := make(chan struct{})
			authMgr.SetMeshService(&tMesh{
				executeHook: func(ctx context.Context, _ mesh.CommandRequest) *msgjson.Error {
					defer close(finished)
					cancel() // Cancel only after execution has started.
					<-ctx.Done()
					return nil
				},
			})
			_, err := authMgr.executeReputationForgivenessCommand(callCtx, req)
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("command error = %v, want cancellation", err)
			}
			select {
			case <-finished:
			case <-time.After(time.Second):
				t.Fatal("execution context was not canceled")
			}
		})
	}

	for _, tt := range []struct {
		name      string
		payload   any
		malformed bool
	}{
		{"response after execution returns", &reputationForgivenessResult{Forgiven: true, Unbanned: true}, false},
		{"malformed result", map[string]string{"forgiven": "not-bool"}, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			authMgr, _ := newTestAuthManager(t)
			requests := make(chan mesh.CommandRequest, 1)
			authMgr.SetMeshService(&tMesh{
				executeHook: func(_ context.Context, req mesh.CommandRequest) *msgjson.Error {
					requests <- req
					return nil // The response is delivered separately below.
				},
			})
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			done := make(chan struct{})
			var result *reputationForgivenessResult
			var resultErr error
			go func() {
				defer close(done)
				result, resultErr = authMgr.executeReputationForgivenessCommand(ctx, req)
			}()
			var command mesh.CommandRequest
			select {
			case command = <-requests:
			case <-ctx.Done():
				t.Fatal("command did not start")
			}
			resp, err := msgjson.NewResponse(command.Msg.ID, tt.payload, nil)
			if err != nil {
				t.Fatal(err)
			}
			if err := command.Respond(resp); err != nil {
				t.Fatal(err)
			}
			<-done
			if tt.malformed {
				var typeErr *json.UnmarshalTypeError
				if !errors.As(resultErr, &typeErr) {
					t.Fatalf("command error = %v, want JSON type error", resultErr)
				}
			} else if resultErr != nil || result == nil || !result.Forgiven || !result.Unbanned {
				t.Fatalf("command result = %+v, error = %v, want forgiven and unbanned", result, resultErr)
			}
		})
	}
}

func TestApplyBondPostedEvent(t *testing.T) {
	user := tNewUser(t)
	event := meshevents.NewBondPostedEvent(&account.Account{
		ID: user.acctID, PubKey: user.privKey.PubKey(),
	}, &meshevents.Bond{
		AssetID:  42,
		CoinID:   []byte{0x09, 0x08, 0x07},
		Amount:   int64(tRegFee * 10),
		Strength: 1,
		LockTime: time.Now().Add(48 * time.Hour).Unix(),
	})
	meta := &db.EventLogMeta{
		Seq:             7,
		Event:           []byte("bond-posted-event"),
		ExpectedTipHash: bytes.Repeat([]byte{0x7a}, db.EventLogTipHashSize),
	}
	logEntry := &db.EventLogEntry{Seq: meta.Seq, Kind: meshevents.EventKindBondPosted}
	stored := &db.BondPostedResult{
		Log: logEntry,
		Bonds: []*db.Bond{
			{Strength: 3, LockTime: event.Bond.LockTime},
			{Strength: 5, LockTime: time.Now().Add(12 * time.Hour).Unix()}, // expired for tier purposes
		},
		Preimages: []*db.PreimageOutcome{{Miss: true}},
		Matches: []*db.MatchResult{
			{MatchOutcome: db.OutcomeNoSwapAsTaker},
			{MatchOutcome: db.OutcomeNoSwapAsTaker},
		},
	}

	for _, tt := range []struct {
		name   string
		result *db.BondPostedResult
		err    error
	}{
		{"returns stored log and signed reputation", stored, nil},
		{"storage error", nil, errors.New("storage failed")},
	} {
		t.Run(tt.name, func(t *testing.T) {
			authMgr, storage := newTestAuthManager(t)
			storage.bondPostedResult = tt.result
			storage.bondPostedErr = tt.err
			applyCtx := &mesh.EventApplyContext{Context: context.Background()}
			authMgr.signer.(*TSigner).setSig(user.randomSignature())
			storage.reputationErr = errors.New("unexpected reputation lookup after apply")
			minThreshold := time.Now().Add(authMgr.bondExpiry).Unix()
			applied, err := authMgr.applyBondPostedEvent(applyCtx, meta, event)
			maxThreshold := time.Now().Add(authMgr.bondExpiry).Unix()
			if !errors.Is(err, tt.err) {
				t.Fatalf("applyBondPostedEvent error = %v, want %v", err, tt.err)
			}
			if storage.bondPostedMeta != meta || storage.bondPostedEvent != event {
				t.Fatal("storage received incorrect metadata or event")
			}
			if want := [3]int{scoringOrderLimit, ScoringMatchLimit, cancelThreshWindow}; storage.bondPostedLimits != want {
				t.Fatalf("reputation limits = %v, want %v", storage.bondPostedLimits, want)
			}

			if tt.err != nil {
				if applied != nil {
					t.Fatal("returned an event log entry on storage error")
				}
				if applyCtx.Result() != nil {
					t.Fatal("set a result after storage failure")
				}
				return
			}
			if applied != tt.result.Log {
				t.Fatalf("applied log = %+v, want %+v", applied, tt.result.Log)
			}
			result := applyCtx.Result().(*msgjson.PostBondResult)
			if len(result.SigBytes()) == 0 {
				t.Fatal("postbond result was not signed")
			}
			rep := result.Reputation
			// Two failed swaps (-22) and one preimage miss (-2) produce one penalty.
			const wantScore = -24
			if rep == nil || rep.BondedTier != 3 || rep.Score != wantScore || rep.Penalties != 1 {
				t.Fatalf("reputation = %+v, want tier 3 and score %d", rep, wantScore)
			}
			if rep.BondExpiryThreshold < minThreshold || rep.BondExpiryThreshold > maxThreshold {
				t.Fatalf("bond expiry threshold %d outside [%d, %d]", rep.BondExpiryThreshold, minThreshold, maxThreshold)
			}
			if !bytes.Equal(result.AccountID, user.acctID[:]) || result.AssetID != event.Bond.AssetID ||
				!bytes.Equal(result.BondID, event.Bond.CoinID) || result.Amount != uint64(event.Bond.Amount) ||
				result.Strength != event.Bond.Strength || result.Expiry != uint64(time.Unix(event.Bond.LockTime, 0).Add(-authMgr.bondExpiry).Unix()) {
				t.Fatalf("postbond result does not match event: %+v", result)
			}
		})
	}

	t.Run("account id mismatch rejected before storage", func(t *testing.T) {
		authMgr, storage := newTestAuthManager(t)
		invalid := *event
		invalidAccount := *event.Account
		invalidAccount.AccountID = tNewUser(t).acctID
		invalid.Account = &invalidAccount
		meshEvent, err := mesh.NewEvent(&invalid)
		if err != nil {
			t.Fatal(err)
		}
		apply := authMgr.Events()[meshevents.EventKindBondPosted]
		if _, err := apply(&mesh.EventApplyContext{Context: context.Background()}, meshEvent); err == nil {
			t.Fatal("event handler accepted mismatched account id")
		}
		if storage.bondPostedEvent != nil {
			t.Fatal("storage was called for invalid event")
		}
	})
}

func TestApplyReputationForgivenEvent(t *testing.T) {
	for _, tt := range []struct {
		name         string
		forgiven     bool
		unknown      bool
		bonded       bool
		readErr      error
		wantUnbanned bool
	}{
		{name: "positive tier", forgiven: true, bonded: true, wantUnbanned: true},
		{name: "not forgiven but positive tier", bonded: true, wantUnbanned: true},
		{name: "known account with zero tier", forgiven: true},
		{name: "unknown account", forgiven: true, unknown: true},
		{name: "reputation read fails after commit", forgiven: true, bonded: true, readErr: errors.New("reputation lookup failed")},
	} {
		t.Run(tt.name, func(t *testing.T) {
			authMgr, storage := newTestAuthManager(t)
			user := tNewUser(t)
			if !tt.unknown {
				storage.acct = &account.Account{ID: user.acctID, PubKey: user.privKey.PubKey()}
			}
			if tt.bonded {
				storage.bonds = []*db.Bond{{Strength: 1, LockTime: time.Now().Add(48 * time.Hour).Unix()}}
			}
			storage.reputationErr = tt.readErr
			event := &meshevents.ReputationForgivenEvent{
				AccountID: user.acctID,
				Scope:     meshevents.ReputationForgivenessScopeUser,
			}
			meta := &db.EventLogMeta{Seq: 9, Event: []byte("reputation-forgiven")}
			logEntry := &db.EventLogEntry{Seq: 9, Kind: meshevents.EventKindReputationForgiven}
			storage.reputationForgivenResult = &db.ReputationForgivenResult{Forgiven: tt.forgiven, Log: logEntry}
			applyCtx := &mesh.EventApplyContext{Context: t.Context()}

			applied, err := authMgr.applyReputationForgivenEvent(applyCtx, meta, event)
			if err != nil {
				t.Fatal(err)
			}
			if storage.reputationForgivenMeta != meta || storage.reputationForgivenUpdate != event {
				t.Fatal("storage did not receive the supplied metadata and event")
			}
			if applied != logEntry {
				t.Fatalf("applied log = %v, want stored log %v", applied, logEntry)
			}
			result, _ := applyCtx.Result().(*reputationForgivenessResult)
			if result == nil || result.Forgiven != tt.forgiven || result.Unbanned != tt.wantUnbanned {
				t.Fatalf("result = %+v, want forgiven %v, unbanned %v", result, tt.forgiven, tt.wantUnbanned)
			}
		})
	}

	// The Validate matrix is covered in meshevents; this only proves an
	// invalid event is rejected before storage is touched.
	t.Run("validation rejects bad events before storage", func(t *testing.T) {
		authMgr, storage := newTestAuthManager(t)
		zeroAccount := &meshevents.ReputationForgivenEvent{Scope: meshevents.ReputationForgivenessScopeUser}
		payload, err := zeroAccount.Encode()
		if err != nil {
			t.Fatalf("Encode error: %v", err)
		}
		applier := authMgr.Events()[meshevents.EventKindReputationForgiven]
		if _, err := applier(&mesh.EventApplyContext{Context: context.Background()}, &mesh.Event{Payload: payload}); err == nil {
			t.Fatalf("applying invalid reputation_forgiven event succeeded")
		}
		if storage.reputationForgivenUpdate != nil {
			t.Fatalf("storage was called for invalid event")
		}
	})
}

func TestAuth(t *testing.T) {
	user := tNewUser(t)
	rig.signer.setSig(user.randomSignature())
	connectUser(t, user)

	msgBytes := randBytes(50)
	sigBytes := signMsg(user.privKey, msgBytes)
	err := rig.mgr.Auth(user.acctID, msgBytes, sigBytes)
	if err != nil {
		t.Fatalf("unexpected auth error: %v", err)
	}

	foreigner := tNewUser(t)
	sigBytes = signMsg(user.privKey, msgBytes)
	err = rig.mgr.Auth(foreigner.acctID, msgBytes, sigBytes)
	if err == nil {
		t.Fatalf("no auth error for foreigner")
	}

	msgBytes = randBytes(50)
	err = rig.mgr.Auth(user.acctID, msgBytes, sigBytes)
	if err == nil {
		t.Fatalf("no error for wrong message")
	}
}

func TestSign(t *testing.T) {
	sig1 := tNewUser(t).randomSignature()
	sig1Bytes := sig1.Serialize()
	rig.signer.setSig(sig1)
	s := &tSignable{b: randBytes(25)}
	rig.mgr.Sign(s)
	if !bytes.Equal(sig1Bytes, s.SigBytes()) {
		t.Fatalf("incorrect signature. expected %x, got %x", sig1.Serialize(), s.SigBytes())
	}

	// Try two at a time
	s2 := &tSignable{b: randBytes(25)}
	rig.mgr.Sign(s, s2)
}

func TestSend(t *testing.T) {
	user := tNewUser(t)
	rig.signer.setSig(user.randomSignature())
	connectUser(t, user)
	foreigner := tNewUser(t)

	type tA struct {
		A int
	}
	payload := &tA{A: 5}
	resp, _ := msgjson.NewResponse(comms.NextID(), payload, nil)
	payload = &tA{A: 10}
	req, _ := msgjson.NewRequest(comms.NextID(), "testroute", payload)

	// Send a message to a foreigner
	if err := rig.mgr.Send(foreigner.acctID, resp); !errors.Is(err, ErrUserNotConnected) {
		t.Fatalf("Send with no mesh peer = %v, want ErrUserNotConnected", err)
	}
	if foreigner.conn.getSend() != nil {
		t.Fatalf("message magically got through to foreigner")
	}
	if user.conn.getSend() != nil {
		t.Fatalf("foreigner message sent to authed user")
	}

	// Now send to the user
	rig.mgr.Send(user.acctID, resp)
	msg := user.conn.getSend()
	if msg == nil {
		t.Fatalf("no message for authed user")
	}
	tr := new(tA)
	r, _ := msg.Response()
	err := json.Unmarshal(r.Result, tr)
	if err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if tr.A != 5 {
		t.Fatalf("expected A = 5, got A = %d", tr.A)
	}

	// Send a request to a foreigner
	rig.mgr.Request(foreigner.acctID, req, func(comms.Link, *msgjson.Message) {})
	if foreigner.conn.getReq() != nil {
		t.Fatalf("request magically got through to foreigner")
	}
	if user.conn.getReq() != nil {
		t.Fatalf("foreigner request sent to authed user")
	}

	// Send a request to an authed user.
	rig.mgr.Request(user.acctID, req, func(comms.Link, *msgjson.Message) {})
	treq := user.conn.getReq()
	if treq == nil {
		t.Fatalf("no request for user")
	}

	tr = new(tA)
	err = json.Unmarshal(treq.msg.Payload, tr)
	if err != nil {
		t.Fatalf("request unmarshal error: %v", err)
	}
	if tr.A != 10 {
		t.Fatalf("expected A = 10, got A = %d", tr.A)
	}
}

func TestConnectErrors(t *testing.T) {
	user := tNewUser(t)
	rig.storage.acct = nil
	rig.signer.setSig(user.randomSignature())

	ensureErr := makeEnsureErr(t)

	// Test an invalid json payload
	msg, err := msgjson.NewRequest(comms.NextID(), "testreq", nil)
	if err != nil {
		t.Fatalf("NewRequest error for invalid payload: %v", err)
	}
	msg.Payload = []byte(`?`)
	rpcErr := rig.mgr.handleConnect(user.conn, msg)
	ensureErr(rpcErr, "invalid payload", msgjson.RPCParseError)

	connect := tNewConnect(user)
	encodeMsg := func() {
		msg, err = msgjson.NewRequest(comms.NextID(), "testreq", connect)
		if err != nil {
			t.Fatalf("NewRequest error for bad account ID: %v", err)
		}
	}
	// connect with an invalid ID
	connect.AccountID = []byte{0x01, 0x02, 0x03, 0x04}
	encodeMsg()
	rpcErr = rig.mgr.handleConnect(user.conn, msg)
	ensureErr(rpcErr, "invalid account ID", msgjson.AuthenticationError)
	connect.AccountID = user.acctID[:]

	// account lookup fails
	encodeMsg()
	rig.storage.accountReadErr = errors.New("account lookup failed")
	rpcErr = rig.mgr.handleConnect(user.conn, msg)
	rig.storage.accountReadErr = nil
	ensureErr(rpcErr, "account lookup failed", msgjson.RPCInternalError)

	// user unknown to storage
	encodeMsg()
	rpcErr = rig.mgr.handleConnect(user.conn, msg)
	ensureErr(rpcErr, "account unknown to storage", msgjson.AccountNotFoundError)
	rig.storage.acct = &account.Account{ID: user.acctID, PubKey: user.privKey.PubKey()}

	// bad signature
	connect.SetSig([]byte{0x09, 0x08})
	encodeMsg()
	rpcErr = rig.mgr.handleConnect(user.conn, msg)
	ensureErr(rpcErr, "bad signature", msgjson.SignatureError)

	// A send error should not return an error, but the client should not be
	// saved to the map.
	// need to "register" the user first
	msgBytes := connect.Serialize()
	connect.SetSig(signMsg(user.privKey, msgBytes))
	encodeMsg()
	user.conn.sendErr = fmt.Errorf("test error")
	rpcErr = rig.mgr.handleConnect(user.conn, msg)
	if rpcErr != nil {
		t.Fatalf("non-nil msgjson.Error after send error: %s", rpcErr.Message)
	}
	user.conn.sendErr = nil
	if rig.mgr.user(user.acctID) != nil {
		t.Fatalf("user registered with send error")
	}
	// clear the response
	if user.conn.getSend() == nil {
		t.Fatalf("no response to clear")
	}

	// success
	rpcErr = rig.mgr.handleConnect(user.conn, msg)
	if rpcErr != nil {
		t.Fatalf("error for good connect: %s", rpcErr.Message)
	}
	// clear the response
	if user.conn.getSend() == nil {
		t.Fatalf("no response to clear")
	}
}

func TestHandleResponse(t *testing.T) {
	user := tNewUser(t)
	rig.signer.setSig(user.randomSignature())
	connectUser(t, user)
	foreigner := tNewUser(t)
	unknownResponse, err := msgjson.NewResponse(comms.NextID(), 10, nil)
	if err != nil {
		t.Fatalf("error encoding unknown response: %v", err)
	}

	// test foreigner. Really just want to make sure that this returns before
	// trying to run a nil handler function, which would panic.
	rig.mgr.handleResponse(foreigner.conn, unknownResponse)

	// test for a missing handler
	rig.mgr.handleResponse(user.conn, unknownResponse)
	m := user.conn.getSend()
	if m == nil {
		t.Fatalf("no error sent for unknown response")
	}
	resp, _ := m.Response()
	if resp.Error == nil {
		t.Fatalf("error not set in response for unknown response")
	}
	if resp.Error.Code != msgjson.UnknownResponseID {
		t.Fatalf("wrong error code for unknown response. expected %d, got %d",
			msgjson.UnknownResponseID, resp.Error.Code)
	}

	// Check that expired response handlers are removed from the map.
	client := rig.mgr.user(user.acctID)
	if client == nil {
		t.Fatalf("client not found")
	}

	newID := comms.NextID()
	client.logReq(newID, func(comms.Link, *msgjson.Message) {},
		0, func() { t.Log("expired (ok)") })
	// Wait until response handler expires.
	if waitFor(func() bool {
		client.mtx.Lock()
		defer client.mtx.Unlock()
		return len(client.respHandlers) == 0
	}, 10*time.Second) {
		t.Fatalf("expected 0 response handlers, found %d", len(client.respHandlers))
	}
	client.mtx.Lock()
	if client.respHandlers[newID] != nil {
		t.Fatalf("response handler should have been expired")
	}
	client.mtx.Unlock()

	// After logging a new request, there should still be exactly one response handler
	// present. A short sleep is added to give a chance for clean-up running in a
	// separate go-routine to finish before we continue asserting on the result.
	newID = comms.NextID()
	client.logReq(newID, func(comms.Link, *msgjson.Message) {}, time.Hour, noop)
	time.Sleep(time.Millisecond)
	client.mtx.Lock()
	if len(client.respHandlers) != 1 {
		t.Fatalf("expected 1 response handler, found %d", len(client.respHandlers))
	}
	if client.respHandlers[newID] == nil {
		t.Fatalf("wrong response handler left after cleanup cycle")
	}
	client.mtx.Unlock()
}

func TestMatchStatus(t *testing.T) {
	user := tNewUser(t)
	rig.signer.setSig(user.randomSignature())
	connectUser(t, user)

	rig.storage.matchStatuses = []*db.MatchStatus{{
		Status:    order.MakerSwapCast,
		IsTaker:   true,
		MakerSwap: []byte{0x01},
	}}

	tTxData := encode.RandomBytes(5)
	rig.mgr.txDataSources[0] = func([]byte) ([]byte, error) {
		return tTxData, nil
	}

	reqPayload := []msgjson.MatchRequest{{MatchID: encode.RandomBytes(32)}}

	req, _ := msgjson.NewRequest(1, msgjson.MatchStatusRoute, reqPayload)

	getStatus := func() *msgjson.MatchStatusResult {
		msgErr := rig.mgr.handleMatchStatus(user.conn, req)
		if msgErr != nil {
			t.Fatalf("handleMatchStatus error: %v", msgErr)
		}

		resp := user.conn.getSend()
		if resp == nil {
			t.Fatalf("no matches sent")
		}

		statuses := []msgjson.MatchStatusResult{}
		err := resp.UnmarshalResult(&statuses)
		if err != nil {
			t.Fatalf("UnmarshalResult error: %v", err)
		}
		if len(statuses) != 1 {
			t.Fatalf("expected 1 match, got %d", len(statuses))
		}
		return &statuses[0]
	}

	// As taker in MakerSwapCast, we expect tx data.
	status := getStatus()
	if !bytes.Equal(status.MakerTxData, tTxData) {
		t.Fatalf("wrong maker tx data. exected %x, got %s", tTxData, status.MakerTxData)
	}

	// As maker, we don't expect any tx data.
	rig.storage.matchStatuses[0].IsTaker = false
	rig.storage.matchStatuses[0].IsMaker = true
	if len(getStatus().TakerTxData) != 0 {
		t.Fatalf("got tx data as maker in MakerSwapCast")
	}

	// As maker in TakerSwapCast, we do expect tx data.
	rig.storage.matchStatuses[0].Status = order.TakerSwapCast
	rig.storage.matchStatuses[0].TakerSwap = []byte{0x01}
	txData := getStatus().TakerTxData
	if !bytes.Equal(txData, tTxData) {
		t.Fatalf("wrong taker tx data. exected %x, got %s", tTxData, txData)
	}

	reqPayload[0].MatchID = []byte{}
	req, _ = msgjson.NewRequest(1, msgjson.MatchStatusRoute, reqPayload)
	msgErr := rig.mgr.handleMatchStatus(user.conn, req)
	if msgErr == nil {
		t.Fatalf("no error for bad match ID")
	}
}

func TestOrderStatus(t *testing.T) {
	user := tNewUser(t)
	rig.signer.setSig(user.randomSignature())
	connectUser(t, user)

	rig.storage.orderStatuses = []*db.OrderStatus{{}}

	reqPayload := []msgjson.OrderStatusRequest{
		{
			OrderID: encode.RandomBytes(order.OrderIDSize),
		},
	}

	req, _ := msgjson.NewRequest(1, msgjson.OrderStatusRoute, reqPayload)

	msgErr := rig.mgr.handleOrderStatus(user.conn, req)
	if msgErr != nil {
		t.Fatalf("handleOrderStatus error: %v", msgErr)
	}

	resp := user.conn.getSend()
	if resp == nil {
		t.Fatalf("no orders sent")
	}

	var statuses []*msgjson.OrderStatus
	err := resp.UnmarshalResult(&statuses)
	if err != nil {
		t.Fatalf("UnmarshalResult error: %v", err)
	}
	if len(statuses) != 1 {
		t.Fatalf("expected 1 order, got %d", len(statuses))
	}

	reqPayload[0].OrderID = []byte{}
	req, _ = msgjson.NewRequest(1, msgjson.OrderStatusRoute, reqPayload)
	msgErr = rig.mgr.handleOrderStatus(user.conn, req)
	if msgErr == nil {
		t.Fatalf("no error for bad order ID")
	}
}

func Test_checkSigS256(t *testing.T) {
	sig := []byte{0x30, 0, 0x02, 0x01, 9, 0x2, 0x01, 10}
	ecdsa.ParseDERSignature(sig) // panic on line 132: sigStr[2] != 0x02 after trimming to sigStr[:(1+2)]

	sig = []byte{0x30, 1, 0x02, 0x01, 9, 0x2, 0x01, 10}
	ecdsa.ParseDERSignature(sig) // panic on line 139: rLen := int(sigStr[index]) with index=3 and len = 3
}
