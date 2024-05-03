// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"

	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/fiatrates"
	"decred.org/dcrdex/server/comms"
	"decred.org/dcrdex/tatanka"
	"github.com/jessevdk/go-flags"
	"github.com/jrick/logrotate/rotator"
)

const (
	Version               = 0
	defaultConfigFilename = "tatanka.conf"
	defaultHost           = "127.0.0.1"
	defaultPort           = "7232"
	missingPort           = "missing port in address"
	defaultHSHost         = defaultHost // should be a loopback address
	defaultHSPort         = "7252"
)

var (
	log              = dex.Disabled
	subsystemLoggers = map[string]dex.Logger{
		"MAIN": dex.Disabled,
	}
)

func main() {
	if err := mainErr(); err != nil {
		fmt.Fprint(os.Stderr, err, "\n")
		os.Exit(1)
	}
	os.Exit(0)
}

func mainErr() (err error) {
	logMaker, cfg := config()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	killChan := make(chan os.Signal, 1)
	signal.Notify(killChan, os.Interrupt)
	go func() {
		<-killChan
		fmt.Println("Shutting down...")
		cancel()
	}()

	net := dex.Mainnet
	switch {
	case cfg.Simnet:
		net = dex.Simnet
	case cfg.Testnet:
		net = dex.Testnet
	}

	t, err := tatanka.New(&tatanka.Config{
		Net:        net,
		DataDir:    cfg.AppDataDir,
		Logger:     logMaker.Logger("🦬"),
		ConfigPath: cfg.ConfigFile,
		RPC: comms.RPCConfig{
			HiddenServiceAddr: cfg.HiddenService,
			ListenAddrs:       cfg.Listeners,
			RPCKey:            cfg.KeyPath,
			RPCCert:           cfg.CertPath,
			NoTLS:             cfg.NoTLS,
			AltDNSNames:       cfg.AltDNSNames,
		},
	})
	if err != nil {
		return fmt.Errorf("tatanka.New error: %w", err)
	}

	tc := dex.NewConnectionMaster(t)
	if err = tc.ConnectOnce(ctx); err != nil {
		return fmt.Errorf("ConnectOnce error: %w", err)
	}

	tc.Wait()
	return nil
}

type Config struct {
	AppDataDir  string `short:"A" long:"appdata" description:"Path to application home directory."`
	ConfigFile  string `short:"C" long:"configfile" description:"Path to configuration file."`
	DebugLevel  string `short:"d" long:"debuglevel" description:"Logging level {trace, debug, info, warn, error, critical}."`
	LocalLogs   bool   `long:"loglocal" description:"Use local time zone time stamps in log entries."`
	ShowVersion bool   `short:"v" long:"version" description:"Display version information and exit."`

	Testnet bool `long:"testnet" description:"Use the test network (default mainnet)."`
	Simnet  bool `long:"simnet" description:"Use the simulation test network (default mainnet)."`

	CertPath      string   `long:"tlscert" description:"TLS certificate file."`
	KeyPath       string   `long:"tlskey" description:"TLS private key file."`
	Listeners     []string `long:"listen" description:"IP addresses on which the RPC server should listen for incoming connections."`
	NoTLS         bool     `long:"notls" description:"Run without TLS encryption."`
	AltDNSNames   []string `long:"altdnsnames" description:"A list of hostnames to include in the RPC certificate (X509v3 Subject Alternative Name)."`
	HiddenService string   `long:"hiddenservice" description:"A host:port on which the RPC server should listen for incoming hidden service connections. No TLS is used for these connections."`

	WebAddr string `long:"webaddr" description:"The public facing address by which peers should connect."`

	FiatOracleConfig fiatrates.Config `group:"Fiat Oracle Config"`
}

func config() (*dex.LoggerMaker, *Config) {
	emitConfigError := func(s string, a ...interface{}) {
		fmt.Fprintln(os.Stderr, fmt.Errorf(s, a...))
		os.Exit(0)
	}

	cfg := Config{}
	var preCfg Config // zero values as defaults
	preParser := flags.NewParser(&preCfg, flags.HelpFlag)
	_, err := preParser.Parse()
	if err != nil {
		if e, ok := err.(*flags.Error); ok && e.Type != flags.ErrHelp {
			emitConfigError("flag pre-parse error: %v", err)
		} else if ok && e.Type == flags.ErrHelp {
			fmt.Fprintln(os.Stdout, err)
			os.Exit(0)
		}
	}

	// Show the version and exit if the version flag was specified.
	if preCfg.ShowVersion {
		fmt.Printf("Tatanka version %d (Go version %s %s/%s)\n",
			Version, runtime.Version(), runtime.GOOS, runtime.GOARCH)
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
			emitConfigError("Unable to determine working directory: %v", err)
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
	parser := flags.NewParser(&cfg, flags.Default)
	// Do not error default config file is missing.
	if _, err := os.Stat(preCfg.ConfigFile); os.IsNotExist(err) {
		// Non-default config file must exist.
		if !isDefaultConfigFile {
			emitConfigError("error characterizing file %q: %v", preCfg.ConfigFile, err)
		}
		// Warn about missing default config file, but continue.
		fmt.Printf("Config file (%s) does not exist. Using defaults.\n",
			preCfg.ConfigFile)
	} else {
		// The config file exists, so attempt to parse it.
		err = flags.NewIniParser(parser).ParseFile(preCfg.ConfigFile)
		if err != nil {
			if _, ok := err.(*os.PathError); !ok {
				emitConfigError("INI file parsing error: %v", err)
			}
		}
		configFile = preCfg.ConfigFile
	}

	// Parse command line options again to ensure they take precedence.
	_, err = parser.Parse()
	if err != nil {
		if e, ok := err.(*flags.Error); !ok || e.Type != flags.ErrHelp {
			parser.WriteHelp(os.Stderr)
		}
		emitConfigError("CLI re-parsing error: %v", err)
	}

	if len(cfg.Listeners) == 0 {
		cfg.Listeners = []string{defaultHost + ":" + defaultPort}
	}
	for i := range cfg.Listeners {
		listen, err := normalizeNetworkAddress(cfg.Listeners[i], defaultHost, defaultPort)
		if err != nil {
			emitConfigError(err.Error())
		}
		cfg.Listeners[i] = listen
	}
	if cfg.HiddenService != "" {
		cfg.HiddenService, err = normalizeNetworkAddress(cfg.HiddenService, defaultHSHost, defaultHSPort)
		if err != nil {
			emitConfigError(err.Error())
		}
	}

	// Create the loggers: Parse and validate the debug level string, create the
	// subsystem loggers, and set package level loggers. The generated
	// LoggerMaker is used by other subsystems to create new loggers with the
	// same backend.
	logMaker, err := parseAndSetDebugLevels(cfg.DebugLevel, !cfg.LocalLogs)
	if err != nil {
		emitConfigError("parseAndSetDebugLevels error: %v", err)
	}

	initLogRotator(filepath.Join(cfg.AppDataDir, "logs"))

	log.Infof("App data folder: %s", cfg.AppDataDir)
	log.Infof("Config file:     %s", configFile)

	return logMaker, &cfg
}

// parseAndSetDebugLevels attempts to parse the specified debug level and set
// the levels accordingly. An appropriate error is returned if anything is
// invalid.
func parseAndSetDebugLevels(debugLevel string, UTC bool) (*dex.LoggerMaker, error) {
	// Create a LoggerMaker with the level string.
	lm, err := dex.NewLoggerMaker(logWriter{}, debugLevel, UTC)
	if err != nil {
		return nil, err
	}

	// Create subsystem loggers.
	for subsysID := range subsystemLoggers {
		subsystemLoggers[subsysID] = lm.Logger(subsysID)
	}

	// Set main's Logger.
	log = subsystemLoggers["MAIN"]

	return lm, nil
}

// logWriter implements an io.Writer that outputs to both standard output and
// the write-end pipe of an initialized log rotator.
type logWriter struct {
	logRotator *rotator.Rotator
}

// Write writes the data in p to standard out and the log rotator.
func (w logWriter) Write(p []byte) (n int, err error) {
	if w.logRotator == nil {
		return os.Stdout.Write(p)
	}
	os.Stdout.Write(p)
	return w.logRotator.Write(p) // not safe concurrent writes, so only one logWriter{} allowed!
}

// initLogRotator initializes the logging rotater to write logs to logFile and
// create roll files in the same directory.  It must be called before the
// package-global log rotater variables are used.
func initLogRotator(dir string) (*rotator.Rotator, error) {
	logFile := filepath.Join(dir, "tatanka.log")
	err := os.MkdirAll(dir, 0700)
	if err != nil {
		return nil, err
	}
	const maxLogRolls = 5
	return rotator.New(logFile, 32*1024, false, maxLogRolls)
}

// normalizeNetworkAddress checks for a valid local network address format and
// adds default host and port if not present. Invalidates addresses that include
// a protocol identifier.
func normalizeNetworkAddress(a, defaultHost, defaultPort string) (string, error) {
	if strings.Contains(a, "://") {
		return a, fmt.Errorf("address %s contains a protocol identifier, which is not allowed", a)
	}
	if a == "" {
		return net.JoinHostPort(defaultHost, defaultPort), nil
	}
	host, port, err := net.SplitHostPort(a)
	if err != nil {
		var addrErr *net.AddrError
		if errors.As(err, &addrErr) && addrErr.Err == missingPort {
			host = strings.Trim(addrErr.Addr, "[]") // JoinHostPort expects no brackets for ipv6 hosts
			normalized := net.JoinHostPort(host, defaultPort)
			host, port, err = net.SplitHostPort(normalized)
			if err != nil {
				return a, fmt.Errorf("unable to address %s after port resolution: %w", normalized, err)
			}
		} else {
			return a, fmt.Errorf("unable to normalize address %s: %w", a, err)
		}
	}
	if host == "" {
		host = defaultHost
	}
	if port == "" {
		port = defaultPort
	}
	return net.JoinHostPort(host, port), nil
}
