// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"decred.org/dcrdex/client/rpcserver"
	"decred.org/dcrdex/dex/msgjson"
)

const (
	showHelpMessage = "Specify -h to show available options"
	listCmdMessage  = "Specify -l to list available commands"
)

var version = semver{major: 0, minor: 0, patch: 0}

// semver holds dexcctl's semver values.
type semver struct {
	major, minor, patch uint32
}

// String satifies fmt.Stringer
func (s semver) String() string {
	return fmt.Sprintf("%d.%d.%d", s.major, s.minor, s.patch)
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, args, stop, err := configure()
	if err != nil {
		return fmt.Errorf("unable to configure: %v", err)
	}

	if stop {
		return nil
	}

	if len(args) < 1 {
		return fmt.Errorf("no command specified\n%s", listCmdMessage)
	}

	// Convert remaining command line args to a slice of interface values
	// to be passed along as parameters to new command creation function.
	//
	// Support using '-' as an argument to allow the argument to be read
	// from a stdin pipe.
	bio := bufio.NewReader(os.Stdin)
	params := make([]string, 0, len(args[1:]))
	for _, arg := range args[1:] {
		if arg == "-" {
			param, err := bio.ReadString('\n')
			if err != nil && err != io.EOF {
				return fmt.Errorf("Failed to read data from stdin: %v", err)
			}
			if err == io.EOF && len(param) == 0 {
				return errors.New("Not enough lines provided on stdin")
			}
			param = strings.TrimRight(param, "\r\n")
			params = append(params, param)
			continue
		}

		params = append(params, arg)
	}

	// Parse the arguments and convert into a type the server accepts.
	payload, err := rpcserver.ParseCmdArgs(args[0], params)
	if err != nil {
		if errors.Is(err, rpcserver.ErrArgs) {
			// This is a known command. Ignoring unreachable error.
			usage, _ := rpcserver.CommandUsage(args[0])
			return fmt.Errorf("%v\n\n%s", err, usage)
		}
		return err
	}

	// Create a request using the parsedArgs.
	msg, err := msgjson.NewRequest(1, args[0], payload)
	if err != nil {
		return fmt.Errorf("unable to create request: %v", err)
	}

	// Marshal the command into a JSON-RPC byte slice in preparation for
	// sending it to the RPC server.
	marshalledJSON, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("unable to marshal message: %v", err)
	}

	// Send the JSON-RPC request to the server using the user-specified
	// connection configuration.
	respMsg, err := sendPostRequest(marshalledJSON, cfg)
	if err != nil {
		return fmt.Errorf("unable to send request: %v", err)
	}

	// Retrieve the payload from the response.
	resp, err := respMsg.Response()
	if err != nil {
		return fmt.Errorf("unable to unmarshal response payload: %v", err)
	}

	if resp.Error != nil {
		return errors.New(resp.Error.Message)
	}

	// Choose how to display the result based on its type.
	strResult := string(resp.Result)
	if strings.HasPrefix(strResult, "{") || strings.HasPrefix(strResult, "[") {
		var dst bytes.Buffer
		if err := json.Indent(&dst, resp.Result, "", "  "); err != nil {
			return fmt.Errorf("failed to format result: %v", err)
		}
		fmt.Println(dst.String())
	} else if strings.HasPrefix(strResult, `"`) {
		var str string
		if err := json.Unmarshal(resp.Result, &str); err != nil {
			return fmt.Errorf("failed to unmarshal result: %v", err)
		}
		fmt.Println(str)
	} else if strResult != "null" {
		fmt.Println(strResult)
	}
	return nil
}
