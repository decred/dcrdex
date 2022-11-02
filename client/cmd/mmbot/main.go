// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package main

/*
Create core, wallets and register for server using dexc before starting the bot.
*/

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"decred.org/dcrdex/client/core"
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/server/admin"

	_ "decred.org/dcrdex/client/asset/btc"
	_ "decred.org/dcrdex/client/asset/dcr"
)

var (
	userDir, _            = os.UserHomeDir()
	defaultConfigFileName = "mm.json"
	defaultCoreDir        = filepath.Join(userDir, ".dexc")
	defaultConfigFilePath = filepath.Join(defaultCoreDir, defaultConfigFileName)
)

func main() {
	if err := mainErr(); err != nil {
		fmt.Fprint(os.Stderr, err, "\n")
		os.Exit(1)
	}
	os.Exit(0)
}

func mainErr() error {
	var (
		c     *core.Core
		pgmID uint64

		// flags
		configFile, coreDir, lang, logLevel string
		testnet, simnet                     bool
		manualRate                          float64
	)

	flag.StringVar(&configFile, "config", defaultConfigFilePath, "the bot program to run")
	flag.StringVar(&coreDir, "coredir", defaultCoreDir, "the core configuration directory")
	flag.StringVar(&lang, "lang", "en-US", "language")
	flag.BoolVar(&testnet, "testnet", false, "use testnet")
	flag.BoolVar(&simnet, "simnet", false, "use simnet")
	flag.StringVar(&logLevel, "log", "info", "universal or per-logger log level. e.g. log=trace, or log=CORE=trace,BOT=trace")
	flag.Float64Var(&manualRate, "emptyrate", 0, "(optional) a rate to use if the book is empty an there are no oracles available")
	flag.Parse()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	loggerMaker, err := dex.NewLoggerMaker(os.Stdout, logLevel)
	if err != nil {
		return err
	}
	log := loggerMaker.NewLogger("MAIN")

	killChan := make(chan os.Signal, 1)
	signal.Notify(killChan, os.Interrupt)
	go func() {
		<-killChan
		if c != nil && pgmID > 0 {
			if err := c.RetireBot(pgmID); err != nil {
				fmt.Fprintf(os.Stderr, "error retiring bot: %v \n", err)
			}
		}
		log.Infof("Shutdown signal received")
		cancel()
	}()

	if coreDir != defaultCoreDir && configFile == defaultConfigFilePath {
		configFile = filepath.Join(coreDir, defaultConfigFileName)
	}

	cfgB, err := os.ReadFile(configFile)
	if err != nil {
		return err
	}

	var pgm *core.MakerProgram
	if err := json.Unmarshal(cfgB, &pgm); err != nil {
		return err
	}

	if manualRate > 0 {
		pgm.EmptyMarketRate = manualRate
	}

	net := dex.Mainnet
	switch {
	case simnet:
		net = dex.Simnet
	case testnet:
		net = dex.Testnet
	}

	c, err = core.New(&core.Config{
		DBPath:   filepath.Join(coreDir, net.String(), "dexc.db"),
		Net:      net,
		Logger:   loggerMaker.NewLogger("CORE"),
		Language: lang,
	})
	if err != nil {
		return err
	}

	// Get the user's password
	appPW, err := admin.PasswordPrompt(ctx, "Enter your password:")
	if err != nil {
		return err
	}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		c.Run(ctx)
		cancel() // in the event that Run returns prematurely prior to context cancellation
	}()

	<-c.Ready()

	// Check for wallets, get addresses.
	u := c.User()
	b, found := u.Assets[pgm.BaseID]
	if !found {
		return fmt.Errorf("no asset found for base asset %d", pgm.BaseID)
	}

	if b.Wallet == nil {
		return fmt.Errorf("no wallet found for base asset %d", pgm.BaseID)
	}

	q, found := u.Assets[pgm.QuoteID]
	if !found {
		return fmt.Errorf("no asset found for quote asset %d", pgm.QuoteID)
	}

	if q.Wallet == nil {
		return fmt.Errorf("no wallet found for quote asset %d", pgm.QuoteID)
	}

	xc, found := u.Exchanges[pgm.Host]
	if !found {
		return fmt.Errorf("no exchange %q", pgm.Host)
	}

	mktName := b.Symbol + "_" + q.Symbol
	_, found = xc.Markets[mktName]
	if !found {
		return fmt.Errorf("no market %q", mktName)
	}

	if _, err := c.Login(appPW); err != nil {
		return fmt.Errorf("login error: %w", err)
	}

	printStartMessage := func() {
		fmt.Printf(":::::: %s Wallet Address: %s \n", strings.ToUpper(b.Symbol), b.Wallet.Address)
		fmt.Printf(":::::: %s Wallet Address: %s \n", strings.ToUpper(q.Symbol), q.Wallet.Address)
		fmt.Println(":::::: Press Enter to begin")
	}

	printStartMessage()

	const enter = 10

	start := make(chan struct{})
	go func() {
		var keys []byte = make([]byte, 1)
		for {
			os.Stdin.Read(keys)
			key := keys[0]
			if key == enter {
				close(start)
			}
		}
	}()

out:
	for {
		select {
		case <-start:
			break out
		case <-time.After(time.Second * 30):
			printStartMessage()
		case <-ctx.Done():
			return nil
		}
	}

	pgmID, err = c.CreateBot(appPW, core.MakerBotV0, pgm)
	if err != nil {
		return fmt.Errorf("CreateBot error: %w", err)
	}

	wg.Wait()

	return nil
}
