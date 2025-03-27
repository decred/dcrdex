// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package conn

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/msgjson"
	"decred.org/dcrdex/tatanka/mj"
	"decred.org/dcrdex/tatanka/tanka"
	tcpclient "decred.org/dcrdex/tatanka/tcp/client"
	"github.com/decred/dcrd/dcrec/secp256k1/v4"
)

const (
	ErrPeerNeedsReconnect = dex.ErrorKind("peer needs reconnect")
	ErrTankaError         = dex.ErrorKind("tanka error")
	ErrBadPeerResponse    = dex.ErrorKind("bad peer response")
)

// NetworkBackend represents a peer's communication protocol.
type NetworkBackend interface {
	Send(*msgjson.Message) error
	Request(msg *msgjson.Message, respHandler func(*msgjson.Message)) error
}

// tatanka is a Tatanka mesh server node.
type tatankaNode struct {
	NetworkBackend
	cm     *dex.ConnectionMaster
	url    string
	peerID tanka.PeerID
	pub    *secp256k1.PublicKey
	config atomic.Value // *mj.TatankaConfig
}

func (tt *tatankaNode) String() string {
	return fmt.Sprintf("%s @ %s", tt.peerID, tt.url)
}

// peer is a network peer with which we have established encrypted
// communication.
type peer struct {
	id                 tanka.PeerID
	ephemeralSharedKey []byte
}

const (
	aesKeySize   = 32
	gcmNonceSize = 12
)

func decryptAES(key []byte, ciphertext []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("failed to create AES cipher: %v", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("failed to create GCM: %v", err)
	}

	if len(ciphertext) < gcmNonceSize {
		return nil, fmt.Errorf("ciphertext too short")
	}
	nonce, ciphertext := ciphertext[:gcmNonceSize], ciphertext[gcmNonceSize:]

	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("decryption failed: %v", err)
	}

	return plaintext, nil
}

func decryptTankagramPayload(key []byte, ciphertext []byte) (*msgjson.Message, error) {
	plaintext, err := decryptAES(key, ciphertext)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt payload: %v", err)
	}

	return msgjson.DecodeMessage(plaintext)
}

func decryptTankagramResult(key []byte, ciphertext []byte) (*mj.TankagramResultPayload, error) {
	plaintext, err := decryptAES(key, ciphertext)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt payload: %v", err)
	}

	var res mj.TankagramResultPayload
	if err := json.Unmarshal(plaintext, &res); err != nil {
		return nil, fmt.Errorf("failed to unmarshal payload: %v", err)
	}

	return &res, nil
}

func encryptAES(key []byte, plaintext []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("failed to create AES cipher: %v", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("failed to create GCM: %v", err)
	}

	nonce := make([]byte, gcmNonceSize)
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("failed to generate nonce: %v", err)
	}

	ciphertext := gcm.Seal(nonce, nonce, plaintext, nil)
	return ciphertext, nil
}

func encryptTankagramResult(key []byte, msg *mj.TankagramResultPayload) ([]byte, error) {
	plaintext, err := json.Marshal(msg)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal payload: %v", err)
	}
	return encryptAES(key, plaintext)
}

func encryptTankagramPayload(key []byte, msg *msgjson.Message) ([]byte, error) {
	plaintext, err := json.Marshal(msg)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal payload: %v", err)
	}

	return encryptAES(key, plaintext)
}

// IncomingTankagram will be emitted when we receive a tankagram from a
// connected peer.
type IncomingTankagram struct {
	Msg        *msgjson.Message
	Respond    func(any) error
	RespondErr func(mj.TankagramError) error
}

// NewMarketSubscriber will be emitted when a new client subscribes to a market.
type NewMarketSubscriber struct {
	MarketName string
	PeerID     tanka.PeerID
}

// MessageHandlers are handlers for different types of messages from the Mesh.
type MessageHandlers struct {
	HandleTatankaRequest      func(tanka.PeerID, *msgjson.Message) *msgjson.Error
	HandleTatankaNotification func(tanka.PeerID, *msgjson.Message)
	HandlePeerMessage         func(tanka.PeerID, *IncomingTankagram) *msgjson.Error
}

// Config is the configuration for the MeshConn.
type Config struct {
	EntryNode  *TatankaCredentials
	Logger     dex.Logger
	Handlers   *MessageHandlers
	PrivateKey *secp256k1.PrivateKey
}

// TatankaCredentials are the connection credentials for a Tatanka node.
type TatankaCredentials struct {
	PeerID tanka.PeerID
	Addr   string
	Cert   []byte
	NoTLS  bool
}

// MeshConn is a Tatanka Mesh connection manager. MeshConn handles both tatanka
// nodes and regular peers.
type MeshConn struct {
	log       dex.Logger
	handlers  *MessageHandlers
	entryNode *TatankaCredentials

	peerID tanka.PeerID
	priv   *secp256k1.PrivateKey

	tankaMtx     sync.RWMutex
	tatankaNodes map[tanka.PeerID]*tatankaNode

	peersMtx sync.RWMutex
	peers    map[tanka.PeerID]*peer
}

// New is the constructor for a new MeshConn.
func New(cfg *Config) *MeshConn {
	var peerID tanka.PeerID
	copy(peerID[:], cfg.PrivateKey.PubKey().SerializeCompressed())
	c := &MeshConn{
		log:          cfg.Logger,
		handlers:     cfg.Handlers,
		priv:         cfg.PrivateKey,
		peerID:       peerID,
		entryNode:    cfg.EntryNode,
		tatankaNodes: make(map[tanka.PeerID]*tatankaNode),
		peers:        make(map[tanka.PeerID]*peer),
	}
	return c
}

func (c *MeshConn) sendErrorTankagramResult(tt *tatankaNode, id uint64, tankagramErr mj.TankagramError, encKey []byte) error {
	enc, err := encryptTankagramResult(encKey, &mj.TankagramResultPayload{
		Error: tankagramErr,
	})
	if err != nil {
		return fmt.Errorf("error encrypting tankagram result: %v", err)
	}
	if err := c.sendResult(tt, id, enc); err != nil {
		return fmt.Errorf("error sending tankagram result: %v", err)
	}
	return nil
}

func (c *MeshConn) sendTankagramResult(tt *tatankaNode, id uint64, payload dex.Bytes, encKey []byte) error {
	enc, err := encryptTankagramResult(encKey, &mj.TankagramResultPayload{
		Error:   mj.TEErrNone,
		Payload: payload,
	})
	if err != nil {
		return err
	}
	return c.sendResult(tt, id, enc)
}

// handleNewPeerTankagram handles a tankagram recieved from a new peer. The
// tankagram should be encrypted with the permanent encryption key derived from
// our permanent private key and the peer's peerID. The contents of the
// tankagram should be an EncryptionKeyPayload, used to establish an ephemeral
// encryption key.
func (c *MeshConn) handleNewPeerTankagram(tt *tatankaNode, gram *mj.Tankagram, id uint64) {
	pub, err := secp256k1.ParsePubKey(gram.From[:])
	if err != nil {
		c.log.Errorf("bad peer ID %s", gram.From)
		return
	}

	sharedSecretPerm := sharedSecret(c.priv, pub)

	tankagramErr := mj.TEErrNone
	var ephPriv *secp256k1.PrivateKey
	var remoteEphPub *secp256k1.PublicKey

	defer func() {
		// If we encountered an error, send an error result.
		if tankagramErr != mj.TEErrNone {
			if err := c.sendErrorTankagramResult(tt, id, tankagramErr, sharedSecretPerm); err != nil {
				c.log.Errorf("error sending tankagram result: %v", err)
			}
			return
		}

		// Otherwise, send our ephemeral pub key.
		res := mj.EncryptionKeyPayload{
			EphemeralPubKey: ephPriv.PubKey().SerializeCompressed(),
		}
		resB, err := json.Marshal(res)
		if err != nil {
			c.log.Errorf("error marshalling ephemeral pubkey: %v", err)
			return
		}
		if err := c.sendTankagramResult(tt, id, resB, sharedSecretPerm); err != nil {
			c.log.Errorf("error sending ephemeral pubkey: %v", err)
			return
		}

		// Add the peer to our peers map.
		c.peersMtx.Lock()
		c.peers[gram.From] = &peer{
			id:                 gram.From,
			ephemeralSharedKey: sharedSecret(ephPriv, remoteEphPub),
		}
		c.peersMtx.Unlock()
	}()

	decryptedPayload, err := decryptTankagramPayload(sharedSecretPerm, gram.EncryptedPayload)
	if err != nil {
		c.log.Errorf("failed to decrypt new peer tankagram with permanent key: %v", err)
		// Assume that they sent a message with an ephemeral key that
		// we do not have, possibly due to restarting the client. They
		// will need to do a new handshake before further communication.
		tankagramErr = mj.TEDecryptionFailed
		return
	}

	c.log.Infof("decrypted new peer tankagram: %+v", decryptedPayload)

	if decryptedPayload.Route != mj.RouteEncryptionKey {
		c.log.Errorf("unexpected route for new peer tankagram %s", decryptedPayload.Route)
		tankagramErr = mj.TEEBadRequest
		return
	}

	var encryptionKeyPayload mj.EncryptionKeyPayload
	if err := json.Unmarshal(decryptedPayload.Payload, &encryptionKeyPayload); err != nil {
		c.log.Errorf("error unmarshalling encryption key payload: %v", err)
		tankagramErr = mj.TEEBadRequest
		return
	}

	remoteEphPub, err = secp256k1.ParsePubKey(encryptionKeyPayload.EphemeralPubKey)
	if err != nil {
		c.log.Errorf("failed to parse ephemeral pubkey: %v", err)
		tankagramErr = mj.TEEBadRequest
		return
	}

	ephPriv, err = secp256k1.GeneratePrivateKey()
	if err != nil {
		c.log.Errorf("failed to generate ephemeral private key: %v", err)
		tankagramErr = mj.TEEPeerError
		return
	}
}

func (c *MeshConn) handleTankagram(tt *tatankaNode, tankagram *msgjson.Message) {
	var gram mj.Tankagram
	if err := tankagram.Unmarshal(&gram); err != nil {
		c.log.Errorf("error unmarshalling tankagram: %v", err)
		return
	}

	c.peersMtx.Lock()
	p, peerExisted := c.peers[gram.From]
	c.peersMtx.Unlock()
	if !peerExisted {
		c.handleNewPeerTankagram(tt, &gram, tankagram.ID)
		return
	}

	// If this isn't the encryption key, this gram.Message is ignored and this
	// is assumed to be encrypted.
	if len(gram.EncryptedPayload) == 0 {
		c.log.Errorf("%s sent a tankagram with no message or data", p.id)
		c.sendErrorTankagramResult(tt, tankagram.ID, mj.TEEBadRequest, p.ephemeralSharedKey)
		return
	}

	decryptedPayload, err := decryptTankagramPayload(p.ephemeralSharedKey, gram.EncryptedPayload)
	if err != nil {
		c.log.Errorf("failed to decrypt tankagram payload with ephemeral key: %v", err)
		c.sendErrorTankagramResult(tt, tankagram.ID, mj.TEDecryptionFailed, p.ephemeralSharedKey)
		return
	}

	c.handlers.HandlePeerMessage(p.id, &IncomingTankagram{
		Msg: decryptedPayload,
		Respond: func(payload any) error {
			payloadB, err := json.Marshal(payload)
			if err != nil {
				c.log.Errorf("error marshalling payload: %v", err)
				return err
			}
			return c.sendTankagramResult(tt, tankagram.ID, payloadB, p.ephemeralSharedKey)
		},
		RespondErr: func(tankagramErr mj.TankagramError) error {
			return c.sendErrorTankagramResult(tt, tankagram.ID, tankagramErr, p.ephemeralSharedKey)
		},
	})
}

func (c *MeshConn) Connect(ctx context.Context) (*sync.WaitGroup, error) {
	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		<-ctx.Done()
		c.tankaMtx.Lock()
		for peerID, tt := range c.tatankaNodes {
			c.log.Infof("Disconnecting old tatanka node %s", tt)
			tt.cm.Disconnect()
			delete(c.tatankaNodes, peerID)
		}
		c.tankaMtx.Unlock()
	}()

	if err := c.addTatankaNode(ctx, c.entryNode); err != nil {
		return nil, err
	}

	return &wg, nil
}

func (c *MeshConn) addTatankaNode(ctx context.Context, creds *TatankaCredentials) error {
	peerID, uri, cert := creds.PeerID, "wss://"+creds.Addr, creds.Cert
	if creds.NoTLS {
		uri = "ws://" + creds.Addr
	}
	pub, err := secp256k1.ParsePubKey(peerID[:])
	if err != nil {
		return fmt.Errorf("error parsing pubkey from peer ID: %w", err)
	}
	log := c.log.SubLogger("TCP")
	cl, err := tcpclient.New(&tcpclient.Config{
		Logger: log,
		URL:    uri + "/ws",
		Cert:   cert,
		HandleMessage: func(msg *msgjson.Message) {
			c.handleTatankaMessage(peerID, msg)
		},
	})
	if err != nil {
		return fmt.Errorf("error creating connection: %w", err)
	}

	cm := dex.NewConnectionMaster(cl)

	c.tankaMtx.Lock()
	defer c.tankaMtx.Unlock()

	if tt := c.tatankaNodes[peerID]; tt != nil {
		tt.cm.Disconnect()
		log.Infof("replacing existing connection for tatanka node %s", tt)
	}

	c.tatankaNodes[peerID] = &tatankaNode{
		NetworkBackend: cl,
		cm:             cm,
		url:            uri,
		peerID:         peerID,
		pub:            pub,
	}

	if err := cm.Connect(ctx); err != nil {
		return fmt.Errorf("error connecting to tatanka node %s at %s: %w. will keep trying to connect", peerID, uri, err)
	}

	return nil
}

// Auth sends our connect message to the tatanka node.
func (c *MeshConn) Auth(tatankaID tanka.PeerID) error {
	c.tankaMtx.RLock()
	tt, found := c.tatankaNodes[tatankaID]
	c.tankaMtx.RUnlock()
	if !found {
		return fmt.Errorf("cannot auth with unknown server %s", tatankaID)
	}
	connectMsg := mj.MustRequest(mj.RouteConnect, &mj.Connect{ID: c.peerID})
	mj.SignMessage(c.priv, connectMsg)
	var cfg *mj.TatankaConfig
	if err := c.requestTT(tt, connectMsg, &cfg, DefaultRequestTimeout); err != nil {
		return err
	}
	tt.config.Store(cfg)
	return nil
}

func (c *MeshConn) tatanka(tatankaID tanka.PeerID) *tatankaNode {
	c.tankaMtx.RLock()
	defer c.tankaMtx.RUnlock()
	return c.tatankaNodes[tatankaID]
}

type TatankaSelectionMode string

const (
	SelectionModeEntryNode TatankaSelectionMode = "EntryNode"
	SelectionModeAll       TatankaSelectionMode = "All"
	SelectionModeAny       TatankaSelectionMode = "Any"
)

// tatankas generates a list of tatanka nodes.
func (c *MeshConn) tatankas(mode TatankaSelectionMode) (tts []*tatankaNode, _ error) {
	c.tankaMtx.RLock()
	defer c.tankaMtx.RUnlock()
	en := c.tatankaNodes[c.entryNode.PeerID]
	switch mode {
	case SelectionModeEntryNode:
		if en == nil {
			return nil, errors.New("no entry node initialized")
		}
		if !en.cm.On() {
			return nil, errors.New("entry node no connected")
		}
		return []*tatankaNode{en}, nil
	case SelectionModeAll, SelectionModeAny:
		tts := make([]*tatankaNode, 0, len(c.tatankaNodes))
		var skipID tanka.PeerID
		// Entry node always goes first, if available.
		if en != nil && en.cm.On() {
			tts = append(tts, en)
			skipID = en.peerID
		}
		for peerID, tt := range c.tatankaNodes {
			if tt.cm.On() && peerID != skipID {
				tts = append(tts, tt)
				if mode == SelectionModeAny {
					return tts, nil
				}
			}
		}
		if len(tts) == 0 {
			return nil, errors.New("no tatanka nodes available")
		}
		return tts, nil
	default:
		return nil, fmt.Errorf("unknown tatanka selection mode %q", mode)
	}
}

func (c *MeshConn) handleTatankaMessage(tatankaID tanka.PeerID, msg *msgjson.Message) {
	if c.log.Level() == dex.LevelTrace {
		c.log.Tracef("Client handling message from tatanka node: route = %s", msg.Route)
	}

	tt := c.tatanka(tatankaID)
	if tt == nil {
		c.log.Error("Message received from unknown tatanka node")
		return
	}

	if err := mj.CheckSig(msg, tt.pub); err != nil {
		// DRAFT TODO: Record for reputation somehow, no?
		c.log.Errorf("tatanka node %s sent a bad signature. disconnecting", tt.peerID)
		return
	}

	if msg.Type == msgjson.Request && msg.Route == mj.RouteTankagram {
		c.handleTankagram(tt, msg)
		return
	}

	switch msg.Type {
	case msgjson.Request:
		c.handlers.HandleTatankaRequest(tatankaID, msg)
	case msgjson.Notification:
		c.handlers.HandleTatankaNotification(tatankaID, msg)
	default:
		c.log.Errorf("tatanka node %s send a message with an unhandleable type %d", tt.peerID, msg.Type)
	}
}

func (c *MeshConn) sendResult(tt *tatankaNode, msgID uint64, result dex.Bytes) error {
	resp, err := msgjson.NewResponse(msgID, result, nil)
	if err != nil {
		return err
	}
	return tt.Send(resp)
}

const DefaultRequestTimeout = 30 * time.Second

type requestConfig struct {
	timeout       time.Duration
	errCodeFunc   func(code int)
	selectionMode TatankaSelectionMode
	examineFunc   func(tatankaURL string, tatankaID tanka.PeerID) (ok bool)
}

// RequestOption is an optional modifier to request behavior.
type RequestOption func(cfg *requestConfig)

// WithTimeout sets the time out for the request. If no timeout is specified
// DefaultRequestTimeout is used.
func WithTimeout(d time.Duration) RequestOption {
	return func(cfg *requestConfig) {
		cfg.timeout = d
	}
}

// WithErrorCode enables the caller to inspect the error code when messaging
// fails.
func WithErrorCode(f func(code int)) RequestOption {
	return func(cfg *requestConfig) {
		cfg.errCodeFunc = f
	}
}

// WithSelectionMode set which tatanka nodes are selected for the request.
func WithSelectionMode(mode TatankaSelectionMode) RequestOption {
	return func(cfg *requestConfig) {
		cfg.selectionMode = mode
	}
}

// WithExamination allows the caller to check the result before returning, and
// continue trying other tatanka nodes if necessary.
func WithExamination(f func(tatankaURL string, tatankaID tanka.PeerID) (resultOK bool)) RequestOption {
	return func(cfg *requestConfig) {
		cfg.examineFunc = f
	}
}

// RequestMesh sends a request to the Mesh.
func (c *MeshConn) RequestMesh(msg *msgjson.Message, thing any, opts ...RequestOption) error {
	cfg := &requestConfig{
		timeout:       DefaultRequestTimeout,
		selectionMode: SelectionModeEntryNode,
	}
	for _, opt := range opts {
		opt(cfg)
	}

	tts, err := c.tatankas(cfg.selectionMode)
	if err != nil {
		return err
	}

	for _, tt := range tts {
		if err := c.requestTT(tt, msg, thing, cfg.timeout); err == nil {
			var resultOK bool = true
			if cfg.examineFunc != nil {
				resultOK = cfg.examineFunc(tt.url, tt.peerID)
			}
			switch cfg.selectionMode {
			case SelectionModeAny, SelectionModeEntryNode:
				if resultOK {
					return nil
				}
			}
		} else { // err != nil
			var msgErr *msgjson.Error
			if cfg.errCodeFunc != nil && errors.As(err, &msgErr) {
				cfg.errCodeFunc(msgErr.Code)
			}
			switch cfg.selectionMode {
			case SelectionModeEntryNode:
				return err
			}
		}
	}

	return errors.New("failed to request from any tatanka nodes")
}

func (c *MeshConn) requestTT(tt *tatankaNode, msg *msgjson.Message, thing any, timeout time.Duration) (err error) {
	errChan := make(chan error)
	if err := tt.Request(msg, func(msg *msgjson.Message) {
		if thing != nil {
			errChan <- msg.UnmarshalResult(thing)
		} else {
			errChan <- nil
		}
	}); err != nil {
		errChan <- fmt.Errorf("request error: %w", err)
	}

	select {
	case err = <-errChan:
	case <-time.After(timeout):
		return fmt.Errorf("timed out (%s) waiting for response from %s for route %q", timeout, tt, msg.Route)
	}
	return err
}

// sharedSecret computes the shared secret used for encrypting and decrypting
// messages between two peers. The shared secret is the hash of the result of
// an elliptic curve diffie-hellman key exchange.
func sharedSecret(ourPriv *secp256k1.PrivateKey, theirPub *secp256k1.PublicKey) []byte {
	sharedSecret := secp256k1.GenerateSharedSecret(ourPriv, theirPub)
	hash := sha256.Sum256(sharedSecret)
	return hash[:]
}

// ConnectPeer establishes a connection to a peer. A handhshake is performed
// to establish an ephemeral shared secret, which is used to encrypt and
// decrypt messages between the two peers for the duration of the connection.
// A permanent shared secret based on the peer IDs is used to encrypt the
// handshake messages.
func (c *MeshConn) ConnectPeer(peerID tanka.PeerID) error {
	// Parse remote permanent public key from peerID
	remotePub, err := secp256k1.ParsePubKey(peerID[:])
	if err != nil {
		return fmt.Errorf("error parsing remote pubkey from peer %s: %v", peerID, err)
	}

	// Compute permanent shared secret, to use for encrypting the handshake
	// messages.
	sharedSecretPerm := sharedSecret(c.priv, remotePub)

	// Generate ephemeral key pair for this session
	ephPrivKey, err := secp256k1.GeneratePrivateKey()
	if err != nil {
		return fmt.Errorf("peer %s: failed to generate ephemeral key: %v", peerID, err)
	}

	encryptionKeyPayload, err := json.Marshal(mj.EncryptionKeyPayload{
		EphemeralPubKey: ephPrivKey.PubKey().SerializeCompressed(),
	})
	if err != nil {
		return fmt.Errorf("peer %s: failed to marshal encryption key payload: %v", peerID, err)
	}

	msg := msgjson.Message{
		Route:   mj.RouteEncryptionKey,
		Payload: encryptionKeyPayload,
	}
	encryptedPayload, err := encryptTankagramPayload(sharedSecretPerm, &msg)
	if err != nil {
		return fmt.Errorf("peer %s: failed to encrypt ephemeral pubkey: %v", peerID, err)
	}

	req := mj.MustRequest(mj.RouteTankagram, &mj.Tankagram{
		From:             c.peerID,
		To:               peerID,
		EncryptedPayload: encryptedPayload,
	})
	mj.SignMessage(c.priv, req)

	var r mj.TankagramResult
	if err := c.RequestMesh(req, &r, WithSelectionMode(SelectionModeAny), WithExamination(func(tatankaURL string, tatankaID tanka.PeerID) bool {
		if r.Result == mj.TRTTransmitted {
			return true
		}
		c.log.Errorf("Tankagram transmission failure connecting to %s via %s @ %s: %q", peerID, tatankaID, tatankaURL, r.Result)
		return false
	})); err != nil {
		return fmt.Errorf("peer %s: failed to send ephemeral key: %v", peerID, err)
	}

	tankagramResult, err := decryptTankagramResult(sharedSecretPerm, r.EncryptedPayload)
	if err != nil {
		return fmt.Errorf("peer %s: error decrypting response: %v", peerID, err)
	}

	var resp mj.EncryptionKeyPayload
	if err := json.Unmarshal(tankagramResult.Payload, &resp); err != nil {
		return fmt.Errorf("peer %s: error unmarshalling response: %v", peerID, err)
	}

	remoteEphPub, err := secp256k1.ParsePubKey(resp.EphemeralPubKey)
	if err != nil {
		return fmt.Errorf("peer %s: error decoding ephemeral pubkey: %v", peerID, err)
	}

	c.peersMtx.Lock()
	defer c.peersMtx.Unlock()
	c.peers[peerID] = &peer{
		id:                 peerID,
		ephemeralSharedKey: sharedSecret(ephPrivKey, remoteEphPub),
	}

	return nil
}

// RequestPeer sends a request to an already-connected peer.
func (c *MeshConn) RequestPeer(peerID tanka.PeerID, msg *msgjson.Message, thing interface{}) error {
	c.peersMtx.RLock()
	p, known := c.peers[peerID]
	c.peersMtx.RUnlock()
	if !known {
		return fmt.Errorf("not connected to peer %s", peerID)
	}

	payload, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("error marshaling payload: %w", err)
	}

	encryptedPayload, err := encryptAES(p.ephemeralSharedKey, payload)
	if err != nil {
		return fmt.Errorf("error encrypting payload for %s: %w", p.id, err)
	}

	tankaGram := &mj.Tankagram{
		From:             c.peerID,
		To:               peerID,
		EncryptedPayload: encryptedPayload,
	}

	var res mj.TankagramResult
	wrappedMsg := mj.MustRequest(mj.RouteTankagram, tankaGram)
	mj.SignMessage(c.priv, wrappedMsg)

	if err := c.RequestMesh(wrappedMsg, &res, WithSelectionMode(SelectionModeAny), WithExamination(func(tatankaURL string, tatankaID tanka.PeerID) bool {
		if res.Result == mj.TRTTransmitted {
			return true
		}
		c.log.Errorf("Tankagram transmission failure sending %s to %s via %s @ %s: %q", msg.Route, peerID, tatankaID, tatankaURL, res.Result)
		return false
	})); err != nil {
		return err
	}

	switch res.Result {
	case mj.TRTErrFromTanka:
		return ErrTankaError
	case mj.TRTNoPath:
		return ErrTankaError
	case mj.TRTErrBadClient:
		c.log.Errorf("ErrBadPeerResponse due to bad client")
		return ErrBadPeerResponse
	}

	decryptedRes, err := decryptTankagramResult(p.ephemeralSharedKey, res.EncryptedPayload)
	if err != nil {
		remotePub, err := p.id.PublicKey()
		if err != nil {
			return fmt.Errorf("peer ID %s does not map to a valid public key: %w", p.id, err)
		}
		permanentSharedKey := sharedSecret(c.priv, remotePub)
		decryptedRes, err := decryptTankagramResult(permanentSharedKey, res.EncryptedPayload)
		if err != nil {
			c.log.Errorf("RequestPeer: could not decrypt tankagram result: %v", err)
			return ErrBadPeerResponse
		}
		if decryptedRes.Error == mj.TEDecryptionFailed {
			return ErrPeerNeedsReconnect
		}
		// If we requested using an ephemeral key, and they responded using the
		// permanent key, the only valid response is a TEDecryptionFailed.
		return ErrBadPeerResponse
	}

	if thing != nil {
		if len(decryptedRes.Payload) == 0 {
			return ErrBadPeerResponse
		}
		if err = json.Unmarshal(decryptedRes.Payload, thing); err != nil {
			c.log.Errorf("error unmarshalling response: %v", err)
			return ErrBadPeerResponse
		}
	}

	return nil
}
