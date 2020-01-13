// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package auth

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"decred.org/dcrdex/dex/msgjson"
	"decred.org/dcrdex/dex/order"
	"decred.org/dcrdex/server/account"
	"decred.org/dcrdex/server/comms"
	"github.com/decred/dcrd/dcrec/secp256k1/v2"
)

func unixMsNow() time.Time {
	return time.Now().Truncate(time.Millisecond).UTC()
}

// reqExpiration is how long a response handler will be saved before it is
// eligible for clean up.
const reqExpiration = time.Minute

// Storage updates and fetches account-related data from what is presumably a
// database.
type Storage interface {
	// CloseAccount closes the account for violation of the specified rule.
	CloseAccount(account.AccountID, account.Rule)
	// Account retrieves account info for the specified account ID.
	Account(account.AccountID) (acct *account.Account, paid, open bool)
	// ActiveMatches fetches the account's active matches.
	ActiveMatches(account.AccountID) []*order.UserMatch
	CreateAccount(*account.Account) (string, error)
	AccountRegAddr(account.AccountID) (string, error)
	PayAccount(account.AccountID, []byte) error
}

// Signer signs messages. It is likely a secp256k1.PrivateKey.
type Signer interface {
	Sign(hash []byte) (*secp256k1.Signature, error)
	PubKey() *secp256k1.PublicKey
}

// FeeChecker is a function for retreiving the details for a fee payment. It
// is satisfied by (dcr.Backend).P2PKHDetails.
type FeeChecker func(coinID []byte) (addr string, val uint64, confs int64, err error)

// A respHandler is the handler for the response to a DEX-originating request. A
// respHandler has a time associated with it so that old unused handlers can be
// detected and deleted.
type respHandler struct {
	expiration time.Time
	f          func(*msgjson.Message)
}

// clientInfo represents a DEX client, including account information and last
// known comms.Link.
type clientInfo struct {
	mtx          sync.Mutex
	acct         *account.Account
	conn         comms.Link
	respHandlers map[uint64]*respHandler
}

// cleanUpHandlers scans the respHandlers for expired handlers and deletes them
// from the map.
func (client *clientInfo) cleanUpHandlers() {
	client.mtx.Lock()
	defer client.mtx.Unlock()
	var expired []uint64
	for id, handler := range client.respHandlers {
		if time.Until(handler.expiration) < 0 {
			expired = append(expired, id)
		}
	}
	for _, id := range expired {
		delete(client.respHandlers, id)
	}
}

// logReq associates the specified response handler with the message ID.
func (client *clientInfo) logReq(id uint64, handler *respHandler) {
	client.mtx.Lock()
	defer client.mtx.Unlock()
	client.respHandlers[id] = handler
	if len(client.respHandlers) > 1 {
		go client.cleanUpHandlers()
	}
}

// respHandler extracts the response handler from the respHandlers map. If the
// handler is found, it is also deleted from the map before being returned.
func (client *clientInfo) respHandler(id uint64) *respHandler {
	client.mtx.Lock()
	defer client.mtx.Unlock()
	handler, found := client.respHandlers[id]
	if found {
		delete(client.respHandlers, id)
	}
	return handler
}

// AuthManager handles authentication-related tasks, including validating client
// signatures, maintaining association between accounts and `comms.Link`s, and
// signing messages with the DEX's private key. AuthManager manages requests to
// the 'connect' route.
type AuthManager struct {
	connMtx    sync.RWMutex
	users      map[account.AccountID]*clientInfo
	conns      map[uint64]*clientInfo
	storage    Storage
	signer     Signer
	startEpoch uint64
	regFee     uint64
	checkFee   FeeChecker
	feeConfs   int64
}

// Config is the configuration settings for the AuthManager, and the only
// argument to its constructor.
type Config struct {
	// Storage is an interface for storing and retreiving account-related info.
	Storage Storage
	// Signer is an interface that signs messages. In practice, Signer is
	// satisfied by a secp256k1.PrivateKey.
	Signer Signer
	// StartEpoch is the epoch at which the current DEX trading session
	// began/begins. When a client connects, trading may or may not be active. The
	// AuthManager returns the StartEpoch as part of the response to a 'connect'
	// request.
	StartEpoch uint64
	// RegistrationFee is the DEX registration fee, in atoms DCR
	RegistrationFee uint64
	// FeeConfs is the number of confirmations required on the registration fee
	// before registration can be completed with notifyfee.
	FeeConfs int64
	// FeeChecker is a method for getting the registration fee output info.
	FeeChecker FeeChecker
}

// NewAuthManager is the constructor for an AuthManager.
func NewAuthManager(cfg *Config) *AuthManager {
	auth := &AuthManager{
		users:      make(map[account.AccountID]*clientInfo),
		conns:      make(map[uint64]*clientInfo),
		storage:    cfg.Storage,
		signer:     cfg.Signer,
		startEpoch: cfg.StartEpoch,
		regFee:     cfg.RegistrationFee,
		checkFee:   cfg.FeeChecker,
		feeConfs:   cfg.FeeConfs,
	}

	comms.Route(msgjson.ConnectRoute, auth.handleConnect)
	comms.Route(msgjson.RegisterRoute, auth.handleRegister)
	comms.Route(msgjson.NotifyFeeRoute, auth.handleNotifyFee)
	return auth
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

// Sign signs the msgjson.Signables with the DEX private key.
func (auth *AuthManager) Sign(signables ...msgjson.Signable) error {
	for i, signable := range signables {
		sigMsg, err := signable.Serialize()
		if err != nil {
			return fmt.Errorf("signature message for signable index %d: %v", i, err)
		}
		sig, err := auth.signer.Sign(sigMsg)
		if err != nil {
			return fmt.Errorf("signature error: %v", err)
		}
		signable.SetSig(sig.Serialize())
	}
	return nil
}

// Send sends the non-Request-type msgjson.Message to the client identified by
// the specified account ID.
func (auth *AuthManager) Send(user account.AccountID, msg *msgjson.Message) {
	client := auth.user(user)
	if client == nil {
		log.Errorf("Send requested for unknown user %x", user[:])
		return
	}
	err := client.conn.Send(msg)
	if err != nil {
		log.Debugf("error sending on link: %v", err)
	}
}

// Request sends the Request-type msgjson.Message to the client identified by
// the specified account ID.
func (auth *AuthManager) Request(user account.AccountID, msg *msgjson.Message, f func(*msgjson.Message)) {
	client := auth.user(user)
	if client == nil {
		log.Errorf("Send requested for unknown user %x", user[:])
		return
	}
	client.logReq(msg.ID, &respHandler{
		expiration: time.Now().Add(reqExpiration),
		f:          f,
	})
	err := client.conn.Request(msg, auth.handleResponse)
	if err != nil {
		log.Debugf("error sending request: %v", err)
	}
}

// Penalize signals that a user has broken a rule of community conduct, and that
// their account should be penalized.
func (auth *AuthManager) Penalize(user account.AccountID, rule account.Rule) {
	client := auth.user(user)
	if client == nil {
		log.Errorf("no client to penalize")
		return
	}
	auth.storage.CloseAccount(client.acct.ID, rule)
	client.conn.Banish() // May not want to do this. Leaving it for now.
	auth.removeClient(client)
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
			Code:    msgjson.AuthenticationError,
			Message: "not account info found for account ID" + connect.AccountID.String(),
		}
	}
	if !paid {
		return &msgjson.Error{
			Code:    msgjson.AuthenticationError,
			Message: "unpaid account",
		}
	}
	if !open {
		return &msgjson.Error{
			Code:    msgjson.AuthenticationError,
			Message: "closed account",
		}
	}
	// Authorize the account.
	sigMsg, err := connect.Serialize()
	if err != nil {
		return &msgjson.Error{
			Code:    msgjson.RPCInternalError,
			Message: "serialization error",
		}
	}
	err = checkSigS256(sigMsg, connect.SigBytes(), acctInfo.PubKey)
	if err != nil {
		return &msgjson.Error{
			Code:    msgjson.SignatureError,
			Message: "signature error: " + err.Error(),
		}
	}

	// Send the connect response, which includes a list of active matches.
	matches := auth.storage.ActiveMatches(user)
	msgMatches := make([]*msgjson.Match, 0, len(matches))
	for _, match := range matches {
		msgMatches = append(msgMatches, &msgjson.Match{
			OrderID:  match.OrderID[:],
			MatchID:  match.MatchID[:],
			Quantity: match.Quantity,
			Rate:     match.Rate,
			Address:  match.Address,
			Time:     match.Time,
			Status:   uint8(match.Status),
			Side:     uint8(match.Side),
		})
	}
	resp := &msgjson.ConnectResponse{
		StartEpoch: auth.startEpoch,
		Matches:    msgMatches,
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
	client := auth.user(acctInfo.ID)
	if client == nil {
		client = &clientInfo{
			acct:         acctInfo,
			conn:         conn,
			respHandlers: make(map[uint64]*respHandler),
		}
	} else {
		client.conn = conn
	}

	auth.addClient(client)
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
		errMsg, err := msgjson.NewResponse(msg.ID, nil,
			msgjson.NewError(msgjson.UnknownResponseID, "unknown response ID"))
		if err != nil {
			log.Errorf("failure creating unknown ID response error message: %v", err)
		} else {
			err := conn.Send(errMsg)
			if err != nil {
				log.Tracef("error sending response failure message: %v", err)
			}
		}
		return
	}
	handler.f(msg)
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
