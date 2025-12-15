package xmr

import (
	"context"
	"encoding/hex"
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
	"github.com/bisoncraft/go-monero/rpc"
)

const (
	WalletServerInitializeWait = 5 * time.Second
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
	toolsDir         string
	daemonIsLocal    bool
	daemonAddr       string
	walletRpcProcess *os.Process
	daemon           *rpc.Client
	daemonState      *rpcDaemon
	wallet           *rpc.Client
	walletInfo       *rpcWallet
	syncing          atomic.Bool
}

func newXmrRpc(cfg *asset.WalletConfig, network dex.Network, toolsDir string, logger dex.Logger) (*xmrRpc, error) {
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
		toolsDir:         toolsDir,
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
	xrpc.syncing.Store(true)
	return xrpc, nil
}

func (r *xmrRpc) isSyncing() bool { return r.syncing.Load() }

func (r *xmrRpc) connect(ctx context.Context) (*sync.WaitGroup, error) {
	var wg sync.WaitGroup
	r.ctx = ctx

	_, _, walletSynced, err := r.walletSynced()
	if err != nil {
		return nil, err
	}

	if !walletSynced {
		// see also: wallet syncStatus
		r.syncing.Store(true)
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		r.monitorDaemon(ctx)
	}()

	return &wg, nil
}

func (r *xmrRpc) openWithPW(ctx context.Context, pw []byte) error {
	err := r.probeDaemon(ctx)
	if err != nil {
		return fmt.Errorf("unable to probe daemon: %v", err)
	}

	if !r.walletServerRunning(ctx) {
		err = r.startWalletServer(ctx)
		if err != nil {
			return fmt.Errorf("cannot start the wallet server - %w", err)
		}
		r.log.Debug("xmr started wallet server - waiting for it to initialize")
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(WalletServerInitializeWait): // must wait before open wallet below
			r.log.Trace("xmr waited 5s for the wallet server to fully initialize")
		}
	} else {
		r.log.Warn("xmr re-entered open with pw")
	}
	if err := r.openWallet(ctx, hex.EncodeToString(pw)); err != nil {
		return err
	}
	r.walletInfo.primaryAddress, _ = r.getPrimaryAddress()
	return nil
}

// monitorDaemon monitors blocks and peer count
func (r *xmrRpc) monitorDaemon(ctx context.Context) {
	ticker := time.NewTicker(BlockTickerInterval) // 2m blocks
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			if err := r.checkDaemon(ctx); err != nil {
				r.log.Errorf("error checking xmr daemon: %v", err)
			}
		case <-ctx.Done():
			return
		}
	}
}

func (r *xmrRpc) checkDaemon(ctx context.Context) error {
	info, err := r.getInfo(ctx)
	if err != nil {
		return fmt.Errorf("DaemonGetInfo: %v", err)
	}
	if info.Status != "OK" {
		return fmt.Errorf("DaemonGetInfo: bad status: %s - expected OK", info.Status)
	}
	r.daemonState.Lock()
	defer r.daemonState.Unlock()
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
	return nil
}
