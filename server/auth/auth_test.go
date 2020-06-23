// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package auth

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"strings"
	"testing"
	"time"

	"decred.org/dcrdex/dex/encode"
	"decred.org/dcrdex/dex/msgjson"
	"decred.org/dcrdex/dex/order"
	"decred.org/dcrdex/server/account"
	"decred.org/dcrdex/server/comms"
	"github.com/decred/dcrd/dcrec/secp256k1/v2"
)

func noop() {}

func randBytes(l int) []byte {
	b := make([]byte, l)
	rand.Read(b)
	return b
}

type ratioData struct {
	oidsCompleted  []order.OrderID
	timesCompleted []int64
	oidsCancels    []order.OrderID
	oidsCanceled   []order.OrderID
	timesCanceled  []int64
}

// TStorage satisfies the Storage interface
type TStorage struct {
	acct     *account.Account
	matches  []*order.UserMatch
	closedID account.AccountID
	acctAddr string
	acctErr  error
	regAddr  string
	regErr   error
	payErr   error
	unpaid   bool
	closed   bool
	ratio    ratioData
}

func (s *TStorage) CloseAccount(id account.AccountID, _ account.Rule) { s.closedID = id }
func (s *TStorage) Account(account.AccountID) (*account.Account, bool, bool) {
	return s.acct, !s.unpaid, !s.closed
}
func (s *TStorage) ActiveMatches(account.AccountID) ([]*order.UserMatch, error) {
	return s.matches, nil
}
func (s *TStorage) CreateAccount(*account.Account) (string, error)          { return s.acctAddr, s.acctErr }
func (s *TStorage) CreateAccountWithAddress(*account.Account, string) error { return s.acctErr }
func (s *TStorage) AccountRegAddr(account.AccountID) (string, error)        { return s.regAddr, s.regErr }
func (s *TStorage) PayAccount(account.AccountID, []byte) error              { return s.payErr }
func (s *TStorage) setRatioData(dat *ratioData) {
	s.ratio = *dat
}
func (s *TStorage) CompletedUserOrders(aid account.AccountID, N int) (oids []order.OrderID, compTimes []int64, err error) {
	return s.ratio.oidsCompleted, s.ratio.timesCompleted, nil
}
func (s *TStorage) ExecutedCancelsForUser(aid account.AccountID, N int) (oids, targets []order.OrderID, execTimes []int64, err error) {
	return s.ratio.oidsCancels, s.ratio.oidsCanceled, s.ratio.timesCanceled, nil
}

// TSigner satisfies the Signer interface
type TSigner struct {
	sig    *secp256k1.Signature
	err    error
	pubkey *secp256k1.PublicKey
}

func (s *TSigner) Sign(hash []byte) (*secp256k1.Signature, error) { return s.sig, s.err }
func (s *TSigner) PubKey() *secp256k1.PublicKey                   { return s.pubkey }

type tReq struct {
	msg      *msgjson.Message
	respFunc func(comms.Link, *msgjson.Message)
}

// tRPCClient satisfies the comms.Link interface.
type TRPCClient struct {
	id         uint64
	ip         string
	sendErr    error
	requestErr error
	banished   bool
	sends      []*msgjson.Message
	reqs       []*tReq
}

func (c *TRPCClient) ID() uint64 { return c.id }
func (c *TRPCClient) IP() string { return c.ip }
func (c *TRPCClient) Send(msg *msgjson.Message) error {
	c.sends = append(c.sends, msg)
	return c.sendErr
}
func (c *TRPCClient) SendError(id uint64, msg *msgjson.Error) {
}
func (c *TRPCClient) Request(msg *msgjson.Message, f func(comms.Link, *msgjson.Message), _ time.Duration, _ func()) error {
	c.reqs = append(c.reqs, &tReq{
		msg:      msg,
		respFunc: f,
	})
	return c.requestErr
}
func (c *TRPCClient) Disconnect() {}
func (c *TRPCClient) Banish()     { c.banished = true }
func (c *TRPCClient) getReq() *tReq {
	if len(c.reqs) == 0 {
		return nil
	}
	req := c.reqs[0]
	c.reqs = c.reqs[1:]
	return req
}
func (c *TRPCClient) getSend() *msgjson.Message {
	if len(c.sends) == 0 {
		return nil
	}
	msg := c.sends[0]
	c.sends = c.sends[1:]
	return msg
}

var tClientID uint64

func tNewRPCClient() *TRPCClient {
	tClientID++
	return &TRPCClient{
		id: tClientID,
		ip: "123.123.123.123",
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

func tNewUser(t *testing.T) *tUser {
	conn := tNewRPCClient()
	// register the RPCClient with a 'connect' Message
	acctID := newAccountID()
	privKey, err := secp256k1.GeneratePrivateKey()
	if err != nil {
		t.Fatalf("error generating private key: %v", err)
	}
	return &tUser{
		conn:    conn,
		acctID:  acctID,
		privKey: privKey,
	}
}

func (u *tUser) randomSignature() *secp256k1.Signature {
	sig, _ := u.privKey.Sign(randBytes(20))
	return sig
}

type testRig struct {
	mgr     *AuthManager
	storage *TStorage
	signer  *TSigner
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

func tNewConnect(user *tUser) *msgjson.Connect {
	return &msgjson.Connect{
		AccountID:  user.acctID[:],
		APIVersion: 0,
		Time:       encode.UnixMilliU(unixMsNow()),
	}
}

func extractConnectResult(t *testing.T, msg *msgjson.Message) *msgjson.ConnectResult {
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
	rig.storage.acct = &account.Account{ID: user.acctID, PubKey: user.privKey.PubKey()}
	connect := tNewConnect(user)
	sigMsg := connect.Serialize()
	sig, err := user.privKey.Sign(sigMsg)
	if err != nil {
		t.Fatalf("error signing message of length %d", len(sigMsg))
	}
	connect.SetSig(sig.Serialize())
	msg, _ := msgjson.NewRequest(comms.NextID(), msgjson.ConnectRoute, connect)
	return msg
}

func connectUser(t *testing.T, user *tUser) *msgjson.Message {
	return tryConnectUser(t, user, false)
}

func tryConnectUser(t *testing.T, user *tUser, wantErr bool) *msgjson.Message {
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
		if rpcErr == nil {
			t.Fatalf("no error for %s ID", tag)
		}
		if rpcErr.Code != code {
			t.Fatalf("wrong error code for %s. expected %d, got %d: %s",
				tag, code, rpcErr.Code, rpcErr.Message)
		}
	}
}

var (
	tCheckFeeAddr         = "DsaAKsMvZ6HrqhmbhLjV9qVbPkkzF5daowT"
	tCheckFeeVal   uint64 = 500_000_000
	tCheckFeeConfs int64  = 5
	tCheckFeeErr   error
)

func tCheckFee([]byte) (addr string, val uint64, confs int64, err error) {
	return tCheckFeeAddr, tCheckFeeVal, tCheckFeeConfs, tCheckFeeErr
}

const (
	tRegFee       uint64 = 500_000_000
	tDexPubKeyHex string = "032e3678f9889206dcea4fc281556c9e543c5d5ffa7efe8d11118b52e29c773f27"
	tFeeAddr      string = "Dcur2mcGjmENx4DhNqDctW5wJCVyT3Qeqkx"
)

var tDexPubKeyBytes = []byte{
	0x03, 0x2e, 0x36, 0x78, 0xf9, 0x88, 0x92, 0x06, 0xdc, 0xea, 0x4f, 0xc2,
	0x81, 0x55, 0x6c, 0x9e, 0x54, 0x3c, 0x5d, 0x5f, 0xfa, 0x7e, 0xfe, 0x8d,
	0x11, 0x11, 0x8b, 0x52, 0xe2, 0x9c, 0x77, 0x3f, 0x27,
}

func resetStorage() {
	*rig.storage = TStorage{acctAddr: tFeeAddr, regAddr: tCheckFeeAddr}
}

func TestMain(m *testing.M) {
	doIt := func() int {
		ctx, shutdown := context.WithCancel(context.Background())
		defer shutdown()
		storage := &TStorage{acctAddr: tFeeAddr, regAddr: tCheckFeeAddr}
		dexKey, _ := secp256k1.ParsePubKey(tDexPubKeyBytes)
		signer := &TSigner{pubkey: dexKey}
		authMgr := NewAuthManager(&Config{
			Storage:         storage,
			Signer:          signer,
			RegistrationFee: tRegFee,
			FeeConfs:        tCheckFeeConfs,
			FeeChecker:      tCheckFee,
			CancelThreshold: 0.8,
		})
		go authMgr.Run(ctx)
		rig = &testRig{
			storage: storage,
			signer:  signer,
			mgr:     authMgr,
		}
		return m.Run()
	}

	os.Exit(doIt())
}

func TestConnect(t *testing.T) {
	// Before connecting, put an activeMatch in storage.
	anyID := newAccountID()
	var mid order.MatchID
	copy(mid[:], anyID[:])
	anyID = newAccountID()
	var oid order.OrderID
	copy(oid[:], anyID[:])
	userMatch := &order.UserMatch{
		OrderID:  oid,
		MatchID:  mid,
		Quantity: 1,
		Rate:     2,
		Address:  "anyaddress",
		Status:   3,
		Side:     4,
	}
	rig.storage.matches = []*order.UserMatch{userMatch}
	rig.storage.setRatioData(&ratioData{
		oidsCompleted:  []order.OrderID{{0x1}},
		timesCompleted: []int64{1234},
		oidsCancels:    []order.OrderID{{0x2}},
		oidsCanceled:   []order.OrderID{{0x1}},
		timesCanceled:  []int64{1235},
	}) // 50%
	defer rig.storage.setRatioData(&ratioData{}) // clean slate

	// Close account on connect with failing cancel ratio.
	rig.mgr.cancelThresh = 0.2
	user := tNewUser(t)
	rig.signer.sig = user.randomSignature()
	tryConnectUser(t, user, false)
	if rig.storage.closedID != user.acctID {
		t.Fatalf("Expected account %v to be closed on connect, got %v", user.acctID, rig.storage.closedID)
	}
	rig.storage.closedID = account.AccountID{}
	rig.mgr.cancelThresh = 0.8 // passable

	// Connect the user.
	user = tNewUser(t)
	respMsg := connectUser(t, user)
	cResp := extractConnectResult(t, respMsg)
	if len(cResp.Matches) != 1 {
		t.Fatalf("no active matches")
	}
	rig.storage.matches = nil
	msgMatch := cResp.Matches[0]
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

	reuser := tNewUser(t)
	reuser.acctID = user.acctID
	reuser.privKey = user.privKey
	connectUser(t, reuser)
	reqID = comms.NextID()
	a10 := &tPayload{A: 10}
	msg, _ = msgjson.NewRequest(reqID, "request", a10)
	rig.mgr.RequestWithTimeout(reuser.acctID, msg, func(comms.Link, *msgjson.Message) {}, 0, func() {})
	// The a10 message should be in the new connection
	if user.conn.getReq() != nil {
		t.Fatalf("old connection received a request after reconnection")
	}
	if reuser.conn.getReq() == nil {
		t.Fatalf("new connection did not receive the request")
	}

	// TODO: test RequestWhenConnected
}

func TestAccountErrors(t *testing.T) {
	user := tNewUser(t)
	connect := queueUser(t, user)
	// Put a match in storage
	var oid order.OrderID
	copy(oid[:], randBytes(32))
	var mid order.MatchID
	copy(mid[:], randBytes(32))
	userMatch := &order.UserMatch{
		OrderID:  oid,
		MatchID:  mid,
		Quantity: 123456,
		Rate:     789123,
		Address:  "anthing",
		Status:   order.NewlyMatched,
		Side:     order.Maker,
	}
	rig.storage.matches = []*order.UserMatch{userMatch}
	rig.mgr.handleConnect(user.conn, connect)
	rig.storage.matches = nil

	// Check the response.
	respMsg := user.conn.getSend()
	result := extractConnectResult(t, respMsg)
	if len(result.Matches) != 1 {
		t.Fatalf("expected 1 match, received %d", len(result.Matches))
	}
	match := result.Matches[0]
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

	// unpaid account.
	rig.storage.unpaid = true
	rpcErr := rig.mgr.handleConnect(user.conn, connect)
	rig.storage.unpaid = false
	if rpcErr == nil {
		t.Fatalf("no error for unpaid account")
	}
	if rpcErr.Code != msgjson.AuthenticationError {
		t.Fatalf("wrong error for unpaid account. wanted %d, got %d",
			msgjson.AuthenticationError, rpcErr.Code)
	}

	// closed accounts allowed to connect
	rig.mgr.removeClient(rig.mgr.user(user.acctID)) // disconnect first
	rig.storage.closed = true
	rpcErr = rig.mgr.handleConnect(user.conn, connect)
	rig.storage.closed = false
	if rpcErr != nil {
		t.Fatalf("should be no error for closed account")
	}
	client := rig.mgr.user(user.acctID)
	if !client.isSuspended() {
		t.Errorf("client should have been in suspended state")
	}

}

func TestRoute(t *testing.T) {
	user := tNewUser(t)
	connectUser(t, user)

	var translated account.AccountID
	rig.mgr.Route("testroute", func(id account.AccountID, msg *msgjson.Message) *msgjson.Error {
		translated = id
		return nil
	})
	f := comms.RouteHandler("testroute")
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

func TestAuth(t *testing.T) {
	user := tNewUser(t)
	connectUser(t, user)

	msgBytes := randBytes(50)
	sig, err := user.privKey.Sign(msgBytes)
	if err != nil {
		t.Fatalf("signing error: %v", err)
	}
	sigBytes := sig.Serialize()
	err = rig.mgr.Auth(user.acctID, msgBytes, sigBytes)
	if err != nil {
		t.Fatalf("unexpected auth error: %v", err)
	}

	foreigner := tNewUser(t)
	sig, err = foreigner.privKey.Sign(msgBytes)
	if err != nil {
		t.Fatalf("foreigner signing error: %v", err)
	}
	sigBytes = sig.Serialize()
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
	// Test a Sign error
	rig.signer.err = fmt.Errorf("test error 2")
	s := &tSignable{b: randBytes(25)}
	err := rig.mgr.Sign(s)
	if err == nil {
		t.Fatalf("no error for forced key signing error")
	}
	if !strings.Contains(err.Error(), "test error 2") {
		t.Fatalf("wrong error for forced key signing error: %v", err)
	}

	rig.signer.err = nil
	sig1 := tNewUser(t).randomSignature()
	sig1Bytes := sig1.Serialize()
	rig.signer.sig = sig1
	s = &tSignable{b: randBytes(25)}
	err = rig.mgr.Sign(s)
	if err != nil {
		t.Fatalf("unexpected error for valid signable: %v", err)
	}
	if !bytes.Equal(sig1Bytes, s.SigBytes()) {
		t.Fatalf("incorrect signature. expected %x, got %x", sig1, s.SigBytes())
	}

	// Try two at a time
	s2 := &tSignable{b: randBytes(25)}
	err = rig.mgr.Sign(s, s2)
	if err != nil {
		t.Fatalf("error for multiple signables: %v", err)
	}
}

func TestSend(t *testing.T) {
	user := tNewUser(t)
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
	rig.mgr.Send(foreigner.acctID, resp)
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

	// TODO: RequestWhenConnected
}

func TestPenalize(t *testing.T) {
	user := tNewUser(t)
	connectUser(t, user)
	foreigner := tNewUser(t)

	// Cannot set account as suspended in the clients map if they are not
	// connected, but should still suspend in DB.
	rig.mgr.Penalize(foreigner.acctID, 0)
	var zeroAcct account.AccountID
	// if rig.storage.closedID != zeroAcct {
	// 	t.Fatalf("foreigner penalty stored")
	// }
	rig.mgr.Penalize(user.acctID, 0)
	if rig.storage.closedID != user.acctID {
		t.Fatalf("penalty not stored")
	}
	rig.storage.closedID = zeroAcct
	if user.conn.banished {
		t.Fatalf("penalized user should not be banished")
	}

	// The user should remain in the map to finish their work.
	if rig.mgr.user(user.acctID) == nil {
		t.Fatalf("penalized user should not be removed from map")
	}
}

func TestConnectErrors(t *testing.T) {
	user := tNewUser(t)
	rig.storage.acct = nil

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

	// user unknown to storage
	encodeMsg()
	rpcErr = rig.mgr.handleConnect(user.conn, msg)
	ensureErr(rpcErr, "account unknown to storage", msgjson.NoAccountFoundError)
	rig.storage.acct = &account.Account{ID: user.acctID, PubKey: user.privKey.PubKey()}

	// User unpaid
	encodeMsg()
	rig.storage.unpaid = true
	rpcErr = rig.mgr.handleConnect(user.conn, msg)
	ensureErr(rpcErr, "account unpaid", msgjson.AuthenticationError)
	rig.storage.unpaid = false

	// bad signature
	connect.SetSig([]byte{0x09, 0x08})
	encodeMsg()
	rpcErr = rig.mgr.handleConnect(user.conn, msg)
	ensureErr(rpcErr, "bad signature", msgjson.SignatureError)

	// A send error should not return an error, but the client should not be
	// saved to the map.
	// need to "register" the user first
	msgBytes := connect.Serialize()
	sig, err := user.privKey.Sign(msgBytes)
	if err != nil {
		t.Fatalf("error signing 'connect': %v", err)
	}
	connect.SetSig(sig.Serialize())
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
	time.Sleep(time.Millisecond) // expire Timer func run in goroutine
	client.mtx.Lock()
	if len(client.respHandlers) != 0 {
		t.Fatalf("expected 0 response handlers, found %d", len(client.respHandlers))
	}
	if client.respHandlers[newID] != nil {
		t.Fatalf("response handler should have been expired")
	}
	client.mtx.Unlock()

	// After logging a new request, there should still only be one. A short
	// sleep is added because the cleanup is run as a goroutine.
	newID = comms.NextID()
	client.logReq(newID, func(comms.Link, *msgjson.Message) {}, time.Hour, noop)
	time.Sleep(time.Millisecond) // expire Timer func run in goroutine
	client.mtx.Lock()
	if len(client.respHandlers) != 1 {
		t.Fatalf("expected 1 response handler, found %d", len(client.respHandlers))
	}
	if client.respHandlers[newID] == nil {
		t.Fatalf("wrong response handler left after cleanup cycle")
	}
	client.mtx.Unlock()
}

func TestHandleRegister(t *testing.T) {
	user := tNewUser(t)
	dummyError := fmt.Errorf("test error")

	newReg := func() *msgjson.Register {
		reg := &msgjson.Register{
			PubKey: user.privKey.PubKey().SerializeCompressed(),
			Time:   encode.UnixMilliU(unixMsNow()),
		}
		sigMsg := reg.Serialize()
		sig, _ := user.privKey.Sign(sigMsg)
		reg.SetSig(sig.Serialize())
		return reg
	}

	newMsg := func(reg *msgjson.Register) *msgjson.Message {
		msg, err := msgjson.NewRequest(comms.NextID(), msgjson.RegisterRoute, reg)
		if err != nil {
			t.Fatalf("NewRequest error: %v", err)
		}
		return msg
	}

	do := func(msg *msgjson.Message) *msgjson.Error {
		return rig.mgr.handleRegister(user.conn, msg)
	}

	ensureErr := makeEnsureErr(t)

	msg := newMsg(newReg())
	msg.Payload = []byte(`?`)
	ensureErr(do(msg), "bad payload", msgjson.RPCParseError)

	reg := newReg()
	reg.PubKey = []byte(`abcd`)
	ensureErr(do(newMsg(reg)), "bad pubkey", msgjson.PubKeyParseError)

	// Signature error
	reg = newReg()
	reg.Sig = []byte{0x01, 0x02}
	ensureErr(do(newMsg(reg)), "bad signature", msgjson.SignatureError)

	// storage.CreateAccount error
	msg = newMsg(newReg())
	rig.storage.acctErr = dummyError
	ensureErr(do(msg), "CreateAccount error", msgjson.RPCInternalError)
	rig.storage.acctErr = nil

	// Sign error
	rig.signer.err = dummyError
	ensureErr(do(msg), "DEX signature error", msgjson.RPCInternalError)
	rig.signer.err = nil

	rig.signer.sig = user.randomSignature()

	// Send a valid registration and check the response.
	// Before starting, make sure there are no responses in the queue.
	respMsg := user.conn.getSend()
	if respMsg != nil {
		b, _ := json.Marshal(respMsg)
		t.Fatalf("unexpected response: %s", string(b))
	}
	rpcErr := do(msg)
	if rpcErr != nil {
		t.Fatalf("error for valid registration: %s", rpcErr.Message)
	}
	respMsg = user.conn.getSend()
	if respMsg == nil {
		t.Fatalf("no register response")
	}
	resp, _ := respMsg.Response()
	regRes := new(msgjson.RegisterResult)
	err := json.Unmarshal(resp.Result, regRes)
	if err != nil {
		t.Fatalf("error unmarshaling payload")
	}

	if regRes.DEXPubKey.String() != tDexPubKeyHex {
		t.Fatalf("wrong DEX pubkey. expected %s, got %s", tDexPubKeyHex, regRes.DEXPubKey.String())
	}
	if regRes.Address != tFeeAddr {
		t.Fatalf("wrong fee address. expected %s, got %s", tFeeAddr, regRes.Address)
	}
	if regRes.Fee != tRegFee {
		t.Fatalf("wrong fee. expected %d, got %d", tRegFee, regRes.Fee)
	}
}

func TestHandleReinstate(t *testing.T) {
	user := tNewUser(t)
	dummyError := fmt.Errorf("test error")
	clientPubKey := user.privKey.PubKey().SerializeCompressed()
	DEXPrivKey, _ := secp256k1.GeneratePrivateKey()
	DEXPubKey := DEXPrivKey.PubKey()

	userAcct := &account.Account{
		ID:     account.NewID(clientPubKey),
		PubKey: DEXPubKey,
	}

	rig.storage.acct = userAcct

	coinid := []byte{
		0xe2, 0x48, 0xd9, 0xea, 0xa1, 0xc4, 0x78, 0xd5, 0x31, 0xc2, 0x41, 0xb4,
		0x5b, 0x7b, 0xd5, 0x8d, 0x7a, 0x06, 0x1a, 0xc6, 0x89, 0x0a, 0x86, 0x2b,
		0x1e, 0x59, 0xb3, 0xc8, 0xf6, 0xad, 0xee, 0xc8, 0x00, 0x00, 0x00, 0x32,
	}

	stamp := encode.UnixMilliU(unixMsNow())
	notify := &msgjson.NotifyFee{
		AccountID: userAcct.ID[:],
		CoinID:    coinid,
		Time:      stamp,
	}
	notifyMsg := notify.Serialize()

	notifySig, _ := DEXPrivKey.Sign(notifyMsg)

	var mgrSingerError error = nil

	holdSigner := rig.mgr.signer

	mgrSigner := &TSigner{
		sig:    notifySig,
		err:    mgrSingerError,
		pubkey: DEXPubKey,
	}

	rig.mgr.signer = mgrSigner

	newReinstate := func() *msgjson.Reinstate {
		host := "host"
		accountProof := msgjson.AccountProof{
			Host:  host,
			Stamp: stamp,
			Sig:   notifySig.Serialize(),
		}
		reinstate := &msgjson.Reinstate{
			ClientPubKey: clientPubKey,
			AccountProof: accountProof.Encode(),
			CoinID:       coinid,
			Time:         encode.UnixMilliU(unixMsNow()),
		}
		sigMsg := reinstate.Serialize()
		sig, _ := user.privKey.Sign(sigMsg)
		reinstate.SetSig(sig.Serialize())
		return reinstate
	}

	newMsg := func(reg *msgjson.Reinstate) *msgjson.Message {
		msg, err := msgjson.NewRequest(comms.NextID(), msgjson.ReinstateRoute, reg)
		if err != nil {
			t.Fatalf("NewRequest error: %v", err)
		}
		return msg
	}

	do := func(msg *msgjson.Message) *msgjson.Error {
		return rig.mgr.handleReinstate(user.conn, msg)
	}

	ensureErr := makeEnsureErr(t)

	msg := newMsg(newReinstate())
	msg.Payload = []byte(`?`)
	ensureErr(do(msg), "bad payload", msgjson.RPCParseError)

	reinstate := newReinstate()
	reinstate.ClientPubKey = []byte(`abcd`)
	ensureErr(do(newMsg(reinstate)), "bad pubkey", msgjson.PubKeyParseError)

	// Signature error
	reinstate = newReinstate()
	reinstate.Sig = []byte{0x01, 0x02}
	ensureErr(do(newMsg(reinstate)), "bad signature", msgjson.SignatureError)

	// storage.CreateAccount error
	msg = newMsg(newReinstate())
	rig.storage.acctErr = dummyError
	ensureErr(do(msg), "CreateAccount error", msgjson.RPCInternalError)
	rig.storage.acctErr = nil

	// Sign error
	mgrSigner.err = dummyError
	ensureErr(do(msg), "DEX signature error", msgjson.RPCInternalError)
	mgrSigner.err = nil

	// Send a valid registration and check the response.
	// Before starting, make sure there are no responses in the queue.

	respMsg := user.conn.getSend()
	if respMsg != nil {
		b, _ := json.Marshal(respMsg)
		t.Fatalf("unexpected response: %s", string(b))
	}
	msg = newMsg(newReinstate())
	rpcErr := do(msg)
	if rpcErr != nil {
		t.Fatalf("error for valid registration: %s", rpcErr.Message)
	}
	respMsg = user.conn.getSend()
	if respMsg == nil {
		t.Fatalf("no register response")
	}
	resp, _ := respMsg.Response()
	regRes := new(msgjson.RegisterResult)
	err := json.Unmarshal(resp.Result, regRes)
	if err != nil {
		t.Fatalf("error unmarshaling payload")
	}

	expectedRegisterResult := &msgjson.RegisterResult{
		DEXPubKey: DEXPubKey.SerializeCompressed(),
	}

	if regRes.DEXPubKey.String() != expectedRegisterResult.DEXPubKey.String() {
		t.Fatalf("wrong DEX pubkey. expected %s, got %s", expectedRegisterResult.DEXPubKey.String(), regRes.DEXPubKey.String())
	}

	rig.mgr.signer = holdSigner
}

func TestHandleNotifyFee(t *testing.T) {
	user := tNewUser(t)
	userAcct := &account.Account{ID: user.acctID, PubKey: user.privKey.PubKey()}
	rig.storage.acct = userAcct
	dummyError := fmt.Errorf("test error")
	coinid := []byte{
		0xe2, 0x48, 0xd9, 0xea, 0xa1, 0xc4, 0x78, 0xd5, 0x31, 0xc2, 0x41, 0xb4,
		0x5b, 0x7b, 0xd5, 0x8d, 0x7a, 0x06, 0x1a, 0xc6, 0x89, 0x0a, 0x86, 0x2b,
		0x1e, 0x59, 0xb3, 0xc8, 0xf6, 0xad, 0xee, 0xc8, 0x00, 0x00, 0x00, 0x32,
	}

	defer func() {
		rig.storage.unpaid = false
	}()
	defer resetStorage()

	newNotify := func() *msgjson.NotifyFee {
		notify := &msgjson.NotifyFee{
			AccountID: user.acctID[:],
			CoinID:    coinid,
			Time:      encode.UnixMilliU(unixMsNow()),
		}
		sigMsg := notify.Serialize()
		sig, _ := user.privKey.Sign(sigMsg)
		notify.SetSig(sig.Serialize())
		return notify
	}

	newMsg := func(notify *msgjson.NotifyFee) *msgjson.Message {
		msg, err := msgjson.NewRequest(comms.NextID(), msgjson.NotifyFeeRoute, notify)
		if err != nil {
			t.Fatalf("NewRequest error: %v", err)
		}
		return msg
	}

	do := func(msg *msgjson.Message) *msgjson.Error {
		return rig.mgr.handleNotifyFee(user.conn, msg)
	}

	// When the process makes it to the chainwaiter, it needs to be handled by
	// getting the Send message. While the chainwaiter can run asynchronously,
	// the first attempt is actually synchronous, so not need for synchronization
	// as long as there is no PayFee error.
	doWaiter := func(msg *msgjson.Message) *msgjson.Error {
		msgErr := rig.mgr.handleNotifyFee(user.conn, msg)
		if msgErr != nil {
			t.Fatal("never made it to the waiter", msgErr.Code, msgErr.Message)
		}
		sent := user.conn.getSend()
		if sent == nil {
			return nil
		}
		resp, err := sent.Response()
		if err != nil {
			t.Fatalf("non-response found")
		}
		return resp.Error
	}

	ensureErr := makeEnsureErr(t)

	msg := newMsg(newNotify())
	msg.Payload = []byte(`?`)
	ensureErr(do(msg), "bad payload", msgjson.RPCParseError)

	notify := newNotify()
	notify.AccountID = []byte{0x01, 0x02}
	msg = newMsg(notify)
	ensureErr(do(msg), "bad account ID", msgjson.AuthenticationError)

	// missing account
	rig.storage.acct = nil
	ensureErr(do(newMsg(newNotify())), "bad account ID", msgjson.AuthenticationError)
	rig.storage.acct = userAcct

	// account already paid
	ensureErr(do(newMsg(newNotify())), "already paid", msgjson.AuthenticationError)

	// Signature error
	rig.storage.unpaid = true // notifyfee cannot be sent for paid account
	notify = newNotify()
	notify.Sig = []byte{0x01, 0x02}
	ensureErr(do(newMsg(notify)), "bad signature", msgjson.SignatureError)

	goodNotify := newNotify()
	goodMsg := newMsg(goodNotify)
	rig.storage.regErr = dummyError
	ensureErr(do(goodMsg), "AccountRegAddr", msgjson.RPCInternalError)
	rig.storage.regErr = nil

	tCheckFeeVal -= 1
	ensureErr(doWaiter(goodMsg), "low fee", msgjson.FeeError)
	tCheckFeeVal += 1

	ogAddr := tCheckFeeAddr
	tCheckFeeAddr = "dummy address"
	ensureErr(doWaiter(goodMsg), "wrong address", msgjson.FeeError)
	tCheckFeeAddr = ogAddr

	rig.storage.payErr = dummyError
	ensureErr(doWaiter(goodMsg), "PayAccount", msgjson.RPCInternalError)
	rig.storage.payErr = nil

	// Sign error
	rig.signer.err = dummyError
	ensureErr(doWaiter(goodMsg), "DEX signature", msgjson.RPCInternalError)
	rig.signer.err = nil
	rig.signer.sig = user.randomSignature()

	// Send a valid notifyfee, and check the response.
	rpcErr := doWaiter(goodMsg)
	if rpcErr != nil {
		t.Fatalf("error sending valid notifyfee: %s", rpcErr.Message)
	}
}

func TestAuthManager_RecordCancel_RecordCompletedOrder(t *testing.T) {
	resetStorage()
	user := tNewUser(t)
	connectUser(t, user)

	client := rig.mgr.user(user.acctID)
	if client == nil {
		t.Fatalf("client not found")
	}

	newOrderID := func() (oid order.OrderID) {
		rand.Read(oid[:])
		return
	}

	oid := newOrderID()
	tCompleted := unixMsNow()
	rig.mgr.RecordCompletedOrder(user.acctID, oid, tCompleted)

	client.mtx.Lock()
	total, cancels := client.recentOrders.counts()
	client.mtx.Unlock()
	if total != 1 {
		t.Errorf("got %d total orders, expected %d", total, 1)
	}
	if cancels != 0 {
		t.Errorf("got %d cancels, expected %d", cancels, 0)
	}

	checkOrd := func(ord *oidStamped, oid order.OrderID, cancel bool, timestamp int64) {
		if ord.OrderID != oid {
			t.Errorf("completed order id mismatch. got %v, expected %v",
				ord.OrderID, oid)
		}
		isCancel := ord.target != nil
		if isCancel != cancel {
			t.Errorf("order marked as cancel=%v, expected %v", isCancel, cancel)
		}
		//tMS := encode.UnixMilli(tCompleted)
		if ord.time != timestamp {
			t.Errorf("completed order time mismatch. got %v, expected %v",
				ord.time, timestamp)
		}
	}

	client.mtx.Lock()
	ord := client.recentOrders.orders[0]
	client.mtx.Unlock()
	checkOrd(ord, oid, false, encode.UnixMilli(tCompleted))

	// another
	oid = newOrderID()
	tCompleted = tCompleted.Add(time.Millisecond) // newer
	rig.mgr.RecordCompletedOrder(user.acctID, oid, tCompleted)

	client.mtx.Lock()
	total, cancels = client.recentOrders.counts()
	client.mtx.Unlock()
	if total != 2 {
		t.Errorf("got %d total orders, expected %d", total, 2)
	}
	if cancels != 0 {
		t.Errorf("got %d cancels, expected %d", cancels, 0)
	}

	client.mtx.Lock()
	ord = client.recentOrders.orders[1]
	client.mtx.Unlock()
	checkOrd(ord, oid, false, encode.UnixMilli(tCompleted))

	// now a cancel
	coid := newOrderID()
	tCompleted = tCompleted.Add(time.Millisecond) // newer
	rig.mgr.RecordCancel(user.acctID, coid, oid, tCompleted)

	client.mtx.Lock()
	total, cancels = client.recentOrders.counts()
	client.mtx.Unlock()
	if total != 3 {
		t.Errorf("got %d total orders, expected %d", total, 3)
	}
	if cancels != 1 {
		t.Errorf("got %d cancels, expected %d", cancels, 1)
	}

	client.mtx.Lock()
	ord = client.recentOrders.orders[2]
	client.mtx.Unlock()
	checkOrd(ord, coid, true, encode.UnixMilli(tCompleted))
}
