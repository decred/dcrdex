package xmr

import (
	"errors"
	"fmt"
	"net/http"
	"os/exec"
	"path"

	"decred.org/dcrdex/dex"
	"github.com/dev-warrior777/go-monero/rpc"
)

const (
	WalletServerRpcName = "monero-wallet-rpc"
	WalletLogfileName   = WalletServerRpcName + ".log"
	WalletLogLevel      = "1"
)

const (
	HttpLocalhost               = "http://127.0.0.1:"
	Json2query                  = "/json_rpc"
	MainnetWalletServerRpcPort  = "18083"
	StagenetWalletServerRpcPort = "38083"
	RegtestWalletServerRpcPort  = MainnetWalletServerRpcPort
)

const (
	DaemonAddressParam                = "--daemon-address="
	RpcBindPortParam                  = "--rpc-bind-port="
	StagenetParam                     = "--stagenet"
	TrustedDaemonParam                = "--trusted-daemon"
	WalletDirParam                    = "--wallet-dir="
	DisableRpcLoginParam              = "--disable-rpc-login"
	DaemonLoginParam                  = "--daemon-login=" // currently unused
	AllowMismatchedDaemonVersionParam = "--allow-mismatched-daemon-version"
	LogFileParam                      = "--log-file="
	LogLevelParam                     = "--log-level="
)

func (r *xmrRpc) startWalletServer() error {
	r.log.Trace("startWalletServer")
	// first check out the daemon which should be up and running remotely or up
	// and running on localhost if it is the end user's personal monerod server.
	daemonAddress := r.serverAddr + Json2query
	daemon := rpc.New(rpc.Config{
		Address: daemonAddress,
		Client:  &http.Client{ /*default no auth HTTP client*/ },
	})
	giResp, err := daemon.DaemonGetInfo(r.ctx)
	if err != nil {
		return err
	}
	if giResp.Status != "OK" {
		r.log.Debug("DaemonGetInfo: bad status: %s", giResp.Status)
		return errBadDaemonStatus
	}

	r.daemonStateMtx.Lock() // future
	r.daemonState.height = giResp.Height
	r.daemonState.connectHeight = giResp.Height
	r.daemonState.blockHash = giResp.TopBlockHash
	r.daemonState.targetHeight = giResp.TargetHeight
	r.daemonState.busySyncing = giResp.BusySyncing
	r.daemonState.synchronized = giResp.Sychronized
	r.daemonState.restricted = giResp.Restricted
	r.daemonState.untrusted = giResp.Untrusted
	r.log.Tracef("daemon %s -- height: %d, height connected: %d busy_syncing: %v, synchronized: %v, restricted: %v, untrusted: %v", r.serverAddr,
		r.daemonState.height, r.daemonState.connectHeight, r.daemonState.busySyncing, r.daemonState.synchronized, r.daemonState.restricted, r.daemonState.untrusted)
	r.daemonStateMtx.Unlock() // future

	// store daemon rpc client
	r.daemon = daemon

	// start wallet server and connect it to the running daemon
	walletRpc := path.Join(r.cliToolsDir, WalletServerRpcName)
	cmd := exec.Command(walletRpc)
	serverAddr := DaemonAddressParam + r.serverAddr
	cmd.Args = append(cmd.Args, serverAddr)
	switch r.net {
	case dex.Mainnet:
		// mainnet
		cmd.Args = append(cmd.Args, RpcBindPortParam+MainnetWalletServerRpcPort)
		if r.serverIsLocal {
			cmd.Args = append(cmd.Args, TrustedDaemonParam) // wallet trusts the daemon
		}
	case dex.Testnet:
		// stagenet
		cmd.Args = append(cmd.Args, StagenetParam)
		cmd.Args = append(cmd.Args, RpcBindPortParam+StagenetWalletServerRpcPort)
		if r.serverIsLocal {
			cmd.Args = append(cmd.Args, TrustedDaemonParam)
		}
	case dex.Simnet:
		// regtest - iff wallet server connects to a daemon which is started
		// with the --regtest parameter
		cmd.Args = append(cmd.Args, RpcBindPortParam+RegtestWalletServerRpcPort) // harness2
		cmd.Args = append(cmd.Args, TrustedDaemonParam)
	default:
		return fmt.Errorf("unknown network")
	}
	walletDir := WalletDirParam + r.dataDir // per net
	cmd.Args = append(cmd.Args, walletDir)
	cmd.Args = append(cmd.Args, DisableRpcLoginParam)
	cmd.Args = append(cmd.Args, AllowMismatchedDaemonVersionParam)
	logfilePath := path.Join(r.dataDir, WalletLogfileName)
	cmd.Args = append(cmd.Args, LogFileParam+logfilePath)
	cmd.Args = append(cmd.Args, LogLevelParam+WalletLogLevel)
	err = cmd.Start()
	if err != nil {
		return fmt.Errorf("child process start: %v", err)
	}
	// started
	r.walletRpcProcess = cmd.Process
	r.log.Debug("wallet rpc server is started")

	// make a wallet rpc client; always local
	switch r.net {
	case dex.Mainnet:
		r.wallet = rpc.New(rpc.Config{
			Address: HttpLocalhost + MainnetWalletServerRpcPort + Json2query,
			Client:  &http.Client{ /*default no auth HTTP client*/ },
		})
	case dex.Testnet: // stagenet
		r.wallet = rpc.New(rpc.Config{
			Address: HttpLocalhost + StagenetWalletServerRpcPort + Json2query,
			Client:  &http.Client{ /*default no auth HTTP client*/ },
		})
	case dex.Simnet:
		r.wallet = rpc.New(rpc.Config{
			Address: HttpLocalhost + RegtestWalletServerRpcPort + Json2query,
			Client:  &http.Client{ /*default no auth HTTP client*/ },
		})
	}
	return nil
}

// stopWalletServer kills the child wallet server process
func (r *xmrRpc) stopWalletServer() error {
	r.log.Trace("stopWalletServer")
	if r.walletRpcProcess == nil {
		return errors.New("no process")
	}
	err := r.stopWallet()
	r.walletRpcProcess.Release()
	r.walletRpcProcess = nil
	return err
}
