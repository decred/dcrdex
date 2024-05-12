package txfee

import (
	"time"
)

type Config struct {
	TatumMainnetAPIKey string `long:"tatumMainnetAPIKey" description:"Free Mainnet API key from tatum.io. Optional."`
	TatumTestnetAPIKey string `long:"tatumTestnetAPIKey" description:"Free Testnet API key from tatum.io."`
	BlockDeamonAPIKey  string `long:"blockDeamonAPIKey" description:"Free API key from blockdaemon.com, refresh every ~6 months."`
}

type Estimate struct {
	Value       uint64
	LastUpdated time.Time
}

type FeeSourceCount struct {
	totalSource int
	totalFee    uint64
}

type blockchairFeeInfo struct {
	SuggestedTxFeePerBytePerSat uint64 `json:"suggested_transaction_fee_per_byte_sat"`
	SuggestedFeeGweiOptions     struct {
		Fast uint64 `json:"fast"`
	} `json:"suggested_transaction_fee_gwei_options"`
}
