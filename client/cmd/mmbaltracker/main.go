package main

/*
 * Starts a process that repeatedly calls the mmavailablebalances command to
 * to check for changes in the available balances for market making on the
 * specified markets. Whenever there is a diff, it is logged. This is used
 * to check for bugs in the balance tracking logic. If there is a diff without
 * a bot being started, stopped, or updated, and the wallet is not handling
 * bonds, then there is a bug.
 */

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"time"

	"decred.org/dcrdex/client/mm"
	"decred.org/dcrdex/dex"
)

var (
	log = dex.StdOutLogger("BALTRACKER", dex.LevelDebug)
)

func printUsage() {
	fmt.Println("Usage: mmbaltracker <configpath> <market>")
	fmt.Println("  <configpath> is the path to the market making configuration file.")
	fmt.Println("  <market> is a market in the form <host>-<baseassetid>-<quoteassetid>. You can specify multiple markets.")
}

func parseMkt(mkt string) (*mm.MarketWithHost, error) {
	parts := strings.Split(mkt, "-")
	if len(parts) != 3 {
		return nil, fmt.Errorf("invalid market format")
	}

	host := parts[0]
	baseID, err := strconv.Atoi(parts[1])
	if err != nil {
		return nil, fmt.Errorf("invalid base asset ID")
	}

	quoteID, err := strconv.Atoi(parts[2])
	if err != nil {
		return nil, fmt.Errorf("invalid quote asset ID")
	}

	return &mm.MarketWithHost{
		Host:    host,
		BaseID:  uint32(baseID),
		QuoteID: uint32(quoteID),
	}, nil
}

type balances struct {
	DEXBalances map[uint32]uint64 `json:"dexBalances"`
	CEXBalances map[uint32]uint64 `json:"cexBalances"`
}

func getAvailableBalances(mkt *mm.MarketWithHost, configPath string) (bals *balances, err error) {
	cmd := exec.Command("dexcctl", "mmavailablebalances", configPath,
		mkt.Host, strconv.Itoa(int(mkt.BaseID)), strconv.Itoa(int(mkt.QuoteID)))
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("error getting available balances: %v", err)
	}

	bals = new(balances)
	err = json.Unmarshal(out, bals)
	if err != nil {
		return nil, fmt.Errorf("error unmarshalling available balances: %v", err)
	}

	return bals, nil
}

func main() {
	if len(os.Args) < 3 {
		printUsage()
		os.Exit(1)
	}

	configPath := os.Args[1]

	currBalances := make(map[mm.MarketWithHost]*balances, len(os.Args)-2)
	for i := 2; i < len(os.Args); i++ {
		mkt, err := parseMkt(os.Args[i])
		if err != nil {
			log.Errorf("Error parsing market: %v\n", err)
			os.Exit(1)
		}

		currBalances[*mkt], err = getAvailableBalances(mkt, configPath)
		if err != nil {
			log.Errorf("Error getting initial balances: %v\n", err)
			os.Exit(1)
		}
	}

	log.Infof("Initial Balances:")
	for mkt, bals := range currBalances {
		log.Infof("Market: %s-%d-%d", mkt.Host, mkt.BaseID, mkt.QuoteID)
		log.Infof("  DEX Balances:")
		for assetID, bal := range bals.DEXBalances {
			log.Infof("    %d: %d", assetID, bal)
		}
		log.Infof("  CEX Balances:")
		for assetID, bal := range bals.CEXBalances {
			log.Infof("    %d: %d", assetID, bal)
		}
	}

	type diff struct {
		assetID uint32
		oldBal  uint64
		newBal  uint64
	}

	checkForDiffs := func(mkt *mm.MarketWithHost) {
		newBals, err := getAvailableBalances(mkt, configPath)
		if err != nil {
			log.Errorf("Error getting balances: %v\n", err)
			return
		}

		dexDiffs := make([]*diff, 0)
		cexDiffs := make([]*diff, 0)

		for assetID, newBal := range newBals.DEXBalances {
			oldBal := currBalances[*mkt].DEXBalances[assetID]
			if oldBal != newBal {
				dexDiffs = append(dexDiffs, &diff{assetID, oldBal, newBal})
			}
			currBalances[*mkt].DEXBalances[assetID] = newBal
		}
		for assetID, newBal := range newBals.CEXBalances {
			oldBal := currBalances[*mkt].CEXBalances[assetID]
			if oldBal != newBal {
				cexDiffs = append(cexDiffs, &diff{assetID, oldBal, newBal})
			}
			currBalances[*mkt].CEXBalances[assetID] = newBal
		}

		var logStr strings.Builder

		if len(dexDiffs) > 0 || len(cexDiffs) > 0 {
			logStr.WriteString("================================================\n")
			logStr.WriteString(fmt.Sprintf("\nDiffs on Market: %s-%d-%d", mkt.Host, mkt.BaseID, mkt.QuoteID))
			if len(dexDiffs) > 0 {
				logStr.WriteString("\n  DEX diffs:")
				for _, d := range dexDiffs {
					logStr.WriteString(fmt.Sprintf("\n    %s: %d -> %d (%d)", dex.BipIDSymbol(d.assetID), d.oldBal, d.newBal, int64(d.newBal)-int64(d.oldBal)))
				}
			}

			if len(cexDiffs) > 0 {
				logStr.WriteString("\n  CEX diffs:")
				for _, d := range cexDiffs {
					logStr.WriteString(fmt.Sprintf("\n    %s: %d -> %d (%d)", dex.BipIDSymbol(d.assetID), d.oldBal, d.newBal, int64(d.newBal)-int64(d.oldBal)))
				}
			}
			logStr.WriteString("\n\n")
			log.Infof(logStr.String())
		}
	}

	ctx, cancel := context.WithCancel(context.Background())

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt)
	go func() {
		<-sigChan
		cancel()
	}()

	timer := time.NewTicker(time.Second * 2)
	for {
		select {
		case <-timer.C:
			for mkt := range currBalances {
				checkForDiffs(&mkt)
			}
		case <-ctx.Done():
			log.Infof("Exiting...")
			os.Exit(0)
		}
	}
}
