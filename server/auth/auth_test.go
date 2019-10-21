// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package auth

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/decred/dcrd/dcrec/secp256k1/v2"
	"github.com/decred/dcrdex/server/account"
	"github.com/decred/dcrdex/server/comms"
	"github.com/decred/dcrdex/server/comms/msgjson"
	"github.com/decred/dcrdex/server/order"
)

func randBytes(l int) []byte {
	b := make([]byte, l)
	rand.Read(b)
	return b
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
}

func (s *TStorage) CloseAccount(id account.AccountID, _ account.Rule) { s.closedID = id }
func (s *TStorage) Account(account.AccountID) (*account.Account, bool, bool) {
	return s.acct, !s.unpaid, !s.closed
}
func (s *TStorage) ActiveMatches(account.AccountID) []*order.UserMatch { return s.matches }
func (s *TStorage) CreateAccount(*account.Account) (string, error)     { return s.acctAddr, s.acctErr }
func (s *TStorage) AccountRegAddr(account.AccountID) (string, error)   { return s.regAddr, s.regErr }
func (s *TStorage) PayAccount(account.AccountID, string, uint32) error { return s.payErr }

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
	sendErr    error
	requestErr error
	banished   bool
	sends      []*msgjson.Message
	reqs       []*tReq
}

func (c *TRPCClient) ID() uint64 { return c.id }
func (c *TRPCClient) Send(msg *msgjson.Message) error {
	c.sends = append(c.sends, msg)
	return c.sendErr
}
func (c *TRPCClient) Request(msg *msgjson.Message, f func(comms.Link, *msgjson.Message)) error {
	c.reqs = append(c.reqs, &tReq{
		msg:      msg,
		respFunc: f,
	})
	return c.requestErr
}
func (c *TRPCClient) Banish() { c.banished = true }
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
	return &TRPCClient{id: tClientID}
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

type testRig struct {
	mgr     *AuthManager
	storage *TStorage
	signer  *TSigner
}

var rig *testRig

type tSignable struct {
	b   []byte
	sig []byte
	err error
}

func (s *tSignable) SetSig(b []byte)  { s.sig = b }
func (s *tSignable) SigBytes() []byte { return s.sig }
func (s *tSignable) Serialize() ([]byte, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.b, nil
}

func tNewConnect(user *tUser) *msgjson.Connect {
	return &msgjson.Connect{
		AccountID:  user.acctID[:],
		APIVersion: 0,
		Time:       uint64(time.Now().Unix()),
	}
}

func extractConnectResponse(t *testing.T, msg *msgjson.Message) *msgjson.ConnectResponse {
	if msg == nil {
		t.Fatalf("no response from 'connect' request")
	}
	resp, _ := msg.Response()
	result := new(msgjson.ConnectResponse)
	err := json.Unmarshal(resp.Result, result)
	if err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	return result
}

func queueUser(t *testing.T, user *tUser) *msgjson.Message {
	rig.storage.acct = &account.Account{ID: user.acctID, PubKey: user.privKey.PubKey()}
	connect := tNewConnect(user)
	sigMsg, err := connect.Serialize()
	if err != nil {
		t.Fatalf("'connect' serialization error: %v", err)
	}
	sig, err := user.privKey.Sign(sigMsg)
	if err != nil {
		t.Fatalf("error signing message of length %d", len(sigMsg))
	}
	connect.SetSig(sig.Serialize())
	msg, _ := msgjson.NewRequest(comms.NextID(), msgjson.ConnectRoute, connect)
	return msg
}

func connectUser(t *testing.T, user *tUser) {
	connect := queueUser(t, user)
	rig.mgr.handleConnect(user.conn, connect)

	// Check the response.
	respMsg := user.conn.getSend()
	if respMsg == nil {
		t.Fatalf("no response from 'connect' request")
	}
	if respMsg.ID != connect.ID {
		t.Fatalf("'connect' response has wrong ID. expected %d, got %d", connect.ID, respMsg.ID)
	}
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

func tCheckFee(txid string, vout uint32) (addr string, val uint64, confs int64, err error) {
	return tCheckFeeAddr, tCheckFeeVal, tCheckFeeConfs, tCheckFeeErr
}

const (
	tRegFee       uint64 = 500_000_000
	tDexPubKeyHex        = "032e3678f9889206dcea4fc281556c9e543c5d5ffa7efe8d11118b52e29c773f27"
	tFeeAddr             = "Dcur2mcGjmENx4DhNqDctW5wJCVyT3Qeqkx"
)

var tDexPubKeyBytes = []byte{
	0x03, 0x2e, 0x36, 0x78, 0xf9, 0x88, 0x92, 0x06, 0xdc, 0xea, 0x4f, 0xc2,
	0x81, 0x55, 0x6c, 0x9e, 0x54, 0x3c, 0x5d, 0x5f, 0xfa, 0x7e, 0xfe, 0x8d,
	0x11, 0x11, 0x8b, 0x52, 0xe2, 0x9c, 0x77, 0x3f, 0x27,
}

func TestMain(m *testing.M) {
	storage := &TStorage{acctAddr: tFeeAddr, regAddr: tCheckFeeAddr}
	dexKey, _ := secp256k1.ParsePubKey(tDexPubKeyBytes)
	signer := &TSigner{pubkey: dexKey}
	rig = &testRig{
		storage: storage,
		signer:  signer,
		mgr: NewAuthManager(&Config{
			Storage:         storage,
			Signer:          signer,
			StartEpoch:      1,
			RegistrationFee: tRegFee,
			FeeConfs:        tCheckFeeConfs,
			FeeChecker:      tCheckFee,
		}),
	}
	os.Exit(m.Run())
}

func TestConnect(t *testing.T) {
	user := tNewUser(t)
	connectUser(t, user)

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
	rig.mgr.Request(user.acctID, msg, func(*msgjson.Message) {
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
	rig.mgr.Request(reuser.acctID, msg, func(*msgjson.Message) {})
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
		Time:     123456789,
		Status:   order.NewlyMatched,
		Side:     order.Maker,
	}
	rig.storage.matches = []*order.UserMatch{userMatch}
	rig.mgr.handleConnect(user.conn, connect)
	rig.storage.matches = nil

	// Check the response.
	respMsg := user.conn.getSend()
	result := extractConnectResponse(t, respMsg)
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
	if match.Time != userMatch.Time {
		t.Fatal("wrong Time: ", match.Time, " != ", userMatch.OrderID)
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

	// closed account
	rig.storage.closed = true
	rpcErr = rig.mgr.handleConnect(user.conn, connect)
	rig.storage.closed = false
	if rpcErr == nil {
		t.Fatalf("no error for closed account")
	}
	if rpcErr.Code != msgjson.AuthenticationError {
		t.Fatalf("wrong error for closed account. wanted %d, got %d",
			msgjson.AuthenticationError, rpcErr.Code)
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
	// Test a serialization error
	s := &tSignable{err: fmt.Errorf("test error")}
	err := rig.mgr.Sign(s)
	if err == nil {
		t.Fatalf("no Sign error for signable serialization error")
	}
	if !strings.Contains(err.Error(), "test error") {
		t.Fatalf("wrong error: %v", err)
	}

	// Test a Sign error
	rig.signer.err = fmt.Errorf("test error 2")
	s = &tSignable{b: randBytes(25)}
	err = rig.mgr.Sign(s)
	if err == nil {
		t.Fatalf("no error for forced key signing error")
	}
	if !strings.Contains(err.Error(), "test error 2") {
		t.Fatalf("wrong error for forced key signing error: %v", err)
	}

	// Do a single signable
	rig.signer.err = nil
	sigMsg1 := randBytes(73)
	privKey := tNewUser(t).privKey
	sig1, err := privKey.Sign(sigMsg1)
	if err != nil {
		t.Fatalf("signing error: %v", err)
	}
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
	rig.mgr.Request(foreigner.acctID, req, func(*msgjson.Message) {})
	if foreigner.conn.getReq() != nil {
		t.Fatalf("request magically got through to foreigner")
	}
	if user.conn.getReq() != nil {
		t.Fatalf("foreigner request sent to authed user")
	}

	// Send a request to an authed user.
	rig.mgr.Request(user.acctID, req, func(*msgjson.Message) {})
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

func TestPenalize(t *testing.T) {
	user := tNewUser(t)
	connectUser(t, user)
	foreigner := tNewUser(t)

	rig.mgr.Penalize(foreigner.acctID, 0)
	var zeroAcct account.AccountID
	if rig.storage.closedID != zeroAcct {
		t.Fatalf("foreigner penalty stored")
	}
	rig.mgr.Penalize(user.acctID, 0)
	if rig.storage.closedID != user.acctID {
		t.Fatalf("penalty not stored")
	}
	rig.storage.closedID = zeroAcct
	if !user.conn.banished {
		t.Fatalf("penalized user not banished")
	}

	// The user should not be in the map
	if rig.mgr.user(user.acctID) != nil {
		t.Fatalf("penalized user not removed from map")
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
	ensureErr(rpcErr, "account unknown to storage", msgjson.AuthenticationError)
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
	msgBytes, err := connect.Serialize()
	if err != nil {
		t.Fatalf("error serializing 'connect': %v", err)
	}
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
	client.respHandlers = map[uint64]*respHandler{
		comms.NextID(): &respHandler{
			expiration: time.Now(),
			f:          func(*msgjson.Message) {},
		},
	}
	// After logging a new request, there should still only be one. A short
	// sleep is added because the cleanup is run as a goroutine.
	newID := comms.NextID()
	client.logReq(newID, &respHandler{
		expiration: time.Now().Add(reqExpiration),
		f:          func(*msgjson.Message) {},
	})
	time.Sleep(time.Millisecond)
	client.mtx.Lock()
	defer client.mtx.Unlock()
	if len(client.respHandlers) != 1 {
		t.Fatalf("expected 1 response handler, found %d", len(client.respHandlers))
	}
	if client.respHandlers[newID] == nil {
		t.Fatalf("wrong response handler left after cleanup cycle")
	}
}

func TestHandleRegister(t *testing.T) {
	user := tNewUser(t)
	dummyError := fmt.Errorf("test error")

	newReg := func() *msgjson.Register {
		reg := &msgjson.Register{
			PubKey: user.privKey.PubKey().SerializeCompressed(),
			Time:   uint64(time.Now().Unix()),
		}
		sigMsg, _ := reg.Serialize()
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

func TestHandleNotifyFee(t *testing.T) {
	user := tNewUser(t)
	userAcct := &account.Account{ID: user.acctID, PubKey: user.privKey.PubKey()}
	rig.storage.acct = userAcct
	dummyError := fmt.Errorf("test error")
	txidb := []byte{
		0xe2, 0x48, 0xd9, 0xea, 0xa1, 0xc4, 0x78, 0xd5, 0x31, 0xc2, 0x41, 0xb4,
		0x5b, 0x7b, 0xd5, 0x8d, 0x7a, 0x06, 0x1a, 0xc6, 0x89, 0x0a, 0x86, 0x2b,
		0x1e, 0x59, 0xb3, 0xc8, 0xf6, 0xad, 0xee, 0xc8,
	}
	// txid := "e248d9eaa1c478d531c241b45b7bd58d7a061ac6890a862b1e59b3c8f6adeec8"
	vout := uint32(50)

	newNotify := func() *msgjson.NotifyFee {
		notify := &msgjson.NotifyFee{
			AccountID: user.acctID[:],
			TxID:      txidb,
			Vout:      vout,
			Time:      uint64(time.Now().Unix()),
		}
		sigMsg, _ := notify.Serialize()
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
	rig.storage.unpaid = true

	// Signature error
	notify = newNotify()
	notify.Sig = []byte{0x01, 0x02}
	ensureErr(do(newMsg(notify)), "bad signature", msgjson.SignatureError)

	goodNotify := newNotify()
	goodMsg := newMsg(goodNotify)
	rig.storage.regErr = dummyError
	ensureErr(do(goodMsg), "AccountRegAddr", msgjson.RPCInternalError)
	rig.storage.regErr = nil

	tCheckFeeErr = dummyError
	ensureErr(do(goodMsg), "checkFee", msgjson.FeeError)
	tCheckFeeErr = nil

	tCheckFeeVal -= 1
	ensureErr(do(goodMsg), "low fee", msgjson.FeeError)
	tCheckFeeVal += 1

	tCheckFeeConfs -= 1
	ensureErr(do(goodMsg), "unconfirmed", msgjson.FeeError)
	tCheckFeeConfs += 1

	ogAddr := tCheckFeeAddr
	tCheckFeeAddr = "dummy address"
	ensureErr(do(goodMsg), "wrong address", msgjson.FeeError)
	tCheckFeeAddr = ogAddr

	rig.storage.payErr = dummyError
	ensureErr(do(goodMsg), "PayAccount", msgjson.RPCInternalError)
	rig.storage.payErr = nil

	// Sign error
	rig.signer.err = dummyError
	ensureErr(do(goodMsg), "DEX signature", msgjson.RPCInternalError)
	rig.signer.err = nil

	// Send a valid notifyfee, and check the response.
	rpcErr := do(goodMsg)
	if rpcErr != nil {
		t.Fatalf("error sending valid notifyfee: %s", rpcErr.Message)
	}
}
