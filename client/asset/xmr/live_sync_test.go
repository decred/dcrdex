//go:build xmrlive

package xmr

import (
	"context"
	"encoding/hex"
	"os"
	"path"
	"testing"
	"time"

	"decred.org/dcrdex/dex"
	"github.com/decred/slog"
)

// Test constants
const (
	// Note: paths to 'monero-wallet-cli' & 'monero-wallet-rpc' have changed to:
	// User home-dir/.dex-monero-tools.
	//
	// If you do not have the tools yet run TestToolsDownload in the toolsdl pkg.

	// Adjust as needed to run a different version / os / arch.
	// Ex: MoneroToolsDir = "monero-win-x64-v0.18.4.4" from downloaded tools
	// in os.UserHomeDir()/.dex-monero-tools/monero-win-x64-v0.18.4.4
	MoneroToolsDir = ".dex-monero-tools/monero-linux-x64-v0.18.4.4"

	CakeMainnetDaemon = "https://xmr-node.cakewallet.com:18081"

	TestPw   = "ab" // can decode before re-encoding to "ab"
	TestSeed = "d92f4e072b78eb782c06ae73ab2fed5ea31b5858b150791d927303af61d38401"

	MoneroCliLogfile    = "monero-wallet-cli.log"
	GeneratedWalletName = "rpcwallet"
)

// go test -v -count=1 -tags=xmrlive -run=TestCliGenerateRefreshWalletCmd

func TestCliGenerateRefreshWalletCmd(t *testing.T) {
	dataDir := t.TempDir() // will be cleaned up by T

	ctx, cancel := context.WithCancel(context.Background()) // used by exec.CommandContext
	defer cancel()

	logger := dex.StdOutLogger("Test", slog.LevelTrace)
	home, _ := os.UserHomeDir()
	userToolsDir := path.Join(home, MoneroToolsDir)
	pwB, _ := hex.DecodeString(TestPw)
	seedB, _ := hex.DecodeString(TestSeed)

	err := cliGenerateRefreshWallet(
		ctx,
		CakeMainnetDaemon,
		logger,
		dex.Mainnet,
		dataDir,
		userToolsDir,
		pwB,
		seedB,
		uint64(time.Now().Unix()))
	if err != nil {
		t.Fatal(err)
	}
	s, err := os.Stat(path.Join(dataDir, MoneroCliLogfile))
	if err != nil {
		t.Fatal(err)
	}
	if s.Size() == 0 {
		t.Fatalf("monero logfile has zero length")
	}
	s1, err := os.Stat(path.Join(dataDir, GeneratedWalletName))
	if err != nil {
		t.Fatal(err)
	}
	if s1.Size() == 0 {
		t.Fatalf("monero wallet data file has zero length")
	}
	s2, err := os.Stat(path.Join(dataDir, GeneratedWalletName+".keys"))
	if err != nil {
		t.Fatal(err)
	}
	if s2.Size() == 0 {
		t.Fatalf("monero wallet keys file has zero length")
	}
}
