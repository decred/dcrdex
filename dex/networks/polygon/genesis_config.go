package polygon

import (
	"embed"
	"encoding/json"
	"fmt"
	"math/big"

	"decred.org/dcrdex/dex"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/params"
)

// The allocs directory contains the genesis allocations for the various polygon
// networks. See: https://github.com/maticnetwork/bor/tree/develop/core/allocs.

//go:embed allocs
var allocs embed.FS

// These are the chain IDs of the various polygon network.
const (
	MainnetChainID = 137
	TestnetChainID = 80001 // Mumbai
	SimnetChainID  = 90001
)

var (
	// ChainIDs is a map of the network name to it's chain ID.
	ChainIDs = map[dex.Network]int64{
		dex.Mainnet: MainnetChainID,
		dex.Testnet: TestnetChainID,
		dex.Simnet:  SimnetChainID,
	}

	// mumbaiChainConfig contains the chain parameters to run a node on the
	// Mumbai test network. This is a copy of
	// https://github.com/maticnetwork/bor/blob/891ec7fef619cac0a796e3e29d5b2d4c095bdd9b/params/config.go#L336.
	mumbaiChainConfig = &params.ChainConfig{
		ChainID:             big.NewInt(80001),
		HomesteadBlock:      big.NewInt(0),
		DAOForkBlock:        nil,
		DAOForkSupport:      true,
		EIP150Block:         big.NewInt(0),
		EIP155Block:         big.NewInt(0),
		EIP158Block:         big.NewInt(0),
		ByzantiumBlock:      big.NewInt(0),
		ConstantinopleBlock: big.NewInt(0),
		PetersburgBlock:     big.NewInt(0),
		IstanbulBlock:       big.NewInt(2722000),
		MuirGlacierBlock:    big.NewInt(2722000),
		BerlinBlock:         big.NewInt(13996000),
		LondonBlock:         big.NewInt(22640000),
	}

	// Called BorTestChainConfig in maticnetwork/bor/params/config.go
	AmoyChainConfig = &params.ChainConfig{
		ChainID:             big.NewInt(80002),
		HomesteadBlock:      big.NewInt(0),
		DAOForkBlock:        nil,
		DAOForkSupport:      true,
		EIP150Block:         big.NewInt(0),
		EIP155Block:         big.NewInt(0),
		EIP158Block:         big.NewInt(0),
		ByzantiumBlock:      big.NewInt(0),
		ConstantinopleBlock: big.NewInt(0),
		PetersburgBlock:     big.NewInt(0),
		IstanbulBlock:       big.NewInt(0),
		MuirGlacierBlock:    big.NewInt(0),
		BerlinBlock:         big.NewInt(0),
		LondonBlock:         big.NewInt(73100),
		// ShanghaiBlock:       big.NewInt(73100),
		// CancunBlock:         big.NewInt(5423600),
	}

	// BorMainnetChainConfig is the genesis block of the Bor Mainnet network.
	// This is a copy of the genesis block from
	// https://github.com/maticnetwork/bor/blob/891ec7fef619cac0a796e3e29d5b2d4c095bdd9b/params/config.go#L395.
	BorMainnetChainConfig = &params.ChainConfig{
		ChainID:             big.NewInt(137),
		HomesteadBlock:      big.NewInt(0),
		DAOForkBlock:        nil,
		DAOForkSupport:      true,
		EIP150Block:         big.NewInt(0),
		EIP155Block:         big.NewInt(0),
		EIP158Block:         big.NewInt(0),
		ByzantiumBlock:      big.NewInt(0),
		ConstantinopleBlock: big.NewInt(0),
		PetersburgBlock:     big.NewInt(0),
		IstanbulBlock:       big.NewInt(3395000),
		MuirGlacierBlock:    big.NewInt(3395000),
		BerlinBlock:         big.NewInt(14750000),
		LondonBlock:         big.NewInt(23850000),
	}
)

// DefaultAmoyGenesisBlock returns the Mumbai network genesis block. See:
// https://github.com/maticnetwork/bor/blob/46f93b1f9415c8778d6e5ea2f3fad440f4572ba5/core/genesis.go#L616
func DefaultAmoyGenesisBlock() *core.Genesis {
	return &core.Genesis{
		Config:     AmoyChainConfig,
		Nonce:      0,
		Timestamp:  1700225065,
		GasLimit:   10000000,
		Difficulty: big.NewInt(1),
		Mixhash:    common.HexToHash("0x0000000000000000000000000000000000000000000000000000000000000000"),
		Coinbase:   common.HexToAddress("0x0000000000000000000000000000000000000000"),
		Alloc:      readPrealloc("allocs/amoy.json"),
	}
}

// defaultMumbaiGenesisBlock returns the Mumbai network genesis block. See:
// https://github.com/maticnetwork/bor/blob/891ec7fef619cac0a796e3e29d5b2d4c095bdd9b/core/genesis.go#L495
func defaultMumbaiGenesisBlock() *core.Genesis {
	return &core.Genesis{
		Config:     mumbaiChainConfig,
		Nonce:      0,
		Timestamp:  1558348305,
		GasLimit:   10000000,
		Difficulty: big.NewInt(1),
		Mixhash:    common.HexToHash("0x0000000000000000000000000000000000000000000000000000000000000000"),
		Coinbase:   common.HexToAddress("0x0000000000000000000000000000000000000000"),
		Alloc:      readPrealloc("allocs/mumbai.json"),
	}
}

// DefaultBorMainnetGenesisBlock returns the Bor Mainnet network gensis block. See:
// https://github.com/maticnetwork/bor/blob/891ec7fef619cac0a796e3e29d5b2d4c095bdd9b/core/genesis.go#L509
func DefaultBorMainnetGenesisBlock() *core.Genesis {
	return &core.Genesis{
		Config:     BorMainnetChainConfig,
		Nonce:      0,
		Timestamp:  1590824836,
		GasLimit:   10000000,
		Difficulty: big.NewInt(1),
		Mixhash:    common.HexToHash("0x0000000000000000000000000000000000000000000000000000000000000000"),
		Coinbase:   common.HexToAddress("0x0000000000000000000000000000000000000000"),
		Alloc:      readPrealloc("allocs/bor_mainnet.json"),
	}
}

func readPrealloc(filename string) types.GenesisAlloc {
	f, err := allocs.Open(filename)
	if err != nil {
		panic(fmt.Sprintf("Could not open genesis preallocation for %s: %v", filename, err))
	}
	defer f.Close()
	decoder := json.NewDecoder(f)
	ga := make(types.GenesisAlloc)
	err = decoder.Decode(&ga)
	if err != nil {
		panic(fmt.Sprintf("Could not parse genesis preallocation for %s: %v", filename, err))
	}
	return ga
}
