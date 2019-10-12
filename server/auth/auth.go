package auth

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/decred/dcrd/dcrec/secp256k1/v2"
	"github.com/decred/dcrdex/server/account"
	"github.com/decred/dcrdex/server/comms"
	"github.com/decred/dcrdex/server/comms/msgjson"
)

// Storage updates and fetches account-related data from what is presumably a
// database.
type Storage interface {
	CloseAccount(account.AccountID, account.Rule)
	Account(account.AccountID) *account.Account
	ActiveMatches(account.AccountID) []msgjson.Match
}

// Signer signs messages. It is likely a secp256k1.PrivateKey.
type Signer interface {
	Sign(hash []byte) (*secp256k1.Signature, error)
}

// A respHandler is the handler for the response to a DEX-originating request. A
// respHandler has a time associated with it so that old unused handlers can be
// detected and deleted.
type respHandler struct {
	sent time.Time
	f    func(*msgjson.Message)
}

// clientInfo represents a DEX client. A client is identified by their
// account.AccountID, and is hopefully linked to a connected comms.Link.
type clientInfo struct {
	mtx          sync.Mutex
	acct         *account.Account
	conn         comms.Link
	respHandlers map[uint64]*respHandler
}

// logReq associates the specified response handler with the message ID.
func (client *clientInfo) logReq(id uint64, handler *respHandler) {
	client.mtx.Lock()
	defer client.mtx.Unlock()
	client.respHandlers[id] = handler
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

// AuthManager handles authorization-related tasks, including validating client
// signatures, linking accounts to websocket connections, and signing messages
// with the DEX's private key. AuthManager manages requests to the 'connect'
// route.
type AuthManager struct {
	connMtx    sync.RWMutex
	users      map[account.AccountID]*clientInfo
	conns      map[uint64]*clientInfo
	respMtx    sync.RWMutex
	storage    Storage
	signer     Signer
	startEpoch uint64
}

// Config is the configuration settings for the AuthManager, and the only
// argument to its constructor.
type Config struct {
	// Storage is an interface for storing and retreiving account-related info.
	Storage Storage
	// Signer is an interface that signs messages. In practice, Signer is
	// satisfied by a secp256k1.PrivateKey.
	Signer Signer
	// When a client connects, trading may or may not be active. The AuthManager
	// returns the StartEpoch as part of the resposne to a 'connect' request.
	StartEpoch uint64
}

// NewAuthManager is the constructor for an AuthManager.
func NewAuthManager(cfg *Config) *AuthManager {
	auth := &AuthManager{
		users:      make(map[account.AccountID]*clientInfo),
		conns:      make(map[uint64]*clientInfo),
		storage:    cfg.Storage,
		signer:     cfg.Signer,
		startEpoch: cfg.StartEpoch,
	}

	comms.Route(msgjson.ConnectRoute, auth.handleConnect)
	return auth
}

// Route wraps the comms.Route function, storing the response handler with the
// associated clientInfo, and sending on the latest websocket connection for the
// client.
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

// Send sends the non-Request-type msgjson.Message to the client identified by the
// specified account ID.
func (auth *AuthManager) Send(user account.AccountID, msg *msgjson.Message) {
	client := auth.user(user)
	if client == nil {
		log.Errorf("Send requested for unknown user %x", user[:])
		return
	}
	client.conn.Send(msg)
}

// Request sends the Request-type msgjson.Message to the client identified by the
// specified account ID.
func (auth *AuthManager) Request(user account.AccountID, msg *msgjson.Message, f func(*msgjson.Message)) {
	client := auth.user(user)
	if client == nil {
		log.Errorf("Send requested for unknown user %x", user[:])
		return
	}
	client.logReq(msg.ID, &respHandler{
		sent: time.Now(),
		f:    f,
	})
	client.conn.Request(msg, auth.handleResponse)
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
	client.conn.Banish()
	auth.removeClient(client)
}

// user gets the client by account ID.
func (auth *AuthManager) user(user account.AccountID) *clientInfo {
	auth.connMtx.RLock()
	defer auth.connMtx.RUnlock()
	return auth.users[user]
}

// conn gets the client by connection ID.
func (auth *AuthManager) conn(conn comms.Link) *clientInfo {
	auth.connMtx.RLock()
	defer auth.connMtx.RUnlock()
	return auth.conns[conn.ID()]
}

// addClient add the client to the users and conns maps.
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
// a response is issued, and a clientInfo is created and stored.
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
	acctInfo := auth.storage.Account(user)
	if acctInfo == nil {
		return &msgjson.Error{
			Code:    msgjson.AuthenticationError,
			Message: "not account info found for account ID" + connect.AccountID.String(),
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
	resp := &msgjson.ConnectResponse{
		StartEpoch: auth.startEpoch,
		Matches:    auth.storage.ActiveMatches(user),
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
	}

	auth.addClient(client)
	return nil
}

// handleResponse handles all responses for AuthManager registered routes,
// essentially wrapping response handlers and translating connection to account.
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
			conn.Send(errMsg)
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
