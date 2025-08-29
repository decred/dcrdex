package xmr

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/dex"
	"github.com/dev-warrior777/go-monero/rpc"
)

const (
	WalletServerInitializeWait = 3 * time.Second
	BlockTickerInterval        = 15 * time.Second // ~2m blocks
)

type rpcDaemon struct {
	sync.RWMutex
	synchronized bool
	untrusted    bool
	restricted   bool
	height       uint64
	blockHash    string
	numPeers     uint32
}

type rpcWallet struct {
	sync.Mutex
	primaryAddress string
	birthdayBlock  uint64
}

type xmrRpc struct {
	ctx              context.Context
	net              dex.Network
	log              dex.Logger
	synclog          dex.Logger
	emit             *asset.WalletEmitter
	peersChange      func(uint32, error)
	dataDir          string
	cliToolsDir      string
	daemonIsLocal    bool
	daemonAddr       string
	walletRpcProcess *os.Process
	daemon           *rpc.Client
	daemonState      *rpcDaemon
	wallet           *rpc.Client
	walletInfo       *rpcWallet
	sync             atomic.Bool
}

func newXmrRpc(cfg *asset.WalletConfig, settings *configSettings, network dex.Network,
	logger dex.Logger) (*xmrRpc, error) {
	isLocalAddress := func(address string) (bool, error) {
		parsedURL, err := url.Parse(address)
		if err != nil {
			return false, err
		}
		host := parsedURL.Hostname()
		if host == "localhost" || strings.HasPrefix(host, "127.0.0.") || strings.HasPrefix(host, "::1") {
			return true, nil
		}
		return false, nil
	}
	daemons := getTrustedDaemons(network, false, cfg.DataDir) // non-tls
	daemonAddr := daemons[0]
	daemonIsLocal, err := isLocalAddress(daemonAddr)
	if err != nil {
		return nil, err
	}
	// daemon rpc client
	daemonClient := rpc.New(rpc.Config{
		Address: daemonAddr + Json2query,
		Client:  &http.Client{},
	})
	// wallet rpc client; always local
	var walletClient *rpc.Client
	switch network {
	case dex.Mainnet:
		walletClient = rpc.New(rpc.Config{
			Address: HttpLocalhost + MainnetWalletServerRpcPort + Json2query,
			Client:  &http.Client{},
		})
	case dex.Testnet: // stagenet
		walletClient = rpc.New(rpc.Config{
			Address: HttpLocalhost + StagenetWalletServerRpcPort + Json2query,
			Client:  &http.Client{},
		})
	case dex.Simnet:
		walletClient = rpc.New(rpc.Config{
			Address: HttpLocalhost + getRegtestWalletServerRpcPort(cfg.DataDir) + Json2query,
			Client:  &http.Client{},
		})
	}

	xrpc := &xmrRpc{
		ctx:              nil, // until connect
		net:              network,
		log:              logger.SubLogger("XRPC"),
		synclog:          logger.SubLogger("SYNC"),
		emit:             cfg.Emit,
		peersChange:      cfg.PeersChange,
		dataDir:          cfg.DataDir,
		cliToolsDir:      settings.CliToolsDir,
		daemonAddr:       daemonAddr,
		daemonIsLocal:    daemonIsLocal,
		walletRpcProcess: nil,
		daemon:           daemonClient,
		daemonState: &rpcDaemon{
			synchronized: false,
			untrusted:    true,
			restricted:   true,
			height:       0,
			blockHash:    "",
			numPeers:     0,
		},
		wallet: walletClient,
		walletInfo: &rpcWallet{
			primaryAddress: "",
			birthdayBlock:  0,
		},
	}
	xrpc.sync.Store(false)
	return xrpc, nil
}

func (r *xmrRpc) syncing() bool { return r.sync.Load() }

func (r *xmrRpc) connect(ctx context.Context) (*sync.WaitGroup, error) {
	var wg sync.WaitGroup
	r.ctx = ctx

	r.log.Trace("connect xmr")

	err := r.probeDaemon()
	if err != nil {
		r.log.Errorf("probeDaemon - %v", err)
		return nil, err
	}

	if !r.walletServerRunning() {
		err = r.startWalletServer(ctx)
		if err != nil {
			return nil, fmt.Errorf("cannot start the wallet server - %w", err)
		}
		time.Sleep(WalletServerInitializeWait)
	} else {
		r.log.Warn("re-entered connect")
	}

	err = r.doOpenWallet(ctx)
	if err != nil {
		return nil, err
	}

	_, _, walletSynced, err := r.walletSynced()
	if err != nil {
		r.wallet.CloseWallet(ctx)
		return nil, err
	}

	if !walletSynced {
		// see also: wallet syncStatus
		r.sync.Store(true)
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		r.monitorDaemon(ctx)
	}()

	return &wg, nil
}

func (r *xmrRpc) doOpenWallet(ctx context.Context) error {
	pw, err := r.getKeystorePw()
	if err != nil {
		return err
	}
	err = r.openWallet(ctx, pw)
	if err != nil {
		r.log.Errorf("cannot open wallet - %w", err)
		return err
	}
	r.log.Debug("opened xmr wallet")
	r.walletInfo.primaryAddress, _ = r.getPrimaryAddress()
	r.log.Infof("Primary Address: %s", r.walletInfo.primaryAddress)
	return nil
}

// monitorDaemon monitors blocks and peer count
func (r *xmrRpc) monitorDaemon(ctx context.Context) {
	ticker := time.NewTicker(BlockTickerInterval) // 2m blocks
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			r.checkDaemon()
		case <-ctx.Done():
			r.log.Trace("Ctx Done in monitorDaemon")
			return
		}
	}
}

func (r *xmrRpc) checkDaemon() {
	info, err := r.getInfo()
	if err != nil {
		r.log.Errorf("DaemonGetInfo: %v", err)
		return
	}
	if info.Status != "OK" {
		r.log.Warnf("DaemonGetInfo: bad status: %s - expected OK", info.Status)
		return
	}
	r.daemonState.Lock()
	defer r.daemonState.Unlock()
	// r.log.Tracef("daemon %s --  height: %d, synced: %v, restricted: %v, untrusted: %v", r.daemonAddr, info.Height, info.Sychronized, info.Restricted, info.Untrusted)
	r.daemonState.synchronized = info.Sychronized
	// check height change
	heightNow := info.Height
	blockHashNow := info.TopBlockHash
	if heightNow > r.daemonState.height {
		r.log.Tracef("daemon %s -- tip change: %d (%s) => %d (%s)", r.daemonAddr, r.daemonState.height, r.daemonState.blockHash, heightNow, blockHashNow)
		r.emit.TipChange(heightNow)
		r.daemonState.height = heightNow
		r.daemonState.blockHash = blockHashNow
	}

	// check peer count
	peersNow := uint32(info.OutgoingConnectionsCount + info.IncomingConnectionsCount)
	// remote restricted nodes hide this info
	if !r.daemonIsLocal && peersNow == 0 {
		peersNow = 1
	}
	// simnet daemon never has any peers
	if r.net == dex.Simnet {
		peersNow = 1
	}
	if peersNow != r.daemonState.numPeers {
		r.peersChange(peersNow, nil)
		r.daemonState.numPeers = peersNow
	}
}

func (r *xmrRpc) getKeystorePw() (string, error) {
	k := new(keystore)
	p, err := k.get(r.net)
	if err != nil {
		return "", fmt.Errorf("keystore error %v", err)
	}
	return p, nil
}

func (r *xmrRpc) unlockApp(spw string) error {
	p, err := r.getKeystorePw()
	if err != nil {
		return fmt.Errorf("keystore error - %v", err)
	}
	if p != spw {
		return fmt.Errorf("incorrect password")
	}
	if r.syncing() {
		return fmt.Errorf("cannot unlock wallet function while syncing")
	}
	return nil
}
