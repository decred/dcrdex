// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"decred.org/dcrdex/dex/encode"
	"decred.org/dcrdex/dex/msgjson"
	"decred.org/dcrdex/dex/order"
	"decred.org/dcrdex/dex/wait"
	"decred.org/dcrdex/server/account"
	"decred.org/dcrdex/server/comms"
	"github.com/decred/dcrd/dcrec/secp256k1/v2"
)

const cancelThreshWindow = 25 // spec

func unixMsNow() time.Time {
	return time.Now().Truncate(time.Millisecond).UTC()
}

// Storage updates and fetches account-related data from what is presumably a
// database.
type Storage interface {
	// CloseAccount closes the account for violation of the specified rule.
	CloseAccount(account.AccountID, account.Rule)
	// Account retrieves account info for the specified account ID.
	Account(account.AccountID) (acct *account.Account, paid, open bool)
	CompletedUserOrders(aid account.AccountID, N int) (oids []order.OrderID, compTimes []int64, err error)
	ExecutedCancelsForUser(aid account.AccountID, N int) (oids, targets []order.OrderID, execTimes []int64, err error)
	// ActiveMatches fetches the account's active matches.
	ActiveMatches(account.AccountID) ([]*order.UserMatch, error)
	CreateAccount(*account.Account) (string, error)
	CreateAccountWithAddress(*account.Account, string) error
	AccountRegAddr(account.AccountID) (string, error)
	PayAccount(account.AccountID, []byte) error
}

// Signer signs messages. It is likely a secp256k1.PrivateKey.
type Signer interface {
	Sign(hash []byte) (*secp256k1.Signature, error)
	PubKey() *secp256k1.PublicKey
}

// FeeChecker is a function for retrieving the details for a fee payment. It
// is satisfied by (dcr.Backend).FeeCoin.
type FeeChecker func(coinID []byte) (addr string, val uint64, confs int64, err error)

// A respHandler is the handler for the response to a DEX-originating request. A
// respHandler has a time associated with it so that old unused handlers can be
// detected and deleted.
type respHandler struct {
	f      func(comms.Link, *msgjson.Message)
	expire *time.Timer
}

// clientInfo represents a DEX client, including account information and last
// known comms.Link.
type clientInfo struct {
	mtx          sync.Mutex
	acct         *account.Account
	conn         comms.Link
	respHandlers map[uint64]*respHandler
	recentOrders *latestOrders
	suspended    bool // penalized, disallow new orders
}

func (client *clientInfo) cancelRatio() float64 {
	client.mtx.Lock()
	total, cancels := client.recentOrders.counts()
	client.mtx.Unlock()
	// completed = total - cancels
	return float64(cancels) / float64(total)
}

func (client *clientInfo) suspend() {
	client.mtx.Lock()
	client.suspended = true
	client.mtx.Unlock()
}

func (client *clientInfo) isSuspended() bool {
	client.mtx.Lock()
	defer client.mtx.Unlock()
	return client.suspended
}

func (client *clientInfo) rmHandler(id uint64) bool {
	client.mtx.Lock()
	defer client.mtx.Unlock()
	_, found := client.respHandlers[id]
	if found {
		delete(client.respHandlers, id)
	}
	return found
}

// logReq associates the specified response handler with the message ID.
func (client *clientInfo) logReq(id uint64, f func(comms.Link, *msgjson.Message), expireTime time.Duration, expire func()) {
	client.mtx.Lock()
	defer client.mtx.Unlock()
	doExpire := func() {
		// Delete the response handler, and call the provided expire function if
		// (*clientInfo).respHandler has not already retrieved the handler
		// function for execution.
		if client.rmHandler(id) {
			expire()
		}
	}
	client.respHandlers[id] = &respHandler{
		f:      f,
		expire: time.AfterFunc(expireTime, doExpire),
	}
}

// respHandler extracts the response handler from the respHandlers map. If the
// handler is found, it is also deleted from the map before being returned, and
// the expiration Timer is stopped.
func (client *clientInfo) respHandler(id uint64) *respHandler {
	client.mtx.Lock()
	defer client.mtx.Unlock()

	handler := client.respHandlers[id]
	if handler == nil {
		return nil
	}

	// Stop the expiration Timer. If the Timer fired after respHandler was
	// called, but we found the response handler in the map, clientInfo.expire
	// is waiting for the lock and will return false, thus preventing the
	// registered expire func from executing.
	handler.expire.Stop()
	delete(client.respHandlers, id)
	return handler
}

// AuthManager handles authentication-related tasks, including validating client
// signatures, maintaining association between accounts and `comms.Link`s, and
// signing messages with the DEX's private key. AuthManager manages requests to
// the 'connect' route.
type AuthManager struct {
	anarchy      bool
	connMtx      sync.RWMutex
	cancelThresh float64
	users        map[account.AccountID]*clientInfo
	conns        map[uint64]*clientInfo
	storage      Storage
	signer       Signer
	regFee       uint64
	checkFee     FeeChecker
	feeConfs     int64
	// latencyQ is a queue for coin waiters to deal with latency.
	latencyQ *wait.TickerQueue

	pendingRequestsMtx sync.Mutex
	pendingRequests    map[account.AccountID]map[uint64]*timedRequest

	pendingMessagesMtx sync.Mutex
	pendingMessages    map[account.AccountID]map[uint64]*timedMessage
}

// Config is the configuration settings for the AuthManager, and the only
// argument to its constructor.
type Config struct {
	// Storage is an interface for storing and retrieving account-related info.
	Storage Storage
	// Signer is an interface that signs messages. In practice, Signer is
	// satisfied by a secp256k1.PrivateKey.
	Signer Signer
	// RegistrationFee is the DEX registration fee, in atoms DCR
	RegistrationFee uint64
	// FeeConfs is the number of confirmations required on the registration fee
	// before registration can be completed with notifyfee.
	FeeConfs int64
	// FeeChecker is a method for getting the registration fee output info.
	FeeChecker FeeChecker

	CancelThreshold float64
	Anarchy         bool
}

// NewAuthManager is the constructor for an AuthManager.
func NewAuthManager(cfg *Config) *AuthManager {
	auth := &AuthManager{
		anarchy:         cfg.Anarchy,
		users:           make(map[account.AccountID]*clientInfo),
		conns:           make(map[uint64]*clientInfo),
		storage:         cfg.Storage,
		signer:          cfg.Signer,
		regFee:          cfg.RegistrationFee,
		checkFee:        cfg.FeeChecker,
		feeConfs:        cfg.FeeConfs,
		cancelThresh:    cfg.CancelThreshold,
		latencyQ:        wait.NewTickerQueue(recheckInterval),
		pendingRequests: make(map[account.AccountID]map[uint64]*timedRequest),
		pendingMessages: make(map[account.AccountID]map[uint64]*timedMessage),
	}

	comms.Route(msgjson.ConnectRoute, auth.handleConnect)
	comms.Route(msgjson.RegisterRoute, auth.handleRegister)
	comms.Route(msgjson.NotifyFeeRoute, auth.handleNotifyFee)
	return auth
}

// RecordCancel records a user's executed cancel order, including the canceled
// order ID, and the time when the cancel was executed.
func (auth *AuthManager) RecordCancel(user account.AccountID, oid, target order.OrderID, t time.Time) {
	tMS := encode.UnixMilli(t)
	auth.recordOrderDone(user, oid, &target, tMS)
}

// RecordCompletedOrder records a user's completed order, where completed means
// a swap involving the order was successfully completed and the order is no
// longer on the books if it ever was.
func (auth *AuthManager) RecordCompletedOrder(user account.AccountID, oid order.OrderID, t time.Time) {
	tMS := encode.UnixMilli(t)
	auth.recordOrderDone(user, oid, nil, tMS)
}

// recordOrderDone an order that has finished processing. This can be a cancel
// order, which matched and unbooked another order, or a trade order that
// completed the swap negotiation. Note that in the case of a cancel, oid refers
// to the ID of the cancel order itself, while target is non-nil for cancel
// orders.
func (auth *AuthManager) recordOrderDone(user account.AccountID, oid order.OrderID, target *order.OrderID, tMS int64) {
	auth.connMtx.RLock()
	defer auth.connMtx.RUnlock()

	client := auth.users[user]
	if client == nil {
		log.Errorf("unknown client for user %v", user)
		return
	}

	client.mtx.Lock()
	client.recentOrders.add(&oidStamped{
		OrderID: oid,
		time:    tMS,
		target:  target,
	})
	client.mtx.Unlock()

	log.Debugf("Recorded order %v that has finished processing: user=%v, time=%v, target=%v",
		oid, user, tMS, target)

	// TODO: decide when and where to count and penalize
}

// Run runs the AuthManager until the context is canceled. Satisfies the
// dex.Runner interface.
func (auth *AuthManager) Run(ctx context.Context) {
	go auth.latencyQ.Run(ctx)
	<-ctx.Done()
	// TODO: wait for latencyQ and running comms route handlers (handleRegister, handleNotifyFee)!
}

// Route wraps the comms.Route function, storing the response handler with the
// associated clientInfo, and sending the message on the current comms.Link for
// the client.
func (auth *AuthManager) Route(route string, handler func(account.AccountID, *msgjson.Message) *msgjson.Error) {
	comms.Route(route, func(conn comms.Link, msg *msgjson.Message) *msgjson.Error {
		client := auth.conn(conn)
		if client == nil {
			return &msgjson.Error{
				Code:    msgjson.UnauthorizedConnection,
				Message: "cannot use route '" + route + "' on an unauthorized connection",
			}
		}
		return handler(client.acct.ID, msg)
	})
}

// Auth validates the signature/message pair with the users public key.
func (auth *AuthManager) Auth(user account.AccountID, msg, sig []byte) error {
	client := auth.user(user)
	if client == nil {
		return fmt.Errorf("user %x not found", user[:])
	}
	return checkSigS256(msg, sig, client.acct.PubKey)
}

// Suspended indicates the the user exists (is presently connected) and is
// suspended. This does not access the persistent storage.
func (auth *AuthManager) Suspended(user account.AccountID) (found, suspended bool) {
	client := auth.user(user)
	if client == nil {
		// TODO: consider hitting auth.storage.Account(user)
		return false, true // suspended for practical purposes
	}
	return true, client.isSuspended()
}

// Sign signs the msgjson.Signables with the DEX private key.
func (auth *AuthManager) Sign(signables ...msgjson.Signable) error {
	for _, signable := range signables {
		sigMsg := signable.Serialize()
		sig, err := auth.signer.Sign(sigMsg)
		if err != nil {
			return fmt.Errorf("signature error: %v", err)
		}
		signable.SetSig(sig.Serialize())
	}
	return nil
}

// DefaultConnectTimeout is the default timeout for a user to connect before a
// pending request or non-request message expires.
const DefaultConnectTimeout = 10 * time.Minute

// Response and notification (non-request) messages

// For notifications and responses.
type pendingMessage struct {
	user        account.AccountID
	msg         *msgjson.Message
	lastAttempt time.Time
	expireFunc  func()
}

type timedMessage struct {
	*pendingMessage
	T *time.Timer
}

// Register a pending message that will expire after connectTimeout. If the user
// connects before that time, the message will be sent to the user.
func (auth *AuthManager) addUserConnectMsg(data *pendingMessage, connectTimeout time.Duration) {
	auth.pendingMessagesMtx.Lock()
	defer auth.pendingMessagesMtx.Unlock()

	user, msgID := data.user, data.msg.ID

	tr := &timedMessage{
		pendingMessage: data,
		T: time.AfterFunc(connectTimeout, func() {
			if auth.rmUserConnectMsg(user, msgID) != nil {
				// Only run the expireFunc if the timeout removed it vs. a
				// connect (addClient call) that didn't stop the timer in time.
				data.expireFunc() // e.g. remove live ackers, penalize, log, etc.
			}
		}),
	}

	userMsgs := auth.pendingMessages[user]
	if userMsgs == nil {
		auth.pendingMessages[user] = map[uint64]*timedMessage{
			msgID: tr,
		}
		return
	}
	userMsgs[msgID] = tr
}

// Retrieve and unregister all connection messages for a given user. This should
// be used to retrieve pending messages on client connection.
func (auth *AuthManager) rmUserConnectMsgs(user account.AccountID) []*pendingMessage {
	auth.pendingMessagesMtx.Lock()
	defer auth.pendingMessagesMtx.Unlock()
	userMsgs := auth.pendingMessages[user]
	if userMsgs == nil {
		return nil
	}
	msgs := make([]*pendingMessage, len(userMsgs))
	for i, tr := range userMsgs {
		msgs[i] = tr.pendingMessage
		// Stop the timer. It's ok if already fired since this timedMessage
		// would have been removed if we lost the race with AfterFunc's call to
		// rmUserConnectMsg. Since we're here, we beat the timer.
		tr.T.Stop()
	}
	delete(auth.pendingMessages, user)
	return msgs
}

// Retrieve and unregister a pending message by user and message ID. This is
// likely to only be used by the connect timeout timer of the timedMessage
// created by addUserConnectMsg.
func (auth *AuthManager) rmUserConnectMsg(user account.AccountID, msgID uint64) *pendingMessage {
	auth.pendingMessagesMtx.Lock()
	defer auth.pendingMessagesMtx.Unlock()
	userMsgs := auth.pendingMessages[user]
	tm := userMsgs[msgID] // ok if user not found, nil map lookup is defined
	if tm == nil {
		// The timer probably fired while messages were being retrieved on
		// connect, and expire lost the race. This return value indicates to the
		// AfterFunc closure if the expireFunc should be called.
		return nil
	}
	tm.T.Stop() // ok if already fired, and it likely is, triggered by the AfterFunc
	delete(userMsgs, msgID)
	if len(userMsgs) == 0 {
		delete(auth.pendingMessages, user) // ok if user not found
	}
	return tm.pendingMessage
}

func (auth *AuthManager) send(user account.AccountID, msg *msgjson.Message, connectTimeout time.Duration, expire func()) error {
	stamp := time.Now().UTC()
	client := auth.user(user)
	if client == nil {
		if connectTimeout > 0 {
			auth.addUserConnectMsg(&pendingMessage{
				user:        user,
				msg:         msg,
				lastAttempt: stamp,
				expireFunc:  expire,
			}, connectTimeout)
			return nil
		}
		log.Errorf("Send requested for unknown user %v", user)
		return fmt.Errorf("unknown user")
	}

	err := client.conn.Send(msg)
	if err != nil {
		log.Debugf("error sending on link: %v", err)
		// Remove client asssuming connection is broken, requiring reconnect.
		auth.removeClient(client)
		if connectTimeout > 0 {
			auth.addUserConnectMsg(&pendingMessage{
				user:        user,
				msg:         msg,
				lastAttempt: stamp,
				expireFunc:  expire,
			}, connectTimeout)
			return nil
		}
	}
	return err
}

// Send sends the non-Request-type msgjson.Message to the client identified by
// the specified account ID.
func (auth *AuthManager) Send(user account.AccountID, msg *msgjson.Message) error {
	return auth.send(user, msg, 0, func() {})
}

// SendWhenConnected is like Send but if they are not already connected or if
// sending the message fails, it will create a pending message that
// handleConnect will attempt if the user does reconnect. The pending message
// remains valid for connectTimeout. There is no error return since send
// failures cause the client to be removed (assumed disconnected) and the
// message to be queued for send on subsequent connect. Error handling should be
// dealt with in the expire function as that is triggered after connectTimeout
// if the user has not connected. Each connection reduces connectTimeout.
func (auth *AuthManager) SendWhenConnected(user account.AccountID, msg *msgjson.Message, connectTimeout time.Duration, expire func()) {
	// Error is nil when connectTimeout > 0.
	_ = auth.send(user, msg, connectTimeout, expire)
}

// Requests

type pendingRequest struct {
	user            account.AccountID
	req             *msgjson.Message
	handlerFunc     func(link comms.Link, resp *msgjson.Message)
	responseTimeout time.Duration
	lastAttempt     time.Time
	expireFunc      func()
}

type timedRequest struct {
	*pendingRequest
	T *time.Timer
}

// Register a pending request that will expire after connectTimeout. If the user
// connects before that time, the request will be sent to the user.
func (auth *AuthManager) addUserConnectReq(data *pendingRequest, connectTimeout time.Duration) {
	auth.pendingRequestsMtx.Lock()
	defer auth.pendingRequestsMtx.Unlock()

	user, reqID := data.user, data.req.ID

	tr := &timedRequest{
		pendingRequest: data,
		T: time.AfterFunc(connectTimeout, func() {
			if auth.rmUserConnectReq(user, reqID) != nil {
				// Only run the expireFunc if the timeout removed it vs. a
				// connect (addClient call) that didn't stop the timer in time.
				data.expireFunc() // e.g. remove live ackers, penalize, etc.
			}
		}),
	}

	userReqs := auth.pendingRequests[user]
	if userReqs == nil {
		auth.pendingRequests[user] = map[uint64]*timedRequest{
			reqID: tr,
		}
		return
	}
	userReqs[reqID] = tr
}

// Retrieve and unregister all connection requests for a given user. This should
// be used to retrieve pending requests on client connection.
func (auth *AuthManager) rmUserConnectReqs(user account.AccountID) []*pendingRequest {
	auth.pendingRequestsMtx.Lock()
	defer auth.pendingRequestsMtx.Unlock()
	userReqs := auth.pendingRequests[user]
	if userReqs == nil {
		return nil
	}
	reqs := make([]*pendingRequest, len(userReqs))
	for i, tr := range userReqs {
		reqs[i] = tr.pendingRequest
		// Stop the timer. It's ok if already fired since this timedRequest
		// would have been removed if we lost the race with AfterFunc's call to
		// rmUserConnectReq. Since we're here, we beat the timer.
		tr.T.Stop()
	}
	delete(auth.pendingRequests, user)
	return reqs
}

// Retrieve and unregister a pending request by user and request ID. This is
// likely to only be used by the connect timeout timer of the timedRequest
// created by addUserConnectReq.
func (auth *AuthManager) rmUserConnectReq(user account.AccountID, reqID uint64) *pendingRequest {
	auth.pendingRequestsMtx.Lock()
	defer auth.pendingRequestsMtx.Unlock()
	userReqs := auth.pendingRequests[user]
	tr := userReqs[reqID] // ok if user not found, nil map lookup is defined
	if tr == nil {
		// The timer probably fired while requests were being retrieved on
		// connect, and expire lost the race. This return value indicates to the
		// AfterFunc closure if the expireFunc should be called.
		return nil
	}
	tr.T.Stop() // ok if already fired, and it likely is, triggered by the AfterFunc
	delete(userReqs, reqID)
	if len(userReqs) == 0 {
		delete(auth.pendingRequests, user) // ok if user not found
	}
	return tr.pendingRequest
}

// DefaultRequestTimeout is the default timeout for requests to wait for
// responses from connected users after the request is successfully sent.
const DefaultRequestTimeout = 30 * time.Second

func (auth *AuthManager) request(user account.AccountID, msg *msgjson.Message, f func(comms.Link, *msgjson.Message),
	expireTimeout, connectTimeout time.Duration, expire func()) error {
	stamp := time.Now().UTC()
	client := auth.user(user)
	if client == nil {
		if connectTimeout > 0 {
			auth.addUserConnectReq(&pendingRequest{
				user:            user,
				req:             msg,
				handlerFunc:     f,
				responseTimeout: expireTimeout,
				lastAttempt:     stamp,
				expireFunc:      expire,
			}, connectTimeout)
			return nil
		}
		log.Errorf("Send requested for unknown user %v", user)
		return fmt.Errorf("unknown user")
	}
	// log.Tracef("Registering '%s' request ID %d for user %v (auth clientInfo)", msg.Route, msg.ID, user)
	client.logReq(msg.ID, f, expireTimeout, expire)
	// auth.handleResponse checks clientInfo map and the found client's request
	// handler map, where the expire function should be found for msg.ID.
	err := client.conn.Request(msg, auth.handleResponse, expireTimeout, func() {})
	if err != nil {
		log.Debugf("error sending request ID %d: %v", msg.ID, err)
		// Remove the responseHandler registered by logReq and stop the expire
		// timer so that it does not eventually fire and run the expire func.
		// The caller receives a non-nil error to deal with it.
		client.respHandler(msg.ID) // drop the removed handler
		// Remove client asssuming connection is broken, requiring reconnect.
		auth.removeClient(client)
		if connectTimeout > 0 {
			auth.addUserConnectReq(&pendingRequest{
				user:            user,
				req:             msg,
				handlerFunc:     f,
				responseTimeout: expireTimeout,
				lastAttempt:     stamp,
				expireFunc:      expire,
			}, connectTimeout)
			return nil
		}
	}
	return err
}

// Request sends the Request-type msgjson.Message to the client identified by
// the specified account ID. The user must respond within DefaultRequestTimeout
// of the request. Late responses are not handled.
func (auth *AuthManager) Request(user account.AccountID, msg *msgjson.Message, f func(comms.Link, *msgjson.Message)) error {
	return auth.request(user, msg, f, DefaultRequestTimeout, 0, func() {})
}

// RequestWhenConnected is like RequestWithTimeout but if they are not already
// connected or if sending the requests fails, it will create a pending request
// that handleConnect will attempt if the user does reconnect. The pending
// request remains valid for connectTimeout. There is no error return since send
// failures cause the client to be removed (assumed disconnected) and the
// request to be queued for send on subsequent connect. Error handling should be
// dealt with in the expire function as that is triggered after connectTimeout
// if the user has not connected or after expireTimeout if the request is
// successfully sent but not met with a response. Each attempt gets the full
// expireTimeout for a response. On the other hand, the user should not get the
// full connectTimeout each time the user connects and a request is resent (see
// handleConnect).
func (auth *AuthManager) RequestWhenConnected(user account.AccountID, msg *msgjson.Message, f func(comms.Link, *msgjson.Message),
	expireTimeout, connectTimeout time.Duration, expire func()) {
	// Error is nil when connectTimeout > 0.
	_ = auth.request(user, msg, f, expireTimeout, connectTimeout, expire)
}

// RequestWithTimeout sends the Request-type msgjson.Message to the client
// identified by the specified account ID. If the user responds within
// expireTime of the request, the response handler is called, otherwise the
// expire function is called. If the response handler is called, it is
// guaranteed that the request Message.ID is equal to the response Message.ID
// (see handleResponse).
func (auth *AuthManager) RequestWithTimeout(user account.AccountID, msg *msgjson.Message, f func(comms.Link, *msgjson.Message),
	expireTimeout time.Duration, expire func()) error {
	return auth.request(user, msg, f, expireTimeout, 0, expire)
}

// Penalize signals that a user has broken a rule of community conduct, and that
// their account should be penalized.
func (auth *AuthManager) Penalize(user account.AccountID, rule account.Rule) {
	if auth.anarchy {
		log.Infof("user %v penalized for rule %v, but not enforcing it", user, rule)
		return
	}

	// TODO: option to close permanently or suspend for a certain time.

	client := auth.user(user)
	if client != nil {
		client.suspend()
	}

	auth.storage.CloseAccount(user /*client.acct.ID*/, rule)

	// We do NOT want to do disconnect if the user has active swaps.  However,
	// we do not want the user to initiate a swap or place a new order, so there
	// should be appropriate checks on order submission and match/swap
	// initiation (TODO).

	// TODO: notify client of penalty / account status change?
}

// user gets the clientInfo for the specified account ID.
func (auth *AuthManager) user(user account.AccountID) *clientInfo {
	auth.connMtx.RLock()
	defer auth.connMtx.RUnlock()
	return auth.users[user]
}

// conn gets the clientInfo for the specified connection ID.
func (auth *AuthManager) conn(conn comms.Link) *clientInfo {
	auth.connMtx.RLock()
	defer auth.connMtx.RUnlock()
	return auth.conns[conn.ID()]
}

// addClient adds the client to the users and conns maps.
func (auth *AuthManager) addClient(client *clientInfo) ([]*pendingRequest, []*pendingMessage) {
	auth.connMtx.Lock()
	defer auth.connMtx.Unlock()
	auth.users[client.acct.ID] = client
	auth.conns[client.conn.ID()] = client

	return auth.rmUserConnectReqs(client.acct.ID), auth.rmUserConnectMsgs(client.acct.ID)
}

// removeClient removes the client from the users and conns map.
func (auth *AuthManager) removeClient(client *clientInfo) {
	auth.connMtx.Lock()
	defer auth.connMtx.Unlock()
	delete(auth.users, client.acct.ID)
	delete(auth.conns, client.conn.ID())
}

// handleConnect is the handler for the 'connect' route. The user is authorized,
// a response is issued, and a clientInfo is created or updated.
func (auth *AuthManager) handleConnect(conn comms.Link, msg *msgjson.Message) *msgjson.Error {
	connect := new(msgjson.Connect)
	err := json.Unmarshal(msg.Payload, &connect)
	if err != nil {
		return &msgjson.Error{
			Code:    msgjson.RPCParseError,
			Message: "error parsing connect: " + err.Error(),
		}
	}
	if len(connect.AccountID) != account.HashSize {
		return &msgjson.Error{
			Code:    msgjson.AuthenticationError,
			Message: "authentication error. invalid account ID",
		}
	}
	var user account.AccountID
	copy(user[:], connect.AccountID[:])
	acctInfo, paid, open := auth.storage.Account(user)
	if acctInfo == nil {
		return &msgjson.Error{
			Code:    msgjson.AuthenticationError,
			Message: "no account info found for account ID" + connect.AccountID.String(),
		}
	}
	if !paid {
		return &msgjson.Error{
			Code:    msgjson.AuthenticationError,
			Message: "unpaid account",
		}
	}
	// Commented to allow suspended accounts to connect and complete swaps, etc.
	// but not place orders.
	// if !open {
	//  return &msgjson.Error{
	//      Code:    msgjson.AuthenticationError,
	//      Message: "closed account",
	//  }
	// }

	// Authorize the account.
	sigMsg := connect.Serialize()
	err = checkSigS256(sigMsg, connect.SigBytes(), acctInfo.PubKey)
	if err != nil {
		return &msgjson.Error{
			Code:    msgjson.SignatureError,
			Message: "signature error: " + err.Error(),
		}
	}

	// Send the connect response, which includes a list of active matches.
	matches, err := auth.storage.ActiveMatches(user)
	if err != nil {
		log.Errorf("ActiveMatches(%x): %v", user, err)
		return &msgjson.Error{
			Code:    msgjson.RPCInternalError,
			Message: "DB error",
		}
	}
	msgMatches := make([]*msgjson.Match, 0, len(matches))
	for _, match := range matches {
		msgMatches = append(msgMatches, &msgjson.Match{
			OrderID:  match.OrderID[:],
			MatchID:  match.MatchID[:],
			Quantity: match.Quantity,
			Rate:     match.Rate,
			Address:  match.Address,
			Status:   uint8(match.Status),
			Side:     uint8(match.Side),
		})
	}

	sig, err := auth.signer.Sign(sigMsg)
	if err != nil {
		log.Errorf("handleConnect signature error: %v", err)
		return &msgjson.Error{
			Code:    msgjson.RPCInternalError,
			Message: "internal error",
		}
	}

	resp := &msgjson.ConnectResult{
		Sig:     sig.Serialize(),
		Matches: msgMatches,
	}
	respMsg, err := msgjson.NewResponse(msg.ID, resp, nil)
	if err != nil {
		return &msgjson.Error{
			Code:    msgjson.RPCInternalError,
			Message: "internal error",
		}
	}
	err = conn.Send(respMsg)
	if err != nil {
		log.Error("error sending connect response: " + err.Error())
		return nil
	}

	// Check to see if there is already an existing client for this account.
	respHandlers := make(map[uint64]*respHandler)
	client := auth.user(acctInfo.ID)
	if client != nil {
		// Disconnect and remove known connections. We are creating a new one,
		// but persist the response handlers.
		auth.connMtx.Lock() // hold clientInfo maps during conn.Disconnect and respHandlers access
		delete(auth.users, client.acct.ID)
		delete(auth.conns, client.conn.ID())
		client.mtx.Lock()
		client.conn.Disconnect()
		respHandlers = client.respHandlers
		client.mtx.Unlock()
		auth.connMtx.Unlock()
	}

	// Retrieve the user's N latest finished (completed or canceled orders)
	// and store them in a latestOrders.
	latestFinished, err := auth.loadRecentFinishedOrders(acctInfo.ID, cancelThreshWindow)
	if err != nil {
		log.Errorf("unable to retrieve user's executed cancels and completed orders: %v", err)
		return &msgjson.Error{
			Code:    msgjson.RPCInternalError,
			Message: "DB error",
		}
	}
	client = &clientInfo{
		acct:         acctInfo,
		conn:         conn,
		respHandlers: respHandlers,
		recentOrders: latestFinished,
		suspended:    !open,
	}
	if cancelRatio := client.cancelRatio(); !auth.anarchy && cancelRatio > auth.cancelThresh {
		// Account should already be closed, but perhaps the server crashed
		// or the account was not penalized before shutdown.
		client.suspended = true
		// The account might now be closed if the cancellation ratio was
		// exceeded while the server was running in anarchy mode.
		auth.storage.CloseAccount(acctInfo.ID, account.CancellationRatio)
		log.Debugf("Suspended account %v (cancellation ratio = %f) connected.",
			acctInfo.ID, cancelRatio)
	}

	pendingReqs, pendingMsgs := auth.addClient(client)
	log.Debugf("User %s connected from %s with %d pending requests and %d pending responses/notifications.",
		acctInfo.ID, conn.IP(), len(pendingReqs), len(pendingMsgs))

	// Send pending requests for this user.
	for _, pr := range pendingReqs {
		// Use the AuthManager method to send so that failed requests, which
		// result in client removal, will go back into the client's pending
		// requests. Subsequent requests that follow removal also fail and go
		// back into the pending requests.
		connectTimeout := DefaultConnectTimeout
		// Decrement the connect timeout for repeated attempts.
		if !pr.lastAttempt.IsZero() {
			connectTimeout -= time.Since(pr.lastAttempt)
		} else {
			log.Warn("last connect attempt Time was not set, using default timeout") // should not happen
		}
		auth.RequestWhenConnected(acctInfo.ID, pr.req, pr.handlerFunc,
			pr.responseTimeout, connectTimeout, pr.expireFunc)
		// consider not sending *match* ack requests for suspended clients.
	}

	// Send pending messages for this user.
	for _, pr := range pendingMsgs {
		connectTimeout := DefaultConnectTimeout
		// Decrement the connect timeout for repeated attempts.
		if !pr.lastAttempt.IsZero() {
			connectTimeout -= time.Since(pr.lastAttempt)
		} else {
			log.Warn("last connect attempt Time was not set, using default timeout") // should not happen
		}
		auth.SendWhenConnected(acctInfo.ID, pr.msg, connectTimeout, pr.expireFunc)
	}

	return nil
}

func (auth *AuthManager) loadRecentFinishedOrders(aid account.AccountID, N int) (*latestOrders, error) {
	// Load the N latest successfully completed orders for the user.
	oids, compTimes, err := auth.storage.CompletedUserOrders(aid, N)
	if err != nil {
		return nil, err
	}

	// Load the N latest executed cancel orders for the user.
	cancelOids, targetOids, cancelTimes, err := auth.storage.ExecutedCancelsForUser(aid, N)
	if err != nil {
		return nil, err
	}

	// Create the sorted list with capacity.
	latestFinished := newLatestOrders(cancelThreshWindow)
	// Insert the completed orders.
	for i := range oids {
		latestFinished.add(&oidStamped{
			OrderID: oids[i],
			time:    compTimes[i],
			//target: nil,
		})
	}
	// Insert the executed cancels, popping off older orders that do not fit in
	// the list.
	for i := range cancelOids {
		latestFinished.add(&oidStamped{
			OrderID: cancelOids[i],
			time:    cancelTimes[i],
			target:  &targetOids[i],
		})
	}

	return latestFinished, nil
}

// handleResponse handles all responses for AuthManager registered routes,
// essentially wrapping response handlers and translating connection ID to
// account ID.
func (auth *AuthManager) handleResponse(conn comms.Link, msg *msgjson.Message) {
	client := auth.conn(conn)
	if client == nil {
		log.Errorf("response from unknown connection")
		return
	}
	handler := client.respHandler(msg.ID)
	if handler == nil {
		log.Debugf("(*AuthManager).handleResponse: unknown msg ID %d", msg.ID)
		errMsg, err := msgjson.NewResponse(msg.ID, nil,
			msgjson.NewError(msgjson.UnknownResponseID, "unknown response ID"))
		if err != nil {
			log.Errorf("failure creating unknown ID response error message: %v", err)
		} else {
			err := conn.Send(errMsg)
			if err != nil {
				log.Tracef("error sending response failure message: %v", err)
				auth.removeClient(client)
			}
		}
		return
	}
	handler.f(conn, msg)
}

// checkSigS256 checks that the message's signature was created with the
// private key for the provided secp256k1 public key.
func checkSigS256(msg, sig []byte, pubKey *secp256k1.PublicKey) error {
	signature, err := secp256k1.ParseDERSignature(sig)
	if err != nil {
		return fmt.Errorf("error decoding secp256k1 Signature from bytes: %v", err)
	}
	if !signature.Verify(msg, pubKey) {
		return fmt.Errorf("secp256k1 signature verification failed")
	}
	return nil
}
