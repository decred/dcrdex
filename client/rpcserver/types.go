// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package rpcserver

import "fmt"

//RPC and Websocket

// VersionResult holds a semver version JSON object
type VersionResult struct {
	Major uint32 `json:"major"`
	Minor uint32 `json:"minor"`
	Patch uint32 `json:"patch"`
}

// ParseCmdArgs parses arguments to commands for rpcserver requests.
func ParseCmdArgs(cmd string, args []interface{}) (interface{}, error) {
	parse, exists := parsers[cmd]
	if !exists {
		return nil, fmt.Errorf("unknown command: %s", cmd)
	}
	return parse(args)
}

// parsers is a map of commands to parsing functions.
var parsers = map[string](func([]interface{}) (interface{}, error)){
	"help": func(args []interface{}) (interface{}, error) {

		if len(args) > 1 {
			return nil, fmt.Errorf("too many arguments: wanted 1 but got %d", len(args))
		} else if len(args) == 0 {
			return nil, nil
		}
		return args[0], nil
	},
	"version": func(args []interface{}) (interface{}, error) {
		if len(args) > 0 {
			return nil, fmt.Errorf("too many arguments: wanted 0 but got %d", len(args))
		}
		return nil, nil
	},
}
