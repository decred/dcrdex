// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package rpcserver

import (
	"errors"
	"fmt"
)

var (
	// ErrArgs is wrapped when arguments to the known command cannot be parsed.
	ErrArgs = errors.New("unable to parse arguments")
	// ErrUnknownCmd is wrapped when the command is not know.
	ErrUnknownCmd = errors.New("unknown command")
)

// versionResponse holds a semver version JSON object.
type versionResponse struct {
	Major uint32 `json:"major"`
	Minor uint32 `json:"minor"`
	Patch uint32 `json:"patch"`
}

// String satisfies the Stringer interface.
func (vr versionResponse) String() string {
	return fmt.Sprintf("%d.%d.%d", vr.Major, vr.Minor, vr.Patch)
}

// preRegisterResponse is used when responding to the preregister route.
type preRegisterResponse struct {
	Fee uint64 `json:"fee"`
}

// ParseCmdArgs parses arguments to commands for rpcserver requests.
func ParseCmdArgs(cmd string, args []string) (interface{}, error) {
	nArg, exists := nArgs[cmd]
	if !exists {
		return nil, fmt.Errorf("%w: %s", ErrUnknownCmd, cmd)
	}
	if err := checkNArgs(len(args), nArg); err != nil {
		return nil, err
	}
	return parsers[cmd](args)
}

// nArgs is a map of routes to the number of arguments accepted. One integer
// indicates an exact match, two are the min and max.
var nArgs = map[string][]int{
	helpRoute:        []int{0, 1},
	versionRoute:     []int{0},
	preRegisterRoute: []int{1},
}

// parsers is a map of commands to parsing functions.
var parsers = map[string](func([]string) (interface{}, error)){
	helpRoute:        parseHelpArgs,
	versionRoute:     func([]string) (interface{}, error) { return nil, nil },
	preRegisterRoute: func(args []string) (interface{}, error) { return args[0], nil },
}

func checkNArgs(have int, want []int) error {
	if len(want) == 1 {
		if want[0] != have {
			return fmt.Errorf("%w: wanted %d but got %d", ErrArgs, want[0], have)
		}
	} else {
		if have < want[0] || have > want[1] {
			return fmt.Errorf("%w: wanted between %d and %d but got %d", ErrArgs, want[0], want[1], have)
		}
	}
	return nil
}

func parseHelpArgs(args []string) (interface{}, error) {
	if len(args) == 0 {
		return nil, nil
	}
	return args[0], nil
}
