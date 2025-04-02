//go:build live

package conn

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"os"
	"os/user"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/msgjson"
	dexbtc "decred.org/dcrdex/dex/networks/btc"
	"decred.org/dcrdex/server/comms"
	"decred.org/dcrdex/tatanka"
	"decred.org/dcrdex/tatanka/chain/utxo"
	"decred.org/dcrdex/tatanka/mj"
	"decred.org/dcrdex/tatanka/tanka"
	"decred.org/dcrdex/tatanka/tcp"
	"github.com/decred/dcrd/dcrec/secp256k1/v4"
)

var (
	logMaker      *dex.LoggerMaker
	usr, _        = user.Current()
	dextestDir    = filepath.Join(usr.HomeDir, "dextest")
	decredRPCPath = filepath.Join(dextestDir, "dcr", "alpha", "rpc.cert")
	chains        = []tatanka.ChainConfig{
		{
			Symbol: "dcr",
			Config: mustEncode(&utxo.DecredConfigFile{
				RPCUser:   "user",
				RPCPass:   "pass",
				RPCListen: "127.0.0.1:19561",
				RPCCert:   decredRPCPath,
			}),
		},
		{
			Symbol: "btc",
			Config: mustEncode(&utxo.BitcoinConfigFile{
				RPCConfig: dexbtc.RPCConfig{
					RPCUser: "user",
					RPCPass: "pass",
					RPCBind: "127.0.0.1",
					RPCPort: 20556,
				},
			}),
		},
	}
)

// ### Helper Functions

func newBootNode(addr string, peerID []byte) tatanka.BootNode {
	tcpCfg, _ := json.Marshal(&tcp.RemoteNodeConfig{
		URL: "ws://" + addr,
	})
	return tatanka.BootNode{
		Protocol: "ws",
		Config:   tcpCfg,
		PeerID:   peerID,
	}
}

func genKeyPair() (*secp256k1.PrivateKey, tanka.PeerID) {
	priv, _ := secp256k1.GeneratePrivateKey()
	var peerID tanka.PeerID
	copy(peerID[:], priv.PubKey().SerializeCompressed())
	return priv, peerID
}

func mustEncode(thing interface{}) json.RawMessage {
	b, err := json.Marshal(thing)
	if err != nil {
		panic("mustEncode: " + err.Error())
	}
	return b
}

// ### Test types

type tTatankaNodeInfo struct {
	priv   *secp256k1.PrivateKey
	peerID tanka.PeerID
	addr   net.Addr
}

type tTatanka struct {
	tatanka  *tatanka.Tatanka
	cm       *dex.ConnectionMaster
	nodeInfo *tTatankaNodeInfo
}

type tConn struct {
	conn                         *MeshConn
	cm                           *dex.ConnectionMaster
	priv                         *secp256k1.PrivateKey
	peerID                       tanka.PeerID
	incomingTatankaRequests      chan *tatankaRequestHandler
	incomingTatankaNotifications chan *tatankaNoteHandler
	incomingPeerMsgs             chan *tankagramHandler
}

// ### Network Setup Functions

// meshNodeInfos creates a list of node info for a mesh of n nodes.
func meshNodeInfos(n int) ([]*tTatankaNodeInfo, error) {
	nodes := make([]*tTatankaNodeInfo, 0, n)

	for i := 0; i < n; i++ {
		priv, peerID := genKeyPair()

		addr, err := net.ResolveTCPAddr("tcp", "localhost:0")
		if err != nil {
			return nil, err
		}

		l, err := net.ListenTCP("tcp", addr)
		if err != nil {
			return nil, err
		}
		defer l.Close()

		nodes = append(nodes, &tTatankaNodeInfo{
			priv:   priv,
			peerID: peerID,
			addr:   l.Addr(),
		})
	}

	return nodes, nil
}

// runServer starts a new Tatanka node with the given node info and other nodes as
// part of its whitelist.
func runServer(t *testing.T, ctx context.Context, thisNode *tTatankaNodeInfo, otherNodes []*tTatankaNodeInfo, disableMessariFiatRateSource bool) (*tTatanka, error) {
	dir := t.TempDir()
	os.MkdirAll(dir, 0755)
	os.WriteFile(filepath.Join(dir, "priv.key"), thisNode.priv.Serialize(), 0644)

	whiteList := make([]tatanka.BootNode, 0, len(otherNodes))
	for _, node := range otherNodes {
		whiteList = append(whiteList, newBootNode(node.addr.String(), node.peerID[:]))
	}

	log := logMaker.Logger(fmt.Sprintf("SRV[%s]", thisNode.addr))
	ttCfg := &tatanka.ConfigFile{Chains: chains}
	rawCfg, _ := json.Marshal(ttCfg)
	cfgPath := filepath.Join(dir, "config.json")
	if err := os.WriteFile(cfgPath, rawCfg, 0644); err != nil {
		log.Errorf("WriteFile error: %v", err)
		return nil, err
	}

	cfg := &tatanka.Config{
		Net:     dex.Simnet,
		DataDir: dir,
		Logger:  log,
		RPC: comms.RPCConfig{
			ListenAddrs: []string{thisNode.addr.String()},
			NoTLS:       true,
		},
		ConfigPath: cfgPath,
		WhiteList:  whiteList,
		MaxClients: 100,
	}

	if disableMessariFiatRateSource {
		// Requesting multiple rates from the same IP can trigger a 429 HTTP
		// error.
		cfg.FiatOracleConfig.DisabledFiatSources = "Messari"
	}

	tatanka, err := tatanka.New(cfg)
	if err != nil {
		log.Errorf("error creating Tatanka node: %v", err)
		return nil, err
	}

	cm := dex.NewConnectionMaster(tatanka)
	if err := cm.ConnectOnce(ctx); err != nil {
		log.Errorf("ConnectOnce error: %v", err)
		return nil, err
	}

	return &tTatanka{
		tatanka:  tatanka,
		cm:       cm,
		nodeInfo: thisNode,
	}, nil
}

type tMesh struct {
	nodeInfos []*tTatankaNodeInfo
	ctx       context.Context
	t         *testing.T

	mtx      sync.RWMutex
	tatankas []*tTatanka
}

func (m *tMesh) tatanka(i int) *tTatanka {
	m.mtx.RLock()
	defer m.mtx.RUnlock()
	return m.tatankas[i]
}

func (m *tMesh) stop(i int) {
	m.mtx.Lock()
	defer m.mtx.Unlock()

	m.tatankas[i].cm.Disconnect()
}

// TODO: simply connecting a node after disconnecting does not work.
// There will be the following error:
// unexpected (http.Server).Serve error: accept tcp4 127.0.0.1:57146: use of closed network connection
func (m *tMesh) start(i int) {
	m.t.Helper()

	m.mtx.Lock()
	defer m.mtx.Unlock()

	if m.tatankas[i].cm.On() {
		m.t.Fatalf("tatanka %d already running", i)
	}

	thisNode := m.nodeInfos[i]
	var otherNodes []*tTatankaNodeInfo
	for j := range m.nodeInfos {
		if j == i {
			continue
		}
		otherNodes = append(otherNodes, m.nodeInfos[j])
	}

	tt, err := runServer(m.t, m.ctx, thisNode, otherNodes, true)
	if err != nil {
		panic(err)
	}

	m.tatankas[i] = tt
}

// setupMesh creates a mesh of numNode tatankas, with each node having all
// other nodes in the mesh as its whitelist.
func setupMesh(t *testing.T, ctx context.Context, numNodes int) (*tMesh, error) {
	nodes, err := meshNodeInfos(numNodes)
	if err != nil {
		return nil, fmt.Errorf("meshNodeInfos error: %w", err)
	}

	tatankas := make([]*tTatanka, 0, numNodes)
	for i := 0; i < numNodes; i++ {
		thisNode := nodes[i]
		otherNodes := make([]*tTatankaNodeInfo, 0, numNodes-1)
		otherNodes = append(otherNodes, nodes[:i]...)
		otherNodes = append(otherNodes, nodes[i+1:]...)

		tt, err := runServer(t, ctx, thisNode, otherNodes, true)
		if err != nil {
			return nil, fmt.Errorf("runServer error: %w", err)
		}

		tatankas = append(tatankas, tt)
	}

	return &tMesh{
		nodeInfos: nodes,
		ctx:       ctx,
		t:         t,
		tatankas:  tatankas,
	}, nil
}

type tatankaRequestHandler struct {
	route   string
	payload json.RawMessage
	respond func(any, *msgjson.Error)
}

type tatankaNoteHandler struct {
	route   string
	payload json.RawMessage
}

type tankagramHandler struct {
	peerID  tanka.PeerID
	route   string
	payload json.RawMessage
	respond func(any, mj.TankagramError)
}

// newTestConn creates a new client connection to a tatanka node.
func newTestConn(t *testing.T, i int, ctx context.Context, tatankaNodeInfo *tTatankaNodeInfo) (*tConn, error) {
	priv, peerID := genKeyPair()

	// Setup channels for incoming messages
	incomingTatankaRequests := make(chan *tatankaRequestHandler, 10)
	incomingTatankaNotifications := make(chan *tatankaNoteHandler, 10)
	incomingPeerMsgs := make(chan *tankagramHandler, 10)

	// Configure connection
	cfg := &Config{
		EntryNode: &TatankaCredentials{
			PeerID: tatankaNodeInfo.peerID,
			Addr:   tatankaNodeInfo.addr.String(),
			NoTLS:  true,
		},
		Handlers: &MessageHandlers{
			HandleTatankaRequest: func(route string, payload json.RawMessage, respond func(any, *msgjson.Error)) {
				incomingTatankaRequests <- &tatankaRequestHandler{route: route, payload: payload, respond: respond}
			},
			HandleTatankaNotification: func(route string, payload json.RawMessage) {
				incomingTatankaNotifications <- &tatankaNoteHandler{route: route, payload: payload}
			},
			HandlePeerMessage: func(peerID tanka.PeerID, route string, payload json.RawMessage, respond func(any, mj.TankagramError)) {
				incomingPeerMsgs <- &tankagramHandler{peerID: peerID, route: route, payload: payload, respond: respond}
			},
		},
		Logger:     logMaker.NewLogger(fmt.Sprintf("CONN%d", i), dex.LevelTrace),
		PrivateKey: priv,
	}

	// Create and connect
	conn := New(cfg)
	cm := dex.NewConnectionMaster(conn)
	if err := cm.ConnectOnce(ctx); err != nil {
		return nil, fmt.Errorf("ConnectOnce error: %w", err)
	}

	return &tConn{
		conn:                         conn,
		cm:                           cm,
		priv:                         priv,
		peerID:                       peerID,
		incomingTatankaRequests:      incomingTatankaRequests,
		incomingTatankaNotifications: incomingTatankaNotifications,
		incomingPeerMsgs:             incomingPeerMsgs,
	}, nil
}

func TestMain(m *testing.M) {
	var err error
	logMaker, err = dex.NewLoggerMaker(os.Stdout, dex.LevelTrace.String())
	if err != nil {
		panic("failed to create custom logger: " + err.Error())
	}
	comms.UseLogger(logMaker.NewLogger("COMMS", dex.LevelTrace))
	os.Exit(m.Run())
}

// TestConnectPeer tests that a client can connect to another client
// and send a tankagram request to it.
func TestConnectPeer(t *testing.T) {
	ctx, cc := context.WithCancel(context.Background())
	fmt.Println(cc)
	// defer cancel()

	// Create a two node mesh
	mesh, err := setupMesh(t, ctx, 2)
	if err != nil {
		t.Fatalf("setupMesh error: %v", err)
	}

	// Create two clients, one connected to each node
	conn1, err := newTestConn(t, 1, ctx, mesh.nodeInfos[0])
	if err != nil {
		t.Fatalf("newTestConn error: %v", err)
	}

	conn2, err := newTestConn(t, 2, ctx, mesh.nodeInfos[1])
	if err != nil {
		t.Fatalf("newTestConn error: %v", err)
	}

	// Connect client 1 to client 2
	err = conn1.conn.ConnectPeer(conn2.peerID)
	if err != nil {
		t.Fatalf("ConnectPeer error: %v", err)
	}

	// Create a test request and expected response
	type testRequest struct {
		Test string `json:"test"`
	}
	testReq, err := msgjson.NewRequest(1, "Test", testRequest{Test: "testingRequest"})
	if err != nil {
		t.Fatalf("NewRequest error: %v", err)
	}
	expResponse := testRequest{Test: "testingResponse"}

	// Send the request and check the response
	wg := sync.WaitGroup{}
	wg.Add(1)
	errChan := make(chan error, 1)
	go func() {
		defer wg.Done()

		var res testRequest
		err = conn1.conn.RequestPeer(conn2.peerID, testReq, &res)
		if err != nil {
			errChan <- fmt.Errorf("RequestPeer error: %w", err)
		}
		if res != expResponse {
			errChan <- fmt.Errorf("expected %+v, got %+v", expResponse, res)
		}
	}()

	// Use client 2 to respond to the request
	select {
	case msg := <-conn2.incomingPeerMsgs:
		msg.respond(expResponse, mj.TEErrNone)
	case <-time.After(time.Second):
		t.Fatalf("timeout waiting for incoming tatanka request peer 2")
	}

	wg.Wait()

	select {
	case err := <-errChan:
		t.Fatal(err)
	default:
	}

	// Simulate client 2 restarting and no longer knowing about the
	// original ephemeral key. Ensure that ErrPeerNeedsReconnect is returned
	// when client 1 makes a request to client 2.
	conn2.conn.peers = make(map[tanka.PeerID]*peer)
	var res testRequest
	err = conn1.conn.RequestPeer(conn2.peerID, testReq, &res)
	if err != ErrPeerNeedsReconnect {
		t.Fatalf("expected ErrPeerNeedsReconnect, got %v", err)
	}
}

// TestFailover tests that if nodes go down, the connection to the mesh
// will switch to other nodes and the subscriptions are restored.
func TestFailover(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	mesh, err := setupMesh(t, ctx, 4)
	if err != nil {
		t.Fatalf("setupMesh error: %v", err)
	}

	mesh.stop(2)
	mesh.stop(3)

	conn1, err := newTestConn(t, 1, ctx, mesh.nodeInfos[0])
	if err != nil {
		t.Fatalf("newTestConn error: %v", err)
	}
	defer conn1.cm.Disconnect()

	conn2, err := newTestConn(t, 2, ctx, mesh.nodeInfos[1])
	if err != nil {
		t.Fatalf("newTestConn error: %v", err)
	}
	defer conn2.cm.Disconnect()

	marketsTopic := tanka.Topic("market")
	dcrBtcSubject := tanka.Subject("dcr/btc")
	conn1.conn.Subscribe(marketsTopic, dcrBtcSubject)
	conn2.conn.Subscribe(marketsTopic, dcrBtcSubject)

	// testBroadcast sends a broadcast on the source client and makes sure that
	// the destination client recieves it.
	testBroadcast := func(bcast *mj.Broadcast, source, dest *tConn) {
		t.Helper()

		var ok bool
		err = source.conn.RequestMesh(mj.MustRequest(mj.RouteBroadcast, bcast), &ok)
		if err != nil {
			t.Fatalf("RequestMesh error: %v", err)
		}

		wg := sync.WaitGroup{}
		wg.Add(1)
		errChan := make(chan error, 1)
		go func() {
			defer wg.Done()
			for {
				select {
				case msg := <-dest.incomingTatankaNotifications:
					if msg.route == mj.RouteBroadcast {
						var broadcast mj.Broadcast
						err := json.Unmarshal(msg.payload, &broadcast)
						if err != nil {
							errChan <- fmt.Errorf("Unmarshal error: %w", err)
							return
						}
						if bytes.Equal(broadcast.Payload, bcast.Payload) {
							return
						}
					}
				case <-time.After(time.Second * 10):
					errChan <- fmt.Errorf("timed out waiting for broadcast")
				}
			}
		}()

		wg.Wait()

		select {
		case err := <-errChan:
			t.Fatal(err)
		default:
		}
	}

	connectedNodes := make(map[tanka.PeerID]map[string]bool)
	updateConnectedNodes := func(conn *tConn) {
		connectedNodes[conn.conn.peerID] = make(map[string]bool)
		hostOnly := func(uri string) string {
			parsedURL, _ := url.Parse(uri)
			return parsedURL.Host
		}
		if conn.conn.primaryNode != nil {
			connectedNodes[conn.conn.peerID][hostOnly(conn.conn.primaryNode.url)] = true
		}
		if conn.conn.secondaryNode != nil {
			connectedNodes[conn.conn.peerID][hostOnly(conn.conn.secondaryNode.url)] = true
		}
	}

	// checkConnected that the connections are connected to the expected nodes
	// on both conn1 and conn2.
	checkConnected := func(expected []string) {
		t.Helper()
		updateConnectedNodes(conn1)
		updateConnectedNodes(conn2)
		for _, node := range expected {
			if !connectedNodes[conn1.conn.peerID][node] {
				t.Fatalf("node %s not connected to conn1", node)
			}
			if !connectedNodes[conn2.conn.peerID][node] {
				t.Fatalf("node %s not connected to conn2", node)
			}
		}
	}

	checkConnected([]string{
		mesh.nodeInfos[0].addr.String(),
		mesh.nodeInfos[1].addr.String(),
	})

	testBroadcast(&mj.Broadcast{
		PeerID:      conn1.peerID,
		Topic:       marketsTopic,
		Subject:     dcrBtcSubject,
		MessageType: mj.MessageTypeTrollBox,
		Payload:     []byte("hello"),
		Stamp:       time.Now(),
	}, conn1, conn2)

	testBroadcast(&mj.Broadcast{
		PeerID:      conn2.peerID,
		Topic:       marketsTopic,
		Subject:     dcrBtcSubject,
		MessageType: mj.MessageTypeTrollBox,
		Payload:     []byte("hello2"),
		Stamp:       time.Now(),
	}, conn2, conn1)

	// Stop the two nodes that the connections were using, and start the two
	// that were previously shut down.
	mesh.stop(0)
	mesh.stop(1)
	mesh.start(2)
	mesh.start(3)

	conn1.conn.maintainMeshConnections()
	conn2.conn.maintainMeshConnections()

	checkConnected([]string{
		mesh.nodeInfos[2].addr.String(),
		mesh.nodeInfos[3].addr.String(),
	})

	testBroadcast(&mj.Broadcast{
		PeerID:      conn1.peerID,
		Topic:       marketsTopic,
		Subject:     dcrBtcSubject,
		MessageType: mj.MessageTypeTrollBox,
		Payload:     []byte("hello3"),
		Stamp:       time.Now(),
	}, conn1, conn2)

	testBroadcast(&mj.Broadcast{
		PeerID:      conn2.peerID,
		Topic:       marketsTopic,
		Subject:     dcrBtcSubject,
		MessageType: mj.MessageTypeTrollBox,
		Payload:     []byte("hello4"),
		Stamp:       time.Now(),
	}, conn2, conn1)
}
