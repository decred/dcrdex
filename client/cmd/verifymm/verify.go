// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"

	"decred.org/dcrdex/dex/msgjson"
	"github.com/decred/dcrd/dcrec/secp256k1/v4"
	"github.com/decred/dcrd/dcrec/secp256k1/v4/ecdsa"
)

// EpochDetail contains the verification result for a single epoch.
type EpochDetail struct {
	EpochIdx     uint64  `json:"epochIdx"`
	SigValid     bool    `json:"sigValid"`
	BuyQty       uint64  `json:"buyQty"`
	SellQty      uint64  `json:"sellQty"`
	MeetsBuyQty  bool    `json:"meetsBuyQty"`
	MeetsSellQty bool    `json:"meetsSellQty"`
	BestBuy      uint64  `json:"bestBuy"`
	BestSell     uint64  `json:"bestSell"`
	SpreadPct    float64 `json:"spreadPct,omitempty"`
	WithinSpread bool    `json:"withinSpread"`
}

// Report is the output of the verification process.
type Report struct {
	TotalEpochs   int           `json:"totalEpochs"`
	ValidSigs     int           `json:"validSigs"`
	InvalidSigs   int           `json:"invalidSigs"`
	CoveredEpochs int           `json:"coveredEpochs"`
	CoveragePct   float64       `json:"coveragePct"`
	WithinSpread  int           `json:"withinSpreadEpochs"`
	AvgSpreadPct  float64       `json:"avgSpreadPct,omitempty"`
	Epochs        []EpochDetail `json:"epochs"`
}

func verify(snapshotsData []byte, pubkeyHex string, startEpoch, endEpoch, minQty uint64, maxSpreadPct float64) (*Report, error) {
	pkBytes, err := hex.DecodeString(pubkeyHex)
	if err != nil {
		return nil, fmt.Errorf("error decoding pubkey hex: %w", err)
	}
	pubKey, err := secp256k1.ParsePubKey(pkBytes)
	if err != nil {
		return nil, fmt.Errorf("error parsing pubkey: %w", err)
	}

	var snaps []*msgjson.MMEpochSnapshot
	if err := json.Unmarshal(snapshotsData, &snaps); err != nil {
		return nil, fmt.Errorf("error unmarshaling snapshots: %w", err)
	}

	// Validate that all snapshots belong to the same market and account.
	if len(snaps) > 0 {
		base, quote := snaps[0].Base, snaps[0].Quote
		acctID := string(snaps[0].AccountID)
		for i, snap := range snaps[1:] {
			if snap.Base != base || snap.Quote != quote {
				return nil, fmt.Errorf("snapshot %d has market %d-%d, expected %d-%d", i+1, snap.Base, snap.Quote, base, quote)
			}
			if string(snap.AccountID) != acctID {
				return nil, fmt.Errorf("snapshot %d has different account ID", i+1)
			}
		}
	}

	report := &Report{}

	var spreadSum float64
	var spreadCount int
	seen := make(map[uint64]bool)

	for _, snap := range snaps {
		if startEpoch > 0 && snap.EpochIdx < startEpoch {
			continue
		}
		if endEpoch > 0 && snap.EpochIdx > endEpoch {
			continue
		}

		if seen[snap.EpochIdx] {
			return nil, fmt.Errorf("duplicate epoch index %d", snap.EpochIdx)
		}
		seen[snap.EpochIdx] = true

		detail := EpochDetail{
			EpochIdx: snap.EpochIdx,
			BestBuy:  snap.BestBuy,
			BestSell: snap.BestSell,
		}

		// Verify signature.
		detail.SigValid = verifySig(pubKey, snap)

		if detail.SigValid {
			report.ValidSigs++
		} else {
			report.InvalidSigs++
		}

		// Calculate total qty per side.
		for _, o := range snap.BuyOrders {
			detail.BuyQty += o.Qty
		}
		for _, o := range snap.SellOrders {
			detail.SellQty += o.Qty
		}

		detail.MeetsBuyQty = detail.BuyQty >= minQty
		detail.MeetsSellQty = detail.SellQty >= minQty
		if detail.MeetsBuyQty && detail.MeetsSellQty && detail.SigValid {
			report.CoveredEpochs++
		}

		// Spread assessment.
		if snap.BestBuy > 0 && snap.BestSell > 0 && snap.BestSell >= snap.BestBuy {
			mid := (float64(snap.BestBuy) + float64(snap.BestSell)) / 2
			spread := float64(snap.BestSell-snap.BestBuy) / mid * 100
			detail.SpreadPct = math.Round(spread*100) / 100
			spreadSum += spread
			spreadCount++
			if maxSpreadPct <= 0 || detail.SpreadPct <= maxSpreadPct {
				detail.WithinSpread = true
				report.WithinSpread++
			}
		}

		report.Epochs = append(report.Epochs, detail)
	}

	report.TotalEpochs = len(report.Epochs)
	if report.TotalEpochs > 0 {
		report.CoveragePct = math.Round(float64(report.CoveredEpochs)/float64(report.TotalEpochs)*10000) / 100
	}
	if spreadCount > 0 {
		report.AvgSpreadPct = math.Round(spreadSum/float64(spreadCount)*100) / 100
	}

	return report, nil
}

func verifySig(pubKey *secp256k1.PublicKey, snap *msgjson.MMEpochSnapshot) bool {
	if len(snap.Sig) == 0 {
		return false
	}
	sig, err := ecdsa.ParseDERSignature(snap.Sig)
	if err != nil {
		return false
	}
	hash := sha256.Sum256(snap.Serialize())
	return sig.Verify(hash[:], pubKey)
}
