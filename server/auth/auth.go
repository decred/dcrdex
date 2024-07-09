// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package auth

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
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
	"decred.org/dcrdex/server/asset"
	"decred.org/dcrdex/server/comms"
	"decred.org/dcrdex/server/db"

	"github.com/decred/dcrd/dcrec/secp256k1/v4"
	"github.com/decred/dcrd/dcrec/secp256k1/v4/ecdsa"
)

const (
	cancelThreshWindow = 100 // spec
	ScoringMatchLimit  = 60  // last N matches (success or at-fault fail) to be considered in swap inaction scoring
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
	// Account retrieves account info for the specified account ID and lock time
	// threshold, which determines when a bond is considered expired.
	Account(account.AccountID, time.Time) (acct *account.Account, bonds []*db.Bond)

	CreateAccountWithBond(acct *account.Account, bond *db.Bond) error
	AddBond(acct account.AccountID, bond *db.Bond) error
	DeleteBond(assetID uint32, coinID []byte) error
	FetchPrepaidBond(bondCoinID []byte) (strength uint32, lockTime int64, err error)
	DeletePrepaidBond(coinID []byte) error
	StorePrepaidBonds(coinIDs [][]byte, strength uint32, lockTime int64) error

	AccountInfo(aid account.AccountID) (*db.Account, error)

	UserOrderStatuses(aid account.AccountID, base, quote uint32, oids []order.OrderID) ([]*db.OrderStatus, error)
	ActiveUserOrderStatuses(aid account.AccountID) ([]*db.OrderStatus, error)
	CompletedUserOrders(aid account.AccountID, N int) (oids []order.OrderID, compTimes []int64, err error)
	ExecutedCancelsForUser(aid account.AccountID, N int) ([]*db.CancelRecord, error)
	CompletedAndAtFaultMatchStats(aid account.AccountID, lastN int) ([]*db.MatchOutcome, error)
	UserMatchFails(aid account.AccountID, lastN int) ([]*db.MatchFail, error)
	ForgiveMatchFail(mid order.MatchID) (bool, error)
	PreimageStats(user account.AccountID, lastN int) ([]*db.PreimageResult, error)
	AllActiveUserMatches(aid account.AccountID) ([]*db.MatchData, error)
	MatchStatuses(aid account.AccountID, base, quote uint32, matchIDs []order.MatchID) ([]*db.MatchStatus, error)
}

// Signer signs messages. The message must be a 32-byte hash.
type Signer interface {
	Sign(hash []byte) *ecdsa.Signature
	PubKey() *secp256k1.PublicKey
}

// FeeChecker is a function for retrieving the details for a fee payment txn.
type FeeChecker func(assetID uint32, coinID []byte) (addr string, val uint64, confs int64, err error)

// BondCoinChecker is a function for locating an unspent bond, and extracting
// the amount, lockTime, and account ID. The confirmations of the bond
// transaction are also provided.
type BondCoinChecker func(ctx context.Context, assetID uint32, ver uint16,
	coinID []byte) (amt, lockTime, confs int64, acct account.AccountID, err error)

// BondTxParser parses a dex fidelity bond transaction and the redeem script of
// the first output of the transaction, which must be the actual bond output.
// The returned account ID is from the second output. This will become a
// multi-asset checker.
//
// NOTE: For DCR, and possibly all assets, the bond script is reconstructed from
// the null data output, and it is verified that the bond output pays to this
// script. As such, there is no provided bondData (redeem script for UTXO
// assets), but this may need for other assets.
type BondTxParser func(assetID uint32, ver uint16, rawTx []byte) (bondCoinID []byte,
	amt int64, lockTime int64, acct account.AccountID, err error)

// TxDataSource retrieves the raw transaction for a coin ID.
type TxDataSource func(coinID []byte) (rawTx []byte, err error)

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
	acct *account.Account
	conn comms.Link

	mtx          sync.Mutex
	respHandlers map[uint64]*respHandler
	tier         int64
	score        int32
	bonds        []*db.Bond // only confirmed and active, not pending
}

// not thread-safe
func (client *clientInfo) bondTier() (bondTier int64) {
	for _, bi := range client.bonds {
		bondTier += int64(bi.Strength)
	}
	return
}

// not thread-safe
func (client *clientInfo) addBond(bond *db.Bond) (bondTier int64) {
	var dup bool
	for _, bi := range client.bonds {
		bondTier += int64(bi.Strength)
		dup = dup || (bi.AssetID == bond.AssetID && bytes.Equal(bi.CoinID, bond.CoinID))
	}

	if !dup { // idempotent
		client.bonds = append(client.bonds, bond)
		bondTier += int64(bond.Strength)
	}

	return
}

// not thread-safe
func (client *clientInfo) pruneBonds(lockTimeThresh int64) (pruned []*db.Bond, bondTier int64) {
	if len(client.bonds) == 0 {
		return
	}

	var n int
	for _, bond := range client.bonds {
		if bond.LockTime >= lockTimeThresh { // not expired
			if len(pruned) > 0 /* n < i */ { // a prior bond was removed, must move this element up in the slice
				client.bonds[n] = bond
			}
			n++
			bondTier += int64(bond.Strength)
			continue
		}
		log.Infof("Expiring user %v bond %v (%s)", client.acct.ID,
			coinIDString(bond.AssetID, bond.CoinID), dex.BipIDSymbol(bond.AssetID))
		pruned = append(pruned, bond)
		// n not incremented, next live bond shifts up
	}
	client.bonds = client.bonds[:n] // no-op if none expired

	return
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
	wg             sync.WaitGroup
	storage        Storage
	signer         Signer
	parseBondTx    BondTxParser
	checkBond      BondCoinChecker // fidelity bond amount, lockTime, acct, and confs
	miaUserTimeout time.Duration
	unbookFun      func(account.AccountID)
	route          func(route string, handler comms.MsgHandler)

	bondExpiry time.Duration // a bond is expired when time.Until(lockTime) < bondExpiry
	bondAssets map[uint32]*msgjson.BondAsset

	freeCancels      bool
	penaltyThreshold int32
	cancelThresh     float64

	// latencyQ is a queue for fee coin waiters to deal with latency.
	latencyQ *wait.TickerQueue

	bondWaiterMtx sync.Mutex
	bondWaiterIdx map[string]struct{}

	connMtx   sync.RWMutex
	users     map[account.AccountID]*clientInfo
	conns     map[uint64]*clientInfo
	unbookers map[account.AccountID]*time.Timer

	violationMtx   sync.Mutex
	matchOutcomes  map[account.AccountID]*latestMatchOutcomes
	preimgOutcomes map[account.AccountID]*latestPreimageOutcomes
	orderOutcomes  map[account.AccountID]*latestOrders // cancel/complete, was in clientInfo.recentOrders

	txDataSources map[uint32]TxDataSource

	prepaidBondMtx sync.Mutex
}

// violation badness
const (
	// preimage miss
	preimageMissScore = -2 // book spoof, no match, no stuck funds

	// failure to act violations
	noSwapAsMakerScore   = -4  // book spoof, match with taker order affected, no stuck funds
	noSwapAsTakerScore   = -11 // maker has contract stuck for 20 hrs
	noRedeemAsMakerScore = -7  // taker has contract stuck for 8 hrs
	noRedeemAsTakerScore = -1  // just dumb, counterparty not inconvenienced

	// cancel rate exceeds threshold
	excessiveCancels = -5

	successScore = 1 // offsets the violations

	DefaultPenaltyThreshold = 20
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
	ViolationCancelRate
)

var violations = map[Violation]struct {
	score int32
	desc  string
}{
	ViolationSwapSuccess:     {successScore, "swap success"},
	ViolationForgiven:        {1, "forgiveness"},
	ViolationPreimageMiss:    {preimageMissScore, "preimage miss"},
	ViolationNoSwapAsMaker:   {noSwapAsMakerScore, "no swap as maker"},
	ViolationNoSwapAsTaker:   {noSwapAsTakerScore, "no swap as taker"},
	ViolationNoRedeemAsMaker: {noRedeemAsMakerScore, "no redeem as maker"},
	ViolationNoRedeemAsTaker: {noRedeemAsTakerScore, "no redeem as taker"},
	ViolationCancelRate:      {excessiveCancels, "excessive cancels"},
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

	Route func(route string, handler comms.MsgHandler)

	// BondExpiry is the time in seconds left until a bond's LockTime is reached
	// that defines when a bond is considered expired.
	BondExpiry uint64
	// BondAssets indicates the supported bond assets and parameters.
	BondAssets map[string]*msgjson.BondAsset
	// BondTxParser performs rudimentary validation of a raw time-locked
	// fidelity bond transaction. e.g. dcr.ParseBondTx
	BondTxParser BondTxParser
	// BondChecker locates an unspent bond, and extracts the amount, lockTime,
	// and account ID, plus txn confirmations.
	BondChecker BondCoinChecker

	// TxDataSources are sources of tx data for a coin ID.
	TxDataSources map[uint32]TxDataSource

	// UserUnbooker is a function for unbooking all of a user's orders.
	UserUnbooker func(account.AccountID)
	// MiaUserTimeout is how long after a user disconnects until UserUnbooker is
	// called for that user.
	MiaUserTimeout time.Duration

	CancelThreshold float64
	FreeCancels     bool

	// PenaltyThreshold defines the score deficit at which a user's bond is
	// revoked.
	PenaltyThreshold uint32
}

// NewAuthManager is the constructor for an AuthManager.
func NewAuthManager(cfg *Config) *AuthManager {
	// A penalty threshold of 0 is not sensible, so have a default.
	penaltyThreshold := int32(cfg.PenaltyThreshold)
	if penaltyThreshold <= 0 {
		penaltyThreshold = DefaultPenaltyThreshold
	}
	// Invert sign for internal use.
	if penaltyThreshold > 0 {
		penaltyThreshold *= -1
	}
	// Re-key the maps for efficiency in AuthManager methods.
	bondAssets := make(map[uint32]*msgjson.BondAsset, len(cfg.BondAssets))
	for _, asset := range cfg.BondAssets {
		bondAssets[asset.ID] = asset
	}

	auth := &AuthManager{
		storage:          cfg.Storage,
		signer:           cfg.Signer,
		bondAssets:       bondAssets,
		bondExpiry:       time.Duration(cfg.BondExpiry) * time.Second,
		parseBondTx:      cfg.BondTxParser, // e.g. dcr's ParseBondTx
		checkBond:        cfg.BondChecker,  // e.g. dcr's BondCoin
		miaUserTimeout:   cfg.MiaUserTimeout,
		unbookFun:        cfg.UserUnbooker,
		route:            cfg.Route,
		freeCancels:      cfg.FreeCancels,
		penaltyThreshold: penaltyThreshold,
		cancelThresh:     cfg.CancelThreshold,
		latencyQ:         wait.NewTickerQueue(recheckInterval),
		users:            make(map[account.AccountID]*clientInfo),
		conns:            make(map[uint64]*clientInfo),
		unbookers:        make(map[account.AccountID]*time.Timer),
		bondWaiterIdx:    make(map[string]struct{}),
		matchOutcomes:    make(map[account.AccountID]*latestMatchOutcomes),
		preimgOutcomes:   make(map[account.AccountID]*latestPreimageOutcomes),
		orderOutcomes:    make(map[account.AccountID]*latestOrders),
		txDataSources:    cfg.TxDataSources,
	}

	// Unauthenticated
	cfg.Route(msgjson.ConnectRoute, auth.handleConnect)
	cfg.Route(msgjson.PostBondRoute, auth.handlePostBond)
	cfg.Route(msgjson.PreValidateBondRoute, auth.handlePreValidateBond)
	cfg.Route(msgjson.MatchStatusRoute, auth.handleMatchStatus)
	cfg.Route(msgjson.OrderStatusRoute, auth.handleOrderStatus)
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

// GraceLimit returns the number of initial orders allowed for a new user before
// the cancellation rate threshold is enforced.
func (auth *AuthManager) GraceLimit() int {
	// Grace period if: total/(1+total) <= thresh OR total <= thresh/(1-thresh).
	return int(math.Round(1e8*auth.cancelThresh/(1-auth.cancelThresh))) / 1e8
}

// RecordCancel records a user's executed cancel order, including the canceled
// order ID, and the time when the cancel was executed.
func (auth *AuthManager) RecordCancel(user account.AccountID, oid, target order.OrderID, epochGap int32, t time.Time) {
	score := auth.recordOrderDone(user, oid, &target, epochGap, t.UnixMilli())

	rep, tierChanged, scoreChanged := auth.computeUserReputation(user, score)
	effectiveTier := rep.EffectiveTier()
	log.Debugf("RecordCancel: user %v strikes %d, bond tier %v => trading tier %v",
		user, score, rep.BondedTier, effectiveTier)
	// If their tier sinks below 1, unbook their orders and send a note.
	if tierChanged && effectiveTier < 1 {
		details := fmt.Sprintf("excessive cancellation rate, new tier = %d", effectiveTier)
		auth.Penalize(user, account.CancellationRate, details)
	}
	if tierChanged {
		go auth.sendTierChanged(user, rep, "excessive, cancellation rate")
	} else if scoreChanged {
		go auth.sendScoreChanged(user, rep)
	}

}

// RecordCompletedOrder records a user's completed order, where completed means
// a swap involving the order was successfully completed and the order is no
// longer on the books if it ever was.
func (auth *AuthManager) RecordCompletedOrder(user account.AccountID, oid order.OrderID, t time.Time) {
	score := auth.recordOrderDone(user, oid, nil, db.EpochGapNA, t.UnixMilli())
	rep, tierChanged, scoreChanged := auth.computeUserReputation(user, score) // may raise tier
	if tierChanged {
		log.Tracef("RecordCompletedOrder: tier changed for user %v strikes %d, bond tier %v => trading tier %v",
			user, score, rep.BondedTier, rep.EffectiveTier())
		go auth.sendTierChanged(user, rep, "successful order completion")
	} else if scoreChanged {
		go auth.sendScoreChanged(user, rep)
	}
}

// recordOrderDone records that an order has finished processing. This can be a
// cancel order, which matched and unbooked another order, or a trade order that
// completed the swap negotiation. Note that in the case of a cancel, oid refers
// to the ID of the cancel order itself, while target is non-nil for cancel
// orders. The user's new score is returned, which can be used to compute the
// user's tier with computeUserTier.
func (auth *AuthManager) recordOrderDone(user account.AccountID, oid order.OrderID, target *order.OrderID, epochGap int32, tMS int64) (score int32) {
	auth.violationMtx.Lock()
	if orderOutcomes, found := auth.orderOutcomes[user]; found {
		orderOutcomes.add(&oidStamped{
			OrderID:  oid,
			time:     tMS,
			target:   target,
			epochGap: epochGap,
		})
		score = auth.userScore(user)
		auth.violationMtx.Unlock()
		log.Debugf("Recorded order %v that has finished processing: user=%v, time=%v, target=%v",
			oid, user, tMS, target)
		return
	}
	auth.violationMtx.Unlock()

	// The user is currently not connected and authenticated. When the user logs
	// back in, their history will be reloaded (loadUserScore) and their tier
	// recomputed, but compute their score now from DB for the caller.
	var err error
	score, err = auth.loadUserScore(user)
	if err != nil {
		log.Errorf("Failed to load order and match outcomes for user %v: %v", user, err)
		return 0
	}

	return
}

// Run runs the AuthManager until the context is canceled. Satisfies the
// dex.Runner interface.
func (auth *AuthManager) Run(ctx context.Context) {
	auth.wg.Add(1)
	go func() {
		defer auth.wg.Done()
		t := time.NewTicker(20 * time.Second)
		defer t.Stop()

		for {
			select {
			case <-t.C:
				auth.checkBonds()
			case <-ctx.Done():
				return
			}
		}
	}()

	auth.wg.Add(1)
	go func() {
		defer auth.wg.Done()
		auth.latencyQ.Run(ctx)
	}()

	<-ctx.Done()
	auth.connMtx.Lock()
	defer auth.connMtx.Unlock()
	for user, ub := range auth.unbookers {
		ub.Stop()
		delete(auth.unbookers, user)
	}

	// Wait for latencyQ and checkBonds.
	auth.wg.Wait()
	// TODO: wait for running comms route handlers and other DB writers.
}

// Route wraps the comms.Route function, storing the response handler with the
// associated clientInfo, and sending the message on the current comms.Link for
// the client.
func (auth *AuthManager) Route(route string, handler func(account.AccountID, *msgjson.Message) *msgjson.Error) {
	auth.route(route, func(conn comms.Link, msg *msgjson.Message) *msgjson.Error {
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

// Message signing and signature verification.

// checkSigS256 checks that the message's signature was created with the
// private key for the provided secp256k1 public key.
func checkSigS256(msg, sig []byte, pubKey *secp256k1.PublicKey) error {
	signature, err := ecdsa.ParseDERSignature(sig)
	if err != nil {
		return fmt.Errorf("error decoding secp256k1 Signature from bytes: %w", err)
	}
	hash := sha256.Sum256(msg)
	if !signature.Verify(hash[:], pubKey) {
		return fmt.Errorf("secp256k1 signature verification failed")
	}
	return nil
}

// Auth validates the signature/message pair with the users public key.
func (auth *AuthManager) Auth(user account.AccountID, msg, sig []byte) error {
	client := auth.user(user)
	if client == nil {
		return dex.NewError(ErrUserNotConnected, user.String())
	}
	return checkSigS256(msg, sig, client.acct.PubKey)
}

// SignMsg signs the message with the DEX private key, returning the DER encoded
// signature. SHA256 is used to hash the message before signing it.
func (auth *AuthManager) SignMsg(msg []byte) []byte {
	hash := sha256.Sum256(msg)
	return auth.signer.Sign(hash[:]).Serialize()
}

// Sign signs the msgjson.Signables with the DEX private key.
func (auth *AuthManager) Sign(signables ...msgjson.Signable) {
	for _, signable := range signables {
		sig := auth.SignMsg(signable.Serialize())
		signable.SetSig(sig)
	}
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
		// Remove client assuming connection is broken, requiring reconnect.
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
		// Remove client assuming connection is broken, requiring reconnect.
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

const (
	// These coefficients are used to compute a user's swap limit adjustment via
	// UserOrderLimitAdjustment based on the cumulative amounts in the different
	// match outcomes.
	successWeight    int64 = 3
	stuckLongWeight  int64 = -5
	stuckShortWeight int64 = -3
	spoofedWeight    int64 = -1
)

func (auth *AuthManager) integrateOutcomes(
	matchOutcomes *latestMatchOutcomes,
	preimgOutcomes *latestPreimageOutcomes,
	orderOutcomes *latestOrders,
) (score, successCount, piMissCount int32) {

	if matchOutcomes != nil {
		matchCounts := matchOutcomes.binViolations()
		for v, count := range matchCounts {
			score += v.Score() * int32(count)
		}
		successCount = int32(matchCounts[ViolationSwapSuccess])
	}
	if preimgOutcomes != nil {
		piMissCount = preimgOutcomes.misses()
		score += ViolationPreimageMiss.Score() * piMissCount
	}
	if !auth.freeCancels {
		totalOrds, cancels := orderOutcomes.counts() // completions := totalOrds - cancels
		if totalOrds > auth.GraceLimit() {
			cancelRate := float64(cancels) / float64(totalOrds)
			if cancelRate > auth.cancelThresh {
				score += ViolationCancelRate.Score()
			}
		}
	}
	return
}

// userScore computes an authenticated user's score from their recent order and
// match outcomes. They must have entries in the outcome maps. Use loadUserScore
// to compute score from history in DB. This must be called with the
// violationMtx locked.
func (auth *AuthManager) userScore(user account.AccountID) (score int32) {
	score, _, _ = auth.integrateOutcomes(auth.matchOutcomes[user], auth.preimgOutcomes[user], auth.orderOutcomes[user])
	return score
}

// UserScore calculates the user's score, loading it from storage if necessary.
func (auth *AuthManager) UserScore(user account.AccountID) (score int32, err error) {
	auth.violationMtx.Lock()
	if _, found := auth.matchOutcomes[user]; found {
		score = auth.userScore(user)
		auth.violationMtx.Unlock()
		return
	}
	auth.violationMtx.Unlock()

	// The user is currently not connected and authenticated. When the user logs
	// back in, their history will be reloaded (loadUserScore) and their tier
	// recomputed, but compute their score now from DB for the caller.
	score, err = auth.loadUserScore(user)
	if err != nil {
		return 0, fmt.Errorf("failed to load order and match outcomes for user %v: %v", user, err)
	}
	return
}

// UserReputation calculates some quantities related to the user's reputation.
// UserReputation satisfies market.AuthManager.
func (auth *AuthManager) UserReputation(user account.AccountID) (tier int64, score, maxScore int32, err error) {
	maxScore = ScoringMatchLimit
	score, err = auth.UserScore(user)
	if err != nil {
		return
	}
	r, _, _ := auth.computeUserReputation(user, score)
	if r != nil {
		return r.EffectiveTier(), r.Score, ScoringMatchLimit, nil

	}
	return
}

// userReputation computes the breakdown of a user's tier and score.
func (auth *AuthManager) userReputation(bondTier int64, score int32) *account.Reputation {
	var penalties int32
	if score < 0 {
		penalties = score / auth.penaltyThreshold
	}
	return &account.Reputation{
		BondedTier: bondTier,
		Penalties:  uint16(penalties),
		Score:      score,
	}
}

// tier computes a user's tier from their conduct score and bond tier.
func (auth *AuthManager) tier(bondTier int64, score int32) int64 {
	return auth.userReputation(bondTier, score).EffectiveTier()
}

// computeUserReputation computes the user's tier given the provided score
// weighed against known active bonds. Note that bondTier is not a specific
// asset, and is just for logging, and it may be removed or changed to a map by
// asset ID. For online users, this will also indicate if the tier changed; this
// will always return false for offline users.
func (auth *AuthManager) computeUserReputation(user account.AccountID, score int32) (r *account.Reputation, tierChanged, scoreChanged bool) {
	client := auth.user(user)
	if client == nil {
		// Offline. Load active bonds and legacyFeePaid flag from DB.
		lockTimeThresh := time.Now().Add(auth.bondExpiry)
		_, bonds := auth.storage.Account(user, lockTimeThresh)
		var bondTier int64
		for _, bond := range bonds {
			bondTier += int64(bond.Strength)
		}
		return auth.userReputation(bondTier, score), false, false
	}

	client.mtx.Lock()
	defer client.mtx.Unlock()
	wasTier := client.tier
	wasScore := client.score
	bondTier := client.bondTier()
	r = auth.userReputation(bondTier, score)
	client.tier = r.EffectiveTier()
	client.score = score
	scoreChanged = wasScore != score
	tierChanged = wasTier != client.tier

	return
}

// ComputeUserTier computes the user's tier from their active bonds and conduct
// score. The bondTier is also returned. The DB is always consulted for
// computing the conduct score. Summing bond amounts may access the DB if the
// user is not presently connected. The tier for an unknown user is -1.
func (auth *AuthManager) ComputeUserReputation(user account.AccountID) *account.Reputation {
	score, err := auth.loadUserScore(user)
	if err != nil {
		log.Errorf("failed to load user score: %v", err)
		return nil
	}
	r, _, _ := auth.computeUserReputation(user, score)
	return r
}

func (auth *AuthManager) registerMatchOutcome(user account.AccountID, misstep NoActionStep, mmid db.MarketMatchID, value uint64, refTime time.Time) (score int32) {
	violation := misstep.Violation()

	auth.violationMtx.Lock()
	if matchOutcomes, found := auth.matchOutcomes[user]; found {
		matchOutcomes.add(&matchOutcome{
			time:    refTime.UnixMilli(),
			mid:     mmid.MatchID,
			outcome: violation,
			value:   value,
			base:    mmid.Base,
			quote:   mmid.Quote,
		})
		score = auth.userScore(user)
		auth.violationMtx.Unlock()
		return
	}
	auth.violationMtx.Unlock()

	// The user is currently not connected and authenticated. When the user logs
	// back in, their history will be reloaded (loadUserScore) and their tier
	// recomputed, but compute their score now from DB for the caller.
	score, err := auth.loadUserScore(user)
	if err != nil {
		log.Errorf("Failed to load order and match outcomes for user %v: %v", user, err)
		return 0
	}

	return
}

// SwapSuccess registers the successful completion of a swap by the given user.
// TODO: provide lots instead of value, or convert to lots somehow. But, Swapper
// has no clue about lot size, and neither does DB!
func (auth *AuthManager) SwapSuccess(user account.AccountID, mmid db.MarketMatchID, value uint64, redeemTime time.Time) {
	score := auth.registerMatchOutcome(user, SwapSuccess, mmid, value, redeemTime)
	rep, tierChanged, scoreChanged := auth.computeUserReputation(user, score) // may raise tier
	effectiveTier := rep.EffectiveTier()
	log.Debugf("Match success for user %v: strikes %d, bond tier %v => tier %v",
		user, score, rep.BondedTier, effectiveTier)
	if tierChanged {
		log.Infof("SwapSuccess: tier change for user %v, strikes %d, bond tier %v => trading tier %v",
			user, score, rep.BondedTier, effectiveTier)
		go auth.sendTierChanged(user, rep, "successful swap completion")
	} else if scoreChanged {
		go auth.sendScoreChanged(user, rep)
	}
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
	violation := misstep.Violation()
	if violation == ViolationInvalid {
		log.Errorf("Invalid inaction step %d", misstep)
		return
	}
	score := auth.registerMatchOutcome(user, misstep, mmid, matchValue, refTime)

	// Recompute tier.
	rep, tierChanged, scoreChanged := auth.computeUserReputation(user, score)
	effectiveTier := rep.EffectiveTier()
	log.Infof("Match failure for user %v: %q (badness %v), strikes %d, bond tier %v => trading tier %v",
		user, violation, violation.Score(), score, rep.BondedTier, effectiveTier)
	// If their tier sinks below 1, unbook their orders and send a note.
	if tierChanged && effectiveTier < 1 {
		details := fmt.Sprintf("swap %v failure (%v) for order %v, new tier = %d",
			mmid.MatchID, misstep, oid, effectiveTier)
		auth.Penalize(user, account.FailureToAct, details)
	}
	if tierChanged {
		reason := fmt.Sprintf("swap failure for match %v order %v: %v", mmid.MatchID, oid, misstep)
		go auth.sendTierChanged(user, rep, reason)
	} else if scoreChanged {
		go auth.sendScoreChanged(user, rep)
	}
}

func (auth *AuthManager) registerPreimageOutcome(user account.AccountID, miss bool, oid order.OrderID, refTime time.Time) (score int32) {
	auth.violationMtx.Lock()
	piOutcomes, found := auth.preimgOutcomes[user]
	if found {
		piOutcomes.add(&preimageOutcome{
			time: refTime.UnixMilli(),
			oid:  oid,
			miss: miss,
		})
		score = auth.userScore(user)
		auth.violationMtx.Unlock()
		return
	}
	auth.violationMtx.Unlock()

	// The user is currently not connected and authenticated. When the user logs
	// back in, their history will be reloaded (loadUserScore) and their tier
	// recomputed, but compute their score now from DB for the caller.
	var err error
	score, err = auth.loadUserScore(user)
	if err != nil {
		log.Errorf("Failed to load order and match outcomes for user %v: %v", user, err)
		return 0
	}

	return
}

// PreimageSuccess registers an accepted preimage for the user.
func (auth *AuthManager) PreimageSuccess(user account.AccountID, epochEnd time.Time, oid order.OrderID) {
	score := auth.registerPreimageOutcome(user, false, oid, epochEnd)
	auth.computeUserReputation(user, score) // may raise tier, but no action needed
}

// MissedPreimage registers a missed preimage violation by the user.
func (auth *AuthManager) MissedPreimage(user account.AccountID, epochEnd time.Time, oid order.OrderID) {
	score := auth.registerPreimageOutcome(user, true, oid, epochEnd)
	if score < auth.penaltyThreshold {
		return
	}

	// Recompute tier.
	rep, tierChanged, scoreChanged := auth.computeUserReputation(user, score)
	effectiveTier := rep.EffectiveTier()
	log.Debugf("MissedPreimage: user %v strikes %d, bond tier %v => trading tier %v", user, score, rep.BondedTier, effectiveTier)
	// If their tier sinks below 1, unbook their orders and send a note.
	if tierChanged && effectiveTier < 1 {
		details := fmt.Sprintf("preimage for order %v not provided upon request: new tier = %d", oid, effectiveTier)
		auth.Penalize(user, account.PreimageReveal, details)
	}
	if tierChanged {
		reason := fmt.Sprintf("preimage not provided upon request for order %v", oid)
		go auth.sendTierChanged(user, rep, reason)
	} else if scoreChanged {
		go auth.sendScoreChanged(user, rep)
	}
}

// Penalize unbooks all of their orders, and notifies them of this action while
// citing the provided rule that corresponds to their most recent infraction.
// This method is to be used when a user's tier drops below 1.
// NOTE: There is now a 'tierchange' route for *any* tier change, but this
// method still handles unbooking of the user's orders.
func (auth *AuthManager) Penalize(user account.AccountID, lastRule account.Rule, extraDetails string) {
	// Unbook all of the user's orders across all markets.
	auth.unbookUserOrders(user)

	log.Debugf("User %v account penalized. Last rule broken = %v. Detail: %s", user, lastRule, extraDetails)

	// Notify user of penalty.
	details := "Ordering has been suspended for this account. Post additional bond to offset violations."
	details = fmt.Sprintf("%s\nLast Broken Rule Details: %s\n%s", details, lastRule.Description(), extraDetails)
	penalty := &msgjson.Penalty{
		Rule:    lastRule,
		Time:    uint64(time.Now().UnixMilli()),
		Details: details,
	}
	penaltyNote := &msgjson.PenaltyNote{
		Penalty: penalty,
	}
	penaltyNote.Sig = auth.SignMsg(penaltyNote.Serialize())
	note, err := msgjson.NewNotification(msgjson.PenaltyRoute, penaltyNote)
	if err != nil {
		log.Errorf("error creating penalty notification: %w", err)
		return
	}
	auth.Notify(user, note)
}

// AcctStatus indicates if the user is presently connected and their tier.
func (auth *AuthManager) AcctStatus(user account.AccountID) (connected bool, tier int64) {
	client := auth.user(user)
	if client == nil {
		// Load user info from DB.
		rep := auth.ComputeUserReputation(user)
		if rep != nil {
			tier = rep.EffectiveTier()
		}
		return
	}
	connected = true

	client.mtx.Lock()
	tier = client.tier
	client.mtx.Unlock()

	return
}

// ForgiveMatchFail forgives a user for a specific match failure, potentially
// allowing them to resume trading if their score becomes passing. NOTE: This
// may become deprecated with mesh, unless matches may be forgiven in some
// automatic network reconciliation process.
func (auth *AuthManager) ForgiveMatchFail(user account.AccountID, mid order.MatchID) (forgiven, unbanned bool, err error) {
	// Forgive the specific match failure in the DB.
	forgiven, err = auth.storage.ForgiveMatchFail(mid)
	if err != nil {
		return
	}

	// Reload outcomes from DB. NOTE: This does not use loadUserScore because we
	// also need to update the matchOutcomes map if the user is online.
	latestMatches, latestPreimageResults, latestFinished, err := auth.loadUserOutcomes(user)
	auth.violationMtx.Lock()
	_, online := auth.matchOutcomes[user]
	if online {
		auth.matchOutcomes[user] = latestMatches // other outcomes unchanged
	}
	auth.violationMtx.Unlock()

	// Recompute the user's score.
	score, _, _ := auth.integrateOutcomes(latestMatches, latestPreimageResults, latestFinished)

	// Recompute tier.
	rep, tierChanged, scoreChanged := auth.computeUserReputation(user, score)
	if tierChanged {
		go auth.sendTierChanged(user, rep, "swap failure forgiven")
	} else if scoreChanged {
		go auth.sendScoreChanged(user, rep)
	}

	unbanned = rep.EffectiveTier() > 0

	return
}

// CreatePrepaidBonds generates pre-paid bonds.
func (auth *AuthManager) CreatePrepaidBonds(n int, strength uint32, durSecs int64) ([][]byte, error) {
	coinIDs := make([][]byte, n)
	const prepaidBondIDLength = 16
	for i := 0; i < n; i++ {
		coinIDs[i] = encode.RandomBytes(prepaidBondIDLength)
	}
	lockTime := time.Now().Add(auth.bondExpiry).Add(time.Duration(durSecs) * time.Second)
	if err := auth.storage.StorePrepaidBonds(coinIDs, strength, lockTime.Unix()); err != nil {
		return nil, err
	}
	return coinIDs, nil
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

// sendTierChanged sends a tierchanged notification to an account.
func (auth *AuthManager) sendTierChanged(acctID account.AccountID, rep *account.Reputation, reason string) {
	effectiveTier := rep.EffectiveTier()
	log.Debugf("Sending tierchanged notification to %v, new tier = %d, reason = %v",
		acctID, effectiveTier, reason)
	tierChangedNtfn := &msgjson.TierChangedNotification{
		Tier:       effectiveTier,
		Reputation: rep,
		Reason:     reason,
	}
	auth.Sign(tierChangedNtfn)
	resp, err := msgjson.NewNotification(msgjson.TierChangeRoute, tierChangedNtfn)
	if err != nil {
		log.Error("TierChangeRoute encoding error: %v", err)
		return
	}
	if err = auth.Send(acctID, resp); err != nil {
		log.Warnf("Error sending tier changed notification to account %v: %v", acctID, err)
		// The user will need to 'connect' to see their current tier and bonds.
	}
}

// sendScoreChanged sends a scorechanged notification to an account.
func (auth *AuthManager) sendScoreChanged(acctID account.AccountID, rep *account.Reputation) {
	note := &msgjson.ScoreChangedNotification{
		Reputation: *rep,
	}
	auth.Sign(note)
	resp, err := msgjson.NewNotification(msgjson.ScoreChangeRoute, note)
	if err != nil {
		log.Error("TierChangeRoute encoding error: %v", err)
		return
	}
	if err = auth.Send(acctID, resp); err != nil {
		log.Warnf("Error sending score changed notification to account %v: %v", acctID, err)
		// The user will need to 'connect' to see their current tier and bonds.
	}
}

// sendBondExpired sends a bondexpired notification to an account.
func (auth *AuthManager) sendBondExpired(acctID account.AccountID, bond *db.Bond, rep *account.Reputation) {
	effectiveTier := rep.EffectiveTier()
	log.Debugf("Sending bondexpired notification to %v for bond %v (%s), new tier = %d",
		acctID, coinIDString(bond.AssetID, bond.CoinID), dex.BipIDSymbol(bond.AssetID), effectiveTier)
	bondExpNtfn := &msgjson.BondExpiredNotification{
		AssetID:    bond.AssetID,
		BondCoinID: bond.CoinID,
		AccountID:  acctID[:],
		Tier:       effectiveTier,
		Reputation: rep,
	}
	auth.Sign(bondExpNtfn)
	resp, err := msgjson.NewNotification(msgjson.BondExpiredRoute, bondExpNtfn)
	if err != nil {
		log.Error("BondExpiredRoute encoding error: %v", err)
		return
	}
	if err = auth.Send(acctID, resp); err != nil {
		log.Warnf("Error sending bond expired notification to account %v: %v", acctID, err)
		// The user will need to 'connect' to see their current tier and bonds.
	}
}

// checkBonds checks all connected users' bonds expiry and recomputes user tier
// on change. This should be run on a ticker.
func (auth *AuthManager) checkBonds() {
	lockTimeThresh := time.Now().Add(auth.bondExpiry).Unix()

	checkClientBonds := func(client *clientInfo) ([]*db.Bond, *account.Reputation) {
		client.mtx.Lock()
		defer client.mtx.Unlock()
		pruned, bondTier := client.pruneBonds(lockTimeThresh)
		if len(pruned) == 0 {
			return nil, nil // no tier change
		}

		auth.violationMtx.Lock()
		score := auth.userScore(client.acct.ID)
		auth.violationMtx.Unlock()

		client.tier = auth.tier(bondTier, score)
		client.score = score

		return pruned, auth.userReputation(bondTier, score)
	}

	auth.connMtx.RLock()
	defer auth.connMtx.RUnlock()

	type checkRes struct {
		rep   *account.Reputation
		bonds []*db.Bond
	}
	expiredBonds := make(map[account.AccountID]checkRes)
	for acct, client := range auth.users {
		pruned, rep := checkClientBonds(client)
		if len(pruned) > 0 {
			log.Infof("Pruned %d expired bonds for user %v, new bond tier = %d, new trading tier = %d",
				len(pruned), acct, rep.BondedTier, client.tier)
			expiredBonds[acct] = checkRes{rep, pruned}
		}
	}

	if len(expiredBonds) == 0 {
		return // skip the goroutine alloc
	}

	auth.wg.Add(1)
	go func() { // godspeed
		defer auth.wg.Done()
		for acct, prunes := range expiredBonds {
			for _, bond := range prunes.bonds {
				if err := auth.storage.DeleteBond(bond.AssetID, bond.CoinID); err != nil {
					log.Errorf("Failed to delete expired bond %v (%s) for user %v: %v",
						coinIDString(bond.AssetID, bond.CoinID), dex.BipIDSymbol(bond.AssetID), acct, err)
				}
				auth.sendBondExpired(acct, bond, prunes.rep)
			}
		}
	}()
}

// addBond registers a new active bond for an authenticated user. This only
// updates their clientInfo.{bonds,tier} fields. It does not touch the DB. If
// the user is not authenticated, it returns -1, -1.
func (auth *AuthManager) addBond(user account.AccountID, bond *db.Bond) *account.Reputation {
	client := auth.user(user)
	if client == nil {
		return nil // offline
	}

	auth.violationMtx.Lock()
	score := auth.userScore(user)
	auth.violationMtx.Unlock()

	client.mtx.Lock()
	defer client.mtx.Unlock()

	bondTier := client.addBond(bond)
	rep := auth.userReputation(bondTier, score)
	client.tier = rep.EffectiveTier()
	client.score = score

	return rep
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
			user, client.conn.Addr(), connID, oldClient.conn.Addr(), oldConnID)
		// When replacing with a new conn, manually deregister the old conn so
		// that when it disconnects it does not remove the new clientInfo.
		delete(auth.conns, oldConnID)
		oldClient.conn.Disconnect()
	}

	// When the conn goes down, automatically unregister the client.
	go func() {
		<-client.conn.Done()
		log.Debugf("Link down: id=%d, ip=%s.", client.conn.ID(), client.conn.Addr())
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
	delete(auth.orderOutcomes, user)
	auth.violationMtx.Unlock()
}

func matchStatusToViol(status order.MatchStatus) Violation {
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

// loadUserOutcomes returns user's latest match and preimage outcomes from order
// and swap data retrieved from the DB.
func (auth *AuthManager) loadUserOutcomes(user account.AccountID) (*latestMatchOutcomes, *latestPreimageOutcomes, *latestOrders, error) {
	// Load the N most recent matches resulting in success or an at-fault match
	// revocation for the user.
	matchOutcomes, err := auth.storage.CompletedAndAtFaultMatchStats(user, ScoringMatchLimit)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("CompletedAndAtFaultMatchStats: %w", err)
	}

	// Load the count of preimage misses in the N most recently placed orders.
	piOutcomes, err := auth.storage.PreimageStats(user, scoringOrderLimit)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("PreimageStats: %w", err)
	}

	latestMatches := newLatestMatchOutcomes(ScoringMatchLimit)
	for _, mo := range matchOutcomes {
		// The Fail flag qualifies MakerRedeemed, which is always success for
		// maker, but fail for taker if revoked.
		v := ViolationSwapSuccess
		if mo.Fail {
			v = matchStatusToViol(mo.Status)
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
	for _, po := range piOutcomes {
		latestPreimageResults.add(&preimageOutcome{
			time: po.Time,
			oid:  po.ID,
			miss: po.Miss,
		})
	}

	// Retrieve the user's N latest finished (completed or canceled orders)
	// and store them in a latestOrders.
	orderOutcomes, err := auth.loadRecentFinishedOrders(user, cancelThreshWindow)
	if err != nil {
		log.Errorf("Unable to retrieve user's executed cancels and completed orders: %v", err)
		return nil, nil, nil, err
	}

	return latestMatches, latestPreimageResults, orderOutcomes, nil
}

// MatchOutcome is a JSON-friendly version of db.MatchOutcome.
type MatchOutcome struct {
	ID     dex.Bytes `json:"matchID"`
	Status string    `json:"status"`
	Fail   bool      `json:"failed"`
	Stamp  int64     `json:"stamp"`
	Value  uint64    `json:"value"`
	BaseID uint32    `json:"baseID"`
	Quote  uint32    `json:"quoteID"`
}

// MatchFail is a failed match and the effect on the user's score.
type MatchFail struct {
	ID      dex.Bytes `json:"matchID"`
	Penalty uint32    `json:"penalty"`
}

// AccountMatchOutcomesN generates a list of recent match outcomes for a user.
func (auth *AuthManager) AccountMatchOutcomesN(user account.AccountID, n int) ([]*MatchOutcome, error) {
	dbOutcomes, err := auth.storage.CompletedAndAtFaultMatchStats(user, n)
	if err != nil {
		return nil, err
	}
	outcomes := make([]*MatchOutcome, len(dbOutcomes))
	for i, o := range dbOutcomes {
		outcomes[i] = &MatchOutcome{
			ID:     o.ID[:],
			Status: o.Status.String(),
			Fail:   o.Fail,
			Stamp:  o.Time,
			Value:  o.Value,
			BaseID: o.Base,
			Quote:  o.Quote,
		}
	}
	return outcomes, nil
}

func (auth *AuthManager) UserMatchFails(user account.AccountID, n int) ([]*MatchFail, error) {
	matchFails, err := auth.storage.UserMatchFails(user, n)
	if err != nil {
		return nil, err
	}
	fails := make([]*MatchFail, len(matchFails))
	for i, fail := range matchFails {
		fails[i] = &MatchFail{
			ID:      fail.ID[:],
			Penalty: uint32(-1 * matchStatusToViol(fail.Status).Score()),
		}
	}
	return fails, nil
}

// loadUserScore computes the user's current score from order and swap data
// retrieved from the DB. Use this instead of userScore if the user is offline.
func (auth *AuthManager) loadUserScore(user account.AccountID) (int32, error) {
	latestMatches, latestPreimageResults, latestFinished, err := auth.loadUserOutcomes(user)
	if err != nil {
		return 0, err
	}

	score, _, _ := auth.integrateOutcomes(latestMatches, latestPreimageResults, latestFinished)
	return score, nil
}

// handleConnect is the handler for the 'connect' route. The user is authorized,
// a response is issued, and a clientInfo is created or updated.
func (auth *AuthManager) handleConnect(conn comms.Link, msg *msgjson.Message) *msgjson.Error {
	connect := new(msgjson.Connect)
	err := msg.Unmarshal(&connect)
	if err != nil || connect == nil {
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
	lockTimeThresh := time.Now().Add(auth.bondExpiry).Truncate(time.Second)
	acctInfo, bonds := auth.storage.Account(user, lockTimeThresh)
	if acctInfo == nil {
		return &msgjson.Error{
			Code:    msgjson.AccountNotFoundError,
			Message: "no account found for account ID: " + connect.AccountID.String(),
		}
	}

	// Tier 0 accounts may connect to complete swaps, etc. but not place new
	// orders.

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

	// Compute the user's score, loading the preimage/order/match outcomes.
	latestMatches, latestPreimageResults, latestFinished, err := auth.loadUserOutcomes(user)
	if err != nil {
		log.Errorf("Failed to compute user %v score: %v", user, err)
		return &msgjson.Error{
			Code:    msgjson.RPCInternalError,
			Message: "DB error",
		}
	}
	score, successCount, piMissCount := auth.integrateOutcomes(latestMatches, latestPreimageResults, latestFinished)

	successScore := successCount * successScore
	piMissScore := piMissCount * preimageMissScore
	// score = violationScore + piMissScore + successScore
	violationScore := score - piMissScore - successScore // work backwards as per above comment
	log.Debugf("User %v score = %d:%d (%d successes) - %d (violations) - %d (%d preimage misses) ",
		user, score, successScore, successCount, -violationScore, -piMissScore, piMissCount)

	// Make outcome entries for the user.
	auth.violationMtx.Lock()
	auth.matchOutcomes[user] = latestMatches
	auth.preimgOutcomes[user] = latestPreimageResults
	auth.orderOutcomes[user] = latestFinished
	auth.violationMtx.Unlock()

	client := &clientInfo{
		acct:         acctInfo,
		conn:         conn,
		respHandlers: respHandlers,
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
			ServerTime:   uint64(match.Epoch.End().UnixMilli()),
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

	// Prepare bond info for response.
	var bondTier int64
	activeBonds := make([]*db.Bond, 0, len(bonds)) // some may have just expired
	msgBonds := make([]*msgjson.Bond, 0, len(bonds))
	for _, bond := range bonds {
		// Double check the DB backend's thresholding.
		lockTime := time.Unix(bond.LockTime, 0)
		if lockTime.Before(lockTimeThresh) {
			log.Warnf("Loaded expired bond from DB (%v), lockTime %v is before %v",
				coinIDString(bond.AssetID, bond.CoinID), lockTime, lockTimeThresh)
			continue // will be expired on next prune
		}
		bondTier += int64(bond.Strength)
		expireTime := lockTime.Add(-auth.bondExpiry)
		msgBonds = append(msgBonds, &msgjson.Bond{
			Version:  bond.Version,
			Amount:   uint64(bond.Amount),
			Expiry:   uint64(expireTime.Unix()),
			CoinID:   bond.CoinID,
			AssetID:  bond.AssetID,
			Strength: bond.Strength, // Added with v2 reputation
		})
		activeBonds = append(activeBonds, bond)
	}

	// Ensure tier and filtered bonds agree.
	rep := auth.userReputation(bondTier, score)
	client.tier = rep.EffectiveTier()
	client.score = score
	client.bonds = activeBonds

	// Sign and send the connect response.
	sig := auth.SignMsg(sigMsg)
	resp := &msgjson.ConnectResult{
		Sig:                 sig,
		ActiveOrderStatuses: msgOrderStatuses,
		ActiveMatches:       msgMatches,
		Score:               score,
		ActiveBonds:         msgBonds,
		Reputation:          rep,
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
		log.Error("Failed to send connect response: " + err.Error())
		return nil
	}

	log.Infof("Authenticated account %v from %v with %d active orders, %d active matches, tier = %v, "+
		"bond tier = %v, score = %v",
		user, conn.Addr(), len(msgOrderStatuses), len(msgMatches), client.tier, bondTier, score)
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
	cancels, err := auth.storage.ExecutedCancelsForUser(aid, N)
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
	for _, c := range cancels {
		tid := c.TargetID
		latestFinished.add(&oidStamped{
			OrderID:  c.ID,
			time:     c.MatchTime,
			target:   &tid,
			epochGap: c.EpochGap,
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

// getTxData gets the tx data for the coin ID.
func (auth *AuthManager) getTxData(assetID uint32, coinID []byte) ([]byte, error) {
	txDataSrc, found := auth.txDataSources[assetID]
	if !found {
		return nil, fmt.Errorf("no tx data source for asset ID %d", assetID)
	}
	return txDataSrc(coinID)
}

// handleMatchStatus handles requests to the 'match_status' route.
func (auth *AuthManager) handleMatchStatus(conn comms.Link, msg *msgjson.Message) *msgjson.Error {
	client := auth.conn(conn)
	if client == nil {
		return msgjson.NewError(msgjson.UnauthorizedConnection,
			"cannot use route 'match_status' on an unauthorized connection")
	}
	var matchReqs []msgjson.MatchRequest
	err := msg.Unmarshal(&matchReqs)
	if err != nil || matchReqs == nil /* null Payload */ {
		return msgjson.NewError(msgjson.RPCParseError, "error parsing match_status request")
	}
	// NOTE: If len(matchReqs)==0 but not nil, Payload was `[]`, demanding a
	// positive response with `[]` in ResponsePayload.Result.

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

	results := make([]*msgjson.MatchStatusResult, 0, count) // should be non-nil even for count==0
	for _, mm := range mkts {
		statuses, err := auth.storage.MatchStatuses(client.acct.ID, mm.base, mm.quote, mm.idList())
		// no results is not an error
		if err != nil {
			log.Errorf("MatchStatuses error: acct = %s, base = %d, quote = %d, matchIDs = %v: %v",
				client.acct.ID, mm.base, mm.quote, mm.matchIDs, err)
			return msgjson.NewError(msgjson.RPCInternalError, "DB error")
		}
		for _, status := range statuses {
			var makerTxData, takerTxData []byte
			var assetID uint32
			switch {
			case status.IsTaker && status.Status == order.MakerSwapCast:
				assetID = mm.base
				if status.TakerSell {
					assetID = mm.quote
				}
				makerTxData, err = auth.getTxData(assetID, status.MakerSwap)
				if err != nil {
					log.Errorf("failed to get maker tx data for %s %s: %v", dex.BipIDSymbol(assetID),
						coinIDString(assetID, status.MakerSwap), err)
					return msgjson.NewError(msgjson.RPCInternalError, "blockchain retrieval error")
				}
			case status.IsMaker && status.Status == order.TakerSwapCast:
				assetID = mm.quote
				if status.TakerSell {
					assetID = mm.base
				}
				takerTxData, err = auth.getTxData(assetID, status.TakerSwap)
				if err != nil {
					log.Errorf("failed to get taker tx data for %s %s: %v", dex.BipIDSymbol(assetID),
						coinIDString(assetID, status.TakerSwap), err)
					return msgjson.NewError(msgjson.RPCInternalError, "blockchain retrieval error")
				}
			}

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
				MakerTxData:   makerTxData,
				TakerTxData:   takerTxData,
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

	var orderReqs []*msgjson.OrderStatusRequest
	err := msg.Unmarshal(&orderReqs)
	if err != nil {
		return msgjson.NewError(msgjson.RPCParseError, "error parsing order_status request")
	}
	if len(orderReqs) == 0 { // includes null and [] Payload
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

func coinIDString(assetID uint32, coinID []byte) string {
	s, err := asset.DecodeCoinID(assetID, coinID)
	if err != nil {
		return "unparsed:" + hex.EncodeToString(coinID)
	}
	return s
}
