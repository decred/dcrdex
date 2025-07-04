package base

import (
	"decred.org/dcrdex/dex"
)

// These are the chain IDs of the various base networks.
const (
	MainnetChainID = 8453
	TestnetChainID = 84532
)

var (
	// ChainIDs is a map of the network name to it's chain ID.
	ChainIDs = map[dex.Network]int64{
		dex.Mainnet: MainnetChainID,
		dex.Testnet: TestnetChainID,
	}
)
