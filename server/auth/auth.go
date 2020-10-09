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
	"github.com/decred/dcrd/dcrec/secp256k1/v3"
	"github.com/decred/dcrd/dcrec/secp256k1/v3/ecdsa"
)

const (
	cancelThreshWindow = 100 // spec
	scoringMatchLimit  = 60  // last N matches (success or at-fault fail) to be considered in swap inaction scoring
	scoringOrderLimit  = 40  // last N orders to be considered in preimage miss scoring

	maxIDsPerOrderStatusRequest = 10_000
)

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
	ForgiveMatchFail(mid order.MatchID) (bool, error)
	// Account retrieves account info for the specified account ID.
	Account(account.AccountID) (acct *account.Account, paid, open bool)
	UserOrderStatuses(aid account.AccountID, base, quote uint32, oids []order.OrderID) ([]*db.OrderStatus, error)
	ActiveUserOrderStatuses(aid account.AccountID) ([]*db.OrderStatus, error)
	CompletedUserOrders(aid account.AccountID, N int) (oids []order.OrderID, compTimes []int64, err error)
	ExecutedCancelsForUser(aid account.AccountID, N int) (oids, targets []order.OrderID, execTimes []int64, err error)
	CompletedAndAtFaultMatchStats(aid account.AccountID, lastN int) ([]*db.MatchOutcome, error)
	PreimageStats(user account.AccountID, lastN int) ([]*db.PreimageResult, error)
	AllActiveUserMatches(aid account.AccountID) ([]*db.MatchData, error)
	MatchStatuses(aid account.AccountID, base, quote uint32, matchIDs []order.MatchID) ([]*db.MatchStatus, error)
	CreateAccount(*account.Account) (string, error)
	AccountRegAddr(account.AccountID) (string, error)
	PayAccount(account.AccountID, []byte) error
}

// Signer signs messages. It is likely a secp256k1.PrivateKey.
type Signer interface {
	Sign(hash []byte) *ecdsa.Signature
	PubKey() *secp256k1.PublicKey
}

// FeeChecker is a function for retrieving the details for a fee payment. It
// is satisfied by (dcr.Backend).FeeCoin.
type FeeChecker func(ctx context.Context, coinID []byte) (addr string, val uint64, confs int64, err error)

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
	log.Tracef("User %v cancellation rate is now %.2f%% (%d cancels : %d successes). Violation = %v", client.acct.ID,
		100*rate, cancels, completions, penalize)
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
	anarchy        bool
	freeCancels    bool
	banScore       uint32
	cancelThresh   float64
	storage        Storage
	signer         Signer
	regFee         uint64
	checkFee       FeeChecker
	feeConfs       int64
	miaUserTimeout time.Duration
	unbookFun      func(account.AccountID)

	// latencyQ is a queue for fee coin waiters to deal with latency.
	latencyQ *wait.TickerQueue

	feeWaiterMtx sync.Mutex
	feeWaiterIdx map[account.AccountID]struct{}

	connMtx   sync.RWMutex
	users     map[account.AccountID]*clientInfo
	conns     map[uint64]*clientInfo
	unbookers map[account.AccountID]*time.Timer

	violationMtx   sync.Mutex
	matchOutcomes  map[account.AccountID]*latestMatchOutcomes
	preimgOutcomes map[account.AccountID]*latestPreimageOutcomes
}

// violation badness
const (
	// preimage miss
	preimageMissScore = 2 // book spoof, no match, no stuck funds

	// failure to act violations
	noSwapAsMakerScore   = 4  // book spoof, match with taker order affected, no stuck funds
	noSwapAsTakerScore   = 11 // maker has contract stuck for 20 hrs
	noRedeemAsMakerScore = 7  // taker has contract stuck for 8 hrs
	noRedeemAsTakerScore = 1  // just dumb, counterparty not inconvenienced

	successScore = -1 // offsets the violations

	defaultBanScore = 20
)

// Violation represents a specific infraction. For example, not broadcasting a
// swap contract transaction by the deadline as the maker.
type Violation int32

const (
	ViolationInvalid Violation = iota - 2
	ViolationForgiven
	ViolationSwapSuccess
	ViolationPreimageMiss
	ViolationNoSwapAsMaker
	ViolationNoSwapAsTaker
	ViolationNoRedeemAsMaker
	ViolationNoRedeemAsTaker
)

var violations = map[Violation]struct {
	score int32
	desc  string
}{
	ViolationSwapSuccess:     {successScore, "swap success"},
	ViolationForgiven:        {-1, "forgiveness"},
	ViolationPreimageMiss:    {preimageMissScore, "preimage miss"},
	ViolationNoSwapAsMaker:   {noSwapAsMakerScore, "no swap as maker"},
	ViolationNoSwapAsTaker:   {noSwapAsTakerScore, "no swap as taker"},
	ViolationNoRedeemAsMaker: {noRedeemAsMakerScore, "no redeem as maker"},
	ViolationNoRedeemAsTaker: {noRedeemAsTakerScore, "no redeem as taker"},
	ViolationInvalid:         {0, "invalid violation"},
}

// Score returns the Violation's score, which is a representation of the
// relative severity of the infraction.
func (v Violation) Score() int32 {
	return violations[v].score
}

// String returns a description of the Violation.
func (v Violation) String() string {
	return violations[v].desc
}

// NoActionStep is the action that the user failed to take. This is used to
// define valid inputs to the Inaction method.
type NoActionStep uint8

const (
	SwapSuccess NoActionStep = iota // success included for accounting purposes
	NoSwapAsMaker
	NoSwapAsTaker
	NoRedeemAsMaker
	NoRedeemAsTaker
)

// Violation returns the corresponding Violation for the misstep represented by
// the NoActionStep.
func (step NoActionStep) Violation() Violation {
	switch step {
	case SwapSuccess:
		return ViolationSwapSuccess
	case NoSwapAsMaker:
		return ViolationNoSwapAsMaker
	case NoSwapAsTaker:
		return ViolationNoSwapAsTaker
	case NoRedeemAsMaker:
		return ViolationNoRedeemAsMaker
	case NoRedeemAsTaker:
		return ViolationNoRedeemAsTaker
	default:
		return ViolationInvalid
	}
}

// String returns the description of the NoActionStep's corresponding Violation.
func (step NoActionStep) String() string {
	return step.Violation().String()
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
	// UserUnbooker is a function for unbooking all of a user's orders.
	UserUnbooker func(account.AccountID)
	// MiaUserTimeout is how long after a user disconnects until UserUnbooker is
	// called for that user.
	MiaUserTimeout time.Duration

	CancelThreshold float64
	Anarchy         bool
	FreeCancels     bool
	// BanScore defines the penalty score when an account gets closed.
	BanScore uint32
}

// NewAuthManager is the constructor for an AuthManager.
func NewAuthManager(cfg *Config) *AuthManager {
	// A ban score of 0 is not sensible, so have a default.
	banScore := cfg.BanScore
	if banScore == 0 {
		banScore = defaultBanScore
	}
	auth := &AuthManager{
		anarchy:        cfg.Anarchy,
		freeCancels:    cfg.FreeCancels || cfg.Anarchy,
		banScore:       banScore,
		users:          make(map[account.AccountID]*clientInfo),
		conns:          make(map[uint64]*clientInfo),
		unbookers:      make(map[account.AccountID]*time.Timer),
		miaUserTimeout: cfg.MiaUserTimeout,
		unbookFun:      cfg.UserUnbooker,
		storage:        cfg.Storage,
		signer:         cfg.Signer,
		regFee:         cfg.RegistrationFee,
		checkFee:       cfg.FeeChecker,
		feeConfs:       cfg.FeeConfs,
		cancelThresh:   cfg.CancelThreshold,
		latencyQ:       wait.NewTickerQueue(recheckInterval),
		feeWaiterIdx:   make(map[account.AccountID]struct{}),
		matchOutcomes:  make(map[account.AccountID]*latestMatchOutcomes),
		preimgOutcomes: make(map[account.AccountID]*latestPreimageOutcomes),
	}

	comms.Route(msgjson.ConnectRoute, auth.handleConnect)
	comms.Route(msgjson.RegisterRoute, auth.handleRegister)
	comms.Route(msgjson.NotifyFeeRoute, auth.handleNotifyFee)
	comms.Route(msgjson.MatchStatusRoute, auth.handleMatchStatus)
	comms.Route(msgjson.OrderStatusRoute, auth.handleOrderStatus)
	return auth
}

func (auth *AuthManager) unbookUserOrders(user account.AccountID) {
	log.Tracef("Unbooking all orders for user %v", user)
	auth.unbookFun(user)
	auth.connMtx.Lock()
	delete(auth.unbookers, user)
	auth.connMtx.Unlock()
}

// ExpectUsers specifies which users are expected to connect within a certain
// time or have their orders unbooked (revoked). This should be run prior to
// starting the AuthManager. This is not part of the constructor since it is
// convenient to obtain this information from the Market's Books, and Market
// requires the AuthManager. The same information could be pulled from storage,
// but the Market is the authoritative book. The AuthManager should be started
// via Run immediately after calling ExpectUsers so the users can connect.
func (auth *AuthManager) ExpectUsers(users map[account.AccountID]struct{}, within time.Duration) {
	log.Debugf("Expecting %d users with booked orders to connect within %v", len(users), within)
	for user := range users {
		user := user // bad go
		auth.unbookers[user] = time.AfterFunc(within, func() { auth.unbookUserOrders(user) })
	}
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
		// It is likely that the user is gone if this is a revoked order.
		// Regardless, connect will rebuild client's recentOrders from the DB.
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
	if penalize && !auth.freeCancels && !client.isSuspended() {
		details := fmt.Sprintf("Suspending user %v for exceeding the cancellation rate threshold %.2f%%. "+
			"(%d cancels : %d successes => %.2f%% )", user, auth.cancelThresh, cancels, completions, 100*rate)
		log.Info(details)
		if err := auth.Penalize(user, account.CancellationRate, details); err != nil {
			log.Errorf("Failed to penalize user %v: %v", user, err)
		}
	}
}

// Run runs the AuthManager until the context is canceled. Satisfies the
// dex.Runner interface.
func (auth *AuthManager) Run(ctx context.Context) {
	go auth.latencyQ.Run(ctx)
	<-ctx.Done()
	auth.connMtx.Lock()
	defer auth.connMtx.Unlock()
	for user, ub := range auth.unbookers {
		ub.Stop()
		delete(auth.unbookers, user)
	}
	// TODO: wait for latencyQ and running comms route handlers (handleRegister, handleNotifyFee)!
}

// Route wraps the comms.Route function, storing the response handler with the
// associated clientInfo, and sending the message on the current comms.Link for
// the client.
func (auth *AuthManager) Route(route string, handler func(context.Context, account.AccountID, *msgjson.Message) *msgjson.Error) {
	comms.Route(route, func(ctx context.Context, conn comms.Link, msg *msgjson.Message) *msgjson.Error {
		client := auth.conn(conn)
		if client == nil {
			return &msgjson.Error{
				Code:    msgjson.UnauthorizedConnection,
				Message: "cannot use route '" + route + "' on an unauthorized connection",
			}
		}
		msgErr := handler(ctx, client.acct.ID, msg)
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
		sig := auth.signer.Sign(sigMsg)
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
		log.Debugf("Send requested for disconnected user %v", user)
		return dex.NewError(ErrUserNotConnected, user.String())
	}

	err := client.conn.Send(msg)
	if err != nil {
		log.Debugf("error sending on link: %v", err)
		// Remove client asssuming connection is broken, requiring reconnect.
		auth.removeClient(client)
		// client.conn.Disconnect() // async removal
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
		log.Debugf("Send requested for disconnected user %v", user)
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
		// client.conn.Disconnect() // async removal
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

// userSwapAmountHistory retrieves the summary of recent swap amounts for the
// given user and market. The user should be connected.
func (auth *AuthManager) userSwapAmountHistory(user account.AccountID, base, quote uint32) *SwapAmounts {
	auth.violationMtx.Lock()
	defer auth.violationMtx.Unlock()
	if outcomes, found := auth.matchOutcomes[user]; found {
		return outcomes.mktSwapAmounts(base, quote)
	}
	return new(SwapAmounts)
}

const (
	// InitUserLotLimit is the number of lots a new user is permitted to have in
	// active orders and swaps. The market should multiply their lot size by
	// this number to get the limit in units of the base asset. This is
	// potentially a per-market setting instead of an auth constant.
	InitUserTakerLotLimit = 6
	AbsTakerLotLimit      = 150
	BookedLotLimit        = 1200

	// These coefficients are used to compute a user's swap limit adjustment via
	// UserOrderLimitAdjustment based on the cumulative amounts in the different
	// match outcomes.
	successWeight    int64 = 2
	stuckLongWeight  int64 = -4
	stuckShortWeight int64 = -2
	spoofedWeight    int64 = -1
)

// UserOrderLimitAdjustment returns a delta in units of the base asset to the
// user's allowed order quantity. This should be added to the market's base
// order size limit.
func (auth *AuthManager) UserOrderLimitAdjustment(user account.AccountID, base, quote uint32) int64 {
	sa := auth.userSwapAmountHistory(user, base, quote)
	return sa.Swapped*successWeight + sa.StuckLong*stuckLongWeight + sa.StuckShort*stuckShortWeight + sa.Spoofed*spoofedWeight
}

// userScore computes the user score from the user's recent match outcomes and
// preimage history. This must be called with the violationMtx locked.
func (auth *AuthManager) userScore(user account.AccountID) (score int32) {
	if outcomes, found := auth.matchOutcomes[user]; found {
		for v, count := range outcomes.binViolations() {
			score += v.Score() * int32(count)
		}
	}
	if outcomes, found := auth.preimgOutcomes[user]; found {
		score += ViolationPreimageMiss.Score() * outcomes.misses()
	}
	return
}

func (auth *AuthManager) registerMatchOutcome(user account.AccountID, misstep NoActionStep, mmid db.MarketMatchID, value uint64, refTime time.Time) int32 {
	violation := misstep.Violation()

	auth.violationMtx.Lock()
	defer auth.violationMtx.Unlock()
	outcomes, found := auth.matchOutcomes[user]
	if !found {
		outcomes = newLatestMatchOutcomes(scoringMatchLimit)
		auth.matchOutcomes[user] = outcomes
	}
	outcomes.add(&matchOutcome{
		time:    encode.UnixMilli(refTime),
		mid:     mmid.MatchID,
		outcome: violation,
		value:   value,
		base:    mmid.Base,
		quote:   mmid.Quote,
	})

	score := auth.userScore(user)
	log.Debugf("Registering outcome %q (badness %d) for user %v, new score = %d",
		violation.String(), violation.Score(), user, score)

	return score
}

// SwapSuccess registers the successful completion of a swap by the given user.
// TODO: provide lots instead of value, or convert to lots somehow. But, Swapper
// has no clue about lot size, and neither does DB!
func (auth *AuthManager) SwapSuccess(user account.AccountID, mmid db.MarketMatchID, value uint64, redeemTime time.Time) {
	auth.registerMatchOutcome(user, SwapSuccess, mmid, value, redeemTime)
}

// Inaction registers an inaction violation by the user at the given step. The
// refTime is time to which the at-fault user's inaction deadline for the match
// is referenced. e.g. For a swap that failed in TakerSwapCast, refTime would be
// the maker's redeem time, which is recorded in the DB when the server
// validates the maker's redemption and informs the taker, and is roughly when
// the actor was first able to take the missed action.
// TODO: provide lots instead of value, or convert to lots somehow. But, Swapper
// has no clue about lot size, and neither does DB!
func (auth *AuthManager) Inaction(user account.AccountID, misstep NoActionStep, mmid db.MarketMatchID, matchValue uint64, refTime time.Time, oid order.OrderID) {
	if misstep.Violation() == ViolationInvalid {
		log.Errorf("Invalid inaction step %d", misstep)
		return
	}
	score := auth.registerMatchOutcome(user, misstep, mmid, matchValue, refTime)
	if score < int32(auth.banScore) {
		return
	}
	log.Debugf("User %v ban score %d is at or above %d. Penalizing.", user, score, auth.banScore)
	details := fmt.Sprintf("swap %v failure (%v) for order %v", mmid.MatchID, misstep, oid)
	if err := auth.Penalize(user, account.FailureToAct, details); err != nil {
		log.Errorf("Failed to penalize user %v: %v", user, err)
	}
}

func (auth *AuthManager) registerPreimageOutcome(user account.AccountID, miss bool, oid order.OrderID, refTime time.Time) int32 {
	auth.violationMtx.Lock()
	defer auth.violationMtx.Unlock()
	outcomes, found := auth.preimgOutcomes[user]
	if !found {
		outcomes = newLatestPreimageOutcomes(scoringOrderLimit)
		auth.preimgOutcomes[user] = outcomes
	}
	outcomes.add(&preimageOutcome{
		time: encode.UnixMilli(refTime),
		oid:  oid,
		miss: miss,
	})

	score := auth.userScore(user)
	if miss {
		log.Debugf("Registering outcome %q (badness %d) for user %v, new score = %d",
			ViolationPreimageMiss.String(), ViolationPreimageMiss.Score(), user, score)
	}
	return score
}

// PreimageSuccess registers an accepted preimage for the user.
func (auth *AuthManager) PreimageSuccess(user account.AccountID, epochEnd time.Time, oid order.OrderID) {
	auth.registerPreimageOutcome(user, false, oid, epochEnd)
}

// MissedPreimage registers a missed preimage violation by the user.
func (auth *AuthManager) MissedPreimage(user account.AccountID, epochEnd time.Time, oid order.OrderID) {
	score := auth.registerPreimageOutcome(user, true, oid, epochEnd)
	if score < int32(auth.banScore) {
		return
	}
	log.Debugf("User %v ban score %d is at or above %d. Penalizing.", user, score, auth.banScore)
	details := fmt.Sprintf("preimage for order %v not provided upon request", oid)
	if err := auth.Penalize(user, account.PreimageReveal, details); err != nil {
		log.Errorf("Failed to penalize user %v: %v", user, err)
	}
}

// Penalize closes the user's account, unbooks all of their orders, and notifies
// them of this action while citing the provided rule that corresponds to their
// most recent infraction.
func (auth *AuthManager) Penalize(user account.AccountID, lastRule account.Rule, extraDetails string) error {
	if !auth.anarchy {
		// If the user is connected, flag the client as suspended.
		client := auth.user(user)
		if client != nil {
			client.suspend()
		}

		// Unbook all of the user's orders across all markets.
		auth.unbookUserOrders(user)

		// Market the account as closed in the DB.
		// TODO: option to close permanently or suspend for a certain time.
		if err := auth.storage.CloseAccount(user /*client.acct.ID*/, lastRule); err != nil {
			return err
		}
	}

	// Notify user of penalty.
	details := "Ordering has been suspended for this account. Contact the exchange operator to reinstate privileges."
	if auth.anarchy {
		details = "You were penalized but the penalty will not be counted against you."
	}
	details = fmt.Sprintf("%s\nLast Broken Rule Details: %s\n%s", details, lastRule.Description(), extraDetails)
	penalty := &msgjson.Penalty{
		Rule:     lastRule,
		Time:     encode.UnixMilliU(time.Now()),
		Duration: uint64(lastRule.Duration().Milliseconds()),
		Details:  details,
	}
	penaltyNote := &msgjson.PenaltyNote{
		Penalty: penalty,
	}
	sig := auth.signer.Sign(penaltyNote.Serialize())
	penaltyNote.Sig = sig.Serialize()
	note, err := msgjson.NewNotification(msgjson.PenaltyRoute, penaltyNote)
	if err != nil {
		return fmt.Errorf("error creating penalty notification: %w", err)
	}
	// TODO: verify that we are not sending a note over max uint16 as it
	// cannot be sent over ws.
	auth.Notify(user, note)
	if auth.anarchy {
		err := fmt.Errorf("user %v penalized for rule %v, but not enforcing it", user, lastRule)
		return err
	}

	log.Debugf("User %v account closed. Last rule broken = %v. Detail: %s", user, lastRule, extraDetails)

	return nil
}

// Unban forgives a user, allowing them to resume trading if their score permits
// it. This is primarily useful for reversing a manual ban. Use ForgiveMatchFail
// to forgive specific match negotiation failures.
func (auth *AuthManager) Unban(user account.AccountID) error {
	client := auth.user(user)
	if client != nil {
		// If client is connected, mark the user as not suspend.
		client.restore()
	}

	if err := auth.storage.RestoreAccount(user); err != nil {
		return err
	}

	return nil
}

// ForgiveMatchFail forgives a user for a specific match failure, potentially
// allowing them to resume trading if their score becomes passing.
func (auth *AuthManager) ForgiveMatchFail(user account.AccountID, mid order.MatchID) (forgiven, unbanned bool, err error) {
	// Forgive the specific match failure in the DB.
	forgiven, err = auth.storage.ForgiveMatchFail(mid)
	if err != nil {
		return
	}

	// Recompute the user's score.
	var score int32
	score, err = auth.loadUserScore(user)
	if err != nil {
		return
	}

	// Restore the account if score is sub-threshold.
	if score < int32(auth.banScore) {
		if err = auth.Unban(user); err == nil {
			unbanned = true
		}
	}

	return
}

// TODO: a way to manipulate/forgive cancellation rate violation.

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

// addClient adds the client to the users and conns maps, and stops any unbook
// timers started when they last disconnected.
func (auth *AuthManager) addClient(client *clientInfo) {
	auth.connMtx.Lock()
	defer auth.connMtx.Unlock()
	user := client.acct.ID
	if unbookTimer, found := auth.unbookers[user]; found {
		if unbookTimer.Stop() {
			log.Debugf("Stopped unbook timer for user %v", user)
		}
		delete(auth.unbookers, user)
	}

	oldClient := auth.users[user]
	auth.users[user] = client

	connID := client.conn.ID()
	auth.conns[connID] = client

	// Now that the new conn ID is registered, disconnect any existing old link
	// unless it is the same.
	if oldClient != nil {
		oldConnID := oldClient.conn.ID()
		if oldConnID == connID {
			return // reused conn, just update maps
		}
		log.Warnf("User %v reauthorized from %v (id %d) with an existing connection from %v (id %d). Disconnecting the old one.",
			user, client.conn.IP(), connID, oldClient.conn.IP(), oldConnID)
		// When replacing with a new conn, manually deregister the old conn so
		// that when it disconnects it does not remove the new clientInfo.
		delete(auth.conns, oldConnID)
		oldClient.conn.Disconnect()
	}

	// When the conn goes down, automatically unregister the client.
	go func() {
		<-client.conn.Done()
		log.Debugf("Link down: id=%d, ip=%s.", client.conn.ID(), client.conn.IP())
		auth.removeClient(client) // must stop if connID already removed
	}()
}

// removeClient removes the client from the users and conns map, and sets a
// timer to unbook all of the user's orders if they do not return within a
// certain time. This is idempotent for a given conn ID.
func (auth *AuthManager) removeClient(client *clientInfo) {
	auth.connMtx.Lock()
	defer auth.connMtx.Unlock()
	connID := client.conn.ID()
	_, connFound := auth.conns[connID]
	if !connFound {
		// conn already removed manually when this user made a new connection.
		// This user is still in the users map, so return.
		return
	}
	user := client.acct.ID
	delete(auth.users, user)
	delete(auth.conns, connID)
	client.conn.Disconnect() // in case not triggered by disconnect
	auth.unbookers[user] = time.AfterFunc(auth.miaUserTimeout, func() { auth.unbookUserOrders(user) })

	auth.violationMtx.Lock()
	delete(auth.matchOutcomes, user)
	delete(auth.preimgOutcomes, user)
	auth.violationMtx.Unlock()
}

// loadUserScore computes the user's current score from order and swap data
// retrieved from the DB.
func (auth *AuthManager) loadUserScore(user account.AccountID) (int32, error) {
	var successCount, piMissCount int32
	// Load the N most recent matches resulting in success or an at-fault match
	// revocation for the user.
	matchOutcomes, err := auth.storage.CompletedAndAtFaultMatchStats(user, scoringMatchLimit)
	if err != nil {
		return 0, fmt.Errorf("CompletedAndAtFaultMatchStats: %w", err)
	}

	matchStatusToViol := func(status order.MatchStatus) Violation {
		switch status {
		case order.NewlyMatched:
			return ViolationNoSwapAsMaker
		case order.MakerSwapCast:
			return ViolationNoSwapAsTaker
		case order.TakerSwapCast:
			return ViolationNoRedeemAsMaker
		case order.MakerRedeemed:
			return ViolationNoRedeemAsTaker
		case order.MatchComplete:
			return ViolationSwapSuccess // should be caught by Fail==false
		default:
			return ViolationInvalid
		}
	}

	// Load the count of preimage misses in the N most recently placed orders.
	piOutcomes, err := auth.storage.PreimageStats(user, scoringOrderLimit)
	if err != nil {
		return 0, fmt.Errorf("PreimageStats: %w", err)
	}

	auth.violationMtx.Lock()
	defer auth.violationMtx.Unlock()

	latestMatches := newLatestMatchOutcomes(scoringMatchLimit)
	auth.matchOutcomes[user] = latestMatches
	for _, mo := range matchOutcomes {
		// The Fail flag qualifies MakerRedeemed, which is always success for
		// maker, but fail for taker if revoked.
		v := ViolationSwapSuccess
		if mo.Fail {
			v = matchStatusToViol(mo.Status)
		} else {
			successCount++
		}
		latestMatches.add(&matchOutcome{
			time:    mo.Time,
			mid:     mo.ID,
			outcome: v,
			value:   mo.Value, // Note: DB knows value, not number of lots!
			base:    mo.Base,
			quote:   mo.Quote,
		})
	}

	latestPreimageResults := newLatestPreimageOutcomes(scoringOrderLimit)
	auth.preimgOutcomes[user] = latestPreimageResults
	for _, po := range piOutcomes {
		if po.Miss {
			piMissCount++
		}
		latestPreimageResults.add(&preimageOutcome{
			time: po.Time,
			oid:  po.ID,
			miss: po.Miss,
		})
	}

	// Integrate the match and preimage outcomes.
	score := auth.userScore(user)

	successScore := successCount * successScore // negative
	piMissScore := piMissCount * preimageMissScore
	// score = violationScore + piMissScore + successScore
	violationScore := score - piMissScore - successScore
	log.Debugf("User %v score = %d: %d (violations) + %d (%d preimage misses) - %d (%d successes)",
		user, score, violationScore, piMissScore, piMissCount, -successScore, successCount)

	return score, nil
}

// handleConnect is the handler for the 'connect' route. The user is authorized,
// a response is issued, and a clientInfo is created or updated.
func (auth *AuthManager) handleConnect(_ context.Context, conn comms.Link, msg *msgjson.Message) *msgjson.Error {
	connect := new(msgjson.Connect)
	err := json.Unmarshal(msg.Payload, &connect)
	if err != nil {
		return &msgjson.Error{
			Code:    msgjson.RPCParseError,
			Message: "error parsing connect request",
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
	// Note: suspended accounts may connect to complete swaps, etc. but not
	// place new orders.

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

	// Compute the user's cancellation rate.
	cancels, completions, rate, penalize := auth.checkCancelRate(client)
	if penalize && open && !auth.freeCancels {
		// Account should already be closed, but perhaps the server crashed
		// or the account was not penalized before shutdown.
		client.suspended = true
		// The account might now be closed if the cancellation rate was
		// exceeded while the server was running in anarchy mode.
		auth.storage.CloseAccount(acctInfo.ID, account.CancellationRate)
		log.Debugf("Suspended account %v (cancellation rate = %.2f%%, %d cancels : %d successes) connected.",
			acctInfo.ID, 100*rate, cancels, completions)
	}

	// Compute the user's ban score.
	score, err := auth.loadUserScore(user)
	if err != nil {
		log.Errorf("Failed to compute user %v score: %v", user, err)
		return &msgjson.Error{
			Code:    msgjson.RPCInternalError,
			Message: "DB error",
		}
	}
	if score >= int32(auth.banScore) && open && !auth.anarchy {
		// Would already be penalized unless server changed the banScore or
		// was previously running in anarchy mode.
		client.suspended = true
		auth.storage.CloseAccount(user, account.FailureToAct)
		log.Debugf("Suspended account %v (score = %d) connected.", acctInfo.ID, score)
	}

	// Get the list of active orders for this user.
	activeOrderStatuses, err := auth.storage.ActiveUserOrderStatuses(user)
	if err != nil {
		log.Errorf("ActiveUserOrderStatuses(%v): %v", user, err)
		return &msgjson.Error{
			Code:    msgjson.RPCInternalError,
			Message: "DB error",
		}
	}

	msgOrderStatuses := make([]*msgjson.OrderStatus, 0, len(activeOrderStatuses))
	for _, orderStatus := range activeOrderStatuses {
		msgOrderStatuses = append(msgOrderStatuses, &msgjson.OrderStatus{
			ID:     orderStatus.ID.Bytes(),
			Status: uint16(orderStatus.Status),
		})
	}

	// Get the list of active matches for this user.
	matches, err := auth.storage.AllActiveUserMatches(user)
	if err != nil {
		log.Errorf("AllActiveUserMatches(%v): %v", user, err)
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

	conn.Authorized()

	// Sign and send the connect response.
	sig := auth.signer.Sign(sigMsg)
	resp := &msgjson.ConnectResult{
		Sig:                 sig.Serialize(),
		ActiveOrderStatuses: msgOrderStatuses,
		ActiveMatches:       msgMatches,
		Score:               score,
	}
	respMsg, err := msgjson.NewResponse(msg.ID, resp, nil)
	if err != nil {
		log.Errorf("handleConnect prepare response error: %v", err)
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
				// client.conn.Disconnect() // async removal
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
func (auth *AuthManager) handleMatchStatus(_ context.Context, conn comms.Link, msg *msgjson.Message) *msgjson.Error {
	client := auth.conn(conn)
	if client == nil {
		return msgjson.NewError(msgjson.UnauthorizedConnection,
			"cannot use route 'match_status' on an unauthorized connection")
	}
	var matchReqs []msgjson.MatchRequest
	err := json.Unmarshal(msg.Payload, &matchReqs)
	if err != nil {
		return msgjson.NewError(msgjson.RPCParseError, "error parsing match_status request")
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
				Active:        status.Active,
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
func (auth *AuthManager) handleOrderStatus(_ context.Context, conn comms.Link, msg *msgjson.Message) *msgjson.Error {
	client := auth.conn(conn)
	if client == nil {
		return msgjson.NewError(msgjson.UnauthorizedConnection,
			"cannot use route 'order_status' on an unauthorized connection")
	}

	var orderReqs []*msgjson.OrderStatusRequest
	err := json.Unmarshal(msg.Payload, &orderReqs)
	if err != nil {
		return msgjson.NewError(msgjson.RPCParseError, "error parsing order_status request")
	}
	if len(orderReqs) == 0 {
		return msgjson.NewError(msgjson.InvalidRequestError, "no order id provided")
	}
	if len(orderReqs) > maxIDsPerOrderStatusRequest {
		return msgjson.NewError(msgjson.InvalidRequestError, "cannot request statuses for more than %v orders",
			maxIDsPerOrderStatusRequest)
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

	results := make([]*msgjson.OrderStatus, 0, uniqueReqsCount)
	for _, mm := range mkts {
		orderStatuses, err := auth.storage.UserOrderStatuses(client.acct.ID, mm.base, mm.quote, mm.idList())
		// no results is not an error
		if err != nil {
			log.Errorf("OrderStatuses error: acct = %s, base = %d, quote = %d, orderIDs = %v: %v",
				client.acct.ID, mm.base, mm.quote, mm.orderIDs, err)
			return msgjson.NewError(msgjson.RPCInternalError, "DB error")
		}
		for _, orderStatus := range orderStatuses {
			results = append(results, &msgjson.OrderStatus{
				ID:     orderStatus.ID.Bytes(),
				Status: uint16(orderStatus.Status),
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
	signature, err := ecdsa.ParseDERSignature(sig)
	if err != nil {
		return fmt.Errorf("error decoding secp256k1 Signature from bytes: %w", err)
	}
	if !signature.Verify(msg, pubKey) {
		return fmt.Errorf("secp256k1 signature verification failed")
	}
	return nil
}
