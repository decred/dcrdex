// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package main

import (
	"fmt"
	"io/ioutil"
	"net"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"decred.org/dcrdex/dex"
	"github.com/decred/dcrd/dcrec/secp256k1/v2"
	"github.com/decred/dcrd/dcrutil/v2"
	flags "github.com/jessevdk/go-flags"
)

const (
	defaultConfigFilename      = "dcrdex.conf"
	defaultLogFilename         = "dcrdex.log"
	defaultRPCCertFilename     = "rpc.cert"
	defaultRPCKeyFilename      = "rpc.key"
	defaultDataDirname         = "data"
	defaultLogLevel            = "info"
	defaultLogDirname          = "logs"
	defaultMarketsConfFilename = "markets.json"
	defaultMaxLogZips          = 16
	defaultPGHost              = "127.0.0.1:5432"
	defaultPGUser              = "dcrdex"
	defaultPGDBName            = "dcrdex"
	defaultDEXPrivKeyFilename  = "dexprivkey"
	defaultRPCHost             = "127.0.0.1"
	defaultRPCPort             = "7232"

	defaultCancelThresh     = 0.6
	defaultRegFeeConfirms   = 4
	defaultRegFeeAmount     = 1e8
	defaultBroadcastTimeout = time.Minute
)

var (
	defaultAppDataDir = dcrutil.AppDataDir("dcrdex", false)
)

type procOpts struct {
	HTTPProfile bool
	CPUProfile  string
}

// dexConf is the data that is required to setup the dex.
type dexConf struct {
	Network          dex.Network
	DBName           string
	DBUser           string
	DBPass           string
	DBHost           string
	DBPort           uint16
	MarketsConfPath  string
	RegFeeXPub       string
	RegFeeConfirms   int64
	RegFeeAmount     uint64
	CancelThreshold  float64
	DEXPrivKey       *secp256k1.PrivateKey
	RPCCert          string
	RPCKey           string
	RPCListen        []string
	BroadcastTimeout time.Duration
	AltDNSNames      []string
	LogMaker         *dex.LoggerMaker
}

type flagsData struct {
	// General application behavior
	AppDataDir  string `short:"A" long:"appdata" description:"Path to application home directory"`
	ConfigFile  string `short:"C" long:"configfile" description:"Path to configuration file"`
	DataDir     string `short:"b" long:"datadir" description:"Directory to store data"`
	LogDir      string `long:"logdir" description:"Directory to log output."`
	DebugLevel  string `short:"d" long:"debuglevel" description:"Logging level {trace, debug, info, warn, error, critical}"`
	MaxLogZips  int    `long:"maxlogzips" description:"The number of zipped log files created by the log rotator to be retained. Setting to 0 will keep all."`
	ShowVersion bool   `short:"V" long:"version" description:"Display version information and exit"`

	Testnet bool `long:"testnet" description:"Use the test network (default mainnet)"`
	Simnet  bool `long:"simnet" description:"Use the simulation test network (default mainnet)"`

	RPCCert     string   `long:"rpccert" description:"RPC server TLS certificate file"`
	RPCKey      string   `long:"rpckey" description:"RPC server TLS private key file"`
	RPCListen   []string `long:"rpclisten" description:"IP addresses on which the RPC server should listen for incoming connections"`
	AltDNSNames []string `long:"altdnsnames" description:"A list of hostnames to include in the RPC certificate (X509v3 Subject Alternative Name)"`

	MarketsConfPath  string        `long:"marketsconfpath" description:"Path to the markets configuration JSON file."`
	BroadcastTimeout time.Duration `long:"bcasttimeout" description:"How long clients have to broadcast expected swap transactions following new blocks"`
	RegFeeXPub       string        `long:"regfeexpub" description:"The extended public key for deriving Decred addresses to which DEX registration fees should be paid."`
	RegFeeConfirms   int64         `long:"regfeeconfirms" description:"The number of confirmations required to consider a registration fee paid."`
	RegFeeAmount     uint64        `long:"regfeeamount" description:"The registration fee amount in atoms."`
	CancelThreshold  float64       `long:"cancelthresh" description:"Cancellation ratio threshold (cancels/completed)."`
	DEXPrivKeyPath   string        `long:"dexprivkeypath" description:"The path to a file containing the DEX private key for message signing."`

	HTTPProfile bool   `long:"httpprof" short:"p" description:"Start HTTP profiler."`
	CPUProfile  string `long:"cpuprofile" description:"File for CPU profiling."`

	PGDBName     string `long:"pgdbname" description:"PostgreSQL DB name."`
	PGUser       string `long:"pguser" description:"PostgreSQL DB user."`
	PGPass       string `long:"pgpass" description:"PostgreSQL DB password."`
	PGHost       string `long:"pghost" description:"PostgreSQL server host:port or UNIX socket (e.g. /run/postgresql)."`
	HidePGConfig bool   `long:"hidepgconfig" description:"Blocks logging of the PostgreSQL db configuration on system start up."`
}

// cleanAndExpandPath expands environment variables and leading ~ in the passed
// path, cleans the result, and returns it.
func cleanAndExpandPath(path string) string {
	// Do not try to clean the empty string
	if path == "" {
		return ""
	}

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

// supportedSubsystems returns a sorted slice of the supported subsystems for
// logging purposes.
func supportedSubsystems() []string {
	// Convert the subsystemLoggers map keys to a slice.
	subsystems := make([]string, 0, len(subsystemLoggers))
	for subsysID := range subsystemLoggers {
		subsystems = append(subsystems, subsysID)
	}

	// Sort the subsystems for stable display.
	sort.Strings(subsystems)
	return subsystems
}

// parseAndSetDebugLevels attempts to parse the specified debug level and set
// the levels accordingly.  An appropriate error is returned if anything is
// invalid.
func parseAndSetDebugLevels(debugLevel string) (*dex.LoggerMaker, error) {
	lm, err := dex.NewLoggerMaker(backendLog, debugLevel)
	if err != nil {
		return nil, err
	}
	setLogLevels(lm.DefaultLevel)
	for subsysID, lvl := range lm.Levels {
		if _, exists := subsystemLoggers[subsysID]; !exists {
			str := "The specified subsystem [%v] is invalid -- " +
				"supported subsystems %v"
			return nil, fmt.Errorf(str, subsysID, supportedSubsystems())
		}
		setLogLevel(subsysID, lvl)
	}
	return lm, nil
}

// normalizeNetworkAddress checks for a valid local network address format and
// adds default host and port if not present. Invalidates addresses that include
// a protocol identifier.
func normalizeNetworkAddress(a, defaultHost, defaultPort string) (string, error) {
	if strings.Contains(a, "://") {
		return a, fmt.Errorf("Address %s contains a protocol identifier, which is not allowed", a)
	}
	if a == "" {
		return defaultHost + ":" + defaultPort, nil
	}
	host, port, err := net.SplitHostPort(a)
	if err != nil {
		if strings.Contains(err.Error(), "missing port in address") {
			normalized := a + ":" + defaultPort
			host, port, err = net.SplitHostPort(normalized)
			if err != nil {
				return a, fmt.Errorf("Unable to address %s after port resolution: %v", normalized, err)
			}
		} else {
			return a, fmt.Errorf("Unable to normalize address %s: %v", a, err)
		}
	}
	if host == "" {
		host = defaultHost
	}
	if port == "" {
		port = defaultPort
	}
	return host + ":" + port, nil
}

// loadConfig initializes and parses the config using a config file and command
// line options.
func loadConfig() (*dexConf, *procOpts, error) {
	loadConfigError := func(err error) (*dexConf, *procOpts, error) {
		return nil, nil, err
	}

	// Default config
	cfg := flagsData{
		AppDataDir: defaultAppDataDir,
		// Defaults for ConfigFile, LogDir, and DataDir are set relative to
		// AppDataDir. They are not to be set here.
		MaxLogZips:       defaultMaxLogZips,
		RPCCert:          defaultRPCCertFilename,
		RPCKey:           defaultRPCKeyFilename,
		DebugLevel:       defaultLogLevel,
		PGDBName:         defaultPGDBName,
		PGUser:           defaultPGUser,
		PGHost:           defaultPGHost,
		MarketsConfPath:  defaultMarketsConfFilename,
		DEXPrivKeyPath:   defaultDEXPrivKeyFilename,
		RegFeeConfirms:   defaultRegFeeConfirms,
		RegFeeAmount:     defaultRegFeeAmount,
		BroadcastTimeout: defaultBroadcastTimeout,
		CancelThreshold:  defaultCancelThresh,
	}

	// Pre-parse the command line options to see if an alternative config file
	// or the version flag was specified. Any errors aside from the help message
	// error can be ignored here since they will be caught by the final parse
	// below.
	var preCfg flagsData // zero values as defaults
	preParser := flags.NewParser(&preCfg, flags.HelpFlag)
	_, err := preParser.Parse()
	if err != nil {
		if e, ok := err.(*flags.Error); ok && e.Type != flags.ErrHelp {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		} else if ok && e.Type == flags.ErrHelp {
			fmt.Fprintln(os.Stdout, err)
			os.Exit(0)
		}
	}

	// Show the version and exit if the version flag was specified.
	if preCfg.ShowVersion {
		fmt.Printf("dcrdex version %s (Go version %s %s/%s)\n",
			Version(), runtime.Version(), runtime.GOOS, runtime.GOARCH)
		os.Exit(0)
	}

	// Special show command to list supported subsystems and exit.
	if preCfg.DebugLevel == "show" {
		fmt.Println("Supported subsystems", supportedSubsystems())
		os.Exit(0)
	}

	// If a non-default appdata folder is specified on the command line, it may
	// be necessary adjust the config file location. If the the config file
	// location was not specified on the command line, the default location
	// should be under the non-default appdata directory. However, if the config
	// file was specified on the command line, it should be used regardless of
	// the appdata directory.
	if preCfg.AppDataDir != "" {
		// appdata was set on the command line. If it is not absolute, make it
		// relative to cwd.
		cfg.AppDataDir, err = filepath.Abs(preCfg.AppDataDir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Unable to determine working directory: %v", err)
			os.Exit(1)
		}
	}
	isDefaultConfigFile := preCfg.ConfigFile == ""
	if isDefaultConfigFile {
		preCfg.ConfigFile = filepath.Join(cfg.AppDataDir, defaultConfigFilename)
	} else if !filepath.IsAbs(preCfg.ConfigFile) {
		preCfg.ConfigFile = filepath.Join(cfg.AppDataDir, preCfg.ConfigFile)
	}

	// Config file name for logging.
	configFile := "NONE (defaults)"

	// Load additional config from file.
	var configFileError error
	parser := flags.NewParser(&cfg, flags.Default)
	// Do not error default config file is missing.
	if _, err := os.Stat(preCfg.ConfigFile); os.IsNotExist(err) {
		// Non-default config file must exist.
		if !isDefaultConfigFile {
			fmt.Fprintln(os.Stderr, err)
			return loadConfigError(err)
		}
		// Warn about missing default config file, but continue.
		fmt.Printf("Config file (%s) does not exist. Using defaults.\n",
			preCfg.ConfigFile)
	} else {
		// The config file exists, so attempt to parse it.
		err = flags.NewIniParser(parser).ParseFile(preCfg.ConfigFile)
		if err != nil {
			if _, ok := err.(*os.PathError); !ok {
				fmt.Fprintln(os.Stderr, err)
				parser.WriteHelp(os.Stderr)
				return loadConfigError(err)
			}
			configFileError = err
		}
		configFile = preCfg.ConfigFile
	}

	// Parse command line options again to ensure they take precedence.
	_, err = parser.Parse()
	if err != nil {
		if e, ok := err.(*flags.Error); !ok || e.Type != flags.ErrHelp {
			parser.WriteHelp(os.Stderr)
		}
		return loadConfigError(err)
	}

	// Warn about missing config file after the final command line parse
	// succeeds. This prevents the warning on help messages and invalid options.
	if configFileError != nil {
		fmt.Printf("%v\n", configFileError)
		return loadConfigError(configFileError)
	}

	// Select the network.
	var numNets int
	network := dex.Mainnet
	if cfg.Testnet {
		numNets++
		network = dex.Testnet
	}
	if cfg.Simnet {
		numNets++
		network = dex.Simnet
	}
	if numNets > 1 {
		err := fmt.Errorf("both testnet and simnet flags specified")
		fmt.Fprintln(os.Stderr, err)
		return loadConfigError(err)
	}

	// Create the app data directory if it doesn't already exist.
	err = os.MkdirAll(cfg.AppDataDir, 0700)
	if err != nil {
		// Show a nicer error message if it's because a symlink is linked to a
		// directory that does not exist (probably because it's not mounted).
		if e, ok := err.(*os.PathError); ok && os.IsExist(err) {
			if link, lerr := os.Readlink(e.Path); lerr == nil {
				str := "is symlink %s -> %s mounted?"
				err = fmt.Errorf(str, e.Path, link)
			}
		}

		err := fmt.Errorf("failed to create home directory: %v", err)
		fmt.Fprintln(os.Stderr, err)
		return loadConfigError(err)
	}

	// If datadir or logdir are defaults or non-default relative paths, prepend
	// the appdata directory.
	if cfg.DataDir == "" {
		cfg.DataDir = filepath.Join(cfg.AppDataDir, defaultDataDirname)
	} else if !filepath.IsAbs(cfg.DataDir) {
		cfg.DataDir = filepath.Join(cfg.AppDataDir, cfg.DataDir)
	}
	if cfg.LogDir == "" {
		cfg.LogDir = filepath.Join(cfg.AppDataDir, defaultLogDirname)
	} else if !filepath.IsAbs(cfg.LogDir) {
		cfg.LogDir = filepath.Join(cfg.AppDataDir, cfg.LogDir)
	}

	// Append the network type to the data directory so it is "namespaced" per
	// network.  In addition to the block database, there are other pieces of
	// data that are saved to disk such as address manager state. All data is
	// specific to a network, so namespacing the data directory means each
	// individual piece of serialized data does not have to worry about changing
	// names per network and such.
	//
	// Make list of old versions of testnet directories here since the network
	// specific DataDir will be used after this.
	cfg.DataDir = cleanAndExpandPath(cfg.DataDir)
	cfg.DataDir = filepath.Join(cfg.DataDir, network.String())
	// Create the data folder if it does not exist.
	err = os.MkdirAll(cfg.DataDir, 0700)
	if err != nil {
		return loadConfigError(err)
	}

	logRotator = nil
	// Append the network type to the log directory so it is "namespaced"
	// per network in the same fashion as the data directory.
	cfg.LogDir = cleanAndExpandPath(cfg.LogDir)
	cfg.LogDir = filepath.Join(cfg.LogDir, network.String())

	// Ensure that all specified files are absolute paths, prepending the
	// appdata path if not.
	if !filepath.IsAbs(cfg.RPCCert) {
		cfg.RPCCert = filepath.Join(cfg.AppDataDir, cfg.RPCCert)
	}
	if !filepath.IsAbs(cfg.RPCKey) {
		cfg.RPCKey = filepath.Join(cfg.AppDataDir, cfg.RPCKey)
	}
	if !filepath.IsAbs(cfg.MarketsConfPath) {
		cfg.MarketsConfPath = filepath.Join(cfg.AppDataDir, cfg.MarketsConfPath)
	}
	if !filepath.IsAbs(cfg.DEXPrivKeyPath) {
		cfg.DEXPrivKeyPath = filepath.Join(cfg.AppDataDir, cfg.DEXPrivKeyPath)
	}

	// Validate each RPC listen host:port.
	var RPCListen []string
	if len(cfg.RPCListen) == 0 {
		RPCListen = []string{defaultRPCHost + ":" + defaultRPCPort}
	}
	for i := range cfg.RPCListen {
		listen, err := normalizeNetworkAddress(cfg.RPCListen[i], defaultRPCHost, defaultRPCPort)
		if err != nil {
			return loadConfigError(err)
		}
		RPCListen = append(RPCListen, listen)
	}

	// Initialize log rotation. After log rotation has been initialized, the
	// logger variables may be used. This creates the LogDir if needed.
	if cfg.MaxLogZips < 0 {
		cfg.MaxLogZips = 0
	}
	initLogRotator(filepath.Join(cfg.LogDir, defaultLogFilename), cfg.MaxLogZips)

	log.Infof("App data folder: %s", cfg.AppDataDir)
	//log.Infof("Data folder:    %s", cfg.DataDir) // TODO: remove DataDir if dcrdex does not need it
	log.Infof("Log folder:      %s", cfg.LogDir)
	log.Infof("Config file:     %s", configFile)

	// Parse, validate, and set debug log level(s).
	logMaker, err := parseAndSetDebugLevels(cfg.DebugLevel)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		parser.WriteHelp(os.Stderr)
		return loadConfigError(err)
	}

	var dbPort uint16
	dbHost := cfg.PGHost
	// For UNIX sockets, do not attempt to parse out a port.
	if !strings.HasPrefix(dbHost, "/") {
		var dbPortStr string
		dbHost, dbPortStr, err = net.SplitHostPort(cfg.PGHost)
		if err != nil {
			return loadConfigError(fmt.Errorf("invalid DB host %q: %v", cfg.PGHost, err))
		}
		port, err := strconv.ParseUint(dbPortStr, 10, 16)
		if err != nil {
			return loadConfigError(fmt.Errorf("invalid DB port %q: %v", dbPortStr, err))
		}
		dbPort = uint16(port)
	}

	// Load the DEX signing key. TODO: Implement a secure key storage scheme.
	pkFileBuffer, err := ioutil.ReadFile(cfg.DEXPrivKeyPath)
	if err != nil {
		return loadConfigError(fmt.Errorf("unable to read DEX private key file %s: %v",
			cfg.DEXPrivKeyPath, err))
	}
	privKey, _ := secp256k1.PrivKeyFromBytes(pkFileBuffer)

	dexCfg := &dexConf{
		Network:          network,
		DBName:           cfg.PGDBName,
		DBHost:           dbHost,
		DBPort:           dbPort,
		DBUser:           cfg.PGUser,
		DBPass:           cfg.PGPass,
		MarketsConfPath:  cfg.MarketsConfPath,
		RegFeeAmount:     cfg.RegFeeAmount,
		RegFeeConfirms:   cfg.RegFeeConfirms,
		RegFeeXPub:       cfg.RegFeeXPub,
		CancelThreshold:  cfg.CancelThreshold,
		DEXPrivKey:       privKey,
		RPCCert:          cfg.RPCCert,
		RPCKey:           cfg.RPCKey,
		RPCListen:        RPCListen,
		BroadcastTimeout: cfg.BroadcastTimeout,
		AltDNSNames:      cfg.AltDNSNames,
		LogMaker:         logMaker,
	}

	opts := &procOpts{
		CPUProfile:  cfg.CPUProfile,
		HTTPProfile: cfg.HTTPProfile,
	}

	return dexCfg, opts, nil
}
