// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sync"
	"time"

	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/encode"
	"decred.org/dcrdex/dex/msgjson"
	"decred.org/dcrdex/dex/order"
	"decred.org/dcrdex/dex/wait"
	"decred.org/dcrdex/server/account"
	"decred.org/dcrdex/server/comms"
	"decred.org/dcrdex/server/db"
	"github.com/decred/dcrd/dcrec/secp256k1/v2"
)

const cancelThreshWindow = 100 // spec

var (
	ErrUserNotConnected = dex.ErrorKind("user not connected")
)

func unixMsNow() time.Time {
	return time.Now().Truncate(time.Millisecond).UTC()
}

// Storage updates and fetches account-related data from what is presumably a
// database.
type Storage interface {
	// CloseAccount closes the account for violation of the specified rule.
	CloseAccount(account.AccountID, account.Rule) error
	// RestoreAccount opens an account that was closed by CloseAccount.
	RestoreAccount(account.AccountID) error
	// Account retrieves account info for the specified account ID.
	Account(account.AccountID) (acct *account.Account, paid, open bool)
	CompletedUserOrders(aid account.AccountID, N int) (oids []order.OrderID, compTimes []int64, err error)
	ExecutedCancelsForUser(aid account.AccountID, N int) (oids, targets []order.OrderID, execTimes []int64, err error)
	AllActiveUserMatches(aid account.AccountID) ([]*db.MatchData, error)
	MatchStatuses(aid account.AccountID, base, quote uint32, matchIDs []order.MatchID) ([]*db.MatchStatus, error)
	OrderStatuses(aid account.AccountID, base, quote uint32, orderIDs []order.OrderID) ([]*db.OrderStatus, error)
	CreateAccount(*account.Account) (string, error)
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

// GraceLimit returns the number of initial orders allowed for a new user before
// the cancellation rate threshold is enforced.
func (auth *AuthManager) GraceLimit() int {
	// Grace period if: total/(1+total) <= thresh OR total <= thresh/(1-thresh).
	return int(math.Round(1e8*auth.cancelThresh/(1-auth.cancelThresh))) / 1e8
}

func (auth *AuthManager) checkCancelRate(client *clientInfo) (cancels, completions int, rate float64, penalize bool) {
	var total int
	total, cancels = client.recentOrders.counts()
	completions = total - cancels
	rate = float64(cancels) / float64(total) // rate will be NaN if total is 0
	penalize = rate > auth.cancelThresh &&   // NaN cancelRate compares false
		total > auth.GraceLimit()
	return
}

func (client *clientInfo) suspend() {
	client.mtx.Lock()
	client.suspended = true
	client.mtx.Unlock()
}

func (client *clientInfo) restore() {
	client.mtx.Lock()
	client.suspended = false
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
	cancelThresh float64
	storage      Storage
	signer       Signer
	regFee       uint64
	checkFee     FeeChecker
	feeConfs     int64

	// latencyQ is a queue for coin waiters to deal with latency.
	latencyQ *wait.TickerQueue

	connMtx sync.RWMutex
	users   map[account.AccountID]*clientInfo
	conns   map[uint64]*clientInfo
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
		anarchy:      cfg.Anarchy,
		users:        make(map[account.AccountID]*clientInfo),
		conns:        make(map[uint64]*clientInfo),
		storage:      cfg.Storage,
		signer:       cfg.Signer,
		regFee:       cfg.RegistrationFee,
		checkFee:     cfg.FeeChecker,
		feeConfs:     cfg.FeeConfs,
		cancelThresh: cfg.CancelThreshold,
		latencyQ:     wait.NewTickerQueue(recheckInterval),
	}

	comms.Route(msgjson.ConnectRoute, auth.handleConnect)
	comms.Route(msgjson.RegisterRoute, auth.handleRegister)
	comms.Route(msgjson.NotifyFeeRoute, auth.handleNotifyFee)
	comms.Route(msgjson.MatchStatusRoute, auth.handleMatchStatus)
	comms.Route(msgjson.OrderStatusRoute, auth.handleOrderStatus)
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
	client := auth.user(user)
	if client == nil {
		log.Errorf("unknown client for user %v", user)
		return
	}

	// Update recent orders and check/set suspended status atomically.
	client.mtx.Lock()
	client.recentOrders.add(&oidStamped{
		OrderID: oid,
		time:    tMS,
		target:  target,
	})

	log.Debugf("Recorded order %v that has finished processing: user=%v, time=%v, target=%v",
		oid, user, tMS, target)

	// Recompute cancellation and penalize violation.
	cancels, completions, rate, penalize := auth.checkCancelRate(client)
	client.mtx.Unlock()
	log.Tracef("User %v cancellation rate is now %v (%d cancels : %d successes). Violation = %v", user,
		rate, cancels, completions, penalize)
	if penalize && !auth.anarchy && !client.isSuspended() {
		details := fmt.Sprintf("Suspending user %v for exceeding the cancellation rate threshold %.2f%%. "+
			"(%d cancels : %d successes => %.2f%% )", user, auth.cancelThresh, cancels, completions, 100*rate)
		log.Info(details)
		auth.Penalize(user, account.CancellationRate, details)
	}
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
		msgErr := handler(client.acct.ID, msg)
		if msgErr != nil {
			log.Debugf("Handling of '%s' request for user %v failed: %v", route, client.acct.ID, msgErr)
		}
		return msgErr
	})
}

// Auth validates the signature/message pair with the users public key.
func (auth *AuthManager) Auth(user account.AccountID, msg, sig []byte) error {
	client := auth.user(user)
	if client == nil {
		return dex.NewError(ErrUserNotConnected, user.String())
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

// Response and notification (non-request) messages

// Send sends the non-Request-type msgjson.Message to the client identified by
// the specified account ID. The message is sent asynchronously, so an error is
// only generated if the specified user is not connected and authorized, if the
// message fails marshalling, or if the link is in a failing state. See
// dex/ws.(*WSLink).Send for more information.
func (auth *AuthManager) Send(user account.AccountID, msg *msgjson.Message) error {
	client := auth.user(user)
	if client == nil {
		log.Errorf("Send requested for unknown user %v", user)
		return dex.NewError(ErrUserNotConnected, user.String())
	}

	err := client.conn.Send(msg)
	if err != nil {
		log.Debugf("error sending on link: %v", err)
		// Remove client asssuming connection is broken, requiring reconnect.
		auth.removeClient(client)
	}
	return err
}

// Notify sends a message to a client. The message should be a notification.
// See msgjson.NewNotification.
func (auth *AuthManager) Notify(acctID account.AccountID, msg *msgjson.Message) {
	if err := auth.Send(acctID, msg); err != nil {
		log.Infof("Failed to send notification to user %s: %v", acctID, err)
	}
}

// Requests

// DefaultRequestTimeout is the default timeout for requests to wait for
// responses from connected users after the request is successfully sent.
const DefaultRequestTimeout = 30 * time.Second

func (auth *AuthManager) request(user account.AccountID, msg *msgjson.Message, f func(comms.Link, *msgjson.Message),
	expireTimeout time.Duration, expire func()) error {

	client := auth.user(user)
	if client == nil {
		log.Errorf("Send requested for unknown user %v", user)
		return dex.NewError(ErrUserNotConnected, user.String())
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
	}
	return err
}

// Request sends the Request-type msgjson.Message to the client identified by
// the specified account ID. The user must respond within DefaultRequestTimeout
// of the request. Late responses are not handled.
func (auth *AuthManager) Request(user account.AccountID, msg *msgjson.Message, f func(comms.Link, *msgjson.Message)) error {
	return auth.request(user, msg, f, DefaultRequestTimeout, func() {})
}

// RequestWithTimeout sends the Request-type msgjson.Message to the client
// identified by the specified account ID. If the user responds within
// expireTime of the request, the response handler is called, otherwise the
// expire function is called. If the response handler is called, it is
// guaranteed that the request Message.ID is equal to the response Message.ID
// (see handleResponse).
func (auth *AuthManager) RequestWithTimeout(user account.AccountID, msg *msgjson.Message, f func(comms.Link, *msgjson.Message),
	expireTimeout time.Duration, expire func()) error {
	return auth.request(user, msg, f, expireTimeout, expire)
}

// Penalize signals that a user has broken a rule of community conduct, and that
// their account should be penalized.
func (auth *AuthManager) Penalize(user account.AccountID, rule account.Rule, extraDetails string) error {
	// Notify user of penalty.
	details := "Ordering has been suspended for this account. Contact the exchange operator to reinstate privileges."
	if auth.anarchy {
		details = "You were penalized but the penalty will not be counted against you."
	}
	details = fmt.Sprintf("%s\nLast Broken Rule Details: %s\n%s", details, rule.Description(), extraDetails)
	penalty := &msgjson.Penalty{
		Rule:     rule,
		Time:     encode.UnixMilliU(time.Now()),
		Duration: uint64(rule.Duration() / 1e6), // nanoseconds to milliseconds
		Details:  details,
	}
	penaltyNote := &msgjson.PenaltyNote{
		Penalty: penalty,
	}
	sig, err := auth.signer.Sign(penaltyNote.Serialize())
	if err != nil {
		return fmt.Errorf("signature error: %v", err)
	}
	penaltyNote.Sig = sig.Serialize()
	note, err := msgjson.NewNotification(msgjson.PenaltyRoute, penaltyNote)
	if err != nil {
		return fmt.Errorf("error creating penalty notification: %v", err)
	}
	// TODO: verify that we are not sending a note over max uint16 as it
	// cannot be sent over ws.
	auth.Notify(user, note)
	if auth.anarchy {
		err := fmt.Errorf("user %v penalized for rule %v, but not enforcing it", user, rule)
		log.Error(err)
		return err
	}

	// TODO: option to close permanently or suspend for a certain time.

	client := auth.user(user)
	if client != nil {
		client.suspend()
	}

	if err := auth.storage.CloseAccount(user /*client.acct.ID*/, rule); err != nil {
		log.Error(err)
		return err
	}

	log.Debugf("user %v penalized for rule %v", user, rule)

	// We do NOT want to do disconnect if the user has active swaps.  However,
	// we do not want the user to initiate a swap or place a new order, so there
	// should be appropriate checks on order submission and match/swap
	// initiation (TODO).

	return nil
}

// Unban forgives a user, allowing them to resume trading.
func (auth *AuthManager) Unban(user account.AccountID) error {
	client := auth.user(user)
	if client != nil {
		client.restore()
	}

	if err := auth.storage.RestoreAccount(user /*client.acct.ID*/); err != nil {
		return err
	}

	log.Debugf("user %v unbanned", user)

	return nil
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
func (auth *AuthManager) addClient(client *clientInfo) {
	auth.connMtx.Lock()
	defer auth.connMtx.Unlock()
	oldClient := auth.users[client.acct.ID]
	if oldClient != nil {
		oldClient.conn.Disconnect()
	}
	auth.users[client.acct.ID] = client
	auth.conns[client.conn.ID()] = client
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
			Code:    msgjson.AccountNotFoundError,
			Message: "no account found for account ID: " + connect.AccountID.String(),
		}
	}
	if !paid {
		// TODO: Send pending responses (e.g. a 'register` response that
		// contains the fee address and amount for the user). Use
		// rmUserConnectMsgs and rmUserConnectReqs to get them by account ID.
		return &msgjson.Error{
			Code:    msgjson.UnpaidAccountError,
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

	// Check to see if there is already an existing client for this account.
	respHandlers := make(map[uint64]*respHandler)
	oldClient := auth.user(acctInfo.ID)
	if oldClient != nil {
		oldClient.mtx.Lock()
		respHandlers = oldClient.respHandlers
		oldClient.mtx.Unlock()
	}

	// Retrieve the user's N latest finished (completed or canceled orders)
	// and store them in a latestOrders.
	latestFinished, err := auth.loadRecentFinishedOrders(acctInfo.ID, cancelThreshWindow)
	if err != nil {
		log.Errorf("Unable to retrieve user's executed cancels and completed orders: %v", err)
		return &msgjson.Error{
			Code:    msgjson.RPCInternalError,
			Message: "DB error",
		}
	}
	client := &clientInfo{
		acct:         acctInfo,
		conn:         conn,
		respHandlers: respHandlers,
		recentOrders: latestFinished,
		suspended:    !open,
	}
	cancels, completions, rate, penalize := auth.checkCancelRate(client)
	if penalize && open && !auth.anarchy {
		// Account should already be closed, but perhaps the server crashed
		// or the account was not penalized before shutdown.
		client.suspended = true
		// The account might now be closed if the cancellation rate was
		// exceeded while the server was running in anarchy mode.
		auth.storage.CloseAccount(acctInfo.ID, account.CancellationRate)
		log.Debugf("Suspended account %v (cancellation rate = %.2f%%, %d cancels : %d successes) connected.",
			acctInfo.ID, cancels, completions, 100*rate)
	}

	// Get the list of active matches for this user.
	matches, err := auth.storage.AllActiveUserMatches(user)
	if err != nil {
		log.Errorf("AllActiveUserMatches(%x): %v", user, err)
		return &msgjson.Error{
			Code:    msgjson.RPCInternalError,
			Message: "DB error",
		}
	}

	// There may be as many as 2*len(matches) match messages if the user matched
	// with themself, but this is likely to be very rare outside of tests.
	msgMatches := make([]*msgjson.Match, 0, len(matches))

	// msgMatchForSide checks if the user is on the given side of the match,
	// appending the match to the slice if so. The Address and Side fields of
	// msgjson.Match will differ depending on the side.
	msgMatchForSide := func(match *db.MatchData, side order.MatchSide) {
		var addr string
		var oid []byte
		switch {
		case side == order.Maker && user == match.MakerAcct:
			addr = match.TakerAddr // counterparty
			oid = match.Maker[:]
			// sell = !match.TakerSell
		case side == order.Taker && user == match.TakerAcct:
			addr = match.MakerAddr // counterparty
			oid = match.Taker[:]
			// sell = match.TakerSell
		default:
			return
		}
		msgMatches = append(msgMatches, &msgjson.Match{
			OrderID:      oid,
			MatchID:      match.ID[:],
			Quantity:     match.Quantity,
			Rate:         match.Rate,
			ServerTime:   encode.UnixMilliU(match.Epoch.End()),
			Address:      addr,
			FeeRateBase:  match.BaseRate,  // contract txn fee rate if user is selling
			FeeRateQuote: match.QuoteRate, // contract txn fee rate if user is buying
			Status:       uint8(match.Status),
			Side:         uint8(side),
		})
	}

	// For each db match entry, create at least one msgjson.Match.
	activeMatchIDs := make(map[order.MatchID]bool, len(matches))
	for _, match := range matches {
		activeMatchIDs[match.ID] = true
		msgMatchForSide(match, order.Maker)
		msgMatchForSide(match, order.Taker)
	}

	// Sign and send the connect response.
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

	auth.addClient(client)
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

// marketMatches is an index of match IDs associated with a particular market.
type marketMatches struct {
	base     uint32
	quote    uint32
	matchIDs map[order.MatchID]bool
}

// add adds a match ID to the marketMatches.
func (mm *marketMatches) add(matchID order.MatchID) bool {
	_, found := mm.matchIDs[matchID]
	mm.matchIDs[matchID] = true
	return !found
}

// idList generates a []order.MatchID from the currently indexed match IDs.
func (mm *marketMatches) idList() []order.MatchID {
	ids := make([]order.MatchID, 0, len(mm.matchIDs))
	for matchID := range mm.matchIDs {
		ids = append(ids, matchID)
	}
	return ids
}

// handleMatchStatus handles requests to the 'match_status' route.
func (auth *AuthManager) handleMatchStatus(conn comms.Link, msg *msgjson.Message) *msgjson.Error {
	client := auth.conn(conn)
	if client == nil {
		return msgjson.NewError(msgjson.UnauthorizedConnection,
			"cannot use route 'match_status' on an unauthorized connection")
	}
	var matchReqs []msgjson.MatchRequest
	err := json.Unmarshal(msg.Payload, &matchReqs)
	if err != nil {
		return msgjson.NewError(msgjson.RPCParseError, "error parsing match_status: %v", err)
	}

	mkts := make(map[string]*marketMatches)
	var count int
	for _, req := range matchReqs {
		mkt, err := dex.MarketName(req.Base, req.Quote)
		if err != nil {
			return msgjson.NewError(msgjson.InvalidRequestError, "market with base=%d, quote=%d is not known", req.Base, req.Quote)
		}
		if len(req.MatchID) != order.MatchIDSize {
			return msgjson.NewError(msgjson.InvalidRequestError, "match ID is wrong length: %s", req.MatchID)
		}
		mktMatches, found := mkts[mkt]
		if !found {
			mktMatches = &marketMatches{
				base:     req.Base,
				quote:    req.Quote,
				matchIDs: make(map[order.MatchID]bool),
			}
			mkts[mkt] = mktMatches
		}
		var matchID order.MatchID
		copy(matchID[:], req.MatchID)
		if mktMatches.add(matchID) {
			count++
		}
	}

	results := make([]*msgjson.MatchStatusResult, 0, count)
	for _, mm := range mkts {
		statuses, err := auth.storage.MatchStatuses(client.acct.ID, mm.base, mm.quote, mm.idList())
		// no results is not an error
		if err != nil {
			log.Errorf("MatchStatuses error: acct = %s, base = %d, quote = %d, matchIDs = %v: %v",
				client.acct.ID, mm.base, mm.quote, mm.matchIDs, err)
			return msgjson.NewError(msgjson.RPCInternalError, "DB error")
		}
		for _, status := range statuses {
			results = append(results, &msgjson.MatchStatusResult{
				MatchID:       status.ID.Bytes(),
				Status:        uint8(status.Status),
				MakerContract: status.MakerContract,
				TakerContract: status.TakerContract,
				MakerSwap:     status.MakerSwap,
				TakerSwap:     status.TakerSwap,
				MakerRedeem:   status.MakerRedeem,
				TakerRedeem:   status.TakerRedeem,
				Secret:        status.Secret,
			})
		}
	}

	log.Tracef("%d results for %d requested match statuses, acct = %s",
		len(results), len(matchReqs), client.acct.ID)

	resp, err := msgjson.NewResponse(msg.ID, results, nil)
	if err != nil {
		log.Errorf("NewResponse error: %v", err)
		return msgjson.NewError(msgjson.RPCInternalError, "Internal error")
	}

	err = conn.Send(resp)
	if err != nil {
		log.Error("error sending match_status response: " + err.Error())
	}
	return nil
}

// marketOrders is an index of order IDs associated with a particular market.
type marketOrders struct {
	base     uint32
	quote    uint32
	orderIDs map[order.OrderID]bool
}

// add adds a match ID to the marketOrders.
func (mo *marketOrders) add(oid order.OrderID) bool {
	_, found := mo.orderIDs[oid]
	mo.orderIDs[oid] = true
	return !found
}

// idList generates a []order.OrderID from the currently indexed order IDs.
func (mo *marketOrders) idList() []order.OrderID {
	ids := make([]order.OrderID, 0, len(mo.orderIDs))
	for oid := range mo.orderIDs {
		ids = append(ids, oid)
	}
	return ids
}

// handleOrderStatus handles requests to the 'order_status' route.
func (auth *AuthManager) handleOrderStatus(conn comms.Link, msg *msgjson.Message) *msgjson.Error {
	client := auth.conn(conn)
	if client == nil {
		return msgjson.NewError(msgjson.UnauthorizedConnection,
			"cannot use route 'order_status' on an unauthorized connection")
	}

	var orderReqs []msgjson.OrderStatusRequest
	err := json.Unmarshal(msg.Payload, &orderReqs)
	if err != nil {
		return msgjson.NewError(msgjson.RPCParseError, "error parsing order_status: %v", err)
	}

	mkts := make(map[string]*marketOrders)
	var uniqueReqsCount int
	for _, req := range orderReqs {
		mkt, err := dex.MarketName(req.Base, req.Quote)
		if err != nil {
			return msgjson.NewError(msgjson.InvalidRequestError, "market with base=%d, quote=%d is not known", req.Base, req.Quote)
		}
		if len(req.OrderID) != order.OrderIDSize {
			return msgjson.NewError(msgjson.InvalidRequestError, "order ID is wrong length: %s", req.OrderID)
		}
		mktOrders, found := mkts[mkt]
		if !found {
			mktOrders = &marketOrders{
				base:     req.Base,
				quote:    req.Quote,
				orderIDs: make(map[order.OrderID]bool),
			}
			mkts[mkt] = mktOrders
		}
		var oid order.OrderID
		copy(oid[:], req.OrderID)
		if mktOrders.add(oid) {
			uniqueReqsCount++
		}
	}

	results := make([]*msgjson.OrderStatusResult, 0, uniqueReqsCount)
	for _, mm := range mkts {
		orderStatuses, err := auth.storage.OrderStatuses(client.acct.ID, mm.base, mm.quote, mm.idList())
		// no results is not an error
		if err != nil {
			log.Errorf("OrderStatuses error: acct = %s, base = %d, quote = %d, orderIDs = %v: %v",
				client.acct.ID, mm.base, mm.quote, mm.orderIDs, err)
			return msgjson.NewError(msgjson.RPCInternalError, "DB error")
		}
		for _, order := range orderStatuses {
			results = append(results, &msgjson.OrderStatusResult{
				OrderID: order.ID.Bytes(),
				Status:  uint16(order.Status),
				Fill:    order.Fill,
			})
		}
	}

	log.Tracef("%d results for %d requested order statuses, acct = %s",
		len(results), uniqueReqsCount, client.acct.ID)

	resp, err := msgjson.NewResponse(msg.ID, results, nil)
	if err != nil {
		log.Errorf("NewResponse error: %v", err)
		return msgjson.NewError(msgjson.RPCInternalError, "Internal error")
	}

	err = conn.Send(resp)
	if err != nil {
		log.Error("error sending order_status response: " + err.Error())
	}
	return nil
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
