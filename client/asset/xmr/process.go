package xmr

import (
	"bufio"
	"context"
	"encoding/hex"
	"fmt"
	"io"
	"log"
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
	CliRestoreHeightParam           = "--restore-height=" // will be used for SendAndLock
	CliCommandAddressParam          = "--command=address"
	CliCommandRefreshParam          = "--command=refresh"
)

const (
	CliSeedTrigger    = "Logging to"
	CliDateTrigger    = "Restore from specific blockchain height"
	CliDateYesTrigger = "Restore height is:"
)

const (
	Seed    = 0
	Date    = 1
	DateYes = 2
	Done    = 3
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
	return DefaultRegtestWalletServerRpcPort
}

// cliGenerateRefreshWallet generates a monero wallet from a given spendkey (seed)
// and password bytes with a given birthday. Birthday should not be later than now.
func cliGenerateRefreshWallet(
	ctx context.Context,
	trustedDaemon string,
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
	cmd.Args = append(cmd.Args, CliPasswordParam+string(pw))
	cmd.Args = append(cmd.Args, CliMnemonicLanguageEnglishParam)
	serverAddr := DaemonAddressParam + trustedDaemon
	cmd.Args = append(cmd.Args, serverAddr)
	cmd.Args = append(cmd.Args, TrustedDaemonParam) // wallet trusts the daemon
	cmd.Args = append(cmd.Args, AllowMismatchedDaemonVersionParam)
	logfilePath := path.Join(dataDir, CliLogfileName)
	cmd.Args = append(cmd.Args, LogFileParam+logfilePath)
	cmd.Args = append(cmd.Args, LogLevelParam+CliLogLevel)
	cmd.Args = append(cmd.Args, CliCommandRefreshParam)

	// TODO(test) remove ->
	var sb strings.Builder
	for _, arg := range cmd.Args {
		sb.WriteString(arg)
		sb.WriteString(" ")
	}
	fmt.Printf("%s\n", sb.String())
	//<-remove

	zero := func(bb []byte) {
		for i := range len(bb) {
			bb[i] = 0
		}
	}

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
	var buf = make([]byte, 0, bufio.MaxScanTokenSize)
	stdoutScanner.Buffer(buf, bufio.MaxScanTokenSize+1)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("unable to create stdout pipe: %v", err)
	}
	stdinWriter := bufio.NewWriter(stdin)

	var wg sync.WaitGroup

	// Start up the child process
	if err := cmd.Start(); err != nil {
		log.Fatal(err)
	}

	wg.Add(1)
	var state = Seed
	next := make(chan int)

	// Handle child STDOUT
	go func() {
		defer wg.Done()
		for stdoutScanner.Scan() {
			line := stdoutScanner.Text()
			fmt.Printf("[Child]: %s\n", line)

			if state == Seed && strings.Contains(line, CliSeedTrigger) {
				next <- state
				state = Date
				continue
			}

			if state == Date && strings.Contains(line, CliDateTrigger) {
				next <- state
				state = DateYes
				continue
			}

			if state == DateYes && strings.Contains(line, CliDateYesTrigger) {
				next <- state
				state = Done
			}
		}
		close(next)

		// EOF or Error
		if err := stdoutScanner.Err(); err != nil && err != io.EOF {
			fmt.Printf("stdoutScanner error: %v\n", err)
			fmt.Printf("current state: %d\n", state)
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
			case Seed:
				fmt.Printf("seed state: %d\n", s)
				stdinWriter.WriteString(cliSpendkeyAnswer)
				stdinWriter.Flush()

				zero(pw)
				zero(seed)

			case Date:
				fmt.Printf("date state: %d\n", s)
				stdinWriter.WriteString(cliDateAnswer)
				stdinWriter.Flush()

			case DateYes:
				fmt.Printf("yes state: %d\n", s)
				stdinWriter.WriteString(cliDateAnswerYes)
				stdinWriter.Flush()

				break out

			default:
				fmt.Printf("unknown state: %d\n", s)
			}
		}
	}

	// tell the child process we are finished sending input.
	stdin.Close()

	fmt.Println("[Parent]: Wrote data to child's stdin and closed stdin pipe.")

	// wait for the child process to exit; closes stdout
	processErr := cmd.Wait()

	// wait for the scanner goroutine
	wg.Wait()

	if processErr != nil {
		fmt.Printf("[Parent]: cmd.Wait finished with error: %v\n", processErr)
		fmt.Printf("[Parent]: end state: %d\n", state)

		// TODO(xmr)
		// We probably have a seeded wallet file in the data dir with a balance of 0.000000000000.
		// Should we delete it to be clean?
		// If user retries create monero-wallet-cli will return "wallet already esists" exit(1).
		//
		// But if we are past the seed entry stage there will be a valid wallet file although
		// it will still need refreshing (syncing)
		return fmt.Errorf("failed to create wallet - error: %w", processErr)
	}

	fmt.Printf("[Parent]: Done!, state: %d\n", state)
	return nil
}
