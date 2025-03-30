//go:build live

package conn

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/user"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/encode"
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

type tMesh struct {
	tatankas []*tTatanka
}

type tConn struct {
	conn                         *MeshConn
	cm                           *dex.ConnectionMaster
	priv                         *secp256k1.PrivateKey
	peerID                       tanka.PeerID
	incomingTatankaRequests      chan *msgjson.Message
	incomingTatankaNotifications chan *msgjson.Message
	incomingPeerMsgs             chan *IncomingTankagram
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

// setupMesh creates a mesh of numNode tatankas, with each node having all
// other nodes in the mesh as its whitelist.
func setupMesh(t *testing.T, ctx context.Context, numNodes int) ([]*tTatanka, error) {
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

	return tatankas, nil
}

// newTestConn creates a new client connection to a tatanka node.
func newTestConn(t *testing.T, i int, ctx context.Context, tatankaNodeInfo *tTatankaNodeInfo) (*tConn, error) {
	priv, peerID := genKeyPair()

	// Setup channels for incoming messages
	incomingTatankaRequests := make(chan *msgjson.Message, 10)
	incomingTatankaNotifications := make(chan *msgjson.Message, 10)
	incomingPeerMsgs := make(chan *IncomingTankagram, 10)

	// Configure connection
	cfg := &Config{
		EntryNode: &TatankaCredentials{
			PeerID: tatankaNodeInfo.peerID,
			Addr:   tatankaNodeInfo.addr.String(),
			NoTLS:  true,
		},
		Handlers: &MessageHandlers{
			HandleTatankaRequest: func(_ tanka.PeerID, msg *msgjson.Message) *msgjson.Error {
				incomingTatankaRequests <- msg
				return nil
			},
			HandleTatankaNotification: func(_ tanka.PeerID, msg *msgjson.Message) {
				incomingTatankaNotifications <- msg
			},
			HandlePeerMessage: func(_ tanka.PeerID, msg *IncomingTankagram) *msgjson.Error {
				incomingPeerMsgs <- msg
				return nil
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

	// Post a DCR bond with 1 year expiration
	const bondExpirationDuration = time.Hour * 24 * 365
	postBondReq := mj.MustRequest(mj.RoutePostBond, []*tanka.Bond{&tanka.Bond{
		PeerID:     peerID,
		AssetID:    42,
		CoinID:     encode.RandomBytes(32),
		Strength:   1,
		Expiration: time.Now().Add(bondExpirationDuration),
	}})
	err := conn.RequestMesh(postBondReq, nil)
	if err != nil {
		return nil, fmt.Errorf("PostBond error: %w", err)
	}

	err = conn.Auth(tatankaNodeInfo.peerID)
	if err != nil {
		return nil, fmt.Errorf("Auth error: %w", err)
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

func TestConnectPeer(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Create a two node mesh
	mesh, err := setupMesh(t, ctx, 2)
	if err != nil {
		t.Fatalf("setupMesh error: %v", err)
	}

	// Create two clients, one connected to each node
	conn1, err := newTestConn(t, 1, ctx, mesh[0].nodeInfo)
	if err != nil {
		t.Fatalf("newTestConn error: %v", err)
	}
	conn2, err := newTestConn(t, 2, ctx, mesh[1].nodeInfo)
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
	go func() {
		defer wg.Done()

		var res testRequest
		err = conn1.conn.RequestPeer(conn2.peerID, testReq, &res)
		if err != nil {
			t.Fatalf("RequestPeer error: %v", err)
		}
		if res != expResponse {
			t.Fatalf("expected %+v, got %+v", expResponse, res)
		}
	}()

	// Use client 2 to respond to the request
	select {
	case msg := <-conn2.incomingPeerMsgs:
		err = msg.Respond(expResponse)
		if err != nil {
			t.Fatalf("Respond error: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatalf("timeout waiting for incoming tatanka request peer 2")
	}

	wg.Wait()

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
