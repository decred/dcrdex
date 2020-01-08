package ui

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"
)

func TestConfig(t *testing.T) {
	// Command line arguments
	oldArgs := os.Args
	cmd := oldArgs[0]
	defer func() { os.Args = oldArgs }()

	// Prepare a temporary directory.
	dir, _ := ioutil.TempDir("", "test")
	defer os.RemoveAll(dir)

	createFile := func(path, contents string) {
		err := ioutil.WriteFile(path, []byte(contents), 0600)
		if err != nil {
			t.Fatalf("error writing %s: %v", path, err)
		}
	}

	check := func(tag string, res bool) {
		if !res {
			t.Fatalf("%s comparison failed", tag)
		}
	}

	createFile(filepath.Join(dir, "dexc_mainnet.conf"),
		"webaddr=:9876")

	createFile(filepath.Join(dir, "dexc_testnet.conf"),
		"notui=1\ntestnet=1\nrpc=1")

	createFile(filepath.Join(dir, "dexc_simnet.conf"),
		"webaddr=:1234\nsimnet=1\nweb=1")

	// Check the mainnet configuration.
	os.Args = []string{cmd, "--dir", dir}
	_, err := Configure()
	if err != nil {
		t.Fatalf("mainnet Configure error: %v", err)
	}
	check("mainnet notui", cfg.NoTUI == false)
	check("mainnet testnet", cfg.Testnet == false)
	check("mainnet simnet", cfg.Simnet == false)
	check("mainnet rpc", cfg.RPCOn == false)
	check("mainnet web", cfg.WebOn == false)
	check("mainnet webaddr", cfg.WebAddr == ":9876")

	// Check the mainnet configuration.
	os.Args = []string{cmd, "--dir", dir, "--testnet"}
	_, err = Configure()
	if err != nil {
		t.Fatalf("simnet Configure error: %v", err)
	}
	check("testnet notui", cfg.NoTUI == true)
	check("testnet testnet", cfg.Testnet == true)
	check("testnet simnet", cfg.Simnet == false)
	check("testnet rpc", cfg.RPCOn == true)
	check("testnet web", cfg.WebOn == false)
	check("testnet webaddr", cfg.WebAddr == defaultWebAddr)

	// Check the mainnet configuration.
	os.Args = []string{cmd, "--dir", dir, "--simnet"}
	_, err = Configure()
	if err != nil {
		t.Fatalf("simnet Configure error: %v", err)
	}
	check("simnet notui", cfg.NoTUI == false)
	check("simnet testnet", cfg.Testnet == false)
	check("simnet simnet", cfg.Simnet == true)
	check("simnet rpc", cfg.RPCOn == false)
	check("simnet web", cfg.WebOn == true)
	check("simnet webaddr", cfg.WebAddr == ":1234")
}
