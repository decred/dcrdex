// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package app

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"decred.org/dcrdex/client/core"
	"decred.org/dcrdex/client/mm"
	"decred.org/dcrdex/client/rpcserver"
	"decred.org/dcrdex/client/webserver"
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/version"
	"github.com/decred/dcrd/dcrutil/v4"
	"github.com/jessevdk/go-flags"
)

const (
	defaultRPCCertFile = "rpc.cert"
	defaultRPCKeyFile  = "rpc.key"
	defaultMainnetHost = "127.0.0.1"
	defaultTestnetHost = "127.0.0.2"
	defaultSimnetHost  = "127.0.0.3"
	walletPairOneHost  = "127.0.0.6"
	walletPairTwoHost  = "127.0.0.7"
	defaultRPCPort     = "5757"
	defaultWebPort     = "5758"
	defaultLogLevel    = "debug"
	configFilename     = "dexc.conf"
)

var (
	defaultApplicationDirectory = dcrutil.AppDataDir("dexc", false)
	defaultConfigPath           = filepath.Join(defaultApplicationDirectory, configFilename)
)

// RPCConfig encapsulates the configuration needed for the RPC server.
type RPCConfig struct {
	RPCAddr string `long:"rpcaddr" description:"RPC server listen address"`
	RPCUser string `long:"rpcuser" description:"RPC server user name"`
	RPCPass string `long:"rpcpass" description:"RPC server password"`
	RPCCert string `long:"rpccert" description:"RPC server certificate file location"`
	RPCKey  string `long:"rpckey" description:"RPC server key file location"`
	// CertHosts is a list of hosts given to certgen.NewTLSCertPair for the
	// "Subject Alternate Name" values of the generated TLS certificate. It is
	// set automatically, not via the config file or cli args.
	CertHosts []string
}

// RPC creates a rpc server configuration.
func (cfg *RPCConfig) RPC(c *core.Core, marketMaker *mm.MarketMaker, log dex.Logger) *rpcserver.Config {
	bwMajor, bwMinor, bwPatch, bwPreRel, bwBuildMeta, err := version.ParseSemVer(Version)
	if err != nil {
		panic(fmt.Errorf("failed to parse version: %w", err))
	}

	runtimeVer := strings.Replace(runtime.Version(), ".", "-", -1)
	runBuildMeta := version.NormalizeString(runtimeVer)
	build := version.NormalizeString(bwBuildMeta)
	if build != "" {
		bwBuildMeta = fmt.Sprintf("%s.%s", build, runBuildMeta)
	}
	bwVersion := &rpcserver.SemVersion{
		VersionString: Version,
		Major:         bwMajor,
		Minor:         bwMinor,
		Patch:         bwPatch,
		Prerelease:    bwPreRel,
		BuildMetadata: bwBuildMeta,
	}

	rpcserver.SetLogger(log)
	return &rpcserver.Config{
		Core:        c,
		MarketMaker: marketMaker,
		Addr:        cfg.RPCAddr,
		User:        cfg.RPCUser,
		Pass:        cfg.RPCPass,
		Cert:        cfg.RPCCert,
		Key:         cfg.RPCKey,
		BWVersion:   bwVersion,
		CertHosts: []string{
			defaultTestnetHost, defaultSimnetHost, defaultMainnetHost,
			walletPairOneHost, walletPairTwoHost,
		},
	}
}

// CoreConfig encapsulates the settings specific to core.Core.
type CoreConfig struct {
	DBPath       string `long:"db" description:"Database filepath. Database will be created if it does not exist."`
	Onion        string `long:"onion" description:"Proxy for .onion addresses, if torproxy not set (eg. 127.0.0.1:9050)."`
	TorProxy     string `long:"torproxy" description:"Connect via TOR (eg. 127.0.0.1:9050)."`
	TorIsolation bool   `long:"torisolation" description:"Enable TOR circuit isolation."`
	// Net is a derivative field set by ResolveConfig.
	Net dex.Network

	TheOneHost string `long:"onehost" description:"Only connect with this server."`

	NoAutoWalletLock   bool `long:"no-wallet-lock" description:"Disable locking of wallets on shutdown or logout. Use this if you want your external wallets to stay unlocked after closing the DEX app."`
	NoAutoDBBackup     bool `long:"no-db-backup" description:"Disable creation of a database backup on shutdown."`
	UnlockCoinsOnLogin bool `long:"release-wallet-coins" description:"On login or wallet creation, instruct the wallet to release any coins that it may have locked."`

	ExtensionModeFile string `long:"extension-mode-file" description:"path to a file that specifies options for running core as an extension."`
}

// WebConfig encapsulates the configuration needed for the web server.
type WebConfig struct {
	WebAddr     string `long:"webaddr" description:"HTTP server address"`
	WebTLS      bool   `long:"webtls" description:"Use a self-signed certificate for HTTPS with the web server. This is implied for a publicly routable (not loopback or private subnet) webaddr. When changing webaddr, you mean need to delete web.cert and web.key."`
	SiteDir     string `long:"sitedir" description:"Path to the 'site' directory with packaged web files. Unspecified = default is good in most cases."`
	NoEmbedSite bool   `long:"no-embed-site" description:"Use on-disk UI files instead of embedded resources. This also reloads the html template with every request. For development purposes."`
	HTTPProfile bool   `long:"httpprof" description:"Start HTTP profiler on /pprof."`
	// Deprecated
	Experimental bool `long:"experimental" description:"DEPRECATED: Enable experimental features"`
	Tor          bool `long:"tor" description:"Enable tor hidden service"`
}

// LogConfig encapsulates the logging-related settings.
type LogConfig struct {
	LogPath    string `long:"logpath" description:"A file to save app logs"`
	DebugLevel string `long:"log" description:"Logging level {trace, debug, info, warn, error, critical}"`
	LocalLogs  bool   `long:"loglocal" description:"Use local time zone time stamps in log entries."`
}

// MMConfig encapsulates the settings specific to market making.
type MMConfig struct {
	BotConfigPath  string `long:"botConfigPath"`
	EventLogDBPath string `long:"eventLogDBPath"`
}

// Config is the common application configuration definition. This composite
// struct captures the configuration needed for core and both web and rpc
// servers, as well as some application-level directives.
type Config struct {
	CoreConfig
	RPCConfig
	WebConfig
	LogConfig
	MMConfig
	// AppData and ConfigPath should be parsed from the command-line,
	// as it makes no sense to set these in the config file itself. If no values
	// are assigned, defaults will be used.
	AppData    string `long:"appdata" description:"Path to application directory."`
	ConfigPath string `long:"config" description:"Path to an INI configuration file."`
	// Testnet and Simnet are used to set the derivative CoreConfig.Net
	// dex.Network field.
	Testnet    bool   `long:"testnet" description:"use testnet"`
	Simnet     bool   `long:"simnet" description:"use simnet"`
	RPCOn      bool   `long:"rpc" description:"turn on the rpc server"`
	NoWeb      bool   `long:"noweb" description:"disable the web server."`
	CPUProfile string `long:"cpuprofile" description:"File for CPU profiling."`
	ShowVer    bool   `short:"V" long:"version" description:"Display version information and exit"`
	Language   string `long:"lang" description:"BCP 47 tag for preferred language, e.g. en-GB, fr, zh-CN"`
}

// Web creates a configuration for the webserver. This is a Config method
// instead of a WebConfig method because Language is an app-level setting used
// by both core and rpcserver.
func (cfg *Config) Web(c *core.Core, mm *mm.MarketMaker, log dex.Logger, utc bool) *webserver.Config {
	addr := cfg.WebAddr
	host, _, err := net.SplitHostPort(addr)
	if err == nil && host != "" {
		addr = host
	} else {
		// If SplitHostPort failed, IPv6 addresses may still have brackets.
		addr = strings.Trim(addr, "[]")
	}
	ip := net.ParseIP(addr)

	var mmCore webserver.MMCore
	if mm != nil {
		mmCore = mm
	}

	var certFile, keyFile string
	if cfg.WebTLS || (ip != nil && !ip.IsLoopback() && !ip.IsPrivate()) || (ip == nil && addr != "localhost") {
		certFile = filepath.Join(cfg.AppData, "web.cert")
		keyFile = filepath.Join(cfg.AppData, "web.key")
	}

	return &webserver.Config{
		DataDir:         filepath.Join(cfg.AppData, "srv"),
		Core:            c,
		MarketMaker:     mmCore,
		Addr:            cfg.WebAddr,
		CustomSiteDir:   cfg.SiteDir,
		Logger:          log,
		UTC:             utc,
		CertFile:        certFile,
		KeyFile:         keyFile,
		Language:        cfg.Language,
		Tor:             cfg.Tor,
		MainLogFilePath: cfg.LogPath,
	}
}

// Core creates a core.Core configuration. This is a Config method
// instead of a CoreConfig method because Language is an app-level setting used
// by both core and rpcserver.
func (cfg *Config) Core(log dex.Logger) *core.Config {
	return &core.Config{
		DBPath:             cfg.DBPath,
		Net:                cfg.Net,
		Logger:             log,
		Onion:              cfg.Onion,
		TorProxy:           cfg.TorProxy,
		TorIsolation:       cfg.TorIsolation,
		Language:           cfg.Language,
		UnlockCoinsOnLogin: cfg.UnlockCoinsOnLogin,
		NoAutoWalletLock:   cfg.NoAutoWalletLock,
		NoAutoDBBackup:     cfg.NoAutoDBBackup,
		ExtensionModeFile:  cfg.ExtensionModeFile,
		TheOneHost:         cfg.TheOneHost,
	}
}

var DefaultConfig = Config{
	AppData:    defaultApplicationDirectory,
	ConfigPath: defaultConfigPath,
	LogConfig:  LogConfig{DebugLevel: defaultLogLevel},
	RPCConfig: RPCConfig{
		CertHosts: []string{defaultTestnetHost, defaultSimnetHost, defaultMainnetHost},
	},
}

// ParseCLIConfig parses the command-line arguments into the provided struct
// with go-flags tags. If the --help flag has been passed, the struct is
// described back to the terminal and the program exits using os.Exit.
func ParseCLIConfig(cfg any) error {
	preParser := flags.NewParser(cfg, flags.HelpFlag|flags.PassDoubleDash)
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
		return flagerr
	}
	return nil
}

// ResolveCLIConfigPaths resolves the app data directory path and the
// configuration file path from the CLI config, (presumably parsed with
// ParseCLIConfig).
func ResolveCLIConfigPaths(cfg *Config) (appData, configPath string) {
	// If the app directory has been changed, replace shortcut chars such
	// as "~" with the full path.
	if cfg.AppData != defaultApplicationDirectory {
		cfg.AppData = dex.CleanAndExpandPath(cfg.AppData)
		// If the app directory has been changed, but the config file path hasn't,
		// reform the config file path with the new directory.
		if cfg.ConfigPath == defaultConfigPath {
			cfg.ConfigPath = filepath.Join(cfg.AppData, configFilename)
		}
	}
	cfg.ConfigPath = dex.CleanAndExpandPath(cfg.ConfigPath)
	return cfg.AppData, cfg.ConfigPath
}

// ParseFileConfig parses the INI file into the provided struct with go-flags
// tags. The CLI args are then parsed, and take precedence over the file values.
func ParseFileConfig(path string, cfg any) error {
	parser := flags.NewParser(cfg, flags.Default)
	err := flags.NewIniParser(parser).ParseFile(path)
	if err != nil {
		if _, ok := err.(*os.PathError); !ok {
			fmt.Fprintln(os.Stderr, err)
			parser.WriteHelp(os.Stderr)
			return err
		}
		// Missing file is not an error.
	}

	// Parse command line options again to ensure they take precedence.
	_, err = parser.Parse()
	if err != nil {
		if e, ok := err.(*flags.Error); !ok || e.Type != flags.ErrHelp {
			parser.WriteHelp(os.Stderr)
		}
		return err
	}
	return nil
}

// ResolveConfig sets derivative fields of the Config struct using the specified
// app data directory (presumably returned from ResolveCLIConfigPaths). Some
// unset values are given defaults.
func ResolveConfig(appData string, cfg *Config) error {
	if cfg.Simnet && cfg.Testnet {
		return fmt.Errorf("simnet and testnet cannot both be specified")
	}

	cfg.AppData = appData

	var defaultDBPath, defaultLogPath, defaultMMEventLogDBPath, defaultMMConfigPath string
	switch {
	case cfg.Testnet:
		cfg.Net = dex.Testnet
		defaultDBPath, defaultLogPath, defaultMMEventLogDBPath, defaultMMConfigPath = setNet(appData, "testnet")
	case cfg.Simnet:
		cfg.Net = dex.Simnet
		defaultDBPath, defaultLogPath, defaultMMEventLogDBPath, defaultMMConfigPath = setNet(appData, "simnet")
	default:
		cfg.Net = dex.Mainnet
		defaultDBPath, defaultLogPath, defaultMMEventLogDBPath, defaultMMConfigPath = setNet(appData, "mainnet")
	}
	defaultHost := DefaultHostByNetwork(cfg.Net)

	// If web or RPC server addresses not set, use network specific
	// defaults
	if cfg.WebAddr == "" {
		cfg.WebAddr = net.JoinHostPort(defaultHost, defaultWebPort)
	}
	if cfg.RPCAddr == "" {
		cfg.RPCAddr = net.JoinHostPort(defaultHost, defaultRPCPort)
	}

	if cfg.RPCCert == "" {
		cfg.RPCCert = filepath.Join(appData, defaultRPCCertFile)
	}

	if cfg.RPCKey == "" {
		cfg.RPCKey = filepath.Join(appData, defaultRPCKeyFile)
	}

	if cfg.DBPath == "" {
		cfg.DBPath = defaultDBPath
	}

	if cfg.LogPath == "" {
		cfg.LogPath = defaultLogPath
	}

	if cfg.MMConfig.BotConfigPath == "" {
		cfg.MMConfig.BotConfigPath = defaultMMConfigPath
	}

	if cfg.MMConfig.EventLogDBPath == "" {
		cfg.MMConfig.EventLogDBPath = defaultMMEventLogDBPath
	}
	return nil
}

// setNet sets the filepath for the network directory and some network specific
// files. It returns a suggested path for the database file and a log file. If
// using a file rotator, the directory of the log filepath as parsed  by
// filepath.Dir is suitable for use.
func setNet(applicationDirectory, net string) (dbPath, logPath, mmEventDBPath, mmCfgPath string) {
	netDirectory := filepath.Join(applicationDirectory, net)
	logDirectory := filepath.Join(netDirectory, "logs")
	logFilename := filepath.Join(logDirectory, "dexc.log")
	mmEventLogDBFilename := filepath.Join(netDirectory, "eventlog.db")
	mmCfgFilename := filepath.Join(netDirectory, "mm_cfg.json")
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
	return filepath.Join(netDirectory, "dexc.db"), logFilename, mmEventLogDBFilename, mmCfgFilename
}

// DefaultHostByNetwork accepts configured network and returns the network
// specific default host
func DefaultHostByNetwork(network dex.Network) string {
	switch network {
	case dex.Testnet:
		return defaultTestnetHost
	case dex.Simnet:
		return defaultSimnetHost
	default:
		return defaultMainnetHost
	}
}
