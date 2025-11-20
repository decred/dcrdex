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
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/dexnet"
	"decred.org/dcrdex/dex/msgjson"
	"decred.org/dcrdex/tatanka"
	"decred.org/dcrdex/tatanka/mj"
	"decred.org/dcrdex/tatanka/tanka"
	"decred.org/dcrdex/tatanka/tcp"
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

// tatankaNode is a Tatanka mesh server node.
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
		return nil, errors.New("ciphertext too short")
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

func encryptTankagramPayload(key []byte, msg *msgjson.Message) ([]byte, error) {
	plaintext, err := json.Marshal(msg)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal payload: %v", err)
	}

	return encryptAES(key, plaintext)
}

func encryptTankagramResult(result any, tankagramError mj.TankagramError, encKey []byte) (dex.Bytes, error) {
	var resB []byte
	if result != nil {
		var err error
		resB, err = json.Marshal(result)
		if err != nil {
			return nil, fmt.Errorf("error marshalling result: %v", err)
		}
	}

	tankagramResult := &mj.TankagramResultPayload{
		Error:   tankagramError,
		Payload: resB,
	}

	plainText, err := json.Marshal(tankagramResult)
	if err != nil {
		return nil, fmt.Errorf("error marshalling tankagram result: %v", err)
	}

	return encryptAES(encKey, plainText)
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
	HandleTatankaRequest      func(route string, payload json.RawMessage, respond func(any, *msgjson.Error))
	HandleTatankaNotification func(route string, payload json.RawMessage)
	HandlePeerMessage         func(peerID tanka.PeerID, route string, payload json.RawMessage, respond func(any, mj.TankagramError))
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
	// Addr should not include the protocol prefix.
	Addr  string
	Cert  []byte
	NoTLS bool
}

func (c *TatankaCredentials) HttpURL() string {
	if c.NoTLS {
		return fmt.Sprintf("http://%s", c.Addr)
	}
	return fmt.Sprintf("https://%s", c.Addr)
}

func (c *TatankaCredentials) WsURL() string {
	if c.NoTLS {
		return fmt.Sprintf("ws://%s", c.Addr)
	}
	return fmt.Sprintf("wss://%s", c.Addr)
}

// MeshConn is a Tatanka Mesh connection manager. MeshConn handles both tatanka
// nodes and regular peers.
type MeshConn struct {
	ctx       context.Context
	log       dex.Logger
	handlers  *MessageHandlers
	entryNode *TatankaCredentials

	subscriptionMtx sync.RWMutex
	subscriptions   map[tanka.Topic]map[tanka.Subject]bool

	maintainingMeshConnections atomic.Bool

	nodesMtx      sync.RWMutex
	primaryNode   *tatankaNode
	secondaryNode *tatankaNode
	knownNodes    []*TatankaCredentials

	peerID tanka.PeerID
	priv   *secp256k1.PrivateKey

	peersMtx sync.RWMutex
	peers    map[tanka.PeerID]*peer
}

// New is the constructor for a new MeshConn.
func New(cfg *Config) *MeshConn {
	var peerID tanka.PeerID
	copy(peerID[:], cfg.PrivateKey.PubKey().SerializeCompressed())
	c := &MeshConn{
		log:           cfg.Logger,
		handlers:      cfg.Handlers,
		priv:          cfg.PrivateKey,
		entryNode:     cfg.EntryNode,
		peerID:        peerID,
		knownNodes:    make([]*TatankaCredentials, 0, 4),
		peers:         make(map[tanka.PeerID]*peer),
		subscriptions: make(map[tanka.Topic]map[tanka.Subject]bool),
	}
	return c
}

// handleNewPeerTankagram handles a tankagram received from a new peer. The
// tankagram should be encrypted with the permanent encryption key derived from
// our permanent private key and the peer's peerID. The contents of the
// tankagram should be an EncryptionKeyPayload, used to establish an ephemeral
// encryption key.
func (c *MeshConn) handleNewPeerTankagram(gram *mj.Tankagram, id uint64, sendResponse func(*msgjson.Message)) {
	pub, err := secp256k1.ParsePubKey(gram.From[:])
	if err != nil {
		c.log.Errorf("Bad peer ID %s", gram.From)
		return
	}

	sharedSecretPerm := sharedSecret(c.priv, pub)

	sendError := func(tankagramErr mj.TankagramError) {
		enc, err := encryptTankagramResult(nil, tankagramErr, sharedSecretPerm)
		if err != nil {
			c.log.Errorf("Error encrypting tankagram result: %v", err)
			return
		}
		resp := mj.MustResponse(id, enc, nil)
		mj.SignMessage(c.priv, resp)
		sendResponse(resp)
	}

	decryptedPayload, err := decryptTankagramPayload(sharedSecretPerm, gram.EncryptedPayload)
	if err != nil {
		c.log.Errorf("failed to decrypt new peer tankagram with permanent key: %v", err)
		// Assume that they sent a message with an ephemeral key that
		// we do not have, possibly due to restarting the client. They
		// will need to do a new handshake before further communication.
		sendError(mj.TEDecryptionFailed)
		return
	}

	if decryptedPayload.Route != mj.RouteEncryptionKey {
		c.log.Errorf("unexpected route for new peer tankagram %s", decryptedPayload.Route)
		sendError(mj.TEEBadRequest)
		return
	}

	var encryptionKeyPayload mj.EncryptionKeyPayload
	if err := json.Unmarshal(decryptedPayload.Payload, &encryptionKeyPayload); err != nil {
		c.log.Errorf("Error unmarshalling encryption key payload: %v", err)
		sendError(mj.TEEBadRequest)
		return
	}

	remoteEphPub, err := secp256k1.ParsePubKey(encryptionKeyPayload.EphemeralPubKey)
	if err != nil {
		c.log.Errorf("Failed to parse ephemeral pubkey: %v", err)
		sendError(mj.TEEBadRequest)
		return
	}

	ephPriv, err := secp256k1.GeneratePrivateKey()
	if err != nil {
		c.log.Errorf("Failed to generate ephemeral private key: %v", err)
		sendError(mj.TEEPeerError)
		return
	}

	res := mj.EncryptionKeyPayload{
		EphemeralPubKey: ephPriv.PubKey().SerializeCompressed(),
	}
	enc, err := encryptTankagramResult(res, mj.TEErrNone, sharedSecretPerm)
	if err != nil {
		c.log.Errorf("Error encrypting tankagram result: %v", err)
		return
	}
	msg := mj.MustResponse(id, enc, nil)
	mj.SignMessage(c.priv, msg)
	sendResponse(msg)

	// Add the peer to our peers map.
	c.peersMtx.Lock()
	c.peers[gram.From] = &peer{
		id:                 gram.From,
		ephemeralSharedKey: sharedSecret(ephPriv, remoteEphPub),
	}
	c.peersMtx.Unlock()
}

// handleTankagram handles a tankagram from a peer.
func (c *MeshConn) handleTankagram(tankagram *msgjson.Message, sendResponse func(*msgjson.Message)) {
	var gram mj.Tankagram
	if err := tankagram.Unmarshal(&gram); err != nil {
		c.log.Errorf("error unmarshalling tankagram: %v", err)
		return
	}

	c.peersMtx.Lock()
	p, peerExisted := c.peers[gram.From]
	c.peersMtx.Unlock()

	// If the peer doesn't exist, we need to do the handshake.
	if !peerExisted {
		c.handleNewPeerTankagram(&gram, tankagram.ID, sendResponse)
		return
	}

	sendError := func(tankagramErr mj.TankagramError, encKey []byte) {
		enc, err := encryptTankagramResult(nil, tankagramErr, encKey)
		if err != nil {
			c.log.Errorf("Error encrypting tankagram result: %v", err)
			return
		}
		sendResponse(mj.MustResponse(tankagram.ID, enc, nil))
	}

	// An empty payload is invalid.
	if len(gram.EncryptedPayload) == 0 {
		sendError(mj.TEEBadRequest, p.ephemeralSharedKey)
		return
	}

	// If the payload cannot be decrypted with the ephemeral shared key,
	// let the peer know that a shared key must be established before
	// we can communicate.
	decryptedPayload, err := decryptTankagramPayload(p.ephemeralSharedKey, gram.EncryptedPayload)
	if err != nil {
		sendError(mj.TEDecryptionFailed, p.ephemeralSharedKey)
		return
	}

	respond := func(resp any, tankagramErr mj.TankagramError) {
		respB, err := encryptTankagramResult(resp, tankagramErr, p.ephemeralSharedKey)
		if err != nil {
			c.log.Errorf("Error encrypting tankagram result: %v", err)
			return
		}
		sendResponse(mj.MustResponse(tankagram.ID, respB, nil))
	}
	c.handlers.HandlePeerMessage(gram.From, decryptedPayload.Route, decryptedPayload.Payload, respond)
}

type numNodesToConnect int

const (
	numNodesToConnectSecondary        numNodesToConnect = 1
	numNodesToConnectPrimarySecondary numNodesToConnect = 2
)

func (c *MeshConn) Connect(ctx context.Context) (*sync.WaitGroup, error) {
	var wg sync.WaitGroup

	wg.Add(1)
	go c.disconnectNodesOnCancel(ctx, &wg)

	c.ctx = ctx

	knownNodes, err := c.fetchKnownNodes(ctx)
	if err != nil {
		return nil, fmt.Errorf("fetching known nodes: %w", err)
	}

	c.nodesMtx.Lock()
	c.knownNodes = knownNodes
	connectedNodes, err := c.connectToKnownNodes(numNodesToConnectPrimarySecondary, nil)
	if err != nil {
		c.nodesMtx.Unlock()
		return nil, fmt.Errorf("connecting to known nodes: %w", err)
	}
	for i, node := range connectedNodes {
		if i == 0 {
			c.primaryNode = node
		} else {
			c.secondaryNode = node
		}
	}
	c.nodesMtx.Unlock()

	wg.Add(1)
	go c.runNodeMaintenance(ctx, &wg)

	return &wg, nil
}

// disconnectNodesOnCancel disconnects the primary and secondary nodes
// when the context is cancelled.
func (c *MeshConn) disconnectNodesOnCancel(ctx context.Context, wg *sync.WaitGroup) {
	defer wg.Done()
	<-ctx.Done()

	c.nodesMtx.Lock()
	defer c.nodesMtx.Unlock()

	if c.primaryNode != nil {
		c.primaryNode.cm.Disconnect()
	}
	if c.secondaryNode != nil {
		c.secondaryNode.cm.Disconnect()
	}
}

func (c *MeshConn) runNodeMaintenance(ctx context.Context, wg *sync.WaitGroup) {
	defer wg.Done()

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			c.nodesMtx.Lock()
			c.maintainMeshConnections()
			c.nodesMtx.Unlock()
		case <-ctx.Done():
			return
		}
	}
}

func (c *MeshConn) getNodeInfo(ctx context.Context, url string) (*tatanka.NodeInfoResponse, error) {
	var resp tatanka.NodeInfoResponse
	if err := dexnet.Get(ctx, fmt.Sprintf("%s/%s", url, mj.RouteNodeInfo), &resp); err != nil {
		return nil, fmt.Errorf("error getting node info from node: %w", err)
	}
	return &resp, nil
}

// fetchKnownNodes queries the entry node for its list of whitelisted nodes
// and returns a list of TatankaCredentials for each node, including the
// entry node itself.
func (c *MeshConn) fetchKnownNodes(ctx context.Context) ([]*TatankaCredentials, error) {
	entryNodeInfo, err := c.getNodeInfo(ctx, c.entryNode.HttpURL())
	if err != nil {
		return nil, fmt.Errorf("error getting node info from entry node: %w", err)
	}

	knownNodes := make([]*TatankaCredentials, 0, len(entryNodeInfo.Whitelist)+1)
	knownNodes = append(knownNodes, c.entryNode)

	for _, node := range entryNodeInfo.Whitelist {
		var peerID tanka.PeerID
		copy(peerID[:], node.PeerID)

		var remoteNodeConfig tcp.RemoteNodeConfig
		if err := json.Unmarshal(node.Config, &remoteNodeConfig); err != nil {
			return nil, fmt.Errorf("error reading boot node configuration: %w", err)
		}

		// Remove protocol from URL
		addr := remoteNodeConfig.URL
		if idx := strings.Index(addr, "://"); idx >= 0 {
			addr = addr[idx+3:]
		}

		knownNodes = append(knownNodes, &TatankaCredentials{
			PeerID: peerID,
			Addr:   addr,
			Cert:   remoteNodeConfig.Cert,
			NoTLS:  len(remoteNodeConfig.Cert) == 0,
		})
	}

	return knownNodes, nil
}

// connectToKnownNodes connects to the specified number of nodes from c.knownNodes.
// If two nodes are being connected, the first returned node will have the
// subscriptions updated because this will be the primary node.
//
// The caller must hold c.nodesMtx.
func (c *MeshConn) connectToKnownNodes(numNodes numNodesToConnect, existingNode *tanka.PeerID) ([]*tatankaNode, error) {
	connectedNodes := make([]*tatankaNode, 0, int(numNodes))

	if c.log.Level() == dex.LevelTrace {
		c.log.Tracef("Connecting to %d known nodes", len(c.knownNodes))
	}

	for _, node := range c.knownNodes {
		if len(connectedNodes) >= int(numNodes) {
			break
		}

		if existingNode != nil && *existingNode == node.PeerID {
			continue
		}

		updateSubscriptions := len(connectedNodes) == 0 && numNodes == numNodesToConnectPrimarySecondary
		tatankaNode, err := c.connectToTatankaNode(c.ctx, node, updateSubscriptions)
		if err != nil {
			c.log.Errorf("Failed to connect to tatanka node %s: %v", node.PeerID, err)
			continue
		}

		connectedNodes = append(connectedNodes, tatankaNode)
	}

	if len(connectedNodes) == 0 {
		return nil, fmt.Errorf("unable to connect to any tatanka nodes")
	}

	return connectedNodes, nil
}

func (c *MeshConn) sendUpdateSubscriptionsRequest(tt *tatankaNode) error {
	subs := c.currentSubscriptions()
	updateSubsReq := mj.MustRequest(mj.RouteUpdateSubscriptions, &mj.UpdateSubscriptions{
		Subscriptions: subs,
	})
	mj.SignMessage(c.priv, updateSubsReq)
	return tt.Send(updateSubsReq)
}

// failoverToSecondary updates the secondary node to be the primary node.
// It also sends the required subscriptions to the new primary node. The
// function returns true if the failover was successful.
func (c *MeshConn) failoverToSecondary() bool {
	c.nodesMtx.Lock()
	defer c.nodesMtx.Unlock()

	c.primaryNode = c.secondaryNode
	c.secondaryNode = nil

	err := c.sendUpdateSubscriptionsRequest(c.primaryNode)
	if err != nil {
		c.log.Errorf("Failed to send update subscriptions request to new primary node: %v", err)
		c.primaryNode.cm.Disconnect()
		c.primaryNode = nil
		return false
	}

	return true
}

// maintainMeshConnections updates the secondary node to be the primary if
// there's no primary, and attempts to find new nodes if there is no primary
// or secondary node.
//
// c.nodesMtx MUST be locked when calling this function.
func (c *MeshConn) maintainMeshConnections() {
	if !c.maintainingMeshConnections.CompareAndSwap(false, true) {
		return
	}
	defer c.maintainingMeshConnections.Store(false)

	c.nodesMtx.RLock()
	if c.primaryNode != nil && c.secondaryNode != nil {
		c.nodesMtx.RUnlock()
		return
	}

	numNodesRequired := numNodesToConnectSecondary
	var existingNode *tanka.PeerID
	var failoverToSecondary bool

	if c.primaryNode == nil && c.secondaryNode != nil {
		// Doing the failover later because we need a write lock to do it.
		failoverToSecondary = true
		existingNode = &c.secondaryNode.peerID
	} else if c.primaryNode != nil && c.secondaryNode == nil {
		existingNode = &c.primaryNode.peerID
	} else { // c.primaryNode == nil && c.secondaryNode == nil
		numNodesRequired = numNodesToConnectPrimarySecondary
	}
	c.nodesMtx.RUnlock()

	// Attempt to failover to the secondary node if necessary. If it fails,
	// we'll try to find a new primary and secondary node.
	if failoverToSecondary && !c.failoverToSecondary() {
		numNodesRequired = numNodesToConnectPrimarySecondary
		existingNode = nil
	}

	if numNodesRequired == numNodesToConnectSecondary {
		c.log.Infof("Attempting to find a new secondary node.")
	} else {
		c.log.Infof("Attempting to find a new primary and secondary node.")
	}

	newNodes, err := c.connectToKnownNodes(numNodesRequired, existingNode)
	if err != nil {
		c.log.Errorf("Failed to update node(s): %w", err)
		return
	}

	c.nodesMtx.Lock()
	for _, node := range newNodes {
		if c.primaryNode == nil {
			c.primaryNode = node
		} else {
			c.secondaryNode = node
		}
	}
	c.nodesMtx.Unlock()
}

func (c *MeshConn) handleNodeDisconnected(peerID tanka.PeerID) {
	if c.ctx.Err() != nil {
		return
	}

	c.nodesMtx.Lock()
	if c.primaryNode != nil && c.primaryNode.peerID == peerID {
		c.primaryNode = nil
		c.log.Infof("Primary node (%s) disconnected.", peerID)
	}
	if c.secondaryNode != nil && c.secondaryNode.peerID == peerID {
		c.secondaryNode = nil
		c.log.Infof("Secondary node (%s) disconnected.", peerID)
	}
	c.nodesMtx.Unlock()

	c.maintainMeshConnections()
}

func (c *MeshConn) currentSubscriptions() map[tanka.Topic][]tanka.Subject {
	subs := make(map[tanka.Topic][]tanka.Subject)

	c.subscriptionMtx.RLock()
	for topic, subjects := range c.subscriptions {
		if len(subjects) == 0 {
			continue
		}
		subs[topic] = make([]tanka.Subject, 0, len(subjects))
		for subject := range subjects {
			subs[topic] = append(subs[topic], subject)
		}
	}
	c.subscriptionMtx.RUnlock()

	return subs
}

func (c *MeshConn) sendConnectRequest(cl *tatankaNode, updateSubscriptions bool) (*mj.TatankaConfig, error) {
	var subs map[tanka.Topic][]tanka.Subject
	if updateSubscriptions {
		subs = c.currentSubscriptions()
	}

	connectReq := mj.MustRequest(mj.RouteConnect, &mj.Connect{
		ID:          c.peerID,
		InitialSubs: subs,
	})
	mj.SignMessage(c.priv, connectReq)

	var tatankaConfig *mj.TatankaConfig
	if err := c.requestTT(cl, connectReq, &tatankaConfig, DefaultRequestTimeout, true); err != nil {
		return nil, fmt.Errorf("error requesting tatanka config: %w", err)
	}

	return tatankaConfig, nil
}

func (c *MeshConn) setupWSConnection(ctx context.Context, creds *TatankaCredentials) (tt *tatankaNode, err error) {
	pub, err := secp256k1.ParsePubKey(creds.PeerID[:])
	if err != nil {
		return nil, fmt.Errorf("error parsing pubkey from peer ID: %w", err)
	}

	url := creds.WsURL() + "/ws"

	initialConnectSuccessful := false
	cfg := &tcpclient.Config{
		Logger: c.log.SubLogger("TCP"),
		URL:    url,
		Cert:   creds.Cert,
		ConnectEventFunc: func(status tcpclient.ConnectionStatus) {
			if !initialConnectSuccessful {
				return
			}
			if status == tcpclient.Disconnected {
				c.handleNodeDisconnected(creds.PeerID)
			}
		},
		HandleMessage: func(msg *msgjson.Message, sendResponse func(*msgjson.Message)) {
			c.handleTatankaMessage(creds.PeerID, pub, msg, sendResponse)
		},
	}
	cl, err := tcpclient.New(cfg)
	if err != nil {
		return nil, fmt.Errorf("error creating connection: %w", err)
	}
	cm := dex.NewConnectionMaster(cl)
	if err := cm.ConnectOnce(ctx); err != nil {
		return nil, fmt.Errorf("error connecting to tatanka node %s at %s: %w", creds.PeerID, cfg.URL, err)
	}

	initialConnectSuccessful = true

	return &tatankaNode{
		NetworkBackend: cl,
		cm:             cm,
		url:            url,
		peerID:         creds.PeerID,
		pub:            pub,
	}, nil
}

func (c *MeshConn) connectToTatankaNode(ctx context.Context, creds *TatankaCredentials, updateSubscriptions bool) (*tatankaNode, error) {
	tt, err := c.setupWSConnection(ctx, creds)
	if err != nil {
		return nil, fmt.Errorf("error setting up WS connection: %w", err)
	}

	tatankaConfig, err := c.sendConnectRequest(tt, updateSubscriptions)
	if err != nil {
		return nil, fmt.Errorf("error sending connect request: %w", err)
	}

	tt.config.Store(tatankaConfig)

	if c.log.Level() == dex.LevelTrace {
		c.log.Tracef("Connected to %s", tt.url)
	}

	return tt, nil
}

func (c *MeshConn) handleTatankaMessage(tatankaID tanka.PeerID, pub *secp256k1.PublicKey, msg *msgjson.Message, sendResponse func(*msgjson.Message)) {
	if c.log.Level() == dex.LevelTrace {
		c.log.Tracef("Client handling message from tatanka node: route = %s", msg.Route)
	}

	if msg.Type == msgjson.Request && msg.Route == mj.RouteTankagram {
		c.handleTankagram(msg, sendResponse)
		return
	}

	switch msg.Type {
	case msgjson.Request:
		respond := func(payload any, err *msgjson.Error) {
			resp := mj.MustResponse(msg.ID, payload, err)
			mj.SignMessage(c.priv, resp)
			sendResponse(resp)
		}
		c.handlers.HandleTatankaRequest(msg.Route, msg.Payload, respond)
	case msgjson.Notification:
		c.handlers.HandleTatankaNotification(msg.Route, msg.Payload)
	default:
		c.log.Errorf("tatanka node %s send a message with an unhandleable type %d", tatankaID, msg.Type)
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
	timeout     time.Duration
	errCodeFunc func(code int)
	examineFunc func(tatankaURL string, tatankaID tanka.PeerID) (ok bool)
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

// WithExamination allows the caller to check the result before returning, and
// continue trying other tatanka nodes if necessary.
func WithExamination(f func(tatankaURL string, tatankaID tanka.PeerID) (resultOK bool)) RequestOption {
	return func(cfg *requestConfig) {
		cfg.examineFunc = f
	}
}

func (c *MeshConn) Subscribe(topic tanka.Topic, subject tanka.Subject) error {
	req := mj.MustRequest(mj.RouteSubscribe, &mj.Subscription{
		Topic:   topic,
		Subject: subject,
	})
	mj.SignMessage(c.priv, req)

	// Only possible non-error response is `true`.
	var ok bool
	err := c.RequestMesh(req, &ok)
	if err != nil {
		return err
	}

	c.subscriptionMtx.Lock()
	defer c.subscriptionMtx.Unlock()

	_, ok = c.subscriptions[topic]
	if !ok {
		c.subscriptions[topic] = make(map[tanka.Subject]bool)
	}
	c.subscriptions[topic][subject] = true

	return nil
}

// RequestMesh sends a request to the Mesh.
func (c *MeshConn) RequestMesh(msg *msgjson.Message, thing any, opts ...RequestOption) error {
	cfg := &requestConfig{
		timeout: DefaultRequestTimeout,
	}
	for _, opt := range opts {
		opt(cfg)
	}

	c.nodesMtx.RLock()
	primaryNode := c.primaryNode
	c.nodesMtx.RUnlock()
	if primaryNode == nil {
		return errors.New("not connected to any tatanka nodes")
	}

	mj.SignMessage(c.priv, msg)
	err := c.requestTT(primaryNode, msg, thing, cfg.timeout)
	if err != nil {
		return err
	}

	if cfg.examineFunc != nil && !cfg.examineFunc(primaryNode.url, primaryNode.peerID) {
		return fmt.Errorf("request failed examination")
	}

	return nil
}

func (c *MeshConn) requestTT(tt *tatankaNode, msg *msgjson.Message, thing any, timeout time.Duration, checkSig ...bool) (err error) {
	errChan := make(chan error)
	if err := tt.Request(msg, func(msg *msgjson.Message) {
		if len(checkSig) > 0 && checkSig[0] {
			if err := mj.CheckSig(msg, tt.pub); err != nil {
				errChan <- fmt.Errorf("signature check failed: %w", err)
				return
			}
		}
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
	if err := c.RequestMesh(req, &r, WithExamination(func(tatankaURL string, tatankaID tanka.PeerID) bool {
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
func (c *MeshConn) RequestPeer(peerID tanka.PeerID, msg *msgjson.Message, thing any) error {
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

	if err := c.RequestMesh(wrappedMsg, &res, WithExamination(func(tatankaURL string, tatankaID tanka.PeerID) bool {
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
