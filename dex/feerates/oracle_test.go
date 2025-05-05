package feerates

import (
	"context"
	"fmt"
	"testing"
	"time"

	"decred.org/dcrdex/client/asset/bch"
	"decred.org/dcrdex/client/asset/btc"
	"decred.org/dcrdex/client/asset/dash"
	"decred.org/dcrdex/client/asset/dcr"
	"decred.org/dcrdex/client/asset/doge"
	"decred.org/dcrdex/client/asset/eth"
	"decred.org/dcrdex/client/asset/ltc"
	"decred.org/dcrdex/client/asset/zec"
	"decred.org/dcrdex/dex"
)

func TestOracleRun(t *testing.T) {
	chains := []uint32{btc.BipID, ltc.BipID, eth.BipID, doge.BipID, dcr.BipID, bch.BipID, zec.BipID, dash.BipID}
	listener := make(chan map[uint32]*Estimate)
	o, err := NewOracle(dex.Mainnet, Config{}, chains, listener)
	if err != nil {
		t.Fatalf("failed to create oracle: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start the oracle in a separate goroutine.
	go o.Run(ctx, dex.StdOutLogger("T", dex.LevelTrace))

	for {
		select {
		case <-time.After(60 * time.Second):
			// Simulate a timeout to avoid blocking forever.
			fmt.Println("Timeout waiting for updates, exiting.")
			return
		case res := <-listener:
			if len(res) != len(chains) {
				fmt.Printf("Expected %d estimates, got %d\n", len(chains), len(res))
			}

			for _, chainID := range chains {
				if estimate, ok := res[chainID]; !ok {
					fmt.Printf("Missing estimate for chainID %d (%s)\n", chainID, dex.BipIDSymbol(chainID))
				} else {
					if estimate == nil || estimate.Value == 0 {
						t.Fatalf("invalid estimate for chainID %d (%s): %v", chainID, dex.BipIDSymbol(chainID), estimate)
					}
					if estimate.LastUpdated.IsZero() {
						t.Fatalf("last updated time is zero for chainID %d (%s)", chainID, dex.BipIDSymbol(chainID))
					}

					// Print the estimate for demonstration purposes. Fee rate
					// estimate values are in atoms for dcr, gwei for ethereum,
					// satoshis for bitcoin and bitcoin clone blockchains (per
					// byte sat), or the lowest non-divisible unit in other
					// non-Bitcoin blockchains.
					fmt.Printf("ChainID: %d (%s), Fee Rate: %d, Last Updated: %s\n", chainID, dex.BipIDSymbol(chainID), estimate.Value, estimate.LastUpdated)
				}
			}
			return
		default: // Continue waiting for updates.
		}
	}
}
