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
)

func randBytes(l int) []byte {
	b := make([]byte, l)
	rand.Read(b)
	return b
}

// TStorage satisfies the Storage interface
type TStorage struct {
	acct     *account.Account
	matches  []msgjson.Match
	closedID account.AccountID
}

func (s *TStorage) CloseAccount(id account.AccountID, _ account.Rule) { s.closedID = id }
func (s *TStorage) Account(account.AccountID) *account.Account        { return s.acct }
func (s *TStorage) ActiveMatches(account.AccountID) []msgjson.Match   { return s.matches }

// TSigner satisfies the Signer interface
type TSigner struct {
	sig *secp256k1.Signature
	err error
}

func (s *TSigner) Sign(hash []byte) (*secp256k1.Signature, error) { return s.sig, s.err }

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
		user := tNewUser(t)
		connectUser(t, user)
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

func connectUser(t *testing.T, user *tUser) {
	rig.storage.acct = &account.Account{ID: user.acctID, PubKey: user.privKey.PubKey()}
	reqID := comms.NextID()
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
	msg, _ := msgjson.NewRequest(reqID, msgjson.ConnectRoute, connect)
	rig.mgr.handleConnect(user.conn, msg)

	// Check the response.
	resp := user.conn.getSend()
	if resp == nil {
		t.Fatalf("no response from 'connect' request")
	}
	if resp.ID != reqID {
		t.Fatalf("'connect' response has wrong ID. expected %d, got %d", reqID, resp.ID)
	}
}

func TestMain(m *testing.M) {
	storage := &TStorage{}
	signer := &TSigner{}
	rig = &testRig{
		storage: storage,
		signer:  signer,
		mgr: NewAuthManager(&Config{
			Storage:    storage,
			Signer:     signer,
			StartEpoch: 1,
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
	resp, err := msgjson.NewResponse(comms.NextID(), payload, nil)
	payload = &tA{A: 10}
	req, err := msgjson.NewRequest(comms.NextID(), "testroute", payload)

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
	err = json.Unmarshal(r.Result, tr)
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

	ensureErr := func(rpcErr *msgjson.Error, tag string, code int) {
		if rpcErr == nil {
			t.Fatalf("no error for %s ID", tag)
		}
		if rpcErr.Code != code {
			t.Fatalf("wrong error code for %s. expected %d, got %d",
				tag, code, rpcErr.Code)
		}
	}

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
}
