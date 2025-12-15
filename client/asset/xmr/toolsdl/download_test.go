//////go:build xmrdl

package toolsdl

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"decred.org/dcrdex/dex"
	"github.com/decred/slog"
)

func TestMachine(t *testing.T) {
	m := getMachine()
	fmt.Println("OS:", m.os)
	fmt.Println("ARCH:", m.arch)
}

func TestGetCurrentLocalToolsDir(t *testing.T) {
	dataDir := t.TempDir()
	dl := &Download{
		DataDir: dataDir,
		Log:     dex.StdOutLogger("Test", slog.LevelTrace),
	}
	_, dir, err := dl.GetCurrentLocalToolsDir()
	if err != nil {
		if errors.Is(err, ErrNoLocalVersion) {
			fmt.Println("Current local version does mot exist")
			return
		}
		t.Fatalf("get current version: - error: %v", err)
	}
	mv, err := newMoneroVersionFromDir(dir)
	if err != nil {
		t.Fatalf("new monero version: - error: %v", err)
	}
	fmt.Printf("current stored version is %s\n", mv.string())
}

func TestGetLatestCanonicalVersion(t *testing.T) {
	dataDir := t.TempDir()
	dl := &Download{
		DataDir: dataDir,
		Log:     dex.StdOutLogger("Test", slog.LevelTrace),
	}
	mv, err := dl.getLatestRemoteCanonicalVersion(context.Background())
	if err != nil {
		if errors.Is(err, ErrNoRemoteVersion) {
			// bad - ask for alternative on IRC
			fmt.Println("Canonical remote version does mot esist")
			return
		}
		t.Fatalf("get current version: - error: %v", err)
	}
	fmt.Printf("latest canonical version from hashes.txt is %s\n", mv.string())
}

func TestToolsDownload(t *testing.T) {
	dataDir := t.TempDir() // cleaned up by T
	dl := &Download{
		DataDir: dataDir,
		Log:     dex.StdOutLogger("Test", slog.LevelTrace),
	}
	toolsDir, err := dl.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	cli := "monero-wallet-cli"
	if dl.m.os == "windows" {
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
	if dl.m.os == "windows" {
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

	fmt.Println("Download success!")
}
