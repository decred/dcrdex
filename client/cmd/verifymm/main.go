// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

// verifymm is a standalone tool that verifies exported MM epoch snapshots.
// Given a JSON file of signed snapshots, a DEX server pubkey, and various
// thresholds, it verifies signatures, calculates coverage, and assesses spread.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
)

func main() {
	snapshotsFile := flag.String("snapshots", "", "path to exported snapshots JSON file")
	pubkeyHex := flag.String("pubkey", "", "DEX server pubkey (hex-encoded, compressed)")
	startEpoch := flag.Uint64("start", 0, "start epoch index (inclusive)")
	endEpoch := flag.Uint64("end", 0, "end epoch index (inclusive)")
	minQty := flag.Uint64("qty", 0, "minimum required quantity per side (atoms)")
	maxSpreadPct := flag.Float64("maxspreadpct", 0, "max acceptable spread from market mid (%)")
	outFile := flag.String("out", "", "output report path (JSON)")

	flag.Parse()

	if *snapshotsFile == "" || *pubkeyHex == "" {
		flag.Usage()
		os.Exit(1)
	}

	snapshotsData, err := os.ReadFile(*snapshotsFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading snapshots file: %v\n", err)
		os.Exit(1)
	}

	report, err := verify(snapshotsData, *pubkeyHex, *startEpoch, *endEpoch, *minQty, *maxSpreadPct)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Verification error: %v\n", err)
		os.Exit(1)
	}

	reportJSON, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error marshaling report: %v\n", err)
		os.Exit(1)
	}

	if *outFile != "" {
		if err := os.WriteFile(*outFile, reportJSON, 0600); err != nil {
			fmt.Fprintf(os.Stderr, "Error writing report: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Report written to %s\n", *outFile)
	} else {
		fmt.Println(string(reportJSON))
	}
}
