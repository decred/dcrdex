// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package main

import (
	"errors"
	"fmt"
	"path/filepath"
	"runtime"

	flags "github.com/jessevdk/go-flags"

	"decred.org/dcrdex/client/rpcserver"
	"decred.org/dcrdex/dex"
	"github.com/decred/dcrd/dcrutil/v4"
)

const (
	defaultRPCPort        = "5757"
	defaultMainnetHost    = "127.0.0.1"
	defaultTestnetHost    = "127.0.0.2"
	defaultSimnetHost     = "127.0.0.3"
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
	Testnet      bool     `long:"testnet" description:"use testnet"`
	Simnet       bool     `long:"simnet" description:"use simnet"`
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
	preParser := flags.NewParser(cfg, flags.HelpFlag|flags.PassDoubleDash|flags.PassAfterNonOption)
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
	if cfg.ShowVersion {
		fmt.Printf("%s version %s (Go version %s %s/%s)\n", appName,
			Version, runtime.Version(), runtime.GOOS, runtime.GOARCH)
		fmt.Printf("%s required RPC server version: %s\n", appName, requiredRPCServerVersion.String())
		return nil, nil, stop, nil
	}

	// Show the available commands and exit if the associated flag was
	// specified.
	if cfg.ListCommands {
		fmt.Println(rpcserver.ListCommands(false))
		return nil, nil, stop, nil
	}

	parser := flags.NewParser(cfg, flags.Default|flags.PassAfterNonOption)

	if dex.FileExists(cfg.Config) {
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
		cfg.RPCCert = dex.CleanAndExpandPath(filepath.Join(appDir, defaultRPCCertFile))
		if !dex.FileExists(cfg.RPCCert) { // Then in ~/.dexc
			cfg.RPCCert = dex.CleanAndExpandPath(filepath.Join(dexcAppDir, defaultRPCCertFile))
		}
	} else {
		// Handle environment variable and tilde expansion in the given path.
		cfg.RPCCert = dex.CleanAndExpandPath(cfg.RPCCert)
	}

	if cfg.Simnet && cfg.Testnet {
		return nil, nil, false, fmt.Errorf("simnet and testnet cannot both be specified")
	}

	if cfg.RPCAddr == "" {
		var rpcHost string
		switch {
		case cfg.Testnet:
			rpcHost = defaultTestnetHost
		case cfg.Simnet:
			rpcHost = defaultSimnetHost
		default:
			rpcHost = defaultMainnetHost
		}
		cfg.RPCAddr = rpcHost + ":" + defaultRPCPort
	}

	return cfg, remainingArgs, false, nil
}
