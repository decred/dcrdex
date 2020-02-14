// Copyright (c) 2020 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package main

import (
	"io/ioutil"
	"os"
	"testing"
)

func TestConfigure(t *testing.T) {
	// print version
	os.Args = []string{"", "-V"}
	_, _, stop, err := configure()
	if err != nil {
		t.Fatal(err)
	}
	if stop != true {
		t.Fatal("did not stop for version info")
	}

	// list commands
	os.Args = []string{"", "-l"}
	_, _, stop, err = configure()
	if err != nil {
		t.Fatal(err)
	}
	if stop != true {
		t.Fatal("did not stop when listing commands")
	}

	// show help
	os.Args = []string{"", "-h"}
	_, _, stop, err = configure()
	if err != nil {
		t.Fatal(err)
	}
	if stop != true {
		t.Fatal("did not stop when showing help")
	}

	// parse command line flags
	os.Args = []string{"", "-ubob", "--rpcpass=password123", "-C.nofile"}
	cfg, _, _, err := configure()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.RPCUser != "bob" && cfg.RPCPass != "password123" {
		t.Fatal("incorrectly parsed command line args")
	}

	// parse config file
	tmp, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	cfgFile := tmp + "/testconfig"
	defer os.Remove(cfgFile)
	b := []byte("rpcaddr=1.2.3.4:3000\nproxyuser=jorb\n")
	err = ioutil.WriteFile(cfgFile, b, 0644)
	if err != nil {
		t.Fatal(err)
	}
	os.Args = []string{"", "-C" + cfgFile}
	cfg, _, _, err = configure()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ProxyUser != "jorb" && cfg.RPCAddr != "1.2.3.4:3000" {
		t.Fatal("incorrectly parsed file")
	}

	// parse args
	os.Args = []string{"", "-C.nofile", "arg1", "arg2"}
	_, args, _, err := configure()
	if err != nil {
		t.Fatal(err)
	}
	if args[0] != "arg1" && args[1] != "arg2" {
		t.Fatal("arguments not parsed correctly")
	}

	// bad flag
	os.Args = []string{"", "-7"}
	_, _, _, err = configure()
	if err == nil {
		t.Fatal("expected failure on bad flag")
	}
}
