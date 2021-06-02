// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"strings"

	"decred.org/dcrdex/client/rpcserver"
	"decred.org/dcrdex/dex/encode"
	"decred.org/dcrdex/dex/msgjson"
	"decred.org/dcrdex/server/admin"
)

const (
	showHelpMessage = "Specify -h to show available options"
	listCmdMessage  = "Specify -l to list available commands"
)

var version = semver{major: 0, minor: 3, patch: 0}

// semver holds dexcctl's semver values.
type semver struct {
	major, minor, patch uint32
}

// String satisfies fmt.Stringer.
func (s semver) String() string {
	return fmt.Sprintf("%d.%d.%d", s.major, s.minor, s.patch)
}

func main() {
	// Create a context that is canceled when a shutdown signal is received.
	ctx := withShutdownCancel(context.Background())
	// Listen for interrupt signals (e.g. CTRL+C).
	go shutdownListener()
	if err := run(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
}

// promptPasswords is a map of routes to password prompts. Passwords are
// prompted in the order given.
var promptPasswords = map[string][]string{
	"cancel":     {"App password:"},
	"init":       {"Set new app password:"},
	"login":      {"App password:"},
	"newwallet":  {"App password:", "Wallet password:"},
	"openwallet": {"App password:"},
	"register":   {"App password:"},
	"trade":      {"App password:"},
	"withdraw":   {"App password:"},
}

// optionalTextFiles is a map of routes to arg index for routes that should read
// the text content of a file, where the file path _may_ be found in the route's
// cmd args at the specified index.
var optionalTextFiles = map[string]int{
	"getfee":    1,
	"register":  2,
	"newwallet": 1,
}

// promptPWs prompts for passwords on stdin and returns an error if prompting
// fails or a password is empty. Returns passwords as a slice of []byte. If
// cmdPWs is provided, the passwords will be drawn from cmdPWs instead of stdin
// prompts.
func promptPWs(ctx context.Context, cmd string, cmdPWs []string) ([]encode.PassBytes, error) {
	prompts, exists := promptPasswords[cmd]
	if !exists {
		return nil, nil
	}
	var err error
	pws := make([]encode.PassBytes, len(prompts))

	if len(cmdPWs) > 0 {
		if len(prompts) != len(cmdPWs) {
			return nil, fmt.Errorf("Wrong number of command-line passwords, expected %d, got %d. Prompts = %v", len(prompts), len(cmdPWs), prompts)
		}
		for i, strPW := range cmdPWs {
			pws[i] = encode.PassBytes(strPW)
		}
		return pws, nil
	}

	// Prompt for passwords one at a time.
	for i, prompt := range prompts {
		pws[i], err = admin.PasswordPrompt(ctx, prompt)
		if err != nil {
			return nil, err
		}
	}
	return pws, nil
}

// readTextFile reads the text content of the file whose path is specified at
// args' index as expected for cmd and sets the args value at the expected index
// to the file's text content. The passed args are modified.
func readTextFile(cmd string, args []string) error {
	fileArgIndx, readFile := optionalTextFiles[cmd]
	// Not an error if file path arg is not provided for optional file args.
	if !readFile || len(args) < fileArgIndx+1 || args[fileArgIndx] == "" {
		return nil
	}
	path := cleanAndExpandPath(args[fileArgIndx])
	if !fileExists(path) {
		return fmt.Errorf("no file found at %s", path)
	}
	fileContents, err := ioutil.ReadFile(path)
	if err != nil {
		return fmt.Errorf("error reading %s: %v", path, err)
	}
	args[fileArgIndx] = string(fileContents)
	return nil
}

func run(ctx context.Context) error {
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

	// Prompt for passwords.
	pws, err := promptPWs(ctx, args[0], cfg.PasswordArgs)
	if err != nil {
		return err
	}

	// Attempt to read TLS certificates.
	err = readTextFile(args[0], params)
	if err != nil {
		return err
	}

	payload := &rpcserver.RawParams{
		PWArgs: pws,
		Args:   params,
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
