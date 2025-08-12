package xmr

import (
	"context"
	"fmt"
	"os/exec"
	"path"
	"runtime"
	"strings"

	"decred.org/dcrdex/dex"
)

const (
	WalletServerRpcName = "monero-wallet-rpc"
	WalletLogfileName   = WalletServerRpcName + ".log"
	WalletLogLevel      = "2"
	WalletFileName      = "dex"
	WalletKeysFileName  = "dex.keys"
)

const (
	CliName        = "monero-wallet-cli"
	CliLogfileName = CliName + ".log"
	CliLogLevel    = "3"
)

const (
	HttpLocalhost                       = "http://127.0.0.1:"
	Json2query                          = "/json_rpc"
	MainnetWalletServerRpcPort          = "18083"
	StagenetWalletServerRpcPort         = "38083"
	DefaultRegtestWalletServerRpcPort   = "18083"
	AlternateRegtestWalletServerRpcPort = "18087"
)

const (
	DaemonAddressParam                = "--daemon-address="
	RpcBindPortParam                  = "--rpc-bind-port="
	StagenetParam                     = "--stagenet"
	TrustedDaemonParam                = "--trusted-daemon"
	WalletDirParam                    = "--wallet-dir="
	WalletFileParam                   = "--wallet-file="
	DisableRpcLoginParam              = "--disable-rpc-login"
	AllowMismatchedDaemonVersionParam = "--allow-mismatched-daemon-version"
	LogFileParam                      = "--log-file="
	LogLevelParam                     = "--log-level="
)

const (
	CliGenerateNewWalletParam       = "--generate-new-wallet"
	CliMnemonicLanguageEnglishParam = "--mnemonic-language=English" // hard code
	CliPasswordParam                = "--password="
	CliPasswordFileParam            = "--password-file=" //TODO(xmr)
	CliOfflineParam                 = "--offline"
	CliCommandBalanceParam          = "--command=balance"
	CliCommandAddressParam          = "--command=address"
	CliCommandRefreshParam          = "--command=refresh"
)

func (r *xmrRpc) probeDaemon() error {
	r.log.Trace("probeDaemon")
	info, err := r.getInfo()
	if err != nil {
		return err
	}
	if info.Status != "OK" {
		return fmt.Errorf("daemon bad status: %w - expected 'OK'", err)
	}
	r.daemonState.Lock()
	r.daemonState.height = info.Height
	r.daemonState.blockHash = info.TopBlockHash
	r.daemonState.synchronized = info.Sychronized
	r.daemonState.restricted = info.Restricted
	r.daemonState.untrusted = info.Untrusted
	r.log.Debugf("daemon %s -- height: %d, synchronized: %v, restricted: %v, untrusted: %v", r.daemonAddr,
		r.daemonState.height, r.daemonState.synchronized, r.daemonState.restricted, r.daemonState.untrusted)
	r.daemonState.Unlock()
	return nil
}

// startWalletServer starts the wallet server and connects it to the running daemon
func (r *xmrRpc) startWalletServer(ctx context.Context) error {
	walletRpc := path.Join(r.cliToolsDir, WalletServerRpcName)
	cmd := exec.CommandContext(ctx, walletRpc)
	serverAddr := DaemonAddressParam + r.daemonAddr
	cmd.Args = append(cmd.Args, serverAddr)
	switch r.net {
	case dex.Mainnet:
		cmd.Args = append(cmd.Args, RpcBindPortParam+MainnetWalletServerRpcPort)
		if r.daemonIsLocal {
			cmd.Args = append(cmd.Args, TrustedDaemonParam) // wallet trusts the daemon
		}
	case dex.Testnet:
		// stagenet
		cmd.Args = append(cmd.Args, StagenetParam)
		cmd.Args = append(cmd.Args, RpcBindPortParam+StagenetWalletServerRpcPort)
		if r.daemonIsLocal {
			cmd.Args = append(cmd.Args, TrustedDaemonParam)
		}
	case dex.Simnet:
		// regtest - iff wallet server connects to a daemon which is started with the --regtest parameter
		cmd.Args = append(cmd.Args, RpcBindPortParam+getRegtestWalletServerRpcPort(r.dataDir)) // harness-beta
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
	err := cmd.Start()
	if err != nil {
		return fmt.Errorf("child process start: %v", err)
	}
	// started
	r.walletRpcProcess = cmd.Process
	r.log.Debug("wallet rpc server is started")
	return nil
}

func getRegtestWalletServerRpcPort(dataDir string) string {
	if runtime.GOOS == "windows" {
		return ""
	}
	if strings.Contains(dataDir, "/simnet-walletpair/dexc1/") {
		return DefaultRegtestWalletServerRpcPort
	}
	if strings.Contains(dataDir, "/simnet-walletpair/dexc2/") {
		return AlternateRegtestWalletServerRpcPort
	}
	return "bad-path"
}

func cliGenerateRefreshWallet(ctx context.Context, trustedDaemon string, net dex.Network, dataDir, cliToolsDir, pw string, refresh bool) error {
	cli := path.Join(cliToolsDir, CliName)
	cmd := exec.CommandContext(ctx, cli)
	switch net {
	case dex.Mainnet:
		// do nothing
	case dex.Testnet: // stagenet
		cmd.Args = append(cmd.Args, StagenetParam)
	case dex.Simnet: // regtest
		return fmt.Errorf("there is no --regtest parameter for monero-wallet-cli - create & sync a wallet manually with simnet tool provided and put into: %s", dataDir)
	default:
		return fmt.Errorf("unknown network")
	}
	cmd.Args = append(cmd.Args, CliGenerateNewWalletParam)
	walletFilePath := path.Join(dataDir, WalletFileName)
	cmd.Args = append(cmd.Args, walletFilePath)
	cmd.Args = append(cmd.Args, CliPasswordParam+pw)
	cmd.Args = append(cmd.Args, CliMnemonicLanguageEnglishParam)
	serverAddr := DaemonAddressParam + trustedDaemon
	cmd.Args = append(cmd.Args, serverAddr)
	cmd.Args = append(cmd.Args, TrustedDaemonParam) // wallet trusts the daemon
	cmd.Args = append(cmd.Args, AllowMismatchedDaemonVersionParam)
	logfilePath := path.Join(dataDir, CliLogfileName)
	cmd.Args = append(cmd.Args, LogFileParam+logfilePath)
	cmd.Args = append(cmd.Args, LogLevelParam+CliLogLevel)
	if refresh {
		cmd.Args = append(cmd.Args, CliCommandRefreshParam)
	} else {
		cmd.Args = append(cmd.Args, CliCommandBalanceParam)
	}

	var sb strings.Builder
	for _, arg := range cmd.Args {
		sb.WriteString(arg)
		sb.WriteString(" ")
	}

	err := cmd.Start()
	if err != nil {
		return fmt.Errorf("cli create wallet process start %w", err)
	}
	err = cmd.Wait()
	if err != nil {
		return fmt.Errorf("create wallet command exited with status %d", cmd.ProcessState.ExitCode())
	}
	return nil
}
