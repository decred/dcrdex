// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package main

import (
	"errors"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"

	flags "github.com/jessevdk/go-flags"

	"decred.org/dcrdex/client/rpcserver"
	"github.com/decred/dcrd/dcrutil/v3"
)

const (
	defaultRPCAddr        = "localhost:5757"
	defaultConfigFilename = "dexcctl.conf"
	defaultRPCCertFile    = "rpc.cert"
)

var (
	appDir            = dcrutil.AppDataDir("dexcctl", false)
	dexcAppDir        = dcrutil.AppDataDir("dexc", false)
	defaultConfigPath = filepath.Join(appDir, defaultConfigFilename)
)

// config defines the configuration options for dexcctl.
type config struct {
	ShowVersion  bool     `short:"V" long:"version" description:"Display version information and exit"`
	ListCommands bool     `short:"l" long:"listcommands" description:"List all of the supported commands and exit"`
	Config       string   `short:"C" long:"config" description:"Path to configuration file"`
	RPCUser      string   `short:"u" long:"rpcuser" description:"RPC username"`
	RPCPass      string   `short:"P" long:"rpcpass" default-mask:"-" description:"RPC password"`
	RPCAddr      string   `short:"a" long:"rpcaddr" description:"RPC server to connect to"`
	RPCCert      string   `short:"c" long:"rpccert" description:"RPC server certificate chain for validation"`
	PrintJSON    bool     `short:"j" long:"json" description:"Print json messages sent and received"`
	Proxy        string   `long:"proxy" description:"Connect via SOCKS5 proxy (eg. 127.0.0.1:9050)"`
	ProxyUser    string   `long:"proxyuser" description:"Username for proxy server"`
	ProxyPass    string   `long:"proxypass" default-mask:"-" description:"Password for proxy server"`
	PasswordArgs []string `short:"p" long:"passarg" description:"Password arguments to bypass stdin prompts."`
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

// configure parses command line options and a config file if present. Returns
// an instantiated *config, leftover command line arguments, and a bool that
// is true if there is nothing further to do (i.e. version was printed and we
// can exit), or a parsing error, in that order.
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
			// This line is printed below the help message.
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
		fmt.Println(rpcserver.ListCommands(false))
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
		// Check in ~/.dexcctl first.
		cfg.RPCCert = cleanAndExpandPath(filepath.Join(appDir, defaultRPCCertFile))
		if !fileExists(cfg.RPCCert) { // Then in ~/.dexc
			cfg.RPCCert = cleanAndExpandPath(filepath.Join(dexcAppDir, defaultRPCCertFile))
		}
	} else {
		// Handle environment variable and tilde expansion in the given path.
		cfg.RPCCert = cleanAndExpandPath(cfg.RPCCert)
	}

	if cfg.RPCAddr == "" {
		cfg.RPCAddr = defaultRPCAddr
	}

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
