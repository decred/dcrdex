// Copyright (c) 2020 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package main

import (
	"errors"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/decred/dcrd/dcrutil/v2"
	flags "github.com/jessevdk/go-flags"
)

const (
	defaultRPCAddr            = "localhost:5757"
	defaultConfigFilename     = "dexcctl.conf"
	defaultRPCCertFile        = "rpc.cert"
	defaultDexcConfigFilename = "dexc.conf"
)

var (
	appDir            = dcrutil.AppDataDir("dexc", false)
	defaultConfigPath = filepath.Join(appDir, defaultConfigFilename)
)

// listCommands categorizes and lists all of the usable commands along with
// their one-line usage.
func listCommands() {
	fmt.Println("TODO")
}

// config defines the configuration options for dexcctl.
type config struct {
	ShowVersion   bool   `short:"V" long:"version" description:"Display version information and exit"`
	ListCommands  bool   `short:"l" long:"listcommands" description:"List all of the supported commands and exit"`
	Config        string `short:"C" long:"config" description:"Path to configuration file"`
	RPCUser       string `short:"u" long:"rpcuser" description:"RPC username"`
	RPCPass       string `short:"P" long:"rpcpass" default-mask:"-" description:"RPC password"`
	RPCAddr       string `short:"a" long:"rpcaddr" description:"RPC server to connect to"`
	RPCCert       string `short:"c" long:"rpccert" description:"RPC server certificate chain for validation"`
	PrintJSON     bool   `short:"j" long:"json" description:"Print json messages sent and received"`
	NoTLS         bool   `long:"notls" description:"Disable TLS"`
	Proxy         string `long:"proxy" description:"Connect via SOCKS5 proxy (eg. 127.0.0.1:9050)"`
	ProxyUser     string `long:"proxyuser" description:"Username for proxy server"`
	ProxyPass     string `long:"proxypass" default-mask:"-" description:"Password for proxy server"`
	TLSSkipVerify bool   `long:"skipverify" description:"Do not verify tls certificates (not recommended!)"`
}

// fileExists reports whether the named file or directory exists.
func fileExists(name string) bool {
	if _, err := os.Stat(name); err != nil {
		if os.IsNotExist(err) {
			return false
		}
	}
	return true
}

// configure parses command line options and a config file if present.
func configure() (*config, []string, bool, error) {
	stop := true
	cfg := &config{
		Config: defaultConfigPath,
	}
	preParser := flags.NewParser(cfg, flags.HelpFlag)
	_, err := preParser.Parse()
	if err != nil {
		var flagErr *flags.Error
		if errors.As(err, &flagErr) && flagErr.Type == flags.ErrHelp {
			fmt.Printf("%v\nThe special parameter `-` indicates that a parameter should be read from the\nnext unread line from standard input.\n", err)
			return nil, nil, stop, nil
		}
		return nil, nil, false, err
	}

	// Show the version and exit if the version flag was specified.
	appName := filepath.Base(os.Args[0])
	appName = strings.TrimSuffix(appName, filepath.Ext(appName))
	if cfg.ShowVersion {
		fmt.Printf("%s version %s (Go version %s %s/%s)\n", appName,
			version, runtime.Version(), runtime.GOOS, runtime.GOARCH)
		return nil, nil, stop, nil
	}

	// Show the available commands and exit if the associated flag was
	// specified.
	if cfg.ListCommands {
		listCommands()
		return nil, nil, stop, nil
	}

	parser := flags.NewParser(cfg, flags.Default)

	if fileExists(cfg.Config) {
		// Load additional config from file.
		err = flags.NewIniParser(parser).ParseFile(cfg.Config)
		if err != nil {
			return nil, nil, false, err
		}
	}

	// Parse command line options again to ensure they take precedence.
	remainingArgs, err := parser.Parse()
	if err != nil {
		return nil, nil, false, err
	}

	if cfg.RPCCert == "" {
		cfg.RPCCert = filepath.Join(appDir, defaultRPCCertFile)
	}

	if cfg.RPCAddr == "" {
		cfg.RPCAddr = defaultRPCAddr
	}

	// Handle environment variable expansion in the RPC certificate path.
	cfg.RPCCert = cleanAndExpandPath(cfg.RPCCert)

	return cfg, remainingArgs, false, nil
}

// cleanAndExpandPath expands environment variables and leading ~ in the
// passed path, cleans the result, and returns it.
func cleanAndExpandPath(path string) string {
	// Nothing to do when no path is given.
	if path == "" {
		return path
	}

	// NOTE: The os.ExpandEnv doesn't work with Windows cmd.exe-style
	// %VARIABLE%, but the variables can still be expanded via POSIX-style
	// $VARIABLE.
	path = os.ExpandEnv(path)

	if !strings.HasPrefix(path, "~") {
		return filepath.Clean(path)
	}

	// Expand initial ~ to the current user's home directory, or ~otheruser
	// to otheruser's home directory.  On Windows, both forward and backward
	// slashes can be used.
	path = path[1:]

	var pathSeparators string
	if runtime.GOOS == "windows" {
		pathSeparators = string(os.PathSeparator) + "/"
	} else {
		pathSeparators = string(os.PathSeparator)
	}

	userName := ""
	if i := strings.IndexAny(path, pathSeparators); i != -1 {
		userName = path[:i]
		path = path[i:]
	}

	homeDir := ""
	var u *user.User
	var err error
	if userName == "" {
		u, err = user.Current()
	} else {
		u, err = user.Lookup(userName)
	}
	if err == nil {
		homeDir = u.HomeDir
	}
	// Fallback to CWD if user lookup fails or user has no home directory.
	if homeDir == "" {
		homeDir = "."
	}

	return filepath.Join(homeDir, path)
}
