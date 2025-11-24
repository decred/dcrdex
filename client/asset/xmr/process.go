package xmr

import (
	"bufio"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"runtime"
	"strings"
	"sync"
	"time"

	"decred.org/dcrdex/dex"
)

const (
	WalletServerRpcName = "monero-wallet-rpc"
	WalletLogfileName   = WalletServerRpcName + ".log"
	WalletLogLevel      = "2"
	WalletFileName      = "rpcwallet"
	WalletKeysFileName  = "rpcwallet.keys"
)

const (
	CliName        = "monero-wallet-cli"
	CliLogfileName = CliName + ".log"
	CliLogLevel    = "0"
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
	CliGenFromSpendKeyParam         = "--generate-from-spend-key"
	CliMnemonicLanguageEnglishParam = "--mnemonic-language=English" // hard code
	CliPasswordParam                = "--password="
	CliCommandRefreshParam          = "--command=refresh"
)

const (
	CliSeedTrigger    = "Logging to"
	CliDateTrigger    = "Restore from specific blockchain height"
	CliDateYesTrigger = "Restore height is:"
)

func (r *xmrRpc) probeDaemon(ctx context.Context) error {
	info, err := r.getInfo(ctx)
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
	if runtime.GOOS == "windows" {
		walletRpc += ".exe"
	}
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
	r.log.Debug("Wallet rpc server is started")
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
	return DefaultRegtestWalletServerRpcPort
}

// cliGenerateRefreshWallet generates a monero wallet from a given spendkey (seed)
// and password bytes with a given birthday. Birthday should not be later than now.
func cliGenerateRefreshWallet(
	ctx context.Context,
	trustedDaemon string,
	log dex.Logger,
	net dex.Network,
	dataDir,
	cliToolsDir string,
	pw, seed []byte,
	birthday uint64) error {

	cli := path.Join(cliToolsDir, CliName)
	if runtime.GOOS == "windows" {
		cli += ".exe"
	}

	// changing any of these parameters will change the behavior of monero-wallet-cli
	// and the state machine goroutine below should be updated.
	cmd := exec.CommandContext(ctx, cli)
	switch net {
	case dex.Mainnet:
		// do nothing
	case dex.Testnet: // stagenet
		cmd.Args = append(cmd.Args, StagenetParam)
	case dex.Simnet: // regtest
		// do nothing - regtest is mimicking mainnet with a daemon started with the --regtest param
	default:
		return fmt.Errorf("unknown network")
	}
	cmd.Args = append(cmd.Args, CliGenFromSpendKeyParam)
	walletFilePath := path.Join(dataDir, WalletFileName)
	cmd.Args = append(cmd.Args, walletFilePath)
	cmd.Args = append(cmd.Args, CliPasswordParam+hex.EncodeToString(pw))
	cmd.Args = append(cmd.Args, CliMnemonicLanguageEnglishParam)
	serverAddr := DaemonAddressParam + trustedDaemon
	cmd.Args = append(cmd.Args, serverAddr)
	cmd.Args = append(cmd.Args, TrustedDaemonParam) // wallet trusts the daemon
	cmd.Args = append(cmd.Args, AllowMismatchedDaemonVersionParam)
	logfilePath := path.Join(dataDir, CliLogfileName)
	cmd.Args = append(cmd.Args, LogFileParam+logfilePath)
	cmd.Args = append(cmd.Args, LogLevelParam+CliLogLevel)
	cmd.Args = append(cmd.Args, CliCommandRefreshParam)

	cliSpendkeyAnswer := hex.EncodeToString(seed)
	if runtime.GOOS == "windows" {
		cliSpendkeyAnswer += "\r"
	}
	cliSpendkeyAnswer += "\n"

	timeString := time.Unix(int64(birthday), 0).String()
	cliDateAnswer := strings.Split(timeString, " ")[0]
	if runtime.GOOS == "windows" {
		cliDateAnswer += "\r"
	}
	cliDateAnswer += "\n"

	cliDateAnswerYes := "Yes"
	if runtime.GOOS == "windows" {
		cliDateAnswerYes += "\r"
	}
	cliDateAnswerYes += "\n"

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("unable to create stdin pipe: %v", err)
	}
	stdoutScanner := bufio.NewScanner(stdout)

	// scanner uses this buffer not it's internal one while reading from the
	// child STDOUT pipe.
	// The extra +1 is to allow scanner to allocate on any edge case such as
	// a very long string in the buffer.
	// Monero does this when printing refresh status on the same cli terminal
	// line, e.g.
	//
	// "Height 3516301 / 3541634\rHeight 3526300 / 3541634\r..."
	//
	// Using CR to overwrite can make a very long token before any '\n' and the
	// refresh (sync) is very long, say from the last checkpoint.
	buf := make([]byte, 0, bufio.MaxScanTokenSize)
	stdoutScanner.Buffer(buf, bufio.MaxScanTokenSize+1)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("unable to create stdout pipe: %v", err)
	}
	stdinWriter := bufio.NewWriter(stdin)

	var wg sync.WaitGroup

	// Start up the child process
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("child process start failed: %v", err)
	}

	const (
		seedState    = "seed"
		dateState    = "date"
		dateYesState = "yes"
		doneState    = "done"
	)

	wg.Add(1)
	var state = seedState
	next := make(chan string)

	// Handle child STDOUT
	go func() {
		defer wg.Done()
		for stdoutScanner.Scan() {
			if ctx.Err() != nil {
				break
			}
			line := stdoutScanner.Text()

			if state == seedState && strings.Contains(line, CliSeedTrigger) {
				next <- state
				state = dateState
				continue
			}

			if state == dateState && strings.Contains(line, CliDateTrigger) {
				next <- state
				state = dateYesState
				continue
			}

			if state == dateYesState && strings.Contains(line, CliDateYesTrigger) {
				next <- state
				state = doneState
			}
		}
		close(next)

		// EOF or Error
		if err := stdoutScanner.Err(); err != nil && err != io.EOF {
			log.Errorf("stdoutScanner error: %v - current state: %s", err, state)
		}
	}()

	// Handle child STDIN
out:
	for {
		select {
		case <-ctx.Done():
			return nil
		case s := <-next:
			switch s {
			case seedState:
				log.Tracef("state: %s", s)
				stdinWriter.WriteString(cliSpendkeyAnswer)
				stdinWriter.Flush()

			case dateState:
				log.Tracef("state: %s", s)
				stdinWriter.WriteString(cliDateAnswer)
				stdinWriter.Flush()

			case dateYesState:
				log.Tracef("state: %s", s)
				stdinWriter.WriteString(cliDateAnswerYes)
				stdinWriter.Flush()

				break out

			default:
				log.Errorf("Unknown state: %s", s)
			}
		}
	}

	log.Debug("Wallet syncing")

	// wait for the scanner goroutine
	wg.Wait()

	log.Debug("Scanner finished reading")

	// tell the child process we are finished sending input.
	stdin.Close()

	log.Debug("Wrote all data to child's stdin and closed stdin pipe")

	// wait for the child process to exit; closes stdout
	processErr := cmd.Wait()

	if processErr != nil {
		// If we are past the seed entry state - then fail - there will be maybe valid wallet files in
		// the data dir with a balance of 0.000000000000.
		//
		// If user tries again to create monero-wallet-cli will return "wallet already esists" exit(1).
		//
		// Delete the generated wallet files!
		deleteWalletFiles(dataDir, log)

		return fmt.Errorf("failed to create wallet - error: %w, end state: %s", processErr, state)
	}

	log.Debugf("exec: done, state: %s", state)
	return nil
}

// deleteWalletFiles deletes wallet & wallet.keys from the data dir.
// This is Dangerous function; current users:
// - cliGenerateRefreshWallet
func deleteWalletFiles(dataDir string, log dex.Logger) {
	log.Warn("Deleting both monero wallet files")

	walletFile := path.Join(dataDir, WalletFileName)
	err := os.Remove(walletFile)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			log.Warnf("%s does not exist", walletFile)
		} else {
			log.Errorf("error: %v deleting %s", err, walletFile)
		}
	}

	walletKeysFile := path.Join(dataDir, WalletKeysFileName)
	err = os.Remove(walletKeysFile)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			log.Warnf("%s does not exist", walletKeysFile)
		} else {
			log.Errorf("error: %v deleting %s", err, walletKeysFile)
		}
	}

	log.Warnf("Deleted any existing monero wallet files in %s", dataDir)
}
