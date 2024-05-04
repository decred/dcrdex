package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/signal"
	"os/user"
	"path/filepath"
	"sync"
	"time"

	"decred.org/dcrdex/dex"
	dexbtc "decred.org/dcrdex/dex/networks/btc"
	"decred.org/dcrdex/server/comms"
	"decred.org/dcrdex/tatanka"
	"decred.org/dcrdex/tatanka/chain/utxo"
	tankaclient "decred.org/dcrdex/tatanka/client"
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

func main() {
	if err := mainErr(); err != nil {
		fmt.Fprintln(os.Stderr, err)
	}
}

func mainErr() (err error) {
	var wg sync.WaitGroup
	defer func() {
		wg.Wait()
	}()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	killChan := make(chan os.Signal, 1)
	signal.Notify(killChan, os.Interrupt)
	go func() {
		<-killChan
		fmt.Println("Shutting down...")
		cancel()
	}()

	tmpDir, _ := os.MkdirTemp("", "")
	defer os.RemoveAll(tmpDir)

	logMaker, err = dex.NewLoggerMaker(os.Stdout, dex.LevelTrace.String())
	if err != nil {
		return fmt.Errorf("failed to create custom logger: %w", err)
	}

	comms.UseLogger(logMaker.NewLogger("COMMS", dex.LevelTrace))

	addrs, err := findOpenAddrs(2)
	if err != nil {
		return fmt.Errorf("findOpenAddrs error: %w", err)
	}

	genKey := func(dir string) (*secp256k1.PrivateKey, tanka.PeerID) {
		priv, _ := secp256k1.GeneratePrivateKey()
		var peerID tanka.PeerID
		copy(peerID[:], priv.PubKey().SerializeCompressed())
		os.MkdirAll(dir, 0755)
		os.WriteFile(filepath.Join(dir, "priv.key"), priv.Serialize(), 0644)
		return priv, peerID
	}
	dir0 := filepath.Join(tmpDir, "tatanka1")
	priv0, pid0 := genKey(dir0)
	dir1 := filepath.Join(tmpDir, "tatanka2")
	priv1, pid1 := genKey(dir1)

	wg.Add(1)
	go func() {
		defer wg.Done()
		defer cancel()

		runServer(ctx, dir0, addrs[0], addrs[1], priv1.PubKey().SerializeCompressed(), true)
	}()

	time.Sleep(time.Second)

	wg.Add(1)
	go func() {
		defer wg.Done()
		defer cancel()

		runServer(ctx, dir1, addrs[1], addrs[0], priv0.PubKey().SerializeCompressed(), true)
	}()

	time.Sleep(time.Second)

	cl1, err := newClient(ctx, addrs[0].String(), pid0, 0)
	if err != nil {
		return fmt.Errorf("error making first connected client: %v", err)
	}

	cl2, err := newClient(ctx, addrs[1].String(), pid1, 1)
	if err != nil {
		return fmt.Errorf("error making second connected client: %v", err)
	}

	// cm.Disconnect()

	if err := cl1.SubscribeMarket(42, 0); err != nil {
		return fmt.Errorf("SubscribeMarket error: %v", err)
	}

	// Miss the relayed new subscription broadcast.
	time.Sleep(time.Second)

	if err := cl2.SubscribeMarket(42, 0); err != nil {
		return fmt.Errorf("SubscribeMarket error: %v", err)
	}

	// Client 1 should receive a notification.
	select {
	case bcastI := <-cl1.Next():
		bcast, is := bcastI.(mj.Broadcast)
		if !is {
			return fmt.Errorf("expected new subscription Broadcast, got %T", bcastI)
		}
		if bcast.MessageType != mj.MessageTypeNewSubscriber {
			return fmt.Errorf("expected new subscription message type, got %s", bcast.MessageType)
		}
		fmt.Printf("Client 1's bounced broadcast received: %+v \n", bcastI)
	case <-time.After(time.Second):
		return errors.New("timed out waiting for client 1 to receive new subscriber notification")
	}

	mktName, err := dex.MarketName(42, 0)
	if err != nil {
		return fmt.Errorf("error constructing market name: %w", err)
	}

	if err := cl1.Broadcast(mj.TopicMarket, tanka.Subject(mktName), mj.MessageTypeTrollBox, &mj.Troll{Msg: "trollin'"}); err != nil {
		return fmt.Errorf("broadcast error: %w", err)
	}

	select {
	case bcastI := <-cl1.Next():
		bcast, is := bcastI.(mj.Broadcast)
		if !is {
			return fmt.Errorf("client 1 expected trollbox Broadcast bounceback, got %T", bcastI)
		}
		if bcast.MessageType != mj.MessageTypeTrollBox {
			return fmt.Errorf("client 1 expected trollbox message type for bounced message, got %s", bcast.MessageType)
		}
		fmt.Printf("Client 1's bounced broadcast received: %+v \n", bcastI)
	case <-time.After(time.Second):
		return errors.New("timed out waiting for client 1 broadcast to bounce back")
	}

	select {
	case bcastI := <-cl2.Next():
		bcast, is := bcastI.(mj.Broadcast)
		if !is {
			return fmt.Errorf("client 2 expected trollbox Broadcast, got %T", bcastI)
		}
		if bcast.MessageType != mj.MessageTypeTrollBox {
			return fmt.Errorf("client 2 expected trollbox message type, got %s", bcast.MessageType)
		}
		fmt.Printf("Client 2's received the broadcast: %+v \n", bcastI)
	case <-time.After(time.Second):
		return errors.New("timed out waiting for client 2 to receive broadcast from client 1")
	}

	// Connect clients
	if _, err := cl1.ConnectPeer(cl2.ID()); err != nil {
		return fmt.Errorf("error connecting peers: %w", err)
	}

	select {
	case newPeerI := <-cl2.Next():
		if _, is := newPeerI.(*tankaclient.IncomingPeerConnect); !is {
			return fmt.Errorf("expected IncomingPeerConnect, got %T", newPeerI)
		}
		fmt.Printf("Client 2's received the new peer notification: %+v \n", newPeerI)
	case <-time.After(time.Second):
		return errors.New("timed out waiting for client 2 to receive broadcast from client 1")
	}

	// Send a tankagram
	const testRoute = "test_route"
	respC := make(chan interface{})
	go func() {
		msg := mj.MustRequest(testRoute, true)
		r, resB, err := cl1.SendTankagram(cl2.ID(), msg)
		if r.Result != mj.TRTTransmitted {
			respC <- fmt.Errorf("not transmitted. %q", r.Result)
			return
		}
		if err != nil {
			respC <- err
			return
		}
		respC <- resB
	}()

	select {
	case gramI := <-cl2.Next():
		gram, is := gramI.(*tankaclient.IncomingTankagram)
		if !is {
			return fmt.Errorf("expected IncomingTankagram, got a %T", gramI)
		}
		if gram.Msg.Route != testRoute || !bytes.Equal(gram.Msg.Payload, []byte("true")) {
			return fmt.Errorf("tankagram ain't right %s, %s", gram.Msg.Route, string(gram.Msg.Payload))
		}
		gram.Respond("ok")
	case <-time.After(time.Second):
		return errors.New("timed out waiting for tankagram from client 1")
	}

	select {
	case respI := <-respC:
		switch resp := respI.(type) {
		case error:
			return fmt.Errorf("error sending tankagram: %v", resp)
		case json.RawMessage:
			var s string
			if err := json.Unmarshal(resp, &s); err != nil {
				return fmt.Errorf("tankagram response didn't unmarshal: %w", err)
			}
			if s != "ok" {
				return fmt.Errorf("wrong tankagram response %q", s)
			}
		}
	case <-time.After(time.Second):
		return errors.New("timed out waiting for SendTankagram to return")
	}

	// Fiat rate live test requires internet connection.
	fmt.Println("Testing fiat rates...")
	if err := cl1.SubscribeToFiatRates(); err != nil {
		return err
	}

	// Wait for rate message.
	<-cl1.Next()

	want := len(chains)
	got := 0
	for _, c := range chains {
		assetID, _ := dex.BipSymbolID(c.Symbol)
		fiatRate := cl1.FiatRate(assetID) // check if rate has been cached.
		if fiatRate != 0 {
			got++
			fmt.Printf("\nRate found for %s\n", dex.BipIDSymbol(assetID))
		}
	}

	if got < want {
		fmt.Printf("\nGot fiat rates for %d out of %d assets\n", got, want)
	}

	fmt.Println("!!!!!!!! Test Success !!!!!!!!")

	cancel()

	wg.Wait()

	cl1.cm.Wait()
	cl2.cm.Wait()

	return nil
}

type connectedClient struct {
	*tankaclient.TankaClient
	cm *dex.ConnectionMaster
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

func runServer(ctx context.Context, dir string, addr, peerAddr net.Addr, peerID []byte, disableMessariFiatRateSource bool) {
	n := newBootNode(peerAddr.String(), peerID)

	log := logMaker.Logger(fmt.Sprintf("SRV[%s]", addr))

	os.MkdirAll(dir, 0755)

	ttCfg := &tatanka.ConfigFile{Chains: chains}
	rawCfg, _ := json.Marshal(ttCfg)
	cfgPath := filepath.Join(dir, "config.json")
	if err := os.WriteFile(cfgPath, rawCfg, 0644); err != nil {
		log.Errorf("WriteFile error: %v", err)
		return
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

	t, err := tatanka.New(cfg)
	if err != nil {
		log.Errorf("error creating Tatanka node: %v", err)
		return
	}

	cm := dex.NewConnectionMaster(t)
	if err := cm.ConnectOnce(ctx); err != nil {
		log.Errorf("ConnectOnce error: %v", err)
		return
	}

	cm.Wait()
}

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

func newClient(ctx context.Context, addr string, peerID tanka.PeerID, i int) (*connectedClient, error) {
	log := logMaker.NewLogger(fmt.Sprintf("tCL[%d:%s]", i, addr), dex.LevelTrace)
	priv, _ := secp256k1.GeneratePrivateKey()

	tc := tankaclient.New(&tankaclient.Config{
		Logger:     log.SubLogger("tTC"),
		PrivateKey: priv,
	})

	cm := dex.NewConnectionMaster(tc)
	if err := cm.ConnectOnce(ctx); err != nil {
		return nil, fmt.Errorf("ConnectOnce error: %w", err)
	}

	if err := tc.AddTatankaNode(ctx, peerID, "ws://"+addr, nil); err != nil {
		cm.Disconnect()
		return nil, fmt.Errorf("error adding server %q", addr)
	}

	if err := tc.PostBond(&tanka.Bond{
		PeerID:     tc.ID(),
		AssetID:    42,
		CoinID:     nil,
		Strength:   1,
		Expiration: time.Now().Add(time.Hour * 24 * 365),
	}); err != nil {
		cm.Disconnect()
		return nil, fmt.Errorf("PostBond error: %v", err)
	}

	if err := tc.Auth(peerID); err != nil {
		cm.Disconnect()
		return nil, fmt.Errorf("auth error: %v", err)
	}

	return &connectedClient{
		TankaClient: tc,
		cm:          cm,
	}, nil
}

func mustEncode(thing interface{}) json.RawMessage {
	b, err := json.Marshal(thing)
	if err != nil {
		panic("mustEncode: " + err.Error())
	}
	return b
}
