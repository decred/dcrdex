package main

import (
	"fmt"
	"os"
	"runtime"

	"decred.org/dcrdex/client/app"
)

func configure() (*app.Config, error) {
	// Pre-parse the command line options to see if an alternative config file
	// or the version flag was specified. Override any environment variables
	// with parsed command line flags.
	iniCfg := app.DefaultConfig
	preCfg := iniCfg
	if err := app.ParseCLIConfig(&preCfg); err != nil {
		return nil, err
	}

	// Show the version and exit if the version flag was specified.
	if preCfg.ShowVer {
		fmt.Printf("%s version %s (Go version %s %s/%s)\n",
			appName, app.Version, runtime.Version(), runtime.GOOS, runtime.GOARCH)
		os.Exit(0)
	}

	appData, configPath := app.ResolveCLIConfigPaths(&preCfg)

	// Load additional config from file.
	if err := app.ParseFileConfig(configPath, &iniCfg); err != nil {
		return nil, err
	}

	cfg := &iniCfg
	return cfg, app.ResolveConfig(appData, cfg)
}
