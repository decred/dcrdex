// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package rpcserver

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"decred.org/dcrdex/dex/msgjson"
)

const (
	helpRoute    = "help"
	versionRoute = "version"
)

// createResponse creates a msgjson response payload.
func createResponse(op string, res interface{}, resErr *msgjson.Error) *msgjson.ResponsePayload {
	encodedRes, err := json.Marshal(res)
	if err != nil {
		err := fmt.Errorf("unable to marshal data for %s: %v", op, err)
		panic(err)
	}
	return &msgjson.ResponsePayload{Result: encodedRes, Error: resErr}
}

// routes maps routes to a handler function.
var routes = map[string]func(s *RPCServer, req *msgjson.Message) *msgjson.ResponsePayload{
	helpRoute:    handleHelp,
	versionRoute: handleVersion,
}

// handleHelp handles requests for help. Returns general help for all commands
// if no arguments are passed or verbose help if the passed argument is a known
// command.
func handleHelp(s *RPCServer, req *msgjson.Message) *msgjson.ResponsePayload {
	reqPayload := ""
	err := json.Unmarshal(req.Payload, &reqPayload)
	if err != nil {
		resErr := &msgjson.Error{Code: msgjson.RPCParseError, Message: "unable to unmarshal request"}
		return createResponse(req.Route, nil, resErr)
	}
	res := ""
	if reqPayload == "" {
		// List all commands if no arguments.
		res = ListCommands()
	} else {
		res, err = CommandUsage(reqPayload)
		if err != nil {
			resErr := &msgjson.Error{Code: msgjson.RPCUnknownRoute, Message: err.Error()}
			return createResponse(req.Route, nil, resErr)
		}
	}
	return createResponse(req.Route, &res, nil)
}

// handleVersion handles requests for version. It takes no arguments and returns
// the semver.
func handleVersion(s *RPCServer, req *msgjson.Message) *msgjson.ResponsePayload {
	res := &versionResponse{
		Major: rpcSemverMajor,
		Minor: rpcSemverMinor,
		Patch: rpcSemverPatch,
	}
	return createResponse(req.Route, res.String(), nil)
}

// ListCommands prints a short usage string for every route available to the
// rpcserver.
func ListCommands() string {
	var sb strings.Builder
	var err error
	for _, r := range sortHelpKeys() {
		_, err = sb.WriteString(fmt.Sprintf("%s %s\n", r, helpMsgs[r][0]))
		if err != nil {
			log.Errorf("unable to parse help message for %s", r)
			return ""
		}
	}
	return sb.String()
}

// CommandUsage returns a help message for cmd or an error if cmd is unknown.
func CommandUsage(cmd string) (string, error) {
	msg, exists := helpMsgs[cmd]
	if !exists {
		return "", fmt.Errorf("%w: %s", ErrUnknownCmd, cmd)
	}
	return fmt.Sprintf("%s %s\n\n%s", cmd, msg[0], msg[1]), nil
}

// sortHelpKeys returns a sorted list of helpMsgs keys.
func sortHelpKeys() []string {
	keys := make([]string, 0, len(helpMsgs))
	for k := range helpMsgs {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		return keys[i] < keys[j]
	})
	return keys
}

// helpMsgs are a map of routes to help messages. The first string is a short
// description of usage. The second is a brief description and a breakdown of
// the arguments and return values.
var helpMsgs = map[string][2]string{
	helpRoute: [2]string{`("cmd")`,
		`Print a help message.

Args:
	cmd (string): Optional. The command to print help for.

Returns:
	string: The help message for command.`,
	},
	versionRoute: [2]string{``,
		`Print the dex client rpcserver version.

Returns:
	string: The dex client rpcserver version.`,
	},
}
