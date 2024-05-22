// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package main

import (
	"fmt"
	"os"
	"runtime"

	"decred.org/dcrdex/client/app"
)

// Config is the configuration for the DEX client application.
type Config struct {
	app.Config
	Kill      bool   `long:"kill" description:"Send a kill signal to a running instance and exit. This is not be supported on darwin"`
	LogStdout bool   `long:"stdout" description:"Log to stdout (in addition to the log file)"`
	Webview   string `long:"webview" description:"Opens a webview window pointing to the provided URL but not supported on darwin. Does nothing else. Precludes applicability of any other settings."`
}

func configure() (*Config, error) {
	// Pre-parse the command line options for one of a few CLI-only settings;
	// --appdata, --config, --help, or --version.
	cliCfg := Config{
		Config: app.DefaultConfig,
	}
	if err := app.ParseCLIConfig(&cliCfg); err != nil {
		return nil, err
	}

	// Show the version and exit if the version flag was specified.
	if cliCfg.ShowVer {
		fmt.Printf("%s version %s (Go version %s %s/%s)\n",
			appName, app.Version, runtime.Version(), runtime.GOOS, runtime.GOARCH)
		os.Exit(0)
	}

	appData, configPath := app.ResolveCLIConfigPaths(&cliCfg.Config)

	// The full config.
	cfg := Config{
		Config: app.DefaultConfig,
	}

	// Load additional config from file. CLI settings are reparsed to override
	// any settings parsed from file.
	if err := app.ParseFileConfig(configPath, &cfg); err != nil {
		return nil, err
	}

	// Resolve unset fields.
	return &cfg, app.ResolveConfig(appData, &cfg.Config)
}
