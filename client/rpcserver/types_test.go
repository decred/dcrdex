// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package rpcserver

import (
	"errors"
	"fmt"
	"testing"

	"decred.org/dcrdex/client/core"
)

func TestParseCmdArgs(t *testing.T) {
	tests := []struct {
		name, cmd string
		args      []string
		want      interface{}
		wantErr   bool
	}{{
		name:    "ok",
		cmd:     "help",
		args:    []string{"some command"},
		want:    "some command",
		wantErr: false,
	}, {
		name:    "route doesnt exist",
		cmd:     "never make this command",
		args:    []string{"some command"},
		wantErr: true,
	}, {
		name:    "wrong number of arguments",
		cmd:     "version",
		args:    []string{"some command"},
		wantErr: true,
	}}
	for _, test := range tests {
		res, err := ParseCmdArgs(test.cmd, test.args)
		if test.wantErr {
			if err == nil {
				t.Fatalf("expected error for test %s",
					test.name)
			}
			continue
		}
		if err != nil {
			t.Fatalf("unexpected error for test %s: %v",
				test.name, err)
		}
		if res != test.want {
			t.Fatalf("got %v but want %v for test %s",
				res, test.want, test.name)
		}
	}
}

func TestNArgs(t *testing.T) {
	// routes and nArgs must have the same keys.
	if len(routes) != len(nArgs) {
		t.Fatal("routes and nArgs have different number of routes")
	}
	for k := range routes {
		if _, exists := nArgs[k]; !exists {
			t.Fatalf("%v exists in routes but not in nArgs", k)
		}
	}
}

func TestCheckNArgs(t *testing.T) {
	tests := []struct {
		name      string
		have      int
		wantNArgs []int
		wantErr   bool
	}{{
		name:      "ok exact",
		have:      3,
		wantNArgs: []int{3},
		wantErr:   false,
	}, {
		name:      "ok between",
		have:      3,
		wantNArgs: []int{2, 4},
		wantErr:   false,
	}, {
		name:      "ok lower",
		have:      2,
		wantNArgs: []int{2, 4},
		wantErr:   false,
	}, {
		name:      "ok upper",
		have:      4,
		wantNArgs: []int{2, 4},
		wantErr:   false,
	}, {
		name:      "not exact",
		have:      3,
		wantNArgs: []int{2},
		wantErr:   true,
	}, {
		name:      "too few",
		have:      2,
		wantNArgs: []int{3, 5},
		wantErr:   true,
	}, {
		name:      "too many",
		have:      7,
		wantNArgs: []int{2, 5},
		wantErr:   true,
	}}
	for _, test := range tests {
		err := checkNArgs(test.have, test.wantNArgs)
		if test.wantErr {
			if err == nil {
				t.Fatalf("expected error for test %s",
					test.name)
			}
			continue
		}
		if err != nil {
			t.Fatalf("unexpected error for test %s: %v",
				test.name, err)
		}
	}
}

func TestParsers(t *testing.T) {
	// routes and parsers must have the same keys.
	if len(routes) != len(parsers) {
		t.Fatal("routes and parsers have different number of routes")
	}
	for k := range routes {
		if _, exists := parsers[k]; !exists {
			t.Fatalf("%v exists in routes but not in parsers", k)
		}
	}
}

func TestParseNewWalletArgs(t *testing.T) {
	argsWithAssetID := func(id string) []string {
		return []string{
			id,
			"default",
			"/home/wallet.conf",
			"password123",
			"password123",
		}
	}
	tests := []struct {
		name    string
		args    []string
		wantErr error
	}{{
		name: "ok",
		args: argsWithAssetID("42"),
	}, {
		name:    "assetID is not int",
		args:    argsWithAssetID("42.1"),
		wantErr: ErrArgs,
	}}
	for _, test := range tests {
		res, err := parseNewWalletArgs(test.args)
		if test.wantErr != nil {
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("unexpected error %v for test %s",
					err, test.name)
			}
			continue
		}
		nwf, ok := res.(*newWalletForm)
		if !ok {
			t.Fatal("result doesn't wrap *newWalletForm")
		}
		if fmt.Sprint(nwf.AssetID) != test.args[0] {
			t.Fatalf("assetID doesn't match")
		}
		if nwf.Account != test.args[1] {
			t.Fatalf("account doesn't match")
		}
		if nwf.INIPath != test.args[2] {
			t.Fatalf("inipath doesn't match")
		}
		if nwf.WalletPass != test.args[3] {
			t.Fatalf("walletPass doesn't match")
		}
		if nwf.AppPass != test.args[4] {
			t.Fatalf("appPass doesn't match")
		}
	}
}

func TestParseOpenWalletArgs(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantErr error
	}{{
		name: "ok",
		args: []string{"42", "password123"},
	}, {
		name:    "assetID is not int",
		args:    []string{"42.1", "password123"},
		wantErr: ErrArgs,
	}}
	for _, test := range tests {
		res, err := parseOpenWalletArgs(test.args)
		if test.wantErr != nil {
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("unexpected error %v for test %s",
					err, test.name)
			}
			continue
		}
		owf, ok := res.(*openWalletForm)
		if !ok {
			t.Fatal("result doesn't wrap *openWalletForm")
		}
		if fmt.Sprint(owf.AssetID) != test.args[0] {
			t.Fatalf("assetID doesn't match")
		}
		if owf.AppPass != test.args[1] {
			t.Fatalf("appPass doesn't match")
		}
	}
}

func TestCheckIntArg(t *testing.T) {
	tests := []struct {
		name    string
		arg     string
		wantErr error
	}{{
		name: "ok",
		arg:  "42",
	}, {
		name:    "assetID is not int",
		arg:     "42.1",
		wantErr: ErrArgs,
	}}
	for _, test := range tests {
		res, err := checkIntArg(test.arg, "name")
		if test.wantErr != nil {
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("unexpected error %v for test %s",
					err, test.name)
			}
			continue
		}
		if fmt.Sprint(res) != test.arg {
			t.Fatalf("strings don't match")
		}
	}
}

func TestParseRegisterArgs(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantErr error
	}{{
		name: "ok",
		args: []string{"dex", "password123", "1000"},
	}, {
		name:    "fee not int",
		args:    []string{"dex", "password123", "1000.0"},
		wantErr: ErrArgs,
	}}
	for _, test := range tests {
		res, err := parseRegisterArgs(test.args)
		if test.wantErr != nil {
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("unexpected error %v for test %s",
					err, test.name)
			}
			continue
		}
		reg, ok := res.(*core.Registration)
		if !ok {
			t.Fatal("result doesn't wrap *core.Registration")
		}
		if reg.DEX != test.args[0] {
			t.Fatalf("dex doesn't match")
		}
		if reg.Password != test.args[1] {
			t.Fatalf("appPass doesn't match")
		}
		if fmt.Sprint(reg.Fee) != test.args[2] {
			t.Fatalf("fee doesn't match")
		}
	}
}
