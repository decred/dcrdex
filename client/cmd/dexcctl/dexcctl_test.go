// Copyright (c) 2020 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package main

import (
	"os"
	"reflect"
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
	err = os.WriteFile(cfgFile, b, 0644)
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

func TestReadTextFile(t *testing.T) {
	saveTextToFile := func(text, filePath string) {
		path := cleanAndExpandPath(filePath)
		file, err := os.Create(path)
		if err != nil {
			t.Fatalf("create test file error: %v", err)
		}
		file.WriteString(text)
		file.Close()
	}
	certTxt := "Hi. I'm a TLS certificate."
	cfgTxt := "Hi, I'm a config"
	tests := []struct {
		name, cmd, txtFilePath, txtToSave string
		args, want                        []string
		wantErr                           bool
	}{{
		name:        "ok with cert",
		cmd:         "getfee",
		args:        []string{"1.2.3.4:3000", "./cert"},
		txtFilePath: "./cert",
		txtToSave:   certTxt,
		want:        []string{"1.2.3.4:3000", certTxt},
	}, {
		name: "ok no cert",
		cmd:  "getfee",
		args: []string{"1.2.3.4:3000"},
		want: []string{"1.2.3.4:3000"},
	}, {
		name: "not a readCerts command",
		cmd:  "not a real command",
	}, {
		name:    "no file at path",
		cmd:     "getfee",
		args:    []string{"1.2.3.4:3000", "./cert"},
		wantErr: true,
	}, {
		name:        "newwallet ok, with cfg file",
		cmd:         "newwallet",
		args:        []string{"42", "./w.conf"},
		txtFilePath: "./w.conf",
		txtToSave:   cfgTxt,
		want:        []string{"42", cfgTxt},
	}, {
		name: "newwallet ok, no cfg file",
		cmd:  "newwallet",
		args: []string{"42"},
		want: []string{"42"},
	}}
	for _, test := range tests {
		if test.txtFilePath != "" {
			saveTextToFile(test.txtToSave, test.txtFilePath)
		}
		err := readTextFile(test.cmd, test.args)
		os.Remove(test.txtFilePath)
		if err != nil {
			if test.wantErr {
				continue
			}
			t.Fatalf("unexepected error for %s: %v", test.name, err)
		} else if test.wantErr {
			t.Fatalf("expected error for test %s", test.name)
		}
		if !reflect.DeepEqual(test.want, test.args) {
			t.Fatalf("wanted %v but got %v for test %s", test.want, test.args, test.name)
		}
	}
}
