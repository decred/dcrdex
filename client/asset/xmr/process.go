package xmr

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path"
	"runtime"

	"decred.org/dcrdex/dex"
	"github.com/dev-warrior777/go-monero/rpc"
)

const WalletRpcName = "monero-wallet-rpc"

func (r *xmrRpc) startWalletServer() error {
	r.log.Trace("startWalletServer")
	// first check out the daemon which should be up and running remotely or up
	// and running on localhost if it is the end user's personal monerod server.
	addressParam := r.serverAddr + "/json_rpc"
	daemon := rpc.New(rpc.Config{
		Address: addressParam,
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
	// store daemon rpc client
	r.daemon = daemon

	// start wallet server and connect to the running daemon
	walletRpc := path.Join(r.cliToolsDir, WalletRpcName)
	cmd := exec.Command(walletRpc)
	serverAddrParam := "--daemon-address=" + r.serverAddr
	cmd.Args = append(cmd.Args, serverAddrParam)
	switch r.net {
	case dex.Mainnet:
		// mainnet
		cmd.Args = append(cmd.Args, "--rpc-bind-port=18083")
		if r.serverIsLocal {
			cmd.Args = append(cmd.Args, "--trusted-daemon")
		}
	case dex.Testnet:
		cmd.Args = append(cmd.Args, "--stagenet")
		cmd.Args = append(cmd.Args, "--rpc-bind-port=38083")
		if r.serverIsLocal {
			cmd.Args = append(cmd.Args, "--trusted-daemon")
		}
	case dex.Simnet:
		cmd.Args = append(cmd.Args, "--regtest")
		cmd.Args = append(cmd.Args, "--rpc-bind-port=28484") // harness
		cmd.Args = append(cmd.Args, "--trusted-daemon")
	default:
		return fmt.Errorf("unknown network")
	}
	walletDirParam := "--wallet-dir=" + r.dataDir // per net
	cmd.Args = append(cmd.Args, walletDirParam)
	if r.serverPass == "" {
		cmd.Args = append(cmd.Args, "--disable-rpc-login")
	} else {
		// specify username[:password] for daemon RPC client
		daemonLoginParam := "--daemon-login=" + r.serverPass
		cmd.Args = append(cmd.Args, daemonLoginParam)
	}
	cmd.Args = append(cmd.Args, "--allow-mismatched-daemon-version")
	logfilePath := path.Join(r.dataDir, "wallet-rpc.log")
	cmd.Args = append(cmd.Args, "--log-file="+logfilePath)
	cmd.Args = append(cmd.Args, "--log-level=2")
	err = cmd.Start()
	if err != nil {
		return fmt.Errorf("child process start: %v", err)
	}
	// started
	r.walletRpcProcess = cmd.Process
	r.log.Debug("wallet rpc server is started")

	// make a wallet rpc client
	switch r.net {
	case dex.Mainnet, dex.Simnet:
		r.wallet = rpc.New(rpc.Config{
			Address: "http://127.0.0.1:18083/json_rpc",
			Client:  &http.Client{ /*default no auth HTTP client*/ },
		})
	case dex.Testnet: // stagenet
		r.wallet = rpc.New(rpc.Config{
			Address: "http://127.0.0.1:38083/json_rpc",
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
	var err error
	if runtime.GOOS == "windows" {
		err = r.walletRpcProcess.Kill()
	} else {
		err = r.walletRpcProcess.Signal(os.Interrupt)
	}
	r.walletRpcProcess.Release()
	return err
}
