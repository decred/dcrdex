//go:build xmrdl

package toolsdl

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"decred.org/dcrdex/dex"
	"github.com/decred/slog"
)

func TestMachine(t *testing.T) {
	m := getMachine()
	t.Logf("OS: %s, Arch: %s", m.os, m.arch)
}

func TestGetBestCurrentLocalToolsDir(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "mainnet", "assetdb", "xmr")
	dl := NewDownload(dataDir, dex.StdOutLogger("Test", slog.LevelTrace))

	// no tools dir
	hasPath, path, err := dl.GetBestCurrentLocalToolsDir()
	if err != nil {
		t.Fatal(err)
	}
	if hasPath {
		t.Fatalf("got unexpected path: %s", path)
	}

	// set up a tools dir
	newDir := filepath.Join(dl.getToolsBasePath(), "monero-linux-x64-v0.18.4.3")
	err = os.MkdirAll(newDir, 0700)
	if err != nil {
		t.Fatal(err)
	}
	_, err = os.Create(filepath.Join(newDir, "monero-wallet-cli"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = os.Create(filepath.Join(newDir, "monero-wallet-rpc"))
	if err != nil {
		t.Fatal(err)
	}
	// test with a tools dir
	hasPath, path, err = dl.GetBestCurrentLocalToolsDir()
	dir := filepath.Base(path)
	if err != nil {
		t.Fatalf("get current version: - error: %v", err)
	}
	if !hasPath {
		t.Log("Current local version does not exist")
	}
	mv, err := newMoneroVersionFromDir(dir)
	if err != nil {
		t.Fatalf("new monero version: - error: %v", err)
	}
	t.Logf("Current stored version is %s\n", mv.string())
}

func TestAllToolsDownload(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "netnet", "assetdb", "xmr")
	dl := NewDownload(dataDir, dex.StdOutLogger("Test", slog.LevelTrace))

	toolsDir, err := dl.Run()
	if err != nil {
		t.Fatal(err)
	}

	cli := "monero-wallet-cli"
	if dl.machine.os == "windows" {
		cli += ".exe"
	}
	s, err := os.Stat(filepath.Join(toolsDir, cli))
	if err != nil {
		t.Fatal(err)
	}
	if s.Size() == 0 {
		t.Fatalf("monero cli has zero length")
	}

	rpc := "monero-wallet-rpc"
	if dl.machine.os == "windows" {
		rpc += ".exe"
	}
	s1, err := os.Stat(filepath.Join(toolsDir, rpc))
	if err != nil {
		t.Fatal(err)
	}
	if s1.Size() == 0 {
		t.Fatalf("monero rpc has zero length")
	}

	hashesTxt := filepath.Join(dl.getToolsBasePath(), "hashes.txt")
	_, err = os.Stat(hashesTxt)
	if err == nil {
		t.Fatalf("hashes.txt should have been deleted after use - %v", err)
	}
	t.Log("Download success!")
}

func TestToolsMAVDownload(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "netnet", "assetdb", "xmr")
	dl := NewDownload(dataDir, dex.StdOutLogger("Test", slog.LevelTrace))
	// normally done in 'Run'
	tempDir, err := os.MkdirTemp("", "share-mtools-mav")
	if err != nil {
		t.Fatalf("error making temp dir - %v", err)
	}
	dl.tempDir = tempDir
	// hard coded
	toolsDir, err := dl.runMavDownload()
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("toolsDir: %s", toolsDir)
}

func TestToolsBasePath(t *testing.T) {
	home, _ := os.UserHomeDir()
	var dataDir = filepath.Join(home, ".dexc", "mainnet", "assetdb", "xmr")
	dl := NewDownload(dataDir, dex.StdOutLogger("Test", slog.LevelTrace))
	tbp := dl.getToolsBasePath()
	if tbp != filepath.Join(home, ".dexc", "share", "monero-tools") {
		t.Fatalf("bad tools path %s", tbp)
	}

	dataDir = filepath.Join(home, ".dexc", "simnet", "assetdb", "xmr")
	dl = NewDownload(dataDir, dex.StdOutLogger("Test", slog.LevelTrace))
	tbp = dl.getToolsBasePath()
	if tbp != filepath.Join(home, ".dexc", "share", "monero-tools") {
		t.Fatalf("bad tools path %s", tbp)
	}

	dataDir = filepath.Join(home, ".dexc", "testnet", "assetdb", "xmr")
	dl = NewDownload(dataDir, dex.StdOutLogger("Test", slog.LevelTrace))
	tbp = dl.getToolsBasePath()
	if tbp != filepath.Join(home, ".dexc", "share", "monero-tools") {
		t.Fatalf("bad tools path %s", tbp)
	}

	dataDir = filepath.Join(home, "testnet", "assetdb", "xmr")
	dl = NewDownload(dataDir, dex.StdOutLogger("Test", slog.LevelTrace))
	tbp = dl.getToolsBasePath()
	if tbp != filepath.Join(home, "share", "monero-tools") {
		t.Fatalf("bad tools path %s", tbp)
	}

	dataDir = filepath.Join(string(os.PathSeparator), "testnet", "assetdb", "xmr")
	dl = NewDownload(dataDir, dex.StdOutLogger("Test", slog.LevelTrace))
	tbp = dl.getToolsBasePath()
	if tbp != filepath.Join(string(os.PathSeparator), "share", "monero-tools") {
		t.Fatalf("bad tools path %s", tbp)
	}

	dataDir = filepath.Join(home, "dextest", "simnet-walletpair", "dexc2", "regtestsimnet", "assetdb", "xmr")
	dl = NewDownload(dataDir, dex.StdOutLogger("Test", slog.LevelTrace))
	tbp = dl.getToolsBasePath()
	if tbp != filepath.Join(home, "dextest", "simnet-walletpair", "dexc2", "share", "monero-tools") {
		t.Fatalf("bad tools path %s", tbp)
	}
}

func TestDownloadHashesFile(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "mainnet", "assetdb", "xmr")
	dl := NewDownload(dataDir, dex.StdOutLogger("Test", slog.LevelTrace))
	dl.tempDir = dataDir // download here
	hashFilePath, err := dl.downloadHashesFile()
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%s\n", hashFilePath)
}

func TestUrlGet(t *testing.T) {
	hashesCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	resp, err := urlGet(hashesCtx, hashesLink)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
}
