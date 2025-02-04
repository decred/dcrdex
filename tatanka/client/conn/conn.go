// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package conn

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"sync"
	"sync/atomic"
	"time"

	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/msgjson"
	"decred.org/dcrdex/tatanka/mj"
	"decred.org/dcrdex/tatanka/tanka"
	tcpclient "decred.org/dcrdex/tatanka/tcp/client"
	"github.com/decred/dcrd/crypto/blake256"
	"github.com/decred/dcrd/dcrec/secp256k1/v4"
)

const rsaPrivateKeyLength = 2048

// NetworkBackend represents a peer's communication protocol.
type NetworkBackend interface {
	Send(*msgjson.Message) error
	Request(msg *msgjson.Message, respHandler func(*msgjson.Message)) error
}

// tatanka is a Tatanka mesh server node.
type tatanka struct {
	NetworkBackend
	cm     *dex.ConnectionMaster
	url    string
	peerID tanka.PeerID
	pub    *secp256k1.PublicKey
	config atomic.Value // *mj.TatankaConfig
}

func (tt *tatanka) String() string {
	return fmt.Sprintf("%s @ %s", tt.peerID, tt.url)
}

// peer is a network peer with which we have established encrypted
// communication.
type peer struct {
	id            tanka.PeerID
	pub           *secp256k1.PublicKey
	decryptionKey *rsa.PrivateKey // ours
	encryptionKey *rsa.PublicKey
}

// wireKey encrypts our RSA public key for transmission.
func (p *peer) wireKey() []byte {
	modulusB := p.decryptionKey.PublicKey.N.Bytes()
	encryptionKey := make([]byte, 8+len(modulusB))
	binary.BigEndian.PutUint64(encryptionKey[:8], uint64(p.decryptionKey.PublicKey.E))
	copy(encryptionKey[8:], modulusB)
	return encryptionKey
}

// https://stackoverflow.com/a/67035019
func (p *peer) decryptRSA(enc []byte) ([]byte, error) {
	msgLen := len(enc)
	hasher := blake256.New()
	step := p.decryptionKey.PublicKey.Size()
	var b []byte
	for start := 0; start < msgLen; start += step {
		finish := start + step
		if finish > msgLen {
			finish = msgLen
		}
		block, err := rsa.DecryptOAEP(hasher, rand.Reader, p.decryptionKey, enc[start:finish], []byte{})
		if err != nil {
			return nil, err
		}
		b = append(b, block...)
	}
	return b, nil
}

func (p *peer) encryptRSA(b []byte) ([]byte, error) {
	msgLen := len(b)
	hasher := blake256.New()
	step := p.encryptionKey.Size() - 2*hasher.Size() - 2
	var enc []byte
	for start := 0; start < msgLen; start += step {
		finish := start + step
		if finish > msgLen {
			finish = msgLen
		}
		block, err := rsa.EncryptOAEP(hasher, rand.Reader, p.encryptionKey, b[start:finish], []byte{})
		if err != nil {
			return nil, err
		}
		enc = append(enc, block...)
	}
	return enc, nil
}

// IncomingPeerConnect will be emitted when a peer requests a connection for
// the transmission of tankagrams.
type IncomingPeerConnect struct {
	PeerID tanka.PeerID
	Reject func()
}

// IncomingTankagram will be emitted when we receive a tankagram from a
// connected peer.
type IncomingTankagram struct {
	Msg     *msgjson.Message
	Respond func(thing interface{}) error
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
	HandlePeerMessage         func(tanka.PeerID, any) *msgjson.Error
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
	tatankaNodes map[tanka.PeerID]*tatanka

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
		tatankaNodes: make(map[tanka.PeerID]*tatanka),
		peers:        make(map[tanka.PeerID]*peer),
	}
	return c
}

func (c *MeshConn) handleTankagram(tt *tatanka, tankagram *msgjson.Message) *msgjson.Error {
	var gram mj.Tankagram
	if err := tankagram.Unmarshal(&gram); err != nil {
		return msgjson.NewError(mj.ErrBadRequest, "unmarshal error")
	}

	c.peersMtx.Lock()
	defer c.peersMtx.Unlock()
	p, peerExisted := c.peers[gram.From]
	if !peerExisted {
		// TODO: We should do a little message verification before accepting
		// new peers.
		if gram.Message == nil || gram.Message.Route != mj.RouteEncryptionKey {
			return msgjson.NewError(mj.ErrBadRequest, "where's your key?")
		}
		pub, err := secp256k1.ParsePubKey(gram.From[:])
		if err != nil {
			c.log.Errorf("could not parse pubkey for tankagram from %s: %w", gram.From, err)
			return msgjson.NewError(mj.ErrBadRequest, "bad pubkey")
		}
		priv, err := rsa.GenerateKey(rand.Reader, rsaPrivateKeyLength)
		if err != nil {
			return msgjson.NewError(mj.ErrInternal, "error generating rsa key: %v", err)
		}
		p = &peer{
			id:            gram.From,
			pub:           pub,
			decryptionKey: priv,
		}

		msg := gram.Message
		if err := mj.CheckSig(msg, p.pub); err != nil {
			c.log.Errorf("%s sent a unencrypted message with a bad signature: %w", p.id, err)
			return msgjson.NewError(mj.ErrBadRequest, "bad gram sig")
		}

		var b dex.Bytes
		if err := msg.Unmarshal(&b); err != nil {
			c.log.Errorf("%s tankagram unmarshal error: %w", err)
			return msgjson.NewError(mj.ErrBadRequest, "unmarshal key error")
		}

		p.encryptionKey, err = decodePubkeyRSA(b)
		if err != nil {
			c.log.Errorf("error decoding RSA pub key from %s: %v", p.id, err)
			return msgjson.NewError(mj.ErrBadRequest, "bad key encoding")
		}

		if err := c.sendResult(tt, tankagram.ID, dex.Bytes(p.wireKey())); err != nil {
			c.log.Errorf("error responding to encryption key message from peer %s", p.id)
		} else {
			c.peers[p.id] = p
			c.handlers.HandlePeerMessage(p.id, &IncomingPeerConnect{
				PeerID: p.id,
				Reject: func() {
					c.peersMtx.Lock()
					delete(c.peers, p.id)
					c.peersMtx.Unlock()
				},
			})
		}
		return nil
	}

	// If this isn't the encryption key, this gram.Message is ignored and this
	// is assumed to be encrypted.
	if len(gram.EncryptedMsg) == 0 {
		c.log.Errorf("%s sent a tankagram with no message or data", p.id)
		return msgjson.NewError(mj.ErrBadRequest, "bad gram")
	}

	b, err := p.decryptRSA(gram.EncryptedMsg)
	if err != nil {
		c.log.Errorf("%s sent an enrypted message that didn't decrypt: %v", p.id, err)
		return msgjson.NewError(mj.ErrBadRequest, "bad encryption")
	}
	msg, err := msgjson.DecodeMessage(b)
	if err != nil {
		c.log.Errorf("%s sent a tankagram that didn't encode a message: %v", p.id, err)
		return msgjson.NewError(mj.ErrBadRequest, "where's the message?")
	}

	c.handlers.HandlePeerMessage(p.id, &IncomingTankagram{
		Msg: msg,
		Respond: func(thing interface{}) error {
			b, err := json.Marshal(thing)
			if err != nil {
				return err
			}
			enc, err := p.encryptRSA(b)
			if err != nil {
				return err
			}
			return c.sendResult(tt, tankagram.ID, dex.Bytes(enc))
		},
	})

	return nil
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
		HandleMessage: func(msg *msgjson.Message) *msgjson.Error {
			return c.handleTatankaMessage(peerID, msg)
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

	c.tatankaNodes[peerID] = &tatanka{
		NetworkBackend: cl,
		cm:             cm,
		url:            uri,
		peerID:         peerID,
		pub:            pub,
	}

	if err := cm.Connect(ctx); err != nil {
		c.log.Errorf("error connecting to tatanka node %s at %s: %v. will keep trying to connect", err)
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

func (c *MeshConn) tatanka(tatankaID tanka.PeerID) *tatanka {
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
func (c *MeshConn) tatankas(mode TatankaSelectionMode) (tts []*tatanka, _ error) {
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
		return []*tatanka{en}, nil
	case SelectionModeAll, SelectionModeAny:
		tts := make([]*tatanka, 0, len(c.tatankaNodes))
		var skipID tanka.PeerID
		// Entry node always goes first, if available.
		if en != nil && en.cm.On() {
			tts = append(tts, en)
			skipID = en.peerID
		}
		for peerID, tt := range c.tatankaNodes {
			if tt.cm.On() && peerID != skipID {
				tts = append(tts, tt)
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

func (c *MeshConn) handleTatankaMessage(tatankaID tanka.PeerID, msg *msgjson.Message) *msgjson.Error {
	if c.log.Level() == dex.LevelTrace {
		c.log.Tracef("Client handling message from tatanka node: route = %s, payload = %s", msg.Route, mj.Truncate(msg.Payload))
	}

	tt := c.tatanka(tatankaID)
	if tt == nil {
		c.log.Error("Message received from unknown tatanka node")
		return msgjson.NewError(mj.ErrUnknownSender, "who are you?")
	}

	if err := mj.CheckSig(msg, tt.pub); err != nil {
		// DRAFT TODO: Record for reputation somehow, no?
		c.log.Errorf("tatanka node %s sent a bad signature. disconnecting", tt.peerID)
		return msgjson.NewError(mj.ErrAuth, "bad sig")
	}

	if msg.Type == msgjson.Request && msg.Route == mj.RouteTankagram {
		return c.handleTankagram(tt, msg)
	}

	switch msg.Type {
	case msgjson.Request:
		return c.handlers.HandleTatankaRequest(tatankaID, msg)
	case msgjson.Notification:
		c.handlers.HandleTatankaNotification(tatankaID, msg)
		return nil
	default:
		c.log.Errorf("tatanka node %s send a message with an unhandleable type %d", msg.Type)
		return msgjson.NewError(mj.ErrBadRequest, "message type %d doesn't work for me", msg.Type)
	}
}

func (c *MeshConn) sendResult(tt *tatanka, msgID uint64, result interface{}) error {
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

func (c *MeshConn) requestTT(tt *tatanka, msg *msgjson.Message, thing any, timeout time.Duration) (err error) {
	errChan := make(chan error)
	if err := tt.Request(msg, func(msg *msgjson.Message) {
		errChan <- msg.UnmarshalResult(thing)
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

// ConnectPeer connects to a peer by sending our encryption key and receiving
// theirs.
func (c *MeshConn) ConnectPeer(peerID tanka.PeerID) error {
	c.peersMtx.Lock()
	defer c.peersMtx.Unlock()
	p, exists := c.peers[peerID]
	if !exists {
		priv, err := rsa.GenerateKey(rand.Reader, rsaPrivateKeyLength)
		if err != nil {
			return fmt.Errorf("error generating rsa key: %v", err)
		}
		remotePub, err := secp256k1.ParsePubKey(peerID[:])
		if err != nil {
			return fmt.Errorf("error parsing remote pubkey: %v", err)
		}
		p = &peer{
			id:            peerID,
			pub:           remotePub,
			decryptionKey: priv,
		}
	}

	msg := mj.MustNotification(mj.RouteEncryptionKey, dex.Bytes(p.wireKey()))
	mj.SignMessage(c.priv, msg) // We sign the embedded message separately.

	req := mj.MustRequest(mj.RouteTankagram, &mj.Tankagram{
		To:      peerID,
		From:    c.peerID,
		Message: msg,
	})

	var r mj.TankagramResult
	if err := c.RequestMesh(req, &r, WithSelectionMode(SelectionModeAny), WithExamination(func(tatankaURL string, tatankaID tanka.PeerID) bool {
		if r.Result == mj.TRTTransmitted {
			return true
		}
		c.log.Errorf("Tankagram transmission failure connecting to %s via %s @ %s: %q", peerID, tatankaID, tatankaURL, r.Result)
		return false
	})); err != nil {
		return err
	}

	// We need to get this to the caller, as a Tankagram result can
	// be used as part of an audit request for reporting penalties.
	pub, err := decodePubkeyRSA(r.Response)
	if err != nil {
		return fmt.Errorf("error decoding RSA pub key from %s: %v", p.id, err)
	}
	p.encryptionKey = pub
	c.peers[peerID] = p
	return nil
}

// RequestPeer sends a request to an already-connected peer.
func (c *MeshConn) RequestPeer(peerID tanka.PeerID, msg *msgjson.Message, thing interface{}) (_ *mj.TankagramResult, err error) {
	c.peersMtx.RLock()
	p, known := c.peers[peerID]
	c.peersMtx.RUnlock()

	if !known {
		return nil, fmt.Errorf("not connected to peer %s", peerID)
	}

	mj.SignMessage(c.priv, msg)
	tankaGram := &mj.Tankagram{
		From: c.peerID,
		To:   peerID,
	}
	tankaGram.EncryptedMsg, err = c.signAndEncryptTankagram(p, msg)
	if err != nil {
		return nil, fmt.Errorf("error signing and encrypting tankagram for %s: %w", p.id, err)
	}
	var res mj.TankagramResult
	wrappedMsg := mj.MustRequest(mj.RouteTankagram, tankaGram)
	if err := c.RequestMesh(wrappedMsg, &res, WithSelectionMode(SelectionModeAny), WithExamination(func(tatankaURL string, tatankaID tanka.PeerID) bool {
		if res.Result == mj.TRTTransmitted {
			return true
		}
		c.log.Errorf("Tankagram transmission failure sending %s to %s via %s @ %s: %q", msg.Route, peerID, tatankaID, tatankaURL, res.Result)
		return false
	})); err != nil {
		return nil, err
	}
	respB, err := p.decryptRSA(res.Response)
	if err != nil {
		return nil, fmt.Errorf("message to %s transmitted, but errored while decoding response: %w", p.id, err)
	}
	if thing != nil {
		if len(respB) == 0 {
			return nil, fmt.Errorf("empty response from %s when non-empty response expected", p.id)
		}
		if err = json.Unmarshal(respB, thing); err != nil {
			return nil, fmt.Errorf("error unmarshaling result from peer %s: %w", p.id, err)
		}
	}
	return &res, nil
}

func (c *MeshConn) signAndEncryptTankagram(p *peer, msg *msgjson.Message) ([]byte, error) {
	mj.SignMessage(c.priv, msg)
	b, err := json.Marshal(msg)
	if err != nil {
		return nil, fmt.Errorf("error marshaling tankagram: %w", err)
	}
	return p.encryptRSA(b)
}

func decodePubkeyRSA(b []byte) (*rsa.PublicKey, error) {
	if len(b) < 9 {
		return nil, fmt.Errorf("invalid payload length of %d", len(b))
	}
	exponentB, modulusB := b[:8], b[8:]
	exponent := int(binary.BigEndian.Uint64(exponentB))
	modulus := new(big.Int).SetBytes(modulusB)
	return &rsa.PublicKey{
		E: exponent,
		N: modulus,
	}, nil
}
