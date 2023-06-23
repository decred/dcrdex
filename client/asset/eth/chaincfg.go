// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package eth

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strings"

	"decred.org/dcrdex/dex"
	dexeth "decred.org/dcrdex/dex/networks/eth"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	ethcore "github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/eth/ethconfig"
	"github.com/ethereum/go-ethereum/params"
)

// CompatibilityData is some addresses and hashes used for validating an RPC
// APIs compatibility with trading.
type CompatibilityData struct {
	Addr      common.Address
	TokenAddr common.Address
	TxHash    common.Hash
	BlockHash common.Hash
}

var (
	mainnetCompatibilityData = CompatibilityData{
		// Vitalik's address from https://twitter.com/VitalikButerin/status/1050126908589887488
		Addr:      common.HexToAddress("0xab5801a7d398351b8be11c439e05c5b3259aec9b"),
		TokenAddr: common.HexToAddress("0xa0b86991c6218b36c1d19d4a2e9eb0ce3606eb48"),
		TxHash:    common.HexToHash("0xea1a717af9fad5702f189d6f760bb9a5d6861b4ee915976fe7732c0c95cd8a0e"),
		BlockHash: common.HexToHash("0x44ebd6f66b4fd546bccdd700869f6a433ef9a47e296a594fa474228f86eeb353"),
	}

	testnetCompatibilityData = CompatibilityData{
		Addr:      common.HexToAddress("0x8879F72728C5eaf5fB3C55e6C3245e97601FBa32"),
		TokenAddr: common.HexToAddress("0x07865c6E87B9F70255377e024ace6630C1Eaa37F"),
		TxHash:    common.HexToHash("0x4e1d455f7eac7e3a5f7c1e0989b637002755eaee3a262f90b0f3aef1f1c4dcf0"),
		BlockHash: common.HexToHash("0x8896021c2666303a85b7e4a6a6f2b075bc705d4e793bf374cd44b83bca23ef9a"),
	}
)

// NetworkCompatibilityData returns the CompatibilityData for the specified
// network. If using simnet, make sure the simnet harness is running.
func NetworkCompatibilityData(net dex.Network) (c CompatibilityData, err error) {
	switch net {
	case dex.Mainnet:
		return mainnetCompatibilityData, nil
	case dex.Testnet:
		return testnetCompatibilityData, nil
	case dex.Simnet:
	default:
		return c, nil
	}
	// simnet
	tDir, err := simnetDataDir()
	if err != nil {
		return
	}

	addr := common.HexToAddress("18d65fb8d60c1199bb1ad381be47aa692b482605")
	var (
		tTxHashFile    = filepath.Join(tDir, "test_tx_hash.txt")
		tBlockHashFile = filepath.Join(tDir, "test_block10_hash.txt")
		tContractFile  = filepath.Join(tDir, "test_token_contract_address.txt")
	)
	readIt := func(path string) string {
		b, err := os.ReadFile(path)
		if err != nil {
			panic(fmt.Sprintf("Problem reading simnet testing file %q: %v", path, err))
		}
		return strings.TrimSpace(string(b)) // mainly the trailing "\r\n"
	}
	return CompatibilityData{
		Addr:      addr,
		TokenAddr: common.HexToAddress(readIt(tContractFile)),
		TxHash:    common.HexToHash(readIt(tTxHashFile)),
		BlockHash: common.HexToHash(readIt(tBlockHashFile)),
	}, nil
}

// simnetDataDir returns the data directory for Ethereum simnet.
func simnetDataDir() (string, error) {
	u, err := user.Current()
	if err != nil {
		return "", fmt.Errorf("error getting current user: %w", err)
	}

	return filepath.Join(u.HomeDir, "dextest", "eth"), nil
}

// ChainConfig returns chain configuration for the specified network.
func ChainConfig(net dex.Network) (c ethconfig.Config, err error) {
	c = ethconfig.Defaults
	switch net {
	// Ethereum
	case dex.Testnet:
		c.Genesis = core.DefaultGoerliGenesisBlock()
	case dex.Mainnet:
		c.Genesis = core.DefaultGenesisBlock()
	case dex.Simnet:
		c.Genesis, err = readSimnetGenesisFile()
		if err != nil {
			return c, fmt.Errorf("readSimnetGenesisFile error: %w", err)
		}
	default:
		return c, fmt.Errorf("unknown network %d", net)

	}
	c.NetworkId = c.Genesis.Config.ChainID.Uint64()
	return
}

// ChainGenesis returns the genesis parameters for the specified network.
func ChainGenesis(net dex.Network) (c *params.ChainConfig, err error) {
	cfg, err := ChainConfig(net)
	if err != nil {
		return nil, err
	}
	return cfg.Genesis.Config, nil
}

// readSimnetGenesisFile reads the simnet genesis file.
func readSimnetGenesisFile() (*ethcore.Genesis, error) {
	dataDir, err := simnetDataDir()
	if err != nil {
		return nil, err
	}

	genesisFile := filepath.Join(dataDir, "genesis.json")
	genesisCfg, err := dexeth.LoadGenesisFile(genesisFile)
	if err != nil {
		return nil, fmt.Errorf("error reading genesis file: %v", err)
	}

	return genesisCfg, nil
}
