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
	// adjust as needed to run a different version
	MoneroToolsDir = "monero-x86_64-linux-gnu-v0.18.4.3"

	CakeMainnetDaemon = "https://xmr-node.cakewallet.com:18081"

	TestPw   = "abc"
	TestSeed = "d92f4e072b78eb782c06ae73ab2fed5ea31b5858b150791d927303af61d38401"

	MoneroCliLogfile    = "monero-wallet-cli.log"
	GeneratedWalletName = "rpcwallet"
)

// go test -v -count=1 -tags=xmrlive -run=TestCliGenerateRefreshWalletCmd

func TestCliGenerateRefreshWalletCmd(t *testing.T) {
	dataDir := t.TempDir() // will be cleaned up by T

	ctx, cancel := context.WithCancel(context.Background()) // used by exec.CommandContext
	defer cancel()

	logger := dex.StdOutLogger("test", slog.LevelTrace)
	home, _ := os.UserHomeDir()
	userToolsDir := path.Join(home, MoneroToolsDir)
	pwB := []byte(TestPw)
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
