// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package ui

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"

	"decred.org/dcrdex/dex"
	"github.com/decred/dcrd/dcrutil/v2"
	flags "github.com/jessevdk/go-flags"
)

const (
	maxLogRolls    = 16
	defaultRPCAddr = "http://localhost:5757"
	defaultWebAddr = "http://localhost:5758"
	// The default config filename. The {netname} will be replaced by the string
	// representation of the network specified on the command line.
	configFilename = "dexc_{netname}.conf"
	defaultLogDir  = "logs"
	defaultLogName = "dex.log"
)

var (
	applicationDirectory      = dcrutil.AppDataDir("dexclient", false)
	logDirectory, logFilename string
	defaultConfigPath         string
	cfg                       *Config
)

// setSubPaths sets the log file path and default configuration file path based
// on the supplied application directory path.
func setSubPaths(appDir string) {
	logDirectory = filepath.Join(appDir, defaultLogDir)
	logFilename = filepath.Join(logDirectory, defaultLogName)
	defaultConfigPath = filepath.Join(appDir, configFilename)
}

// Config is the application configuration. Arguments can be supplied by
// command line, or by INI configuration file.
type Config struct {
	DataDir string `long:"dir" description:"Path to application directory"`
	Config  string `long:"config" description:"Path to an INI configuration file. default: [home]/dexc_{network}.conf"`
	RPCOn   bool   `long:"rpc" description:"turn on the rpc server"`
	RPCAddr string `long:"rpcaddr" description:"RPCServer listen address"`
	WebOn   bool   `long:"web" description:"turn on the web server"`
	WebAddr string `long:"webaddr" description:"HTTP server address"`
	NoTUI   bool   `long:"notui" description:"disable the terminal-based user interface. must be used with --rpc or --web"`
	// Testnet OR Simnet should be specified at the command line if the {network}
	// tag is used to version the configuration filename.
	Testnet bool `long:"testnet" description:"use testnet"`
	Simnet  bool `long:"simnet" description:"use simnet"`
}

var defaultConfig = Config{
	DataDir: applicationDirectory,
	RPCAddr: defaultRPCAddr,
	WebAddr: defaultWebAddr,
}

// Configure creates and returns the application configuration struct. The
// global cfg variable is also set.
func Configure() (*Config, error) {
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

	setSubPaths(preCfg.DataDir)
	if preCfg.Config == "" {
		preCfg.Config = filepath.Join(preCfg.DataDir, configFilename)
	}
	cfgPath := cleanAndExpandPath(preCfg.Config)
	cfgPath = strings.Replace(cfgPath, "{netname}", netFromConfig(&preCfg).String(), -1)

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

	if iniCfg.Simnet && iniCfg.Testnet {
		return nil, fmt.Errorf("simnet and testnet cannot both be specified")
	}
	cfg = &iniCfg
	return cfg, nil
}

// netFromConfig parses the dex.Network from the configuration.
func netFromConfig(c *Config) dex.Network {
	switch {
	case c.Testnet:
		return dex.Testnet
	case c.Simnet:
		return dex.Simnet
	default:
		return dex.Mainnet
	}
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
