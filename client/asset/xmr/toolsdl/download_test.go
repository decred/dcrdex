//go:build xmrdl

package toolsdl

import (
	"context"
	"errors"
	"fmt"
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
	dl := new(Download)
	dl.SetLogger(dex.StdOutLogger("Test", slog.LevelTrace))
	_, dir, err := dl.GetCurrentLocalToolsDir()
	if err != nil {
		if errors.Is(err, ErrNoLocalVersion) {
			fmt.Println("Current local version does mot exist")
			return
		}
		t.Fatalf("get current version: - error: %v", err)
	}
	mv, err := newMoneroVersionDir(dir)
	if err != nil {
		t.Fatalf("new monero version: - error: %v", err)
	}
	fmt.Printf("current stored version is %s\n", mv.string())
}

func TestGetLatestCanonicalVersion(t *testing.T) {
	dl := new(Download)
	dl.SetLogger(dex.StdOutLogger("Test", slog.LevelTrace))
	mv, err := dl.GetLatestRemoteCanonicalVersion(context.Background())
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
	dl := new(Download)
	dl.SetLogger(dex.StdOutLogger("Test", slog.LevelTrace))
	_, err := dl.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	fmt.Println("Download success!")
}
