// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package main

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"

	"decred.org/dcrdex/client/cmd/dexc/version"
	"decred.org/dcrdex/dex"
	"github.com/decred/dcrd/dcrutil/v3"
	flags "github.com/jessevdk/go-flags"
)

const (
	maxLogRolls        = 16
	defaultRPCCertFile = "rpc.cert"
	defaultRPCKeyFile  = "rpc.key"
	defaultMainnetHost = "127.0.0.1"
	defaultTestnetHost = "127.0.0.2"
	defaultSimnetHost  = "127.0.0.3"
	defaultRPCPort     = "5757"
	defaultWebPort     = "5758"
	configFilename     = "dexc.conf"
	defaultLogLevel    = "debug"
)

var (
	defaultApplicationDirectory = dcrutil.AppDataDir("dexc", false)
	defaultConfigPath           = filepath.Join(defaultApplicationDirectory, configFilename)
	logFilename, netDirectory   string
	logDirectory                string
	cfg                         *Config
)

// setNet sets the filepath for the network directory and some network specific
// files. It returns a suggested path for the database file.
func setNet(applicationDirectory, net string) string {
	netDirectory = filepath.Join(applicationDirectory, net)
	logDirectory = filepath.Join(netDirectory, "logs")
	logFilename = filepath.Join(logDirectory, "dexc.log")
	err := os.MkdirAll(netDirectory, 0700)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create net directory: %v\n", err)
		os.Exit(1)
	}
	err = os.MkdirAll(logDirectory, 0700)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create log directory: %v\n", err)
		os.Exit(1)
	}
	return filepath.Join(netDirectory, "dexc.db")
}

// defaultHostByNetwork accepts configured network and returns the network
// specific default host
func defaultHostByNetwork(network dex.Network) string {
	switch network {
	case dex.Testnet:
		return defaultTestnetHost
	case dex.Simnet:
		return defaultSimnetHost
	default:
		return defaultMainnetHost
	}
}

// Config is the configuration for the DEX client application.
type Config struct {
	AppData      string `long:"appdata" description:"Path to application directory."`
	Config       string `long:"config" description:"Path to an INI configuration file."`
	DBPath       string `long:"db" description:"Database filepath. Database will be created if it does not exist."`
	RPCOn        bool   `long:"rpc" description:"turn on the rpc server"`
	RPCAddr      string `long:"rpcaddr" description:"RPC server listen address"`
	RPCUser      string `long:"rpcuser" description:"RPC server user name"`
	RPCPass      string `long:"rpcpass" description:"RPC server password"`
	RPCCert      string `long:"rpccert" description:"RPC server certificate file location"`
	RPCKey       string `long:"rpckey" description:"RPC server key file location"`
	WebAddr      string `long:"webaddr" description:"HTTP server address"`
	NoWeb        bool   `long:"noweb" description:"disable the web server."`
	Testnet      bool   `long:"testnet" description:"use testnet"`
	Simnet       bool   `long:"simnet" description:"use simnet"`
	ReloadHTML   bool   `long:"reload-html" description:"Reload the webserver's page template with every request. For development purposes."`
	DebugLevel   string `long:"log" description:"Logging level {trace, debug, info, warn, error, critical}"`
	LocalLogs    bool   `long:"loglocal" description:"Use local time zone time stamps in log entries."`
	ShowVer      bool   `short:"V" long:"version" description:"Display version information and exit"`
	TorProxy     string `long:"torproxy" description:"Connect via TOR (eg. 127.0.0.1:9050)."`
	TorIsolation bool   `long:"torisolation" description:"Enable TOR circuit isolation."`
	Net          dex.Network
	CertHosts    []string
}

var defaultConfig = Config{
	AppData:    defaultApplicationDirectory,
	Config:     defaultConfigPath,
	DebugLevel: defaultLogLevel,
	CertHosts: []string{defaultTestnetHost, defaultSimnetHost,
		defaultMainnetHost},
}

// configure processes the application configuration.
func configure() (*Config, error) {
	// Pre-parse the command line options to see if an alternative config file
	// or the version flag was specified. Override any environment variables
	// with parsed command line flags.
	iniCfg := defaultConfig
	preCfg := iniCfg
	preParser := flags.NewParser(&preCfg, flags.HelpFlag|flags.PassDoubleDash)
	_, flagerr := preParser.Parse()

	if flagerr != nil {
		e, ok := flagerr.(*flags.Error)
		if !ok || e.Type != flags.ErrHelp {
			preParser.WriteHelp(os.Stderr)
		}
		if ok && e.Type == flags.ErrHelp {
			preParser.WriteHelp(os.Stdout)
			os.Exit(0)
		}
		return nil, flagerr
	}

	// Show the version and exit if the version flag was specified.
	if preCfg.ShowVer {
		fmt.Printf("%s version %s (Go version %s %s/%s)\n",
			version.AppName, version.Version(), runtime.Version(), runtime.GOOS, runtime.GOARCH)
		os.Exit(0)
	}

	// If the app directory has been changed, but the config file path hasn't,
	// reform the config file path with the new directory.
	if preCfg.AppData != defaultApplicationDirectory && preCfg.Config == defaultConfigPath {
		preCfg.Config = filepath.Join(preCfg.AppData, configFilename)
	}
	cfgPath := cleanAndExpandPath(preCfg.Config)

	// Load additional config from file.
	parser := flags.NewParser(&iniCfg, flags.Default)
	err := flags.NewIniParser(parser).ParseFile(cfgPath)
	if err != nil {
		if _, ok := err.(*os.PathError); !ok {
			fmt.Fprintln(os.Stderr, err)
			parser.WriteHelp(os.Stderr)
			return nil, err
		}
		// Missing file is not an error.
	}

	// Parse command line options again to ensure they take precedence.
	_, err = parser.Parse()
	if err != nil {
		if e, ok := err.(*flags.Error); !ok || e.Type != flags.ErrHelp {
			parser.WriteHelp(os.Stderr)
		}
		return nil, err
	}

	// Set the global *Config.
	cfg = &iniCfg

	if cfg.Simnet && cfg.Testnet {
		return nil, fmt.Errorf("simnet and testnet cannot both be specified")
	}

	var defaultDBPath string
	switch {
	case cfg.Testnet:
		cfg.Net = dex.Testnet
		defaultDBPath = setNet(preCfg.AppData, "testnet")
	case cfg.Simnet:
		cfg.Net = dex.Simnet
		defaultDBPath = setNet(preCfg.AppData, "simnet")
	default:
		cfg.Net = dex.Mainnet
		defaultDBPath = setNet(preCfg.AppData, "mainnet")
	}
	defaultHost := defaultHostByNetwork(cfg.Net)

	// If web or RPC server addresses not set, use network specific
	// defaults
	if cfg.WebAddr == "" {
		cfg.WebAddr = defaultHost + ":" + defaultWebPort
	}
	if cfg.RPCAddr == "" {
		cfg.RPCAddr = defaultHost + ":" + defaultRPCPort
	}

	if cfg.RPCCert == "" {
		cfg.RPCCert = filepath.Join(preCfg.AppData, defaultRPCCertFile)
	}

	if cfg.RPCKey == "" {
		cfg.RPCKey = filepath.Join(preCfg.AppData, defaultRPCKeyFile)
	}

	if cfg.DBPath == "" {
		cfg.DBPath = defaultDBPath
	}

	return cfg, nil
}

// cleanAndExpandPath expands environment variables and leading ~ in the passed
// path, cleans the result, and returns it.
func cleanAndExpandPath(path string) string {
	// NOTE: The os.ExpandEnv doesn't work with Windows cmd.exe-style
	// %VARIABLE%, but the variables can still be expanded via POSIX-style
	// $VARIABLE.
	path = os.ExpandEnv(path)

	if !strings.HasPrefix(path, "~") {
		return filepath.Clean(path)
	}

	// Expand initial ~ to the current user's home directory, or ~otheruser to
	// otheruser's home directory.  On Windows, both forward and backward
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
