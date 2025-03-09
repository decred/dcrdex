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

func newBootNode(addr string, peerID []byte) tatanka.BootNode {
	tcpCfg, _ := json.Marshal(&tcp.RemoteNodeConfig{
		URL: "ws://" + addr,
		// Cert: ,
	})
	return tatanka.BootNode{
		Protocol: "ws",
		Config:   tcpCfg,
		PeerID:   peerID,
	}
}

func findOpenAddrs(n int) ([]net.Addr, error) {
	addrs := make([]net.Addr, 0, n)
	for i := 0; i < n; i++ {
		addr, err := net.ResolveTCPAddr("tcp", "localhost:0")
		if err != nil {
			return nil, err
		}

		l, err := net.ListenTCP("tcp", addr)
		if err != nil {
			return nil, err
		}
		defer l.Close()
		addrs = append(addrs, l.Addr())
	}

	return addrs, nil
}

type testTatanka struct {
	tatanka *tatanka.Tatanka
	cm      *dex.ConnectionMaster
	privKey *secp256k1.PrivateKey
	peerID  tanka.PeerID
	addr    string
}

func genKey() (*secp256k1.PrivateKey, tanka.PeerID) {
	priv, _ := secp256k1.GeneratePrivateKey()
	var peerID tanka.PeerID
	copy(peerID[:], priv.PubKey().SerializeCompressed())
	return priv, peerID
}

func runServer(t *testing.T, ctx context.Context, addr, peerAddr net.Addr, disableMessariFiatRateSource bool) (*testTatanka, error) {
	priv, peerID := genKey()

	dir := t.TempDir()
	os.MkdirAll(dir, 0755)
	os.WriteFile(filepath.Join(dir, "priv.key"), priv.Serialize(), 0644)

	n := newBootNode(peerAddr.String(), peerID[:])
	log := logMaker.Logger(fmt.Sprintf("SRV[%s]", addr))
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
			ListenAddrs: []string{addr.String()},
			NoTLS:       true,
		},
		ConfigPath: cfgPath,
		WhiteList:  []tatanka.BootNode{n},
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

	return &testTatanka{
		tatanka: tatanka,
		addr:    addr.String(),
		cm:      cm,
		privKey: priv,
		peerID:  peerID,
	}, nil
}

type testMesh struct {
	tatankas []*testTatanka
}

func setupMesh(t *testing.T, ctx context.Context, numNodes int) (*testMesh, error) {
	addrs, err := findOpenAddrs(numNodes)
	if err != nil {
		return nil, fmt.Errorf("findOpenAddrs error: %w", err)
	}

	tatankas := make([]*testTatanka, 0, numNodes)
	for i := 0; i < numNodes; i++ {
		var peerAddr net.Addr
		if i == numNodes-1 {
			peerAddr = addrs[0]
		} else {
			peerAddr = addrs[i+1]
		}
		tt, err := runServer(t, ctx, addrs[i], peerAddr, true)
		if err != nil {
			return nil, fmt.Errorf("runServer error: %w", err)
		}
		tatankas = append(tatankas, tt)
	}

	return &testMesh{
		tatankas: tatankas,
	}, nil
}

func mustEncode(thing interface{}) json.RawMessage {
	b, err := json.Marshal(thing)
	if err != nil {
		panic("mustEncode: " + err.Error())
	}
	return b
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

type testConn struct {
	conn                         *MeshConn
	cm                           *dex.ConnectionMaster
	priv                         *secp256k1.PrivateKey
	peerID                       tanka.PeerID
	incomingTatankaRequests      chan *msgjson.Message
	incomingTatankaNotifications chan *msgjson.Message
	incomingPeerMsgs             chan *IncomingTankagram
}

func newTestConn(t *testing.T, i int, ctx context.Context, tatankaAddr string, tatankaPeerID tanka.PeerID) (*testConn, error) {
	priv, peerID := genKey()

	incomingTatankaRequests := make(chan *msgjson.Message, 10)
	incomingTatankaNotifications := make(chan *msgjson.Message, 10)
	incomingPeerMsgs := make(chan *IncomingTankagram, 10)

	cfg := &Config{
		EntryNode: &TatankaCredentials{
			PeerID: tatankaPeerID,
			Addr:   tatankaAddr,
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

	conn := New(cfg)
	cm := dex.NewConnectionMaster(conn)
	if err := cm.ConnectOnce(ctx); err != nil {
		return nil, fmt.Errorf("ConnectOnce error: %w", err)
	}

	postBondReq := mj.MustRequest(mj.RoutePostBond, []*tanka.Bond{&tanka.Bond{
		PeerID:     peerID,
		AssetID:    42,
		CoinID:     encode.RandomBytes(32),
		Strength:   1,
		Expiration: time.Now().Add(time.Hour * 24 * 365),
	}})
	err := conn.RequestMesh(postBondReq, nil)
	if err != nil {
		return nil, fmt.Errorf("PostBond error: %w", err)
	}

	err = conn.Auth(tatankaPeerID)
	if err != nil {
		return nil, fmt.Errorf("Auth error: %w", err)
	}

	return &testConn{
		conn:                         conn,
		cm:                           cm,
		priv:                         priv,
		peerID:                       peerID,
		incomingTatankaRequests:      incomingTatankaRequests,
		incomingTatankaNotifications: incomingTatankaNotifications,
		incomingPeerMsgs:             incomingPeerMsgs,
	}, nil
}

func TestConnectPeer(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	testMesh, err := setupMesh(t, ctx, 2)
	if err != nil {
		t.Fatalf("setupMesh error: %v", err)
	}

	conn1, err := newTestConn(t, 1, ctx, testMesh.tatankas[0].addr, testMesh.tatankas[0].peerID)
	if err != nil {
		t.Fatalf("newTestConn error: %v", err)
	}

	conn2, err := newTestConn(t, 2, ctx, testMesh.tatankas[0].addr, testMesh.tatankas[0].peerID)
	if err != nil {
		t.Fatalf("newTestConn error: %v", err)
	}

	err = conn1.conn.ConnectPeer(conn2.peerID)
	if err != nil {
		t.Fatalf("ConnectPeer error: %v", err)
	}

	type testRequest struct {
		Test string `json:"test"`
	}

	testReq, err := msgjson.NewRequest(1, "Test", testRequest{Test: "testingRequest"})
	if err != nil {
		t.Fatalf("NewRequest error: %v", err)
	}

	expResponse := testRequest{Test: "testingResponse"}

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
}
