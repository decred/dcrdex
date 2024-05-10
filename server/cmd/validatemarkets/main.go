package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"os/user"
	"path/filepath"

	"decred.org/dcrdex/dex"
	_ "decred.org/dcrdex/server/asset/importall"
	dexsrv "decred.org/dcrdex/server/dex"
	"github.com/decred/dcrd/dcrutil/v4"
)

const (
	defaultMarketsConfFilename = "markets.json"
)

var (
	defaultAppDataDir = dcrutil.AppDataDir("dcrdex", false)
	defaultConfigPath = filepath.Join(defaultAppDataDir, defaultMarketsConfFilename)
)

func main() {
	if err := mainErr(); err != nil {
		fmt.Fprint(os.Stderr, err, "\n")
		os.Exit(1)
	}
	os.Exit(0)
}

func mainErr() error {
	var cfgPath string
	var testnet, simnet, harness, debug bool
	flag.BoolVar(&testnet, "testnet", false, "use testnet")
	flag.BoolVar(&simnet, "simnet", false, "use simnet")
	flag.BoolVar(&harness, "harness", false, "use filepath for for simnet harness")
	flag.BoolVar(&debug, "debug", false, "extra logging")
	flag.StringVar(&cfgPath, "path", defaultConfigPath, "path to configuration file")
	flag.Parse()

	net := dex.Mainnet
	if testnet {
		if simnet || harness {
			return errors.New("can't select testnet and simnet (or harness) together")
		}
		net = dex.Testnet
	} else if simnet {
		if harness {
			return errors.New("can't select simnet and harness together")
		}
		net = dex.Simnet
	} else if harness {
		net = dex.Simnet
		u, _ := user.Current()
		cfgPath = filepath.Join(u.HomeDir, "dextest", "dcrdex", defaultMarketsConfFilename)
	}

	logLvl := dex.LevelInfo
	if debug {
		logLvl = dex.LevelDebug
	}
	log := dex.StdOutLogger("MS", logLvl, false)

	err := dexsrv.ValidateConfigFile(cfgPath, net, log)
	if err != nil {
		return err
	}
	return nil
}
