// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package rpcserver

import (
	"testing"
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
				t.Fatalf("expected error for test %s", test.name)
			}
			continue
		}
		if err != nil {
			t.Fatalf("unexpected error for test %s: %v", test.name, err)
		}
		if res != test.want {
			t.Fatalf("got %v but want %v for test %s", res, test.want, test.name)
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
				t.Fatalf("expected error for test %s", test.name)
			}
			continue
		}
		if err != nil {
			t.Fatalf("unexpected error for test %s: %v", test.name, err)
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
