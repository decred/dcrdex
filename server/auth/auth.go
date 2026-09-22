// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

// Package auth authenticates clients, manages their sessions, and handles
// account bonds and reputation.
package auth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
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
	"decred.org/dcrdex/server/mesh"
	"decred.org/dcrdex/server/meshevents"

	"github.com/decred/dcrd/dcrec/secp256k1/v4"
	"github.com/decred/dcrd/dcrec/secp256k1/v4/ecdsa"
)

const (
	cancelThreshWindow = 100 // spec
	ScoringMatchLimit  = 60  // last N matches (success or at-fault fail) to be considered in swap inaction scoring
	scoringOrderLimit  = 40  // last N orders to be considered in preimage miss scoring

	maxIDsPerOrderStatusRequest = 10_000

	// freeCancelThreshold is the minimum number of epochs a user should wait before
	// placing a cancel order, if they want to avoid penalization. It is set to 2,
	// which means if a user places a cancel order in the same epoch as its limit
	// order, or the next epoch, the user will be penalized. This value is chosen
	// because it is the minimum value such that the order remains booked for at
	// least one full epoch and one full match cycle.
	freeCancelThreshold = 2
)

var (
	ErrUserNotConnected = dex.ErrorKind("user not connected")
)

const (
	reputationForgivenessCommandTimeout = 2 * time.Minute
	reputationEventRefreshTimeout       = 5 * time.Second
)

// Storage updates and fetches account-related data from what is presumably a
// database.
type Storage interface {
	// Account returns the account and bonds whose lock time is at least
	// lockTimeThresh. It returns a nil account and nil error if the account
	// does not exist.
	Account(ctx context.Context, acctID account.AccountID, lockTimeThresh time.Time) (acct *account.Account, bonds []*db.Bond, err error)

	// ApplyBondPostedEvent applies the auth bond_posted event in one database
	// transaction.
	ApplyBondPostedEvent(context.Context, *db.EventLogMeta, *meshevents.BondPostedEvent, int, int, int) (*db.BondPostedResult, error)
	ApplyPrepaidBondsCreatedEvent(context.Context, *db.EventLogMeta, *meshevents.PrepaidBondsCreatedEvent) (*db.EventLogEntry, error)

	FetchPrepaidBond(bondCoinID []byte) (strength uint32, lockTime int64, err error)

	AccountInfo(aid account.AccountID) (*db.Account, error)

	UserOrderStatuses(aid account.AccountID, base, quote uint32, oids []order.OrderID) ([]*db.OrderStatus, error)
	ActiveUserOrderStatuses(aid account.AccountID) ([]*db.OrderStatus, error)
	CompletedAndAtFaultMatchStats(aid account.AccountID, lastN int) ([]*db.MatchOutcome, error)
	UserMatchFails(aid account.AccountID, lastN int) ([]*db.MatchFail, error)
	AllActiveUserMatches(aid account.AccountID) ([]*db.MatchData, error)
	MatchStatuses(aid account.AccountID, base, quote uint32, matchIDs []order.MatchID) ([]*db.MatchStatus, error)

	db.ReputationArchiver
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

// proxyResponseKey identifies a pending proxied request by account and request ID.
type proxyResponseKey struct {
	user account.AccountID
	id   uint64
}

// clientInfo represents a DEX client, including account information and last
// known comms.Link.
type clientInfo struct {
	acct         *account.Account
	conn         comms.Link
	respHandlers map[uint64]*respHandler
	mtx          sync.Mutex
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

// AuthManager authenticates clients, manages their sessions, and processes
// bond and reputation requests. It signs outgoing messages and routes
// communication to authenticated clients.
type AuthManager struct {
	wg          sync.WaitGroup
	ctx         context.Context
	storage     Storage
	signer      Signer
	parseBondTx BondTxParser
	checkBond   BondCoinChecker // fidelity bond amount, lockTime, acct, and confs
	route       func(route string, handler comms.MsgHandler)

	bondExpiry time.Duration // a bond is expired when time.Until(lockTime) < bondExpiry
	bondAssets map[uint32]*msgjson.BondAsset

	freeCancels      bool
	penaltyThreshold int32
	cancelThresh     float64

	// rep caches account scores and bonds, including expired bonds.
	rep *repCache

	// latencyQ is a queue for fee coin waiters to deal with latency.
	latencyQ *wait.TickerQueue

	bondWaiterMtx sync.Mutex
	bondWaiterIdx map[string]struct{}

	connMtx sync.RWMutex
	users   map[account.AccountID]*clientInfo
	conns   map[uint64]*clientInfo

	// repNotifyMtx serializes reputation updates sent to clients.
	repNotifyMtx sync.Mutex

	txDataSources map[uint32]TxDataSource

	mesh MeshService

	proxyRespMtx      sync.Mutex
	proxyRespHandlers map[proxyResponseKey]*respHandler

	connectCallbackMtx sync.RWMutex
	connectCallbacks   []func(account.AccountID)
}

// violation badness
const (
	// preimage miss
	preimageMissScore    = -2 // book spoof, no match, no stuck funds
	preimageSuccessScore = 0

	// failure to act violations
	matchCompletedScore  = 1   // offsets the violations
	noSwapAsMakerScore   = -4  // no swap broadcast at NewlyMatched
	noAddrAsTakerScore   = -4  // taker failed to provide per-match address in time, blocking the maker
	noSwapAsTakerScore   = -11 // maker has contract stuck for 20 hrs
	noRedeemAsMakerScore = -7  // taker has contract stuck for 8 hrs
	noRedeemAsTakerScore = -1  // just dumb, counterparty not inconvenienced

	// cancel rate exceeds threshold
	excessiveCancelsScore = -5
	orderCompleteScore    = 0

	DefaultPenaltyThreshold = 20
)

type Outcome = db.Outcome

var outcomeScores = map[Outcome]int32{
	db.OutcomeForgiven: 1,

	// preimage results
	db.OutcomePreimageMiss:    preimageMissScore,
	db.OutcomePreimageSuccess: preimageSuccessScore,

	// match results
	db.OutcomeSwapSuccess:     matchCompletedScore,
	db.OutcomeNoSwapAsMaker:   noSwapAsMakerScore,
	db.OutcomeNoSwapAsTaker:   noSwapAsTakerScore,
	db.OutcomeNoRedeemAsMaker: noRedeemAsMakerScore,
	db.OutcomeNoRedeemAsTaker: noRedeemAsTakerScore,
	db.OutcomeNoAddrAsTaker:   noAddrAsTakerScore,

	// orders cancellations (completed/canceled)
	db.OutcomeOrderCanceled: excessiveCancelsScore,
	db.OutcomeOrderComplete: orderCompleteScore,

	db.OutcomeInvalid: 0,
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

	// BondExpiry is the minimum remaining lock time, in seconds, for a bond
	// to contribute to the account's tier.
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

	CancelThreshold float64
	FreeCancels     bool

	// PenaltyThreshold is the number of negative score points per reputation
	// penalty. Each penalty reduces the account's effective tier by one.
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
		storage:           cfg.Storage,
		signer:            cfg.Signer,
		bondAssets:        bondAssets,
		bondExpiry:        time.Duration(cfg.BondExpiry) * time.Second,
		parseBondTx:       cfg.BondTxParser, // e.g. dcr's ParseBondTx
		checkBond:         cfg.BondChecker,  // e.g. dcr's BondCoin
		route:             cfg.Route,
		freeCancels:       cfg.FreeCancels,
		penaltyThreshold:  penaltyThreshold,
		cancelThresh:      cfg.CancelThreshold,
		rep:               newRepCache(repCacheCapacity, repCacheMaxAge),
		latencyQ:          wait.NewTickerQueue(recheckInterval),
		users:             make(map[account.AccountID]*clientInfo),
		conns:             make(map[uint64]*clientInfo),
		bondWaiterIdx:     make(map[string]struct{}),
		txDataSources:     cfg.TxDataSources,
		proxyRespHandlers: make(map[proxyResponseKey]*respHandler),
	}

	cfg.Storage.SetReputationInputsListener(func(users ...account.AccountID) {
		if len(users) == 0 {
			return
		}
		auth.rep.invalidate(users...)
		notifyUsers := append([]account.AccountID(nil), users...)
		go auth.notifyReputationInputsChanged(notifyUsers)
	})

	// Unauthenticated
	cfg.Route(msgjson.ConnectRoute, auth.handleConnect)
	cfg.Route(msgjson.PostBondRoute, auth.handlePostBond)
	cfg.Route(msgjson.PreValidateBondRoute, auth.handlePreValidateBond)
	cfg.Route(msgjson.MatchStatusRoute, auth.handleMatchStatus)
	cfg.Route(msgjson.OrderStatusRoute, auth.handleOrderStatus)
	return auth
}

// SetMeshService configures the mesh service. It must be set before the comms
// routes serve traffic.
func (auth *AuthManager) SetMeshService(svc MeshService) {
	auth.mesh = svc
}

// GraceLimit returns the number of initial orders allowed for a new user before
// the cancellation rate threshold is enforced.
func (auth *AuthManager) GraceLimit() int {
	// Grace period if: total/(1+total) <= thresh OR total <= thresh/(1-thresh).
	return int(math.Round(1e8*auth.cancelThresh/(1-auth.cancelThresh))) / 1e8
}

// These callbacks are retained until market and swap record reputation through mesh events.
func (auth *AuthManager) RecordCancel(user account.AccountID, oid, target order.OrderID, epochGap int32, t time.Time) {
}

func (auth *AuthManager) RecordCompletedOrder(user account.AccountID, oid order.OrderID, t time.Time) {
}

// Connect runs the AuthManager until the context is canceled. Satisfies the
// dex.Connector interface.
func (auth *AuthManager) Connect(ctx context.Context) (*sync.WaitGroup, error) {
	auth.ctx = ctx
	auth.wg.Add(1)
	go func() {
		defer auth.wg.Done()
		auth.latencyQ.Run(ctx)
	}()

	auth.wg.Add(1)
	go func() {
		defer auth.wg.Done()
		ticker := time.NewTicker(30 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				hits, misses, invalidations, evictions := auth.rep.stats()
				log.Debugf("Reputation cache: %d hits, %d misses, %d invalidations, %d evictions",
					hits, misses, invalidations, evictions)
			case <-ctx.Done():
				return
			}
		}
	}()

	// TODO: wait for running comms route handlers and other DB writers.
	return &auth.wg, nil
}

// OnConnect registers a callback that is invoked after a user successfully
// connects (or reconnects). This allows other subsystems (e.g. the Swapper) to
// perform actions when a user comes online, such as re-sending missed
// notifications.
func (auth *AuthManager) OnConnect(f func(account.AccountID)) {
	auth.connectCallbackMtx.Lock()
	auth.connectCallbacks = append(auth.connectCallbacks, f)
	auth.connectCallbackMtx.Unlock()
}

// Route registers a message handler that requires an authenticated client.
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

// Send sends a response or notification to the user on this node or the peer.
// Local sends are queued; proxied sends wait for the peer's relay result.
// It returns ErrUserNotConnected if the user is absent or no peer can relay.
func (auth *AuthManager) Send(user account.AccountID, msg *msgjson.Message) error {
	if client := auth.user(user); client != nil {
		return auth.send(client, msg)
	}
	ctx, cancel := context.WithTimeout(context.Background(), DefaultRequestTimeout)
	defer cancel()
	err := auth.mesh.ProxyClientMessage(ctx, &mesh.ClientProxyMessage{
		User:            user,
		Msg:             msg,
		DeliverToClient: true,
	})
	if errors.Is(err, mesh.ErrClientProxyUnavailable) || errors.Is(err, mesh.ErrClientNotConnected) {
		log.Debugf("Send requested for disconnected user %v", user)
		return dex.NewError(ErrUserNotConnected, user.String())
	}
	return err
}

// Notify is identical to Send and remains only to keep existing callers compiling.
// Remove it once all call sites use Send.
func (auth *AuthManager) Notify(acctID account.AccountID, msg *msgjson.Message) error {
	return auth.Send(acctID, msg)
}

// SendIfLocal sends a message to a locally connected user.
// It returns nil if the user is not connected locally.
func (auth *AuthManager) SendIfLocal(user account.AccountID, msg *msgjson.Message) error {
	client := auth.user(user)
	if client == nil {
		return nil
	}
	return auth.send(client, msg)
}

func (auth *AuthManager) send(client *clientInfo, msg *msgjson.Message) error {
	err := client.conn.Send(msg)
	if err != nil {
		log.Debugf("error sending on link: %v", err)
		// Remove client assuming connection is broken, requiring reconnect.
		auth.removeClient(client)
	}
	return err
}

// Requests

// DefaultRequestTimeout is the default wait for a client response.
const DefaultRequestTimeout = 30 * time.Second

// Request sends a request using DefaultRequestTimeout. See RequestWithTimeout
// for delivery, callback, and error behavior.
func (auth *AuthManager) Request(user account.AccountID, msg *msgjson.Message, f func(comms.Link, *msgjson.Message)) error {
	return auth.request(user, msg, f, DefaultRequestTimeout, func() {})
}

// RequestWithTimeout sends a request locally or through the peer. The response
// handler receives the original request ID and a nil link for proxied responses.
// Unanswered requests call expire after expireTimeout; nonpositive timeouts use
// DefaultRequestTimeout. Local send failures return an error. Proxy failures
// call expire asynchronously. Late responses are ignored.
func (auth *AuthManager) RequestWithTimeout(user account.AccountID, msg *msgjson.Message, f func(comms.Link, *msgjson.Message),
	expireTimeout time.Duration, expire func()) error {
	return auth.request(user, msg, f, expireTimeout, expire)
}

// RequestIfLocal sends a request to a locally connected user.
// It returns nil without sending if the user is not connected locally.
func (auth *AuthManager) RequestIfLocal(user account.AccountID, msg *msgjson.Message, f func(comms.Link, *msgjson.Message)) error {
	client := auth.user(user)
	if client == nil {
		return nil
	}
	return auth.requestLocal(client, msg, f, DefaultRequestTimeout, func() {})
}

func (auth *AuthManager) request(user account.AccountID, msg *msgjson.Message, f func(comms.Link, *msgjson.Message),
	expireTimeout time.Duration, expire func()) error {
	if expireTimeout <= 0 {
		expireTimeout = DefaultRequestTimeout
	}
	if client := auth.user(user); client != nil {
		return auth.requestLocal(client, msg, f, expireTimeout, expire)
	}
	return auth.requestPeer(user, msg, f, expireTimeout, expire)
}

func (auth *AuthManager) requestLocal(client *clientInfo, msg *msgjson.Message, f func(comms.Link, *msgjson.Message),
	expireTimeout time.Duration, expire func()) error {

	client.logReq(msg.ID, f, expireTimeout, expire)
	// client.logReq handles expiration, so the connection needs no expiration callback.
	err := client.conn.Request(msg, auth.handleResponse, expireTimeout, func() {})
	if err != nil {
		log.Debugf("error sending request ID %d: %v", msg.ID, err)
		// Cancel expiration because the caller handles the send error.
		client.respHandler(msg.ID)
		// Remove client assuming connection is broken, requiring reconnect.
		auth.removeClient(client)
	}
	return err
}

// requestPeer registers the response deadline before relaying the request.
// Relay failures call expire asynchronously, just like an unanswered request.
func (auth *AuthManager) requestPeer(user account.AccountID, msg *msgjson.Message, f func(comms.Link, *msgjson.Message),
	expireTimeout time.Duration, expire func()) error {
	timeoutMS := uint64(expireTimeout / time.Millisecond)

	handler := auth.registerProxyRespHandler(user, msg.ID, f, expireTimeout, expire)
	meshSvc := auth.mesh
	go func() {
		err := meshSvc.ProxyClientMessage(context.Background(), &mesh.ClientProxyMessage{
			User:      user,
			Msg:       msg,
			TimeoutMS: timeoutMS,
		})
		if err != nil {
			log.Debugf("proxied request %q for user %v failed: %v", msg.Route, user, err)
			if auth.removeProxyRespHandler(proxyResponseKey{user: user, id: msg.ID}, handler) {
				expire()
			}
		}
	}()

	return nil
}

// HandleProxiedClientMessage delivers a message to a local client or
// dispatches a client response to its pending request handler.
func (auth *AuthManager) HandleProxiedClientMessage(_ context.Context, req *mesh.ClientProxyMessage) error {
	if req == nil {
		return fmt.Errorf("nil proxied client message")
	}
	if req.Msg == nil {
		return fmt.Errorf("nil proxied client message payload")
	}

	switch req.Msg.Type {
	case msgjson.Request:
		return auth.proxyClientRequest(req)
	case msgjson.Response:
		// Responses either go to a local client or complete a request
		// this node sent through a peer.
		if req.DeliverToClient {
			return auth.sendProxiedClientMessage(req.User, req.Msg)
		}
		if handler := auth.takeProxyRespHandler(req.User, req.Msg.ID); handler != nil {
			go handler.f(nil, req.Msg)
			return nil
		}
		log.Debugf("Dropping late proxied client response %d for user %v", req.Msg.ID, req.User)
		return nil
	case msgjson.Notification:
		return auth.sendProxiedClientMessage(req.User, req.Msg)
	default:
		return fmt.Errorf("unsupported proxied client message type %d", req.Msg.Type)
	}
}

func (auth *AuthManager) proxyClientRequest(req *mesh.ClientProxyMessage) error {
	client := auth.user(req.User)
	if client == nil {
		return mesh.ErrClientNotConnected
	}
	timeout := time.Duration(req.TimeoutMS) * time.Millisecond
	if timeout <= 0 {
		timeout = DefaultRequestTimeout
	}

	peerRequestID := req.Msg.ID
	clientRequest := *req.Msg
	// Avoid collisions with requests generated by this node.
	clientRequest.ID = comms.NextID()

	handleResponse := func(_ comms.Link, resp *msgjson.Message) {
		if resp == nil {
			log.Debugf("proxied client response %d for user %v was nil", peerRequestID, req.User)
			return
		}
		peerResponse := *resp
		peerResponse.ID = peerRequestID
		meshSvc := auth.mesh
		go func() {
			err := meshSvc.ProxyClientMessage(context.Background(), &mesh.ClientProxyMessage{
				User: req.User,
				Msg:  &peerResponse,
			})
			if err != nil {
				log.Debugf("proxied client response %d for user %v failed: %v", peerRequestID, req.User, err)
			}
		}()
	}
	return auth.requestLocal(client, &clientRequest, handleResponse, timeout, func() {
		log.Debugf("proxied client request %q for user %v timed out locally", req.Msg.Route, req.User)
	})
}

func (auth *AuthManager) sendProxiedClientMessage(user account.AccountID, msg *msgjson.Message) error {
	client := auth.user(user)
	if client == nil {
		log.Debugf("Proxied send requested for disconnected user %v", user)
		return mesh.ErrClientNotConnected
	}
	return auth.send(client, msg)
}

func (auth *AuthManager) registerProxyRespHandler(user account.AccountID, id uint64, f func(comms.Link, *msgjson.Message), expireTimeout time.Duration, expire func()) *respHandler {
	key := proxyResponseKey{user: user, id: id}
	handler := &respHandler{f: f}
	auth.proxyRespMtx.Lock()
	defer auth.proxyRespMtx.Unlock()
	if previous := auth.proxyRespHandlers[key]; previous != nil {
		previous.expire.Stop()
	}
	handler.expire = time.AfterFunc(expireTimeout, func() {
		// A replacement may have registered after this timer started firing.
		if auth.removeProxyRespHandler(key, handler) {
			expire()
		}
	})
	auth.proxyRespHandlers[key] = handler
	return handler
}

// takeProxyRespHandler removes the response handler and stops its timer.
func (auth *AuthManager) takeProxyRespHandler(user account.AccountID, id uint64) *respHandler {
	key := proxyResponseKey{user: user, id: id}
	auth.proxyRespMtx.Lock()
	defer auth.proxyRespMtx.Unlock()

	handler := auth.proxyRespHandlers[key]
	if handler == nil {
		return nil
	}
	handler.expire.Stop()
	delete(auth.proxyRespHandlers, key)
	return handler
}

// removeProxyRespHandler removes key only if it still refers to handler.
func (auth *AuthManager) removeProxyRespHandler(key proxyResponseKey, handler *respHandler) bool {
	auth.proxyRespMtx.Lock()
	defer auth.proxyRespMtx.Unlock()
	if auth.proxyRespHandlers[key] != handler {
		return false
	}
	handler.expire.Stop()
	delete(auth.proxyRespHandlers, key)
	return true
}

func (auth *AuthManager) integrateOutcomes(
	matchOutcomes *latestOutcomes[*db.MatchResult],
	preimgOutcomes *latestOutcomes[*db.PreimageOutcome],
	orderOutcomes *latestOutcomes[*db.OrderOutcome],
) (score, successCount, piMissCount int32) {

	if matchOutcomes != nil {
		matchCounts := matchOutcomes.binViolations()
		for v, count := range matchCounts {
			score += outcomeScores[v] * int32(count)
		}
		successCount = int32(matchCounts[db.OutcomeSwapSuccess])
	}
	if preimgOutcomes != nil {
		counts := preimgOutcomes.binViolations()
		piMissCount = int32(counts[db.OutcomePreimageMiss])
		score += outcomeScores[db.OutcomePreimageMiss] * piMissCount
	}
	if !auth.freeCancels {
		counts := orderOutcomes.binViolations()
		successes, cancels := int32(counts[db.OutcomeOrderComplete]), int32(counts[db.OutcomeOrderCanceled])
		totalOrds := int(successes + cancels)
		if totalOrds > auth.GraceLimit() {
			cancelRate := float64(cancels) / float64(totalOrds)
			if cancelRate > auth.cancelThresh {
				score += outcomeScores[db.OutcomeOrderCanceled]
			}
		}
	}
	return
}

// UserReputationAt returns the user's tier, score, and maximum score,
// with bond expiry evaluated at asOf.
func (auth *AuthManager) UserReputationAt(user account.AccountID, asOf time.Time) (tier int64, score, maxScore int32, err error) {
	maxScore = ScoringMatchLimit
	data, err := auth.rep.get(auth.ctx, user, auth.loadUserRepData)
	if err != nil {
		return
	}
	if !data.exists {
		return 0, data.score, maxScore, nil
	}
	r := auth.reputationFromData(data, asOf.Add(auth.bondExpiry).Unix())
	return r.EffectiveTier(), r.Score, maxScore, nil
}

// UserReputation returns the user's tier, score, and maximum score at the current time.
func (auth *AuthManager) UserReputation(user account.AccountID) (tier int64, score, maxScore int32, err error) {
	return auth.UserReputationAt(user, time.Now())
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

// reputationFromData calculates reputation using bonds locked until at least
// bondExpiryThreshold.
func (auth *AuthManager) reputationFromData(data *repData, bondExpiryThreshold int64) *account.Reputation {
	rep := auth.userReputation(data.bondTier(bondExpiryThreshold), data.score)
	rep.BondExpiryThreshold = bondExpiryThreshold
	return rep
}

func (auth *AuthManager) loadUserReputation(ctx context.Context, user account.AccountID) (*account.Reputation, error) {
	data, err := auth.rep.get(ctx, user, auth.loadUserRepData)
	if err != nil {
		return nil, err
	}
	if !data.exists {
		return nil, nil
	}
	bondExpiryThreshold := time.Now().Add(auth.bondExpiry).Unix()
	return auth.reputationFromData(data, bondExpiryThreshold), nil
}

func (auth *AuthManager) loadUserReputationWithTimeout(ctx context.Context, user account.AccountID) (*account.Reputation, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	readCtx, cancel := context.WithTimeout(ctx, reputationEventRefreshTimeout)
	defer cancel()
	return auth.loadUserReputation(readCtx, user)
}

// loadUserRepData loads a user's score and bonds.
func (auth *AuthManager) loadUserRepData(ctx context.Context, user account.AccountID) (*repData, error) {
	score, err := auth.loadUserScoreContext(ctx, user)
	if err != nil {
		return nil, err
	}

	// TODO(mesh): Remove old expired bonds once we know they are no longer
	// needed to replay events. All nodes must agree on when to remove them.
	// Until then, the database, snapshots, and cached bond lists keep growing.
	acct, bonds, err := auth.storage.Account(ctx, user, time.Time{})
	if err != nil {
		return nil, err
	}

	return newRepData(acct != nil, score, bonds), nil
}

// ComputeUserReputation computes the user's reputation from their active bonds
// and conduct score. Returns nil for an unknown user, and also (with the
// error only logged) when the reputation load fails; use AcctRepStatus to
// distinguish the two.
func (auth *AuthManager) ComputeUserReputation(user account.AccountID) *account.Reputation {
	rep, err := auth.loadUserReputation(auth.ctx, user)
	if err != nil {
		log.Errorf("failed to load user reputation: %v", err)
		return nil
	}
	return rep
}

// AcctRepStatus reports local connectivity and reputation. Unlike AcctStatus,
// a reputation load failure is returned as an error, not as tier 0. For an
// unknown account, rep and err are both nil.
func (auth *AuthManager) AcctRepStatus(user account.AccountID) (connected bool, rep *account.Reputation, err error) {
	connected = auth.user(user) != nil
	rep, err = auth.loadUserReputation(auth.ctx, user)
	return
}

func (auth *AuthManager) SwapSuccess(user account.AccountID, mmid db.MarketMatchID, value uint64, redeemTime time.Time) {
}

func (auth *AuthManager) Inaction(user account.AccountID, outcome Outcome, mmid db.MarketMatchID, matchValue uint64, refTime time.Time, oid order.OrderID) {
}

func (auth *AuthManager) PreimageSuccess(user account.AccountID, epochEnd time.Time, oid order.OrderID) {
}

func (auth *AuthManager) MissedPreimage(user account.AccountID, epochEnd time.Time, oid order.OrderID) {
}

// AcctStatus indicates if the user is presently connected and their tier.
func (auth *AuthManager) AcctStatus(user account.AccountID) (connected bool, tier int64) {
	connected = auth.user(user) != nil
	rep := auth.ComputeUserReputation(user)
	if rep != nil {
		tier = rep.EffectiveTier()
	}
	return
}

// ForgiveMatchFail forgives a user's match failure. It reports whether the match
// was forgiven and whether refreshed reputation confirms a positive effective
// tier. If reputation cannot be determined, unbanned is false.
func (auth *AuthManager) ForgiveMatchFail(user account.AccountID, mid order.MatchID) (forgiven, unbanned bool, err error) {
	result, err := auth.executeReputationForgivenessCommand(context.Background(), &meshevents.ReputationForgivenEvent{
		AccountID: user,
		Scope:     meshevents.ReputationForgivenessScopeMatch,
		MatchID:   &mid,
	})
	if err != nil {
		return false, false, err
	}
	return result.Forgiven, result.Unbanned, nil
}

// CreatePrepaidBonds creates and stores n prepaid bond tokens with the given
// strength and lifetime in seconds, and returns their coin IDs.
func (auth *AuthManager) CreatePrepaidBonds(n int, strength uint32, durSecs int64) ([][]byte, error) {
	if n < 0 {
		return nil, fmt.Errorf("pre-paid bond count cannot be negative")
	}
	if n == 0 {
		return [][]byte{}, nil
	}
	if auth.mesh == nil {
		return nil, fmt.Errorf("mesh service not configured")
	}

	reqMsg, err := msgjson.NewRequest(comms.NextID(), commandKindCreatePrepaidBonds, &createPrepaidBondsRequest{
		Count:        n,
		Strength:     strength,
		DurationSecs: durSecs,
	})
	if err != nil {
		return nil, err
	}

	responses := make(chan *msgjson.Message, 1)
	ctx, cancel := context.WithTimeout(context.Background(), txWaitExpiration)
	defer cancel()

	if rpcErr := auth.mesh.ExecuteCommand(ctx, mesh.CommandRequest{
		Kind: commandKindCreatePrepaidBonds,
		Msg:  reqMsg,
		Respond: func(resp *msgjson.Message) error {
			select {
			case responses <- resp:
			default:
			}
			return nil
		},
	}); rpcErr != nil {
		return nil, rpcErr
	}

	select {
	case resp := <-responses:
		var result createPrepaidBondsResult
		if err := resp.UnmarshalResult(&result); err != nil {
			return nil, err
		}
		return result.CoinIDs, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

type createPrepaidBondsRequest struct {
	Count        int    `json:"n"`
	Strength     uint32 `json:"strength"`
	DurationSecs int64  `json:"durSecs"`
}

type createPrepaidBondsResult struct {
	CoinIDs [][]byte `json:"coinIDs"`
}

func (auth *AuthManager) executeCreatePrepaidBonds(cmdCtx *mesh.CommandContext) *msgjson.Error {
	var req createPrepaidBondsRequest
	if err := cmdCtx.Request.Msg.Unmarshal(&req); err != nil {
		return msgjson.NewError(msgjson.RPCParseError, "error parsing create prepaid bonds request: %v", err)
	}
	if req.Count < 0 {
		return msgjson.NewError(msgjson.RPCArgumentsError, "pre-paid bond count cannot be negative")
	}
	if req.Count == 0 {
		if err := cmdCtx.Completion.Complete(cmdCtx.Context, &createPrepaidBondsResult{CoinIDs: [][]byte{}}); err != nil {
			return msgjson.NewError(msgjson.RPCInternalError, "failed to complete pre-paid bond creation")
		}
		return nil
	}

	lockTime := time.Now().Add(auth.bondExpiry).Add(time.Duration(req.DurationSecs) * time.Second).Unix()
	coinIDs := make([][]byte, req.Count)
	bonds := make([]*meshevents.PrepaidBond, req.Count)
	for i := 0; i < req.Count; i++ {
		coinIDs[i] = encode.RandomBytes(prepaidBondIDLength)
		bonds[i] = &meshevents.PrepaidBond{
			CoinID:   coinIDs[i],
			Strength: req.Strength,
			LockTime: lockTime,
		}
	}
	event, err := mesh.NewEvent(&meshevents.PrepaidBondsCreatedEvent{Bonds: bonds})
	if err != nil {
		return msgjson.NewError(msgjson.RPCInternalError, "failed to encode pre-paid bond creation event")
	}

	if err = cmdCtx.Completion.Emit(cmdCtx.Context, event, func() any {
		return &createPrepaidBondsResult{CoinIDs: coinIDs}
	}); err != nil {
		mesh.LogApplyFailure(log, err, "Failed to store pre-paid bonds: %v", err)
		return mesh.ClientError(err, msgjson.RPCInternalError, "failed to store pre-paid bonds")
	}
	return nil
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

// notifyReputationInputsChanged loads and sends reputation to locally
// connected users, even if their score and tier have not changed.
//
// TODO(mesh): Avoid sending notifications when only reputation inputs,
// rather than the user's score or tier, have changed.
func (auth *AuthManager) notifyReputationInputsChanged(users []account.AccountID) {
	auth.repNotifyMtx.Lock()
	defer auth.repNotifyMtx.Unlock()
	for _, user := range users {
		if auth.user(user) == nil {
			continue
		}
		rep, err := auth.loadUserReputationWithTimeout(context.Background(), user)
		if err != nil {
			log.Errorf("failed to load reputation after inputs change for account %v: %v", user, err)
			continue
		}
		if rep == nil {
			continue
		}
		auth.sendScoreChanged(user, rep)
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
		log.Errorf("ScoreChangeRoute encoding error: %v", err)
		return
	}
	if err = auth.SendIfLocal(acctID, resp); err != nil {
		log.Warnf("Error sending score changed notification to account %v: %v", acctID, err)
		// The user will need to 'connect' to see their current tier and bonds.
	}
}

// addClient adds the client to the users and conns maps.
func (auth *AuthManager) addClient(client *clientInfo) {
	auth.connMtx.Lock()
	defer auth.connMtx.Unlock()
	user := client.acct.ID

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

// removeClient unregisters the client from the users and conns maps. It is
// idempotent for a given conn ID.
func (auth *AuthManager) removeClient(client *clientInfo) {
	auth.connMtx.Lock()
	connID := client.conn.ID()
	if _, connFound := auth.conns[connID]; !connFound {
		// conn already removed manually when this user made a new connection.
		// This user is still in the users map, so return.
		auth.connMtx.Unlock()
		return
	}
	delete(auth.users, client.acct.ID)
	delete(auth.conns, connID)
	auth.connMtx.Unlock()
	client.conn.Disconnect() // in case not triggered by disconnect
}

func matchStatusToOutcome(s order.MatchStatus) Outcome {
	switch s {
	case order.NewlyMatched:
		return db.OutcomeNoSwapAsMaker
	case order.MakerSwapCast:
		return db.OutcomeNoSwapAsTaker
	case order.TakerSwapCast:
		return db.OutcomeNoRedeemAsMaker
	case order.MakerRedeemed:
		return db.OutcomeNoRedeemAsTaker
	case order.MatchComplete:
		return db.OutcomeSwapSuccess // should be caught by Fail==false
	default:
		return db.OutcomeInvalid
	}
}

// loadUserOutcomes returns the user's latest reputation outcomes from the
// reputation points table.
func (auth *AuthManager) loadUserOutcomes(user account.AccountID) (pimgs *latestOutcomes[*db.PreimageOutcome], matches *latestOutcomes[*db.MatchResult], ords *latestOutcomes[*db.OrderOutcome], err error) {
	return auth.loadUserOutcomesContext(auth.ctx, user)
}

func (auth *AuthManager) loadUserOutcomesContext(ctx context.Context, user account.AccountID) (pimgs *latestOutcomes[*db.PreimageOutcome], matches *latestOutcomes[*db.MatchResult], ords *latestOutcomes[*db.OrderOutcome], err error) {
	if ctx == nil {
		ctx = context.Background()
	}
	dbPimgs, dbMatches, dbOrds, err := auth.storage.GetUserReputationData(ctx, user, scoringOrderLimit, ScoringMatchLimit, cancelThreshWindow)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("error loading reputation data for user %s: %w", user, err)
	}
	return newLatestOutcomes(dbPimgs, scoringOrderLimit),
		newLatestOutcomes(dbMatches, ScoringMatchLimit),
		newLatestOutcomes(dbOrds, cancelThreshWindow), nil
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
		matchStatus := matchStatusToOutcome(fail.Status)
		fails[i] = &MatchFail{
			ID:      fail.ID[:],
			Penalty: uint32(-1 * outcomeScores[matchStatus]),
		}
	}
	return fails, nil
}

func (auth *AuthManager) loadUserScoreContext(ctx context.Context, user account.AccountID) (int32, error) {
	latestPreimageResults, latestMatches, latestFinished, err := auth.loadUserOutcomesContext(ctx, user)
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
	acctInfo, bonds, err := auth.storage.Account(auth.ctx, user, lockTimeThresh)
	if err != nil {
		log.Errorf("Account read failed for user %v on connect: %v", user, err)
		return &msgjson.Error{
			Code:    msgjson.RPCInternalError,
			Message: "failed to retrieve account",
		}
	}
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

	latestPreimageResults, latestMatches, latestFinished, err := auth.loadUserOutcomes(user)
	if err != nil {
		log.Errorf("Failed to compute user %v score: %v", user, err)
		return &msgjson.Error{
			Code:    msgjson.RPCInternalError,
			Message: "DB error",
		}
	}
	score, successCount, piMissCount := auth.integrateOutcomes(latestMatches, latestPreimageResults, latestFinished)

	successScore := successCount * matchCompletedScore
	piMissScore := piMissCount * preimageMissScore
	// score = violationScore + piMissScore + successScore
	violationScore := score - piMissScore - successScore // work backwards as per above comment
	log.Debugf("User %v score = %d:%d (%d successes) - %d (violations) - %d (%d preimage misses) ",
		user, score, successScore, successCount, -violationScore, -piMissScore, piMissCount)

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
			addr = match.TakerSwapAddr // counterparty's per-match address
			oid = match.Maker[:]
			// sell = !match.TakerSell
		case side == order.Taker && user == match.TakerAcct:
			addr = match.MakerSwapAddr // counterparty's per-match address
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
	}

	rep := auth.userReputation(bondTier, score)
	rep.BondExpiryThreshold = lockTimeThresh.Unix()

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
		user, conn.Addr(), len(msgOrderStatuses), len(msgMatches), rep.EffectiveTier(), bondTier, score)
	auth.addClient(client)

	auth.connectCallbackMtx.RLock()
	callbacks := auth.connectCallbacks
	auth.connectCallbackMtx.RUnlock()
	for _, cb := range callbacks {
		cb(user)
	}

	return nil
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

// ForgiveUser forgives all penalty outcomes for a user.
func (auth *AuthManager) ForgiveUser(user account.AccountID) error {
	_, err := auth.executeReputationForgivenessCommand(context.Background(), &meshevents.ReputationForgivenEvent{
		AccountID: user,
		Scope:     meshevents.ReputationForgivenessScopeUser,
	})
	return err
}

type reputationForgivenessResult struct {
	Forgiven bool `json:"forgiven"`
	Unbanned bool `json:"unbanned"`
}

func (auth *AuthManager) executeReputationForgivenessCommand(ctx context.Context, req *meshevents.ReputationForgivenEvent) (*reputationForgivenessResult, error) {
	if auth.mesh == nil {
		return nil, fmt.Errorf("mesh service is not configured")
	}
	cmdCtx, cancel := context.WithTimeout(ctx, reputationForgivenessCommandTimeout)
	defer cancel()

	msg, err := msgjson.NewRequest(comms.NextID(), commandKindForgiveReputation, req)
	if err != nil {
		return nil, err
	}

	responses := make(chan *msgjson.Message, 1)
	execErrs := make(chan *msgjson.Error, 1)
	go func() {
		if rpcErr := auth.mesh.ExecuteCommand(cmdCtx, mesh.CommandRequest{
			Kind: commandKindForgiveReputation,
			User: req.AccountID,
			Msg:  msg,
			Respond: func(resp *msgjson.Message) error {
				select {
				case responses <- resp:
				default:
				}
				return nil
			},
		}); rpcErr != nil {
			execErrs <- rpcErr
		}
	}()

	var authDone <-chan struct{}
	if auth.ctx != nil {
		authDone = auth.ctx.Done()
	}

	select {
	case rpcErr := <-execErrs:
		return nil, rpcErr
	case resp := <-responses:
		if resp == nil {
			return nil, fmt.Errorf("nil reputation forgiveness response")
		}
		var result reputationForgivenessResult
		if err := resp.UnmarshalResult(&result); err != nil {
			return nil, err
		}
		return &result, nil
	case <-authDone:
		return nil, auth.ctx.Err()
	case <-cmdCtx.Done():
		return nil, cmdCtx.Err()
	}
}

func (auth *AuthManager) executeForgiveReputation(cmdCtx *mesh.CommandContext) *msgjson.Error {
	var event meshevents.ReputationForgivenEvent
	if err := cmdCtx.Request.Msg.Unmarshal(&event); err != nil {
		return msgjson.NewError(msgjson.RPCParseError, "error parsing reputation forgiveness request")
	}
	if event.AccountID != cmdCtx.Request.User {
		return msgjson.NewError(msgjson.RPCInternalError, "reputation forgiveness command account mismatch")
	}

	if err := event.Validate(); err != nil {
		return msgjson.NewError(msgjson.RPCParseError, "invalid reputation forgiveness request: %v", err)
	}
	meshEvent, err := mesh.NewEvent(&event)
	if err != nil {
		return msgjson.NewError(msgjson.RPCInternalError, "failed to encode reputation forgiveness event")
	}

	if err = cmdCtx.Completion.Emit(cmdCtx.Context, meshEvent, nil); err != nil {
		mesh.LogApplyFailure(log, err, "Failed to apply reputation forgiveness event for account %v: %v", event.AccountID, err)
		return mesh.ClientError(err, msgjson.RPCInternalError, "failed to apply reputation forgiveness: %v", err)
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
