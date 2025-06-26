package xmr

import (
	"context"
	"errors"
	"fmt"
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
	BlockTickerInterval = 15 * time.Second // 2m blocks
	AutoRefreshInterval = 30               // 30s
)

var errBadDaemonStatus = errors.New("bad daemon status")

type rpcDaemon struct {
	busySyncing   bool
	synchronized  bool
	untrusted     bool
	restricted    bool
	connectHeight uint64
	targetHeight  uint64
	height        uint64
	blockHash     string
	numPeers      uint32
}

type rpcWallet struct {
	isOpen        bool
	birthdayBlock uint64
}

type xmrRpc struct {
	ctx              context.Context
	net              dex.Network
	log              dex.Logger
	emit             *asset.WalletEmitter
	peersChange      func(uint32, error)
	dataDir          string
	cliToolsDir      string
	serverIsLocal    bool
	serverAddr       string
	walletRpcProcess *os.Process
	daemon           *rpc.Client
	daemonState      *rpcDaemon
	daemonStateMtx   sync.RWMutex
	wallet           *rpc.Client
	walletState      *rpcWallet
	connected        atomic.Bool
	rescanning       atomic.Bool
}

func newXmrRpc(cfg *asset.WalletConfig, settings *xmrConfigSettings, network dex.Network,
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
	var svrIsLocal bool
	var svrAddr string
	switch network {
	case dex.Mainnet:
		svrAddr = settings.ServerAddr
	case dex.Testnet:
		svrAddr = "http://node3.monerodevs.org:38089" // stagenet remote
		// svrAddr = "http://localhost:38081"         // stagenet local
	case dex.Simnet:
		svrAddr = "http://localhost:18081"
	}
	svrIsLocal, err := isLocalAddress(svrAddr)
	if err != nil {
		return nil, err
	}

	xrpc := &xmrRpc{
		ctx:              nil, // until connect
		net:              network,
		log:              logger.SubLogger("XRPC"),
		emit:             cfg.Emit,
		peersChange:      cfg.PeersChange,
		dataDir:          cfg.DataDir,
		cliToolsDir:      settings.CliToolsDir,
		serverIsLocal:    svrIsLocal,
		serverAddr:       svrAddr,
		walletRpcProcess: nil,
		daemon:           nil,
		daemonState: &rpcDaemon{
			busySyncing:   true,
			synchronized:  false,
			untrusted:     true,
			restricted:    true,
			connectHeight: 0,
			targetHeight:  0,
			height:        0,
			blockHash:     "",
			numPeers:      0,
		},
		wallet: nil,
		walletState: &rpcWallet{
			isOpen:        false,
			birthdayBlock: 0,
		},
	}
	xrpc.connected.Store(false)
	xrpc.rescanning.Store(false)
	return xrpc, nil
}

func (r *xmrRpc) connect(ctx context.Context) (*sync.WaitGroup, error) {
	r.ctx = ctx

	r.log.Trace("connect xmr")

	err := r.startWalletServer()
	if err != nil {
		return nil, err
	}

	// take a short rest here before accessing wallet server .. was getting
	// 'connection refused' The log showing a lot of activity on start up.
	r.log.Trace("sleeping 3s after starting wallet server")
	time.Sleep(3 * time.Second)

	if r.keysFileMissing() {
		r.log.Debug("wallet keys file not found, creating new wallet")
		err = r.createWallet()
		if err != nil {
			r.log.Errorf("create_wallet - %v", err)
			r.stopWalletServer()
			return nil, fmt.Errorf(": %w - daemon user/pass correct?", err)
		}
		r.log.Infof("new monero wallet created on %s", r.net.String())
		count, err := r.getBlockHeightFast()
		r.log.Tracef("getBlockHeightFast: bday height: %d, error: %v", count, err)
		if err != nil { // other options also
			return nil, err
		}
		r.walletState.birthdayBlock = count // not yet used
	}

	// wallet exists

	err = r.openWallet()
	if err != nil {
		r.log.Errorf("cannot open wallet - %v", err)
		r.stopWalletServer()
		return nil, err
	}
	r.log.Trace("opened xmr wallet")

	err = r.setAutoRefresh()
	if err != nil {
		r.log.Errorf("cannot set auto refresh - %v", err)
		r.closeWallet()
		r.stopWalletServer()
		return nil, err
	}

	r.walletState.isOpen = true

	feeRate, _ := r.getFeeRate()
	r.log.Debugf("feeRate: %d", feeRate)

	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		r.monitorDaemon(ctx)
		r.shutdown()
	}()

	r.connected.Store(true)
	return &wg, nil
}

func (r *xmrRpc) shutdown() {
	r.closeWallet()
	r.stopWalletServer()
}

// monitor blocks and peer count
func (r *xmrRpc) monitorDaemon(ctx context.Context) {
	ticker := time.NewTicker(BlockTickerInterval) // 2m blocks
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			r.checkDaemon(ctx)
		case <-ctx.Done():
			return
		}
		if ctx.Err() != nil {
			return
		}
	}
}

func (r *xmrRpc) checkDaemon(ctx context.Context) {
	giResp, err := r.daemon.DaemonGetInfo(ctx)
	if err != nil {
		r.log.Errorf("DaemonGetInfo: %v", err)
		return
	}
	if giResp.Status != "OK" {
		r.log.Warnf("DaemonGetInfo: bad status: %s", giResp.Status)
		return
	}
	r.daemonStateMtx.Lock()
	defer r.daemonStateMtx.Unlock()
	// r.log.Tracef("daemon %s -- synced: %v, busy syncing: %v, restricted: %v, untrusted: %v", r.serverAddr, giResp.Sychronized, giResp.BusySyncing, giResp.Restricted, giResp.Untrusted)
	r.daemonState.synchronized = giResp.Sychronized
	r.daemonState.busySyncing = giResp.BusySyncing
	// check height change
	heightNow := giResp.Height
	blockHashNow := giResp.TopBlockHash
	if heightNow > r.daemonState.height {
		r.log.Tracef("daemon %s -- tip change: %d (%s) => %d (%s)", r.serverAddr, r.daemonState.height, r.daemonState.blockHash, heightNow, blockHashNow)
		r.emit.TipChange(heightNow)
		r.daemonState.height = heightNow
		r.daemonState.blockHash = blockHashNow
	}
	// check peer count
	peersNow := uint32(giResp.OutgoingConnectionsCount + giResp.IncomingConnectionsCount)
	// remote nodes may (or may not) hide this info
	if !r.serverIsLocal && peersNow == 0 {
		peersNow = 1
	}
	if peersNow != r.daemonState.numPeers {
		r.peersChange(peersNow, nil)
		r.daemonState.numPeers = peersNow
	}
}
