package feerates

import (
	"time"
)

type Config struct {
	TatumAPIKey       string `long:"tatumAPIKey" description:"Free Mainnet or Testnet API key from tatum.io."`
	BlockDeamonAPIKey string `long:"blockDeamonAPIKey" description:"Free API key from blockdaemon.com, refresh every ~6 months."`
}

type Estimate struct {
	Value       uint64
	LastUpdated time.Time
}

type feeRateSourceCount struct {
	totalSource int
	totalFee    uint64
}
